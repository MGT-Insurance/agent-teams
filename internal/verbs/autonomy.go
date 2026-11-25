// autonomy.go — the `ateam clear-gate --autonomy` check and the helpers shared
// with `ateam autonomy` (autonomy_kong.go). Owned by rm8 T7 (agent-teams-lrj.8).
// Contract: docs/outsider-requests.md ("Autonomy").
package verbs

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// autonomyNow is the clock for grant/revoke/clear-gate; tests replace it.
var autonomyNow = time.Now

// outsiderAutonomyRef is the reserved --autonomy ref for an outsider
// initiative: it needs no grant line, only steward-approves.
const outsiderAutonomyRef = "outsider"

// gateClassLabelPrefix is the label T2 writes on an open gate.
const gateClassLabelPrefix = "gate-class:"

// clearGateAutonomyCheck verifies that --autonomy <ref> is honored for the open
// gate before clear-gate proceeds. Called by clearGateKong.Run only when
// --autonomy is given. It succeeds only when:
//   - ActiveAutonomy is true for that exact ref and every gate-class label on the
//     initiative is in the granted classes; or
//   - ref is "outsider", the initiative is outsider, and every gate class is in
//     steward-approves.
//
// On success it appends the note "cleared under autonomy ref=<ref> class=<class>".
// It does not check the caller: the DRI runs it inside a Dispatcher run.
func clearGateAutonomyCheck(ctx *cli.Context, c *clearGateKong) error {
	iss, err := bd.ShowIssue(ctx.BD, c.ID)
	if err != nil {
		return err
	}
	f := initiative.Of(iss)
	var classes []string
	for _, l := range iss.Labels {
		if cl, ok := strings.CutPrefix(l, gateClassLabelPrefix); ok && cl != "" {
			classes = append(classes, cl)
		}
	}
	if len(classes) == 0 {
		return cli.Usagef("ateam clear-gate --autonomy: %s has no open gate with a gate-class label; nothing to clear under autonomy", c.ID)
	}
	var allowed []string
	if c.Autonomy == outsiderAutonomyRef {
		if f.Origin != initiative.OriginOutsider {
			return cli.Usagef("ateam clear-gate --autonomy outsider: %s is not an outsider initiative", c.ID)
		}
		allowed = f.StewardApproves
	} else {
		gates, ref, ok := initiative.ActiveAutonomy(f, autonomyNow().UTC())
		if !ok {
			return cli.Usagef("ateam clear-gate --autonomy %s: %s has no active autonomy grant (missing, revoked, expired, or malformed)", c.Autonomy, c.ID)
		}
		if ref != c.Autonomy {
			return cli.Usagef("ateam clear-gate --autonomy %s: the active grant on %s has a different ref", c.Autonomy, c.ID)
		}
		allowed = gates
	}
	for _, cl := range classes {
		if !slices.Contains(allowed, cl) {
			return cli.Usagef("ateam clear-gate --autonomy %s: gate class %q is not covered on %s (covered: %s)", c.Autonomy, cl, c.ID, strings.Join(allowed, ","))
		}
	}
	note := fmt.Sprintf("cleared under autonomy ref=%s class=%s", c.Autonomy, strings.Join(classes, ","))
	if _, err := ctx.BD.Run("update", c.ID, "--append-notes", note); err != nil {
		return fmt.Errorf("ateam clear-gate --autonomy: record note: %w", err)
	}
	return nil
}
