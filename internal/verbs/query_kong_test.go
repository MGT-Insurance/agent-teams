// query_kong_test.go holds core-path tests for the native kong structs in query.go.
// White-box (package verbs) so unexported structs are directly constructable.
package verbs

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// buildQueryCtx wires a fake bd client that returns responses keyed by bd subcommand.
func buildQueryCtx(t *testing.T, home string, responses map[string][]byte) (*cli.Context, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		if name != "bd" {
			t.Errorf("unexpected binary %q", name)
			return nil, nil, nil
		}
		// args: [-C, home, subcommand, ...]
		if len(args) < 3 {
			return nil, nil, nil
		}
		sub := args[2]
		resp, ok := responses[sub]
		if !ok {
			t.Errorf("unexpected subcommand %q", sub)
			return nil, nil, nil
		}
		return resp, nil, nil
	}
	client := bd.NewClientWithExec(home, execFn)
	ctx := &cli.Context{Home: home, BD: client, Stdout: out, Stderr: &bytes.Buffer{}}
	return ctx, out
}

// ── wsKong ────────────────────────────────────────────────────────────────────

func TestWsKongPrintsHome(t *testing.T) {
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/test/home", Stdout: out, Stderr: &bytes.Buffer{}}
	cmd := &wsKong{}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("wsKong.Run: %v", err)
	}
	if got := out.String(); got != "/test/home\n" {
		t.Errorf("wsKong output = %q, want %q", got, "/test/home\n")
	}
}

func TestWsKongNilCtxReturnsError(t *testing.T) {
	if err := (&wsKong{}).Run(nil); err == nil {
		t.Error("expected error for nil ctx, got nil")
	}
}

// ── listKong ──────────────────────────────────────────────────────────────────

func TestListKongCallsBD(t *testing.T) {
	ctx, out := buildQueryCtx(t, "/ws", map[string][]byte{
		"list": []byte("● at-1 · My Initiative   [● P1 · OPEN]\n"),
	})
	if err := (&listKong{}).Run(ctx); err != nil {
		t.Fatalf("listKong.Run: %v", err)
	}
	if out.Len() == 0 {
		t.Error("listKong produced no output")
	}
}

func TestListKongNilCtx(t *testing.T) {
	if err := (&listKong{}).Run(nil); err == nil {
		t.Error("expected error for nil ctx")
	}
}

// ── listJSONKong ──────────────────────────────────────────────────────────────

func TestListJSONKongEmitsJSON(t *testing.T) {
	issues := []bd.Issue{{ID: "at-x1", Title: "T", Status: "open", CreatedAt: "2026-06-01"}}
	raw, _ := json.Marshal(issues)
	ctx, out := buildQueryCtx(t, "/ws", map[string][]byte{
		"list": append(raw, '\n'),
	})
	if err := (&listJSONKong{}).Run(ctx); err != nil {
		t.Fatalf("listJSONKong.Run: %v", err)
	}
	if out.Len() == 0 {
		t.Error("listJSONKong produced no output")
	}
}

// ── showKong ──────────────────────────────────────────────────────────────────

func TestShowKongCallsBD(t *testing.T) {
	ctx, out := buildQueryCtx(t, "/ws", map[string][]byte{
		"show": []byte("● at-abc · Some Initiative   [● P1 · OPEN]\n"),
	})
	cmd := &showKong{ID: "at-abc"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("showKong.Run: %v", err)
	}
	if out.Len() == 0 {
		t.Error("showKong produced no output")
	}
}

func TestShowKongNilCtx(t *testing.T) {
	if err := (&showKong{ID: "at-x"}).Run(nil); err == nil {
		t.Error("expected error for nil ctx")
	}
}

// ── learningsKong ─────────────────────────────────────────────────────────────

func TestLearningsKongFiltersRole(t *testing.T) {
	memoriesJSON := []byte(`{"planner:foo":"body1","dri:bar":"should not appear"}` + "\n")
	ctx, out := buildQueryCtx(t, "/ws", map[string][]byte{
		"memories": memoriesJSON,
	})
	cmd := &learningsKong{Role: "planner"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("learningsKong.Run: %v", err)
	}
	got := out.String()
	if !bytes.Contains([]byte(got), []byte("planner:foo")) {
		t.Errorf("expected planner:foo in output; got: %q", got)
	}
	if bytes.Contains([]byte(got), []byte("dri:")) {
		t.Errorf("dri: key must not appear; got: %q", got)
	}
}

func TestLearningsKongNilCtx(t *testing.T) {
	if err := (&learningsKong{Role: "planner"}).Run(nil); err == nil {
		t.Error("expected error for nil ctx")
	}
}

// ── recallKong ────────────────────────────────────────────────────────────────

func TestRecallKongFiltersQuery(t *testing.T) {
	memoriesJSON := []byte(`{"planner:foo":"ship it fast","planner:bar":"slow path"}` + "\n")
	ctx, out := buildQueryCtx(t, "/ws", map[string][]byte{
		"memories": memoriesJSON,
	})
	cmd := &recallKong{Role: "planner", Query: "ship"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("recallKong.Run: %v", err)
	}
	got := out.String()
	if !bytes.Contains([]byte(got), []byte("planner:foo")) {
		t.Errorf("expected matching key planner:foo; got: %q", got)
	}
	if bytes.Contains([]byte(got), []byte("planner:bar")) {
		t.Errorf("non-matching key planner:bar must not appear; got: %q", got)
	}
}

func TestRecallKongNilCtx(t *testing.T) {
	if err := (&recallKong{Role: "r", Query: "q"}).Run(nil); err == nil {
		t.Error("expected error for nil ctx")
	}
}

// ── primeKong ─────────────────────────────────────────────────────────────────

func TestPrimeKongPrintsUserKeys(t *testing.T) {
	memoriesJSON := []byte(`{"user:pref1":"always test","schema_version":1,"dri:foo":"bar"}` + "\n")
	ctx, out := buildQueryCtx(t, "/ws", map[string][]byte{
		"memories": memoriesJSON,
	})
	if err := (&primeKong{}).Run(ctx); err != nil {
		t.Fatalf("primeKong.Run: %v", err)
	}
	got := out.String()
	if !bytes.Contains([]byte(got), []byte("pref1")) {
		t.Errorf("expected user pref1 in output; got: %q", got)
	}
	if bytes.Contains([]byte(got), []byte("dri:foo")) {
		t.Errorf("non-user key must not appear; got: %q", got)
	}
}

func TestPrimeKongNilCtx(t *testing.T) {
	if err := (&primeKong{}).Run(nil); err == nil {
		t.Error("expected error for nil ctx")
	}
}

// ── rolesKong ─────────────────────────────────────────────────────────────────

func TestRolesKongListsNamespaces(t *testing.T) {
	memoriesJSON := []byte(`{"planner:foo":"a","dri:bar":"b","user:pref":"c"}` + "\n")
	ctx, out := buildQueryCtx(t, "/ws", map[string][]byte{
		"memories": memoriesJSON,
	})
	if err := (&rolesKong{}).Run(ctx); err != nil {
		t.Fatalf("rolesKong.Run: %v", err)
	}
	got := out.String()
	for _, role := range []string{"dri", "planner", "user"} {
		if !bytes.Contains([]byte(got), []byte(role)) {
			t.Errorf("expected role %q in output; got: %q", role, got)
		}
	}
}

func TestRolesKongNilCtx(t *testing.T) {
	if err := (&rolesKong{}).Run(nil); err == nil {
		t.Error("expected error for nil ctx")
	}
}
