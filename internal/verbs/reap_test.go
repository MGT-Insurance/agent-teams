package verbs

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── test helpers (reap-specific; fakeBD/makeCtx come from dispatch_test.go,
// reapFakeAgents/makeReapCtx/fakeStops come from reap_orphans_test.go) ───────

// reapFixedNow pins the clock reap tests reason against.
var reapFixedNow = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

// reapReviewIssue builds a review-shaped bd.Issue: a "pr-url:" line makes
// initiative.ReviewPRURL(iss) succeed, and worktree/session/runtime lines
// feed initiative.Of. runtime "" is omitted so f.Runtime defaults to "" (the
// contract's "empty runtime defaults to claude" case).
func reapReviewIssue(id, status string, closedAt time.Time, notes, worktree, sessionID, runtime string) bd.Issue {
	desc := fmt.Sprintf("worktree: %s\nsession: %s\npr-url: https://github.com/owner/repo/pull/42\n", worktree, sessionID)
	if runtime != "" {
		desc += "runtime: " + runtime + "\n"
	}
	return bd.Issue{
		ID:          id,
		Status:      status,
		Description: desc,
		Notes:       notes,
		ClosedAt:    closedAt.UTC().Format(time.RFC3339),
	}
}

// reapScanFakeBD returns a fakeBD whose RunJSON serves issues for the
// `list --status=closed --json` call reap's scan mode makes.
func reapScanFakeBD(issues []bd.Issue) *fakeBD {
	return &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if len(args) == 0 || args[0] != "list" {
				return fmt.Errorf("reapScanFakeBD: unexpected RunJSON args %v", args)
			}
			out, ok := dst.(*[]bd.Issue)
			if !ok {
				return fmt.Errorf("reapScanFakeBD: unexpected dst type %T", dst)
			}
			*out = issues
			return nil
		},
	}
}

// reapShowFakeBD returns a fakeBD whose Run serves `bd show <id> --json` for
// one-off mode's initiative-id resolution (form 1). If id doesn't match, Run
// errors — bd.ShowIssue then reports not-found, and runOneOff falls through
// to form 2/3.
func reapShowFakeBD(id string, iss bd.Issue) *fakeBD {
	return &fakeBD{
		runFn: func(args ...string) (string, error) {
			if len(args) < 2 || args[0] != "show" || args[1] != id {
				return "", fmt.Errorf("reapShowFakeBD: not found: %v", args)
			}
			out, err := json.Marshal([]bd.Issue{iss})
			if err != nil {
				return "", err
			}
			return string(out), nil
		},
	}
}

// fakeRm records ids passed to `claude rm`.
type fakeRm struct{ removed []string }

func (f *fakeRm) fn() rmSessionFunc {
	return func(id string) error {
		f.removed = append(f.removed, id)
		return nil
	}
}

// fakeWorktreeRemover records worktrees passed to the removeWorktree seam.
type fakeWorktreeRemover struct{ removed []string }

func (f *fakeWorktreeRemover) fn() reapRemoveWorktreeFunc {
	return func(worktree string) error {
		f.removed = append(f.removed, worktree)
		return nil
	}
}

// fakeNoter records initiative ids the reaped-note seam was called for.
type fakeNoter struct{ noted []string }

func (f *fakeNoter) fn() reapNoteFunc {
	return func(ctx *cli.Context, id string, at time.Time) error {
		f.noted = append(f.noted, id)
		return nil
	}
}

// alwaysClean/alwaysDirty are worktreeCleanFunc stand-ins for the common
// cases; exists is always true (the worktree is present on disk).
func alwaysClean(string) (bool, bool, error) { return true, true, nil }
func alwaysDirty(string) (bool, bool, error) { return true, false, nil }

// fakePending returns a pendingReviewCommentFunc yielding a fixed answer.
func fakePending(pending bool, err error) pendingReviewCommentFunc {
	return func(string, int) (bool, error) { return pending, err }
}

// failIfCalledPending fails the test if the pending-comment probe is
// invoked at all — used to prove a gate (already-reaped, codex-runtime)
// short-circuits before ever reaching gh.
func failIfCalledPending(t *testing.T) pendingReviewCommentFunc {
	return func(string, int) (bool, error) {
		t.Helper()
		t.Fatal("pending-comment probe must not be called")
		return false, nil
	}
}

