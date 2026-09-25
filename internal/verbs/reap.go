// Package verbs — reap.go implements the `ateam reap [target]` verb.
//
// reap is the loop-closing teardown for a review-pr initiative: once a review
// session's PR-review work is done and its initiative closes, the Claude
// session and its git worktree still sit around (the harness never reclaims
// either on its own — see the WORKTREE REMOVAL note below). reap has two
// modes, selected by the optional positional target:
//
//   - SCAN mode (no target): what pr-shepherd calls every tick. Enumerates
//     CLOSED initiatives, applies the review-shape / grace gates, and tears
//     down each survivor (session + worktree removal). A review-shaped
//     worktree is force-removed regardless of its git dirty/unpushed state —
//     it checks out the PR author's branch and never carries changes meant
//     to be pushed from here, so the clean-only gating a normal work
//     worktree needs doesn't apply (contract: agent-teams-sbh8.1, superseded
//     2026-09-23 — see its SUPERSEDED-BY note; agent-teams-6hgr.1). It is
//     NOT, however, removed while its own PR is still open: agent-teams-8st0.13
//     gates the removal on the PR reaching MERGED or CLOSED (reusing
//     hung_tick.go's gh probe/cache), keeping the worktree available for a
//     late re-review or comment-reply request; an inconclusive probe keeps
//     it too, retried next tick, same as any other unresolved teardown. An
//     already-reaped initiative (a durable "reaped:" bd note) is NOT
//     skipped: it is re-swept every tick like any other survivor, so a
//     leftover worktree a prior scan left behind, or a reopened-then-
//     reclosed initiative's new session, is retried until its teardown
//     actually completes — the note only prevents a DUPLICATE note being
//     written, never a repeat sweep (agent-teams-6hgr.1, replacing the old
//     write-once-skip). Gating trusts the initiative's own closed state for
//     session/worktree LIVENESS — reap never probes session status itself
//     (contract: agent-teams-sbh8.15, "trust the initiative state") — but
//     the PR-merged/closed gate above is a deliberate, narrow exception,
//     scoped to whether the worktree may be removed at all.
//   - ONE-OFF mode (a target given): reaps exactly that one target right now,
//     bypassing every gate — an explicit human action, not necessarily a
//     review-pr one.
//
// reap is distinct from the sibling `ateam reap-orphans` (reap_orphans.go):
// that verb only stops background sessions whose worktree cwd vanished; this
// verb does full teardown (session + worktree) for both scan and one-off
// targets. See contract bead agent-teams-sbh8.1 (frozen, authoritative over
// this file's comments) for the decisions behind every gate and ordering
// choice below.
package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// defaultReapGrace is SCAN mode's default minimum time since bd close before
// an initiative becomes reap-eligible (contract: "GRACE CLOCK"). A target
// given in ONE-OFF mode ignores this entirely.
const defaultReapGrace = 20 * time.Minute

// reapGitTimeout bounds every OTHER git subprocess reap itself runs directly
// — the worktree clean-check's own git calls (defaultWorktreeClean) and the
// best-effort `git worktree prune` after a successful removal — so a wedged
// git can't stall a scan tick. Mirrors gitProbeTimeout's reasoning
// (hung_workproduct.go). It does NOT bound the actual `git worktree remove`
// (nor its git-common-dir resolve): those run under the separate, longer
// reapWorktreeRemoveTimeout below, because a legitimate large-worktree
// removal can take longer than this constant allows. (This comment used to
// claim reapGitTimeout bounded removal too — false: that path ran through
// gitutil's plain exec.Command with no context or timeout at all, review
// finding agent-teams-442q.9/F1. See reapWorktreeRemoveTimeout's doc comment
// for the fix and the combined-tick timing arithmetic, including the
// current 90s pr-shepherd hard budget — PR #5, up from the 30s this comment
// used to assume.)
const reapGitTimeout = 5 * time.Second

// RegisterReapKong registers the reap verb onto p.
func RegisterReapKong(p *cli.Parser) {
	p.AddVerb("reap", "Tear down closed review sessions past grace (scan mode, what pr-shepherd calls every tick) or one target right now (one-off mode); see reap-orphans for stop-only cwd-missing cleanup.", &reapKong{
		agentsFunc:      defaultAgentsJSONAll,
		now:             time.Now,
		stopSession:     defaultStopSession,
		rmSession:       defaultRmSession,
		removeWorktree:  reapRemoveWorktreeWithTimeout,
		worktreeClean:   defaultWorktreeClean,
		worktreeExists:  defaultWorktreeExists,
		ghCommitPresent: defaultGHCommitPresent,
		noteFunc:        defaultReapNote,
		notifyCtx:       defaultReapNotifyCtx,
		prState:         defaultPRState,
		ghPreflight:     defaultHungReviewCommentPreflight,
	})
}

// rmSessionFunc removes a claude session by short id. Injected so tests
// substitute a fake without executing real claude commands.
type rmSessionFunc func(id string) error

// reapRemoveWorktreeFunc removes worktree from disk, resolving its owning
// repo root itself so callers never need to know or pass it, bounded by the
// given timeout. Injected so tests substitute a fake without a real git
// subprocess, and so removeWorktreeIfClean can choose a mode-aware bound —
// short for steady-state, generous for --bulk (agent-teams-442q.13; see
// reapWorktreeRemoveTimeout/reapBulkWorktreeRemoveTimeout).
type reapRemoveWorktreeFunc func(worktree string, timeout time.Duration) error

// worktreeGitStatus classifies a worktree's git-inspection result — the ONE
// git-inspection path shared by the steady-state clean-only gate
// (removeWorktreeIfClean) and the bulk-mode gh-verify override
// (bulkGHVerifyRemovable), so neither re-runs `git status --porcelain` /
// `git rev-list` separately for the same worktree.
type worktreeGitStatus int

const (
	// wtDirty: porcelain non-empty with at least one change that is not a
	// pure tracked-file deletion (an untracked file, a staged add/modify, a
	// rename/copy, or an unmerged conflict — see wtDirtyRecoverable for the
	// deletions-only case), or the git inspection itself was inconclusive (a
	// status/rev-list subprocess failure, or an unresolvable HEAD sha).
	// ALWAYS skipped, bulk mode included — any non-deletion working-tree
	// change, or an unresolvable check, always wins over any gh-verify
	// override; the contract requires PROOF of no unpushed/uncommitted work,
	// not merely absence of proof of some.
	wtDirty worktreeGitStatus = iota
	// wtDirtyRecoverable: porcelain non-empty, but EVERY line is a pure,
	// UNSTAGED tracked-file deletion (" D" only — never "D ") — no
	// untracked, staged, modify, rename, copy, or unmerged entry. This is
	// the signature a `git worktree remove` leaves when killed
	// mid-operation (see reapWorktreeRemoveTimeout's doc comment): it is a
	// recursive unlink of working-tree files that never touches the index,
	// so it can only ever produce unstaged deletions, then aborts, leaving
	// HEAD and the index intact. Every "missing" file is still in HEAD, so
	// once gh-verify (bulkGHVerifyRemovable) proves HEAD is on GitHub,
	// removing the rest of this corpse loses nothing. A STAGED deletion
	// ("D ") is deliberately excluded (agent-teams-442q.14/Finding 1): it
	// means an agent ran `git rm` and never committed — real, uncommitted
	// intent a killed `git worktree remove` cannot produce — so it stays
	// wtDirty. Steady-state (non-bulk) treats this exactly like wtDirty —
	// skip, no gh call; only bulk mode may override it, the same way it
	// overrides wtUnpushed below.
	wtDirtyRecoverable
	// wtUnpushed: porcelain empty, but HEAD carries commits absent from
	// every local remote-tracking ref. Steady-state (non-bulk) treats this
	// exactly like wtDirty — skip, no gh call. Bulk mode alone may override
	// it via bulkGHVerifyRemovable: this is the false-positive case a
	// checked-out PR branch produces once its branch is deleted on GitHub
	// after merge, pruning the local tracking ref even though GitHub's own
	// history still has the commit.
	wtUnpushed
	// wtClean: porcelain empty AND every HEAD commit is reachable from some
	// remote-tracking ref. Always safe to remove — no gh check needed.
	wtClean
)

// worktreeCleanFunc classifies worktree's git state: exists is false when the
// path is not present on disk at all (a missing worktree is a clean no-op,
// never dirty). When exists is true, status is one of wtDirty/
// wtDirtyRecoverable/wtUnpushed/wtClean (meaningless when exists is false);
// headSHA is worktree's resolved HEAD commit, populated for
// status==wtUnpushed and status==wtDirtyRecoverable — the two cases a caller
// might need it for a gh-verify override — and empty otherwise. Injected so
// tests substitute a fake without a real git subprocess.
type worktreeCleanFunc func(worktree string) (exists bool, status worktreeGitStatus, headSHA string, err error)

