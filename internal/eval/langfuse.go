package eval

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Canonical Langfuse score names frozen by the eval-harness contract
// (agent-teams-grft.1, LANGFUSE MAPPING). L3 emits these metrics under
// these exact names; do not invent variants.
const (
	scoreCostUSD            = "cost_usd"
	scoreNTotalTokens       = "n_total_tokens"
	scoreNToolcalls         = "n_toolcalls"
	scoreNTurns             = "n_turns"
	scoreLatencyS           = "latency_s"
	scoreCorrectness        = "correctness"
	scoreObjectiveFloorPass = "objective_floor_pass"
)

// langfuseDataset is the single dataset the eval task corpus is pushed
// under; every TaskSpec becomes one item in it.
const langfuseDataset = "agent-teams-eval"

const langfuseHTTPTimeout = 30 * time.Second

// langfuseClient is the minimal REST client Push needs against Langfuse's
// public API (https://api.reference.langfuse.com), verified against the
// Fern-generated langfuse-python client (which mirrors the OpenAPI spec):
//
//	POST {host}/api/public/v2/datasets       — ensure dataset (idempotent by name); returns the dataset id
//	POST {host}/api/public/dataset-items     — upsert dataset item (idempotent by id)
//	POST {host}/api/public/otel/v1/traces    — one OTLP/JSON span: the trace root, carrying the
//	                                           langfuse.experiment.* attributes that link it to the item
//	POST {host}/api/public/ingestion         — batch: score-create only
//
// Langfuse Cloud retires the legacy trace ingestion event and the legacy
// dataset run-item link endpoint on November 16, 2026; the OTLP span
// replaces both.
type langfuseClient struct {
	host       string
	publicKey  string
	secretKey  string
	httpClient *http.Client
}

// Push creates/reuses a dataset item for task, records the run as one OTLP
// span carrying experiment attributes (experiment name = cfg.Name+"-"+Hash()),
// and attaches metrics + judge as scores. See the
// LANGFUSE MAPPING section of agent-teams-grft.1 for the canonical score
// names: cost_usd, n_total_tokens, n_toolcalls, n_turns, latency_s,
// correctness (numeric); objective_floor_pass (boolean).
//
// Credentials arrive via env: LANGFUSE_HOST, LANGFUSE_PUBLIC_KEY,
// LANGFUSE_SECRET_KEY. A missing credential fails loudly rather than
// silently no-op'ing.
func Push(res RunResult, task TaskSpec) error {
	c, err := newLangfuseClientFromEnv()
	if err != nil {
		return err
	}
	return c.push(res, task)
}

// langfuseConfigured reports whether all three Langfuse env vars are set.
// Collect uses this to decide whether Langfuse push is even attempted:
// Langfuse is optional infrastructure, never required to run the eval
// suite (agent-teams-grft.7 scope note from Eric).
func langfuseConfigured() bool {
	return os.Getenv("LANGFUSE_HOST") != "" &&
		os.Getenv("LANGFUSE_PUBLIC_KEY") != "" &&
		os.Getenv("LANGFUSE_SECRET_KEY") != ""
}

func newLangfuseClientFromEnv() (*langfuseClient, error) {
	host := os.Getenv("LANGFUSE_HOST")
	publicKey := os.Getenv("LANGFUSE_PUBLIC_KEY")
	secretKey := os.Getenv("LANGFUSE_SECRET_KEY")

	var missing []string
	if host == "" {
		missing = append(missing, "LANGFUSE_HOST")
	}
	if publicKey == "" {
		missing = append(missing, "LANGFUSE_PUBLIC_KEY")
	}
	if secretKey == "" {
		missing = append(missing, "LANGFUSE_SECRET_KEY")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("eval: langfuse push: missing required env var(s): %s", strings.Join(missing, ", "))
	}

	return &langfuseClient{
		host:       strings.TrimRight(host, "/"),
		publicKey:  publicKey,
		secretKey:  secretKey,
		httpClient: &http.Client{Timeout: langfuseHTTPTimeout},
	}, nil
}

