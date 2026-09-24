//go:build unix

// Package verbs — pull_stale_unix.go implements agent-teams-qdeh.3's
// stale-pull remedy: terminateHungPull, the package var pull_guard.go (the
// frozen contract, qdeh.1/qdeh.2) declares as its stale-remedy seam.
// Overriding it via init() here — rather than editing pull_guard.go — keeps
// this file disjoint from the contract and guard tracks.
//
// MECHANISM (ruled by Eric 2026-09-24, Q3): terminate only the hung pull's
// git/ssh transport, found by process ancestry from the dolt sql-server PID
// (recorded in <home>/.beads/dolt-server.pid) -> its `git fetch` child (or
// whichever git subcommand it is currently blocked on — see below) ->
// descendants. Never signal the server PID, never a negative/process-group
// signal; every PID's identity (pid + lstart + command) is re-read
// immediately before it is signaled, so a recycled PID is skipped rather
// than mis-signaled.
//
// EMPIRICAL KEYSTONE (agent-teams-qdeh.3, note on the bead has the full
// process trees): reproduced on an isolated scratch bd project with a
// GIT_SSH_COMMAND that hangs forever. Two findings changed this file's
// design from the bead's literal 2-step text:
//
//  1. Killing the ssh leaf does not reliably make its git parent exit — git
//     retried once, spawning a BRAND NEW ssh child under the SAME git PID.
//     Naively then killing the git parent from a STALE snapshot (one that
//     predates that respawn) leaks the new child as an orphan that runs
//     forever — the exact failure mode this remedy exists to avoid, one
//     level down. Fix: re-snapshot immediately before signaling a git
//     parent, and signal ITS then-current children in the same pass, before
//     the parent.
//  2. The identity recheck uses pid+lstart+command, not +ppid: a process's
//     ppid legitimately becomes 1 the instant its old parent dies (exactly
//     what happens to the orphan above mid-remedy), so ppid is not a stable
//     signal here — using it would make the safety check reject the very
//     orphan it must still clean up.
//
// The keystone's synthetic remote hangs at `git ls-remote` (the first
// network hop, since the fake remote never answers even a ref
// advertisement) rather than at the `git fetch ...refs/dolt/data` step the
// real incident's log shows — but the ancestry/kill mechanism is identical
// in both cases, and selectTransportTargets's match pattern is drawn from
// (and verified against) the real incident's log line, not the synthetic
// repro.
package verbs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/workspace"
)

func init() {
	terminateHungPull = terminateHungPullUnix
}

const (
	// staleRemedyPSTimeout bounds each `ps` exec the remedy issues.
	staleRemedyPSTimeout = 3 * time.Second

	// staleRemedySSHGrace is how long phase A waits, after signaling the
	// ssh-transport leaves, before re-snapshotting for phase B — long
	// enough for a git process to notice its transport died and exit on
	// its own (the outcome the real incident's log shows: a TCP reset
	// made git exit immediately and released every queued pull).
	staleRemedySSHGrace = 2 * time.Second

	// staleRemedySettleGrace is the short pause after phase B (killing the
	// git parent's then-current children plus the parent) before the
	// final processlist probe, giving the server a moment to notice the
	// query's subprocess exited.
	staleRemedySettleGrace = 1 * time.Second

	// staleRemedyProbeBudget bounds how long terminateHungPull waits for
	// p.ConnID to disappear from information_schema.processlist after the
	// remedy signals are sent, per the frozen decision table (pull_guard.go).
	staleRemedyProbeBudget = 5 * time.Second

	// staleRemedyProbePoll is the interval between processlist re-checks
	// within staleRemedyProbeBudget.
	staleRemedyProbePoll = 250 * time.Millisecond
)

// psEntry is one row of `ps -A -o pid,ppid,pgid,lstart,command` (or the
// single-PID `-p <pid>` form psLookupPID uses). PID+LStart+Command together
// are this process's identity signature for the PID-reuse guard: LStart
// (its exact start time) can't be forged by a later, unrelated process
// landing on the same recycled PID, unlike PPID, which legitimately changes
// the instant a process's parent dies (see the package doc comment above).
type psEntry struct {
	PID     int
	PPID    int
	PGID    int
	LStart  string
	Command string
}

