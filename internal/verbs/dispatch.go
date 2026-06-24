// This file is owned by Track D (dispatch verbs).
package verbs

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/gitutil"
)

// RegisterDispatch registers the dispatch verbs:
// new-initiative, dispatch, resume.
func RegisterDispatch(reg cli.Registry) {
	reg.Register(&newInitiativeCommand{})
	reg.Register(&dispatchCommand{git: gitutil.New(), launch: launchBGSession})
	reg.Register(&resumeCommand{launch: launchBGSession})
}

// RegisterDispatchKong registers dispatch verbs onto p. Initially bridges all
// verbs from RegisterDispatch; ring-track conversion replaces each bridge with a
// native kong struct in this function without touching any other file.
// Note: dispatchCommand and resumeCommand have injected launchFunc fields —
// mark those kong:"-" when converting to native structs.
func RegisterDispatchKong(p *cli.Parser) {
	bridgeTrack(p, RegisterDispatch)
}

// ---- new-initiative --------------------------------------------------------

type newInitiativeCommand struct{}

func (c *newInitiativeCommand) Name() string { return "new-initiative" }

// Run implements: new-initiative <directory> <dri-arg…>
// Spawns a background claude session with cwd=<directory>, forwarding
// <dri-arg...> to /dri. Mirrors bash lines 280-300.
func (c *newInitiativeCommand) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam new-initiative: not implemented")
	}
	if len(args) < 1 {
		return cli.Usagef("ateam new-initiative: missing <directory>")
	}
	dir := args[0]
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
	if len(args) < 2 {
		return cli.Usagef("ateam new-initiative: missing <dri-arg> (initiative id or problem statement)")
	}
	driArg := strings.Join(args[1:], " ")
	if driArg == "" {
		return cli.Usagef("ateam new-initiative: missing <dri-arg> (initiative id or problem statement)")
	}
	return launchBGSession(ctx, dir, driArg)
}

// memoryRoutingRule is the canonical memory-routing instruction appended to
// every bg-DRI session at harness-instruction altitude so it overrides the
// built-in file-memory prompt. Source of truth: contract bead agent-teams-8qm.
const memoryRoutingRule = `MEMORY ROUTING (agent-teams). Ignore the harness's built-in file-based memory feature here: do NOT write MEMORY.md or any file under a Claude memory/ directory (e.g. ~/.claude/projects/*/memory/). Persistent memory routes by kind:
- Role/process learnings (transferable across repos) -> ateam learn <role> <slug> --file <tmpfile>, where <role> is dri | planner | implementer | tester | reviewer.
- User/cross-project preferences & feedback -> ateam learn user <slug> --file <tmpfile>.
- Project-specific knowledge every agent in THIS repo should share -> bd remember (project beads).
Default to ateam learn. Use bd remember only for repo-shared project facts. Never MEMORY.md.`

// bgSessionArgs returns the argv slice (everything after "claude") for a
// background DRI launch. Extracted so tests can assert the argv without
// executing the command.
func bgSessionArgs(name, driArg string) []string {
	return []string{
		"--bg",
		"-n", name,
		"--permission-mode", "bypassPermissions",
		"--append-system-prompt", memoryRoutingRule,
		"/dri " + driArg,
	}
}

// launchFunc is the function type for launching a background DRI session.
// Both dispatchCommand and resumeCommand hold an injected field of this type
// so tests can substitute a fake without touching a package global.
type launchFunc func(ctx *cli.Context, dir, driArg string) error

// launchBGSession checks for claude, derives the session name from dir's
// basename, and launches: claude --bg -n <name> --permission-mode
// bypassPermissions --append-system-prompt <memoryRoutingRule> "/dri <driArg>"
// with Dir set to dir.
func launchBGSession(ctx *cli.Context, dir, driArg string) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return cli.Depf("ateam: 'claude' not found in PATH")
	}
	name := filepath.Base(dir)
	args := bgSessionArgs(name, driArg)
	cmd := exec.Command("claude", args...)
	cmd.Dir = dir
	cmd.Stdout = ctx.Stdout
	cmd.Stderr = ctx.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude --bg: %w", err)
	}
	return nil
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

// ---- dispatch --------------------------------------------------------------

// gitRunner is the subset of gitutil.Runner used by dispatch, extracted so
// tests can inject a fake without building a full runner.
type gitRunner interface {
	RepoRoot(dir string) (string, error)
	DefaultBranch(repoRoot string) string
	WorktreeExists(repoRoot, wtPath string) bool
	AddWorktree(repoRoot, wtPath, branch, base string) error
	RemoveWorktree(repoRoot, wtPath string) error
}

type dispatchCommand struct {
	git    gitRunner
	launch launchFunc
}

func (c *dispatchCommand) Name() string { return "dispatch" }

