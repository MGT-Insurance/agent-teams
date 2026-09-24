//go:build unix

package procx_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/procx"
)

// processGoneWithin polls until pid no longer exists (signal 0 fails, most
// commonly with ESRCH) or the deadline passes.
func processGoneWithin(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// TestRunBounded_TimeoutKillsWholeProcessGroup proves the core contract of
// the contract bead: on ctx timeout, RunBounded kills the WHOLE process
// group, so a grandchild the command itself spawned (here, a backgrounded
// `sleep 30`) dies too — not just the direct `sh` child — and the returned
// error wraps context.DeadlineExceeded.
func TestRunBounded_TimeoutKillsWholeProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild.pid")
	// `sleep 30 & echo $! > pidFile; wait` backgrounds the grandchild,
	// records its pid, then waits on it — matching the acceptance criterion's
	// `sh -c 'sleep 30 & wait'` shape with pid capture added so the test can
	// observe the grandchild.
	script := "sleep 30 & echo $! > " + pidFile + "\nwait\n"

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, err := procx.RunBounded(ctx, "sh", "-c", script)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error from a call that outlives its ctx deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected err to wrap context.DeadlineExceeded, got %v", err)
	}
	if elapsed > 1*time.Second {
		t.Fatalf("RunBounded took %v to return after a 200ms deadline, want under 1s", elapsed)
	}

	var pidBytes []byte
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, readErr := os.ReadFile(pidFile); readErr == nil && len(strings.TrimSpace(string(b))) > 0 {
			pidBytes = b
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(pidBytes) == 0 {
		t.Fatal("grandchild never wrote its pid — test setup is broken")
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if parseErr != nil {
		t.Fatalf("parse grandchild pid %q: %v", pidBytes, parseErr)
	}
	if !processGoneWithin(pid, 2*time.Second) {
		t.Fatalf("grandchild pid %d is still alive after the timed-out call returned — the process group was not fully killed", pid)
	}
}

// TestRunBounded_SuccessReturnsStdout proves normal completion is
// unaffected: a call well under its deadline returns its stdout with a nil
// error.
func TestRunBounded_SuccessReturnsStdout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, _, err := procx.RunBounded(ctx, "printf", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "hello" {
		t.Fatalf("expected stdout %q, got %q", "hello", out)
	}
}

// TestRunBounded_NonZeroExit_NoTimeout proves a non-timeout failure (nonzero
// exit, well within the deadline) is passed through as a plain error, not
// mistaken for a ctx timeout.
func TestRunBounded_NonZeroExit_NoTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, stderr, err := procx.RunBounded(ctx, "sh", "-c", `echo boom >&2; exit 1`)
	if err == nil {
		t.Fatal("expected a non-nil error for a nonzero exit")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("nonzero-exit error should not wrap context.DeadlineExceeded: %v", err)
	}
	if !strings.Contains(string(stderr), "boom") {
		t.Fatalf("expected stderr to contain %q, got %q", "boom", stderr)
	}
}
