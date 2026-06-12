package verbs_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/erlloyd/agent-teams/internal/bd"
	"github.com/erlloyd/agent-teams/internal/cli"
	"github.com/erlloyd/agent-teams/internal/verbs"
)

// newCtx builds a cli.Context backed by a fake bd.Client that responds to
// subcommand calls via the provided map: key is the first arg passed to bd
// (the subcommand), value is the stdout bytes the fake returns.
func newCtx(t *testing.T, home string, responses map[string][]byte) (*cli.Context, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		if name != "bd" {
			t.Errorf("exec called with %q, want bd", name)
			return nil, nil, errors.New("unexpected binary")
		}
		// args is [-C, home, subcommand, ...]
		if len(args) < 3 {
			t.Errorf("expected at least 3 args, got %v", args)
			return nil, nil, errors.New("too few args")
		}
		sub := args[2] // subcommand after -C <home>
		resp, ok := responses[sub]
		if !ok {
			t.Errorf("unexpected subcommand %q (full args: %v)", sub, args)
			return nil, nil, errors.New("unexpected subcommand")
		}
		return resp, nil, nil
	}
	client := bd.NewClientWithExec(home, execFn)
	ctx := &cli.Context{
		Home:   home,
		BD:     client,
		Stdout: out,
		Stderr: &bytes.Buffer{},
	}
	return ctx, out
}

// captureArgs returns an ExecFunc that records every call's args slice.
func captureArgs(calls *[][]string) bd.ExecFunc {
	return func(name string, args ...string) ([]byte, []byte, error) {
		cp := make([]string, len(args))
		copy(cp, args)
		*calls = append(*calls, cp)
		return []byte("result\n"), nil, nil
	}
}

// ── ws ────────────────────────────────────────────────────────────────────────

func TestWsPrintsHome(t *testing.T) {
	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, ok := reg.Lookup("ws")
	if !ok {
		t.Fatal("ws not registered")
	}

	out := &bytes.Buffer{}
	ctx := &cli.Context{
		Home:   "/my/workspace",
		Stdout: out,
		Stderr: &bytes.Buffer{},
	}
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("ws.Run: %v", err)
	}
	if got := out.String(); got != "/my/workspace\n" {
		t.Errorf("ws output = %q, want %q", got, "/my/workspace\n")
	}
}

func TestWsNilCtxReturnsError(t *testing.T) {
	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("ws")
	if err := cmd.Run(nil, nil); err == nil {
		t.Error("expected error for nil ctx, got nil")
	}
}

// ── list ──────────────────────────────────────────────────────────────────────

func TestListCallsBDArgs(t *testing.T) {
	var calls [][]string
	client := bd.NewClientWithExec("/ws", captureArgs(&calls))
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("list.Run: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(calls))
	}
	wantArgs := []string{"-C", "/ws", "list", "--status=open"}
	for i, w := range wantArgs {
		if i >= len(calls[0]) || calls[0][i] != w {
			t.Errorf("bd args[%d] = %q, want %q (full: %v)", i, calls[0][i], w, calls[0])
		}
	}
}

func TestListWritesOutput(t *testing.T) {
	ctx, out := newCtx(t, "/ws", map[string][]byte{
		"list": []byte("● issue-1 · My Issue   [● P1 · OPEN]\n"),
	})
	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("list.Run: %v", err)
	}
	if out.Len() == 0 {
		t.Error("list produced no output")
	}
}

// ── list-json ─────────────────────────────────────────────────────────────────

func TestListJSONCallsBDArgs(t *testing.T) {
	var calls [][]string
	issues := []bd.Issue{{ID: "at-abc", Title: "T", Status: "open", CreatedAt: "2026-06-01"}}
	raw, _ := json.Marshal(issues)
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		cp := make([]string, len(args))
		copy(cp, args)
		calls = append(calls, cp)
		return append(raw, '\n'), nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("list-json")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("list-json.Run: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(calls))
	}
	wantArgs := []string{"-C", "/ws", "list", "--status=open", "--json"}
	for i, w := range wantArgs {
		if i >= len(calls[0]) || calls[0][i] != w {
			t.Errorf("bd args[%d] = %q, want %q (full: %v)", i, calls[0][i], w, calls[0])
		}
	}
}

