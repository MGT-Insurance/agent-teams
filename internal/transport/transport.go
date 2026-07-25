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
	Title        string // short subject; rendering is transport-specific (e.g. telegram uses it as the forum topic name)
	Body         string // the question / note text
	// General, when true, posts to the transport's General channel instead of
	// a per-initiative thread: no topic is created, no thread_ref is used or
	// returned. Mutually exclusive in intent with ThreadRef, though a
	// transport should prefer General when both are set.
	General bool
	// ChatRef optionally targets a specific conversation instead of the
	// configured channel — the transport-opaque ref of an inbound message
	// (transport.Reply.MessageRef) whose conversation the reply belongs in.
	// Empty means the configured channel, which is every existing caller's
	// behavior. Opaque to every caller: only the owning transport parses it.
	//
	// Mutually exclusive in intent with General: a caller sets one or the
	// other, never both. If both are set anyway, the transport MUST prefer
	// ChatRef — the specific destination wins over the default channel. That
	// is deliberately the OPPOSITE of the General-vs-ThreadRef precedence
	// documented above, and the difference is the point: silently
	// downgrading a reply meant for one specific conversation into the
	// shared channel is a data-leak-shaped failure, not a routing
	// preference.
	ChatRef string
}

// Reply is an inbound human response received by the transport.
//
// The transport fills ThreadRef from the platform (e.g. message_thread_id).
// InitiativeID is left empty by the transport; the relay fills it by looking
// up the "thread:<ThreadRef>" label on the initiative bead.
//
// When the transport receives a non-topic message (e.g. a message in the
// General topic), it emits a Reply with ThreadRef == "" and makes no routing
// decision about it. The relay decides what such a message means, keying off
// the addressing fields below (MentionsSelf, Mentions, Direct); the
// transport's only obligation is to report those faithfully.
type Reply struct {
	InitiativeID string // filled by relay, not transport
	ThreadRef    string // Telegram message_thread_id as string; "" for non-topic messages
	Text         string // the human's reply text
	// Mentions holds every @-mentioned username in Text ("@" stripped,
	// lowercased). Transport-agnostic — no platform entity types leak here.
	Mentions []string
	// MentionsSelf is true iff this transport's own identity (e.g. the bot's
	// own @username) is among Mentions.
	MentionsSelf bool
	// Direct is true when this reply arrived in a 1:1 conversation with the
	// bot rather than a shared channel — a Telegram private-chat DM. In a 1:1
	// conversation there is no one else to address, so the message is
	// implicitly addressed to this bot: the relay treats Direct exactly as it
	// treats an explicit @mention of itself (relay.go rule 1), without
	// requiring the human to type one.
	Direct bool
	// MessageRef is an opaque, transport-native handle to THIS inbound
	// message. It serves two purposes: acking the message back (e.g. a read
	// receipt), and addressing an outbound reply into the conversation the
	// message came from (OutboundMessage.ChatRef). Empty when the transport
	// can't address individual messages.
	//
	// The handle may be COMPOSITE. A transport whose message ids are unique
	// only within a conversation is expected to encode the conversation into
	// the ref, because the id alone cannot address the message. No code
	// outside the owning transport may parse, split, or compare it —
	// Relay-opaque, so no relay knowledge enters this package and no relay
	// knowledge of the ref's shape leaks out of the transport.
	MessageRef string
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

// Frozen function signatures — implemented in the direct-chat track's own
// file, internal/transport/telegram/telegram.go (agent-teams-ncn5.2), which
// this contract does not own:
//
//	func (t *Telegram) allowsDirectChat(chatID int64) bool
//
// allowsDirectChat is THE single, isolated authorization decision point for
// 1:1 conversations, built the way isContentLess isolates the content-less
// policy in that same file: flipping the policy is a one-function edit.
//
// Its parameter is the chat id rather than a message, deliberately, because
// the same predicate must serve BOTH directions: inbound it decides whether a
// private-chat message is admitted at all (and so emitted as a Reply with
// Direct == true), outbound it validates the chat decoded from an
// OutboundMessage.ChatRef before the transport will send there. One function,
// one policy, both directions — so the two directions cannot drift apart.
//
// Only TWO of the four hardcoded Bot API destinations in that file become
// per-message. The asymmetry is deliberate; do NOT parameterize the other two
// for symmetry:
//
//   - setMessageReaction — per-message. Telegram message ids are unique only
//     within a chat, so acking against the configured chat id would fire the
//     read receipt at an unrelated message in the group.
//   - sendMessage — per-message. This is what puts a reply back into the
//     conversation it came from. For a private chat it must keep omitting
//     message_thread_id entirely, which the existing empty-thread-ref path
//     already does.
//   - createForumTopic — STAYS on the configured chat id. Forum topics exist
//     only in a supergroup with topics enabled; a topic in a private chat is
//     not a thing, so a per-message chat ref here would be meaningless.
//   - CloseTopic — STAYS on the configured chat id, same reason.