// worktreeExistsFunc reports whether worktree is present on disk as a
// directory — the same existence guard worktreeCleanFunc's own os.Stat
// applies, standing alone with no accompanying git status/rev-list probe.
// removeWorktreeIfClean's review-shaped force-remove path (prURL != "") needs
// only this: dirty/unpushed git state is irrelevant to that decision (see its
// doc comment), so running worktreeClean's ~15s of sequential git
// subprocesses there is pure waste that eats into the scan tick's timeout
// budget (review finding on agent-teams-6hgr.1's force-remove change).
// Injected so tests substitute a fake without touching a real filesystem.
type worktreeExistsFunc func(worktree string) bool

// ghCommitPresentFunc reports whether GitHub still retains commit sha in
// ownerRepo (owner/repo, lower-cased, as parsePrURL returns it) — the bulk
// mode gh-verify override's sole probe. Injected so tests substitute a fake
// without shelling to a real gh binary.
type ghCommitPresentFunc func(ownerRepo, sha string) (present bool, err error)

// reapNoteFunc writes the durable "reaped: <RFC3339>" bd note on id that
// makes reap at-most-once per initiative. Injected so tests substitute a
// fake instead of shelling to a real bd binary.
type reapNoteFunc func(ctx *cli.Context, id string, at time.Time) error

// reapNotifyCtxFunc returns the context SCAN mode runs under and its cancel
// func. Injected so tests can simulate a mid-scan cancellation (a SIGTERM
// arriving) deterministically, without sending a real OS signal to the test
// process — that would risk killing the whole `go test` run.
type reapNotifyCtxFunc func() (context.Context, context.CancelFunc)

// defaultReapNotifyCtx is the production reapNotifyCtxFunc: a context
// cancelled when the process receives SIGTERM (pr-shepherd's own hard-budget
// kill signal), so runScan's loop can notice and stop starting new survivors
// — and, in the steady state, so it never needs to: the soft scan deadline
// below already exits well before pr-shepherd's 30s budget fires.
func defaultReapNotifyCtx() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGTERM)
}

// reapKong implements `ateam reap [target]`. The DI fields are tagged
// kong:"-" so kong ignores them; tests substitute fakes without touching the
// struct registration — same pattern as reapOrphansKong.
type reapKong struct {
	Target       string        `arg:"" name:"target" optional:"" help:"Initiative id or a bare Claude short session id to reap right now, bypassing the grace/reaped-note gates. Omit to scan every closed review initiative (what pr-shepherd calls every tick)."`
	Grace        time.Duration `name:"grace" default:"20m" help:"Scan mode only: minimum time since bd close before an initiative becomes reap-eligible."`
	ScanDeadline time.Duration `name:"scan-deadline" default:"15s" help:"Scan mode only: soft wall-clock budget — stop starting new survivors once elapsed exceeds this, so the tick exits cleanly under pr-shepherd's 30s hard budget. 0 = unbounded (for an unbudgeted human-run bulk clear)."`
	Max          int           `name:"max" default:"0" help:"Scan mode only: max survivors to tear down in one tick. 0 = unbounded (the soft deadline governs steady-state ticks)."`
	Bulk         bool          `name:"bulk" help:"Scan mode only: one-time human-invoked unbudgeted drain. Forces --scan-deadline=0 and --max=0, also re-sweeps already-reaped initiatives whose worktree is still present on disk, and prints running progress to stderr. A clean worktree whose HEAD is missing from every local remote (its PR branch deleted on GitHub after merge) is verified via gh before removal, never forced. Pair with --dry-run to preview first."`
	DryRun       bool          `name:"dry-run" help:"Scan mode only: report what would be torn down without stopping any session, removing any worktree, or writing any reaped note."`

	agentsFunc      agentsJSONFunc         `kong:"-"`
	now             func() time.Time       `kong:"-"`
	stopSession     stopSessionFunc        `kong:"-"`
	rmSession       rmSessionFunc          `kong:"-"`
	removeWorktree  reapRemoveWorktreeFunc `kong:"-"`
	worktreeClean   worktreeCleanFunc      `kong:"-"`
	worktreeExists  worktreeExistsFunc     `kong:"-"`
	ghCommitPresent ghCommitPresentFunc    `kong:"-"`
	noteFunc        reapNoteFunc           `kong:"-"`
	notifyCtx       reapNotifyCtxFunc      `kong:"-"`

	// prState and ghPreflight are agent-teams-8st0.13's PR-merged/closed gate
	// on a review-shaped worktree's removal — the exact seams hung_tick.go
	// wires as hungTickDeps.prState/ghPreflight (defaultPRState,
	// defaultHungReviewCommentPreflight), reused here rather than a second gh
	// probe. removeWorktreeIfClean's review-shaped branch builds a
	// hungPRStateProbe from these once per scan (sharing hung-tick's own
	// StewardHome-relative TTL cache file, hung-pr-state-cache.json — safe to
	// share: reads/writes are per-tick snapshots, and saveHungPRStateCache
	// writes atomically via temp-file-then-rename, so a concurrent hung-tick
	// write can only ever cost a redundant probe next tick, never a
	// corrupted file or a wrong gate decision). nil-safe: a nil prState makes
	// hungPRStateProbe.evaluate report probed=false, which this gate treats
	// exactly like an OPEN PR — keep the worktree, retry next tick.
	prState     prStateFunc  `kong:"-"`
	ghPreflight func() error `kong:"-"`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *reapKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam reap: nil context")
	}

	// Dependency guard: claude must be in PATH (mirrors reap_orphans.go).
	// The codex path additionally needs codex on PATH, but codex teardown
	// itself is Ring 1 and not implemented here (see resolveOneOffTarget).
	if _, err := exec.LookPath("claude"); err != nil {
		return cli.Depf("ateam reap: 'claude' not found in PATH")
	}
	if c.Target != "" && (c.Bulk || c.DryRun) {
		return fmt.Errorf("ateam reap: --bulk and --dry-run are scan-mode only; omit the target")
	}
	if c.Bulk {
		// Bulk-clear implies an unbounded scan: no soft deadline, no batch cap.
		c.ScanDeadline = 0
		c.Max = 0
	}

	if c.Target == "" {
		// SCAN mode only: a context cancelled on SIGTERM — pr-shepherd's own
		// hard-budget kill signal — so runScan's loop can notice and stop
		// starting new survivors instead of being killed mid-teardown and
		// orphaning a grandchild `claude` process.
		scanCtx, cancel := c.notifyCtx()
		defer cancel()
		return c.runScan(ctx, scanCtx)
	}
	return c.runOneOff(ctx, c.Target)
}

// ── SCAN mode ────────────────────────────────────────────────────────────────

// runScan enumerates closed initiatives via ctx.BD (the global initiative
// registry) and reaps every review-shaped, not-yet-reaped survivor past
// grace. Never hard-errors on a single initiative (log + continue) — only a
// whole-scan failure (the bd list call itself) returns non-nil, per the
// contract's exit rule.
//
// scanCtx is checked before starting each survivor's teardown, alongside the
// wall-clock soft deadline and batch bound: once any of the three trips, the
// loop stops STARTING new work and falls through to the end-of-scan summary
// print — the survivor already in flight finishes (each of its own
// subprocess calls is separately bounded by runBoundedClaude), but no
// further one starts. This is what lets one tick exit cleanly under
// pr-shepherd's 30s hard budget no matter how large the backlog is, leaving
// the rest for the next tick.
//
// Prints one end-of-scan summary line to ctx.Stdout unconditionally (Eric,
// 2026-09-23) — the human-visible aggregate of the per-initiative outcomes
// already journaled to reap-journal.jsonl, which is what pr-shepherd
// surfaces in its own logs (the log-level change making it visible there is
// a separate bead, agent-teams-6hgr.5). Printed on every exit path,
// including the three early-stop cases above and dry-run — never gated
// behind --dry-run or --bulk.
// reapScanSummary aggregates one scan tick's per-initiative outcomes into
// the single end-of-scan summary line runScan prints to ctx.Stdout (Eric,
// 2026-09-23) — the human-visible complement to the per-attempt entries
// already appended to reap-journal.jsonl, and what pr-shepherd surfaces in
// its own logs (agent-teams-6hgr.5 makes that visible; this struct is the
// reap.go half). Counts are populated by recordPreview (--dry-run: no real
// teardown happened) or recordReal (a real attempt was made), never both for
// the same survivor.
type reapScanSummary struct {
	reviewInitiatives      int // every review-shaped issue this tick saw, before any gate
	graceSkipped           int // skip-grace: not yet past c.Grace
	badClosedAt            int // skip-bad-closedat: closed_at missing/unparseable
	codexSkipped           int // skip-codex-ring1: codex teardown not implemented here
	processed              int // reached a real or previewed teardown attempt
	alreadyReapedRevisited int // of processed, already carried a reaped: note
	sessionsTornDown       int // session action == "reaped" (real teardown only)
	worktreesRemoved       int // worktree outcome is a "removed" variant (real only)
	worktreesAlreadyGone   int // worktree outcome == worktree-absent (real only)
	worktreesSkipped       int // worktree outcome == worktree-dirty-skipped (real only)
	worktreesFailed        int // worktree outcome == worktree-remove-failed (real only)
	worktreesKeptPRState   int // worktree outcome is a "kept-pr-*" variant — PR not yet merged/closed, or the probe was inconclusive (real only; agent-teams-8st0.13)
}

