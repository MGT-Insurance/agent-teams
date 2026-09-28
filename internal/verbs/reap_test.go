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

// fakeWorktreeRemover records worktrees (and the timeout each was called
// with) passed to the removeWorktree seam. err, when set, is returned on
// every call instead of nil — for asserting on removeWorktreeIfClean's
// removal-failure path (agent-teams-442q.13/Finding 2) without waiting out a
// real timeout.
type fakeWorktreeRemover struct {
	removed  []string
	timeouts []time.Duration
	err      error
}

func (f *fakeWorktreeRemover) fn() reapRemoveWorktreeFunc {
	return func(worktree string, timeout time.Duration) error {
		f.removed = append(f.removed, worktree)
		f.timeouts = append(f.timeouts, timeout)
		return f.err
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

// alwaysDirtyRecoverable simulates a deletion-corpse worktree (agent-teams-
// 442q.11): porcelain non-empty but every change is a pure tracked-file
// deletion, HEAD intact — the same fixed sha as alwaysUnpushed so gh-verify
// override tests share fixtures.
func alwaysDirtyRecoverable(string) (bool, worktreeGitStatus, string, error) {
	return true, wtDirtyRecoverable, "deadbeefcafef00d", nil
}

// reapAlwaysMergedPRState is newReapVerb's default prState seam
// (agent-teams-8st0.13): every pre-existing reap test built its review-shaped
// fixture (reapReviewIssue) expecting unconditional force-removal, so the
// fixture's PR must resolve MERGED by default — the "PR is done" case,
// exercised as the live path by every test that doesn't override it. A
// handful of dedicated tests below override verb.prState to OPEN/error to
// exercise the new kept-worktree branches instead.
func reapAlwaysMergedPRState(string, int) (string, error) { return "MERGED", nil }

// reapAlwaysGHOk is newReapVerb's default ghPreflight seam: gh is always
// reachable, so the PR-state gate's preflight check never itself blocks a
// probe.
func reapAlwaysGHOk() error { return nil }

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

// lastReapJournalOutcome reads home's reap-journal.jsonl and returns the last
// entry's WorktreeOutcome field, failing the test if the file is missing or
// its last line doesn't parse — used to assert on the exact journal outcome
// string a scan produced, not just on which fakes were called.
func lastReapJournalOutcome(t *testing.T, home string) string {
	t.Helper()
	data, err := os.ReadFile(reapJournalPath(home))
	if err != nil {
		t.Fatalf("read reap journal: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var entry reapJournalEntry
	last := lines[len(lines)-1]
	if err := json.Unmarshal([]byte(last), &entry); err != nil {
		t.Fatalf("unmarshal reap journal last line %q: %v", last, err)
	}
	return entry.WorktreeOutcome
}

// reapJournalLineCount returns the number of lines in home's
// reap-journal.jsonl, or 0 if the file doesn't exist yet — used to assert
// that a no-op retry tick (agent-teams-8st0.13, follow-up) appends nothing.
func reapJournalLineCount(t *testing.T, home string) int {
	t.Helper()
	data, err := os.ReadFile(reapJournalPath(home))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read reap journal: %v", err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

// newReapVerb builds a reapKong with every DI seam wired to the given fakes,
// grace defaulted to defaultReapGrace unless overridden by the caller.
// worktreeExists is derived from clean's own exists return — none of these
// callers need to fake existence independently of the clean-check result, so
// this keeps every existing clean fixture (alwaysClean/alwaysDirty/etc.)
// working unchanged for the force-remove path, which now consults
// worktreeExists instead of worktreeClean.
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
		worktreeExists: func(worktree string) bool {
			exists, _, _, _ := clean(worktree)
			return exists
		},
		// Safe default: no test relies on this without overriding it
		// explicitly (verb.ghCommitPresent = ...fn()) — inconclusive must
		// always protect, so an un-overridden seam never authorizes removal.
		ghCommitPresent: func(string, string) (bool, error) { return false, nil },
		noteFunc:        noter.fn(),
		notifyCtx:       defaultReapNotifyCtx,
		// agent-teams-8st0.13: default the PR-state gate to MERGED so every
		// pre-existing review-worktree fixture keeps exercising the
		// force-remove path unchanged; override verb.prState/verb.ghPreflight
		// explicitly in a test that needs OPEN/inconclusive.
		prState:     reapAlwaysMergedPRState,
		ghPreflight: reapAlwaysGHOk,
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

// (4) already reaped:-noted => re-swept anyway (agent-teams-6hgr.1: the note
// no longer skips a scan, it only gates the note-write itself) — session and
// worktree torn down again, but the note is NOT duplicated.
func TestReap_Scan_AlreadyReapedRevisited_TornDownAgain(t *testing.T) {
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
	if len(stops.stopped) != 1 || stops.stopped[0] != "abcaaa" {
		t.Errorf("expected the session torn down despite the existing reaped note; got %v", stops.stopped)
	}
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("expected the worktree removed despite the existing reaped note; got %v", remover.removed)
	}
	if len(noter.noted) != 0 {
		t.Errorf("expected no duplicate reaped note; got %v", noter.noted)
	}
}

// (regression b, agent-teams-6hgr.1) a reopened-then-reclosed initiative: an
// existing reaped: note from a PRIOR close/reap, but a NEW live session — a
// different id than anything in the initiative's own Sessions field (which
// still names the OLD, now-gone session), matched only via its cwd ==
// f.Worktree — plus the worktree still on disk. A normal non-bulk scan must
// tear the new session down and remove the worktree anyway; the stale note
// must never strand either.
func TestReap_Scan_ReopenedThenReclosed_NewSessionAndWorktreeTornDown(t *testing.T) {
	worktree := "/tmp/reap-wt-reopen"
	oldSessionID := "sess-uuid-reopen-old" // named in the initiative's Sessions field; long gone
	newSessionID := "sess-uuid-reopen-new" // the second run's session; matched only via CWD
	iss := reapReviewIssue("at-reopen", "closed", reapFixedNow.Add(-time.Hour), "reaped: 2026-09-16T09:00:00Z", worktree, oldSessionID, "")
	sessions := []agentSession{{ID: "abcreopen", SessionID: newSessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 1 || stops.stopped[0] != "abcreopen" {
		t.Errorf("expected the NEW session torn down despite the old reaped note; got %v", stops.stopped)
	}
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("expected the worktree removed despite the old reaped note; got %v", remover.removed)
	}
	if len(noter.noted) != 0 {
		t.Errorf("expected no duplicate reaped note; got %v", noter.noted)
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

// (6) DIRTY worktree, review-shaped => session torn down, worktree FORCE
// removed regardless (agent-teams-6hgr.1, bug 1 — a review worktree checks
// out the PR author's branch and never carries changes meant to be pushed
// from here, so the clean-only gate a normal work worktree needs doesn't
// apply), journal outcome worktree-removed-forced, note written.
func TestReap_Scan_DirtyWorktree_ForceRemoved(t *testing.T) {
	worktree := "/tmp/reap-wt-6"
	sessionID := "sess-uuid-6"
	iss := reapReviewIssue("at-6", "closed", reapFixedNow.Add(-time.Hour), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abcddd", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	home := t.TempDir()
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysDirty)

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), home)
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 1 || len(rms.removed) != 1 {
		t.Errorf("expected session torn down; got stops=%v rms=%v", stops.stopped, rms.removed)
	}
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("expected the dirty worktree force-removed; got %v", remover.removed)
	}
	if got := lastReapJournalOutcome(t, home); got != "worktree-removed-forced" {
		t.Errorf("expected journal outcome worktree-removed-forced; got %q", got)
	}
	if len(noter.noted) != 1 || noter.noted[0] != "at-6" {
		t.Errorf("expected reaped note written; got %v", noter.noted)
	}
}

// (6b, review finding on agent-teams-6hgr.1's own force-remove change) the
// review-shaped force-remove path must decide purely on existence
// (worktreeExists) and never invoke worktreeClean's git status/rev-list
// probe at all — that probe's ~15s of sequential subprocesses is pure waste
// on a path whose outcome never consults dirty/unpushed status, and eats
// into the scan tick's timeout budget (reapWorktreeRemoveTimeout's doc
// comment has the corrected arithmetic). Built directly (not via
// newReapVerb, which derives worktreeExists FROM worktreeClean for fixture
// convenience) so worktreeClean can be instrumented as a call counter
// instead: this test fails against the pre-fix code, which always called
// worktreeClean regardless of prURL.
func TestReap_Scan_ForceRemove_NeverCallsWorktreeClean(t *testing.T) {
	worktree := "/tmp/reap-wt-6b"
	sessionID := "sess-uuid-6b"
	iss := reapReviewIssue("at-6b", "closed", reapFixedNow.Add(-time.Hour), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc6b", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	worktreeCleanCalls := 0

	verb := &reapKong{
		Grace:          defaultReapGrace,
		ScanDeadline:   reapScanSoftDeadlineDefault,
		Max:            reapScanBatchDefault,
		agentsFunc:     reapFakeAgents(sessions),
		now:            func() time.Time { return reapFixedNow },
		stopSession:    stops.stopFunc(),
		rmSession:      rms.fn(),
		removeWorktree: remover.fn(),
		worktreeClean: func(string) (bool, worktreeGitStatus, string, error) {
			worktreeCleanCalls++
			return true, wtClean, "", nil
		},
		worktreeExists:  func(string) bool { return true },
		ghCommitPresent: func(string, string) (bool, error) { return false, nil },
		noteFunc:        noter.fn(),
		notifyCtx:       defaultReapNotifyCtx,
		prState:         reapAlwaysMergedPRState,
		ghPreflight:     reapAlwaysGHOk,
	}

	home := t.TempDir()
	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), home)
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if worktreeCleanCalls != 0 {
		t.Errorf("expected worktreeClean never called on the review-shaped force-remove path; got %d calls", worktreeCleanCalls)
	}
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("expected the worktree force-removed; got %v", remover.removed)
	}
	if got := lastReapJournalOutcome(t, home); got != "worktree-removed-forced" {
		t.Errorf("expected journal outcome worktree-removed-forced; got %q", got)
	}
}

// (6c, agent-teams-8st0.13) PR still OPEN => the worktree is kept, not
// force-removed, even past grace — the session is still torn down
// (independent gate), and no reaped note is written, so this initiative is
// retried next tick.
func TestReap_Scan_ReviewOpenPR_WorktreeKept(t *testing.T) {
	worktree := "/tmp/reap-wt-6c"
	sessionID := "sess-uuid-6c"
	iss := reapReviewIssue("at-6c", "closed", reapFixedNow.Add(-time.Hour), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc6c", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	home := t.TempDir()
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)
	verb.prState = func(string, int) (string, error) { return "OPEN", nil }

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), home)
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 1 || len(rms.removed) != 1 {
		t.Errorf("expected the session torn down independently of the worktree gate; got stops=%v rms=%v", stops.stopped, rms.removed)
	}
	if len(remover.removed) != 0 {
		t.Errorf("expected the worktree kept while the PR is OPEN; got removed=%v", remover.removed)
	}
	if got := lastReapJournalOutcome(t, home); got != "worktree-kept-pr-open" {
		t.Errorf("expected journal outcome worktree-kept-pr-open; got %q", got)
	}
	if len(noter.noted) != 0 {
		t.Errorf("expected no reaped note while the worktree is kept; got %v", noter.noted)
	}
}

