package verbs

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── classifyInitiative (unit-level) ──────────────────────────────────────────

func TestClassifyInitiative(t *testing.T) {
	const wt = "/fake/wt"
	dirExists := func(string) bool { return true }
	dirMissing := func(string) bool { return false }
	pid := 42

	tests := []struct {
		name      string
		labels    []string
		sessions  []agentSession
		dirExists dirExistsFunc
		wantClass string
		wantCwd   bool
	}{
		{
			name:      "no session at all => DEAD",
			labels:    nil,
			sessions:  nil,
			dirExists: dirExists,
			wantClass: hungClassDead,
			wantCwd:   true,
		},
		{
			name:      "cwd missing overrides everything => DEAD",
			labels:    nil,
			sessions:  []agentSession{{CWD: wt, Status: "busy", PID: &pid}},
			dirExists: dirMissing,
			wantClass: hungClassDead,
			wantCwd:   false,
		},
		{
			name:      "matched session but pid nil => DEAD",
			labels:    nil,
			sessions:  []agentSession{{CWD: wt, Status: "idle"}}, // PID nil
			dirExists: dirExists,
			wantClass: hungClassDead,
			wantCwd:   true,
		},
		{
			name:      "busy + pid present => WORKING",
			labels:    nil,
			sessions:  []agentSession{{CWD: wt, Status: "busy", PID: &pid}},
			dirExists: dirExists,
			wantClass: hungClassWorking,
			wantCwd:   true,
		},
		{
			name:      "state=working overrides status => WORKING",
			labels:    nil,
			sessions:  []agentSession{{CWD: wt, Status: "idle", State: "working", PID: &pid}},
			dirExists: dirExists,
			wantClass: hungClassWorking,
			wantCwd:   true,
		},
		{
			name:      "idle + human + gate:question => AWAITING-HUMAN",
			labels:    []string{"human", "gate:question"},
			sessions:  []agentSession{{CWD: wt, Status: "idle", PID: &pid}},
			dirExists: dirExists,
			wantClass: hungClassAwaitingHuman,
			wantCwd:   true,
		},
		{
			name:      "waiting + human + gate:review => AWAITING-HUMAN",
			labels:    []string{"human", "gate:review"},
			sessions:  []agentSession{{CWD: wt, Status: "waiting", PID: &pid}},
			dirExists: dirExists,
			wantClass: hungClassAwaitingHuman,
			wantCwd:   true,
		},
		{
			name:      "idle, no gate => STUCK",
			labels:    nil,
			sessions:  []agentSession{{CWD: wt, Status: "idle", PID: &pid}},
			dirExists: dirExists,
			wantClass: hungClassStuck,
			wantCwd:   true,
		},
		{
			name:      "idle, human label but no gate label => STUCK",
			labels:    []string{"human"},
			sessions:  []agentSession{{CWD: wt, Status: "idle", PID: &pid}},
			dirExists: dirExists,
			wantClass: hungClassStuck,
			wantCwd:   true,
		},
		{
			// agent-teams-6rru.13: DEAD used to short-circuit before the
			// gate check ran whenever pid was absent -- a genuinely gated
			// initiative whose session died must classify AWAITING-HUMAN,
			// not DEAD.
			name:      "pid absent (tracked-but-dead) + human + gate:question => AWAITING-HUMAN, not DEAD",
			labels:    []string{"human", "gate:question"},
			sessions:  []agentSession{{CWD: wt, Status: ""}}, // PID nil
			dirExists: dirExists,
			wantClass: hungClassAwaitingHuman,
			wantCwd:   true,
		},
		{
			name:      "no session matched at all + human + gate:review => AWAITING-HUMAN, not DEAD",
			labels:    []string{"human", "gate:review"},
			sessions:  nil, // matched is nil -> pid trivially absent
			dirExists: dirExists,
			wantClass: hungClassAwaitingHuman,
			wantCwd:   true,
		},
		{
			// cwd-missing must still preempt the gate check -- DEAD wins
			// regardless of labels once the worktree itself is gone.
			name:      "cwd missing overrides gate labels too => DEAD",
			labels:    []string{"human", "gate:review"},
			sessions:  []agentSession{{CWD: wt, Status: "idle", PID: &pid}},
			dirExists: dirMissing,
			wantClass: hungClassDead,
			wantCwd:   false,
		},
		{
			// agent-teams-6rru.15 repro at the classifyInitiative level: a
			// live session whose cwd wandered into a sibling track
			// worktree is matched by Name and classifies WORKING, not
			// DEAD.
			name:      "wandered live session (Name matches, cwd is a sibling track worktree) => WORKING, not DEAD",
			labels:    nil,
			sessions:  []agentSession{{CWD: wt + "-track-h", Name: filepath.Base(wt), Status: "busy", PID: &pid}},
			dirExists: dirExists,
			wantClass: hungClassWorking,
			wantCwd:   true,
		},
		{
			// Full at-wisp-e50 repro: a dead duplicate session sits at the
			// registered worktree's cwd, while the live session has
			// wandered into a track worktree -- same Name on both. The
			// live one must win, classifying WORKING.
			name:   "dead duplicate at registered cwd + live wandered session (same Name) => WORKING",
			labels: nil,
			sessions: []agentSession{
				{CWD: wt, Name: filepath.Base(wt)},                                         // dead duplicate, no pid
				{CWD: wt + "-track-h", Name: filepath.Base(wt), Status: "busy", PID: &pid}, // live, wandered
			},
			dirExists: dirExists,
			wantClass: hungClassWorking,
			wantCwd:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotClass, _, gotCwd := classifyInitiative(tc.labels, tc.sessions, wt, tc.dirExists)
			if gotClass != tc.wantClass {
				t.Errorf("classification = %q, want %q", gotClass, tc.wantClass)
			}
			if gotCwd != tc.wantCwd {
				t.Errorf("cwdPresent = %v, want %v", gotCwd, tc.wantCwd)
			}
		})
	}
}