// recordPreview tallies one --dry-run survivor: only processed/
// alreadyReapedRevisited move, since a preview never touches a session or a
// worktree.
func (s *reapScanSummary) recordPreview(alreadyReaped bool) {
	s.processed++
	if alreadyReaped {
		s.alreadyReapedRevisited++
	}
}

// recordReal tallies one real (non-dry-run) survivor's actual teardown
// outcome — action from teardownClaudeSession, wtOutcome from
// removeWorktreeIfClean.
func (s *reapScanSummary) recordReal(alreadyReaped bool, action, wtOutcome string) {
	s.processed++
	if alreadyReaped {
		s.alreadyReapedRevisited++
	}
	if action == "reaped" {
		s.sessionsTornDown++
	}
	switch wtOutcome {
	case "worktree-removed", "worktree-removed-forced", "worktree-removed-gh-verified", "worktree-removed-corpse-gh-verified":
		s.worktreesRemoved++
	case "worktree-absent":
		s.worktreesAlreadyGone++
	case "worktree-dirty-skipped":
		s.worktreesSkipped++
	case "worktree-remove-failed":
		s.worktreesFailed++
	case "worktree-kept-pr-open", "worktree-kept-pr-unknown":
		s.worktreesKeptPRState++
	}
}

// isKeptPRStateOutcome reports whether wtOutcome is one of
// removeWorktreeIfClean's two review-shaped "kept, PR not yet terminal"
// outcomes (agent-teams-8st0.13): "worktree-kept-pr-open" or
// "worktree-kept-pr-unknown". runScan's journal-skip gate below uses this to
// recognize a tick that did nothing new.
func isKeptPRStateOutcome(wtOutcome string) bool {
	return wtOutcome == "worktree-kept-pr-open" || wtOutcome == "worktree-kept-pr-unknown"
}

// String renders the one-line, stable/parseable-ish summary runScan prints
// at the end of every scan tick.
func (s reapScanSummary) String() string {
	return fmt.Sprintf(
		"review=%d processed=%d already-reaped-revisited=%d sessions-torn-down=%d worktrees-removed=%d worktrees-already-gone=%d worktrees-skipped=%d worktrees-failed=%d worktrees-kept-pr-state=%d grace-skipped=%d codex-skipped=%d",
		s.reviewInitiatives, s.processed, s.alreadyReapedRevisited, s.sessionsTornDown, s.worktreesRemoved, s.worktreesAlreadyGone, s.worktreesSkipped, s.worktreesFailed, s.worktreesKeptPRState, s.graceSkipped, s.codexSkipped,
	)
}

func (c *reapKong) runScan(ctx *cli.Context, scanCtx context.Context) error {
	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--status=closed", "--json"); err != nil {
		return fmt.Errorf("ateam reap: list closed initiatives: %w", err)
	}

	now := c.now()
	callerID := os.Getenv("CLAUDE_SESSION_ID")

	// Hoisted OUT of the loop: one `claude agents` call serves every survivor
	// this tick, reused below for both the teardown match and the
	// caller-cwd guard. Previously called once PER survivor — the dominant
	// cause of a tick never finishing, since a single hang stalled every
	// subsequent survivor before any of them could be marked reaped.
	sessions, sessErr := c.agentsFunc()

	// prProbe is agent-teams-8st0.13's PR-merged/closed gate on a
	// review-shaped worktree's removal, built once for the whole tick so its
	// TTL cache (shared with hung_tick.go's own probe) is loaded once and
	// flushed once, not per survivor.
	prProbe := newHungPRStateProbe(ctx, hungTickDeps{now: c.now, prState: c.prState, ghPreflight: c.ghPreflight})
	defer prProbe.flush()

	// Bulk-clear only: an upfront count (same gates the loop below applies)
	// so a human watching stderr sees the drain's size before minutes of
	// per-worktree removal work, plus a running "N/M" line per survivor.
	var total, processed int
	if c.Bulk {
		total = c.bulkEligibleCount(issues, now)
		verb := "reaping"
		if c.DryRun {
			verb = "would reap"
		}
		fmt.Fprintf(ctx.Stderr, "reap --bulk: %d eligible survivor(s), %s\n", total, verb)
	}

	scanStart := time.Now()
	var reaped int
	var summary reapScanSummary

	for _, iss := range issues {
		prURL, ok := initiative.ReviewPRURL(iss)
		if !ok {
			continue // not review-shaped: untouched, never reap's concern
		}
		summary.reviewInitiatives++
		f := initiative.Of(iss)
		if f.Runtime == "codex" {
			summary.codexSkipped++
			c.journal(ctx, now, iss.ID, "", f.Runtime, "scan", "skip-codex-ring1", "")
			continue // Ring 1: codex teardown is not implemented by this verb
		}
		// alreadyReaped no longer skips this initiative (agent-teams-6hgr.1
		// — a durable note used to be the SOLE gate keeping a scan from
		// ever revisiting an initiative, which stranded a leftover worktree
		// or a reopened-then-reclosed session's new session behind it
		// forever). It still gates the note-write below to write-once: a
		// re-swept initiative that already has a note never gets a
		// duplicate one.
		alreadyReaped := hasReapedNote(iss.Notes)

		closedAt, err := time.Parse(time.RFC3339, iss.ClosedAt)
		if iss.ClosedAt == "" || err != nil {
			// Distinct from "skip-grace" below: this initiative is
			// permanently stuck (closed_at will never become parseable on a
			// later tick), whereas "skip-grace" self-resolves.
			fmt.Fprintf(ctx.Stderr, "reap: %s: closed_at missing or unparseable (%q), skipping\n", iss.ID, iss.ClosedAt)
			summary.badClosedAt++
			c.journal(ctx, now, iss.ID, "", f.Runtime, "scan", "skip-bad-closedat", "")
			continue
		}
		if now.Sub(closedAt) < c.Grace {
			summary.graceSkipped++
			c.journal(ctx, now, iss.ID, "", f.Runtime, "scan", "skip-grace", "")
			continue
		}

		// Soft budget check, right before the expensive part (teardown):
		// skip-only iterations above never cost meaningful wall-clock time,
		// so they don't count against it. A trip here stops STARTING new
		// survivors but falls through to the end-of-scan summary print
		// below rather than returning directly, so every exit path reports
		// what the tick actually did.
		stop := false
		select {
		case <-scanCtx.Done():
			stop = true
		default:
		}
		if !stop && c.ScanDeadline > 0 && time.Since(scanStart) >= c.ScanDeadline {
			stop = true
		}
		if !stop && c.Max > 0 && reaped >= c.Max {
			stop = true
		}
		if stop {
			break
		}

		if c.DryRun {
			// Read-only preview: query session/worktree state through the
			// same seams but never call stop/rm/remove/note.
			sessOutcome := c.previewSessionOutcome(sessions, sessErr, matchInitiativeSession(f), callerID)
			wtOutcome := c.previewWorktreeOutcome(ctx, f.Worktree, callerWorktreeCWD(sessions, callerID), prURL, iss.ID, prProbe)
			processed++
			summary.recordPreview(alreadyReaped)
			if c.Bulk {
				fmt.Fprintf(ctx.Stderr, "reap --bulk --dry-run: [%d/%d] %s session=%s worktree=%s (%s)\n", processed, total, iss.ID, sessOutcome, wtOutcome, f.Worktree)
			}
			continue
		}

		action := c.teardownClaudeSession(ctx, sessions, sessErr, matchInitiativeSession(f), callerID)
		wtOutcome := c.removeWorktreeIfClean(ctx, f.Worktree, callerWorktreeCWD(sessions, callerID), prURL, iss.ID, prProbe)
		// agent-teams-8st0.13 (Eric, follow-up): a still-open-PR survivor is
		// revisited every tick until its PR reaches a terminal state, but once
		// the session half is already done — action=="no-session", nothing
		// left to tear down — and the worktree is STILL kept for the same
		// reason as last tick, journaling again only repeats what the
		// previous entry already said. Skip that one exact no-op combination,
		// stateless: no persisted marker, just this tick's own action/
		// wtOutcome values. Any tick that did something — the session was
		// actually torn down (action=="reaped", the first sighting) or the
		// worktree outcome is anything other than still-kept (removed, or a
		// real failure) — still journals normally.
		if action != "no-session" || !isKeptPRStateOutcome(wtOutcome) {
			c.journal(ctx, now, iss.ID, "", f.Runtime, "scan", action, wtOutcome)
		}
		summary.recordReal(alreadyReaped, action, wtOutcome)

		if action != "failed" {
			// Written only on FULL teardown success: the session action
			// didn't fail AND the worktree is actually gone — removed just
			// now, or already absent (worktreeTornDown). A
			// worktree-remove-failed, dirty-skipped, unknown, or
			// skipped-caller-cwd outcome withholds the note so this
			// initiative is retried next tick instead of silently marked
			// done (agent-teams-6hgr.1, bug 2a — a worktree-remove-failed
			// outcome used to get noted anyway whenever the session action
			// alone hadn't failed). Skipped when alreadyReaped: the note is
			// write-once, so a re-sweep that only finishes removing a
			// leftover worktree never appends a duplicate.
			if worktreeTornDown(wtOutcome) && !alreadyReaped {
				if err := c.noteFunc(ctx, iss.ID, now); err != nil {
					fmt.Fprintf(ctx.Stderr, "reap: %s: write reaped note: %v\n", iss.ID, err)
				}
			}
			reaped++
		}

		if c.Bulk {
			processed++
			fmt.Fprintf(ctx.Stderr, "reap --bulk: [%d/%d] %s session=%s worktree=%s\n", processed, total, iss.ID, action, wtOutcome)
		}
	}

	if c.Bulk {
		fmt.Fprintf(ctx.Stderr, "reap --bulk: done — %d/%d processed\n", processed, total)
	}
	fmt.Fprintf(ctx.Stdout, "reap: scan summary — %s\n", summary.String())
	return nil
}

