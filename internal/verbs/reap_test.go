package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

// alwaysClean/alwaysDirty/alwaysUnpushed are worktreeCleanFunc stand-ins for
// the common cases; exists is always true (the worktree is present on
// disk). alwaysUnpushed carries a fixed HEAD sha so bulk-mode gh-verify
// tests have something to feed the ghCommitPresent seam.
func alwaysClean(string) (bool, worktreeGitStatus, string, error) { return true, wtClean, "", nil }
func alwaysDirty(string) (bool, worktreeGitStatus, string, error) { return true, wtDirty, "", nil }
func alwaysUnpushed(string) (bool, worktreeGitStatus, string, error) {
	return true, wtUnpushed, "deadbeefcafef00d", nil
}

// fakeGHCommitPresent returns a ghCommitPresentFunc that records every
// (ownerRepo, sha) it was asked about and answers per the fixed present/err
// given, without shelling to a real gh binary.
type fakeGHCommitPresent struct {
	calls   [][2]string
	present bool
	err     error
}

func (f *fakeGHCommitPresent) fn() ghCommitPresentFunc {
	return func(ownerRepo, sha string) (bool, error) {
		f.calls = append(f.calls, [2]string{ownerRepo, sha})
		return f.present, f.err
	}
}

// newReapVerb builds a reapKong with every DI seam wired to the given fakes,
// grace defaulted to defaultReapGrace unless overridden by the caller.
func newReapVerb(sessions []agentSession, stops *fakeStops, rms *fakeRm, remover *fakeWorktreeRemover, noter *fakeNoter, clean worktreeCleanFunc) *reapKong {
	return &reapKong{
		Grace:          defaultReapGrace,
		ScanDeadline:   reapScanSoftDeadlineDefault,
		Max:            reapScanBatchDefault,
		agentsFunc:     reapFakeAgents(sessions),
		now:            func() time.Time { return reapFixedNow },
		stopSession:    stops.stopFunc(),
		rmSession:      rms.fn(),
		removeWorktree: remover.fn(),
		worktreeClean:  clean,
		// Safe default: no test relies on this without overriding it
		// explicitly (verb.ghCommitPresent = ...fn()) — inconclusive must
		// always protect, so an un-overridden seam never authorizes removal.
		ghCommitPresent: func(string, string) (bool, error) { return false, nil },
		noteFunc:        noter.fn(),
		notifyCtx:       defaultReapNotifyCtx,
	}
}

// ── SCAN mode ────────────────────────────────────────────────────────────────

