package verbs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erlloyd/agent-teams/internal/bd"
	"github.com/erlloyd/agent-teams/internal/cli"
)

// ---- fakes -----------------------------------------------------------------

// fakeGit implements gitRunner for tests. All fields default to happy-path
// behaviour; override per-test.
type fakeGit struct {
	repoRootFn       func(dir string) (string, error)
	defaultBranchFn  func(repoRoot string) string
	worktreeExistsFn func(repoRoot, wtPath string) bool
	addWorktreeFn    func(repoRoot, wtPath, branch, base string) error
	removeWorktreeFn func(repoRoot, wtPath string) error
}

func (f *fakeGit) RepoRoot(dir string) (string, error) {
	if f.repoRootFn != nil {
		return f.repoRootFn(dir)
	}
	return dir, nil
}
func (f *fakeGit) DefaultBranch(repoRoot string) string {
	if f.defaultBranchFn != nil {
		return f.defaultBranchFn(repoRoot)
	}
	return "main"
}
func (f *fakeGit) WorktreeExists(repoRoot, wtPath string) bool {
	if f.worktreeExistsFn != nil {
		return f.worktreeExistsFn(repoRoot, wtPath)
	}
	return false
}
func (f *fakeGit) AddWorktree(repoRoot, wtPath, branch, base string) error {
	if f.addWorktreeFn != nil {
		return f.addWorktreeFn(repoRoot, wtPath, branch, base)
	}
	return nil
}
func (f *fakeGit) RemoveWorktree(repoRoot, wtPath string) error {
	if f.removeWorktreeFn != nil {
		return f.removeWorktreeFn(repoRoot, wtPath)
	}
	return nil
}

// fakeBD implements cli.BDRunner for tests.
type fakeBD struct {
	runFn     func(args ...string) (string, error)
	runJSONFn func(dst any, args ...string) error
}

func (f *fakeBD) Run(args ...string) (string, error) {
	if f.runFn != nil {
		return f.runFn(args...)
	}
	return "", nil
}
func (f *fakeBD) RunJSON(dst any, args ...string) error {
	if f.runJSONFn != nil {
		return f.runJSONFn(dst, args...)
	}
	return nil
}

// makeCtx builds a cli.Context with captured stdout/stderr and the supplied BD.
func makeCtx(bd cli.BDRunner, home string) (*cli.Context, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	return &cli.Context{
		Home:   home,
		BD:     bd,
		Stdout: &stdout,
		Stderr: &stderr,
	}, &stdout, &stderr
}

// ---- dispatch happy path (--no-launch) -------------------------------------

func TestDispatch_NoLaunch_HappyPath(t *testing.T) {
	// Create a real temp dir to act as the "repo root" so WorktreeExists can
	// stat it, and a sub-dir for the worktree target that does NOT exist yet.
	repoDir := t.TempDir()
	home := t.TempDir()

	// The worktree path is <home>-worktrees/<slug>; it must not exist yet.
	wtRoot := home + "-worktrees"
	expectedSlug := "add-undo-stack"
	expectedWt := filepath.Join(wtRoot, expectedSlug)

	var capturedBodyFile string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			// Find the --body-file arg and record it.
			for _, a := range args {
				if strings.HasPrefix(a, "--body-file=") {
					capturedBodyFile = strings.TrimPrefix(a, "--body-file=")
				}
			}
			// Populate the issue by unmarshalling JSON into *bd.Issue.
			if issue, ok := dst.(*bd.Issue); ok {
				return json.Unmarshal([]byte(`{"id":"at-test1","title":"Add undo stack"}`), issue)
			}
			return nil
		},
	}

	fg := &fakeGit{
		repoRootFn: func(dir string) (string, error) { return repoDir, nil },
	}

	ctx, stdout, _ := makeCtx(fbd, home)
	cmd := &dispatchCommand{git: fg}

	err := cmd.Run(ctx, []string{
		"--problem", "Add undo stack",
		"--repo", repoDir,
		"--no-launch",
	})
	if err != nil {
		t.Fatalf("dispatch --no-launch: unexpected error: %v", err)
	}

	// Verify initiative_id, slug, base_branch, and worktree path in stdout.
	out := stdout.String()
	if !strings.Contains(out, "initiative_id: at-test1") {
		t.Errorf("stdout missing 'initiative_id: at-test1':\n%s", out)
	}
	if !strings.Contains(out, "slug: "+expectedSlug) {
		t.Errorf("stdout missing 'slug: %s':\n%s", expectedSlug, out)
	}
	if !strings.Contains(out, "base_branch: main") {
		t.Errorf("stdout missing 'base_branch: main':\n%s", out)
	}
	if !strings.Contains(out, expectedWt) {
		t.Errorf("stdout missing worktree path %q:\n%s", expectedWt, out)
	}

	// Verify the body file was written with the worktree line.
	if capturedBodyFile != "" {
		body, err := os.ReadFile(capturedBodyFile)
		if err == nil && !strings.Contains(string(body), "worktree: "+expectedWt) {
			t.Errorf("body file missing 'worktree: %s':\n%s", expectedWt, string(body))
		}
	}
}

