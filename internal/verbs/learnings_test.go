package verbs

import (
	"fmt"
	"strings"
	"testing"
)

// TestLearnings_OnlyRoleKeys verifies that only keys with the requested role
// prefix appear in output, and that cross-role keys and schema_version are
// excluded.
func TestLearnings_OnlyRoleKeys(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"implementer:foo": "implementer body\n\nHOW TO APPLY\nApply like this.",
				"dri:bar":         "dri body mentioning implementer",
				"planner:baz":     "planner body",
				"schema_version":  1,
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsCmd{}

	if err := cmd.Run(ctx, []string{"implementer"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()

	// Must contain the full implementer body including multi-line content.
	if !strings.Contains(out, "implementer:foo") {
		t.Errorf("expected implementer:foo key in output; got:\n%s", out)
	}
	if !strings.Contains(out, "HOW TO APPLY") {
		t.Errorf("expected full body including HOW TO APPLY line; got:\n%s", out)
	}
	if !strings.Contains(out, "Apply like this.") {
		t.Errorf("expected full body including Apply line; got:\n%s", out)
	}

	// Must NOT contain cross-role keys.
	if strings.Contains(out, "dri:") {
		t.Errorf("dri: key must not appear in output; got:\n%s", out)
	}
	if strings.Contains(out, "planner:") {
		t.Errorf("planner: key must not appear in output; got:\n%s", out)
	}

	// Must NOT contain schema_version.
	if strings.Contains(out, "schema_version") {
		t.Errorf("schema_version must not appear in output; got:\n%s", out)
	}
}

// TestLearnings_FullBodyNoCrossRoleBleed verifies the cross-role bleed scenario:
// a dri: memory whose body mentions "implementer" must NOT appear when querying
// the implementer role.
func TestLearnings_FullBodyNoCrossRoleBleed(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"implementer:real": "the real implementer memory",
				"dri:bar":          "this body mentions the word implementer",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsCmd{}

	if err := cmd.Run(ctx, []string{"implementer"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()

	if !strings.Contains(out, "implementer:real") {
		t.Errorf("expected implementer:real in output; got:\n%s", out)
	}
	if strings.Contains(out, "dri:bar") {
		t.Errorf("dri:bar must not bleed through even though body mentions implementer; got:\n%s", out)
	}
}

// TestLearnings_SchemaVersionNeverLeaks asserts schema_version int is always excluded.
func TestLearnings_SchemaVersionNeverLeaks(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"schema_version":   1, // int — must never appear
				"implementer:real": "good memory",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsCmd{}

	if err := cmd.Run(ctx, []string{"implementer"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(stdout.String(), "schema_version") {
		t.Errorf("schema_version leaked into output:\n%s", stdout.String())
	}
}

// TestLearnings_MultiLineBody verifies that multi-line bodies are printed in
// full (not collapsed or truncated).
func TestLearnings_MultiLineBody(t *testing.T) {
	body := "line one\nline two\nHOW TO APPLY\nstep A\nstep B"
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{"implementer:multiline": body}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsCmd{}

	if err := cmd.Run(ctx, []string{"implementer"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	for _, line := range []string{"line one", "line two", "HOW TO APPLY", "step A", "step B"} {
		if !strings.Contains(out, line) {
			t.Errorf("expected %q in full-body output; got:\n%s", line, out)
		}
	}
}

// TestLearnings_EmptyRoleSet verifies empty stdout and nil error when no
// matching role: keys exist.
func TestLearnings_EmptyRoleSet(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"schema_version": 1,
				"dri:something":  "value",
				"planner:other":  "value",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsCmd{}

	if err := cmd.Run(ctx, []string{"implementer"}); err != nil {
		t.Fatalf("expected nil error for empty role set; got: %v", err)
	}
	if stdout.Len() > 0 {
		t.Errorf("expected empty stdout for zero implementer: memories; got:\n%s", stdout.String())
	}
}

// TestLearnings_SortedKeys verifies output is key-sorted for determinism.
func TestLearnings_SortedKeys(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"implementer:zzz": "last",
				"implementer:aaa": "first",
				"implementer:mmm": "middle",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsCmd{}

	if err := cmd.Run(ctx, []string{"implementer"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	posAAA := strings.Index(out, "implementer:aaa")
	posMMM := strings.Index(out, "implementer:mmm")
	posZZZ := strings.Index(out, "implementer:zzz")
	if posAAA < 0 || posMMM < 0 || posZZZ < 0 {
		t.Fatalf("one or more keys missing from output:\n%s", out)
	}
	if !(posAAA < posMMM && posMMM < posZZZ) {
		t.Errorf("keys not in sorted order (aaa=%d, mmm=%d, zzz=%d):\n%s", posAAA, posMMM, posZZZ, out)
	}
}

// TestLearnings_BlankLineBetweenEntries verifies blank line separator between
// multiple entries.
func TestLearnings_BlankLineBetweenEntries(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"implementer:aaa": "body a",
				"implementer:bbb": "body b",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsCmd{}

	if err := cmd.Run(ctx, []string{"implementer"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	// There must be a blank line between the two entries. With two entries,
	// the output is: key\nbody\n\nkey\nbody\n — so there must be "\n\n".
	if !strings.Contains(out, "\n\n") {
		t.Errorf("expected blank line between entries; got:\n%q", out)
	}
}

// TestLearnings_MissingRoleReturnsUsageError verifies missing <role> arg returns
// a usage error with exit code 2.
func TestLearnings_MissingRoleReturnsUsageError(t *testing.T) {
	fbd := &fakeBD{}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsCmd{}

	err := cmd.Run(ctx, nil)
	if err == nil {
		t.Fatal("expected usage error, got nil")
	}
}

// TestLearnings_NilContextReturnsError verifies nil context returns an error.
func TestLearnings_NilContextReturnsError(t *testing.T) {
	cmd := &learningsCmd{}
	err := cmd.Run(nil, []string{"implementer"})
	if err == nil {
		t.Fatal("expected error for nil context; got nil")
	}
}

// TestLearnings_BDErrorPropagates verifies bd failures are returned as errors.
func TestLearnings_BDErrorPropagates(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			return fmt.Errorf("bd memories: simulated failure")
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsCmd{}

	err := cmd.Run(ctx, []string{"implementer"})
	if err == nil {
		t.Fatal("expected error from bd failure; got nil")
	}
	if !strings.Contains(err.Error(), "simulated failure") {
		t.Errorf("error message should contain 'simulated failure'; got: %v", err)
	}
}
