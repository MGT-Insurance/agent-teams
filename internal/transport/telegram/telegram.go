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
// Send with General==true skips topic creation entirely and posts straight
// to the General channel (no message_thread_id), returning "".
//
// # Inbound
//
// getUpdates long-poll. Messages where is_topic_message==true and the chat id
// matches the configured supergroup are delivered with their ThreadRef set;
// non-topic messages (General channel) are delivered with Reply{ThreadRef:
// ""} — the relay routes those by @mention rather than bouncing them. Receive
// resolves this bot's own @username via getMe from within the poll loop
// (retried each iteration while unresolved) so inbound messages can report
// Reply.MentionsSelf.
package telegram

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/mgt-insurance/agent-teams/internal/transport"
)

// Factory constructs a Telegram transport for RegisterTransport. It satisfies
// the transport factory signature by wrapping New with a nil http client.
func Factory(home string) (transport.Transport, error) {
	return New(home, nil)
}

// longPollTimeout is the getUpdates long-poll duration.
const longPollTimeout = 30

// maxTopicNameLen is a defensive cap on the forum topic name. msg.Title is
// already ≤72 chars (dispatch's shortTitle cap) by the time it reaches here;
// this backstops any caller that isn't already bounded.
const maxTopicNameLen = 64

// Telegram implements transport.Transport via the Telegram Bot API.
type Telegram struct {
	token      string
	chatID     string
	httpClient httpDoer
	baseURL    string // overridable in tests; defaults to "https://api.telegram.org"

	// ownUsername is this bot's own @username (lowercased, "@" stripped).
	// Resolved via getMe from within Receive's poll loop — retried at the
	// top of each iteration while still empty (see Receive) — and cached
	// here once resolved. Never resolved in New — New must stay
	// network-free for send-only/Enabled paths.
	ownUsername string
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

// Send delivers msg to the human. If msg.General is true, the message is
// posted to the General channel: no forum topic is opened, no
// message_thread_id is sent, and "" is returned. Otherwise, if msg.ThreadRef
// is "", a new forum topic is opened via createForumTopic and its id is
// returned as threadRef; if msg.ThreadRef is non-empty, sendMessage is called
// with it as message_thread_id.
func (t *Telegram) Send(msg transport.OutboundMessage) (string, error) {
	if msg.General {
		if err := t.sendMessage("", msg.Body); err != nil {
			return "", fmt.Errorf("telegram: sendMessage: %w", err)
		}
		return "", nil
	}

	threadRef := msg.ThreadRef

	if threadRef == "" {
		// Open a new forum topic named after the initiative's friendly
		// title. No [<InitiativeID>] prefix: the id is meaningless noise in
		// the title and stays available internally via the thread:<ref>
		// bead label (routing) and this topic's first message body.
		id, err := t.createForumTopic(truncateUTF8(msg.Title, maxTopicNameLen))
		if err != nil {
			return "", fmt.Errorf("telegram: createForumTopic: %w", err)
		}
		threadRef = id
	}

	// Build the message body. On reuse of an existing thread the title is
	// included as a header (no [<InitiativeID>] prefix, same rationale as
	// the topic name) so replies stay scannable.
	body := msg.Body
	if msg.ThreadRef != "" {
		body = fmt.Sprintf("%s\n\n%s", msg.Title, msg.Body)
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
// Before each poll, Receive resolves this bot's own @username via getMe if it
// isn't already known (never in New — New must stay network-free for
// send-only/Enabled paths). A getMe failure is logged and non-fatal: polling
// proceeds with ownUsername still empty, so MentionsSelf simply never
// matches yet; getMe is retried at the top of the next loop iteration —
// paced by the existing getUpdates long-poll / error-retry sleep, no
// separate retry loop — until it succeeds, after which the result is cached
// and never re-fetched.
//
// Receive runs until handler returns a non-nil error, which is propagated.
func (t *Telegram) Receive(handler func(transport.Reply) error) error {
	var offset int
	for {
		if t.ownUsername == "" {
			if username, err := t.getMe(); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "telegram: getMe: %v\n", err)
			} else {
				t.ownUsername = username
			}
		}

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

			// STRICT (Eric, at-gqqd): content-less reply (no text, no
			// caption) — drop at the relay; never forward to the steward.
			if isContentLess(msg) {
				fmt.Fprintln(os.Stderr, "telegram: dropping content-less reply (no text/caption)")
				continue
			}

			var reply transport.Reply
			reply.Text = messageBody(msg)
			reply.Mentions, reply.MentionsSelf = t.parseMentions(msg)

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

// isContentLess reports whether msg has no actionable body: no text and no
// caption. Media type alone (sticker, photo, voice, video, video_note,
// animation, document, audio) is NOT sufficient — text or a caption is
// required. Sticker and video_note are never captioned by Telegram, so under
// this predicate they always report content-less. This is the single,
// isolated content-less decision point (STRICT, Eric, at-gqqd): flipping the
// policy is a one-function edit.
func isContentLess(msg *message) bool {
	return msg.Text == "" && msg.Caption == ""
}

// messageBody returns the actionable, non-empty body to relay for msg. If
// msg.Text is non-empty it is returned unchanged (existing text-message
// behavior). Otherwise a bracketed placeholder is built from msg's media
// content — detection order matters: animation is checked BEFORE document
// because Telegram sends both an animation and a document object for GIFs.
// When msg.Caption is non-empty it is appended after the bracket separated
// by a single space (caption applies to photo/video/document/audio/
// animation/voice — not sticker or video_note, which Telegram never
// captions). A non-nil msg that matches none of the known types falls back
// to "[non-text message]": messageBody never returns "" for a non-nil msg,
// so no empty envelope can be produced again, even for currently-unhandled
// content (location, contact, poll, dice, venue, game).
func messageBody(msg *message) string {
	if msg.Text != "" {
		return msg.Text
	}

	switch {
	case msg.Sticker != nil:
		if msg.Sticker.Emoji != "" {
			return "[sticker " + msg.Sticker.Emoji + "]"
		}
		return "[sticker]"
	case msg.Animation != nil:
		return withCaption("[animation]", msg.Caption)
	case msg.Photo != nil:
		return withCaption("[photo]", msg.Caption)
	case msg.Voice != nil:
		return withCaption("[voice message]", msg.Caption)
	case msg.VideoNote != nil:
		return "[video note]"
	case msg.Video != nil:
		return withCaption("[video]", msg.Caption)
	case msg.Audio != nil:
		return withCaption("[audio]", msg.Caption)
	case msg.Document != nil:
		label := "[document]"
		if msg.Document.FileName != "" {
			label = "[document: " + msg.Document.FileName + "]"
		}
		return withCaption(label, msg.Caption)
	default:
		return "[non-text message]"
	}
}

// withCaption appends caption to label separated by a single space, or
// returns label unchanged if caption is empty.
func withCaption(label, caption string) string {
	if caption == "" {
		return label
	}
	return label + " " + caption
}

// parseMentions extracts @-mentioned usernames from msg's "mention" entities
// ("text_mention" entities are for users without usernames and never apply to
// bots, so they're ignored). Telegram entity Offset/Length are UTF-16 code
// unit counts, not bytes or runes, so msg.Text is re-encoded to UTF-16 before
// slicing — otherwise a multi-byte character before the mention would
// misalign the extraction. Returns the mentions (lowercased, "@" stripped)
// and whether this transport's own username is among them.
func (t *Telegram) parseMentions(msg *message) (mentions []string, mentionsSelf bool) {
	if len(msg.Entities) == 0 {
		return nil, false
	}
	units := utf16.Encode([]rune(msg.Text))
	for _, e := range msg.Entities {
		if e.Type != "mention" {
			continue
		}
		if e.Offset < 0 || e.Length < 0 || e.Offset+e.Length > len(units) {
			continue
		}
		name := strings.ToLower(strings.TrimPrefix(string(utf16.Decode(units[e.Offset:e.Offset+e.Length])), "@"))
		if name == "" {
			continue
		}
		mentions = append(mentions, name)
		if t.ownUsername != "" && name == t.ownUsername {
			mentionsSelf = true
		}
	}
	return mentions, mentionsSelf
}

// ── Telegram Bot API calls ────────────────────────────────────────────────────

// getMe calls getMe and returns this bot's own @username, lowercased and with
// the "@" stripped (the Bot API's username field never includes one).
func (t *Telegram) getMe() (string, error) {
	resp, err := t.httpClient.Get(t.apiURL("getMe"))
	if err != nil {
		return "", t.sanitizeTransportErr(err)
	}
	defer resp.Body.Close()

	var r struct {
		OK     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
		Description string `json:"description"`
	}
	if err := decodeJSON(resp.Body, &r); err != nil {
		return "", err
	}
	if !r.OK {
		return "", fmt.Errorf("API error: %s", r.Description)
	}
	return strings.ToLower(strings.TrimPrefix(r.Result.Username, "@")), nil
}

// apiURL constructs the Bot API endpoint URL for a method.
func (t *Telegram) apiURL(method string) string {
	// Token is embedded in the URL path — never log the URL.
	return fmt.Sprintf("%s/bot%s/%s", t.baseURL, t.token, method)
}

// sanitizeTransportErr strips the request URL — which embeds the bot token —
// out of a transport-level error returned by PostForm/Get before it can reach
// a log line or stderr. Connection-level failures (DNS, TLS, timeout, conn
// refused) come back as *url.Error, whose Error() string includes the full
// request URL; unwrapping to its inner Err drops the URL entirely. Any other
// error is passed through a string-replace of the raw token as a fallback, in
// case a future http.Client implementation surfaces the URL some other way.
func (t *Telegram) sanitizeTransportErr(err error) error {
	if err == nil {
		return nil
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	if t.token != "" && strings.Contains(err.Error(), t.token) {
		return errors.New(strings.ReplaceAll(err.Error(), t.token, "<redacted>"))
	}
	return err
}

// createForumTopic calls createForumTopic and returns the message_thread_id.
func (t *Telegram) createForumTopic(name string) (string, error) {
	resp, err := t.httpClient.PostForm(t.apiURL("createForumTopic"), url.Values{
		"chat_id": {t.chatID},
		"name":    {name},
	})
	if err != nil {
		return "", t.sanitizeTransportErr(err)
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

// CloseTopic closes the forum topic identified by threadRef via the Bot API
// closeForumTopic method. It satisfies the verbs package's optional
// TopicCloser-shaped interface (asserted at the `ateam close` call site)
// rather than being added to transport.Transport, which stays
// initiative-agnostic — most transports have no notion of a closeable
// thread.
func (t *Telegram) CloseTopic(threadRef string) error {
	resp, err := t.httpClient.PostForm(t.apiURL("closeForumTopic"), url.Values{
		"chat_id":           {t.chatID},
		"message_thread_id": {threadRef},
	})
	if err != nil {
		return t.sanitizeTransportErr(err)
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

// sendMessage posts text into a forum topic, or into the General channel when
// threadRef is "" (Telegram posts to General when message_thread_id is
// omitted from the request entirely — an empty-string value is not
// equivalent and must not be sent).
func (t *Telegram) sendMessage(threadRef, text string) error {
	values := url.Values{
		"chat_id": {t.chatID},
		"text":    {text},
	}
	if threadRef != "" {
		values.Set("message_thread_id", threadRef)
	}
	resp, err := t.httpClient.PostForm(t.apiURL("sendMessage"), values)
	if err != nil {
		return t.sanitizeTransportErr(err)
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
		return nil, t.sanitizeTransportErr(err)
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
	MessageID       int             `json:"message_id"`
	MessageThreadID int             `json:"message_thread_id"`
	IsTopicMessage  bool            `json:"is_topic_message"`
	Text            string          `json:"text"`
	Caption         string          `json:"caption"`
	Entities        []messageEntity `json:"entities"`
	Chat            chat            `json:"chat"`

	// Media fields — present only for the corresponding non-text message
	// type. messageBody uses these purely for content-type detection
	// (non-nil check); Sticker.Emoji and Document.FileName are the only
	// nested values actually read. Typed as *json.RawMessage (not a bare
	// json.RawMessage) where only presence matters, to avoid modeling
	// Telegram's full nested shapes: a bare json.RawMessage captures the
	// literal bytes of an explicit JSON null, so it decodes to a non-nil
	// (but "null"-valued) slice for `"photo": null` — wrongly signaling
	// presence. The pointer form correctly decodes an explicit null to a
	// nil pointer, matching the absent-key case, just like Sticker/Document
	// below.
	Sticker   *sticker         `json:"sticker"`
	Animation *json.RawMessage `json:"animation"`
	Photo     *json.RawMessage `json:"photo"`
	Voice     *json.RawMessage `json:"voice"`
	VideoNote *json.RawMessage `json:"video_note"`
	Video     *json.RawMessage `json:"video"`
	Audio     *json.RawMessage `json:"audio"`
	Document  *document        `json:"document"`
}

// sticker carries the sticker's emoji, if any — the only semantic signal a
// bare sticker placeholder can surface (see messageBody).
type sticker struct {
	Emoji string `json:"emoji"`
}

// document carries the document's original filename, if any (see
// messageBody).
type document struct {
	FileName string `json:"file_name"`
}

type chat struct {
	ID int64 `json:"id"`
}

// messageEntity is a Telegram MessageEntity. Offset and Length are UTF-16
// code unit counts, per the Bot API spec — see parseMentions.
type messageEntity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
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

// truncateUTF8 truncates s to at most maxBytes bytes without splitting a
// multi-byte UTF-8 rune (agent-teams-6rru.14: a byte-index slice can cut a
// rune in half, sending invalid UTF-8 to the Telegram API). Ranging over a
// string yields the byte offset of each rune boundary in increasing order;
// the last offset at or below maxBytes is the largest length that is still
// a complete sequence of runes.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := 0
	for i := range s {
		if i > maxBytes {
			break
		}
		cut = i
	}
	return s[:cut]
}
