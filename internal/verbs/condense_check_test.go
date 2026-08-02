package verbs

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestCondenseCheck_ZeroWritesOccur mirrors
// write_test.go:TestCondense_ZeroWritesOccur — condense-check must never
// issue a remember/forget call (SEAM 1: read-only, zero writes).
func TestCondenseCheck_ZeroWritesOccur(t *testing.T) {
	var calls []string
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			calls = append(calls, args[0])
			return "", nil
		},
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:hot:foo":   "hot body",
				"dri:fresh:bar": strings.Repeat("x", 15000),
			}
			return nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	if err := (&condenseCheckKong{}).Run(ctx); err != nil {
		t.Fatalf("condense-check.Run: %v", err)
	}
	for _, c := range calls {
		if c == "remember" || c == "forget" {
			t.Errorf("condense-check issued a write call %q — must be zero-write", c)
		}
	}
}

// TestCondenseCheck_FireWhenFreshAloneExceedsThreshold verifies the sole
// trigger: fresh-alone approx tokens > condenseFreshThresholdTokens fires,
// regardless of how small hot∪fresh is.
func TestCondenseCheck_FireWhenFreshAloneExceedsThreshold(t *testing.T) {
	// 15000 bytes / 3 = 5000 approx tokens > 4000 threshold.
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:fresh:bar": strings.Repeat("x", 15000),
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &condenseCheckKong{Role: "dri", JSON: true}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("condense-check.Run: %v", err)
	}
	results := decodeCondenseCheckResults(t, stdout)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Verdict != "FIRE" {
		t.Errorf("verdict = %q, want FIRE (fresh_approx_tokens=%d)", r.Verdict, r.FreshApproxTokens)
	}
	if r.FreshApproxTokens != 5000 {
		t.Errorf("fresh_approx_tokens = %d, want 5000", r.FreshApproxTokens)
	}
	if r.Reason == "" {
		t.Error("reason must not be empty on FIRE")
	}
}

// TestCondenseCheck_SkipUnderThreshold verifies a role well under threshold
// (fresh small, hot large) skips — the removed hot∪fresh ceiling must NOT be
// a trigger (SEAM 2 round-6 amendment).
func TestCondenseCheck_SkipUnderThreshold(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				// Hot alone is huge (would have tripped the old 8000
				// hot∪fresh ceiling) but fresh alone is tiny — must SKIP.
				"dri:hot:big":    strings.Repeat("h", 30000),
				"dri:fresh:tiny": "small fresh note",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &condenseCheckKong{Role: "dri", JSON: true}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("condense-check.Run: %v", err)
	}
	results := decodeCondenseCheckResults(t, stdout)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Verdict != "SKIP" {
		t.Errorf("verdict = %q, want SKIP (huge hot must not trip the removed ceiling); fresh_approx_tokens=%d hot_approx_tokens=%d",
			r.Verdict, r.FreshApproxTokens, r.HotApproxTokens)
	}
}

// TestCondenseCheck_AllRolesSkipsUserAndApplied verifies that omitting the
// role arg enumerates roles the same way `ateam roles` does, excluding the
// "user" and "applied" namespaces (SEAM 1).
func TestCondenseCheck_AllRolesSkipsUserAndApplied(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:fresh:a":      "body",
				"planner:hot:b":    "body",
				"user:some-pref":   "body",
				"applied:dri:some": `{"count":1,"last_applied":"x"}`,
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &condenseCheckKong{JSON: true}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("condense-check.Run: %v", err)
	}
	results := decodeCondenseCheckResults(t, stdout)
	var roles []string
	for _, r := range results {
		roles = append(roles, r.Role)
	}
	want := []string{"dri", "planner"}
	if len(roles) != len(want) {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	for i, w := range want {
		if roles[i] != w {
			t.Errorf("roles[%d] = %q, want %q", i, roles[i], w)
		}
	}
}

// TestCondenseCheck_JSONFieldNames locks the exact JSON field names frozen by
// contract agent-teams-0yd3.1 SEAM 1.
func TestCondenseCheck_JSONFieldNames(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{"dri:fresh:a": "hello"}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &condenseCheckKong{Role: "dri", JSON: true}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("condense-check.Run: %v", err)
	}
	for _, field := range []string{
		`"role"`, `"learnings_bytes"`, `"approx_tokens"`, `"fresh_bytes"`,
		`"fresh_approx_tokens"`, `"hot_approx_tokens"`, `"verdict"`, `"reason"`,
	} {
		if !strings.Contains(stdout.String(), field) {
			t.Errorf("output missing frozen field %s; got: %s", field, stdout.String())
		}
	}
}

