// kong_illp_test.go: core-path tests for the kong structs converted in bead
// agent-teams-illp (match, status, watchers, worktree_setup, lock).
package verbs

import (
	"os"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── condense-lock kong struct ─────────────────────────────────────────────────

// TestCondenseLockKong_AcquireReleaseRoundtrip verifies acquire creates the lock
// and release removes it via the native kong struct.
func TestCondenseLockKong_AcquireReleaseRoundtrip(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	cmd := &condenseLockKong{Action: "acquire"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	path := condenseLockPath(home)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("lock file missing after acquire: %v", err)
	}

	rel := &condenseLockKong{Action: "release"}
	if err := rel.Run(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("lock file still present after release; stat: %v", err)
	}
}

// TestCondenseLockKong_SecondAcquireHeld verifies the held sentinel fires when
// a fresh lock is already acquired.
func TestCondenseLockKong_SecondAcquireHeld(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	cmd := &condenseLockKong{Action: "acquire"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// Inject a frozen clock so the lock is never stale.
	inner := &condenseLockCmd{nowFn: func() time.Time { return time.Now() }}
	ctx2, _, _ := makeCtx(&fakeBD{}, home)
	err := inner.Run(ctx2, []string{"acquire"})
	if err == nil {
		t.Fatal("expected held error; got nil")
	}
	silent, ok := err.(*cli.SilentError)
	if !ok {
		t.Fatalf("expected *cli.SilentError, got %T: %v", err, err)
	}
	if silent.Code != condenseLockHeldCode {
		t.Errorf("code = %d, want %d", silent.Code, condenseLockHeldCode)
	}
}

// ── resumeMatchKong ───────────────────────────────────────────────────────────

// TestResumeMatchKong_NilContextErrors verifies nil context returns an error.
func TestResumeMatchKong_NilContextErrors(t *testing.T) {
	cmd := &resumeMatchKong{WorktreePath: "/some/path"}
	if err := cmd.Run(nil); err == nil {
		t.Fatal("expected error for nil context; got nil")
	}
}

// TestResumeMatchClosedKong_NilContextErrors verifies nil context returns an error.
func TestResumeMatchClosedKong_NilContextErrors(t *testing.T) {
	cmd := &resumeMatchClosedKong{WorktreePath: "/some/path"}
	if err := cmd.Run(nil); err == nil {
		t.Fatal("expected error for nil context; got nil")
	}
}

// ── auditKong ─────────────────────────────────────────────────────────────────

// TestAuditKong_NilContextErrors verifies nil context returns an error.
func TestAuditKong_NilContextErrors(t *testing.T) {
	if err := (&auditKong{}).Run(nil); err == nil {
		t.Fatal("expected error for nil context; got nil")
	}
}

// ── executionStatusKong ───────────────────────────────────────────────────────

// TestExecutionStatusKong_NilContextErrors verifies nil context returns an error.
func TestExecutionStatusKong_NilContextErrors(t *testing.T) {
	cmd := &executionStatusKong{agentsFunc: defaultAgentsJSON}
	if err := cmd.Run(nil); err == nil {
		t.Fatal("expected error for nil context; got nil")
	}
}

// ── watchersKong ──────────────────────────────────────────────────────────────

// TestWatchersKong_NilContextErrors verifies nil context returns an error.
func TestWatchersKong_NilContextErrors(t *testing.T) {
	cmd := &watchersKong{agentsFunc: defaultAgentsJSON}
	if err := cmd.Run(nil); err == nil {
		t.Fatal("expected error for nil context; got nil")
	}
}

// ── worktreeSetupKong ─────────────────────────────────────────────────────────

// fakeWtGit satisfies the wtGitRunner interface used by worktreeSetupKong.
type fakeWtGit struct{}

func (f *fakeWtGit) RepoRoot(dir string) (string, error) { return dir, nil }
func (f *fakeWtGit) CommonDir(dir string) (string, error) { return dir + "/.git", nil }

// TestWorktreeSetupKong_NilContextErrors verifies nil context returns an error.
func TestWorktreeSetupKong_NilContextErrors(t *testing.T) {
	cmd := &worktreeSetupKong{git: &fakeWtGit{}, runner: defaultCmdRunner}
	if err := cmd.Run(nil); err == nil {
		t.Fatal("expected error for nil context; got nil")
	}
}
