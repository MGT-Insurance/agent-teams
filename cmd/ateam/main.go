// Command ateam is the agent-teams workspace-access CLI.
// It replaces the plugins/agent-teams/scripts/ateam bash script.
//
// Usage: ateam <verb> [args…]
package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/verbs"
	"github.com/mgt-insurance/agent-teams/internal/workspace"

	// Register available transports (self-register via init()).
	_ "github.com/mgt-insurance/agent-teams/internal/transport/telegram"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	stderr := os.Stderr
	stdout := os.Stdout

	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}

	// Empty verb → usage error.
	if verb == "" {
		fmt.Fprintln(stderr, "ateam: verb required")
		fmt.Fprint(stderr, cli.UsageText)
		return 2
	}

	home := workspace.Home()

	// Guard: bd must be in PATH before any verb (mirrors bash line 17).
	if _, err := exec.LookPath("bd"); err != nil {
		fmt.Fprintln(stderr, "ateam: 'bd' not found in PATH")
		return 3
	}

	// ws is special-cased: it prints home and exits 0 regardless of whether
	// the workspace is initialized (mirrors bash lines 19-23).
	if verb == "ws" {
		fmt.Fprintln(stdout, home)
		return 0
	}

	// All other verbs require an initialized workspace.
	if !workspace.Initialized(home) {
		fmt.Fprintf(stderr, "ateam: workspace not initialized — run /setup-agent-teams (expected: %s/.beads)\n", home)
		return 4
	}

	// Build registry from all four track registration functions.
	// Explicit wiring — no init() side effects.
	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	verbs.RegisterMatch(reg)
	// Register notify first so gate can call it in-process via a registry lookup.
	verbs.RegisterNotify(reg)
	notifyCmd, _ := reg.Lookup("notify")
	verbs.RegisterWrite(reg, func(ctx *cli.Context, id, file string) error {
		return notifyCmd.Run(ctx, []string{id, "--file", file})
	})
	verbs.RegisterDispatch(reg)
	verbs.RegisterCost(reg)
	verbs.RegisterWorktreeSetup(reg)
	verbs.RegisterMessaging(reg)
	verbs.RegisterRouteEvent(reg)

	cmd, ok := reg.Lookup(verb)
	if !ok {
		fmt.Fprintf(stderr, "ateam: unknown verb '%s'\n", verb)
		fmt.Fprint(stderr, cli.UsageText)
		return 2
	}

	bdClient := bd.NewClient(home)
	ctx := &cli.Context{
		Home:   home,
		BD:     bdClient,
		Stdout: stdout,
		Stderr: stderr,
	}

	err := cmd.Run(ctx, args[1:])
	if err != nil {
		// SilentError: the verb already wrote its output; don't re-print.
		if _, silent := err.(*cli.SilentError); !silent {
			fmt.Fprintln(stderr, err.Error())
		}
		return cli.ExitCode(err)
	}
	return 0
}
