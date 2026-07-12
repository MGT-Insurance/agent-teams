package eval

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ---- Collect: orchestration with fakes --------------------------------------
//
// These stub runExtractMetrics/runJudge (collect.go's seams, mirroring
// run.go's runGitClone/runDispatch) and use the langfuse_test.go httptest
// pattern for Push, so Collect's suite never shells a real LLM or hits a
// real Langfuse instance.

// writeCollectFixture writes the eval/tasks/<task.ID>.json and
// eval/runs/<manifest.RunID>/manifest.json files Collect/Clean read by
// convention, rooted at dir (the caller must have t.Chdir'd into dir).
func writeCollectFixture(t *testing.T, dir string, manifest RunManifest, task TaskSpec) {
	t.Helper()
	tasksDir := filepath.Join(dir, "eval", "tasks")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	taskData, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tasksDir, task.ID+".json"), taskData, 0o644); err != nil {
		t.Fatal(err)
	}

	runDir := filepath.Join(dir, "eval", "runs", manifest.RunID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "manifest.json"), manifestData, 0o644); err != nil {
		t.Fatal(err)
	}
}

func stubExtractMetrics(t *testing.T, fn func(RunManifest) (Metrics, error)) {
	t.Helper()
	orig := runExtractMetrics
	runExtractMetrics = fn
	t.Cleanup(func() { runExtractMetrics = orig })
}

func stubJudge(t *testing.T, fn func(RunManifest, TaskSpec) (JudgeResult, error)) {
	t.Helper()
	orig := runJudge
	runJudge = fn
	t.Cleanup(func() { runJudge = orig })
}

func stubPush(t *testing.T, fn func(RunResult, TaskSpec) error) {
	t.Helper()
	orig := runPush
	runPush = fn
	t.Cleanup(func() { runPush = orig })
}

func collectTestTask(id string) TaskSpec {
	return TaskSpec{
		ID:                 id,
		Archetype:          "webapp-bugfix",
		RunShape:           "implement",
		FixtureRepo:        "webapp-medium",
		FixtureRef:         "v0-bug-1",
		Problem:            "fix it",
		AcceptanceCriteria: []string{"criterion 1"},
		BuildCheck:         "true",
	}
}

func TestCollect_HappyPath(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)

	cfg := ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus"}
	task := collectTestTask("webapp-bugfix-1")
	runID := task.ID + "-" + cfg.Hash() + "-1720000000"
	manifest := RunManifest{
		RunID:        runID,
		TaskID:       task.ID,
		Config:       cfg,
		InitiativeID: "agent-teams-abcd",
		Branch:       runID,
		WorktreePath: "/tmp/some-worktree",
	}
	writeCollectFixture(t, workDir, manifest, task)

	wantMetrics := Metrics{CostUSD: 1.5, TotalTokens: 100, WallClockSeconds: 60, ToolCallCount: 3, NTurns: 2}
	stubExtractMetrics(t, func(m RunManifest) (Metrics, error) {
		if m.InitiativeID != manifest.InitiativeID {
			t.Errorf("ExtractMetrics got InitiativeID = %q, want %q", m.InitiativeID, manifest.InitiativeID)
		}
		return wantMetrics, nil
	})
	wantJudge := JudgeResult{ObjectiveFloorPass: true, CorrectnessScore: 0.9, Rationale: "looks right"}
	stubJudge(t, func(m RunManifest, tk TaskSpec) (JudgeResult, error) {
		if tk.ID != task.ID {
			t.Errorf("Judge got task ID = %q, want %q", tk.ID, task.ID)
		}
		return wantJudge, nil
	})

	srv, _ := newFakeLangfuseServer(t, nil)
	setLangfuseEnv(t, srv.URL)

	result, pushed, err := Collect(runID)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !pushed {
		t.Error("pushed = false, want true (Langfuse creds were set)")
	}
	if result.RunID != runID {
		t.Errorf("RunID = %q, want %q", result.RunID, runID)
	}
	if result.TaskID != task.ID {
		t.Errorf("TaskID = %q, want %q", result.TaskID, task.ID)
	}
	if result.Config.Hash() != cfg.Hash() {
		t.Errorf("Config mismatch: got %+v, want %+v", result.Config, cfg)
	}
	if result.Metrics != wantMetrics {
		t.Errorf("Metrics = %+v, want %+v", result.Metrics, wantMetrics)
	}
	if !reflect.DeepEqual(result.Judge, wantJudge) {
		t.Errorf("Judge = %+v, want %+v", result.Judge, wantJudge)
	}

	resultPath := filepath.Join(workDir, "eval", "runs", runID, "result.json")
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("result.json not written: %v", err)
	}
	var onDisk RunResult
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("result.json invalid JSON: %v", err)
	}
	if onDisk.RunID != runID {
		t.Errorf("on-disk RunID = %q, want %q", onDisk.RunID, runID)
	}
}

