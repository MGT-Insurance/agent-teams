package verbs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAcquireRelayLock_CleanHome_WritesPidfileAndKeepsFdOpen(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	lock, pid, err := acquireRelayLock(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer lock.Release()

	if pid != os.Getpid() {
		t.Errorf("acquireRelayLock pid = %d, want %d", pid, os.Getpid())
	}
	if lock == nil || lock.file == nil {
		t.Fatal("expected a lock with an open fd on success")
	}

	got, err := os.ReadFile(relayPidfilePath(ctx))
	if err != nil {
		t.Fatalf("expected pidfile written: %v", err)
	}
	if pidfileEntryPid(strings.TrimSpace(string(got))) != strconv.Itoa(os.Getpid()) {
		t.Errorf("pidfile content = %q, want pid %d", got, os.Getpid())
	}
}

// TestAcquireRelayLock_SecondAcquireFailsWithIncumbentPid confirms flock
// contends on two separate os.OpenFile calls made from the SAME process —
// flock locks are per-open-file-description, not per-process, so this is
// the behavior the bead's design depends on (verified here on darwin rather
// than assumed).
func TestAcquireRelayLock_SecondAcquireFailsWithIncumbentPid(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	lock1, pid1, err := acquireRelayLock(ctx)
	if err != nil {
		t.Fatalf("first acquire: unexpected error: %v", err)
	}
	defer lock1.Release()
	if pid1 != os.Getpid() {
		t.Errorf("first acquire pid = %d, want %d", pid1, os.Getpid())
	}

	lock2, pid2, err := acquireRelayLock(ctx)
	defer lock2.Release() // must be a safe no-op: lock2 is nil on a losing acquire
	if !errors.Is(err, errRelayLockHeld) {
		t.Fatalf("second acquire: expected errRelayLockHeld, got: %v", err)
	}
	if lock2 != nil {
		t.Error("expected a nil lock on a losing acquire")
	}
	if pid2 != os.Getpid() {
		t.Errorf("second acquire reported incumbent pid %d, want %d", pid2, os.Getpid())
	}
}

func TestRelayLock_Release_RemovesPidfileAndAllowsReacquire(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	lock, _, err := acquireRelayLock(ctx)
	if err != nil {
		t.Fatalf("acquire: unexpected error: %v", err)
	}

	lock.Release()
	if _, err := os.Stat(relayPidfilePath(ctx)); !os.IsNotExist(err) {
		t.Errorf("expected pidfile removed after Release, stat err: %v", err)
	}

	lock2, _, err := acquireRelayLock(ctx)
	if err != nil {
		t.Fatalf("re-acquire after Release: unexpected error: %v", err)
	}
	defer lock2.Release()
}

// TestAcquireRelayLock_StalePidfile_TakenOverWithoutManualCleanup is the
// anti-wedge guarantee this bead exists for. The seeded pidfile records a
// pid that IS currently alive (this test process's own pid) — the exact
// case a bare pidAlive check cannot distinguish from a live incumbent, and
// would refuse to take over, wedging the relay until a human deletes the
// file. Nobody actually holds the flock on it, though, so acquire must
// succeed and take over regardless of what the file's contents claim.
func TestAcquireRelayLock_StalePidfile_TakenOverWithoutManualCleanup(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	path := relayPidfilePath(ctx)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("seed mailbox dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d\t/some/other/home\n", os.Getpid())), 0o644); err != nil {
		t.Fatalf("seed stale pidfile: %v", err)
	}

	lock, pid, err := acquireRelayLock(ctx)
	if err != nil {
		t.Fatalf("expected takeover of an unflocked stale pidfile to succeed, got: %v", err)
	}
	defer lock.Release()
	if pid != os.Getpid() {
		t.Errorf("acquireRelayLock pid = %d, want %d", pid, os.Getpid())
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pidfile after acquire: %v", err)
	}
	if !strings.Contains(string(got), ctx.Home) {
		t.Errorf("expected pidfile overwritten with this process's home, got: %q", got)
	}
}

func TestRelayLock_Release_NilReceiver_NoPanic(t *testing.T) {
	var lock *relayLock
	lock.Release() // must not panic

	lock2 := &relayLock{}
	lock2.Release() // zero-value file field must not panic either
}