// TestCondenseCheck_TextOutputOneLinePerRole verifies the bare (non-JSON)
// path emits exactly one line per role plus a header line.
func TestCondenseCheck_TextOutputOneLinePerRole(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:fresh:a":   "body",
				"planner:hot:b": "body",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &condenseCheckKong{}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("condense-check.Run: %v", err)
	}
	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	// 1 header + 2 roles.
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header + 2 roles), got %d: %v", len(lines), lines)
	}
}

// TestCondenseCheck_NilContextErrors verifies a nil context returns an error
// rather than panicking.
func TestCondenseCheck_NilContextErrors(t *testing.T) {
	cmd := &condenseCheckKong{}
	if err := cmd.Run(nil); err == nil {
		t.Error("expected error for nil context, got nil")
	}
}

// decodeCondenseCheckResults is a test helper decoding --json stdout output.
func decodeCondenseCheckResults(t *testing.T, stdout *bytes.Buffer) []condenseCheckRoleResult {
	t.Helper()
	var results []condenseCheckRoleResult
	if err := json.NewDecoder(stdout).Decode(&results); err != nil {
		t.Fatalf("decode condense-check JSON: %v (raw: %q)", err, stdout.String())
	}
	return results
}

// TestCondenseCheck_LearningsBytesEqualsRealLearningsPayload pins
// condense_check.go's re-derived learningsBytes to the payload
// `ateam learnings <role>` actually prints.
//
// The two live in file-disjoint tracks and must stay in sync by hand — the
// comment above learningsBytes says so, and until now nothing enforced it.
// This compares against the REAL printed bytes (len of what runLearnings put
// on stdout between its header and trailer), not against the "N chars" figure
// runLearnings self-reports, so a formula that drifted in BOTH places would
// still be caught here.
//
// The fixture deliberately spans both tiers with more than one entry each, so
// the blank-line separators and the per-key line overhead are exercised rather
// than cancelling out, and includes a multi-byte rune so a byte/rune mix-up
// cannot pass.
func TestCondenseCheck_LearningsBytesEqualsRealLearningsPayload(t *testing.T) {
	fixture := map[string]any{
		"dri:hot:alpha":              "first hot body",
		"dri:hot:beta-longer-slug":   "second hot body with café — multi-byte",
		"dri:hot:gamma":              strings.Repeat("g", 300),
		"dri:fresh:delta":            "a fresh body",
		"dri:fresh:epsilon-long-key": strings.Repeat("e", 200),
		// Noise that must NOT be counted: another role, and a non-string value.
		"tester:hot:ignored": "other role",
		"schema_version":     1,
	}
	newFakeBD := func() *fakeBD {
		return &fakeBD{
			runJSONFn: func(dst any, args ...string) error {
				m := dst.(*map[string]any)
				*m = fixture
				return nil
			},
		}
	}

	// What `ateam learnings dri` really prints.
	lctx, lstdout, _ := makeCtx(newFakeBD(), t.TempDir())
	if err := (&learningsKong{Role: "dri"}).Run(lctx); err != nil {
		t.Fatalf("learnings.Run: %v", err)
	}
	out := lstdout.String()
	body := stripHeaderLine(out)
	idx := strings.LastIndex(body, "[learnings ")
	if idx < 0 {
		t.Fatalf("no trailer line found in learnings output:\n%q", out)
	}
	payload := body[:idx]
	wantBytes := len(payload)
	if wantBytes == 0 {
		t.Fatal("fixture produced an empty payload; fixture is broken")
	}

	// What condense-check computes for the same store.
	cctx, cstdout, _ := makeCtx(newFakeBD(), t.TempDir())
	if err := (&condenseCheckKong{Role: "dri", JSON: true}).Run(cctx); err != nil {
		t.Fatalf("condense-check.Run: %v", err)
	}
	results := decodeCondenseCheckResults(t, cstdout)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if got := results[0].LearningsBytes; got != wantBytes {
		t.Errorf("condense-check learnings_bytes = %d, but `learnings dri` printed a %d-byte payload; "+
			"the two byte models have diverged (condense_check.go learningsBytes vs query.go runLearnings payload builder)",
			got, wantBytes)
	}

	// The header and trailer both self-report the same size; a mismatch with
	// the real payload would mean runLearnings' own reporting drifted too.
	for _, m := range []struct {
		what string
		re   *regexp.Regexp
		line string
	}{
		{"header", headerRe, out[:strings.Index(out, "\n")+1]},
		{"trailer", trailerRe, body[idx:]},
	} {
		sub := m.re.FindStringSubmatch(m.line)
		if sub == nil {
			t.Fatalf("%s line did not match expected shape; got:\n%q", m.what, m.line)
		}
		if sub[3] != strconv.Itoa(wantBytes) {
			t.Errorf("%s reports %s chars, want %d", m.what, sub[3], wantBytes)
		}
	}
}