func TestCollect_MissingManifest_Errors(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, _, err := Collect("no-such-run"); err == nil {
		t.Fatal("Collect: want error for missing manifest, got nil")
	}
}

func TestCollect_MissingTaskSpec_Errors(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)

	runID := "task-x-abc123-1720000000"
	manifest := RunManifest{RunID: runID, TaskID: "task-x"}
	runDir := filepath.Join(workDir, "eval", "runs", runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Deliberately no eval/tasks/task-x.json written.

	if _, _, err := Collect(runID); err == nil {
		t.Fatal("Collect: want error for missing task spec, got nil")
	}
}

func TestCollect_MetricsError_Propagates(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)

	cfg := ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus"}
	task := collectTestTask("t1")
	runID := task.ID + "-" + cfg.Hash() + "-1720000001"
	manifest := RunManifest{RunID: runID, TaskID: task.ID, Config: cfg}
	writeCollectFixture(t, workDir, manifest, task)

	stubExtractMetrics(t, func(RunManifest) (Metrics, error) {
		return Metrics{}, errors.New("metrics boom")
	})
	stubJudge(t, func(RunManifest, TaskSpec) (JudgeResult, error) {
		t.Fatal("runJudge should not be called after ExtractMetrics fails")
		return JudgeResult{}, nil
	})

	if _, _, err := Collect(runID); err == nil {
		t.Fatal("Collect: want error when ExtractMetrics fails, got nil")
	}
}

func TestCollect_JudgeError_Propagates(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)

	cfg := ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus"}
	task := collectTestTask("t2")
	runID := task.ID + "-" + cfg.Hash() + "-1720000002"
	manifest := RunManifest{RunID: runID, TaskID: task.ID, Config: cfg}
	writeCollectFixture(t, workDir, manifest, task)

	stubExtractMetrics(t, func(RunManifest) (Metrics, error) { return Metrics{}, nil })
	stubJudge(t, func(RunManifest, TaskSpec) (JudgeResult, error) {
		return JudgeResult{}, errors.New("judge boom")
	})

	if _, _, err := Collect(runID); err == nil {
		t.Fatal("Collect: want error when Judge fails, got nil")
	}
	if _, err := os.Stat(filepath.Join(workDir, "eval", "runs", runID, "result.json")); err == nil {
		t.Error("result.json should not be written when Judge fails")
	}
}

// TestCollect_NoLangfuseCreds_Succeeds is the required coverage for the
// push-optional scope change: Collect must succeed with zero Langfuse env
// vars set, writing result.json and reporting pushed=false rather than
// erroring.
func TestCollect_NoLangfuseCreds_Succeeds(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)

	t.Setenv("LANGFUSE_HOST", "")
	t.Setenv("LANGFUSE_PUBLIC_KEY", "")
	t.Setenv("LANGFUSE_SECRET_KEY", "")

	cfg := ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus"}
	task := collectTestTask("t3")
	runID := task.ID + "-" + cfg.Hash() + "-1720000003"
	manifest := RunManifest{RunID: runID, TaskID: task.ID, Config: cfg}
	writeCollectFixture(t, workDir, manifest, task)

	stubExtractMetrics(t, func(RunManifest) (Metrics, error) { return Metrics{}, nil })
	stubJudge(t, func(RunManifest, TaskSpec) (JudgeResult, error) { return JudgeResult{}, nil })
	stubPush(t, func(RunResult, TaskSpec) error {
		t.Fatal("runPush should not be called when Langfuse env vars are unset")
		return nil
	})

	result, pushed, err := Collect(runID)
	if err != nil {
		t.Fatalf("Collect without Langfuse creds: %v", err)
	}
	if pushed {
		t.Error("pushed = true, want false (no Langfuse creds set)")
	}
	if result.RunID != runID {
		t.Errorf("RunID = %q, want %q", result.RunID, runID)
	}
	if _, err := os.Stat(filepath.Join(workDir, "eval", "runs", runID, "result.json")); err != nil {
		t.Errorf("result.json should be written even when push is skipped: %v", err)
	}
}

