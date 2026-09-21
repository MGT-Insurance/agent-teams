//go:build unix

package verbs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// writeFakeClaude writes an executable shell script named "claude" into a
// fresh temp dir and prepends that dir to PATH for the test, so
// runBoundedClaude — which always invokes the literal "claude" binary —
// exercises this script instead of requiring a real CLI on PATH.
func writeFakeClaude(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatalf("write fake claude script: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestRunBoundedClaude_KillsWholeProcessGroup proves the core contract of the
// contract bead: on timeout, runBoundedClaude kills the WHOLE process group,
// so a grandchild the "claude" process itself spawned dies too — not just
// the direct child. exec.CommandContext's default cancellation only kills
// the direct child, which would leave a grandchild orphaned and running —
// the exact bug this helper exists to close.
func TestRunBoundedClaude_KillsWholeProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild.pid")
	// Background a long sleep (the grandchild — a child of the "claude"
	// process, in the same process group since we don't setsid it), record
	// its pid, then have the "claude" process itself also outlive the
	// timeout so the call is still in-flight when the group kill fires.
	writeFakeClaude(t, fmt.Sprintf("sleep 5 &\necho $! > %s\nsleep 5\n", pidFile))

	// A 1s budget (not a tighter one) so the fake "claude" reliably reaches
	// `echo $! > pidFile` and records the grandchild before the group kill
	// fires — under heavy machine load a sub-second budget kills the shell
	// mid-startup, before it writes the pid, and the test cannot observe the
	// grandchild it means to assert on. The grandchild sleeps 5s, so a 1s
	// budget still leaves the call in-flight when the timeout kills the group.
	_, err := runBoundedClaude(context.Background(), 1*time.Second, "agents", "--json")
	if err == nil {
		t.Fatal("expected an error from a call that outlives its timeout")
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
		t.Fatalf("grandchild pid %d is still alive after the timed-out call returned — the process group was not fully killed (only the direct child was)", pid)
	}
}

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

// TestRunBoundedClaude_SuccessReturnsStdout proves the happy path: a call
// well under the timeout returns its stdout with a nil error.
func TestRunBoundedClaude_SuccessReturnsStdout(t *testing.T) {
	writeFakeClaude(t, "printf hello")

	out, err := runBoundedClaude(context.Background(), claudeCallTimeout, "agents", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "hello" {
		t.Fatalf("expected stdout %q, got %q", "hello", out)
	}
}

// TestRunBoundedClaude_NonZeroExit_ErrorEmbedsOutput proves a non-timeout
// failure (nonzero exit) returns an error embedding combined stdout+stderr,
// matching the diagnostic text the call sites' own CombinedOutput()-based
// error messages carried before this helper existed.
func TestRunBoundedClaude_NonZeroExit_ErrorEmbedsOutput(t *testing.T) {
	writeFakeClaude(t, `echo "boom" >&2; exit 1`)

	_, err := runBoundedClaude(context.Background(), claudeCallTimeout, "rm", "some-id")
	if err == nil {
		t.Fatal("expected a non-nil error for a nonzero exit")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected the error to embed stderr output; got %v", err)
	}
}
