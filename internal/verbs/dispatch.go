// This file is owned by Track D (dispatch verbs).
package verbs

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/gitutil"
	"github.com/mgt-insurance/agent-teams/internal/transport"
)

// RegisterDispatchKong registers dispatch verbs onto p using native kong structs.
func RegisterDispatchKong(p *cli.Parser) {
	p.AddVerb("new-initiative", "Spawn a background DRI session in <directory>.", &newInitiativeKong{
		launch: launchBGSession,
	})
	p.AddVerb("dispatch", "Create a worktree, register an initiative, and optionally launch a DRI session.", &dispatchKong{
		git:              gitutil.New(),
		launch:           launchBGSession,
		launchRaw:        rawLaunchBGSession,
		createEpic:       createEpicInRepo,
		transportFor:     transport.For,
		transportEnabled: transport.Enabled,
		labelAdd:         defaultLabelAdd,
	})
	p.AddVerb("resume", "Re-launch a background DRI session for an existing initiative.", &resumeKong{
		launch:    launchBGSession,
		launchRaw: rawLaunchBGSession,
	})
}

// ---- new-initiative (kong) --------------------------------------------------

// newInitiativeKong is the kong-native form of new-initiative.
// <directory> is required; remaining args form the problem statement / initiative id.
type newInitiativeKong struct {
	Dir     string   `arg:"" name:"directory" help:"Directory to run the DRI session in."`
	DriArgs []string `arg:"" name:"dri-arg" optional:"" help:"Initiative id or problem statement words."`

	// launch is injected at registration time; kong:"-" keeps kong from treating
	// it as a flag. Tests stub it so they never exec a real `claude --bg` session.
	launch launchFunc `kong:"-"`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *newInitiativeKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam new-initiative: not implemented")
	}
	dir := c.Dir
	if dir == "" {
		return cli.Usagef("ateam new-initiative: missing <directory>")
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return cli.Usagef("ateam new-initiative: not a directory: %s", dir)
	}
	if !fi.IsDir() {
		return cli.Usagef("ateam new-initiative: not a directory: %s", dir)
	}
	if len(c.DriArgs) == 0 {
		return cli.Usagef("ateam new-initiative: missing <dri-arg> (initiative id or problem statement)")
	}
	driArg := strings.Join(c.DriArgs, " ")
	if driArg == "" {
		return cli.Usagef("ateam new-initiative: missing <dri-arg> (initiative id or problem statement)")
	}
	launch := c.launch
	if launch == nil {
		launch = launchBGSession
	}
	return launch(ctx, dir, driArg)
}

// ---- dispatch (kong) --------------------------------------------------------

// dispatchKong is the kong-native form of dispatch.
// git, launch, createEpic, and launchRaw are injected at registration time;
// kong:"-" keeps kong from treating them as flags. Tests stub all four so they
// never exec a real git/claude/bd binary.
type dispatchKong struct {
	Problem      string `name:"problem"       help:"One-line problem statement (required)." required:""`
	Repo         string `name:"repo"          help:"Target directory to resolve repo from (default: cwd)."`
	BaseBranch   string `name:"base-branch"   help:"Override base branch (default: detected)."`
	Slug         string `name:"slug"          help:"Kebab-case slug (default: derived from --problem)."`
	BodyFile     string `name:"body-file"     help:"Path to file whose content is appended to the initiative body after schema lines."`
	IDOnly       bool   `name:"id-only"       help:"Print only the initiative id."`
	NoLaunch     bool   `name:"no-launch"     help:"Create worktree and register, but do not launch claude bg session."`
	LaunchPrompt string `name:"launch-prompt" help:"Custom prompt for bg session (replaces /dri <id>). {id} is replaced with initiative id."`
	SkipEpic     bool   `name:"skip-epic"     help:"Skip root epic creation in the project repo."`
	Model        string `name:"model"         help:"Model override for bg session (default: opus)."`
	Standby      bool   `name:"standby"       help:"Register in standby mode — the launched DRI parks on startup awaiting human direction instead of clarifying/planning."`
	Advisor      string `name:"advisor"       help:"Advisor model override for this launch (e.g. \"opus\"). Only affects the --launch-prompt path; when omitted/empty, preserves current behavior exactly (hardcoded \"\" for --launch-prompt, env-derived for the /dri path)."`

	git        gitRunner       `kong:"-"`
	launch     launchFunc      `kong:"-"`
	createEpic epicCreatorFunc `kong:"-"`
	launchRaw  rawLaunchFunc   `kong:"-"`

	// transportFor, transportEnabled, and labelAdd back the eager Telegram
	// (or configured transport) topic creation below. Injected at
	// registration time so tests can substitute fakes without touching a
	// real transport; a test that leaves any of the three nil simply does
	// not exercise eager topic creation (mirrors createEpic's nil-check
	// pattern above).
	transportFor     transportForFunc     `kong:"-"`
	transportEnabled transportEnabledFunc `kong:"-"`
	labelAdd         labelAddFunc         `kong:"-"`
}

