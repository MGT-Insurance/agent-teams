package eval

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// otlpExport mirrors the slice of ExportTraceServiceRequest the tests assert on.
type otlpExport struct {
	ResourceSpans []struct {
		Resource struct {
			Attributes []otlpKV `json:"attributes"`
		} `json:"resource"`
		ScopeSpans []struct {
			Scope struct {
				Name string `json:"name"`
			} `json:"scope"`
			Spans []struct {
				TraceID           string   `json:"traceId"`
				SpanID            string   `json:"spanId"`
				ParentSpanID      string   `json:"parentSpanId"`
				Name              string   `json:"name"`
				Kind              int      `json:"kind"`
				StartTimeUnixNano string   `json:"startTimeUnixNano"`
				EndTimeUnixNano   string   `json:"endTimeUnixNano"`
				Attributes        []otlpKV `json:"attributes"`
			} `json:"spans"`
		} `json:"scopeSpans"`
	} `json:"resourceSpans"`
}

type otlpKV struct {
	Key   string `json:"key"`
	Value struct {
		StringValue *string `json:"stringValue"`
		ArrayValue  *struct {
			Values []struct {
				StringValue string `json:"stringValue"`
			} `json:"values"`
		} `json:"arrayValue"`
	} `json:"value"`
}

// otlpRequests returns the recorded OTLP requests.
func otlpRequests(reqs []recordedRequest) []recordedRequest {
	var out []recordedRequest
	for _, r := range reqs {
		if r.path == "/api/public/otel/v1/traces" {
			out = append(out, r)
		}
	}
	return out
}

// decodeSingleSpan decodes an OTLP request body and asserts it holds exactly
// one resourceSpan, one scopeSpan, and one span.
func decodeSingleSpan(t *testing.T, r recordedRequest) (otlpExport, map[string]otlpKV) {
	t.Helper()
	var exp otlpExport
	if err := json.Unmarshal(r.rawBody, &exp); err != nil {
		t.Fatalf("decode OTLP body: %v", err)
	}
	if len(exp.ResourceSpans) != 1 || len(exp.ResourceSpans[0].ScopeSpans) != 1 || len(exp.ResourceSpans[0].ScopeSpans[0].Spans) != 1 {
		t.Fatalf("OTLP body shape: want 1 resourceSpan/1 scopeSpan/1 span, got %s", r.rawBody)
	}
	attrs := map[string]otlpKV{}
	for _, kv := range exp.ResourceSpans[0].ScopeSpans[0].Spans[0].Attributes {
		attrs[kv.Key] = kv
	}
	return exp, attrs
}

func strAttr(t *testing.T, attrs map[string]otlpKV, key string) string {
	t.Helper()
	kv, ok := attrs[key]
	if !ok || kv.Value.StringValue == nil {
		t.Fatalf("span attribute %q missing or not a stringValue", key)
	}
	return *kv.Value.StringValue
}

func testTask() TaskSpec {
	return TaskSpec{
		ID:                 "webapp-bugfix-1",
		Archetype:          "webapp-bugfix",
		RunShape:           "implement",
		FixtureRepo:        "https://example.com/fixtures/webapp-medium.git",
		FixtureRef:         "v1-clean",
		Problem:            "Fix the off-by-one error in the pagination component.",
		AcceptanceCriteria: []string{"pagination shows correct page count", "no console errors"},
		BuildCheck:         "go build ./...",
	}
}

func testResult(cfg ConfigFingerprint) RunResult {
	return RunResult{
		RunID:  "webapp-bugfix-1-" + cfg.Hash() + "-1720000000",
		TaskID: "webapp-bugfix-1",
		Config: cfg,
		Metrics: Metrics{
			CostUSD:          1.23,
			InputTokens:      1000,
			OutputTokens:     500,
			TotalTokens:      1500,
			WallClockSeconds: 42.5,
			ToolCallCount:    7,
			NTurns:           3,
		},
		Judge: JudgeResult{
			ObjectiveFloorPass: true,
			CorrectnessScore:   0.85,
			CriteriaResults: []CriterionResult{
				{Criterion: "pagination shows correct page count", Met: true, Note: "verified"},
			},
			Rationale: "Fix matches acceptance criteria.",
		},
	}
}

