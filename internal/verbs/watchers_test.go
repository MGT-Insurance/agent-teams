package verbs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// writePidfile creates <mailboxDir>/<id>.watcher.pid containing pid.
func writePidfile(t *testing.T, mailboxDir, id string, pid int) {
	t.Helper()
	if err := os.MkdirAll(mailboxDir, 0o755); err != nil {
		t.Fatalf("MkdirAll mailbox: %v", err)
	}
	path := filepath.Join(mailboxDir, id+".watcher.pid")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d", pid)), 0o644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
}

// makeWatchersCtx builds a Context pointing at home with captured stdout/stderr.
func makeWatchersCtx(fbd cli.BDRunner, home string) (*cli.Context, *strings.Builder) {
	var stdout strings.Builder
	return &cli.Context{
		Home:   home,
		BD:     fbd,
		Stdout: &stdout,
		Stderr: &strings.Builder{},
	}, &stdout
}

// ── watcherState unit tests ───────────────────────────────────────────────────

func TestWatcherState_MissingPidfile(t *testing.T) {
	dir := t.TempDir()
	state, pid := watcherState(filepath.Join(dir, "nofile.watcher.pid"))
	if state != "MISSING-WATCHER" {
		t.Errorf("state = %q, want MISSING-WATCHER", state)
	}
	if pid != 0 {
		t.Errorf("pid = %d, want 0", pid)
	}
}

func TestWatcherState_LivePid(t *testing.T) {
	dir := t.TempDir()
	selfPid := os.Getpid()
	writePidfile(t, dir, "at-live", selfPid)

	state, pid := watcherState(filepath.Join(dir, "at-live.watcher.pid"))
	if state != "OK" {
		t.Errorf("state = %q, want OK", state)
	}
	if pid != selfPid {
		t.Errorf("pid = %d, want %d", pid, selfPid)
	}
}

func TestWatcherState_DeadPid(t *testing.T) {
	dir := t.TempDir()
	// Use a very large pid that is almost certainly not alive.
	const deadPid = 9999999
	writePidfile(t, dir, "at-dead", deadPid)

	state, pid := watcherState(filepath.Join(dir, "at-dead.watcher.pid"))
	if state != "STALE-PIDFILE" {
		t.Errorf("state = %q, want STALE-PIDFILE", state)
	}
	if pid != deadPid {
		t.Errorf("pid = %d, want %d", pid, deadPid)
	}
}

// ── watchersCmd.Run integration tests ────────────────────────────────────────

// fakeIssues returns an openInitiativesFunc that provides the given issues.
func fakeIssues(issues []bd.Issue) openInitiativesFunc {
	return func() ([]bd.Issue, error) { return issues, nil }
}

// fakeAgents returns an agentsJSONFunc that provides the given sessions.
func fakeAgents(sessions []agentSession) agentsJSONFunc {
	return func() ([]agentSession, error) { return sessions, nil }
}

func TestWatchers_OK_Row(t *testing.T) {
	home := t.TempDir()
	mailboxDir := filepath.Join(home, "mailbox")
	selfPid := os.Getpid()
	wt := "/wt/at-ok"

	writePidfile(t, mailboxDir, "at-ok", selfPid)

	issues := []bd.Issue{{ID: "at-ok", Title: "My open initiative", Description: "worktree: " + wt + "\n"}}
	sessions := []agentSession{{CWD: wt, Kind: "background", Status: "busy"}}

	cmd := &watchersCmd{
		agentsFunc:      fakeAgents(sessions),
		initiativesFunc: fakeIssues(issues),
	}

	ctx, stdout := makeWatchersCtx(&fakeBD{}, home)
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "OK") {
		t.Errorf("expected OK in output; got:\n%s", out)
	}
	if !strings.Contains(out, "at-ok") {
		t.Errorf("expected initiative id in output; got:\n%s", out)
	}
	if !strings.Contains(out, "yes") {
		t.Errorf("expected live-session=yes in output; got:\n%s", out)
	}
}