// bulkEligibleCount reports how many issues would be attempted in bulk-clear
// mode — review-shaped, non-codex, and either not-yet-reaped-past-grace or
// already reaped (bulk mode's reaped-but-present sweep revisits those to
// catch a worktree a prior scan left behind). Used only for the upfront
// progress line; the loop in runScan applies the identical gates issue by
// issue as it goes, so this can never authorize an action the loop wouldn't.
func (c *reapKong) bulkEligibleCount(issues []bd.Issue, now time.Time) int {
	n := 0
	for _, iss := range issues {
		if _, ok := initiative.ReviewPRURL(iss); !ok {
			continue
		}
		f := initiative.Of(iss)
		if f.Runtime == "codex" {
			continue
		}
		if hasReapedNote(iss.Notes) {
			n++
			continue
		}
		closedAt, err := time.Parse(time.RFC3339, iss.ClosedAt)
		if iss.ClosedAt == "" || err != nil {
			continue
		}
		if now.Sub(closedAt) < c.Grace {
			continue
		}
		n++
	}
	return n
}

// previewSessionOutcome reports, for --dry-run, what teardownClaudeSession
// would do without stopping or removing anything: "would-stop" when a
// matching non-caller session exists, "no-session" when none matches, or
// "failed" when listing sessions itself errored.
func (c *reapKong) previewSessionOutcome(sessions []agentSession, sessErr error, match func(agentSession) bool, callerID string) string {
	if sessErr != nil {
		return "failed"
	}
	for i := range sessions {
		s := sessions[i]
		if !match(s) {
			continue
		}
		id := sessionStopID(s)
		if callerID != "" && (id == callerID || s.SessionID == callerID || s.ID == callerID) {
			continue
		}
		return "would-stop"
	}
	return "no-session"
}

// previewWorktreeOutcome reports, for --dry-run, what removeWorktreeIfClean
// would do without touching disk — the same outcome vocabulary, substituting
// "worktree-would-remove"/"worktree-would-remove-forced"/
// "worktree-would-remove-gh-verified"/"worktree-would-remove-corpse-gh-verified"
// for the mutating "worktree-removed"/"worktree-removed-forced"/
// "worktree-removed-gh-verified"/"worktree-removed-corpse-gh-verified"
// (--dry-run never attempts a removal, so it never reports
// worktree-remove-failed either). prURL is the initiative's own review PR
// URL (empty in one-off mode, which never sets c.Bulk). When prURL is
// non-empty (review-shaped — the only way runScan's dry-run branch reaches
// this, since the loop above already filters to review-shaped issues), this
// mirrors removeWorktreeIfClean's force-remove rule: status/headSHA are
// still resolved (existence still needs worktreeClean's os.Stat) but not
// otherwise consulted, and bulkGHVerifyRemovable is never called. When
// prURL is empty, the old clean-only / bulk gh-verify override path applies
// unchanged: it is only consulted when worktreeClean reports wtUnpushed or
// wtDirtyRecoverable — the same two statuses removeWorktreeIfClean itself
// overrides on that path (agent-teams-442q.11), kept as separate outcome
// strings per status (agent-teams-442q.14/Finding 2) so a dry-run preview
// distinguishes a reclaimable deletion corpse from a reclaimable
// stale-remote-ref worktree exactly like the real run does.
//
// When prURL is non-empty and c.Bulk is false, this also mirrors
// removeWorktreeIfClean's PR-merged/closed gate (agent-teams-8st0.13):
// initiativeID and prProbe feed the same hungPRStateProbe the real run
// consults, so a preview never claims a removal the real run would actually
// keep. "worktree-would-keep-pr-open"/"worktree-would-keep-pr-unknown"
// substitute for the mutating "worktree-kept-pr-open"/
// "worktree-kept-pr-unknown" (--dry-run never withholds a note either, since
// it never writes one). --bulk skips the gate here exactly as it does for
// real, since --bulk stays ungated (a human escape hatch).
func (c *reapKong) previewWorktreeOutcome(ctx *cli.Context, worktree, callerWorktree, prURL, initiativeID string, prProbe *hungPRStateProbe) string {
	if worktree == "" {
		return "worktree-unknown"
	}
	if callerWorktree != "" && worktree == callerWorktree {
		return "worktree-skipped-caller-cwd"
	}
	exists, status, headSHA, _ := c.worktreeClean(worktree)
	if !exists {
		return "worktree-absent"
	}
	if prURL != "" {
		if !c.Bulk {
			state, probed := prProbe.evaluate(hungScanEntry{ID: initiativeID, ReviewPRURL: prURL})
			if !probed {
				return "worktree-would-keep-pr-unknown"
			}
			if state != "MERGED" && state != "CLOSED" {
				return "worktree-would-keep-pr-open"
			}
		}
		if status == wtClean {
			return "worktree-would-remove"
		}
		return "worktree-would-remove-forced"
	}
	switch status {
	case wtClean:
		return "worktree-would-remove"
	case wtDirtyRecoverable:
		if c.Bulk && c.bulkGHVerifyRemovable(ctx, worktree, prURL, headSHA) {
			return "worktree-would-remove-corpse-gh-verified"
		}
		return "worktree-dirty-skipped"
	case wtUnpushed:
		if c.Bulk && c.bulkGHVerifyRemovable(ctx, worktree, prURL, headSHA) {
			return "worktree-would-remove-gh-verified"
		}
		return "worktree-dirty-skipped"
	default: // wtDirty
		return "worktree-dirty-skipped"
	}
}

// callerWorktreeCWD resolves callerID's own cwd from sessions (the live
// `claude agents` list), so scan mode and one-off form-1 can spare the
// calling session's worktree the same way teardownClaudeSession already
// spares its process (contract: "WORKTREE REMOVAL" footgun — force-removing
// the caller's own cwd worktree breaks the live session, ENOENT /bin/sh).
// Returns "" when callerID is empty or no session matches it — matched the
// same three-way id comparison teardownClaudeSession uses.
func callerWorktreeCWD(sessions []agentSession, callerID string) string {
	if callerID == "" {
		return ""
	}
	for _, s := range sessions {
		id := sessionStopID(s)
		if id == callerID || s.SessionID == callerID || s.ID == callerID {
			return s.CWD
		}
	}
	return ""
}

// matchInitiativeSession returns the predicate for finding the claude
// session tied to f: SessionID equal to one of f.Sessions (the full sessionId
// UUID) OR CWD equal to f.Worktree (contract: "RUNTIME BRANCH").
func matchInitiativeSession(f initiative.Fields) func(agentSession) bool {
	return func(s agentSession) bool {
		if f.Worktree != "" && s.CWD == f.Worktree {
			return true
		}
		for _, sid := range f.Sessions {
			if sid != "" && s.SessionID == sid {
				return true
			}
		}
		return false
	}
}

// ── ONE-OFF mode ─────────────────────────────────────────────────────────────

