// Package telegram implements the Transport interface using Telegram Bot API
// forum Topics (private supergroup with topics enabled).
//
// # One-time manual setup (human required)
//
// Eric creates the supergroup, enables Topics, adds the bot, and grants it
// admin with the can_manage_topics right. The bot cannot do this itself.
//
// # Config
//
// Bot token: env AGENT_TEAMS_TELEGRAM_TOKEN, or file ~/.agent-teams/telegram/token (mode 0600).
// Chat id:   env AGENT_TEAMS_TELEGRAM_CHAT_ID, or file ~/.agent-teams/telegram/chat-id (mode 0600).
//
// # Thread model
//
// One Telegram forum topic per initiative. Send with ThreadRef=="" opens a new
// topic via createForumTopic and returns its message_thread_id as threadRef.
// Subsequent sends pass that threadRef as message_thread_id to sendMessage.
//
// # Inbound
//
// getUpdates long-poll. Only messages where is_topic_message==true and the
// chat id matches the configured supergroup are delivered to the handler.
// Non-topic messages (General topic, DMs) emit a Reply{ThreadRef: ""} so the
// relay can bounce them.
package telegram

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/transport"
)

func init() {
	transport.RegisterTransport("telegram", func(home string) (transport.Transport, error) {
		return New(home, nil)
	})
}

// longPollTimeout is the getUpdates long-poll duration.
const longPollTimeout = 30

// Telegram implements transport.Transport via the Telegram Bot API.
type Telegram struct {
	token      string
	chatID     string
	httpClient httpDoer
	baseURL    string // overridable in tests; defaults to "https://api.telegram.org"
}

// httpDoer is the subset of *http.Client used by Telegram. Injected for tests.
type httpDoer interface {
	Get(url string) (*http.Response, error)
	PostForm(url string, data url.Values) (*http.Response, error)
}

// New constructs a Telegram transport. client may be nil (uses http.DefaultClient).
// home is the resolved workspace home (workspace.Home()).
func New(home string, client httpDoer) (*Telegram, error) {
	token, err := loadSecret(home, "AGENT_TEAMS_TELEGRAM_TOKEN", "telegram/token")
	if err != nil {
		return nil, fmt.Errorf("telegram: token: %w", err)
	}
	chatID, err := loadSecret(home, "AGENT_TEAMS_TELEGRAM_CHAT_ID", "telegram/chat-id")
	if err != nil {
		return nil, fmt.Errorf("telegram: chat-id: %w", err)
	}
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	return &Telegram{
		token:      token,
		chatID:     chatID,
		httpClient: client,
		baseURL:    "https://api.telegram.org",
	}, nil
}

// Name returns "telegram".
func (t *Telegram) Name() string { return "telegram" }

// Send delivers msg to the human. If msg.ThreadRef is "", a new forum topic is
// opened via createForumTopic and its id is returned as threadRef. Otherwise
// sendMessage is called with msg.ThreadRef as message_thread_id.
func (t *Telegram) Send(msg transport.OutboundMessage) (string, error) {
	threadRef := msg.ThreadRef

	if threadRef == "" {
		// Open a new forum topic named "[<InitiativeID>] <Title>".
		topicName := fmt.Sprintf("[%s] %s", msg.InitiativeID, msg.Title)
		id, err := t.createForumTopic(topicName)
		if err != nil {
			return "", fmt.Errorf("telegram: createForumTopic: %w", err)
		}
		threadRef = id
	}

	// Build the message body. On reuse of an existing thread the title is
	// included as a header so replies stay scannable.
	body := msg.Body
	if msg.ThreadRef != "" {
		body = fmt.Sprintf("[%s] %s\n\n%s", msg.InitiativeID, msg.Title, msg.Body)
	}

	if err := t.sendMessage(threadRef, body); err != nil {
		return "", fmt.Errorf("telegram: sendMessage: %w", err)
	}
	return threadRef, nil
}