// recordedRequest captures one inbound request to the fake Langfuse server
// for assertions.
type recordedRequest struct {
	method  string
	path    string
	body    map[string]any
	rawBody []byte
	header  http.Header
	user    string
	pass    string
}

// fakeDatasetID is the dataset id the fake returns from POST /v2/datasets.
const fakeDatasetID = "fake-dataset-id"

// newFakeLangfuseServer returns an httptest server that accepts every
// request this package's langfuseClient issues and records them. respErrors
// lets a test inject a non-empty "errors" array into the ingestion response.
func newFakeLangfuseServer(t *testing.T, ingestionErrors []map[string]any) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	return newFakeLangfuseServerOTLP(t, ingestionErrors, `{}`)
}

// newFakeLangfuseServerOTLP is newFakeLangfuseServer with a caller-chosen
// raw 2xx response body for the OTLP traces endpoint.
func newFakeLangfuseServerOTLP(t *testing.T, ingestionErrors []map[string]any, otlpResp string) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	var reqs []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw []byte
		if r.Body != nil {
			raw, _ = io.ReadAll(r.Body)
		}
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		user, pass, _ := r.BasicAuth()
		reqs = append(reqs, recordedRequest{method: r.Method, path: r.URL.Path, body: body, rawBody: raw, header: r.Header.Clone(), user: user, pass: pass})

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/public/otel/v1/traces":
			_, _ = w.Write([]byte(otlpResp))
		case "/api/public/v2/datasets":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": fakeDatasetID})
		case "/api/public/ingestion":
			resp := map[string]any{"successes": []any{}, "errors": []any{}}
			if ingestionErrors != nil {
				resp["errors"] = ingestionErrors
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "fake-id"})
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

func setLangfuseEnv(t *testing.T, host string) {
	t.Helper()
	t.Setenv("LANGFUSE_HOST", host)
	t.Setenv("LANGFUSE_PUBLIC_KEY", "pk-test")
	t.Setenv("LANGFUSE_SECRET_KEY", "sk-test")
}

func TestPush_MissingCreds(t *testing.T) {
	t.Setenv("LANGFUSE_HOST", "")
	t.Setenv("LANGFUSE_PUBLIC_KEY", "")
	t.Setenv("LANGFUSE_SECRET_KEY", "")

	err := Push(testResult(ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus"}), testTask())
	if err == nil {
		t.Fatal("Push with no creds: got nil error, want loud failure")
	}
	for _, want := range []string{"LANGFUSE_HOST", "LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention missing var %q", err.Error(), want)
		}
	}
}

