package transport

import (
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/sentlog"
)

// forTestTransport is a minimal registered Transport used to exercise the real
// For — including whatever decorator For installs around it.
type forTestTransport struct{}

func (forTestTransport) Name() string { return "fortest" }

func (forTestTransport) Send(msg OutboundMessage) (string, error) { return "ref-for", nil }

func (forTestTransport) Receive(handler func(Reply) error) error { return nil }

func init() {
	RegisterTransport("fortest", func(string) (Transport, error) { return forTestTransport{}, nil })
}

// TestForInstallsTheLoggingDecorator pins that For WRAPS at all. Nothing else
// in the Go suite does: replacing the wrap with `return t, nil` left
// go test ./internal/... entirely green, so the decorator could be deleted
// without a single failure (agent-teams-48dh.19). Asserted through behaviour
// rather than through Unwrap, so this case pins the wrap and only the wrap.
func TestForInstallsTheLoggingDecorator(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENT_TEAMS_TRANSPORT", "fortest")

	tr, err := For(home)
	if err != nil {
		t.Fatalf("For: %v", err)
	}

	ref, err := tr.Send(OutboundMessage{
		InitiativeID: "at-test",
		Title:        "t",
		Body:         "b",
		Sender:       sentlog.KindNotify,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if ref != "ref-for" {
		t.Fatalf("ref = %q, want ref-for (the inner transport's result must pass through)", ref)
	}

	// A sent-log record exists only if For wrapped the transport.
	rec := lastRecord(t, home)
	if rec["initiative"] != "at-test" {
		t.Fatalf("initiative = %v, want at-test", rec["initiative"])
	}
	if rec["sender"] != string(sentlog.KindNotify) {
		t.Fatalf("sender = %v, want %q", rec["sender"], sentlog.KindNotify)
	}
}

// senderCap is a local Send-shaped capability interface. Send is on every
// Transport's method set, so it is itself discoverable through Capability —
// which means BOTH the logging decorator For installs and the inner
// transport it wraps satisfy senderCap. This test pins WHICH one Capability
// returns (agent-teams-48dh.29).
type senderCap interface {
	Send(msg OutboundMessage) (string, error)
}

// TestCapabilityReturnsOutermostForSendShapedInterface pins that Capability
// walks OUTERMOST-FIRST: it asserts before it unwraps. Asserted
// behaviourally rather than by identity — a Send through the value
// Capability returns must append a sentlog record, which only the logging
// decorator does. If the walk is ever reversed to unwrap-then-assert, this
// same lookup would return the inner transport instead, sends through it
// would reach Eric with nothing written to sent.jsonl, and this test must
// catch that before it ships (capability.go's MIRROR RISK comment).
func TestCapabilityReturnsOutermostForSendShapedInterface(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENT_TEAMS_TRANSPORT", "fortest")

	tr, err := For(home)
	if err != nil {
		t.Fatalf("For: %v", err)
	}

	sc, ok := Capability[senderCap](tr)
	if !ok {
		t.Fatalf("Capability[senderCap] reported absent; Send is on every Transport's method set so it must be found")
	}

	if _, err := sc.Send(OutboundMessage{
		InitiativeID: "at-test",
		Title:        "t",
		Body:         "b",
		Sender:       sentlog.KindNotify,
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// A sent-log record exists only if Capability returned the logging
	// decorator, not the inner transport.
	rec := lastRecord(t, home)
	if rec["initiative"] != "at-test" {
		t.Fatalf("Capability[senderCap] did not return the logging decorator: no sentlog record for this send (rec = %v)", rec)
	}
}
