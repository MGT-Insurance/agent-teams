package verbs

import (
	"fmt"
	"strings"
	"testing"
)

// TestFreshDrain_DrainsFreshToCold verifies that fresh: keys are promoted to
// cold keys (role:<slug>) and then removed.
func TestFreshDrain_DrainsFreshToCold(t *testing.T) {
	var calls [][]string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"implementer:fresh:tip-a": "body a",
				"implementer:fresh:tip-b": "body b",
				"implementer:old-cold":    "should not be touched",
			}
			return nil
		},
		runFn: func(args ...string) (string, error) {
			cp := make([]string, len(args))
			copy(cp, args)
			calls = append(calls, cp)
			return "", nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &freshDrainKong{Role: "implementer"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect 4 bd calls: remember+forget for each fresh key (2 keys × 2 calls).
	if len(calls) != 4 {
		t.Fatalf("expected 4 bd calls, got %d: %v", len(calls), calls)
	}

	// Calls are sorted by key, so tip-a comes before tip-b.
	assertCall(t, calls[0], "remember", "--key=implementer:tip-a", "body a")
	assertCall(t, calls[1], "forget", "implementer:fresh:tip-a")
	assertCall(t, calls[2], "remember", "--key=implementer:tip-b", "body b")
	assertCall(t, calls[3], "forget", "implementer:fresh:tip-b")

	// Output summary.
	out := stdout.String()
	if !strings.Contains(out, "drained 2") {
		t.Errorf("expected 'drained 2' in output; got: %q", out)
	}
}

// TestFreshDrain_CollisionOverwritesCold verifies that an existing cold entry
// is overwritten unconditionally (fresh = newer).
func TestFreshDrain_CollisionOverwritesCold(t *testing.T) {
	var calls [][]string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"dri:fresh:tip": "new fresh body",
				"dri:tip":       "old cold body",
			}
			return nil
		},
		runFn: func(args ...string) (string, error) {
			cp := make([]string, len(args))
			copy(cp, args)
			calls = append(calls, cp)
			return "", nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &freshDrainKong{Role: "dri"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should write new body to cold key.
	if len(calls) < 1 {
		t.Fatalf("expected bd calls, got 0")
	}
	assertCall(t, calls[0], "remember", "--key=dri:tip", "new fresh body")

	out := stdout.String()
	if !strings.Contains(out, "drained 1") {
		t.Errorf("expected 'drained 1' in output; got: %q", out)
	}
}

// TestFreshDrain_IdempotentNoop verifies that calling fresh-drain when no
// fresh keys exist is a clean no-op (zero bd writes, count = 0).
func TestFreshDrain_IdempotentNoop(t *testing.T) {
	var calls [][]string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				"planner:some-cold": "just a cold key",
			}
			return nil
		},
		runFn: func(args ...string) (string, error) {
			cp := make([]string, len(args))
			copy(cp, args)
			calls = append(calls, cp)
			return "", nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &freshDrainKong{Role: "planner"}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(calls) != 0 {
		t.Errorf("expected 0 bd write calls for no-op; got %d: %v", len(calls), calls)
	}

	out := stdout.String()
	if !strings.Contains(out, "drained 0") {
		t.Errorf("expected 'drained 0' in output; got: %q", out)
	}
}

// TestFreshDrain_MissingRole verifies usage error when role arg is missing.
func TestFreshDrain_MissingRole(t *testing.T) {
	err := (&freshDrainKong{}).Validate()
	if err == nil {
		t.Fatal("expected usage error, got nil")
	}
	if !strings.Contains(err.Error(), "missing <role>") {
		t.Errorf("expected 'missing <role>' in error; got: %v", err)
	}
}

// TestFreshDrain_NilContextReturnsError verifies nil context returns an error.
func TestFreshDrain_NilContextReturnsError(t *testing.T) {
	err := (&freshDrainKong{Role: "implementer"}).Run(nil)
	if err == nil {
		t.Fatal("expected error for nil context; got nil")
	}
}

// TestFreshDrain_BDErrorPropagates verifies bd failures are returned as errors.
func TestFreshDrain_BDErrorPropagates(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			return fmt.Errorf("bd memories: simulated failure")
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	err := (&freshDrainKong{Role: "implementer"}).Run(ctx)
	if err == nil {
		t.Fatal("expected error from bd failure; got nil")
	}
}

// assertCall checks that a recorded bd call matches expected verb and args.
func assertCall(t *testing.T, got []string, wantVerb string, wantArgs ...string) {
	t.Helper()
	if len(got) == 0 {
		t.Errorf("assertCall: empty call, want verb=%q", wantVerb)
		return
	}
	if got[0] != wantVerb {
		t.Errorf("call verb = %q, want %q (full call: %v)", got[0], wantVerb, got)
	}
	for i, want := range wantArgs {
		idx := i + 1
		if idx >= len(got) {
			t.Errorf("call arg[%d] missing, want %q (full call: %v)", idx, want, got)
			continue
		}
		if got[idx] != want {
			t.Errorf("call arg[%d] = %q, want %q (full call: %v)", idx, got[idx], want, got)
		}
	}
}
