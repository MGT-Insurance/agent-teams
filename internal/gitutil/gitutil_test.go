package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---- Slugify (pure, no injection needed) -----------------------------------

func TestSlugify(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"add undo stack", "add-undo-stack"},
		{"Add Undo Stack", "add-undo-stack"},
		{"  leading and trailing  ", "leading-and-trailing"},
		{"multiple---hyphens", "multiple-hyphens"},
		{"hello/world", "hello-world"},
		{"hello.world", "hello-world"},
		{"SHOUTING CAPS NOW", "shouting-caps-now"},
		{"123 numeric 456", "123-numeric-456"},
		// Special characters collapse to single hyphen.
		{"feat: add new !@#$ feature", "feat-add-new-feature"},
		// Empty / whitespace-only → empty string.
		{"", ""},
		{"   ", ""},
		{"!@#$%", ""},
		// Length cap at 50; truncate at last hyphen.
		{"a-very-long-slug-that-exceeds-fifty-characters-in-total-length", "a-very-long-slug-that-exceeds-fifty-characters-in"},
	}
	for _, tt := range tests {
		got := Slugify(tt.in)
		if got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ---- integration tests against a temp git repo ----------------------------

// initTempRepo creates a minimal git repo in a temp dir and returns its path.
func initTempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	// Need at least one commit so worktree add works.
	run("commit", "--allow-empty", "-m", "init")
	return dir
}

func TestRepoRoot(t *testing.T) {
	dir := initTempRepo(t)
	r := New()

	got, err := r.RepoRoot(dir)
	if err != nil {
		t.Fatalf("RepoRoot(%q): unexpected error: %v", dir, err)
	}
	// On macOS /var → /private/var; normalise with EvalSymlinks.
	gotReal, _ := filepath.EvalSymlinks(got)
	dirReal, _ := filepath.EvalSymlinks(dir)
	if gotReal != dirReal {
		t.Errorf("RepoRoot = %q, want %q", gotReal, dirReal)
	}
}

func TestRepoRoot_NotARepo(t *testing.T) {
	dir := t.TempDir()
	r := New()
	_, err := r.RepoRoot(dir)
	if err == nil {
		t.Fatal("expected error for non-repo dir, got nil")
	}
}

func TestRepoRoot_WithInjectedFake(t *testing.T) {
	fake := NewWithExec(func(name string, args ...string) ([]byte, []byte, error) {
		return []byte("/some/repo\n"), nil, nil
	})
	got, err := fake.RepoRoot("/any/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/some/repo" {
		t.Errorf("got %q, want /some/repo", got)
	}
}

func TestDefaultBranch_FallsBackToMain(t *testing.T) {
	// Fake git that returns an error (no origin configured).
	fake := NewWithExec(func(name string, args ...string) ([]byte, []byte, error) {
		return nil, []byte("fatal: not a symbolic ref\n"), &exec.ExitError{}
	})
	got := fake.DefaultBranch("/repo")
	if got != "main" {
		t.Errorf("expected fallback 'main', got %q", got)
	}
}

func TestDefaultBranch_ParsesRef(t *testing.T) {
	fake := NewWithExec(func(name string, args ...string) ([]byte, []byte, error) {
		return []byte("refs/remotes/origin/develop\n"), nil, nil
	})
	got := fake.DefaultBranch("/repo")
	if got != "develop" {
		t.Errorf("expected 'develop', got %q", got)
	}
}

func TestWorktreeExists_MissingDir(t *testing.T) {
	dir := initTempRepo(t)
	r := New()
	// A path that does not exist on disk should return false.
	absent := filepath.Join(dir, "no-such-worktree")
	if r.WorktreeExists(dir, absent) {
		t.Error("WorktreeExists returned true for non-existent path")
	}
}

func TestWorktreeExists_ExistingDir(t *testing.T) {
	dir := initTempRepo(t)
	r := New()
	// Any existing directory counts as a collision.
	if !r.WorktreeExists(dir, dir) {
		t.Error("WorktreeExists returned false for the repo dir itself")
	}
}

func TestAddWorktree(t *testing.T) {
	dir := initTempRepo(t)
	r := New()
	wtPath := filepath.Join(t.TempDir(), "my-worktree")

	if err := r.AddWorktree(dir, wtPath, "my-branch", "HEAD"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree dir not created: %v", err)
	}

	// WorktreeExists should now return true.
	if !r.WorktreeExists(dir, wtPath) {
		t.Error("WorktreeExists returned false after AddWorktree")
	}
}

func TestAddWorktree_Error(t *testing.T) {
	fake := NewWithExec(func(name string, args ...string) ([]byte, []byte, error) {
		return nil, []byte("fatal: branch already exists\n"), &exec.ExitError{}
	})
	err := fake.AddWorktree("/repo", "/wt", "branch", "main")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "git worktree add") {
		t.Errorf("error message missing context: %v", err)
	}
}
