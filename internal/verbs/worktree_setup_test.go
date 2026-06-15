package verbs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erlloyd/agent-teams/internal/cli"
)

// fakeWTGit implements wtGitRunner for tests.
type fakeWTGit struct {
	repoRootFn func(dir string) (string, error)
	commonDirFn func(dir string) (string, error)
}

func (f *fakeWTGit) RepoRoot(dir string) (string, error) {
	if f.repoRootFn != nil {
		return f.repoRootFn(dir)
	}
	return dir, nil
}

func (f *fakeWTGit) CommonDir(dir string) (string, error) {
	if f.commonDirFn != nil {
		return f.commonDirFn(dir)
	}
	// Simulate a worktree: commonDir is <repoRoot>/.git
	return filepath.Join(dir, ".git"), nil
}

// makeWTCtx builds a cli.Context with captured stdout/stderr for worktree-setup tests.
func makeWTCtx(home string) (*cli.Context, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	return &cli.Context{
		Home:   home,
		Stdout: &stdout,
		Stderr: &stderr,
	}, &stdout, &stderr
}

// writeHookFile creates the worktree-hooks/<repoKey> file in home pointing at scriptPath.
func writeHookFile(t *testing.T, home, repoKey, scriptPath string) {
	t.Helper()
	hooksDir := filepath.Join(home, "worktree-hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("create worktree-hooks dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, repoKey), []byte(scriptPath+"\n"), 0o600); err != nil {
		t.Fatalf("write hook file: %v", err)
	}
}

// writeTinyScript writes a shell script to path with the given content and makes it executable.
func writeTinyScript(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write script %s: %v", path, err)
	}
}

// ---- (a) no hook configured --------------------------------------------------

func TestWorktreeSetup_NoHookConfigured(t *testing.T) {
	home := t.TempDir()
	repoRoot := t.TempDir()
	wtDir := t.TempDir()

	fg := &fakeWTGit{
		repoRootFn:  func(dir string) (string, error) { return repoRoot, nil },
		commonDirFn: func(dir string) (string, error) { return filepath.Join(repoRoot, ".git"), nil },
	}

	ctx, stdout, stderr := makeWTCtx(home)
	cmd := &worktreeSetupCommand{git: fg, runner: defaultCmdRunner}

	err := cmd.Run(ctx, []string{wtDir})
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	out := stdout.String()
	repoKey := filepath.Base(repoRoot)
	// Slugify may transform the key; use contains for simplicity
	if !strings.Contains(out, "no worktree-setup hook configured for") {
		t.Errorf("expected 'no worktree-setup hook configured' in stdout, got: %q", out)
	}
	_ = repoKey
	if stderr.String() != "" {
		t.Errorf("expected empty stderr, got: %q", stderr.String())
	}
}

// ---- (b) hook present, script succeeds ---------------------------------------

func TestWorktreeSetup_ScriptSucceeds(t *testing.T) {
	home := t.TempDir()
	repoRoot := t.TempDir()
	wtDir := t.TempDir()
	scriptDir := t.TempDir()

	scriptPath := filepath.Join(scriptDir, "setup.sh")
	argsFile := filepath.Join(scriptDir, "args.txt")

	// Script writes its args to a file so we can assert them.
	writeTinyScript(t, scriptPath, "#!/bin/sh\necho \"$1 $2\" > "+argsFile+"\n")

	fg := &fakeWTGit{
		repoRootFn:  func(dir string) (string, error) { return repoRoot, nil },
		commonDirFn: func(dir string) (string, error) { return filepath.Join(repoRoot, ".git"), nil },
	}

	// repoKey = Slugify(basename(srcCheckout)); here srcCheckout = filepath.Dir(commonDir) = repoRoot
	repoKey := slugifyBasename(repoRoot)
	writeHookFile(t, home, repoKey, scriptPath)

	ctx, _, stderr := makeWTCtx(home)
	cmd := &worktreeSetupCommand{git: fg, runner: defaultCmdRunner}

	err := cmd.Run(ctx, []string{wtDir})
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
	if stderr.String() != "" {
		t.Errorf("expected empty stderr on success, got: %q", stderr.String())
	}

	// Assert script received correct args.
	argsData, readErr := os.ReadFile(argsFile)
	if readErr != nil {
		t.Fatalf("args file not written by script: %v", readErr)
	}
	gotArgs := strings.TrimSpace(string(argsData))
	// wtDir may not be absolute before Run resolves it; use filepath.Clean for comparison
	absWtDir, _ := filepath.Abs(wtDir)
	wantSrcCheckout := filepath.Dir(filepath.Join(repoRoot, ".git"))
	wantArgs := absWtDir + " " + wantSrcCheckout
	if gotArgs != wantArgs {
		t.Errorf("script args = %q, want %q", gotArgs, wantArgs)
	}
}