// (6d) PR-state probe errors (gh down, PR not found, timeout, ...) => the
// worktree is kept exactly like an OPEN PR — proof of MERGED/CLOSED is
// required, an inconclusive probe never authorizes removal.
func TestReap_Scan_ReviewPRStateProbeError_WorktreeKept(t *testing.T) {
	worktree := "/tmp/reap-wt-6d"
	sessionID := "sess-uuid-6d"
	iss := reapReviewIssue("at-6d", "closed", reapFixedNow.Add(-time.Hour), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc6d", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	home := t.TempDir()
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)
	verb.prState = func(string, int) (string, error) { return "", fmt.Errorf("simulated gh pr view failure") }

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), home)
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.removed) != 0 {
		t.Errorf("expected the worktree kept on a probe error; got removed=%v", remover.removed)
	}
	if got := lastReapJournalOutcome(t, home); got != "worktree-kept-pr-unknown" {
		t.Errorf("expected journal outcome worktree-kept-pr-unknown; got %q", got)
	}
	if len(noter.noted) != 0 {
		t.Errorf("expected no reaped note on a probe error; got %v", noter.noted)
	}
}

// (6e) PR CLOSED (not merged) => removed exactly like MERGED — both are
// terminal states that clear the gate.
func TestReap_Scan_ReviewClosedPR_WorktreeRemoved(t *testing.T) {
	worktree := "/tmp/reap-wt-6e"
	sessionID := "sess-uuid-6e"
	iss := reapReviewIssue("at-6e", "closed", reapFixedNow.Add(-time.Hour), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc6e", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)
	verb.prState = func(string, int) (string, error) { return "CLOSED", nil }

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("expected the worktree removed once the PR is CLOSED; got %v", remover.removed)
	}
	if len(noter.noted) != 1 || noter.noted[0] != "at-6e" {
		t.Errorf("expected reaped note written; got %v", noter.noted)
	}
}

