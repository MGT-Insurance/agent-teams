package verbs

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// failingLaunch makes the run fail after launch (the lead vanishes) and calls
// onLaunch when the session would start, so a test can inspect the checkout at
// that moment and simulate the session's work.
func failingLaunch(e *cbEnv, onLaunch func()) {
	e.polls = []func() []agentSession{func() []agentSession { return nil }}
	e.pollN = 0
	e.tmux = nil
	e.common.Tmux = func(args ...string) error {
		e.tmux = append(e.tmux, args)
		switch args[0] {
		case "new-session":
			onLaunch()
		case "has-session":
			return errors.New("no session")
		}
		return nil
	}
}

func writeFileT(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if strings.TrimSpace(s) == want {
			return true
		}
	}
	return false
}

// TestCloudBoot_RestartRestoresSavedWIP covers a restarted run: attempt 1 fails
// with an uncommitted file and cloud-boot saves it to wip/<id>/main; attempt 2
// boots from a fresh checkout at the same base and must find that file before
// the session launches, note the restore, and later save by fast-forward.
func TestCloudBoot_RestartRestoresSavedWIP(t *testing.T) {
	e := newCBEnv(t)
	remote := wipRepo(t, e)

	// Attempt 1: an uncommitted probe file, then the run fails.
	writeFileT(t, filepath.Join(e.cwd, "probe-wip.txt"), "attempt 1 work\n")
	failingLaunch(e, func() {})
	if err := e.start(); err == nil {
		t.Fatal("attempt 1: want the lead-gone error")
	}
	const ref = "wip/at-ab12/main"
	saved := gitT(t, remote, "rev-parse", ref)
	if want := "wip saved: " + ref + " at " + saved + " from run dsp-run1"; !containsString(e.notes, want) {
		t.Fatalf("attempt 1 notes = %q, want %q", e.notes, want)
	}

	// Attempt 2: a fresh checkout at the same base.
	fresh := filepath.Join(t.TempDir(), "run2")
	gitT(t, t.TempDir(), "clone", remote, fresh)
	e.cwd = fresh
	var atLaunch string
	var launched bool
	failingLaunch(e, func() {
		launched = true
		if b, err := os.ReadFile(filepath.Join(fresh, "probe-wip.txt")); err == nil {
			atLaunch = string(b)
		}
		// The session adds more work before it fails.
		writeFileT(t, filepath.Join(fresh, "attempt2.txt"), "attempt 2 work\n")
	})
	if err := e.start(); err == nil {
		t.Fatal("attempt 2: want the lead-gone error")
	}
	if !launched {
		t.Fatal("attempt 2: session never launched")
	}
	if atLaunch != "attempt 1 work\n" {
		t.Errorf("probe-wip.txt at launch = %q, want attempt 1's content", atLaunch)
	}
	restored := "wip restored: " + ref + " at " + saved
	if !containsString(e.notes, restored) {
		t.Errorf("notes = %q, want one equal to %q", e.notes, restored)
	}

	// Attempt 2's own save must fast-forward the same ref, not fork a -<sha7> ref.
	if got := remoteRefs(t, remote); got != "refs/heads/"+ref {
		t.Errorf("remote wip refs = %q, want only refs/heads/%s", got, ref)
	}
	tip := gitT(t, remote, "rev-parse", ref)
	if tip == saved {
		t.Errorf("%s did not advance past attempt 1's save %s", ref, saved)
	} else if err := exec.Command("git", "-C", remote, "merge-base", "--is-ancestor", saved, tip).Run(); err != nil {
		t.Errorf("%s is not a fast-forward of attempt 1's save %s: %v", tip, saved, err)
	}
}

// launchAppendPrompt returns the --append-system-prompt value of the
// new-session call, or "" when none was made.
func launchAppendPrompt(e *cbEnv) string {
	for _, args := range e.tmux {
		if args[0] != "new-session" {
			continue
		}
		for i, a := range args {
			if a == "--append-system-prompt" && i+1 < len(args) {
				return args[i+1]
			}
		}
	}
	return ""
}