// ── scanHung (integration-level) ─────────────────────────────────────────────

// makeHungCtx builds a minimal *cli.Context with a temp workspace Home (so
// the durable anchor file lands in a scratch dir) and a fake bd client that
// always returns issues regardless of the list args passed.
func makeHungCtx(t *testing.T, issues []bd.Issue) *cli.Context {
	t.Helper()
	home := t.TempDir()
	return &cli.Context{
		Home:   home,
		BD:     bd.NewClientWithExec(home, fakeListExec(issues)),
		Stdout: &strings.Builder{},
		Stderr: &strings.Builder{},
	}
}

func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestScanHung_NoAgents_GracefulDegrade(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{ID: "at-1", Title: "one", Description: "worktree: " + wt, Status: "open"}}
	ctx := makeHungCtx(t, issues)

	agentsErr := fmt.Errorf("claude not in PATH")
	out, err := scanHung(ctx, func() ([]agentSession, error) { return nil, agentsErr }, fixedNow(time.Now()), true)
	if err != nil {
		t.Fatalf("scanHung returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(out))
	}
	if out[0].Classification != hungClassUnknown {
		t.Errorf("classification = %q, want %q", out[0].Classification, hungClassUnknown)
	}
	if out[0].Hung {
		t.Error("hung should be false on graceful degrade")
	}
}

func TestScanHung_Working_NotHung(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{ID: "at-1", Title: "one", Description: "worktree: " + wt, Status: "open"}}
	ctx := makeHungCtx(t, issues)

	pid := 1
	sessions := []agentSession{{CWD: wt, Status: "busy", PID: &pid}}
	out, err := scanHung(ctx, func() ([]agentSession, error) { return sessions, nil }, fixedNow(time.Now()), true)
	if err != nil {
		t.Fatalf("scanHung returned error: %v", err)
	}
	if out[0].Classification != hungClassWorking {
		t.Fatalf("classification = %q, want WORKING", out[0].Classification)
	}
	if out[0].Hung {
		t.Error("WORKING must never be hung")
	}
	if out[0].StuckSince != "" {
		t.Errorf("stuck_since = %q, want empty for WORKING", out[0].StuckSince)
	}

	anchors := loadHungState(hungStatePath(ctx))
	if _, ok := anchors["at-1"]; ok {
		t.Error("expected no anchor persisted for a WORKING initiative")
	}
}

