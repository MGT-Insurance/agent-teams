// tie_session_test.go: core-path tests for `ateam tie-session`
// (agent-teams-zalv.3, at-ps11) — the writer half of the session-to-
// initiative tie. Not exhaustive; see the contract bead (agent-teams-zalv.1)
// for the full schema/API this implements.
package verbs

import (
	"os"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// noCallBD panics if either bd method is invoked — used to assert a silent
// no-op path makes zero bd calls.
func noCallBD(t *testing.T) *fakeBD {
	t.Helper()
	return &fakeBD{
		runFn: func(args ...string) (string, error) {
			t.Fatalf("unexpected bd call (expected silent no-op): %v", args)
			return "", nil
		},
		runJSONFn: func(dst any, args ...string) error {
			t.Fatalf("unexpected bd call (expected silent no-op): %v", args)
			return nil
		},
	}
}

// ── arg+flag / env fallback / cwd fallback ───────────────────────────────────

func TestTieSession_ArgAndFlagGiven(t *testing.T) {
	var capturedBody string
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			switch args[0] {
			case "show":
				return issueJSON(bd.Issue{ID: "at-target", Description: "problem: x\n"}), nil
			case "update":
				for _, a := range args {
					if strings.HasPrefix(a, "--body-file=") {
						data, err := readFileT(t, strings.TrimPrefix(a, "--body-file="))
						if err != nil {
							t.Fatalf("read body file: %v", err)
						}
						capturedBody = data
					}
				}
				return "", nil
			}
			return "", nil
		},
		runJSONFn: func(dst any, args ...string) error {
			issues, ok := dst.(*[]bd.Issue)
			if !ok {
				return nil
			}
			*issues = []bd.Issue{{ID: "at-target", Status: "open", Description: "problem: x\n"}}
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())

	cmd := &tieSessionKong{InitiativeID: "at-target", SessionID: "sess-explicit"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(capturedBody, "session: sess-explicit") {
		t.Errorf("expected session line written, body=%q", capturedBody)
	}
	if !strings.Contains(stdout.String(), "at-target") {
		t.Errorf("expected confirmation line on stdout, got %q", stdout.String())
	}
}

func TestTieSession_FallsBackToEnvVar(t *testing.T) {
	t.Setenv(sessionIDEnvVar, "sess-from-env")

	var updateCalled bool
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			switch args[0] {
			case "show":
				return issueJSON(bd.Issue{ID: "at-target", Description: "problem: x\n"}), nil
			case "update":
				updateCalled = true
				return "", nil
			}
			return "", nil
		},
		runJSONFn: func(dst any, args ...string) error {
			issues, ok := dst.(*[]bd.Issue)
			if !ok {
				return nil
			}
			*issues = []bd.Issue{{ID: "at-target", Status: "open", Description: "problem: x\n"}}
			return nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	cmd := &tieSessionKong{InitiativeID: "at-target"} // no --session-id
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updateCalled {
		t.Error("expected bd update to be called using the env-var session id")
	}
}

func TestTieSession_FallsBackToCwdResolution(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	var updateCalled bool
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			switch args[0] {
			case "show":
				return issueJSON(bd.Issue{ID: "at-cwd-match", Description: "worktree: " + cwd + "\n"}), nil
			case "update":
				updateCalled = true
				return "", nil
			}
			return "", nil
		},
		runJSONFn: func(dst any, args ...string) error {
			issues, ok := dst.(*[]bd.Issue)
			if !ok {
				return nil
			}
			*issues = []bd.Issue{{ID: "at-cwd-match", Status: "open", Description: "worktree: " + cwd + "\n"}}
			return nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	cmd := &tieSessionKong{SessionID: "sess-cwd"} // no initiative-id arg
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updateCalled {
		t.Error("expected bd update to be called after resolving the initiative from cwd")
	}
}

// ── silent no-op cases ───────────────────────────────────────────────────────

func TestTieSession_NoOp_NoSessionID(t *testing.T) {
	t.Setenv(sessionIDEnvVar, "")
	fbd := noCallBD(t)
	ctx, stdout, stderr := makeCtx(fbd, t.TempDir())

	cmd := &tieSessionKong{InitiativeID: "at-target"} // no flag, empty env
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("expected clean no-op, got error: %v", err)
	}
	if stdout.String() != "" || stderr.String() != "" {
		t.Errorf("expected no output, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestTieSession_NoOp_UnknownSentinel(t *testing.T) {
	fbd := noCallBD(t)
	ctx, stdout, stderr := makeCtx(fbd, t.TempDir())

	// "unknown" is what session-start-inbox.sh (and its sibling hook scripts)
	// pass as --session-id when stdin carries no .session_id; it must be
	// treated exactly like no session id, never written to the registry.
	cmd := &tieSessionKong{InitiativeID: "at-target", SessionID: "unknown"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("expected clean no-op, got error: %v", err)
	}
	if stdout.String() != "" || stderr.String() != "" {
		t.Errorf("expected no output, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestTieSession_NoOp_NoOpenInitiativeForCwd(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			issues, ok := dst.(*[]bd.Issue)
			if !ok {
				return nil
			}
			*issues = []bd.Issue{{ID: "at-other", Status: "open", Description: "worktree: /nowhere/else\n"}}
			return nil
		},
		runFn: func(args ...string) (string, error) {
			t.Fatalf("unexpected bd call (expected no-op before any show/update): %v", args)
			return "", nil
		},
	}
	ctx, stdout, stderr := makeCtx(fbd, t.TempDir())

	cmd := &tieSessionKong{SessionID: "sess-unmatched"} // no initiative-id arg
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("expected clean no-op, got error: %v", err)
	}
	if stdout.String() != "" || stderr.String() != "" {
		t.Errorf("expected no output, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestTieSession_NoOp_StewardCwd(t *testing.T) {
	home := t.TempDir()
	fbd := noCallBD(t)
	ctx, stdout, stderr := makeCtx(fbd, home)

	sessionDir := StewardSessionDir(ctx)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(StewardSessionMarkerPath(ctx), []byte("steward\n"), 0o644); err != nil {
		t.Fatalf("WriteFile marker: %v", err)
	}

	origCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(sessionDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origCwd); err != nil {
			t.Fatalf("restore Chdir: %v", err)
		}
	})

	cmd := &tieSessionKong{SessionID: "sess-steward"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("expected clean no-op for Steward cwd, got error: %v", err)
	}
	if stdout.String() != "" || stderr.String() != "" {
		t.Errorf("expected no output, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

// ── idempotent re-run / cross-initiative warning ─────────────────────────────

func TestTieSession_IdempotentReRunIsNoOp(t *testing.T) {
	var updateCalled bool
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			switch args[0] {
			case "show":
				return issueJSON(bd.Issue{ID: "at-target", Description: "problem: x\nsession: sess-again\n"}), nil
			case "update":
				updateCalled = true
				return "", nil
			}
			return "", nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())

	cmd := &tieSessionKong{InitiativeID: "at-target", SessionID: "sess-again"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updateCalled {
		t.Error("expected no bd update call on idempotent re-run (respawn)")
	}
	if !strings.Contains(stdout.String(), "at-target") {
		t.Errorf("expected confirmation line even on idempotent no-op, got %q", stdout.String())
	}
}

func TestTieSession_TiedToOtherOpenInitiative_WarnsNoWrite(t *testing.T) {
	var updateCalled bool
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			switch args[0] {
			case "show":
				return issueJSON(bd.Issue{ID: "at-new", Description: "problem: y\n"}), nil
			case "update":
				updateCalled = true
				return "", nil
			}
			return "", nil
		},
		runJSONFn: func(dst any, args ...string) error {
			issues, ok := dst.(*[]bd.Issue)
			if !ok {
				return nil
			}
			*issues = []bd.Issue{
				{ID: "at-new", Status: "open", Description: "problem: y\n"},
				{ID: "at-other", Status: "open", Description: "problem: x\nsession: sess-conflict\n"},
			}
			return nil
		},
	}
	ctx, stdout, stderr := makeCtx(fbd, t.TempDir())

	cmd := &tieSessionKong{InitiativeID: "at-new", SessionID: "sess-conflict"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("expected warn-and-continue (exit 0), got error: %v", err)
	}
	if updateCalled {
		t.Error("expected no bd update call when tied to a different open initiative")
	}
	if !strings.Contains(stderr.String(), "at-other") {
		t.Errorf("expected warning naming the conflicting initiative on stderr, got %q", stderr.String())
	}
	if stdout.String() != "" {
		t.Errorf("expected no stdout confirmation on the conflict path, got %q", stdout.String())
	}
}

// ── kong wiring ───────────────────────────────────────────────────────────────

// TestTieSessionKong_ParsesOptionalArgAndFlag exercises the verb through the
// real kong parser (not direct struct construction, unlike the Run tests
// above) to confirm the positional initiative-id is genuinely optional and
// --session-id parses, matching the shape the SessionStart hook and any
// direct CLI invocation both rely on.
func TestTieSessionKong_ParsesOptionalArgAndFlag(t *testing.T) {
	p, err := cli.NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	RegisterTieSessionKong(p)

	kctx, err := p.Parse([]string{"tie-session", "at-explicit", "--session-id", "sess-1"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	target := kctx.Selected().Target
	if target.CanAddr() {
		target = target.Addr()
	}
	cmd, ok := target.Interface().(*tieSessionKong)
	if !ok {
		t.Fatalf("Selected().Target is not (addressable to) *tieSessionKong: %T", target.Interface())
	}
	if cmd.InitiativeID != "at-explicit" || cmd.SessionID != "sess-1" {
		t.Errorf("parsed = %+v, want InitiativeID=at-explicit SessionID=sess-1", cmd)
	}

	p2, err := cli.NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	RegisterTieSessionKong(p2)
	if _, err := p2.Parse([]string{"tie-session", "--session-id", "sess-2"}); err != nil {
		t.Fatalf("Parse with no positional arg (must be optional): %v", err)
	}
}
