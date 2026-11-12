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
