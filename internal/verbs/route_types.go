// This file is owned by Track R (route-pr-event verbs).
package verbs

import (
	"fmt"
	"os"
	"os/exec"
)

// PRTransition represents the kind of GitHub event that triggered routing.
type PRTransition string

const (
	TransitionCIFailed          PRTransition = "ci_failed"
	TransitionChangesRequested  PRTransition = "changes_requested"
	TransitionReviewRequested   PRTransition = "review_requested"
	TransitionBotFindings       PRTransition = "bot_findings"
	TransitionApproved          PRTransition = "approved"
	TransitionMerged            PRTransition = "merged"
	TransitionStale             PRTransition = "stale"
	TransitionOther             PRTransition = "other"
)

// PREvent carries the structured event data from the pr-shepherd fork.
type PREvent struct {
	Repo       string       // owner/repo (nameWithOwner)
	PRNumber   int          // pull request number
	PRURL      string       // full https URL; accepted from --pr-url for logging/forward-compat; NOT used for matching (matching uses Repo + PRNumber)
	Transition PRTransition // what happened
	Body       string       // CI text / review body / empty
}

// MatchHow records which matching strategy succeeded.
type MatchHow int

const (
	// MatchNone means no initiative claimed this PR.
	MatchNone MatchHow = iota
	// MatchPRField means the initiative's pr: field (or a pr URL in its
	// notes/description) matched the event's repo + pr-number exactly.
	MatchPRField
	// MatchBranch means the basename of the initiative's repo: path matched
	// the event repo name, and its branch: matched the event head_branch.
	// Note: same repo-name under a different owner is a theoretical collision;
	// this is acceptable for the v1 loop.
	MatchBranch
)

// MatchResult is the output of the match engine (fkr.19).
// InitiativeID and Worktree are empty when How == MatchNone.
type MatchResult struct {
	InitiativeID string
	Worktree     string
	How          MatchHow
}

// ateamRunner is the seam that route-pr-event (fkr.21) uses to exec
// `ateam send` and `ateam dispatch`. Injected so tests can assert which
// args would be executed without actually running a subprocess.
type ateamRunner func(args ...string) error

// defaultAteamRunner resolves the running ateam binary via os.Executable
// and execs it with the given args. It is the production implementation.
func defaultAteamRunner(args ...string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("route-pr-event: resolve self binary: %w", err)
	}
	cmd := exec.Command(self, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
