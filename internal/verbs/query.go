// Package verbs contains per-track verb registration functions.
// This file is owned by Track A (read/query verbs).
package verbs

import (
	"fmt"

	"github.com/erlloyd/agent-teams/internal/cli"
)

// RegisterQuery registers the read/query verbs:
// ws, list, list-json, human-list, show, learnings.
//
// NOTE: ws is also special-cased in main before workspace initialization is
// checked; it is registered here for completeness and usage listing.
func RegisterQuery(reg cli.Registry) {
	reg.Register(&wsCmd{})
	reg.Register(&listCmd{})
	reg.Register(&listJSONCmd{})
	reg.Register(&humanListCmd{})
	reg.Register(&showCmd{})
	reg.Register(&learningsCmd{})
}

// wsCmd prints the workspace home path.
type wsCmd struct{}

func (c *wsCmd) Name() string { return "ws" }

func (c *wsCmd) Run(ctx *cli.Context, _ []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam ws: no context")
	}
	fmt.Fprintln(ctx.Stdout, ctx.Home)
	return nil
}

// listCmd passes through: bd list --status=open
type listCmd struct{}

func (c *listCmd) Name() string { return "list" }

func (c *listCmd) Run(ctx *cli.Context, _ []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam list: no context")
	}
	out, err := ctx.BD.Run("list", "--status=open")
	if err != nil {
		return err
	}
	fmt.Fprintln(ctx.Stdout, out)
	return nil
}

// listJSONCmd passes through: bd list --status=open --json
type listJSONCmd struct{}

func (c *listJSONCmd) Name() string { return "list-json" }

func (c *listJSONCmd) Run(ctx *cli.Context, _ []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam list-json: no context")
	}
	out, err := ctx.BD.Run("list", "--status=open", "--json")
	if err != nil {
		return err
	}
	fmt.Fprintln(ctx.Stdout, out)
	return nil
}

// humanListCmd passes through: bd human list
type humanListCmd struct{}

func (c *humanListCmd) Name() string { return "human-list" }

func (c *humanListCmd) Run(ctx *cli.Context, _ []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam human-list: no context")
	}
	out, err := ctx.BD.Run("human", "list")
	if err != nil {
		return err
	}
	fmt.Fprintln(ctx.Stdout, out)
	return nil
}

// showCmd passes through: bd show <id>
type showCmd struct{}

func (c *showCmd) Name() string { return "show" }

func (c *showCmd) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam show: no context")
	}
	if len(args) == 0 || args[0] == "" {
		return cli.Usagef("ateam show: missing <id>")
	}
	out, err := ctx.BD.Run("show", args[0])
	if err != nil {
		return err
	}
	fmt.Fprintln(ctx.Stdout, out)
	return nil
}

// learningsCmd passes through: bd memories <role>
type learningsCmd struct{}

func (c *learningsCmd) Name() string { return "learnings" }

func (c *learningsCmd) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam learnings: no context")
	}
	if len(args) == 0 || args[0] == "" {
		return cli.Usagef("ateam learnings: missing <role>")
	}
	out, err := ctx.BD.Run("memories", args[0])
	if err != nil {
		return err
	}
	fmt.Fprintln(ctx.Stdout, out)
	return nil
}
