// Tests for optional-capability discovery through the REAL transport.For
// (agent-teams-48dh.19).
//
// Every other capability test in this package injects a fake through the
// transportFor / relayTransportForFunc seam, so none of them ever sees what
// transport.For actually returns — which is how the logging decorator was
// able to strip CloseTopic and Ack from every production call site while the
// suite stayed green. These cases deliberately leave the seam alone and let
// the real For run: the fake goes in through the transport registry instead.
package verbs

import (
	"fmt"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/transport"
)

// capabilityFake implements Transport plus BOTH optional capabilities
// (topicCloser and relayAcker), so a test can prove each one survives the
// decorator transport.For installs.
type capabilityFake struct {
	replies    []transport.Reply
	sends      []transport.OutboundMessage
	closeCalls []string
	acked      []transport.Reply
}

func (f *capabilityFake) Name() string { return "captest-capable" }

func (f *capabilityFake) Send(msg transport.OutboundMessage) (string, error) {
	f.sends = append(f.sends, msg)
	return "999", nil
}

func (f *capabilityFake) Receive(handler func(transport.Reply) error) error {
	for _, r := range f.replies {
		if err := handler(r); err != nil {
			return err
		}
	}
	return nil
}

func (f *capabilityFake) CloseTopic(threadRef string) error {
	f.closeCalls = append(f.closeCalls, threadRef)
	return nil
}

func (f *capabilityFake) Ack(reply transport.Reply) error {
	f.acked = append(f.acked, reply)
	return nil
}

// plainCapabilityFake implements Transport and NOTHING else — the genuine
// "capability absent" case both call sites are written to skip.
type plainCapabilityFake struct {
	replies []transport.Reply
	sends   []transport.OutboundMessage
}

func (f *plainCapabilityFake) Name() string { return "captest-plain" }

func (f *plainCapabilityFake) Send(msg transport.OutboundMessage) (string, error) {
	f.sends = append(f.sends, msg)
	return "999", nil
}

func (f *plainCapabilityFake) Receive(handler func(transport.Reply) error) error {
	for _, r := range f.replies {
		if err := handler(r); err != nil {
			return err
		}
	}
	return nil
}

// The registry is process-global and RegisterTransport panics on a duplicate
// name, so both fakes are registered once per test binary and reset by each
// test. transport.For constructs through these factories.
var (
	capableTransport = &capabilityFake{}
	plainTransport   = &plainCapabilityFake{}
)

func init() {
	transport.RegisterTransport("captest-capable", func(string) (transport.Transport, error) {
		return capableTransport, nil
	})
	transport.RegisterTransport("captest-plain", func(string) (transport.Transport, error) {
		return plainTransport, nil
	})
}

// useRegisteredTransport points transport.For at name and gives the logging
// decorator a scratch workspace home to write its sent-log into.
func useRegisteredTransport(t *testing.T, name string) {
	t.Helper()
	t.Setenv("AGENT_TEAMS_TRANSPORT", name)
	t.Setenv("AGENT_TEAMS_HOME", t.TempDir())
}

// TestClose_CloseTopicSurvivesRealTransportFor is case (a) for the close verb:
// with transportFor left nil so sendCloseSignal resolves through the real
// transport.For, a registered transport that implements CloseTopic must still
// be discovered as a topicCloser. Fails if the decorator hides the capability.
func TestClose_CloseTopicSurvivesRealTransportFor(t *testing.T) {
	useRegisteredTransport(t, "captest-capable")
	*capableTransport = capabilityFake{}

	nbd := &closeSignalFakeBD{
		issue: bd.Issue{
			ID:     "at-00o",
			Title:  "my initiative",
			Labels: []string{"at-00o", "thread:999"},
		},
	}
	cmd := &closeKong{
		ID:                 "at-00o",
		runUpdateLocalMain: func(string) (string, error) { return "", fmt.Errorf("no repo") },
		// transportFor intentionally left nil — the real transport.For runs.
	}
	ctx, _, errBuf := newCloseSignalCtx(nbd)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(capableTransport.sends) != 1 {
		t.Fatalf("farewell Send calls = %d, want 1 (stderr: %q)", len(capableTransport.sends), errBuf.String())
	}
	if len(capableTransport.closeCalls) != 1 || capableTransport.closeCalls[0] != "999" {
		t.Fatalf("CloseTopic capability lost through transport.For: closeCalls = %v, want [999] (stderr: %q)", capableTransport.closeCalls, errBuf.String())
	}
}

