// This file is owned by Track D (dispatch verbs).
package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/gitutil"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
	"github.com/mgt-insurance/agent-teams/internal/repoconfig"
	"github.com/mgt-insurance/agent-teams/internal/sentlog"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
	"github.com/mgt-insurance/agent-teams/internal/transport"
	"github.com/mgt-insurance/agent-teams/internal/workspaceconfig"
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
		prTitle:          defaultPRTitle,
		runtimeStart:     startRuntimeWorker,
		codexCheck:       sessionruntime.RequireCompatibleCodex,
		setup:            runWorktreeSetup,
	})
	p.AddVerb("resume", "Re-launch a background DRI session for an existing initiative.", &resumeKong{
		launch:       launchBGSession,
		launchRaw:    rawLaunchBGSession,
		runtimeStart: startRuntimeWorker,
		codexCheck:   sessionruntime.RequireCompatibleCodex,
		agentsFunc:   defaultAgentsJSONAll,
		stopSession:  defaultStopSession,
		setup:        runWorktreeSetup,
		git:          gitutil.New(),
		gitPrune:     defaultPruneWorktreesFn,
		fetchPRHead:  defaultFetchPRHeadFn,
		addDetached:  defaultAddDetachedWorktreeFn,
		ghPRCheckout: defaultGHPRCheckoutFn,
		prState:      defaultPRState,
	})
	p.AddHiddenVerb("runtime-worker", "Internal managed app-server turn submitter.", &runtimeWorkerKong{})
}

// ---- new-initiative (kong) --------------------------------------------------

// initiativeIDPattern matches agent-teams' registered-initiative id shape:
// the "at-" prefix (seen throughout this package/tests, e.g. "at-1ldm",
// "at-abc123") followed by one or more lowercase letters/digits. Used only to
// classify new-initiative's single driArg as an id (vs. a free-text problem
// statement) for the ATEAM_INITIATIVE env var — biased toward the
// false-negative direction: a real id that fails this pattern just costs a
// missing env var, whereas a false positive would inject a bogus initiative
// id into a launched session's environment.
var initiativeIDPattern = regexp.MustCompile(`^at-[a-z0-9]+$`)

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
	// driArg is an initiative id only when it matches the registry's id shape
	// (initiativeIDPattern) — a free-text problem statement carries no
	// initiative id yet (new-initiative registers one during /dri, not here),
	// and naturally fails the pattern (any space, or a first word that isn't
	// "at-something", fails to match). ATEAM_INITIATIVE is omitted rather
	// than guessed wrong.
	initiativeID := ""
	if initiativeIDPattern.MatchString(driArg) {
		initiativeID = driArg
	}
	return launch(ctx, dir, driArg, "dri", initiativeID)
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
	NoLaunch     bool   `name:"no-launch"     help:"Create worktree and register, but do not launch a background agent session."`
	LaunchPrompt string `name:"launch-prompt" help:"Custom prompt for bg session (replaces /dri <id>). {id} is replaced with initiative id."`
	SkipEpic     bool   `name:"skip-epic"     help:"Skip root epic creation in the project repo."`
	Model        string `name:"model"         help:"Model override for the background session (Claude default: claude-opus-4-8; Codex default: user config)."`
	Standby      bool   `name:"standby"       help:"Register in standby mode — the launched DRI parks on startup awaiting human direction instead of clarifying/planning."`
	Advisor      string `name:"advisor"       help:"Advisor model override for this launch (e.g. \"opus\"). Only affects the --launch-prompt path; when omitted/empty, preserves current behavior exactly (hardcoded \"\" for --launch-prompt, config.toml-derived for the /dri path)."`
	Topic        string `name:"topic"         help:"Post the registration line into a reserved shared topic (only \"reviews\") instead of opening a per-initiative topic. No thread: label is written on the initiative bead."`
	Runtime      string `name:"runtime"       help:"Agent runtime: claude, codex, or auto. Precedence: concrete flag, $ATEAM_RUNTIME, $AGENT_TEAMS_HOME/config.toml work_runtime (or review_runtime with --topic reviews), then claude. Invalid consulted config fails."`

	git          gitRunner                           `kong:"-"`
	launch       launchFunc                          `kong:"-"`
	createEpic   epicCreatorFunc                     `kong:"-"`
	launchRaw    rawLaunchFunc                       `kong:"-"`
	runtimeStart runtimeStartFunc                    `kong:"-"`
	codexCheck   func(context.Context, string) error `kong:"-"`
	setup        worktreeSetupFunc                   `kong:"-"`

	// transportFor, transportEnabled, and labelAdd back the eager Telegram
	// (or configured transport) topic creation below. Injected at
	// registration time so tests can substitute fakes without touching a
	// real transport; a test that leaves any of the three nil simply does
	// not exercise eager topic creation (mirrors createEpic's nil-check
	// pattern above).
	transportFor     transportForFunc     `kong:"-"`
	transportEnabled transportEnabledFunc `kong:"-"`
	labelAdd         labelAddFunc         `kong:"-"`

	// prTitle backs the --topic path's PR-title lookup (contract seam
	// prTitleFunc, steward_seams.go). Injected like the three above so tests
	// never spawn a real `gh`; a nil prTitle simply renders the line without
	// its title segment, which is the same fail-soft outcome as a failed
	// fetch.
	prTitle prTitleFunc `kong:"-"`
}

// worktreeSetupFunc is the injected dispatch seam for the shared
// worktree-setup implementation. It returns a populated result only for a
// configured hook that could not be provisioned.
type worktreeSetupFunc func(*cli.Context, string) (worktreeSetupResult, error)

// runWorktreeSetup reuses the standalone verb implementation while keeping
// arbitrary hook output out of primary dispatch. Dispatch emits only its
// normalized warning after receiving the structured failure result; in
// particular, hook stdout/stderr must never leak credentials into --id-only or
// regular dispatch output.
func runWorktreeSetup(ctx *cli.Context, wtPath string) (worktreeSetupResult, error) {
	setupCtx := *ctx
	setupCtx.Stdout = io.Discard
	setupCtx.Stderr = io.Discard
	return (&worktreeSetupKong{
		git:    gitutil.New(),
		runner: defaultCmdRunner,
		WtPath: wtPath,
	}).run(&setupCtx)
}

// codexDRIPrompt names the installed skill explicitly. Codex exposes plugin
// skills with a plugin-name prefix, so a bare "/dri" prompt depends on fuzzy
// trigger matching and can collide with another plugin. This prompt gives the
// model an unambiguous trigger while keeping the initiative id as durable
// input rather than conversation context.
func codexDRIPrompt(initiativeID string) string {
	return "Use the agent-teams-codex:dri skill to drive initiative " + initiativeID + "."
}

// transportEnabledFunc is the function type for checking whether a usable
// transport is configured (transport.Enabled). Injected so tests can
// substitute a fake without touching real transport config/env.
type transportEnabledFunc func(home string) bool