func TestCollect_PushError_Propagates(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)

	cfg := ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus"}
	task := collectTestTask("t4")
	runID := task.ID + "-" + cfg.Hash() + "-1720000004"
	manifest := RunManifest{RunID: runID, TaskID: task.ID, Config: cfg}
	writeCollectFixture(t, workDir, manifest, task)

	stubExtractMetrics(t, func(RunManifest) (Metrics, error) { return Metrics{}, nil })
	stubJudge(t, func(RunManifest, TaskSpec) (JudgeResult, error) { return JudgeResult{}, nil })

	// Creds ARE present, but the fake Langfuse server reports an ingestion
	// error, so Push fails for a real reason (not a missing-creds skip).
	srv, _ := newFakeLangfuseServer(t, []map[string]any{
		{"id": "evt-1", "status": 400, "message": "invalid score value"},
	})
	setLangfuseEnv(t, srv.URL)

	_, pushed, err := Collect(runID)
	if err == nil {
		t.Fatal("Collect: want error when Push fails with creds present, got nil")
	}
	if pushed {
		t.Error("pushed = true, want false on a Push failure")
	}
	// result.json is written before Push runs, so a Push failure doesn't lose
	// the locally computed result.
	if _, err := os.Stat(filepath.Join(workDir, "eval", "runs", runID, "result.json")); err != nil {
		t.Errorf("result.json should be written before Push runs: %v", err)
	}
}

// ---- Clean: real git worktree/branch removal (integration gap #3) ----------

func TestClean_RemovesWorktreeAndBranch(t *testing.T) {
	fixtureRepo := newSyntheticRepo(t, map[string]string{"a.txt": "one\n"})
	tagHEAD(t, fixtureRepo, "v0-baseline")

	workDir := t.TempDir()
	t.Chdir(workDir)

	cfg := ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus"}
	task := TaskSpec{
		ID:                 "t4",
		Archetype:          "webapp-bugfix",
		RunShape:           "implement",
		FixtureRepo:        fixtureRepo,
		FixtureRef:         "v0-baseline",
		Problem:            "fix it",
		AcceptanceCriteria: []string{"criterion 1"},
		BuildCheck:         "true",
	}
	runID := task.ID + "-" + cfg.Hash() + "-1720000004"
	branch := runID
	worktreePath := filepath.Join(t.TempDir(), "run-worktree")
	runGit(t, fixtureRepo, "worktree", "add", "-b", branch, worktreePath, "v0-baseline")

	manifest := RunManifest{RunID: runID, TaskID: task.ID, Config: cfg, Branch: branch, WorktreePath: worktreePath}
	writeCollectFixture(t, workDir, manifest, task)

	if err := Clean(runID); err != nil {
		t.Fatalf("Clean: %v", err)
	}

	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Errorf("worktree %s still exists after Clean", worktreePath)
	}

	out, err := exec.Command("git", "-C", fixtureRepo, "branch", "--list", branch).CombinedOutput()
	if err != nil {
		t.Fatalf("git branch --list: %v", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("branch %s still exists after Clean: %s", branch, out)
	}
}

func TestClean_MissingManifest_Errors(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := Clean("no-such-run"); err == nil {
		t.Fatal("Clean: want error for missing manifest, got nil")
	}
}