// runOneOff resolves target per the contract's ordered rule — (1) initiative
// id, (2) bare claude short session id, (3) bare codex thread id — and tears
// it down NOW, bypassing every scan-mode gate. Returns non-nil when target
// cannot be resolved or its teardown fails: a human ran this expecting a
// result, so failure must be visible.
func (c *reapKong) runOneOff(ctx *cli.Context, target string) error {
	now := c.now()
	callerID := os.Getenv("CLAUDE_SESSION_ID")

	// Form 1: initiative id, resolved in the global bd registry.
	if iss, err := bd.ShowIssue(ctx.BD, target); err == nil {
		f := initiative.Of(iss)

		if f.Runtime == "codex" {
			// Codex session teardown is Ring 1 and not implemented here, but
			// the worktree IS deterministically known from the initiative —
			// contract: "the ONLY form that gives codex a removable
			// worktree" / "To reap a codex worktree one-off, pass the
			// INITIATIVE ID". Remove it if clean; leave the session alone.
			wtOutcome := c.removeWorktreeIfClean(ctx, f.Worktree, "", "", "", nil)
			c.journal(ctx, now, iss.ID, target, "codex", "one-off", "skip-codex-ring1", wtOutcome)
			return fmt.Errorf("ateam reap: %s is a codex initiative; codex session teardown is not implemented (Ring 1) — worktree outcome: %s", target, wtOutcome)
		}

		sessions, sessErr := c.agentsFunc()
		action := c.teardownClaudeSession(ctx, sessions, sessErr, matchInitiativeSession(f), callerID)
		wtOutcome := c.removeWorktreeIfClean(ctx, f.Worktree, callerWorktreeCWD(sessions, callerID), "", "", nil)
		c.journal(ctx, now, iss.ID, target, "claude", "one-off", action, wtOutcome)

		// Written only on FULL teardown success — the session action didn't
		// fail AND the worktree is actually gone (worktreeTornDown) — the
		// same invariant runScan's scan-mode note-write gate uses
		// (agent-teams-6hgr.1, bug 2a). Gating on action alone used to write
		// the note even when the worktree removal was skipped (dirty,
		// non-review target) or failed, misrepresenting teardown as complete
		// for a one-off `ateam reap <id>` a human ran expecting a result
		// (review finding on agent-teams-6hgr.1's own note-write fix).
		if action != "failed" && worktreeTornDown(wtOutcome) {
			if err := c.noteFunc(ctx, iss.ID, now); err != nil {
				fmt.Fprintf(ctx.Stderr, "reap: %s: write reaped note: %v\n", iss.ID, err)
			}
		}
		if action == "failed" {
			return fmt.Errorf("ateam reap: teardown failed for %s", target)
		}
		return nil
	}

	// Form 2: bare claude short session id.
	sessions, sessErr := c.agentsFunc()
	if sessErr != nil {
		return fmt.Errorf("ateam reap: list claude sessions: %w", sessErr)
	}
	var matched *agentSession
	for i := range sessions {
		if sessions[i].ID == target || sessions[i].SessionID == target {
			matched = &sessions[i]
			break
		}
	}
	if matched != nil {
		id := sessionStopID(*matched)
		if callerID != "" && (id == callerID || matched.SessionID == callerID || matched.ID == callerID) {
			return fmt.Errorf("ateam reap: refusing to reap the calling session")
		}
		action := c.stopAndRm(ctx, id)
		// No caller-cwd guard needed here: the explicit refusal above already
		// rejects this whole form when matched is the calling session.
		wtOutcome := c.removeWorktreeIfClean(ctx, matched.CWD, "", "", "", nil)
		// No bead was resolved for a bare session id, so no reaped note.
		c.journal(ctx, now, "", target, "claude", "one-off", action, wtOutcome)
		if action == "failed" {
			return fmt.Errorf("ateam reap: teardown failed for %s", target)
		}
		return nil
	}

	// Form 3: bare codex thread id (assumed — not an initiative, not a
	// claude session). Codex teardown is Ring 1 and not implemented here;
	// still resolve+journal per the contract so the attempt is visible.
	c.journal(ctx, now, "", target, "codex", "one-off", "failed", "worktree-unknown")
	return fmt.Errorf("ateam reap: %s did not resolve to an initiative or a claude session; codex thread teardown is not implemented (Ring 1)", target)
}

// ── shared teardown ──────────────────────────────────────────────────────────

// teardownClaudeSession finds the first session in sessions matching match
// (excluding the calling session, identified by callerID) and stops+removes
// it. Returns "no-session" if none match, "failed" if listing sessions or the
// stop/rm calls themselves errored, else "reaped".
func (c *reapKong) teardownClaudeSession(ctx *cli.Context, sessions []agentSession, sessErr error, match func(agentSession) bool, callerID string) string {
	if sessErr != nil {
		fmt.Fprintf(ctx.Stderr, "reap: list claude sessions: %v\n", sessErr)
		return "failed"
	}

	var matched *agentSession
	for i := range sessions {
		s := sessions[i]
		if !match(s) {
			continue
		}
		id := sessionStopID(s)
		// Belt-and-suspenders: never tear down the calling session.
		if callerID != "" && (id == callerID || s.SessionID == callerID || s.ID == callerID) {
			continue
		}
		matched = &sessions[i]
		break
	}
	if matched == nil {
		return "no-session"
	}
	return c.stopAndRm(ctx, sessionStopID(*matched))
}

// stopAndRm runs `claude stop <id>` then `claude rm <id>`, returning "failed"
// if either errors, else "reaped".
func (c *reapKong) stopAndRm(ctx *cli.Context, id string) string {
	if err := c.stopSession(id); err != nil {
		fmt.Fprintf(ctx.Stderr, "reap: stop %s: %v\n", id, err)
		return "failed"
	}
	if err := c.rmSession(id); err != nil {
		fmt.Fprintf(ctx.Stderr, "reap: rm %s: %v\n", id, err)
		return "failed"
	}
	return "reaped"
}

// removeWorktreeIfClean removes worktree from disk — force-removed when
// worktree is review-shaped (prURL non-empty), otherwise per the contract's
// original clean-only safety rule. callerWorktree is the calling session's
// own cwd (from callerWorktreeCWD; "" when unknown) — when worktree matches
// it, removal is skipped unconditionally, since force-removing the running
// caller's own cwd worktree breaks the live session; this guard is checked
// BEFORE the review-shaped force-remove rule below, so it is never
// overridden by it.
//
// prURL is the initiative's own review PR URL — non-empty ONLY when this is
// called from runScan (the scan loop already filters to review-shaped
// issues via initiative.ReviewPRURL before it ever reaches here); one-off
// mode always passes "" regardless of whether its resolved target happens
// to be review-shaped, so one-off's behavior for a non-review target is
// unchanged (agent-teams-6hgr.1 scopes the force-remove rule to the
// review-shaped SCAN path only). When prURL is non-empty, the whole decision
// delegates to forceRemoveWorktree, which — once the PR-merged/closed gate
// below clears — removes UNCONDITIONALLY regardless of dirty, unpushed, or
// inconclusive git state — because a review worktree checks out the PR
// author's branch and never carries changes meant to be pushed from here, so
// the clean-only / gh-verify gating that protects a normal work worktree does
// not apply (contract agent-teams-sbh8.1, superseded 2026-09-23 — see its
// SUPERSEDED-BY note). worktreeClean is never called on this path at all —
// only worktreeExists's bare directory stat — since dirty/unpushed status is
// irrelevant to a decision that ignores it either way, and worktreeClean's
// ~15s of sequential git subprocesses was pure waste there, eating into the
// scan tick's timeout budget (review finding on agent-teams-6hgr.1's own
// force-remove change — see forceRemoveWorktree's doc comment for the
// before/after arithmetic). bulkGHVerifyRemovable is likewise never called on
// this path.
//
// agent-teams-8st0.13: a review worktree is also kept — never force-removed
// — until its own PR reaches a terminal state. Unless c.Bulk (a human escape
// hatch that stays ungated, exactly like one-off mode), forceRemoveWorktree
// probes prURL's PR state through prProbe (initiativeID identifies the
// initiative for its log line only) before removing anything: an OPEN PR
// keeps the worktree ("worktree-kept-pr-open"), an inconclusive probe
// (error, timeout, PR not found, or no prState seam wired) does too
// ("worktree-kept-pr-unknown" — proof of MERGED/CLOSED is required, mirroring
// every other inconclusive-protects rule in this file), and only MERGED or
// CLOSED clears the gate. Neither kept outcome is worktreeTornDown, so
// runScan's note-write gate withholds the reaped note and this initiative is
// retried next tick — the same retry-until-done mechanism a
// worktree-remove-failed outcome already relies on. This is why a
// route.go comment_reply for a still-open PR can keep finding this worktree
// (or, once it eventually IS torn down, agent-teams-8st0.14 spawns a fresh
// comment-reply session instead).
//
// When prURL is empty (one-off mode, any target), the ORIGINAL clean-only
// rule still applies unchanged: removal proceeds when status==wtClean, or
// in --bulk mode when worktreeClean reports wtUnpushed or wtDirtyRecoverable
// AND gh-verify confirms HEAD is still on GitHub (real, non-deletion
// uncommitted/untracked changes always win — see wtDirty's doc comment). A
// dirty worktree is reclaimed in --bulk ONLY when its dirt is exclusively
// unstaged tracked-file deletions (wtDirtyRecoverable — every "missing" file
// is still in HEAD) AND gh-verify confirms HEAD is on GitHub; every other
// dirty state (any non-deletion change, untracked files, or an inconclusive
// check) is still always skipped. In production this bulk gh-verify branch
// is now unreachable — --bulk is scan-mode-only and scan-mode's prURL is
// never empty for anything reaching this function — kept only until
// agent-teams-6hgr.3 removes the now-dead machinery.
//
// The actual removal runs under a mode-aware timeout —
// reapWorktreeRemoveTimeout steady-state, the far more generous
// reapBulkWorktreeRemoveTimeout under --bulk (see their doc comments;
// agent-teams-442q.13). Returns the journal outcome string:
// "worktree-unknown" (no worktree path resolved at all),
// "worktree-skipped-caller-cwd" (worktree is the calling session's own cwd),
// "worktree-absent" (nothing on disk to remove — a clean no-op),
// "worktree-dirty-skipped" (uncommitted/deleted changes not overridden by a
// bulk gh-verify, or the clean-check itself failed — never force-delete on
// an inconclusive check; unreachable for a review-shaped worktree, which is
// always force-removed instead), "worktree-remove-failed" (removal was
// attempted — clean, forced, or gh-verified — but the removeWorktree call
// itself errored or timed out; kept distinct from worktree-dirty-skipped
// since this worktree was NEVER dirty-and-skipped, only its removal failed,
// and it may now need manual attention — agent-teams-442q.13/Finding 2),
// "worktree-removed" (status was wtClean, forced or not — no need to
// distinguish), "worktree-removed-forced" (review-shaped and force-removed
// despite a non-clean or unchecked git state — kept distinct so the
// journal/summary still show when a force-removal papered over real
// dirty/unpushed state), or (the now-dead bulk gh-verify override, one-off
// mode only) "worktree-removed-gh-verified" for a reclaimed
// stale-remote-ref (wtUnpushed) worktree, or
// "worktree-removed-corpse-gh-verified" for a reclaimed deletion-corpse
// (wtDirtyRecoverable) worktree — kept distinct (agent-teams-442q.14/Finding
// 2) since the journal is the only durable record of which force-removal
// path fired — or (review-shaped, non-bulk, PR not yet terminal)
// "worktree-kept-pr-open" or "worktree-kept-pr-unknown".
//
// initiativeID and prProbe feed forceRemoveWorktree's PR-state gate; both are
// ignored whenever prURL is empty (one-off mode never needs them).
func (c *reapKong) removeWorktreeIfClean(ctx *cli.Context, worktree, callerWorktree, prURL, initiativeID string, prProbe *hungPRStateProbe) string {
	if worktree == "" {
		return "worktree-unknown"
	}
	if callerWorktree != "" && worktree == callerWorktree {
		fmt.Fprintf(ctx.Stdout, "reap: worktree %s is the calling session's own cwd, skipping removal\n", worktree)
		return "worktree-skipped-caller-cwd"
	}

	if prURL != "" {
		return c.forceRemoveWorktree(ctx, worktree, prURL, initiativeID, prProbe)
	}

	exists, status, headSHA, err := c.worktreeClean(worktree)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "reap: check worktree %s: %v\n", worktree, err)
	}
	if !exists {
		return "worktree-absent"
	}

	// ghVerifiedOutcome is "" when no gh-verify override fired, or the
	// specific "worktree-removed(-corpse)?-gh-verified" string that
	// override earned.
	removable := status == wtClean
	ghVerifiedOutcome := ""
	if (status == wtUnpushed || status == wtDirtyRecoverable) && c.Bulk && c.bulkGHVerifyRemovable(ctx, worktree, prURL, headSHA) {
		removable = true
		if status == wtDirtyRecoverable {
			ghVerifiedOutcome = "worktree-removed-corpse-gh-verified"
		} else {
			ghVerifiedOutcome = "worktree-removed-gh-verified"
		}
	}
	if !removable {
		fmt.Fprintf(ctx.Stdout, "reap: worktree %s is not provably clean, skipping removal\n", worktree)
		return "worktree-dirty-skipped"
	}

	timeout := reapWorktreeRemoveTimeout
	if c.Bulk {
		timeout = reapBulkWorktreeRemoveTimeout
	}
	if err := c.removeWorktree(worktree, timeout); err != nil {
		fmt.Fprintf(ctx.Stderr, "reap: remove worktree %s: %v\n", worktree, err)
		return "worktree-remove-failed"
	}
	if ghVerifiedOutcome != "" {
		return ghVerifiedOutcome
	}
	return "worktree-removed"
}

