package verbs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

func TestStewardStart_NoPriorState_InitsAndLaunches(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	var launchCalled bool
	var launchDir string
	cmd := &stewardStartKong{
		agentsFunc: func() ([]agentSession, error) { return nil, nil },
		launchFunc: func(ctx *cli.Context, dir string) error {
			launchCalled = true
			launchDir = dir
			return nil
		},
		killFunc:       func(pid int) {},
		relaySpawnFunc: func(ctx *cli.Context) (int, error) { return 4242, nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sessionDir := StewardSessionDir(ctx)
	if !launchCalled {
		t.Fatal("expected launch to be invoked")
	}
	if launchDir != sessionDir {
		t.Errorf("launch dir = %q, want %q", launchDir, sessionDir)
	}
	if _, err := os.Stat(StewardSessionMarkerPath(ctx)); err != nil {
		t.Errorf("expected session marker to exist: %v", err)
	}
	if !strings.Contains(stdout.String(), sessionDir) {
		t.Errorf("expected stdout to mention session dir, got: %q", stdout.String())
	}
}

func TestStewardStart_LiveSteward_Refuses(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	sessionDir := StewardSessionDir(ctx)

	livePID := 4242
	var launchCalled bool
	cmd := &stewardStartKong{
		agentsFunc: func() ([]agentSession, error) {
			return []agentSession{{CWD: sessionDir, PID: &livePID, ID: "abc123"}}, nil
		},
		launchFunc: func(ctx *cli.Context, dir string) error {
			launchCalled = true
			return nil
		},
		killFunc:       func(pid int) {},
		relaySpawnFunc: func(ctx *cli.Context) (int, error) { return 4242, nil },
	}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected refusal error, got nil")
	}
	if !strings.Contains(err.Error(), "abc123") {
		t.Errorf("expected error to name session id abc123, got: %v", err)
	}
	if launchCalled {
		t.Error("expected launch NOT to be invoked when a live steward session exists")
	}
}

func TestStewardStart_LiveSteward_FallsBackToSessionIDWhenShortIDEmpty(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	sessionDir := StewardSessionDir(ctx)

	livePID := 4242
	cmd := &stewardStartKong{
		agentsFunc: func() ([]agentSession, error) {
			return []agentSession{{CWD: sessionDir, PID: &livePID, SessionID: "full-session-id"}}, nil
		},
		relaySpawnFunc: func(ctx *cli.Context) (int, error) { return 4242, nil },
	}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected refusal error, got nil")
	}
	if !strings.Contains(err.Error(), "full-session-id") {
		t.Errorf("expected error to fall back to full session id, got: %v", err)
	}
}

