// Package verbs — reap_orphans.go implements the `ateam reap-orphans` verb.
//
// Background sessions whose git-worktree cwd was deleted become orphans that
// flood the harness with ENOENT posix_spawn errors on every hook invocation.
// This verb enumerates live sessions, identifies background ones with a missing
// cwd, and stops them with `claude stop <id>`.
package verbs

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// dirExistsFunc is the function type for checking whether a directory exists.
// Injected so tests can substitute a fake without real filesystem ops.
type dirExistsFunc func(path string) bool

// stopSessionFunc is the function type for stopping a claude session.
// Injected so tests can substitute a fake without executing real claude commands.
type stopSessionFunc func(id string) error

// RegisterReapOrphansKong registers the reap-orphans verb onto p.
func RegisterReapOrphansKong(p *cli.Parser) {
	p.AddVerb("reap-orphans", "Stop background sessions whose worktree cwd no longer exists.", &reapOrphansKong{
		agentsFunc:  defaultAgentsJSON,
		dirExists:   defaultDirExists,
		stopSession: defaultStopSession,
	})
}

// reapOrphansKong implements `ateam reap-orphans`.
// The three DI fields are tagged kong:"-" so kong ignores them;
// tests substitute fakes without touching the struct registration.
type reapOrphansKong struct {
	agentsFunc  agentsJSONFunc  `kong:"-"`
	dirExists   dirExistsFunc   `kong:"-"`
	stopSession stopSessionFunc `kong:"-"`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *reapOrphansKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam reap-orphans: nil context")
	}

	// Dependency guard: claude must be in PATH.
	if _, err := exec.LookPath("claude"); err != nil {
		return cli.Depf("ateam reap-orphans: 'claude' not found in PATH")
	}

	sessions, err := c.agentsFunc()
	if err != nil {
		return fmt.Errorf("ateam reap-orphans: list sessions: %w", err)
	}

	// Belt-and-suspenders: never stop the calling session.
	callerID := os.Getenv("CLAUDE_SESSION_ID")

	var reaped int
	var foundAnyOrphan bool

	for _, s := range sessions {
		id := sessionStopID(s)

		// Skip the calling session by any of its ids.
		if callerID != "" && (id == callerID || s.SessionID == callerID || s.ID == callerID) {
			continue
		}

		cwdMissing := !c.dirExists(s.CWD)

		if s.Kind == "background" && cwdMissing {
			foundAnyOrphan = true
			fmt.Fprintf(ctx.Stdout, "reaping background orphan %s cwd=%s\n", id, s.CWD)
			if err := c.stopSession(id); err != nil {
				fmt.Fprintf(ctx.Stderr, "reap-orphans: stop %s: %v\n", id, err)
			} else {
				reaped++
			}
			continue
		}

		// Non-background (interactive) session with a missing cwd: advisory only.
		// MUST NOT be stopped — human decides.
		if s.Kind != "background" && cwdMissing {
			fmt.Fprintf(ctx.Stdout, "skipped interactive orphan %s cwd=%s — stop manually if desired\n", id, s.CWD)
		}
	}

	if !foundAnyOrphan {
		fmt.Fprintln(ctx.Stdout, "no orphaned background sessions found")
	} else {
		fmt.Fprintf(ctx.Stdout, "reaped %d background orphan session(s)\n", reaped)
	}
	return nil
}

// sessionStopID returns the id to pass to `claude stop` for a session.
// Background sessions have a short ID; fall back to SessionID if ID is empty,
// then to Name as a last resort.
func sessionStopID(s agentSession) string {
	if s.ID != "" {
		return s.ID
	}
	if s.SessionID != "" {
		return s.SessionID
	}
	return s.Name
}

// defaultDirExists reports whether path is an existing directory.
func defaultDirExists(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.IsDir()
}

// defaultStopSession runs `claude stop <id>`.
func defaultStopSession(id string) error {
	cmd := exec.Command("claude", "stop", id)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude stop %s: %w (output: %s)", id, err, string(out))
	}
	return nil
}