func TestScanHung_AwaitingHuman_NotHung(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{
		ID: "at-1", Title: "one", Description: "worktree: " + wt,
		Labels: []string{"human", "gate:question"}, Status: "open",
	}}
	ctx := makeHungCtx(t, issues)

	pid := 1
	sessions := []agentSession{{CWD: wt, Status: "idle", PID: &pid}}
	out, err := scanHung(ctx, func() ([]agentSession, error) { return sessions, nil }, fixedNow(time.Now()), true)
	if err != nil {
		t.Fatalf("scanHung returned error: %v", err)
	}
	if out[0].Classification != hungClassAwaitingHuman {
		t.Fatalf("classification = %q, want AWAITING-HUMAN", out[0].Classification)
	}
	if out[0].Hung {
		t.Error("AWAITING-HUMAN must never be hung")
	}

	anchors := loadHungState(hungStatePath(ctx))
	if _, ok := anchors["at-1"]; ok {
		t.Error("expected no anchor persisted for an AWAITING-HUMAN initiative")
	}
}

// TestScanHung_AwaitingHuman_PidAbsent is agent-teams-6rru.13's gap: a
// genuinely gated initiative whose session has died (PID absent, not just
// idle) must still classify AWAITING-HUMAN, not DEAD.
func TestScanHung_AwaitingHuman_PidAbsent(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{
		ID: "at-1", Title: "one", Description: "worktree: " + wt,
		Labels: []string{"human", "gate:question"}, Status: "open",
	}}
	ctx := makeHungCtx(t, issues)

	sessions := []agentSession{{CWD: wt, Status: ""}} // PID nil: tracked-but-dead
	out, err := scanHung(ctx, func() ([]agentSession, error) { return sessions, nil }, fixedNow(time.Now()), true)
	if err != nil {
		t.Fatalf("scanHung returned error: %v", err)
	}
	if out[0].Classification != hungClassAwaitingHuman {
		t.Fatalf("classification = %q, want AWAITING-HUMAN", out[0].Classification)
	}
	if out[0].PIDPresent {
		t.Error("pid_present should be false")
	}
	if out[0].Hung {
		t.Error("AWAITING-HUMAN must never be hung")
	}
}

// TestScanHung_WanderedLiveSession_WorksNotDead is the agent-teams-6rru.15
// repro at the scanHung integration level: a live session whose cwd wandered
// into a sibling track worktree (not the registered worktree) is matched via
// Name and classifies WORKING, not DEAD.
func TestScanHung_WanderedLiveSession_WorksNotDead(t *testing.T) {
	registeredWt := t.TempDir()
	issues := []bd.Issue{{ID: "at-1", Title: "one", Description: "worktree: " + registeredWt, Status: "open"}}
	ctx := makeHungCtx(t, issues)

	pid := 18349
	trackWt := t.TempDir() // stands in for a sibling track worktree
	sessions := []agentSession{
		{CWD: trackWt, Name: filepath.Base(registeredWt), Status: "busy", PID: &pid},
	}
	out, err := scanHung(ctx, func() ([]agentSession, error) { return sessions, nil }, fixedNow(time.Now()), true)
	if err != nil {
		t.Fatalf("scanHung returned error: %v", err)
	}
	if out[0].Classification != hungClassWorking {
		t.Fatalf("classification = %q, want WORKING (session cwd wandered but Name matches)", out[0].Classification)
	}
	if !out[0].PIDPresent {
		t.Error("expected pid_present=true for the wandered live session")
	}
}

