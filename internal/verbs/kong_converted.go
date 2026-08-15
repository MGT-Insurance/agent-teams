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
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/cost"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
	"github.com/mgt-insurance/agent-teams/internal/sentlog"
	"github.com/mgt-insurance/agent-teams/internal/transport"
	"github.com/mgt-insurance/agent-teams/internal/workspace"
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
	// (fail-soft) or a temp file path + cleanup + epicID when it succeeds.
	bodyFile := c.File
	var epicID string
	if c.createEpic != nil {
		if modPath, eid, cleanup := appendEpicToBody(ctx, c.File, c.Title, c.createEpic); cleanup != nil {
			bodyFile = modPath
			epicID = eid
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

	// Label the root epic with the initiative ID (fail-soft).
	if epicID != "" {
		bodyBytes, _ := os.ReadFile(bodyFile)
		registered := initiative.Of(bd.Issue{Description: string(bodyBytes)})
		repoPath := registered.Repo
		if repoPath == "" {
			repoPath = registered.Worktree
		}
		if repoPath != "" {
			cmd := exec.Command("bd", "-C", repoPath, "label", "add", epicID, issue.ID)
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(ctx.Stderr, "ateam register: warning: could not label epic %s with %s (fail-soft): %v\n", epicID, issue.ID, err)
			}
		}
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

// gateNotifyFunc is called after gate labels are set to route the ask to
// the Steward. Injected so tests can verify invocations and simulate
// failures without a real transport. nil means skip notify (zero-value
// gateKong, test usage).
type gateNotifyFunc func(ctx *cli.Context, id, file string) error

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

	// PR scopes the gate to one PR, per the frozen grammar
	// "<base>:<pr-url>" (docs/multi-pr-contract.md §3): the emitted label
	// becomes "gate:<kind>:<pr>" instead of the bare, initiative-scoped
	// "gate:<kind>". Omitted (legacy, unchanged): bare form. "human" is
	// unaffected either way — it stays bare and initiative-scoped by design.
	PR string `name:"pr" help:"Full PR URL to scope the gate to one PR (per-PR label); omitted sets the bare, initiative-scoped gate (legacy)."`

	// notify is called after labels are set to route the ask to the Steward
	// (see notifyToSteward, steward_route.go). Best-effort: a failure warns
	// to stderr but does not fail the gate. nil means skip (zero-value
	// struct, test usage without a notify hook). Routing is gated solely on
	// notifyToSteward's own steward-marker presence check (e3mq.24) — there
	// is no transport-configured precondition here (e3mq.26).
	notify gateNotifyFunc `kong:"-"`
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

	// Resolve --pr to its canonical form and require it to be one of the
	// initiative's ACTUAL resolved PRs before minting a per-PR label from it
	// (agent-teams-ssib.25) — a typo'd or differently-spelled --pr must be
	// rejected, not silently turned into a label nothing can ever pair with.
	pr := c.PR
	if pr != "" {
		canon, _, err := resolvePR(ctx, "ateam gate", c.ID, pr)
		if err != nil {
			return err
		}
		pr = canon
	}

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
		if pr != "" {
			// Tag the block with the PR it's about, so human-list can pair
			// each per-PR row with the block that's actually about IT,
			// instead of always showing the initiative's latest ask
			// regardless of which PR it was for — a bare gate never sets
			// this, so a single-PR (or no-PR) initiative's ask blocks are
			// never tagged and human-list's fallback for that case (the
			// latest block, tag or no tag) is unaffected. buildAskBlock's
			// output always starts with this exact literal, so this is a
			// safe one-shot insertion, not a fragile string scan. Tagged
			// with the CANONICAL pr (resolved above), matching what
			// ResolvedPRs hands human-list, so the tag and the row it must
			// pair with always compare equal (agent-teams-ssib.25).
			block = strings.Replace(block, "<<<ateam-ask\n", "<<<ateam-ask\npr: "+pr+"\n", 1)
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
	gateLabel := "gate:" + c.Kind
	if pr != "" {
		gateLabel += ":" + pr
	}
	out, runErr = ctx.BD.Run("label", "add", c.ID, gateLabel)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	if runErr != nil {
		return runErr
	}

	// Best-effort steward routing: fire the notify path so the ask reaches
	// the Steward. notifyToSteward gates on its own steward-marker presence
	// check (e3mq.24); no transport-configured precondition belongs here
	// (e3mq.26) — a machine with a steward but no Telegram config must still
	// route gates to it. A notify failure warns to stderr but stays
	// non-fatal to the gate.
	//
	// For structured-ask gates, send the human-readable form (buildAskMessage)
	// rather than the raw sentinel block. The bead note (noteFile) is
	// unchanged — only the notify body differs. This is lazy: the temp file
	// is built only inside this branch to stay zero-footprint when notify
	// is unset.
	if c.notify != nil {
		notifyFile := noteFile
		if structuredUsed {
			ask := &gateAsk{
				decision:       c.Decision,
				recommendation: c.Recommendation,
				alternative:    c.Alternative,
				contextFile:    c.ContextFile,
			}
			msg := buildAskMessage(ask)
			if tmp, tmpErr := os.CreateTemp("", "ateam-gate-notify-*"); tmpErr == nil {
				tmpNotifyPath := tmp.Name()
				if _, writeErr := tmp.WriteString(msg); writeErr == nil {
					tmp.Close()
					notifyFile = tmpNotifyPath
					defer os.Remove(tmpNotifyPath)
				} else {
					tmp.Close()
					os.Remove(tmpNotifyPath)
				}
			}
			// On any temp-file failure, fall back to noteFile (sentinel block).
		}
		if notifyErr := c.notify(ctx, c.ID, notifyFile); notifyErr != nil {
			fmt.Fprintf(ctx.Stderr, "ateam gate: warning: notify failed (gate still recorded): %v\n", notifyErr)
		}
	}
	return nil
}

// ── clear-gate ────────────────────────────────────────────────────────────────

// clearGateKong is the kong-converted form of clear-gate.
// Takes a positional <id>, an optional --file flag, and an optional --pr
// discriminator (docs/multi-pr-contract.md §3).
type clearGateKong struct {
	ID   string `arg:"" name:"id"  help:"Initiative ID."`
	File string `name:"file"       help:"Path to response file (optional)."`

	// PR scopes the clear to ONE PR's gate/handoff labels, fixing
	// agent-teams-ssib.4's over-wipe: without --pr, clearing one PR's gate
	// used to unconditionally strip every other PR's gate and handoff too.
	// Omitted: the whole-initiative reset — every gate/handoff label this
	// initiative carries, bare AND per-PR, is removed. external_review.go
	// §9's H -> U / R -> U transitions still get exactly the unconditional
	// clear they depend on; --pr is surgical (one PR only), bare is total
	// (every PR).
	PR string `name:"pr" help:"Full PR URL to scope the clear to one PR's gate; omitted clears every gate/handoff label on the initiative, bare and per-PR (unconditional)."`
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

	if c.PR == "" {
		return c.clearBareLegacy(ctx)
	}
	return c.clearOnePR(ctx)
}

// clearBareLegacy is the unconditional whole-initiative reset for the
// no-`--pr` call — external_review.go §9's H -> U / R -> U transitions
// depend on the four bare labels going away unconditionally, and that part
// is preserved byte-for-byte.
//
// It ALSO sweeps every per-PR-suffixed gate/handoff label the initiative
// carries ("gate:review:<url>", "gate:question:<url>",
// "external-review:<url>"), not just the four bare ones. Bare clear-gate is
// the DRI playbook's documented "I am done with this initiative" call
// (resume, standby-release, and the Phase 5 close-out immediately before
// `ateam close`) — its callers assume it clears EVERYTHING gate-related. A
// --pr gate widens what "everything" contains, so a bare clear must widen
// its sweep to match: otherwise a per-PR label survives every bare
// clear-gate forever, and a CLOSED initiative keeps reporting REVIEWABLE /
// NEEDS-DECISION with no way to dismiss it (agent-teams-ssib.4, one level
// up from the --pr-scoped half clearOnePR fixes). --pr stays surgical (one
// PR only); bare stays total (every PR on the initiative).
//
// Reading current labels first (to find the per-PR ones — their PR URLs
// aren't otherwise knowable) is best-effort: if the read fails, this falls
// back to the historical four-bare-label sweep alone, which is still
// correct for the common case of an initiative that never used --pr.
func (c *clearGateKong) clearBareLegacy(ctx *cli.Context) error {
	var extra []string
	if issue, err := bd.ShowIssue(ctx.BD, c.ID); err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam clear-gate: warning: could not read labels for %s (%v) — clearing bare labels only\n", c.ID, err)
	} else {
		extra = perPRGateLabels(issue.Labels)
	}

	labels := append([]string{"human", "gate:review", "gate:question", externalReviewLabel}, extra...)
	for _, label := range labels {
		out, err := ctx.BD.Run("label", "remove", c.ID, label)
		if out != "" {
			fmt.Fprintln(ctx.Stdout, out)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// perPRGateLabels returns every label in labels that is a per-PR-suffixed
// gate or handoff label ("gate:review:<url>", "gate:question:<url>",
// "external-review:<url>"), in their original order. The three BARE bases
// are handled separately by clearBareLegacy's always-attempted four-label
// removal, so this only needs to find the per-PR additions a --pr gate call
// may have left behind.
func perPRGateLabels(labels []string) []string {
	var found []string
	for _, l := range labels {
		for _, base := range []string{"gate:review:", "gate:question:", externalReviewLabel + ":"} {
			if strings.HasPrefix(l, base) {
				found = append(found, l)
				break
			}
		}
	}
	return found
}

// clearOnePR clears ONLY c.PR's per-PR gate and handoff labels, leaving
// every other PR's gate and handoff declaration untouched (fixes
// agent-teams-ssib.4's over-wipe). The shared, bare "human" label — which
// stays initiative-scoped by design (docs/multi-pr-contract.md §3) — is
// removed only once NO PR on the initiative remains gated; while any other
// PR is still gated, "human" stays.
func (c *clearGateKong) clearOnePR(ctx *cli.Context) error {
	// Resolve --pr to the SAME canonical form a `gate --pr`/`handoff --pr`
	// call would have used to build the label in the first place
	// (agent-teams-ssib.25) — without this, clearing with a differently-
	// cased or http-vs-https spelling of the same PR would silently remove
	// nothing (bd label remove is exact-match) and leave the gate stuck.
	// Fails loudly if --pr doesn't name one of the initiative's actual
	// resolved PRs; the unconditional bare `clear-gate` (no --pr) remains
	// the escape hatch for a stray per-PR label that predates this fix or
	// no longer matches a resolved PR.
	pr, issue, err := resolvePR(ctx, "ateam clear-gate", c.ID, c.PR)
	if err != nil {
		return err
	}

	// A bare, initiative-wide gate label can't be scoped to one PR. If this
	// PR carries no per-PR label of its own but the initiative DOES carry a
	// bare gate, the removals below would all be no-ops against labels that
	// were never there — and `bd label remove` prints a ✓ regardless,
	// reporting a confident false success while nothing actually changes
	// (agent-teams-ssib.30, reproduced live: three ✓ lines, labels and
	// human-list unchanged). Refuse loudly and name the fix that actually
	// works, matching resolvePR's own posture on an unresolved --pr.
	hasOwnPerPRLabel := hasLabel(issue.Labels, "gate:review:"+pr) ||
		hasLabel(issue.Labels, "gate:question:"+pr) ||
		hasLabel(issue.Labels, externalReviewLabel+":"+pr)
	hasBareGate := hasLabel(issue.Labels, "gate:review") || hasLabel(issue.Labels, "gate:question")
	if !hasOwnPerPRLabel && hasBareGate {
		return cli.Usagef("ateam clear-gate: %s's gate is initiative-wide, not per-PR — run `ateam clear-gate %s` without --pr to clear it", c.ID, c.ID)
	}

	for _, base := range []string{"gate:review", "gate:question", externalReviewLabel} {
		out, err := ctx.BD.Run("label", "remove", c.ID, base+":"+pr)
		if out != "" {
			fmt.Fprintln(ctx.Stdout, out)
		}
		if err != nil {
			return err
		}
	}

	issueAfter, err := bd.ShowIssue(ctx.BD, c.ID)
	if err != nil {
		// Can't verify whether another PR is still gated without reading
		// current labels — fail soft by leaving "human" in place. A stray
		// "human" label is a false-positive nag; wrongly stripping it while
		// another PR is still gated silently drops that PR's ask, which is
		// worse.
		fmt.Fprintf(ctx.Stderr, "ateam clear-gate: warning: could not read labels for %s (%v) — leaving \"human\" label as-is\n", c.ID, err)
		return nil
	}
	if anyGateLabelRemains(issueAfter.Labels) {
		return nil
	}
	out, err := ctx.BD.Run("label", "remove", c.ID, "human")
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return err
}

// anyGateLabelRemains reports whether labels still contain a review or
// question gate for any PR — bare, legacy form, or per-PR "<base>:<url>"
// suffixed form (docs/multi-pr-contract.md §3). Used by clear-gate to decide
// whether the shared "human" label is safe to remove once one PR's gate is
// cleared. external-review is deliberately excluded: it is additive on top
// of a review gate (external_review.go §2), never a gate on its own, so it
// carries no signal here.
func anyGateLabelRemains(labels []string) bool {
	return hasGateKind(labels, "gate:review") || hasGateKind(labels, "gate:question")
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
	content := strings.TrimRight(string(data), "\n")
	tier, capBytes := learnCap(c.Slug)
	if len(content) > capBytes {
		return learnCapError(tier, capBytes, len(content))
	}
	key := learnKey(c.Role, c.Slug)
	out, runErr := ctx.BD.Run("remember", "--key="+key, content)
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

	// runUpdateLocalMain is injected at registration time so tests can
	// substitute a fake without exec'ing a real script. If nil,
	// runLocalMainUpdate falls back to runUpdateLocalMainScript.
	runUpdateLocalMain updateLocalMainFunc `kong:"-"`

	// transportFor resolves the active transport for the close-signal
	// farewell message + topic close. Injected at registration time
	// (transport.For) so tests can substitute a fake. If nil,
	// sendCloseSignal falls back to transport.For.
	transportFor transportForFunc `kong:"-"`
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

	var out string
	var err error
	if reason != "" {
		out, err = ctx.BD.Run("close", c.ID, "--reason="+reason)
	} else {
		out, err = ctx.BD.Run("close", c.ID)
	}
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	if err != nil {
		return err
	}

	c.runLocalMainUpdate(ctx)
	c.sendCloseSignal(ctx)
	return nil
}

// closeSignalFarewell is posted into the initiative's Telegram topic (if
// one exists) immediately before the topic is closed via sendCloseSignal.
const closeSignalFarewell = "This initiative is closed. The topic is now closed as a signal — you can reopen it from the Telegram UI anytime; messages posted here will be routed to the Steward."

// topicCloser is an optional transport capability (Telegram forum topics
// implement it) for closing a thread as a visible "this is done" signal.
// Asserted here at the call site rather than folded into
// transport.Transport, which stays initiative-agnostic and shared by
// transports that have no notion of a closeable thread.
type topicCloser interface {
	CloseTopic(threadRef string) error
}

// sendCloseSignal posts a farewell message into the initiative's Telegram
// forum topic and closes the topic, as a visible signal that the initiative
// is done. Best-effort and fail-soft: bd close above has already succeeded
// by the time this runs, so nothing here can fail `ateam close`. The
// "nothing to do" cases (no thread label, no transport configured) are
// silent skips — that's the normal state for most closes/installs; actual
// transport errors during Send/CloseTopic are logged to stderr as warnings.
func (c *closeKong) sendCloseSignal(ctx *cli.Context) {
	issue, err := bd.ShowIssue(ctx.BD, c.ID)
	if err != nil {
		return
	}
	threadRef := threadLabelValue(issue.Labels)
	if threadRef == "" {
		return
	}

	transportFor := c.transportFor
	if transportFor == nil {
		transportFor = transport.For
	}
	t, err := transportFor(workspace.Home())
	if err != nil {
		// No transport configured: the normal default state for installs
		// that haven't set up Telegram — silent skip, not a warning.
		return
	}

	msg := transport.OutboundMessage{
		InitiativeID: c.ID,
		ThreadRef:    threadRef,
		Title:        issue.Title,
		Body:         closeSignalFarewell,
		Sender:       sentlog.KindClose,
	}
	if _, sendErr := t.Send(msg); sendErr != nil {
		fmt.Fprintf(ctx.Stderr, "ateam close: warning: farewell message failed: %v\n", sendErr)
		return
	}

	closer, ok := transport.Capability[topicCloser](t)
	if !ok {
		return
	}
	if closeErr := closer.CloseTopic(threadRef); closeErr != nil {
		fmt.Fprintf(ctx.Stderr, "ateam close: warning: closing topic failed: %v\n", closeErr)
	}
}

// runLocalMainUpdate best-effort fast-forwards the initiative's local main
// checkout after a successful close. Never returns an error — any failure
// (issue lookup, missing repo: line, script exec) is fail-soft: printed as a
// one-line note and swallowed.
func (c *closeKong) runLocalMainUpdate(ctx *cli.Context) {
	issue, err := bd.ShowIssue(ctx.BD, c.ID)
	if err != nil {
		return
	}
	f := initiative.Of(issue)
	repo := f.Repo
	if repo == "" {
		repo = f.Worktree
	}
	if repo == "" {
		return
	}
	run := c.runUpdateLocalMain
	if run == nil {
		run = runUpdateLocalMainScript
	}
	out, err := run(repo)
	if err != nil {
		fmt.Fprintf(ctx.Stdout, "update-local-main: skipped (%v)\n", err)
		return
	}
	if out != "" {
		fmt.Fprint(ctx.Stdout, out)
	}
}

// updateLocalMainFunc runs update-local-main.sh against repoPath and returns
// its combined output. Injected on closeKong so tests can fake it.
type updateLocalMainFunc func(repoPath string) (string, error)

// runUpdateLocalMainScript resolves update-local-main.sh relative to the
// running ateam binary (self-locating — see route_types.go's
// defaultAteamRunner for the same os.Executable() pattern) and execs it
// against repoPath. Returns an error if the binary can't be located, the
// script doesn't exist there, or the script itself exits non-zero.
func runUpdateLocalMainScript(repoPath string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve self binary: %w", err)
	}
	script := filepath.Join(filepath.Dir(self), "..", "hooks", "scripts", "update-local-main.sh")
	if _, err := os.Stat(script); err != nil {
		return "", fmt.Errorf("update-local-main.sh not found at %s: %w", script, err)
	}
	out, err := exec.Command(script, repoPath).CombinedOutput()
	return string(out), err
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

// ── applied ───────────────────────────────────────────────────────────────────

// appliedRecord is the JSON body shape stored at an applied:<role>:<slug> key.
type appliedRecord struct {
	Count       int    `json:"count"`
	LastApplied string `json:"last_applied"`
}

// appliedKey computes the bd memory key for an applied-signal record. slug is
// the BARE slug (the part after any hot:/fresh:/cold: tier prefix) — the
// applied counter is deliberately tier-independent (contract
// agent-teams-u71p.1), and the "applied:" top-level prefix keeps it out of
// every existing "<role>:" scan (condense, fresh-drain, etc.).
func appliedKey(role, slug string) string {
	return "applied:" + role + ":" + slug
}

// appliedKong is the kong-converted form of applied.
// Takes positional <role> and <slug> (bare slug, no tier prefix).
type appliedKong struct {
	Role string `arg:"" name:"role" help:"Role name (e.g. planner, implementer)."`
	Slug string `arg:"" name:"slug" help:"Bare learning slug (the part after any hot:/fresh:/cold: tier prefix)."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
// Behavior is read-modify-write against the global memory KV: read the
// current applied:<role>:<slug> record (defaulting to count 0 if absent or
// malformed), increment the count, stamp last_applied to now (UTC RFC3339),
// and write it back via the same "remember" path ateam learn uses. This is a
// rough signal — non-atomic RMW is an accepted tradeoff (contract
// agent-teams-u71p.1).
func (c *appliedKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam applied: no context")
	}
	key := appliedKey(c.Role, c.Slug)

	var raw map[string]any
	if err := ctx.BD.RunJSON(&raw, "memories", "--json"); err != nil {
		return err
	}

	var rec appliedRecord
	if v, ok := raw[key]; ok {
		if s, ok := v.(string); ok {
			// Best-effort parse; a malformed/absent existing body starts
			// fresh at count 0 rather than erroring — rough signal.
			_ = json.Unmarshal([]byte(s), &rec)
		}
	}
	rec.Count++
	rec.LastApplied = time.Now().UTC().Format(time.RFC3339)

	body, err := json.Marshal(rec)
	if err != nil {
		return err
	}

	out, err := ctx.BD.Run("remember", "--key="+key, string(body))
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return err
}

// condenseBareSlug derives the bare slug (no tier prefix) from a "<role>:..."
// memory key, given rolePrefix = "<role>:". A key of role:hot:<slug> or
// role:fresh:<slug> strips the tier tag; a bare/cold key role:<slug> (no tier
// tag) passes through unchanged.
func condenseBareSlug(rolePrefix, key string) string {
	rest := strings.TrimPrefix(key, rolePrefix)
	if s, ok := strings.CutPrefix(rest, "hot:"); ok {
		return s
	}
	if s, ok := strings.CutPrefix(rest, "fresh:"); ok {
		return s
	}
	return rest
}

// lookupApplied reads the sibling applied:<role>:<slug> record (if any) out
// of the already-fetched memories map and returns its count/last_applied.
// Absent or malformed records return (0, "").
func lookupApplied(raw map[string]any, role, slug string) (count int, lastApplied string) {
	v, ok := raw[appliedKey(role, slug)]
	if !ok {
		return 0, ""
	}
	s, ok := v.(string)
	if !ok {
		return 0, ""
	}
	var rec appliedRecord
	if err := json.Unmarshal([]byte(s), &rec); err != nil {
		return 0, ""
	}
	return rec.Count, rec.LastApplied
}

// ── condense ──────────────────────────────────────────────────────────────────

// condenseBudgetTokens is the hot-tier token budget the condense agent targets.
const condenseBudgetTokens = 6000

// condenseFreshThresholdTokens is the SOLE fire/skip trigger for
// `ateam condense-check` (contract agent-teams-0yd3.1 SEAM 2, round-6
// amendment, threshold value decided by Eric 2026-07-28): a role fires when
// its fresh-tier material ALONE exceeds this many approx tokens. The former
// hot∪fresh 8,000 ceiling is explicitly NOT a trigger — it is retained only
// as a reported number (condenseCheckRoleResult.ApproxTokens), never
// branched on. Do not resurrect it as a second leg without a fresh contract
// amendment; see SEAM 2 item 4 (a trigger must be CLEARABLE by the action it
// triggers — the ceiling failed that test).
const condenseFreshThresholdTokens = 4000

// condenseApproxTokensDivisor is the bytes-per-token heuristic from
// SKILL.md's "one token ≈ 3 bytes of English text" rule of thumb, computed
// here in Go (SEAM 2) instead of by the model.
const condenseApproxTokensDivisor = 3

// condenseColdSummaryMaxChars is the max length (in runes) of a cold entry's
// elided-body summary line (SEAM 3): the first line of the body, truncated.
const condenseColdSummaryMaxChars = 120

// condenseHotFreshCapBytes / condenseColdCapBytes restate, for the packet's
// own instruction contract text, the write-time per-entry byte caps enforced
// by learnCap (write.go:85, frozen by contract agent-teams-b2xr.2). Declared
// here (not imported from learnCap) because write.go is owned by a different
// track (Track C) and this file must stay file-disjoint from it; keep these
// in sync with learnCap by hand if the frozen caps ever change.
const condenseHotFreshCapBytes = 900
const condenseColdCapBytes = 1500

// condenseInstructionContract is the instruction contract emitted to the
// consuming condense agent. The agent applies the result DIRECTLY and
// autonomously via ateam learn / ateam forget — no human review gate.
//
// SEAM 3 requires this text to state (a) that cold bodies are elided from
// the packet and how to retrieve one on demand, and (b) the per-entry byte
// cap — so the curation agent learns the cap from the contract, not by
// getting a write rejected.
var condenseInstructionContract = fmt.Sprintf(`Condense the memories above into a hot tier for this role.

PACKET SHAPE: each entry's "key" is RELATIVE to this packet's "role" (every
entry in this packet is already this one role) — it is "hot:<slug>",
"fresh:<slug>", or a bare "<slug>" for cold, NOT the full "<role>:..." form.
Tier is encoded in that prefix; there is no separate tier field. Pass "key"
verbatim to ateam recall as <term> (see below) — that holds for EVERY tier,
cold included, because recall matches the full store key and "key" is
always a substring of it (this is the .18 guarantee; do not break it).
Passing "key" verbatim to ateam learn is safe ONLY for hot and fresh
entries, whose "key" already carries the "hot:" or "fresh:" prefix (e.g.
ateam learn <role> <key> --file <f>) — do not re-prepend the role. A COLD
entry's "key" is a BARE slug with no prefix: passing it verbatim to ateam
learn does NOT rewrite the cold entry — it silently WRITES A NEW FRESH
ENTRY instead (a bare slug defaults to the fresh tier), leaving the stale
cold original untouched. That duplicate is then injected into every
session (ateam learnings serves hot ∪ fresh), which re-arms the very
fresh-tier trigger a curation pass exists to clear. To rewrite, merge, or
refresh a cold entry, prepend "cold:" yourself: ateam learn <role>
cold:<key> --file <f>. Name this asymmetry, because it is easy to get
backwards: for the SAME bare key, ateam learn <role> <key> writes to FRESH
(its default tier) while ateam forget <role> <key> deletes from COLD
(forget forms role:<slug> directly, with no fresh fallback) — one bare
argument, opposite tiers, depending on the verb. Hot AND fresh entries
carry a full "body". Fresh entries are
the PRIMARY PROMOTION CANDIDATES: they were being served to every session
(hot ∪ fresh) until the moment they were drained, so they get full-body
visibility on purpose, same as hot — do not treat them like cold. Cold
entries carry ONLY a "summary" (first line of the body, truncated to %d
chars) — their full body is deliberately NOT included in this packet, to
keep it small. Before deciding whether to promote, merge, or evict a cold
entry, read its full body — ateam recall is the ONLY retrieval path this
contract offers, and it is guaranteed to match (see PACKET SHAPE above):
  ateam recall <role> <term>   (tokenized, ranked search over key + body)
    -- pass this entry's OWN "key" field verbatim as <term>: it is
    guaranteed to match, because recall's search runs against the full
    store key, and this entry's "key" is always a substring of it. Expect
    the header to read exactly "1 matches". Do NOT substitute a
    descriptive phrase: recall splits <term> on whitespace and counts an
    entry as a match if ANY single token appears anywhere in its key or
    body, so a plausible phrase (e.g. "self hosting bootstrap") matches
    most of the store and reads as a pass while proving nothing. Read the
    "[recall <role> ...: N matches]" COUNT, never merely whether output
    appeared. On zero matches recall prints a "nearest:" list -- it is NOT
    a "did you mean": every candidate scored zero, so it is just that
    role's alphabetically first keys, identical for every failing query.
applied_count / last_applied are joined on EVERY entry — hot, fresh, AND cold
alike, never skipped by tier. To keep the packet small, a field is OMITTED
when it is exactly its zero value: a missing applied_count means 0, a missing
last_applied means never applied — this is a pure encoding convention, not a
missing join. They are your ranking signal for what to promote or evict, not
decorative metadata: prefer promoting candidates with a high applied_count,
and weigh a never-applied cold entry (no applied_count field at all) as an
eviction candidate.

Rules:
- PROMOTE or REFRESH high-signal or repeatedly-learned items into hot via: ateam learn <role> hot:<slug> --file <f>
- DEMOTE stale hot items down to cold by rewriting them at the cold key then deleting the hot key via: ateam learn <role> cold:<slug> --file <f>, then ateam forget <role> hot:<slug>
- Within cold: MERGE duplicates, REWRITE for brevity, and EVICT truly-dead items via: ateam learn <role> cold:<slug> --file <f> (for rewrites) or ateam forget <role> <slug> (for evictions)
- Each written entry has a write-time byte cap: hot/fresh %d bytes, cold %d bytes — write succinctly the first time; do not discover the cap by rejection
- Target the hot budget — see this packet's "hot_budget_tokens" field, in tokens (~15-25 succinct learnings); keep each hot item succinct but complete
- Apply ALL changes AUTONOMOUSLY with no human review gate
- After applying, emit one line: "promoted N / merged M / evicted K / hot now X tokens"
- v1 has NO eviction floor — trust Dolt history for recoverability`,
	condenseColdSummaryMaxChars, condenseHotFreshCapBytes, condenseColdCapBytes)

// condenseMemory is a single memory record in the condense packet (SEAM 3).
// Tier is NOT a separate wire field — it is already encoded in Key
// (role:hot:<slug>, role:fresh:<slug>, or a bare role:<slug> for cold), so a
// redundant field would only cost bytes; condenseInstructionContract states
// this convention for the consuming agent. Hot and fresh entries carry Body
// in full; cold entries instead carry Summary (first line of the body,
// truncated to condenseColdSummaryMaxChars) with Body left empty — the full
// cold body is elided from the packet (see condenseInstructionContract for
// the on-demand retrieval path).
//
// AppliedCount/LastApplied are joined from the memory's sibling
// applied:<role>:<slug> record and computed IDENTICALLY for every tier,
// including cold — the join itself is never skipped or tier-gated, which is
// what "retained on every tier" (SEAM 3) means. Both use omitempty: a memory
// that has never had `ateam applied` called on it joins to the exact zero
// value (0 / ""), and omitting an exact zero value loses no information —
// condenseInstructionContract states the convention explicitly (absent means
// 0 / never-applied) so the curation agent never has to guess whether
// "missing" means "zero" or "join failed". This is NOT the same move as
// cold's body elision (which drops real content behind a retrieval path);
// there is nothing to retrieve here, the zero value is the complete value.
// Key is RELATIVE to the packet's own Role field (which every memory in the
// packet shares by construction — condenseKong.Run filters by <role>:
// prefix) — it is "hot:<slug>", "fresh:<slug>", or a bare "<slug>" for cold,
// NOT the full store key "<role>:hot:<slug>". This is deliberate, not just a
// byte-count trim: it is exactly the string the contract's own rules already
// pass as the second CLI argument (ateam learn <role> hot:<slug>, ateam
// forget <role> <slug>), so the agent can use Key verbatim instead of first
// stripping a role prefix it already has at pkt.Role.
type condenseMemory struct {
	Key          string `json:"key"`
	Body         string `json:"body,omitempty"`
	Summary      string `json:"summary,omitempty"`
	AppliedCount int    `json:"applied_count,omitempty"`
	LastApplied  string `json:"last_applied,omitempty"`
}

// Tier returns "hot", "fresh", or "cold" for m, derived from Key's own
// (role-relative) prefix — mirrors condenseKong.Run's classification, which
// is the authoritative source.
func (m condenseMemory) Tier() string {
	switch {
	case strings.HasPrefix(m.Key, "hot:"):
		return "hot"
	case strings.HasPrefix(m.Key, "fresh:"):
		return "fresh"
	default:
		return "cold"
	}
}

// condensePacket is the full structured packet emitted to stdout.
type condensePacket struct {
	Role      string           `json:"role"`
	Memories  []condenseMemory `json:"memories"`
	HotBudget int              `json:"hot_budget_tokens"`
	Contract  string           `json:"instruction_contract"`
}

// condenseSummary returns the first line of body, trimmed and truncated to
// condenseColdSummaryMaxChars runes (an ellipsis marks truncation).
// Rune-based truncation avoids splitting a multi-byte UTF-8 character.
func condenseSummary(body string) string {
	line := body
	if idx := strings.IndexByte(body, '\n'); idx >= 0 {
		line = body[:idx]
	}
	line = strings.TrimSpace(line)
	runes := []rune(line)
	if len(runes) <= condenseColdSummaryMaxChars {
		return line
	}
	return string(runes[:condenseColdSummaryMaxChars]) + "…"
}

// condenseKong is the kong-converted form of condense.
// Takes a positional <role>.
type condenseKong struct {
	Role string `arg:"" name:"role" help:"Role to condense memories for."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
//
// Tier discrimination (SEAM 3) is by the key's CURRENT tier prefix as found
// in the store: <role>:hot:* and <role>:fresh:* entries carry a full body
// (fresh material has not yet had a hot/cold verdict made on it, so it needs
// full-body visibility exactly like hot); bare <role>:<slug> entries (no
// tier tag — settled cold) carry summary-only. The emitted packet Key is the
// store key with the <role>: prefix stripped (role-relative — see
// condenseMemory's doc comment), NOT the raw store key. This assumes
// condense is invoked while fresh: keys are still tagged as such; see
// discovery bead filed against agent-teams-0yd3 for the SKILL.md ordering
// implication (fresh-drain must not strip the fresh: tag before condense
// reads it, or newly-drained material would be silently indistinguishable
// from long-settled cold and would wrongly ship as summary-only).
func (c *condenseKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam condense: no context")
	}
	prefix := c.Role + ":"
	hotPrefix := prefix + "hot:"
	freshPrefix := prefix + "fresh:"

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
		slug := condenseBareSlug(prefix, k)
		appliedCount, lastApplied := lookupApplied(raw, c.Role, slug)
		body := raw[k].(string)

		mem := condenseMemory{
			Key:          strings.TrimPrefix(k, prefix),
			AppliedCount: appliedCount,
			LastApplied:  lastApplied,
		}
		switch {
		case strings.HasPrefix(k, hotPrefix):
			mem.Body = body
		case strings.HasPrefix(k, freshPrefix):
			mem.Body = body
		default:
			mem.Summary = condenseSummary(body)
		}
		memories = append(memories, mem)
	}

	packet := condensePacket{
		Role:      c.Role,
		Memories:  memories,
		HotBudget: condenseBudgetTokens,
		Contract:  condenseInstructionContract,
	}

	// Compact (unindented) JSON: pretty-printing costs real bytes at packet
	// scale (indentation/newlines across ~150+ entries) for zero information
	// gain — the consumer is an LLM/tool, not a human skimming a terminal.
	// Kept compact deliberately; do not add SetIndent back without re-running
	// the acceptance check (SEAM 3: dri/implementer packet under 20,000
	// approx tokens).
	return json.NewEncoder(ctx.Stdout).Encode(packet)
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

// appendEpicToBody reads the file at originalPath, extracts the project repo
// path from the body, creates a root epic via creator, and returns a new temp
// file path with "epic: <id>" appended, the epicID, and a cleanup function to
// remove it. Returns ("", "", nil) on any failure so callers fall back to the
// original file.
func appendEpicToBody(ctx *cli.Context, originalPath, title string, creator epicCreatorFunc) (path string, epicID string, cleanup func()) {
	bodyBytes, err := os.ReadFile(originalPath)
	if err != nil {
		return "", "", nil
	}
	bodyStr := string(bodyBytes)
	// The file is a prospective description, so it reads through the same
	// seam a stored one does; worktree is the fallback when no repo line is
	// present.
	prospective := initiative.Of(bd.Issue{Description: bodyStr})
	repoPath := prospective.Repo
	if repoPath == "" {
		repoPath = prospective.Worktree
	}
	if repoPath == "" {
		return "", "", nil
	}
	eid, epicErr := creator(repoPath, title)
	if epicErr != nil {
		fmt.Fprintf(ctx.Stderr, "ateam register: warning: could not create root epic (fail-soft): %v\n", epicErr)
		return "", "", nil
	}
	if eid == "" {
		return "", "", nil
	}
	modified := strings.TrimRight(bodyStr, "\n") + "\nepic: " + eid + "\n"
	tmp, err := os.CreateTemp("", "ateam-register-*")
	if err != nil {
		return "", "", nil
	}
	if _, err := tmp.WriteString(modified); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", "", nil
	}
	tmp.Close()
	tmpPath := tmp.Name()
	return tmpPath, eid, func() { os.Remove(tmpPath) }
}

// ── update-description ────────────────────────────────────────────────────────

// updateDescriptionKong updates an initiative's description from a file.
type updateDescriptionKong struct {
	ID   string `arg:"" name:"id"   help:"Initiative ID to update."`
	File string `name:"file" help:"Path to new description file (required)." required:""`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *updateDescriptionKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam update-description: no context")
	}
	out, err := ctx.BD.Run("update", c.ID, "--body-file="+c.File)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	return err
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
	p.AddVerb("gate", "Add a gate (human-review request) to an initiative.", &gateKong{
		notify: notifyToSteward,
	})
	p.AddVerb("clear-gate", "Clear the human-review gate on an initiative.", &clearGateKong{})
	p.AddVerb("handoff", "Declare (or clear) that the human is done looking at an initiative; it's on the team for external review.", &handoffKong{})
	p.AddVerb("learn", "Store a memory for a role.", &learnKong{})
	p.AddVerb("close", "Close an initiative.", &closeKong{
		runUpdateLocalMain: runUpdateLocalMainScript,
		transportFor:       transport.For,
	})
	p.AddVerb("pull", "Pull the remote beads database (dolt pull).", &pullKong{})
	p.AddVerb("sync", "Pull then push the beads database (bounded non-ff retry).", &syncKong{})
	p.AddVerb("forget", "Delete a role memory by key.", &forgetKong{})
	p.AddVerb("applied", "Record an applied-signal for a role's learning (bumps count + last_applied).", &appliedKong{})
	p.AddVerb("condense", "Emit a structured memory packet for a role.", &condenseKong{})
	p.AddVerb("fresh-drain", "Drain fresh: memories to cold for a role.", &freshDrainKong{})
	p.AddVerb("update-description", "Update an initiative's description from a file.", &updateDescriptionKong{})
	RegisterCondenseLock(p)
	RegisterCondenseCheck(p)
}

// RegisterAllKong is the FROZEN dispatcher called by main.go.
func RegisterAllKong(p *cli.Parser) {
	RegisterWriteKong(p)
	RegisterCostKong(p)
	RegisterQueryKong(p)
	RegisterMatchKong(p)
	RegisterDispatchKong(p)
	RegisterRuntimeKong(p)
	RegisterSetupKong(p)
	RegisterWorktreeSetupKong(p)
	RegisterMailKong(p)
	RegisterRouteEventKong(p)
	RegisterStatusKong(p)
	RegisterHungScanKong(p)
	RegisterWatchersKong(p)
	RegisterReapOrphansKong(p)
	RegisterNotifyKong(p)
	RegisterRelayKong(p)
	RegisterStewardKong(p)
	RegisterTieSessionKong(p)
	RegisterSentKong(p)
	RegisterAgentsJSONKong(p)
	RegisterSpawnCheckKong(p)
	RegisterPreflightKong(p)
	RegisterPRKong(p)
}
