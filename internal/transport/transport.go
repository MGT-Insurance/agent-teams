// Package transport defines the Transport interface and supporting types for
// delivering messages between the agent-teams system and human operators.
//
// A Transport carries outbound notifications from the DRI to a human, and
// routes inbound human replies back into the system. The transport does not
// know about initiatives — that mapping is the relay's responsibility.
//
// # Adding a new transport
//
// Implement the Transport interface in a sub-package, register it in For, and
// add a config key. Zero changes to the notify verb, relay, or DRI/skill code.
package transport

import (
	"fmt"
	"os"
	"strings"
)

// Sender pushes one message to the human.
type Sender interface {
	// Send pushes one message to the human. Returns a transport-native thread
	// handle (e.g. Telegram message_thread_id as string) so replies correlate
	// back. When OutboundMessage.ThreadRef == "" the transport opens a new
	// thread and returns its id.
	Send(msg OutboundMessage) (threadRef string, err error)
}

// Receiver long-polls the transport for inbound human replies.
type Receiver interface {
	// Receive blocks, invoking handler once per inbound human reply. Runs
	// until the process exits or a context cancellation is signalled via the
	// handler returning a permanent error. Each invocation of handler must
	// complete before Receive calls it again.
	Receive(handler func(Reply) error) error
}

// Transport is the full bidirectional contract.
type Transport interface {
	Sender
	Receiver
	// Name returns a short identifier for this transport, e.g. "telegram".
	Name() string
}

// OutboundMessage is a notification the DRI or gate fires sends to the human.
type OutboundMessage struct {
	InitiativeID string // our-side recipient handle (e.g. "at-00o")
	ThreadRef    string // transport thread to continue, or "" to open a new one
	Title        string // short subject; rendered as "[<InitiativeID>] <Title>"
	Body         string // the question / note text
}

// Reply is an inbound human response received by the transport.
//
// The transport fills ThreadRef from the platform (e.g. message_thread_id).
// InitiativeID is left empty by the transport; the relay fills it by looking
// up the "thread:<ThreadRef>" label on the initiative bead.
//
// When the transport receives a non-topic message (e.g. a message in the
// General topic), it emits a Reply with ThreadRef == "" so the relay can
// bounce it with "reply inside the initiative's topic."
type Reply struct {
	InitiativeID string // filled by relay, not transport
	ThreadRef    string // Telegram message_thread_id as string; "" for non-topic messages
	Text         string // the human's reply text
}

// factory is the function signature all transport constructors must satisfy.
type factory func(home string) (Transport, error)

// registry maps transport names to their factory functions.
var registry = map[string]factory{}

// RegisterTransport adds a factory under name. Called by sub-package init()
// functions. Panics on duplicate name (programming error).
func RegisterTransport(name string, f factory) {
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("transport: duplicate registration: %q", name))
	}
	registry[name] = f
}

// Enabled reports whether a usable transport is configured. It returns true iff
// the selected transport name is registered AND the factory call succeeds (which
// means all required config — e.g. Telegram token and chat-id — is resolvable).
//
// Callers that fire on a best-effort basis (e.g. the gate auto-notify) should
// check Enabled before calling For to avoid surfacing config-absent errors as
// warnings when the operator intentionally has not set up messaging.
func Enabled(home string) bool {
	name := selectedName(home)
	f, ok := registry[name]
	if !ok {
		return false
	}
	_, err := f(home)
	return err == nil
}

// For returns the Transport selected by the AGENT_TEAMS_TRANSPORT env var, or
// by the file ~/.agent-teams/transport (first line, trimmed). Defaults to
// "telegram" when no config is found. Returns an error if the selected name is
// not registered.
//
// home is the resolved workspace home (workspace.Home()).
func For(home string) (Transport, error) {
	name := selectedName(home)
	f, ok := registry[name]
	if !ok {
		var known []string
		for k := range registry {
			known = append(known, k)
		}
		return nil, fmt.Errorf("transport: unknown transport %q (registered: %s)", name, strings.Join(known, ", "))
	}
	return f(home)
}

// selectedName resolves the configured transport name.
// Priority: env AGENT_TEAMS_TRANSPORT → file <home>/transport → "telegram".
func selectedName(home string) string {
	if v := os.Getenv("AGENT_TEAMS_TRANSPORT"); v != "" {
		return strings.TrimSpace(v)
	}
	data, err := os.ReadFile(fmt.Sprintf("%s/transport", home))
	if err == nil {
		if name := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0]); name != "" {
			return name
		}
	}
	return "telegram"
}
