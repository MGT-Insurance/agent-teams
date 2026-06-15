// This file is owned by Track GO (worktree-setup verb).
package verbs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/erlloyd/agent-teams/internal/cli"
	"github.com/erlloyd/agent-teams/internal/gitutil"
)

// RegisterWorktreeSetup registers the worktree-setup verb.
func RegisterWorktreeSetup(reg cli.Registry) {
	reg.Register(&worktreeSetupCommand{
		git:    gitutil.New(),
		runner: defaultCmdRunner,
	})
}

// cmdRunner is the signature for running an external command, streaming
// stdout/stderr to the provided writers. Injected for testing.
type cmdRunner func(name string, args []string, stdout, stderr interface{ Write([]byte) (int, error) }) error

func defaultCmdRunner(name string, args []string, stdout, stderr interface{ Write([]byte) (int, error) }) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// wtGitRunner is the subset of gitutil.Runner used by worktree-setup,
// extracted so tests can inject a fake without a real git binary.
type wtGitRunner interface {
	RepoRoot(dir string) (string, error)
	CommonDir(dir string) (string, error)
}

type worktreeSetupCommand struct {
	git    wtGitRunner
	runner cmdRunner
}

func (c *worktreeSetupCommand) Name() string { return "worktree-setup" }

// Run implements: worktree-setup [<wtPath>]
//
// wtPath defaults to cwd. Resolves repoRoot via git.RepoRoot(wtPath) and
// srcCheckout via dirname(git.CommonDir(wtPath)). Looks up a hook file at
// <ctx.Home>/worktree-hooks/<repo-key>. If present, runs the script
// <hookScript> <wtPath> <srcCheckout>. Hook failures are non-fatal (exit 0).
// Only genuine precondition errors (not a git worktree) return a UsageError.
func (c *worktreeSetupCommand) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam worktree-setup: not implemented")
	}

	// 1. Resolve wtPath.
	wtPath := ""
	if len(args) > 0 {
		wtPath = args[0]
	}
	if wtPath == "" {
		var err error
		wtPath, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("worktree-setup: cannot determine cwd: %w", err)
		}
	}
	// Resolve to absolute path.
	absWtPath, err := filepath.Abs(wtPath)
	if err != nil {
		return fmt.Errorf("worktree-setup: cannot resolve path %q: %w", wtPath, err)
	}
	wtPath = absWtPath

	// 2. Resolve srcCheckout. Failures here are precondition errors.
	// RepoRoot is called as a git-repo presence check; its value is not used
	// because for a worktree it returns the worktree path, not the main checkout.
	if _, err := c.git.RepoRoot(wtPath); err != nil {
		return cli.Usagef("worktree-setup: not a git worktree: %s", wtPath)
	}
	commonDir, err := c.git.CommonDir(wtPath)
	if err != nil {
		return cli.Usagef("worktree-setup: not a git worktree: %s", wtPath)
	}
	srcCheckout := filepath.Dir(commonDir)

	// 3. repo-key = Slugify(basename(srcCheckout)) — the MAIN checkout, not the
	// worktree. RepoRoot(wtPath) of a git worktree returns the worktree's own
	// path, giving a unique per-worktree key that never matches the registered
	// hook. srcCheckout (dirname of commonDir) always points to the main repo.
	repoKey := gitutil.Slugify(filepath.Base(srcCheckout))

	// 4. Look up hook file.
	hookFile := filepath.Join(ctx.Home, "worktree-hooks", repoKey)
	data, err := os.ReadFile(hookFile)
	if err != nil {
		// No hook configured — not an error.
		fmt.Fprintf(ctx.Stdout, "no worktree-setup hook configured for %s\n", repoKey)
		return nil
	}

	scriptPath := strings.TrimSpace(string(data))

	// 5. Verify script exists.
	if _, statErr := os.Stat(scriptPath); statErr != nil {
		loudHookWarning(ctx.Stderr, scriptPath, -1, "script not found at configured path")
		return nil
	}

	// 6. Run the script.
	runErr := c.runner(scriptPath, []string{wtPath, srcCheckout}, ctx.Stdout, ctx.Stderr)
	if runErr != nil {
		exitCode := 1
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		loudHookWarning(ctx.Stderr, scriptPath, exitCode, runErr.Error())
	}
	return nil
}

// loudHookWarning writes a clearly-marked multi-line warning to w. exitCode -1
// means the script was not found (no exit code to report).
func loudHookWarning(w interface{ Write([]byte) (int, error) }, scriptPath string, exitCode int, detail string) {
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "╔══════════════════════════════════════════════════════╗")
	fmt.Fprintln(w, "║  WARNING: worktree-setup hook failed (non-fatal)     ║")
	fmt.Fprintln(w, "╚══════════════════════════════════════════════════════╝")
	fmt.Fprintf(w, "  script: %s\n", scriptPath)
	if exitCode >= 0 {
		fmt.Fprintf(w, "  exit code: %d\n", exitCode)
	}
	fmt.Fprintf(w, "  detail: %s\n", detail)
	fmt.Fprintln(w, "  The worktree was still created. Run the setup script")
	fmt.Fprintln(w, "  manually or re-run 'ateam worktree-setup' to retry.")
	fmt.Fprintln(w, "")
}
