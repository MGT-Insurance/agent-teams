// This file is owned by Track C (write verbs).
package verbs

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// RegisterWrite registers the write verbs:
// register, note, gate, clear-gate, learn, close, reopen, pull, sync, forget, condense,
// fresh-drain, condense-lock.
func RegisterWrite(reg cli.Registry) {
	reg.Register(&registerCmd{})
	reg.Register(&noteCmd{})
	reg.Register(&gateCmd{})
	reg.Register(&clearGateCmd{})
	reg.Register(&learnCmd{})
	reg.Register(&closeCmd{})
	reg.Register(&reopenCmd{})
	reg.Register(&pullCmd{})
	reg.Register(&syncCmd{})
	reg.Register(&forgetCmd{})
	reg.Register(&condenseCmd{})
	reg.Register(&freshDrainCmd{})
	reg.Register(&condenseLockCmd{})
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
	key := learnKey(role, slug)
	out, runErr := ctx.BD.Run("remember", "--key="+key, string(data))
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return runErr
}

// learnKey computes the bd memory key for a learn invocation.
// Precedence:
//   - "cold:<slug>" → role:<slug> (bare cold key, no tier tag)
//   - "hot:<slug>" or "fresh:<slug>" → role:<slug> (passthrough)
//   - anything else → role:fresh:<slug> (default to fresh tier)
func learnKey(role, slug string) string {
	if strings.HasPrefix(slug, "cold:") {
		return role + ":" + slug[len("cold:"):]
	}
	if strings.HasPrefix(slug, "hot:") || strings.HasPrefix(slug, "fresh:") {
		return role + ":" + slug
	}
	return role + ":fresh:" + slug
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

// ── pull ──────────────────────────────────────────────────────────────────────

type pullCmd struct{}

func (c *pullCmd) Name() string { return "pull" }

func (c *pullCmd) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return cli.Usagef("ateam pull: no context")
	}
	out, err := ctx.BD.Run("dolt", "pull")
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
	if out, err := ctx.BD.Run("dolt", "pull"); err != nil {
		return err
	} else if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	out, err := ctx.BD.Run("dolt", "push")
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	if err == nil {
		return nil
	}
	// Bounded non-ff retry: if push fails with a non-fast-forward error (dolt
	// emits "non-fast-forward" in stderr, which bd.Client.Run appends to the
	// returned error), pull again to absorb the remote advance and retry the
	// push exactly once. Any other push error (e.g. auth) is returned immediately
	// without retry since the non-fast-forward substring won't match.
	if !strings.Contains(err.Error(), "non-fast-forward") {
		return err
	}
	if out, pullErr := ctx.BD.Run("dolt", "pull"); pullErr != nil {
		return pullErr
	} else if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	out, err = ctx.BD.Run("dolt", "push")
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return err
}

// ── forget ────────────────────────────────────────────────────────────────────

// forgetCmd removes a memory by key. The key is formed as <role>:<slug>.
// Callers pass slug as "hot:<name>" to target the hot-tier key <role>:hot:<name>.
// This serves both hot demotion cleanup and cold eviction.
type forgetCmd struct{}

func (c *forgetCmd) Name() string { return "forget" }

func (c *forgetCmd) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam forget: no context")
	}
	role, slug, err := parseForgetArgs(args)
	if err != nil {
		return err
	}
	key := role + ":" + slug
	out, runErr := ctx.BD.Run("forget", key)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return runErr
}

// parseForgetArgs parses <role> <slug> positional args.
func parseForgetArgs(args []string) (role, slug string, err error) {
	if len(args) == 0 || args[0] == "" {
		return "", "", cli.Usagef("ateam forget: missing <role>")
	}
	if len(args) < 2 || args[1] == "" {
		return "", "", cli.Usagef("ateam forget: missing <slug>")
	}
	return args[0], args[1], nil
}

// ── condense ──────────────────────────────────────────────────────────────────

// condenseBudgetTokens is the hot-tier token budget the condense agent targets.
const condenseBudgetTokens = 6000

