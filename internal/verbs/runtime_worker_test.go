package verbs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
	"github.com/mgt-insurance/agent-teams/internal/workspaceconfig"
)

type fakeRuntimeAdapter struct {
	launchFn func(sessionruntime.Request, sessionruntime.SessionSink) error
	resumeFn func(sessionruntime.Request, sessionruntime.SessionRef) error
}

func (f fakeRuntimeAdapter) Kind() sessionruntime.Kind { return sessionruntime.Codex }
func (f fakeRuntimeAdapter) Launch(_ context.Context, req sessionruntime.Request, sink sessionruntime.SessionSink) error {
	return f.launchFn(req, sink)
}
func (f fakeRuntimeAdapter) Resume(_ context.Context, req sessionruntime.Request, ref sessionruntime.SessionRef) error {
	return f.resumeFn(req, ref)
}

func TestRuntimeWorkerLaunchDurablyTiesCodexThread(t *testing.T) {
	var capturedBody string
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			switch args[0] {
			case "show":
				return issueJSON(bd.Issue{ID: "at-worker1", Status: "open", Description: "runtime: codex\n"}), nil
			case "update":
				for _, arg := range args {
					if strings.HasPrefix(arg, "--body-file=") {
						data, err := os.ReadFile(strings.TrimPrefix(arg, "--body-file="))
						if err != nil {
							return "", err
						}
						capturedBody = string(data)
					}
				}
				return "", nil
			default:
				return "", fmt.Errorf("unexpected bd call: %v", args)
			}
		},
		runJSONFn: func(dst any, args ...string) error {
			issues := dst.(*[]bd.Issue)
			*issues = []bd.Issue{{ID: "at-worker1", Status: "open", Description: "runtime: codex\n"}}
			return nil
		},
	}
	home := t.TempDir()
	ctx, _, _ := makeCtx(fbd, home)
	adapter := fakeRuntimeAdapter{launchFn: func(req sessionruntime.Request, sink sessionruntime.SessionSink) error {
		if req.InitiativeID != "at-worker1" || req.Prompt != "$dri at-worker1" || req.AgentTeamsHome != home {
			t.Fatalf("request = %+v", req)
		}
		return sink(sessionruntime.SessionRef{Runtime: sessionruntime.Codex, ID: "thread-xyz"})
	}}
	cmd := &runtimeWorkerKong{
		Runtime:      "codex",
		InitiativeID: "at-worker1",
		Worktree:     t.TempDir(),
		Prompt:       "$dri at-worker1",
		codex:        adapter,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(capturedBody, "session: thread-xyz\n") {
		t.Fatalf("thread was not durably tied:\n%s", capturedBody)
	}
	if _, err := os.Stat(sessionruntime.EventLogPath(home, "at-worker1")); err != nil {
		t.Fatalf("event log: %v", err)
	}
}

func TestRuntimeWorkerResumeDoesNotRetieSession(t *testing.T) {
	ctx, _, _ := makeCtx(&fakeBD{runFn: func(args ...string) (string, error) {
		t.Fatalf("resume must not mutate beads: %v", args)
		return "", nil
	}}, t.TempDir())
	adapter := fakeRuntimeAdapter{resumeFn: func(req sessionruntime.Request, ref sessionruntime.SessionRef) error {
		if ref.ID != "thread-existing" || ref.Runtime != sessionruntime.Codex {
			t.Fatalf("ref = %+v", ref)
		}
		return nil
	}}
	err := (&runtimeWorkerKong{
		Runtime:      "codex",
		InitiativeID: "at-worker2",
		Worktree:     t.TempDir(),
		Prompt:       "wake",
		ResumeID:     "thread-existing",
		codex:        adapter,
	}).Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRuntimeWorkerResolvesCodexAutoCompactWindowPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		config     string
		wantWindow *int64
	}{
		{name: "unset"},
		{name: "workspace config", config: "auto_compact_window = 300000\n", wantWindow: testInt64Pointer(300000)},
	}

	for _, tt := range tests {
		for _, resume := range []bool{false, true} {
			mode := "launch"
			if resume {
				mode = "resume"
			}
			t.Run(tt.name+" "+mode, func(t *testing.T) {
				home := t.TempDir()
				if tt.config != "" {
					if err := os.WriteFile(filepath.Join(home, workspaceconfig.FileName), []byte(tt.config), 0o600); err != nil {
						t.Fatal(err)
					}
				}

				var captured sessionruntime.Request
				calls := 0
				adapter := fakeRuntimeAdapter{
					launchFn: func(req sessionruntime.Request, _ sessionruntime.SessionSink) error {
						calls++
						captured = req
						return nil
					},
					resumeFn: func(req sessionruntime.Request, _ sessionruntime.SessionRef) error {
						calls++
						captured = req
						return nil
					},
				}
				ctx, _, _ := makeCtx(&fakeBD{}, home)
				cmd := &runtimeWorkerKong{
					Runtime:      "codex",
					InitiativeID: "at-window",
					Worktree:     "/worktree",
					Prompt:       "work",
					codex:        adapter,
				}
				if resume {
					cmd.ResumeID = "thread-123"
				}
				if err := cmd.Run(ctx); err != nil {
					t.Fatalf("Run: %v", err)
				}
				if calls != 1 {
					t.Fatalf("adapter calls = %d, want 1", calls)
				}
				assertOptionalInt64(t, captured.AutoCompactWindow, tt.wantWindow)
				if captured.AgentTeamsHome != home || captured.InitiativeID != "at-window" || captured.Worktree != "/worktree" || captured.Prompt != "work" {
					t.Fatalf("request = %+v", captured)
				}
			})
		}
	}
}

func TestRuntimeWorkerRejectsInvalidAutoCompactWindowBeforeAdapter(t *testing.T) {
	tests := []struct {
		name, config, wantContext string
	}{
		{name: "invalid workspace config", config: "auto_compact_window = 0\n", wantContext: "auto_compact_window"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, workspaceconfig.FileName)
			if err := os.WriteFile(path, []byte(tt.config), 0o600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			adapter := fakeRuntimeAdapter{
				launchFn: func(sessionruntime.Request, sessionruntime.SessionSink) error {
					calls++
					return nil
				},
				resumeFn: func(sessionruntime.Request, sessionruntime.SessionRef) error {
					calls++
					return nil
				},
			}
			ctx, _, _ := makeCtx(&fakeBD{}, home)
			err := (&runtimeWorkerKong{
				Runtime:      "codex",
				InitiativeID: "at-invalid",
				Worktree:     "/worktree",
				Prompt:       "work",
				codex:        adapter,
			}).Run(ctx)
			if err == nil || !strings.Contains(err.Error(), tt.wantContext) {
				t.Fatalf("Run error = %v, want context %q", err, tt.wantContext)
			}
			if !strings.Contains(err.Error(), path) {
				t.Fatalf("Run error = %v, want config path %q", err, path)
			}
			if calls != 0 {
				t.Fatalf("adapter calls = %d, want 0", calls)
			}
			if _, statErr := os.Stat(sessionruntime.EventLogPath(home, "at-invalid")); !os.IsNotExist(statErr) {
				t.Fatalf("event log created before validation: %v", statErr)
			}
		})
	}
}

func assertOptionalInt64(t *testing.T, got, want *int64) {
	t.Helper()
	if got == nil || want == nil {
		if got != nil || want != nil {
			t.Fatalf("optional int64 = %v, want %v", got, want)
		}
		return
	}
	if *got != *want {
		t.Fatalf("optional int64 = %d, want %d", *got, *want)
	}
}

func testInt64Pointer(value int64) *int64 {
	return &value
}