// ---- repo-key comes from main checkout, not the worktree toplevel ------------
//
// Regression: RepoRoot(wtPath) of a git worktree returns the worktree's own
// path (e.g. /x/at-yrd-go), NOT the main checkout (/x/agent-teams). A hook
// registered as "agent-teams" would never match a key derived from "at-yrd-go".
// The key MUST come from srcCheckout = filepath.Dir(commonDir).

func TestWorktreeSetup_RepoKeyFromMainCheckout(t *testing.T) {
	home := t.TempDir()
	scriptDir := t.TempDir()
	wtDir := t.TempDir()

	// Simulate: worktree toplevel is /x/at-yrd-go, but commonDir points back to
	// the main checkout at /x/agent-teams/.git — as happens with real worktrees.
	mainCheckout := t.TempDir() // represents /x/agent-teams
	worktreeRoot := t.TempDir() // represents /x/at-yrd-go (different basename)

	scriptPath := filepath.Join(scriptDir, "setup.sh")
	argsFile := filepath.Join(scriptDir, "args.txt")
	writeTinyScript(t, scriptPath, "#!/bin/sh\necho \"$1 $2\" > "+argsFile+"\n")

	fg := &fakeWTGit{
		repoRootFn:  func(dir string) (string, error) { return worktreeRoot, nil },
		commonDirFn: func(dir string) (string, error) { return filepath.Join(mainCheckout, ".git"), nil },
	}

	// Register hook under the MAIN checkout's key, not the worktree's.
	mainKey := slugifyBasename(mainCheckout)
	writeHookFile(t, home, mainKey, scriptPath)

	ctx, stdout, stderr := makeWTCtx(home)
	cmd := &worktreeSetupCommand{git: fg, runner: defaultCmdRunner}

	err := cmd.Run(ctx, []string{wtDir})
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
	// If the key had been derived from worktreeRoot instead, the hook file
	// wouldn't be found and stdout would contain "no worktree-setup hook configured".
	if strings.Contains(stdout.String(), "no worktree-setup hook configured") {
		t.Errorf("hook was not found: key was derived from worktree root instead of main checkout\nstdout: %q", stdout.String())
	}
	if stderr.String() != "" {
		t.Errorf("expected empty stderr on success, got: %q", stderr.String())
	}

	// Verify script received wtDir (absolute) and mainCheckout as args.
	argsData, readErr := os.ReadFile(argsFile)
	if readErr != nil {
		t.Fatalf("args file not written by script: %v", readErr)
	}
	gotArgs := strings.TrimSpace(string(argsData))
	absWtDir, _ := filepath.Abs(wtDir)
	wantArgs := absWtDir + " " + mainCheckout
	if gotArgs != wantArgs {
		t.Errorf("script args = %q, want %q", gotArgs, wantArgs)
	}
}

// ---- (c) hook present, script fails (non-zero exit) --------------------------

func TestWorktreeSetup_ScriptFails(t *testing.T) {
	home := t.TempDir()
	repoRoot := t.TempDir()
	wtDir := t.TempDir()
	scriptDir := t.TempDir()

	scriptPath := filepath.Join(scriptDir, "fail.sh")
	writeTinyScript(t, scriptPath, "#!/bin/sh\nexit 42\n")

	fg := &fakeWTGit{
		repoRootFn:  func(dir string) (string, error) { return repoRoot, nil },
		commonDirFn: func(dir string) (string, error) { return filepath.Join(repoRoot, ".git"), nil },
	}

	repoKey := slugifyBasename(repoRoot)
	writeHookFile(t, home, repoKey, scriptPath)

	ctx, _, stderr := makeWTCtx(home)
	cmd := &worktreeSetupCommand{git: fg, runner: defaultCmdRunner}

	err := cmd.Run(ctx, []string{wtDir})
	// Must still return nil (non-fatal).
	if err != nil {
		t.Fatalf("expected nil (hook failure is non-fatal), got: %v", err)
	}

	errOut := stderr.String()
	if !strings.Contains(errOut, "WARNING") {
		t.Errorf("expected LOUD warning in stderr, got: %q", errOut)
	}
	if !strings.Contains(errOut, scriptPath) {
		t.Errorf("expected script path in warning, got: %q", errOut)
	}
	if !strings.Contains(errOut, "42") {
		t.Errorf("expected exit code 42 in warning, got: %q", errOut)
	}
}

// ---- (d) script path configured but missing ----------------------------------