// transportEnabledFunc is the function type for checking whether a usable
// transport is configured (transport.Enabled). Injected so tests can
// substitute a fake without touching real transport config/env.
type transportEnabledFunc func(home string) bool

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *dispatchKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam dispatch: not implemented")
	}

	// 1. Resolve repo root.
	repoDir := c.Repo
	if repoDir == "" {
		var err error
		repoDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("dispatch: cannot determine cwd: %w", err)
		}
	}
	repoRoot, err := c.git.RepoRoot(repoDir)
	if err != nil {
		fmt.Fprintln(ctx.Stderr, "dispatch: not inside a git repo: "+repoDir)
		return cli.Silent(1)
	}

	// 2. Base branch.
	base := c.BaseBranch
	if base == "" {
		base = c.git.DefaultBranch(repoRoot)
	}

	// 3. Slug.
	resolvedSlug := c.Slug
	if resolvedSlug == "" {
		resolvedSlug = gitutil.Slugify(c.Problem)
	}
	if resolvedSlug == "" {
		return cli.Usagef("dispatch: --problem produced an empty slug; provide --slug explicitly")
	}

	// 4. Worktree path: <workspace.Home()>-worktrees/<slug>
	wtRoot := ctx.Home + "-worktrees"
	wtPath := filepath.Join(wtRoot, resolvedSlug)

	// 5. Collision check.
	if c.git.WorktreeExists(repoRoot, wtPath) {
		fmt.Fprintf(ctx.Stderr,
			"dispatch: worktree already exists for slug %q at %s — pick a different --slug or remove the existing worktree\n",
			resolvedSlug, wtPath)
		return cli.Silent(1)
	}

	// 6. Create worktree.
	if err := c.git.AddWorktree(repoRoot, wtPath, resolvedSlug, base); err != nil {
		return fmt.Errorf("dispatch: %w", err)
	}

	// 7. Register the initiative via bd.
	team := gitutil.Slugify(filepath.Base(repoRoot)) + "-" + resolvedSlug
	shortTitle := c.Problem
	if len(shortTitle) > 72 {
		shortTitle = shortTitle[:72]
	}

	body := "problem: " + c.Problem + "\n" +
		"repo: " + repoRoot + "\n" +
		"worktree: " + wtPath + "\n" +
		"branch: " + resolvedSlug + "\n" +
		"team: " + team + "\n" +
		"mode: bg\n"
	if c.Standby {
		body += "standby: true\n"
	}

	// Try to create a root epic bead in the project repo (fail-soft).
	// repoRoot is already resolved above so no extraction is needed.
	// Skipped when --skip-epic is set.
	if !c.SkipEpic && c.createEpic != nil {
		if epicID, epicErr := c.createEpic(repoRoot, shortTitle); epicErr != nil {
			fmt.Fprintf(ctx.Stderr, "dispatch: warning: could not create root epic (fail-soft): %v\n", epicErr)
		} else if epicID != "" {
			body += "epic: " + epicID + "\n"
		}
	}

	if c.BodyFile != "" {
		extra, err := os.ReadFile(c.BodyFile)
		if err != nil {
			_ = c.git.RemoveWorktree(repoRoot, wtPath)
			return cli.Usagef("dispatch: --body-file %q: %v", c.BodyFile, err)
		}
		if len(strings.TrimSpace(string(extra))) > 0 {
			body += "\n" + string(extra)
		}
	}

	tmpFile, err := os.CreateTemp("", "ateam-dispatch-*.txt")
	if err != nil {
		return fmt.Errorf("dispatch: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("dispatch: write temp file: %w", err)
	}
	tmpFile.Close()

	var issue bd.Issue
	if err := ctx.BD.RunJSON(&issue, "create",
		"--title="+shortTitle,
		"--type=task",
		"--priority=2",
		"--body-file="+tmpPath,
		"--json",
	); err != nil {
		regErr := fmt.Errorf("dispatch: register initiative: %w", err)
		if rmErr := c.git.RemoveWorktree(repoRoot, wtPath); rmErr != nil {
			return fmt.Errorf("%w; also failed to remove worktree %s (remove manually): %v", regErr, wtPath, rmErr)
		}
		return regErr
	}

	if issue.ID == "" {
		_ = c.git.RemoveWorktree(repoRoot, wtPath)
		return fmt.Errorf("dispatch: bd create returned no id (does this bd support --json on create?)")
	}

	// Label the root epic with the initiative ID (fail-soft).
	// Skipped when --skip-epic is set.
	if !c.SkipEpic {
		if epicID := extractEpicID(body); epicID != "" {
			cmd := exec.Command("bd", "-C", repoRoot, "label", "add", epicID, issue.ID)
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(ctx.Stderr, "dispatch: warning: could not label epic %s with %s (fail-soft): %v\n", epicID, issue.ID, err)
			}
		}
	}

	// 7.5. Eagerly create the initiative's Telegram topic (fail-soft): best-
	// effort, mirrors the epic-creation fail-soft above. A machine with no
	// transport configured, or any error along the way, must not fail dispatch.
	if c.transportEnabled != nil && c.transportFor != nil && c.labelAdd != nil {
		c.createInitialTopic(ctx, issue)
	}

	// 8. Launch background DRI unless --no-launch.
	if !c.NoLaunch {
		if c.LaunchPrompt != "" {
			// Custom prompt path: substitute {id} and bypass c.launch (which
			// would prepend /dri).
			prompt := strings.ReplaceAll(c.LaunchPrompt, "{id}", issue.ID)
			// advisor defaults to "": the raw --launch-prompt path (PR-review /
			// dispatch-review-pr) is out of advisor-mode scope by default per
			// contract decision 5 (agent-teams-wvx2.1) — but that decision was
			// amended to allow an explicit opt-in via --advisor, so callers
			// that want advisor mode for a custom-prompt launch can request it.
			if err := c.launchRaw(ctx, wtPath, prompt, c.Model, c.Advisor); err != nil {
				return fmt.Errorf("dispatch: launch: %w", err)
			}
		} else {
			if err := c.launch(ctx, wtPath, issue.ID); err != nil {
				return fmt.Errorf("dispatch: launch: %w", err)
			}
		}
	}

	// 9. Output.
	if c.IDOnly {
		fmt.Fprintln(ctx.Stdout, issue.ID)
		return nil
	}

	sessionName := resolvedSlug
	fmt.Fprintf(ctx.Stdout, "initiative_id: %s\n", issue.ID)
	fmt.Fprintf(ctx.Stdout, "worktree: %s\n", wtPath)
	fmt.Fprintf(ctx.Stdout, "slug: %s\n", resolvedSlug)
	fmt.Fprintf(ctx.Stdout, "base_branch: %s\n", base)
	fmt.Fprintf(ctx.Stdout, "team: %s\n", team)
	if !c.NoLaunch {
		fmt.Fprintf(ctx.Stdout, "\nBackground session launched: %s\n", sessionName)
		printWatchControl(ctx.Stdout, sessionName)
	}
	return nil
}

