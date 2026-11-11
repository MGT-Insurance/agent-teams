package verbs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// commitFile adds a committed (unpushed) file in dir.
func commitFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-m", name)
}

func TestCloudBootWipSanitizeCollisionsLandOnDistinctRefs(t *testing.T) {
	e := newCBEnv(t)
	remote := wipRepo(t, e)
	failRun(e)
	for _, b := range []string{"feat/x", "feat-x"} {
		wt := filepath.Join(t.TempDir(), "wt")
		gitT(t, e.cwd, "worktree", "add", "-b", b, wt)
		commitFile(t, wt, strings.ReplaceAll(b, "/", "_")+".txt")
	}
	if err := e.start(); err == nil {
		t.Fatal("want error")
	}
	refs := strings.Split(remoteRefs(t, remote), "\n")
	if len(refs) != 2 {
		t.Fatalf("refs = %q, want 2 distinct", refs)
	}
	if refs[0] == refs[1] || !strings.HasPrefix(refs[0], "refs/heads/wip/at-ab12/feat-x") || !strings.HasPrefix(refs[1], "refs/heads/wip/at-ab12/feat-x") {
		t.Fatalf("refs = %q", refs)
	}
	if len(e.notes) != 2 {
		t.Errorf("notes = %q, want 2", e.notes)
	}
}

// A name reused within one run must not fast-forward the ref an earlier
// worktree just saved, even when the later commit descends from it.
func TestCloudBootWipSameNameInOneRunStaysOnSeparateRefs(t *testing.T) {
	e := newCBEnv(t)
	remote := wipRepo(t, e)
	failRun(e)
	a := filepath.Join(t.TempDir(), "a")
	gitT(t, e.cwd, "worktree", "add", "-b", "feat/x", a)
	commitFile(t, a, "a.txt")
	b := filepath.Join(t.TempDir(), "b")
	gitT(t, e.cwd, "worktree", "add", "-b", "feat-x", b, "feat/x")
	commitFile(t, b, "b.txt")
	if err := e.start(); err == nil {
		t.Fatal("want error")
	}
	if refs := strings.Split(remoteRefs(t, remote), "\n"); len(refs) != 2 {
		t.Fatalf("refs = %q, want 2 (the descendant must not fast-forward the first ref)", refs)
	}
}