func TestWorktreeSetup_ConfiguredScriptMissing(t *testing.T) {
	home := t.TempDir()
	repoRoot := t.TempDir()
	wtDir := t.TempDir()

	missingScript := filepath.Join(t.TempDir(), "nonexistent.sh")

	fg := &fakeWTGit{
		repoRootFn:  func(dir string) (string, error) { return repoRoot, nil },
		commonDirFn: func(dir string) (string, error) { return filepath.Join(repoRoot, ".git"), nil },
	}

	repoKey := slugifyBasename(repoRoot)
	writeHookFile(t, home, repoKey, missingScript)

	ctx, _, stderr := makeWTCtx(home)
	cmd := &worktreeSetupCommand{git: fg, runner: defaultCmdRunner}

	err := cmd.Run(ctx, []string{wtDir})
	if err != nil {
		t.Fatalf("expected nil (missing script is non-fatal), got: %v", err)
	}

	errOut := stderr.String()
	if !strings.Contains(errOut, "WARNING") {
		t.Errorf("expected LOUD warning in stderr for missing script, got: %q", errOut)
	}
	if !strings.Contains(errOut, missingScript) {
		t.Errorf("expected missing script path in warning, got: %q", errOut)
	}
}

// ---- (e) wtPath is not a git worktree ----------------------------------------

func TestWorktreeSetup_NotAGitWorktree(t *testing.T) {
	home := t.TempDir()
	wtDir := t.TempDir()

	fg := &fakeWTGit{
		repoRootFn: func(dir string) (string, error) {
			return "", fmt.Errorf("not inside a git repo: %s", dir)
		},
	}

	ctx, _, _ := makeWTCtx(home)
	cmd := &worktreeSetupCommand{git: fg, runner: defaultCmdRunner}

	err := cmd.Run(ctx, []string{wtDir})
	if err == nil {
		t.Fatal("expected UsageError for non-worktree path, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2 (UsageError), got %d", code)
	}
	if !strings.Contains(err.Error(), "not a git worktree") {
		t.Errorf("expected 'not a git worktree' in error, got: %v", err)
	}
}

// ---- (e2) CommonDir fails also returns UsageError ---------------------------

func TestWorktreeSetup_CommonDirFails(t *testing.T) {
	home := t.TempDir()
	wtDir := t.TempDir()
	repoRoot := t.TempDir()

	fg := &fakeWTGit{
		repoRootFn: func(dir string) (string, error) { return repoRoot, nil },
		commonDirFn: func(dir string) (string, error) {
			return "", fmt.Errorf("git-common-dir failed")
		},
	}

	ctx, _, _ := makeWTCtx(home)
	cmd := &worktreeSetupCommand{git: fg, runner: defaultCmdRunner}

	err := cmd.Run(ctx, []string{wtDir})
	if err == nil {
		t.Fatal("expected UsageError when CommonDir fails, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2 (UsageError), got %d", code)
	}
}

// ---- default wtPath (cwd) ---------------------------------------------------

func TestWorktreeSetup_DefaultCwd(t *testing.T) {
	home := t.TempDir()
	repoRoot := t.TempDir()

	fg := &fakeWTGit{
		repoRootFn:  func(dir string) (string, error) { return repoRoot, nil },
		commonDirFn: func(dir string) (string, error) { return filepath.Join(repoRoot, ".git"), nil },
	}

	// No hook configured — just verifies default-cwd path reaches the hook-check.
	ctx, stdout, _ := makeWTCtx(home)
	cmd := &worktreeSetupCommand{git: fg, runner: defaultCmdRunner}

	err := cmd.Run(ctx, []string{}) // empty args = use cwd
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
	if !strings.Contains(stdout.String(), "no worktree-setup hook configured for") {
		t.Errorf("expected no-hook message in stdout, got: %q", stdout.String())
	}
}

// ---- ctx == nil guard -------------------------------------------------------

func TestWorktreeSetup_NilCtx(t *testing.T) {
	fg := &fakeWTGit{}
	cmd := &worktreeSetupCommand{git: fg, runner: defaultCmdRunner}
	err := cmd.Run(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil ctx, got nil")
	}
}

// slugifyBasename is a test helper that mimics gitutil.Slugify(filepath.Base(p))
// without importing gitutil (same package, so we use the unexported path directly).
// We keep it local to avoid coupling the test to gitutil internals.
func slugifyBasename(p string) string {
	base := filepath.Base(p)
	// Replicate the Slugify logic: lowercase, non-alnum runs → "-", cap 50.
	var sb strings.Builder
	inSep := false
	for _, r := range strings.ToLower(base) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if inSep && sb.Len() > 0 {
				sb.WriteByte('-')
			}
			sb.WriteRune(r)
			inSep = false
		} else {
			inSep = true
		}
	}
	s := sb.String()
	if len(s) > 50 {
		s = s[:50]
		if i := strings.LastIndexByte(s, '-'); i > 0 {
			s = s[:i]
		}
	}
	return s
}