// createInitialTopic eagerly opens the initiative's Telegram (or configured
// transport) forum topic and records the returned thread ref as a
// "thread:<ref>" label on the freshly-created initiative bead, so the first
// `ateam notify` reuses this topic instead of opening a second one. The
// initial message body carries the id (Initiative registered: <problem>) so
// it is discoverable in the topic even though the friendly title carries no
// id.
//
// Best-effort and fail-soft, mirroring the epic-creation fail-soft above:
// no transport configured is a silent skip (the normal state for installs
// without Telegram set up); any error resolving or sending through the
// transport is warned to ctx.Stderr. Nothing here can fail dispatch — the
// bd create above has already succeeded by the time this runs.
func (c *dispatchKong) createInitialTopic(ctx *cli.Context, issue bd.Issue) {
	if !c.transportEnabled(ctx.Home) {
		return
	}
	t, err := c.transportFor(ctx.Home)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "dispatch: warning: could not open initiative topic (fail-soft): %v\n", err)
		return
	}

	msg := transport.OutboundMessage{
		InitiativeID: issue.ID,
		Title:        issue.Title,
		Body:         "Initiative registered: " + c.Problem,
	}
	if _, err := sendAndLabelThread(ctx, issue.ID, t, msg, c.labelAdd, "dispatch"); err != nil {
		fmt.Fprintf(ctx.Stderr, "dispatch: warning: could not open initiative topic (fail-soft): %v\n", err)
	}
}