func TestScanHung_Dead_PidNilAndCwdMissing(t *testing.T) {
	// Case 1: matched session, but pid nil.
	wt := t.TempDir()
	issues := []bd.Issue{{ID: "at-1", Title: "one", Description: "worktree: " + wt, Status: "open"}}
	ctx := makeHungCtx(t, issues)
	sessions := []agentSession{{CWD: wt, Status: "idle"}} // PID nil
	out, err := scanHung(ctx, func() ([]agentSession, error) { return sessions, nil }, fixedNow(time.Now()), true)
	if err != nil {
		t.Fatalf("scanHung returned error: %v", err)
	}
	if out[0].Classification != hungClassDead {
		t.Fatalf("classification = %q, want DEAD (pid nil)", out[0].Classification)
	}
	if out[0].Hung {
		t.Error("DEAD must never be hung")
	}

	// Case 2: worktree directory does not exist on disk at all.
	missingWt := filepath.Join(t.TempDir(), "does-not-exist")
	issues2 := []bd.Issue{{ID: "at-2", Title: "two", Description: "worktree: " + missingWt, Status: "open"}}
	ctx2 := makeHungCtx(t, issues2)
	pid := 1
	sessions2 := []agentSession{{CWD: missingWt, Status: "idle", PID: &pid}}
	out2, err := scanHung(ctx2, func() ([]agentSession, error) { return sessions2, nil }, fixedNow(time.Now()), true)
	if err != nil {
		t.Fatalf("scanHung returned error: %v", err)
	}
	if out2[0].Classification != hungClassDead {
		t.Fatalf("classification = %q, want DEAD (cwd missing)", out2[0].Classification)
	}
	if out2[0].CWDPresent {
		t.Error("cwd_present should be false for a missing worktree directory")
	}
}

