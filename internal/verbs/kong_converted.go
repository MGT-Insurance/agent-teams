// kong_converted.go holds the verbs converted to native kong structs.
// LOOP bead (agent-teams-f738): reopen, register, cost.
// rtix bead (agent-teams-rtix): note, gate, clear-gate, learn, close,
//
//	pull, sync, forget, condense, fresh-drain.
//
// Ownership rule: enh tracks that convert additional verbs in their respective
// files must NOT re-convert any verb that already lives here.
package verbs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/cost"
)

// ── reopen (trivial positional) ───────────────────────────────────────────────

// reopenKong is the kong-converted form of reopen. Takes a single positional <id>.
type reopenKong struct {
	ID string `arg:"" name:"id" help:"Initiative ID to reopen."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *reopenKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam reopen: no context")
	}
	out, err := ctx.BD.Run("reopen", c.ID)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return err
}

// ── register (mid-flags) ──────────────────────────────────────────────────────

// registerKong is the kong-converted form of register.
// Takes --title and --file flags.
type registerKong struct {
	Title string `name:"title" help:"Initiative title (required)." required:""`
	File  string `name:"file"  help:"Path to body file (required)."  required:""`

	// createEpic is injected at registration time so tests can substitute a
	// fake without calling a real bd binary. If nil, epic creation is skipped
	// (fail-soft default for tests that don't inject it).
	createEpic epicCreatorFunc `kong:"-"`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *registerKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam register: no context")
	}
	if _, err := os.Stat(c.File); err != nil {
		return cli.Usagef("ateam register: file not found: %s", c.File)
	}

	// Try to create a root epic in the project repo and append its id to the
	// body. appendEpicToBody returns the original file path on any failure
	// (fail-soft) or a temp file path + cleanup when it succeeds.
	bodyFile := c.File
	if c.createEpic != nil {
		if modPath, cleanup := appendEpicToBody(ctx, c.File, c.Title, c.createEpic); cleanup != nil {
			bodyFile = modPath
			defer cleanup()
		}
	}

	var issue bd.Issue
	if err := ctx.BD.RunJSON(&issue, "create",
		"--title="+c.Title,
		"--type=task",
		"--priority=2",
		"--body-file="+bodyFile,
		"--json",
	); err != nil {
		return err
	}
	if issue.ID == "" {
		return cli.Depf("ateam register: bd create returned no id (does this bd support --json on create?)")
	}
	fmt.Fprintln(ctx.Stdout, issue.ID)
	return nil
}

// ── cost (positional + flag) ──────────────────────────────────────────────────

// costKong is the kong-converted form of cost.
// Collapses the manual flag.FlagSet pre-scan; kong handles flag/positional ordering.
type costKong struct {
	ID   string `arg:"" name:"initiative-id" help:"Initiative ID to report cost for."`
	JSON bool   `name:"json" help:"Output JSON instead of a table."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *costKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam cost: no context")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("ateam cost: %w", err)
	}
	jobsDir := home + "/.claude/jobs"
	projectsDir := home + "/.claude/projects"

	report, err := cost.Attribute(c.ID, jobsDir, projectsDir)
	if err != nil {
		return fmt.Errorf("ateam cost: %w", err)
	}

	if c.JSON {
		return renderJSONKong(ctx, report)
	}
	return renderTableKong(ctx, report)
}

// renderJSONKong and renderTableKong delegate to the same internal helpers used
// by the legacy costCmd in cost.go (buildJSONReport).
func renderJSONKong(ctx *cli.Context, r cost.Report) error {
	return renderJSON(ctx, r)
}

func renderTableKong(ctx *cli.Context, r cost.Report) error {
	return renderTable(ctx, r)
}

// ── note ─────────────────────────────────────────────────────────────────────

