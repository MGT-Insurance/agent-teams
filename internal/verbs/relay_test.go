package verbs

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/transport"
)

// ── fakes ─────────────────────────────────────────────────────────────────────

// relayFakeTransport is a minimal Transport for relay tests. Receive invokes handler
// for each reply in replies, then returns recvErr (if non-nil) or nil.
type relayFakeTransport struct {
	replies  []transport.Reply
	recvErr  error
	received bool
}

func (f *relayFakeTransport) Name() string { return "fake" }
func (f *relayFakeTransport) Send(_ transport.OutboundMessage) (string, error) {
	return "", nil
}
func (f *relayFakeTransport) Receive(handler func(transport.Reply) error) error {
	f.received = true
	for _, r := range f.replies {
		if err := handler(r); err != nil {
			return err
		}
	}
	return f.recvErr
}

// fakeBDQuery captures label queries and returns configured issues.
type fakeBDQuery struct {
	results map[string][]bd.Issue // keyed by label
	err     map[string]error
}

func newFakeBDQuery() *fakeBDQuery {
	return &fakeBDQuery{
		results: map[string][]bd.Issue{},
		err:     map[string]error{},
	}
}

func (f *fakeBDQuery) query(_, label string) ([]bd.Issue, error) {
	if err, ok := f.err[label]; ok {
		return nil, err
	}
	return f.results[label], nil
}

// fakeSend records the calls and can return an error.
type fakeSend struct {
	calls []struct{ id, file string }
	err   error
}

func (f *fakeSend) send(_ *cli.Context, id, file string) error {
	f.calls = append(f.calls, struct{ id, file string }{id, file})
	return f.err
}

// newRelayCtx builds a cli.Context with captured stdout/stderr buffers.
func newRelayCtx(t *testing.T) *cli.Context {
	t.Helper()
	return &cli.Context{
		Home:   t.TempDir(),
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	}
}

func relayStdout(ctx *cli.Context) string { return ctx.Stdout.(*bytes.Buffer).String() }
func relayStderr(ctx *cli.Context) string { return ctx.Stderr.(*bytes.Buffer).String() }

// ── relay verb — opt-in (Enabled=false) ───────────────────────────────────────

// TestRelay_EnabledFalse_CleanExit verifies that when messaging is not
// configured, relay prints a no-op message and exits 0 without calling Receive.
func TestRelay_EnabledFalse_CleanExit(t *testing.T) {
	ft := &relayFakeTransport{}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:      func(string) bool { return false },
		transportFor: func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:      newFakeBDQuery().query,
		send:         (&fakeSend{}).send,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if ft.received {
		t.Fatal("Receive must NOT be called when Enabled=false")
	}
	if !strings.Contains(relayStdout(ctx), "not configured") {
		t.Errorf("expected 'not configured' in stdout, got: %q", relayStdout(ctx))
	}
}

// TestRelay_EnabledFalse_NoStderrNoise verifies that Enabled=false produces no
// warnings or error output to stderr.
func TestRelay_EnabledFalse_NoStderrNoise(t *testing.T) {
	ctx := newRelayCtx(t)
	cmd := &relayKong{
		enabled:      func(string) bool { return false },
		transportFor: func(string) (transport.Transport, error) { return &relayFakeTransport{}, nil },
		bdQuery:      newFakeBDQuery().query,
		send:         (&fakeSend{}).send,
	}
	_ = cmd.Run(ctx)
	if relayStderr(ctx) != "" {
		t.Errorf("expected empty stderr when disabled, got: %q", relayStderr(ctx))
	}
}

// ── handler: mapped thread → ateam send ───────────────────────────────────────

// TestRelay_MappedThread_SendCalled verifies that a reply with a known
// thread ref triggers ateam send with the right initiative id and a non-empty
// temp file path.
func TestRelay_MappedThread_SendCalled(t *testing.T) {
	bdq := newFakeBDQuery()
	bdq.results["thread:42"] = []bd.Issue{{ID: "at-001", Status: "open"}}

	fs := &fakeSend{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "42", Text: "looks good"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:      func(string) bool { return true },
		transportFor: func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:      bdq.query,
		send:         fs.send,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(fs.calls))
	}
	if fs.calls[0].id != "at-001" {
		t.Errorf("send id = %q, want at-001", fs.calls[0].id)
	}
	if fs.calls[0].file == "" {
		t.Error("send file must be non-empty")
	}
}

// ── handler: empty ThreadRef → skip ───────────────────────────────────────────