// (1) closed review past grace, live session => stop+rm with the SHORT id,
// clean worktree removed, reaped note written.
func TestReap_Scan_ReapsPastGraceCleanWorktree(t *testing.T) {
	worktree := "/tmp/reap-wt-1"
	sessionID := "sess-uuid-1"
	iss := reapReviewIssue("at-1", "closed", reapFixedNow.Add(-30*time.Minute), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc123", SessionID: sessionID, CWD: worktree, Kind: "background"}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)

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
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 0 || len(rms.removed) != 0 || len(remover.removed) != 0 || len(noter.noted) != 0 {
		t.Errorf("expected no action within grace; got stops=%v rms=%v remover=%v noter=%v", stops.stopped, rms.removed, remover.removed, noter.noted)
	}
}

// (4) already reaped:-noted => skipped, idempotent.
func TestReap_Scan_AlreadyReapedSkipped(t *testing.T) {
	worktree := "/tmp/reap-wt-4"
	sessionID := "sess-uuid-4"
	iss := reapReviewIssue("at-4", "closed", reapFixedNow.Add(-time.Hour), "reaped: 2026-09-17T10:00:00Z", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abcaaa", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)

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
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)

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
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysDirty)

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

// (7) f.Runtime == "codex" => untouched (Ring 1; no session/worktree/note
// action).
func TestReap_Scan_CodexRuntimeUntouched(t *testing.T) {
	worktree := "/tmp/reap-wt-7"
	iss := reapReviewIssue("at-7", "closed", reapFixedNow.Add(-time.Hour), "", worktree, "sess-uuid-7", "codex")
	sessions := []agentSession{{ID: "abceee", SessionID: "sess-uuid-7", CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)

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
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 0 || len(rms.removed) != 0 || len(remover.removed) != 0 || len(noter.noted) != 0 {
		t.Errorf("expected a non-review initiative untouched; got stops=%v rms=%v remover=%v noter=%v", stops.stopped, rms.removed, remover.removed, noter.noted)
	}
}

// (9) the calling session is never torn down, even when it matches — and
// since its worktree is also the caller's own cwd, that worktree must NOT be
// removed either (F4: force-removing the caller's own cwd worktree breaks
// the live session out from under it).
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
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 0 || len(rms.removed) != 0 {
		t.Errorf("expected the calling session never torn down; got stops=%v rms=%v", stops.stopped, rms.removed)
	}
	// The worktree IS the calling session's own cwd, so it must be spared —
	// the old assertion here (worktree still removed) is exactly the footgun
	// F4 fixes: with the guard reverted, this fails.
	if len(remover.removed) != 0 {
		t.Errorf("expected worktree NOT removed when it is the calling session's own cwd; got %v", remover.removed)
	}
}

// (9b) a DIFFERENT initiative's worktree is still removed as normal even
// though the calling session is present (and resolvable) in the sessions
// list — the caller-cwd guard is scoped to the caller's OWN cwd, not to
// "any scan while a caller session exists".
func TestReap_Scan_OtherInitiativeWorktree_StillRemovedWhileCallerSessionPresent(t *testing.T) {
	callerID := "caller-sess-9b"
	t.Setenv("CLAUDE_SESSION_ID", callerID)

	targetWorktree := "/tmp/reap-wt-9b-target"
	targetSessionID := "sess-uuid-9b-target"
	iss := reapReviewIssue("at-9b", "closed", reapFixedNow.Add(-time.Hour), "", targetWorktree, targetSessionID, "")
	sessions := []agentSession{
		{ID: "abc9b1", SessionID: targetSessionID, CWD: targetWorktree},
		{ID: "abc9b2", SessionID: callerID, CWD: "/tmp/caller-own-wt"},
	}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 1 || stops.stopped[0] != "abc9b1" {
		t.Errorf("expected the target (non-caller) session torn down; got %v", stops.stopped)
	}
	if len(remover.removed) != 1 || remover.removed[0] != targetWorktree {
		t.Errorf("expected the target worktree removed despite a caller session being present elsewhere; got %v", remover.removed)
	}
}

// ── ONE-OFF mode ─────────────────────────────────────────────────────────────

// (10) target = initiative id => session + clean worktree removed, EVERY
// gate bypassed (open status, within grace), note written.
func TestReap_OneOff_TargetInitiativeID_GatesBypassed(t *testing.T) {
	worktree := "/tmp/reap-wt-10"
	sessionID := "sess-uuid-10"
	// Open (not closed), just closed a second ago — every scan-mode gate
	// would block this; one-off must bypass all of them.
	iss := reapReviewIssue("at-10", "open", reapFixedNow, "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abchhh", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)

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
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)

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
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)

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
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysDirty)

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

// (14) one-off form-1 (target = initiative id) must also spare a worktree
// that is the calling session's own cwd — the same F4 guard as scan mode,
// applied at the other call site the bead names (reap.go one-off form-1).
func TestReap_OneOff_TargetInitiativeID_NeverRemovesCallerOwnWorktree(t *testing.T) {
	worktree := "/tmp/reap-wt-14"
	callerID := "caller-sess-14"
	t.Setenv("CLAUDE_SESSION_ID", callerID)

	iss := reapReviewIssue("at-14", "closed", reapFixedNow, "", worktree, callerID, "")
	sessions := []agentSession{{ID: "abcjjj", SessionID: callerID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)

	ctx, _, _ := makeCtx(reapShowFakeBD("at-14", iss), t.TempDir())
	verb.Target = "at-14"
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 0 || len(rms.removed) != 0 {
		t.Errorf("expected the calling session never torn down; got stops=%v rms=%v", stops.stopped, rms.removed)
	}
	if len(remover.removed) != 0 {
		t.Errorf("expected worktree NOT removed when it is the calling session's own cwd (one-off form-1); got %v", remover.removed)
	}
}

// ── bulk-clear mode (agent-teams-442q.5) ────────────────────────────────────

// (19) Bulk mode's reaped-but-present sweep: an already-reaped initiative
// whose worktree is STILL on disk gets that worktree removed when Bulk=true
// (session teardown still attempted — here it's a no-op, no matching
// session), but the reaped note is NOT written again (write-once).
func TestReap_Bulk_RemovesReapedButPresentWorktree_NoDuplicateNote(t *testing.T) {
	worktree := "/tmp/reap-wt-19"
	iss := reapReviewIssue("at-19", "closed", reapFixedNow.Add(-time.Hour), "reaped: 2026-09-16T10:00:00Z", worktree, "sess-uuid-19", "")
	// No live session left — it was already stopped by a prior scan.
	sessions := []agentSession{}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)
	verb.Bulk = true

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("expected the leftover worktree %s removed in bulk mode; got %v", worktree, remover.removed)
	}
	if len(noter.noted) != 0 {
		t.Errorf("expected no duplicate reaped note for an already-reaped initiative; got %v", noter.noted)
	}
}

