package eval

import "errors"

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

// Collect assembles a RunResult for runID: it loads the persisted
// RunManifest, calls ExtractMetrics and Judge, and merges the results. It is
// the orchestration entry point behind `eval collect <RunID>` (see
// COMPLETION SIGNAL in agent-teams-grft.1); pushing to Langfuse (Push) is a
// separate, explicit step the caller chains afterward.
//
// This signature is not part of the contract's frozen EXPOSED SIGNATURES
// set — grft.7 (L6 integrator) owns collect.go and may adjust it.
func Collect(runID string, task TaskSpec) (RunResult, error) {
	return RunResult{}, errors.New("not implemented")
}