// psSnapshotAll, psLookupPID, and signalPID are test seams over the real
// `ps`/kill calls: tests inject a fake process table and a spy signal
// sender instead of touching the live process tree.
var (
	psSnapshotAll = realPSSnapshotAll
	psLookupPID   = realPSLookupPID
	signalPID     = func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }
)

func realPSSnapshotAll(ctx context.Context) ([]psEntry, error) {
	cctx, cancel := context.WithTimeout(ctx, staleRemedyPSTimeout)
	defer cancel()
	out, _, err := runBoundedExec(cctx, "ps", "-A", "-o", "pid=,ppid=,pgid=,lstart=,command=")
	if err != nil {
		return nil, fmt.Errorf("ps -A: %w", err)
	}
	return parsePSOutput(string(out)), nil
}

func realPSLookupPID(ctx context.Context, pid int) (psEntry, bool, error) {
	cctx, cancel := context.WithTimeout(ctx, staleRemedyPSTimeout)
	defer cancel()
	out, _, err := runBoundedExec(cctx, "ps", "-p", strconv.Itoa(pid), "-o", "pid=,ppid=,pgid=,lstart=,command=")
	if err != nil {
		// ps exits non-zero once the pid no longer exists (both BSD/macOS
		// and Linux ps): treat any exec failure here as "not alive" rather
		// than a hard error, since that's overwhelmingly the actual cause.
		return psEntry{}, false, nil
	}
	entries := parsePSOutput(string(out))
	if len(entries) == 0 {
		return psEntry{}, false, nil
	}
	return entries[0], true, nil
}

// parsePSOutput parses `ps -o pid=,ppid=,pgid=,lstart=,command=` output.
// pid/ppid/pgid are the first three whitespace-delimited tokens; lstart is
// the next 5 (ctime-style "Www Mmm dd hh:mm:ss yyyy", itself space-separated
// so it can't be told apart from the trailing command by whitespace alone);
// everything after that is the command, verbatim spaces and all. Malformed
// rows (fewer than 8 tokens, or non-numeric pid/ppid/pgid) are skipped
// rather than erroring the whole snapshot.
func parsePSOutput(out string) []psEntry {
	var entries []psEntry
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		pgid, err3 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		entries = append(entries, psEntry{
			PID:     pid,
			PPID:    ppid,
			PGID:    pgid,
			LStart:  strings.Join(fields[3:8], " "),
			Command: strings.Join(fields[8:], " "),
		})
	}
	return entries
}

// readDoltServerFiles reads <home>/.beads/dolt-server.pid and
// dolt-server.port, the files bd's managed dolt sql-server writes on start.
func readDoltServerFiles(home string) (pid int, port string, err error) {
	pidBytes, err := os.ReadFile(filepath.Join(home, ".beads", "dolt-server.pid"))
	if err != nil {
		return 0, "", fmt.Errorf("read dolt-server.pid: %w", err)
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		return 0, "", fmt.Errorf("parse dolt-server.pid: %w", err)
	}
	portBytes, err := os.ReadFile(filepath.Join(home, ".beads", "dolt-server.port"))
	if err != nil {
		return 0, "", fmt.Errorf("read dolt-server.port: %w", err)
	}
	port = strings.TrimSpace(string(portBytes))
	if port == "" {
		return 0, "", errors.New("dolt-server.port: empty")
	}
	return pid, port, nil
}

// isDoltSQLServer reports whether command looks like the dolt sql-server
// bound to port: its first token's basename is "dolt" and it contains both
// "sql-server" and a "-P <port>" flag. This is the safety check (frozen,
// qdeh.3) that runs BEFORE any signal is sent.
func isDoltSQLServer(command, port string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 || filepath.Base(fields[0]) != "dolt" {
		return false
	}
	if !strings.Contains(command, "sql-server") {
		return false
	}
	return strings.Contains(command, "-P "+port) || strings.Contains(command, "-P"+port)
}

