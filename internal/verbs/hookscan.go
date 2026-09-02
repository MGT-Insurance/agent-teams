// This file implements the combined hook-scan verb (agent-teams-1y0m.8): a
// single-Dolt-open replacement for the two calls
// plugins/agent-teams/hooks/scripts/inbox-drain.sh previously made on every
// UserPromptSubmit prompt — `ateam resolve-initiative` (1 bd open) followed
// by `ateam mail inbox --peek` (2 bd opens: recipient-resolve + unread
// query). Embedded Dolt takes an exclusive per-process lock at storage-open,
// so under many concurrent sessions those opens serialize and the hook can
// be SIGTERM'd at Claude Code's 30s UserPromptSubmit timeout, silently
// dropping the mail signal.
//
// This verb answers both questions the hook needs — the initiative id for a
// path, and whether that id has unread mail — from exactly ONE `bd list
// --status=open --include-infra --json` call. --include-infra additively
// surfaces message-type beads alongside the initiative ("task"-type) beads
// already returned by a plain --status=open list (verified: the two queries
// return the identical set of task-type issues, --include-infra only adds
// message/agent/role beads on top), so reusing matchByWorktreeOrAncestor
// against this superset list is safe and unchanged from resolveInitiativeKong.
package verbs

import (
	"fmt"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
	"github.com/mgt-insurance/agent-teams/internal/repoconfig"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
)

// RegisterHookScanKong registers the hook-scan verb onto p using native kong
// structs.
func RegisterHookScanKong(p *cli.Parser) {
	p.AddVerb("hook-scan", "Resolve a path to its owning initiative and report unread mail, from one Dolt open.", &hookScanKong{})
}

// hookScanKong is the kong-converted form of the combined resolve+unread
// scan. There are two mutually exclusive ways to identify the recipient:
//
//   - Path: resolved via matchByWorktreeOrAncestor exactly like
//     resolveInitiativeKong (ancestor-or-self, longest-worktree-wins,
//     silenced when the matched initiative's repo: field is disabled) —
//     reused verbatim, not re-derived (the matching rule is frozen in
//     internal/initiative/doc.go).
//   - ID: an already-known recipient (e.g. StewardHandle, which
//     inbox-drain.sh's own resolve-steward.sh identifies via a marker file —
//     the Steward has no worktree: line to match against, so path resolution
//     can never find it). Skips resolution and checks unread mail for that
//     id directly.
//
// Both branches read the SAME single issue list fetched at the top of Run,
// so either way this verb makes exactly one bd invocation — except when
// SessionID resolves a match (see SessionID doc below), which costs one
// additional bd call inside resolveInitiativeBySession.
type hookScanKong struct {
	Path      string `arg:"" optional:"" name:"path" help:"Absolute path to resolve to its owning initiative, ancestor-or-self. Mutually exclusive with --id."`
	ID        string `name:"id" help:"Skip path/worktree resolution; check unread mail directly for this already-known recipient id."`
	SessionID string `name:"session-id" help:"Current Claude session id — when Path resolution is in play, tried first via its durable initiative tie before falling back to Path. Ignored when --id is given."`
}

// Validate enforces the Path/ID exclusivity — exactly one must be given.
func (c *hookScanKong) Validate() error {
	if c.Path == "" && c.ID == "" {
		return cli.Usagef("ateam hook-scan: requires PATH or --id")
	}
	if c.Path != "" && c.ID != "" {
		return cli.Usagef("ateam hook-scan: PATH and --id are mutually exclusive")
	}
	return nil
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
//
// OUTPUT CONTRACT, which inbox-drain.sh depends on: on a match, "id: <id>"
// then "unread: <N>" (the actual unread count, not a 0/1 flag), each on its
// own line, nothing else. On no match (unregistered path, disabled repo), no
// output at all and exit 0 — a hook's normal, expected condition for any
// session outside a registered worktree, matching resolveInitiativeKong's
// contract so hooks never start reporting failures on an ordinary session. A
// bd failure now returns the error (non-zero exit) instead of exit 0 —
// errors propagate rather than being silently swallowed; inbox-drain.sh's
// `2>/dev/null || true` capture absorbs this unchanged.
func (c *hookScanKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam hook-scan: nil context")
	}

	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--status=open", "--include-infra", "--json"); err != nil {
		return err
	}

	id := c.ID
	if id == "" {
		var match *bd.Issue
		// Session-first, Claude runtime only: hook-scan serves the
		// Claude-plugin shell hooks exclusively (wake-watcher.sh,
		// inbox-drain.sh) — the Codex plugin resolves unread mail via its own
		// codex-hook path (resolveCodexHookInitiative), never this verb. This
		// restores signaling for a session whose launch cwd doesn't match its
		// registered worktree (at-0xnp1): its durable session tie still
		// resolves it even though the path-only match below would find
		// nothing.
		if c.SessionID != "" {
			if issue, found, err := resolveInitiativeBySession(ctx, sessionruntime.Claude, c.SessionID); err == nil && found {
				match = &issue
			}
		}
		if match == nil {
			// Match candidates exclude message-type beads: --include-infra
			// widens the list beyond the plain --status=open
			// resolveInitiativeKong used, and a message body containing a
			// literal "worktree: <path>" line could otherwise misroute to
			// the message bead instead of the initiative that actually owns
			// that worktree. Only initiatives carry a worktree: line, so
			// excluding message-type is sufficient — other non-initiative
			// beads (agent/role) have no worktree: line to match.
			match = matchByWorktreeOrAncestor(excludeMessageType(issues), c.Path)
			if match == nil {
				return nil
			}
		}
		if repo := initiative.Of(*match).Repo; repo != "" && !repoconfig.Enabled(repo) {
			return nil
		}
		id = match.ID
	}

	// Duplicate-steward defense-in-depth backstop (agent-teams-e3mq.31),
	// restored exactly as inboxKong.Run (messaging.go) applies it: a
	// duplicate steward session must not surface a mail signal for the
	// incumbent's mailbox. inbox-drain.sh's steward branch calls hook-scan
	// via `2>/dev/null || true`, so this error is absorbed and no signal is
	// emitted — matching the old peek-guard behavior.
	if id == StewardHandle {
		if err := checkStewardInboxGuard(ctx); err != nil {
			return err
		}
	}

	fmt.Fprintf(ctx.Stdout, "id: %s\n", id)

	// unread predicate mirrors queryUnreadMessages/filterMessageType
	// (messaging.go): type==message AND assignee==id AND status==open AND
	// NOT label "read". Status==open is already guaranteed by the list call
	// above; the explicit check documents the reused predicate exactly. Uses
	// the FULL issue list (not the match-candidate exclusion above) — that
	// exclusion only matters for worktree matching.
	unread := 0
	for _, iss := range filterMessageType(issues) {
		if iss.Assignee == id && iss.Status == "open" && !hasLabel(iss.Labels, "read") {
			unread++
		}
	}
	fmt.Fprintf(ctx.Stdout, "unread: %d\n", unread)
	return nil
}

// excludeMessageType returns issues with IssueType != "message" — the
// inverse of filterMessageType (messaging.go). Used to build the
// match-candidate slice for matchByWorktreeOrAncestor so message beads,
// which can never legitimately carry a worktree: line, are never match
// candidates (see FIX 3 doc above Run).
func excludeMessageType(issues []bd.Issue) []bd.Issue {
	var out []bd.Issue
	for _, iss := range issues {
		if iss.IssueType != "message" {
			out = append(out, iss)
		}
	}
	return out
}
