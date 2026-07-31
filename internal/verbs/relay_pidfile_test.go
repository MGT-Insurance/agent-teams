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

// TestRelayLock_Release_DoesNotUnlinkAnAcquirerThatWonDuringRelease pins the
// unlink-before-close ordering in Release (agent-teams-25c5.1 delta): a real
// goroutine racing real OS scheduling can't be forced to land inside a
// microsecond window reliably, so this uses releaseAfterUnlinkHook — fired
// by Release exactly between its unlink and its close — as a deterministic
// stand-in for "another acquireRelayLock call happens in that window."
//
// From the hook, a second acquire is attempted at exactly that instant. With
// the pidfile already unlinked (the fixed order), that acquire opens a
// brand-new inode and wins cleanly; the outer Release's subsequent Close
// only drops the first lock's now-orphaned fd, which cannot affect the new
// inode. The assertion is on the FINAL on-disk state once Release fully
// returns: the second acquirer's pidfile must still be there.
//
// Mutation-tested: reverting Release to close-then-unlink makes this test
// fail, because the hook then fires with the first lock's flock already
// dropped but its pidfile still linked, so the second acquire opens and
// re-locks that SAME inode in place — and Release's next line (now
// os.Remove, unchanged path) deletes the directory entry the second
// acquirer just became the legitimate holder of.
func TestRelayLock_Release_DoesNotUnlinkAnAcquirerThatWonDuringRelease(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	lockA, _, err := acquireRelayLock(ctx)
	if err != nil {
		t.Fatalf("first acquire: unexpected error: %v", err)
	}

	var lockB *relayLock
	var pidB int
	var errB error
	t.Cleanup(func() { releaseAfterUnlinkHook = nil })
	releaseAfterUnlinkHook = func() {
		lockB, pidB, errB = acquireRelayLock(ctx)
	}

	lockA.Release()
	if lockB != nil {
		defer lockB.Release()
	}

	if errB != nil {
		t.Fatalf("acquire during the unlink/close window: unexpected error: %v", errB)
	}
	if pidB != os.Getpid() {
		t.Errorf("acquire during the unlink/close window: pid = %d, want %d", pidB, os.Getpid())
	}

	got, err := os.ReadFile(relayPidfilePath(ctx))
	if err != nil {
		t.Fatalf("expected the winning acquirer's pidfile to survive lockA.Release(), stat/read err: %v", err)
	}
	if pidfileEntryPid(strings.TrimSpace(string(got))) != strconv.Itoa(os.Getpid()) {
		t.Errorf("pidfile content after Release = %q, want the second acquirer's pid %d", got, os.Getpid())
	}
}

func TestRelayLock_Release_NilReceiver_NoPanic(t *testing.T) {
	var lock *relayLock
	lock.Release() // must not panic

	lock2 := &relayLock{}
	lock2.Release() // zero-value file field must not panic either
}
