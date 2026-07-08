// Command eval drives the agent-teams eval harness: it dispatches DRI runs
// under different agent-teams configurations and collects cost/correctness
// metrics for comparison in Langfuse. See internal/eval and bead
// agent-teams-grft.1 for the frozen contract this implements.
//
// Usage: eval run|collect ...
package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printUsage(os.Stderr)
		return 1
	}

	switch args[0] {
	case "run":
		fmt.Fprintln(os.Stderr, "eval run: not implemented")
		return 1
	case "collect":
		fmt.Fprintln(os.Stderr, "eval collect: not implemented")
		return 1
	default:
		printUsage(os.Stderr)
		return 1
	}
}

func printUsage(w *os.File) {
	fmt.Fprintln(w, "Usage: eval run|collect ...")
}