// Validate rejects an unrecognized --topic value. The contract
// (steward_seams.go) requires this to be a usage error rather than a silent
// fallback to per-initiative topic creation; running here — kong's Validate
// hook, before Run — means it costs no worktree and no bead.
//
// The message carries no "dispatch:" prefix of its own: unlike Run's errors,
// kong prefixes what a Validate hook returns with "ateam: dispatch: ".
func (c *dispatchKong) Validate() error {
	if c.Topic != "" && c.Topic != ReviewsHandle {
		return cli.Usagef("unknown --topic %q (supported: %s)", c.Topic, ReviewsHandle)
	}
	return nil
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *dispatchKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam dispatch: not implemented")
	}
	runtimeClass := workspaceconfig.WorkRuntime
	if c.Topic == ReviewsHandle {
		runtimeClass = workspaceconfig.ReviewRuntime
	}
	runtimeKind, err := sessionruntime.ResolveNew(c.Runtime, os.Getenv("ATEAM_RUNTIME"), func() (string, bool, error) {
		return workspaceconfig.RuntimeDefault(ctx.Home, runtimeClass)
	})
	if err != nil {
		return cli.Usagef("dispatch: %v", err)
	}
	if runtimeKind == sessionruntime.Codex && c.Advisor != "" {
		return cli.Usagef("dispatch: --advisor is only supported by the Claude runtime")
	}
	if runtimeKind == sessionruntime.Codex && c.codexCheck != nil {
		if err := c.codexCheck(context.Background(), ""); err != nil {
			return fmt.Errorf("dispatch: Codex runtime unavailable: %w", err)
		}
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

	if !repoconfig.Enabled(repoRoot) {
		fmt.Fprintf(ctx.Stderr, "dispatch: agent-teams is not enabled for %s — add a %s file there (see internal/repoconfig) or remove its \"disabled: true\" line\n",
			repoRoot, repoconfig.FileName)
		return cli.Silent(1)
	}

	// 2. Base branch.
	base := c.BaseBranch
	if base == "" {
		base = c.git.DefaultBranch(repoRoot)
	}

	// 3. Validate --problem, then derive the slug from it.
	//
	// --problem is documented as a one-line statement and is the ONLY
	// human-supplied value in the routing header — every other field is
	// machine-derived and cannot carry a newline. A multi-line value would look
	// like a canonical field on its second line and win under first-wins, which
	// is the bug this whole initiative exists to close. Reject it here, as the
	// usage error it is, rather than ten lines later once the worktree exists.
	if strings.ContainsAny(c.Problem, "\r\n") {
		return cli.Usagef("dispatch: --problem must be a single line; put multi-line prose in --body-file, which is appended below the routing header")
	}

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

	// 7. Attempt setup before every later primary lifecycle side effect. A
	// configured hook failure is reported and durably recorded, but intentionally
	// does not undo the new worktree or block registration, topic creation, or
	// launch. Unexpected setup precondition errors remain dispatch errors.
	var setupWarning string
	if c.setup != nil {
		if result, setupErr := c.setup(ctx, wtPath); setupErr != nil {
			if result.Outcome == "" {
				return fmt.Errorf("dispatch: worktree setup: %w", setupErr)
			}
			setupWarning = result.warningLine()
			fmt.Fprintf(ctx.Stderr, "dispatch: WARNING: worktree setup failed; lifecycle continues\n%s\n", setupWarning)
		}
	}

	// 8. Register the initiative via bd.
	team := gitutil.Slugify(filepath.Base(repoRoot)) + "-" + resolvedSlug
	shortTitle := c.Problem
	if len(shortTitle) > 72 {
		shortTitle = shortTitle[:72]
	}

	fields := initiative.Fields{
		Problem:  c.Problem,
		Repo:     repoRoot,
		Worktree: wtPath,
		Branch:   resolvedSlug,
		Team:     team,
		Mode:     "bg",
		Runtime:  string(runtimeKind),
		Standby:  c.Standby,
	}

	// Try to create a root epic bead in the project repo (fail-soft).
	// repoRoot is already resolved above so no extraction is needed.
	// Skipped when --skip-epic is set.
	var epicID string
	if !c.SkipEpic && c.createEpic != nil {
		if id, epicErr := c.createEpic(repoRoot, shortTitle); epicErr != nil {
			fmt.Fprintf(ctx.Stderr, "dispatch: warning: could not create root epic (fail-soft): %v\n", epicErr)
		} else {
			epicID = id
			fields.Epic = id
		}
	}

	// This is a BRAND-NEW initiative, which is the only thing initiative.New
	// may compose (see internal/initiative/doc.go, frozen item 4). Its
	// line-break rejection should be unreachable from here — --problem is
	// guarded above and every other field is machine-derived — but it is the
	// component's invariant, not ours, so the error is handled rather than
	// discarded.
	plan, err := initiative.New(fields)
	if err != nil {
		_ = c.git.RemoveWorktree(repoRoot, wtPath)
		return fmt.Errorf("dispatch: %w", err)
	}
	body := plan.Description
	if setupWarning != "" {
		body += "\n" + setupWarning + "\n"
	}

	if c.BodyFile != "" {
		extra, err := os.ReadFile(c.BodyFile)
		if err != nil {
			_ = c.git.RemoveWorktree(repoRoot, wtPath)
			return cli.Usagef("dispatch: --body-file %q: %v", c.BodyFile, err)
		}
		if len(strings.TrimSpace(string(extra))) > 0 {
			// fields is exactly the header composed above, so CollisionsIn
			// judges the body file against the keys that header actually
			// wrote — by the same rule the reader uses, not a second one
			// that happens to agree.
			for _, col := range fields.CollisionsIn(string(extra)) {
				fmt.Fprintf(ctx.Stderr,
					"dispatch: warning: --body-file line %d redefines routing field %q (first-wins — this line is IGNORED; the header's value stands): %s\n",
					col.Line, col.Key, col.Text)
			}
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

	// Label the root epic with the initiative ID (fail-soft). epicID is the
	// id createEpic returned above, empty when --skip-epic was set or the
	// fail-soft branch fired.
	if epicID != "" {
		cmd := exec.Command("bd", "-C", repoRoot, "label", "add", epicID, issue.ID)
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(ctx.Stderr, "dispatch: warning: could not label epic %s with %s (fail-soft): %v\n", epicID, issue.ID, err)
		}
	}

	// 8.5. Eagerly create the initiative's Telegram topic (fail-soft): best-
	// effort, mirrors the epic-creation fail-soft above. A machine with no
	// transport configured, or any error along the way, must not fail dispatch.
	if c.transportEnabled != nil && c.transportFor != nil && c.labelAdd != nil {
		c.createInitialTopic(ctx, issue, body)
	}

	// 9. Launch background DRI unless --no-launch.
	if !c.NoLaunch {
		if runtimeKind == sessionruntime.Codex {
			prompt := c.LaunchPrompt
			if prompt == "" {
				prompt = codexDRIPrompt(issue.ID)
			} else {
				prompt = strings.ReplaceAll(prompt, "{id}", issue.ID)
			}
			start := c.runtimeStart
			if start == nil {
				start = startRuntimeWorker
			}
			if err := start(ctx, runtimeStartRequest{
				Runtime:      runtimeKind,
				InitiativeID: issue.ID,
				Worktree:     wtPath,
				Prompt:       prompt,
				Model:        c.Model,
			}); err != nil {
				return fmt.Errorf("dispatch: launch: %w", err)
			}
		} else if c.LaunchPrompt != "" {
			// Custom prompt path: substitute {id} and bypass c.launch (which
			// would prepend /dri).
			prompt := strings.ReplaceAll(c.LaunchPrompt, "{id}", issue.ID)
			// advisor defaults to "": the raw --launch-prompt path (PR-review /
			// dispatch-review-pr) is out of advisor-mode scope by default per
			// contract decision 5 (agent-teams-wvx2.1) — but that decision was
			// amended to allow an explicit opt-in via --advisor, so callers
			// that want advisor mode for a custom-prompt launch can request it.
			if err := c.launchRaw(ctx, wtPath, prompt, c.Model, c.Advisor, "dri", issue.ID); err != nil {
				return fmt.Errorf("dispatch: launch: %w", err)
			}
		} else {
			if err := c.launch(ctx, wtPath, issue.ID, "dri", issue.ID); err != nil {
				return fmt.Errorf("dispatch: launch: %w", err)
			}
		}
	}

	// 10. Output.
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
		if runtimeKind == sessionruntime.Codex {
			printCodexControls(ctx.Stdout, ctx.Home, issue.ID, "")
		} else {
			printWatchControl(ctx.Stdout, sessionName)
		}
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
// With --topic set this opens no per-initiative topic at all: it posts a
// single line into that handle's shared topic instead (sendSharedTopicLine),
// and body carries the PR metadata that line is built from.
//
// Best-effort and fail-soft, mirroring the epic-creation fail-soft above:
// no transport configured is a silent skip (the normal state for installs
// without Telegram set up); any error resolving or sending through the
// transport is warned to ctx.Stderr. Nothing here can fail dispatch — the
// bd create above has already succeeded by the time this runs.
func (c *dispatchKong) createInitialTopic(ctx *cli.Context, issue bd.Issue, body string) {
	if !c.transportEnabled(ctx.Home) {
		return
	}
	t, err := c.transportFor(ctx.Home)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "dispatch: warning: could not open initiative topic (fail-soft): %v\n", err)
		return
	}

	if c.Topic != "" {
		c.sendSharedTopicLine(ctx, t, body)
		return
	}

	msg := transport.OutboundMessage{
		InitiativeID: issue.ID,
		Title:        issue.Title,
		Body:         "Initiative registered: " + c.Problem,
		Sender:       sentlog.KindDispatch,
	}
	returnedRef, err := sendAndLabelThread(ctx, issue.ID, t, msg, c.labelAdd, "dispatch")
	if err == nil {
		return
	}
	if returnedRef != "" {
		// Send succeeded (returnedRef is set) but the thread label never
		// landed — sendAndLabelThread already retried and logged the loud
		// stderr error. This is the "worse than no topic" case (Part A,
		// agent-teams-6rru.10 comment on .1): the topic is replyable but the
		// relay can never map a reply back to this initiative. Still
		// fail-soft for dispatch (must not fail dispatch), but say so
		// explicitly instead of reusing the generic "could not open"
		// message, which would wrongly suggest no topic exists at all.
		fmt.Fprintf(ctx.Stderr, "dispatch: warning: initiative topic (ref %s) created but UNROUTABLE — thread label never recorded (fail-soft): %v\n", returnedRef, err)
		return
	}
	fmt.Fprintf(ctx.Stderr, "dispatch: warning: could not open initiative topic (fail-soft): %v\n", err)
}

