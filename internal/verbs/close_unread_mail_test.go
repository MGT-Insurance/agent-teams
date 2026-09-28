// Tests for closeKong's CONTRACT agent-teams-8st0.18 item 5 guard
// (agent-teams-8st0.20): a review-shaped initiative with unread mail refuses
// to close, with no override. Non-review initiatives are unaffected. See
// close_signal_test.go for closeSignalFakeBD (a different fixture: it only
// answers "close" and "show", never "list") and mail_unread_test.go for the
// unreadMailFor/openMailWithDedupKey/closeMailAs unit tests this guard reuses.
package verbs

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

// unreadMailCloseFakeBD answers `bd show <id> --json` with issue and `bd
// list ...` (unreadMailFor's query) with messages; it records whether `bd
// close` was ever invoked, so a refusal can be proven to leave the
// initiative untouched.
type unreadMailCloseFakeBD struct {
	issue       bd.Issue
	messages    []bd.Issue
	listErr     error
	closeCalled bool
}

func (f *unreadMailCloseFakeBD) Run(args ...string) (string, error) {
	if len(args) >= 1 && args[0] == "close" {
		f.closeCalled = true
		return "ok", nil
	}
	if len(args) >= 3 && args[0] == "show" && args[2] == "--json" {
		raw, err := json.Marshal([]bd.Issue{f.issue})
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	return "", nil
}

func (f *unreadMailCloseFakeBD) RunJSON(dst any, args ...string) error {
	if len(args) >= 1 && args[0] == "list" {
		if f.listErr != nil {
			return f.listErr
		}
		*dst.(*[]bd.Issue) = f.messages
		return nil
	}
	return nil
}

// TestClose_ReviewShapedWithUnreadMail_Refuses covers the ACCEPTANCE case:
// review-shaped + 2 unread -> exit 1, message names the count and the id,
// and bd close is never invoked (the initiative stays open).
func TestClose_ReviewShapedWithUnreadMail_Refuses(t *testing.T) {
	fbd := &unreadMailCloseFakeBD{
		issue:    bd.Issue{ID: "at-5", Description: "pr-url: https://github.com/o/r/pull/1\n"},
		messages: []bd.Issue{{ID: "at-m1", IssueType: "message"}, {ID: "at-m2", IssueType: "message"}},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	err := (&closeKong{ID: "at-5"}).Run(ctx)
	if err == nil {
		t.Fatal("expected close to refuse, got nil error")
	}
	want := "ateam close: at-5 has 2 unread message(s); run ateam mail inbox and handle them"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
	if fbd.closeCalled {
		t.Error("bd close must not be called when refusing on unread mail")
	}
}

// TestClose_ReviewShapedNoUnreadMail_ClosesAsBefore covers the ACCEPTANCE
// case: review-shaped + 0 unread -> closes normally.
func TestClose_ReviewShapedNoUnreadMail_ClosesAsBefore(t *testing.T) {
	fbd := &unreadMailCloseFakeBD{
		issue:    bd.Issue{ID: "at-5", Description: "pr-url: https://github.com/o/r/pull/1\n"},
		messages: nil,
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	if err := (&closeKong{ID: "at-5"}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fbd.closeCalled {
		t.Error("expected bd close to be called when there is no unread mail")
	}
}

// TestClose_NonReviewWithUnreadMail_ClosesAsBefore covers the ACCEPTANCE
// case: a non-review initiative is never checked, even with unread mail
// present — closing it is unchanged.
func TestClose_NonReviewWithUnreadMail_ClosesAsBefore(t *testing.T) {
	fbd := &unreadMailCloseFakeBD{
		issue:    bd.Issue{ID: "at-5", Description: "repo: /some/repo\n"}, // no pr-url line
		messages: []bd.Issue{{ID: "at-m1", IssueType: "message"}},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	if err := (&closeKong{ID: "at-5"}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fbd.closeCalled {
		t.Error("expected bd close to be called for a non-review initiative regardless of unread mail")
	}
}

// TestClose_UnreadMailQueryErrorPropagates verifies a bd list failure while
// checking unread mail surfaces as a close error rather than a silent
// fall-through to a (potentially wrong) close.
func TestClose_UnreadMailQueryErrorPropagates(t *testing.T) {
	wantErr := errors.New("bd list boom")
	fbd := &unreadMailCloseFakeBD{
		issue:   bd.Issue{ID: "at-5", Description: "pr-url: https://github.com/o/r/pull/1\n"},
		listErr: wantErr,
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	err := (&closeKong{ID: "at-5"}).Run(ctx)
	if err == nil {
		t.Fatal("expected the unread-mail query error to propagate, got nil")
	}
	if !strings.Contains(err.Error(), "bd list boom") {
		t.Errorf("error = %q, want it to contain the underlying query error", err.Error())
	}
	if fbd.closeCalled {
		t.Error("bd close must not be called when the unread-mail check itself fails")
	}
}

// TestClose_ShowIssueErrorFallsThroughToClose verifies that if the pre-close
// bd show lookup itself fails (e.g. bd show is broken, or the id doesn't
// exist), refuseIfUnreadReviewMail swallows it rather than blocking close —
// the close call below is left to surface bd's own "not found" error.
func TestClose_ShowIssueErrorFallsThroughToClose(t *testing.T) {
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "show" {
				return "", errors.New("bd show: not found")
			}
			return "ok", nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	if err := (&closeKong{ID: "at-5"}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
