package verbs

import (
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// RegisterPoKong registers the `po` parent verb (product-owner tooling).
// Frozen contract: internal/po/doc.go. Each leaf Run delegates to a function
// in its own file (po_charter_run.go, po_state_run.go, po_startup_run.go).
func RegisterPoKong(p *cli.Parser) {
	p.AddVerb("po", "Product-owner (PO) tooling: charter check, per-PO state, startup.", &poCmd{})
}

// poCmd is the group node; no Run method (see stewardCmd).
type poCmd struct {
	Charter poCharterCmd  `cmd:"" name:"charter" help:"PO charter tooling."`
	State   poStateCmd    `cmd:"" name:"state" help:"Per-PO state in the hosted store."`
	Startup poStartupKong `cmd:"" name:"startup" help:"Print the PO's startup text: charter, view, claims, ledger stats, learnings."`
}

type poCharterCmd struct {
	Check poCharterCheckKong `cmd:"" name:"check" help:"Parse and validate a PO charter file."`
}

type poStateCmd struct {
	Get  poStateGetKong  `cmd:"" name:"get" help:"Print the value at one per-PO state key."`
	Set  poStateSetKong  `cmd:"" name:"set" help:"Write the value at one per-PO state key."`
	List poStateListKong `cmd:"" name:"list" help:"List per-PO state keys, optionally by prefix."`
}

// poCharterCheckKong is `ateam po charter check <file>`.
type poCharterCheckKong struct {
	File string `arg:"" name:"file" help:"Path to the charter markdown file."`
}

func (c *poCharterCheckKong) Run(ctx *cli.Context) error { return poCharterCheckRun(ctx, c) }

// poStateGetKong is `ateam po state get <area> <key>`.
type poStateGetKong struct {
	Area string `arg:"" name:"area" help:"PO area (kebab-case)."`
	Key  string `arg:"" name:"key" help:"Key suffix after po:<area>: (e.g. view, claim:<id>)."`
}

func (c *poStateGetKong) Run(ctx *cli.Context) error { return poStateGetRun(ctx, c) }

// poStateSetKong is `ateam po state set <area> <key> [--file F | --value V]`.
type poStateSetKong struct {
	Area  string `arg:"" name:"area" help:"PO area (kebab-case)."`
	Key   string `arg:"" name:"key" help:"Key suffix after po:<area>: (e.g. view, claim:<id>)."`
	File  string `name:"file" help:"Read the value from this file."`
	Value string `name:"value" help:"The value, inline."`
}

func (c *poStateSetKong) Run(ctx *cli.Context) error { return poStateSetRun(ctx, c) }

// poStateListKong is `ateam po state list <area> [--prefix P]`.
type poStateListKong struct {
	Area   string `arg:"" name:"area" help:"PO area (kebab-case)."`
	Prefix string `name:"prefix" help:"Only keys whose suffix after po:<area>: starts with this."`
}

func (c *poStateListKong) Run(ctx *cli.Context) error { return poStateListRun(ctx, c) }

// poStartupKong is `ateam po startup <area> [--charter F]`.
type poStartupKong struct {
	Area    string `arg:"" name:"area" help:"PO area (kebab-case)."`
	Charter string `name:"charter" help:"Charter file path (default users/eric.lloyd/po-charters/<area>.md)."`
}

func (c *poStartupKong) Run(ctx *cli.Context) error { return poStartupRun(ctx, c) }
