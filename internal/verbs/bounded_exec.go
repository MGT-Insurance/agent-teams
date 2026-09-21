//go:build unix

// Package verbs — bounded_exec.go is the shared bounded-exec helper for every
// `claude` CLI subprocess reap (and its sibling verbs) shell out to, plus the
// tunables that bound a reap scan tick.
//
// WHY: reap's three claude-CLI subprocesses (`claude agents`, `claude stop`,
// `claude rm`) used to run with no timeout and no process group. pr-shepherd
// SIGTERMs only the direct `ateam` child on its 30s budget, so an in-flight
// `claude` grandchild was orphaned and kept running — the seam that made a
// scan tick unable to finish under budget. runBoundedClaude closes that gap:
// every call gets a per-call timeout, and on cancel/timeout the WHOLE process
// group is killed (not just the direct child), so a grandchild claude spawns
// dies with it. Unix-only is correct here: the 4 shipped binaries are
// darwin/linux amd64/arm64 only (scripts/build-binaries.sh); no Windows
// target exists.
package verbs

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// claudeCallTimeout bounds every `claude` subprocess reap (and its siblings)
// shell out to. stop/rm/agents are all fast, interactive-adjacent calls, so
// 5s comfortably covers a healthy call while still closing well under
// pr-shepherd's 30s hard budget if one hangs.
const claudeCallTimeout = 5 * time.Second

// reapScanSoftDeadlineDefault is the SCAN mode default: `ateam reap` stops
// STARTING new survivors once this much wall-clock time has elapsed, so the
// process exits cleanly under pr-shepherd's 30s budget even with many
// survivors queued. 0 (settable via --scan-deadline=0) means unbounded — the
// same scan code path then drains the entire backlog in one run, which is
// what a human wants for a manual bulk clear.
const reapScanSoftDeadlineDefault = 15 * time.Second

// reapScanBatchDefault is the SCAN mode default per-tick batch bound (max
// survivors torn down per scan): 0 means unbounded, letting the soft deadline
// alone govern steady-state ticks.
const reapScanBatchDefault = 0

// runBoundedClaude runs `claude <args...>`, bounding it to timeout (derived
// from ctx) and killing the WHOLE process group on cancel/timeout —
// exec.CommandContext alone only kills the direct child, which orphans
// anything claude itself spawned. Returns stdout on success (the shape a JSON
// parser needs); on failure the returned error embeds combined stdout+stderr
// for diagnostics, matching what the call sites' CombinedOutput()-based error
// messages already carried.
func runBoundedClaude(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.Command("claude", args...)
	// Setpgid makes this child the leader of its own process group (pgid ==
	// its own pid), so -pid below targets the whole group, not just it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claude %s: start: %w", strings.Join(args, " "), err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			return stdout.Bytes(), fmt.Errorf("claude %s: %w (output: %s)", strings.Join(args, " "), err, combinedOutput(stdout.Bytes(), stderr.Bytes()))
		}
		return stdout.Bytes(), nil
	case <-cctx.Done():
		if cmd.Process != nil {
			// Kill the whole process group: a bare cmd.Process.Kill() only
			// signals the direct child, leaving any grandchild orphaned —
			// exactly the bug this helper exists to close.
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		<-done // reap the process so cmd.Wait's goroutine never leaks
		return stdout.Bytes(), fmt.Errorf("claude %s: %w (output: %s)", strings.Join(args, " "), cctx.Err(), combinedOutput(stdout.Bytes(), stderr.Bytes()))
	}
}

// combinedOutput joins stdout and stderr the way CombinedOutput() would,
// for embedding in an error message.
func combinedOutput(stdout, stderr []byte) string {
	if len(stderr) == 0 {
		return string(stdout)
	}
	if len(stdout) == 0 {
		return string(stderr)
	}
	return string(stdout) + string(stderr)
}