// Run implements: dispatch [--problem <text>] [--repo <path>] [--base-branch <name>]
// [--slug <kebab>] [--body-file <path>] [--id-only] [--no-launch]
func (c *dispatchCommand) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam dispatch: not implemented")
	}
	fs := flag.NewFlagSet("dispatch", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)

	problem := fs.String("problem", "", "one-line problem statement (required)")
	repo := fs.String("repo", "", "target directory to resolve repo from (default: cwd)")
	baseBranch := fs.String("base-branch", "", "override base branch (default: detected)")
	slug := fs.String("slug", "", "kebab-case slug (default: derived from --problem)")
	bodyFile := fs.String("body-file", "", "path to file whose content is appended to the initiative body after schema lines")
	idOnly := fs.Bool("id-only", false, "print only the initiative id")
	noLaunch := fs.Bool("no-launch", false, "create worktree and register, but do not launch claude bg session")

	if err := fs.Parse(args); err != nil {
		// flag already wrote its error to ctx.Stderr; don't double-print.
		return cli.Silent(2)
	}

	if *problem == "" {
		return cli.Usagef("dispatch: --problem is required")
	}

	// 1. Resolve repo root.
	repoDir := *repo
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
	base := *baseBranch
	if base == "" {
		base = c.git.DefaultBranch(repoRoot)
	}

	// 3. Slug.
	resolvedSlug := *slug
	if resolvedSlug == "" {
		resolvedSlug = gitutil.Slugify(*problem)
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
	shortTitle := *problem
	if len(shortTitle) > 72 {
		shortTitle = shortTitle[:72]
	}

	body := "problem: " + *problem + "\n" +
		"repo: " + repoRoot + "\n" +
		"worktree: " + wtPath + "\n" +
		"branch: " + resolvedSlug + "\n" +
		"team: " + team + "\n" +
		"mode: bg\n"

	// If --body-file is set, read it and append its content after the schema lines.
	// Schema lines must stay first (the compaction hook greps for a `worktree:` line).
	if *bodyFile != "" {
		extra, err := os.ReadFile(*bodyFile)
		if err != nil {
			_ = c.git.RemoveWorktree(repoRoot, wtPath)
			return cli.Usagef("dispatch: --body-file %q: %v", *bodyFile, err)
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
		// Registration failed — remove the worktree so dispatch is retryable.
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

	// 8. Launch background DRI unless --no-launch.
	if !*noLaunch {
		if err := c.launch(ctx, wtPath, issue.ID); err != nil {
			return fmt.Errorf("dispatch: launch: %w", err)
		}
	}

	// 9. Output.
	if *idOnly {
		fmt.Fprintln(ctx.Stdout, issue.ID)
		return nil
	}

	sessionName := resolvedSlug
	fmt.Fprintf(ctx.Stdout, "initiative_id: %s\n", issue.ID)
	fmt.Fprintf(ctx.Stdout, "worktree: %s\n", wtPath)
	fmt.Fprintf(ctx.Stdout, "slug: %s\n", resolvedSlug)
	fmt.Fprintf(ctx.Stdout, "base_branch: %s\n", base)
	fmt.Fprintf(ctx.Stdout, "team: %s\n", team)
	if !*noLaunch {
		fmt.Fprintf(ctx.Stdout, "\nBackground session launched: %s\n", sessionName)
		printWatchControl(ctx.Stdout, sessionName)
	}
	return nil
}

// ---- resume ----------------------------------------------------------------

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

type resumeCommand struct {
	launch launchFunc
}

func (c *resumeCommand) Name() string { return "resume" }

// Run implements: resume <id>
// Looks up the open initiative by id, validates it, and re-launches a
// background DRI session in its registered worktree via launchBGSession.
func (c *resumeCommand) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam resume: nil context")
	}
	if len(args) == 0 || args[0] == "" {
		return cli.Usagef("ateam resume: missing <id>")
	}
	id := args[0]

	issue, err := bd.ShowIssue(ctx.BD, id)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam resume: no such initiative: %s\n", id)
		return cli.Silent(1)
	}

	if issue.Status == "closed" {
		fmt.Fprintf(ctx.Stderr, "ateam resume: initiative %s is closed — use ateam reopen first if you want to resume it\n", id)
		return cli.Silent(1)
	}

	dir := worktreePath(issue.Description)
	if dir == "" {
		fmt.Fprintf(ctx.Stderr, "ateam resume: initiative %s has no worktree: line in its description\n", id)
		return cli.Silent(1)
	}

	if _, err := os.Stat(dir); err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam resume: worktree path does not exist: %s\n", dir)
		return cli.Silent(1)
	}

	if err := c.launch(ctx, dir, id); err != nil {
		return err
	}

	sessionName := filepath.Base(dir)
	fmt.Fprintf(ctx.Stdout, "initiative_id: %s\n", id)
	fmt.Fprintf(ctx.Stdout, "worktree: %s\n", dir)
	fmt.Fprintf(ctx.Stdout, "\nBackground session launched: %s\n", sessionName)
	printWatchControl(ctx.Stdout, sessionName)
	return nil
}