func (c *langfuseClient) push(res RunResult, task TaskSpec) error {
	datasetID, err := c.ensureDataset()
	if err != nil {
		return fmt.Errorf("eval: langfuse push: ensure dataset: %w", err)
	}
	if err := c.upsertDatasetItem(task); err != nil {
		return fmt.Errorf("eval: langfuse push: upsert dataset item: %w", err)
	}
	if err := c.exportSpan(res, task, datasetID); err != nil {
		return fmt.Errorf("eval: langfuse push: export span: %w", err)
	}
	if err := c.pushScores(res); err != nil {
		return fmt.Errorf("eval: langfuse push: push scores: %w", err)
	}
	return nil
}

// sha256Hex returns the lowercase hex of the first n bytes of
// sha256(utf8(s)) starting at offset off.
func sha256Hex(s string, off, n int) string {
	d := sha256.Sum256([]byte(s))
	return hex.EncodeToString(d[off : off+n])
}

// traceIDFor derives the 32-hex OTLP trace id from runID: sha256(runID)[0:16].
// This equals the Langfuse SDK create_trace_id(seed=runID). RunID itself is
// not a valid OTLP trace id.
func traceIDFor(runID string) string { return sha256Hex(runID, 0, 16) }

// spanIDFor derives the 16-hex root span id from runID: sha256(runID)[16:24].
func spanIDFor(runID string) string { return sha256Hex(runID, 16, 8) }

// experimentIDFor derives the 32-hex experiment id from runName:
// sha256(runName)[0:16], so every run under one config shares an experiment.
func experimentIDFor(runName string) string { return sha256Hex(runName, 0, 16) }

// ensureDataset creates the corpus dataset. Verified against the Langfuse
// server route (createDatasetForApi -> upsertDataset in
// langfuse/langfuse@web/src/features/datasets/server/actions/createDataset.ts):
// upsertDataset performs a genuine DB-level prisma.dataset.upsert keyed on
// (projectId, name) whenever the request omits "id" (as this call always
// does), returning 200 on every call including repeats with the same name.
// The "Dataset name already in use" 409 path only fires when the caller
// passes an explicit id that collides with a differently-named dataset,
// which this call never does. So calling this repeatedly with the same name
// is genuinely idempotent and no separate get-first check is needed.
// Returns the dataset id from the response; an empty id is an error because
// the experiment span attributes need it.
func (c *langfuseClient) ensureDataset() (string, error) {
	var ds struct {
		ID string `json:"id"`
	}
	if err := c.post("/api/public/v2/datasets", map[string]any{"name": langfuseDataset}, &ds); err != nil {
		return "", err
	}
	if ds.ID == "" {
		return "", fmt.Errorf("dataset response has no id")
	}
	return ds.ID, nil
}

// upsertDatasetItem maps TaskSpec -> dataset item per the CONTRACT: input =
// Problem + FixtureRef; expectedOutput = AcceptanceCriteria. Dataset items
// are upserted on id, so task.ID doubles as the stable dataset-item id.
func (c *langfuseClient) upsertDatasetItem(task TaskSpec) error {
	body := map[string]any{
		"datasetName": langfuseDataset,
		"id":          task.ID,
		"input": map[string]any{
			"problem":    task.Problem,
			"fixtureRef": task.FixtureRef,
		},
		"expectedOutput": map[string]any{
			"acceptanceCriteria": task.AcceptanceCriteria,
		},
	}
	return c.post("/api/public/dataset-items", body, nil)
}

// otlpAttr is one OTLP/JSON KeyValue. Value is a single-key object such as
// {"stringValue": "x"} or {"arrayValue": {...}}.
type otlpAttr struct {
	Key   string         `json:"key"`
	Value map[string]any `json:"value"`
}

func otlpString(key, v string) otlpAttr {
	return otlpAttr{Key: key, Value: map[string]any{"stringValue": v}}
}

