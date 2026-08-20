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
	})
	p.AddVerb("resume", "Re-launch a background DRI session for an existing initiative.", &resumeKong{
		launch:       launchBGSession,
		launchRaw:    rawLaunchBGSession,
		runtimeStart: startRuntimeWorker,
		codexCheck:   sessionruntime.RequireCompatibleCodex,
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
	Advisor      string `name:"advisor"       help:"Advisor model override for this launch (e.g. \"opus\"). Only affects the --launch-prompt path; when omitted/empty, preserves current behavior exactly (hardcoded \"\" for --launch-prompt, env-derived for the /dri path)."`
	Topic        string `name:"topic"         help:"Post the registration line into a reserved shared topic (only \"reviews\") instead of opening a per-initiative topic. No thread: label is written on the initiative bead."`
	Runtime      string `name:"runtime"       help:"Agent runtime: claude, codex, or auto (default: $ATEAM_RUNTIME, then claude)."`

	git          gitRunner                           `kong:"-"`
	launch       launchFunc                          `kong:"-"`
	createEpic   epicCreatorFunc                     `kong:"-"`
	launchRaw    rawLaunchFunc                       `kong:"-"`
	runtimeStart runtimeStartFunc                    `kong:"-"`
	codexCheck   func(context.Context, string) error `kong:"-"`

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
	runtimeKind, err := sessionruntime.ResolveNew(c.Runtime, os.Getenv("ATEAM_RUNTIME"))
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

	// 7. Register the initiative via bd.
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

	// 7.5. Eagerly create the initiative's Telegram topic (fail-soft): best-
	// effort, mirrors the epic-creation fail-soft above. A machine with no
	// transport configured, or any error along the way, must not fail dispatch.
	if c.transportEnabled != nil && c.transportFor != nil && c.labelAdd != nil {
		c.createInitialTopic(ctx, issue, body)
	}

	// 8. Launch background DRI unless --no-launch.
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

	launch       launchFunc                          `kong:"-"`
	launchRaw    rawLaunchFunc                       `kong:"-"`
	runtimeStart runtimeStartFunc                    `kong:"-"`
	codexCheck   func(context.Context, string) error `kong:"-"`
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

	if _, err := os.Stat(dir); err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam resume: worktree path does not exist: %s\n", dir)
		return cli.Silent(1)
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

// memoryRoutingRule is the canonical memory-routing instruction appended to
// every bg-DRI session at harness-instruction altitude so it overrides the
// built-in file-memory prompt. Source of truth: contract bead agent-teams-8qm.
const memoryRoutingRule = `MEMORY ROUTING (agent-teams). Ignore the harness's built-in file-based memory feature here: do NOT write MEMORY.md or any file under a Claude memory/ directory (e.g. ~/.claude/projects/*/memory/). Persistent memory routes by kind:
- Role/process learnings (transferable across repos) -> ateam learn <role> <slug> --file <tmpfile>, where <role> is dri | planner | implementer | tester | reviewer | investigator.
- User/cross-project preferences & feedback -> ateam learn user <slug> --file <tmpfile>.
- Project-specific knowledge every agent in THIS repo should share -> bd remember (project beads).
Default to ateam learn. Use bd remember only for repo-shared project facts. Never MEMORY.md.`

// driDefaultModel is the model background sessions launch on when no explicit
// override is supplied. Pinned to the concrete id claude-opus-4-8 rather than
// the bare "opus" alias so the default stays put instead of silently following
// whatever "latest opus" resolves to. No [1m] suffix: claude-opus-4-8 is
// natively 1M-context on a first-party endpoint, so the alias-vs-id choice does
// not change the context window. The suffix is a second route to the same
// window, and it does not survive the one case that really does clamp to 200000
// (long-context credits exhausted), so it buys nothing here.
// Kept as a constant so this default and the export-plugin-options.sh hook
// default cannot drift apart (tests/hook-export-plugin-options.test.sh).
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
// launch: an optional env map, and nothing else.
//
// Notably absent: autoCompactWindow. It never enters this payload — re-adding
// it here is a regression, guarded by
// TestBGSessionArgs_SettingsOmitsAutoCompactWindow — because the window is
// settable through a different mechanism: the claude CLI's own --autocompact
// flag, appended to argv by bgSessionArgs whenever
// CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW is non-empty (see
// driAutoCompactWindow). The flag and this struct's absent field write the
// same resolver slot, so there is never a reason to carry the value in both
// places. The CLI resolves the window as (2.1.222, function qX):
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
// The rest of this comment is the argument for the DEFAULT (empty, i.e.
// --autocompact omitted) — not an argument against the knob existing. Sending
// nothing falls through to "auto" on a 1M-context model — the model-default
// tier is gated on the real window being under 1M, so a 1M model skips it —
// giving a ~967000 trigger that tracks whatever model the session actually
// runs on. Any value pinned here can only lower that: this call site used to
// request 200000, which produced a 167000 trigger
// (200000 - 20000 - 13000, matching compactions observed at 167,030 / 167,041
// / 167,052) — a self-inflicted ~6x reduction of the trigger the same session
// reaches with nothing set.
//
// The window is not where the waste is, either. Measured over 51 compactions
// in one three-day DRI session, the first API request after a compaction
// already carried a median 101k tokens (max 169,710): the fixed prefix plus
// the re-injected tool/skill/agent/hook listings are re-established every
// time. Shrinking that beats widening the window.
type bgSessionSettings struct {
	Env *bgSessionEnv `json:"env,omitempty"`
}

// bgSessionSettingsJSON builds the --settings JSON argument for a background
// session launch: an "env" map carrying ATEAM_ROLE/ATEAM_INITIATIVE when
// either is non-empty, and "" when neither is (the caller then omits the flag
// rather than passing an empty object). role and initiativeID are independent:
// initiativeID is omitted whenever the launcher doesn't know the initiative id
// (e.g. new-initiative given a bare problem statement, or the steward, which
// is fleet-scoped and never carries one).
//
// CLI arg, not env var: the daemon's spare-session pool claims pre-warmed
// processes via IPC rather than exec'ing fresh ones from this call's argv, so
// cmd.Env set here never reaches the claimed session (verified live) — but the
// claim payload does carry --settings, so it is honored.
func bgSessionSettingsJSON(role, initiativeID string) string {
	if role == "" && initiativeID == "" {
		return ""
	}
	settings := bgSessionSettings{Env: &bgSessionEnv{Role: role, Initiative: initiativeID}}
	b, err := json.Marshal(settings)
	if err != nil {
		// Env's fields are plain strings — Marshal cannot fail here.
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
// filesystem or environment. autoCompactWindow, when non-empty, appends
// "--autocompact <autoCompactWindow>" to the argv verbatim — no parsing, no
// range check; the claude CLI's own --autocompact flag validates form and
// range and fails loudly on bad input (see bgSessionSettings for why nothing
// duplicates that check here). When empty (the default), argv is
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
	if settings := bgSessionSettingsJSON(role, initiativeID); settings != "" {
		args = append(args, "--settings", settings)
	}
	if autoCompactWindow != "" {
		args = append(args, "--autocompact", autoCompactWindow)
	}
	args = append(args, "--append-system-prompt", memoryRoutingRule)
	if advisor != "" {
		args = append(args, "--advisor", advisor)
	}
	args = append(args, "--agents", agentsJSON)
	return append(args, prompt)
}

// driAdvisorSettings reads CLAUDE_PLUGIN_OPTION_USE_ADVISORS and
// CLAUDE_PLUGIN_OPTION_DRI_MODEL and returns the (model, advisor) pair for DRI
// session launches. CLAUDE_PLUGIN_OPTION_DRI_MODEL (default driDefaultModel
// when unset or empty — the hook that publishes this var defaults it to the
// same value, so the two layers agree) is the "strong model" slot: when
// advisors are enabled (the env var is exactly "true"), it becomes the advisor
// model and the DRI session worker stays "sonnet"; when advisors are disabled —
// any other value (unset, "", "false", or anything not exactly "true") — it
// becomes the DRI session's own model and there is no advisor. Unit testable
// via t.Setenv.
// Only launchBGSession (the /dri path) calls this; the raw --launch-prompt
// path does not read these env vars — it defaults to advisor "" unless the
// caller explicitly passes --advisor (dispatchKong.Advisor).
func driAdvisorSettings() (model, advisor string) {
	driModel := os.Getenv("CLAUDE_PLUGIN_OPTION_DRI_MODEL")
	if driModel == "" {
		driModel = driDefaultModel
	}
	if os.Getenv("CLAUDE_PLUGIN_OPTION_USE_ADVISORS") == "true" {
		return "sonnet", driModel
	}
	return driModel, ""
}

// driAutoCompactWindow reads CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW and
// returns it verbatim — empty when unset, unparsed and unvalidated otherwise.
// The claude CLI's own --autocompact flag (bgSessionArgs) owns validation;
// see bgSessionSettings for why this helper must not duplicate it. Unlike
// driAdvisorSettings, this is read by rawLaunchBGSession rather than
// launchBGSession, so it covers every session this producer launches,
// including the raw --launch-prompt path, not just /dri. Unit testable via
// t.Setenv.
func driAutoCompactWindow() string {
	return os.Getenv("CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW")
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
	name := filepath.Base(dir)
	args := bgSessionArgs(name, prompt, model, advisor, role, initiativeID, agentsJSON, driAutoCompactWindow())
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
// role and initiativeID flow straight through to --settings.
func launchBGSession(ctx *cli.Context, dir, driArg, role, initiativeID string) error {
	model, advisor := driAdvisorSettings()
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