// sendSharedTopicLine handles --topic: it posts the frozen
// ReviewsStartLineFormat line into the shared, bead-less Reviews topic
// (StewardReviewsThreadPath) rather than opening a topic for this
// initiative. Deliberately writes NO "thread:" label — see the --topic
// contract in steward_seams.go for the two mechanisms (relay ambiguity and
// close-closes-it-for-everyone) that make a shared topic addressed by
// per-initiative labels actively broken.
//
// ReviewsTopicTitle, not issue.Title: the title is the topic NAME at
// creation and dispatch is in practice the first send, so passing the
// initiative's own title would name the shared topic after whichever PR
// happened to be reviewed first.
//
// Fail-soft like its caller: a missing/failed send is warned, never fatal.
func (c *dispatchKong) sendSharedTopicLine(ctx *cli.Context, t transport.Transport, body string) {
	fields := initiative.JSONFields(bd.Issue{Description: body})
	prNumber, _ := fields["pr-number"].(string)
	ownerRepo, _ := fields["pr-repo"].(string)
	prURL, _ := fields["pr-url"].(string)

	// Unreachable from the two real callers (route.go's spawnReviewInitiative
	// and the dispatch-review-pr skill both always write all three). Unlike
	// the PR title, which is optional by design, these three are the line's
	// identity and its affordance — so posting a half-rendered line into the
	// feed this initiative exists to de-noise is worse than posting nothing.
	// The warning names the absent keys: without them, a caller that forgot
	// one sees only a dispatch that succeeded with no line in the topic.
	var missing []string
	for _, f := range []struct{ key, value string }{
		{"pr-number", prNumber},
		{"pr-repo", ownerRepo},
		{"pr-url", prURL},
	} {
		if f.value == "" {
			missing = append(missing, f.key)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(ctx.Stderr, "dispatch: warning: --topic %s but the initiative body has no %s — nothing posted to the shared topic (fail-soft)\n", c.Topic, strings.Join(missing, ", "))
		return
	}

	msg := transport.OutboundMessage{
		InitiativeID: ReviewsHandle,
		Title:        ReviewsTopicTitle,
		Body:         fmt.Sprintf(ReviewsStartLineFormat, prNumber, filepath.Base(ownerRepo), c.titleSegment(ctx, ownerRepo, prNumber), prURL),
		Sender:       sentlog.KindDispatch,
	}
	if _, err := sendSharedTopic(ctx, StewardReviewsThreadPath(ctx), t, msg, "ateam dispatch"); err != nil {
		fmt.Fprintf(ctx.Stderr, "dispatch: warning: could not post to the shared %s topic (fail-soft): %v\n", c.Topic, err)
	}
}

// titleSegment builds ReviewsStartLineFormat's third argument: " — " plus the
// PR title, or "" when it can't be had. Every failure mode — no seam
// injected, an unparseable pr-number, a gh error, an empty title — collapses
// to "", which renders the line without a dangling separator. Mandated by
// the contract: a title fetch may never fail a dispatch.
func (c *dispatchKong) titleSegment(ctx *cli.Context, ownerRepo, prNumber string) string {
	if c.prTitle == nil {
		return ""
	}
	n, err := strconv.Atoi(prNumber)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "dispatch: warning: pr-number %q is not a number — posting without the PR title (fail-soft)\n", prNumber)
		return ""
	}
	title, err := c.prTitle(ownerRepo, n)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "dispatch: warning: could not fetch PR title — posting without it (fail-soft): %v\n", err)
		return ""
	}
	if title == "" {
		return ""
	}
	return " — " + title
}

// prTitleTimeout bounds defaultPRTitle's gh subprocess (contract: 10s).
const prTitleTimeout = 10 * time.Second