// noteKong is the kong-converted form of note.
// Takes a positional <id> and a required --file flag.
type noteKong struct {
	ID   string `arg:"" name:"id"   help:"Initiative ID."`
	File string `name:"file"        help:"Path to note file (required)." required:""`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *noteKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam note: no context")
	}
	if _, err := os.Stat(c.File); err != nil {
		return cli.Usagef("ateam note: file not found: %s", c.File)
	}
	out, err := ctx.BD.Run("note", c.ID, "--file="+c.File)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return err
}

// ── gate ─────────────────────────────────────────────────────────────────────

// gateKong is the kong-converted form of gate.
// Two mutually-exclusive entry paths: prose (--file) vs structured
// (--decision + optional companions). xor:"gateform" is placed only on
// --file and --decision so they conflict with each other; --recommendation,
// --alternative, and --context-file combine freely alongside --decision.
//   - Prose: --file <path>
//   - Structured: --decision <text> [--recommendation <text>]
//     [--alternative <text>] [--context-file <path>]
//
// Length constraints (--decision ≤120, --context-file content ≤280) and the
// "at least one form required" invariant are enforced in Validate.
type gateKong struct {
	ID string `arg:"" name:"id" help:"Initiative ID."`

	// Prose form.
	File string `name:"file" xor:"gateform" help:"Path to prose note file (mutually exclusive with --decision)."`

	// Structured form flags. Only --decision carries xor:"gateform" so --file
	// and --decision remain mutually exclusive while the remaining flags
	// combine freely alongside --decision.
	Decision       string `name:"decision"       xor:"gateform" help:"Decision question (≤120 chars, required in structured form)."`
	Recommendation string `name:"recommendation"                help:"Recommended answer."`
	Alternative    string `name:"alternative"                   help:"Alternative answer."`
	ContextFile    string `name:"context-file"                  help:"Path to optional context file (content ≤280 chars)."`

	// Kind applies to both forms.
	Kind string `name:"kind" enum:"review,question" default:"question" help:"Gate kind: review or question."`
}

// Validate enforces constraints not expressible as tags:
//   - If structured flags are used: --decision required; --decision ≤120 chars; context-file content ≤280 chars.
//   - If neither form is provided: --file required error.
//   - Prose form: file must exist.
func (c *gateKong) Validate(_ *kong.Context) error {
	structuredUsed := c.Decision != "" || c.Recommendation != "" ||
		c.Alternative != "" || c.ContextFile != ""

	if structuredUsed {
		if c.Decision == "" {
			return cli.Usagef("ateam gate: --decision required when using structured form")
		}
		if len(c.Decision) > 120 {
			return cli.Usagef("ateam gate: --decision exceeds 120 chars (got %d)", len(c.Decision))
		}
		if c.ContextFile != "" {
			data, err := os.ReadFile(c.ContextFile)
			if err != nil {
				return cli.Usagef("ateam gate: context-file not found: %s", c.ContextFile)
			}
			// TrimRight mirrors buildAskBlock behaviour.
			if trimmed := len(strings.TrimRight(string(data), "\n")); trimmed > 280 {
				return cli.Usagef("ateam gate: --context-file content exceeds 280 chars (got %d)", trimmed)
			}
		}
		return nil
	}

	// Prose form: --file must be supplied.
	if c.File == "" {
		return cli.Usagef("ateam gate: --file required")
	}
	if _, err := os.Stat(c.File); err != nil {
		return cli.Usagef("ateam gate: file not found: %s", c.File)
	}
	return nil
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *gateKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam gate: no context")
	}

	noteFile := c.File
	structuredUsed := c.Decision != "" || c.Recommendation != "" ||
		c.Alternative != "" || c.ContextFile != ""

	if structuredUsed {
		ask := &gateAsk{
			decision:       c.Decision,
			recommendation: c.Recommendation,
			alternative:    c.Alternative,
			contextFile:    c.ContextFile,
		}
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

	out, runErr := ctx.BD.Run("note", c.ID, "--file="+noteFile)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	if runErr != nil {
		return runErr
	}
	out, runErr = ctx.BD.Run("label", "add", c.ID, "human")
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	if runErr != nil {
		return runErr
	}
	out, runErr = ctx.BD.Run("label", "add", c.ID, "gate:"+c.Kind)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return runErr
}

