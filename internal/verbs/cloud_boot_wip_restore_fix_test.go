package verbs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const restoreTestID = "at-ab12"

// saveAttempt1 runs a failing attempt in e.cwd so cloud-boot saves its work.
func saveAttempt1(t *testing.T, e *cbEnv) {
	t.Helper()
	failingLaunch(e, func() {})
	if err := e.start(); err == nil {
		t.Fatal("attempt 1: want the lead-gone error")
	}
}

func commitNamed(t *testing.T, dir, name, body, msg string) string {
	t.Helper()
	writeFileT(t, filepath.Join(dir, name), body)
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-m", msg)
	return gitT(t, dir, "rev-parse", "HEAD")
}

// TestRestore_DetachedRestoreKeepsCommitsOnTopOfTip: a detached save must not
// move a checkout whose HEAD already holds commits on top of the saved tip.
func TestRestore_DetachedRestoreKeepsCommitsOnTopOfTip(t *testing.T) {
	e := newCBEnv(t)
	remote := wipRepo(t, e)
	gitT(t, e.cwd, "checkout", "--detach")
	writeFileT(t, filepath.Join(e.cwd, "det.txt"), "detached work\n")
	saveAttempt1(t, e)
	tip := gitT(t, remote, "rev-parse", "wip/"+restoreTestID+"/run")

	// A sandbox where the DRI already restored the tip and committed on top.
	fresh := filepath.Join(t.TempDir(), "run2")
	gitT(t, t.TempDir(), "clone", remote, fresh)
	gitT(t, fresh, "fetch", "origin", "wip/"+restoreTestID+"/run")
	gitT(t, fresh, "checkout", "--detach", tip)
	dri := commitNamed(t, fresh, "dri.txt", "dri\n", "dri work on top")

	got := e.common.restoreWIP(e.ctx, restoreTestID, fresh)
	if head := gitT(t, fresh, "rev-parse", "HEAD"); head != dri {
		t.Errorf("HEAD = %s, want it left at the DRI's commit %s", head, dri)
	}
	if got != "" {
		t.Errorf("summary = %q, want empty for a tip HEAD already contains", got)
	}
}

// TestRestore_DetachedRestoreRefusesHeadWithOtherCommits: HEAD has commits the
// tip lacks and the tip is not in HEAD, so detaching would orphan them.
func TestRestore_DetachedRestoreRefusesHeadWithOtherCommits(t *testing.T) {
	e := newCBEnv(t)
	remote := wipRepo(t, e)
	gitT(t, e.cwd, "checkout", "--detach")
	writeFileT(t, filepath.Join(e.cwd, "det.txt"), "detached work\n")
	saveAttempt1(t, e)

	fresh := filepath.Join(t.TempDir(), "run2")
	gitT(t, t.TempDir(), "clone", remote, fresh)
	gitT(t, fresh, "checkout", "--detach")
	other := commitNamed(t, fresh, "other.txt", "other\n", "unrelated detached work")

	got := e.common.restoreWIP(e.ctx, restoreTestID, fresh)
	if head := gitT(t, fresh, "rev-parse", "HEAD"); head != other {
		t.Errorf("HEAD = %s, want it left at %s", head, other)
	}
	if !strings.Contains(got, "Needs you") || strings.Contains(got, "Restored at boot") {
		t.Errorf("summary = %q, want a Needs you entry and no restore", got)
	}
}

// TestRestore_ResumeAfterRebaseIsSilent: after a restore, rebasing the restored
// branch must not make the next resume report it again.
func TestRestore_ResumeAfterRebaseIsSilent(t *testing.T) {
	e := newCBEnv(t)
	remote := wipRepo(t, e)
	writeFileT(t, filepath.Join(e.cwd, "probe-wip.txt"), "attempt 1 work\n")
	saveAttempt1(t, e)

	fresh := filepath.Join(t.TempDir(), "run2")
	gitT(t, t.TempDir(), "clone", remote, fresh)
	if got := e.common.restoreWIP(e.ctx, restoreTestID, fresh); !strings.Contains(got, "Restored at boot") {
		t.Fatalf("first restore summary = %q, want Restored at boot", got)
	}

	// Origin main moves; the DRI rebases the restored branch onto it.
	other := filepath.Join(t.TempDir(), "other")
	gitT(t, t.TempDir(), "clone", remote, other)
	commitNamed(t, other, "moved.txt", "m\n", "main moved")
	gitT(t, other, "push", "origin", "main")
	gitT(t, fresh, "fetch", "origin")
	gitT(t, fresh, "rebase", "origin/main")
	rebased := gitT(t, fresh, "rev-parse", "HEAD")

	if got := e.common.restoreWIP(e.ctx, restoreTestID, fresh); got != "" {
		t.Errorf("resume summary = %q, want empty", got)
	}
	if head := gitT(t, fresh, "rev-parse", "HEAD"); head != rebased {
		t.Errorf("HEAD = %s, want %s untouched", head, rebased)
	}
}

