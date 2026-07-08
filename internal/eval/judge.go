package eval

import "errors"

// Judge runs task.BuildCheck in the produced worktree for ObjectiveFloorPass;
// calls an LLM — shell `claude -p` or Anthropic API — with the diff +
// AcceptanceCriteria to produce CorrectnessScore + per-criterion results.
func Judge(m RunManifest, task TaskSpec) (JudgeResult, error) {
	return JudgeResult{}, errors.New("not implemented")
}
