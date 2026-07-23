// This file is owned by Track B (JSON-parsing verbs).
package verbs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

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
	p.AddVerb("resume-match", "Find the open initiative for a worktree path.", &resumeMatchKong{})
	p.AddVerb("resume-match-closed", "Find the most-recently-closed initiative for a worktree path.", &resumeMatchClosedKong{})
}

// hasWorktreeLine reports whether any line in description starts with "worktree:".
func hasWorktreeLine(description string) bool {
	for _, line := range strings.Split(description, "\n") {
		if strings.HasPrefix(line, "worktree:") {
			return true
		}
	}
	return false
}

// sessionIDs parses all "session: <id>" lines from description, in
// registration order (first line = first-registered session, per the
// at-ps11 contract, agent-teams-zalv.1 §1).
func sessionIDs(description string) []string {
	var ids []string
	for _, line := range strings.Split(description, "\n") {
		if strings.HasPrefix(line, "session: ") {
			ids = append(ids, strings.TrimRight(strings.TrimPrefix(line, "session: "), " \t\r"))
		}
	}
	return ids
}

// hasSessionLine reports whether any line in description starts with
// "session:" — the migration discriminator (mirrors hasWorktreeLine): an
// initiative with no session: lines is a legacy entry and matchers must fall
// back to the worktree/Name match for it (agent-teams-zalv.1 §5).
func hasSessionLine(description string) bool {
	for _, line := range strings.Split(description, "\n") {
		if strings.HasPrefix(line, "session:") {
			return true
		}
	}
	return false
}

// appendSessionID ties sessionID to initiativeID by appending a
// "session: <id>" line to the initiative's description, via the sanctioned
// global-workspace write path (bd update --body-file, same mechanism as
// updateDescriptionKong).
//
// Idempotent: if sessionID is already recorded on initiativeID (e.g. a
// respawn reusing the same session id), this is a no-op.
//
// One-open-initiative guard: before appending, scans every OPEN initiative;
// if sessionID is already recorded on a DIFFERENT open initiative, returns an
// error instead of silently tying the session to two initiatives at once
// (agent-teams-zalv.1 §2).
func appendSessionID(ctx *cli.Context, initiativeID, sessionID string) error {
	issue, err := bd.ShowIssue(ctx.BD, initiativeID)
	if err != nil {
		return fmt.Errorf("appendSessionID: bd show %s: %w", initiativeID, err)
	}
	for _, id := range sessionIDs(issue.Description) {
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
		for _, id := range sessionIDs(other.Description) {
			if id == sessionID {
				return fmt.Errorf(
					"appendSessionID: session %s is already tied to open initiative %s — refusing to also tie it to %s",
					sessionID, other.ID, initiativeID)
			}
		}
	}

	newDescription := strings.TrimRight(issue.Description, "\n") + "\nsession: " + sessionID + "\n"
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
// no session: lines (legacy, pre-migration entry — hasSessionLine is the
// discriminator), this falls back to the existing worktree/Name match
// (matchSessionByWorktree) wrapped to a 0-or-1-element slice, so legacy
// routing/classification is unchanged byte-for-byte (agent-teams-zalv.1 §2,
// §5).
func matchSessionsForInitiative(sessions []agentSession, iss bd.Issue) []agentSession {
	ids := sessionIDs(iss.Description)
	if len(ids) == 0 {
		if s := matchSessionByWorktree(sessions, worktreePath(iss.Description)); s != nil {
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

// findOffenders returns issues whose description has no line starting with "worktree:".
func findOffenders(issues []bd.Issue) []bd.Issue {
	var out []bd.Issue
	for _, iss := range issues {
		if !hasWorktreeLine(iss.Description) {
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
		if wt := worktreePath(issues[i].Description); wt != "" && canonicalPath(wt) == want {
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
// matches. Used ONLY by resolveMyInitiative (the `ateam mail inbox` path) —
// matchByWorktree remains strict-equality for resume-match, where prefix
// matching would risk resuming the wrong initiative.
func matchByWorktreeOrAncestor(issues []bd.Issue, path string) *bd.Issue {
	want := canonicalPath(path)
	var best *bd.Issue
	var bestLen int
	for i := range issues {
		wt := worktreePath(issues[i].Description)
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
		if wt := worktreePath(iss.Description); wt != "" && canonicalPath(wt) == want {
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

	offenders := findOffenders(issues)
	if len(offenders) == 0 {
		fmt.Fprintln(ctx.Stdout, "audit: clean — global workspace contains only initiative-tracking beads")
		return nil
	}

	fmt.Fprintln(ctx.Stderr, "audit: LEAKED work beads in the global workspace — these belong in the PROJECT repo, NOT here:")
	for _, iss := range offenders {
		fmt.Fprintf(ctx.Stderr, "  %s\t%s\n", iss.ID, iss.Title)
	}
	fmt.Fprintln(ctx.Stderr, "")
	fmt.Fprintln(ctx.Stderr, "The global workspace holds ONLY initiative-tracking beads + role memories.")
	fmt.Fprintln(ctx.Stderr, "Move each to its project repo's .beads and delete it here (bd -C <workspace> delete <id>).")

	return cli.Silent(1)
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
