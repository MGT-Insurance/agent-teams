package verbs

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

func TestCodexHooksTieAndSurfaceUnreadMail(t *testing.T) {
	for _, tc := range []struct {
		event, wireEvent, wantHook string
		wantTie                    bool
	}{
		{event: "session-start", wireEvent: "SessionStart", wantHook: "SessionStart", wantTie: true},
		{event: "user-prompt-submit", wireEvent: "UserPromptSubmit", wantHook: "UserPromptSubmit"},
	} {
		t.Run(tc.event, func(t *testing.T) {
			ctx, stdout, _ := makeCtx(&fakeBD{}, t.TempDir())
			tied := false
			deps := codexHookDeps{
				resolve: func(*cli.Context, string) (bd.Issue, error) { return bd.Issue{ID: "at-codex"}, nil },
				tie: func(_ *cli.Context, id, session string) error {
					tied = id == "at-codex" && session == "thread-1"
					return nil
				},
				unread: func(*cli.Context, string) ([]bd.Issue, error) { return []bd.Issue{{ID: "msg-1"}}, nil },
				repair: func(*cli.Context, string) error { return nil },
			}
			input := strings.NewReader(`{"session_id":"thread-1","cwd":"/w","hook_event_name":"` + tc.wireEvent + `"}`)
			if err := runCodexHook(ctx, tc.event, input, deps); err != nil {
				t.Fatal(err)
			}
			if tied != tc.wantTie {
				t.Fatalf("tied = %v, want %v", tied, tc.wantTie)
			}
			if !strings.Contains(stdout.String(), `"hookEventName":"`+tc.wantHook+`"`) || !strings.Contains(stdout.String(), "ateam mail inbox") {
				t.Fatalf("output = %s", stdout.String())
			}
		})
	}
}

func TestCodexStopContinuesExactlyOnce(t *testing.T) {
	resolveCalls := 0
	deps := codexHookDeps{
		resolve: func(*cli.Context, string) (bd.Issue, error) { resolveCalls++; return bd.Issue{ID: "at-codex"}, nil },
		unread:  func(*cli.Context, string) ([]bd.Issue, error) { return []bd.Issue{{ID: "msg-1"}}, nil },
		repair:  func(*cli.Context, string) error { return nil },
	}
	ctx, stdout, _ := makeCtx(&fakeBD{}, t.TempDir())
	if err := runCodexHook(ctx, "stop", strings.NewReader(`{"cwd":"/w","hook_event_name":"Stop","stop_hook_active":false}`), deps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"decision":"block"`) || !strings.Contains(stdout.String(), "ateam mail inbox") {
		t.Fatalf("first output = %s", stdout.String())
	}

	ctx, stdout, _ = makeCtx(&fakeBD{}, t.TempDir())
	if err := runCodexHook(ctx, "stop", strings.NewReader(`{"cwd":"/w","hook_event_name":"Stop","stop_hook_active":true}`), deps); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout.String()) != "{}" || resolveCalls != 1 {
		t.Fatalf("second output = %q, resolve calls = %d", stdout.String(), resolveCalls)
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
			deps := codexHookDeps{resolve: func(*cli.Context, string) (bd.Issue, error) { return bd.Issue{}, tc.err }}
			if err := runCodexHook(ctx, "session-start", bytes.NewBufferString(`{"cwd":"/w"}`), deps); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("output = %q", stdout.String())
			}
		})
	}
}
