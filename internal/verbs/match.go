// This file is owned by Track B (JSON-parsing verbs).
package verbs

import (
	"fmt"
	"sort"
	"strings"

	"github.com/erlloyd/agent-teams/internal/bd"
	"github.com/erlloyd/agent-teams/internal/cli"
)

// RegisterMatch registers the JSON-parsing verbs:
// audit, resume-match, resume-match-closed.
func RegisterMatch(reg cli.Registry) {
	reg.Register(&auditCommand{})
	reg.Register(&resumeMatchCommand{})
	reg.Register(&resumeMatchClosedCommand{})
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

// ── audit ─────────────────────────────────────────────────────────────────────

type auditCommand struct{}

func (c *auditCommand) Name() string { return "audit" }

func (c *auditCommand) Run(ctx *cli.Context, _ []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam audit: nil context")
	}
	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--all", "--json"); err != nil {
		// bd error: treat like no issues (match bash `|| true` behavior).
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

// ── resume-match ──────────────────────────────────────────────────────────────

type resumeMatchCommand struct{}

func (c *resumeMatchCommand) Name() string { return "resume-match" }

func (c *resumeMatchCommand) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam resume-match: nil context")
	}
	if len(args) == 0 || args[0] == "" {
		return cli.Usagef("ateam resume-match: missing <worktree-path>")
	}
	path := args[0]

	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--status=open", "--json"); err != nil {
		// bd error: print nothing and exit 0 (bash swallows errors).
		return nil
	}

	if match := matchByWorktree(issues, path); match != nil {
		fmt.Fprintln(ctx.Stdout, match.ID)
	}
	return nil
}

// ── resume-match-closed ───────────────────────────────────────────────────────

type resumeMatchClosedCommand struct{}

func (c *resumeMatchClosedCommand) Name() string { return "resume-match-closed" }

func (c *resumeMatchClosedCommand) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam resume-match-closed: nil context")
	}
	if len(args) == 0 || args[0] == "" {
		return cli.Usagef("ateam resume-match-closed: missing <worktree-path>")
	}
	path := args[0]

	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--status=closed", "--json"); err != nil {
		// bd error: print nothing and exit 0.
		return nil
	}

	matches := matchAllByWorktree(issues, path)
	if len(matches) > 0 {
		fmt.Fprintln(ctx.Stdout, matches[0].ID)
	}
	return nil
}