// (20) The same already-reaped-but-present initiative, run WITHOUT --bulk,
// stays untouched — confirms bulk mode is additive, never the default.
func TestReap_NoBulk_AlreadyReapedPresentWorktree_StillSkipped(t *testing.T) {
	worktree := "/tmp/reap-wt-20"
	iss := reapReviewIssue("at-20", "closed", reapFixedNow.Add(-time.Hour), "reaped: 2026-09-16T10:00:00Z", worktree, "sess-uuid-20", "")

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(nil, &stops, &rms, &remover, &noter, alwaysClean)

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.removed) != 0 {
		t.Errorf("expected the leftover worktree left alone without --bulk; got %v", remover.removed)
	}
}

// (21) --bulk forces an unbounded scan even if ScanDeadline/Max were set to
// tiny values: all survivors get processed in one tick.
func TestReap_Bulk_ForcesUnboundedDeadlineAndMax(t *testing.T) {
	issues, sessions := reapSurvivorFixture(5, "bulkforce")

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)
	verb.Bulk = true
	verb.ScanDeadline = time.Nanosecond // would trip immediately if honored
	verb.Max = 1                        // would stop after 1 if honored

	ctx, _, _ := makeCtx(reapScanFakeBD(issues), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 5 {
		t.Errorf("expected --bulk to force scan-deadline=0 and max=0, processing all 5 survivors; got %v", stops.stopped)
	}
}

// (22) --dry-run performs zero mutations: no stop, no rm, no worktree
// removal, no reaped note — for a normal not-yet-reaped survivor.
func TestReap_DryRun_NoMutations(t *testing.T) {
	worktree := "/tmp/reap-wt-22"
	sessionID := "sess-uuid-22"
	iss := reapReviewIssue("at-22", "closed", reapFixedNow.Add(-time.Hour), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc22", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)
	verb.Bulk = true
	verb.DryRun = true

	ctx, _, stderr := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 0 || len(rms.removed) != 0 || len(remover.removed) != 0 || len(noter.noted) != 0 {
		t.Errorf("expected zero mutations under --dry-run; got stops=%v rms=%v remover=%v noter=%v", stops.stopped, rms.removed, remover.removed, noter.noted)
	}
	if !strings.Contains(stderr.String(), "would-stop") || !strings.Contains(stderr.String(), "worktree-would-remove") {
		t.Errorf("expected the dry-run preview to report would-stop/worktree-would-remove; got %q", stderr.String())
	}
}

// (23) Bulk mode prints an upfront eligible count and a final "done" summary
// to stderr, so a human isn't staring at silence for minutes.
func TestReap_Bulk_PrintsProgressToStderr(t *testing.T) {
	issues, sessions := reapSurvivorFixture(3, "progress")

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)
	verb.Bulk = true

	ctx, _, stderr := makeCtx(reapScanFakeBD(issues), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stderr.String()
	if !strings.Contains(out, "3 eligible") {
		t.Errorf("expected the upfront eligible count in stderr; got %q", out)
	}
	if !strings.Contains(out, "done — 3/3 processed") {
		t.Errorf("expected a final done/processed summary in stderr; got %q", out)
	}
}

// (24) --bulk and --dry-run are rejected in one-off mode (a target given) —
// they only make sense scanning the whole backlog.
func TestReap_BulkOrDryRun_WithTarget_Errors(t *testing.T) {
	for _, tc := range []struct {
		name string
		bulk bool
		dry  bool
	}{
		{"bulk", true, false},
		{"dryrun", false, true},
		{"both", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stops fakeStops
			var rms fakeRm
			var remover fakeWorktreeRemover
			var noter fakeNoter
			verb := newReapVerb(nil, &stops, &rms, &remover, &noter, alwaysClean)
			verb.Bulk = tc.bulk
			verb.DryRun = tc.dry
			verb.Target = "at-24"

			ctx, _, _ := makeCtx(reapShowFakeBD("no-such-id", bd.Issue{}), t.TempDir())
			if err := verb.Run(ctx); err == nil {
				t.Fatal("expected an error combining --bulk/--dry-run with a one-off target")
			}
		})
	}
}

