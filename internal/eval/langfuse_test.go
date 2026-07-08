package eval

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
	method string
	path   string
	body   map[string]any
	user   string
	pass   string
}

// newFakeLangfuseServer returns an httptest server that accepts every
// request this package's langfuseClient issues and records them. respErrors
// lets a test inject a non-empty "errors" array into the ingestion response.
func newFakeLangfuseServer(t *testing.T, ingestionErrors []map[string]any) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	var reqs []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		user, pass, _ := r.BasicAuth()
		reqs = append(reqs, recordedRequest{method: r.Method, path: r.URL.Path, body: body, user: user, pass: pass})

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
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
		"/api/public/ingestion",
		"/api/public/dataset-run-items",
	}
	for _, p := range wantPaths {
		if gotPaths[p] != 1 {
			t.Errorf("path %s called %d times, want 1", p, gotPaths[p])
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
				if body["id"] != res.RunID {
					t.Errorf("trace-create body.id = %v, want %v", body["id"], res.RunID)
				}
			case "score-create":
				gotScoreCount++
				name, _ := body["name"].(string)
				want, ok := wantScores[name]
				if !ok {
					t.Errorf("unexpected score name %q (not one of the 7 canonical names)", name)
					continue
				}
				seen[name] = true
				if body["traceId"] != res.RunID {
					t.Errorf("score %s traceId = %v, want %v", name, body["traceId"], res.RunID)
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
		if gotTraceCount != 1 {
			t.Errorf("ingestion batch trace-create count = %d, want 1", gotTraceCount)
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

	// Dataset run item links the trace to the item under cfg.Name+Hash().
	wantRunName := cfg.Name + "-" + cfg.Hash()
	for _, r := range *reqs {
		if r.path != "/api/public/dataset-run-items" {
			continue
		}
		if r.body["runName"] != wantRunName {
			t.Errorf("dataset-run-items runName = %v, want %v", r.body["runName"], wantRunName)
		}
		if r.body["datasetItemId"] != task.ID {
			t.Errorf("dataset-run-items datasetItemId = %v, want %v", r.body["datasetItemId"], task.ID)
		}
		if r.body["traceId"] != res.RunID {
			t.Errorf("dataset-run-items traceId = %v, want %v", r.body["traceId"], res.RunID)
		}
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

	var runNames []string
	for _, r := range *reqs {
		if r.path != "/api/public/dataset-run-items" {
			continue
		}
		if r.body["datasetItemId"] != task.ID {
			t.Errorf("run item datasetItemId = %v, want %v (same item across configs)", r.body["datasetItemId"], task.ID)
		}
		runNames = append(runNames, r.body["runName"].(string))
	}
	if len(runNames) != 2 {
		t.Fatalf("got %d dataset-run-items calls, want 2", len(runNames))
	}
	if runNames[0] == runNames[1] {
		t.Errorf("distinct configs produced the same run name %q, want two distinct fingerprint-tagged runs", runNames[0])
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
