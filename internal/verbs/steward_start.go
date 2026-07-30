// This file implements `ateam steward start` (agent-teams-e3mq.33): the
// one-command sanctioned Steward launch. It composes stewardInit (steward.go)
// with two pre-flight checks and a launch exec, mirroring the manual sequence
// documented in plugins/agent-teams/skills/steward/SKILL.md:
//
//	ateam steward init && cd ~/.agent-teams/steward/session && \
//	  claude --bg --permission-mode bypassPermissions \
//	    --settings '{"env":{"ATEAM_ROLE":"steward"}}' "/agent-teams:steward"
package verbs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// stewardStartKong is the kong struct for `ateam steward start`.
// agentsFunc, launchFunc, killFunc, and relaySpawnFunc are DI seams so tests
// can substitute fakes without querying/exec-ing real claude/ateam processes
// or sending real signals; kong:"-" keeps kong from treating them as flags.
type stewardStartKong struct {
	agentsFunc     agentsJSONFunc    `kong:"-"`
	launchFunc     stewardLaunchFunc `kong:"-"`
	killFunc       stewardKillFunc   `kong:"-"`
	relaySpawnFunc relaySpawnFunc    `kong:"-"`
}

// Run initializes the Steward session directory, refuses to launch a second
// live Steward, cleans up an orphaned watcher pidfile when safe to do so, and
// then execs the sanctioned launch command with its cwd set to the session
// directory.
func (c *stewardStartKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam steward start: nil context")
	}

	agentsFunc := c.agentsFunc
	if agentsFunc == nil {
		agentsFunc = defaultAgentsJSONAll
	}
	launch := c.launchFunc
	if launch == nil {
		launch = defaultStewardLaunch
	}
	kill := c.killFunc
	if kill == nil {
		kill = defaultStewardKill
	}
	spawnRelay := c.relaySpawnFunc
	if spawnRelay == nil {
		spawnRelay = defaultRelaySpawn
	}

	// 1. Init — same idempotent core `ateam steward init` uses.
	sessionDir, err := stewardInit(ctx)
	if err != nil {
		return fmt.Errorf("ateam steward start: init: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "steward session: %s\n", sessionDir)

	// 2. Singleton pre-flight: refuse if a live Steward session is already
	// running. Fail-soft on the introspection tool itself (missing `claude`
	// on PATH, or `claude agents` erroring) — a broken query must not block
	// launch; only a *confirmed* live session refuses.
	sessions, agentsErr := agentsFunc()
	if agentsErr != nil {
		fmt.Fprintf(ctx.Stderr, "ateam steward start: warning: could not query live sessions (%v); skipping singleton check and orphan-watcher cleanup\n", agentsErr)
	} else if live := findLiveStewardSession(sessions, sessionDir); live != nil {
		id := live.ID
		if id == "" {
			id = live.SessionID
		}
		return fmt.Errorf(
			"ateam steward start: refusing to launch: a live steward session is already running (%s) — "+
				"attach with `claude attach %s` or stop it with `claude stop %s`",
			id, id, id)
	} else {
		// 3. Orphan-watcher hygiene — only reached when the query SUCCEEDED
		// and found zero live steward sessions, i.e. a live watcher pid can be
		// provably attributed to no legitimate owner. On query failure a live
		// watcher pid can't be ruled out as belonging to an incumbent steward,
		// so it must NOT be killed — doing so would free the watcher slot for
		// a duplicate steward to claim, the exact takeover e3mq.29/e3mq.30
		// closed.
		cleanOrphanStewardWatcher(ctx, kill)
	}

	// 4. Launch. claude's own stdout (including the session id it prints) is
	// streamed straight through so the human sees it.
	if err := launch(ctx, sessionDir); err != nil {
		return fmt.Errorf("ateam steward start: launch: %w", err)
	}

	// 5. Ensure the singleton relay is running (agent-teams-5y8a.4,
	// supersedes agent-teams-17xs.6): the Steward's own session is up as of
	// step 4 above, so a relay failure here is reported and NOT propagated —
	// failing the whole command over relay supervision would make a
	// successfully-launched Steward session look like a failed `steward
	// start`. Fail-soft, loud on stderr, same posture as the agentsFunc
	// query failure in step 2 above.
	if err := ensureRelayRunning(ctx, spawnRelay); err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam steward start: warning: relay: %v — steward is running but relay was not started; run `ateam relay` manually\n", err)
	}

	return nil
}

// findLiveStewardSession returns the first session in sessions whose cwd
// matches sessionDir (canonicalPath-normalised) AND carries a live pid, or
// nil if none match. This mirrors the liveness rule
// session-start-inbox.sh's duplicate-advisory branch uses (`.pid != null`) —
// NOT SKILL.md step 0's `.state != "done"`. The two shell call sites disagree
// on which field to key on; PID presence is the one this package's own
// agentSession doc comment (messaging.go) declares authoritative ("Never
// branch on State; it's unreliable. Only PID presence and Status matter."),
// so that's the rule this pre-flight check follows too.
func findLiveStewardSession(sessions []agentSession, sessionDir string) *agentSession {
	want := canonicalPath(sessionDir)
	for i := range sessions {
		if canonicalPath(sessions[i].CWD) == want && sessions[i].PID != nil {
			return &sessions[i]
		}
	}
	return nil
}

