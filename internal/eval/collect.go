package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Metrics holds the descriptive cost/latency/tool-use numbers for one run.
//
// Field → canonical Langfuse score name (align for cross-time comparability,
// per Anthropic "Demystifying evals for AI agents"). These are DESCRIPTIVE
// metrics, never correctness pass/fail.
type Metrics struct {
	CostUSD          float64 `json:"costUsd"`          // score: cost_usd
	InputTokens      int64   `json:"inputTokens"`      // (breakdown; not scored directly)
	OutputTokens     int64   `json:"outputTokens"`     // (breakdown; not scored directly)
	TotalTokens      int64   `json:"totalTokens"`      // score: n_total_tokens (= Input+Output)
	WallClockSeconds float64 `json:"wallClockSeconds"` // score: latency_s
	ToolCallCount    int     `json:"toolCallCount"`    // score: n_toolcalls
	NTurns           int     `json:"nTurns"`           // score: n_turns (assistant-turn count; free from the same JSONL walk)
	// Reserved for R5 (richer metrics): clarification count/type, rework, iterativeness.
}

// CriterionResult is one AcceptanceCriteria verdict from the LLM judge.
type CriterionResult struct {
	Criterion string `json:"criterion"`
	Met       bool   `json:"met"`
	Note      string `json:"note"`
}

// JudgeResult is the correctness verdict for one run.
type JudgeResult struct {
	ObjectiveFloorPass bool              `json:"objectiveFloorPass"` // BuildCheck exit 0
	CorrectnessScore   float64           `json:"correctnessScore"`   // 0..1 from LLM judge
	CriteriaResults    []CriterionResult `json:"criteriaResults"`
	Rationale          string            `json:"rationale"`
}

// RunResult is produced by `eval collect`, merges metrics + judge.
// Persisted under eval/runs/<RunID>/result.json
type RunResult struct {
	RunID   string            `json:"runId"`
	TaskID  string            `json:"taskId"`
	Config  ConfigFingerprint `json:"config"`
	Metrics Metrics           `json:"metrics"`
	Judge   JudgeResult       `json:"judge"`
}

// runExtractMetrics, runJudge, and runPush are Collect's injectable seams,
// mirroring run.go's package-level exec vars (runGitClone/runDispatch):
// tests substitute fakes so Collect's suite never shells a real LLM or hits
// a real Langfuse instance. A test overrides runJudge with
// judger{llm: fakeFn}.judge — the same seam judge_test.go already uses to
// exercise Judge without the judger's frozen package-level constructor — to
// avoid invoking the real `claude -p` binary.
var runExtractMetrics = ExtractMetrics
var runJudge = Judge
var runPush = Push

// Collect assembles a RunResult for runID: it loads the persisted
// RunManifest, looks up the run's TaskSpec by convention (eval/tasks/
// <TaskID>.json — TaskID is the only spec-identifying field the manifest
// carries), calls ExtractMetrics and Judge, and persists eval/runs/<RunID>/
// result.json. It is the orchestration entry point behind `eval collect
// <RunID>` (see COMPLETION SIGNAL in agent-teams-grft.1: the operator
// invokes this manually once the DRI run has finished).
//
// Langfuse push is optional, never required: result.json is the complete
// scorecard datum and Collect succeeds with zero Langfuse env vars set. If
// LANGFUSE_HOST/LANGFUSE_PUBLIC_KEY/LANGFUSE_SECRET_KEY are all set, Collect
// pushes after result.json is durably on disk and returns pushed=true; a
// push failure with creds present is still a hard error (not swallowed),
// but result.json is already written by that point. If any Langfuse env var
// is missing, Collect returns pushed=false with no error — the caller
// (cmd/eval) is responsible for surfacing that as a one-line notice. Use
// `eval push <RunID>` to backfill a Langfuse push for a run collected
// before credentials existed.
//
// This signature is not part of the contract's frozen EXPOSED SIGNATURES
// set — grft.7 (L6 integrator) owns collect.go and may adjust it.
func Collect(runID string) (result RunResult, pushed bool, err error) {
	manifest, err := loadManifest(runID)
	if err != nil {
		return RunResult{}, false, err
	}

	task, err := LoadTaskSpec(TaskSpecPath(manifest.TaskID))
	if err != nil {
		return RunResult{}, false, fmt.Errorf("eval: collect %s: load task spec: %w", runID, err)
	}

	metrics, err := runExtractMetrics(manifest)
	if err != nil {
		return RunResult{}, false, fmt.Errorf("eval: collect %s: extract metrics: %w", runID, err)
	}

	judgeResult, err := runJudge(manifest, task)
	if err != nil {
		return RunResult{}, false, fmt.Errorf("eval: collect %s: judge: %w", runID, err)
	}

	result = RunResult{
		RunID:   runID,
		TaskID:  manifest.TaskID,
		Config:  manifest.Config,
		Metrics: metrics,
		Judge:   judgeResult,
	}

	if err := writeResult(result); err != nil {
		return RunResult{}, false, err
	}

	if !langfuseConfigured() {
		return result, false, nil
	}

	if err := runPush(result, task); err != nil {
		return RunResult{}, false, fmt.Errorf("eval: collect %s: push: %w", runID, err)
	}

	return result, true, nil
}

