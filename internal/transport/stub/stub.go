//go:build e2e

// Package stub implements a no-network loopback transport for e2e testing.
//
// Activate with AGENT_TEAMS_TRANSPORT=stub and AGENT_TEAMS_STUB_DIR=<dir>.
//
// # Send
//
// Appends a JSON record to <dir>/sent.jsonl and returns a deterministic
// threadRef by incrementing <dir>/next-ref (an integer counter file).
//
// # Receive
//
// Reads reply files matching <dir>/reply-*.json. Each file must be a JSON
// object with a "text" string field and a "thread_ref" string field. Files
// are consumed (removed) in sorted order, the handler is invoked once per
// file, and Receive returns after draining all present files (non-blocking
// — no network wait).
//
// # Registration
//
// Factory constructs the stub transport; cmd/ateam registers it under the
// name "stub" via transport.RegisterTransport at boot, gated by the e2e
// build tag, so a normal `go build` / production binary does NOT include it.
package stub

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/transport"
)

// Factory constructs the stub transport for RegisterTransport, reading its
// directory from AGENT_TEAMS_STUB_DIR.
func Factory(home string) (transport.Transport, error) {
	dir := os.Getenv("AGENT_TEAMS_STUB_DIR")
	if dir == "" {
		return nil, fmt.Errorf("stub: AGENT_TEAMS_STUB_DIR not set")
	}
	return &Stub{dir: dir}, nil
}

// Stub is the loopback transport.
type Stub struct {
	dir string
}

// Name returns "stub".
func (s *Stub) Name() string { return "stub" }

// Send appends the outbound message to <dir>/sent.jsonl and returns a
// deterministic threadRef from a counter stored in <dir>/next-ref.
//
// The switch below RESOLVES A DESTINATION rather than merely recording the
// fields, mirroring telegram.Send's own structure case for case, and records
// it as "destination". Recording the raw fields alone would let a test assert
// that a reply carried a chat ref while staying blind to where the message
// actually went; the resolved destination is the thing a DM reply is supposed
// to get right, so it is what the test asserts on.
//
// ChatRef is the FIRST case, not an equal alternative to General, because
// transport.OutboundMessage.ChatRef makes it beat General when both are set —
// the opposite of the General-vs-ThreadRef precedence, deliberately, since
// downgrading a reply meant for one private conversation into the shared
// channel is a data-leak-shaped failure rather than a routing preference.
// Ordering it structurally (first case wins) is why a message that somehow
// arrives with both set still lands in the private chat.
//
// Neither of those two cases models a forum topic: thread_ref is "" and the
// next-ref counter is not bumped. That mirrors the Telegram transport in both
// cases — a General post opens no topic, and a 1:1 chat has no topics to open.
func (s *Stub) Send(msg transport.OutboundMessage) (string, error) {
	threadRef := msg.ThreadRef
	var destination string
	switch {
	case msg.ChatRef != "":
		threadRef = ""
		destination = "chat:" + msg.ChatRef
	case msg.General:
		threadRef = ""
		destination = "general"
	default:
		if threadRef == "" {
			ref, err := s.nextRef()
			if err != nil {
				return "", fmt.Errorf("stub: nextRef: %w", err)
			}
			threadRef = ref
		}
		destination = "topic:" + threadRef
	}

	record := map[string]any{
		"initiative_id": msg.InitiativeID,
		"thread_ref":    threadRef,
		"title":         msg.Title,
		"body":          msg.Body,
		"chat_ref":      msg.ChatRef,
		"general":       msg.General,
		"destination":   destination,
	}
	data, err := json.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("stub: marshal sent record: %w", err)
	}

	f, err := os.OpenFile(filepath.Join(s.dir, "sent.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("stub: open sent.jsonl: %w", err)
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%s\n", data); err != nil {
		return "", fmt.Errorf("stub: write sent.jsonl: %w", err)
	}

	return threadRef, nil
}

// Receive drains reply-*.json files from <dir>, calling handler once per
// file. Removes each file after a successful handler call. Returns after
// all present files are consumed (non-blocking — no network long-poll).
//
// Optional "mentions" ([]string) and "mentions_self" (bool) fields drive the
// relay's @mention routing rules in e2e tests. The stub has no token/getMe,
// so per-machine bot identity is modeled by each reply file setting
// mentions_self directly rather than being derived from mentions.
//
// Optional "direct" (bool) and "message_ref" (string) model a 1:1 DM: the
// stub has no chat types either, so a reply file declares Direct itself the
// same way it declares mentions_self. message_ref is passed through verbatim
// — it is transport-opaque above this layer, and the Telegram transport's
// composite "<chat_id>:<message_id>" shape is just a string here.
func (s *Stub) Receive(handler func(transport.Reply) error) error {
	matches, err := filepath.Glob(filepath.Join(s.dir, "reply-*.json"))
	if err != nil {
		return fmt.Errorf("stub: glob reply files: %w", err)
	}
	sort.Strings(matches)

	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("stub: read %s: %w", path, err)
		}

		var r struct {
			ThreadRef    string   `json:"thread_ref"`
			Text         string   `json:"text"`
			Mentions     []string `json:"mentions"`
			MentionsSelf bool     `json:"mentions_self"`
			Direct       bool     `json:"direct"`
			MessageRef   string   `json:"message_ref"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return fmt.Errorf("stub: parse %s: %w", path, err)
		}

		reply := transport.Reply{
			ThreadRef:    r.ThreadRef,
			Text:         r.Text,
			Mentions:     r.Mentions,
			MentionsSelf: r.MentionsSelf,
			Direct:       r.Direct,
			MessageRef:   r.MessageRef,
		}

		if err := handler(reply); err != nil {
			return err
		}

		// Consume the file so re-runs don't replay it.
		os.Remove(path)
	}

	return nil
}

// nextRef atomically bumps the counter in <dir>/next-ref and returns the
// previous value as a string (starting from "1").
func (s *Stub) nextRef() (string, error) {
	path := filepath.Join(s.dir, "next-ref")

	// Read current counter (default 0 if absent).
	cur := 0
	if data, err := os.ReadFile(path); err == nil {
		n, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if parseErr == nil {
			cur = n
		}
	}

	next := cur + 1
	if err := os.WriteFile(path, []byte(strconv.Itoa(next)), 0o644); err != nil {
		return "", err
	}

	return strconv.Itoa(next), nil
}
