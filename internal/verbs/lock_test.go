package verbs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// newLockCmd returns a condenseLockCmd whose clock is fixed at t0.
func newLockCmd(t0 time.Time) *condenseLockCmd {
	return &condenseLockCmd{nowFn: func() time.Time { return t0 }}
}

// writeStaleLock writes a lock file with a timestamp in the past.
func writeStaleLock(t *testing.T, path string, age time.Duration) {
	t.Helper()
	staleUnix := time.Now().Add(-age).Unix()
	content := fmt.Sprintf("pid\n%d\n", staleUnix)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("setup: write stale lock: %v", err)
	}
}

// TestCondenseLock_AcquireCreatesLockFile verifies acquire creates the lock file.
func TestCondenseLock_AcquireCreatesLockFile(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	cmd := newLockCmd(time.Now())

	if err := cmd.Run(ctx, []string{"acquire"}); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	path := condenseLockPath(home)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("lock file not created: %v", err)
	}
}

// TestCondenseLock_SecondAcquireReturnsHeld verifies that a second acquire
// while a fresh lock is held returns the condenseLockHeldCode sentinel.
func TestCondenseLock_SecondAcquireReturnsHeld(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	cmd := newLockCmd(now)

	ctx, _, _ := makeCtx(&fakeBD{}, home)
	if err := cmd.Run(ctx, []string{"acquire"}); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Second acquire should fail with condenseLockHeldCode.
	ctx2, _, _ := makeCtx(&fakeBD{}, home)
	err := cmd.Run(ctx2, []string{"acquire"})
	if err == nil {
		t.Fatal("expected held error on second acquire; got nil")
	}
	silent, ok := err.(*cli.SilentError)
	if !ok {
		t.Fatalf("expected *cli.SilentError, got %T: %v", err, err)
	}
	if silent.Code != condenseLockHeldCode {
		t.Errorf("held code = %d, want %d", silent.Code, condenseLockHeldCode)
	}
}

// TestCondenseLock_ReleaseRemovesLock verifies release removes the lock file
// so a subsequent acquire succeeds.
func TestCondenseLock_ReleaseRemovesLock(t *testing.T) {
	home := t.TempDir()
	cmd := newLockCmd(time.Now())
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	if err := cmd.Run(ctx, []string{"acquire"}); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	if err := cmd.Run(ctx, []string{"release"}); err != nil {
		t.Fatalf("release failed: %v", err)
	}

	path := condenseLockPath(home)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected lock file gone after release; stat err: %v", err)
	}

	// Subsequent acquire must succeed.
	if err := cmd.Run(ctx, []string{"acquire"}); err != nil {
		t.Fatalf("re-acquire after release failed: %v", err)
	}
}

// TestCondenseLock_StaleTimestampIsStolen verifies that a lock file whose
// timestamp is older than condenseLockStaleAge is stolen by a new acquire.
func TestCondenseLock_StaleTimestampIsStolen(t *testing.T) {
	home := t.TempDir()
	path := condenseLockPath(home)

	// Write a stale lock (11 minutes old — beyond the 10-minute threshold).
	writeStaleLock(t, path, 11*time.Minute)

	now := time.Now()
	cmd := newLockCmd(now)
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	if err := cmd.Run(ctx, []string{"acquire"}); err != nil {
		t.Fatalf("acquire on stale lock failed: %v", err)
	}

	// Lock file must still exist (stolen/overwritten, not deleted).
	if _, err := os.Stat(path); err != nil {
		t.Errorf("lock file should exist after steal; got: %v", err)
	}

	// Second acquire against the freshly-stolen lock must return held.
	ctx2, _, _ := makeCtx(&fakeBD{}, home)
	err := cmd.Run(ctx2, []string{"acquire"})
	if err == nil {
		t.Fatal("expected held after steal; got nil")
	}
	if _, ok := err.(*cli.SilentError); !ok {
		t.Fatalf("expected *cli.SilentError after steal; got %T: %v", err, err)
	}
}

// TestCondenseLock_ReleaseIdempotent verifies release on a missing lock is a no-op.
func TestCondenseLock_ReleaseIdempotent(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	cmd := newLockCmd(time.Now())

	if err := cmd.Run(ctx, []string{"release"}); err != nil {
		t.Fatalf("release on non-existent lock should be a no-op; got: %v", err)
	}
}

// TestCondenseLock_MissingActionReturnsUsageError verifies usage error when
// action is omitted.
func TestCondenseLock_MissingActionReturnsUsageError(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	err := (&condenseLockCmd{}).Run(ctx, nil)
	if err == nil {
		t.Fatal("expected usage error for missing action; got nil")
	}
}

// TestCondenseLock_UnknownActionReturnsUsageError verifies usage error for
// unknown action.
func TestCondenseLock_UnknownActionReturnsUsageError(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	err := (&condenseLockCmd{}).Run(ctx, []string{"badaction"})
	if err == nil {
		t.Fatal("expected usage error for unknown action; got nil")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("error should mention 'unknown action'; got: %v", err)
	}
}

// TestCondenseLock_NilContextReturnsError verifies nil context returns an error.
func TestCondenseLock_NilContextReturnsError(t *testing.T) {
	err := (&condenseLockCmd{}).Run(nil, []string{"acquire"})
	if err == nil {
		t.Fatal("expected error for nil context; got nil")
	}
}

// TestParseLockTimestamp verifies the lock file parser handles valid and
// invalid content correctly.
func TestParseLockTimestamp(t *testing.T) {
	ts, err := parseLockTimestamp("pid\n1234567890\n")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if ts != 1234567890 {
		t.Errorf("ts = %d, want 1234567890", ts)
	}

	_, err = parseLockTimestamp("malformed")
	if err == nil {
		t.Error("expected error for malformed content; got nil")
	}
}

// TestCondenseLock_LockFileContainsTimestamp verifies acquire writes a
// parseable timestamp.
func TestCondenseLock_LockFileContainsTimestamp(t *testing.T) {
	home := t.TempDir()
	fixed := time.Unix(1700000000, 0)
	cmd := newLockCmd(fixed)
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	if err := cmd.Run(ctx, []string{"acquire"}); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".condense.lock"))
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	ts, err := parseLockTimestamp(string(data))
	if err != nil {
		t.Fatalf("parse lock file: %v", err)
	}
	if ts != fixed.Unix() {
		t.Errorf("timestamp = %d, want %d", ts, fixed.Unix())
	}
}

// TestCondenseLock_HeldCodeIsDistinct verifies condenseLockHeldCode is not
// zero, 1, 2, 3, or 4 — those are reserved for other cli error types.
func TestCondenseLock_HeldCodeIsDistinct(t *testing.T) {
	reserved := map[int]string{
		0: "success",
		1: "generic error",
		2: "UsageError",
		3: "DepError",
		4: "WorkspaceError",
	}
	if name, conflict := reserved[condenseLockHeldCode]; conflict {
		t.Errorf("condenseLockHeldCode=%d conflicts with %q", condenseLockHeldCode, name)
	}
}
