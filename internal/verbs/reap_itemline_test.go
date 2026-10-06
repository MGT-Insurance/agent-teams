package verbs

import (
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

func TestReapItemLine(t *testing.T) {
	cases := []struct {
		name, title, session, wt, want string
	}{
		{"session reaped, worktree removed", "T", "reaped", "worktree-removed", "reap: at-1 session stopped, worktree removed · T\n"},
		{"session failed only", "T", "failed", "worktree-kept-pr-open", "reap: at-1 session stop failed · T\n"},
		{"session reaped, removal failed", "T", "reaped", "worktree-remove-failed", "reap: at-1 session stopped, worktree removal failed · T\n"},
		{"forced", "T", "no-session", "worktree-removed-forced", "reap: at-1 worktree removed (forced) · T\n"},
		{"corpse", "T", "no-session", "worktree-removed-corpse", "reap: at-1 worktree removed (corpse) · T\n"},
		{"gh verified", "T", "no-session", "worktree-removed-gh-verified", "reap: at-1 worktree removed (verified on GitHub) · T\n"},
		{"corpse gh verified", "T", "no-session", "worktree-removed-corpse-gh-verified", "reap: at-1 worktree removed (corpse, verified on GitHub) · T\n"},
		{"empty title", "", "reaped", "worktree-removed", "reap: at-1 session stopped, worktree removed\n"},
		{"nothing to report: kept pr open", "T", "no-session", "worktree-kept-pr-open", ""},
		{"nothing to report: absent", "T", "no-session", "worktree-absent", ""},
		{"nothing to report: dirty skipped", "T", "no-session", "worktree-dirty-skipped", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reapItemLine("at-1", tc.title, tc.session, tc.wt); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReapScanPrintsItemLinesOnlyForActedSurvivors(t *testing.T) {
	wtActed := "/tmp/reap-wt-item-1"
	wtAbsent := "/tmp/reap-wt-item-2"

	acted := reapReviewIssue("at-item-1", "closed", reapFixedNow.Add(-time.Hour), "", wtActed, "sess-item-1", "")
	acted.Title = "Review PR #10354 (MGT-Insurance/midgard)"
	silent := reapReviewIssue("at-item-2", "closed", reapFixedNow.Add(-time.Hour), "reaped: 2026-09-16T09:00:00Z", wtAbsent, "sess-item-2-unused", "")
	grace := reapReviewIssue("at-item-3", "closed", reapFixedNow.Add(-5*time.Minute), "", "/tmp/reap-wt-item-3", "sess-item-3", "")
	issues := []bd.Issue{acted, silent, grace}

	clean := func(worktree string) (bool, worktreeGitStatus, string, error) {
		return worktree != wtAbsent, wtClean, "", nil
	}
	var stops fakeStops
	var rms fakeRm
	var noter fakeNoter
	verb := &reapKong{
		Grace:            defaultReapGrace,
		ScanDeadline:     reapScanSoftDeadlineDefault,
		Max:              reapScanBatchDefault,
		agentsFunc:       reapFakeAgents([]agentSession{{ID: "abc-item-1", SessionID: "sess-item-1", CWD: wtActed}}),
		now:              func() time.Time { return reapFixedNow },
		stopSession:      stops.stopFunc(),
		rmSession:        rms.fn(),
		removeWorktree:   func(string, time.Duration) error { return nil },
		worktreeClean:    clean,
		worktreeExists:   func(w string) bool { e, _, _, _ := clean(w); return e },
		isCorpseWorktree: func(string) bool { return false },
		removeCorpse:     func(string, time.Duration) error { return nil },
		ghCommitPresent:  func(string, string) (bool, error) { return false, nil },
		noteFunc:         noter.fn(),
		notifyCtx:        defaultReapNotifyCtx,
		prState:          reapAlwaysMergedPRState,
		ghPreflight:      reapAlwaysGHOk,
	}
	ctx, stdout, _ := makeCtx(reapScanFakeBD(issues), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	t.Logf("stdout:\n%s", out)
	want := "reap: at-item-1 session stopped, worktree removed (forced) · Review PR #10354 (MGT-Insurance/midgard)\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected line %q; got %q", want, out)
	}
	if strings.Contains(out, "reap: at-item-2 ") || strings.Contains(out, "reap: at-item-3 ") {
		t.Errorf("silent revisit / grace skip must not print an item line; got %q", out)
	}
}