// ── bulk gh-verify override (Option C) ──────────────────────────────────────
// A checked-out review-pr worktree with no uncommitted changes, but whose
// HEAD carries commits absent from every local remote-tracking ref
// (worktreeClean's wtUnpushed), is a false positive when its PR branch was
// deleted on GitHub after merge — GitHub still has the commit, only the
// local tracking ref was pruned. --bulk alone may verify this via gh and
// remove the worktree anyway; every other mode/status always skips.

// reapGHVerifyIssue is reapReviewIssue with a fixed worktree/session so every
// gh-verify test below shares the same owner/repo (from reapReviewIssue's
// hardcoded "https://github.com/owner/repo/pull/42") and HEAD sha (from
// alwaysUnpushed's fixed "deadbeefcafef00d").
func reapGHVerifyIssue(id string) (bd.Issue, []agentSession) {
	worktree := "/tmp/reap-wt-" + id
	sessionID := "sess-uuid-" + id
	iss := reapReviewIssue(id, "closed", reapFixedNow.Add(-time.Hour), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc-" + id, SessionID: sessionID, CWD: worktree}}
	return iss, sessions
}

// (25) bulk + porcelain-clean + unpushed + gh HAS the commit => removed, and
// the gh-verify seam is called exactly once with the initiative's owner/repo
// and resolved HEAD sha.
func TestReap_Bulk_UnpushedWorktree_GHHasCommit_Removed(t *testing.T) {
	iss, sessions := reapGHVerifyIssue("gh25")

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysUnpushed)
	verb.Bulk = true
	fakeGH := &fakeGHCommitPresent{present: true}
	verb.ghCommitPresent = fakeGH.fn()

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.removed) != 1 || remover.removed[0] != "/tmp/reap-wt-gh25" {
		t.Errorf("expected the gh-verified worktree removed; got %v", remover.removed)
	}
	if len(fakeGH.calls) != 1 || fakeGH.calls[0] != [2]string{"owner/repo", "deadbeefcafef00d"} {
		t.Errorf("expected exactly one gh-verify call for owner/repo@deadbeefcafef00d; got %v", fakeGH.calls)
	}
}

// (26) bulk + porcelain-clean + unpushed + gh reports the commit MISSING =>
// skipped: no proof, no removal.
func TestReap_Bulk_UnpushedWorktree_GHMissing_Skipped(t *testing.T) {
	iss, sessions := reapGHVerifyIssue("gh26")

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysUnpushed)
	verb.Bulk = true
	fakeGH := &fakeGHCommitPresent{present: false}
	verb.ghCommitPresent = fakeGH.fn()

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.removed) != 0 {
		t.Errorf("expected no removal when gh reports the commit missing; got %v", remover.removed)
	}
	if len(fakeGH.calls) != 1 {
		t.Errorf("expected the gh-verify seam still called exactly once; got %v", fakeGH.calls)
	}
}

// (27) bulk + porcelain-clean + unpushed + the gh call itself ERRORS =>
// skipped — inconclusive always protects, same as a missing commit.
func TestReap_Bulk_UnpushedWorktree_GHErrors_Skipped(t *testing.T) {
	iss, sessions := reapGHVerifyIssue("gh27")

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysUnpushed)
	verb.Bulk = true
	fakeGH := &fakeGHCommitPresent{present: false, err: fmt.Errorf("simulated gh api failure")}
	verb.ghCommitPresent = fakeGH.fn()

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.removed) != 0 {
		t.Errorf("expected no removal when the gh call itself errors; got %v", remover.removed)
	}
}

