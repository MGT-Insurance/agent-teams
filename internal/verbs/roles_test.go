package verbs

import (
	"strings"
	"testing"
)

// TestRoles_DistinctSortedRoles verifies that roles are deduped, sorted, and
// printed one per line.
func TestRoles_DistinctSortedRoles(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:foo":         "body a",
				"dri:hot:bar":     "body b",
				"planner:baz":     "body c",
				"implementer:qux": "body d",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &rolesCmd{}

	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := strings.TrimSpace(stdout.String())
	lines := strings.Split(out, "\n")

	want := []string{"dri", "implementer", "planner"}
	if len(lines) != len(want) {
		t.Fatalf("got lines %v, want %v", lines, want)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line[%d] = %q, want %q", i, lines[i], w)
		}
	}
}

// TestRoles_HotKeysCollapseToRole verifies that `dri:hot:bar` yields role "dri",
// not "dri:hot" or any other form, and collapses with plain `dri:` keys.
func TestRoles_HotKeysCollapseToRole(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:foo":     "body 1",
				"dri:hot:bar": "body 2",
				"planner:baz": "body 3",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &rolesCmd{}

	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := strings.TrimSpace(stdout.String())
	lines := strings.Split(out, "\n")

	// dri and planner — exactly two roles, dri appears once despite two dri: keys.
	want := []string{"dri", "planner"}
	if len(lines) != len(want) {
		t.Fatalf("got lines %v, want %v", lines, want)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line[%d] = %q, want %q", i, lines[i], w)
		}
	}
}

// TestRoles_ExcludesColonlessAndNonString verifies that colonless keys
// (e.g. schema_version) and non-string values are excluded.
func TestRoles_ExcludesColonlessAndNonString(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"schema_version": 1,           // int — no colon, non-string
				"nocoIon":        "no colon",  // string but no colon
				"dri:real":       "body here", // valid
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &rolesCmd{}

	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := strings.TrimSpace(stdout.String())
	lines := strings.Split(out, "\n")

	if len(lines) != 1 || lines[0] != "dri" {
		t.Errorf("got lines %v, want [dri]", lines)
	}
	if strings.Contains(out, "schema_version") {
		t.Errorf("schema_version must not appear in output; got:\n%s", out)
	}
}

// TestRoles_EmptyWhenNoMemories verifies no output when the store is empty.
func TestRoles_EmptyWhenNoMemories(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &rolesCmd{}

	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := stdout.String(); got != "" {
		t.Errorf("expected empty output, got %q", got)
	}
}

// TestRoles_NilContextErrors verifies that a nil context returns an error
// rather than panicking.
func TestRoles_NilContextErrors(t *testing.T) {
	cmd := &rolesCmd{}
	if err := cmd.Run(nil, nil); err == nil {
		t.Error("expected error for nil context, got nil")
	}
}