// TestRestore_ResumeOfDetachedWorktreeIsSilent: a track worktree restored
// detached has content, which must not read as an occupied path on resume.
func TestRestore_ResumeOfDetachedWorktreeIsSilent(t *testing.T) {
	e := newCBEnv(t)
	remote := wipRepo(t, e)
	trackRoot := filepath.Join(t.TempDir(), "proj-worktrees")
	wt := filepath.Join(trackRoot, "team-det")
	gitT(t, e.cwd, "worktree", "add", "--detach", wt)
	writeFileT(t, filepath.Join(wt, "det.txt"), "detached track work\n")
	saveAttempt1(t, e)

	run := e.cwd
	freshSandbox(t, remote, run, trackRoot)
	if got := e.common.restoreWIP(e.ctx, restoreTestID, run); !strings.Contains(got, "Restored at boot") {
		t.Fatalf("first restore summary = %q, want Restored at boot", got)
	}
	if got := e.common.restoreWIP(e.ctx, restoreTestID, run); got != "" {
		t.Errorf("resume summary = %q, want empty", got)
	}
}

// TestRestore_FetchesOnlyWipRefsAndRecordedBranches: unrelated origin branches
// are not downloaded, and one ref's fetch failure leaves the others restored.
func TestRestore_FetchesOnlyWipRefsAndRecordedBranches(t *testing.T) {
	e := newCBEnv(t)
	remote := wipRepo(t, e)
	run := e.cwd
	trackRoot := filepath.Join(t.TempDir(), "proj-worktrees")
	wtA := filepath.Join(trackRoot, "team-track-a")
	writeFileT(t, filepath.Join(run, "run-dirty.txt"), "run uncommitted\n")
	gitT(t, run, "worktree", "add", "-b", "track-a", wtA)
	commitNamed(t, wtA, "track-a.txt", "a\n", "track-a work")
	saveAttempt1(t, e)

	freshSandbox(t, remote, run, trackRoot)
	// A branch pushed after the sandbox cloned, unrelated to any save.
	other := filepath.Join(t.TempDir(), "other")
	gitT(t, t.TempDir(), "clone", remote, other)
	gitT(t, other, "checkout", "-b", "unrelated")
	commitNamed(t, other, "u.txt", "u\n", "unrelated")
	gitT(t, other, "push", "origin", "unrelated")

	// Fail the fetch of track-a's wip ref only.
	e.common.Git = func(ctx context.Context, dir string, args ...string) (string, error) {
		if args[0] == "fetch" && strings.Contains(strings.Join(args, " "), "/track-a") {
			return "", errors.New("git fetch: simulated network failure")
		}
		return runGitBounded(ctx, dir, args...)
	}
	got := e.common.restoreWIP(e.ctx, restoreTestID, run)

	if out := gitT(t, run, "for-each-ref", "refs/remotes/origin/unrelated"); out != "" {
		t.Errorf("restore fetched an unrelated origin branch: %q", out)
	}
	if currentBranch(run) != "main" {
		t.Errorf("run checkout on %q, want main restored despite track-a's fetch failure", currentBranch(run))
	}
	if _, err := os.Stat(filepath.Join(run, "run-dirty.txt")); err != nil {
		t.Errorf("main's save was not restored: %v", err)
	}
	i := strings.Index(got, "Needs you")
	if i < 0 || !strings.Contains(got[i:], "wip/"+restoreTestID+"/track-a") || !strings.Contains(got[i:], "simulated network failure") {
		t.Errorf("summary = %q, want track-a under Needs you with the fetch error", got)
	}
	if !strings.Contains(got, "Restored at boot") {
		t.Errorf("summary = %q, want the run checkout listed as restored", got)
	}
}