// forceRemoveWorktree implements removeWorktreeIfClean's review-shaped
// (prURL != "") branch: existence only, via worktreeExists's bare directory
// stat — never worktreeClean's git status/rev-list probe, since a review
// worktree's dirty/unpushed state is irrelevant to whether force-removal is
// safe (removeWorktreeIfClean's doc comment has the reasoning). Skipping that
// probe here is the fix for a review finding on agent-teams-6hgr.1's own
// force-remove change: worktreeClean's three sequential git subprocesses,
// each bounded by reapGitTimeout (5s), cost up to 15s on a path whose outcome
// never consulted them — pure waste that pushed a worst-case single-survivor
// scan tick to within 5s of pr-shepherd's 90s hard SIGTERM budget (see
// reapWorktreeRemoveTimeout's doc comment for the corrected arithmetic).
// Timeout selection mirrors removeWorktreeIfClean's own clean-only path:
// reapWorktreeRemoveTimeout steady-state, reapBulkWorktreeRemoveTimeout under
// --bulk — --bulk is scan-mode-only and scan-mode's prURL is never empty, so
// in production every --bulk removal now takes this path and needs the same
// generous bound the old gh-verified-safe removals did (agent-teams-442q.13).
//
// agent-teams-8st0.13: existence is checked FIRST, before the PR-state gate
// below, so an already-absent worktree is reported (and, via
// worktreeTornDown, notable) regardless of PR state — there is nothing left
// to protect. Unless c.Bulk, the gate then probes prURL's state through
// prProbe (initiativeID, in the hungScanEntry it builds, is used only for
// that probe's own log line): an OPEN state keeps the worktree
// ("worktree-kept-pr-open"), an inconclusive probe (error, timeout, PR not
// found, or c.prState left nil) also keeps it ("worktree-kept-pr-unknown" —
// proof of MERGED/CLOSED is required, never merely absence of proof of
// OPEN), and only MERGED or CLOSED proceeds to the actual removal below.
// --bulk skips this gate entirely — a human-invoked drain stays the ungated
// escape hatch it always was, matching one-off mode's own prURL="" bypass.
func (c *reapKong) forceRemoveWorktree(ctx *cli.Context, worktree, prURL, initiativeID string, prProbe *hungPRStateProbe) string {
	if !c.worktreeExists(worktree) {
		return "worktree-absent"
	}
	if !c.Bulk {
		state, probed := prProbe.evaluate(hungScanEntry{ID: initiativeID, ReviewPRURL: prURL})
		if !probed {
			fmt.Fprintf(ctx.Stdout, "reap: worktree %s: PR state unknown, keeping until it can be confirmed merged/closed\n", worktree)
			return "worktree-kept-pr-unknown"
		}
		if state != "MERGED" && state != "CLOSED" {
			fmt.Fprintf(ctx.Stdout, "reap: worktree %s: PR still %s, keeping until merged or closed\n", worktree, state)
			return "worktree-kept-pr-open"
		}
	}

	timeout := reapWorktreeRemoveTimeout
	if c.Bulk {
		timeout = reapBulkWorktreeRemoveTimeout
	}
	if err := c.removeWorktree(worktree, timeout); err != nil {
		fmt.Fprintf(ctx.Stderr, "reap: remove worktree %s: %v\n", worktree, err)
		return "worktree-remove-failed"
	}
	return "worktree-removed-forced"
}

// worktreeTornDown reports whether wtOutcome (a removeWorktreeIfClean
// journal outcome string) represents the worktree being fully gone from
// disk after this reap attempt — removed just now, in any variant, or
// already absent before this tick. This is runScan's note-write gate
// (agent-teams-6hgr.1, bug 2a): the reaped: note is written only when the
// worktree outcome is one of these, never on worktree-remove-failed,
// worktree-dirty-skipped, worktree-unknown, worktree-skipped-caller-cwd,
// worktree-kept-pr-open, or worktree-kept-pr-unknown — each of those leaves
// real residual state that must be retried, not silently marked done. The
// last two (agent-teams-8st0.13) are the review-worktree PR-merged/closed
// gate keeping this initiative unreaped on purpose, for as long as its PR
// stays open or unconfirmed.
func worktreeTornDown(wtOutcome string) bool {
	switch wtOutcome {
	case "worktree-removed", "worktree-removed-forced", "worktree-removed-gh-verified", "worktree-removed-corpse-gh-verified", "worktree-absent":
		return true
	default:
		return false
	}
}

// bulkGHVerifyRemovable is the bulk-mode-only override for a worktree whose
// HEAD carries commits absent from every LOCAL remote-tracking ref
// (worktreeClean reported wtUnpushed): this is a false positive when the
// worktree's PR branch was deleted on GitHub after merge, pruning the local
// tracking ref, even though GitHub's own history still has the commit.
// Returns false — never remove — whenever proof is missing: no PR URL, no
// resolvable HEAD sha, an unparsable PR URL, or the gh call itself erroring
// or reporting the commit absent. Inconclusive always protects.
func (c *reapKong) bulkGHVerifyRemovable(ctx *cli.Context, worktree, prURL, headSHA string) bool {
	if prURL == "" || headSHA == "" {
		return false
	}
	ownerRepo, _, ok := parsePrURL(prURL)
	if !ok {
		return false
	}
	present, err := c.ghCommitPresent(ownerRepo, headSHA)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "reap: gh-verify worktree %s (%s@%s): %v\n", worktree, ownerRepo, headSHA, err)
		return false
	}
	if !present {
		fmt.Fprintf(ctx.Stdout, "reap: gh-verify worktree %s: %s@%s not found on GitHub, skipping removal\n", worktree, ownerRepo, headSHA)
	}
	return present
}

