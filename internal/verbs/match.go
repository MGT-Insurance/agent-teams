// This file is owned by Track B (JSON-parsing verbs).
package verbs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
	"github.com/mgt-insurance/agent-teams/internal/repoconfig"
)

// errSessionTiedElsewhere is the sentinel wrapped by appendSessionID's
// one-open-initiative guard. Callers that need to distinguish this specific
// conflict from any other appendSessionID failure (e.g. tie_session.go's
// warn-but-don't-break-session-start path) should use errors.Is against this
// value rather than matching on the error string.
var errSessionTiedElsewhere = errors.New("session already tied to another open initiative")

// canonicalPath resolves symlinks in p so cwd-vs-worktree comparisons aren't
// defeated by symlinked directories (e.g. macOS /tmp -> /private/tmp). Falls
// back to filepath.Clean when p doesn't exist (EvalSymlinks requires the path
// to exist — e.g. a worktree already removed) or on any other resolution
// error, so callers always get a normalised string to compare.
func canonicalPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}

// RegisterMatchKong registers match verbs onto p using native kong structs.
func RegisterMatchKong(p *cli.Parser) {
	p.AddVerb("audit", "Audit global workspace for leaked work beads.", &auditKong{})
	p.AddVerb("resume-match", "Find the open initiative whose worktree is EXACTLY this path (no ancestor matching).", &resumeMatchKong{})
	p.AddVerb("resume-match-closed", "Find the most-recently-closed initiative whose worktree is EXACTLY this path (no ancestor matching).", &resumeMatchClosedKong{})
	p.AddVerb("resolve-initiative", "Find the open initiative owning this path, ancestor-or-self (the worktree root or any subdirectory of it).", &resolveInitiativeKong{})
}

// appendSessionID ties sessionID to initiativeID by writing back the
// description initiative.WithSession composes, via the sanctioned
// global-workspace write path (bd update --body-file, same mechanism as
// updateDescriptionKong). WithSession is append-only, so nothing already on
// the bead — including canonical keys internal/initiative does not model — is
// re-derived or dropped.
//
// This function is a GUARD PLUS an append, and only the append half lives in
// initiative.WithSession. Do not collapse it into a bare WithSession call:
//
// Idempotent: if sessionID is already recorded on initiativeID (e.g. a
// respawn reusing the same session id), this is a no-op — it returns before
// listing open initiatives and before any write, which is stronger than
// WithSession's own idempotency (that returns an unchanged description, which
// would still cost a list and a redundant bd update here).
//
// One-open-initiative guard: before appending, scans every OPEN initiative;
// if sessionID is already recorded on a DIFFERENT open initiative, returns an
// error instead of silently tying the session to two initiatives at once
// (agent-teams-zalv.1 §2). initiative.WithSession deliberately does NOT
// implement this — the check needs a live bd client to enumerate open
// initiatives, which a pure function is not given, so it stays this caller's
// responsibility. Deleting it compiles cleanly and leaves WithSession's own
// tests green; internal/verbs/session_test.go is what catches its absence.
//
// Validation: sessionID must be non-empty and contain no whitespace (including
// newlines) — it is spliced verbatim into a "session: <id>" description line,
// so an unvalidated value could inject extra lines or corrupt the field parse
// for a future untrusted caller. WithSession re-checks both, but the check
// stays here too so rejection happens before any read/write.
//
// The signature is load-bearing: tie_session.go calls this and is the only
// other caller. Change the body, not the shape.
func appendSessionID(ctx *cli.Context, initiativeID, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("appendSessionID: sessionID must not be empty")
	}
	if strings.ContainsAny(sessionID, " \t\r\n") {
		return fmt.Errorf("appendSessionID: sessionID must not contain whitespace: %q", sessionID)
	}

	issue, err := bd.ShowIssue(ctx.BD, initiativeID)
	if err != nil {
		return fmt.Errorf("appendSessionID: bd show %s: %w", initiativeID, err)
	}
	for _, id := range initiative.Of(issue).Sessions {
		if id == sessionID {
			return nil // already tied to this initiative — respawn reuses the same id
		}
	}

	var openIssues []bd.Issue
	if err := ctx.BD.RunJSON(&openIssues, "list", "--status=open", "--json"); err != nil {
		return fmt.Errorf("appendSessionID: list open initiatives: %w", err)
	}
	for _, other := range openIssues {
		if other.ID == initiativeID {
			continue
		}
		for _, id := range initiative.Of(other).Sessions {
			if id == sessionID {
				return fmt.Errorf(
					"appendSessionID: session %s is already tied to open initiative %s — refusing to also tie it to %s: %w",
					sessionID, other.ID, initiativeID, errSessionTiedElsewhere)
			}
		}
	}

	plan, err := initiative.WithSession(issue, sessionID)
	if err != nil {
		return fmt.Errorf("appendSessionID: %w", err)
	}
	newDescription := plan.Description

	tmpFile, err := os.CreateTemp("", "ateam-tie-session-*.txt")
	if err != nil {
		return fmt.Errorf("appendSessionID: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpFile.WriteString(newDescription); err != nil {
		tmpFile.Close()
		return fmt.Errorf("appendSessionID: write temp file: %w", err)
	}
	tmpFile.Close()

	if _, err := ctx.BD.Run("update", initiativeID, "--body-file="+tmpPath); err != nil {
		return fmt.Errorf("appendSessionID: bd update %s: %w", initiativeID, err)
	}
	return nil
}