// ---- resume (kong) ----------------------------------------------------------

// resumeKong is the kong-native form of resume.
// launch/launchRaw are injected at registration time; kong:"-" keeps kong
// from treating them as flags.
type resumeKong struct {
	ID           string `arg:"" name:"id" optional:"" help:"Initiative ID to resume."`
	LaunchPrompt string `name:"launch-prompt" help:"Custom launch prompt for the session (default: /dri <id>)."`
	Model        string `name:"model" help:"Model for a --launch-prompt session (default: opus). Requires --launch-prompt."`

	launch    launchFunc    `kong:"-"`
	launchRaw rawLaunchFunc `kong:"-"`
}

// Validate checks that the required ID arg is non-empty.
func (c *resumeKong) Validate() error {
	if c.ID == "" {
		return cli.Usagef("ateam resume: <id> is required")
	}
	if c.Model != "" && c.LaunchPrompt == "" {
		return cli.Usagef("ateam resume: --model requires --launch-prompt")
	}
	return nil
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *resumeKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam resume: nil context")
	}

	issue, err := bd.ShowIssue(ctx.BD, c.ID)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam resume: no such initiative: %s\n", c.ID)
		return cli.Silent(1)
	}

	if issue.Status == "closed" {
		fmt.Fprintf(ctx.Stderr, "ateam resume: initiative %s is closed — use ateam reopen first if you want to resume it\n", c.ID)
		return cli.Silent(1)
	}

	dir := worktreePath(issue.Description)
	if dir == "" {
		fmt.Fprintf(ctx.Stderr, "ateam resume: initiative %s has no worktree: line in its description\n", c.ID)
		return cli.Silent(1)
	}

	if _, err := os.Stat(dir); err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam resume: worktree path does not exist: %s\n", dir)
		return cli.Silent(1)
	}

	var launchErr error
	if c.LaunchPrompt != "" {
		launchErr = c.launchRaw(ctx, dir, c.LaunchPrompt, c.Model, "")
	} else {
		launchErr = c.launch(ctx, dir, c.ID)
	}
	if launchErr != nil {
		return launchErr
	}

	sessionName := filepath.Base(dir)
	fmt.Fprintf(ctx.Stdout, "initiative_id: %s\n", c.ID)
	fmt.Fprintf(ctx.Stdout, "worktree: %s\n", dir)
	fmt.Fprintf(ctx.Stdout, "\nBackground session launched: %s\n", sessionName)
	printWatchControl(ctx.Stdout, sessionName)
	return nil
}

