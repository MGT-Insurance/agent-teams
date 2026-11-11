package verbs

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// wipRepo makes a bare remote and a clone of it with one pushed commit on
// main, and points the fake run at the clone.
func wipRepo(t *testing.T, e *cbEnv) (remote string) {
	t.Helper()
	remote = filepath.Join(t.TempDir(), "remote.git")
	gitT(t, t.TempDir(), "init", "--bare", "-b", "main", remote)
	clone := filepath.Join(t.TempDir(), "run")
	gitT(t, t.TempDir(), "clone", remote, clone)
	gitT(t, clone, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(clone, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, clone, "add", "-A")
	gitT(t, clone, "commit", "-m", "init")
	gitT(t, clone, "push", "origin", "main")
	e.cwd = clone
	e.common.Git = runGitBounded
	return remote
}

// failRun makes the lead vanish while the initiative is open: a non-park error.
func failRun(e *cbEnv) {
	e.polls = []func() []agentSession{func() []agentSession { return nil }}
	e.common.Tmux = func(args ...string) error {
		e.tmux = append(e.tmux, args)
		if args[0] == "has-session" {
			return errors.New("no session")
		}
		return nil
	}
}

func remoteRefs(t *testing.T, remote string) string {
	return gitT(t, remote, "for-each-ref", "--format=%(refname)", "refs/heads/wip/")
}

func TestCloudBootFailedRunPushesDirtyWorktreesToWipRefs(t *testing.T) {
	e := newCBEnv(t)
	remote := wipRepo(t, e)
	failRun(e)
	// Main checkout: uncommitted edit plus an ignored file that must stay out.
	if err := os.WriteFile(filepath.Join(e.cwd, "a.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.cwd, ".gitignore"), []byte("secret.env\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.cwd, "secret.env"), []byte("k\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Linked worktree on a slash branch with an unpushed commit and a clean tree.
	wt := filepath.Join(t.TempDir(), "wt")
	gitT(t, e.cwd, "worktree", "add", "-b", "feat/x", wt)
	if err := os.WriteFile(filepath.Join(wt, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, wt, "add", "-A")
	gitT(t, wt, "commit", "-m", "work")

	err := e.start()
	if err == nil || !strings.Contains(err.Error(), "exited while initiative at-ab12 is open") {
		t.Fatalf("err = %v, want the original lead-gone error", err)
	}
	if got, want := remoteRefs(t, remote), "refs/heads/wip/at-ab12/feat-x\nrefs/heads/wip/at-ab12/main"; got != want {
		t.Fatalf("remote wip refs = %q, want %q", got, want)
	}
	if got := gitT(t, remote, "show", "wip/at-ab12/main:a.txt"); got != "edited" {
		t.Errorf("wip/main a.txt = %q, want edited", got)
	}
	if out := gitT(t, remote, "ls-tree", "--name-only", "wip/at-ab12/main"); strings.Contains(out, "secret.env") {
		t.Errorf("ignored file was pushed: %q", out)
	}
	if len(e.notes) != 2 {
		t.Fatalf("notes = %q, want 2", e.notes)
	}
	sha := gitT(t, remote, "rev-parse", "wip/at-ab12/main")
	want := "wip saved: wip/at-ab12/main at " + sha + " from run dsp-run1"
	found := false
	for _, n := range e.notes {
		found = found || n == want
	}
	if !found {
		t.Errorf("notes = %q, want one equal to %q", e.notes, want)
	}
}

func TestCloudBootNoWipPushWhenNothingToSaveOrNotAFailure(t *testing.T) {
	t.Run("park", func(t *testing.T) {
		e := newCBEnv(t)
		remote := wipRepo(t, e)
		_ = os.WriteFile(filepath.Join(e.cwd, "a.txt"), []byte("dirty\n"), 0o644)
		e.leadSeq("sid-1", "idle")
		e.common.IdlePolls = 1
		wantExit(t, e.start(), cloudBootParkExit)
		if got := remoteRefs(t, remote); got != "" {
			t.Errorf("wip refs after park = %q", got)
		}
	})
	t.Run("closed", func(t *testing.T) {
		e := newCBEnv(t)
		remote := wipRepo(t, e)
		_ = os.WriteFile(filepath.Join(e.cwd, "a.txt"), []byte("dirty\n"), 0o644)
		e.leadSeq("sid-1", "busy")
		e.closeAt = 4
		if err := e.start(); err != nil {
			t.Fatal(err)
		}
		if got := remoteRefs(t, remote); got != "" {
			t.Errorf("wip refs after close = %q", got)
		}
	})
	t.Run("clean and pushed", func(t *testing.T) {
		e := newCBEnv(t)
		remote := wipRepo(t, e)
		failRun(e)
		if err := e.start(); err == nil {
			t.Fatal("want error")
		}
		if got := remoteRefs(t, remote); got != "" {
			t.Errorf("wip refs for a clean tree = %q", got)
		}
		if len(e.notes) != 0 {
			t.Errorf("notes = %q, want none", e.notes)
		}
	})
}

func TestCloudBootWipPushFailureKeepsOriginalError(t *testing.T) {
	e := newCBEnv(t)
	remote := wipRepo(t, e)
	failRun(e)
	_ = os.WriteFile(filepath.Join(e.cwd, "a.txt"), []byte("dirty\n"), 0o644)
	if err := os.RemoveAll(remote); err != nil {
		t.Fatal(err)
	}
	err := e.start()
	if err == nil || !strings.Contains(err.Error(), "exited while initiative at-ab12 is open") {
		t.Fatalf("err = %v, want the original lead-gone error", err)
	}
	if len(e.notes) != 0 {
		t.Errorf("notes = %q, want none after a failed push", e.notes)
	}
	if !strings.Contains(e.ctx.Stderr.(interface{ String() string }).String(), "cloud-boot: save wip") {
		t.Error("push failure not logged to stderr")
	}
}

func TestWipRefName(t *testing.T) {
	for _, c := range []struct{ branch, dir, want string }{
		{"main", "/x/y", "wip/i1/main"},
		{"feat/x y", "/x/y", "wip/i1/feat-x-y"},
		{"", "/x/my.wt", "wip/i1/my.wt"},
		{"", "/", "wip/i1/worktree"},
	} {
		if got := wipRefName("i1", c.branch, c.dir); got != c.want {
			t.Errorf("wipRefName(%q,%q) = %q, want %q", c.branch, c.dir, got, c.want)
		}
	}
}
