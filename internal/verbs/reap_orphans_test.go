package verbs

import (
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── test helpers ──────────────────────────────────────────────────────────────

// makeReapCtx builds a minimal *cli.Context with captured stdout/stderr.
func makeReapCtx() (*cli.Context, *strings.Builder, *strings.Builder) {
	var stdout, stderr strings.Builder
	return &cli.Context{
		Stdout: &stdout,
		Stderr: &stderr,
	}, &stdout, &stderr
}

// reapFakeAgents returns a fixed agentsJSONFunc that yields the given sessions.
func reapFakeAgents(sessions []agentSession) agentsJSONFunc {
	return func() ([]agentSession, error) {
		return sessions, nil
	}
}

// fakeStops records which ids were passed to claude stop.
type fakeStops struct {
	stopped []string
}

func (f *fakeStops) stopFunc() stopSessionFunc {
	return func(id string) error {
		f.stopped = append(f.stopped, id)
		return nil
	}
}

// ── selection logic tests ─────────────────────────────────────────────────────

// TestReapOrphans_BackgroundMissingCwd verifies that a background session with
// a missing cwd is reaped.
func TestReapOrphans_BackgroundMissingCwd(t *testing.T) {
	sessions := []agentSession{
		{ID: "abc123", Kind: "background", CWD: "/nonexistent/path"},
	}
	dirExistsNever := func(string) bool { return false }
	var stops fakeStops

	verb := &reapOrphansKong{
		agentsFunc:  reapFakeAgents(sessions),
		dirExists:   dirExistsNever,
		stopSession: stops.stopFunc(),
	}

	ctx, stdout, _ := makeReapCtx()
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stops.stopped) != 1 || stops.stopped[0] != "abc123" {
		t.Errorf("expected abc123 to be stopped; got %v", stops.stopped)
	}
	if !strings.Contains(stdout.String(), "reaped 1 background orphan session(s)") {
		t.Errorf("expected reap count in output; got: %s", stdout.String())
	}
}

// TestReapOrphans_BackgroundExistingCwd verifies that a background session
// with an existing cwd is NOT reaped.
func TestReapOrphans_BackgroundExistingCwd(t *testing.T) {
	sessions := []agentSession{
		{ID: "abc123", Kind: "background", CWD: "/existing/path"},
	}
	dirExistsAlways := func(string) bool { return true }
	var stops fakeStops

	verb := &reapOrphansKong{
		agentsFunc:  reapFakeAgents(sessions),
		dirExists:   dirExistsAlways,
		stopSession: stops.stopFunc(),
	}

	ctx, stdout, _ := makeReapCtx()
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stops.stopped) != 0 {
		t.Errorf("expected no stops; got %v", stops.stopped)
	}
	if !strings.Contains(stdout.String(), "no orphaned background sessions found") {
		t.Errorf("expected no-orphan message; got: %s", stdout.String())
	}
}

// TestReapOrphans_InteractiveMissingCwd verifies that an interactive session
// with a missing cwd is NOT stopped, only an advisory is printed.
func TestReapOrphans_InteractiveMissingCwd(t *testing.T) {
	sessions := []agentSession{
		{SessionID: "sess-xyz", Kind: "interactive", CWD: "/gone/path"},
	}
	dirExistsNever := func(string) bool { return false }
	var stops fakeStops

	verb := &reapOrphansKong{
		agentsFunc:  reapFakeAgents(sessions),
		dirExists:   dirExistsNever,
		stopSession: stops.stopFunc(),
	}

	ctx, stdout, _ := makeReapCtx()
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stops.stopped) != 0 {
		t.Errorf("expected no stops for interactive session; got %v", stops.stopped)
	}
	if !strings.Contains(stdout.String(), "skipped interactive orphan") {
		t.Errorf("expected advisory for interactive orphan; got: %s", stdout.String())
	}
}

// TestReapOrphans_CallerSessionSkipped verifies that the current session
// (identified via CLAUDE_SESSION_ID env var) is never stopped even if its
// cwd is missing.
func TestReapOrphans_CallerSessionSkipped(t *testing.T) {
	t.Setenv("CLAUDE_SESSION_ID", "caller-id")

	sessions := []agentSession{
		{ID: "caller-id", Kind: "background", CWD: "/gone/caller"},
		{ID: "other-id", Kind: "background", CWD: "/gone/other"},
	}
	dirExistsNever := func(string) bool { return false }
	var stops fakeStops

	verb := &reapOrphansKong{
		agentsFunc:  reapFakeAgents(sessions),
		dirExists:   dirExistsNever,
		stopSession: stops.stopFunc(),
	}

	ctx, _, _ := makeReapCtx()
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, id := range stops.stopped {
		if id == "caller-id" {
			t.Errorf("stopped the caller session — must never happen")
		}
	}
	if len(stops.stopped) != 1 || stops.stopped[0] != "other-id" {
		t.Errorf("expected only other-id to be stopped; got %v", stops.stopped)
	}
}

// TestReapOrphans_NoneFound verifies the clean "no orphaned sessions" message
// when all background sessions have existing cwds.
func TestReapOrphans_NoneFound(t *testing.T) {
	sessions := []agentSession{
		{ID: "bg1", Kind: "background", CWD: "/live/a"},
		{Kind: "interactive", CWD: "/live/b"},
	}
	dirExistsAlways := func(string) bool { return true }
	var stops fakeStops

	verb := &reapOrphansKong{
		agentsFunc:  reapFakeAgents(sessions),
		dirExists:   dirExistsAlways,
		stopSession: stops.stopFunc(),
	}

	ctx, stdout, _ := makeReapCtx()
	if err := verb.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stops.stopped) != 0 {
		t.Errorf("expected no stops; got %v", stops.stopped)
	}
	out := stdout.String()
	if !strings.Contains(out, "no orphaned background sessions found") {
		t.Errorf("expected no-orphan message; got: %s", out)
	}
}

// TestSessionStopID_PrefersID verifies the short ID is preferred over SessionID.
func TestSessionStopID_PrefersID(t *testing.T) {
	s := agentSession{ID: "short", SessionID: "full-uuid", Name: "name"}
	if got := sessionStopID(s); got != "short" {
		t.Errorf("expected short id; got %q", got)
	}
}

// TestSessionStopID_FallsBackToSessionID verifies fallback to SessionID.
func TestSessionStopID_FallsBackToSessionID(t *testing.T) {
	s := agentSession{SessionID: "full-uuid", Name: "name"}
	if got := sessionStopID(s); got != "full-uuid" {
		t.Errorf("expected full-uuid; got %q", got)
	}
}

// TestSessionStopID_FallsBackToName verifies fallback to Name.
func TestSessionStopID_FallsBackToName(t *testing.T) {
	s := agentSession{Name: "my-session"}
	if got := sessionStopID(s); got != "my-session" {
		t.Errorf("expected name; got %q", got)
	}
}
