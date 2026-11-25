// gate_class.go — `ateam gate --class` and the clear-gate class cleanup.
// STUB owned by rm8 T2 (agent-teams-lrj.3); C0 (agent-teams-lrj.1) only
// front-loads the seams so T2 touches this file alone. Contract:
// docs/outsider-requests.md ("Gate class").
package verbs

import (
	"fmt"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// errGateClassNotImplemented is returned by every T2 stub until it lands.
var errGateClassNotImplemented = fmt.Errorf("not implemented (agent-teams-lrj.3)")

// gateClassApply handles `ateam gate --class <class>`: it writes the
// "gate-class:<class>" label and, for an outsider or active-autonomy
// initiative only, the "class: <class>" mail line. Called by gateKong.Run
// only when --class is given. STUB (T2).
func gateClassApply(ctx *cli.Context, c *gateKong) error {
	return errGateClassNotImplemented
}

// gateClassClear removes the gate-class label(s) when a gate is cleared.
// Called by clearGateKong.Run on every clear. No-op until T2.
func gateClassClear(ctx *cli.Context, c *clearGateKong) error {
	return nil
}
