// This file is owned by Track C (write verbs).
package verbs

import (
	"fmt"
	"os"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// RegisterWrite registers the write verbs:
// register, note, gate, clear-gate, learn, close, reopen, sync.
func RegisterWrite(reg cli.Registry) {
	reg.Register(&registerCmd{})
	reg.Register(&noteCmd{})
	reg.Register(&gateCmd{})
	reg.Register(&clearGateCmd{})
	reg.Register(&learnCmd{})
	reg.Register(&closeCmd{})
	reg.Register(&reopenCmd{})
	reg.Register(&syncCmd{})
}

// parseFlag parses a single --flag value or --flag=value token from args[i].
// If it matches the flag prefix, it returns the value and how many tokens were consumed.
// Returns ("", 0) if the token does not match.
func parseFlag(args []string, i int, flag string) (value string, consumed int) {
	arg := args[i]
	eqForm := flag + "="
	if strings.HasPrefix(arg, eqForm) {
		return arg[len(eqForm):], 1
	}
	if arg == flag && i+1 < len(args) {
		return args[i+1], 2
	}
	return "", 0
}

// ── register ─────────────────────────────────────────────────────────────────

type registerCmd struct{}

func (c *registerCmd) Name() string { return "register" }

func (c *registerCmd) Run(ctx *cli.Context, args []string) error {
	title, file, err := parseRegisterFlags(args)
	if err != nil {
		return err
	}
	if title == "" {
		return cli.Usagef("ateam register: --title required")
	}
	if file == "" {
		return cli.Usagef("ateam register: --file required")
	}
	if _, statErr := os.Stat(file); statErr != nil {
		return cli.Usagef("ateam register: file not found: %s", file)
	}
	var issue bd.Issue
	if err := ctx.BD.RunJSON(&issue, "create", "--title="+title, "--type=task", "--priority=2", "--body-file="+file, "--json"); err != nil {
		return err
	}
	if issue.ID == "" {
		return cli.Depf("ateam register: bd create returned no id (does this bd support --json on create?)")
	}
	fmt.Fprintln(ctx.Stdout, issue.ID)
	return nil
}

// parseRegisterFlags parses --title and --file from args.
func parseRegisterFlags(args []string) (title, file string, err error) {
	for i := 0; i < len(args); {
		if v, n := parseFlag(args, i, "--title"); n > 0 {
			title = v
			i += n
			continue
		}
		if v, n := parseFlag(args, i, "--file"); n > 0 {
			file = v
			i += n
			continue
		}
		return "", "", cli.Usagef("ateam register: unknown flag %q", args[i])
	}
	return title, file, nil
}

// ── note ─────────────────────────────────────────────────────────────────────

type noteCmd struct{}

func (c *noteCmd) Name() string { return "note" }

func (c *noteCmd) Run(ctx *cli.Context, args []string) error {
	id, file, err := parseIDFileFlags("note", args)
	if err != nil {
		return err
	}
	out, runErr := ctx.BD.Run("note", id, "--file="+file)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return runErr
}

// ── gate ─────────────────────────────────────────────────────────────────────

// gateAsk holds the parsed structured-ask fields for ateam gate.
type gateAsk struct {
	decision       string
	recommendation string
	alternative    string
	contextFile    string
}

type gateCmd struct{}

func (c *gateCmd) Name() string { return "gate" }

func (c *gateCmd) Run(ctx *cli.Context, args []string) error {
	id, file, kind, ask, err := parseGateFlags(args)
	if err != nil {
		return err
	}

	noteFile := file
	if ask != nil {
		// Structured form: build the sentinel block and write to a temp file.
		block, buildErr := buildAskBlock(ask)
		if buildErr != nil {
			return buildErr
		}
		tmp, tmpErr := os.CreateTemp("", "ateam-gate-ask-*")
		if tmpErr != nil {
			return fmt.Errorf("ateam gate: create temp file: %w", tmpErr)
		}
		tmpPath := tmp.Name()
		if _, writeErr := tmp.WriteString(block); writeErr != nil {
			tmp.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("ateam gate: write temp file: %w", writeErr)
		}
		tmp.Close()
		defer os.Remove(tmpPath)
		noteFile = tmpPath
	}

	out, runErr := ctx.BD.Run("note", id, "--file="+noteFile)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	if runErr != nil {
		return runErr
	}
	out, runErr = ctx.BD.Run("label", "add", id, "human")
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	if runErr != nil {
		return runErr
	}
	out, runErr = ctx.BD.Run("label", "add", id, "gate:"+kind)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return runErr
}

// buildAskBlock serializes a gateAsk into the sentinel-delimited format from
// contract j9s section 2. The context field may be empty; all other fields are
// expected to be pre-validated by parseGateFlags.
func buildAskBlock(ask *gateAsk) (string, error) {
	var b strings.Builder
	b.WriteString("<<<ateam-ask\n")
	b.WriteString("decision: " + ask.decision + "\n")
	b.WriteString("recommendation: " + ask.recommendation + "\n")
	b.WriteString("alternative: " + ask.alternative + "\n")
	if ask.contextFile != "" {
		data, err := os.ReadFile(ask.contextFile)
		if err != nil {
			return "", cli.Usagef("ateam gate: context-file not found: %s", ask.contextFile)
		}
		ctx := strings.TrimRight(string(data), "\n")
		if len(ctx) > 280 {
			return "", cli.Usagef("ateam gate: --context-file content exceeds 280 chars (got %d)", len(ctx))
		}
		b.WriteString("context: " + ctx + "\n")
	}
	b.WriteString(">>>")
	return b.String(), nil
}

// parseGateFlags parses <id> [--file <f>] [--kind=review|question]
// [--decision <d>] [--recommendation <r>] [--alternative <a>]
// [--context-file <f>] from args.
// kind defaults to "question" when omitted.
// Returns a non-nil ask when the structured form is used (--decision present).
// --file and the structured flags are mutually exclusive.
func parseGateFlags(args []string) (id, file, kind string, ask *gateAsk, err error) {
	if len(args) == 0 {
		return "", "", "", nil, cli.Usagef("ateam gate: missing <id>")
	}
	id = args[0]
	kind = "question"
	var parsed gateAsk
	for i := 1; i < len(args); {
		if v, n := parseFlag(args, i, "--file"); n > 0 {
			file = v
			i += n
			continue
		}
		if v, n := parseFlag(args, i, "--kind"); n > 0 {
			kind = v
			i += n
			continue
		}
		if v, n := parseFlag(args, i, "--decision"); n > 0 {
			parsed.decision = v
			i += n
			continue
		}
		if v, n := parseFlag(args, i, "--recommendation"); n > 0 {
			parsed.recommendation = v
			i += n
			continue
		}
		if v, n := parseFlag(args, i, "--alternative"); n > 0 {
			parsed.alternative = v
			i += n
			continue
		}
		if v, n := parseFlag(args, i, "--context-file"); n > 0 {
			parsed.contextFile = v
			i += n
			continue
		}
		return "", "", "", nil, cli.Usagef("ateam gate: unknown flag %q", args[i])
	}
	if kind != "review" && kind != "question" {
		return "", "", "", nil, cli.Usagef("ateam gate: --kind must be review or question")
	}

	structuredUsed := parsed.decision != "" || parsed.recommendation != "" ||
		parsed.alternative != "" || parsed.contextFile != ""

	if structuredUsed {
		// Structured form validation.
		if file != "" {
			return "", "", "", nil, cli.Usagef("ateam gate: --file and structured flags (--decision etc.) are mutually exclusive")
		}
		if parsed.decision == "" {
			return "", "", "", nil, cli.Usagef("ateam gate: --decision required when using structured form")
		}
		if len(parsed.decision) > 120 {
			return "", "", "", nil, cli.Usagef("ateam gate: --decision exceeds 120 chars (got %d)", len(parsed.decision))
		}
		return id, "", kind, &parsed, nil
	}

	// Prose / back-compat form.
	if file == "" {
		return "", "", "", nil, cli.Usagef("ateam gate: --file required")
	}
	if _, statErr := os.Stat(file); statErr != nil {
		return "", "", "", nil, cli.Usagef("ateam gate: file not found: %s", file)
	}
	return id, file, kind, nil, nil
}

// ── clear-gate ────────────────────────────────────────────────────────────────

type clearGateCmd struct{}

func (c *clearGateCmd) Name() string { return "clear-gate" }

func (c *clearGateCmd) Run(ctx *cli.Context, args []string) error {
	id, file, err := parseClearGateFlags(args)
	if err != nil {
		return err
	}
	if file != "" {
		if _, statErr := os.Stat(file); statErr != nil {
			return cli.Usagef("ateam clear-gate: file not found: %s", file)
		}
		out, runErr := ctx.BD.Run("comment", id, "--file="+file)
		if out != "" {
			fmt.Fprintln(ctx.Stdout, out)
		}
		if runErr != nil {
			return runErr
		}
	}
	out, runErr := ctx.BD.Run("label", "remove", id, "human")
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	if runErr != nil {
		return runErr
	}
	// Remove both gate:* labels. bd label remove is idempotent (exit 0 even
	// when the label is absent), so these succeed regardless of gate kind.
	out, runErr = ctx.BD.Run("label", "remove", id, "gate:review")
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	if runErr != nil {
		return runErr
	}
	out, runErr = ctx.BD.Run("label", "remove", id, "gate:question")
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return runErr
}

// parseClearGateFlags parses <id> [--file <f>] from args.
func parseClearGateFlags(args []string) (id, file string, err error) {
	if len(args) == 0 {
		return "", "", cli.Usagef("ateam clear-gate: missing <id>")
	}
	id = args[0]
	for i := 1; i < len(args); {
		if v, n := parseFlag(args, i, "--file"); n > 0 {
			file = v
			i += n
			continue
		}
		return "", "", cli.Usagef("ateam clear-gate: unknown flag %q", args[i])
	}
	return id, file, nil
}

// ── learn ─────────────────────────────────────────────────────────────────────

type learnCmd struct{}

func (c *learnCmd) Name() string { return "learn" }

func (c *learnCmd) Run(ctx *cli.Context, args []string) error {
	role, slug, file, err := parseLearnFlags(args)
	if err != nil {
		return err
	}
	data, readErr := os.ReadFile(file)
	if readErr != nil {
		return cli.Usagef("ateam learn: file not found: %s", file)
	}
	out, runErr := ctx.BD.Run("remember", "--key="+role+":"+slug, string(data))
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return runErr
}

// parseLearnFlags parses <role> <slug> --file <f> from args.
func parseLearnFlags(args []string) (role, slug, file string, err error) {
	if len(args) == 0 {
		return "", "", "", cli.Usagef("ateam learn: missing <role>")
	}
	role = args[0]
	if len(args) < 2 {
		return "", "", "", cli.Usagef("ateam learn: missing <slug>")
	}
	slug = args[1]
	for i := 2; i < len(args); {
		if v, n := parseFlag(args, i, "--file"); n > 0 {
			file = v
			i += n
			continue
		}
		return "", "", "", cli.Usagef("ateam learn: unknown flag %q", args[i])
	}
	if file == "" {
		return "", "", "", cli.Usagef("ateam learn: --file required")
	}
	return role, slug, file, nil
}

// ── close ─────────────────────────────────────────────────────────────────────

type closeCmd struct{}

func (c *closeCmd) Name() string { return "close" }

func (c *closeCmd) Run(ctx *cli.Context, args []string) error {
	id, reason, file, err := parseCloseFlags(args)
	if err != nil {
		return err
	}
	if file != "" {
		data, readErr := os.ReadFile(file)
		if readErr != nil {
			return cli.Usagef("ateam close: file not found: %s", file)
		}
		reason = string(data)
	}
	if reason != "" {
		out, runErr := ctx.BD.Run("close", id, "--reason="+reason)
		if out != "" {
			fmt.Fprintln(ctx.Stdout, out)
		}
		return runErr
	}
	out, runErr := ctx.BD.Run("close", id)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return runErr
}

// parseCloseFlags parses <id> [--reason <t>] [--file <f>] from args.
func parseCloseFlags(args []string) (id, reason, file string, err error) {
	if len(args) == 0 {
		return "", "", "", cli.Usagef("ateam close: missing <id>")
	}
	id = args[0]
	for i := 1; i < len(args); {
		if v, n := parseFlag(args, i, "--reason"); n > 0 {
			reason = v
			i += n
			continue
		}
		if v, n := parseFlag(args, i, "--file"); n > 0 {
			file = v
			i += n
			continue
		}
		return "", "", "", cli.Usagef("ateam close: unknown flag %q", args[i])
	}
	return id, reason, file, nil
}

// ── reopen ────────────────────────────────────────────────────────────────────

type reopenCmd struct{}

func (c *reopenCmd) Name() string { return "reopen" }

func (c *reopenCmd) Run(ctx *cli.Context, args []string) error {
	if len(args) == 0 {
		return cli.Usagef("ateam reopen: missing <id>")
	}
	out, err := ctx.BD.Run("reopen", args[0])
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return err
}

// ── sync ──────────────────────────────────────────────────────────────────────

type syncCmd struct{}

func (c *syncCmd) Name() string { return "sync" }

func (c *syncCmd) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return cli.Usagef("ateam sync: no context")
	}
	out, err := ctx.BD.Run("dolt", "push")
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return err
}

// ── shared helpers ────────────────────────────────────────────────────────────

// parseIDFileFlags parses <id> --file <f> from args for note.
func parseIDFileFlags(verb string, args []string) (id, file string, err error) {
	if len(args) == 0 {
		return "", "", cli.Usagef("ateam %s: missing <id>", verb)
	}
	id = args[0]
	for i := 1; i < len(args); {
		if v, n := parseFlag(args, i, "--file"); n > 0 {
			file = v
			i += n
			continue
		}
		return "", "", cli.Usagef("ateam %s: unknown flag %q", verb, args[i])
	}
	if file == "" {
		return "", "", cli.Usagef("ateam %s: --file required", verb)
	}
	if _, statErr := os.Stat(file); statErr != nil {
		return "", "", cli.Usagef("ateam %s: file not found: %s", verb, file)
	}
	return id, file, nil
}
