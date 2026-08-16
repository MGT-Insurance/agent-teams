package verbs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
)

type runtimeCmd struct {
	Check runtimeCheckKong `cmd:"" help:"Check whether a runtime is available."`
}

type runtimeCheckKong struct {
	Runtime  string `arg:"" name:"runtime" help:"Runtime to inspect (codex)."`
	Optional bool   `name:"optional" help:"Report absent or incompatible optional runtimes as warnings without failing."`
	JSON     bool   `name:"json" help:"Emit the compatibility report as JSON."`

	executable string `kong:"-"`
}

func RegisterRuntimeKong(p *cli.Parser) {
	p.AddVerb("runtime", "Inspect and control agent runtimes.", &runtimeCmd{})
}

func (c *runtimeCheckKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam runtime check: nil context")
	}
	if c.Runtime != "codex" {
		return cli.Usagef("ateam runtime check: unsupported runtime %q (supported: codex)", c.Runtime)
	}
	report := sessionruntime.CheckCodexCompatibility(context.Background(), c.executable)
	if c.JSON {
		raw, err := json.Marshal(report)
		if err != nil {
			return fmt.Errorf("ateam runtime check: encode report: %w", err)
		}
		fmt.Fprintln(ctx.Stdout, string(raw))
	} else {
		switch report.State {
		case sessionruntime.CodexCompatible:
			fmt.Fprintf(ctx.Stdout, "codex: compatible standalone installation (CLI %s, managed %s)\n", report.CLIVersion, report.ManagedVersion)
		case sessionruntime.CodexAbsent:
			fmt.Fprintln(ctx.Stderr, "codex: absent — install the official standalone Codex CLI to use --runtime codex")
		default:
			fmt.Fprintf(ctx.Stderr, "codex: incompatible installation at %s — %s; reinstall with the official standalone Codex installer\n", report.Executable, report.Detail)
		}
	}
	if report.State != sessionruntime.CodexCompatible && !c.Optional {
		return cli.Silent(1)
	}
	return nil
}
