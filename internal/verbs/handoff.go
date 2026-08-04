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
//	ateam handoff <id>            // declare: Eric has looked; it's on the team
//	ateam handoff <id> --clear    // undo: Eric has re-opened the question
type handoffKong struct {
	ID    string `arg:"" name:"id" help:"Initiative ID."`
	Clear bool   `name:"clear" help:"Undo the declaration; the question is the human's again."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
//
// Adds or removes exactly externalReviewLabel via `bd label add|remove` —
// the same mechanism gateKong/clearGateKong already use for "human" and
// "gate:*" (kong_converted.go). No other label is touched: "human" and
// "gate:review" are deliberately left in place (external_review.go §2).
// `bd label add`/`remove` on an already-(ab)sent label is a no-op, so both
// directions are idempotent.
func (c *handoffKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam handoff: no context")
	}

	if c.Clear {
		out, err := ctx.BD.Run("label", "remove", c.ID, externalReviewLabel)
		if out != "" {
			fmt.Fprintln(ctx.Stdout, out)
		}
		return err
	}

	// Read labels first so a missing gate:review can be warned about
	// (external_review.go §3, §9's U/Q -> H row) without blocking the
	// declaration — Eric's fact is recorded either way. The lookup feeds only
	// the warning, and nothing else in the system can reconstruct the
	// declaration, so a bd failure degrades the warning rather than dropping
	// the fact (agent-teams-p9dm.42).
	issue, err := bd.ShowIssue(ctx.BD, c.ID)
	switch {
	case err != nil:
		fmt.Fprintf(ctx.Stderr, "ateam handoff: warning: could not read labels for %s (%v) — declaring anyway\n", c.ID, err)
	case !hasLabel(issue.Labels, "gate:review"):
		fmt.Fprintf(ctx.Stderr, "ateam handoff: warning: %s has no gate:review label — the reported status will not change until a review gate exists\n", c.ID)
	}

	out, err := ctx.BD.Run("label", "add", c.ID, externalReviewLabel)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return err
}
