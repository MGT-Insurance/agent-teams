package verbs

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestPrime_OnlyUserKeys verifies only user: prefixed keys appear in output,
// and non-user keys (dri:, implementer:, schema_version) are excluded.
func TestPrime_OnlyUserKeys(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"user:terse":     "lead with the answer",
				"dri:approach":   "investigate first",
				"schema_version": "1",
				"implementer:x":  "some note",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &primeCmd{}

	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "**terse**") {
		t.Errorf("expected user:terse slug in output; got:\n%s", out)
	}
	if strings.Contains(out, "dri:") {
		t.Errorf("dri: key must not appear in output; got:\n%s", out)
	}
	if strings.Contains(out, "schema_version") {
		t.Errorf("schema_version must not appear in output; got:\n%s", out)
	}
	if strings.Contains(out, "implementer:") {
		t.Errorf("implementer: key must not appear in output; got:\n%s", out)
	}
}

// TestPrime_SchemaVersionNeverLeaks asserts schema_version is always excluded.
func TestPrime_SchemaVersionNeverLeaks(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"schema_version": 1, // int, like real bd output — must never appear
				"user:pref":      "some value",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &primeCmd{}

	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(stdout.String(), "schema_version") {
		t.Errorf("schema_version leaked into output:\n%s", stdout.String())
	}
}

// TestPrime_EmptyUserSet verifies empty stdout and nil error when no user: keys exist.
func TestPrime_EmptyUserSet(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"schema_version": "1",
				"dri:something":  "value",
			}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &primeCmd{}

	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("expected nil error for empty user set; got: %v", err)
	}
	if stdout.Len() > 0 {
		t.Errorf("expected empty stdout for zero user: memories; got:\n%s", stdout.String())
	}
}

// TestPrime_Cap12_KeySortDeterminism verifies that >12 user: memories are capped
// at exactly 12, and the result is key-sorted.
func TestPrime_Cap12_KeySortDeterminism(t *testing.T) {
	// Build 15 user: keys.
	memories := map[string]any{}
	for i := 0; i < 15; i++ {
		key := fmt.Sprintf("user:pref-%02d", i)
		memories[key] = fmt.Sprintf("value for pref %d", i)
	}

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = memories
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &primeCmd{}

	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()

	// Count bullet lines.
	var bullets int
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "- **") {
			bullets++
		}
	}
	if bullets != 12 {
		t.Errorf("expected exactly 12 bullets; got %d:\n%s", bullets, out)
	}

	// First 12 keys sorted: pref-00 .. pref-11.
	for i := 0; i < 12; i++ {
		slug := fmt.Sprintf("pref-%02d", i)
		if !strings.Contains(out, "**"+slug+"**") {
			t.Errorf("expected slug %q in output; got:\n%s", slug, out)
		}
	}

	// Key 12..14 must NOT appear.
	for i := 12; i < 15; i++ {
		slug := fmt.Sprintf("pref-%02d", i)
		if strings.Contains(out, "**"+slug+"**") {
			t.Errorf("slug %q must be excluded (cap 12); got:\n%s", slug, out)
		}
	}
}

// TestPrime_Truncation verifies a body longer than 300 runes is truncated
// with an ellipsis, and newlines are collapsed to spaces.
func TestPrime_Truncation(t *testing.T) {
	// Build a body > 300 runes with embedded newlines.
	longBody := strings.Repeat("a", 150) + "\n" + strings.Repeat("b", 200)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{"user:long": longBody}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &primeCmd{}

	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()

	// No raw newlines in the body portion (header newline is fine).
	lines := strings.SplitN(out, "\n", 3)
	// Line 0 is the header, line 1 is the bullet.
	if len(lines) < 2 {
		t.Fatalf("unexpected output shape:\n%s", out)
	}
	bulletLine := lines[1]

	// Must end with ellipsis.
	if !strings.HasSuffix(bulletLine, "…") {
		t.Errorf("truncated body must end with ellipsis; got: %q", bulletLine)
	}

	// Body portion: text after "- **long**: "
	bodyStart := strings.Index(bulletLine, ": ")
	if bodyStart < 0 {
		t.Fatalf("no ': ' separator in bullet line: %q", bulletLine)
	}
	bodyPart := bulletLine[bodyStart+2:]

	// Rune count of body must be 301 (300 content chars + 1 ellipsis rune).
	runeLen := utf8.RuneCountInString(bodyPart)
	if runeLen != 301 {
		t.Errorf("body part rune count = %d, want 301 (300 + ellipsis); body: %q", runeLen, bodyPart)
	}

	// No raw newlines in the bullet line.
	if strings.Contains(bulletLine, "\n") {
		t.Errorf("newlines must be collapsed in output; got: %q", bulletLine)
	}
}

// TestPrime_ShortBodyNoEllipsis verifies bodies at or under 300 runes are not truncated.
func TestPrime_ShortBodyNoEllipsis(t *testing.T) {
	exactBody := strings.Repeat("x", 300)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{"user:exact": exactBody}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &primeCmd{}

	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if strings.Contains(out, "…") {
		t.Errorf("300-rune body must not be truncated; got:\n%s", out)
	}
}

// TestPrime_SlugRendering verifies user: prefix is stripped from the slug.
func TestPrime_SlugRendering(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{"user:foo-bar": "some preference"}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &primeCmd{}

	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "**foo-bar**") {
		t.Errorf("expected slug **foo-bar**; got:\n%s", out)
	}
	if strings.Contains(out, "user:") {
		t.Errorf("user: prefix must be stripped from slug; got:\n%s", out)
	}
}

// TestPrime_BDErrorPropagates verifies bd failures are returned as non-nil errors.
func TestPrime_BDErrorPropagates(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			return fmt.Errorf("bd memories: simulated failure")
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	cmd := &primeCmd{}

	err := cmd.Run(ctx, nil)
	if err == nil {
		t.Fatal("expected error from bd failure; got nil")
	}
	if !strings.Contains(err.Error(), "simulated failure") {
		t.Errorf("error message should contain 'simulated failure'; got: %v", err)
	}
}

// TestPrime_NilContext verifies nil context returns an error.
func TestPrime_NilContext(t *testing.T) {
	cmd := &primeCmd{}
	err := cmd.Run(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil context; got nil")
	}
}

// TestPrime_OutputHeader verifies the header line is present when memories exist.
func TestPrime_OutputHeader(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{"user:verbosity": "terse"}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &primeCmd{}

	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.HasPrefix(out, "## agent-teams: cross-project user preferences\n") {
		t.Errorf("output must start with the header line; got:\n%s", out)
	}
}