// TestScanHung_StuckAnchorLifecycle drives the durable stuck-since anchor
// through its full lifecycle: set on first STUCK observation, elapsed grows
// (and hung flips true) as an injected clock advances, then cleared the
// instant the session stops being STUCK (busy in this test).
func TestScanHung_StuckAnchorLifecycle(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{ID: "at-1", Title: "one", Description: "worktree: " + wt, Status: "open"}}
	ctx := makeHungCtx(t, issues)

	pid := 1
	idleSessions := []agentSession{{CWD: wt, Status: "idle", PID: &pid}}
	agentsFunc := func() ([]agentSession, error) { return idleSessions, nil }

	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)

	// First scan: STUCK, anchor set, not yet hung.
	out, err := scanHung(ctx, agentsFunc, fixedNow(t0), true)
	if err != nil {
		t.Fatalf("scanHung returned error: %v", err)
	}
	if out[0].Classification != hungClassStuck {
		t.Fatalf("classification = %q, want STUCK", out[0].Classification)
	}
	if out[0].Hung {
		t.Error("should not be hung on first STUCK observation")
	}
	if out[0].StuckSince == "" {
		t.Fatal("expected stuck_since to be set")
	}
	firstStuckSince := out[0].StuckSince

	anchors := loadHungState(hungStatePath(ctx))
	anchor, ok := anchors["at-1"]
	if !ok {
		t.Fatal("expected anchor persisted for STUCK initiative")
	}
	if anchor.StuckSince != firstStuckSince {
		t.Errorf("persisted anchor stuck_since = %q, want %q", anchor.StuckSince, firstStuckSince)
	}

	// Second scan, 5 minutes later: still STUCK, anchor NOT reset, not yet hung.
	t1 := t0.Add(5 * time.Minute)
	out, err = scanHung(ctx, agentsFunc, fixedNow(t1), true)
	if err != nil {
		t.Fatalf("scanHung returned error: %v", err)
	}
	if out[0].StuckSince != firstStuckSince {
		t.Errorf("stuck_since changed across scans: got %q, want unchanged %q", out[0].StuckSince, firstStuckSince)
	}
	if out[0].StuckElapsedSeconds != 300 {
		t.Errorf("stuck_elapsed_seconds = %d, want 300", out[0].StuckElapsedSeconds)
	}
	if out[0].Hung {
		t.Error("should not be hung yet at 5 minutes (threshold is 15m)")
	}

	// Third scan, 20 minutes past t0 (past the 15m threshold): HUNG, anchor still unchanged.
	t2 := t0.Add(20 * time.Minute)
	out, err = scanHung(ctx, agentsFunc, fixedNow(t2), true)
	if err != nil {
		t.Fatalf("scanHung returned error: %v", err)
	}
	if !out[0].Hung {
		t.Error("expected hung=true after exceeding the stuck threshold")
	}
	if out[0].StuckSince != firstStuckSince {
		t.Errorf("stuck_since changed across scans: got %q, want unchanged %q", out[0].StuckSince, firstStuckSince)
	}

	// Fourth scan: session goes busy (WORKING). Anchor must be cleared.
	busySessions := []agentSession{{CWD: wt, Status: "busy", PID: &pid}}
	out, err = scanHung(ctx, func() ([]agentSession, error) { return busySessions, nil }, fixedNow(t2.Add(time.Minute)), true)
	if err != nil {
		t.Fatalf("scanHung returned error: %v", err)
	}
	if out[0].Classification != hungClassWorking {
		t.Fatalf("classification = %q, want WORKING", out[0].Classification)
	}
	anchors = loadHungState(hungStatePath(ctx))
	if _, ok := anchors["at-1"]; ok {
		t.Error("expected anchor to be cleared once the initiative stops being STUCK")
	}

	// Fifth scan: back to idle/STUCK. A NEW stuck-since must be recorded
	// (proving the anchor was genuinely cleared, not just left stale).
	t3 := t2.Add(2 * time.Minute)
	out, err = scanHung(ctx, agentsFunc, fixedNow(t3), true)
	if err != nil {
		t.Fatalf("scanHung returned error: %v", err)
	}
	if out[0].Classification != hungClassStuck {
		t.Fatalf("classification = %q, want STUCK", out[0].Classification)
	}
	if out[0].StuckSince == firstStuckSince {
		t.Error("expected a fresh stuck_since after the anchor was cleared by the WORKING scan")
	}
	if out[0].Hung {
		t.Error("freshly re-STUCK initiative should not be hung immediately")
	}
}

// ── saveHungState atomicity ──────────────────────────────────────────────────

// TestSaveHungState_AtomicRoundTripNoTempLeft verifies the temp-file+rename
// write (agent-teams-6rru.17): the state round-trips through loadHungState
// with every field intact, and no hung-state-*.json temp file is left behind
// in the directory after a successful save.
func TestSaveHungState_AtomicRoundTripNoTempLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, hungStateFileName)

	want := map[string]hungAnchor{
		"at-1": {
			StuckSince:   "2026-07-21T10:00:00Z",
			AlertedAt:    "2026-07-21T10:10:00Z",
			WakeAttempts: 2,
			LastWakeAt:   "2026-07-21T10:05:00Z",
		},
		"at-2": {StuckSince: "2026-07-21T09:00:00Z"},
	}
	if err := saveHungState(path, want); err != nil {
		t.Fatalf("saveHungState: %v", err)
	}

	got := loadHungState(path)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read state dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != hungStateFileName {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("state dir = %v, want exactly [%s] (temp files must be renamed away, not left behind)", names, hungStateFileName)
	}
}

// ── hungScanKong.Run ──────────────────────────────────────────────────────────