// defaultPRTitle is the production prTitleFunc: `gh pr view`, bounded at
// prTitleTimeout so a hung subprocess can never stall a dispatch that has
// already succeeded.
func defaultPRTitle(ownerRepo string, prNumber int) (string, error) {
	cmdCtx, cancel := context.WithTimeout(context.Background(), prTitleTimeout)
	defer cancel()
	out, err := exec.CommandContext(cmdCtx, "gh", "pr", "view", strconv.Itoa(prNumber),
		"--repo", ownerRepo, "--json", "title", "-q", ".title").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ---- resume (kong) ----------------------------------------------------------

// resumeKong is the kong-native form of resume.
// launch/launchRaw are injected at registration time; kong:"-" keeps kong
// from treating them as flags.
type resumeKong struct {
	ID           string `arg:"" name:"id" optional:"" help:"Initiative ID to resume."`
	LaunchPrompt string `name:"launch-prompt" help:"Custom launch prompt for the session (default: the runtime's DRI skill with <id>)."`
	Model        string `name:"model" help:"Model for a --launch-prompt session (Claude default: claude-opus-4-8; Codex default: user config). Requires --launch-prompt."`
	Runtime      string `name:"runtime" help:"Assert the initiative runtime (claude or codex)."`
	Supersede    bool   `name:"supersede" help:"Stop any session(s) already live on this initiative first, then launch. Without this flag, resume refuses when a live session exists."`

	launch       launchFunc                          `kong:"-"`
	launchRaw    rawLaunchFunc                       `kong:"-"`
	runtimeStart runtimeStartFunc                    `kong:"-"`
	codexCheck   func(context.Context, string) error `kong:"-"`

	// agentsFunc/stopSession back the duplicate-live-session guard below.
	// Nil in a caller that hasn't opted in (e.g. the mail-path escalation in
	// messaging.go's defaultResume, hardened separately by agent-teams-ndr4.3)
	// skips the guard entirely, preserving that caller's existing behavior.
	agentsFunc  agentsJSONFunc  `kong:"-"`
	stopSession stopSessionFunc `kong:"-"`

	// setup/git/gitPrune/fetchPRHead/addDetached/ghPRCheckout/prState back
	// recreateWorktree below (contract agent-teams-8st0.18 item 2,
	// agent-teams-8st0.28): recreating a missing worktree at the same path,
	// gated by a PR-state probe for a review-shaped initiative. Unlike
	// agentsFunc/stopSession above, these are NOT optional-feature seams —
	// every production registration (RegisterDispatchKong) sets all seven,
	// so recreateWorktree calls them directly with no nil-fallback, matching
	// dispatchKong.git's own convention. A test that exercises the
	// missing-worktree path must set whichever of these its scenario
	// reaches; one that never does (dir exists) never touches them.
	setup        worktreeSetupFunc       `kong:"-"`
	git          resumeWorktreeGit       `kong:"-"`
	gitPrune     pruneWorktreesFunc      `kong:"-"`
	fetchPRHead  fetchPRHeadFunc         `kong:"-"`
	addDetached  addDetachedWorktreeFunc `kong:"-"`
	ghPRCheckout ghPRCheckoutFunc        `kong:"-"`
	prState      prStateFunc             `kong:"-"`
}

// resumeWorktreeGit is the git surface resumeKong needs to recreate a
// missing worktree (contract agent-teams-8st0.18 item 2) — a separate,
// smaller interface from dispatchKong's gitRunner above: resume never
// creates a worktree off the default branch (RULED 2026-09-28: a review
// branch is NEVER created that way), so it has no DefaultBranch/
// WorktreeExists member, and it needs two members gitRunner doesn't
// (BranchExists, AttachWorktree). *gitutil.Runner already implements both
// interfaces structurally; extending gitRunner itself instead would force
// every existing fake built against it (other tracks' test files included)
// to grow two new methods it has no use for.
type resumeWorktreeGit interface {
	AddWorktree(repoRoot, wtPath, branch, base string) error
	BranchExists(repoRoot, ref string) bool
	AttachWorktree(repoRoot, wtPath, branch string) error
}

// pruneWorktreesFunc, fetchPRHeadFunc, addDetachedWorktreeFunc, and
// ghPRCheckoutFunc are resumeKong's remaining injected git/gh seams for
// recreateWorktree — kept as bare func types (mirroring worktreeSetupFunc/
// prTitleFunc above) rather than added to resumeWorktreeGit or gitutil.go,
// since they are resume-specific one-off operations, not general-purpose
// git plumbing other verbs would reuse.
type pruneWorktreesFunc func(repoRoot string) error
type fetchPRHeadFunc func(repoRoot, branch string, prNumber int) error
type addDetachedWorktreeFunc func(repoRoot, wtPath string) error
type ghPRCheckoutFunc func(wtPath, branch string, prNumber int) error

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

	f := initiative.Of(issue)
	runtimeKind, err := sessionruntime.AssertStored(f.Runtime, c.Runtime)
	if err != nil {
		return cli.Usagef("ateam resume: %v", err)
	}
	if runtimeKind == sessionruntime.Codex && c.codexCheck != nil {
		if err := c.codexCheck(context.Background(), ""); err != nil {
			return fmt.Errorf("ateam resume: Codex runtime unavailable: %w", err)
		}
	}
	dir := f.Worktree
	if dir == "" {
		fmt.Fprintf(ctx.Stderr, "ateam resume: initiative %s has no worktree: line in its description\n", c.ID)
		return cli.Silent(1)
	}

	if f.Repo != "" && !repoconfig.Enabled(f.Repo) {
		fmt.Fprintf(ctx.Stderr, "ateam resume: agent-teams is not enabled for %s — add a %s file there (see internal/repoconfig) or remove its \"disabled: true\" line\n",
			f.Repo, repoconfig.FileName)
		return cli.Silent(1)
	}

	// prURL/isReview is CONTRACT agent-teams-8st0.18's review-shaped
	// predicate (initiative.ReviewPRURL — same one hung_scan.go's
	// hungScanEntry.ReviewPRURL uses): a non-empty "pr-url:" Description
	// line. Computed once here, ahead of both consumers below — the
	// missing-worktree recreation gate (item 2) and the default-prompt
	// selection (item 3) — so a malformed pr-url line is handled once
	// rather than by two independent parses that could disagree.
	prURL, isReview := initiative.ReviewPRURL(issue)

	if _, err := os.Stat(dir); err != nil {
		if err := c.recreateWorktree(ctx, f, dir, isReview, prURL); err != nil {
			return err
		}
	}

	// Duplicate-live-session guard (agent-teams-ndr4.1): resume used to launch
	// unconditionally, which is the direct cause of duplicate concurrent
	// sessions on one initiative — any caller (steward recovery, human, mail
	// fall-through) racing a live session spawns a second one, and both can
	// act (e.g. both post reviews). agentsFunc is nil for callers that
	// haven't opted into this guard yet; see the field doc comment.
	//
	// Fails CLOSED on a query error (agentsFunc itself, or the --supersede
	// re-query below): if we can't enumerate sessions we can't rule out a
	// duplicate — and can't enumerate what to stop either — so refuse rather
	// than risk launching a second session (mirrors reapOrphansKong.Run).
	if c.agentsFunc != nil {
		liveSessions := func() ([]agentSession, error) {
			sessions, err := c.agentsFunc()
			if err != nil {
				return nil, err
			}
			var live []agentSession
			for _, s := range matchSessionsForInitiative(sessions, issue) {
				if s.PID != nil {
					live = append(live, s)
				}
			}
			return live, nil
		}

		live, err := liveSessions()
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "ateam resume: could not verify live sessions for %s (%v); refusing to avoid a possible duplicate — retry, or stop the session yourself\n", c.ID, err)
			return cli.Silent(1)
		}
		if len(live) > 0 {
			ids := make([]string, len(live))
			for i, s := range live {
				ids[i] = sessionStopID(s)
			}
			if !c.Supersede {
				fmt.Fprintf(ctx.Stderr, "ateam resume: initiative %s already has a live session: %s — pass --supersede to stop it and relaunch\n",
					c.ID, strings.Join(ids, ", "))
				return cli.Silent(1)
			}
			stop := c.stopSession
			if stop == nil {
				stop = defaultStopSession
			}
			for _, id := range ids {
				if err := stop(id); err != nil {
					fmt.Fprintf(ctx.Stderr, "ateam resume: warning: stop %s failed (%v); continuing\n", id, err)
				}
			}

			// Verify the stop(s) actually worked before launching: a
			// stopSession error can be a benign race (session already died
			// on its own — re-query shows it gone, safe to launch) or a real
			// failure (session still alive — launching now would duplicate
			// it). Only the re-query can distinguish the two.
			stillLive, err := liveSessions()
			if err != nil {
				fmt.Fprintf(ctx.Stderr, "ateam resume: could not verify live sessions for %s (%v); refusing to avoid a possible duplicate — retry, or stop the session yourself\n", c.ID, err)
				return cli.Silent(1)
			}
			if len(stillLive) > 0 {
				stillIDs := make([]string, len(stillLive))
				for i, s := range stillLive {
					stillIDs[i] = sessionStopID(s)
				}
				fmt.Fprintf(ctx.Stderr, "ateam resume: session %s still live after supersede stop; refusing to avoid a duplicate\n", strings.Join(stillIDs, ", "))
				return cli.Silent(1)
			}
		}
	}

	var launchErr error
	var codexSession string
	if runtimeKind == sessionruntime.Codex {
		if len(f.Sessions) == 0 {
			fmt.Fprintf(ctx.Stderr, "ateam resume: Codex initiative %s has no session: thread id yet\n", c.ID)
			return cli.Silent(1)
		}
		codexSession = f.Sessions[len(f.Sessions)-1]
		prompt := c.LaunchPrompt
		if prompt == "" {
			prompt = codexDRIPrompt(c.ID)
		}
		start := c.runtimeStart
		if start == nil {
			start = startRuntimeWorker
		}
		launchErr = start(ctx, runtimeStartRequest{
			Runtime:      runtimeKind,
			InitiativeID: c.ID,
			Worktree:     dir,
			Prompt:       prompt,
			Model:        c.Model,
			ResumeID:     codexSession,
		})
	} else if c.LaunchPrompt != "" {
		launchErr = c.launchRaw(ctx, dir, c.LaunchPrompt, c.Model, "", "dri", c.ID)
	} else if isReview {
		// CONTRACT agent-teams-8st0.18 item 3: a review-shaped initiative
		// resumed with no explicit --launch-prompt runs the review-pr skill
		// (sonnet), not a full DRI — mirrors route.go's spawnReviewInitiative
		// and dispatch-review-pr's own launch-prompt choice for the same
		// initiative kind.
		launchErr = c.launchRaw(ctx, dir, "/agent-teams:review-pr "+c.ID, "sonnet", "", "dri", c.ID)
	} else {
		launchErr = c.launch(ctx, dir, c.ID, "dri", c.ID)
	}
	if launchErr != nil {
		return launchErr
	}

	sessionName := filepath.Base(dir)
	fmt.Fprintf(ctx.Stdout, "initiative_id: %s\n", c.ID)
	fmt.Fprintf(ctx.Stdout, "worktree: %s\n", dir)
	fmt.Fprintf(ctx.Stdout, "\nBackground session launched: %s\n", sessionName)
	if runtimeKind == sessionruntime.Codex {
		printCodexControls(ctx.Stdout, ctx.Home, c.ID, codexSession)
	} else {
		printWatchControl(ctx.Stdout, sessionName)
	}
	return nil
}