// Receive long-polls Telegram for updates, invoking handler for each inbound
// message. Messages where is_topic_message==false or the chat id does not
// match the configured supergroup are not passed to handler — except that
// non-topic messages from the configured chat are emitted as Reply{ThreadRef:""}
// so the relay can bounce them with "reply inside the initiative's topic."
//
// Receive runs until handler returns a non-nil error, which is propagated.
func (t *Telegram) Receive(handler func(transport.Reply) error) error {
	var offset int
	for {
		updates, err := t.getUpdates(offset)
		if err != nil {
			// Transient network errors: log and retry.
			_, _ = fmt.Fprintf(os.Stderr, "telegram: getUpdates: %v\n", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, upd := range updates {
			offset = upd.UpdateID + 1

			msg := upd.Message
			if msg == nil {
				continue
			}

			// Reject messages from other chats.
			chatIDStr := strconv.FormatInt(msg.Chat.ID, 10)
			if chatIDStr != t.chatID {
				continue
			}

			var reply transport.Reply
			reply.Text = msg.Text

			if msg.IsTopicMessage && msg.MessageThreadID != 0 {
				reply.ThreadRef = strconv.Itoa(msg.MessageThreadID)
			}
			// ThreadRef == "" for non-topic messages; relay bounces these.

			if err := handler(reply); err != nil {
				return err
			}
		}
	}
}

// ── Telegram Bot API calls ────────────────────────────────────────────────────

// apiURL constructs the Bot API endpoint URL for a method.
func (t *Telegram) apiURL(method string) string {
	// Token is embedded in the URL path — never log the URL.
	return fmt.Sprintf("%s/bot%s/%s", t.baseURL, t.token, method)
}

// createForumTopic calls createForumTopic and returns the message_thread_id.
func (t *Telegram) createForumTopic(name string) (string, error) {
	resp, err := t.httpClient.PostForm(t.apiURL("createForumTopic"), url.Values{
		"chat_id": {t.chatID},
		"name":    {name},
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var r struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageThreadID int `json:"message_thread_id"`
		} `json:"result"`
		Description string `json:"description"`
	}
	if err := decodeJSON(resp.Body, &r); err != nil {
		return "", err
	}
	if !r.OK {
		return "", fmt.Errorf("API error: %s", r.Description)
	}
	return strconv.Itoa(r.Result.MessageThreadID), nil
}

// sendMessage posts text into a forum topic.
func (t *Telegram) sendMessage(threadRef, text string) error {
	resp, err := t.httpClient.PostForm(t.apiURL("sendMessage"), url.Values{
		"chat_id":           {t.chatID},
		"message_thread_id": {threadRef},
		"text":              {text},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var r struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := decodeJSON(resp.Body, &r); err != nil {
		return err
	}
	if !r.OK {
		return fmt.Errorf("API error: %s", r.Description)
	}
	return nil
}

// getUpdates long-polls for updates starting at offset.
func (t *Telegram) getUpdates(offset int) ([]update, error) {
	endpoint := fmt.Sprintf("%s?offset=%d&timeout=%d&allowed_updates=%s",
		t.apiURL("getUpdates"),
		offset,
		longPollTimeout,
		url.QueryEscape(`["message"]`),
	)
	resp, err := t.httpClient.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var r struct {
		OK          bool     `json:"ok"`
		Result      []update `json:"result"`
		Description string   `json:"description"`
	}
	if err := decodeJSON(resp.Body, &r); err != nil {
		return nil, err
	}
	if !r.OK {
		return nil, fmt.Errorf("API error: %s", r.Description)
	}
	return r.Result, nil
}

// ── API response types ────────────────────────────────────────────────────────

type update struct {
	UpdateID int      `json:"update_id"`
	Message  *message `json:"message"`
}

type message struct {
	MessageID       int    `json:"message_id"`
	MessageThreadID int    `json:"message_thread_id"`
	IsTopicMessage  bool   `json:"is_topic_message"`
	Text            string `json:"text"`
	Chat            chat   `json:"chat"`
}

type chat struct {
	ID int64 `json:"id"`
}

// ── helpers ───────────────────────────────────────────────────────────────────

// loadSecret loads a secret value. Priority: env var → file at <home>/<relPath>.
// The file must exist with mode 0600; a looser mode is silently accepted in
// tests (the file content is returned regardless of mode).
func loadSecret(home, envKey, relPath string) (string, error) {
	if v := os.Getenv(envKey); v != "" {
		return strings.TrimSpace(v), nil
	}
	p := filepath.Join(home, relPath)
	data, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("env %s not set and %s: %w", envKey, p, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func decodeJSON(r io.Reader, dst any) error {
	return json.NewDecoder(r).Decode(dst)
}
