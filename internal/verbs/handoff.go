// handoff.go implements `ateam handoff`, owned by agent-teams-p9dm.23.
// Semantics are frozen by the at-jno7 CONTRACT track (external_review.go §3,
// §9) — this file only wires the verb; it does not redefine the label
// string, the transition, or the warning behavior.
package verbs

import (
	"fmt"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// handoffKong is the kong-converted form of handoff:
//
//	ateam handoff <id>            // declare: the human has looked; it's on the team
//	ateam handoff <id> --clear    // undo: the human has re-opened the question
//	ateam handoff <id> --pr <url> // scope the declaration to one PR (docs/multi-pr-contract.md §3)
type handoffKong struct {
	ID    string `arg:"" name:"id" help:"Initiative ID."`
	Clear bool   `name:"clear" help:"Undo the declaration; the question is the human's again."`

	// PR scopes the handoff to one PR: the emitted label becomes
	// "external-review:<pr>" instead of the bare, initiative-scoped
	// "external-review" (docs/multi-pr-contract.md §3). Omitted (legacy,
	// unchanged): bare form.
	PR string `name:"pr" help:"Full PR URL to scope the handoff to one PR; omitted uses the bare, initiative-scoped label (legacy)."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
//
// Adds or removes exactly one externalReviewLabel-based label (bare, or
// "<label>:<pr>" when --pr is given) via `bd label add|remove` — the same
// mechanism gateKong/clearGateKong already use for "human" and "gate:*"
// (kong_converted.go). No other label is touched: "human" and "gate:review"
// (or their per-PR forms) are deliberately left in place (external_review.go
// §2). `bd label add`/`remove` on an already-(ab)sent label is a no-op, so
// both directions are idempotent.
func (c *handoffKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam handoff: no context")
	}

	// Resolve --pr to its canonical form and require it to be one of the
	// initiative's ACTUAL resolved PRs (agent-teams-ssib.25) — the same
	// resolver gate/clear-gate use, so a handoff label always lines up
	// byte-for-byte with the review gate it's meant to pair with, whatever
	// spelling the caller typed.
	pr := c.PR
	if pr != "" {
		canon, err := resolvePR(ctx, "ateam handoff", c.ID, pr)
		if err != nil {
			return err
		}
		pr = canon
	}

	label := externalReviewLabel
	reviewLabel := "gate:review"
	if pr != "" {
		label += ":" + pr
		reviewLabel += ":" + pr
	}

	if c.Clear {
		out, err := ctx.BD.Run("label", "remove", c.ID, label)
		if out != "" {
			fmt.Fprintln(ctx.Stdout, out)
		}
		return err
	}

	// Read labels first so a missing gate:review (or its per-PR form) can be
	// warned about (external_review.go §3, §9's U/Q -> H row) without
	// blocking the declaration — Eric's fact is recorded either way. The
	// lookup feeds only the warning, and nothing else in the system can
	// reconstruct the declaration, so a bd failure degrades the warning
	// rather than dropping the fact (agent-teams-p9dm.42).
	issue, err := bd.ShowIssue(ctx.BD, c.ID)
	switch {
	case err != nil:
		fmt.Fprintf(ctx.Stderr, "ateam handoff: warning: could not read labels for %s (%v) — declaring anyway\n", c.ID, err)
	case !hasLabel(issue.Labels, reviewLabel):
		fmt.Fprintf(ctx.Stderr, "ateam handoff: warning: %s has no %s label — the reported status will not change until a review gate exists\n", c.ID, reviewLabel)
	}

	out, err := ctx.BD.Run("label", "add", c.ID, label)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return err
}