// recreateWorktree implements CONTRACT agent-teams-8st0.18 item 2's resume
// side (agent-teams-8st0.28): dir does not exist, so rebuild it at the same
// path before resume tries to launch into it. RULED 2026-09-28: a review
// worktree is NEVER created from the default branch — a REVIEW worktree is
// always attached to an existing local branch or built from the PR's own
// head, never from f.Repo's default branch. Returns errNothingToReview,
// unwrapped, when a review-shaped initiative's PR already turned out to be
// MERGED or CLOSED — callers (mail send, the hung-scan backstop) key off
// that exact sentinel via errors.Is.
func (c *resumeKong) recreateWorktree(ctx *cli.Context, f initiative.Fields, dir string, isReview bool, prURL string) error {
	var ownerRepo string
	var prNumber int
	if isReview {
		var ok bool
		ownerRepo, prNumber, ok = parsePrURL(prURL)
		if !ok {
			fmt.Fprintf(ctx.Stderr, "ateam resume: initiative %s has an unparsable pr-url %q; cannot probe PR state or recreate its worktree\n", c.ID, prURL)
			return cli.Silent(1)
		}
		// Probe PR state FIRST, before touching git at all: a MERGED/CLOSED
		// PR means there is nothing left to review, so no worktree should be
		// created even if a stale local branch is still lying around.
		state, probed := probeReviewPRState(ctx, c.prState, ownerRepo, prNumber)
		if !probed {
			fmt.Fprintf(ctx.Stderr, "ateam resume: could not determine PR state for %s#%d; retry once gh is reachable\n", ownerRepo, prNumber)
			return cli.Silent(1)
		}
		if state == ghPRStateMerged || state == ghPRStateClosed {
			return errNothingToReview
		}
	}

	if f.Repo == "" {
		fmt.Fprintf(ctx.Stderr, "ateam resume: initiative %s has no repo: line in its description; cannot recreate worktree %s\n", c.ID, dir)
		return cli.Silent(1)
	}
	if f.Branch == "" {
		fmt.Fprintf(ctx.Stderr, "ateam resume: initiative %s has no branch: line in its description; cannot recreate worktree %s\n", c.ID, dir)
		return cli.Silent(1)
	}

	// Best-effort: stale worktree administrative entries (the directory was
	// removed directly rather than via `git worktree remove`) would
	// otherwise make the `git worktree add`/`AttachWorktree` calls below
	// fail on bookkeeping rather than on anything about the branch itself.
	// A failure here is warned, never fatal — the add below still gets a
	// real chance to run and reports its own error if prune's staleness
	// wasn't actually the problem.
	if err := c.gitPrune(f.Repo); err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam resume: warning: git worktree prune failed (continuing): %v\n", err)
	}

	var recreateErr error
	switch {
	case c.git.BranchExists(f.Repo, "refs/heads/"+f.Branch):
		recreateErr = c.git.AttachWorktree(f.Repo, dir, f.Branch)
	case isReview:
		recreateErr = c.recreateReviewWorktree(f.Repo, dir, f.Branch, prNumber)
	case c.git.BranchExists(f.Repo, "refs/remotes/origin/"+f.Branch):
		recreateErr = c.git.AddWorktree(f.Repo, dir, f.Branch, "origin/"+f.Branch)
	default:
		fmt.Fprintf(ctx.Stderr, "ateam resume: branch %q not found locally or at origin for %s; cannot recreate worktree (work may be gone)\n", f.Branch, c.ID)
		return cli.Silent(1)
	}
	if recreateErr != nil {
		fmt.Fprintf(ctx.Stderr, "ateam resume: recreate worktree for %s: %v\n", c.ID, recreateErr)
		return cli.Silent(1)
	}

	if _, err := c.setup(ctx, dir); err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam resume: warning: worktree setup failed after recreate (continuing): %v\n", err)
	}
	return nil
}

// recreateReviewWorktree implements the REVIEW branch's two-strategy
// worktree build (contract item 2, no local branch case): fetch the PR's own
// head ref directly first — never the default branch. If that fails (e.g. a
// fork PR whose head ref the origin remote doesn't expose), fall back to an
// ad-hoc DETACHED worktree (no branch at all, so it can't violate the
// never-branch-from-default rule either) plus `gh pr checkout`, which
// resolves a fork's remote itself. Both failing is reported to the caller,
// which turns it into a loud error and creates nothing further; the detached
// worktree left behind by a fetch-succeeded-checkout-failed split is not
// cleaned up here — a rare double failure, left for a human to remove.
func (c *resumeKong) recreateReviewWorktree(repoRoot, wtPath, branch string, prNumber int) error {
	if err := c.fetchPRHead(repoRoot, branch, prNumber); err == nil {
		return c.git.AttachWorktree(repoRoot, wtPath, branch)
	}
	if err := c.addDetached(repoRoot, wtPath); err != nil {
		return fmt.Errorf("add detached worktree: %w", err)
	}
	if err := c.ghPRCheckout(wtPath, branch, prNumber); err != nil {
		return fmt.Errorf("fetch pull/%d/head failed and the gh pr checkout fallback also failed: %w", prNumber, err)
	}
	return nil
}

// probeReviewPRState answers whether a review-shaped initiative's PR is
// already MERGED or CLOSED, reusing hung_tick.go's own PR-state probe cache
// (hungPRStateCache/defaultPRState) rather than a second cache file — same
// question, same TTL, one persisted cache regardless of which verb asks.
// probed==false means "could not determine" (e.g. a gh failure): the caller
// must treat that as inconclusive, never as OPEN.
func probeReviewPRState(ctx *cli.Context, prState prStateFunc, ownerRepo string, prNumber int) (state string, probed bool) {
	if prState == nil {
		return "", false
	}
	cachePath := hungPRStateCachePath(ctx)
	cache := loadHungPRStateCache(cachePath)
	now := time.Now()
	key := prStateCacheKey(ownerRepo, prNumber)
	if cached, fresh := cache.lookup(key, now, hungPRStateTTL); fresh {
		return cached, true
	}
	result, err := prState(ownerRepo, prNumber)
	if err != nil {
		return "", false
	}
	cache.put(key, result, now)
	if saveErr := saveHungPRStateCache(cachePath, cache); saveErr != nil {
		fmt.Fprintf(ctx.Stderr, "ateam resume: warning: persist pr-state cache: %v\n", saveErr)
	}
	return result, true
}

