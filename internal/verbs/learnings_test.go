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
	cmd := &learningsKong{Role: "implementer"}

	if err := cmd.Run(ctx); err != nil {
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
	cmd := &learningsKong{Role: "implementer"}

	if err := cmd.Run(ctx); err != nil {
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
	cmd := &learningsKong{Role: "implementer"}

	if err := cmd.Run(ctx); err != nil {
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
	cmd := &learningsKong{Role: "implementer"}

	if err := cmd.Run(ctx); err != nil {
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
	cmd := &learningsKong{Role: "implementer"}

	if err := cmd.Run(ctx); err != nil {
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
	cmd := &learningsKong{Role: "implementer"}

	if err := cmd.Run(ctx); err != nil {
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
	cmd := &learningsKong{Role: "implementer"}

	if err := cmd.Run(ctx); err != nil {
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
	// Validate() on an empty Role returns UsageError.
	err := (&learningsKong{}).Validate()
	if err == nil {
		t.Fatal("expected usage error, got nil")
	}
}

// TestLearnings_NilContextReturnsError verifies nil context returns an error.
func TestLearnings_NilContextReturnsError(t *testing.T) {
	err := (&learningsKong{Role: "implementer"}).Run(nil)
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
	err := (&learningsKong{Role: "implementer"}).Run(ctx)
	if err == nil {
		t.Fatal("expected error from bd failure; got nil")
	}
	if !strings.Contains(err.Error(), "simulated failure") {
		t.Errorf("error message should contain 'simulated failure'; got: %v", err)
	}
}

// TestLearnings_HotLayerPreferred verifies that when a role has :hot: keys,
// only those are emitted — not the cold role: keys.
func TestLearnings_HotLayerPreferred(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:hot:condensed": "hot memory body",
				"dri:old-cold":      "cold memory body",
				"dri:another-cold":  "another cold body",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsKong{Role: "dri"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()

	if !strings.Contains(out, "dri:hot:condensed") {
		t.Errorf("expected hot key in output; got:\n%s", out)
	}
	if strings.Contains(out, "dri:old-cold") {
		t.Errorf("cold key must not appear when hot keys exist; got:\n%s", out)
	}
	if strings.Contains(out, "dri:another-cold") {
		t.Errorf("cold key must not appear when hot keys exist; got:\n%s", out)
	}
}

// TestLearnings_ZeroHotFallback verifies that when a role has no :hot: keys,
// all role: keys are emitted (backward-compat for healthy roles).
func TestLearnings_ZeroHotFallback(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"implementer:foo": "body foo",
				"implementer:bar": "body bar",
				"dri:hot:x":       "should not appear",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsKong{Role: "implementer"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()

	if !strings.Contains(out, "implementer:foo") {
		t.Errorf("expected implementer:foo in fallback output; got:\n%s", out)
	}
	if !strings.Contains(out, "implementer:bar") {
		t.Errorf("expected implementer:bar in fallback output; got:\n%s", out)
	}
	if strings.Contains(out, "dri:") {
		t.Errorf("dri: keys must not appear in implementer output; got:\n%s", out)
	}
}

// TestLearnings_ZeroHotFallbackEmitsAllRoleKeys verifies the spec invariant:
// when a role has zero hot keys, ALL its role: keys are emitted — cold and any
// hypothetical hot keys alike. Seeds dri:a and dri:b (both cold) with no hot
// keys; asserts both appear in output.
func TestLearnings_ZeroHotFallbackEmitsAllRoleKeys(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:a": "body a",
				"dri:b": "body b",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsKong{Role: "dri"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()

	if !strings.Contains(out, "dri:a") {
		t.Errorf("expected dri:a in zero-hot fallback output; got:\n%s", out)
	}
	if !strings.Contains(out, "dri:b") {
		t.Errorf("expected dri:b in zero-hot fallback output; got:\n%s", out)
	}
}

// TestRecall_MatchByKey verifies recall matches on key substring.
func TestRecall_MatchByKey(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:deploy-process": "body about deployment",
				"dri:code-review":    "body about reviewing",
				"planner:something":  "other role body",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &recallKong{Role: "dri", Query: "deploy"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()

	if !strings.Contains(out, "dri:deploy-process") {
		t.Errorf("expected deploy-process key in output; got:\n%s", out)
	}
	if strings.Contains(out, "dri:code-review") {
		t.Errorf("code-review key must not appear for query 'deploy'; got:\n%s", out)
	}
	if strings.Contains(out, "planner:") {
		t.Errorf("planner keys must not appear in dri recall; got:\n%s", out)
	}
}

// TestRecall_MatchByBody verifies recall matches on body substring.
func TestRecall_MatchByBody(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:aaa": "this body mentions rebase workflow",
				"dri:bbb": "something unrelated here",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &recallKong{Role: "dri", Query: "rebase"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()

	if !strings.Contains(out, "dri:aaa") {
		t.Errorf("expected dri:aaa in output (body match); got:\n%s", out)
	}
	if strings.Contains(out, "dri:bbb") {
		t.Errorf("dri:bbb must not appear (no body match); got:\n%s", out)
	}
}

// TestRecall_NoMatch verifies empty stdout and nil error when nothing matches.
func TestRecall_NoMatch(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:foo": "body with some text",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &recallKong{Role: "dri", Query: "xyzzy-not-present"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout.Len() > 0 {
		t.Errorf("expected empty output for no-match; got:\n%s", stdout.String())
	}
}

// TestRecall_RolePrefixIsolation verifies recall does not bleed cross-role.
func TestRecall_RolePrefixIsolation(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:thing":        "matching target body",
				"planner:thing":    "cross-role key — must not appear",
				"implementer:blah": "also cross-role",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &recallKong{Role: "dri", Query: "thing"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()

	if !strings.Contains(out, "dri:thing") {
		t.Errorf("expected dri:thing in output; got:\n%s", out)
	}
	if strings.Contains(out, "planner:") {
		t.Errorf("planner: must not bleed through; got:\n%s", out)
	}
	if strings.Contains(out, "implementer:") {
		t.Errorf("implementer: must not bleed through; got:\n%s", out)
	}
}

// TestRecall_HotAndColdSearched verifies recall covers both hot and cold keys.
func TestRecall_HotAndColdSearched(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:hot:summary": "condensed hot body with keyword",
				"dri:old-cold":    "cold body also has keyword",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &recallKong{Role: "dri", Query: "keyword"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()

	if !strings.Contains(out, "dri:hot:summary") {
		t.Errorf("expected hot key in recall output; got:\n%s", out)
	}
	if !strings.Contains(out, "dri:old-cold") {
		t.Errorf("expected cold key in recall output; got:\n%s", out)
	}
}

// TestRecall_MissingRoleReturnsUsageError verifies missing <role> arg returns error.
func TestRecall_MissingRoleReturnsUsageError(t *testing.T) {
	err := (&recallKong{}).Validate()
	if err == nil {
		t.Fatal("expected usage error, got nil")
	}
}

// TestRecall_MissingQueryReturnsUsageError verifies missing <query> arg returns error.
func TestRecall_MissingQueryReturnsUsageError(t *testing.T) {
	err := (&recallKong{Role: "dri"}).Validate()
	if err == nil {
		t.Fatal("expected usage error for missing query, got nil")
	}
}

// TestRecall_NilContextReturnsError verifies nil context returns an error.
func TestRecall_NilContextReturnsError(t *testing.T) {
	err := (&recallKong{Role: "dri", Query: "something"}).Run(nil)
	if err == nil {
		t.Fatal("expected error for nil context; got nil")
	}
}

// TestRecall_CaseInsensitiveMatch verifies query matching is case-insensitive.
func TestRecall_CaseInsensitiveMatch(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:foo": "body with UPPERCASE content",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &recallKong{Role: "dri", Query: "uppercase"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "dri:foo") {
		t.Errorf("expected case-insensitive match; got:\n%s", out)
	}
}

// ── learnings fresh-tier tests (B2: hot UNION fresh, zero-tier fallback) ──────

// TestLearnings_FreshOnlyServed verifies that a role with only fresh: keys (no
// hot: keys) serves those fresh keys.
func TestLearnings_FreshOnlyServed(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"implementer:fresh:tip-a": "fresh body a",
				"implementer:fresh:tip-b": "fresh body b",
				"implementer:old-cold":    "cold body — must not appear",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsKong{Role: "implementer"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()

	if !strings.Contains(out, "implementer:fresh:tip-a") {
		t.Errorf("expected fresh:tip-a in output; got:\n%s", out)
	}
	if !strings.Contains(out, "implementer:fresh:tip-b") {
		t.Errorf("expected fresh:tip-b in output; got:\n%s", out)
	}
	if strings.Contains(out, "implementer:old-cold") {
		t.Errorf("cold key must not appear when fresh keys exist; got:\n%s", out)
	}
}

// TestLearnings_HotAndFreshUnion verifies that when both hot: and fresh: keys
// exist, the served set is their union (both appear in output).
func TestLearnings_HotAndFreshUnion(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:hot:condensed":  "hot body",
				"dri:fresh:new-tip":  "fresh body",
				"dri:cold-stale":     "cold body — must not appear",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsKong{Role: "dri"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()

	if !strings.Contains(out, "dri:hot:condensed") {
		t.Errorf("expected hot key in union output; got:\n%s", out)
	}
	if !strings.Contains(out, "dri:fresh:new-tip") {
		t.Errorf("expected fresh key in union output; got:\n%s", out)
	}
	if strings.Contains(out, "dri:cold-stale") {
		t.Errorf("cold key must not appear when hot+fresh keys exist; got:\n%s", out)
	}
}

// TestLearnings_BothEmptyFallsBackToAllRoleKeys verifies that when a role has
// neither hot: nor fresh: keys, all role: keys are served (zero-tier fallback).
func TestLearnings_BothEmptyFallsBackToAllRoleKeys(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"planner:alpha": "body alpha",
				"planner:beta":  "body beta",
				"dri:hot:other": "should not appear",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsKong{Role: "planner"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()

	if !strings.Contains(out, "planner:alpha") {
		t.Errorf("expected planner:alpha in zero-tier fallback; got:\n%s", out)
	}
	if !strings.Contains(out, "planner:beta") {
		t.Errorf("expected planner:beta in zero-tier fallback; got:\n%s", out)
	}
	if strings.Contains(out, "dri:") {
		t.Errorf("dri: keys must not appear in planner output; got:\n%s", out)
	}
}