func otlpStringArray(key string, vs ...string) otlpAttr {
	vals := make([]map[string]any, 0, len(vs))
	for _, v := range vs {
		vals = append(vals, map[string]any{"stringValue": v})
	}
	return otlpAttr{Key: key, Value: map[string]any{"arrayValue": map[string]any{"values": vals}}}
}

// mustJSON marshals v for use as an OTLP string attribute. The inputs are
// plain structs and maps, so a marshal failure is a programming error.
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("eval: mustJSON: %v", err))
	}
	return string(b)
}

// buildSpanRequest builds the single-span ExportTraceServiceRequest that
// represents this run: the trace root, with the Langfuse trace and
// experiment attributes on it. Timestamps are decimal strings of Unix
// nanoseconds (OTLP/JSON encodes 64-bit ints as strings); the span starts
// WallClockSeconds before end.
func buildSpanRequest(res RunResult, task TaskSpec, datasetID string, end time.Time) map[string]any {
	traceID := traceIDFor(res.RunID)
	spanID := spanIDFor(res.RunID)
	runName := res.Config.Name + "-" + res.Config.Hash()
	start := end.Add(-time.Duration(res.Metrics.WallClockSeconds * float64(time.Second)))

	attrs := []otlpAttr{
		otlpString("langfuse.trace.name", res.TaskID),
		otlpStringArray("langfuse.trace.tags", res.Config.Name, res.Config.Hash()),
		otlpString("langfuse.trace.metadata.runId", res.RunID),
		otlpString("langfuse.trace.metadata.taskId", res.TaskID),
		otlpString("langfuse.trace.metadata.config", mustJSON(res.Config)),
		otlpString("langfuse.observation.input", mustJSON(map[string]any{
			"problem":    task.Problem,
			"fixtureRef": task.FixtureRef,
		})),
		otlpString("langfuse.observation.output", res.Judge.Rationale),
		otlpString("langfuse.experiment.id", experimentIDFor(runName)),
		otlpString("langfuse.experiment.name", runName),
		otlpString("langfuse.experiment.dataset.id", datasetID),
		otlpString("langfuse.experiment.item.id", task.ID),
		otlpString("langfuse.experiment.item.root_observation_id", spanID),
		otlpString("langfuse.experiment.item.expected_output", mustJSON(map[string]any{
			"acceptanceCriteria": task.AcceptanceCriteria,
		})),
	}

	span := map[string]any{
		"traceId":           traceID,
		"spanId":            spanID,
		"name":              res.TaskID,
		"kind":              1,
		"startTimeUnixNano": fmt.Sprintf("%d", start.UnixNano()),
		"endTimeUnixNano":   fmt.Sprintf("%d", end.UnixNano()),
		"attributes":        attrs,
	}
	return map[string]any{
		"resourceSpans": []any{map[string]any{
			"resource": map[string]any{
				"attributes": []otlpAttr{otlpString("service.name", "agent-teams-eval")},
			},
			"scopeSpans": []any{map[string]any{
				"scope": map[string]any{"name": "agent-teams-eval"},
				"spans": []any{span},
			}},
		}},
	}
}

// exportSpan sends the run's root span to Langfuse's OTLP endpoint. It
// replaces the retired legacy trace event and dataset run-item link: the
// langfuse.experiment.* attributes tie the trace to its dataset item and
// experiment. A 2xx whose body reports partialSuccess.rejectedSpans > 0 is
// an error.
func (c *langfuseClient) exportSpan(res RunResult, task TaskSpec, datasetID string) error {
	var resp struct {
		PartialSuccess struct {
			RejectedSpans json.Number `json:"rejectedSpans"`
			ErrorMessage  string      `json:"errorMessage"`
		} `json:"partialSuccess"`
	}
	req := buildSpanRequest(res, task, datasetID, time.Now().UTC())
	hdr := map[string]string{"x-langfuse-ingestion-version": "4"}
	if err := c.postWithHeaders("/api/public/otel/v1/traces", req, hdr, &resp); err != nil {
		return err
	}
	// OTLP/JSON encodes int64 as a string; accept either form.
	rejected, err := resp.PartialSuccess.RejectedSpans.Int64()
	if resp.PartialSuccess.RejectedSpans != "" && err != nil {
		return fmt.Errorf("otlp response: bad rejectedSpans %q", resp.PartialSuccess.RejectedSpans)
	}
	if rejected > 0 {
		return fmt.Errorf("otlp export rejected %d span(s): %s", rejected, resp.PartialSuccess.ErrorMessage)
	}
	return nil
}

