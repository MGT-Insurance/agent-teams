// Command prompt-sync renders and checks generated agent-teams prompts.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mgt-insurance/agent-teams/internal/promptsync"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type patternsFlag []string

func (p *patternsFlag) String() string { return fmt.Sprint([]string(*p)) }
func (p *patternsFlag) Set(value string) error {
	*p = append(*p, value)
	return nil
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		printUsage(stdout)
		return 0
	}
	if args[0] != "check" && args[0] != "write" {
		fmt.Fprintf(stderr, "prompt-sync: unknown command %q\n", args[0])
		printUsage(stderr)
		return 2
	}

	command := args[0]
	flags := flag.NewFlagSet("prompt-sync "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	allowUnmigrated := flags.Bool("allow-unmigrated", false, "permit explicitly classified temporarily-unmigrated entries")
	var patterns patternsFlag
	flags.Var(&patterns, "manifest", "repository-relative manifest glob; repeat for split manifests")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "prompt-sync %s: unexpected arguments: %v\n", command, flags.Args())
		return 2
	}
	config := promptsync.Config{Root: *root, ManifestPatterns: patterns, AllowUnmigrated: *allowUnmigrated}
	var report promptsync.Report
	var err error
	if command == "check" {
		report, err = promptsync.Check(config)
	} else {
		report, err = promptsync.Write(config)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprint(stdout, promptsync.FormatReport(report))
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage: prompt-sync <command> [options]

Commands:
  check   render in memory and compare exact checked-in output bytes (read-only)
  write   render and atomically update only paired manifest outputs

Options:
  --root <dir>             repository root (default .)
  --manifest <glob>        relative manifest glob; repeat for split manifests
  --allow-unmigrated       permit temporary classifications during loop closure

Strict mode is the default: temporarily-unmigrated entries fail unless the
explicit migration flag is present.`)
}