// (28) bulk + porcelain-DIRTY (real uncommitted/untracked changes), even with
// gh ready to report the commit present => skipped, and the gh seam is NEVER
// called: working-tree changes always win over any gh-verify override.
func TestReap_Bulk_DirtyWorktree_NeverGHOverridden(t *testing.T) {
	iss, sessions := reapGHVerifyIssue("gh28")

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysDirty)
	verb.Bulk = true
	fakeGH := &fakeGHCommitPresent{present: true}
	verb.ghCommitPresent = fakeGH.fn()

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.removed) != 0 {
		t.Errorf("expected a dirty worktree never removed, gh notwithstanding; got %v", remover.removed)
	}
	if len(fakeGH.calls) != 0 {
		t.Errorf("expected the gh-verify seam never called for a dirty worktree; got %v", fakeGH.calls)
	}
}

// (29) the identical unpushed worktree WITHOUT --bulk => skipped, and the
// gh-verify seam is NEVER called — the steady-state (non-bulk) gate is
// unchanged by this override.
func TestReap_NoBulk_UnpushedWorktree_GHNeverCalled(t *testing.T) {
	iss, sessions := reapGHVerifyIssue("gh29")

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysUnpushed)
	fakeGH := &fakeGHCommitPresent{present: true}
	verb.ghCommitPresent = fakeGH.fn()

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.removed) != 0 {
		t.Errorf("expected no removal without --bulk; got %v", remover.removed)
	}
	if len(fakeGH.calls) != 0 {
		t.Errorf("expected the gh-verify seam never called outside --bulk; got %v", fakeGH.calls)
	}
}

// (30) bulk + already-clean (rev-list==0) => removed WITHOUT ever calling
// gh — a provably clean worktree needs no gh-verify override.
func TestReap_Bulk_AlreadyCleanWorktree_RemovedWithoutGH(t *testing.T) {
	iss, sessions := reapGHVerifyIssue("gh30")

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)
	verb.Bulk = true
	fakeGH := &fakeGHCommitPresent{present: true}
	verb.ghCommitPresent = fakeGH.fn()

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.removed) != 1 || remover.removed[0] != "/tmp/reap-wt-gh30" {
		t.Errorf("expected the already-clean worktree removed; got %v", remover.removed)
	}
	if len(fakeGH.calls) != 0 {
		t.Errorf("expected the gh-verify seam never called for an already-clean worktree; got %v", fakeGH.calls)
	}
}

// ── hoist / soft deadline / batch bound / scan cancellation ─────────────────
// Covers the impl bead's own acceptance criteria: the hoisted agentsFunc call,
// the soft wall-clock deadline that lets one tick exit cleanly under budget,
// and a cancelled scan context aborting further teardown.

// reapSurvivorFixture builds n review-shaped, past-grace, distinct closed
// initiatives, each with its own matching live session — a fixture for
// exercising the hoist/deadline/cancellation tests below, where every
// survivor is independently eligible for teardown.
func reapSurvivorFixture(n int, prefix string) ([]bd.Issue, []agentSession) {
	issues := make([]bd.Issue, 0, n)
	sessions := make([]agentSession, 0, n)
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("at-%s-%d", prefix, i)
		wt := fmt.Sprintf("/tmp/reap-wt-%s-%d", prefix, i)
		sess := fmt.Sprintf("sess-%s-%d", prefix, i)
		issues = append(issues, reapReviewIssue(id, "closed", reapFixedNow.Add(-time.Hour), "", wt, sess, ""))
		sessions = append(sessions, agentSession{ID: fmt.Sprintf("abc-%s-%d", prefix, i), SessionID: sess, CWD: wt})
	}
	return issues, sessions
}

// countingAgents wraps sessions in an agentsJSONFunc that also counts calls.
func countingAgents(sessions []agentSession, calls *int) agentsJSONFunc {
	return func() ([]agentSession, error) {
		*calls++
		return sessions, nil
	}
}

// (15) agentsFunc must be hoisted out of the survivor loop: one `claude
// agents` call serves every survivor in the tick, not one per survivor.
func TestReap_Scan_AgentsFuncHoisted_CalledAtMostOnce(t *testing.T) {
	issues, sessions := reapSurvivorFixture(3, "hoist")

	var calls int
	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)
	verb.agentsFunc = countingAgents(sessions, &calls)

	ctx, _, _ := makeCtx(reapScanFakeBD(issues), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected agentsFunc called exactly once for the whole scan; got %d calls", calls)
	}
	if len(stops.stopped) != 3 {
		t.Errorf("expected all 3 survivors torn down using the single hoisted call; got %v", stops.stopped)
	}
}