// stewardWatcherPidfilePath returns the path to the Steward's wake-watcher
// pidfile: <ctx.Home>/mailbox/steward.watcher.pid. Mirrors
// checkStewardInboxGuard's construction in messaging.go.
func stewardWatcherPidfilePath(ctx *cli.Context) string {
	return filepath.Join(ctx.Home, "mailbox", StewardHandle+".watcher.pid")
}

// cleanOrphanStewardWatcher inspects the Steward's watcher pidfile (only
// called once step 2 in stewardStartKong.Run has established no live steward
// session exists, so nothing legitimately owns it) and clears whatever it
// finds:
//   - no pidfile -> nothing to clean, silent.
//   - dead or unparseable pid -> remove the stale pidfile.
//   - live pid -> an orphan watcher (this exact orphan left a relaunched
//     steward deaf twice in live testing): kill it via kill (SIGTERM, briefly
//     confirmed dead, SIGKILL if it survives), then remove the pidfile.
//
// Prints one line describing what, if anything, was cleaned.
func cleanOrphanStewardWatcher(ctx *cli.Context, kill stewardKillFunc) {
	path := stewardWatcherPidfilePath(ctx)
	data, err := os.ReadFile(path)
	if err != nil {
		return // no pidfile (or unreadable) -> nothing to clean
	}

	entry := strings.TrimRight(string(data), "\n")
	pid, perr := strconv.Atoi(pidfileEntryPid(entry))
	if perr != nil || pid <= 0 || !pidAlive(pid) {
		_ = os.Remove(path)
		fmt.Fprintf(ctx.Stdout, "cleaned: removed stale watcher pidfile %s\n", path)
		return
	}

	kill(pid)
	_ = os.Remove(path)
	fmt.Fprintf(ctx.Stdout, "cleaned: killed orphan watcher (pid %d) and removed pidfile %s\n", pid, path)
}

// stewardKillFunc terminates the process at pid. Injected so tests can
// substitute a fake rather than sending real signals; production wiring is
// defaultStewardKill.
type stewardKillFunc func(pid int)

// defaultStewardKill sends SIGTERM to pid, briefly polls for death (pidAlive,
// up to ~1s), and escalates to SIGKILL if the process survives.
func defaultStewardKill(pid int) {
	_ = syscall.Kill(pid, syscall.SIGTERM)
	for i := 0; i < 10; i++ {
		if !pidAlive(pid) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

// stewardLaunchFunc launches the Steward's background session with its
// working directory set to dir. Injected so tests can substitute a fake
// without exec-ing a real claude binary; production wiring is
// defaultStewardLaunch.
type stewardLaunchFunc func(ctx *cli.Context, dir string) error

// stewardSettingsJSON is the --settings JSON argument for the Steward launch,
// publishing ATEAM_ROLE=steward per the role-signal contract
// (agent-teams-142k.1). No ATEAM_INITIATIVE and no autoCompactWindow request:
// the steward is fleet-scoped (no single initiative id), and no background
// session pins an auto-compact window — see bgSessionSettings in dispatch.go
// for why. Same payload shape bgSessionSettingsJSON("steward", "") produces.
const stewardSettingsJSON = `{"env":{"ATEAM_ROLE":"steward"}}`

// stewardLaunchArgs returns the argv slice (everything after "claude") for
// the sanctioned Steward launch. Pure: extracted so tests can assert the argv
// without exec-ing a real claude binary.
func stewardLaunchArgs() []string {
	return []string{
		"--bg",
		"--permission-mode", "bypassPermissions",
		"--settings", stewardSettingsJSON,
		"/agent-teams:steward",
	}
}

// defaultStewardLaunch execs the sanctioned Steward launch command —
// `claude --bg --permission-mode bypassPermissions --settings
// '{"env":{"ATEAM_ROLE":"steward"}}' /agent-teams:steward` — with its working
// directory set to dir via exec.Command's .Dir (never os.Chdir, which would
// change the whole ateam process's cwd). claude's own stdout/stderr,
// including the background session id it prints, are streamed straight
// through to ctx.Stdout/ctx.Stderr.
func defaultStewardLaunch(ctx *cli.Context, dir string) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return cli.Depf("ateam steward start: 'claude' not found in PATH")
	}
	cmd := exec.Command("claude", stewardLaunchArgs()...)
	cmd.Dir = dir
	cmd.Stdout = ctx.Stdout
	cmd.Stderr = ctx.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude --bg: %w", err)
	}
	return nil
}