func TestPush_HappyPath(t *testing.T) {
	srv, reqs := newFakeLangfuseServer(t, nil)
	setLangfuseEnv(t, srv.URL)

	cfg := ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus"}
	task := testTask()
	res := testResult(cfg)

	if err := Push(res, task); err != nil {
		t.Fatalf("Push: %v", err)
	}

	gotPaths := map[string]int{}
	for _, r := range *reqs {
		gotPaths[r.path]++
		if r.method != http.MethodPost {
			t.Errorf("path %s: method = %s, want POST", r.path, r.method)
		}
		if r.user != "pk-test" || r.pass != "sk-test" {
			t.Errorf("path %s: basic auth = %s/%s, want pk-test/sk-test", r.path, r.user, r.pass)
		}
	}
	wantPaths := []string{
		"/api/public/v2/datasets",
		"/api/public/dataset-items",
		"/api/public/otel/v1/traces",
		"/api/public/ingestion",
	}
	for _, p := range wantPaths {
		if gotPaths[p] != 1 {
			t.Errorf("path %s called %d times, want 1", p, gotPaths[p])
		}
	}
	if n := gotPaths["/api/public/dataset-run-items"]; n != 0 {
		t.Errorf("retired /api/public/dataset-run-items called %d times, want 0", n)
	}

	traceID := traceIDFor(res.RunID)
	spanID := spanIDFor(res.RunID)
	runName := cfg.Name + "-" + cfg.Hash()
	hex32 := regexp.MustCompile(`^[0-9a-f]{32}$`)
	hex16 := regexp.MustCompile(`^[0-9a-f]{16}$`)

	// OTLP span: shape, ids, headers, and every contract attribute.
	otlp := otlpRequests(*reqs)
	if len(otlp) != 1 {
		t.Fatalf("got %d OTLP requests, want 1", len(otlp))
	}
	if got := otlp[0].header.Get("Content-Type"); got != "application/json" {
		t.Errorf("OTLP Content-Type = %q, want application/json", got)
	}
	if got := otlp[0].header.Get("x-langfuse-ingestion-version"); got != "4" {
		t.Errorf("OTLP x-langfuse-ingestion-version = %q, want 4", got)
	}
	exp, attrs := decodeSingleSpan(t, otlp[0])
	span := exp.ResourceSpans[0].ScopeSpans[0].Spans[0]
	if span.TraceID != traceID || !hex32.MatchString(span.TraceID) {
		t.Errorf("span traceId = %q, want %q (32 lowercase hex)", span.TraceID, traceID)
	}
	if span.SpanID != spanID || !hex16.MatchString(span.SpanID) {
		t.Errorf("span spanId = %q, want %q (16 lowercase hex)", span.SpanID, spanID)
	}
	if span.ParentSpanID != "" {
		t.Errorf("span parentSpanId = %q, want none (trace root)", span.ParentSpanID)
	}
	if span.Kind != 1 {
		t.Errorf("span kind = %d, want 1", span.Kind)
	}
	if span.Name != res.TaskID {
		t.Errorf("span name = %q, want %q", span.Name, res.TaskID)
	}
	start, err1 := strconv.ParseInt(span.StartTimeUnixNano, 10, 64)
	end, err2 := strconv.ParseInt(span.EndTimeUnixNano, 10, 64)
	if err1 != nil || err2 != nil || start > end {
		t.Errorf("span start/end = %q/%q, want decimal strings with start <= end", span.StartTimeUnixNano, span.EndTimeUnixNano)
	}
	if got := exp.ResourceSpans[0].ScopeSpans[0].Scope.Name; got != "agent-teams-eval" {
		t.Errorf("scope name = %q, want agent-teams-eval", got)
	}
	if rk := exp.ResourceSpans[0].Resource.Attributes; len(rk) != 1 || rk[0].Key != "service.name" || rk[0].Value.StringValue == nil || *rk[0].Value.StringValue != "agent-teams-eval" {
		t.Errorf("resource attributes = %+v, want service.name=agent-teams-eval", rk)
	}

	cfgJSON, _ := json.Marshal(cfg)
	wantStrings := map[string]string{
		"langfuse.trace.name":                          res.TaskID,
		"langfuse.trace.metadata.runId":                res.RunID,
		"langfuse.trace.metadata.taskId":               res.TaskID,
		"langfuse.trace.metadata.config":               string(cfgJSON),
		"langfuse.observation.input":                   `{"fixtureRef":"v1-clean","problem":"Fix the off-by-one error in the pagination component."}`,
		"langfuse.observation.output":                  res.Judge.Rationale,
		"langfuse.experiment.id":                       experimentIDFor(runName),
		"langfuse.experiment.name":                     runName,
		"langfuse.experiment.dataset.id":               fakeDatasetID,
		"langfuse.experiment.item.id":                  task.ID,
		"langfuse.experiment.item.root_observation_id": spanID,
		"langfuse.experiment.item.expected_output":     `{"acceptanceCriteria":["pagination shows correct page count","no console errors"]}`,
	}
	for k, want := range wantStrings {
		if got := strAttr(t, attrs, k); got != want {
			t.Errorf("attribute %s = %q, want %q", k, got, want)
		}
	}
	if !hex32.MatchString(strAttr(t, attrs, "langfuse.experiment.id")) {
		t.Errorf("experiment id %q is not 32 lowercase hex", strAttr(t, attrs, "langfuse.experiment.id"))
	}
	tags := attrs["langfuse.trace.tags"].Value.ArrayValue
	if tags == nil || len(tags.Values) != 2 || tags.Values[0].StringValue != cfg.Name || tags.Values[1].StringValue != cfg.Hash() {
		t.Errorf("langfuse.trace.tags = %+v, want arrayValue [%q, %q]", tags, cfg.Name, cfg.Hash())
	}
	for _, banned := range []string{"langfuse.trace.input", "langfuse.trace.output", "langfuse.environment", "langfuse.observation.type"} {
		if _, ok := attrs[banned]; ok {
			t.Errorf("attribute %s must not be set", banned)
		}
	}

	// Dataset item maps input = Problem + FixtureRef, expectedOutput = AcceptanceCriteria.
	for _, r := range *reqs {
		if r.path != "/api/public/dataset-items" {
			continue
		}
		if r.body["datasetName"] != langfuseDataset {
			t.Errorf("dataset-items datasetName = %v, want %v", r.body["datasetName"], langfuseDataset)
		}
		if r.body["id"] != task.ID {
			t.Errorf("dataset-items id = %v, want %v", r.body["id"], task.ID)
		}
		input, _ := r.body["input"].(map[string]any)
		if input["problem"] != task.Problem || input["fixtureRef"] != task.FixtureRef {
			t.Errorf("dataset-items input = %v, want problem=%q fixtureRef=%q", input, task.Problem, task.FixtureRef)
		}
		expected, _ := r.body["expectedOutput"].(map[string]any)
		criteria, _ := expected["acceptanceCriteria"].([]any)
		if len(criteria) != len(task.AcceptanceCriteria) {
			t.Errorf("dataset-items expectedOutput.acceptanceCriteria = %v, want %v", criteria, task.AcceptanceCriteria)
		}
	}

	// Ingestion batch carries one trace-create + exactly the 7 canonical scores.
	wantScores := map[string]struct {
		value    float64
		dataType string
	}{
		scoreCostUSD:            {1.23, "NUMERIC"},
		scoreNTotalTokens:       {1500, "NUMERIC"},
		scoreNToolcalls:         {7, "NUMERIC"},
		scoreNTurns:             {3, "NUMERIC"},
		scoreLatencyS:           {42.5, "NUMERIC"},
		scoreCorrectness:        {0.85, "NUMERIC"},
		scoreObjectiveFloorPass: {1, "BOOLEAN"},
	}
	for _, r := range *reqs {
		if r.path != "/api/public/ingestion" {
			continue
		}
		batch, _ := r.body["batch"].([]any)
		gotScoreCount := 0
		gotTraceCount := 0
		seen := map[string]bool{}
		for _, ev := range batch {
			event, _ := ev.(map[string]any)
			body, _ := event["body"].(map[string]any)
			switch event["type"] {
			case "trace-create":
				gotTraceCount++
			case "score-create":
				gotScoreCount++
				name, _ := body["name"].(string)
				want, ok := wantScores[name]
				if !ok {
					t.Errorf("unexpected score name %q (not one of the 7 canonical names)", name)
					continue
				}
				seen[name] = true
				if body["traceId"] != traceID {
					t.Errorf("score %s traceId = %v, want %v", name, body["traceId"], traceID)
				}
				if body["observationId"] != spanID {
					t.Errorf("score %s observationId = %v, want %v", name, body["observationId"], spanID)
				}
				if body["id"] != traceID+"-"+name {
					t.Errorf("score %s id = %v, want %v", name, body["id"], traceID+"-"+name)
				}
				if got, _ := body["value"].(float64); got != want.value {
					t.Errorf("score %s value = %v, want %v", name, got, want.value)
				}
				if body["dataType"] != want.dataType {
					t.Errorf("score %s dataType = %v, want %v", name, body["dataType"], want.dataType)
				}
			default:
				t.Errorf("unexpected ingestion event type %v", event["type"])
			}
		}
		if gotTraceCount != 0 {
			t.Errorf("ingestion batch trace-create count = %d, want 0 (retired event type)", gotTraceCount)
		}
		if gotScoreCount != 7 {
			t.Errorf("ingestion batch score-create count = %d, want 7", gotScoreCount)
		}
		for name := range wantScores {
			if !seen[name] {
				t.Errorf("canonical score %q missing from ingestion batch", name)
			}
		}
	}
}

