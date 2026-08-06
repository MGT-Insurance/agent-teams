package verbs

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
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
		if req.InitiativeID != "at-worker1" || req.Prompt != "$dri at-worker1" {
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