// (6f) --bulk stays ungated (a human escape hatch, same as one-off mode):
// even an OPEN PR is force-removed under --bulk, and the PR-state seam is
// never even consulted.
func TestReap_Bulk_ReviewOpenPR_StillForceRemoved_ProbeNeverCalled(t *testing.T) {
	worktree := "/tmp/reap-wt-6f"
	sessionID := "sess-uuid-6f"
	iss := reapReviewIssue("at-6f", "closed", reapFixedNow.Add(-time.Hour), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc6f", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	probeCalls := 0
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)
	verb.prState = func(string, int) (string, error) {
		probeCalls++
		return "OPEN", nil
	}
	verb.Bulk = true

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("expected --bulk to force-remove despite the OPEN PR; got %v", remover.removed)
	}
	if probeCalls != 0 {
		t.Errorf("expected the PR-state probe never consulted under --bulk; got %d calls", probeCalls)
	}
}

// (6g) explicit PR MERGED => removed. TestReap_Scan_ReapsPastGraceCleanWorktree
// already exercises MERGED implicitly (newReapVerb's default prState seam),
// but Eric asked this witnessed directly and by name, not only via a default
// no test names as the MERGED case.
func TestReap_Scan_ReviewMergedPR_WorktreeRemoved(t *testing.T) {
	worktree := "/tmp/reap-wt-6g"
	sessionID := "sess-uuid-6g"
	iss := reapReviewIssue("at-6g", "closed", reapFixedNow.Add(-time.Hour), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc6g", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)
	verb.prState = func(string, int) (string, error) { return "MERGED", nil }

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("expected the worktree removed once the PR is MERGED; got %v", remover.removed)
	}
	if len(noter.noted) != 1 || noter.noted[0] != "at-6g" {
		t.Errorf("expected reaped note written; got %v", noter.noted)
	}
}

// TestReap_Scan_ReviewOpenPR_RepeatedTicks_QuietAndNoExtraWrites pins Eric's
// follow-up requirement on agent-teams-8st0.13, across three ticks:
//
//   - Tick 1 (PR OPEN): the session teardown (teardownClaudeSession) is
//     gated ONLY on bd-closed + grace, never on PR state, so it tears the
//     session down on schedule even though the worktree stays kept. This
//     first "kept" sighting still journals (a real thing happened: the
//     session teardown).
//   - Tick 2 (still PR OPEN, session now actually gone from `claude
//     agents` — simulated by swapping in an empty agentsFunc, mirroring
//     what tick 1's real stop+rm would have produced): must be cheap and
//     quiet — no repeat stop/rm, no bd write (the reaped note, the only bd
//     mutation this file makes — cutting commit volume is the whole point
//     of the PR-state gate), and no new reap-journal.jsonl line either
//     (agent-teams-8st0.13 option A: a stateless skip when action ==
//     "no-session" and the worktree outcome is still a "kept-pr-*"
//     variant — nothing new to say).
//   - Tick 3 (PR now MERGED): the worktree is removed and the reaped note
//     is written exactly once — the gate clearing doesn't leave any
//     residual "kept" state to reconcile.
func TestReap_Scan_ReviewOpenPR_RepeatedTicks_QuietAndNoExtraWrites(t *testing.T) {
	worktree := "/tmp/reap-wt-6h"
	sessionID := "sess-uuid-6h"
	iss := reapReviewIssue("at-6h", "closed", reapFixedNow.Add(-time.Hour), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc6h", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	home := t.TempDir()
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)
	verb.prState = func(string, int) (string, error) { return "OPEN", nil }

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), home)
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("tick 1: unexpected error: %v", err)
	}
	if len(stops.stopped) != 1 || len(rms.removed) != 1 {
		t.Fatalf("tick 1: expected the session torn down independently of the PR-state gate; got stops=%v rms=%v", stops.stopped, rms.removed)
	}
	if len(remover.removed) != 0 || len(noter.noted) != 0 {
		t.Fatalf("tick 1: expected the worktree kept and no reaped note; got remover=%v noter=%v", remover.removed, noter.noted)
	}
	if got := reapJournalLineCount(t, home); got != 1 {
		t.Fatalf("tick 1: expected exactly 1 journal line (the first kept-open sighting); got %d", got)
	}

	// Tick 2: the session is now actually gone from `claude agents` — a real
	// stop+rm from tick 1 would have removed it from that listing too.
	verb.agentsFunc = reapFakeAgents(nil)
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("tick 2: unexpected error: %v", err)
	}
	if len(stops.stopped) != 1 || len(rms.removed) != 1 {
		t.Errorf("tick 2: expected no repeat stop/rm now that the session is gone; got stops=%v rms=%v", stops.stopped, rms.removed)
	}
	if len(remover.removed) != 0 {
		t.Errorf("tick 2: expected the worktree still kept (PR still OPEN); got %v", remover.removed)
	}
	if len(noter.noted) != 0 {
		t.Errorf("tick 2: expected no bd write on the retry tick; got %v", noter.noted)
	}
	if got := reapJournalLineCount(t, home); got != 1 {
		t.Errorf("tick 2: expected still exactly 1 journal line (no-op retry skips journaling); got %d", got)
	}

	// Tick 3: the PR is now MERGED — the gate clears, the worktree is
	// removed, and the reaped note is written exactly once. The clock must
	// advance past hungPRStateTTL first: ticks 1-2 cached "OPEN" under this
	// same PR URL (StewardHome's on-disk cache is keyed and TTL-checked
	// against c.now(), which newReapVerb otherwise pins to a single fixed
	// instant), so a fixed clock would keep serving that stale "OPEN" verdict
	// straight through this tick regardless of verb.prState's new answer.
	verb.now = func() time.Time { return reapFixedNow.Add(hungPRStateTTL + time.Minute) }
	verb.prState = func(string, int) (string, error) { return "MERGED", nil }
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("tick 3: unexpected error: %v", err)
	}
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("tick 3: expected the worktree removed once the PR is MERGED; got %v", remover.removed)
	}
	if len(noter.noted) != 1 || noter.noted[0] != "at-6h" {
		t.Errorf("tick 3: expected the reaped note written exactly once; got %v", noter.noted)
	}
	if got := reapJournalLineCount(t, home); got != 2 {
		t.Errorf("tick 3: expected a new journal line (worktree actually removed); got %d", got)
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
// removal skipped (never forced), gates bypassed as in (10). Note: this
// target passes "" as removeWorktreeIfClean's prURL regardless of the
// underlying initiative being review-shaped — one-off mode always does
// (see runOneOff/removeWorktreeIfClean's doc comments) — so the dirty
// clean-only gate still applies here, unlike scan mode's force-remove.
// Critically, no reaped: note is written despite the session teardown
// succeeding (action != "failed"): the note gate also requires
// worktreeTornDown(wtOutcome), and worktree-dirty-skipped is not torn
// down — a review finding on agent-teams-6hgr.1's own note-write fix
// (this test previously never asserted on noter.noted at all, silently
// passing whether or not the note-write bug was present).
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
	if len(noter.noted) != 0 {
		t.Errorf("expected NO reaped note when the worktree removal was skipped, not torn down; got %v", noter.noted)
	}
}

