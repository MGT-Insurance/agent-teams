package bd_test

import (
	"encoding/json"
	"fmt"
	"testing"

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
