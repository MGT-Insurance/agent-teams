// This file implements the flock-backed singleton lock `ateam relay` uses to
// self-register (agent-teams-25c5.1): the pidfile at relayPidfilePath is now
// claimed via an advisory kernel flock rather than by convention, so a
// hand-started `ateam relay` and a Steward-spawned one contend on the SAME
// lock instead of one being invisible to the other. The lock is released by
// the kernel the instant the holder's fd closes — by any means (normal
// return, os.Exit, panic, SIGTERM, SIGKILL, or a machine reboot) — so a
// crashed relay's claim is torn down automatically: a dead pid left in the
// file can never wedge a future acquire, unlike a bare pidAlive check, which
// can't distinguish "this pid is alive and holds the lock" from "this pid
// was recycled by an unrelated process."
package verbs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// relayPidfilePath returns the path to the relay singleton's pidfile:
// <ctx.Home>/mailbox/relay.pid.
func relayPidfilePath(ctx *cli.Context) string {
	return filepath.Join(ctx.Home, "mailbox", "relay.pid")
}

// errRelayLockHeld is returned by acquireRelayLock when another live relay
// already holds the singleton lock.
var errRelayLockHeld = errors.New("relay lock already held by another process")

// relayLock is an acquired singleton claim on the relay pidfile: the open
// *os.File backing it holds an advisory flock for as long as the fd stays
// open. Zero value is not valid; obtain one from acquireRelayLock.
type relayLock struct {
	file *os.File
	path string
}

// acquireRelayLock claims the relay singleton lock at relayPidfilePath. On
// success, the returned *relayLock owns an open, flocked file recording this
// process's pid — keep it alive for the relay's lifetime and Release() it on
// exit. A stale pidfile (dead holder, or none actually holding the flock) is
// taken over silently; no manual cleanup is ever required. On failure
// because another live relay holds the lock, acquireRelayLock returns a nil
// lock, the incumbent's pid (best-effort, 0 if unreadable), and
// errRelayLockHeld — callers must never Release() a lock from a losing
// acquire, since there is none to release (Release is nil-receiver-safe, so
// `defer lock.Release()` after a losing acquire is a harmless no-op, not a
// removal of the incumbent's pidfile).
func acquireRelayLock(ctx *cli.Context) (*relayLock, int, error) {
	path := relayPidfilePath(ctx)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, 0, fmt.Errorf("create mailbox dir: %w", err)
	}

	// O_CREATE|O_RDWR, NOT O_TRUNC: a losing acquire must still be able to
	// read the incumbent's pid back out of the file below.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, 0, fmt.Errorf("open relay pidfile: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if err != syscall.EWOULDBLOCK {
			f.Close()
			return nil, 0, fmt.Errorf("flock relay pidfile: %w", err)
		}
		data, _ := os.ReadFile(path)
		f.Close()
		pid, _ := strconv.Atoi(pidfileEntryPid(strings.TrimSpace(string(data))))
		return nil, pid, errRelayLockHeld
	}

	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, 0, fmt.Errorf("truncate relay pidfile: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		f.Close()
		return nil, 0, fmt.Errorf("seek relay pidfile: %w", err)
	}
	pid := os.Getpid()
	if _, err := fmt.Fprintf(f, "%d\t%s\n", pid, ctx.Home); err != nil {
		f.Close()
		return nil, 0, fmt.Errorf("write relay pidfile: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return nil, 0, fmt.Errorf("sync relay pidfile: %w", err)
	}

	return &relayLock{file: f, path: path}, pid, nil
}

// releaseAfterUnlinkHook, when non-nil, is invoked by Release immediately
// after the pidfile has been unlinked but before the flock-holding fd is
// closed. Test-only synchronization point for pinning the unlink-before-
// close ordering below deterministically — the window it guards is a few
// syscalls wide and not reliably reproducible by racing real goroutines
// against real OS scheduling. Always nil outside relay_pidfile_test.go.
var releaseAfterUnlinkHook func()

// Release drops this lock's claim on the pidfile: unlinks the path FIRST,
// while the flock is still held, and only THEN closes the fd (which drops
// the flock). This order is load-bearing, not incidental. Closing before
// unlinking opens a window where another process's acquireRelayLock can
// open the still-linked pidfile, win the now-free flock on that SAME
// inode, and become the legitimate new holder — and then this Release's
// later os.Remove unlinks the entry out from under it, leaving that live
// holder with no pidfile on disk at all (invisible to ensureRelayRunning,
// which spawns a duplicate relay, and to teardownRelay, which then can't
// find it to stop). Unlinking first closes that window: any acquirer that
// opens the path after this point necessarily gets a brand-new inode
// (O_CREATE against a missing path), entirely independent of the one this
// Close() drops — so this process's cleanup can never invalidate a claim
// it doesn't hold. Safe to call on a nil receiver and idempotent — a
// losing acquireRelayLock returns a nil *relayLock, and a second Release
// on an already-released lock is a no-op, so neither ever touches a
// pidfile this process doesn't own.
func (l *relayLock) Release() {
	if l == nil || l.file == nil {
		return
	}
	_ = os.Remove(l.path)
	if releaseAfterUnlinkHook != nil {
		releaseAfterUnlinkHook()
	}
	_ = l.file.Close()
	l.file = nil
}