// TaskSpecPath returns the frozen relative path of taskID's spec file. The
// manifest only carries TaskID, not the spec's original path, so this
// convention — eval/tasks/<TaskID>.json, matching eval/tasks/
// webapp-bugfix-1.json's own filename — doubles as the lookup key. Exported
// so `eval push` (cmd/eval) can locate a run's task spec from a persisted
// RunResult the same way Collect does.
func TaskSpecPath(taskID string) string {
	return filepath.Join("eval", "tasks", taskID+".json")
}

// LoadResult reads a RunResult previously persisted by Collect at
// eval/runs/<RunID>/result.json. Used by `eval push <RunID>` to backfill a
// Langfuse push for a run collected before credentials existed — re-push is
// idempotent (Push's trace id = RunID, dataset item id = TaskID).
func LoadResult(runID string) (RunResult, error) {
	path := filepath.Join("eval", "runs", runID, "result.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return RunResult{}, fmt.Errorf("eval: load result %s: %w", path, err)
	}
	var res RunResult
	if err := json.Unmarshal(data, &res); err != nil {
		return RunResult{}, fmt.Errorf("eval: parse result %s: %w", path, err)
	}
	return res, nil
}

// loadManifest reads the RunManifest persisted by Run() at the frozen
// relative path eval/runs/<RunID>/manifest.json.
func loadManifest(runID string) (RunManifest, error) {
	path := filepath.Join("eval", "runs", runID, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return RunManifest{}, fmt.Errorf("eval: collect %s: load manifest %s: %w", runID, path, err)
	}
	var m RunManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return RunManifest{}, fmt.Errorf("eval: collect %s: parse manifest %s: %w", runID, path, err)
	}
	return m, nil
}

// writeResult persists res under the frozen relative path
// eval/runs/<RunID>/result.json.
func writeResult(res RunResult) error {
	dir := filepath.Join("eval", "runs", res.RunID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("eval: create run dir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: marshal result: %w", err)
	}
	path := filepath.Join(dir, "result.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("eval: write result %s: %w", path, err)
	}
	return nil
}

// Clean removes the per-run worktree and branch that Run() created under the
// run's resolved fixture clone (integration gap #3 recorded on
// agent-teams-grft.7: `ateam dispatch`'s AddWorktree leaves these
// accumulating under EVAL_FIXTURES_DIR with no automatic cleanup). It is the
// orchestration entry point behind `eval clean <RunID>`.
//
// v1 scope: best-effort and not idempotent. If the worktree or branch is
// already gone, the underlying git commands surface that as an error rather
// than silently succeeding — acceptable for a human-driven, serial v1
// workflow; do not build retry/idempotency on top without a concrete need.
func Clean(runID string) error {
	manifest, err := loadManifest(runID)
	if err != nil {
		return err
	}
	task, err := LoadTaskSpec(TaskSpecPath(manifest.TaskID))
	if err != nil {
		return fmt.Errorf("eval: clean %s: load task spec: %w", runID, err)
	}
	repoDir, err := resolveFixtureClone(task.FixtureRepo, fixturesDir())
	if err != nil {
		return fmt.Errorf("eval: clean %s: resolve fixture: %w", runID, err)
	}

	if out, err := exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", manifest.WorktreePath).CombinedOutput(); err != nil {
		return fmt.Errorf("eval: clean %s: git worktree remove %s: %s", runID, manifest.WorktreePath, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("git", "-C", repoDir, "branch", "-D", manifest.Branch).CombinedOutput(); err != nil {
		return fmt.Errorf("eval: clean %s: git branch -D %s: %s", runID, manifest.Branch, strings.TrimSpace(string(out)))
	}
	return nil
}