// (13b, review finding on agent-teams-6hgr.1's own note-write fix) one-off
// mode's OTHER note-strand path: the worktree removal is ATTEMPTED (unlike
// (13)'s skip) but the removeWorktree seam itself errors — mirrors scan
// mode's TestReap_Scan_WorktreeRemoveFailed_NoNote_RetriedNextTick. Session
// teardown still succeeds (action != "failed"), so gating the note on
// action alone would wrongly mark this initiative reaped even though its
// worktree is still sitting on disk, needing manual attention or a retry.
func TestReap_OneOff_WorktreeRemoveFailed_NoNote(t *testing.T) {
	worktree := "/tmp/reap-wt-13b"
	sessionID := "sess-uuid-13b"
	iss := reapReviewIssue("at-13b", "closed", reapFixedNow, "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc13b", SessionID: sessionID, CWD: worktree}}

	var stops fakeStops
	var rms fakeRm
	remover := fakeWorktreeRemover{err: fmt.Errorf("simulated removal timeout")}
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)

	ctx, _, _ := makeCtx(reapShowFakeBD("at-13b", iss), t.TempDir())
	verb.Target = "at-13b"
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stops.stopped) != 1 || len(rms.removed) != 1 {
		t.Errorf("expected session torn down despite the removal failure; got stops=%v rms=%v", stops.stopped, rms.removed)
	}
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("expected removal attempted on the failing seam; got %v", remover.removed)
	}
	if len(noter.noted) != 0 {
		t.Errorf("expected NO reaped note when the worktree removal itself failed; got %v", noter.noted)
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
// is now ALSO removed (agent-teams-6hgr.1: re-eligibility is no longer
// --bulk-only) — a leftover worktree a prior scan left behind is cleaned up
// on the very next normal tick, not just a human-invoked drain.
func TestReap_NoBulk_AlreadyReapedPresentWorktree_StillRemoved(t *testing.T) {
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
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("expected the leftover worktree removed even without --bulk; got %v", remover.removed)
	}
	if len(noter.noted) != 0 {
		t.Errorf("expected no duplicate reaped note; got %v", noter.noted)
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

// ── bulk gh-verify override (Option C) — SUPERSEDED for review-shaped issues ──
// A checked-out review-pr worktree with no uncommitted changes, but whose
// HEAD carries commits absent from every local remote-tracking ref
// (worktreeClean's wtUnpushed), used to be a false positive --bulk alone
// could verify via gh and remove anyway, on the theory its PR branch was
// deleted on GitHub after merge, pruning the local tracking ref — see
// bulkGHVerifyRemovable's doc comment.
//
// agent-teams-6hgr.1 supersedes this override for every issue
// reapGHVerifyIssue below builds (review-shaped, prURL non-empty):
// removeWorktreeIfClean now force-removes a review-shaped worktree
// unconditionally, so this override never fires in production any more —
// --bulk is scan-mode-only, and scan-mode's prURL is never empty for
// anything reaching removeWorktreeIfClean. The tests below now assert that
// supersession (force-removed, gh never called) rather than the old
// skip-unless-gh-verified behavior; the underlying now-dead machinery
// itself is removed by the follow-up cleanup bead agent-teams-6hgr.3, not
// here.

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

// (25) bulk + porcelain-clean + unpushed, review-shaped => force-removed
// directly; the gh-verify seam is now dead for this case (agent-teams-6hgr.1)
// and is never called.
func TestReap_Bulk_UnpushedWorktree_ForceRemovedWithoutGH(t *testing.T) {
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
		t.Errorf("expected the worktree force-removed; got %v", remover.removed)
	}
	if len(fakeGH.calls) != 0 {
		t.Errorf("expected the gh-verify seam never called (superseded by force-remove); got %v", fakeGH.calls)
	}
}

// (26) bulk + porcelain-clean + unpushed, review-shaped => force-removed
// even though gh would have reported the commit missing — gh is never
// consulted at all (agent-teams-6hgr.1 supersedes the old skip-unless-
// verified behavior for a review-shaped worktree).
func TestReap_Bulk_UnpushedWorktree_ForceRemovedDespiteGHMissing(t *testing.T) {
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
	if len(remover.removed) != 1 || remover.removed[0] != "/tmp/reap-wt-gh26" {
		t.Errorf("expected the worktree force-removed despite gh reporting the commit missing; got %v", remover.removed)
	}
	if len(fakeGH.calls) != 0 {
		t.Errorf("expected the gh-verify seam never called; got %v", fakeGH.calls)
	}
}

// (27) bulk + porcelain-clean + unpushed, review-shaped => force-removed
// even though the gh call itself would error — gh is never consulted.
func TestReap_Bulk_UnpushedWorktree_ForceRemovedDespiteGHError(t *testing.T) {
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
	if len(remover.removed) != 1 || remover.removed[0] != "/tmp/reap-wt-gh27" {
		t.Errorf("expected the worktree force-removed despite a gh error; got %v", remover.removed)
	}
	if len(fakeGH.calls) != 0 {
		t.Errorf("expected the gh-verify seam never called; got %v", fakeGH.calls)
	}
}

// (28) bulk + porcelain-DIRTY (real uncommitted/untracked changes),
// review-shaped => force-removed regardless — even genuine dirt is
// force-removed for a review-shaped worktree (agent-teams-6hgr.1, bug 1's
// "remove regardless of dirty/clean" design decision applies to real dirt
// too, not just wtUnpushed/wtDirtyRecoverable) — and the gh seam is never
// called.
func TestReap_Bulk_DirtyWorktree_ForceRemovedWithoutGH(t *testing.T) {
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
	if len(remover.removed) != 1 || remover.removed[0] != "/tmp/reap-wt-gh28" {
		t.Errorf("expected the dirty worktree force-removed; got %v", remover.removed)
	}
	if len(fakeGH.calls) != 0 {
		t.Errorf("expected the gh-verify seam never called for a review-shaped worktree; got %v", fakeGH.calls)
	}
}

// (29) the identical unpushed worktree WITHOUT --bulk => force-removed too —
// the force-remove-review rule (agent-teams-6hgr.1) applies regardless of
// --bulk, and the gh-verify seam is NEVER called.
func TestReap_NoBulk_UnpushedWorktree_ForceRemoved_GHNeverCalled(t *testing.T) {
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
	if len(remover.removed) != 1 || remover.removed[0] != "/tmp/reap-wt-gh29" {
		t.Errorf("expected the worktree force-removed without --bulk; got %v", remover.removed)
	}
	if len(fakeGH.calls) != 0 {
		t.Errorf("expected the gh-verify seam never called; got %v", fakeGH.calls)
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

// ── bulk gh-verify override for deletion-corpse worktrees (agent-teams-442q.11) — SUPERSEDED for review-shaped issues ──
// A worktree killed mid `git worktree remove` (wtDirtyRecoverable — porcelain
// non-empty but every change is a pure tracked-file deletion, HEAD intact)
// used to get the identical --bulk-only gh-verify override as wtUnpushed
// above. agent-teams-6hgr.1 supersedes this override for every issue
// reapGHVerifyIssue below builds (review-shaped): removeWorktreeIfClean now
// force-removes a review-shaped worktree unconditionally, so this override
// never fires in production any more. The tests below assert that
// supersession (force-removed, gh never called, outcome
// "worktree-removed-forced") rather than the old gh-verify behavior; the
// underlying now-dead machinery itself is removed by the follow-up cleanup
// bead agent-teams-6hgr.3, not here.

// (31) bulk + deletion-corpse (wtDirtyRecoverable), review-shaped =>
// force-removed directly with journal outcome "worktree-removed-forced" —
// the gh-verify seam is now dead for this case (agent-teams-6hgr.1) and is
// never called.
func TestReap_Bulk_DirtyRecoverableWorktree_ForceRemovedWithoutGH(t *testing.T) {
	iss, sessions := reapGHVerifyIssue("gh31")
	home := t.TempDir()

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysDirtyRecoverable)
	verb.Bulk = true
	fakeGH := &fakeGHCommitPresent{present: true}
	verb.ghCommitPresent = fakeGH.fn()

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), home)
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.removed) != 1 || remover.removed[0] != "/tmp/reap-wt-gh31" {
		t.Errorf("expected the deletion-corpse worktree force-removed; got %v", remover.removed)
	}
	if len(fakeGH.calls) != 0 {
		t.Errorf("expected the gh-verify seam never called (superseded by force-remove); got %v", fakeGH.calls)
	}
	if got := lastReapJournalOutcome(t, home); got != "worktree-removed-forced" {
		t.Errorf("expected journal outcome worktree-removed-forced; got %q", got)
	}
}