// verifyServerIdentity confirms entries contains a process at pid whose
// command is a dolt sql-server bound to port. Any mismatch — missing PID,
// or a command that isn't that server — is an error, and the caller must
// send NO signals at all: this check exists precisely to make a
// misconfigured or stale dolt-server.pid a no-op instead of a signal sent
// at the wrong process.
func verifyServerIdentity(entries []psEntry, pid int, port string) (psEntry, error) {
	for _, e := range entries {
		if e.PID != pid {
			continue
		}
		if !isDoltSQLServer(e.Command, port) {
			return psEntry{}, fmt.Errorf("terminateHungPull: pid %d is not a dolt sql-server on port %s (command: %q)", pid, port, e.Command)
		}
		return e, nil
	}
	return psEntry{}, fmt.Errorf("terminateHungPull: pid %d (from dolt-server.pid) not found in process table", pid)
}

// isGitFetchDoltData matches the real incident's observed hung command
// (~/.agent-teams/.beads/dolt-server.log, connection 207815 and its queued
// siblings): "git fetch --no-tags --refmap= origin +refs/dolt/data:...".
func isGitFetchDoltData(command string) bool {
	return strings.HasPrefix(command, "git fetch") && strings.Contains(command, "refs/dolt/data")
}

// descendantsOf returns every transitive descendant of rootPID found in
// entries (BFS over PPID), excluding guardPID (the dolt sql-server) from
// ever being selected even if some pathological entry claims it as a
// descendant.
func descendantsOf(entries []psEntry, rootPID, guardPID int) []psEntry {
	var out []psEntry
	seen := map[int]bool{rootPID: true}
	queue := []int{rootPID}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, e := range entries {
			if e.PPID != parent || e.PID == guardPID || seen[e.PID] {
				continue
			}
			seen[e.PID] = true
			out = append(out, e)
			queue = append(queue, e.PID)
		}
	}
	return out
}

// selectTransportTargets returns the dolt sql-server's (serverPID) direct
// git-fetch-refs/dolt/data children ("gitParents") plus every transitive
// descendant of those (their ssh transport, "descendants") — by ANCESTRY
// only: a same-named git process elsewhere in the process table (PPID !=
// serverPID) is never a target, and serverPID itself is never a target.
// Returns an error (no targets at all) if the server has no such child —
// guardedPull's stale branch treats that as a remedy failure, per the
// frozen decision table.
func selectTransportTargets(entries []psEntry, serverPID int) (descendants, gitParents []psEntry, err error) {
	for _, e := range entries {
		if e.PID == serverPID || e.PPID != serverPID {
			continue
		}
		if isGitFetchDoltData(e.Command) {
			gitParents = append(gitParents, e)
		}
	}
	if len(gitParents) == 0 {
		return nil, nil, fmt.Errorf("terminateHungPull: no git fetch (refs/dolt/data) child found under dolt sql-server pid %d", serverPID)
	}
	for _, g := range gitParents {
		descendants = append(descendants, descendantsOf(entries, g.PID, serverPID)...)
	}
	return descendants, gitParents, nil
}

// identityMatches reports whether current is still the same process
// original described — see the package doc comment for why this compares
// pid+lstart+command and deliberately NOT ppid.
func identityMatches(original, current psEntry) bool {
	return original.PID == current.PID &&
		original.LStart == current.LStart &&
		original.Command == current.Command
}

// signalIfUnchanged re-reads target's current identity via psLookupPID
// immediately before signaling — the PID-reuse guard the contract requires
// — and sends sig only if it still matches target and target isn't
// serverPID. Returns true if a signal was actually sent.
func signalIfUnchanged(ctx context.Context, target psEntry, serverPID int, sig syscall.Signal) (bool, error) {
	if target.PID == serverPID {
		return false, fmt.Errorf("terminateHungPull: refusing to signal server pid %d", serverPID)
	}
	current, alive, err := psLookupPID(ctx, target.PID)
	if err != nil {
		return false, err
	}
	if !alive {
		return false, nil // already gone
	}
	if !identityMatches(target, current) {
		return false, nil // recycled pid; leave it alone
	}
	if err := signalPID(target.PID, sig); err != nil {
		return false, fmt.Errorf("signal pid %d: %w", target.PID, err)
	}
	return true, nil
}

