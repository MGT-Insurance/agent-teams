package eval

import (
	"errors"
	"time"
)

// RunManifest is produced by `eval run`, one JSON per sample.
// Persisted under eval/runs/<RunID>/manifest.json
type RunManifest struct {
	RunID        string            `json:"runId"` // taskID + "-" + config.Hash() + "-" + unix-ts
	TaskID       string            `json:"taskId"`
	Config       ConfigFingerprint `json:"config"`
	InitiativeID string            `json:"initiativeId"` // the ateam-dispatch initiative id (cost/transcript discovery key)
	Branch       string            `json:"branch"`
	WorktreePath string            `json:"worktreePath"`
	StartedAt    time.Time         `json:"startedAt"`
}

// Run resolves task.FixtureRepo to a local clone under EVAL_FIXTURES_DIR
// (clone if a URL / not yet cached), then shells
// `ateam dispatch --repo <resolved-clone> --base-branch <task.FixtureRef>
// --problem <task.Problem> --model <cfg.DRIModel> [--advisor <cfg.Advisor>]`,
// captures the initiative id + branch + worktree, writes manifest.json.
func Run(task TaskSpec, cfg ConfigFingerprint) (RunManifest, error) {
	return RunManifest{}, errors.New("not implemented")
}