// TestTraceIDFor_MatchesLangfuseSeededTraceID pins the id derivation to the
// Langfuse SDK create_trace_id(seed) algorithm. The literals come from
// python3 -c 'import hashlib;d=hashlib.sha256(b"<RunID>").digest();print(d[:16].hex(), d[16:24].hex())'.
func TestTraceIDFor_MatchesLangfuseSeededTraceID(t *testing.T) {
	const runID = "eval-webapp-bugfix-1-abc123def456-1720000000"
	if got, want := traceIDFor(runID), "79d3b7291ec030bac1ca7585fcb48d7f"; got != want {
		t.Errorf("traceIDFor(%q) = %q, want %q", runID, got, want)
	}
	if got, want := spanIDFor(runID), "cbaff7c8005db5bf"; got != want {
		t.Errorf("spanIDFor(%q) = %q, want %q", runID, got, want)
	}
}

func TestPush_OTLPPartialSuccessRejectedSpansIsError(t *testing.T) {
	srv, _ := newFakeLangfuseServerOTLP(t, nil, `{"partialSuccess":{"rejectedSpans":1,"errorMessage":"bad span"}}`)
	setLangfuseEnv(t, srv.URL)

	err := Push(testResult(ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus"}), testTask())
	if err == nil {
		t.Fatal("Push with rejected spans: got nil error, want failure")
	}
	if !strings.Contains(err.Error(), "bad span") {
		t.Errorf("error %q does not surface the partialSuccess message", err.Error())
	}
}

func TestPush_OTLPPartialSuccessStringRejectedSpansIsError(t *testing.T) {
	srv, _ := newFakeLangfuseServerOTLP(t, nil, `{"partialSuccess":{"rejectedSpans":"2","errorMessage":"bad span"}}`)
	setLangfuseEnv(t, srv.URL)

	if err := Push(testResult(ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus"}), testTask()); err == nil {
		t.Fatal("Push with string-encoded rejectedSpans: got nil error, want failure")
	}
}

func TestPush_DatasetResponseWithoutIDIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	setLangfuseEnv(t, srv.URL)

	err := Push(testResult(ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus"}), testTask())
	if err == nil || !strings.Contains(err.Error(), "no id") {
		t.Fatalf("Push with id-less dataset response: err = %v, want error mentioning missing id", err)
	}
}

func TestPush_TwoConfigsProduceDistinctTaggedRuns(t *testing.T) {
	srv, reqs := newFakeLangfuseServer(t, nil)
	setLangfuseEnv(t, srv.URL)

	task := testTask()
	cfgA := ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus"}
	cfgB := ConfigFingerprint{Name: "sonnet-advisor", DRIModel: "sonnet", Advisor: "opus"}

	if err := Push(testResult(cfgA), task); err != nil {
		t.Fatalf("Push(cfgA): %v", err)
	}
	if err := Push(testResult(cfgB), task); err != nil {
		t.Fatalf("Push(cfgB): %v", err)
	}

	otlp := otlpRequests(*reqs)
	if len(otlp) != 2 {
		t.Fatalf("got %d OTLP requests, want 2", len(otlp))
	}
	_, attrsA := decodeSingleSpan(t, otlp[0])
	_, attrsB := decodeSingleSpan(t, otlp[1])
	for i, attrs := range []map[string]otlpKV{attrsA, attrsB} {
		if got := strAttr(t, attrs, "langfuse.experiment.item.id"); got != task.ID {
			t.Errorf("span %d item.id = %q, want %q (same item across configs)", i, got, task.ID)
		}
	}
	nameA, nameB := strAttr(t, attrsA, "langfuse.experiment.name"), strAttr(t, attrsB, "langfuse.experiment.name")
	if nameA == nameB {
		t.Errorf("distinct configs produced the same experiment name %q, want two distinct fingerprint-tagged runs", nameA)
	}
	if idA, idB := strAttr(t, attrsA, "langfuse.experiment.id"), strAttr(t, attrsB, "langfuse.experiment.id"); idA == idB {
		t.Errorf("distinct configs produced the same experiment id %q, want distinct ids", idA)
	}
}

func TestPush_IngestionPartialFailureIsNotSilent(t *testing.T) {
	srv, _ := newFakeLangfuseServer(t, []map[string]any{
		{"id": "evt-1", "status": 400, "message": "invalid score value"},
	})
	setLangfuseEnv(t, srv.URL)

	err := Push(testResult(ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus"}), testTask())
	if err == nil {
		t.Fatal("Push with ingestion errors: got nil error, want failure surfaced")
	}
	if !strings.Contains(err.Error(), "invalid score value") {
		t.Errorf("error %q does not surface the ingestion error message", err.Error())
	}
}

func TestPush_NonOKStatusPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))
	t.Cleanup(srv.Close)
	setLangfuseEnv(t, srv.URL)

	err := Push(testResult(ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus"}), testTask())
	if err == nil {
		t.Fatal("Push against failing server: got nil error, want failure")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q does not mention status 500", err.Error())
	}
}