// ── clear-gate ────────────────────────────────────────────────────────────────

// clearGateKong is the kong-converted form of clear-gate.
// Takes a positional <id> and an optional --file flag.
type clearGateKong struct {
	ID   string `arg:"" name:"id"  help:"Initiative ID."`
	File string `name:"file"       help:"Path to response file (optional)."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *clearGateKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam clear-gate: no context")
	}
	if c.File != "" {
		if _, err := os.Stat(c.File); err != nil {
			return cli.Usagef("ateam clear-gate: file not found: %s", c.File)
		}
		out, err := ctx.BD.Run("comment", c.ID, "--file="+c.File)
		if out != "" {
			fmt.Fprintln(ctx.Stdout, out)
		}
		if err != nil {
			return err
		}
	}
	out, err := ctx.BD.Run("label", "remove", c.ID, "human")
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	if err != nil {
		return err
	}
	out, err = ctx.BD.Run("label", "remove", c.ID, "gate:review")
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	if err != nil {
		return err
	}
	out, err = ctx.BD.Run("label", "remove", c.ID, "gate:question")
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return err
}

// ── learn ─────────────────────────────────────────────────────────────────────

// learnKong is the kong-converted form of learn.
// Takes positional <role> and <slug>, and a required --file flag.
type learnKong struct {
	Role string `arg:"" name:"role" help:"Role name (e.g. planner, implementer)."`
	Slug string `arg:"" name:"slug" help:"Memory slug; prefix with hot:, fresh:, or cold: to target a tier."`
	File string `name:"file" help:"Path to file containing memory content (required)." required:""`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *learnKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam learn: no context")
	}
	data, err := os.ReadFile(c.File)
	if err != nil {
		return cli.Usagef("ateam learn: file not found: %s", c.File)
	}
	key := learnKey(c.Role, c.Slug)
	out, runErr := ctx.BD.Run("remember", "--key="+key, string(data))
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return runErr
}

// ── close ─────────────────────────────────────────────────────────────────────

// closeKong is the kong-converted form of close.
// --file takes precedence over --reason when both are provided (preserved from
// legacy parseCloseFlags behaviour). Validation: no additional constraints.
type closeKong struct {
	ID     string `arg:"" name:"id"  help:"Initiative ID."`
	Reason string `name:"reason"     help:"Close reason text."`
	File   string `name:"file"       help:"Path to file containing close reason (takes precedence over --reason)."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *closeKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam close: no context")
	}
	reason := c.Reason
	if c.File != "" {
		data, err := os.ReadFile(c.File)
		if err != nil {
			return cli.Usagef("ateam close: file not found: %s", c.File)
		}
		reason = string(data)
	}
	if reason != "" {
		out, err := ctx.BD.Run("close", c.ID, "--reason="+reason)
		if out != "" {
			fmt.Fprintln(ctx.Stdout, out)
		}
		return err
	}
	out, err := ctx.BD.Run("close", c.ID)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return err
}

// ── pull ──────────────────────────────────────────────────────────────────────

// pullKong is the kong-converted form of pull. No arguments.
type pullKong struct{}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *pullKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam pull: no context")
	}
	out, err := ctx.BD.Run("dolt", "pull")
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return err
}

// ── sync ──────────────────────────────────────────────────────────────────────

// syncKong is the kong-converted form of sync. No arguments.
type syncKong struct{}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *syncKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return cli.Usagef("ateam sync: no context")
	}
	// Commit the working set FIRST. `bd dolt pull` refuses a dirty working set
	// (the events audit table dirties on every bd write), so an uncommitted WS
	// would deadlock the pull ("local changes would be stomped by merge"). A
	// clean WS yields "nothing to commit" — that is a no-op, not a failure; any
	// other commit error aborts before we touch the remote.
	if out, err := ctx.BD.Run("dolt", "commit"); err != nil {
		if !strings.Contains(strings.ToLower(out+" "+err.Error()), "nothing to commit") {
			return err
		}
		if out != "" {
			fmt.Fprintln(ctx.Stdout, out)
		}
	} else if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
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
	// Bounded non-ff retry: pull to absorb the remote advance, then retry push once.
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