func TestWatchers_MissingWatcher_Row(t *testing.T) {
	home := t.TempDir()
	wt := "/wt/at-missing"

	issues := []bd.Issue{{ID: "at-missing", Title: "No watcher", Description: "worktree: " + wt + "\n"}}

	cmd := &watchersCmd{
		agentsFunc:      fakeAgents(nil),
		initiativesFunc: fakeIssues(issues),
	}

	ctx, stdout := makeWatchersCtx(&fakeBD{}, home)
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "MISSING-WATCHER") {
		t.Errorf("expected MISSING-WATCHER in output; got:\n%s", out)
	}
}

func TestWatchers_StalePidfile_Row(t *testing.T) {
	home := t.TempDir()
	mailboxDir := filepath.Join(home, "mailbox")
	const deadPid = 9999999
	wt := "/wt/at-stale"

	writePidfile(t, mailboxDir, "at-stale", deadPid)

	issues := []bd.Issue{{ID: "at-stale", Title: "Stale watcher", Description: "worktree: " + wt + "\n"}}

	cmd := &watchersCmd{
		agentsFunc:      fakeAgents(nil),
		initiativesFunc: fakeIssues(issues),
	}

	ctx, stdout := makeWatchersCtx(&fakeBD{}, home)
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "STALE-PIDFILE") {
		t.Errorf("expected STALE-PIDFILE in output; got:\n%s", out)
	}
}

func TestWatchers_OrphanedPidfile_Row(t *testing.T) {
	home := t.TempDir()
	mailboxDir := filepath.Join(home, "mailbox")
	// Write a pidfile for an initiative that is NOT in the open set.
	writePidfile(t, mailboxDir, "at-orphan", os.Getpid())

	// Open initiatives do NOT include "at-orphan".
	issues := []bd.Issue{{ID: "at-other", Title: "Other initiative", Description: "worktree: /wt/other\n"}}

	cmd := &watchersCmd{
		agentsFunc:      fakeAgents(nil),
		initiativesFunc: fakeIssues(issues),
	}

	ctx, stdout := makeWatchersCtx(&fakeBD{}, home)
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "at-orphan") {
		t.Errorf("expected orphan id in output; got:\n%s", out)
	}
	// Orphan pidfile with an alive pid is a live watcher for a non-open
	// initiative -> ORPHAN-RUNNING (never OK, never plain STALE-PIDFILE).
	if !strings.Contains(out, "ORPHAN-RUNNING") {
		t.Errorf("expected ORPHAN-RUNNING state in alive-orphan row; got:\n%s", out)
	}
	if !strings.Contains(out, "<orphan>") {
		t.Errorf("expected <orphan> title in output; got:\n%s", out)
	}
}

func TestWatchers_NilContext(t *testing.T) {
	cmd := &watchersCmd{agentsFunc: defaultAgentsJSON}
	err := cmd.Run(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil context, got nil")
	}
}

func TestWatchers_AgentsError_SessionUnknown(t *testing.T) {
	home := t.TempDir()
	wt := "/wt/at-agents-err"

	issues := []bd.Issue{{ID: "at-agents-err", Title: "Agents error", Description: "worktree: " + wt + "\n"}}

	cmd := &watchersCmd{
		agentsFunc:      func() ([]agentSession, error) { return nil, fmt.Errorf("claude not found") },
		initiativesFunc: fakeIssues(issues),
	}

	ctx, stdout := makeWatchersCtx(&fakeBD{}, home)
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "unknown") {
		t.Errorf("expected 'unknown' for live-session when agents fail; got:\n%s", out)
	}
}

// ── edge-case tests (tester-authored) ─────────────────────────────────────────