func TestStewardStart_DeadPidfile_CleansAndProceeds(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	pidfile := stewardWatcherPidfilePath(ctx)
	if err := os.MkdirAll(filepath.Dir(pidfile), 0o755); err != nil {
		t.Fatalf("seed mailbox dir: %v", err)
	}
	const deadPid = 9999999
	if err := os.WriteFile(pidfile, []byte(strconv.Itoa(deadPid)), 0o644); err != nil {
		t.Fatalf("seed dead pidfile: %v", err)
	}

	var launchCalled bool
	var killCalled bool
	cmd := &stewardStartKong{
		agentsFunc: func() ([]agentSession, error) { return nil, nil },
		launchFunc: func(ctx *cli.Context, dir string) error {
			launchCalled = true
			return nil
		},
		killFunc:       func(pid int) { killCalled = true },
		relaySpawnFunc: func(ctx *cli.Context) (int, error) { return 4242, nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if killCalled {
		t.Error("expected kill NOT to be called for a dead pidfile")
	}
	if !launchCalled {
		t.Error("expected launch to be invoked")
	}
	if _, err := os.Stat(pidfile); !os.IsNotExist(err) {
		t.Errorf("expected stale pidfile removed, stat err: %v", err)
	}
	if !strings.Contains(stdout.String(), "cleaned") {
		t.Errorf("expected a cleaned-pidfile note in stdout, got: %q", stdout.String())
	}
}

func TestStewardStart_LiveOrphanWatcher_KillsCleansAndProceeds(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	// Spawn a real short-lived subprocess to act as the orphaned watcher —
	// sending SIGTERM to os.Getpid() would kill the test runner itself, so a
	// real child process is required to safely exercise the kill path. A
	// direct child stays a zombie (still "alive" to kill(pid,0)/pidAlive)
	// until reaped, so death is detected via a background Wait() rather than
	// by polling pidAlive after the fact.
	sleeper := exec.Command("sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Fatalf("spawn sleeper: %v", err)
	}
	orphanPID := sleeper.Process.Pid
	exited := make(chan struct{})
	go func() {
		_ = sleeper.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = sleeper.Process.Kill()
	})

	pidfile := stewardWatcherPidfilePath(ctx)
	if err := os.MkdirAll(filepath.Dir(pidfile), 0o755); err != nil {
		t.Fatalf("seed mailbox dir: %v", err)
	}
	if err := os.WriteFile(pidfile, []byte(fmt.Sprintf("%d\tsome-other-session", orphanPID)), 0o644); err != nil {
		t.Fatalf("seed live orphan pidfile: %v", err)
	}

	var launchCalled bool
	cmd := &stewardStartKong{
		agentsFunc: func() ([]agentSession, error) { return nil, nil },
		launchFunc: func(ctx *cli.Context, dir string) error {
			launchCalled = true
			return nil
		},
		killFunc:       defaultStewardKill,
		relaySpawnFunc: func(ctx *cli.Context) (int, error) { return 4242, nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !launchCalled {
		t.Error("expected launch to be invoked")
	}
	if _, err := os.Stat(pidfile); !os.IsNotExist(err) {
		t.Errorf("expected orphan pidfile removed, stat err: %v", err)
	}
	if !strings.Contains(stdout.String(), "killed orphan watcher") {
		t.Errorf("expected orphan-kill note in stdout, got: %q", stdout.String())
	}

	// Confirm the process actually died (SIGTERM should be sufficient for `sleep`).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(orphanPID) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("expected orphan pid %d to be dead after cleanup", orphanPID)
}

func TestStewardStart_NoPidfile_ProceedsSilently(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	var launchCalled bool
	cmd := &stewardStartKong{
		agentsFunc: func() ([]agentSession, error) { return nil, nil },
		launchFunc: func(ctx *cli.Context, dir string) error {
			launchCalled = true
			return nil
		},
		killFunc:       func(pid int) {},
		relaySpawnFunc: func(ctx *cli.Context) (int, error) { return 4242, nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !launchCalled {
		t.Error("expected launch to be invoked")
	}
	if strings.Contains(stdout.String(), "cleaned") {
		t.Errorf("expected no cleanup note when no pidfile exists, got: %q", stdout.String())
	}
}

func TestStewardStart_AgentsQueryFails_WarnsAndProceeds(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, stderr := makeCtx(&fakeBD{}, home)

	// Seed the pidfile with a real, currently-alive pid. On a broken `claude
	// agents` query, a live watcher pid can't be attributed as an orphan (an
	// incumbent steward might legitimately own it), so start must skip
	// hygiene entirely and leave it untouched — killing it here would free
	// the watcher slot for a duplicate steward, the exact takeover
	// e3mq.29/e3mq.30 closed (review finding on 398d3c3).
	sleeper := exec.Command("sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Fatalf("spawn sleeper: %v", err)
	}
	livePID := sleeper.Process.Pid
	t.Cleanup(func() {
		_ = sleeper.Process.Kill()
		_ = sleeper.Wait()
	})

	pidfile := stewardWatcherPidfilePath(ctx)
	if err := os.MkdirAll(filepath.Dir(pidfile), 0o755); err != nil {
		t.Fatalf("seed mailbox dir: %v", err)
	}
	pidfileContent := fmt.Sprintf("%d\tsome-other-session", livePID)
	if err := os.WriteFile(pidfile, []byte(pidfileContent), 0o644); err != nil {
		t.Fatalf("seed live pidfile: %v", err)
	}

	var launchCalled bool
	var killCalled bool
	cmd := &stewardStartKong{
		agentsFunc: func() ([]agentSession, error) { return nil, fmt.Errorf("claude not found") },
		launchFunc: func(ctx *cli.Context, dir string) error {
			launchCalled = true
			return nil
		},
		killFunc:       func(pid int) { killCalled = true },
		relaySpawnFunc: func(ctx *cli.Context) (int, error) { return 4242, nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !launchCalled {
		t.Error("expected launch to be invoked despite agents query failure (fail-soft)")
	}
	if !strings.Contains(stderr.String(), "warning") {
		t.Errorf("expected a warning on stderr, got: %q", stderr.String())
	}
	if killCalled {
		t.Error("expected kill NOT to be called when the agents query fails — a live pid can't be attributed as an orphan")
	}
	got, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("expected pidfile to remain untouched, stat err: %v", err)
	}
	if string(got) != pidfileContent {
		t.Errorf("expected pidfile content untouched, got: %q", got)
	}
	if strings.Contains(stdout.String(), "cleaned") {
		t.Errorf("expected no cleanup note when the agents query fails, got: %q", stdout.String())
	}
}

func TestStewardStart_LaunchFailure_Propagates(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	cmd := &stewardStartKong{
		agentsFunc: func() ([]agentSession, error) { return nil, nil },
		launchFunc: func(ctx *cli.Context, dir string) error {
			return fmt.Errorf("boom")
		},
		killFunc:       func(pid int) {},
		relaySpawnFunc: func(ctx *cli.Context) (int, error) { return 4242, nil },
	}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected launch failure to propagate, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected propagated error to contain underlying cause, got: %v", err)
	}
}

func TestStewardStart_LaunchInvokedWithSessionDir(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	var gotDir string
	cmd := &stewardStartKong{
		agentsFunc: func() ([]agentSession, error) { return nil, nil },
		launchFunc: func(ctx *cli.Context, dir string) error {
			gotDir = dir
			return nil
		},
		killFunc:       func(pid int) {},
		relaySpawnFunc: func(ctx *cli.Context) (int, error) { return 4242, nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := StewardSessionDir(ctx); gotDir != want {
		t.Errorf("launch dir = %q, want %q", gotDir, want)
	}
}

func TestStewardStart_NilContext(t *testing.T) {
	if err := (&stewardStartKong{}).Run(nil); err == nil {
		t.Fatal("expected error for nil context")
	}
}

// ── --relay flag (agent-teams-25c5.3) ───────────────────────────────────────

// TestStewardStart_NoRelayFlag_DoesNotSpawnRelay confirms the flipped default:
// without --relay, ensureRelayRunning is never reached — relaySpawnFunc is not
// invoked and no relay pidfile is written.
func TestStewardStart_NoRelayFlag_DoesNotSpawnRelay(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	var spawnCalled bool
	cmd := &stewardStartKong{
		agentsFunc: func() ([]agentSession, error) { return nil, nil },
		launchFunc: func(ctx *cli.Context, dir string) error { return nil },
		killFunc:   func(pid int) {},
		relaySpawnFunc: func(ctx *cli.Context) (int, error) {
			spawnCalled = true
			return 4242, nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spawnCalled {
		t.Error("expected relaySpawnFunc NOT to be invoked without --relay")
	}
	if _, err := os.Stat(relayPidfilePath(ctx)); !os.IsNotExist(err) {
		t.Errorf("expected no relay pidfile written without --relay, stat err: %v", err)
	}
}

// TestStewardStart_NoRelayFlag_WritesNoRelayNote confirms the else-branch
// this bead adds: without --relay, stdout carries a "relay: " prefixed note
// that inbound replies need a running relay.
func TestStewardStart_NoRelayFlag_WritesNoRelayNote(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	cmd := &stewardStartKong{
		agentsFunc:     func() ([]agentSession, error) { return nil, nil },
		launchFunc:     func(ctx *cli.Context, dir string) error { return nil },
		killFunc:       func(pid int) {},
		relaySpawnFunc: func(ctx *cli.Context) (int, error) { return 4242, nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "relay: not started") {
		t.Errorf("expected a \"relay: not started\" note in stdout, got: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "inbound replies need a running relay") {
		t.Errorf("expected stdout to explain inbound replies need a running relay, got: %q", stdout.String())
	}
}

// TestStewardStart_NoRelayNote_IsMediumAgnostic pins Eric's stated reason for
// the no-relay note's wording (agent-teams-25c5.7): "don't mention telegram
// because we might change the delivery medium in the future." The delivery
// medium is Telegram today, but the string must not name it so the line
// stays true if that changes.
func TestStewardStart_NoRelayNote_IsMediumAgnostic(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	cmd := &stewardStartKong{
		agentsFunc:     func() ([]agentSession, error) { return nil, nil },
		launchFunc:     func(ctx *cli.Context, dir string) error { return nil },
		killFunc:       func(pid int) {},
		relaySpawnFunc: func(ctx *cli.Context) (int, error) { return 4242, nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(strings.ToLower(stdout.String()), "telegram") {
		t.Errorf("expected no-relay stdout to be medium-agnostic (no \"telegram\" in any casing), got: %q", stdout.String())
	}
}

// TestStewardStart_RelayFlag_OmitsNoRelayNote confirms --relay suppresses
// the no-relay note added by this bead: a relay was started, so the "needs
// a running relay" warning would be false.
func TestStewardStart_RelayFlag_OmitsNoRelayNote(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	cmd := &stewardStartKong{
		Relay:          true,
		agentsFunc:     func() ([]agentSession, error) { return nil, nil },
		launchFunc:     func(ctx *cli.Context, dir string) error { return nil },
		killFunc:       func(pid int) {},
		relaySpawnFunc: func(ctx *cli.Context) (int, error) { return 4242, nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(stdout.String(), "not started") {
		t.Errorf("expected no \"not started\" note in stdout with --relay, got: %q", stdout.String())
	}
}

// TestStewardStart_RelayFlag_SpawnsRelayAndWritesPidfile confirms --relay
// fully preserves today's behaviour: relaySpawnFunc is invoked exactly once
// and the "started (pid N)" note appears in stdout.
func TestStewardStart_RelayFlag_SpawnsRelayAndWritesPidfile(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	spawnCount := 0
	cmd := &stewardStartKong{
		Relay:      true,
		agentsFunc: func() ([]agentSession, error) { return nil, nil },
		launchFunc: func(ctx *cli.Context, dir string) error { return nil },
		killFunc:   func(pid int) {},
		relaySpawnFunc: func(ctx *cli.Context) (int, error) {
			spawnCount++
			return 4242, nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spawnCount != 1 {
		t.Errorf("expected relaySpawnFunc invoked exactly once with --relay, got %d", spawnCount)
	}
	if !strings.Contains(stdout.String(), "started (pid 4242)") {
		t.Errorf("expected relay start note in stdout, got: %q", stdout.String())
	}
}

// TestStewardStartKong_RelayFlag_BindsViaParser is a parser-level assertion
// that kong actually wires --relay to stewardStartKong.Relay. No existing
// in-package precedent parses through cli.Parser and inspects the resulting
// struct field directly (TestAllVerbsRegistered in verbs_test.go is an
// external-package smoke test that only checks a verb is recognized, not that
// a specific flag binds) — this test builds its own stewardCmd, registers it,
// and reads the field back after Parse.
func TestStewardStartKong_RelayFlag_BindsViaParser(t *testing.T) {
	p, err := cli.NewParser(kong.Exit(func(int) {}))
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	cmd := &stewardCmd{}
	p.AddVerb("steward", "Steward persona.", cmd)

	if _, err := p.Parse([]string{"steward", "start", "--relay"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cmd.Start.Relay {
		t.Error("expected --relay to bind stewardStartKong.Relay = true")
	}
}

// TestStewardStartKong_RelayFlag_DefaultsFalse confirms the flag defaults to
// false when omitted — the flipped default this bead exists to ship.
func TestStewardStartKong_RelayFlag_DefaultsFalse(t *testing.T) {
	p, err := cli.NewParser(kong.Exit(func(int) {}))
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	cmd := &stewardCmd{}
	p.AddVerb("steward", "Steward persona.", cmd)

	if _, err := p.Parse([]string{"steward", "start"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cmd.Start.Relay {
		t.Error("expected --relay to default to false")
	}
}

// ── stewardLaunchArgs: --settings ATEAM_ROLE=steward (agent-teams-142k.3) ───

// TestStewardLaunchArgs_ContainsSettingsFlag verifies that the sanctioned
// Steward launch argv carries "--settings" immediately followed by the exact
// stewardSettingsJSON string — the mechanism that publishes ATEAM_ROLE=steward
// per the role-signal contract (agent-teams-142k.1). No ATEAM_INITIATIVE (the
// steward is fleet-scoped) and no autoCompactWindow in the settings payload
// (it travels as a separate argv pair — see
// TestStewardLaunchArgs_AutoCompactWindow — not through stewardSettingsJSON).
func TestStewardLaunchArgs_ContainsSettingsFlag(t *testing.T) {
	args := stewardLaunchArgs("")

	found := false
	for i, a := range args {
		if a == "--settings" {
			if i+1 >= len(args) {
				t.Fatal("--settings has no following value in argv")
			}
			val := args[i+1]
			if val != stewardSettingsJSON {
				t.Errorf("value after --settings = %q, want %q", val, stewardSettingsJSON)
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("argv missing --settings; got: %v", args)
	}
	if stewardSettingsJSON != `{"env":{"ATEAM_ROLE":"steward"}}` {
		t.Errorf("stewardSettingsJSON = %q, want %q", stewardSettingsJSON, `{"env":{"ATEAM_ROLE":"steward"}}`)
	}
}

// TestStewardLaunchArgs_StandardArgsPresent verifies the remaining flags and
// prompt are unchanged by the --settings addition.
func TestStewardLaunchArgs_StandardArgsPresent(t *testing.T) {
	args := stewardLaunchArgs("")

	hasPair := func(flag, val string) bool {
		for i, a := range args {
			if a == flag && i+1 < len(args) && args[i+1] == val {
				return true
			}
		}
		return false
	}
	if !hasPair("--permission-mode", "bypassPermissions") {
		t.Errorf("argv missing \"--permission-mode\" \"bypassPermissions\" pair; got: %v", args)
	}
	found := false
	for _, a := range args {
		if a == "--bg" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("argv missing --bg; got: %v", args)
	}
	if last := args[len(args)-1]; last != "/agent-teams:steward" {
		t.Errorf("last argv element = %q, want %q", last, "/agent-teams:steward")
	}
}

// TestStewardLaunchArgs_OmitsAutocompactFlagWhenEmpty is the byte-identical
// guard (agent-teams-ufoy.4): with autoCompactWindow == "" (today's default),
// argv must not contain "--autocompact" at all, mirroring
// TestBGSessionArgs_OmitsAutocompactFlagWhenEmpty in dispatch_test.go for the
// steward's own argv producer.
func TestStewardLaunchArgs_OmitsAutocompactFlagWhenEmpty(t *testing.T) {
	args := stewardLaunchArgs("")
	for _, a := range args {
		if a == "--autocompact" {
			t.Fatalf("argv must omit --autocompact when the window is empty; got: %v", args)
		}
	}
	if last := args[len(args)-1]; last != "/agent-teams:steward" {
		t.Errorf("last argv element = %q, want %q", last, "/agent-teams:steward")
	}
}

// TestStewardLaunchArgs_AutocompactFlagWhenSet pins the pass-through-verbatim
// contract for the steward's argv producer, matching
// TestBGSessionArgs_AutocompactFlagWhenSet in dispatch_test.go: when
// autoCompactWindow is non-empty, stewardLaunchArgs appends "--autocompact"
// followed by the exact string given, exactly once, and the trailing
// "/agent-teams:steward" prompt stays last.
func TestStewardLaunchArgs_AutocompactFlagWhenSet(t *testing.T) {
	for _, window := range []string{"450000", "500k", "1m", "200", "auto"} {
		args := stewardLaunchArgs(window)

		count := 0
		for i, a := range args {
			if a != "--autocompact" {
				continue
			}
			count++
			if i+1 >= len(args) {
				t.Fatalf("--autocompact has no following value in argv: %v", args)
			}
			if args[i+1] != window {
				t.Errorf("--autocompact value = %q, want %q", args[i+1], window)
			}
		}
		if count != 1 {
			t.Errorf("window %q: --autocompact appeared %d times, want 1; args: %v", window, count, args)
		}
		if last := args[len(args)-1]; last != "/agent-teams:steward" {
			t.Errorf("window %q: last argv element = %q, want %q", window, last, "/agent-teams:steward")
		}
	}
}
