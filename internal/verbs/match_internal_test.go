// match_internal_test.go covers resolveInitiativeBySession (match.go). Lives
// in package verbs (not verbs_test, like match_test.go) because that symbol
// is unexported — same reason canonicalpath_test.go does, for canonicalPath.
package verbs

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
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

// ── one open list (agent-teams-c4i9.20.34.3) ────────────────────────────────

// listShowBD answers bd list with open and bd show with shown, and records
// every call's argv in order. A write (update) always succeeds.
func listShowBD(open []bd.Issue, shown *bd.Issue, calls *[]string) *fakeBD {
	return &fakeBD{
		runFn: func(args ...string) (string, error) {
			*calls = append(*calls, strings.Join(args, " "))
			if args[0] == "show" && shown != nil {
				return issueJSON(*shown), nil
			}
			return "", nil
		},
		runJSONFn: func(dst any, args ...string) error {
			*calls = append(*calls, strings.Join(args, " "))
			return unmarshalIssues(dst, open...)
		},
	}
}

func assertCalls(t *testing.T, calls []string, wantPrefixes ...string) {
	t.Helper()
	if len(calls) != len(wantPrefixes) {
		t.Fatalf("bd invocations = %d %q, want %d", len(calls), calls, len(wantPrefixes))
	}
	for i, want := range wantPrefixes {
		if !strings.HasPrefix(calls[i], want) {
			t.Errorf("call %d = %q, want prefix %q", i, calls[i], want)
		}
	}
}

const plainOpenList = "list --status=open --limit=0 --json"

// infraRows are rows --include-infra adds to a plain open list; each carries a
// session line and a worktree line, as a quoted message body or a copied
// description could.
func infraRows(worktree string) []bd.Issue {
	body := "quoted:\nsession: sess-1\nworktree: " + worktree + "\n"
	return []bd.Issue{
		{ID: "wisp-msg", IssueType: "message", Ephemeral: true, Status: "open", Description: body},
		{ID: "wisp-task", IssueType: "task", Ephemeral: true, Status: "open", Description: body},
		{ID: "agent-1", IssueType: "agent", Status: "open", Description: body},
		{ID: "role-1", IssueType: "role", Status: "open", Description: body},
	}
}

// Session-match results over initiativeRows(include-infra list) equal today's
// results over the plain list, including with infra rows that quote the
// session line.
func TestMatchInitiativeBySession_InfraListMatchesPlainList(t *testing.T) {
	plain := []bd.Issue{
		{ID: "at-a", Status: "open", Description: "worktree: /a\nsession: sess-1\n"},
		{ID: "at-b", Status: "open", Description: "worktree: /b\nsession: sess-2\n"},
		{ID: "at-codex", Status: "open", Description: "runtime: codex\nsession: sess-3\n"},
	}
	withInfra := append(append([]bd.Issue{}, plain...), infraRows("/a")...)

	for _, sid := range []string{"sess-1", "sess-2", "sess-3", "sess-none", ""} {
		wantIss, wantOK := matchInitiativeBySession(plain, sessionruntime.Claude, sid)
		gotIss, gotOK := matchInitiativeBySession(initiativeRows(withInfra), sessionruntime.Claude, sid)
		if gotOK != wantOK || gotIss.ID != wantIss.ID {
			t.Errorf("session %q: got (%q,%v), want (%q,%v)", sid, gotIss.ID, gotOK, wantIss.ID, wantOK)
		}
		// Control: the unfiltered list is what the hazard would corrupt.
		if sid == "sess-1" {
			if _, ok := matchInitiativeBySession(withInfra, sessionruntime.Claude, sid); ok {
				t.Errorf("control: unfiltered infra list should be ambiguous for %q", sid)
			}
		}
	}
}

func TestResolveInitiativeBySession_OneListCall(t *testing.T) {
	var calls []string
	f := listShowBD([]bd.Issue{{ID: "at-a", Status: "open", Description: "session: sess-1\n"}}, nil, &calls)
	ctx, _, _ := makeCtx(f, t.TempDir())

	iss, ok, err := resolveInitiativeBySession(ctx, sessionruntime.Claude, "sess-1")
	if err != nil || !ok || iss.ID != "at-a" {
		t.Fatalf("got (%q,%v,%v), want (at-a,true,nil)", iss.ID, ok, err)
	}
	assertCalls(t, calls, plainOpenList)
}