// (32) bulk + deletion-corpse, review-shaped => force-removed even though gh
// would have reported the commit missing — gh is never consulted.
func TestReap_Bulk_DirtyRecoverableWorktree_ForceRemovedDespiteGHMissing(t *testing.T) {
	iss, sessions := reapGHVerifyIssue("gh32")
	home := t.TempDir()

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysDirtyRecoverable)
	verb.Bulk = true
	fakeGH := &fakeGHCommitPresent{present: false}
	verb.ghCommitPresent = fakeGH.fn()

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), home)
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.removed) != 1 || remover.removed[0] != "/tmp/reap-wt-gh32" {
		t.Errorf("expected the worktree force-removed despite gh reporting the commit missing; got %v", remover.removed)
	}
	if len(fakeGH.calls) != 0 {
		t.Errorf("expected the gh-verify seam never called; got %v", fakeGH.calls)
	}
	if got := lastReapJournalOutcome(t, home); got != "worktree-removed-forced" {
		t.Errorf("expected journal outcome worktree-removed-forced; got %q", got)
	}
}

// (33) the identical deletion-corpse worktree WITHOUT --bulk => force-removed
// too — the force-remove-review rule applies regardless of --bulk, and the
// gh-verify seam is NEVER called.
func TestReap_NoBulk_DirtyRecoverableWorktree_ForceRemoved(t *testing.T) {
	iss, sessions := reapGHVerifyIssue("gh33")
	home := t.TempDir()

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysDirtyRecoverable)
	fakeGH := &fakeGHCommitPresent{present: true}
	verb.ghCommitPresent = fakeGH.fn()

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), home)
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.removed) != 1 || remover.removed[0] != "/tmp/reap-wt-gh33" {
		t.Errorf("expected the worktree force-removed without --bulk; got %v", remover.removed)
	}
	if len(fakeGH.calls) != 0 {
		t.Errorf("expected the gh-verify seam never called; got %v", fakeGH.calls)
	}
	if got := lastReapJournalOutcome(t, home); got != "worktree-removed-forced" {
		t.Errorf("expected journal outcome worktree-removed-forced; got %q", got)
	}
}