func TestHungScanKong_Run_NilCtx(t *testing.T) {
	cmd := &hungScanKong{
		agentsFunc: func() ([]agentSession, error) { return nil, nil },
		now:        time.Now,
	}
	if err := cmd.Run(nil); err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestHungScanKong_Run_EmitsJSON(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{ID: "at-1", Title: "one", Description: "worktree: " + wt, Status: "open"}}
	ctx := makeHungCtx(t, issues)

	pid := 1
	sessions := []agentSession{{CWD: wt, Status: "busy", PID: &pid}}
	cmd := &hungScanKong{
		agentsFunc: func() ([]agentSession, error) { return sessions, nil },
		now:        time.Now,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got := ctx.Stdout.(*strings.Builder).String()
	if !strings.Contains(got, `"classification":"WORKING"`) {
		t.Errorf("output missing expected classification: %s", got)
	}
	if !strings.Contains(got, `"id":"at-1"`) {
		t.Errorf("output missing expected id: %s", got)
	}
}

// TestHungScanKong_Run_DoesNotPersist is the core agent-teams-6rru.19
// regression test: the CLI hung-scan path (hungScanKong.Run -> scanHung with
// persist=false) must never write hung-state.json, even when it classifies an
// initiative STUCK (the case that, before this bead, drove a saveHungState
// call on every invocation). With no pre-existing state file, Run must leave
// none behind.
func TestHungScanKong_Run_DoesNotPersist(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{ID: "at-1", Title: "one", Description: "worktree: " + wt, Status: "open"}}
	ctx := makeHungCtx(t, issues)

	pid := 1
	sessions := []agentSession{{CWD: wt, Status: "idle", PID: &pid}} // STUCK: idle, no gate
	cmd := &hungScanKong{
		agentsFunc: func() ([]agentSession, error) { return sessions, nil },
		now:        time.Now,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got := ctx.Stdout.(*strings.Builder).String()
	if !strings.Contains(got, `"classification":"STUCK"`) {
		t.Fatalf("expected STUCK classification (the write-triggering case pre-.19), got: %s", got)
	}

	statePath := hungStatePath(ctx)
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("expected no hung-state.json to be created by the CLI path, stat err = %v", err)
	}
	if anchors := loadHungState(statePath); len(anchors) != 0 {
		t.Errorf("expected loadHungState to return empty after a read-only CLI scan, got %+v", anchors)
	}
}

// TestScanHung_PersistFalse_LeavesExistingAnchorFileUnchanged is the
// companion core-path test: when a hung-state.json already exists,
// scanHung(..., persist=false) must leave it byte-for-byte unchanged, even
// though it still classifies (and would, if persist=true, re-anchor) a STUCK
// initiative on this scan.
func TestScanHung_PersistFalse_LeavesExistingAnchorFileUnchanged(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{ID: "at-1", Title: "one", Description: "worktree: " + wt, Status: "open"}}
	ctx := makeHungCtx(t, issues)

	statePath := hungStatePath(ctx)
	seeded := map[string]hungAnchor{
		"at-1": {StuckSince: "2026-07-21T09:00:00Z"},
	}
	if err := saveHungState(statePath, seeded); err != nil {
		t.Fatalf("seed anchor state: %v", err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read seeded state file: %v", err)
	}

	// A different STUCK session at a later time than the seeded StuckSince --
	// if persist were true this would leave StuckSince unchanged (already
	// anchored) but still trigger a write of newAnchors. With persist=false
	// no write may occur at all.
	pid := 1
	sessions := []agentSession{{CWD: wt, Status: "idle", PID: &pid}}
	out, err := scanHung(ctx, func() ([]agentSession, error) { return sessions, nil }, fixedNow(time.Now()), false)
	if err != nil {
		t.Fatalf("scanHung returned error: %v", err)
	}
	if out[0].Classification != hungClassStuck {
		t.Fatalf("classification = %q, want STUCK", out[0].Classification)
	}
	if out[0].StuckSince != seeded["at-1"].StuckSince {
		t.Errorf("read-only scan should still report the existing anchor's stuck_since: got %q, want %q", out[0].StuckSince, seeded["at-1"].StuckSince)
	}

	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state file after read-only scan: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("hung-state.json changed after a persist=false scan:\n before: %s\n after:  %s", before, after)
	}
}