// forgetKong is the kong-converted form of forget.
// Takes positional <role> and <slug>; key is formed as role:slug.
type forgetKong struct {
	Role string `arg:"" name:"role" help:"Role name."`
	Slug string `arg:"" name:"slug" help:"Memory slug (e.g. hot:name targets role:hot:name)."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *forgetKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam forget: no context")
	}
	key := c.Role + ":" + c.Slug
	out, err := ctx.BD.Run("forget", key)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return err
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

// condenseKong is the kong-converted form of condense.
// Takes a positional <role>.
type condenseKong struct {
	Role string `arg:"" name:"role" help:"Role to condense memories for."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *condenseKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam condense: no context")
	}
	prefix := c.Role + ":"

	var raw map[string]any
	if err := ctx.BD.RunJSON(&raw, "memories", "--json"); err != nil {
		return err
	}

	var keys []string
	for k, v := range raw {
		if strings.HasPrefix(k, prefix) {
			if _, ok := v.(string); ok {
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)

	memories := make([]condenseMemory, 0, len(keys))
	for _, k := range keys {
		memories = append(memories, condenseMemory{Key: k, Body: raw[k].(string)})
	}

	packet := condensePacket{
		Role:      c.Role,
		Memories:  memories,
		HotBudget: condenseBudgetTokens,
		Contract:  condenseInstructionContract,
	}

	enc := json.NewEncoder(ctx.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(packet)
}

// ── fresh-drain ───────────────────────────────────────────────────────────────

// freshDrainKong is the kong-converted form of fresh-drain.
// Takes a positional <role>.
type freshDrainKong struct {
	Role string `arg:"" name:"role" required:"" help:"Role whose fresh: memories to drain to cold."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *freshDrainKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam fresh-drain: no context")
	}
	freshPrefix := c.Role + ":fresh:"

	var raw map[string]any
	if err := ctx.BD.RunJSON(&raw, "memories", "--json"); err != nil {
		return err
	}

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
		coldKey := c.Role + ":" + slug

		if _, err := ctx.BD.Run("remember", "--key="+coldKey, body); err != nil {
			return err
		}
		if _, err := ctx.BD.Run("forget", k); err != nil {
			return err
		}
	}

	fmt.Fprintf(ctx.Stdout, "fresh-drain %s: drained %d\n", c.Role, len(freshKeys))
	return nil
}

// ── epic creation helpers ─────────────────────────────────────────────────────

// epicCreatorFunc is the function type for creating a root epic bead in a
// project repo. Injected into registerKong and dispatchKong so tests can
// substitute a fake without calling a real bd binary.
type epicCreatorFunc func(repoPath, title string) (string, error)

// createEpicInRepo creates a root epic bead in the project repo at repoPath
// and returns its id. It uses exec.Command("bd", "-C", repoPath, ...) directly
// so it targets the PROJECT repo rather than the global workspace (ctx.BD
// always targets the global workspace). Returns ("", err) on failure.
func createEpicInRepo(repoPath, title string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("bd", "-C", repoPath, "create",
		"--type=epic",
		"--title="+title,
		"--priority=2",
		"--json",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimRight(stderr.String(), "\n")
		if msg != "" {
			return "", fmt.Errorf("bd create epic: %w\n%s", err, msg)
		}
		return "", fmt.Errorf("bd create epic: %w", err)
	}
	var issue bd.Issue
	if err := json.Unmarshal(stdout.Bytes(), &issue); err != nil {
		return "", fmt.Errorf("bd create epic: unmarshal: %w (raw: %.200s)", err, stdout.String())
	}
	if issue.ID == "" {
		return "", fmt.Errorf("bd create epic: returned no id")
	}
	return issue.ID, nil
}

// extractRepoPath scans body for the first "repo: <path>" line and returns the
// path. Falls back to the first "worktree: <path>" line when no repo: line is
// present. Returns "" if neither is found.
func extractRepoPath(body string) string {
	var worktree string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "repo: ") {
			v := strings.TrimRight(strings.TrimPrefix(line, "repo: "), " \t\r")
			if v != "" {
				return v
			}
		}
		if worktree == "" && strings.HasPrefix(line, "worktree: ") {
			worktree = strings.TrimRight(strings.TrimPrefix(line, "worktree: "), " \t\r")
		}
	}
	return worktree
}

// appendEpicToBody reads the file at originalPath, extracts the project repo
// path from the body, creates a root epic via creator, and returns a new temp
// file path with "epic: <id>" appended plus a cleanup function to remove it.
// Returns ("", nil) on any failure so callers fall back to the original file.
func appendEpicToBody(ctx *cli.Context, originalPath, title string, creator epicCreatorFunc) (string, func()) {
	bodyBytes, err := os.ReadFile(originalPath)
	if err != nil {
		return "", nil
	}
	bodyStr := string(bodyBytes)
	repoPath := extractRepoPath(bodyStr)
	if repoPath == "" {
		return "", nil
	}
	epicID, epicErr := creator(repoPath, title)
	if epicErr != nil {
		fmt.Fprintf(ctx.Stderr, "ateam register: warning: could not create root epic (fail-soft): %v\n", epicErr)
		return "", nil
	}
	if epicID == "" {
		return "", nil
	}
	modified := strings.TrimRight(bodyStr, "\n") + "\nepic: " + epicID + "\n"
	tmp, err := os.CreateTemp("", "ateam-register-*")
	if err != nil {
		return "", nil
	}
	if _, err := tmp.WriteString(modified); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", nil
	}
	tmp.Close()
	tmpPath := tmp.Name()
	return tmpPath, func() { os.Remove(tmpPath) }
}

// ── registration helpers ──────────────────────────────────────────────────────

// RegisterWriteKong registers the write-track verbs onto p using native kong
// structs. cost is NOT registered here — it lives in RegisterCostKong (cost.go).
func RegisterWriteKong(p *cli.Parser) {
	p.AddVerb("reopen", "Reopen a closed initiative.", &reopenKong{})
	p.AddVerb("register", "Register a new initiative from a body file.", &registerKong{
		createEpic: createEpicInRepo,
	})
	p.AddVerb("note", "Add a note to an initiative.", &noteKong{})
	p.AddVerb("gate", "Add a gate (human-review request) to an initiative.", &gateKong{})
	p.AddVerb("clear-gate", "Clear the human-review gate on an initiative.", &clearGateKong{})
	p.AddVerb("learn", "Store a memory for a role.", &learnKong{})
	p.AddVerb("close", "Close an initiative.", &closeKong{})
	p.AddVerb("pull", "Pull the remote beads database (dolt pull).", &pullKong{})
	p.AddVerb("sync", "Pull then push the beads database (bounded non-ff retry).", &syncKong{})
	p.AddVerb("forget", "Delete a role memory by key.", &forgetKong{})
	p.AddVerb("condense", "Emit a structured memory packet for a role.", &condenseKong{})
	p.AddVerb("fresh-drain", "Drain fresh: memories to cold for a role.", &freshDrainKong{})
	RegisterCondenseLock(p)
}

// RegisterAllKong is the FROZEN dispatcher called by main.go.
func RegisterAllKong(p *cli.Parser) {
	RegisterWriteKong(p)
	RegisterCostKong(p)
	RegisterQueryKong(p)
	RegisterMatchKong(p)
	RegisterDispatchKong(p)
	RegisterWorktreeSetupKong(p)
	RegisterMessagingKong(p)
	RegisterMailKong(p)
	RegisterRouteEventKong(p)
	RegisterStatusKong(p)
	RegisterWatchersKong(p)
	RegisterReapOrphansKong(p)
}
