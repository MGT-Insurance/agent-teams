package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
)

type runtimeCmd struct {
	Check runtimeCheckKong `cmd:"" help:"Check whether a runtime is available."`
	Open  runtimeOpenKong  `cmd:"" help:"Open a runtime's native session view."`
}

type runtimeCheckKong struct {
	Runtime  string `arg:"" name:"runtime" help:"Runtime to inspect (codex)."`
	Optional bool   `name:"optional" help:"Report absent or incompatible optional runtimes as warnings without failing."`
	JSON     bool   `name:"json" help:"Emit the compatibility report as JSON."`

	executable string `kong:"-"`
}

type runtimeOpenKong struct {
	Runtime string `arg:"" name:"runtime" help:"Runtime whose native session view to open (claude or codex)."`

	executable  string                `kong:"-"`
	ensureCodex ensureCodexDaemonFunc `kong:"-"`
	openNative  nativeRuntimeOpenFunc `kong:"-"`
}

type ensureCodexDaemonFunc func(context.Context, string) (sessionruntime.ManagedDaemonInfo, error)
type nativeRuntimeOpenFunc func(*cli.Context, string, ...string) error

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

func (c *runtimeOpenKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam runtime open: nil context")
	}
	kind, err := sessionruntime.ParseKind(c.Runtime)
	if err != nil {
		return cli.Usagef("ateam runtime open: %v", err)
	}
	openNative := c.openNative
	if openNative == nil {
		openNative = defaultNativeRuntimeOpen
	}

	switch kind {
	case sessionruntime.Claude:
		return openNative(ctx, "claude", "agents", "--permission-mode", "bypassPermissions")
	case sessionruntime.Codex:
		report := sessionruntime.CheckCodexCompatibility(context.Background(), c.executable)
		if report.State != sessionruntime.CodexCompatible {
			if report.State == sessionruntime.CodexAbsent {
				return fmt.Errorf("ateam runtime open codex: Codex is not installed; run agent-teams-codex:setup-agent-teams")
			}
			return fmt.Errorf("ateam runtime open codex: incompatible Codex installation at %s: %s; run agent-teams-codex:setup-agent-teams", report.Executable, report.Detail)
		}
		ensureCodex := c.ensureCodex
		if ensureCodex == nil {
			ensureCodex = sessionruntime.EnsureManagedCodexDaemon
		}
		info, err := ensureCodex(context.Background(), report.Executable)
		if err != nil {
			return fmt.Errorf("ateam runtime open codex: %w", err)
		}
		return openNative(ctx, info.ManagedCodexPath,
			"resume",
			"--remote", "unix://"+info.SocketPath,
			"--all",
			"--include-non-interactive",
			"--sandbox", "danger-full-access",
			"--ask-for-approval", "never",
		)
	default:
		return cli.Usagef("ateam runtime open: unsupported runtime %q", kind)
	}
}

func defaultNativeRuntimeOpen(ctx *cli.Context, executable string, args ...string) error {
	cmd := exec.Command(executable, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = ctx.Stdout
	cmd.Stderr = ctx.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ateam runtime open: %s: %w", executable, err)
	}
	return nil
}
