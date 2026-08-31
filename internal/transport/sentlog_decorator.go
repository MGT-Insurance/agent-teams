package transport

import (
	"fmt"
	"os"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/sentlog"
)

// loggingTransport wraps a Transport with an append-only audit trail of
// every outbound Send (agent-teams-48dh, contract agent-teams-48dh.1 §1).
// Constructed only by For — Enabled's config probe never wraps. Receive and
// Name pass straight through unchanged; this is outbound-only for v1.
type loggingTransport struct {
	inner Transport
	home  string
}

// newLoggingTransport wraps inner so every Send is recorded to
// sentlog.Path(home) in addition to being delivered.
func newLoggingTransport(inner Transport, home string) *loggingTransport {
	return &loggingTransport{inner: inner, home: home}
}

// Name passes straight through to the wrapped transport.
func (l *loggingTransport) Name() string { return l.inner.Name() }

// Unwrap exposes the wrapped transport so optional capabilities the decorator
// does not implement (topicCloser, relayAcker) remain discoverable through it
// via Capability. Without this, wrapping silently strips every capability
// outside Name/Send/Receive (agent-teams-48dh.19).
func (l *loggingTransport) Unwrap() Transport { return l.inner }

// Receive passes straight through to the wrapped transport — outbound only
// for v1 (contract §1).
func (l *loggingTransport) Receive(handler func(Reply) error) error {
	return l.inner.Receive(handler)
}

// Send calls the inner Send, then appends one sentlog record built from msg
// plus the returned threadRef and outcome/error.
//
// Contract §6: writing the record must NEVER change the outcome of a send.
// The inner Send's (threadRef, err) is returned verbatim regardless of
// whether recording below succeeds — a message that reached Eric must not
// be reported as failed because the audit log was unwritable.
func (l *loggingTransport) Send(msg OutboundMessage) (string, error) {
	threadRef, sendErr := l.inner.Send(msg)
	l.record(msg, threadRef, sendErr)
	return threadRef, sendErr
}

// record builds and appends the sentlog.Record for one Send call. Never
// returns an error to the caller — a log-write failure is warned to stderr
// and swallowed (contract §6).
func (l *loggingTransport) record(msg OutboundMessage, threadRef string, sendErr error) {
	sender := msg.Sender
	if !sender.Known() {
		fmt.Fprintf(os.Stderr, "transport: warning: message sent with undeclared/unrecognised sender %q — recording as %s\n", msg.Sender, sentlog.KindUndeclared)
		sender = sentlog.KindUndeclared
	}

	// §2.1: the ref Send actually opened/used wins when non-empty; otherwise
	// fall back to what the caller asked for (possibly "" — a new topic that
	// failed before anything was returned, or a General-channel send that
	// never uses a thread at all).
	ref := threadRef
	if ref == "" {
		ref = msg.ThreadRef
	}

	rec := sentlog.Derive(l.home)
	rec.Timestamp = time.Now().UTC().Format(time.RFC3339)
	rec.Sender = sender
	rec.Transport = l.inner.Name()
	rec.Initiative = msg.InitiativeID
	rec.ThreadRef = ref
	rec.ChatRef = msg.ChatRef
	rec.General = msg.General
	rec.Title = msg.Title
	rec.Body = msg.Body
	rec.Image = msg.ImagePath
	if sendErr != nil {
		rec.Outcome = sentlog.OutcomeFailed
		rec.Error = sentlog.RedactError(sendErr)
	} else {
		rec.Outcome = sentlog.OutcomeSent
	}

	if err := sentlog.Append(l.home, rec); err != nil {
		fmt.Fprintf(os.Stderr, "transport: warning: could not append sent-log record (send outcome unaffected): %v\n", err)
	}
}
