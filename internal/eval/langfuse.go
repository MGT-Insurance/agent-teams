package eval

import (
	"bytes"
	"crypto/rand"
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
//	POST {host}/api/public/v2/datasets       — ensure dataset (idempotent by name)
//	POST {host}/api/public/dataset-items     — upsert dataset item (idempotent by id)
//	POST {host}/api/public/ingestion         — batch: trace-create + score-create
//	POST {host}/api/public/dataset-run-items — link a trace to an item under a run name
type langfuseClient struct {
	host       string
	publicKey  string
	secretKey  string
	httpClient *http.Client
}

// Push creates/reuses a dataset item for task, records an experiment run
// tagged by cfg.Name+Hash(), and attaches metrics + judge as scores. See the
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
	if err := c.ensureDataset(); err != nil {
		return fmt.Errorf("eval: langfuse push: ensure dataset: %w", err)
	}
	if err := c.upsertDatasetItem(task); err != nil {
		return fmt.Errorf("eval: langfuse push: upsert dataset item: %w", err)
	}
	if err := c.pushTraceAndScores(res, task); err != nil {
		return fmt.Errorf("eval: langfuse push: push trace/scores: %w", err)
	}
	runName := res.Config.Name + "-" + res.Config.Hash()
	if err := c.linkDatasetRunItem(runName, task.ID, res.RunID); err != nil {
		return fmt.Errorf("eval: langfuse push: link dataset run item: %w", err)
	}
	return nil
}

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
func (c *langfuseClient) ensureDataset() error {
	return c.post("/api/public/v2/datasets", map[string]any{"name": langfuseDataset}, nil)
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

// pushTraceAndScores creates a trace representing this run (id = res.RunID,
// so re-pushing the same run updates the same trace) and attaches the seven
// canonical scores to it, in a single ingestion batch call.
func (c *langfuseClient) pushTraceAndScores(res RunResult, task TaskSpec) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	events := []map[string]any{
		{
			"id":        newEventID(),
			"timestamp": now,
			"type":      "trace-create",
			"body": map[string]any{
				"id":     res.RunID,
				"name":   res.TaskID,
				"input":  task.Problem,
				"output": res.Judge.Rationale,
				"tags":   []string{res.Config.Name, res.Config.Hash()},
				"metadata": map[string]any{
					"config": res.Config,
					"taskId": res.TaskID,
				},
			},
		},
	}
	for _, s := range canonicalScores(res) {
		events = append(events, map[string]any{
			"id":        newEventID(),
			"timestamp": now,
			"type":      "score-create",
			"body": map[string]any{
				"traceId":  res.RunID,
				"name":     s.name,
				"value":    s.value,
				"dataType": s.dataType,
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

// linkDatasetRunItem records the experiment run: it links res.RunID's trace
// to task's dataset item under runName (cfg.Name+Hash()). Two runs of the
// same task under different configs produce two distinct run names linked
// to the same datasetItemId — the comparison primitive.
func (c *langfuseClient) linkDatasetRunItem(runName, datasetItemID, traceID string) error {
	body := map[string]any{
		"runName":       runName,
		"datasetItemId": datasetItemID,
		"traceId":       traceID,
	}
	return c.post("/api/public/dataset-run-items", body, nil)
}

// post issues a JSON POST to path with HTTP Basic Auth (public key as
// username, secret key as password, per Langfuse's public API auth
// scheme), decoding a 2xx response into out (if non-nil) and returning a
// descriptive error otherwise.
func (c *langfuseClient) post(path string, body, out any) error {
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
