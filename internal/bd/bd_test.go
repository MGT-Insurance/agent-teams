package bd_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

// fakeExec builds an ExecFunc that returns a fixed response for "bd" calls.
// Verifies that the injected args start with ["-C", wantHome].
func fakeExec(t *testing.T, wantHome string, respondJSON []byte) bd.ExecFunc {
	t.Helper()
	return func(name string, args ...string) ([]byte, []byte, error) {
		if name != "bd" {
			t.Errorf("exec called with %q, want bd", name)
		}
		if len(args) < 2 || args[0] != "-C" || args[1] != wantHome {
			t.Errorf("expected args to start with [-C %s], got %v", wantHome, args)
		}
		return respondJSON, nil, nil
	}
}

func TestRunBuildsArgs(t *testing.T) {
	capturedArgs := []string(nil)
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		capturedArgs = args
		return []byte("hello\n"), nil, nil
	}
	c := bd.NewClientWithExec("/my/home", execFn)
	out, err := c.Run("list", "--status=open")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "hello" {
		t.Errorf("Run output = %q, want %q", out, "hello")
	}
	// Should have prepended -C /my/home
	want := []string{"-C", "/my/home", "list", "--status=open"}
	if len(capturedArgs) != len(want) {
		t.Fatalf("args len = %d, want %d: %v", len(capturedArgs), len(want), capturedArgs)
	}
	for i, w := range want {
		if capturedArgs[i] != w {
			t.Errorf("args[%d] = %q, want %q", i, capturedArgs[i], w)
		}
	}
}

func TestRunErrorIncludesStderr(t *testing.T) {
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return nil, []byte("bd: no such database\n"), fmt.Errorf("exit status 1")
	}
	c := bd.NewClientWithExec("/home", execFn)
	_, err := c.Run("list")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if msg := err.Error(); msg == "" {
		t.Error("error message empty")
	}
}

// TestRunContext_ObservesCtxDeadline proves RunContext is bounded by the ctx
// it's given: a ContextExecFunc fake that blocks until ctx is done and then
// returns ctx.Err() causes RunContext to return quickly with an error
// wrapping context.DeadlineExceeded — the contract bead's core promise for
// the exec seam every pull-guard caller builds on.
func TestRunContext_ObservesCtxDeadline(t *testing.T) {
	fn := func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	c := bd.NewClientWithContextExec("/ws", fn)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.RunContext(ctx, "dolt", "pull")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error from a ctx that expired mid-call")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected err to wrap context.DeadlineExceeded, got %v", err)
	}
	if elapsed > 1*time.Second {
		t.Fatalf("RunContext took %v to return after a 50ms deadline, want under 1s", elapsed)
	}
}

// TestRunContext_ViaNewClientWithExec_StillWorks proves Run — which now
// delegates to RunContext(context.Background(), ...) — is unaffected for a
// Client built with the plain, context-less NewClientWithExec.
func TestRunContext_ViaNewClientWithExec_StillWorks(t *testing.T) {
	c := bd.NewClientWithExec("/ws", func(name string, args ...string) ([]byte, []byte, error) {
		return []byte("ok\n"), nil, nil
	})
	out, err := c.RunContext(context.Background(), "status")
	if err != nil {
		t.Fatalf("RunContext: %v", err)
	}
	if out != "ok" {
		t.Errorf("RunContext output = %q, want %q", out, "ok")
	}
}

func TestRunJSON(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-abc", Title: "Test Issue", Status: "open", CreatedAt: "2026-06-01"},
	}
	raw, _ := json.Marshal(issues)

	c := bd.NewClientWithExec("/ws", fakeExec(t, "/ws", raw))
	var got []bd.Issue
	if err := c.RunJSON(&got, "list", "--status=open", "--json"); err != nil {
		t.Fatalf("RunJSON: %v", err)
	}
	if len(got) != 1 || got[0].ID != "at-abc" {
		t.Errorf("RunJSON result = %+v, want [{at-abc ...}]", got)
	}
}

func TestRunJSONBadJSON(t *testing.T) {
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return []byte("not json"), nil, nil
	}
	c := bd.NewClientWithExec("/ws", execFn)
	var got []bd.Issue
	err := c.RunJSON(&got, "list", "--json")
	if err == nil {
		t.Fatal("expected unmarshal error, got nil")
	}
}

// ---- ShowIssue -------------------------------------------------------------

// fakeRunner implements the runner interface consumed by ShowIssue.
type fakeRunner struct {
	runFn func(args ...string) (string, error)
}

func (f *fakeRunner) Run(args ...string) (string, error) {
	return f.runFn(args...)
}

func TestShowIssue_HappyPath(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-x", Description: "worktree: /p\nbranch: b", Status: "open"},
	}
	raw, _ := json.Marshal(issues)

	r := &fakeRunner{runFn: func(args ...string) (string, error) {
		return string(raw), nil
	}}

	got, err := bd.ShowIssue(r, "at-x")
	if err != nil {
		t.Fatalf("ShowIssue: %v", err)
	}
	if got.ID != "at-x" {
		t.Errorf("ID = %q, want %q", got.ID, "at-x")
	}
	if got.Description != "worktree: /p\nbranch: b" {
		t.Errorf("Description = %q, want %q", got.Description, "worktree: /p\nbranch: b")
	}
}

func TestShowIssue_EmptyArray(t *testing.T) {
	r := &fakeRunner{runFn: func(args ...string) (string, error) {
		return "[]", nil
	}}

	_, err := bd.ShowIssue(r, "at-missing")
	if err == nil {
		t.Fatal("expected error for empty array, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestShowIssue_RunError(t *testing.T) {
	r := &fakeRunner{runFn: func(args ...string) (string, error) {
		return "", fmt.Errorf("bd show: exit status 1")
	}}

	_, err := bd.ShowIssue(r, "at-err")
	if err == nil {
		t.Fatal("expected error from Run, got nil")
	}
	if !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("expected Run error propagated, got: %v", err)
	}
}

func TestShowIssue_BadJSON(t *testing.T) {
	r := &fakeRunner{runFn: func(args ...string) (string, error) {
		return "not json", nil
	}}

	_, err := bd.ShowIssue(r, "at-bad")
	if err == nil {
		t.Fatal("expected unmarshal error, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("expected 'unmarshal' in error, got: %v", err)
	}
}
