package transport

// unwrapper is implemented by any decorator that wraps another Transport —
// today only the logging decorator For installs. Capability walks this chain.
type unwrapper interface {
	Unwrap() Transport
}

// Capability discovers an OPTIONAL transport capability T on t, looking
// THROUGH any decorator layers For has installed.
//
// Optional capabilities (e.g. the verbs package's topicCloser / relayAcker,
// implemented by *telegram.Telegram) deliberately live outside the Transport
// interface, which stays capability-minimal. Call sites discover them by type
// assertion and skip when absent. A bare `t.(T)` breaks the moment For wraps
// the transport in a decorator that implements only Name/Send/Receive: the
// assertion fails silently and the capability is quietly lost
// (agent-teams-48dh.19). Capability walks the Unwrap chain instead, so
// discovery is structurally total — a new optional capability is found with no
// change to any decorator — while genuine absence stays truthfully absent, and
// the skip-if-absent branch at each call site stays real.
//
// Executing a discovered capability runs it on the inner transport, bypassing
// the decorator. That is correct for the capabilities that exist today:
// CloseTopic and Ack are not Sends — neither constructs an OutboundMessage —
// so there is nothing for the sent-log to record. The decorator stays honest;
// it logs Sends, which is the whole contract (agent-teams-48dh.1 §1,
// outbound-only).
//
// MIRROR RISK: a future optional capability that DOES emit a user-visible
// message would, discovered this way, bypass the sent-log. The backstop
// already exists — the textual build gate (tests/sent-log.test.sh case7/8)
// pins the count of non-test OutboundMessage literals at exactly 6, so any
// new send path trips it and forces the decision explicitly rather than
// losing the record silently. (Written unqualified on purpose: the gate
// greps for the package-qualified literal, and a comment must not inflate
// its count.)
func Capability[T any](t Transport) (T, bool) {
	for t != nil {
		if c, ok := t.(T); ok {
			return c, true
		}
		u, ok := t.(unwrapper)
		if !ok {
			break
		}
		t = u.Unwrap()
	}
	var zero T
	return zero, false
}