// ---- dispatch: not a repo --------------------------------------------------

func TestDispatch_NotARepo(t *testing.T) {
	home := t.TempDir()
	fg := &fakeGit{
		repoRootFn: func(dir string) (string, error) {
			return "", fmt.Errorf("not inside a git repo: %s", dir)
		},
	}
	ctx, _, stderr := makeCtx(&fakeBD{}, home)
	cmd := &dispatchCommand{git: fg}

	err := cmd.Run(ctx, []string{"--problem", "Some work"})
	if err == nil {
		t.Fatal("expected error for non-repo, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "not inside a git repo") {
		t.Errorf("expected 'not inside a git repo' in stderr, got: %s", stderr.String())
	}
}

// ---- dispatch: empty slug --------------------------------------------------

func TestDispatch_EmptySlug(t *testing.T) {
	home := t.TempDir()
	repoDir := t.TempDir()
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	cmd := &dispatchCommand{git: fg}

	// A problem that slugifies to empty (pure punctuation).
	err := cmd.Run(ctx, []string{"--problem", "!@#$%", "--repo", repoDir})
	if err == nil {
		t.Fatal("expected error for empty slug, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2 (UsageError), got %d", code)
	}
}

// ---- dispatch: worktree collision ------------------------------------------

func TestDispatch_WorktreeCollision(t *testing.T) {
	home := t.TempDir()
	repoDir := t.TempDir()
	fg := &fakeGit{
		repoRootFn:       func(dir string) (string, error) { return repoDir, nil },
		worktreeExistsFn: func(repoRoot, wtPath string) bool { return true }, // collision
	}
	ctx, _, stderr := makeCtx(&fakeBD{}, home)
	cmd := &dispatchCommand{git: fg}

	err := cmd.Run(ctx, []string{
		"--problem", "Some work",
		"--repo", repoDir,
		"--no-launch",
	})
	if err == nil {
		t.Fatal("expected error for collision, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	msg := stderr.String()
	if !strings.Contains(msg, "worktree already exists") {
		t.Errorf("expected collision message, got: %s", msg)
	}
	if !strings.Contains(msg, "pick a different --slug") {
		t.Errorf("expected pick-a-different-slug hint, got: %s", msg)
	}
}

// ---- dispatch: registration failure removes the worktree -------------------

// TestDispatch_RegisterFailure_RemovesWorktree verifies FIX 2: when bd create
// fails after the worktree was created, dispatch removes the worktree so the
// command is cleanly retryable.
func TestDispatch_RegisterFailure_RemovesWorktree(t *testing.T) {
	home := t.TempDir()
	repoDir := t.TempDir()

	var removedRepo, removedWt string
	fg := &fakeGit{
		repoRootFn: func(dir string) (string, error) { return repoDir, nil },
		removeWorktreeFn: func(repoRoot, wtPath string) error {
			removedRepo = repoRoot
			removedWt = wtPath
			return nil
		},
	}

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			return fmt.Errorf("bd create: simulated failure")
		},
	}

	ctx, _, _ := makeCtx(fbd, home)
	cmd := &dispatchCommand{git: fg}

	err := cmd.Run(ctx, []string{
		"--problem", "Some feature",
		"--slug", "some-feature",
		"--repo", repoDir,
		"--no-launch",
	})
	if err == nil {
		t.Fatal("expected error from registration failure, got nil")
	}
	if !strings.Contains(err.Error(), "register initiative") {
		t.Errorf("error missing 'register initiative': %v", err)
	}

	// Worktree removal must have been invoked.
	expectedWt := filepath.Join(home+"-worktrees", "some-feature")
	if removedWt != expectedWt {
		t.Errorf("RemoveWorktree called with wt=%q, want %q", removedWt, expectedWt)
	}
	if removedRepo != repoDir {
		t.Errorf("RemoveWorktree called with repo=%q, want %q", removedRepo, repoDir)
	}
}

// ---- dispatch: missing --problem -------------------------------------------

func TestDispatch_MissingProblem(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	cmd := &dispatchCommand{git: &fakeGit{}}

	err := cmd.Run(ctx, []string{})
	if err == nil {
		t.Fatal("expected UsageError, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// ---- dispatch: --id-only output --------------------------------------------

func TestDispatch_IDOnly(t *testing.T) {
	home := t.TempDir()
	repoDir := t.TempDir()

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error { return nil },
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, stdout, _ := makeCtx(fbd, home)
	cmd := &dispatchCommand{git: fg}

	err := cmd.Run(ctx, []string{
		"--problem", "some work",
		"--repo", repoDir,
		"--no-launch",
		"--id-only",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// --id-only with an empty (zero-value) issue id should just print a blank
	// line (the zero value of issue.ID). The key assertion is that the full
	// report block is NOT present.
	out := stdout.String()
	if strings.Contains(out, "worktree:") {
		t.Errorf("--id-only should not print worktree line, got:\n%s", out)
	}
	if strings.Contains(out, "base_branch:") {
		t.Errorf("--id-only should not print base_branch line, got:\n%s", out)
	}
}

// ---- dispatch: registry body contains worktree line -----------------------

func TestDispatch_RegistryBodyWorktreeLine(t *testing.T) {
	home := t.TempDir()
	repoDir := t.TempDir()

	expectedSlug := "my-work"
	expectedWt := filepath.Join(home+"-worktrees", expectedSlug)

	var gotBody string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			for _, a := range args {
				if strings.HasPrefix(a, "--body-file=") {
					path := strings.TrimPrefix(a, "--body-file=")
					b, err := os.ReadFile(path)
					if err == nil {
						gotBody = string(b)
					}
				}
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(fbd, home)
	cmd := &dispatchCommand{git: fg}

	err := cmd.Run(ctx, []string{
		"--problem", "My work",
		"--slug", expectedSlug,
		"--repo", repoDir,
		"--no-launch",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantLine := "worktree: " + expectedWt
	if !strings.Contains(gotBody, wantLine) {
		t.Errorf("body missing %q:\n%s", wantLine, gotBody)
	}
	if !strings.Contains(gotBody, "mode: bg") {
		t.Errorf("body missing 'mode: bg':\n%s", gotBody)
	}
}

// ---- new-initiative: arg validation ----------------------------------------

func TestNewInitiative_MissingDirectory(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}
	cmd := &newInitiativeCommand{}

	err := cmd.Run(ctx, []string{})
	if err == nil {
		t.Fatal("expected UsageError, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestNewInitiative_MissingDRIArg(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}
	cmd := &newInitiativeCommand{}

	err := cmd.Run(ctx, []string{dir})
	if err == nil {
		t.Fatal("expected UsageError for missing dri-arg, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestNewInitiative_MissingClaude(t *testing.T) {
	// Only run when 'claude' is NOT in PATH.
	if _, err := exec.LookPath("claude"); err == nil {
		t.Skip("claude is in PATH; skipping missing-claude test")
	}
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}
	cmd := &newInitiativeCommand{}

	err := cmd.Run(ctx, []string{dir, "some-initiative-id"})
	if err == nil {
		t.Fatal("expected DepError, got nil")
	}
	if code := cli.ExitCode(err); code != 3 {
		t.Errorf("expected exit 3 (DepError), got %d", code)
	}
}

func TestNewInitiative_NonExistentDirectory(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}
	cmd := &newInitiativeCommand{}

	err := cmd.Run(ctx, []string{"/no/such/directory/exists/ever", "arg"})
	if err == nil {
		t.Fatal("expected UsageError for non-existent dir, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestNewInitiative_RegularFileNotDirectory(t *testing.T) {
	// Create a real file (not a directory) and pass it as the <directory> arg.
	f, err := os.CreateTemp(t.TempDir(), "not-a-dir-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()

	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}
	cmd := &newInitiativeCommand{}

	runErr := cmd.Run(ctx, []string{f.Name(), "some-initiative"})
	if runErr == nil {
		t.Fatal("expected UsageError for regular file, got nil")
	}
	if code := cli.ExitCode(runErr); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
	if !strings.Contains(runErr.Error(), "not a directory") {
		t.Errorf("expected 'not a directory' in error, got: %v", runErr)
	}
}
