// autonomy_kong.go — the `ateam autonomy grant|revoke|show` verb. STUB owned by
// rm8 T7 (agent-teams-lrj.8). C0 only registers the parent verb and the three
// subcommands so --help shows them and each errors with its bead id.
package verbs

import (
	"fmt"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// RegisterAutonomyKong registers the `autonomy` parent verb onto p.
func RegisterAutonomyKong(p *cli.Parser) {
	p.AddVerb("autonomy", "Grant, revoke or show an initiative's time-boxed steward autonomy (docs/outsider-requests.md).", &autonomyCmd{})
}

// autonomyCmd has no Run (see prCmd: a parent Run would fire on every leaf).
type autonomyCmd struct {
	Grant  autonomyGrantKong  `cmd:"" name:"grant" help:"Grant autonomy (steward only, on Eric's own message)."`
	Revoke autonomyRevokeKong `cmd:"" name:"revoke" help:"Revoke autonomy (steward only)."`
	Show   autonomyShowKong   `cmd:"" name:"show" help:"Show an initiative's autonomy line and whether it is active."`
}

type autonomyGrantKong struct {
	ID string `arg:"" name:"id" help:"Initiative ID."`
}
type autonomyRevokeKong struct {
	ID string `arg:"" name:"id" help:"Initiative ID."`
}
type autonomyShowKong struct {
	ID string `arg:"" name:"id" help:"Initiative ID."`
}

func errAutonomyNotImplemented() error { return fmt.Errorf("not implemented (agent-teams-lrj.8)") }

func (c *autonomyGrantKong) Run(ctx *cli.Context) error  { return errAutonomyNotImplemented() }
func (c *autonomyRevokeKong) Run(ctx *cli.Context) error { return errAutonomyNotImplemented() }
func (c *autonomyShowKong) Run(ctx *cli.Context) error   { return errAutonomyNotImplemented() }