func TestListJSONEmitsValidJSON(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-x1", Title: "Init", Status: "open", CreatedAt: "2026-06-01"},
		{ID: "at-x2", Title: "Impl", Status: "open", CreatedAt: "2026-06-02"},
	}
	raw, _ := json.Marshal(issues)
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return append(raw, '\n'), nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("list-json")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("list-json.Run: %v", err)
	}

	var parsed []bd.Issue
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if len(parsed) != 2 {
		t.Errorf("parsed %d issues, want 2", len(parsed))
	}
}

// ── human-list ────────────────────────────────────────────────────────────────

func TestHumanListCallsBDArgs(t *testing.T) {
	var calls [][]string
	client := bd.NewClientWithExec("/ws", captureArgs(&calls))
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("human-list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(calls))
	}
	wantArgs := []string{"-C", "/ws", "human", "list"}
	for i, w := range wantArgs {
		if i >= len(calls[0]) || calls[0][i] != w {
			t.Errorf("bd args[%d] = %q, want %q (full: %v)", i, calls[0][i], w, calls[0])
		}
	}
}

// ── show ──────────────────────────────────────────────────────────────────────

func TestShowMissingIDReturnsUsageError(t *testing.T) {
	ctx, _ := newCtx(t, "/ws", nil)
	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("show")

	err := cmd.Run(ctx, nil)
	if err == nil {
		t.Fatal("expected UsageError, got nil")
	}
	if cli.ExitCode(err) != 2 {
		t.Errorf("expected exit code 2, got %d (err: %v)", cli.ExitCode(err), err)
	}
}

func TestShowEmptyIDReturnsUsageError(t *testing.T) {
	ctx, _ := newCtx(t, "/ws", nil)
	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("show")

	err := cmd.Run(ctx, []string{""})
	if err == nil {
		t.Fatal("expected UsageError, got nil")
	}
	if cli.ExitCode(err) != 2 {
		t.Errorf("expected exit code 2, got %d", cli.ExitCode(err))
	}
}

func TestShowCallsBDArgs(t *testing.T) {
	var calls [][]string
	client := bd.NewClientWithExec("/ws", captureArgs(&calls))
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("show")
	if err := cmd.Run(ctx, []string{"at-abc123"}); err != nil {
		t.Fatalf("show.Run: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(calls))
	}
	wantArgs := []string{"-C", "/ws", "show", "at-abc123"}
	for i, w := range wantArgs {
		if i >= len(calls[0]) || calls[0][i] != w {
			t.Errorf("bd args[%d] = %q, want %q (full: %v)", i, calls[0][i], w, calls[0])
		}
	}
}

// ── learnings ─────────────────────────────────────────────────────────────────

func TestLearningsMissingRoleReturnsUsageError(t *testing.T) {
	ctx, _ := newCtx(t, "/ws", nil)
	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("learnings")

	err := cmd.Run(ctx, nil)
	if err == nil {
		t.Fatal("expected UsageError, got nil")
	}
	if cli.ExitCode(err) != 2 {
		t.Errorf("expected exit code 2, got %d (err: %v)", cli.ExitCode(err), err)
	}
}

func TestLearningsCallsBDArgs(t *testing.T) {
	var calls [][]string
	client := bd.NewClientWithExec("/ws", captureArgs(&calls))
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("learnings")
	if err := cmd.Run(ctx, []string{"planner"}); err != nil {
		t.Fatalf("learnings.Run: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(calls))
	}
	wantArgs := []string{"-C", "/ws", "memories", "planner"}
	for i, w := range wantArgs {
		if i >= len(calls[0]) || calls[0][i] != w {
			t.Errorf("bd args[%d] = %q, want %q (full: %v)", i, calls[0][i], w, calls[0])
		}
	}
}

func TestLearningsWritesOutput(t *testing.T) {
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return []byte("memory: foo\n"), nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("learnings")
	if err := cmd.Run(ctx, []string{"implementer"}); err != nil {
		t.Fatalf("learnings.Run: %v", err)
	}
	if out.Len() == 0 {
		t.Error("learnings produced no output")
	}
	if got := out.String(); got != "memory: foo\n" {
		t.Errorf("learnings output = %q, want %q", got, "memory: foo\n")
	}
}