// ── real implementations ─────────────────────────────────────────────────────

// defaultRmSession runs `claude rm <id>`, bounded via runBoundedClaude.
// reap_orphans.go only wraps `claude stop`; reap additionally needs `claude
// rm` to actually clear the session from the agents view.
func defaultRmSession(id string) error {
	_, err := runBoundedClaude(context.Background(), claudeCallTimeout, "rm", id)
	return err
}

// reapWorktreeRemoveTimeout bounds reapRemoveWorktreeWithTimeout's
// git-common-dir resolve AND the actual `git worktree remove` as ONE shared,
// combined deadline — not one timeout per call — in STEADY-STATE (non-bulk)
// mode only; --bulk uses the far more generous reapBulkWorktreeRemoveTimeout
// instead (removeWorktreeIfClean selects between them on c.Bulk). A single
// context.WithTimeout(_, reapWorktreeRemoveTimeout) is threaded through both
// git.CommonDir and git.RemoveWorktree via boundedGitRunner
// (bounded_exec.go), so together they get this budget once, not twice. On
// expiry the whole process group is killed (same pattern as
// runBoundedClaude) and the call returns an error rather than falsely
// reporting the worktree removed — the caller's best-effort `git worktree
// prune` cleans up any partial removal that reached it, though a removal
// that times out INSIDE git.RemoveWorktree itself never reaches that prune
// at all (reapBulkWorktreeRemoveTimeout's doc comment has the consequence);
// either way the survivor is simply retried on the next tick instead of
// silently marked done.
//
// Sized to satisfy two constraints at once. First, it must never interrupt a
// legitimate large-worktree removal: observed ~15s for a large worktree
// (contract agent-teams-442q.3 live-verify), so 45s leaves 3x margin.
// Second, it must still keep a worst-case scan tick under pr-shepherd's 90s
// hard SIGTERM budget (pr-shepherd PR #5), even when the survivor that trips
// this timeout starts right at the edge of the 15s soft ScanDeadline. Summing
// every sequential bounded call on that one survivor's teardown path: every
// scan-mode survivor is review-shaped (the loop filters to
// initiative.ReviewPRURL before it ever reaches teardown), so it always
// takes forceRemoveWorktree's existence-only path — a bare os.Stat, not a
// bounded subprocess — never worktreeClean's git status/rev-list probe:
//
//	15s  ScanDeadline slop (survivor starts just under the soft deadline)
//	 5s  stop                                          (claudeCallTimeout)
//	 5s  rm                                             (claudeCallTimeout)
//	45s  this timeout                          (reapWorktreeRemoveTimeout)
//	 5s  `git worktree prune`, best-effort                (reapGitTimeout)
//	== 75s total, a 15s margin under the 90s hard budget.
const reapWorktreeRemoveTimeout = 45 * time.Second

// reapBulkWorktreeRemoveTimeout bounds `git worktree remove` in --bulk mode
// only (removeWorktreeIfClean selects this over reapWorktreeRemoveTimeout
// when c.Bulk is true). --bulk already forces ScanDeadline=0 and Max=0 — an
// unbudgeted, human-invoked drain with no per-tick deadline to protect — so
// unlike the steady-state bound above, this one only needs to be generous,
// not tight. Sized from live evidence (agent-teams-442q.13): a production
// --bulk run against 942 worktrees, applying reapWorktreeRemoveTimeout's own
// 45s bound to every removal, timed out 31 of 72 gh-verified-safe removals
// mid-delete under real disk/CPU load on a shared machine — and because the
// best-effort `git worktree prune` in reapRemoveWorktreeWithTimeout only runs
// AFTER git.RemoveWorktree returns nil, a removal that times out inside
// RemoveWorktree itself never reaches that prune, so 7 of the 31 were left as
// a permanent, git-invisible corpse no future reap can ever reach again. Ten
// minutes leaves generous margin for the largest observed worktrees (~15s,
// contract agent-teams-442q.3) while still bounding a genuine hang; a --bulk
// run that outlives this on every survivor is a different problem this
// timeout cannot solve.
const reapBulkWorktreeRemoveTimeout = 10 * time.Minute

// reapRemoveWorktreeWithTimeout removes worktree, resolving its owning repo
// root via git's common-dir (the shared .git directory every linked
// worktree points back to) so the caller never needs to separately track or
// pass the main repo root. `claude rm` does NOT remove an ateam worktree —
// proven live (contract, "WORKTREE REMOVAL") — so this explicit step is
// mandatory for actually reclaiming the disk space and `git worktree list`
// entry. This is the production reapRemoveWorktreeFunc, wired directly in
// RegisterReapKong; taking timeout as a parameter lets removeWorktreeIfClean
// choose a mode-aware bound (reapWorktreeRemoveTimeout steady-state,
// reapBulkWorktreeRemoveTimeout for --bulk) and lets a test exercise the
// real timeout/process-group-kill behavior on a short fuse without waiting
// out either constant for real — the same shape runBoundedClaude's own tests
// use, calling it directly with a short custom timeout.
func reapRemoveWorktreeWithTimeout(worktree string, timeout time.Duration) error {
	// ONE shared deadline across both the git-common-dir resolve and the
	// actual `git worktree remove` — see reapWorktreeRemoveTimeout's doc
	// comment for why a combined budget, not one timeout per call, is what
	// the arithmetic above assumes. boundedGitRunner also kills the whole
	// process group on expiry, closing review finding F1 (agent-teams-
	// 442q.9): this path previously ran through gitutil's plain
	// exec.Command with no context or timeout at all.
	cctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	git := boundedGitRunner(cctx)

	commonDir, err := git.CommonDir(worktree)
	if err != nil {
		return fmt.Errorf("resolve repo root for worktree %s: %w", worktree, err)
	}
	repoRoot := filepath.Dir(commonDir)
	if err := git.RemoveWorktree(repoRoot, worktree); err != nil {
		return err
	}
	// Best-effort prune of any stale worktree administrative entries left
	// behind; the worktree itself is already gone at this point, so a prune
	// failure here never fails the reap. Its own timeout (reapGitTimeout) is
	// independent of the combined removal budget above.
	pruneCtx, pruneCancel := context.WithTimeout(context.Background(), reapGitTimeout)
	defer pruneCancel()
	_ = exec.CommandContext(pruneCtx, "git", "-C", repoRoot, "worktree", "prune").Run()
	return nil
}

// defaultWorktreeExists is the production worktreeExistsFunc: a bare
// directory stat, nothing else — the same guard defaultWorktreeClean's own
// os.Stat applies, standing alone with no git subprocess attached.
func defaultWorktreeExists(worktree string) bool {
	info, err := os.Stat(worktree)
	return err == nil && info.IsDir()
}

// defaultWorktreeClean reports whether worktree has no uncommitted changes
// (`git status --porcelain` empty) and no commits reachable from HEAD that
// are absent from every remote-tracking ref (`git rev-list --count HEAD
// --not --remotes` == "0"). This is upstream-agnostic: it catches any
// commit missing from ALL remotes, not just a single configured upstream,
// so it also works on branches with no upstream configured at all (for
// example local-only review-pr-<N> branches). A missing directory reports
// exists=false.
//
// Non-empty porcelain is not automatically wtDirty: when EVERY line is a
// pure, UNSTAGED tracked-file deletion (allTrackedDeletions — " D" only, no
// staged "D ", untracked, add, modify, rename, or unmerged entry), this is
// the signature a `git worktree remove` leaves when killed mid-removal — a
// recursive unlink that never touches the index, so the working-tree files
// are gone from disk but HEAD and the index still have them intact. That
// case resolves HEAD via `git rev-parse HEAD` and reports
// wtDirtyRecoverable, falling back to wtDirty if the sha resolve itself
// fails (proof required, same as any other inconclusive check). A staged
// deletion ("D "), or any other non-empty porcelain, reports wtDirty
// directly, no sha resolve attempted — real, uncommitted intent, not a
// killed-removal corpse.
//
// Any other inconclusive result — the status/rev-list subprocess itself
// failing — also reports status=wtDirty: the contract requires PROOF of no
// unpushed/uncommitted work before a force-remove, not merely absence of
// proof of some. When the rev-list count is non-zero, status is wtUnpushed
// and headSHA is resolved via `git rev-parse HEAD` for a caller's own
// gh-verify override (bulkGHVerifyRemovable) — the one other case (alongside
// wtDirtyRecoverable) that needs it.
func defaultWorktreeClean(worktree string) (exists bool, status worktreeGitStatus, headSHA string, err error) {
	info, statErr := os.Stat(worktree)
	if statErr != nil || !info.IsDir() {
		return false, wtDirty, "", nil
	}

	statusCtx, statusCancel := context.WithTimeout(context.Background(), reapGitTimeout)
	defer statusCancel()
	statusOut, err := exec.CommandContext(statusCtx, "git", "-C", worktree, "status", "--porcelain").Output()
	if err != nil {
		return true, wtDirty, "", fmt.Errorf("git status --porcelain: %w", err)
	}
	if strings.TrimSpace(string(statusOut)) != "" {
		if allTrackedDeletions(string(statusOut)) {
			shaCtx, shaCancel := context.WithTimeout(context.Background(), reapGitTimeout)
			defer shaCancel()
			shaOut, shaErr := exec.CommandContext(shaCtx, "git", "-C", worktree, "rev-parse", "HEAD").Output()
			if shaErr == nil {
				return true, wtDirtyRecoverable, strings.TrimSpace(string(shaOut)), nil
			}
			// Can't resolve the sha a gh-verify override would need — no
			// proof possible, degrade to wtDirty like any other
			// inconclusive check.
		}
		return true, wtDirty, "", nil
	}

	localOnlyCtx, localOnlyCancel := context.WithTimeout(context.Background(), reapGitTimeout)
	defer localOnlyCancel()
	localOnlyOut, err := exec.CommandContext(localOnlyCtx, "git", "-C", worktree, "rev-list", "--count", "HEAD", "--not", "--remotes").Output()
	if err != nil {
		// Cannot prove every commit exists on some remote.
		return true, wtDirty, "", nil
	}
	if strings.TrimSpace(string(localOnlyOut)) == "0" {
		return true, wtClean, "", nil
	}

	shaCtx, shaCancel := context.WithTimeout(context.Background(), reapGitTimeout)
	defer shaCancel()
	shaOut, shaErr := exec.CommandContext(shaCtx, "git", "-C", worktree, "rev-parse", "HEAD").Output()
	if shaErr != nil {
		// Can't resolve the sha a gh-verify override would need — no proof
		// possible, degrade to the same "never force-delete" bucket as any
		// other inconclusive check.
		return true, wtDirty, "", nil
	}
	return true, wtUnpushed, strings.TrimSpace(string(shaOut)), nil
}

