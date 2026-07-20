// Tests for closeKong's fail-soft close-signal (farewell message +
// CloseTopic) added in agent-teams-7dup.1. See notify_test.go for the shared
// fakeTransport / fakeTransportFor helpers reused here.
package verbs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/transport"
)

// ── closeSignalFakeBD ────────────────────────────────────────────────────────

// closeSignalFakeBD is an injectable cli.BDRunner that responds to `bd close`
// and `bd show <id> --json`. Named to avoid collision with dispatch_test.go's
// fakeBD and notify_test.go's notifyFakeBD.
type closeSignalFakeBD struct {
	// issue returned by bd show <id> --json (after close)
	issue bd.Issue
	// closeErr, if non-nil, is returned by bd close
	closeErr error
	// closeCalls records the args passed to bd close
	closeCalls [][]string
}

func (f *closeSignalFakeBD) Run(args ...string) (string, error) {
	if len(args) >= 1 && args[0] == "close" {
		f.closeCalls = append(f.closeCalls, args)
		if f.closeErr != nil {
			return "", f.closeErr
		}
		return "ok", nil
	}
	if len(args) >= 3 && args[0] == "show" && args[2] == "--json" {
		raw, err := json.Marshal([]bd.Issue{f.issue})
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	return "", fmt.Errorf("closeSignalFakeBD: unexpected Run(%v)", args)
}

func (f *closeSignalFakeBD) RunJSON(dst any, args ...string) error {
	return fmt.Errorf("closeSignalFakeBD: unexpected RunJSON(%v)", args)
}

func newCloseSignalCtx(b cli.BDRunner) (*cli.Context, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	return &cli.Context{
		Home:   "/fake/home",
		BD:     b,
		Stdout: out,
		Stderr: errBuf,
	}, out, errBuf
}

// ── sendCloseSignal ───────────────────────────────────────────────────────────

// TestClose_SendsFarewellAndClosesTopic_WhenThreadLabelPresent confirms the
// happy path: a thread label on the initiative bead triggers a farewell Send
// followed by CloseTopic, both against the injected transport.
func TestClose_SendsFarewellAndClosesTopic_WhenThreadLabelPresent(t *testing.T) {
	nbd := &closeSignalFakeBD{
		issue: bd.Issue{
			ID:     "at-00o",
			Title:  "my initiative",
			Labels: []string{"at-00o", "thread:999"},
		},
	}
	ft := &fakeTopicCloserTransport{fakeTransport: fakeTransport{returnRef: "999"}}

	cmd := &closeKong{
		ID:                 "at-00o",
		runUpdateLocalMain: func(repoPath string) (string, error) { return "", fmt.Errorf("no repo") },
		transportFor:       func(home string) (transport.Transport, error) { return ft, nil },
	}
	ctx, _, errBuf := newCloseSignalCtx(nbd)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	if ft.calls[0].ThreadRef != "999" {
		t.Errorf("Send ThreadRef = %q, want 999", ft.calls[0].ThreadRef)
	}
	if !strings.Contains(ft.calls[0].Body, "closed") {
		t.Errorf("farewell body = %q, want it to mention the topic is closed", ft.calls[0].Body)
	}
	if len(ft.closeTopicCalls) != 1 || ft.closeTopicCalls[0] != "999" {
		t.Errorf("CloseTopic calls = %v, want [\"999\"]", ft.closeTopicCalls)
	}
	if errBuf.String() != "" {
		t.Errorf("expected no stderr warnings, got %q", errBuf.String())
	}
}

// TestClose_SkipsSignal_WhenNoThreadLabel confirms the silent-skip path when
// the initiative bead has no thread label.
func TestClose_SkipsSignal_WhenNoThreadLabel(t *testing.T) {
	nbd := &closeSignalFakeBD{
		issue: bd.Issue{ID: "at-00o", Title: "my initiative", Labels: []string{"at-00o"}},
	}
	ft := &fakeTopicCloserTransport{fakeTransport: fakeTransport{returnRef: "999"}}

	cmd := &closeKong{
		ID:                 "at-00o",
		runUpdateLocalMain: func(repoPath string) (string, error) { return "", fmt.Errorf("no repo") },
		transportFor:       func(home string) (transport.Transport, error) { return ft, nil },
	}
	ctx, _, errBuf := newCloseSignalCtx(nbd)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(ft.calls) != 0 {
		t.Errorf("expected no Send calls when no thread label, got %d", len(ft.calls))
	}
	if len(ft.closeTopicCalls) != 0 {
		t.Errorf("expected no CloseTopic calls when no thread label, got %d", len(ft.closeTopicCalls))
	}
	if errBuf.String() != "" {
		t.Errorf("expected no stderr warnings (silent skip), got %q", errBuf.String())
	}
}

// TestClose_SkipsSignal_WhenNoTransportConfigured confirms the silent-skip
// path when transportFor errors (no transport configured) — the normal
// default state for installs without Telegram set up.
func TestClose_SkipsSignal_WhenNoTransportConfigured(t *testing.T) {
	nbd := &closeSignalFakeBD{
		issue: bd.Issue{ID: "at-00o", Title: "my initiative", Labels: []string{"at-00o", "thread:999"}},
	}
	cmd := &closeKong{
		ID:                 "at-00o",
		runUpdateLocalMain: func(repoPath string) (string, error) { return "", fmt.Errorf("no repo") },
		transportFor:       func(home string) (transport.Transport, error) { return nil, fmt.Errorf("not configured") },
	}
	ctx, _, errBuf := newCloseSignalCtx(nbd)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if errBuf.String() != "" {
		t.Errorf("expected no stderr warnings (silent skip), got %q", errBuf.String())
	}
}

// TestClose_SucceedsDespiteTransportSendFailure confirms that a transport
// error during the farewell Send is logged to stderr but does not fail
// `ateam close` — the bd close has already succeeded by that point.
func TestClose_SucceedsDespiteTransportSendFailure(t *testing.T) {
	nbd := &closeSignalFakeBD{
		issue: bd.Issue{ID: "at-00o", Title: "my initiative", Labels: []string{"at-00o", "thread:999"}},
	}
	ft := &fakeTopicCloserTransport{fakeTransport: fakeTransport{returnErr: fmt.Errorf("telegram: API error: forbidden")}}

	cmd := &closeKong{
		ID:                 "at-00o",
		runUpdateLocalMain: func(repoPath string) (string, error) { return "", fmt.Errorf("no repo") },
		transportFor:       func(home string) (transport.Transport, error) { return ft, nil },
	}
	ctx, _, errBuf := newCloseSignalCtx(nbd)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run should not fail on transport error, got: %v", err)
	}
	if len(ft.closeTopicCalls) != 0 {
		t.Errorf("CloseTopic should not be called when Send fails, got %d calls", len(ft.closeTopicCalls))
	}
	if !strings.Contains(errBuf.String(), "farewell message failed") {
		t.Errorf("expected stderr warning about farewell failure, got %q", errBuf.String())
	}
}

