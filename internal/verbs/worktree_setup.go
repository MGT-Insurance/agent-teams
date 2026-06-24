// This file is owned by Track GO (worktree-setup verb).
package verbs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/gitutil"
)

// RegisterWorktreeSetupKong registers worktree-setup verbs onto p using a native
// kong struct.
func RegisterWorktreeSetupKong(p *cli.Parser) {
	p.AddVerb("worktree-setup", "Run repo-specific worktree-setup hook for the given worktree.", &worktreeSetupKong{
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

// ── kong struct ───────────────────────────────────────────────────────────────

// worktreeSetupKong is the kong-converted form of worktreeSetupCommand.
type worktreeSetupKong struct {
	// DI fields: kong must ignore these.
	git    wtGitRunner `kong:"-"`
	runner cmdRunner   `kong:"-"`

	WtPath string `arg:"" name:"wtPath" optional:"" help:"Worktree path (defaults to cwd)."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *worktreeSetupKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam worktree-setup: not implemented")
	}

	wtPath := c.WtPath
	if wtPath == "" {
		var err error
		wtPath, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("worktree-setup: cannot determine cwd: %w", err)
		}
	}
	absWtPath, err := filepath.Abs(wtPath)
	if err != nil {
		return fmt.Errorf("worktree-setup: cannot resolve path %q: %w", wtPath, err)
	}
	wtPath = absWtPath

	if _, err := c.git.RepoRoot(wtPath); err != nil {
		return cli.Usagef("worktree-setup: not a git worktree: %s", wtPath)
	}
	commonDir, err := c.git.CommonDir(wtPath)
	if err != nil {
		return cli.Usagef("worktree-setup: not a git worktree: %s", wtPath)
	}
	srcCheckout := filepath.Dir(commonDir)

	repoKey := gitutil.Slugify(filepath.Base(srcCheckout))

	hookFile := filepath.Join(ctx.Home, "worktree-hooks", repoKey)
	data, err := os.ReadFile(hookFile)
	if err != nil {
		fmt.Fprintf(ctx.Stdout, "no worktree-setup hook configured for %s\n", repoKey)
		return nil
	}

	scriptPath := strings.TrimSpace(string(data))

	if _, statErr := os.Stat(scriptPath); statErr != nil {
		loudHookWarning(ctx.Stderr, scriptPath, -1, "script not found at configured path")
		return nil
	}

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