// resolve-initiative with --session-id lists once (was twice), whether the tie
// hits or the path match decides.
func TestResolveInitiative_OneListCall(t *testing.T) {
	open := []bd.Issue{
		{ID: "at-tied", Status: "open", Description: "worktree: /elsewhere\nsession: sess-1\n"},
		{ID: "at-path", Status: "open", Description: "worktree: /a/wt\n"},
	}
	for _, tc := range []struct{ session, want string }{
		{"sess-1", "at-tied"},
		{"sess-none", "at-path"},
		{"", "at-path"},
	} {
		var calls []string
		ctx, stdout, _ := makeCtx(listShowBD(open, nil, &calls), t.TempDir())
		cmd := &resolveInitiativeKong{Path: "/a/wt", SessionID: tc.session}
		if err := cmd.Run(ctx); err != nil {
			t.Fatalf("session %q: %v", tc.session, err)
		}
		if got := strings.TrimSpace(stdout.String()); got != tc.want {
			t.Errorf("session %q: stdout %q, want %q", tc.session, got, tc.want)
		}
		assertCalls(t, calls, plainOpenList)
	}
}

func TestResolveInitiative_ListErrorIsSilent(t *testing.T) {
	f := &fakeBD{runJSONFn: func(any, ...string) error { return errors.New("bd down") }}
	ctx, stdout, _ := makeCtx(f, t.TempDir())
	if err := (&resolveInitiativeKong{Path: "/a/wt", SessionID: "sess-1"}).Run(ctx); err != nil {
		t.Fatalf("err = %v, want nil (exit 0 on bd error)", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout %q, want empty", stdout.String())
	}
}

// appendSessionID with a known open target: first tie is list + update (was
// show + list + update); already tied is one list (was one show).
func TestAppendSessionID_OneListCall(t *testing.T) {
	open := []bd.Issue{{ID: "at-init", Status: "open", Description: "problem: x\n"}}
	var calls []string
	ctx, _, _ := makeCtx(listShowBD(open, nil, &calls), t.TempDir())
	if err := appendSessionID(ctx, "at-init", "sess-1"); err != nil {
		t.Fatalf("first tie: %v", err)
	}
	assertCalls(t, calls, plainOpenList, "update at-init")

	tied := []bd.Issue{{ID: "at-init", Status: "open", Description: "problem: x\nsession: sess-1\n"}}
	calls = nil
	ctx, _, _ = makeCtx(listShowBD(tied, nil, &calls), t.TempDir())
	if err := appendSessionID(ctx, "at-init", "sess-1"); err != nil {
		t.Fatalf("already tied: %v", err)
	}
	assertCalls(t, calls, plainOpenList)
}

// A target absent from the open list (closed) still ties through the bd show
// fallback.
func TestAppendSessionID_ClosedTargetUsesShowFallback(t *testing.T) {
	closed := bd.Issue{ID: "at-closed", Status: "closed", Description: "problem: x\n"}
	var calls []string
	ctx, _, _ := makeCtx(listShowBD(nil, &closed, &calls), t.TempDir())
	if err := appendSessionID(ctx, "at-closed", "sess-1"); err != nil {
		t.Fatalf("closed target: %v", err)
	}
	assertCalls(t, calls, plainOpenList, "show at-closed", "update at-closed")
}

func TestAppendSessionID_GuardUsesSameList(t *testing.T) {
	open := []bd.Issue{
		{ID: "at-init", Status: "open", Description: "problem: x\n"},
		{ID: "at-other", Status: "open", Description: "session: sess-1\n"},
	}
	var calls []string
	ctx, _, _ := makeCtx(listShowBD(open, nil, &calls), t.TempDir())
	err := appendSessionID(ctx, "at-init", "sess-1")
	if !errors.Is(err, errSessionTiedElsewhere) {
		t.Fatalf("err = %v, want errSessionTiedElsewhere", err)
	}
	assertCalls(t, calls, plainOpenList)
}

// tie-session without --initiative-id: first tie is list + update (was
// list + show + list + update); already tied is one list (was list + show).
func TestTieSession_CwdResolutionBDCalls(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	fresh := []bd.Issue{{ID: "at-cwd", Status: "open", Description: "worktree: " + cwd + "\n"}}
	var calls []string
	ctx, stdout, _ := makeCtx(listShowBD(fresh, nil, &calls), t.TempDir())
	if err := (&tieSessionKong{SessionID: "sess-1"}).Run(ctx); err != nil {
		t.Fatalf("first tie: %v", err)
	}
	assertCalls(t, calls, plainOpenList, "update at-cwd")
	if !strings.Contains(stdout.String(), "tied session sess-1 to at-cwd") {
		t.Errorf("stdout %q missing confirmation", stdout.String())
	}

	tied := []bd.Issue{{ID: "at-cwd", Status: "open", Description: "worktree: " + cwd + "\nsession: sess-1\n"}}
	calls = nil
	ctx, _, _ = makeCtx(listShowBD(tied, nil, &calls), t.TempDir())
	if err := (&tieSessionKong{SessionID: "sess-1"}).Run(ctx); err != nil {
		t.Fatalf("already tied: %v", err)
	}
	assertCalls(t, calls, plainOpenList)
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
