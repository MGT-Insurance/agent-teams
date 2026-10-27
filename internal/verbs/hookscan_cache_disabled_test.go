package verbs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/repoconfig"
)

// TestHookScanCache_DisabledRepoWritesUntrusted confirms a scan that skips the
// unread count because the repo is disabled leaves an untrusted entry, so the
// first call after re-enable takes the full path and sees mail sent meanwhile.
func TestHookScanCache_DisabledRepoWritesUntrusted(t *testing.T) {
	home := t.TempDir()
	repoDir := t.TempDir()
	marker := filepath.Join(repoDir, repoconfig.FileName)
	if err := os.WriteFile(marker, []byte("disabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	init := bd.Issue{ID: "at-111", Description: "worktree: /a/b/wt\nrepo: " + repoDir + "\n"}

	stdout, _, calls := cacheScan(t, home, []bd.Issue{init}, cachePath1, "--session-id", "s1")
	if stdout != "" || calls != 1 {
		t.Fatalf("disabled: calls=%d stdout=%q", calls, stdout)
	}
	e, ok := getCache(t, home, "s1")
	if !ok || e.ID != "at-111" || e.Repo != repoDir || e.Trusted {
		t.Fatalf("cache = %+v ok=%v, want untrusted entry with id/repo", e, ok)
	}

	if err := os.WriteFile(marker, []byte("disabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mail := bd.Issue{ID: "at-m1", IssueType: "message", Assignee: "at-111", Status: "open"}
	stdout, _, calls = cacheScan(t, home, []bd.Issue{init, mail}, cachePath1, "--session-id", "s1")
	if calls != 1 || stdout != "id: at-111\nunread: 1\n" {
		t.Fatalf("after re-enable: calls=%d stdout=%q, want full scan finding mail", calls, stdout)
	}
}