// TestRelay_AckSurvivesRealTransportFor is case (a) for the relay verb:
// transportFor is the real transport.For (not a fake), so Run's relayAcker
// discovery sees exactly what production sees. Fails if the decorator hides
// the capability, which leaves c.ack nil and silently drops read receipts.
func TestRelay_AckSurvivesRealTransportFor(t *testing.T) {
	useRegisteredTransport(t, "captest-capable")
	*capableTransport = capabilityFake{
		replies: []transport.Reply{{ThreadRef: "42", Text: "looks good", MessageRef: "555"}},
	}

	bdq := newFakeBDQuery()
	bdq.results["thread:42"] = []bd.Issue{{ID: "at-001", Status: "open"}}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             transport.Enabled,
		transportFor:        transport.For,
		bdQuery:             bdq.query,
		send:                (&fakeSend{}).send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
		// ack left unset — Run must auto-wire it from the resolved transport.
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(capableTransport.acked) != 1 || capableTransport.acked[0].MessageRef != "555" {
		t.Fatalf("Ack capability lost through transport.For: acked = %+v, want one Ack with MessageRef 555 (stderr: %q)", capableTransport.acked, relayStderr(ctx))
	}
}

// TestClose_TopicCloserAbsent_ThroughRealTransportFor is case (c) for close:
// a registered transport with no CloseTopic must still report ABSENT through
// the real For, so the skip-if-absent branch stays exercised. Discovery that
// reports presence unconditionally fails here.
func TestClose_TopicCloserAbsent_ThroughRealTransportFor(t *testing.T) {
	useRegisteredTransport(t, "captest-plain")
	*plainTransport = plainCapabilityFake{}

	nbd := &closeSignalFakeBD{
		issue: bd.Issue{
			ID:     "at-00o",
			Title:  "my initiative",
			Labels: []string{"at-00o", "thread:999"},
		},
	}
	cmd := &closeKong{
		ID:                 "at-00o",
		runUpdateLocalMain: func(string) (string, error) { return "", fmt.Errorf("no repo") },
	}
	ctx, _, errBuf := newCloseSignalCtx(nbd)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(plainTransport.sends) != 1 {
		t.Fatalf("farewell Send calls = %d, want 1", len(plainTransport.sends))
	}
	if errBuf.String() != "" {
		t.Fatalf("expected a silent skip when the transport cannot close topics, got stderr %q", errBuf.String())
	}
}

// TestRelay_AckerAbsent_ThroughRealTransportFor is case (c) for relay: with a
// transport that has no Ack, forwarding still succeeds and no read receipt is
// attempted.
func TestRelay_AckerAbsent_ThroughRealTransportFor(t *testing.T) {
	useRegisteredTransport(t, "captest-plain")
	*plainTransport = plainCapabilityFake{
		replies: []transport.Reply{{ThreadRef: "42", Text: "looks good", MessageRef: "555"}},
	}

	bdq := newFakeBDQuery()
	bdq.results["thread:42"] = []bd.Issue{{ID: "at-001", Status: "open"}}
	fs := &fakeSend{}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		enabled:             transport.Enabled,
		transportFor:        transport.For,
		bdQuery:             bdq.query,
		send:                fs.send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(fs.calls) != 1 {
		t.Fatalf("expected forwarding to still succeed, got %d send calls", len(fs.calls))
	}
	if cmd.ack != nil {
		t.Fatalf("ack was wired from a transport that does not implement relayAcker")
	}
}
