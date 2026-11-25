package verbs

import (
	"fmt"
	"os"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/po"
)

// poCharterCheckRun implements `ateam po charter check <file>`: it prints OK
// plus a summary, or one "<rule>: <message>" line per violation, and exits 1
// on any violation.
func poCharterCheckRun(ctx *cli.Context, c *poCharterCheckKong) error {
	data, err := os.ReadFile(c.File)
	if err != nil {
		return fmt.Errorf("ateam po charter check: %w", err)
	}
	ch, viol := po.CheckCharter(data)
	if len(viol) == 0 {
		fmt.Fprintf(ctx.Stdout, "OK %s\n", po.Summary(ch))
		return nil
	}
	for _, v := range viol {
		fmt.Fprintf(ctx.Stdout, "%s: %s\n", v.Rule, v.Msg)
	}
	return cli.Silent(1)
}
