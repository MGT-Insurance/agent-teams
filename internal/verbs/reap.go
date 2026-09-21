// Package verbs — reap.go implements the `ateam reap [target]` verb.
//
// reap is the loop-closing teardown for a review-pr initiative: once a review
// session's PR-review work is done and its initiative closes, the Claude
// session and its git worktree still sit around (the harness never reclaims
// either on its own — see the WORKTREE REMOVAL note below). reap has two
// modes, selected by the optional positional target:
//
//   - SCAN mode (no target): what pr-shepherd calls every tick. Enumerates
//     CLOSED initiatives, applies the review-shape / grace / already-reaped
//     gates, and tears down each survivor (session + clean-only worktree
//     removal). Gating trusts the initiative's own closed state — reap never
//     probes session status or liveness itself (contract: agent-teams-sbh8.15,
//     "trust the initiative state").
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
	"github.com/mgt-insurance/agent-teams/internal/gitutil"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// defaultReapGrace is SCAN mode's default minimum time since bd close before
// an initiative becomes reap-eligible (contract: "GRACE CLOCK"). A target
// given in ONE-OFF mode ignores this entirely.
const defaultReapGrace = 20 * time.Minute

// reapGitTimeout bounds every git subprocess reap itself runs (worktree
// clean-check, removal, prune) so a wedged git can't stall a scan tick —
// mirrors gitProbeTimeout's reasoning (hung_workproduct.go). Reduced from the
// original 10s so soft-deadline + worst-case single-survivor teardown (stop +
// rm + 2 git ops) stays comfortably under pr-shepherd's 30s hard budget.
const reapGitTimeout = 5 * time.Second

// RegisterReapKong registers the reap verb onto p.
func RegisterReapKong(p *cli.Parser) {
	p.AddVerb("reap", "Tear down closed review sessions past grace (scan mode, what pr-shepherd calls every tick) or one target right now (one-off mode); see reap-orphans for stop-only cwd-missing cleanup.", &reapKong{
		agentsFunc:     defaultAgentsJSONAll,
		now:            time.Now,
		stopSession:    defaultStopSession,
		rmSession:      defaultRmSession,
		removeWorktree: defaultReapRemoveWorktree,
		worktreeClean:  defaultWorktreeClean,
		noteFunc:       defaultReapNote,
		notifyCtx:      defaultReapNotifyCtx,
	})
}

// rmSessionFunc removes a claude session by short id. Injected so tests
// substitute a fake without executing real claude commands.
type rmSessionFunc func(id string) error

// reapRemoveWorktreeFunc removes worktree from disk, resolving its owning
// repo root itself so callers never need to know or pass it. Injected so
// tests substitute a fake without a real git subprocess.
type reapRemoveWorktreeFunc func(worktree string) error

