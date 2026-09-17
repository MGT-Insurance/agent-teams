// Package verbs — reap.go implements the `ateam reap [target]` verb.
//
// reap is the loop-closing teardown for a review-pr initiative: once a review
// session's PR-review work is done and its initiative closes, the Claude
// session and its git worktree still sit around (the harness never reclaims
// either on its own — see the WORKTREE REMOVAL note below). reap has two
// modes, selected by the optional positional target:
//
//   - SCAN mode (no target): what pr-shepherd calls every tick. Enumerates
//     CLOSED initiatives, applies the review-shape / grace / pending-comment /
//     already-reaped gates, and tears down each survivor (session + clean-only
//     worktree removal).
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
	"path/filepath"
	"strings"
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
// mirrors gitProbeTimeout's reasoning (hung_workproduct.go).
const reapGitTimeout = 10 * time.Second

// RegisterReapKong registers the reap verb onto p.
func RegisterReapKong(p *cli.Parser) {
	p.AddVerb("reap", "Tear down closed review sessions past grace (scan mode, what pr-shepherd calls every tick) or one target right now (one-off mode); see reap-orphans for stop-only cwd-missing cleanup.", &reapKong{
		agentsFunc:     defaultAgentsJSONAll,
		now:            time.Now,
		stopSession:    defaultStopSession,
		rmSession:      defaultRmSession,
		removeWorktree: defaultReapRemoveWorktree,
		worktreeClean:  defaultWorktreeClean,
		pendingComment: defaultPendingReviewComment,
		noteFunc:       defaultReapNote,
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

// reapKong implements `ateam reap [target]`. The DI fields are tagged
// kong:"-" so kong ignores them; tests substitute fakes without touching the
// struct registration — same pattern as reapOrphansKong.
type reapKong struct {
	Target string        `arg:"" name:"target" optional:"" help:"Initiative id or a bare Claude short session id to reap right now, bypassing the grace/pending/reaped-note gates. Omit to scan every closed review initiative (what pr-shepherd calls every tick)."`
	Grace  time.Duration `name:"grace" default:"20m" help:"Scan mode only: minimum time since bd close before an initiative becomes reap-eligible."`

	agentsFunc     agentsJSONFunc           `kong:"-"`
	now            func() time.Time         `kong:"-"`
	stopSession    stopSessionFunc          `kong:"-"`
	rmSession      rmSessionFunc            `kong:"-"`
	removeWorktree reapRemoveWorktreeFunc   `kong:"-"`
	worktreeClean  worktreeCleanFunc        `kong:"-"`
	pendingComment pendingReviewCommentFunc `kong:"-"`
	noteFunc       reapNoteFunc             `kong:"-"`
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

	if c.Target == "" {
		return c.runScan(ctx)
	}
	return c.runOneOff(ctx, c.Target)
}

// ── SCAN mode ────────────────────────────────────────────────────────────────

// runScan enumerates closed initiatives via ctx.BD (the global initiative
// registry) and reaps every review-shaped, not-yet-reaped survivor past
// grace with no pending comment. Never hard-errors on a single initiative
// (log + continue) — only a whole-scan failure (the bd list call itself)
// returns non-nil, per the contract's exit rule.
func (c *reapKong) runScan(ctx *cli.Context) error {
	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--status=closed", "--json"); err != nil {
		return fmt.Errorf("ateam reap: list closed initiatives: %w", err)
	}

	now := c.now()
	callerID := os.Getenv("CLAUDE_SESSION_ID")

	for _, iss := range issues {
		prURL, ok := initiative.ReviewPRURL(iss)
		if !ok {
			continue // not review-shaped: untouched, never reap's concern
		}
		f := initiative.Of(iss)
		if f.Runtime == "codex" {
			continue // Ring 1: codex teardown is not implemented by this verb
		}
		if hasReapedNote(iss.Notes) {
			continue // already reaped: at-most-once, no repeat gh probe
		}

		closedAt, err := time.Parse(time.RFC3339, iss.ClosedAt)
		if iss.ClosedAt == "" || err != nil {
			fmt.Fprintf(ctx.Stderr, "reap: %s: closed_at missing or unparseable (%q), skipping\n", iss.ID, iss.ClosedAt)
			c.journal(ctx, now, iss.ID, "", f.Runtime, "scan", "skip-grace", "")
			continue
		}
		if now.Sub(closedAt) < c.Grace {
			c.journal(ctx, now, iss.ID, "", f.Runtime, "scan", "skip-grace", "")
			continue
		}

		if c.hasPendingComment(prURL) {
			c.journal(ctx, now, iss.ID, "", f.Runtime, "scan", "skip-pending", "")
			continue
		}

		sessions, sessErr := c.agentsFunc()
		action := c.teardownClaudeSession(ctx, sessions, sessErr, matchInitiativeSession(f), callerID)
		wtOutcome := c.removeWorktreeIfClean(ctx, f.Worktree)
		c.journal(ctx, now, iss.ID, "", f.Runtime, "scan", action, wtOutcome)

		if action != "failed" {
			// Written after teardown (or when the session was already gone,
			// action=="no-session") — never on a genuine teardown failure,
			// so a failing stop/rm is retried on the next tick rather than
			// silently marked done.
			if err := c.noteFunc(ctx, iss.ID, now); err != nil {
				fmt.Fprintf(ctx.Stderr, "reap: %s: write reaped note: %v\n", iss.ID, err)
			}
		}
	}
	return nil
}

// hasPendingComment probes whether prURL has a pending unanswered inline
// review comment, treating any unparseable URL, probe error, or nil probe
// func as pending — S3(d)'s "proof of absence required" contract (mirrors
// hung_tick's conservative default).
func (c *reapKong) hasPendingComment(prURL string) bool {
	if c.pendingComment == nil {
		return true
	}
	ownerRepo, prNumber, ok := parsePrURL(prURL)
	if !ok {
		return true
	}
	pending, err := c.pendingComment(ownerRepo, prNumber)
	if err != nil {
		return true
	}
	return pending
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
			wtOutcome := c.removeWorktreeIfClean(ctx, f.Worktree)
			c.journal(ctx, now, iss.ID, target, "codex", "one-off", "skip-codex-ring1", wtOutcome)
			return fmt.Errorf("ateam reap: %s is a codex initiative; codex session teardown is not implemented (Ring 1) — worktree outcome: %s", target, wtOutcome)
		}

		sessions, sessErr := c.agentsFunc()
		action := c.teardownClaudeSession(ctx, sessions, sessErr, matchInitiativeSession(f), callerID)
		wtOutcome := c.removeWorktreeIfClean(ctx, f.Worktree)
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
		wtOutcome := c.removeWorktreeIfClean(ctx, matched.CWD)
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
// contract's clean-only safety rule. Returns the journal outcome string:
// "worktree-unknown" (no worktree path resolved at all), "worktree-absent"
// (nothing on disk to remove — a clean no-op), "worktree-dirty-skipped"
// (uncommitted or unpushed work, or the clean-check itself failed — never
// force-delete on an inconclusive check), or "worktree-removed".
func (c *reapKong) removeWorktreeIfClean(ctx *cli.Context, worktree string) string {
	if worktree == "" {
		return "worktree-unknown"
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

// defaultRmSession runs `claude rm <id>`. reap_orphans.go only wraps `claude
// stop`; reap additionally needs `claude rm` to actually clear the session
// from the agents view.
func defaultRmSession(id string) error {
	cmd := exec.Command("claude", "rm", id)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude rm %s: %w (output: %s)", id, err, string(out))
	}
	return nil
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

// reapJournalPath returns <ctx.Home>/reap-journal.jsonl.
func reapJournalPath(home string) string {
	return filepath.Join(home, reapJournalFileName)
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
// parent directory and file as needed. Best-effort: an I/O error here must
// never block or fail reap itself, so this returns an error for the caller
// to log, not to abort on.
func appendReapJournal(path string, entry reapJournalEntry) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
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