// freshSandbox simulates the next attempt's sandbox: it deletes the given
// directories and re-clones the run checkout at its same path.
func freshSandbox(t *testing.T, remote, runDir string, gone ...string) {
	t.Helper()
	for _, d := range append([]string{runDir}, gone...) {
		if err := os.RemoveAll(d); err != nil {
			t.Fatal(err)
		}
	}
	gitT(t, t.TempDir(), "clone", remote, runDir)
}

func currentBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "<no checkout: " + err.Error() + ">"
	}
	return strings.TrimSpace(string(out))
}

// TestCloudBoot_RestartRecreatesEveryWorktree: attempt 1 saves the run checkout
// and two track worktrees; attempt 2 starts in a fresh sandbox and must have all
// three back at their original paths and branches before the session launches.
func TestCloudBoot_RestartRecreatesEveryWorktree(t *testing.T) {
	e := newCBEnv(t)
	remote := wipRepo(t, e)
	run := e.cwd
	trackRoot := filepath.Join(t.TempDir(), "proj-worktrees")
	if err := os.MkdirAll(trackRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	wtA := filepath.Join(trackRoot, "team-track-a")
	wtB := filepath.Join(trackRoot, "team-track-b")

	// Attempt 1 work.
	writeFileT(t, filepath.Join(run, "run-dirty.txt"), "run uncommitted\n")
	gitT(t, run, "worktree", "add", "-b", "track-a", wtA)
	writeFileT(t, filepath.Join(wtA, "a-committed.txt"), "a committed\n")
	gitT(t, wtA, "add", "-A")
	gitT(t, wtA, "commit", "-m", "track-a work")
	writeFileT(t, filepath.Join(wtA, "a-dirty.txt"), "a uncommitted\n")
	gitT(t, run, "worktree", "add", "-b", "feature/b", wtB)
	writeFileT(t, filepath.Join(wtB, "b-committed.txt"), "b committed\n")
	gitT(t, wtB, "add", "-A")
	gitT(t, wtB, "commit", "-m", "feature/b work")

	failingLaunch(e, func() {})
	if err := e.start(); err == nil {
		t.Fatal("attempt 1: want the lead-gone error")
	}
	refsAfter1 := remoteRefs(t, remote)
	if n := len(strings.Split(refsAfter1, "\n")); refsAfter1 == "" || n != 3 {
		t.Fatalf("attempt 1 wip refs = %q, want 3", refsAfter1)
	}

	// Attempt 2: fresh sandbox, worktree directories gone.
	freshSandbox(t, remote, run, trackRoot)
	e.notes = nil
	type want struct{ dir, branch, file, body string }
	wants := []want{
		{run, "main", "run-dirty.txt", "run uncommitted\n"},
		{wtA, "track-a", "a-committed.txt", "a committed\n"},
		{wtA, "track-a", "a-dirty.txt", "a uncommitted\n"},
		{wtB, "feature/b", "b-committed.txt", "b committed\n"},
	}
	var launched bool
	failingLaunch(e, func() {
		launched = true
		for _, w := range wants {
			if got := currentBranch(w.dir); got != w.branch {
				t.Errorf("at launch, %s is on %q, want branch %q", w.dir, got, w.branch)
			}
			b, err := os.ReadFile(filepath.Join(w.dir, w.file))
			if err != nil {
				t.Errorf("at launch, %s missing saved file %s: %v", w.dir, w.file, err)
			} else if string(b) != w.body {
				t.Errorf("at launch, %s/%s = %q, want %q", w.dir, w.file, b, w.body)
			}
		}
		// The session adds more work in every worktree that came back.
		for _, d := range []string{run, wtA, wtB} {
			if _, err := os.Stat(d); err == nil {
				writeFileT(t, filepath.Join(d, "attempt2.txt"), "attempt 2 work\n")
			}
		}
	})
	if err := e.start(); err == nil {
		t.Fatal("attempt 2: want the lead-gone error")
	}
	if !launched {
		t.Fatal("attempt 2: session never launched")
	}
	prompt := launchAppendPrompt(e)
	if !strings.Contains(prompt, "Restored at boot") {
		t.Errorf("--append-system-prompt has no \"Restored at boot\" list")
	}
	for _, br := range []string{"main", "track-a", "feature/b"} {
		if !strings.Contains(prompt, br) {
			t.Errorf("--append-system-prompt does not name restored branch %q", br)
		}
	}

	// Each wip ref fast-forwards: no -<sha7> fork refs after attempt 2's save.
	if got := remoteRefs(t, remote); got != refsAfter1 {
		t.Errorf("wip refs after attempt 2 = %q, want the same set as after attempt 1: %q", got, refsAfter1)
	}
}

// TestCloudBoot_RestartLeavesDivergedSaveForModel: a saved branch whose origin
// tip moved between attempts is neither merged nor moved; the model is told.
func TestCloudBoot_RestartLeavesDivergedSaveForModel(t *testing.T) {
	e := newCBEnv(t)
	remote := wipRepo(t, e)
	run := e.cwd
	trackRoot := filepath.Join(t.TempDir(), "proj-worktrees")
	if err := os.MkdirAll(trackRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	wtD := filepath.Join(trackRoot, "team-track-d")

	gitT(t, run, "worktree", "add", "-b", "track-d", wtD)
	gitT(t, wtD, "push", "origin", "track-d")
	writeFileT(t, filepath.Join(wtD, "saved-only.txt"), "saved\n")
	gitT(t, wtD, "add", "-A")
	gitT(t, wtD, "commit", "-m", "unpushed track-d work")

	failingLaunch(e, func() {})
	if err := e.start(); err == nil {
		t.Fatal("attempt 1: want the lead-gone error")
	}
	saved := gitT(t, remote, "rev-parse", "wip/at-ab12/track-d")

	// Someone moves origin's track-d between attempts, without the save.
	other := filepath.Join(t.TempDir(), "other")
	gitT(t, t.TempDir(), "clone", remote, other)
	gitT(t, other, "checkout", "track-d")
	writeFileT(t, filepath.Join(other, "moved-on.txt"), "someone else\n")
	gitT(t, other, "add", "-A")
	gitT(t, other, "commit", "-m", "origin moved")
	gitT(t, other, "push", "origin", "track-d")
	moved := gitT(t, remote, "rev-parse", "track-d")

	freshSandbox(t, remote, run, trackRoot)
	e.notes = nil
	var launched bool
	failingLaunch(e, func() {
		launched = true
		// Any local track-d must not hold the saved work.
		if tip, err := exec.Command("git", "-C", run, "rev-parse", "--verify", "-q", "refs/heads/track-d").Output(); err == nil {
			got := strings.TrimSpace(string(tip))
			if got == saved {
				t.Errorf("local track-d was moved to the saved tip %s", saved)
			}
			if exec.Command("git", "-C", run, "merge-base", "--is-ancestor", saved, got).Run() == nil {
				t.Errorf("local track-d %s contains the diverged save %s", got, saved)
			}
			if merges := gitT(t, run, "rev-list", "--merges", "refs/heads/track-d"); merges != "" {
				t.Errorf("local track-d has merge commits: %q", merges)
			}
		}
		if _, err := os.Stat(filepath.Join(wtD, "saved-only.txt")); err == nil {
			t.Errorf("saved-only.txt is present in the restored track-d checkout")
		}
		if currentBranch(wtD) == "track-d" {
			if merges := gitT(t, wtD, "rev-list", "--merges", "HEAD"); merges != "" {
				t.Errorf("track-d checkout has merge commits: %q", merges)
			}
		}
	})
	if err := e.start(); err == nil {
		t.Fatal("attempt 2: want the lead-gone error")
	}
	if !launched {
		t.Fatal("attempt 2: session never launched")
	}
	if got := gitT(t, remote, "rev-parse", "track-d"); got != moved {
		t.Errorf("origin track-d = %s, want it left at %s", got, moved)
	}
	prompt := launchAppendPrompt(e)
	i := strings.Index(prompt, "Needs you")
	if i < 0 {
		t.Fatalf("--append-system-prompt has no \"Needs you\" list")
	}
	if !strings.Contains(prompt[i:], "track-d") {
		t.Errorf("\"Needs you\" list does not name diverged branch track-d: %q", prompt[i:])
	}
}

// TestCloudBoot_RestartListsLegacyRefForModel: a wip ref saved before the
// trailers existed has no recorded worktree, so it is listed, not restored.
func TestCloudBoot_RestartListsLegacyRefForModel(t *testing.T) {
	e := newCBEnv(t)
	remote := wipRepo(t, e)
	other := filepath.Join(t.TempDir(), "other")
	gitT(t, t.TempDir(), "clone", remote, other)
	writeFileT(t, filepath.Join(other, "legacy.txt"), "legacy\n")
	gitT(t, other, "add", "-A")
	gitT(t, other, "commit", "-m", "legacy wip, no trailers")
	gitT(t, other, "push", "origin", "HEAD:refs/heads/wip/at-ab12/legacy")

	failingLaunch(e, func() {})
	if err := e.start(); err == nil {
		t.Fatal("want the lead-gone error")
	}
	prompt := launchAppendPrompt(e)
	i := strings.Index(prompt, "Needs you")
	if i < 0 || !strings.Contains(prompt[i:], "wip/at-ab12/legacy") {
		t.Errorf("\"Needs you\" list does not name the legacy ref: %q", prompt)
	}
	if strings.Contains(prompt, "Restored at boot") {
		t.Errorf("legacy ref was reported as restored: %q", prompt)
	}
	if _, err := os.Stat(filepath.Join(e.cwd, "legacy.txt")); err == nil {
		t.Error("legacy.txt was restored into the run checkout")
	}
}

// TestCloudBoot_RestartSkipsSaveThatSandboxStillHas: a resumed sandbox whose
// local branch already contains the saved tip gets no restore and no summary.
func TestCloudBoot_RestartSkipsSaveThatSandboxStillHas(t *testing.T) {
	e := newCBEnv(t)
	wipRepo(t, e)
	writeFileT(t, filepath.Join(e.cwd, "probe-wip.txt"), "attempt 1 work\n")
	failingLaunch(e, func() {})
	if err := e.start(); err == nil {
		t.Fatal("attempt 1: want the lead-gone error")
	}
	e.notes = nil
	failingLaunch(e, func() {})
	if err := e.start(); err == nil {
		t.Fatal("attempt 2: want the lead-gone error")
	}
	for _, n := range e.notes {
		if strings.HasPrefix(strings.TrimSpace(n), "wip restored:") {
			t.Errorf("note %q: a save the sandbox still has was restored", n)
		}
	}
	if prompt := launchAppendPrompt(e); strings.Contains(prompt, "Restored at boot") || strings.Contains(prompt, "Needs you") {
		t.Errorf("summary appended for a save already present: %q", prompt)
	}
}

// TestCloudBoot_SaveRecordsTrailers: the wip tip names its branch and worktree,
// and a clean worktree with an unpushed commit still gets a trailer commit.
func TestCloudBoot_SaveRecordsTrailers(t *testing.T) {
	e := newCBEnv(t)
	remote := wipRepo(t, e)
	wt := filepath.Join(t.TempDir(), "wt")
	gitT(t, e.cwd, "worktree", "add", "-b", "track-t", wt)
	writeFileT(t, filepath.Join(wt, "t.txt"), "t\n")
	gitT(t, wt, "add", "-A")
	gitT(t, wt, "commit", "-m", "committed, clean")
	failingLaunch(e, func() {})
	if err := e.start(); err == nil {
		t.Fatal("want the lead-gone error")
	}
	tr := func(ref, key string) string {
		return gitT(t, remote, "log", "-1", "--format=%(trailers:key="+key+",valueonly)", ref)
	}
	if got := tr("wip/at-ab12/track-t", "Ateam-Branch"); got != "track-t" {
		t.Errorf("Ateam-Branch = %q, want track-t", got)
	}
	if got := tr("wip/at-ab12/track-t", "Ateam-Worktree"); !sameDir(got, wt) {
		t.Errorf("Ateam-Worktree = %q, want %q", got, wt)
	}
}
