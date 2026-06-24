// This file is owned by Track B (JSON-parsing verbs).
package verbs

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

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

// matchByWorktree returns the first issue in issues whose description contains
// an exact line equal to "worktree: "+path (not prefix, not contains).
func matchByWorktree(issues []bd.Issue, path string) *bd.Issue {
	needle := "worktree: " + path
	for i := range issues {
		for _, line := range strings.Split(issues[i].Description, "\n") {
			if line == needle {
				return &issues[i]
			}
		}
	}
	return nil
}

// matchAllByWorktree returns all issues whose description contains an exact
// line equal to "worktree: "+path, sorted by CreatedAt descending.
func matchAllByWorktree(issues []bd.Issue, path string) []bd.Issue {
	needle := "worktree: " + path
	var out []bd.Issue
	for _, iss := range issues {
		for _, line := range strings.Split(iss.Description, "\n") {
			if line == needle {
				out = append(out, iss)
				break
			}
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
