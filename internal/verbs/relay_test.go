package verbs

import (
	"bytes"
	"fmt"
	"os"
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

// fakeSend records the calls and can return an error. Since the destination
// is now always the Steward, there is no per-call recipient id to capture —
// the mapped initiative id instead travels inside the file's envelope.
// handleReply removes the temp file via defer right after send returns, so
// send must parse the envelope synchronously (while the file still exists)
// rather than leaving that to the caller after Run returns.
type fakeSend struct {
	calls     []string               // file paths, in call order
	envelopes []StewardReplyEnvelope // parsed envelope, captured at call time
	err       error
}

func (f *fakeSend) send(_ *cli.Context, file string) error {
	f.calls = append(f.calls, file)
	f.envelopes = append(f.envelopes, parseEnvelopeFile(file))
	return f.err
}

// parseEnvelopeFile reads and parses the Relay→Steward envelope written to
// file. Panics on failure — in these tests the file is always written by
// handleReply just before send is called, so a read or parse failure here
// means the fake was misused, not that the code under test misbehaved.
func parseEnvelopeFile(file string) StewardReplyEnvelope {
	data, err := os.ReadFile(file)
	if err != nil {
		panic(fmt.Sprintf("parseEnvelopeFile: read %q: %v", file, err))
	}
	env, ok := ParseStewardReplyEnvelope(string(data))
	if !ok {
		panic(fmt.Sprintf("parseEnvelopeFile: %q is not a well-formed steward-reply envelope: %q", file, data))
	}
	return env
}

// fakeSendCapture records raw file contents at call time (envelope-agnostic —
// used by direct-channel tests, which parse via ParseStewardDirectEnvelope
// rather than the initiative-reply ParseStewardReplyEnvelope fakeSend uses).
type fakeSendCapture struct {
	calls  []string // file paths, in call order
	bodies []string // raw file contents, captured at call time
	err    error
}

func (f *fakeSendCapture) send(_ *cli.Context, file string) error {
	f.calls = append(f.calls, file)
	data, err := os.ReadFile(file)
	if err != nil {
		panic(fmt.Sprintf("fakeSendCapture: read %q: %v", file, err))
	}
	f.bodies = append(f.bodies, string(data))
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
// thread ref triggers ateam mail send with a non-empty temp file path whose
// contents are a steward-reply envelope carrying the mapped initiative id
// and Eric's reply text as the body.
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
	if fs.calls[0] == "" {
		t.Fatal("send file must be non-empty")
	}
	env := fs.envelopes[0]
	if env.InitiativeID != "at-001" {
		t.Errorf("envelope InitiativeID = %q, want at-001", env.InitiativeID)
	}
	if env.Body != "looks good" {
		t.Errorf("envelope Body = %q, want %q", env.Body, "looks good")
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
		bdQueryAll:   newFakeBDQuery().query, // no closed match either — safety net finds nothing
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

// ── handler: closed-initiative safety net (agent-teams-7dup.2) ───────────────

// TestRelay_ClosedInitiativeThread_RoutesToSteward verifies the case-0
// safety net: when bdQuery finds zero OPEN initiatives for the reply's
// thread label but bdQueryAll finds exactly one CLOSED initiative carrying
// it, the reply is routed to the Steward as a steward-closed-initiative
// envelope instead of being silently dropped, and the generic "no open
// initiative" skip log is suppressed.
func TestRelay_ClosedInitiativeThread_RoutesToSteward(t *testing.T) {
	bdq := newFakeBDQuery() // no open matches for "thread:50"
	bdqAll := newFakeBDQuery()
	bdqAll.results["thread:50"] = []bd.Issue{{ID: "at-050", Status: "closed"}}

	fs := &fakeSendCapture{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "50", Text: "still relevant?"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:      func(string) bool { return true },
		transportFor: func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:      bdq.query,
		bdQueryAll:   bdqAll.query,
		send:         fs.send,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(fs.calls))
	}
	env, ok := ParseStewardClosedInitiativeEnvelope(fs.bodies[0])
	if !ok {
		t.Fatalf("send file contents not a well-formed steward-closed-initiative envelope: %q", fs.bodies[0])
	}
	if env.InitiativeID != "at-050" {
		t.Errorf("envelope InitiativeID = %q, want at-050", env.InitiativeID)
	}
	if env.Body != "still relevant?" {
		t.Errorf("envelope Body = %q, want %q", env.Body, "still relevant?")
	}
	if !strings.Contains(relayStderr(ctx), "routed message to steward for closed initiative at-050") {
		t.Errorf("expected routed-to-steward log, stderr: %q", relayStderr(ctx))
	}
	if strings.Contains(relayStderr(ctx), "no open initiative found") {
		t.Errorf("should not log the generic skip message when routed, stderr: %q", relayStderr(ctx))
	}
}

// TestRelay_AmbiguousClosedInitiativeThread_Skipped verifies that 2+ CLOSED
// matches (like 0 matches) fall through to the existing skip behavior rather
// than the closed-initiative routing path.
func TestRelay_AmbiguousClosedInitiativeThread_Skipped(t *testing.T) {
	bdq := newFakeBDQuery()
	bdqAll := newFakeBDQuery()
	bdqAll.results["thread:51"] = []bd.Issue{
		{ID: "at-051", Status: "closed"},
		{ID: "at-052", Status: "closed"},
	}
	fs := &fakeSendCapture{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "51", Text: "reply"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:      func(string) bool { return true },
		transportFor: func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:      bdq.query,
		bdQueryAll:   bdqAll.query,
		send:         fs.send,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 0 {
		t.Errorf("expected no send calls for ambiguous closed matches, got %d", len(fs.calls))
	}
	if !strings.Contains(relayStderr(ctx), "no open initiative") {
		t.Errorf("expected 'no open initiative' skip log, stderr: %q", relayStderr(ctx))
	}
}

// TestRelay_ClosedInitiativeQueryError_Skipped verifies that a bdQueryAll
// error falls through to the existing skip behavior rather than aborting
// the relay loop.
func TestRelay_ClosedInitiativeQueryError_Skipped(t *testing.T) {
	bdq := newFakeBDQuery()
	bdqAll := newFakeBDQuery()
	bdqAll.err["thread:52"] = fmt.Errorf("bd timeout")
	fs := &fakeSendCapture{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "52", Text: "reply"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:      func(string) bool { return true },
		transportFor: func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:      bdq.query,
		bdQueryAll:   bdqAll.query,
		send:         fs.send,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 0 {
		t.Errorf("expected no send calls on bdQueryAll error, got %d", len(fs.calls))
	}
	if !strings.Contains(relayStderr(ctx), "no open initiative") {
		t.Errorf("expected fallback 'no open initiative' skip log, stderr: %q", relayStderr(ctx))
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
	sendFn := func(ctx *cli.Context, file string) error {
		callCount++
		env := parseEnvelopeFile(file)
		fs.calls = append(fs.calls, file)
		fs.envelopes = append(fs.envelopes, env)
		if env.InitiativeID == "at-001" {
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
	if fs.envelopes[1].InitiativeID != "at-002" {
		t.Errorf("second send envelope InitiativeID = %q, want at-002", fs.envelopes[1].InitiativeID)
	}
	if !strings.Contains(relayStderr(ctx), "ateam mail send steward failed (initiative at-001)") {
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
	if len(fs.calls) != 1 {
		t.Fatalf("expected exactly 1 send call, got %d", len(fs.calls))
	}
	if got := fs.envelopes[0].InitiativeID; got != "at-006" {
		t.Errorf("expected envelope InitiativeID at-006, got %q", got)
	}
}

// ── handler: direct-channel short-circuit ─────────────────────────────────────

// TestRelay_DirectThread_RoutesToSteward verifies that a reply whose thread
// ref matches the persisted Steward direct-message thread ref (contract:
// StewardDirectThreadPath) is routed to the Steward via a steward-direct
// envelope, bypassing the bd initiative lookup entirely.
func TestRelay_DirectThread_RoutesToSteward(t *testing.T) {
	ctx := newRelayCtx(t)
	if err := writeThreadRefFile(StewardDirectThreadPath(ctx), "direct-123"); err != nil {
		t.Fatalf("seed direct thread ref: %v", err)
	}

	bdQueryCalled := false
	fs := &fakeSendCapture{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "direct-123", Text: "hello steward"}},
	}

	cmd := &relayKong{
		enabled:      func(string) bool { return true },
		transportFor: func(string) (transport.Transport, error) { return ft, nil },
		bdQuery: func(home, label string) ([]bd.Issue, error) {
			bdQueryCalled = true
			return nil, nil
		},
		send: fs.send,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bdQueryCalled {
		t.Error("bd query must not be called for a direct-channel message")
	}
	if len(fs.calls) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(fs.calls))
	}
	env, ok := ParseStewardDirectEnvelope(fs.bodies[0])
	if !ok {
		t.Fatalf("send file contents not a well-formed steward-direct envelope: %q", fs.bodies[0])
	}
	if env.Body != "hello steward" {
		t.Errorf("envelope Body = %q, want %q", env.Body, "hello steward")
	}
}

// TestRelay_DirectThread_NonMatchingThreadRef_TakesInitiativePath verifies
// that when a direct-channel thread ref IS persisted, a reply whose
// ThreadRef does not match it still takes the existing initiative-reply
// path (bd lookup + steward-reply envelope), not the direct short-circuit.
func TestRelay_DirectThread_NonMatchingThreadRef_TakesInitiativePath(t *testing.T) {
	ctx := newRelayCtx(t)
	if err := writeThreadRefFile(StewardDirectThreadPath(ctx), "direct-123"); err != nil {
		t.Fatalf("seed direct thread ref: %v", err)
	}

	bdq := newFakeBDQuery()
	bdq.results["thread:42"] = []bd.Issue{{ID: "at-001", Status: "open"}}

	fs := &fakeSend{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "42", Text: "looks good"}},
	}
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
	if env := fs.envelopes[0]; env.InitiativeID != "at-001" {
		t.Errorf("envelope InitiativeID = %q, want at-001", env.InitiativeID)
	}
}

// TestRelay_NoDirectThreadFile_FallsThroughToInitiativePath verifies that
// when the Steward direct-message thread-ref file does not exist at all
// (direct channel never opened), a reply falls through to the existing
// initiative-reply path — the short-circuit never fires.
func TestRelay_NoDirectThreadFile_FallsThroughToInitiativePath(t *testing.T) {
	ctx := newRelayCtx(t) // t.TempDir() is empty — no direct-thread file present

	bdq := newFakeBDQuery()
	bdq.results["thread:42"] = []bd.Issue{{ID: "at-001", Status: "open"}}

	fs := &fakeSend{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "42", Text: "looks good"}},
	}
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
	if env := fs.envelopes[0]; env.InitiativeID != "at-001" {
		t.Errorf("envelope InitiativeID = %q, want at-001", env.InitiativeID)
	}
}
