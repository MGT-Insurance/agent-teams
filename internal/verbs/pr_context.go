// pr_context.go — `ateam pr context <initiative>`. STUB owned by rm8 T6
// (agent-teams-lrj.7); C0 (agent-teams-lrj.1) only front-loads the seam.
// The output is frozen byte-for-byte in docs/outsider-requests.md ("Multi-PR
// context block").
package verbs

import (
	"fmt"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// prContextKong implements `ateam pr context <initiative-id>`.
type prContextKong struct {
	InitiativeID string `arg:"" name:"initiative-id" help:"Initiative ID whose PR list the context block is built from."`
}

// Run is a STUB (T6).
func (c *prContextKong) Run(ctx *cli.Context) error {
	return fmt.Errorf("not implemented (agent-teams-lrj.7)")
}
