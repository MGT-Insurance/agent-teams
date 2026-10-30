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
	"time"

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
// Both branches read the SAME single issue list, so a full scan makes exactly
// one bd invocation, including when SessionID is given: the session match runs
// over that list too. A quiet prompt makes none: see hookscan_cache.go.
type hookScanKong struct {
	Path      string `arg:"" optional:"" name:"path" help:"Absolute path to resolve to its owning initiative, ancestor-or-self. Mutually exclusive with --id."`
	ID        string `name:"id" help:"Skip path/worktree resolution; check unread mail directly for this already-known recipient id."`
	SessionID string `name:"session-id" help:"Current Claude session id — when Path resolution is in play, tried first via its durable initiative tie before falling back to Path. Ignored when --id is given."`
	HintID    string `name:"hint-id" help:"Initiative id the launcher published (ATEAM_INITIATIVE). Tried after the session tie and before path resolution; ignored unless it names an open initiative in the scanned list. Ignored with --id."`
}

// Validate enforces the Path/ID exclusivity — exactly one must be given.
func (c *hookScanKong) Validate() error {
	if c.Path == "" && c.ID == "" {
		return cli.Usagef("ateam hook-scan: requires PATH or --id")
	}
	if c.Path != "" && c.ID != "" {
		return cli.Usagef("ateam hook-scan: PATH and --id are mutually exclusive")
	}
	if c.HintID != "" && c.ID != "" {
		return cli.Usagef("ateam hook-scan: --hint-id and --id are mutually exclusive")
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

	// Cache key: the --id value (steward form) or the session id (path form).
	// With neither, there is no cache and every call takes the full path.
	cacheKey := c.ID
	if cacheKey == "" {
		cacheKey = c.SessionID
	}
	now := time.Now()
	cached, haveCache := readHookScanCache(ctx, cacheKey)
	if haveCache && c.ID != "" && cached.ID != c.ID {
		haveCache = false
	}

	// Fast path, no bd: a trusted, fresh cache entry with no unread mail and
	// no doorbell. The id comes from the cache; the repo-enabled check is the
	// same one the full path applies.
	if haveCache && cached.Trusted && cached.fresh(now) && cached.Unread == 0 &&
		!hookScanDoorbellPresent(ctx, cached.ID) {
		if cached.Repo != "" && !repoconfig.Enabled(cached.Repo) {
			return nil
		}
		fmt.Fprintf(ctx.Stdout, "id: %s\nunread: 0\n", cached.ID)
		return nil
	}

	issues, err := listOpenIssues(ctx, true)
	if err != nil {
		return err
	}

	id := c.ID
	repo := ""
	if id == "" {
		// Match candidates are initiativeRows(issues): --include-infra widens
		// the list beyond the plain --status=open resolveInitiativeKong uses
		// (message, agent, role, and ephemeral rows), and a message body
		// containing a literal "session: <id>" or "worktree: <path>" line
		// could otherwise misroute to the message bead instead of the
		// initiative that owns the session or worktree. Only initiatives carry
		// those lines, so this is the same candidate set resolve-initiative
		// sees.
		candidates := initiativeRows(issues)
		var match *bd.Issue
		// Session-first, Claude runtime only: hook-scan serves the
		// Claude-plugin shell hooks exclusively (wake-watcher.sh,
		// inbox-drain.sh) — the Codex plugin resolves unread mail via its own
		// codex-hook path (resolveCodexHookInitiative), never this verb. This
		// restores signaling for a session whose launch cwd doesn't match its
		// registered worktree (at-0xnp1): its durable session tie still
		// resolves it even though the path-only match below would find
		// nothing.
		if issue, found := matchInitiativeBySession(candidates, sessionruntime.Claude, c.SessionID); found {
			match = &issue
		}
		// Launcher hint next: a claimed bg spare's cwd can lag its worktree
		// while the async session tie has not landed yet, so the launcher's
		// ATEAM_INITIATIVE names the initiative directly. Only an open
		// initiative in the scanned list counts.
		if match == nil && c.HintID != "" {
			for _, row := range candidates {
				if row.ID == c.HintID {
					row := row
					match = &row
					break
				}
			}
		}
		if match == nil {
			match = matchByWorktreeOrAncestor(candidates, c.Path)
			if match == nil {
				deleteHookScanCache(ctx, cacheKey)
				return nil
			}
		}
		id = match.ID
		repo = initiative.Of(*match).Repo
	}

	// Doorbell race guard: inbox-drain.sh deletes the doorbell after this verb
	// returns, so mail that lands between the list above and that delete loses
	// its doorbell. Mark the entry untrusted so the next prompt rescans.
	entry := hookScanCacheEntry{
		ID:        id,
		Repo:      repo,
		CheckedAt: now,
		Trusted:   !hookScanDoorbellPresent(ctx, id),
	}

	if repo != "" && !repoconfig.Enabled(repo) {
		// Unread was not counted, so the entry must not be trusted: mail sent
		// while disabled has no doorbell, and re-enabling must rescan.
		entry.Trusted = false
		writeHookScanCache(ctx, cacheKey, entry)
		return nil
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
	entry.Unread = unread
	writeHookScanCache(ctx, cacheKey, entry)

	// Tell inbox-drain.sh when this scan was a cache-expiry or untrusted-cache
	// rescan that found mail, so it can log mail that arrived without a
	// doorbell. A rescan forced by a doorbell or by cached unread mail (the
	// per-prompt nag) is not a discovery and stays silent.
	if unread > 0 && haveCache && cached.Unread == 0 && (!cached.Trusted || !cached.fresh(now)) {
		reason := "ttl"
		if !cached.Trusted {
			reason = "untrusted"
		}
		age := max(now.Sub(cached.CheckedAt), 0)
		fmt.Fprintf(ctx.Stderr, "scan: full reason=%s cache_age=%ds\n", reason, int(age.Seconds()))
	}

	fmt.Fprintf(ctx.Stdout, "id: %s\nunread: %d\n", id, unread)
	return nil
}