// defaultPruneWorktreesFn removes git's administrative entries for worktree
// paths that no longer exist on disk, so a subsequent `git worktree add`/
// `AttachWorktree` at a path a human or process deleted directly (rather
// than via `git worktree remove`) does not fail on stale bookkeeping.
// Equivalent to: git -C <repoRoot> worktree prune
func defaultPruneWorktreesFn(repoRoot string) error {
	out, err := exec.Command("git", "-C", repoRoot, "worktree", "prune").CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree prune: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// defaultFetchPRHeadFn fetches a PR's own head commit into a local branch —
// resume's first REVIEW-worktree-recreation strategy (contract item 2): the
// PR's head, never the base/default branch.
// Equivalent to: git -C <repoRoot> fetch origin pull/<prNumber>/head:<branch>
func defaultFetchPRHeadFn(repoRoot, branch string, prNumber int) error {
	refspec := fmt.Sprintf("pull/%d/head:%s", prNumber, branch)
	out, err := exec.Command("git", "-C", repoRoot, "fetch", "origin", refspec).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch origin %s: %s", refspec, strings.TrimSpace(string(out)))
	}
	return nil
}

// defaultAddDetachedWorktreeFn adds a worktree at wtPath in detached-HEAD
// state — no branch of any kind, so it can never violate the never-branch-
// from-default rule. Used only as the staging step for the gh pr checkout
// fallback (defaultGHPRCheckoutFn), when fetching the PR head directly
// failed (e.g. a fork PR the remote doesn't expose).
// Equivalent to: git -C <repoRoot> worktree add --detach <wtPath>
func defaultAddDetachedWorktreeFn(repoRoot, wtPath string) error {
	out, err := exec.Command("git", "-C", repoRoot, "worktree", "add", "--detach", wtPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add --detach %s: %s", wtPath, strings.TrimSpace(string(out)))
	}
	return nil
}

// defaultGHPRCheckoutFn checks out prNumber's PR as local branch name inside
// the worktree at wtPath — the fork-PR fallback (gh resolves a fork's own
// remote itself, which a plain `git fetch origin pull/<n>/head` cannot).
// Equivalent to (run inside wtPath): gh pr checkout <prNumber> --branch <branch>
func defaultGHPRCheckoutFn(wtPath, branch string, prNumber int) error {
	cmd := exec.Command("gh", "pr", "checkout", strconv.Itoa(prNumber), "--branch", branch)
	cmd.Dir = wtPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gh pr checkout %d --branch %s: %s", prNumber, branch, strings.TrimSpace(string(out)))
	}
	return nil
}

// memoryRoutingRule is the canonical memory-routing instruction appended to
// every bg-DRI session at harness-instruction altitude so it overrides the
// built-in file-memory prompt. Source of truth: contract bead agent-teams-8qm.
const memoryRoutingRule = `MEMORY ROUTING (agent-teams). Ignore the harness's built-in file-based memory feature here: do NOT write MEMORY.md or any file under a Claude memory/ directory (e.g. ~/.claude/projects/*/memory/). Persistent memory routes by kind:
- Role/process learnings (transferable across repos) -> ateam learn <role> <slug> --file <tmpfile>, where <role> is dri | planner | implementer | tester | reviewer | investigator.
- User/cross-project preferences & feedback -> ateam learn user <slug> --file <tmpfile>.
- Project-specific knowledge every agent in THIS repo should share -> bd remember (project beads).
Default to ateam learn. Use bd remember only for repo-shared project facts. Never MEMORY.md.`

// driGuardrails is the terse, always-on DRI hard-guardrail digest appended
// alongside memoryRoutingRule on every bg DRI launch/relaunch, so the hard
// rules ride the same compaction-immune --append-system-prompt channel as the
// memory-routing rule (--append-system-prompt is resent every turn, never
// summarized away by /compact — verified live, agent-teams-kxlb.1). This is a
// minimal turn-1 FLOOR, not a substitute for the full /dri skill: its first
// bullet tells a compacted DRI to re-invoke the skill via the Skill tool to
// restore full fidelity. DRI-owned instruction content, carried verbatim from
// the contract bead (agent-teams-kxlb.5, amending kxlb.2); do not reword
// without checking with the DRI/planner first.
const driGuardrails = "DRI HARD GUARDRAILS (floor — re-invoke the /dri skill to restore full guidance):\n" +
	"- If the /dri skill is not in your context (after compaction), re-invoke it via the Skill tool BEFORE any orchestration action.\n" +
	"- You ORCHESTRATE; never implement and never run live verification yourself — delegate to role subagents.\n" +
	"- This checkout IS your isolation: never EnterWorktree, never dirty it. For every delegated worktree: create it, attempt `ateam worktree-setup <absolute-path>` to completion, report any failure (echo and durable initiative note), record the track, then spawn; setup/report failure is fail-open, so continue and include the warning in the spawn brief. Never use a hand-rolled script.\n" +
	"- Never merge without explicit human confirmation; leave delivered work OPEN and review-gated.\n" +
	"- Work beads live in the project repo under the epic; the global workspace (ateam) is initiative tracking only."

// driSystemPromptAppend is the full value passed to --append-system-prompt on
// every bg DRI launch: memoryRoutingRule followed by driGuardrails,
// concatenated into ONE string. Repeated --append-system-prompt flags are not
// assumed to work, so both instruction sets must ride the same flag value.
const driSystemPromptAppend = memoryRoutingRule + "\n\n" + driGuardrails

// driDefaultModel is the model background sessions launch on when no explicit
// override is supplied. Pinned to the concrete id claude-opus-4-8 rather than
// the bare "opus" alias so the default stays put instead of silently following
// whatever "latest opus" resolves to. No [1m] suffix: claude-opus-4-8 is
// natively 1M-context on a first-party endpoint, so the alias-vs-id choice does
// not change the context window. The suffix is a second route to the same
// window, and it does not survive the one case that really does clamp to 200000
// (long-context credits exhausted), so it buys nothing here.
// Kept as a constant so this default stays in sync with the mirrored
// claudeDriModelDefault literal in internal/workspaceconfig/config.go
// (config.go:39-43) — that package keeps its own separate literal rather than
// importing internal/verbs, so the two constants must be updated together.
const driDefaultModel = "claude-opus-4-8"

// bgSessionEnv is the "env" map merged into a background session's --settings
// JSON, publishing the role-signal contract (agent-teams-142k.1): ATEAM_ROLE
// (open enum, v1 values "dri"/"steward") and ATEAM_INITIATIVE (the initiative
// id, when the launcher knows it). Consumers MUST treat unknown ATEAM_ROLE
// values and its absence identically — a generic fallback, never an error.
// Field order (Role before Initiative) matches the contract's documented
// example JSON exactly.
type bgSessionEnv struct {
	Role       string `json:"ATEAM_ROLE,omitempty"`
	Initiative string `json:"ATEAM_INITIATIVE,omitempty"`
}

