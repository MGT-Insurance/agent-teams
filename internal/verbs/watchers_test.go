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
	if !strings.Contains(out, "STALE") {
		t.Errorf("expected STALE state in orphan row; got:\n%s", out)
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
