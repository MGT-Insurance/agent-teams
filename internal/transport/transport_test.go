package transport

import (
	"strings"
	"testing"
)

// probeTG is a minimal Transport implementer, used only as a typed-nil
// pointer below (agent-teams-48dh.30). Its methods are never called: the
// point is that For must reject the nil before any method call happens.
type probeTG struct{}

func (*probeTG) Name() string                             { return "probe" }
func (*probeTG) Send(msg OutboundMessage) (string, error) { return "", nil }
func (*probeTG) Receive(handler func(Reply) error) error  { return nil }

// nilTypedTransportFactory mirrors the real-world shape that produces a
// typed nil: a constructor returning (*probeTG, error) forwarded through a
// factory signature of (Transport, error), with a nil pointer and a nil
// error (telegram.Factory forwards New(home, nil) the same way). The
// resulting Transport interface value is non-nil (it carries the *probeTG
// type) but holds no concrete value.
func nilTypedTransportFactory(string) (Transport, error) {
	var tg *probeTG
	return tg, nil
}

func init() {
	RegisterTransport("niltyped", nilTypedTransportFactory)
	RegisterTransport("nilinterface", func(string) (Transport, error) { return nil, nil })
}

// TestForRejectsTypedNilTransport pins that For returns an error — not a
// later panic — when a factory returns a non-nil Transport interface
// holding a nil concrete pointer. Before the isNilTransport guard, this
// typed nil passed straight through: Capability's `t != nil` (an interface
// comparison) does not catch it either, so a caller using the
// skip-if-absent branch on an optional capability would panic on the first
// method call instead of skipping (reachable via a factory that, like
// telegram.Factory, forwards a (*T, error) constructor return with a nil
// error).
func TestForRejectsTypedNilTransport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENT_TEAMS_TRANSPORT", "niltyped")

	tr, err := For(home)
	if err == nil {
		t.Fatalf("For returned a nil error for a typed-nil transport; want an error (tr = %#v)", tr)
	}
	if tr != nil {
		t.Fatalf("For returned a non-nil Transport alongside an error: %#v", tr)
	}
	if !strings.Contains(err.Error(), "nil transport") {
		t.Fatalf("error = %q, want it to mention a nil transport", err.Error())
	}
}

// TestForRejectsInterfaceNilTransport pins the simpler case: a factory that
// returns a genuinely nil Transport interface (not just a typed nil) is
// also rejected by the same guard.
func TestForRejectsInterfaceNilTransport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENT_TEAMS_TRANSPORT", "nilinterface")

	tr, err := For(home)
	if err == nil {
		t.Fatalf("For returned a nil error for a nil transport interface; want an error (tr = %#v)", tr)
	}
	if tr != nil {
		t.Fatalf("For returned a non-nil Transport alongside an error: %#v", tr)
	}
}
