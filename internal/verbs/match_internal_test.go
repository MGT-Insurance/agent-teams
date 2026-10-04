// match_internal_test.go covers resolveInitiativeBySession (match.go). Lives
// in package verbs (not verbs_test, like match_test.go) because that symbol
// is unexported — same reason canonicalpath_test.go does, for canonicalPath.
package verbs

import (
	"encoding/json"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
)

// TestResolveInitiativeBySession_EmptySessionID verifies the empty-id guard
// short-circuits before any bd call — mirroring
// TestAppendSessionID_RejectsInvalidSessionID's pattern for the sibling guard.
func TestResolveInitiativeBySession_EmptySessionID(t *testing.T) {
	f := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			t.Fatalf("unexpected bd call for empty sessionID: %v", args)
			return nil
		},
	}
	ctx, _, _ := makeCtx(f, t.TempDir())

	iss, ok, err := resolveInitiativeBySession(ctx, sessionruntime.Claude, "")
	if err != nil {
		t.Fatalf("resolveInitiativeBySession: unexpected error: %v", err)
	}
	if ok {
		t.Errorf("resolveInitiativeBySession: ok = true, want false for empty sessionID")
	}
	if iss.ID != "" {
		t.Errorf("resolveInitiativeBySession: iss.ID = %q, want empty", iss.ID)
	}
}

// TestResolveInitiativeBySession_ZeroMatch verifies no open initiative
// carrying sessionID returns (_, false, nil) rather than an error.
func TestResolveInitiativeBySession_ZeroMatch(t *testing.T) {
	f := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			return unmarshalIssues(dst, bd.Issue{ID: "at-other", Status: "open", Description: "session: sess-other\n"})
		},
	}
	ctx, _, _ := makeCtx(f, t.TempDir())

	_, ok, err := resolveInitiativeBySession(ctx, sessionruntime.Claude, "sess-missing")
	if err != nil {
		t.Fatalf("resolveInitiativeBySession: unexpected error: %v", err)
	}
	if ok {
		t.Errorf("resolveInitiativeBySession: ok = true, want false for zero matches")
	}
}

// TestResolveInitiativeBySession_OneMatch is the happy path: exactly one open
// initiative carries sessionID under the requested runtime.
func TestResolveInitiativeBySession_OneMatch(t *testing.T) {
	f := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			return unmarshalIssues(dst,
				bd.Issue{ID: "at-other", Status: "open", Description: "session: sess-other\n"},
				bd.Issue{ID: "at-mine", Status: "open", Description: "session: sess-mine\n"},
			)
		},
	}
	ctx, _, _ := makeCtx(f, t.TempDir())

	iss, ok, err := resolveInitiativeBySession(ctx, sessionruntime.Claude, "sess-mine")
	if err != nil {
		t.Fatalf("resolveInitiativeBySession: unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("resolveInitiativeBySession: ok = false, want true for a unique match")
	}
	if iss.ID != "at-mine" {
		t.Errorf("resolveInitiativeBySession: iss.ID = %q, want at-mine", iss.ID)
	}
}

// TestResolveInitiativeBySession_MoreThanOneMatchNeverGuesses covers the
// never-guess rule: two open initiatives both carrying sessionID under the
// same runtime must return (_, false, nil), not either one of them.
func TestResolveInitiativeBySession_MoreThanOneMatchNeverGuesses(t *testing.T) {
	f := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			return unmarshalIssues(dst,
				bd.Issue{ID: "at-a", Status: "open", Description: "session: sess-dup\n"},
				bd.Issue{ID: "at-b", Status: "open", Description: "session: sess-dup\n"},
			)
		},
	}
	ctx, _, _ := makeCtx(f, t.TempDir())

	_, ok, err := resolveInitiativeBySession(ctx, sessionruntime.Claude, "sess-dup")
	if err != nil {
		t.Fatalf("resolveInitiativeBySession: unexpected error: %v", err)
	}
	if ok {
		t.Errorf("resolveInitiativeBySession: ok = true, want false — must never guess between >1 match")
	}
}

// TestResolveInitiativeBySession_RuntimeMismatchExcluded verifies the same
// sessionID recorded on a DIFFERENT-runtime initiative is not returned — the
// {runtime, id} key means a Claude lookup must not resolve a Codex initiative
// carrying the same opaque id, and vice versa (the leak hazard this contract
// exists to guard against).
func TestResolveInitiativeBySession_RuntimeMismatchExcluded(t *testing.T) {
	f := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			return unmarshalIssues(dst,
				bd.Issue{ID: "at-codex", Status: "open", Description: "runtime: codex\nsession: same-opaque-id\n"},
			)
		},
	}
	ctx, _, _ := makeCtx(f, t.TempDir())

	_, ok, err := resolveInitiativeBySession(ctx, sessionruntime.Claude, "same-opaque-id")
	if err != nil {
		t.Fatalf("resolveInitiativeBySession: unexpected error: %v", err)
	}
	if ok {
		t.Errorf("resolveInitiativeBySession: ok = true, want false — Claude lookup must not match a Codex initiative's id")
	}
}

// unmarshalIssues marshals issues the way `bd list --json` does and decodes
// them into dst, for use as a fakeBD.runJSONFn body.
func unmarshalIssues(dst any, issues ...bd.Issue) error {
	raw, err := json.Marshal(issues)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}