// matchSessionsForInitiative returns the sessions tied to iss, ordered
// PRIMARY-FIRST (registration order): callers wanting the primary/DRI take
// [0]; callers aggregating over the tied set (e.g. hung-scan) iterate all.
//
// When iss has "session:" lines, only LIVE sessions (PID present) whose
// SessionID is in that set are returned, in registration order. When iss has
// no session: lines (legacy, pre-migration entry — an empty f.Sessions is the
// discriminator), this falls back to the existing worktree/Name match
// (matchSessionByWorktree) wrapped to a 0-or-1-element slice, so legacy
// routing/classification is unchanged byte-for-byte (agent-teams-zalv.1 §2,
// §5).
func matchSessionsForInitiative(sessions []agentSession, iss bd.Issue) []agentSession {
	f := initiative.Of(iss)
	ids := f.Sessions
	if len(ids) == 0 {
		if s := matchSessionByWorktree(sessions, f.Worktree); s != nil {
			return []agentSession{*s}
		}
		return nil
	}

	byID := make(map[string]*agentSession, len(sessions))
	for i := range sessions {
		if sessions[i].SessionID != "" {
			byID[sessions[i].SessionID] = &sessions[i]
		}
	}
	var out []agentSession
	for _, id := range ids {
		if s, ok := byID[id]; ok && s.PID != nil {
			out = append(out, *s)
		}
	}
	return out
}

// findOffenders returns issues carrying no worktree routing field — work
// beads that leaked into the global workspace, which holds only
// initiative-tracking beads.
func findOffenders(issues []bd.Issue) []bd.Issue {
	var out []bd.Issue
	for _, iss := range issues {
		if initiative.Of(iss).Worktree == "" {
			out = append(out, iss)
		}
	}
	return out
}

// matchByWorktree returns the first issue in issues whose "worktree: <path>"
// line resolves (after symlink normalisation) to the same path as path.
func matchByWorktree(issues []bd.Issue, path string) *bd.Issue {
	want := canonicalPath(path)
	for i := range issues {
		if wt := initiative.Of(issues[i]).Worktree; wt != "" && canonicalPath(wt) == want {
			return &issues[i]
		}
	}
	return nil
}

// matchByWorktreeOrAncestor returns the issue whose "worktree: <path>" line is
// the most specific match for path: either an exact match, or the longest
// worktree path that is a proper ancestor directory of path (so cwd may be
// any subdirectory of the registered worktree, e.g. apps/mithril nested under
// the worktree root). The trailing separator guard means a sibling directory
// that merely shares a string prefix (worktree /a/b vs cwd /a/b-foo) never
// matches. Used by resolveMyInitiative (the `ateam mail inbox` path) and by
// resolveInitiativeKong (the `ateam resolve-initiative` verb the plugin's
// hooks call) — matchByWorktree remains strict-equality for resume-match,
// where prefix matching would risk resuming the wrong initiative.
func matchByWorktreeOrAncestor(issues []bd.Issue, path string) *bd.Issue {
	want := canonicalPath(path)
	var best *bd.Issue
	var bestLen int
	for i := range issues {
		wt := initiative.Of(issues[i]).Worktree
		if wt == "" {
			continue
		}
		wtCanon := canonicalPath(wt)
		if wtCanon != want && !strings.HasPrefix(want, wtCanon+string(filepath.Separator)) {
			continue
		}
		if len(wtCanon) > bestLen {
			best = &issues[i]
			bestLen = len(wtCanon)
		}
	}
	return best
}

