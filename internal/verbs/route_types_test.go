// This file is owned by Track R (route-pr-event verbs).
package verbs

import (
	"errors"
	"testing"
)

func TestPRTransitionConstants(t *testing.T) {
	transitions := []PRTransition{
		TransitionCIFailed,
		TransitionChangesRequested,
		TransitionReviewRequested,
		TransitionBotFindings,
		TransitionApproved,
		TransitionMerged,
		TransitionStale,
		TransitionOther,
	}
	// All constants must be non-empty strings.
	for _, tr := range transitions {
		if string(tr) == "" {
			t.Errorf("PRTransition constant is empty")
		}
	}
}

func TestPRTransitionValues(t *testing.T) {
	cases := []struct {
		tr   PRTransition
		want string
	}{
		{TransitionCIFailed, "ci_failed"},
		{TransitionChangesRequested, "changes_requested"},
		{TransitionReviewRequested, "review_requested"},
		{TransitionBotFindings, "bot_findings"},
		{TransitionApproved, "approved"},
		{TransitionMerged, "merged"},
		{TransitionStale, "stale"},
		{TransitionOther, "other"},
	}
	for _, c := range cases {
		if string(c.tr) != c.want {
			t.Errorf("PRTransition %q: got %q, want %q", c.want, string(c.tr), c.want)
		}
	}
}

func TestMatchHowConstants(t *testing.T) {
	if MatchNone != 0 {
		t.Errorf("MatchNone must be 0 (zero value), got %d", MatchNone)
	}
	if MatchPRField == MatchNone {
		t.Error("MatchPRField must differ from MatchNone")
	}
	if MatchBranch == MatchNone {
		t.Error("MatchBranch must differ from MatchNone")
	}
	if MatchPRField == MatchBranch {
		t.Error("MatchPRField must differ from MatchBranch")
	}
}

func TestMatchResultZeroValue(t *testing.T) {
	var m MatchResult
	// Zero value must represent "no match".
	if m.How != MatchNone {
		t.Errorf("zero MatchResult.How: got %d, want MatchNone (%d)", m.How, MatchNone)
	}
	if m.InitiativeID != "" {
		t.Errorf("zero MatchResult.InitiativeID: got %q, want empty", m.InitiativeID)
	}
	if m.Worktree != "" {
		t.Errorf("zero MatchResult.Worktree: got %q, want empty", m.Worktree)
	}
}

func TestMatchResultFields(t *testing.T) {
	m := MatchResult{
		InitiativeID: "at-abc",
		Worktree:     "/some/worktree",
		How:          MatchPRField,
	}
	if m.InitiativeID != "at-abc" {
		t.Errorf("InitiativeID: got %q", m.InitiativeID)
	}
	if m.Worktree != "/some/worktree" {
		t.Errorf("Worktree: got %q", m.Worktree)
	}
	if m.How != MatchPRField {
		t.Errorf("How: got %d", m.How)
	}
}

func TestPREventFields(t *testing.T) {
	ev := PREvent{
		Repo:       "owner/repo",
		PRNumber:   42,
		PRURL:      "https://github.com/owner/repo/pull/42",
		Transition: TransitionReviewRequested,
		Body:       "lgtm",
	}
	if ev.Repo != "owner/repo" {
		t.Errorf("Repo: got %q", ev.Repo)
	}
	if ev.PRNumber != 42 {
		t.Errorf("PRNumber: got %d", ev.PRNumber)
	}
	if ev.PRURL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("PRURL: got %q", ev.PRURL)
	}
	if ev.Transition != TransitionReviewRequested {
		t.Errorf("Transition: got %q", ev.Transition)
	}
	if ev.Body != "lgtm" {
		t.Errorf("Body: got %q", ev.Body)
	}
}

func TestAteamRunnerSeam(t *testing.T) {
	// Verify the seam is injectable — a fake runner can be assigned and called.
	var recorded []string
	fake := ateamRunner(func(args ...string) error {
		recorded = args
		return nil
	})
	if err := fake("send", "at-abc", "--file", "/tmp/body.txt", "--sender", "pr-shepherd"); err != nil {
		t.Fatalf("fake runner returned error: %v", err)
	}
	if len(recorded) != 6 {
		t.Fatalf("expected 6 args, got %d: %v", len(recorded), recorded)
	}
	if recorded[0] != "send" {
		t.Errorf("args[0]: got %q, want \"send\"", recorded[0])
	}
	if recorded[1] != "at-abc" {
		t.Errorf("args[1]: got %q, want \"at-abc\"", recorded[1])
	}
}

func TestAteamRunnerSeamErrorPropagation(t *testing.T) {
	sentinel := errors.New("ateam exited 1")
	failing := ateamRunner(func(args ...string) error {
		return sentinel
	})
	if err := failing("send", "at-xyz"); err != sentinel {
		t.Errorf("error not propagated: got %v, want %v", err, sentinel)
	}
}
