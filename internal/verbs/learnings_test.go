package verbs

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mgt-insurance/agent-teams/internal/cli"
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

// TestLearnings_EmptyRoleSet verifies the loud EMPTY marker (not silence) when
// no matching role: keys exist (agent-teams-bbsz.21).
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
	if got := stdout.String(); got != "[learnings implementer: EMPTY]\n" {
		t.Errorf("expected loud EMPTY marker for zero implementer: memories; got:\n%q", got)
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

// TestLearnings_MissingRoleReturnsUsageError verifies kong enforces the required
// <role> positional at parse time (exit-2 parity: kong.ParseError → ExitCode 2).
func TestLearnings_MissingRoleReturnsUsageError(t *testing.T) {
	p, err := cli.NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	p.AddVerb("learnings", "Print role memories.", &learningsKong{})
	_, parseErr := p.Parse([]string{"learnings"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing <role>, got nil")
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

// TestRecall_NoMatch verifies the loud zero-match header (not silence) plus a
// nearest-keys line when nothing matches (agent-teams-bbsz.22, fixing
// bbsz.13's silent zero-byte miss).
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
	want := "[recall dri \"xyzzy-not-present\": 0 matches]\nnearest: dri:foo\n"
	if got := stdout.String(); got != want {
		t.Errorf("expected loud zero-match header + nearest line; got:\n%q\nwant:\n%q", got, want)
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

// TestRecall_MissingRoleReturnsUsageError verifies kong enforces the required
// <role> positional at parse time.
func TestRecall_MissingRoleReturnsUsageError(t *testing.T) {
	p, err := cli.NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	p.AddVerb("recall", "Search role memories.", &recallKong{})
	_, parseErr := p.Parse([]string{"recall"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing <role>, got nil")
	}
}

// TestRecall_MissingQueryReturnsUsageError verifies kong enforces the required
// <query> positional at parse time when only <role> is provided.
func TestRecall_MissingQueryReturnsUsageError(t *testing.T) {
	p, err := cli.NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	p.AddVerb("recall", "Search role memories.", &recallKong{})
	_, parseErr := p.Parse([]string{"recall", "dri"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing <query>, got nil")
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

// ── recall widening (agent-teams-bbsz.22) ──────────────────────────────────────

// TestRecall_HeaderAlwaysPrintedOnMatch verifies the header line is printed
// (exact shape) even when there ARE matches, not just on a miss.
func TestRecall_HeaderAlwaysPrintedOnMatch(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{"dri:foo": "body about deploy"}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &recallKong{Role: "dri", Query: "deploy"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantHeader := `[recall dri "deploy": 1 matches]` + "\n"
	if got := stdout.String(); !strings.HasPrefix(got, wantHeader) {
		t.Errorf("expected output to start with header %q; got:\n%q", wantHeader, got)
	}
}

// TestRecall_MultiTermRanksByDistinctMatchCount verifies tokenized
// multi-term ranking: the query is split on whitespace, and a key matching
// MORE distinct query tokens ranks ahead of a key matching fewer — even when
// the fewer-match key would otherwise sort first alphabetically.
func TestRecall_MultiTermRanksByDistinctMatchCount(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				// "aaa" sorts first alphabetically but matches only 1 of 2
				// query tokens ("worktree"); "zzz" matches both.
				"dri:aaa": "worktree notes only",
				"dri:zzz": "worktree and race notes",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &recallKong{Role: "dri", Query: "worktree race"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "2 matches") {
		t.Errorf("expected 2 matches in header; got:\n%s", out)
	}
	posZZZ := strings.Index(out, "dri:zzz")
	posAAA := strings.Index(out, "dri:aaa")
	if posZZZ < 0 || posAAA < 0 {
		t.Fatalf("expected both keys in output; got:\n%s", out)
	}
	if !(posZZZ < posAAA) {
		t.Errorf("expected 2-distinct-token match (dri:zzz) ranked ahead of 1-token match (dri:aaa); got:\n%s", out)
	}
}

// TestRecall_TieBreaksByKeyAscending verifies that keys matching the same
// number of distinct tokens are ordered by key ascending.
func TestRecall_TieBreaksByKeyAscending(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:zzz": "mentions worktree",
				"dri:aaa": "mentions worktree",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &recallKong{Role: "dri", Query: "worktree"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	posAAA := strings.Index(out, "dri:aaa")
	posZZZ := strings.Index(out, "dri:zzz")
	if posAAA < 0 || posZZZ < 0 {
		t.Fatalf("expected both keys in output; got:\n%s", out)
	}
	if !(posAAA < posZZZ) {
		t.Errorf("expected tie broken by key ascending (aaa before zzz); got:\n%s", out)
	}
}

// TestRecall_ZeroMatchesListsNearestKeysCappedAtFive verifies that on a
// zero-match query, the header is followed by a "nearest:" line listing at
// most recallNearestCount keys, and that the command still exits 0 (fixing
// bbsz.13's silent zero-byte miss without becoming a hard failure).
func TestRecall_ZeroMatchesListsNearestKeysCappedAtFive(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:a": "alpha", "dri:b": "bravo", "dri:c": "charlie",
				"dri:d": "delta", "dri:e": "echo", "dri:f": "foxtrot",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &recallKong{Role: "dri", Query: "nonexistent-term-xyz"}

	err := cmd.Run(ctx)
	if err != nil {
		t.Fatalf("expected exit-0 (nil error) on zero matches; got: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, `[recall dri "nonexistent-term-xyz": 0 matches]`) {
		t.Errorf("expected zero-match header; got:\n%s", out)
	}
	nearestLine := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "nearest:") {
			nearestLine = line
			break
		}
	}
	if nearestLine == "" {
		t.Fatalf("expected a nearest: line; got:\n%s", out)
	}
	gotKeys := strings.Fields(strings.TrimPrefix(nearestLine, "nearest: "))
	if len(gotKeys) > recallNearestCount {
		t.Errorf("expected at most %d nearest keys, got %d: %v", recallNearestCount, len(gotKeys), gotKeys)
	}
	if len(gotKeys) != recallNearestCount {
		t.Errorf("expected exactly %d nearest keys (6 role candidates available), got %d: %v", recallNearestCount, len(gotKeys), gotKeys)
	}
}

// TestRecall_ZeroMatchesNoRoleKeysOmitsNearestLine verifies that when the
// role has no keys at all, the zero-match header prints with no trailing
// "nearest:" line (nothing to list).
func TestRecall_ZeroMatchesNoRoleKeysOmitsNearestLine(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{"planner:other": "unrelated role"}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &recallKong{Role: "dri", Query: "anything"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := `[recall dri "anything": 0 matches]` + "\n"
	if got := stdout.String(); got != want {
		t.Errorf("expected header only, no nearest line; got:\n%q\nwant:\n%q", got, want)
	}
}

// TestRecall_EmptyQueryTokenizesToZeroTokens pins current behavior for
// `ateam recall <role> ""`: an empty query tokenizes to zero tokens, so
// every candidate scores 0 and nothing matches — the header reports 0
// matches and the nearest fallback (alphabetical-first role keys) still
// prints.
func TestRecall_EmptyQueryTokenizesToZeroTokens(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:a": "alpha", "dri:b": "bravo", "dri:c": "charlie",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &recallKong{Role: "dri", Query: ""}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, `[recall dri "": 0 matches]`) {
		t.Errorf("expected zero-match header; got:\n%s", out)
	}
	if !strings.Contains(out, "nearest: dri:a dri:b dri:c") {
		t.Errorf("expected nearest line listing all role keys; got:\n%s", out)
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
				"dri:hot:condensed": "hot body",
				"dri:fresh:new-tip": "fresh body",
				"dri:cold-stale":    "cold body — must not appear",
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

// TestLearnings_HotBlockPrecedesFreshBlock verifies the agent-teams-bbsz.23
// serve order: the whole hot block leads, followed by the whole fresh block —
// even when a fresh key would sort alphabetically ahead of a hot key under a
// flat sort.Strings across the union (here "fresh:aaa" < "hot:zzz"
// alphabetically, but hot must still print first).
func TestLearnings_HotBlockPrecedesFreshBlock(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:hot:zzz":   "hot body z",
				"dri:fresh:aaa": "fresh body a",
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
	posHot := strings.Index(out, "dri:hot:zzz")
	posFresh := strings.Index(out, "dri:fresh:aaa")
	if posHot < 0 || posFresh < 0 {
		t.Fatalf("expected both keys in output; got:\n%s", out)
	}
	if !(posHot < posFresh) {
		t.Errorf("expected hot block before fresh block despite alphabetical order (hot=%d, fresh=%d):\n%s", posHot, posFresh, out)
	}
}

// TestLearnings_HotAndFreshEachSortedWithinTheirBlock verifies that within
// the hot-first serve order, keys inside each tier are still sorted
// alphabetically among themselves.
func TestLearnings_HotAndFreshEachSortedWithinTheirBlock(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:hot:zzz":   "hot z",
				"dri:hot:aaa":   "hot a",
				"dri:fresh:zzz": "fresh z",
				"dri:fresh:aaa": "fresh a",
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
	pos := func(s string) int {
		p := strings.Index(out, s)
		if p < 0 {
			t.Fatalf("expected %q in output; got:\n%s", s, out)
		}
		return p
	}
	hotA, hotZ := pos("dri:hot:aaa"), pos("dri:hot:zzz")
	freshA, freshZ := pos("dri:fresh:aaa"), pos("dri:fresh:zzz")

	if !(hotA < hotZ) {
		t.Errorf("expected hot:aaa before hot:zzz within the hot block; got:\n%s", out)
	}
	if !(freshA < freshZ) {
		t.Errorf("expected fresh:aaa before fresh:zzz within the fresh block; got:\n%s", out)
	}
	if !(hotZ < freshA) {
		t.Errorf("expected the entire hot block before the entire fresh block; got:\n%s", out)
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

// ── learnings delivery trailer (agent-teams-bbsz.21) ───────────────────────────

// trailerRe parses a "[learnings <role>: N entries, M chars, hot X fresh Y]"
// line into its numeric fields for assertions below.
var trailerRe = regexp.MustCompile(`^\[learnings (\S+): (\d+) entries, (\d+) chars, hot (\d+) fresh (\d+)\]\n$`)

// headerRe parses the leading "[learnings <role>: N entries, M chars, hot X
// fresh Y — read in full; ...]" advisory line (agent-teams-bbsz.33) into the
// same numeric fields as trailerRe, so a test can assert the two carry
// identical stats.
var headerRe = regexp.MustCompile(`^\[learnings (\S+): (\d+) entries, (\d+) chars, hot (\d+) fresh (\d+) — read in full; do NOT pipe through head/tail or truncate; output ends at the matching trailer line\]\n$`)

// stripHeaderLine removes the leading advisory header line (through its
// trailing newline) from out, returning the remainder. Used by tests that
// need to isolate the entries payload from the header that now precedes it.
func stripHeaderLine(out string) string {
	nl := strings.Index(out, "\n")
	if nl < 0 {
		return out
	}
	return out[nl+1:]
}

// TestLearnings_TrailerReportsCountsAndPayloadByteLength verifies the trailer
// line's shape and that its "N entries"/"hot X fresh Y" figures match the
// served set, and — most importantly — that "M chars" equals the exact byte
// length of whatever was printed above the trailer (not a value recomputed by
// re-implementing the same formatting logic the production code used, so this
// is a real check of the printed bytes rather than a tautology).
func TestLearnings_TrailerReportsCountsAndPayloadByteLength(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"implementer:hot:a":   "AAA",
				"implementer:fresh:b": "BBB",
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
	body := stripHeaderLine(out)
	idx := strings.LastIndex(body, "[learnings ")
	if idx < 0 {
		t.Fatalf("no trailer line found in output:\n%q", out)
	}
	payload, trailer := body[:idx], body[idx:]

	m := trailerRe.FindStringSubmatch(trailer)
	if m == nil {
		t.Fatalf("trailer line did not match expected shape; got:\n%q", trailer)
	}
	role, nEntries, mChars, hotX, freshY := m[1], m[2], m[3], m[4], m[5]

	if role != "implementer" {
		t.Errorf("role = %q, want %q", role, "implementer")
	}
	if nEntries != "2" {
		t.Errorf("entries = %q, want %q (1 hot + 1 fresh)", nEntries, "2")
	}
	if hotX != "1" {
		t.Errorf("hot count = %q, want %q", hotX, "1")
	}
	if freshY != "1" {
		t.Errorf("fresh count = %q, want %q", freshY, "1")
	}
	// "chars" is defined as len() of the payload string — bytes, not runes —
	// verified directly against the printed prefix so the check does not
	// depend on the exact per-entry formatting.
	if wantChars := fmt.Sprintf("%d", len(payload)); mChars != wantChars {
		t.Errorf("chars = %q, want %q (len of printed payload %q)", mChars, wantChars, payload)
	}
}

// TestLearnings_TrailerCharsCountsBytesNotRunes documents and pins the "M
// chars" choice: it is len(payload) — a byte count — not a rune count. A body
// containing a multi-byte UTF-8 rune makes the two diverge, so this proves
// which one the trailer actually reports.
func TestLearnings_TrailerCharsCountsBytesNotRunes(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			// "é" is 2 bytes but 1 rune, so byte length and rune length of the
			// payload provably differ.
			*m = map[string]any{"implementer:hot:x": "café"}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsKong{Role: "implementer"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	body := stripHeaderLine(out)
	idx := strings.LastIndex(body, "[learnings ")
	if idx < 0 {
		t.Fatalf("no trailer line found in output:\n%q", out)
	}
	payload, trailer := body[:idx], body[idx:]

	byteLen := len(payload)
	runeLen := utf8.RuneCountInString(payload)
	if byteLen == runeLen {
		t.Fatalf("fixture does not exercise a byte/rune divergence (both = %d); fixture is broken", byteLen)
	}

	m := trailerRe.FindStringSubmatch(trailer)
	if m == nil {
		t.Fatalf("trailer line did not match expected shape; got:\n%q", trailer)
	}
	if gotChars := m[3]; gotChars != fmt.Sprintf("%d", byteLen) {
		t.Errorf("chars = %s, want byte length %d (rune length would have been %d)", gotChars, byteLen, runeLen)
	}
}

// TestLearnings_TrailerZeroTierFallbackReportsZeroHotFresh verifies that in
// the zero-tier fallback path (no hot/fresh keys, all role: keys served) the
// trailer still fires with the served count, and hot/fresh both read 0 since
// none of the served keys carry either prefix.
func TestLearnings_TrailerZeroTierFallbackReportsZeroHotFresh(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"planner:alpha": "body alpha",
				"planner:beta":  "body beta",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsKong{Role: "planner"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	trailer := stdout.String()
	trailer = trailer[strings.LastIndex(trailer, "[learnings "):]
	m := trailerRe.FindStringSubmatch(trailer)
	if m == nil {
		t.Fatalf("trailer line did not match expected shape; got:\n%q", trailer)
	}
	if m[2] != "2" {
		t.Errorf("entries = %s, want 2 (zero-tier fallback serves both keys)", m[2])
	}
	if m[4] != "0" || m[5] != "0" {
		t.Errorf("hot/fresh = %s/%s, want 0/0 in zero-tier fallback", m[4], m[5])
	}
}

// ── learnings leading advisory header (agent-teams-bbsz.33) ───────────────────

// TestLearnings_HeaderIsFirstLineTrailerIsLastLine verifies the non-empty
// output shape: the advisory header line comes first, the unchanged trailer
// line comes last, and the two carry identical stats — so a reading session
// can detect truncation (e.g. from piping through `head`) by checking the
// trailer is present and matches the header.
func TestLearnings_HeaderIsFirstLineTrailerIsLastLine(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"implementer:hot:a":   "AAA",
				"implementer:fresh:b": "BBB",
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
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least a header and trailer line; got:\n%q", out)
	}
	firstLine := lines[0] + "\n"
	lastLine := lines[len(lines)-1] + "\n"

	headerMatch := headerRe.FindStringSubmatch(firstLine)
	if headerMatch == nil {
		t.Fatalf("first line is not the advisory header; got:\n%q", firstLine)
	}
	trailerMatch := trailerRe.FindStringSubmatch(lastLine)
	if trailerMatch == nil {
		t.Fatalf("last line is not the trailer; got:\n%q", lastLine)
	}

	// Same stats (role, entries, chars, hot, fresh) on both ends.
	for i, label := range []string{"role", "entries", "chars", "hot", "fresh"} {
		if headerMatch[i+1] != trailerMatch[i+1] {
			t.Errorf("%s mismatch: header=%q trailer=%q", label, headerMatch[i+1], trailerMatch[i+1])
		}
	}

	// The header must not itself be mistaken for the trailer (they must be
	// distinct lines, not the same line printed twice).
	if firstLine == lastLine {
		t.Fatalf("header and trailer must be distinct lines; got the same line twice:\n%q", firstLine)
	}

	// Advisory wording present.
	for _, phrase := range []string{"read in full", "do NOT pipe through head/tail or truncate", "output ends at the matching trailer line"} {
		if !strings.Contains(firstLine, phrase) {
			t.Errorf("expected header to contain %q; got:\n%q", phrase, firstLine)
		}
	}
}

// TestLearnings_EmptyCaseHasNoHeader verifies the EMPTY case is unchanged: a
// single "[learnings <role>: EMPTY]" line with no leading advisory header
// (the dashboard sentinel contract depends on this exact shape).
func TestLearnings_EmptyCaseHasNoHeader(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{"planner:other": "unrelated role"}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &learningsKong{Role: "implementer"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stdout.String(); got != "[learnings implementer: EMPTY]\n" {
		t.Errorf("expected unchanged single-line EMPTY marker; got:\n%q", got)
	}
}