// memoryRoutingRule is the canonical memory-routing instruction appended to
// every bg-DRI session at harness-instruction altitude so it overrides the
// built-in file-memory prompt. Source of truth: contract bead agent-teams-8qm.
const memoryRoutingRule = `MEMORY ROUTING (agent-teams). Ignore the harness's built-in file-based memory feature here: do NOT write MEMORY.md or any file under a Claude memory/ directory (e.g. ~/.claude/projects/*/memory/). Persistent memory routes by kind:
- Role/process learnings (transferable across repos) -> ateam learn <role> <slug> --file <tmpfile>, where <role> is dri | planner | implementer | tester | reviewer.
- User/cross-project preferences & feedback -> ateam learn user <slug> --file <tmpfile>.
- Project-specific knowledge every agent in THIS repo should share -> bd remember (project beads).
Default to ateam learn. Use bd remember only for repo-shared project facts. Never MEMORY.md.`

// autoCompactWindowSettingsJSON is the --settings JSON argument requesting
// Claude Code's auto-compact trigger window for background DRI sessions.
//
// CLI arg, not env var: the daemon's spare-session pool claims pre-warmed
// processes via IPC rather than exec'ing fresh ones from this call's argv,
// so cmd.Env set here never reaches the claimed session (verified live) —
// but the claim payload does carry --settings, so it is honored. Caveat:
// the CLI resolves autoCompactWindow with priority env > --settings >
// client-default, so a stray CLAUDE_CODE_AUTO_COMPACT_WINDOW in the
// daemon's own environment would silently win over this value with no
// error surfaced. Not observed on this machine as of 2026-07-16 (checked
// env, shell profiles, daemon env, settings files); see agent-teams-g8xc
// for that investigation and at-0gno for this re-verification.
//
// 200000 here is a request, not the effective trigger: the CLI engine
// reserves a further fixed margin below the configured value, confirmed
// live via --debug-file on CLI 2.1.211 at ~20000 tokens (200000 requested
// -> effectiveWindow=180000; 500000 requested -> effectiveWindow=480000).
// Production DRI sessions compact at roughly 180000 tokens, not 200000.
const autoCompactWindowSettingsJSON = `{"autoCompactWindow":200000}`

// bgSessionArgs returns the argv slice (everything after "claude") for a
// background session launch. prompt is the raw positional argument passed to
// claude (e.g. "/dri at-abc123" or a custom skill invocation). model overrides
// the default "opus" model when non-empty. advisor, when non-empty, appends
// "--advisor <advisor>" to the argv (a hidden claude CLI flag taking a model
// alias). Pure: does not read env. Extracted so tests can assert the argv
// without executing the command.
func bgSessionArgs(name, prompt, model, advisor string) []string {
	if model == "" {
		model = "opus"
	}
	args := []string{
		"--bg",
		"-n", name,
		"--model", model,
		"--permission-mode", "bypassPermissions",
		"--settings", autoCompactWindowSettingsJSON,
		"--append-system-prompt", memoryRoutingRule,
	}
	if advisor != "" {
		args = append(args, "--advisor", advisor)
	}
	return append(args, prompt)
}

// driAdvisorSettings reads CLAUDE_PLUGIN_OPTION_USE_ADVISORS and
// CLAUDE_PLUGIN_OPTION_DRI_MODEL and returns the (model, advisor) pair for DRI
// session launches. CLAUDE_PLUGIN_OPTION_DRI_MODEL (default "opus" when unset
// or empty) is the "strong model" slot: when advisors are enabled (the env
// var is exactly "true"), it becomes the advisor model and the DRI session
// worker stays "sonnet"; when advisors are disabled — any other value
// (unset, "", "false", or anything not exactly "true") — it becomes the DRI
// session's own model and there is no advisor. Unit testable via t.Setenv.
// Only launchBGSession (the /dri path) calls this; the raw --launch-prompt
// path does not read these env vars — it defaults to advisor "" unless the
// caller explicitly passes --advisor (dispatchKong.Advisor).
func driAdvisorSettings() (model, advisor string) {
	driModel := os.Getenv("CLAUDE_PLUGIN_OPTION_DRI_MODEL")
	if driModel == "" {
		driModel = "opus"
	}
	if os.Getenv("CLAUDE_PLUGIN_OPTION_USE_ADVISORS") == "true" {
		return "sonnet", driModel
	}
	return driModel, ""
}

