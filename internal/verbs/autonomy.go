// autonomy.go — `ateam clear-gate --autonomy` check. STUB owned by rm8 T7
// (agent-teams-lrj.8); C0 (agent-teams-lrj.1) only front-loads the seam.
// Contract: docs/outsider-requests.md ("Autonomy").
package verbs

import (
	"fmt"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// clearGateAutonomyCheck verifies that --autonomy <ref> matches an active
// autonomy grant covering the gate's class before clear-gate proceeds. Called
// by clearGateKong.Run only when --autonomy is given. STUB (T7).
func clearGateAutonomyCheck(ctx *cli.Context, c *clearGateKong) error {
	return fmt.Errorf("not implemented (agent-teams-lrj.8)")
}