func TestRelay_EmptyThreadRef_Skipped(t *testing.T) {
	fs := &fakeSend{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "", Text: "reply in general"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:      func(string) bool { return true },
		transportFor: func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:      newFakeBDQuery().query,
		send:         fs.send,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 0 {
		t.Errorf("expected no send calls for empty ThreadRef, got %d", len(fs.calls))
	}
	if !strings.Contains(relayStderr(ctx), "non-topic") {
		t.Errorf("expected 'non-topic' log in stderr, got: %q", relayStderr(ctx))
	}
}

// ── handler: unmapped thread → skip ───────────────────────────────────────────

func TestRelay_UnmappedThread_Skipped(t *testing.T) {
	fs := &fakeSend{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "99", Text: "reply"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:      func(string) bool { return true },
		transportFor: func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:      newFakeBDQuery().query, // returns empty for "thread:99"
		send:         fs.send,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 0 {
		t.Errorf("expected no send calls for unmapped thread, got %d", len(fs.calls))
	}
	if !strings.Contains(relayStderr(ctx), "no open initiative") {
		t.Errorf("expected 'no open initiative' in stderr, got: %q", relayStderr(ctx))
	}
}

// ── handler: ambiguous thread → skip ──────────────────────────────────────────

func TestRelay_AmbiguousThread_Skipped(t *testing.T) {
	bdq := newFakeBDQuery()
	bdq.results["thread:7"] = []bd.Issue{
		{ID: "at-001", Status: "open"},
		{ID: "at-002", Status: "open"},
	}
	fs := &fakeSend{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "7", Text: "reply"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:      func(string) bool { return true },
		transportFor: func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:      bdq.query,
		send:         fs.send,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 0 {
		t.Errorf("expected no send calls for ambiguous thread, got %d", len(fs.calls))
	}
	if !strings.Contains(relayStderr(ctx), "ambiguous") {
		t.Errorf("expected 'ambiguous' in stderr, got: %q", relayStderr(ctx))
	}
}

// ── handler: bad reply doesn't abort the loop ─────────────────────────────────

// TestRelay_BadReplyDoesNotAbort verifies that a send failure on one reply does
// not abort the relay loop — subsequent replies are still processed.
func TestRelay_BadReplyDoesNotAbort(t *testing.T) {
	bdq := newFakeBDQuery()
	bdq.results["thread:1"] = []bd.Issue{{ID: "at-001", Status: "open"}}
	bdq.results["thread:2"] = []bd.Issue{{ID: "at-002", Status: "open"}}

	callCount := 0
	fs := &fakeSend{}
	sendFn := func(ctx *cli.Context, id, file string) error {
		callCount++
		fs.calls = append(fs.calls, struct{ id, file string }{id, file})
		if id == "at-001" {
			return fmt.Errorf("send failed")
		}
		return nil
	}

	ft := &relayFakeTransport{
		replies: []transport.Reply{
			{ThreadRef: "1", Text: "first"},
			{ThreadRef: "2", Text: "second"},
		},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:      func(string) bool { return true },
		transportFor: func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:      bdq.query,
		send:         sendFn,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("loop must not abort on a bad reply, got: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected send called 2 times (both replies processed), got %d", callCount)
	}
	// First reply failed; second succeeded.
	if fs.calls[1].id != "at-002" {
		t.Errorf("second send id = %q, want at-002", fs.calls[1].id)
	}
	if !strings.Contains(relayStderr(ctx), "ateam send at-001 failed") {
		t.Errorf("expected send failure log for at-001, stderr: %q", relayStderr(ctx))
	}
}

// ── handler: bd query error → skip, loop continues ───────────────────────────

func TestRelay_BDQueryError_SkipsReply(t *testing.T) {
	bdq := newFakeBDQuery()
	bdq.err["thread:5"] = fmt.Errorf("bd timeout")
	bdq.results["thread:6"] = []bd.Issue{{ID: "at-006", Status: "open"}}

	fs := &fakeSend{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{
			{ThreadRef: "5", Text: "bad"},
			{ThreadRef: "6", Text: "good"},
		},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:      func(string) bool { return true },
		transportFor: func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:      bdq.query,
		send:         fs.send,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("bd error must not abort loop, got: %v", err)
	}
	if len(fs.calls) != 1 || fs.calls[0].id != "at-006" {
		t.Errorf("expected exactly 1 send for at-006, got %v", fs.calls)
	}
}