// condenseInstructionContract is the instruction contract emitted to the
// consuming condense agent. The agent applies the result DIRECTLY and
// autonomously via ateam learn / ateam forget — no human review gate.
const condenseInstructionContract = `Condense the memories above into a hot tier for this role.

Rules:
- PROMOTE or REFRESH high-signal or repeatedly-learned items into hot (key: <role>:hot:<slug>) via: ateam learn <role> hot:<slug> --file <f>
- DEMOTE stale hot items down to cold by rewriting them at the cold key (role:<slug>) then deleting the hot key via: ateam learn <role> cold:<slug> --file <f>, then ateam forget <role> hot:<slug>
- Within cold: MERGE duplicates, REWRITE for brevity, and EVICT truly-dead items via: ateam learn <role> cold:<slug> --file <f> (for rewrites) or ateam forget <role> <slug> (for evictions)
- Target the hot budget (~6000 tokens, ~15-25 succinct learnings); keep each hot item succinct but complete
- Apply ALL changes AUTONOMOUSLY with no human review gate
- After applying, emit one line: "promoted N / merged M / evicted K / hot now X tokens"
- v1 has NO eviction floor — trust Dolt history for recoverability`

// condenseMemory is a single memory record in the condense packet.
type condenseMemory struct {
	Key  string `json:"key"`
	Body string `json:"body"`
}

// condensePacket is the full structured packet emitted to stdout.
type condensePacket struct {
	Role      string           `json:"role"`
	Memories  []condenseMemory `json:"memories"`
	HotBudget int              `json:"hot_budget_tokens"`
	Contract  string           `json:"instruction_contract"`
}

// condenseCmd emits a structured packet of all role: memories to stdout.
// It is DETERMINISTIC — no LLM call, no memory writes.
type condenseCmd struct{}

func (c *condenseCmd) Name() string { return "condense" }

func (c *condenseCmd) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam condense: no context")
	}
	if len(args) == 0 || args[0] == "" {
		return cli.Usagef("ateam condense: missing <role>")
	}
	role := args[0]
	prefix := role + ":"

	var raw map[string]any
	if err := ctx.BD.RunJSON(&raw, "memories", "--json"); err != nil {
		return err
	}

	// Collect all role: keys (hot and cold tiers) whose values are strings.
	// Invariant: callers must run `ateam fresh-drain <role>` before this verb
	// so that role:fresh:* keys have been moved to cold. The /agent-teams:condense
	// skill enforces this; a direct `ateam condense <role>` would include fresh
	// keys in the packet, mislabeled as cold in the agent's view.
	var keys []string
	for k, v := range raw {
		if strings.HasPrefix(k, prefix) {
			if _, ok := v.(string); ok {
				keys = append(keys, k)
			}
		}
	}

	// Sort for determinism.
	sort.Strings(keys)

	memories := make([]condenseMemory, 0, len(keys))
	for _, k := range keys {
		memories = append(memories, condenseMemory{Key: k, Body: raw[k].(string)})
	}

	packet := condensePacket{
		Role:      role,
		Memories:  memories,
		HotBudget: condenseBudgetTokens,
		Contract:  condenseInstructionContract,
	}

	enc := json.NewEncoder(ctx.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(packet)
}

// ── fresh-drain ───────────────────────────────────────────────────────────────

// freshDrainCmd moves every role:fresh:<slug> memory to role:<slug> (cold),
// overwriting any existing cold value (fresh = newer). It is deterministic,
// requires no LLM, and is idempotent (no fresh keys → clean no-op).
type freshDrainCmd struct{}

func (c *freshDrainCmd) Name() string { return "fresh-drain" }

func (c *freshDrainCmd) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam fresh-drain: no context")
	}
	if len(args) == 0 || args[0] == "" {
		return cli.Usagef("ateam fresh-drain: missing <role>")
	}
	role := args[0]
	freshPrefix := role + ":fresh:"

	var raw map[string]any
	if err := ctx.BD.RunJSON(&raw, "memories", "--json"); err != nil {
		return err
	}

	// Collect fresh keys, sorted for determinism.
	var freshKeys []string
	for k, v := range raw {
		if _, ok := v.(string); !ok {
			continue
		}
		if strings.HasPrefix(k, freshPrefix) {
			freshKeys = append(freshKeys, k)
		}
	}
	sort.Strings(freshKeys)

	for _, k := range freshKeys {
		slug := k[len(freshPrefix):]
		body := raw[k].(string)
		coldKey := role + ":" + slug

		if _, err := ctx.BD.Run("remember", "--key="+coldKey, body); err != nil {
			return err
		}
		if _, err := ctx.BD.Run("forget", k); err != nil {
			return err
		}
	}

	fmt.Fprintf(ctx.Stdout, "fresh-drain %s: drained %d\n", role, len(freshKeys))
	return nil
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