// matchAllByWorktree returns all issues whose "worktree: <path>" line
// resolves (after symlink normalisation) to the same path as path, sorted by
// CreatedAt descending.
func matchAllByWorktree(issues []bd.Issue, path string) []bd.Issue {
	want := canonicalPath(path)
	var out []bd.Issue
	for _, iss := range issues {
		if wt := initiative.Of(iss).Worktree; wt != "" && canonicalPath(wt) == want {
			out = append(out, iss)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out
}

// ── kong structs ──────────────────────────────────────────────────────────────

// auditKong is the kong-converted form of auditCommand.
type auditKong struct{}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *auditKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam audit: nil context")
	}
	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--all", "--json"); err != nil {
		issues = nil
	}

	ok := true
	offenders := findOffenders(issues)
	if len(offenders) == 0 {
		fmt.Fprintln(ctx.Stdout, "audit: clean — global workspace contains only initiative-tracking beads")
	} else {
		ok = false
		fmt.Fprintln(ctx.Stderr, "audit: LEAKED work beads in the global workspace — these belong in the PROJECT repo, NOT here:")
		for _, iss := range offenders {
			fmt.Fprintf(ctx.Stderr, "  %s\t%s\n", iss.ID, iss.Title)
		}
		fmt.Fprintln(ctx.Stderr, "")
		fmt.Fprintln(ctx.Stderr, "The global workspace holds ONLY initiative-tracking beads + role memories.")
		fmt.Fprintln(ctx.Stderr, "Move each to its project repo's .beads and delete it here (bd -C <workspace> delete <id>).")
	}

	if !checkGlobalPrimeBudget(ctx) {
		ok = false
	}

	if !ok {
		return cli.Silent(1)
	}
	return nil
}

// resumeMatchKong is the kong-converted form of resumeMatchCommand.
type resumeMatchKong struct {
	WorktreePath string `arg:"" name:"worktree-path" help:"Absolute path to the worktree directory."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *resumeMatchKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam resume-match: nil context")
	}

	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--status=open", "--json"); err != nil {
		return nil
	}

	if match := matchByWorktree(issues, c.WorktreePath); match != nil {
		fmt.Fprintln(ctx.Stdout, match.ID)
	}
	return nil
}

// resolveInitiativeKong resolves a filesystem path to the OPEN initiative that
// owns it. It exists so the plugin's shell hooks stop re-deriving the routing
// field rule in jq: before this verb, wake-watcher.sh, inbox-drain.sh,
// session-start-inbox.sh and compact-recovery.sh each shelled `bd -C $ATH list
// --status=open --json` and picked the worktree line apart themselves, which
// both violated the cardinal rule (raw bd against the global workspace) and
// meant a change to the rule in internal/initiative silently stopped matching
// there — hooks fail soft, so initiative resolution would just quietly stop
// with no error anywhere.
//
// ANCESTOR-OR-SELF semantics, via matchByWorktreeOrAncestor: the registered
// worktree root itself or any subdirectory of it resolves, and the most
// specific (longest) worktree wins. Deliberately distinct from resume-match and
// resume-match-closed, which are EXACT (see matchByWorktree) because /dri owns
// its checkout root and prefix matching there could resume the wrong
// initiative. Three verbs over two semantics is easy to confuse, so each one's
// kong help text names its own semantics — keep it that way when adding a
// fourth.
//
// OUTPUT CONTRACT, which all four hooks depend on: the bare initiative id on
// stdout and nothing else, or NO output at all with exit 0 when no open
// initiative owns the path. "No match" is a normal, expected condition for a
// hook (any plain claude session in an unregistered directory hits it), so it
// must never become an error or the hooks would start reporting failures on
// every ordinary session. A bd failure is treated the same way, matching
// resumeMatchKong.
//
// A path match whose initiative has a "repo:" field is additionally silenced
// (treated as no match) when repoconfig.Enabled reports that repo disabled —
// this is the wake-watcher/inbox-drain/etc. half of the .agent-teams kill
// switch: a repo that goes disabled after its initiative was dispatched stops
// re-arming hooks for it, the same as if the initiative had never resolved. A
// missing "repo:" field (legacy data) skips this check rather than resolving
// a marker file against an empty/relative path.
type resolveInitiativeKong struct {
	Path string `arg:"" name:"path" help:"Absolute path to resolve — a registered worktree root or any subdirectory of one."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *resolveInitiativeKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam resolve-initiative: nil context")
	}

	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--status=open", "--json"); err != nil {
		return nil
	}

	match := matchByWorktreeOrAncestor(issues, c.Path)
	if match == nil {
		return nil
	}
	if repo := initiative.Of(*match).Repo; repo != "" && !repoconfig.Enabled(repo) {
		return nil
	}
	fmt.Fprintln(ctx.Stdout, match.ID)
	return nil
}

// resumeMatchClosedKong is the kong-converted form of resumeMatchClosedCommand.
type resumeMatchClosedKong struct {
	WorktreePath string `arg:"" name:"worktree-path" help:"Absolute path to the worktree directory."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *resumeMatchClosedKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam resume-match-closed: nil context")
	}

	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--status=closed", "--json"); err != nil {
		return nil
	}

	matches := matchAllByWorktree(issues, c.WorktreePath)
	if len(matches) > 0 {
		fmt.Fprintln(ctx.Stdout, matches[0].ID)
	}
	return nil
}
