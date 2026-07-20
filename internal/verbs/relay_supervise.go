// This file implements the launch-story fold of `ateam relay` into the
// Steward's lifecycle (agent-teams-5y8a.4, superseding agent-teams-17xs.6):
// `ateam relay` (relay.go) is a plain OS process that long-polls a transport
// and blocks in transport.Receive until killed — nothing supervises it
// today, so a Steward with no relay is deaf unless a human runs `ateam
// relay` by hand and keeps it alive. ensureRelayRunning (called from `ateam
// steward start`, steward_start.go) and teardownRelay (called from `ateam
// steward remove`, steward.go) give it a singleton-guarded lifecycle,
// mirroring the wake-watcher pidfile pattern
// (mailbox/<id>.watcher.pid, cleanOrphanStewardWatcher in steward_start.go)
// but simpler: relay is not tied to any Claude session, so there's no
// `claude agents` cross-check needed — presence of the pidfile plus
// pidAlive is the whole liveness test.
package verbs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// relaySpawnFunc launches a new background `ateam relay` process and returns
// its pid without waiting for it to exit. Injected so tests can substitute a
// fake rather than exec-ing a real binary; production wiring is
// defaultRelaySpawn.
type relaySpawnFunc func(ctx *cli.Context) (int, error)

// relayPidfilePath returns the path to the relay singleton's pidfile:
// <ctx.Home>/mailbox/relay.pid.
func relayPidfilePath(ctx *cli.Context) string {
	return filepath.Join(ctx.Home, "mailbox", "relay.pid")
}

// ensureRelayRunning makes sure exactly one background `ateam relay` process
// is running for this machine, singleton-guarded by relayPidfilePath:
//
//   - a live pid in the pidfile -> left running untouched.
//   - no pidfile, or a stale/dead/unparseable one -> the stale pidfile (if
//     any) is reaped, then a new `ateam relay` is spawned via spawn and its
//     pid recorded.
//
// The per-machine singleton also protects the single-consumer bot token —
// two relays polling the same transport would race the provider's receive
// endpoint (e.g. Telegram getUpdates 409).
func ensureRelayRunning(ctx *cli.Context, spawn relaySpawnFunc) error {
	path := relayPidfilePath(ctx)

	if data, err := os.ReadFile(path); err == nil {
		pid, perr := strconv.Atoi(strings.TrimSpace(string(data)))
		if perr == nil && pid > 0 && pidAlive(pid) {
			fmt.Fprintf(ctx.Stdout, "relay: already running (pid %d)\n", pid)
			return nil
		}
		_ = os.Remove(path)
		fmt.Fprintf(ctx.Stdout, "relay: reaped stale pidfile %s\n", path)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create mailbox dir: %w", err)
	}
	pid, err := spawn(ctx)
	if err != nil {
		return fmt.Errorf("spawn relay: %w", err)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return fmt.Errorf("write relay pidfile: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "relay: started (pid %d)\n", pid)
	return nil
}

// defaultRelaySpawn execs `ateam relay` as a detached background OS process
// — NOT a Claude session; relay is a plain blocking poll loop
// (transport.Receive in relay.go blocks until killed) — and returns its pid
// without waiting for it to exit. stdout/stderr are redirected to
// mailbox/relay.log since there is no terminal to stream them to (the
// parent `ateam steward start` invocation has already returned by the time
// relay produces output), and the child is placed in its own session
// (Setsid) so it survives the parent's process group and isn't killed by a
// controlling-terminal hangup.
func defaultRelaySpawn(ctx *cli.Context) (int, error) {
	if _, err := exec.LookPath("ateam"); err != nil {
		return 0, cli.Depf("ensure relay running: 'ateam' not found in PATH")
	}

	mailboxDir := filepath.Join(ctx.Home, "mailbox")
	if err := os.MkdirAll(mailboxDir, 0o755); err != nil {
		return 0, fmt.Errorf("create mailbox dir: %w", err)
	}
	logFile, err := os.OpenFile(filepath.Join(mailboxDir, "relay.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open relay log: %w", err)
	}
	defer logFile.Close() // the child holds its own fd (dup'd by os/exec at Start); safe to close ours after

	cmd := exec.Command("ateam", "relay")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start ateam relay: %w", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release() // fire-and-forget: steward start does not wait for relay to exit
	return pid, nil
}

// teardownRelay stops the background `ateam relay` process (if any) tracked
// by relayPidfilePath and removes the pidfile — the counterpart to
// ensureRelayRunning, called from `ateam steward remove` (steward.go) to
// tear the relay down alongside the session. Idempotent: no pidfile is not
// an error. A live pid is stopped via kill (stewardKillFunc's existing
// SIGTERM-then-SIGKILL behavior, defined in steward_start.go and reused
// as-is since nothing about it is steward-specific); a dead/unparseable
// pidfile is just removed. Returns the pid that was stopped, or 0 if none
// was running (whether because no pidfile existed or its pid was already
// dead).
func teardownRelay(ctx *cli.Context, kill stewardKillFunc) int {
	path := relayPidfilePath(ctx)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	defer os.Remove(path)

	pid, perr := strconv.Atoi(strings.TrimSpace(string(data)))
	if perr != nil || pid <= 0 || !pidAlive(pid) {
		return 0
	}
	kill(pid)
	return pid
}