// bgSessionSettings is the --settings JSON payload for a background session
// launch: an optional env map, plus autoCompactEnabled/autoCompactWindow when
// a window was configured and parses to a real token count.
//
// autoCompactWindow ALSO rides here now (agent-teams-4pc5.3), not only as the
// claude CLI's own --autocompact flag (still appended to argv by bgSessionArgs
// whenever the resolved window is non-empty — see driAutoCompactWindow, which
// reads config.toml's auto_compact_window key). Both are needed, for two
// independent reasons proven empirically on real claimed-spare bg sessions:
//
//  1. Most bg launches are served by the daemon's pre-warmed spare pool
//     (`claude bg-spare`), not a fresh exec from this argv. A claimed spare's
//     compaction window was fixed at its generic warm-up exec (no
//     --autocompact); the CLAUDE-flag only reaches a genuinely cold launch.
//     --settings, by contrast, IS honored by the claim payload — so the
//     window has to travel there too, or every pool-served session (which is
//     nearly all of them) silently keeps the default ~967k window.
//  2. autoCompactWindow alone is not enough even via --settings: the CLI only
//     compacts when autoCompactEnabled is also true, and the user's global
//     default for that flag is false. Without autoCompactEnabled:true riding
//     alongside, a pinned window sits there unused.
//
// Evidence (agent-teams-4pc5.3, 2026-08-26): {"autoCompactWindow":100000,
// "env":{...}} on a claimed spare climbed to ~102k and never compacted;
// {"autoCompactEnabled":true,"autoCompactWindow":100000,"env":{...}} on
// another claimed spare compacted right on schedule.
//
// The CLI resolves the window as (2.1.222, function qX):
//
//	W  = first match of: CLAUDE_CODE_AUTO_COMPACT_WINDOW env >
//	     configured (the --autocompact flag OR an autoCompactWindow settings
//	     key, merged by gFu — same slot, both report source "settings") >
//	     server clientdata > experiment gate > model-default (200000, reached
//	     ONLY when the model's real window is under 1M) > per-model table
//	     (claude-sonnet-5 -> 967000, or 500000 on the remote_cowork /
//	     local-agent surfaces) > auto (the model's full window)
//	W  = min(realModelWindow, W)
//	effective = W - min(maxOutputTokens, 20000)
//	compact  at effective - 13000
//
// The rest of this comment is the argument for the DEFAULT (empty, i.e. both
// --autocompact and the settings keys omitted) — not an argument against the
// knob existing. Sending nothing falls through to "auto" on a 1M-context
// model — the model-default tier is gated on the real window being under 1M,
// so a 1M model skips it — giving a ~967000 trigger that tracks whatever
// model the session actually runs on. Any value pinned here can only lower
// that: this call site used to request 200000, which produced a 167000
// trigger (200000 - 20000 - 13000, matching compactions observed at 167,030 /
// 167,041 / 167,052) — a self-inflicted ~6x reduction of the trigger the same
// session reaches with nothing set.
//
// The window is not where the waste is, either. Measured over 51 compactions
// in one three-day DRI session, the first API request after a compaction
// already carried a median 101k tokens (max 169,710): the fixed prefix plus
// the re-injected tool/skill/agent/hook listings are re-established every
// time. Shrinking that beats widening the window.
type bgSessionSettings struct {
	Env                *bgSessionEnv `json:"env,omitempty"`
	AutoCompactEnabled *bool         `json:"autoCompactEnabled,omitempty"`
	AutoCompactWindow  *int          `json:"autoCompactWindow,omitempty"`
}

// parseAutoCompactWindowTokens parses the free-form value accepted by the
// claude CLI's own --autocompact flag into a token count, for use ONLY in
// deciding whether bgSessionSettingsJSON also pins the window into --settings
// (see bgSessionSettings). The CLI flag itself (bgSessionArgs) still carries
// the raw value through verbatim, unparsed — that flag remains the sole
// source of validation/error-reporting for a value the CLI itself rejects;
// this parser never invents validation beyond the forms below and simply
// declines (ok=false) to duplicate a value it cannot confidently turn into an
// integer token count.
//
// Accepted forms, mirroring what --autocompact documents:
//   - a plain integer token count ("300000")
//   - a k/m suffix ("300k" -> 300000, "1m" -> 1000000)
//   - a bare integer from 100-1000, read as thousands-of-tokens shorthand
//     ("200" -> 200000)
//   - the literal "auto"
//
// "auto", "", and anything else that doesn't match one of the forms above all
// return ok=false ("no window" — omit both settings keys and the flag, i.e.
// today's default).
func parseAutoCompactWindowTokens(value string) (tokens int, ok bool) {
	tokens64, automatic, err := parseAutoCompactWindowValue(value)
	if err != nil || automatic || (strconv.IntSize == 32 && tokens64 > 1<<31-1) {
		return 0, false
	}
	return int(tokens64), true
}

// parseAutoCompactWindowValue is the shared strict parser for the Claude
// --autocompact forms. The Claude settings wrapper above deliberately folds
// both "auto" and invalid values into omission, while Codex callers use the
// distinct automatic result and error to implement explicit precedence.
func parseAutoCompactWindowValue(value string) (tokens int64, automatic bool, err error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return 0, false, fmt.Errorf("empty auto-compact window")
	}
	if strings.EqualFold(v, "auto") {
		return 0, true, nil
	}
	lower := strings.ToLower(v)
	multiplier := int64(1)
	numPart := lower
	switch {
	case strings.HasSuffix(lower, "k"):
		multiplier = 1000
		numPart = strings.TrimSuffix(lower, "k")
	case strings.HasSuffix(lower, "m"):
		multiplier = 1_000_000
		numPart = strings.TrimSuffix(lower, "m")
	}
	n, err := strconv.ParseInt(numPart, 10, 64)
	if err != nil || n <= 0 {
		return 0, false, fmt.Errorf("invalid auto-compact window %q", value)
	}
	if multiplier == 1 && n >= 100 && n <= 1000 {
		// Bare number in the CLI's thousands-shorthand range.
		multiplier = 1000
	}
	const maxInt64 = int64(1<<63 - 1)
	if n > maxInt64/multiplier {
		return 0, false, fmt.Errorf("auto-compact window %q overflows signed 64-bit tokens", value)
	}
	return n * multiplier, false, nil
}

// bgSessionSettingsJSON builds the --settings JSON argument for a background
// session launch: an "env" map carrying ATEAM_ROLE/ATEAM_INITIATIVE when
// either is non-empty, plus autoCompactEnabled/autoCompactWindow when
// autoCompactWindow parses to a real token count (parseAutoCompactWindowTokens
// — see bgSessionSettings for why both keys travel together and why they must
// ride --settings rather than rely on the argv flag alone). Returns "" only
// when there is nothing at all to carry (no role, no initiativeID, no usable
// window) — the caller then omits the --settings flag entirely rather than
// passing an empty object. role and initiativeID are independent:
// initiativeID is omitted whenever the launcher doesn't know the initiative id
// (e.g. new-initiative given a bare problem statement, or the steward, which
// is fleet-scoped and never carries one).
//
// CLI arg, not env var: the daemon's spare-session pool claims pre-warmed
// processes via IPC rather than exec'ing fresh ones from this call's argv, so
// cmd.Env set here never reaches the claimed session (verified live) — but the
// claim payload does carry --settings, so it is honored.
func bgSessionSettingsJSON(role, initiativeID, autoCompactWindow string) string {
	tokens, windowOK := parseAutoCompactWindowTokens(autoCompactWindow)
	if role == "" && initiativeID == "" && !windowOK {
		return ""
	}
	var settings bgSessionSettings
	if role != "" || initiativeID != "" {
		settings.Env = &bgSessionEnv{Role: role, Initiative: initiativeID}
	}
	if windowOK {
		enabled := true
		settings.AutoCompactEnabled = &enabled
		settings.AutoCompactWindow = &tokens
	}
	b, err := json.Marshal(settings)
	if err != nil {
		// All fields are plain strings/bools/ints behind pointers — Marshal
		// cannot fail here.
		return ""
	}
	return string(b)
}