// launchFunc is the function type for launching a background DRI session.
// dispatchKong and resumeKong hold an injected field of this type so tests
// can substitute a fake without touching a package global.
type launchFunc func(ctx *cli.Context, dir, driArg string) error

// rawLaunchFunc is the function type for launching a background session with a
// custom raw prompt (no /dri prefix is added), an optional model override, and
// an optional advisor model. Used by the --launch-prompt path in dispatchKong;
// injected by tests to avoid exec-ing a real claude binary.
type rawLaunchFunc func(ctx *cli.Context, dir, prompt, model, advisor string) error

// rawLaunchBGSession launches a background claude session with an arbitrary
// prompt (no /dri prefix). model overrides the default "opus" model when
// non-empty; advisor, when non-empty, adds "--advisor <advisor>" to the argv.
// Shared by the --launch-prompt production path and tests (via injection into
// dispatchKong.launchRaw).
func rawLaunchBGSession(ctx *cli.Context, dir, prompt, model, advisor string) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return cli.Depf("ateam: 'claude' not found in PATH")
	}
	name := filepath.Base(dir)
	args := bgSessionArgs(name, prompt, model, advisor)
	cmd := exec.Command("claude", args...)
	cmd.Dir = dir
	cmd.Stdout = ctx.Stdout
	cmd.Stderr = ctx.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude --bg: %w", err)
	}
	return nil
}

// launchBGSession launches a background DRI session: prepends "/dri " to
// driArg and delegates to rawLaunchBGSession. Reads driAdvisorSettings() to
// decide whether the session runs sonnet+opus-advisor (use_advisors enabled)
// or the default opus-only. This is the ONLY launch path that reads the
// advisor env var — dispatch /dri, new-initiative, and resume all flow
// through here, per the advisor-mode-toggle contract (agent-teams-wvx2.1).
func launchBGSession(ctx *cli.Context, dir, driArg string) error {
	model, advisor := driAdvisorSettings()
	return rawLaunchBGSession(ctx, dir, "/dri "+driArg, model, advisor)
}

// printWatchControl writes the standard "Watch and control" block to w.
// sessionName is the basename of the worktree directory, which is the name
// passed to claude --bg -n.
func printWatchControl(w io.Writer, sessionName string) {
	fmt.Fprintf(w, "\nWatch and control:\n")
	fmt.Fprintf(w, "  claude agents          # list background sessions\n")
	fmt.Fprintf(w, "  claude logs %s         # recent output without attaching\n", sessionName)
	fmt.Fprintf(w, "  claude attach %s       # open it in this terminal\n", sessionName)
	fmt.Fprintf(w, "  claude stop %s         # abort it early\n", sessionName)
}

// ── shared dispatch helpers ────────────────────────────────────────────────────

// gitRunner is the subset of gitutil.Runner used by dispatchKong, extracted so
// tests can inject a fake without building a full runner.
type gitRunner interface {
	RepoRoot(dir string) (string, error)
	DefaultBranch(repoRoot string) string
	WorktreeExists(repoRoot, wtPath string) bool
	AddWorktree(repoRoot, wtPath, branch, base string) error
	RemoveWorktree(repoRoot, wtPath string) error
}

// worktreePath extracts the value of the first "worktree: <path>" line from
// description. Returns "" if no such line is present.
func worktreePath(description string) string {
	for _, line := range strings.Split(description, "\n") {
		if strings.HasPrefix(line, "worktree: ") {
			return strings.TrimRight(strings.TrimPrefix(line, "worktree: "), " \t\r")
		}
	}
	return ""
}

// extractEpicID scans body for the first "epic: <id>" line and returns the id.
// Returns "" if no such line is present.
func extractEpicID(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "epic: ") {
			return strings.TrimRight(strings.TrimPrefix(line, "epic: "), " \t\r")
		}
	}
	return ""
}
