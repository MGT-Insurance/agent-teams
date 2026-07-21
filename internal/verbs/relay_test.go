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

// ── ownership-gating default fakes (agent-teams-5y8a.5) ─────────────────────
//
// alwaysClaimsLocally / alwaysFallbackResponder / neverKnownStewardTopic are
// the behavior-preserving defaults for the three new relay-gating seams
// (claimsLocally, isFallbackResponder, knownStewardTopic) wired into every
// existing relayKong literal below: those tests predate multi-machine
// gating and assume single-machine behavior — this machine claims every
// tied initiative, is always the fallback responder for untied traffic, and
// never sees a peer steward topic. Tests that exercise the gating itself
// (below the existing suite) override the relevant one.
func alwaysClaimsLocally(bd.Issue) bool                { return true }
func alwaysFallbackResponder(*cli.Context) bool        { return true }
func neverKnownStewardTopic(*cli.Context, string) bool { return false }

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
		enabled:             func(string) bool { return false },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             newFakeBDQuery().query,
		send:                (&fakeSend{}).send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
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
		enabled:             func(string) bool { return false },
		transportFor:        func(string) (transport.Transport, error) { return &relayFakeTransport{}, nil },
		bdQuery:             newFakeBDQuery().query,
		send:                (&fakeSend{}).send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
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
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             bdq.query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
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
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             newFakeBDQuery().query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: func(*cli.Context) bool { return false },
		knownStewardTopic:   neverKnownStewardTopic,
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

// ── handler: unmapped thread → routed to steward as unrouted ─────────────────

// TestRelay_UnmappedThread_Skipped verifies that a thread with no OPEN and no
// CLOSED initiative match (agent-teams-8beo.2) is routed to the Steward as a
// steward-unrouted envelope instead of being silently dropped, while the
// existing "no open initiative" diagnostic log is still emitted.
func TestRelay_UnmappedThread_Skipped(t *testing.T) {
	fs := &fakeSendCapture{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "99", Text: "reply"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             newFakeBDQuery().query, // returns empty for "thread:99"
		bdQueryClosed:       newFakeBDQuery().query, // no closed match either — safety net finds nothing
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("expected exactly 1 send call (routed as unrouted), got %d", len(fs.calls))
	}
	env, ok := ParseStewardUnroutedEnvelope(fs.bodies[0])
	if !ok {
		t.Fatalf("send file contents not a well-formed steward-unrouted envelope: %q", fs.bodies[0])
	}
	if env.ThreadRef != "99" {
		t.Errorf("envelope ThreadRef = %q, want %q", env.ThreadRef, "99")
	}
	if env.Reason == "" {
		t.Error("envelope Reason must be non-empty")
	}
	if env.Body != "reply" {
		t.Errorf("envelope Body = %q, want %q", env.Body, "reply")
	}
	if !strings.Contains(relayStderr(ctx), "no open initiative") {
		t.Errorf("expected 'no open initiative' in stderr, got: %q", relayStderr(ctx))
	}
}

// ── handler: closed-initiative safety net (agent-teams-7dup.2) ───────────────