// newReapVerb builds a reapKong with every DI seam wired to the given fakes,
// grace defaulted to defaultReapGrace unless overridden by the caller.
func newReapVerb(sessions []agentSession, stops *fakeStops, rms *fakeRm, remover *fakeWorktreeRemover, noter *fakeNoter, clean worktreeCleanFunc, pending pendingReviewCommentFunc) *reapKong {
	return &reapKong{
		Grace:          defaultReapGrace,
		agentsFunc:     reapFakeAgents(sessions),
		now:            func() time.Time { return reapFixedNow },
		stopSession:    stops.stopFunc(),
		rmSession:      rms.fn(),
		removeWorktree: remover.fn(),
		worktreeClean:  clean,
		pendingComment: pending,
		noteFunc:       noter.fn(),
	}
}

// ── SCAN mode ────────────────────────────────────────────────────────────────

// (1) closed review past grace, live session, no pending comment => stop+rm
// with the SHORT id, clean worktree removed, reaped note written.
func TestReap_Scan_ReapsPastGraceCleanWorktree(t *testing.T) {
	worktree := "/tmp/reap-wt-1"
	sessionID := "sess-uuid-1"
	iss := reapReviewIssue("at-1", "closed", reapFixedNow.Add(-30*time.Minute), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc123", SessionID: sessionID, CWD: worktree, Kind: "background"}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean, fakePending(false, nil))

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stops.stopped) != 1 || stops.stopped[0] != "abc123" {
		t.Errorf("expected stop(abc123); got %v", stops.stopped)
	}
	if len(rms.removed) != 1 || rms.removed[0] != "abc123" {
		t.Errorf("expected rm(abc123); got %v", rms.removed)
	}
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("expected worktree %s removed; got %v", worktree, remover.removed)
	}
	if len(noter.noted) != 1 || noter.noted[0] != "at-1" {
		t.Errorf("expected reaped note on at-1; got %v", noter.noted)
	}
}

