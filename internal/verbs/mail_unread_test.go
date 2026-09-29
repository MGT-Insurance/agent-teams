package verbs

import (
	"errors"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

// ── unreadMailFor ─────────────────────────────────────────────────────────────

// TestUnreadMailFor_QueriesOpenUnreadMessagesForRecipient verifies unreadMailFor
// queries bd list scoped to the recipient's open, unread, message-type
// issues (mirroring queryUnreadMessages) and filters out non-message issues.
func TestUnreadMailFor_QueriesOpenUnreadMessagesForRecipient(t *testing.T) {
	var gotArgs []string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			gotArgs = args
			out, ok := dst.(*[]bd.Issue)
			if !ok {
				t.Fatalf("unreadMailFor: dst = %T, want *[]bd.Issue", dst)
			}
			*out = []bd.Issue{
				{ID: "at-msg-1", IssueType: "message", Assignee: "at-recipient"},
				{ID: "at-epic-1", IssueType: "epic", Assignee: "at-recipient"},
			}
			return nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	got, err := unreadMailFor(ctx, "at-recipient")
	if err != nil {
		t.Fatalf("unreadMailFor: %v", err)
	}
	if len(got) != 1 || got[0].ID != "at-msg-1" {
		t.Fatalf("unreadMailFor = %+v, want only the message-type issue", got)
	}
	assertArgsContain(t, gotArgs, "--assignee=at-recipient", "--exclude-label=read", "--status=open")
}

// TestUnreadMailFor_QueryErrorPropagates verifies a bd list failure surfaces
// to the caller rather than being swallowed.
func TestUnreadMailFor_QueryErrorPropagates(t *testing.T) {
	wantErr := errors.New("bd list boom")
	fbd := &fakeBD{runJSONFn: func(dst any, args ...string) error { return wantErr }}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	if _, err := unreadMailFor(ctx, "at-recipient"); !errors.Is(err, wantErr) {
		t.Fatalf("unreadMailFor error = %v, want %v", err, wantErr)
	}
}

// ── openMailWithDedupKey ──────────────────────────────────────────────────────

// TestOpenMailWithDedupKey_EmptyKeyAlwaysMisses verifies an empty dedup key
// never hits bd at all — dedup is opt-in per CONTRACT 8st0.18 item 1a.
func TestOpenMailWithDedupKey_EmptyKeyAlwaysMisses(t *testing.T) {
	called := false
	fbd := &fakeBD{runJSONFn: func(dst any, args ...string) error {
		called = true
		return nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	id, hit, err := openMailWithDedupKey(ctx, "at-recipient", "")
	if err != nil || hit || id != "" {
		t.Fatalf("openMailWithDedupKey(empty key) = (%q, %v, %v), want (\"\", false, nil)", id, hit, err)
	}
	if called {
		t.Error("openMailWithDedupKey(empty key) must not query bd at all")
	}
}

// TestOpenMailWithDedupKey_Hit verifies a matching OPEN message assigned to
// id with the dedup label is reported as a hit, and the query is scoped to
// the recipient, the dedup label, and open status.
func TestOpenMailWithDedupKey_Hit(t *testing.T) {
	var gotArgs []string
	fbd := &fakeBD{runJSONFn: func(dst any, args ...string) error {
		gotArgs = args
		out := dst.(*[]bd.Issue)
		*out = []bd.Issue{{ID: "at-existing", IssueType: "message"}}
		return nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	id, hit, err := openMailWithDedupKey(ctx, "at-recipient", "abc123")
	if err != nil {
		t.Fatalf("openMailWithDedupKey: %v", err)
	}
	if !hit || id != "at-existing" {
		t.Fatalf("openMailWithDedupKey = (%q, %v), want (\"at-existing\", true)", id, hit)
	}
	assertArgsContain(t, gotArgs, "--assignee=at-recipient", "--label=dedup:abc123", "--status=open")
}

// TestOpenMailWithDedupKey_MissWhenNoneAssigned verifies no matching message
// reports a clean miss rather than an error.
func TestOpenMailWithDedupKey_MissWhenNoneAssigned(t *testing.T) {
	fbd := &fakeBD{runJSONFn: func(dst any, args ...string) error {
		*dst.(*[]bd.Issue) = nil
		return nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	id, hit, err := openMailWithDedupKey(ctx, "at-recipient", "abc123")
	if err != nil || hit || id != "" {
		t.Fatalf("openMailWithDedupKey(no match) = (%q, %v, %v), want (\"\", false, nil)", id, hit, err)
	}
}

// TestOpenMailWithDedupKey_NonMessageIssueDoesNotCount verifies filterMessageType
// is applied, so a non-message issue that happens to carry the dedup label
// (defensive against inconsistent bd --type honoring, mirrors mail.go) is not
// reported as a hit.
func TestOpenMailWithDedupKey_NonMessageIssueDoesNotCount(t *testing.T) {
	fbd := &fakeBD{runJSONFn: func(dst any, args ...string) error {
		*dst.(*[]bd.Issue) = []bd.Issue{{ID: "at-epic-1", IssueType: "epic"}}
		return nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	if _, hit, _ := openMailWithDedupKey(ctx, "at-recipient", "abc123"); hit {
		t.Error("expected a non-message issue to not count as a dedup hit")
	}
}

// TestOpenMailWithDedupKey_QueryErrorPropagates verifies a bd list failure
// surfaces rather than being reported as a miss.
func TestOpenMailWithDedupKey_QueryErrorPropagates(t *testing.T) {
	wantErr := errors.New("bd list boom")
	fbd := &fakeBD{runJSONFn: func(dst any, args ...string) error { return wantErr }}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	if _, _, err := openMailWithDedupKey(ctx, "at-recipient", "abc123"); err == nil {
		t.Fatal("openMailWithDedupKey: expected the query error to propagate, got nil")
	}
}

// ── closeMailAs ───────────────────────────────────────────────────────────────

// TestCloseMailAs_LabelsNotesAndForceClosesEachMessage verifies the frozen
// per-message order: label add, then note, then bd close --force.
func TestCloseMailAs_LabelsNotesAndForceClosesEachMessage(t *testing.T) {
	var calls [][]string
	fbd := &fakeBD{runFn: func(args ...string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		return "", nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	err := closeMailAs(ctx, []string{"at-m1", "at-m2"}, "delivery:moot", "PR merged; nothing to act on")
	if err != nil {
		t.Fatalf("closeMailAs: %v", err)
	}
	want := [][]string{
		{"label", "add", "at-m1", "delivery:moot"},
		{"note", "at-m1", "PR merged; nothing to act on"},
		{"close", "at-m1", "--force"},
		{"label", "add", "at-m2", "delivery:moot"},
		{"note", "at-m2", "PR merged; nothing to act on"},
		{"close", "at-m2", "--force"},
	}
	if len(calls) != len(want) {
		t.Fatalf("closeMailAs calls = %v, want %v", calls, want)
	}
	for i, w := range want {
		if !equalArgs(calls[i], w) {
			t.Errorf("closeMailAs call[%d] = %v, want %v", i, calls[i], w)
		}
	}
}

// TestCloseMailAs_EmptyNoteSkipsNoteCall verifies note == "" omits the note
// step entirely rather than appending an empty note.
func TestCloseMailAs_EmptyNoteSkipsNoteCall(t *testing.T) {
	var calls [][]string
	fbd := &fakeBD{runFn: func(args ...string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		return "", nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	if err := closeMailAs(ctx, []string{"at-m1"}, "delivery:moot", ""); err != nil {
		t.Fatalf("closeMailAs: %v", err)
	}
	want := [][]string{
		{"label", "add", "at-m1", "delivery:moot"},
		{"close", "at-m1", "--force"},
	}
	if len(calls) != len(want) {
		t.Fatalf("closeMailAs calls = %v, want %v (no note call)", calls, want)
	}
}

// TestCloseMailAs_PerMessageFailureDoesNotAbortTheRest verifies a failure on
// one message's label step still runs its note+close steps and moves on to
// close the remaining messages, returning the first error encountered.
func TestCloseMailAs_PerMessageFailureDoesNotAbortTheRest(t *testing.T) {
	var calls [][]string
	labelErr := errors.New("label add failed")
	fbd := &fakeBD{runFn: func(args ...string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) >= 3 && args[0] == "label" && args[2] == "at-m1" {
			return "", labelErr
		}
		return "", nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	err := closeMailAs(ctx, []string{"at-m1", "at-m2"}, "delivery:moot", "note text")
	if err == nil {
		t.Fatal("closeMailAs: expected the label failure to be reported, got nil error")
	}
	// Every step for both ids still ran despite at-m1's label failure.
	want := [][]string{
		{"label", "add", "at-m1", "delivery:moot"},
		{"note", "at-m1", "note text"},
		{"close", "at-m1", "--force"},
		{"label", "add", "at-m2", "delivery:moot"},
		{"note", "at-m2", "note text"},
		{"close", "at-m2", "--force"},
	}
	if len(calls) != len(want) {
		t.Fatalf("closeMailAs calls = %v, want %v", calls, want)
	}
}

// TestCloseMailAs_EmptyIDsIsANoOp verifies no ids means no bd calls and no
// error — closeMailAs never invents work.
func TestCloseMailAs_EmptyIDsIsANoOp(t *testing.T) {
	called := false
	fbd := &fakeBD{runFn: func(args ...string) (string, error) {
		called = true
		return "", nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	if err := closeMailAs(ctx, nil, "delivery:moot", "note"); err != nil {
		t.Fatalf("closeMailAs(nil ids): %v", err)
	}
	if called {
		t.Error("closeMailAs(nil ids) must not call bd at all")
	}
}

// ── test helpers ──────────────────────────────────────────────────────────────

// assertArgsContain fails the test unless every want string appears
// somewhere in got.
func assertArgsContain(t *testing.T, got []string, want ...string) {
	t.Helper()
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("args %v missing expected element %q", got, w)
		}
	}
}

// equalArgs reports whether a and b hold the same strings in the same order.
func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