// (34) bulk + --dry-run + deletion-corpse (wtDirtyRecoverable), review-shaped
// => the preview reports "worktree-would-remove-forced", proving --dry-run's
// read-only path (previewWorktreeOutcome) reflects the same force-remove
// rule removeWorktreeIfClean applies for real (agent-teams-6hgr.1) — and the
// gh-verify seam is never consulted, superseding the old
// "worktree-would-remove-corpse-gh-verified" preview.
func TestReap_Bulk_DryRun_DirtyRecoverableWorktree_WouldForceRemove(t *testing.T) {
	iss, sessions := reapGHVerifyIssue("gh34")

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysDirtyRecoverable)
	verb.Bulk = true
	verb.DryRun = true
	fakeGH := &fakeGHCommitPresent{present: true}
	verb.ghCommitPresent = fakeGH.fn()

	ctx, _, stderr := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr.String(), "worktree-would-remove-forced") {
		t.Errorf("expected the dry-run preview to report worktree-would-remove-forced; got %q", stderr.String())
	}
	if len(remover.removed) != 0 {
		t.Errorf("expected zero mutations under --dry-run; got %v", remover.removed)
	}
	if len(fakeGH.calls) != 0 {
		t.Errorf("expected the gh-verify seam never called; got %v", fakeGH.calls)
	}
}

// (35) bulk + --dry-run + deletion-corpse, review-shaped, gh reports the
// commit MISSING => the preview still reports "worktree-would-remove-forced"
// — the force-remove rule doesn't consult gh at all, dry-run included.
func TestReap_Bulk_DryRun_DirtyRecoverableWorktree_WouldForceRemoveDespiteGHMissing(t *testing.T) {
	iss, sessions := reapGHVerifyIssue("gh35")

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysDirtyRecoverable)
	verb.Bulk = true
	verb.DryRun = true
	fakeGH := &fakeGHCommitPresent{present: false}
	verb.ghCommitPresent = fakeGH.fn()

	ctx, _, stderr := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr.String(), "worktree-would-remove-forced") {
		t.Errorf("expected the dry-run preview to report worktree-would-remove-forced; got %q", stderr.String())
	}
	if len(remover.removed) != 0 {
		t.Errorf("expected zero mutations under --dry-run; got %v", remover.removed)
	}
	if len(fakeGH.calls) != 0 {
		t.Errorf("expected the gh-verify seam never called; got %v", fakeGH.calls)
	}
}

// ── mode-aware bulk removal timeout & remove-failed outcome (agent-teams-442q.13) ──
// A live production --bulk run against 942 worktrees applied
// reapWorktreeRemoveTimeout's steady-state 45s bound to every removal
// uniformly, timing out 31 of 72 gh-verified-safe removals under real
// disk/CPU load — 7 of those became permanent, git-invisible corpses,
// because the best-effort `git worktree prune` never runs when
// git.RemoveWorktree itself times out. These cases prove
// removeWorktreeIfClean now selects a mode-aware timeout, and that a
// removal failure reports a distinct outcome instead of the misleading
// "worktree-dirty-skipped" (this worktree was never dirty — only its
// removal failed).

// (36) steady-state (non-bulk): removeWorktreeIfClean passes
// reapWorktreeRemoveTimeout to the removeWorktree seam.
func TestReap_Scan_RemoveWorktreeTimeout_NonBulk_UsesSteadyStateConstant(t *testing.T) {
	worktree := "/tmp/reap-wt-timeout-nonbulk"
	sessionID := "sess-uuid-timeout-nonbulk"
	iss := reapReviewIssue("at-timeout-nonbulk", "closed", reapFixedNow.Add(-30*time.Minute), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc-timeout-nonbulk", SessionID: sessionID, CWD: worktree, Kind: "background"}}

	var stops fakeStops
	var rms fakeRm
	var remover fakeWorktreeRemover
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)

	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.timeouts) != 1 || remover.timeouts[0] != reapWorktreeRemoveTimeout {
		t.Errorf("expected removeWorktree called with reapWorktreeRemoveTimeout (%v); got %v", reapWorktreeRemoveTimeout, remover.timeouts)
	}
}

// (37) --bulk: removeWorktreeIfClean passes the far more generous
// reapBulkWorktreeRemoveTimeout instead — the fix for the live-run timeout
// storm above.
func TestReap_Bulk_RemoveWorktreeTimeout_UsesBulkConstant(t *testing.T) {
	worktree := "/tmp/reap-wt-timeout-bulk"
	sessionID := "sess-uuid-timeout-bulk"
	iss := reapReviewIssue("at-timeout-bulk", "closed", reapFixedNow.Add(-30*time.Minute), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc-timeout-bulk", SessionID: sessionID, CWD: worktree, Kind: "background"}}

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
	if len(remover.timeouts) != 1 || remover.timeouts[0] != reapBulkWorktreeRemoveTimeout {
		t.Errorf("expected removeWorktree called with reapBulkWorktreeRemoveTimeout (%v); got %v", reapBulkWorktreeRemoveTimeout, remover.timeouts)
	}
}

// (38) removeWorktree itself errors (a timeout, in production) => the
// journal records the distinct "worktree-remove-failed" outcome, not
// "worktree-dirty-skipped" — this worktree was clean (alwaysClean), only its
// removal attempt failed (agent-teams-442q.13/Finding 2).
func TestReap_Scan_RemoveWorktreeFails_DistinctOutcome(t *testing.T) {
	worktree := "/tmp/reap-wt-remove-failed"
	sessionID := "sess-uuid-remove-failed"
	iss := reapReviewIssue("at-remove-failed", "closed", reapFixedNow.Add(-30*time.Minute), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abc-remove-failed", SessionID: sessionID, CWD: worktree, Kind: "background"}}

	var stops fakeStops
	var rms fakeRm
	remover := fakeWorktreeRemover{err: fmt.Errorf("simulated removal timeout")}
	var noter fakeNoter
	verb := newReapVerb(sessions, &stops, &rms, &remover, &noter, alwaysClean)

	home := t.TempDir()
	ctx, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), home)
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remover.removed) != 1 || remover.removed[0] != worktree {
		t.Errorf("expected removal still attempted on the failing seam; got %v", remover.removed)
	}
	if got := lastReapJournalOutcome(t, home); got != "worktree-remove-failed" {
		t.Errorf("expected journal outcome worktree-remove-failed; got %q", got)
	}
}

