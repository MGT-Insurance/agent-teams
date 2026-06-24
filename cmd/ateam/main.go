// Command ateam is the agent-teams workspace-access CLI.
// It replaces the plugins/agent-teams/scripts/ateam bash script.
//
// Usage: ateam <verb> [args…]
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/alecthomas/kong"
	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/verbs"
	"github.com/mgt-insurance/agent-teams/internal/workspace"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	stderr := os.Stderr
	stdout := os.Stdout

	// Empty args → print kong top-level help and exit 0.
	// Handle this before building the parser so uninitialized workspaces still
	// get help (requirement: help-before-init-guard).
	if len(args) == 0 || (len(args) == 1 && args[0] == "") {
		return runHelp(args, stdout, stderr)
	}

	// "help" as the first token → treat as --help (top-level help).
	if args[0] == "help" {
		return runHelp(nil, stdout, stderr)
	}

	home := workspace.Home()

	// ws is special-cased: prints home and exits 0 regardless of workspace
	// initialization (mirrors bash lines 19-23).
	if args[0] == "ws" {
		fmt.Fprintln(stdout, home)
		return 0
	}

	// Track whether kong's help machinery triggered (printed + suppressed exit).
	helpShown := false

	parser, err := cli.NewParser(
		kong.Writers(stdout, stderr),
		kong.Exit(func(code int) { helpShown = true }),
	)
	if err != nil {
		fmt.Fprintln(stderr, "ateam: internal error: "+err.Error())
		return 1
	}
	verbs.RegisterAllKong(parser)

	kctx, parseErr := parser.Parse(args)

	// Help was triggered (--help, -h, or a subcommand --help). Guards must NOT
	// run in this path — the workspace may not be initialized.
	if helpShown {
		return 0
	}

	// Kong parse error → usage problem, exit 2.
	if parseErr != nil {
		fmt.Fprintln(stderr, "ateam: "+parseErr.Error())
		return 2
	}

	// Guard: bd must be in PATH before any verb (mirrors bash line 17).
	if _, err := exec.LookPath("bd"); err != nil {
		fmt.Fprintln(stderr, "ateam: 'bd' not found in PATH")
		return 3
	}

	// All verbs except ws require an initialized workspace.
	if !workspace.Initialized(home) {
		fmt.Fprintf(stderr, "ateam: workspace not initialized — run /setup-agent-teams (expected: %s/.beads)\n", home)
		return 4
	}

	bdClient := bd.NewClient(home)
	cliCtx := &cli.Context{
		Home:   home,
		BD:     bdClient,
		Stdout: stdout,
		Stderr: stderr,
	}

	// Bind cliCtx so every verb's Run(*cli.Context) receives it.
	kctx.Bind(cliCtx)

	runErr := kctx.Run(cliCtx)
	if runErr != nil {
		// errors.As: kong wraps the verb's returned error, so a direct type
		// assertion would miss a *SilentError and double-print output the verb
		// already wrote.
		var silent *cli.SilentError
		if !errors.As(runErr, &silent) {
			fmt.Fprintln(stderr, runErr.Error())
		}
	}
	return cli.ExitCode(runErr)
}

// runHelp builds a parser, registers all verbs, prints top-level help, and
// returns 0. Called for: empty args, bare "help" verb.
// Does NOT run any workspace or bd guards — help must work pre-init.
func runHelp(args []string, stdout, stderr *os.File) int {
	parser, err := cli.NewParser(
		kong.Writers(stdout, stderr),
		kong.Exit(func(int) {}),
	)
	if err != nil {
		fmt.Fprintln(stderr, "ateam: internal error: "+err.Error())
		return 1
	}
	verbs.RegisterAllKong(parser)
	// Parse --help to trigger kong's built-in help printer.
	_, _ = parser.Parse([]string{"--help"})
	return 0
}