// worktreeCleanFunc reports whether worktree is safe to remove: exists is
// false when the path is not present on disk at all (a missing worktree is a
// clean no-op, never dirty); clean is only meaningful when exists is true and
// means no uncommitted changes AND the branch is not ahead of its upstream.
// Injected so tests substitute a fake without a real git subprocess.
type worktreeCleanFunc func(worktree string) (exists bool, clean bool, err error)

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
	Bulk         bool          `name:"bulk" help:"Scan mode only: one-time human-invoked unbudgeted drain. Forces --scan-deadline=0 and --max=0, also re-sweeps already-reaped initiatives whose worktree is still present on disk (clean-only, never forced), and prints running progress to stderr. Pair with --dry-run to preview first."`
	DryRun       bool          `name:"dry-run" help:"Scan mode only: report what would be torn down without stopping any session, removing any worktree, or writing any reaped note."`

	agentsFunc     agentsJSONFunc         `kong:"-"`
	now            func() time.Time       `kong:"-"`
	stopSession    stopSessionFunc        `kong:"-"`
	rmSession      rmSessionFunc          `kong:"-"`
	removeWorktree reapRemoveWorktreeFunc `kong:"-"`
	worktreeClean  worktreeCleanFunc      `kong:"-"`
	noteFunc       reapNoteFunc           `kong:"-"`
	notifyCtx      reapNotifyCtxFunc      `kong:"-"`
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
// loop stops STARTING new work and returns — the survivor already in flight
// finishes (each of its own subprocess calls is separately bounded by
// runBoundedClaude), but no further one starts. This is what lets one tick
// exit cleanly under pr-shepherd's 30s hard budget no matter how large the
// backlog is, leaving the rest for the next tick.
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

	for _, iss := range issues {
		_, ok := initiative.ReviewPRURL(iss)
		if !ok {
			continue // not review-shaped: untouched, never reap's concern
		}
		f := initiative.Of(iss)
		if f.Runtime == "codex" {
			c.journal(ctx, now, iss.ID, "", f.Runtime, "scan", "skip-codex-ring1", "")
			continue // Ring 1: codex teardown is not implemented by this verb
		}
		// In bulk mode ONLY, an already-reaped initiative is not skipped
		// outright: a prior scan may have torn down its session but left a
		// clean worktree on disk (the plain gate below never re-visits a
		// noted initiative). alreadyReaped gates the note-write below so
		// it stays a write-once note even though this sweep can revisit the
		// same initiative on every bulk run until its worktree is finally
		// gone.
		alreadyReaped := hasReapedNote(iss.Notes)
		if alreadyReaped && !c.Bulk {
			continue // already reaped: at-most-once, no repeat gh probe
		}

		closedAt, err := time.Parse(time.RFC3339, iss.ClosedAt)
		if iss.ClosedAt == "" || err != nil {
			// Distinct from "skip-grace" below: this initiative is
			// permanently stuck (closed_at will never become parseable on a
			// later tick), whereas "skip-grace" self-resolves.
			fmt.Fprintf(ctx.Stderr, "reap: %s: closed_at missing or unparseable (%q), skipping\n", iss.ID, iss.ClosedAt)
			c.journal(ctx, now, iss.ID, "", f.Runtime, "scan", "skip-bad-closedat", "")
			continue
		}
		if now.Sub(closedAt) < c.Grace {
			c.journal(ctx, now, iss.ID, "", f.Runtime, "scan", "skip-grace", "")
			continue
		}

		// Soft budget check, right before the expensive part (teardown):
		// skip-only iterations above never cost meaningful wall-clock time,
		// so they don't count against it.
		select {
		case <-scanCtx.Done():
			return nil
		default:
		}
		if c.ScanDeadline > 0 && time.Since(scanStart) >= c.ScanDeadline {
			return nil
		}
		if c.Max > 0 && reaped >= c.Max {
			return nil
		}

		if c.DryRun {
			// Read-only preview: query session/worktree state through the
			// same seams but never call stop/rm/remove/note.
			sessOutcome := c.previewSessionOutcome(sessions, sessErr, matchInitiativeSession(f), callerID)
			wtOutcome := c.previewWorktreeOutcome(f.Worktree, callerWorktreeCWD(sessions, callerID))
			processed++
			if c.Bulk {
				fmt.Fprintf(ctx.Stderr, "reap --bulk --dry-run: [%d/%d] %s session=%s worktree=%s (%s)\n", processed, total, iss.ID, sessOutcome, wtOutcome, f.Worktree)
			}
			continue
		}

		action := c.teardownClaudeSession(ctx, sessions, sessErr, matchInitiativeSession(f), callerID)
		wtOutcome := c.removeWorktreeIfClean(ctx, f.Worktree, callerWorktreeCWD(sessions, callerID))
		c.journal(ctx, now, iss.ID, "", f.Runtime, "scan", action, wtOutcome)

		if action != "failed" {
			// Written after teardown (or when the session was already gone,
			// action=="no-session") — never on a genuine teardown failure,
			// so a failing stop/rm is retried on the next tick rather than
			// silently marked done. Skipped when alreadyReaped: the note is
			// write-once, so a bulk re-sweep that only finishes removing a
			// leftover worktree never appends a duplicate.
			if !alreadyReaped {
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
// "worktree-would-remove" for the mutating "worktree-removed".
func (c *reapKong) previewWorktreeOutcome(worktree, callerWorktree string) string {
	if worktree == "" {
		return "worktree-unknown"
	}
	if callerWorktree != "" && worktree == callerWorktree {
		return "worktree-skipped-caller-cwd"
	}
	exists, clean, _ := c.worktreeClean(worktree)
	if !exists {
		return "worktree-absent"
	}
	if !clean {
		return "worktree-dirty-skipped"
	}
	return "worktree-would-remove"
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
			wtOutcome := c.removeWorktreeIfClean(ctx, f.Worktree, "")
			c.journal(ctx, now, iss.ID, target, "codex", "one-off", "skip-codex-ring1", wtOutcome)
			return fmt.Errorf("ateam reap: %s is a codex initiative; codex session teardown is not implemented (Ring 1) — worktree outcome: %s", target, wtOutcome)
		}

		sessions, sessErr := c.agentsFunc()
		action := c.teardownClaudeSession(ctx, sessions, sessErr, matchInitiativeSession(f), callerID)
		wtOutcome := c.removeWorktreeIfClean(ctx, f.Worktree, callerWorktreeCWD(sessions, callerID))
		c.journal(ctx, now, iss.ID, target, "claude", "one-off", action, wtOutcome)

		if action != "failed" {
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
		wtOutcome := c.removeWorktreeIfClean(ctx, matched.CWD, "")
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

// removeWorktreeIfClean removes worktree from disk when it is clean, per the
// contract's clean-only safety rule. callerWorktree is the calling session's
// own cwd (from callerWorktreeCWD; "" when unknown) — when worktree matches
// it, removal is skipped unconditionally, since force-removing the running
// caller's own cwd worktree breaks the live session. Returns the journal
// outcome string: "worktree-unknown" (no worktree path resolved at all),
// "worktree-skipped-caller-cwd" (worktree is the calling session's own cwd),
// "worktree-absent" (nothing on disk to remove — a clean no-op),
// "worktree-dirty-skipped" (uncommitted or unpushed work, or the clean-check
// itself failed — never force-delete on an inconclusive check), or
// "worktree-removed".
func (c *reapKong) removeWorktreeIfClean(ctx *cli.Context, worktree, callerWorktree string) string {
	if worktree == "" {
		return "worktree-unknown"
	}
	if callerWorktree != "" && worktree == callerWorktree {
		fmt.Fprintf(ctx.Stdout, "reap: worktree %s is the calling session's own cwd, skipping removal\n", worktree)
		return "worktree-skipped-caller-cwd"
	}
	exists, clean, err := c.worktreeClean(worktree)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "reap: check worktree %s: %v\n", worktree, err)
	}
	if !exists {
		return "worktree-absent"
	}
	if !clean {
		fmt.Fprintf(ctx.Stdout, "reap: worktree %s is not provably clean, skipping removal\n", worktree)
		return "worktree-dirty-skipped"
	}
	if err := c.removeWorktree(worktree); err != nil {
		fmt.Fprintf(ctx.Stderr, "reap: remove worktree %s: %v\n", worktree, err)
		return "worktree-dirty-skipped"
	}
	return "worktree-removed"
}

// ── real implementations ─────────────────────────────────────────────────────

// defaultRmSession runs `claude rm <id>`, bounded via runBoundedClaude.
// reap_orphans.go only wraps `claude stop`; reap additionally needs `claude
// rm` to actually clear the session from the agents view.
func defaultRmSession(id string) error {
	_, err := runBoundedClaude(context.Background(), claudeCallTimeout, "rm", id)
	return err
}

// defaultReapRemoveWorktree removes worktree, resolving its owning repo root
// via git's common-dir (the shared .git directory every linked worktree
// points back to) so the caller never needs to separately track or pass the
// main repo root. `claude rm` does NOT remove an ateam worktree — proven
// live (contract, "WORKTREE REMOVAL") — so this explicit step is mandatory
// for actually reclaiming the disk space and `git worktree list` entry.
func defaultReapRemoveWorktree(worktree string) error {
	git := gitutil.New()
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
	// failure here never fails the reap.
	cctx, cancel := context.WithTimeout(context.Background(), reapGitTimeout)
	defer cancel()
	_ = exec.CommandContext(cctx, "git", "-C", repoRoot, "worktree", "prune").Run()
	return nil
}

// defaultWorktreeClean reports whether worktree has no uncommitted changes
// (`git status --porcelain` empty) and no commits reachable from HEAD that
// are absent from every remote-tracking ref (`git rev-list --count HEAD
// --not --remotes` == "0"). This is upstream-agnostic: it catches any
// commit missing from ALL remotes, not just a single configured upstream,
// so it also works on branches with no upstream configured at all (for
// example local-only review-pr-<N> branches). A missing directory reports
// exists=false. Any inconclusive result — the status/rev-list subprocess
// itself failing — reports clean=false: the contract requires PROOF of no
// unpushed work before a force-remove, not merely absence of proof of some.
func defaultWorktreeClean(worktree string) (exists bool, clean bool, err error) {
	info, statErr := os.Stat(worktree)
	if statErr != nil || !info.IsDir() {
		return false, false, nil
	}

	statusCtx, statusCancel := context.WithTimeout(context.Background(), reapGitTimeout)
	defer statusCancel()
	statusOut, err := exec.CommandContext(statusCtx, "git", "-C", worktree, "status", "--porcelain").Output()
	if err != nil {
		return true, false, fmt.Errorf("git status --porcelain: %w", err)
	}
	if strings.TrimSpace(string(statusOut)) != "" {
		return true, false, nil
	}

	localOnlyCtx, localOnlyCancel := context.WithTimeout(context.Background(), reapGitTimeout)
	defer localOnlyCancel()
	localOnlyOut, err := exec.CommandContext(localOnlyCtx, "git", "-C", worktree, "rev-list", "--count", "HEAD", "--not", "--remotes").Output()
	if err != nil {
		// Cannot prove every commit exists on some remote.
		return true, false, nil
	}
	if strings.TrimSpace(string(localOnlyOut)) != "0" {
		return true, false, nil
	}
	return true, true, nil
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
// concern.
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