func TestCloudBootWipNeverOverwritesExistingRef(t *testing.T) {
	e := newCBEnv(t)
	remote := wipRepo(t, e)
	failRun(e)
	// An unrelated history already sits on the ref this run would use.
	other := filepath.Join(t.TempDir(), "other")
	gitT(t, t.TempDir(), "init", "-b", "x", other)
	commitFile(t, other, "other.txt")
	gitT(t, other, "push", remote, "x:refs/heads/wip/at-ab12/main")
	before := gitT(t, remote, "rev-parse", "wip/at-ab12/main")

	if err := os.WriteFile(filepath.Join(e.cwd, "a.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.start(); err == nil {
		t.Fatal("want error")
	}
	if got := gitT(t, remote, "rev-parse", "wip/at-ab12/main"); got != before {
		t.Fatalf("existing wip ref moved: %s -> %s", before, got)
	}
	refs := strings.Split(remoteRefs(t, remote), "\n")
	if len(refs) != 2 {
		t.Fatalf("refs = %q, want the original plus a suffixed one", refs)
	}
	var suffixed string
	for _, r := range refs {
		if r != "refs/heads/wip/at-ab12/main" {
			suffixed = r
		}
	}
	if !strings.HasPrefix(suffixed, "refs/heads/wip/at-ab12/main-") {
		t.Fatalf("suffixed ref = %q", suffixed)
	}
	if got := gitT(t, remote, "show", strings.TrimPrefix(suffixed, "refs/heads/")+":a.txt"); got != "mine" {
		t.Errorf("suffixed a.txt = %q, want mine", got)
	}
}

func TestCloudBootWipPushesUnpushedCommitWhenNothingStages(t *testing.T) {
	e := newCBEnv(t)
	remote := wipRepo(t, e)
	failRun(e)
	// A submodule with an edited file shows in status but stages nothing.
	subSrc := filepath.Join(t.TempDir(), "sub")
	gitT(t, t.TempDir(), "init", "-b", "main", subSrc)
	commitFile(t, subSrc, "s.txt")
	gitT(t, e.cwd, "-c", "protocol.file.allow=always", "submodule", "add", subSrc, "sub")
	gitT(t, e.cwd, "commit", "-m", "add sub")
	gitT(t, e.cwd, "push", "origin", "main")
	commitFile(t, e.cwd, "unpushed.txt")
	if err := os.WriteFile(filepath.Join(e.cwd, "sub", "s.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if st := gitT(t, e.cwd, "status", "--porcelain"); st == "" {
		t.Fatal("fixture: want a dirty submodule in status")
	}
	if err := e.start(); err == nil {
		t.Fatal("want error")
	}
	if got := remoteRefs(t, remote); got != "refs/heads/wip/at-ab12/main" {
		t.Fatalf("refs = %q, want the unpushed commit pushed", got)
	}
	if got := gitT(t, remote, "show", "wip/at-ab12/main:unpushed.txt"); got != "unpushed.txt" {
		t.Errorf("unpushed.txt = %q", got)
	}
}

func TestCloudBootWipIgnoresClaudeDir(t *testing.T) {
	t.Run("alone makes no push", func(t *testing.T) {
		e := newCBEnv(t)
		remote := wipRepo(t, e)
		failRun(e)
		if err := os.MkdirAll(filepath.Join(e.cwd, ".claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(e.cwd, ".claude", "settings.local.json"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := e.start(); err == nil {
			t.Fatal("want error")
		}
		if got := remoteRefs(t, remote); got != "" {
			t.Errorf("wip refs = %q, want none", got)
		}
		if len(e.notes) != 0 {
			t.Errorf("notes = %q, want none", e.notes)
		}
	})
	t.Run("excluded from a real save", func(t *testing.T) {
		e := newCBEnv(t)
		remote := wipRepo(t, e)
		failRun(e)
		_ = os.MkdirAll(filepath.Join(e.cwd, ".claude"), 0o755)
		_ = os.WriteFile(filepath.Join(e.cwd, ".claude", "settings.local.json"), []byte("{}\n"), 0o644)
		_ = os.WriteFile(filepath.Join(e.cwd, "a.txt"), []byte("edited\n"), 0o644)
		if err := e.start(); err == nil {
			t.Fatal("want error")
		}
		if out := gitT(t, remote, "ls-tree", "-r", "--name-only", "wip/at-ab12/main"); strings.Contains(out, ".claude") {
			t.Errorf(".claude was pushed: %q", out)
		}
	})
}

func TestSaveWIPStopsAtOverallBudget(t *testing.T) {
	e := newCBEnv(t)
	old := cloudBootWIPBudget
	cloudBootWIPBudget = 200 * time.Millisecond
	t.Cleanup(func() { cloudBootWIPBudget = old })
	calls := 0
	e.common.Git = func(ctx context.Context, dir string, args ...string) (string, error) {
		calls++
		if args[0] == "worktree" {
			return "worktree /a\nbranch refs/heads/a\n\nworktree /b\nbranch refs/heads/b\n\nworktree /c\nbranch refs/heads/c\n", nil
		}
		<-ctx.Done() // a stuck remote: blocks until the budget runs out
		return "", ctx.Err()
	}
	start := time.Now()
	e.common.saveWIP(e.ctx, "at-ab12", e.cwd)
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("saveWIP took %v, want about the 200ms budget", d)
	}
	if calls != 2 {
		t.Errorf("git calls = %d, want 2 (list, then one blocked status; the rest skipped)", calls)
	}
}

func TestRunGitBoundedReturnsWhenGrandchildHoldsPipe(t *testing.T) {
	shim := t.TempDir()
	// The shim backgrounds a child that inherits git's stdout, as
	// git-remote-https does, then waits on it.
	script := "#!/bin/sh\nsleep 8 &\nwait\n"
	if err := os.WriteFile(filepath.Join(shim, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldT, oldW := cloudBootGitTimeout, cloudBootWaitDelay
	cloudBootGitTimeout, cloudBootWaitDelay = 200*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { cloudBootGitTimeout, cloudBootWaitDelay = oldT, oldW })
	start := time.Now()
	_, err := runGitBounded(context.Background(), t.TempDir(), "push")
	if err == nil {
		t.Fatal("want a timeout error")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("runGitBounded took %v, want about timeout+WaitDelay", d)
	}
}
