// kong_converted.go holds the 3 representative verbs converted to kong structs
// for the LOOP bead (agent-teams-f738). This file is owned by the LOOP track;
// enh tracks that convert additional verbs in their respective files must NOT
// re-convert reopen, register, or cost (they live here now).
package verbs

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/cost"
)

// ── reopen (trivial positional) ───────────────────────────────────────────────

// reopenKong is the kong-converted form of reopen. Takes a single positional <id>.
type reopenKong struct {
	ID string `arg:"" name:"id" help:"Initiative ID to reopen."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *reopenKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam reopen: no context")
	}
	out, err := ctx.BD.Run("reopen", c.ID)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return err
}

// ── register (mid-flags) ──────────────────────────────────────────────────────

// registerKong is the kong-converted form of register.
// Takes --title and --file flags.
type registerKong struct {
	Title string `name:"title" help:"Initiative title (required)." required:""`
	File  string `name:"file"  help:"Path to body file (required)."  required:""`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *registerKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam register: no context")
	}
	if _, err := os.Stat(c.File); err != nil {
		return cli.Usagef("ateam register: file not found: %s", c.File)
	}
	var issue bd.Issue
	if err := ctx.BD.RunJSON(&issue, "create",
		"--title="+c.Title,
		"--type=task",
		"--priority=2",
		"--body-file="+c.File,
		"--json",
	); err != nil {
		return err
	}
	if issue.ID == "" {
		return cli.Depf("ateam register: bd create returned no id (does this bd support --json on create?)")
	}
	fmt.Fprintln(ctx.Stdout, issue.ID)
	return nil
}

// ── cost (positional + flag) ──────────────────────────────────────────────────

// costKong is the kong-converted form of cost.
// Collapses the manual flag.FlagSet pre-scan; kong handles flag/positional ordering.
type costKong struct {
	ID   string `arg:"" name:"initiative-id" help:"Initiative ID to report cost for."`
	JSON bool   `name:"json" help:"Output JSON instead of a table."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *costKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam cost: no context")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("ateam cost: %w", err)
	}
	jobsDir := home + "/.claude/jobs"
	projectsDir := home + "/.claude/projects"

	report, err := cost.Attribute(c.ID, jobsDir, projectsDir)
	if err != nil {
		return fmt.Errorf("ateam cost: %w", err)
	}

	if c.JSON {
		return renderJSONKong(ctx, report)
	}
	return renderTableKong(ctx, report)
}

// renderJSONKong and renderTableKong delegate to the same internal helpers used
// by the legacy costCmd in cost.go (buildJSONReport).
func renderJSONKong(ctx *cli.Context, r cost.Report) error {
	out := buildJSONReport(r)
	enc := json.NewEncoder(ctx.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func renderTableKong(ctx *cli.Context, r cost.Report) error {
	return renderTable(ctx, r)
}

// ── registration helpers ──────────────────────────────────────────────────────

// RegisterWriteKong registers the write-track verbs onto p. reopen and register
// use native kong structs; condense-lock is registered natively via
// RegisterCondenseLock (lock.go); the remaining write verbs are bridged.
// cost is NOT registered here — it lives in RegisterCostKong (cost.go).
func RegisterWriteKong(p *cli.Parser) {
	// Native kong verbs (write-track converted verbs).
	p.AddVerb("reopen", "Reopen a closed initiative.", &reopenKong{})
	p.AddVerb("register", "Register a new initiative from a body file.", &registerKong{})

	// condense-lock is now natively converted; register it via its own file.
	RegisterCondenseLock(p)

	// Bridge the remaining write verbs (legacy cli.Command, passthrough args).
	for _, cmd := range legacyWriteVerbs() {
		p.AddBridgeVerb(cmd)
	}
}

// legacyWriteVerbs returns all write-track cli.Commands EXCEPT the natively
// converted ones (reopen, register, condense-lock) so they can be bridged onto
// the kong parser. cost is excluded because it lives in a separate file (cost.go)
// and is registered independently via RegisterCostKong.
func legacyWriteVerbs() []cli.Command {
	reg := make(cli.Registry)
	RegisterWrite(reg)
	skip := map[string]bool{"reopen": true, "register": true, "condense-lock": true}
	out := make([]cli.Command, 0, len(reg)-len(skip))
	for name, cmd := range reg {
		if !skip[name] {
			out = append(out, cmd)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// RegisterAllKong is the FROZEN dispatcher called by main.go. It delegates to
// per-track RegisterXKong functions — one per existing RegisterX file. This
// function must never be edited again after the loop closes; ring-track
// conversion beads edit ONLY their own RegisterXKong inside their own file.
func RegisterAllKong(p *cli.Parser) {
	RegisterWriteKong(p)         // write.go + kong_converted.go (3 native + bridges)
	RegisterCostKong(p)          // cost.go  (native kong struct)
	RegisterQueryKong(p)         // query.go
	RegisterMatchKong(p)         // match.go
	RegisterDispatchKong(p)      // dispatch.go
	RegisterWorktreeSetupKong(p) // worktree_setup.go
	RegisterMessagingKong(p)     // messaging.go
	RegisterRouteEventKong(p)    // route.go
	RegisterStatusKong(p)        // status.go
	RegisterWatchersKong(p)      // watchers.go
}

// bridgeTrack registers all commands from registerFn as bridge verbs on p.
// Called by per-track RegisterXKong functions while verbs are still unconverted.
func bridgeTrack(p *cli.Parser, registerFn func(cli.Registry)) {
	reg := make(cli.Registry)
	registerFn(reg)
	cmds := make([]cli.Command, 0, len(reg))
	for _, cmd := range reg {
		cmds = append(cmds, cmd)
	}
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name() < cmds[j].Name() })
	for _, cmd := range cmds {
		p.AddBridgeVerb(cmd)
	}
}
