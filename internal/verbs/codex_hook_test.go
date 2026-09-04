package verbs

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

func TestCodexHookSessionStartTiesAllSourcesAndSurfacesUnreadOnlyOnColdCatchup(t *testing.T) {
	for _, tc := range []struct {
		name        string
		sourceJSON  string
		wantUnread  bool
		wantContext bool
	}{
		{name: "startup", sourceJSON: `,"source":"startup"`, wantUnread: true, wantContext: true},
		{name: "resume", sourceJSON: `,"source":"resume"`, wantUnread: true, wantContext: true},
		{name: "clear", sourceJSON: `,"source":"clear"`},
		{name: "compact", sourceJSON: `,"source":"compact"`},
		{name: "missing"},
		{name: "unknown", sourceJSON: `,"source":"unknown"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, stdout, _ := makeCtx(&fakeBD{}, t.TempDir())
			tied := 0
			unread := 0
			deps := codexHookDeps{
				resolve: func(*cli.Context, string, string) (bd.Issue, error) { return bd.Issue{ID: "at-codex"}, nil },
				tie: func(_ *cli.Context, id, session string) error {
					if id == "at-codex" && session == "thread-1" {
						tied++
					}
					return nil
				},
				unread: func(*cli.Context, string) ([]bd.Issue, error) {
					unread++
					return []bd.Issue{{ID: "msg-1"}}, nil
				},
			}
			input := strings.NewReader(`{"session_id":"thread-1","cwd":"/w","hook_event_name":"SessionStart"` + tc.sourceJSON + `}`)
			if err := runCodexHook(ctx, "session-start", input, deps); err != nil {
				t.Fatal(err)
			}
			if tied != 1 {
				t.Fatalf("tie calls = %d, want 1", tied)
			}
			if got := unread == 1; got != tc.wantUnread {
				t.Fatalf("unread called = %v, want %v", got, tc.wantUnread)
			}
			if got := strings.Contains(stdout.String(), "ateam mail inbox"); got != tc.wantContext {
				t.Fatalf("output = %s", stdout.String())
			}
			if tc.wantContext && !strings.Contains(stdout.String(), `"hookEventName":"SessionStart"`) {
				t.Fatalf("output = %s, want SessionStart hook-specific context", stdout.String())
			}
		})
	}
}

// TestRunCodexHookThreadsSessionIDToResolve verifies runCodexHook passes the
// hook input session ID through to initiative resolution.
func TestRunCodexHookThreadsSessionIDToResolve(t *testing.T) {
	var gotCWD, gotSessionID string
	deps := codexHookDeps{
		resolve: func(_ *cli.Context, cwd, sessionID string) (bd.Issue, error) {
			gotCWD, gotSessionID = cwd, sessionID
			return bd.Issue{ID: "at-codex"}, nil
		},
		tie:    func(*cli.Context, string, string) error { return nil },
		unread: func(*cli.Context, string) ([]bd.Issue, error) { return nil, nil },
	}
	ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
	input := strings.NewReader(`{"session_id":"thread-9","cwd":"/w","hook_event_name":"SessionStart"}`)
	if err := runCodexHook(ctx, "session-start", input, deps); err != nil {
		t.Fatal(err)
	}
	if gotCWD != "/w" || gotSessionID != "thread-9" {
		t.Errorf("deps.resolve called with cwd=%q sessionID=%q, want cwd=/w sessionID=thread-9", gotCWD, gotSessionID)
	}
}

func TestCodexHookRejectsRemovedEventsWithoutSideEffects(t *testing.T) {
	for _, event := range []string{"user-prompt-submit", "stop"} {
		t.Run(event, func(t *testing.T) {
			calls := 0
			deps := codexHookDeps{
				resolve: func(*cli.Context, string, string) (bd.Issue, error) {
					calls++
					return bd.Issue{ID: "at-codex"}, nil
				},
				tie: func(*cli.Context, string, string) error {
					calls++
					return nil
				},
				unread: func(*cli.Context, string) ([]bd.Issue, error) {
					calls++
					return nil, nil
				},
			}
			ctx, stdout, _ := makeCtx(&fakeBD{}, t.TempDir())
			err := runCodexHook(ctx, event, strings.NewReader(`{"session_id":"thread-1","cwd":"/w","source":"startup"}`), deps)
			if err == nil || !strings.Contains(err.Error(), `unsupported event "`+event+`"`) {
				t.Fatalf("runCodexHook error = %v, want unsupported event", err)
			}
			if calls != 0 || stdout.Len() != 0 {
				t.Fatalf("side effects = %d, output = %q; want neither", calls, stdout.String())
			}
		})
	}
}

// TestResolveCodexHookInitiativeSessionFirstHitFromNonMatchingCwd is the
// Codex half of ring .4 (at-1k234): a Codex initiative tied via
// "session: <id>" resolves even when cwd matches no registered worktree at
// all — restoring correct unread counting under a launch-cwd mismatch,
// mid-drift or on a later hook call after the SessionStart tie already
// exists.
func TestResolveCodexHookInitiativeSessionFirstHitFromNonMatchingCwd(t *testing.T) {
	f := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			return unmarshalIssues(dst,
				bd.Issue{ID: "at-codex-mine", Status: "open", Description: "worktree: /a/b/wt\nruntime: codex\nsession: thread-mine\n"},
				bd.Issue{ID: "at-codex-other", Status: "open", Description: "worktree: /x/y/wt\nruntime: codex\nsession: thread-other\n"},
			)
		},
	}
	ctx, _, _ := makeCtx(f, t.TempDir())

	issue, err := resolveCodexHookInitiative(ctx, "/no/such/path", "thread-mine")
	if err != nil {
		t.Fatalf("resolveCodexHookInitiative: unexpected error: %v", err)
	}
	if issue.ID != "at-codex-mine" {
		t.Errorf("resolveCodexHookInitiative: issue.ID = %q, want at-codex-mine (session tie must win over a non-matching cwd)", issue.ID)
	}
}

// TestResolveCodexHookInitiativeNoTieFallsBackToCwd preserves existing
// behavior: an empty sessionID, or one tied to no open initiative, falls
// through to matchByWorktreeOrAncestor(cwd) unchanged.
func TestResolveCodexHookInitiativeNoTieFallsBackToCwd(t *testing.T) {
	for _, sessionID := range []string{"", "thread-untied"} {
		t.Run("sessionID="+sessionID, func(t *testing.T) {
			f := &fakeBD{
				runJSONFn: func(dst any, args ...string) error {
					return unmarshalIssues(dst,
						bd.Issue{ID: "at-codex-mine", Status: "open", Description: "worktree: /a/b/wt\nruntime: codex\n"},
					)
				},
			}
			ctx, _, _ := makeCtx(f, t.TempDir())

			issue, err := resolveCodexHookInitiative(ctx, "/a/b/wt", sessionID)
			if err != nil {
				t.Fatalf("resolveCodexHookInitiative: unexpected error: %v", err)
			}
			if issue.ID != "at-codex-mine" {
				t.Errorf("resolveCodexHookInitiative: issue.ID = %q, want at-codex-mine (cwd fallback)", issue.ID)
			}
		})
	}
}

// TestResolveCodexHookInitiativeNoMatchAnywhere preserves the existing
// errCodexHookNoInitiative contract when neither the session tie nor the cwd
// match anything.
func TestResolveCodexHookInitiativeNoMatchAnywhere(t *testing.T) {
	f := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			return unmarshalIssues(dst)
		},
	}
	ctx, _, _ := makeCtx(f, t.TempDir())

	_, err := resolveCodexHookInitiative(ctx, "/no/such/path", "thread-untied")
	if !errors.Is(err, errCodexHookNoInitiative) {
		t.Errorf("resolveCodexHookInitiative: err = %v, want errCodexHookNoInitiative", err)
	}
}

// TestResolveCodexHookInitiativeSessionFirstErrorPropagates verifies the ring
// .4 review fix (agent-teams-y814.8, at-1k234): a bd error from the
// session-first resolveInitiativeBySession call must propagate, matching
// this function's own cwd-fallback contract just below it (which already
// does `return bd.Issue{}, err` on a bd failure) — it must not be silently
// swallowed via `err == nil && found` and fall through to the cwd match.
func TestResolveCodexHookInitiativeSessionFirstErrorPropagates(t *testing.T) {
	// The session-first list call (inside resolveInitiativeBySession) and the
	// cwd-fallback list call issue the IDENTICAL bd args ("list",
	// "--status=open", "--json"), so a swallow-and-fall-through bug can't be
	// caught by inspecting args the way hook-scan's test does — it has to be
	// caught by call ORDER: fail only the first call (session-first) and
	// succeed the second (cwd fallback, matching cwd) with a real issue. A
	// swallowing implementation reaches the second call and returns that
	// issue with no error; the fixed implementation returns the first call's
	// error and never makes a second call.
	wantErr := errors.New("bd list: boom")
	calls := 0
	f := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			calls++
			if calls == 1 {
				return wantErr
			}
			return unmarshalIssues(dst, bd.Issue{ID: "at-codex-mine", Status: "open", Description: "worktree: /a/b/wt\nruntime: codex\n"})
		},
	}
	ctx, _, _ := makeCtx(f, t.TempDir())

	_, err := resolveCodexHookInitiative(ctx, "/a/b/wt", "thread-mine")
	if !errors.Is(err, wantErr) {
		t.Fatalf("resolveCodexHookInitiative: err = %v, want it to wrap %v (session-first bd error must propagate, not fall through to a second cwd-fallback bd call)", err, wantErr)
	}
	if calls != 1 {
		t.Errorf("resolveCodexHookInitiative: made %d bd calls, want exactly 1 (must return on the session-first error, never reach cwd fallback)", calls)
	}
}

func TestCodexHookDiagnosticsAndNonInitiativeNoop(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{name: "non initiative", err: errCodexHookNoInitiative, want: "{}"},
		{name: "registry failure", err: errors.New("beads unavailable"), want: "beads unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, stdout, _ := makeCtx(&fakeBD{}, t.TempDir())
			deps := codexHookDeps{resolve: func(*cli.Context, string, string) (bd.Issue, error) { return bd.Issue{}, tc.err }}
			if err := runCodexHook(ctx, "session-start", bytes.NewBufferString(`{"cwd":"/w"}`), deps); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("output = %q", stdout.String())
			}
		})
	}
}
