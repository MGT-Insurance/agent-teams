// This file is owned by Track A (watcher-state reconciliation).
package verbs

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// openInitiativesFunc is the function type for loading open initiatives.
// Injected so tests can supply a fake and avoid shelling to real bd.
type openInitiativesFunc func() ([]bd.Issue, error)

// RegisterWatchers adds the watchers verb to reg.
func RegisterWatchers(reg cli.Registry) {
	reg.Register(&watchersCmd{
		agentsFunc:      defaultAgentsJSON,
		initiativesFunc: nil, // nil signals: use ctx.BD at runtime
	})
}

// RegisterWatchersKong registers watcher verbs onto p. Initially bridges all
// verbs from RegisterWatchers; ring-track conversion replaces each bridge with a
// native kong struct in this function without touching any other file.
// Note: watchersCmd has injected agentsFunc/initiativesFunc fields — mark those
// kong:"-" when converting to a native struct.
func RegisterWatchersKong(p *cli.Parser) {
	bridgeTrack(p, RegisterWatchers)
}

// watchersCmd implements `ateam watchers`.
type watchersCmd struct {
	// agentsFunc is injected so tests can substitute a fake without touching os/exec.
	agentsFunc agentsJSONFunc
	// initiativesFunc is injected so tests can supply open initiatives without
	// shelling to bd. When nil, Run loads from ctx.BD.
	initiativesFunc openInitiativesFunc
}

func (c *watchersCmd) Name() string { return "watchers" }

// Run implements: ateam watchers
//
// For each open initiative: reads its watcher pidfile, checks liveness, and
// joins against live Claude sessions. Also scans mailbox/*.watcher.pid for
// pidfiles whose initiative id is not in the open set (orphaned/stale).
//
// Output columns: id, title, watcher-state, pid, live-session.
func (c *watchersCmd) Run(ctx *cli.Context, _ []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam watchers: nil context")
	}

	// Load open initiatives — either injected (tests) or via ctx.BD.
	var issues []bd.Issue
	if c.initiativesFunc != nil {
		var err error
		issues, err = c.initiativesFunc()
		if err != nil {
			return fmt.Errorf("ateam watchers: list initiatives: %w", err)
		}
	} else {
		if err := ctx.BD.RunJSON(&issues, "list", "--status=open", "--json"); err != nil {
			return fmt.Errorf("ateam watchers: list initiatives: %w", err)
		}
	}

	// Load live sessions. Graceful degrade: on failure, report session as 'unknown'.
	sessions, agentsErr := c.agentsFunc()

	// Build a set of open initiative ids for the orphan scan below.
	openIDs := make(map[string]struct{}, len(issues))
	for _, iss := range issues {
		openIDs[iss.ID] = struct{}{}
	}

	mailboxDir := filepath.Join(ctx.Home, "mailbox")

	// Print header.
	fmt.Fprintf(ctx.Stdout, "%-20s %-36s %-16s %-8s %s\n",
		"ID", "TITLE", "WATCHER-STATE", "PID", "LIVE-SESSION")

	// Per open initiative row.
	for _, iss := range issues {
		wt := worktreePath(iss.Description)
		pidFile := filepath.Join(mailboxDir, iss.ID+".watcher.pid")

		state, pid := watcherState(pidFile)

		liveSession := "unknown"
		if agentsErr == nil {
			if hasLiveSession(sessions, wt) {
				liveSession = "yes"
			} else {
				liveSession = "no"
			}
		}

		pidStr := "-"
		if pid > 0 {
			pidStr = strconv.Itoa(pid)
		}

		title := truncate(iss.Title, 36)
		fmt.Fprintf(ctx.Stdout, "%-20s %-36s %-16s %-8s %s\n",
			iss.ID, title, state, pidStr, liveSession)
	}

	// Orphan scan: pidfiles for non-open initiatives.
	orphans, err := filepath.Glob(filepath.Join(mailboxDir, "*.watcher.pid"))
	if err != nil {
		// Glob only errors on malformed patterns; treat as no orphans.
		orphans = nil
	}
	for _, pidFile := range orphans {
		base := filepath.Base(pidFile)
		// Strip ".watcher.pid" suffix to get the initiative id.
		id := strings.TrimSuffix(base, ".watcher.pid")
		if _, open := openIDs[id]; open {
			continue // already reported above
		}
		// Orphaned pidfile — initiative is not open. Either way it is an anomaly
		// (a watcher must not exist for a non-open initiative), so never "OK":
		//   - pid alive -> ORPHAN-RUNNING (a live watcher with no open initiative; needs killing)
		//   - pid dead/invalid -> STALE-PIDFILE (a leftover file; needs cleanup)
		baseState, pid := watcherState(pidFile)
		orphanState := "STALE-PIDFILE"
		if baseState == "OK" {
			orphanState = "ORPHAN-RUNNING"
		}
		pidStr := "-"
		if pid > 0 {
			pidStr = strconv.Itoa(pid)
		}
		liveSession := "unknown"
		if agentsErr == nil {
			liveSession = "no" // orphan: no worktree to match against
		}
		fmt.Fprintf(ctx.Stdout, "%-20s %-36s %-16s %-8s %s\n",
			id, "<orphan>", orphanState, pidStr, liveSession)
	}

	return nil
}

// watcherState reads the pidfile at path and returns the watcher state plus
// the pid it found (0 when absent or unreadable).
//
// States:
//   - "OK"             — pidfile present and pid is alive
//   - "STALE-PIDFILE"  — pidfile present but pid is dead
//   - "MISSING-WATCHER"— no pidfile (or unreadable)
func watcherState(pidFile string) (state string, pid int) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		// No pidfile.
		return "MISSING-WATCHER", 0
	}
	pidStr := strings.TrimSpace(string(data))
	p, err := strconv.Atoi(pidStr)
	if err != nil || p <= 0 {
		// Pidfile present but contents invalid — treat as stale.
		return "STALE-PIDFILE", 0
	}
	if pidAlive(p) {
		return "OK", p
	}
	return "STALE-PIDFILE", p
}

// pidAlive reports whether the process with the given pid exists.
// Uses signal 0: no error (or EPERM) means the process exists.
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// truncate shortens s to at most n runes, appending "…" if cut.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}