// TestWatcherState_GarbagePidfile: pidfile contains non-numeric content → STALE-PIDFILE, pid=0.
func TestWatcherState_GarbagePidfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "at-garbage.watcher.pid")
	if err := os.WriteFile(path, []byte("not-a-number\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	state, pid := watcherState(path)
	if state != "STALE-PIDFILE" {
		t.Errorf("state = %q, want STALE-PIDFILE", state)
	}
	if pid != 0 {
		t.Errorf("pid = %d, want 0 for garbage content", pid)
	}
}

// TestWatcherState_NegativePid: pidfile contains a negative pid → STALE-PIDFILE, pid=0.
func TestWatcherState_NegativePid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "at-neg.watcher.pid")
	if err := os.WriteFile(path, []byte("-42"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	state, pid := watcherState(path)
	if state != "STALE-PIDFILE" {
		t.Errorf("state = %q, want STALE-PIDFILE for negative pid", state)
	}
	if pid != 0 {
		t.Errorf("pid = %d, want 0 for negative pid", pid)
	}
}

// TestWatcherState_ZeroPid: pidfile contains "0" → STALE-PIDFILE, pid=0.
func TestWatcherState_ZeroPid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "at-zero.watcher.pid")
	if err := os.WriteFile(path, []byte("0"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	state, pid := watcherState(path)
	if state != "STALE-PIDFILE" {
		t.Errorf("state = %q, want STALE-PIDFILE for zero pid", state)
	}
	if pid != 0 {
		t.Errorf("pid = %d, want 0 for zero pid", pid)
	}
}

// TestWatchers_OrphanAlive_ReportedAsRunning: an orphaned pidfile whose pid IS
// still alive (simulated via os.Getpid()) is a live watcher for a non-open
// initiative. It must read ORPHAN-RUNNING — never OK (it is an anomaly) and
// never STALE-PIDFILE (that label is reserved for a dead/leftover pidfile).
func TestWatchers_OrphanAlive_ReportedAsRunning(t *testing.T) {
	home := t.TempDir()
	mailboxDir := filepath.Join(home, "mailbox")
	selfPid := os.Getpid()

	// Write a pidfile for a non-open initiative with the current (alive) process pid.
	writePidfile(t, mailboxDir, "at-orphan-alive", selfPid)

	// Open initiatives do NOT include "at-orphan-alive".
	issues := []bd.Issue{{ID: "at-other", Title: "Other", Description: "worktree: /wt/other\n"}}

	cmd := &watchersCmd{
		agentsFunc:      fakeAgents(nil),
		initiativesFunc: fakeIssues(issues),
	}

	ctx, stdout := makeWatchersCtx(&fakeBD{}, home)
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "at-orphan-alive") {
		t.Errorf("expected orphan id in output; got:\n%s", out)
	}
	if !strings.Contains(out, "ORPHAN-RUNNING") {
		t.Errorf("expected ORPHAN-RUNNING state for orphan with alive pid; got:\n%s", out)
	}
	// The live pid must appear in the output row.
	if !strings.Contains(out, fmt.Sprintf("%d", selfPid)) {
		t.Errorf("expected live pid %d in orphan row; got:\n%s", selfPid, out)
	}
}

// TestWatchers_EmptyOpenSet_WithOrphans: no open initiatives, but orphan pidfiles
// are present → verb succeeds, header is printed, STALE rows appear for orphans.
func TestWatchers_EmptyOpenSet_WithOrphans(t *testing.T) {
	home := t.TempDir()
	mailboxDir := filepath.Join(home, "mailbox")

	writePidfile(t, mailboxDir, "at-closed-1", 9999998)
	writePidfile(t, mailboxDir, "at-closed-2", 9999997)

	cmd := &watchersCmd{
		agentsFunc:      fakeAgents(nil),
		initiativesFunc: fakeIssues(nil), // empty open set
	}

	ctx, stdout := makeWatchersCtx(&fakeBD{}, home)
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "at-closed-1") {
		t.Errorf("expected at-closed-1 orphan row; got:\n%s", out)
	}
	if !strings.Contains(out, "at-closed-2") {
		t.Errorf("expected at-closed-2 orphan row; got:\n%s", out)
	}
	if !strings.Contains(out, "STALE") {
		t.Errorf("expected STALE state for orphan rows; got:\n%s", out)
	}
}

// TestWatchers_InitiativesError: when the injected initiativesFunc returns an error,
// Run propagates it and does not panic or produce partial output.
func TestWatchers_InitiativesError(t *testing.T) {
	home := t.TempDir()

	cmd := &watchersCmd{
		agentsFunc:      fakeAgents(nil),
		initiativesFunc: func() ([]bd.Issue, error) { return nil, fmt.Errorf("bd list failed") },
	}

	ctx, _ := makeWatchersCtx(&fakeBD{}, home)
	err := cmd.Run(ctx, nil)
	if err == nil {
		t.Fatal("expected error when initiativesFunc fails, got nil")
	}
	if !strings.Contains(err.Error(), "bd list failed") {
		t.Errorf("error = %q; want to contain 'bd list failed'", err.Error())
	}
}