// (16) a soft scan deadline stops STARTING new survivors once elapsed
// wall-clock time exceeds it, so a tick exits well before processing the
// whole backlog — while durably marking (reaped note) every survivor it did
// start, never one it skipped.
func TestReap_Scan_SoftDeadline_ExitsCleanlyAndMarksOnlyStartedSurvivors(t *testing.T) {
	const perSurvivor = 50 * time.Millisecond
	const deadline = 70 * time.Millisecond

	issues, sessions := reapSurvivorFixture(5, "deadline")

	var stops fakeStops
	slowStop := func(id string) error {
		time.Sleep(perSurvivor) // simulates a slow (but still bounded) claude stop
		stops.stopped = append(stops.stopped, id)
		return nil
	}
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)
	verb.stopSession = slowStop
	verb.ScanDeadline = deadline

	ctx, _, _ := makeCtx(reapScanFakeBD(issues), t.TempDir())

	start := time.Now()
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	elapsed := time.Since(start)

	if len(stops.stopped) == 0 {
		t.Fatal("expected at least one survivor processed before the deadline tripped")
	}
	if len(stops.stopped) >= 5 {
		t.Fatalf("expected the soft deadline to stop the scan before all 5 survivors were processed; got %v", stops.stopped)
	}
	if len(noter.noted) != len(stops.stopped) {
		t.Errorf("expected a durable reaped note for exactly the survivors actually started; stopped=%v noted=%v", stops.stopped, noter.noted)
	}
	if elapsed >= 4*perSurvivor {
		t.Errorf("expected the scan to exit well before processing every survivor (5*%v); took %v", perSurvivor, elapsed)
	}
}

// (17) a batch bound (Max) stops STARTING new survivors once that many have
// been torn down in the tick, independent of the soft deadline.
func TestReap_Scan_BatchBound_StopsAfterMaxSurvivors(t *testing.T) {
	issues, sessions := reapSurvivorFixture(5, "batch")

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)
	verb.Max = 2

	ctx, _, _ := makeCtx(reapScanFakeBD(issues), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 2 {
		t.Errorf("expected exactly Max=2 survivors torn down; got %v", stops.stopped)
	}
	if len(noter.noted) != 2 {
		t.Errorf("expected exactly 2 durable reaped notes; got %v", noter.noted)
	}
}

// (18) a cancelled scan context (simulating a SIGTERM arriving mid-scan)
// aborts further teardown: the survivor already in flight finishes and is
// durably marked, but no new one starts, and Run returns promptly (no
// goroutine or process leak).
func TestReap_Scan_CancelledScanContext_StopsStartingFurtherSurvivors(t *testing.T) {
	issues, sessions := reapSurvivorFixture(3, "cancel")

	scanCtx, cancel := context.WithCancel(context.Background())
	var stops fakeStops
	cancelAfterFirst := func(id string) error {
		stops.stopped = append(stops.stopped, id)
		cancel() // simulate a SIGTERM landing right after the first teardown
		return nil
	}
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)
	verb.stopSession = cancelAfterFirst
	verb.notifyCtx = func() (context.Context, context.CancelFunc) { return scanCtx, cancel }

	ctx, _, _ := makeCtx(reapScanFakeBD(issues), t.TempDir())

	done := make(chan error, 1)
	go func() { done <- verb.Run(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return promptly after the scan context was cancelled — possible goroutine/process leak")
	}

	if len(stops.stopped) != 1 {
		t.Errorf("expected exactly 1 survivor processed before the cancelled context stopped the scan; got %v", stops.stopped)
	}
	if len(noter.noted) != 1 {
		t.Errorf("expected exactly 1 durable reaped note for the survivor actually started; got %v", noter.noted)
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

// ── defaultWorktreeClean (real git, upstream-agnostic clean-check) ──────────
// (runGit lives in routing_ownership_test.go)

// TestReap_DefaultWorktreeClean_LocalOnlyCommit covers case (b) from the bead: a
// commit that exists on no remote-tracking ref must never be treated as
// clean, since removing the worktree would lose it permanently. It reports
// wtUnpushed (not wtDirty) with the resolved HEAD sha, so a bulk-mode
// gh-verify override has something to check.
func TestReap_DefaultWorktreeClean_LocalOnlyCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "--allow-empty", "-m", "initial")
	shaOut, shaErr := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if shaErr != nil {
		t.Fatalf("git rev-parse HEAD: %v", shaErr)
	}
	wantSHA := strings.TrimSpace(string(shaOut))

	exists, status, headSHA, err := defaultWorktreeClean(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true for a real directory")
	}
	if status != wtUnpushed {
		t.Fatalf("expected status=wtUnpushed: HEAD has a commit absent from every remote; got %v", status)
	}
	if headSHA != wantSHA {
		t.Errorf("expected headSHA %q; got %q", wantSHA, headSHA)
	}
}

