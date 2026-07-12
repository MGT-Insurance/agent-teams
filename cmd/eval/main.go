// Command eval drives the agent-teams eval harness: it dispatches DRI runs
// under different agent-teams configurations and collects cost/correctness
// metrics for comparison in Langfuse. See internal/eval and bead
// agent-teams-grft.1 for the frozen contract this implements.
//
// Usage:
//
//	eval run --task <path> --config <name>            dispatch a DRI run under a frozen preset, print its RunID
//	eval run --task <path> --model <m> [--advisor <a>] dispatch a DRI run under an ad hoc model/advisor pair
//	eval collect <RunID>                               assemble metrics+judge, push to Langfuse if configured
//	eval push <RunID>                                  push a previously collected result to Langfuse
//	eval clean <RunID>                                 remove the run's leftover fixture worktree/branch
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/eval"
)

// configs is the tiny v1 config registry (agent-teams-grft.1 CONFIG AXIS):
// --config <name> selects one of the two frozen v1 ConfigFingerprints.
// Hardcoding these two here is acceptable per grft.7's WHAT (stub-permitted);
// reserved R4 fields (PerRoleModels/PromptVariantHash/PluginRef) stay zero.
var configs = map[string]eval.ConfigFingerprint{
	"opus-noadvisor": {Name: "opus-noadvisor", DRIModel: "opus", Advisor: ""},
	"sonnet-advisor": {Name: "sonnet-advisor", DRIModel: "sonnet", Advisor: "opus"},
}

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
		return runCmd(args[1:])
	case "collect":
		return collectCmd(args[1:])
	case "push":
		return pushCmd(args[1:])
	case "clean":
		return cleanCmd(args[1:])
	default:
		printUsage(os.Stderr)
		return 1
	}
}

func runCmd(args []string) int {
	fs := flag.NewFlagSet("eval run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	taskPath := fs.String("task", "", "path to a TaskSpec JSON file")
	configName := fs.String("config", "", "frozen config preset ("+configNames()+"); mutually exclusive with --model/--advisor")
	model := fs.String("model", "", "DRI model (e.g. opus, sonnet, fable); mutually exclusive with --config")
	advisor := fs.String("advisor", "", "advisor model, default off; requires --model; mutually exclusive with --config")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *taskPath == "" {
		fmt.Fprintln(os.Stderr, "eval run: --task is required")
		return 1
	}
	cfg, err := resolveConfig(*configName, *model, *advisor)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval run:", err)
		return 1
	}

	task, err := eval.LoadTaskSpec(*taskPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval run:", err)
		return 1
	}
	manifest, err := eval.Run(task, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval run:", err)
		return 1
	}
	fmt.Println(manifest.RunID)
	return 0
}

// resolveConfig picks the ConfigFingerprint for `eval run` from either the
// frozen --config presets or the --model/--advisor axis flags — exactly one
// form is allowed. Pure and side-effect-free so it's testable without
// dispatching anything.
//
// Axis-flag Name is derived deterministically: "<model>-noadvisor" with
// advisor off, "<model>-advisor:<advisor>" with it on. ConfigFingerprint.Hash()
// already gives identity for Langfuse; model strings are passed through
// as-is, not checked against an allowlist (dispatch owns that validation).
func resolveConfig(configName, model, advisor string) (eval.ConfigFingerprint, error) {
	usingConfig := configName != ""
	usingAxes := model != "" || advisor != ""

	switch {
	case usingConfig && usingAxes:
		return eval.ConfigFingerprint{}, fmt.Errorf("--config and --model/--advisor are mutually exclusive")
	case !usingConfig && !usingAxes:
		return eval.ConfigFingerprint{}, fmt.Errorf("one of --config or --model is required")
	case usingConfig:
		cfg, ok := configs[configName]
		if !ok {
			return eval.ConfigFingerprint{}, fmt.Errorf("unknown --config %q (known: %s)", configName, configNames())
		}
		return cfg, nil
	case model == "":
		return eval.ConfigFingerprint{}, fmt.Errorf("--advisor requires --model")
	default:
		name := model + "-noadvisor"
		if advisor != "" {
			name = model + "-advisor:" + advisor
		}
		return eval.ConfigFingerprint{Name: name, DRIModel: model, Advisor: advisor}, nil
	}
}

func collectCmd(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: eval collect <RunID>")
		return 1
	}
	result, pushed, err := eval.Collect(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval collect:", err)
		return 1
	}
	printSummary(result)
	if !pushed {
		fmt.Println("push skipped (no LANGFUSE_HOST set)")
	}
	return 0
}

func pushCmd(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: eval push <RunID>")
		return 1
	}
	runID := args[0]
	result, err := eval.LoadResult(runID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval push:", err)
		return 1
	}
	task, err := eval.LoadTaskSpec(eval.TaskSpecPath(result.TaskID))
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval push:", err)
		return 1
	}
	if err := eval.Push(result, task); err != nil {
		fmt.Fprintln(os.Stderr, "eval push:", err)
		return 1
	}
	fmt.Println("pushed:", runID)
	return 0
}

func cleanCmd(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: eval clean <RunID>")
		return 1
	}
	if err := eval.Clean(args[0]); err != nil {
		fmt.Fprintln(os.Stderr, "eval clean:", err)
		return 1
	}
	fmt.Println("cleaned:", args[0])
	return 0
}

func printSummary(res eval.RunResult) {
	fmt.Printf("RunID:             %s\n", res.RunID)
	fmt.Printf("Task:              %s\n", res.TaskID)
	fmt.Printf("Config:            %s (%s)\n", res.Config.Name, res.Config.Hash())
	fmt.Printf("Cost (USD):        %.4f\n", res.Metrics.CostUSD)
	fmt.Printf("Total tokens:      %d\n", res.Metrics.TotalTokens)
	fmt.Printf("Wall clock (s):    %.1f\n", res.Metrics.WallClockSeconds)
	fmt.Printf("Tool calls:        %d\n", res.Metrics.ToolCallCount)
	fmt.Printf("Turns:             %d\n", res.Metrics.NTurns)
	fmt.Printf("Objective floor:   %v\n", res.Judge.ObjectiveFloorPass)
	fmt.Printf("Correctness score: %.2f\n", res.Judge.CorrectnessScore)
}

func configNames() string {
	names := make([]string, 0, len(configs))
	for n := range configs {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func printUsage(w *os.File) {
	fmt.Fprintln(w, "Usage: eval run --task <path> --config <name>")
	fmt.Fprintln(w, "       eval run --task <path> --model <m> [--advisor <a>]")
	fmt.Fprintln(w, "       eval collect <RunID>")
	fmt.Fprintln(w, "       eval push <RunID>")
	fmt.Fprintln(w, "       eval clean <RunID>")
}
