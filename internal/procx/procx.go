//go:build unix

// Package procx provides the one bounded-subprocess-exec implementation the
// repo's several process-group-kill-on-timeout callers delegate to, instead
// of each keeping its own copy of the pgid-kill dance.
//
// WHY: a bare exec.CommandContext only kills the direct child on
// cancellation/timeout; anything that child itself spawned (a grandchild) is
// orphaned and keeps running. RunBounded closes that gap by making the child
// the leader of its own process group and killing the whole group.
//
// Unix-only is correct here: the shipped ateam binaries are darwin/linux
// amd64/arm64 only (scripts/build-binaries.sh); no Windows target exists.
package procx

import (
	"bytes"
	"context"
	"os/exec"
	"syscall"
)

// RunBounded runs name(args...), killing the WHOLE process group on ctx's
// cancellation/timeout, and returns split stdout/stderr. ctx already carries
// whatever deadline the caller wants: passing the SAME already-deadlined ctx
// into several sequential calls gives them one shared combined budget rather
// than a fresh full budget each.
//
// On success, err is cmd.Wait's error (nil, or a non-zero-exit
// *exec.ExitError). On ctx expiry before the command exits, err is ctx.Err()
// (so errors.Is(err, context.DeadlineExceeded) holds for a caller that set a
// deadline), and the whole process group — including any grandchild the
// command spawned — is killed before RunBounded returns.
func RunBounded(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.Command(name, args...)
	// Setpgid makes this child the leader of its own process group (pgid ==
	// its own pid), so -pid below targets the whole group, not just it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if startErr := cmd.Start(); startErr != nil {
		return nil, nil, startErr
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case waitErr := <-done:
		return outBuf.Bytes(), errBuf.Bytes(), waitErr
	case <-ctx.Done():
		// Kill the whole process group: a bare cmd.Process.Kill() only
		// signals the direct child, leaving any grandchild orphaned.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done // reap the process so cmd.Wait's goroutine never leaks
		return outBuf.Bytes(), errBuf.Bytes(), ctx.Err()
	}
}