// terminateHungPullUnix is terminateHungPull's real implementation
// (overridden onto the pull_guard.go package var by this file's init).
// Two phases, per the keystone note on this bead:
//
//	Phase A: SIGTERM every currently-known ssh-transport descendant. Wait
//	         staleRemedySSHGrace for git to notice and exit on its own.
//	Phase B: re-snapshot (load-bearing: a git parent that survived phase A
//	         may have retried and spawned a NEW ssh child — killing the
//	         parent from a stale view of its children orphans that child).
//	         Signal each surviving git parent's THEN-CURRENT children,
//	         then the parent itself.
//
// Then probe information_schema.processlist for p.ConnID until it's gone or
// staleRemedyProbeBudget elapses.
func terminateHungPullUnix(ctx context.Context, p inFlightPull) error {
	home := workspace.Home()

	serverPID, port, err := readDoltServerFiles(home)
	if err != nil {
		return fmt.Errorf("terminateHungPull: %w", err)
	}

	snapshot, err := psSnapshotAll(ctx)
	if err != nil {
		return fmt.Errorf("terminateHungPull: %w", err)
	}

	serverEntry, err := verifyServerIdentity(snapshot, serverPID, port)
	if err != nil {
		return err // no signals sent
	}

	descendants, gitParents, err := selectTransportTargets(snapshot, serverEntry.PID)
	if err != nil {
		return err // no signals sent
	}

	// Phase A: ssh-transport leaves first.
	for _, d := range descendants {
		if _, err := signalIfUnchanged(ctx, d, serverEntry.PID, syscall.SIGTERM); err != nil {
			return fmt.Errorf("terminateHungPull: phase A: %w", err)
		}
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(staleRemedySSHGrace):
	}

	// Phase B: re-snapshot before touching any git parent, so we signal
	// its CURRENT children (which may not be the ones from the initial
	// snapshot — see the package doc comment) before the parent itself.
	fresh, err := psSnapshotAll(ctx)
	if err != nil {
		return fmt.Errorf("terminateHungPull: phase B snapshot: %w", err)
	}
	for _, g := range gitParents {
		current, alive, err := psLookupPID(ctx, g.PID)
		if err != nil {
			return fmt.Errorf("terminateHungPull: phase B: recheck git pid %d: %w", g.PID, err)
		}
		if !alive || !identityMatches(g, current) {
			continue // exited on its own, or pid recycled; nothing to do
		}
		for _, d := range descendantsOf(fresh, current.PID, serverEntry.PID) {
			if _, err := signalIfUnchanged(ctx, d, serverEntry.PID, syscall.SIGTERM); err != nil {
				return fmt.Errorf("terminateHungPull: phase B: %w", err)
			}
		}
		if _, err := signalIfUnchanged(ctx, current, serverEntry.PID, syscall.SIGTERM); err != nil {
			return fmt.Errorf("terminateHungPull: phase B: %w", err)
		}
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(staleRemedySettleGrace):
	}

	return waitForPullCleared(ctx, home, p.ConnID)
}

// waitForPullCleared polls information_schema.processlist (via
// probeInFlightPull) until connID is no longer the in-flight DOLT_PULL, or
// staleRemedyProbeBudget elapses.
func waitForPullCleared(ctx context.Context, home string, connID int64) error {
	c := bd.NewClient(home)
	deadline := time.Now().Add(staleRemedyProbeBudget)
	for {
		row, err := probeInFlightPull(ctx, c)
		if err == nil && (row == nil || row.ConnID != connID) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("terminateHungPull: conn %d still in flight after %s", connID, staleRemedyProbeBudget)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(staleRemedyProbePoll):
		}
	}
}
