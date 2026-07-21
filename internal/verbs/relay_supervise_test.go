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

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── ensureRelayRunning ───────────────────────────────────────────────────────

func TestEnsureRelayRunning_NoPidfile_Spawns(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	var spawnCalled bool
	spawn := func(ctx *cli.Context) (int, error) {
		spawnCalled = true
		return 4242, nil
	}

	if err := ensureRelayRunning(ctx, spawn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !spawnCalled {
		t.Error("expected spawn to be invoked when no pidfile exists")
	}

	got, err := os.ReadFile(relayPidfilePath(ctx))
	if err != nil {
		t.Fatalf("expected pidfile written: %v", err)
	}
	if string(got) != "4242" {
		t.Errorf("pidfile content = %q, want %q", got, "4242")
	}
	if !strings.Contains(stdout.String(), "started (pid 4242)") {
		t.Errorf("expected started note in stdout, got: %q", stdout.String())
	}
}

func TestEnsureRelayRunning_LivePidfile_DoesNotSpawnDuplicate(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	pidfile := relayPidfilePath(ctx)
	if err := os.MkdirAll(filepath.Dir(pidfile), 0o755); err != nil {
		t.Fatalf("seed mailbox dir: %v", err)
	}
	// os.Getpid() is guaranteed alive for the duration of the test process.
	self := os.Getpid()
	if err := os.WriteFile(pidfile, []byte(strconv.Itoa(self)), 0o644); err != nil {
		t.Fatalf("seed live pidfile: %v", err)
	}

	var spawnCalled bool
	spawn := func(ctx *cli.Context) (int, error) {
		spawnCalled = true
		return 9999, nil
	}

	if err := ensureRelayRunning(ctx, spawn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spawnCalled {
		t.Error("expected spawn NOT to be invoked for a live relay pidfile")
	}
	got, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("expected pidfile to remain: %v", err)
	}
	if string(got) != strconv.Itoa(self) {
		t.Errorf("expected pidfile untouched, got: %q", got)
	}
	if !strings.Contains(stdout.String(), fmt.Sprintf("already running (pid %d)", self)) {
		t.Errorf("expected already-running note, got: %q", stdout.String())
	}
}

func TestEnsureRelayRunning_StalePidfile_ReapsAndSpawns(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	pidfile := relayPidfilePath(ctx)
	if err := os.MkdirAll(filepath.Dir(pidfile), 0o755); err != nil {
		t.Fatalf("seed mailbox dir: %v", err)
	}
	const deadPid = 9999999
	if err := os.WriteFile(pidfile, []byte(strconv.Itoa(deadPid)), 0o644); err != nil {
		t.Fatalf("seed stale pidfile: %v", err)
	}

	var spawnCalled bool
	spawn := func(ctx *cli.Context) (int, error) {
		spawnCalled = true
		return 5150, nil
	}

	if err := ensureRelayRunning(ctx, spawn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !spawnCalled {
		t.Error("expected spawn to be invoked after reaping a stale pidfile")
	}
	got, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("expected new pidfile written: %v", err)
	}
	if string(got) != "5150" {
		t.Errorf("pidfile content = %q, want %q", got, "5150")
	}
	if !strings.Contains(stdout.String(), "reaped stale pidfile") {
		t.Errorf("expected reaped note in stdout, got: %q", stdout.String())
	}
}

func TestEnsureRelayRunning_SpawnFailure_Propagates(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	spawn := func(ctx *cli.Context) (int, error) {
		return 0, fmt.Errorf("boom")
	}

	err := ensureRelayRunning(ctx, spawn)
	if err == nil {
		t.Fatal("expected spawn failure to propagate")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected propagated error to contain underlying cause, got: %v", err)
	}
	if _, err := os.Stat(relayPidfilePath(ctx)); !os.IsNotExist(err) {
		t.Errorf("expected no pidfile written on spawn failure, stat err: %v", err)
	}
}

// ── defaultRelaySpawn ────────────────────────────────────────────────────────

// TestDefaultRelaySpawn_PinsAgentTeamsHomeEnv is a regression guard for
// agent-teams-5y8a.14's second bug: defaultRelaySpawn must pin the child's
// AGENT_TEAMS_HOME to ctx.Home regardless of what the parent ateam process's
// own environment carries, so a relay spawned from a test/temp ctx.Home can
// never resolve the real workspace (and real bot token) instead. Exercises
// defaultRelaySpawn directly via a fake `ateam` on PATH that captures its
// received env instead of actually polling a transport.
func TestDefaultRelaySpawn_PinsAgentTeamsHomeEnv(t *testing.T) {
	scriptDir := t.TempDir()
	captureFile := filepath.Join(scriptDir, "captured-env")
	script := fmt.Sprintf("#!/bin/sh\necho \"$AGENT_TEAMS_HOME\" > %q\n", captureFile)
	if err := os.WriteFile(filepath.Join(scriptDir, "ateam"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ateam script: %v", err)
	}

	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })
	os.Setenv("PATH", scriptDir+string(os.PathListSeparator)+oldPath)

	// Simulate a parent environment with a DIFFERENT AGENT_TEAMS_HOME set —
	// the fix must override this, not inherit it.
	oldHome, hadHome := os.LookupEnv("AGENT_TEAMS_HOME")
	t.Cleanup(func() {
		if hadHome {
			os.Setenv("AGENT_TEAMS_HOME", oldHome)
		} else {
			os.Unsetenv("AGENT_TEAMS_HOME")
		}
	})
	os.Setenv("AGENT_TEAMS_HOME", "/should-not-be-used")

	ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())

	if _, err := defaultRelaySpawn(ctx); err != nil {
		t.Fatalf("defaultRelaySpawn: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var got []byte
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(captureFile); err == nil {
			got = b
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got == nil {
		t.Fatal("fake ateam never captured AGENT_TEAMS_HOME (spawn may have failed silently)")
	}
	if want := ctx.Home + "\n"; string(got) != want {
		t.Errorf("child AGENT_TEAMS_HOME = %q, want %q", got, want)
	}
}

// ── teardownRelay ─────────────────────────────────────────────────────────────

func TestTeardownRelay_NoPidfile_NoOp(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	var killCalled bool
	pid := teardownRelay(ctx, func(int) { killCalled = true })
	if pid != 0 {
		t.Errorf("expected pid 0 when no pidfile, got %d", pid)
	}
	if killCalled {
		t.Error("expected kill NOT to be called when no pidfile exists")
	}
}

func TestTeardownRelay_LivePid_KillsAndRemovesPidfile(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	sleeper := exec.Command("sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Fatalf("spawn sleeper: %v", err)
	}
	livePID := sleeper.Process.Pid
	t.Cleanup(func() {
		_ = sleeper.Process.Kill()
		_ = sleeper.Wait()
	})

	pidfile := relayPidfilePath(ctx)
	if err := os.MkdirAll(filepath.Dir(pidfile), 0o755); err != nil {
		t.Fatalf("seed mailbox dir: %v", err)
	}
	if err := os.WriteFile(pidfile, []byte(strconv.Itoa(livePID)), 0o644); err != nil {
		t.Fatalf("seed live pidfile: %v", err)
	}

	var killedPID int
	got := teardownRelay(ctx, func(pid int) { killedPID = pid })
	if got != livePID {
		t.Errorf("teardownRelay returned pid %d, want %d", got, livePID)
	}
	if killedPID != livePID {
		t.Errorf("expected kill called with pid %d, got %d", livePID, killedPID)
	}
	if _, err := os.Stat(pidfile); !os.IsNotExist(err) {
		t.Errorf("expected pidfile removed, stat err: %v", err)
	}
}

func TestTeardownRelay_DeadPid_RemovesPidfileWithoutKilling(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	pidfile := relayPidfilePath(ctx)
	if err := os.MkdirAll(filepath.Dir(pidfile), 0o755); err != nil {
		t.Fatalf("seed mailbox dir: %v", err)
	}
	const deadPid = 9999999
	if err := os.WriteFile(pidfile, []byte(strconv.Itoa(deadPid)), 0o644); err != nil {
		t.Fatalf("seed dead pidfile: %v", err)
	}

	var killCalled bool
	pid := teardownRelay(ctx, func(int) { killCalled = true })
	if pid != 0 {
		t.Errorf("expected pid 0 for a dead pidfile, got %d", pid)
	}
	if killCalled {
		t.Error("expected kill NOT to be called for a dead pidfile")
	}
	if _, err := os.Stat(pidfile); !os.IsNotExist(err) {
		t.Errorf("expected pidfile removed, stat err: %v", err)
	}
}

// ── integration through the kong verbs ──────────────────────────────────────

func TestStewardStart_EnsuresRelayRunning(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	var relaySpawned bool
	cmd := &stewardStartKong{
		agentsFunc: func() ([]agentSession, error) { return nil, nil },
		launchFunc: func(ctx *cli.Context, dir string) error { return nil },
		killFunc:   func(pid int) {},
		relaySpawnFunc: func(ctx *cli.Context) (int, error) {
			relaySpawned = true
			return 1234, nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !relaySpawned {
		t.Error("expected steward start to ensure the relay is running")
	}
	if !strings.Contains(stdout.String(), "started (pid 1234)") {
		t.Errorf("expected relay start note in stdout, got: %q", stdout.String())
	}
}

func TestStewardStart_RelaySpawnFailure_DoesNotFailCommand(t *testing.T) {
	home := t.TempDir()
	ctx, _, stderr := makeCtx(&fakeBD{}, home)

	var launchCalled bool
	cmd := &stewardStartKong{
		agentsFunc: func() ([]agentSession, error) { return nil, nil },
		launchFunc: func(ctx *cli.Context, dir string) error {
			launchCalled = true
			return nil
		},
		killFunc: func(pid int) {},
		relaySpawnFunc: func(ctx *cli.Context) (int, error) {
			return 0, fmt.Errorf("ateam not in PATH")
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("expected relay spawn failure to be fail-soft, got error: %v", err)
	}
	if !launchCalled {
		t.Error("expected steward launch to still be invoked")
	}
	if !strings.Contains(stderr.String(), "warning: relay") {
		t.Errorf("expected relay warning on stderr, got: %q", stderr.String())
	}
}

func TestStewardStart_SecondStart_DoesNotSpawnDuplicateRelay(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	spawnCount := 0
	newCmd := func() *stewardStartKong {
		return &stewardStartKong{
			agentsFunc: func() ([]agentSession, error) { return nil, nil },
			launchFunc: func(ctx *cli.Context, dir string) error { return nil },
			killFunc:   func(pid int) {},
			relaySpawnFunc: func(ctx *cli.Context) (int, error) {
				spawnCount++
				return os.Getpid(), nil // a pid guaranteed alive for the second call's liveness check
			},
		}
	}

	if err := newCmd().Run(ctx); err != nil {
		t.Fatalf("first start: unexpected error: %v", err)
	}
	if err := newCmd().Run(ctx); err != nil {
		t.Fatalf("second start: unexpected error: %v", err)
	}
	if spawnCount != 1 {
		t.Errorf("expected relay spawn exactly once across two steward starts, got %d", spawnCount)
	}
}

func TestStewardRemove_TearsDownRelay(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	sleeper := exec.Command("sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Fatalf("spawn sleeper: %v", err)
	}
	livePID := sleeper.Process.Pid
	t.Cleanup(func() {
		_ = sleeper.Process.Kill()
		_ = sleeper.Wait()
	})

	pidfile := relayPidfilePath(ctx)
	if err := os.MkdirAll(filepath.Dir(pidfile), 0o755); err != nil {
		t.Fatalf("seed mailbox dir: %v", err)
	}
	if err := os.WriteFile(pidfile, []byte(strconv.Itoa(livePID)), 0o644); err != nil {
		t.Fatalf("seed live relay pidfile: %v", err)
	}

	var killedPID int
	cmd := &stewardRemoveKong{
		killFunc: func(pid int) { killedPID = pid },
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if killedPID != livePID {
		t.Errorf("expected relay killed with pid %d, got %d", livePID, killedPID)
	}
	if _, err := os.Stat(pidfile); !os.IsNotExist(err) {
		t.Errorf("expected relay pidfile removed, stat err: %v", err)
	}
	if !strings.Contains(stdout.String(), fmt.Sprintf("stopped: relay (pid %d)", livePID)) {
		t.Errorf("expected stopped-relay note in stdout, got: %q", stdout.String())
	}
}

func TestStewardRemove_NoRelayRunning_NotesAndSucceeds(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	if err := (&stewardRemoveKong{}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "note: no relay running") {
		t.Errorf("expected no-relay-running note, got: %q", stdout.String())
	}
}