// TestClose_SucceedsDespiteCloseTopicFailure confirms that a transport error
// during CloseTopic is logged to stderr but does not fail `ateam close`.
func TestClose_SucceedsDespiteCloseTopicFailure(t *testing.T) {
	nbd := &closeSignalFakeBD{
		issue: bd.Issue{ID: "at-00o", Title: "my initiative", Labels: []string{"at-00o", "thread:999"}},
	}
	ft := &fakeTopicCloserTransport{
		fakeTransport: fakeTransport{returnRef: "999"},
		closeTopicErr: fmt.Errorf("telegram: API error: topic not found"),
	}

	cmd := &closeKong{
		ID:                 "at-00o",
		runUpdateLocalMain: func(repoPath string) (string, error) { return "", fmt.Errorf("no repo") },
		transportFor:       func(home string) (transport.Transport, error) { return ft, nil },
	}
	ctx, _, errBuf := newCloseSignalCtx(nbd)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run should not fail on transport error, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "closing topic failed") {
		t.Errorf("expected stderr warning about topic-close failure, got %q", errBuf.String())
	}
}

// ── fakeTopicCloserTransport ─────────────────────────────────────────────────

// fakeTopicCloserTransport extends fakeTransport (notify_test.go) with a
// CloseTopic method so it satisfies the verbs package's topicCloser
// interface for close-signal tests.
type fakeTopicCloserTransport struct {
	fakeTransport
	closeTopicErr   error
	closeTopicCalls []string
}

func (f *fakeTopicCloserTransport) CloseTopic(threadRef string) error {
	f.closeTopicCalls = append(f.closeTopicCalls, threadRef)
	return f.closeTopicErr
}