// bgSessionArgs returns the argv slice (everything after "claude") for a
// background session launch. prompt is the raw positional argument passed to
// claude (e.g. "/dri at-abc123" or a custom skill invocation). model overrides
// driDefaultModel when non-empty. advisor, when non-empty, appends
// "--advisor <advisor>" to the argv (a hidden claude CLI flag taking a model
// alias). role and initiativeID are merged into --settings via
// bgSessionSettingsJSON. agentsJSON is the --agents payload generated from
// plugins/agent-teams/roles/*.md (agentsjson.go) — the workaround for
// anthropics/claude-code#81746, see agent-teams-wf7o.9 — and is always
// emitted; resolving/validating it is the CALLER's job (rawLaunchBGSession),
// not this function's: bgSessionArgs stays pure and does not read the
// filesystem or environment. autoCompactWindow is passed straight through to
// bgSessionSettingsJSON (which pins it into --settings when it parses to a
// real token count — see bgSessionSettings for why) AND, when non-empty,
// still appends "--autocompact <autoCompactWindow>" to the argv verbatim — no
// parsing, no range check there; the claude CLI's own --autocompact flag
// validates form and range and fails loudly on bad input, belt-and-suspenders
// for the rare cold launch that execs fresh instead of claiming a pool spare.
// When empty (the default), argv AND the --settings payload are
// byte-identical to before this parameter existed. Extracted so tests can
// assert the argv without executing the command.
func bgSessionArgs(name, prompt, model, advisor, role, initiativeID, agentsJSON, autoCompactWindow string) []string {
	if model == "" {
		model = driDefaultModel
	}
	args := []string{
		"--bg",
		"-n", name,
		"--model", model,
		"--permission-mode", "bypassPermissions",
	}
	if settings := bgSessionSettingsJSON(role, initiativeID, autoCompactWindow); settings != "" {
		args = append(args, "--settings", settings)
	}
	if autoCompactWindow != "" {
		args = append(args, "--autocompact", autoCompactWindow)
	}
	// driGuardrails are DRI-orchestration rules (delegate, don't implement;
	// never merge unconfirmed; etc.) that make no sense for a non-DRI bg
	// session — e.g. a review-pr session launched via --launch-prompt with
	// role hardcoded "dri" (route.go -> launchRaw) but a
	// "/agent-teams:review-pr ..." prompt that neither orchestrates nor runs
	// rings. Gate strictly on the prompt prefix, since role alone can't tell
	// true DRI launches apart from custom --launch-prompt ones: only a real
	// /dri launch (launchBGSession always prepends "/dri ") gets the
	// guardrail digest; every other bg session still gets memoryRoutingRule.
	appendVal := memoryRoutingRule
	if strings.HasPrefix(prompt, "/dri ") {
		appendVal = driSystemPromptAppend
	}
	args = append(args, "--append-system-prompt", appendVal)
	if advisor != "" {
		args = append(args, "--advisor", advisor)
	}
	args = append(args, "--agents", agentsJSON)
	return append(args, prompt)
}

// driAdvisorSettings reads config.toml's use_advisors and claude_dri_model
// keys (workspaceconfig.UseAdvisors / workspaceconfig.ClaudeDriModel) and
// returns the (model, advisor) pair for DRI session launches. claude_dri_model
// (default claude-opus-4-8 when absent from config.toml — ClaudeDriModel's own
// hardcoded default) is the "strong model" slot: when advisors are enabled
// (use_advisors is true), it becomes the advisor model and the DRI session
// worker stays "sonnet"; when advisors are disabled (the config.toml default),
// it becomes the DRI session's own model and there is no advisor. Any reader
// error (e.g. a malformed config.toml) is propagated, never swallowed — same
// precedent as workspaceconfig.RuntimeDefault's callers (dispatchKong.Run) and
// resolveCodexAutoCompactWindow's use of workspaceconfig.AutoCompactWindow.
// Only launchBGSession (the /dri path) calls this; the raw --launch-prompt
// path does not read config.toml here — it defaults to advisor "" unless the
// caller explicitly passes --advisor (dispatchKong.Advisor).
func driAdvisorSettings(home string) (model, advisor string, err error) {
	driModel, _, err := workspaceconfig.ClaudeDriModel(home)
	if err != nil {
		return "", "", err
	}
	useAdvisors, _, err := workspaceconfig.UseAdvisors(home)
	if err != nil {
		return "", "", err
	}
	if useAdvisors {
		return "sonnet", driModel, nil
	}
	return driModel, "", nil
}

// driAutoCompactWindow reads config.toml's auto_compact_window key
// (workspaceconfig.AutoCompactWindow) and returns it formatted as a decimal
// string — empty when the key is absent, strconv.FormatInt(v, 10) otherwise.
// The claude CLI's own --autocompact flag (bgSessionArgs) owns validation of
// the resulting string; see bgSessionSettings for why this helper must not
// duplicate it. Any reader error is propagated, never swallowed, mirroring
// resolveCodexAutoCompactWindow's handling of the same config key. Unlike
// driAdvisorSettings, this is read by rawLaunchBGSession rather than
// launchBGSession, so it covers every session this producer launches,
// including the raw --launch-prompt path, not just /dri.
func driAutoCompactWindow(home string) (string, error) {
	window, configured, err := workspaceconfig.AutoCompactWindow(home)
	if err != nil {
		return "", err
	}
	if !configured {
		return "", nil
	}
	return strconv.FormatInt(window, 10), nil
}

// launchFunc is the function type for launching a background DRI session.
// dispatchKong and resumeKong hold an injected field of this type so tests
// can substitute a fake without touching a package global. role and
// initiativeID are merged into the launched session's --settings env map
// (agent-teams-142k.1); initiativeID may be "" when the launcher doesn't know
// the initiative id.
type launchFunc func(ctx *cli.Context, dir, driArg, role, initiativeID string) error

// rawLaunchFunc is the function type for launching a background session with a
// custom raw prompt (no /dri prefix is added), an optional model override, and
// an optional advisor model. Used by the --launch-prompt path in dispatchKong;
// injected by tests to avoid exec-ing a real claude binary. role and
// initiativeID are merged into --settings the same way as launchFunc.
type rawLaunchFunc func(ctx *cli.Context, dir, prompt, model, advisor, role, initiativeID string) error

// rawLaunchBGSession launches a background claude session with an arbitrary
// prompt (no /dri prefix). model overrides driDefaultModel when
// non-empty; advisor, when non-empty, adds "--advisor <advisor>" to the argv.
// role and initiativeID are merged into --settings via bgSessionArgs. Shared
// by the --launch-prompt production path and tests (via injection into
// dispatchKong.launchRaw).
//
// Resolves the --agents payload (agentsjson.go) itself — bgSessionArgs stays
// pure and does not read the filesystem — and fails loud (returns an error,
// launches nothing) when that payload can't be built, per the fail-loud
// contract in agent-teams-wf7o.9 artifact (4): a session launched without
// --agents would silently run every named teammate as a generic agent.
//
// Also resolves the auto-compact window (driAutoCompactWindow) itself, here
// rather than in launchBGSession, so the knob applies to every launch this
// function makes — including --launch-prompt — not only the /dri path that
// driAdvisorSettings is scoped to.
func rawLaunchBGSession(ctx *cli.Context, dir, prompt, model, advisor, role, initiativeID string) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return cli.Depf("ateam: 'claude' not found in PATH")
	}
	agentsJSON, err := buildAgentsPayload()
	if err != nil {
		return fmt.Errorf("ateam: build --agents payload: %w", err)
	}
	autoCompactWindow, err := driAutoCompactWindow(ctx.Home)
	if err != nil {
		return fmt.Errorf("ateam: %w", err)
	}
	name := filepath.Base(dir)
	args := bgSessionArgs(name, prompt, model, advisor, role, initiativeID, agentsJSON, autoCompactWindow)
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
// advisor settings from config.toml — dispatch /dri, new-initiative, and
// resume all flow through here, per the advisor-mode-toggle contract
// (agent-teams-wvx2.1).
// role and initiativeID flow straight through to --settings.
func launchBGSession(ctx *cli.Context, dir, driArg, role, initiativeID string) error {
	model, advisor, err := driAdvisorSettings(ctx.Home)
	if err != nil {
		return fmt.Errorf("ateam: %w", err)
	}
	return rawLaunchBGSession(ctx, dir, "/dri "+driArg, model, advisor, role, initiativeID)
}

// printWatchControl writes the standard "Watch and control" block to w.
// sessionName is the basename of the worktree directory, which is the name
// passed to claude --bg -n.
func printWatchControl(w io.Writer, sessionName string) {
	fmt.Fprintf(w, "\nWatch and control:\n")
	fmt.Fprintf(w, "  ateam runtime open claude  # open the native agents view\n")
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

// The routing-field readers this file used to own (worktreePath, modeValue,
// extractEpicID, extractBodyField) and the --body-file redefinition scanner
// (warnBodyFileFieldRedefinitions) are gone: reading goes through
// initiative.Of, and the collision rule is initiative.Fields.CollisionsIn —
// one rule shared with the reader rather than two that happen to agree.
//
// The pr-* trio sendSharedTopicLine needs has no Fields member (initiative
// package doc, frozen item 3: the field set is not closed), so that reader
// projects from initiative.JSONFields — still the one shared scan, not a
// fourth hand-rolled prefix match.
