// This file is owned by Track P (PR identity and routing, agent-teams-ssib.7).
// pr.go — `ateam pr add`, the sanctioned write path onto the "pr" rail
// (docs/multi-pr-contract.md §2b). Deliberately its own file, own
// RegisterPRKong, matching the established per-verb pattern (worktree_setup.go,
// spawncheck.go, preflight.go, ...) rather than growing kong_converted.go.
package verbs

import (
	"fmt"
	"os"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// RegisterPRKong registers the `pr` parent verb (currently just `pr add`) onto p.
func RegisterPRKong(p *cli.Parser) {
	p.AddVerb("pr", "Manage an initiative's recorded GitHub PR(s).", &prCmd{})
}

// prCmd is the kong parent struct for `ateam pr <subcommand>`. No Run method:
// kong runs every node with a Run method from the selected leaf up through its
// parents, so a Run here would fire on every subcommand (see mail_register.go).
type prCmd struct {
	Add prAddKong `cmd:"" name:"add" help:"Record a GitHub PR URL on an initiative's pr rail."`
}

// prAddKong implements `ateam pr add <initiative-id> <pr-url>` — the
// sanctioned write path onto the "pr" rail going forward
// (docs/multi-pr-contract.md §2b), via initiative.WithPR. Append-only and
// idempotent on a repeat URL: calling it again for a second, then a third PR
// is how those get recorded on one initiative.
type prAddKong struct {
	InitiativeID string `arg:"" name:"initiative-id" help:"Initiative ID to record the PR on."`
	URL          string `arg:"" name:"pr-url" help:"Full GitHub PR URL, e.g. https://github.com/owner/repo/pull/3."`
}

// Validate rejects a malformed pr-url before any bd read/write. WithPR itself
// only enforces the field-line rule's structural constraints (no
// leading/trailing whitespace, no line break) — it deliberately does not
// validate the GitHub-URL shape (docs/multi-pr-contract.md §2, "It
// deliberately does NOT validate that url matches prURLRE"), leaving that to
// a caller with a reason to reject a malformed value. This is that caller.
func (c *prAddKong) Validate() error {
	if c.InitiativeID == "" {
		return cli.Usagef("ateam pr add: initiative-id must not be empty")
	}
	if !initiative.PRURLRE.MatchString(c.URL) {
		return cli.Usagef("ateam pr add: pr-url must be a full GitHub PR URL (https://github.com/<owner>/<repo>/pull/<number>), got %q", c.URL)
	}
	return nil
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *prAddKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam pr add: no context")
	}

	issue, err := bd.ShowIssue(ctx.BD, c.InitiativeID)
	if err != nil {
		return fmt.Errorf("ateam pr add: bd show %s: %w", c.InitiativeID, err)
	}

	plan, err := initiative.WithPR(issue, c.URL)
	if err != nil {
		return fmt.Errorf("ateam pr add: %w", err)
	}
	if plan.Description == issue.Description {
		fmt.Fprintf(ctx.Stdout, "pr add: %s already recorded on %s\n", c.URL, c.InitiativeID)
		return nil
	}

	tmpFile, err := os.CreateTemp("", "ateam-pr-add-*.txt")
	if err != nil {
		return fmt.Errorf("ateam pr add: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpFile.WriteString(plan.Description); err != nil {
		tmpFile.Close()
		return fmt.Errorf("ateam pr add: write temp file: %w", err)
	}
	tmpFile.Close()

	if _, err := ctx.BD.Run("update", c.InitiativeID, "--body-file="+tmpPath); err != nil {
		return fmt.Errorf("ateam pr add: bd update %s: %w", c.InitiativeID, err)
	}
	fmt.Fprintf(ctx.Stdout, "pr add: recorded %s on %s\n", c.URL, c.InitiativeID)
	return nil
}
