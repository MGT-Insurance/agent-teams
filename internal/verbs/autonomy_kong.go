// autonomy_kong.go — the `ateam autonomy grant|revoke|show` verb (rm8 T7,
// agent-teams-lrj.8). Contract: docs/outsider-requests.md section 9.
package verbs

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
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
	ID    string `arg:"" name:"id" help:"Initiative ID."`
	Gates string `name:"gates" help:"Comma list of gate classes: plan, scope, design-fork, live-test, delivery."`
	Until string `name:"until" help:"RFC3339 time or a duration (e.g. 8h); at most 72h from now."`
	Ref   string `name:"ref" help:"Source ref of Eric's own message granting this."`
}
type autonomyRevokeKong struct {
	ID  string `arg:"" name:"id" help:"Initiative ID."`
	Ref string `name:"ref" help:"Source ref of Eric's own message revoking this."`
}
type autonomyShowKong struct {
	ID string `arg:"" name:"id" help:"Initiative ID."`
}

// autonomyCallerCheck is the code-enforced trust rule: grant and revoke refuse
// inside any Dispatcher run (DSP_BEAD set) and for any peer:* or non-steward
// agent:* BEADS_ACTOR. A human local shell (empty or other actor) and
// agent:steward pass.
func autonomyCallerCheck(verb string) error {
	if os.Getenv("DSP_BEAD") != "" {
		return cli.Usagef("ateam autonomy %s: refused inside a Dispatcher run (DSP_BEAD is set); only the steward or a human shell may write autonomy", verb)
	}
	actor := os.Getenv("BEADS_ACTOR")
	if strings.HasPrefix(actor, "peer:") || (strings.HasPrefix(actor, "agent:") && actor != "agent:steward") {
		return cli.Usagef("ateam autonomy %s: refused for caller %q; only agent:steward or a human shell may write autonomy", verb, actor)
	}
	return nil
}

// parseAutonomyUntil accepts an RFC3339 time or a Go duration and returns the
// UTC deadline, enforcing the future and the 72h cap.
func parseAutonomyUntil(v string, now time.Time) (time.Time, error) {
	var until time.Time
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		until = t.UTC()
	} else if d, derr := time.ParseDuration(v); derr == nil {
		until = now.Add(d).UTC()
	} else {
		return time.Time{}, cli.Usagef("ateam autonomy grant: --until %q is neither an RFC3339 time nor a duration", v)
	}
	until = until.Truncate(time.Second)
	if !until.After(now) {
		return time.Time{}, cli.Usagef("ateam autonomy grant: --until %s is not in the future", until.Format(time.RFC3339))
	}
	if until.After(now.Add(initiative.MaxAutonomyWindow)) {
		return time.Time{}, cli.Usagef("ateam autonomy grant: --until exceeds the %s maximum", initiative.MaxAutonomyWindow)
	}
	return until, nil
}

func writeAutonomyLine(ctx *cli.Context, id, value, note string) error {
	iss, err := bd.ShowIssue(ctx.BD, id)
	if err != nil {
		return err
	}
	plan, err := initiative.WithAutonomy(iss, value)
	if err != nil {
		return err
	}
	if plan.Description != iss.Description {
		f, err := os.CreateTemp("", "ateam-autonomy-*.txt")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err := f.WriteString(plan.Description); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		if _, err := ctx.BD.Run("update", id, "--body-file="+f.Name()); err != nil {
			return fmt.Errorf("bd update %s: %w", id, err)
		}
	}
	if _, err := ctx.BD.Run("update", id, "--append-notes", note); err != nil {
		return fmt.Errorf("bd update %s: %w", id, err)
	}
	return nil
}

func (c *autonomyGrantKong) Run(ctx *cli.Context) error {
	if err := autonomyCallerCheck("grant"); err != nil {
		return err
	}
	if c.Gates == "" || c.Until == "" || c.Ref == "" {
		return cli.Usagef("ateam autonomy grant: --gates, --until and --ref are required")
	}
	var gates []string
	for _, g := range strings.Split(c.Gates, ",") {
		if g = strings.TrimSpace(g); g != "" && !slices.Contains(gates, g) {
			gates = append(gates, g)
		}
	}
	if len(gates) == 0 {
		return cli.Usagef("ateam autonomy grant: --gates needs at least one class")
	}
	if strings.TrimSpace(c.Ref) == "" || strings.ContainsAny(c.Ref, "\r\n") {
		return cli.Usagef("ateam autonomy grant: --ref must be a non-empty single line")
	}
	until, err := parseAutonomyUntil(c.Until, autonomyNow().UTC())
	if err != nil {
		return err
	}
	value := fmt.Sprintf("gates=%s; until=%s; by=eric; via=steward; ref=%s", strings.Join(gates, ","), until.Format(time.RFC3339), strings.TrimSpace(c.Ref))
	if err := writeAutonomyLine(ctx, c.ID, value, "autonomy granted "+value); err != nil {
		return err
	}
	fmt.Fprintf(ctx.Stdout, "autonomy granted on %s: %s\n", c.ID, value)
	return nil
}

func (c *autonomyRevokeKong) Run(ctx *cli.Context) error {
	if err := autonomyCallerCheck("revoke"); err != nil {
		return err
	}
	if strings.TrimSpace(c.Ref) == "" || strings.ContainsAny(c.Ref, "\r\n") {
		return cli.Usagef("ateam autonomy revoke: --ref must be a non-empty single line")
	}
	value := fmt.Sprintf("revoked %s; ref=%s", autonomyNow().UTC().Truncate(time.Second).Format(time.RFC3339), strings.TrimSpace(c.Ref))
	if err := writeAutonomyLine(ctx, c.ID, value, "autonomy revoked "+value); err != nil {
		return err
	}
	fmt.Fprintf(ctx.Stdout, "autonomy revoked on %s: %s\n", c.ID, value)
	return nil
}

func (c *autonomyShowKong) Run(ctx *cli.Context) error {
	iss, err := bd.ShowIssue(ctx.BD, c.ID)
	if err != nil {
		return err
	}
	f := initiative.Of(iss)
	if f.Autonomy == "" {
		fmt.Fprintf(ctx.Stdout, "autonomy: none (%s)\n", c.ID)
		return nil
	}
	fmt.Fprintf(ctx.Stdout, "autonomy: %s\n", f.Autonomy)
	if gates, ref, ok := initiative.ActiveAutonomy(f, autonomyNow().UTC()); ok {
		fmt.Fprintf(ctx.Stdout, "active: yes (gates=%s ref=%s)\n", strings.Join(gates, ","), ref)
	} else {
		fmt.Fprintln(ctx.Stdout, "active: no")
	}
	return nil
}
