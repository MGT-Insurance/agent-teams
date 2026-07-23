// session_test.go: core-path tests for the session-line helpers
// (agent-teams-zalv.2 / at-ps11): sessionIDs, hasSessionLine, appendSessionID,
// matchSessionsForInitiative. Not exhaustive — see the contract bead
// (agent-teams-zalv.1) for the full schema/API this implements.
package verbs

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

// issueJSON marshals a single issue the way `bd show <id> --json` does
// (single-element array — see bd.ShowIssue).
func issueJSON(iss bd.Issue) string {
	out, err := json.Marshal([]bd.Issue{iss})
	if err != nil {
		panic(err)
	}
	return string(out)
}

// issuesJSON marshals a set of issues the way `bd list --json` does.
func issuesJSON(issues ...bd.Issue) string {
	out, err := json.Marshal(issues)
	if err != nil {
		panic(err)
	}
	return string(out)
}

// readFileT reads path, failing the test on error.
func readFileT(t *testing.T, path string) (string, error) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ── sessionIDs / hasSessionLine ──────────────────────────────────────────────

func TestSessionIDs_PreservesRegistrationOrder(t *testing.T) {
	desc := "problem: x\nsession: sess-1\nworktree: /a/b\nsession: sess-2\nsession: sess-3\n"
	got := sessionIDs(desc)
	want := []string{"sess-1", "sess-2", "sess-3"}
	if len(got) != len(want) {
		t.Fatalf("sessionIDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sessionIDs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSessionIDs_NoLines(t *testing.T) {
	if got := sessionIDs("problem: x\nworktree: /a/b\n"); got != nil {
		t.Errorf("sessionIDs = %v, want nil", got)
	}
}

func TestHasSessionLine(t *testing.T) {
	if hasSessionLine("problem: x\nworktree: /a/b\n") {
		t.Error("hasSessionLine: expected false for legacy description with no session: lines")
	}
	if !hasSessionLine("problem: x\nsession: sess-1\n") {
		t.Error("hasSessionLine: expected true when a session: line is present")
	}
}

// ── appendSessionID ──────────────────────────────────────────────────────────

func TestAppendSessionID_IdempotentReAppendIsNoOp(t *testing.T) {
	var updateCalled bool
	f := &fakeBD{
		runFn: func(args ...string) (string, error) {
			switch args[0] {
			case "show":
				return issueJSON(bd.Issue{ID: "at-init", Description: "problem: x\nsession: sess-1\n"}), nil
			case "update":
				updateCalled = true
				return "", nil
			}
			return "", nil
		},
	}
	ctx, _, _ := makeCtx(f, t.TempDir())

	if err := appendSessionID(ctx, "at-init", "sess-1"); err != nil {
		t.Fatalf("appendSessionID: unexpected error: %v", err)
	}
	if updateCalled {
		t.Error("appendSessionID: expected no bd update call for an already-tied session id (idempotent no-op)")
	}
}

func TestAppendSessionID_CrossOpenInitiativeGuardRejects(t *testing.T) {
	var updateCalled bool
	f := &fakeBD{
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
			return json.Unmarshal([]byte(issuesJSON(
				bd.Issue{ID: "at-new", Status: "open", Description: "problem: y\n"},
				bd.Issue{ID: "at-other", Status: "open", Description: "problem: x\nsession: sess-1\n"},
			)), dst)
		},
	}
	ctx, _, _ := makeCtx(f, t.TempDir())

	err := appendSessionID(ctx, "at-new", "sess-1")
	if err == nil {
		t.Fatal("appendSessionID: expected an error tying a session already on a different open initiative")
	}
	if !strings.Contains(err.Error(), "at-other") {
		t.Errorf("appendSessionID: error %q should name the conflicting initiative", err.Error())
	}
	if updateCalled {
		t.Error("appendSessionID: expected no bd update call when the cross-initiative guard rejects")
	}
}

func TestAppendSessionID_NewSessionAppendsLine(t *testing.T) {
	// The body file passed to `bd update --body-file` is read here, inside
	// the runFn callback, because appendSessionID removes it (via defer) as
	// soon as the call returns — reading it back after appendSessionID
	// returns would race the cleanup.
	var capturedBody string
	f := &fakeBD{
		runFn: func(args ...string) (string, error) {
			switch args[0] {
			case "show":
				return issueJSON(bd.Issue{ID: "at-init", Description: "problem: x\n"}), nil
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
			return json.Unmarshal([]byte(issuesJSON(bd.Issue{ID: "at-init", Status: "open", Description: "problem: x\n"})), dst)
		},
	}
	ctx, _, _ := makeCtx(f, t.TempDir())

	if err := appendSessionID(ctx, "at-init", "sess-new"); err != nil {
		t.Fatalf("appendSessionID: unexpected error: %v", err)
	}
	if capturedBody == "" {
		t.Fatal("appendSessionID: expected bd update --body-file to be called")
	}
	if !strings.Contains(capturedBody, "session: sess-new") {
		t.Errorf("appendSessionID: written description %q missing new session line", capturedBody)
	}
	if !strings.Contains(capturedBody, "problem: x") {
		t.Errorf("appendSessionID: written description %q dropped existing content", capturedBody)
	}
}

// ── matchSessionsForInitiative ───────────────────────────────────────────────

func TestMatchSessionsForInitiative_SessionSetPrimaryFirst(t *testing.T) {
	pidA, pidB := 1, 2
	iss := bd.Issue{Description: "session: sess-a\nsession: sess-b\n"}
	sessions := []agentSession{
		{SessionID: "sess-b", PID: &pidB},
		{SessionID: "sess-a", PID: &pidA},
	}
	got := matchSessionsForInitiative(sessions, iss)
	if len(got) != 2 {
		t.Fatalf("matchSessionsForInitiative: got %d sessions, want 2", len(got))
	}
	if got[0].SessionID != "sess-a" {
		t.Errorf("matchSessionsForInitiative: primary = %q, want sess-a (registration order)", got[0].SessionID)
	}
	if got[1].SessionID != "sess-b" {
		t.Errorf("matchSessionsForInitiative: got[1] = %q, want sess-b", got[1].SessionID)
	}
}

func TestMatchSessionsForInitiative_SessionSetExcludesDead(t *testing.T) {
	pidLive := 1
	iss := bd.Issue{Description: "session: sess-dead\nsession: sess-live\n"}
	sessions := []agentSession{
		{SessionID: "sess-dead", PID: nil},
		{SessionID: "sess-live", PID: &pidLive},
	}
	got := matchSessionsForInitiative(sessions, iss)
	if len(got) != 1 || got[0].SessionID != "sess-live" {
		t.Errorf("matchSessionsForInitiative: got %v, want only the live session", got)
	}
}

func TestMatchSessionsForInitiative_LegacyFallsBackToWorktree(t *testing.T) {
	pid := 9
	iss := bd.Issue{Description: "worktree: /repo-root/at-legacy\n"}
	sessions := []agentSession{
		{CWD: "/repo-root/at-legacy", Status: "busy", PID: &pid},
	}
	got := matchSessionsForInitiative(sessions, iss)
	if len(got) != 1 {
		t.Fatalf("matchSessionsForInitiative: got %d sessions, want 1 (legacy worktree match)", len(got))
	}
	if got[0].PID == nil || *got[0].PID != pid {
		t.Error("matchSessionsForInitiative: expected the worktree-matched session")
	}
}

func TestMatchSessionsForInitiative_LegacyNoMatch(t *testing.T) {
	iss := bd.Issue{Description: "worktree: /repo-root/at-legacy\n"}
	sessions := []agentSession{{CWD: "/other/path"}}
	got := matchSessionsForInitiative(sessions, iss)
	if got != nil {
		t.Errorf("matchSessionsForInitiative: got %v, want nil", got)
	}
}
