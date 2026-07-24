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