// TestRelay_ClosedInitiativeThread_RoutesToSteward verifies the case-0
// safety net: when bdQuery finds zero OPEN initiatives for the reply's
// thread label but bdQueryClosed finds exactly one CLOSED initiative
// carrying it, the reply is routed to the Steward as a
// steward-closed-initiative envelope instead of being silently dropped, and
// the generic "no open initiative" skip log is suppressed.
func TestRelay_ClosedInitiativeThread_RoutesToSteward(t *testing.T) {
	bdq := newFakeBDQuery() // no open matches for "thread:50"
	bdqClosed := newFakeBDQuery()
	bdqClosed.results["thread:50"] = []bd.Issue{{ID: "at-050", Status: "closed"}}

	fs := &fakeSendCapture{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "50", Text: "still relevant?"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             bdq.query,
		bdQueryClosed:       bdqClosed.query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
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
// matches (like 0 matches) fall through to the closed-initiative safety
// net's failure path, and (agent-teams-8beo.2) are routed to the Steward as
// a steward-unrouted envelope carrying an "ambiguous: N closed initiatives"
// reason instead of being silently dropped.
func TestRelay_AmbiguousClosedInitiativeThread_Skipped(t *testing.T) {
	bdq := newFakeBDQuery()
	bdqClosed := newFakeBDQuery()
	bdqClosed.results["thread:51"] = []bd.Issue{
		{ID: "at-051", Status: "closed"},
		{ID: "at-052", Status: "closed"},
	}
	fs := &fakeSendCapture{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "51", Text: "reply"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             bdq.query,
		bdQueryClosed:       bdqClosed.query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("expected exactly 1 send call (routed as unrouted), got %d", len(fs.calls))
	}
	env, ok := ParseStewardUnroutedEnvelope(fs.bodies[0])
	if !ok {
		t.Fatalf("send file contents not a well-formed steward-unrouted envelope: %q", fs.bodies[0])
	}
	if env.ThreadRef != "51" {
		t.Errorf("envelope ThreadRef = %q, want %q", env.ThreadRef, "51")
	}
	if !strings.Contains(env.Reason, "ambiguous: 2 closed initiatives") {
		t.Errorf("envelope Reason = %q, want it to mention ambiguous: 2 closed initiatives", env.Reason)
	}
	if !strings.Contains(relayStderr(ctx), "no open initiative") {
		t.Errorf("expected 'no open initiative' skip log, stderr: %q", relayStderr(ctx))
	}
}

// TestRelay_ClosedInitiativeQueryError_Skipped verifies that a bdQueryClosed
// error falls through to the closed-initiative safety net's failure path,
// and (agent-teams-8beo.2) is routed to the Steward as a steward-unrouted
// envelope carrying a "bd query error: ..." reason instead of aborting the
// relay loop or being silently dropped.
func TestRelay_ClosedInitiativeQueryError_Skipped(t *testing.T) {
	bdq := newFakeBDQuery()
	bdqClosed := newFakeBDQuery()
	bdqClosed.err["thread:52"] = fmt.Errorf("bd timeout")
	fs := &fakeSendCapture{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "52", Text: "reply"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             bdq.query,
		bdQueryClosed:       bdqClosed.query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("expected exactly 1 send call (routed as unrouted), got %d", len(fs.calls))
	}
	env, ok := ParseStewardUnroutedEnvelope(fs.bodies[0])
	if !ok {
		t.Fatalf("send file contents not a well-formed steward-unrouted envelope: %q", fs.bodies[0])
	}
	if env.ThreadRef != "52" {
		t.Errorf("envelope ThreadRef = %q, want %q", env.ThreadRef, "52")
	}
	if !strings.Contains(env.Reason, "bd query error") {
		t.Errorf("envelope Reason = %q, want it to mention bd query error", env.Reason)
	}
	if !strings.Contains(relayStderr(ctx), "no open initiative") {
		t.Errorf("expected fallback 'no open initiative' skip log, stderr: %q", relayStderr(ctx))
	}
}

// ── handler: ambiguous thread → routed to steward as unrouted ────────────────

// TestRelay_AmbiguousThread_Skipped verifies that 2+ OPEN initiatives sharing
// a thread label (agent-teams-8beo.2) are routed to the Steward as a
// steward-unrouted envelope carrying an "ambiguous: N open initiatives"
// reason instead of being silently dropped, while the existing "ambiguous"
// diagnostic log is still emitted.
func TestRelay_AmbiguousThread_Skipped(t *testing.T) {
	bdq := newFakeBDQuery()
	bdq.results["thread:7"] = []bd.Issue{
		{ID: "at-001", Status: "open"},
		{ID: "at-002", Status: "open"},
	}
	fs := &fakeSendCapture{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "7", Text: "reply"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             bdq.query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("expected exactly 1 send call (routed as unrouted), got %d", len(fs.calls))
	}
	env, ok := ParseStewardUnroutedEnvelope(fs.bodies[0])
	if !ok {
		t.Fatalf("send file contents not a well-formed steward-unrouted envelope: %q", fs.bodies[0])
	}
	if env.ThreadRef != "7" {
		t.Errorf("envelope ThreadRef = %q, want %q", env.ThreadRef, "7")
	}
	if !strings.Contains(env.Reason, "ambiguous: 2 open initiatives") {
		t.Errorf("envelope Reason = %q, want it to mention ambiguous: 2 open initiatives", env.Reason)
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
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             bdq.query,
		send:                sendFn,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
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

// ── handler: bd query error → routed to steward as unrouted, loop continues ──

// TestRelay_BDQueryError_RoutesToSteward verifies that a bd query error in
// handleReply's top-level lookup (agent-teams-8beo.3) is routed to the
// Steward as a steward-unrouted envelope carrying a "bd query error: ..."
// reason, instead of being silently dropped — while a second, successful
// reply in the same batch is still routed normally via a steward-reply
// envelope. Renamed from TestRelay_BDQueryError_SkipsReply, which locked in
// the prior silent-drop behavior this fix closes.
func TestRelay_BDQueryError_RoutesToSteward(t *testing.T) {
	bdq := newFakeBDQuery()
	bdq.err["thread:5"] = fmt.Errorf("bd timeout")
	bdq.results["thread:6"] = []bd.Issue{{ID: "at-006", Status: "open"}}

	fs := &fakeSendCapture{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{
			{ThreadRef: "5", Text: "bad"},
			{ThreadRef: "6", Text: "good"},
		},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             bdq.query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("bd error must not abort loop, got: %v", err)
	}
	if len(fs.calls) != 2 {
		t.Fatalf("expected exactly 2 send calls (errored reply routed as unrouted, successful reply routed normally), got %d", len(fs.calls))
	}

	unrouted, ok := ParseStewardUnroutedEnvelope(fs.bodies[0])
	if !ok {
		t.Fatalf("first send file contents not a well-formed steward-unrouted envelope: %q", fs.bodies[0])
	}
	if unrouted.ThreadRef != "5" {
		t.Errorf("unrouted envelope ThreadRef = %q, want %q", unrouted.ThreadRef, "5")
	}
	if !strings.Contains(unrouted.Reason, "bd query error") {
		t.Errorf("unrouted envelope Reason = %q, want it to mention bd query error", unrouted.Reason)
	}
	if unrouted.Body != "bad" {
		t.Errorf("unrouted envelope Body = %q, want %q", unrouted.Body, "bad")
	}

	reply, ok := ParseStewardReplyEnvelope(fs.bodies[1])
	if !ok {
		t.Fatalf("second send file contents not a well-formed steward-reply envelope: %q", fs.bodies[1])
	}
	if reply.InitiativeID != "at-006" {
		t.Errorf("reply envelope InitiativeID = %q, want %q", reply.InitiativeID, "at-006")
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
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
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
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             bdq.query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
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
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             bdq.query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
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

// ── handler: briefing-channel short-circuit ───────────────────────────────────

// TestRelay_BriefingThread_RoutesToSteward verifies that a reply whose thread
// ref matches the persisted Steward briefing-channel thread ref (contract:
// StewardBriefingThreadPath) is routed to the Steward via a
// steward-briefing-reply envelope, bypassing the bd initiative lookup
// entirely.
func TestRelay_BriefingThread_RoutesToSteward(t *testing.T) {
	ctx := newRelayCtx(t)
	if err := writeThreadRefFile(StewardBriefingThreadPath(ctx), "briefing-5"); err != nil {
		t.Fatalf("seed briefing thread ref: %v", err)
	}

	bdQueryCalled := false
	fs := &fakeSendCapture{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "briefing-5", Text: "what's the status on at-001?"}},
	}

	cmd := &relayKong{
		enabled:      func(string) bool { return true },
		transportFor: func(string) (transport.Transport, error) { return ft, nil },
		bdQuery: func(home, label string) ([]bd.Issue, error) {
			bdQueryCalled = true
			return nil, nil
		},
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bdQueryCalled {
		t.Error("bd query must not be called for a briefing-channel message")
	}
	if len(fs.calls) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(fs.calls))
	}
	env, ok := ParseStewardBriefingReplyEnvelope(fs.bodies[0])
	if !ok {
		t.Fatalf("send file contents not a well-formed steward-briefing-reply envelope: %q", fs.bodies[0])
	}
	if env.Body != "what's the status on at-001?" {
		t.Errorf("envelope Body = %q, want %q", env.Body, "what's the status on at-001?")
	}
}

// TestRelay_BriefingThread_NonMatchingThreadRef_TakesInitiativePath verifies
// that when a briefing-channel thread ref IS persisted, a reply whose
// ThreadRef does not match it still takes the existing initiative-reply
// path (bd lookup + steward-reply envelope), not the briefing short-circuit.
func TestRelay_BriefingThread_NonMatchingThreadRef_TakesInitiativePath(t *testing.T) {
	ctx := newRelayCtx(t)
	if err := writeThreadRefFile(StewardBriefingThreadPath(ctx), "briefing-5"); err != nil {
		t.Fatalf("seed briefing thread ref: %v", err)
	}

	bdq := newFakeBDQuery()
	bdq.results["thread:42"] = []bd.Issue{{ID: "at-001", Status: "open"}}

	fs := &fakeSend{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "42", Text: "looks good"}},
	}
	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             bdq.query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
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

// TestRelay_NoBriefingThreadFile_FallsThroughToInitiativePath verifies that
// when the Steward briefing-channel thread-ref file does not exist at all
// (no briefing ever posted), a reply falls through to the existing
// initiative-reply path — the short-circuit never fires.
func TestRelay_NoBriefingThreadFile_FallsThroughToInitiativePath(t *testing.T) {
	ctx := newRelayCtx(t) // t.TempDir() is empty — no briefing-thread file present

	bdq := newFakeBDQuery()
	bdq.results["thread:42"] = []bd.Issue{{ID: "at-001", Status: "open"}}

	fs := &fakeSend{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "42", Text: "looks good"}},
	}
	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             bdq.query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
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

// ── handler: multi-machine ownership gating (agent-teams-5y8a.5) ────────────

// TestRelay_TiedReply_NotClaimedLocally_Skipped verifies the tied-reply gate:
// when a reply's thread resolves to exactly one open initiative but
// claimsLocally reports THIS machine does not hold that initiative's
// checkout, the reply is skipped rather than routed — only the owning
// machine's relay forwards a tied reply.
func TestRelay_TiedReply_NotClaimedLocally_Skipped(t *testing.T) {
	bdq := newFakeBDQuery()
	bdq.results["thread:42"] = []bd.Issue{{ID: "at-001", Status: "open"}}

	fs := &fakeSend{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "42", Text: "looks good"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             bdq.query,
		send:                fs.send,
		claimsLocally:       func(bd.Issue) bool { return false },
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 0 {
		t.Errorf("expected no send calls (not claimed locally), got %d", len(fs.calls))
	}
	if !strings.Contains(relayStderr(ctx), "not claimed locally") {
		t.Errorf("expected 'not claimed locally' in stderr, got: %q", relayStderr(ctx))
	}
}

// TestRelay_UntiedReply_NotFallbackResponder_Skipped verifies the untied
// catch-all gate: when a reply's thread has no open (or closed) initiative
// match and isFallbackResponder reports THIS machine is not the designated
// fallback responder, the reply is skipped — not routed to the Steward —
// so only the fallback machine forwards untied traffic.
func TestRelay_UntiedReply_NotFallbackResponder_Skipped(t *testing.T) {
	fs := &fakeSendCapture{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "99", Text: "reply"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             newFakeBDQuery().query, // no open match for "thread:99"
		bdQueryClosed:       newFakeBDQuery().query, // no closed match either
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: func(*cli.Context) bool { return false },
		knownStewardTopic:   neverKnownStewardTopic,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 0 {
		t.Errorf("expected no send calls (not fallback responder), got %d", len(fs.calls))
	}
	if !strings.Contains(relayStderr(ctx), "not fallback responder") {
		t.Errorf("expected 'not fallback responder' in stderr, got: %q", relayStderr(ctx))
	}
}

// TestRelay_UntiedReply_FallbackResponder_RoutesUnrouted verifies that the
// designated fallback responder still routes untied traffic to the Steward
// as a steward-unrouted envelope — the gate above only suppresses
// non-fallback machines.
func TestRelay_UntiedReply_FallbackResponder_RoutesUnrouted(t *testing.T) {
	fs := &fakeSendCapture{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "99", Text: "reply"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             newFakeBDQuery().query,
		bdQueryClosed:       newFakeBDQuery().query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: func(*cli.Context) bool { return true },
		knownStewardTopic:   neverKnownStewardTopic,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("expected 1 send call (fallback responder routes untied reply), got %d", len(fs.calls))
	}
	env, ok := ParseStewardUnroutedEnvelope(fs.bodies[0])
	if !ok {
		t.Fatalf("send file contents not a well-formed steward-unrouted envelope: %q", fs.bodies[0])
	}
	if env.ThreadRef != "99" {
		t.Errorf("envelope ThreadRef = %q, want %q", env.ThreadRef, "99")
	}
}

// ── freshen-before-untied (agent-teams-6rru.10 Part B) ───────────────────────

// sequencedBDQuery returns a different (possibly erroring) result on each
// successive call for a configured label, tracking a call count — used to
// exercise freshenBeforeUntied, where the SAME label is queried twice
// (initial + freshen) and must resolve differently across the two calls.
// Any other label returns a zero-match result, matching newFakeBDQuery's
// default.
type sequencedBDQuery struct {
	label   string
	results [][]bd.Issue // results[i] is returned on the (i+1)th call for label
	errs    []error      // errs[i], if non-nil, is returned instead of results[i]
	calls   int
}

func (s *sequencedBDQuery) query(_, label string) ([]bd.Issue, error) {
	if label != s.label {
		return nil, nil
	}
	i := s.calls
	s.calls++
	if i < len(s.errs) && s.errs[i] != nil {
		return nil, s.errs[i]
	}
	if i >= len(s.results) {
		i = len(s.results) - 1
	}
	if i < 0 {
		return nil, nil
	}
	return s.results[i], nil
}

// TestRelay_UntiedReply_FreshenResolves_RoutesTied confirms the core Part-B
// fix: when the first query finds zero open matches but a freshen (dolt pull
// + one bounded-backoff re-query) resolves to exactly one, the reply is
// routed as tied — not strayed to the unrouted catch-all.
func TestRelay_UntiedReply_FreshenResolves_RoutesTied(t *testing.T) {
	sbdq := &sequencedBDQuery{
		label: "thread:83",
		results: [][]bd.Issue{
			nil,                              // first query: label not yet visible
			{{ID: "at-083", Status: "open"}}, // freshen re-query: now visible
		},
	}
	pullCalls := 0
	fs := &fakeSend{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "83", Text: "on it"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             sbdq.query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
		doltPull: func(home string) error {
			pullCalls++
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sbdq.calls != 2 {
		t.Fatalf("expected 2 bdQuery calls (initial + freshen), got %d", sbdq.calls)
	}
	if pullCalls != 1 {
		t.Errorf("expected exactly 1 doltPull call, got %d", pullCalls)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("expected exactly 1 send call (routed after freshen, not strayed), got %d", len(fs.calls))
	}
	if env := fs.envelopes[0]; env.InitiativeID != "at-083" {
		t.Errorf("envelope InitiativeID = %q, want at-083 (routed after freshen, not strayed)", env.InitiativeID)
	}
	if !strings.Contains(relayStderr(ctx), "freshen resolved") {
		t.Errorf("expected a 'freshen resolved' diagnostic on stderr, got: %q", relayStderr(ctx))
	}
}

// TestRelay_UntiedReply_FreshenStillZero_StraysOnce confirms existing
// behavior is preserved when freshen does NOT resolve the label: the reply
// is still strayed to the unrouted catch-all, and exactly once (not once per
// query attempt).
func TestRelay_UntiedReply_FreshenStillZero_StraysOnce(t *testing.T) {
	sbdq := &sequencedBDQuery{
		label:   "thread:71",
		results: [][]bd.Issue{nil, nil}, // still zero after freshen
	}
	pullCalls := 0
	fs := &fakeSendCapture{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "71", Text: "reply"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             sbdq.query,
		bdQueryClosed:       newFakeBDQuery().query, // no closed match either
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
		doltPull: func(home string) error {
			pullCalls++
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sbdq.calls != 2 {
		t.Fatalf("expected 2 bdQuery calls (initial + freshen), got %d", sbdq.calls)
	}
	if pullCalls != 1 {
		t.Errorf("expected exactly 1 doltPull call, got %d", pullCalls)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("expected exactly 1 send call (stray, unchanged behavior), got %d", len(fs.calls))
	}
	env, ok := ParseStewardUnroutedEnvelope(fs.bodies[0])
	if !ok {
		t.Fatalf("send file contents not a well-formed steward-unrouted envelope: %q", fs.bodies[0])
	}
	if env.ThreadRef != "71" {
		t.Errorf("envelope ThreadRef = %q, want %q", env.ThreadRef, "71")
	}
}

// TestRelay_TiedReply_FreshenNotInvoked confirms freshen is gated to the
// untied path only: an already-tied reply (exactly one open match on the
// first query) must never trigger a dolt pull or a second query.
func TestRelay_TiedReply_FreshenNotInvoked(t *testing.T) {
	bdq := newFakeBDQuery()
	bdq.results["thread:54"] = []bd.Issue{{ID: "at-054", Status: "open"}}

	fs := &fakeSend{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "54", Text: "reply"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             bdq.query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
		doltPull: func(home string) error {
			t.Fatal("doltPull must not be called for an already-tied reply (freshen is gated to the untied path only)")
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(fs.calls))
	}
	if env := fs.envelopes[0]; env.InitiativeID != "at-054" {
		t.Errorf("envelope InitiativeID = %q, want at-054", env.InitiativeID)
	}
}

// TestRelay_BDQueryError_FreshenResolves_RoutesTied confirms freshen also
// applies symmetrically to the bd-query-error branch (not just the
// zero-open-match branch): a query error on the first attempt, resolved by
// the freshen re-query, routes as tied rather than straying with the
// original "bd query error" reason.
func TestRelay_BDQueryError_FreshenResolves_RoutesTied(t *testing.T) {
	sbdq := &sequencedBDQuery{
		label: "thread:78",
		errs:  []error{fmt.Errorf("bd timeout")}, // first call errors
		results: [][]bd.Issue{
			nil,                              // unused: call 0 errors instead
			{{ID: "at-078", Status: "open"}}, // freshen re-query succeeds
		},
	}
	fs := &fakeSend{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "78", Text: "reply"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             sbdq.query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
		doltPull:            func(home string) error { return nil },
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sbdq.calls != 2 {
		t.Fatalf("expected 2 bdQuery calls (initial error + freshen), got %d", sbdq.calls)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("expected exactly 1 send call (routed after freshen, not strayed), got %d", len(fs.calls))
	}
	if env := fs.envelopes[0]; env.InitiativeID != "at-078" {
		t.Errorf("envelope InitiativeID = %q, want at-078", env.InitiativeID)
	}
}

// TestRelay_PeerStewardTopic_Skipped verifies the peer-topic gate: when a
// reply's thread ref is a KNOWN steward topic belonging to ANOTHER machine
// (knownStewardTopic true), the reply is skipped before the bd label query
// even runs — that peer's own relay already routes it locally.
func TestRelay_PeerStewardTopic_Skipped(t *testing.T) {
	bdQueryCalled := false
	fs := &fakeSendCapture{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "peer-briefing-9", Text: "reply in a peer's topic"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:      func(string) bool { return true },
		transportFor: func(string) (transport.Transport, error) { return ft, nil },
		bdQuery: func(home, label string) ([]bd.Issue, error) {
			bdQueryCalled = true
			return nil, nil
		},
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   func(*cli.Context, string) bool { return true },
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bdQueryCalled {
		t.Error("bd query must not be called for a known peer steward topic")
	}
	if len(fs.calls) != 0 {
		t.Errorf("expected no send calls (peer steward topic), got %d", len(fs.calls))
	}
	if !strings.Contains(relayStderr(ctx), "skipping peer steward topic") {
		t.Errorf("expected 'skipping peer steward topic' in stderr, got: %q", relayStderr(ctx))
	}
}

// TestRelay_NonTopic_FallbackResponder_RoutesUnrouted verifies the
// agent-teams-17xs.8 decision scoped to the fallback responder: a
// General-topic/DM reply (ThreadRef=="") is routed to the Steward as a
// steward-unrouted envelope when this machine is the designated fallback
// responder, instead of being silently dropped.
func TestRelay_NonTopic_FallbackResponder_RoutesUnrouted(t *testing.T) {
	fs := &fakeSendCapture{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "", Text: "reply in general"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             newFakeBDQuery().query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: func(*cli.Context) bool { return true },
		knownStewardTopic:   neverKnownStewardTopic,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("expected 1 send call (fallback responder routes non-topic message), got %d", len(fs.calls))
	}
	env, ok := ParseStewardUnroutedEnvelope(fs.bodies[0])
	if !ok {
		t.Fatalf("send file contents not a well-formed steward-unrouted envelope: %q", fs.bodies[0])
	}
	if env.Body != "reply in general" {
		t.Errorf("envelope Body = %q, want %q", env.Body, "reply in general")
	}
}

// TestRelay_NonTopic_NotFallbackResponder_Skipped verifies that a
// non-fallback machine keeps the original silent skip for a General-topic/DM
// reply — only the designated fallback responder forwards it.
func TestRelay_NonTopic_NotFallbackResponder_Skipped(t *testing.T) {
	fs := &fakeSend{}
	ft := &relayFakeTransport{
		replies: []transport.Reply{{ThreadRef: "", Text: "reply in general"}},
	}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             func(string) bool { return true },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             newFakeBDQuery().query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: func(*cli.Context) bool { return false },
		knownStewardTopic:   neverKnownStewardTopic,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs.calls) != 0 {
		t.Errorf("expected no send calls, got %d", len(fs.calls))
	}
	if !strings.Contains(relayStderr(ctx), "skipping non-topic message") {
		t.Errorf("expected 'skipping non-topic message' in stderr, got: %q", relayStderr(ctx))
	}
}