// pushScores attaches the seven canonical scores to the run's root
// observation in a single ingestion batch call. The score id is
// traceId-name, so a same-day re-push overwrites instead of duplicating.
func (c *langfuseClient) pushScores(res RunResult) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	traceID := traceIDFor(res.RunID)
	spanID := spanIDFor(res.RunID)

	var events []map[string]any
	for _, s := range canonicalScores(res) {
		events = append(events, map[string]any{
			"id":        newEventID(),
			"timestamp": now,
			"type":      "score-create",
			"body": map[string]any{
				"id":            traceID + "-" + s.name,
				"traceId":       traceID,
				"observationId": spanID,
				"name":          s.name,
				"value":         s.value,
				"dataType":      s.dataType,
			},
		})
	}

	var ingestResp struct {
		Errors []struct {
			ID      string `json:"id"`
			Status  int    `json:"status"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.post("/api/public/ingestion", map[string]any{"batch": events}, &ingestResp); err != nil {
		return err
	}
	if len(ingestResp.Errors) > 0 {
		return fmt.Errorf("langfuse ingestion reported %d event error(s): %+v", len(ingestResp.Errors), ingestResp.Errors)
	}
	return nil
}

type langfuseScore struct {
	name     string
	value    float64
	dataType string
}

// canonicalScores maps RunResult onto the seven CONTRACT-frozen score
// names. Boolean scores are sent as 1/0 per Langfuse's ScoreBody contract
// ("Boolean score values must equal either 1 or 0").
func canonicalScores(res RunResult) []langfuseScore {
	objectiveFloorPass := 0.0
	if res.Judge.ObjectiveFloorPass {
		objectiveFloorPass = 1.0
	}
	return []langfuseScore{
		{scoreCostUSD, res.Metrics.CostUSD, "NUMERIC"},
		{scoreNTotalTokens, float64(res.Metrics.TotalTokens), "NUMERIC"},
		{scoreNToolcalls, float64(res.Metrics.ToolCallCount), "NUMERIC"},
		{scoreNTurns, float64(res.Metrics.NTurns), "NUMERIC"},
		{scoreLatencyS, res.Metrics.WallClockSeconds, "NUMERIC"},
		{scoreCorrectness, res.Judge.CorrectnessScore, "NUMERIC"},
		{scoreObjectiveFloorPass, objectiveFloorPass, "BOOLEAN"},
	}
}

// post issues a JSON POST to path with HTTP Basic Auth (public key as
// username, secret key as password, per Langfuse's public API auth
// scheme), decoding a 2xx response into out (if non-nil) and returning a
// descriptive error otherwise.
func (c *langfuseClient) post(path string, body, out any) error {
	return c.postWithHeaders(path, body, nil, out)
}

// postWithHeaders is post with extra request headers.
func (c *langfuseClient) postWithHeaders(path string, body any, headers map[string]string, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.host+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.publicKey, c.secretKey)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", http.MethodPost, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s %s: read response: %w", http.MethodPost, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: status %d: %s", http.MethodPost, path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("%s %s: decode response: %w", http.MethodPost, path, err)
		}
	}
	return nil
}

// newEventID returns a random UUID v4 string, the format Langfuse's
// ingestion envelope "id" field requires for event deduplication.
func newEventID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("eval: newEventID: crypto/rand: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