// TestReap_DefaultWorktreeClean_UncommittedChange covers case (c): an uncommitted
// change must block removal regardless of the commit history.
func TestReap_DefaultWorktreeClean_UncommittedChange(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "--allow-empty", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("uncommitted"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	exists, status, _, err := defaultWorktreeClean(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true for a real directory")
	}
	if status != wtDirty {
		t.Fatalf("expected status=wtDirty for an uncommitted change; got %v", status)
	}
}

// TestReap_DefaultWorktreeClean_MissingDirectory covers case (d): a worktree path
// that no longer exists on disk reports exists=false, never an error.
func TestReap_DefaultWorktreeClean_MissingDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	exists, status, _, err := defaultWorktreeClean(missing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Fatal("expected exists=false for a missing directory")
	}
	if status == wtClean {
		t.Fatal("expected a non-clean status for a missing directory")
	}
}

// TestReap_DefaultWorktreeClean_CommitOnRemoteTrackingRef covers case (a): once a
// commit is reachable from a remote-tracking ref, it is provably not
// local-only, so a clean tree on top of it reports clean=true. This is the
// upstream-agnostic replacement for the old "@{u}..HEAD" check — it works
// even though this branch has no configured upstream, matching the
// review-pr-<N> branches this fix targets (also proven live against the
// review-pr-8285 worktree: status-clean, HEAD an ancestor of origin/main,
// `rev-list --count HEAD --not --remotes` == 0).
func TestReap_DefaultWorktreeClean_CommitOnRemoteTrackingRef(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	remoteDir := t.TempDir()
	runGit(t, remoteDir, "init", "--bare")

	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "--allow-empty", "-m", "initial")
	runGit(t, dir, "remote", "add", "origin", remoteDir)
	runGit(t, dir, "push", "origin", "HEAD:refs/heads/main")
	// A plain push does not reliably update the local remote-tracking ref
	// across all git versions/configs; fetch to make refs/remotes/origin/main
	// unambiguous before asserting on it.
	runGit(t, dir, "fetch", "origin")

	exists, status, _, err := defaultWorktreeClean(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true for a real directory")
	}
	if status != wtClean {
		t.Fatalf("expected status=wtClean: HEAD is reachable from a remote-tracking ref and the tree is clean; got %v", status)
	}
}

// ── reapRemoveWorktreeWithTimeout / defaultReapRemoveWorktree (F1 fix) ──────

