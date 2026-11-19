package verbs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

const (
	// cloudBootDiagTimeout bounds each diagnostic command on resume.
	cloudBootDiagTimeout = 5 * time.Second
	// cloudBootDiagKernelLines is how much of the kernel log tail is logged.
	cloudBootDiagKernelLines = 30
)

// cloudDiagFunc runs a diagnostic command, bounded by ctx, and returns stdout.
type cloudDiagFunc func(ctx context.Context, name string, args ...string) (string, error)

// kmsgPath is the dmesg fallback; passed as the command name to a cloudDiagFunc
// it means "read this file", which keeps the seam a single function.
const kmsgPath = "/dev/kmsg"

func runDiagBounded(ctx context.Context, name string, args ...string) (string, error) {
	if name == kmsgPath {
		return readKmsg(ctx)
	}
	ctx, cancel := context.WithTimeout(ctx, cloudBootDiagTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = cloudBootWaitDelay
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// lastLines returns the last n lines of s, trimmed of trailing newlines.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// logResumeDiagnostics logs memory and kernel-log context at the start of a
// resume, to explain a prior run that vanished (for example an OOM kill). It
// is fail-open: every failure is a one-line note, and each command is bounded.
func (c *cloudBootCommon) logResumeDiagnostics(ctx *cli.Context) {
	run := c.Diag
	if run == nil {
		run = runDiagBounded
	}
	bctx := context.Background()

	if out, err := run(bctx, "free", "-m"); err != nil {
		fmt.Fprintf(ctx.Stderr, "cloud-boot: resume diag: free -m unavailable: %v\n", err)
	} else {
		fmt.Fprintf(ctx.Stderr, "cloud-boot: resume diag: free -m\n%s\n", strings.TrimRight(out, "\n"))
	}

	out, err := run(bctx, "dmesg")
	src := "dmesg"
	if err != nil {
		derr := err
		out, err = run(bctx, kmsgPath)
		src = kmsgPath
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "cloud-boot: resume diag: kernel log unreadable, skipped: dmesg: %v; /dev/kmsg: %v\n", derr, err)
			return
		}
	}
	fmt.Fprintf(ctx.Stderr, "cloud-boot: resume diag: kernel log tail (%s, last %d lines)\n%s\n",
		src, cloudBootDiagKernelLines, lastLines(out, cloudBootDiagKernelLines))
}

// readKmsg reads /dev/kmsg without blocking at the end of the buffer, bounded
// by the diagnostic timeout, and returns the records read (raw kmsg format).
func readKmsg(ctx context.Context) (string, error) {
	f, err := os.OpenFile(kmsgPath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	ctx, cancel := context.WithTimeout(ctx, cloudBootDiagTimeout)
	defer cancel()
	var lines []string
	buf := make([]byte, 8192)
	for ctx.Err() == nil {
		n, err := f.Read(buf)
		if err != nil || n == 0 {
			break // EAGAIN at the end of the buffer, or EPIPE on overrun
		}
		lines = append(lines, strings.TrimRight(string(buf[:n]), "\n"))
	}
	if len(lines) == 0 {
		return "", errors.New("no records")
	}
	return strings.Join(lines, "\n"), nil
}