// (2) within grace => skipped entirely (no session/worktree/note action).
func TestReap_Scan_WithinGraceSkipped(t *testing.T) {
	worktree := "/tmp/reap-wt-2"
	sessionID := "sess-uuid-2"
	iss := reapReviewIssue("at-2", "closed", reapFixedNow.Add(-5*time.Minute), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc456", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean, failIfCalledPending(t))

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 0 || len(rms.removed) != 0 || len(remover.removed) != 0 || len(noter.noted) != 0 {
		t.Errorf("expected no action within grace; got stops=%v rms=%v remover=%v noter=%v", stops.stopped, rms.removed, remover.removed, noter.noted)
	}
}

// (3) pending comment => skipped.
func TestReap_Scan_PendingCommentSkipped(t *testing.T) {
	worktree := "/tmp/reap-wt-3"
	sessionID := "sess-uuid-3"
	iss := reapReviewIssue("at-3", "closed", reapFixedNow.Add(-time.Hour), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc789", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean, fakePending(true, nil))

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 0 || len(rms.removed) != 0 || len(remover.removed) != 0 || len(noter.noted) != 0 {
		t.Errorf("expected no action with a pending comment; got stops=%v rms=%v remover=%v noter=%v", stops.stopped, rms.removed, remover.removed, noter.noted)
	}
}

// (4) already reaped:-noted => skipped, no gh probe.
func TestReap_Scan_AlreadyReapedSkipped_NoGhProbe(t *testing.T) {
	worktree := "/tmp/reap-wt-4"
	sessionID := "sess-uuid-4"
	iss := reapReviewIssue("at-4", "closed", reapFixedNow.Add(-time.Hour), "reaped: 2026-09-17T10:00:00Z", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abcaaa", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean, failIfCalledPending(t))

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 0 || len(rms.removed) != 0 || len(remover.removed) != 0 || len(noter.noted) != 0 {
		t.Errorf("expected no action on an already-reaped initiative; got stops=%v rms=%v remover=%v noter=%v", stops.stopped, rms.removed, remover.removed, noter.noted)
	}
}

// (5) no matching session => no-op on the session, but the worktree is still
// removed if clean and the note is still written.
func TestReap_Scan_NoMatchingSession_WorktreeStillRemoved_NoteWritten(t *testing.T) {
	worktree := "/tmp/reap-wt-5"
	iss := reapReviewIssue("at-5", "closed", reapFixedNow.Add(-time.Hour), "", worktree, "sess-uuid-5", "")
	// No session in the live list matches this initiative at all.
	sessions := []agentSession{{ID: "unrelated", SessionID: "sess-uuid-unrelated", CWD: "/tmp/other"}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean, fakePending(false, nil))

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 0 || len(rms.removed) != 0 {
		t.Errorf("expected no session teardown; got stops=%v rms=%v", stops.stopped, rms.removed)
	}
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("expected worktree %s removed despite no-session; got %v", worktree, remover.removed)
	}
	if len(noter.noted) != 1 || noter.noted[0] != "at-5" {
		t.Errorf("expected reaped note despite no-session; got %v", noter.noted)
	}
}

// (6) DIRTY worktree => session torn down, worktree removal SKIPPED
// (never forced), note still written.
func TestReap_Scan_DirtyWorktree_SessionTornDown_WorktreeSkipped(t *testing.T) {
	worktree := "/tmp/reap-wt-6"
	sessionID := "sess-uuid-6"
	iss := reapReviewIssue("at-6", "closed", reapFixedNow.Add(-time.Hour), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abcddd", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysDirty, fakePending(false, nil))

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 1 || len(rms.removed) != 1 {
		t.Errorf("expected session torn down despite dirty worktree; got stops=%v rms=%v", stops.stopped, rms.removed)
	}
	if len(remover.removed) != 0 {
		t.Errorf("expected worktree removal skipped for a dirty worktree; got %v", remover.removed)
	}
	if len(noter.noted) != 1 || noter.noted[0] != "at-6" {
		t.Errorf("expected reaped note written despite dirty-worktree skip; got %v", noter.noted)
	}
}

// (7) f.Runtime == "codex" => untouched (Ring 1; no gh probe, no session/
// worktree/note action).
func TestReap_Scan_CodexRuntimeUntouched(t *testing.T) {
	worktree := "/tmp/reap-wt-7"
	iss := reapReviewIssue("at-7", "closed", reapFixedNow.Add(-time.Hour), "", worktree, "sess-uuid-7", "codex")
	sessions := []agentSession{{ID: "abceee", SessionID: "sess-uuid-7", CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean, failIfCalledPending(t))

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 0 || len(rms.removed) != 0 || len(remover.removed) != 0 || len(noter.noted) != 0 {
		t.Errorf("expected a codex-runtime initiative untouched; got stops=%v rms=%v remover=%v noter=%v", stops.stopped, rms.removed, remover.removed, noter.noted)
	}
}

// (8) non-review closed initiative (no pr-url) => untouched.
func TestReap_Scan_NonReviewInitiativeUntouched(t *testing.T) {
	iss := bd.Issue{
		ID:          "at-8",
		Status:      "closed",
		Description: "worktree: /tmp/reap-wt-8\nsession: sess-uuid-8\n",
		ClosedAt:    reapFixedNow.Add(-time.Hour).UTC().Format(time.RFC3339),
	}
	sessions := []agentSession{{ID: "abcfff", SessionID: "sess-uuid-8", CWD: "/tmp/reap-wt-8"}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean, failIfCalledPending(t))

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 0 || len(rms.removed) != 0 || len(remover.removed) != 0 || len(noter.noted) != 0 {
		t.Errorf("expected a non-review initiative untouched; got stops=%v rms=%v remover=%v noter=%v", stops.stopped, rms.removed, remover.removed, noter.noted)
	}
}

// (9) the calling session is never torn down, even when it matches.
func TestReap_Scan_NeverTearsDownCallingSession(t *testing.T) {
	worktree := "/tmp/reap-wt-9"
	callerID := "caller-sess-9"
	t.Setenv("CLAUDE_SESSION_ID", callerID)

	iss := reapReviewIssue("at-9", "closed", reapFixedNow.Add(-time.Hour), "", worktree, callerID, "")
	sessions := []agentSession{{ID: "abcggg", SessionID: callerID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean, fakePending(false, nil))

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 0 || len(rms.removed) != 0 {
		t.Errorf("expected the calling session never torn down; got stops=%v rms=%v", stops.stopped, rms.removed)
	}
	// The worktree is still processed independently of the session guard.
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("expected worktree still removed when only the calling session matched; got %v", remover.removed)
	}
}

// ── ONE-OFF mode ─────────────────────────────────────────────────────────────

// (10) target = initiative id => session + clean worktree removed, EVERY
// gate bypassed (open status, within grace, pending comment all true), note
// written.
func TestReap_OneOff_TargetInitiativeID_GatesBypassed(t *testing.T) {
	worktree := "/tmp/reap-wt-10"
	sessionID := "sess-uuid-10"
	// Open (not closed), just closed a second ago, with a pending comment —
	// every scan-mode gate would block this; one-off must bypass all of them.
	iss := reapReviewIssue("at-10", "open", reapFixedNow, "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abchhh", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean, fakePending(true, nil))

	ctx, _, _ := makeCtx(reapShowFakeBD("at-10", iss), t.TempDir())
	verb.Target = "at-10"
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 1 || stops.stopped[0] != "abchhh" {
		t.Errorf("expected stop(abchhh); got %v", stops.stopped)
	}
	if len(rms.removed) != 1 || rms.removed[0] != "abchhh" {
		t.Errorf("expected rm(abchhh); got %v", rms.removed)
	}
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("expected worktree %s removed; got %v", worktree, remover.removed)
	}
	if len(noter.noted) != 1 || noter.noted[0] != "at-10" {
		t.Errorf("expected reaped note on at-10; got %v", noter.noted)
	}
}

// (11) target = bare claude short id (no matching initiative) => resolved
// via the session's own cwd, session + worktree gone, no note (no bead to
// note against).
func TestReap_OneOff_TargetBareShortID_NoNote(t *testing.T) {
	worktree := "/tmp/reap-wt-11"
	sessions := []agentSession{{ID: "shortid11", SessionID: "sess-uuid-11", CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean, fakePending(false, nil))

	// No initiative resolves this id: bd show errors for anything.
	ctx, _, _ := makeCtx(reapShowFakeBD("no-such-id", bd.Issue{}), t.TempDir())
	verb.Target = "shortid11"
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 1 || stops.stopped[0] != "shortid11" {
		t.Errorf("expected stop(shortid11); got %v", stops.stopped)
	}
	if len(rms.removed) != 1 || rms.removed[0] != "shortid11" {
		t.Errorf("expected rm(shortid11); got %v", rms.removed)
	}
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("expected worktree %s removed; got %v", worktree, remover.removed)
	}
	if len(noter.noted) != 0 {
		t.Errorf("expected no reaped note for a bare session id (no bead); got %v", noter.noted)
	}
}

// (12) an unresolvable target (no initiative, no claude session) returns a
// non-nil error and takes no teardown action.
func TestReap_OneOff_UnresolvableTarget_Error(t *testing.T) {
	sessions := []agentSession{{ID: "someone-else", SessionID: "sess-uuid-other", CWD: "/tmp/other"}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean, fakePending(false, nil))

	ctx, _, _ := makeCtx(reapShowFakeBD("no-such-id", bd.Issue{}), t.TempDir())
	verb.Target = "totally-unresolvable-target"
	if err := verb.Run(ctx); err == nil {
		t.Fatal("expected a non-nil error for an unresolvable target")
	}
	if len(stops.stopped) != 0 || len(rms.removed) != 0 || len(remover.removed) != 0 || len(noter.noted) != 0 {
		t.Errorf("expected no teardown action for an unresolvable target; got stops=%v rms=%v remover=%v noter=%v", stops.stopped, rms.removed, remover.removed, noter.noted)
	}
}

// (13) DIRTY worktree in one-off mode => session torn down, worktree
// removal skipped (never forced), gates bypassed as in (10).
func TestReap_OneOff_DirtyWorktree_SessionGone_WorktreeSkipped(t *testing.T) {
	worktree := "/tmp/reap-wt-13"
	sessionID := "sess-uuid-13"
	iss := reapReviewIssue("at-13", "closed", reapFixedNow, "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abciii", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysDirty, fakePending(true, nil))

	ctx, _, _ := makeCtx(reapShowFakeBD("at-13", iss), t.TempDir())
	verb.Target = "at-13"
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 1 || len(rms.removed) != 1 {
		t.Errorf("expected session torn down despite dirty worktree; got stops=%v rms=%v", stops.stopped, rms.removed)
	}
	if len(remover.removed) != 0 {
		t.Errorf("expected worktree removal skipped for a dirty worktree; got %v", remover.removed)
	}
}

// ── dependency guard ─────────────────────────────────────────────────────────

// TestReap_NilContext verifies the nil-context guard mirrored from
// reapOrphansKong.Run.
func TestReap_NilContext(t *testing.T) {
	verb := &reapKong{}
	if err := verb.Run(nil); err == nil {
		t.Fatal("expected an error for a nil context")
	}
}

// ── hasReapedNote ────────────────────────────────────────────────────────────

func TestHasReapedNote(t *testing.T) {
	cases := []struct {
		notes string
		want  bool
	}{
		{"", false},
		{"some other note", false},
		{"reaped: 2026-09-17T10:00:00Z", true},
		{"first note\nreaped: 2026-09-17T10:00:00Z\n", true},
		{"  reaped: 2026-09-17T10:00:00Z", true},
	}
	for _, c := range cases {
		if got := hasReapedNote(c.notes); got != c.want {
			t.Errorf("hasReapedNote(%q) = %v, want %v", c.notes, got, c.want)
		}
	}
}