// (39, agent-teams-6hgr.1) a failed worktree removal must never get the
// reaped: note — session teardown alone (action != "failed") is not enough;
// the worktree must also be verifiably gone. Otherwise the note strands the
// leftover worktree forever, since bug 2's re-eligibility fix only helps an
// UN-noted initiative (an already-reaped one is still revisited, but a
// production build that treats "noted" as "done" would never retry it). A
// second tick, with the removal seam now working, must complete teardown
// and only then write the note.
func TestReap_Scan_WorktreeRemoveFailed_NoNote_RetriedNextTick(t *testing.T) {
	worktree := "/tmp/reap-wt-retry"
	sessionID := "sess-uuid-retry"
	iss := reapReviewIssue("at-retry", "closed", reapFixedNow.Add(-time.Hour), "", worktree, sessionID, "")
	sessions := []agentSession{{ID: "abcretry", SessionID: sessionID, CWD: worktree}}

	// Tick 1: session teardown succeeds, but removal fails.
	var stops1 fakeStops
	var rms1 fakeRm
	remover1 := fakeWorktreeRemover{err: fmt.Errorf("simulated removal timeout")}
	var noter1 fakeNoter
	verb1 := newReapVerb(sessions, &stops1, &rms1, &remover1, &noter1, alwaysClean)

	home := t.TempDir()
	ctx1, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), home)
	if err := verb1.Run(ctx1); err != nil {
		t.Fatalf("tick 1: unexpected error: %v", err)
	}
	if len(stops1.stopped) != 1 || stops1.stopped[0] != "abcretry" {
		t.Errorf("tick 1: expected the session torn down; got %v", stops1.stopped)
	}
	if len(remover1.removed) != 1 || remover1.removed[0] != worktree {
		t.Errorf("tick 1: expected removal attempted; got %v", remover1.removed)
	}
	if len(noter1.noted) != 0 {
		t.Errorf("tick 1: expected NO reaped note when removal fails; got %v", noter1.noted)
	}

	// Tick 2: same un-noted issue, removal now succeeds.
	var stops2 fakeStops
	var rms2 fakeRm
	var remover2 fakeWorktreeRemover
	var noter2 fakeNoter
	verb2 := newReapVerb(sessions, &stops2, &rms2, &remover2, &noter2, alwaysClean)

	ctx2, _, _ := makeCtx(reapScanFakeBD([]bd.Issue{iss}), home)
	if err := verb2.Run(ctx2); err != nil {
		t.Fatalf("tick 2: unexpected error: %v", err)
	}
	if len(remover2.removed) != 1 || remover2.removed[0] != worktree {
		t.Errorf("tick 2: expected the retried removal to succeed; got %v", remover2.removed)
	}
	if len(noter2.noted) != 1 || noter2.noted[0] != "at-retry" {
		t.Errorf("tick 2: expected the reaped note written once teardown fully completes; got %v", noter2.noted)
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

// ── defaultWorktreeClean: deletion-corpse classification (wtDirtyRecoverable, agent-teams-442q.11) ──
// A `git worktree remove` killed mid-operation is a recursive unlink of
// working-tree files that never touches the index, then aborts, leaving HEAD
// and the index intact: git status --porcelain shows only UNSTAGED
// tracked-file deletions (" D"), never a staged deletion ("D "), untracked,
// add, modify, rename, or unmerged entry. These five cases prove
// defaultWorktreeClean tells that exact signature apart from every other
// kind of "dirty", including the staged-deletion case a killed removal can
// never produce (agent-teams-442q.14/Finding 1).

// TestReap_DefaultWorktreeClean_PureTrackedDeletion_Recoverable covers the
// deletion-corpse signature itself: a tracked file deleted from disk (never
// re-added or re-committed) reports wtDirtyRecoverable with HEAD's resolved
// sha — every "missing" file is still in HEAD, so a caller's gh-verify
// override has proof to check against.
func TestReap_DefaultWorktreeClean_PureTrackedDeletion_Recoverable(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	filePath := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(filePath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-m", "add tracked file")

	shaOut, shaErr := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if shaErr != nil {
		t.Fatalf("git rev-parse HEAD: %v", shaErr)
	}
	wantSHA := strings.TrimSpace(string(shaOut))

	if err := os.Remove(filePath); err != nil {
		t.Fatalf("delete tracked file: %v", err)
	}

	exists, status, headSHA, err := defaultWorktreeClean(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true for a real directory")
	}
	if status != wtDirtyRecoverable {
		t.Fatalf("expected status=wtDirtyRecoverable for a pure tracked-file deletion; got %v", status)
	}
	if headSHA != wantSHA {
		t.Errorf("expected headSHA %q; got %q", wantSHA, headSHA)
	}
}

// TestReap_DefaultWorktreeClean_UntrackedFile_Dirty covers an untracked file
// alone: never a pure deletion, so it must stay wtDirty even though nothing
// tracked changed.
func TestReap_DefaultWorktreeClean_UntrackedFile_Dirty(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "--allow-empty", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	exists, status, _, err := defaultWorktreeClean(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true for a real directory")
	}
	if status != wtDirty {
		t.Fatalf("expected status=wtDirty for an untracked file (not a pure deletion); got %v", status)
	}
}

// TestReap_DefaultWorktreeClean_StagedModification_Dirty covers a staged
// modification: content NOT at HEAD, so it can never be proven recoverable
// via gh-verify — this is exactly why the predicate is deletions-only.
func TestReap_DefaultWorktreeClean_StagedModification_Dirty(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	filePath := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(filePath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-m", "add tracked file")

	if err := os.WriteFile(filePath, []byte("modified\n"), 0o644); err != nil {
		t.Fatalf("modify tracked file: %v", err)
	}
	runGit(t, dir, "add", "tracked.txt")

	exists, status, _, err := defaultWorktreeClean(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true for a real directory")
	}
	if status != wtDirty {
		t.Fatalf("expected status=wtDirty for a staged modification; got %v", status)
	}
}

// TestReap_DefaultWorktreeClean_DeletionPlusUntracked_Dirty covers a mix: one
// pure tracked-file deletion PLUS one untracked file. A single non-deletion
// line anywhere in the porcelain must disqualify the whole worktree.
func TestReap_DefaultWorktreeClean_DeletionPlusUntracked_Dirty(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	filePath := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(filePath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-m", "add tracked file")

	if err := os.Remove(filePath); err != nil {
		t.Fatalf("delete tracked file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	exists, status, _, err := defaultWorktreeClean(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true for a real directory")
	}
	if status != wtDirty {
		t.Fatalf("expected status=wtDirty for a deletion mixed with an untracked file; got %v", status)
	}
}

// TestReap_DefaultWorktreeClean_StagedDeletion_Dirty covers a STAGED deletion
// (`git rm`, never committed — porcelain "D ", not " D"): a killed `git
// worktree remove` can never produce this (it never touches the index), so
// it must NOT be classified as wtDirtyRecoverable — it means an agent
// deliberately staged a removal and never committed, real uncommitted intent
// that the deletion-corpse override must not paper over (agent-teams-
// 442q.14/Finding 1).
func TestReap_DefaultWorktreeClean_StagedDeletion_Dirty(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	filePath := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(filePath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-m", "add tracked file")

	runGit(t, dir, "rm", "tracked.txt")

	exists, status, _, err := defaultWorktreeClean(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true for a real directory")
	}
	if status != wtDirty {
		t.Fatalf("expected status=wtDirty for a staged deletion (uncommitted intent, not the corpse signature); got %v", status)
	}
}

// ── reapRemoveWorktreeWithTimeout (F1 fix) ──────────────────────────────────

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

// TestReapRemoveWorktreeWithTimeout_RealWorktree_Succeeds is the happy-path
// proof that switching to a shared bounded context (boundedGitRunner)
// didn't break real removal: a real worktree, removed through the
// production steady-state constant (reapWorktreeRemoveTimeout), actually
// disappears from disk and from `git worktree list`.
func TestReapRemoveWorktreeWithTimeout_RealWorktree_Succeeds(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repoRoot, wtPath := initRepoWithWorktree(t, "reap-remove-test")

	if err := reapRemoveWorktreeWithTimeout(wtPath, reapWorktreeRemoveTimeout); err != nil {
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

// ── end-of-scan summary line (agent-teams-6hgr.1) ───────────────────────────

// TestReap_Scan_SummaryLine_CorrectCounts builds one closed initiative per
// distinct outcome the summary aggregates, then asserts the single printed
// "reap: scan summary — ..." line — always printed, --dry-run/--bulk or not
// — carries the exact counts feeding it (agent-teams-6hgr.5 depends on this
// line's shape to surface it in pr-shepherd's logs).
func TestReap_Scan_SummaryLine_CorrectCounts(t *testing.T) {
	wtRemoved := "/tmp/reap-wt-sum-1" // clean => worktree-removed
	wtForced := "/tmp/reap-wt-sum-2"  // dirty + already-reaped => worktree-removed-forced, revisited
	wtAbsent := "/tmp/reap-wt-sum-3"  // absent => worktree-absent, no matching session
	wtFailed := "/tmp/reap-wt-sum-4"  // clean, but removal itself errors => worktree-remove-failed

	issues := []bd.Issue{
		reapReviewIssue("at-sum-1", "closed", reapFixedNow.Add(-time.Hour), "", wtRemoved, "sess-sum-1", ""),
		reapReviewIssue("at-sum-2", "closed", reapFixedNow.Add(-time.Hour), "reaped: 2026-09-16T09:00:00Z", wtForced, "sess-sum-2", ""),
		reapReviewIssue("at-sum-3", "closed", reapFixedNow.Add(-time.Hour), "", wtAbsent, "sess-sum-3-unused", ""),
		reapReviewIssue("at-sum-4", "closed", reapFixedNow.Add(-time.Hour), "", wtFailed, "sess-sum-4", ""),
		reapReviewIssue("at-sum-5", "closed", reapFixedNow.Add(-5*time.Minute), "", "/tmp/reap-wt-sum-5", "sess-sum-5", ""), // within grace
		reapReviewIssue("at-sum-6", "closed", reapFixedNow.Add(-time.Hour), "", "/tmp/reap-wt-sum-6", "sess-sum-6", "codex"),
	}
	sessions := []agentSession{
		{ID: "abc-sum-1", SessionID: "sess-sum-1", CWD: wtRemoved},
		{ID: "abc-sum-2", SessionID: "sess-sum-2", CWD: wtForced},
		// at-sum-3 has no matching live session at all.
		{ID: "abc-sum-4", SessionID: "sess-sum-4", CWD: wtFailed},
	}

	// worktreeClean dispatches per-path: absent for wtAbsent, dirty for
	// wtForced, clean for everything else (wtRemoved and wtFailed alike —
	// wtFailed's failure comes from the removeWorktree seam, not its status).
	clean := func(worktree string) (bool, worktreeGitStatus, string, error) {
		switch worktree {
		case wtAbsent:
			return false, wtClean, "", nil
		case wtForced:
			return true, wtDirty, "", nil
		default:
			return true, wtClean, "", nil
		}
	}

	var stops fakeStops
	var rms fakeRm
	var noter fakeNoter
	removeWorktree := func(worktree string, timeout time.Duration) error {
		if worktree == wtFailed {
			return fmt.Errorf("simulated removal timeout")
		}
		return nil
	}

	verb := &reapKong{
		Grace:          defaultReapGrace,
		ScanDeadline:   reapScanSoftDeadlineDefault,
		Max:            reapScanBatchDefault,
		agentsFunc:     reapFakeAgents(sessions),
		now:            func() time.Time { return reapFixedNow },
		stopSession:    stops.stopFunc(),
		rmSession:      rms.fn(),
		removeWorktree: removeWorktree,
		worktreeClean:  clean,
		worktreeExists: func(worktree string) bool {
			exists, _, _, _ := clean(worktree)
			return exists
		},
		ghCommitPresent: func(string, string) (bool, error) { return false, nil },
		noteFunc:        noter.fn(),
		notifyCtx:       defaultReapNotifyCtx,
		prState:         reapAlwaysMergedPRState,
		ghPreflight:     reapAlwaysGHOk,
	}

	ctx, stdout, _ := makeCtx(reapScanFakeBD(issues), t.TempDir())
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "reap: scan summary — review=6 processed=4 already-reaped-revisited=1 sessions-torn-down=3 worktrees-removed=2 worktrees-already-gone=1 worktrees-skipped=0 worktrees-failed=1 worktrees-kept-pr-state=0 grace-skipped=1 codex-skipped=1"
	if !strings.Contains(stdout.String(), want) {
		t.Errorf("expected summary line %q; got stdout %q", want, stdout.String())
	}

	// Sanity-check the counts against the fakes directly, so a wrong summary
	// formula can't coincidentally match a wrong set of side effects.
	if len(noter.noted) != 2 {
		t.Errorf("expected exactly 2 reaped notes (at-sum-1, at-sum-3 — at-sum-2 already noted, at-sum-4 failed); got %v", noter.noted)
	}
}