// writeFakeGit writes an executable shell script named "git" into a fresh
// temp dir and prepends that dir to PATH. reapRemoveWorktreeWithTimeout
// (via boundedGitRunner/gitutil) always invokes the literal "git" binary, so
// this makes it exercise the fake script instead of a real git.
func writeFakeGit(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "git")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatalf("write fake git script: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestReapRemoveWorktreeWithTimeout_RemovalHangReturnsErrorAndKillsGroup
// proves the fix for review finding agent-teams-442q.9/F1: `git worktree
// remove` (and its git-common-dir resolve) previously ran through gitutil's
// plain exec.Command with no context or timeout at all — the SECOND root
// cause named in the parent contract's WHY, never fixed until now. With the
// fix, a wedged removal times out (rather than hanging a scan tick
// indefinitely) and the whole process group is killed, not just the direct
// git process.
func TestReapRemoveWorktreeWithTimeout_RemovalHangReturnsErrorAndKillsGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild.pid")

	// rev-parse --git-common-dir succeeds fast; `worktree remove` hangs,
	// backgrounding a grandchild first so the test can prove it gets killed
	// too, not just the direct git process.
	writeFakeGit(t, fmt.Sprintf(`case "$*" in
  *"rev-parse --git-common-dir"*)
    echo ".git"
    ;;
  *"worktree remove"*)
    sleep 5 &
    echo $! > %s
    sleep 5
    ;;
esac
`, pidFile))

	worktree := t.TempDir()

	// A 1s budget for the same load-tolerance reason
	// TestRunBoundedClaude_KillsWholeProcessGroup (bounded_exec_test.go)
	// uses one, not reapWorktreeRemoveTimeout's real 45s.
	err := reapRemoveWorktreeWithTimeout(worktree, 1*time.Second)
	if err == nil {
		t.Fatal("expected an error from a removal that outlives its timeout")
	}

	var pidBytes []byte
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, readErr := os.ReadFile(pidFile); readErr == nil && len(strings.TrimSpace(string(b))) > 0 {
			pidBytes = b
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(pidBytes) == 0 {
		t.Fatal("grandchild never wrote its pid — test setup is broken")
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if parseErr != nil {
		t.Fatalf("parse grandchild pid %q: %v", pidBytes, parseErr)
	}
	if !processGoneWithin(pid, 2*time.Second) {
		t.Fatalf("grandchild pid %d is still alive after the timed-out removal returned — the process group was not fully killed", pid)
	}
}

// TestDefaultReapRemoveWorktree_RealWorktree_Succeeds is the happy-path
// proof that switching to a shared bounded context (boundedGitRunner)
// didn't break real removal: a real worktree, removed through the
// production constant (reapWorktreeRemoveTimeout), actually disappears from
// disk and from `git worktree list`.
func TestDefaultReapRemoveWorktree_RealWorktree_Succeeds(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repoRoot, wtPath := initRepoWithWorktree(t, "reap-remove-test")

	if err := defaultReapRemoveWorktree(wtPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(wtPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected worktree path to be gone; stat err = %v", statErr)
	}
	out, err := exec.Command("git", "-C", repoRoot, "worktree", "list", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git worktree list: %v", err)
	}
	if strings.Contains(string(out), wtPath) {
		t.Fatalf("expected %s to be gone from git worktree list; got:\n%s", wtPath, out)
	}
}

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

// ── appendReapJournal / rotation (F1) ───────────────────────────────────────
// Mirrors TestAppendHungJournal_RotatesPastCap (hung_workproduct_test.go):
// scan mode appends to reap-journal.jsonl every tick forever, so without a
// size cap the file grows unbounded.

func TestAppendReapJournal_RotatesPastCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reap-journal.jsonl")

	origCap := reapJournalMaxBytes
	reapJournalMaxBytes = 10 // trivially small so one write already exceeds it on the next append
	defer func() { reapJournalMaxBytes = origCap }()

	if err := appendReapJournal(path, reapJournalEntry{InitiativeID: "at-1"}); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := appendReapJournal(path, reapJournalEntry{InitiativeID: "at-2"}); err != nil {
		t.Fatalf("second append (should rotate first): %v", err)
	}

	backup := path + ".1"
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("expected rotated backup %s to exist: %v", backup, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read post-rotation journal: %v", err)
	}
	if !strings.Contains(string(data), "at-2") {
		t.Errorf("expected the post-rotation journal to contain the second entry, got %q", data)
	}
	if strings.Contains(string(data), "at-1") {
		t.Errorf("expected the pre-rotation entry to have moved to the backup, not stayed in the live file: %q", data)
	}
}

// ── whole-scan failure (F6) ─────────────────────────────────────────────────

// TestReap_Scan_ListClosedFails covers the whole-scan failure path: when the
// `list --status=closed` bd call itself errors, runScan must return non-nil
// (distinct from a single initiative's teardown failing, which logs and
// continues per the contract's exit rule).
func TestReap_Scan_ListClosedFails(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			return fmt.Errorf("simulated bd list failure")
		},
	}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(nil, &stops, &rms, &remover, &noter, alwaysClean)

	ctx, _, _ := makeCtx(fbd, t.TempDir())
	if err := verb.Run(ctx); err == nil {
		t.Fatal("expected a non-nil error when list --status=closed itself fails")
	}
	if len(stops.stopped) != 0 || len(rms.removed) != 0 || len(remover.removed) != 0 || len(noter.noted) != 0 {
		t.Errorf("expected no teardown action on a whole-scan failure; got stops=%v rms=%v remover=%v noter=%v", stops.stopped, rms.removed, remover.removed, noter.noted)
	}
}