// allTrackedDeletions reports whether every non-empty line of porcelain (raw
// `git status --porcelain` output) is a pure, UNSTAGED tracked-file deletion:
// status code " D" only, and nothing else. A single line with any other code
// — "??" untracked, "D " staged deletion, "A " staged add, "M "/" M"
// modified, "R "/"C " rename/copy, or an unmerged conflict marker like
// "UU"/"AA"/"DD" — makes the whole worktree ineligible and returns false.
// "D " is deliberately excluded, not merely another rejected code
// (agent-teams-442q.14/Finding 1): a killed `git worktree remove` is a
// recursive unlink of working-tree files that never touches the index, so it
// can only ever leave unstaged deletions; a staged deletion instead means an
// agent ran `git rm` and never committed — real, uncommitted intent this
// predicate must not paper over. An empty or all-blank porcelain also
// returns false (defaultWorktreeClean only calls this when porcelain is known
// non-empty, but this stays conservative on its own regardless of caller).
func allTrackedDeletions(porcelain string) bool {
	sawDeletion := false
	for _, line := range strings.Split(porcelain, "\n") {
		if line == "" {
			continue
		}
		if len(line) < 2 {
			return false
		}
		if line[:2] != " D" {
			return false
		}
		sawDeletion = true
	}
	return sawDeletion
}

// reapGHVerifyTimeout bounds the bulk-mode gh commit-presence probe
// (defaultGHCommitPresent) so a hanging gh can't stall a bulk-clear run —
// mirrors reapGitTimeout's reasoning for reap's git subprocesses, sized like
// the existing gh probes elsewhere in this package (hungReviewCommentProbeTimeout).
const reapGHVerifyTimeout = 10 * time.Second

// defaultGHCommitPresent runs `gh api repos/<owner>/<repo>/commits/<sha>`,
// bounded by reapGHVerifyTimeout, and reports whether GitHub has the commit:
// present=true only on a zero exit (gh reports 404 as a non-zero exit for a
// missing commit, which this folds into present=false with the error
// attached for the caller to log — the bulk gh-verify override treats a
// missing commit and a gh error identically: inconclusive protects).
func defaultGHCommitPresent(ownerRepo, sha string) (present bool, err error) {
	cctx, cancel := context.WithTimeout(context.Background(), reapGHVerifyTimeout)
	defer cancel()
	if err := exec.CommandContext(cctx, "gh", "api", fmt.Sprintf("repos/%s/commits/%s", ownerRepo, sha)).Run(); err != nil {
		return false, fmt.Errorf("gh api commits/%s: %w", sha, err)
	}
	return true, nil
}

// defaultReapNote writes the durable "reaped: <RFC3339>" bd note that makes
// reap at-most-once per initiative — mirrors defaultHungClose's note
// discipline (hung_tick.go), going through ctx.BD.Run directly rather than
// any higher-level `ateam` verb.
func defaultReapNote(ctx *cli.Context, id string, at time.Time) error {
	note := fmt.Sprintf("reaped: %s", at.UTC().Format(time.RFC3339))
	tmp, err := os.CreateTemp("", "ateam-reap-note-*")
	if err != nil {
		return fmt.Errorf("create reap-note temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(note + "\n"); err != nil {
		tmp.Close()
		return fmt.Errorf("write reap-note temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close reap-note temp file: %w", err)
	}
	if _, err := ctx.BD.Run("note", id, "--file="+tmpPath); err != nil {
		return fmt.Errorf("bd note: %w", err)
	}
	return nil
}

// hasReapedNote reports whether notes already carries a "reaped:" line —
// reap's at-most-once idempotency gate. Mirrors hasReviewPostedNote's
// line-prefix scan (hung_scan.go).
func hasReapedNote(notes string) bool {
	for _, line := range strings.Split(notes, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "reaped:") {
			return true
		}
	}
	return false
}

// ── journal ──────────────────────────────────────────────────────────────────

// reapJournalFileName is the append-only journal reap writes one line to per
// attempt, mirroring hung-journal.jsonl's role for the hung-scan backstop
// (hung_workproduct.go) but kept in its own file since it tracks a distinct
// concern. SCAN mode's real (non-dry-run) path skips the write for one
// specific repeat tick (agent-teams-8st0.13, follow-up): a review-shaped
// survivor whose session teardown already found nothing to do
// (action=="no-session") and whose worktree is still kept for the same
// PR-not-yet-terminal reason as before (isKeptPRStateOutcome) — a no-op
// retry earns no new line, since it has nothing new to say. Every other real
// attempt still journals, including the first tick a "kept" outcome
// appears (the session teardown itself did something that tick).
const reapJournalFileName = "reap-journal.jsonl"

// reapJournalEntry is one journal line: which target reap attempted, in
// which mode, and what it decided for the session and the worktree.
type reapJournalEntry struct {
	Timestamp       string `json:"ts"`
	InitiativeID    string `json:"id,omitempty"`
	Target          string `json:"target,omitempty"`
	Runtime         string `json:"runtime,omitempty"`
	Mode            string `json:"mode"` // "scan" | "one-off"
	SessionAction   string `json:"session_action"`
	WorktreeOutcome string `json:"worktree_outcome,omitempty"`
}

// reapJournalMaxBytes caps the journal file's size; rotateReapJournalIfNeeded
// moves the file aside to a single ".1" backup once it's exceeded, mirroring
// hungJournalMaxBytes/rotateHungJournalIfNeeded (hung_workproduct.go) — scan
// mode appends to this file every tick forever, so without a cap it grows
// unbounded. A var (not a const) so tests can shrink it rather than writing
// 5 MiB of fixtures to exercise rotation.
var reapJournalMaxBytes int64 = 5 * 1024 * 1024 // 5 MiB

// reapJournalPath returns <ctx.Home>/reap-journal.jsonl.
func reapJournalPath(home string) string {
	return filepath.Join(home, reapJournalFileName)
}

// rotateReapJournalIfNeeded moves path aside to path+".1" (best-effort,
// overwriting any previous backup) once it exceeds reapJournalMaxBytes. A
// stat/rename failure is swallowed — the journal is diagnostic, never load-
// bearing for reap's own correctness.
func rotateReapJournalIfNeeded(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < reapJournalMaxBytes {
		return
	}
	_ = os.Rename(path, path+".1")
}

// journal builds and appends one reapJournalEntry, logging (never failing
// the reap) if the write itself errors.
func (c *reapKong) journal(ctx *cli.Context, now time.Time, initiativeID, target, runtime, mode, sessionAction, worktreeOutcome string) {
	entry := reapJournalEntry{
		Timestamp:       now.UTC().Format(time.RFC3339),
		InitiativeID:    initiativeID,
		Target:          target,
		Runtime:         runtime,
		Mode:            mode,
		SessionAction:   sessionAction,
		WorktreeOutcome: worktreeOutcome,
	}
	if err := appendReapJournal(reapJournalPath(ctx.Home), entry); err != nil {
		fmt.Fprintf(ctx.Stderr, "reap: journal write failed: %v\n", err)
	}
}

// appendReapJournal appends one JSON-marshaled line to path, creating the
// parent directory and file as needed, rotating first if the file has grown
// past the cap. Best-effort: an I/O error here must never block or fail reap
// itself, so this returns an error for the caller to log, not to abort on.
func appendReapJournal(path string, entry reapJournalEntry) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	rotateReapJournalIfNeeded(path)

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}
