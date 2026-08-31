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
// DM allow-list (OPTIONAL): env AGENT_TEAMS_TELEGRAM_DM_ALLOWLIST, or file
// ~/.agent-teams/telegram/dm-allowlist — the Telegram user ids permitted to
// hold a 1:1 conversation with the bot, one per line or comma-separated (#
// comments and blank lines ignored). Absent or empty admits no DMs at all; a
// malformed entry is a startup error. See allowsDirectChat.
//
// Finding your own id (the bootstrap step: the allow-list wants a number,
// not a username): post any message to the configured group as you
// normally would — e.g. the General topic. The relay's log names your
// numeric sender id the first time it sees you post there, on a line
// containing "sender id <N>". That number IS the id to add to
// dm-allowlist: a Telegram private chat's chat.id equals the sender's own
// user id, the same value the allow-list is keyed on, so the id attached
// to an ordinary group message is exactly the id a DM needs. Add it as its
// own line (or comma-separated with others) — ids are positive integers.
// See the sender-id-logging in Receive.
//
// # Thread model
//
// One Telegram forum topic per initiative. Send with ThreadRef=="" opens a new
// topic via createForumTopic and returns its message_thread_id as threadRef.
// Subsequent sends pass that threadRef as message_thread_id to sendMessage.
// Send with General==true skips topic creation entirely and posts straight
// to the General channel (no message_thread_id), returning "".
//
// Send with a non-empty ChatRef also skips topic creation and posts one
// message into the conversation that ref names — a DM, or the configured
// supergroup itself — returning "". The ref is the composite MessageRef
// Receive emitted for an inbound message, and the chat it names is
// authorized against allowsDirectChat before anything is sent; see
// chatFromRef. It beats General when both are set.
//
// # Inbound
//
// getUpdates long-poll. Messages where is_topic_message==true and the chat id
// matches the configured supergroup are delivered with their ThreadRef set.
// Two kinds of message are delivered with Reply{ThreadRef: ""} instead: a
// non-topic message in the configured supergroup (the General channel), and
// an admitted DM. This package makes no routing decision about either — the
// relay does, keying off the addressing fields (Reply.MentionsSelf,
// Reply.Mentions, Reply.Direct), which this package's only obligation is to
// report faithfully. Receive resolves this bot's own @username via getMe from
// within the poll loop (retried each iteration while unresolved) so inbound
// messages can report Reply.MentionsSelf.
package telegram

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
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

// ackReactionEmoji is the read-receipt reaction set on the originating
// message via setMessageReaction (agent-teams-a0ml.3). "eyes" — reads as
// "seen" — and is in Telegram's default allowed-reaction set. One emoji
// only (Eric).
const ackReactionEmoji = "\U0001F440"

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

	// logOut is where Receive's poll-cycle log lines (transport.Logf) are
	// written. Defaults to os.Stderr in New(); a nil logOut (e.g. a struct
	// literal built directly in tests) falls back to os.Stderr too, via
	// logf below.
	logOut io.Writer

	// dmAllowlist is the set of Telegram user ids permitted to DM the bot,
	// read by allowsDirectChat and by nothing else. Nil or empty admits
	// nobody. Populated in New from the optional dm-allowlist config; see
	// parseDMAllowlist for the format.
	dmAllowlist map[int64]bool

	// seenSenders records which senders' Telegram user ids have already
	// been logged by the dm-allowlist bootstrap line in Receive
	// (agent-teams-ncn5.15), so each sender's id is logged once per
	// process lifetime rather than once per message — an active General
	// topic would otherwise spam the relay log forever with information
	// that's only useful once, at setup. Nil until the first group
	// message arrives; reading a nil map is safe, only writes need the
	// lazy-init check in Receive.
	seenSenders map[int64]bool
}

// logf writes one timestamped, indentation-scoped log line via
// transport.Logf, falling back to os.Stderr when t.logOut is nil (struct
// literals built directly in tests don't always set it).
func (t *Telegram) logf(depth int, format string, args ...any) {
	w := t.logOut
	if w == nil {
		w = os.Stderr
	}
	transport.Logf(w, depth, format, args...)
}

// httpDoer is the subset of *http.Client used by Telegram. Injected for tests.
type httpDoer interface {
	Get(url string) (*http.Response, error)
	PostForm(url string, data url.Values) (*http.Response, error)
	// PostMultipart posts fields plus one file part (fileField/fileName/
	// fileContent) as multipart/form-data — sendPhoto's local image upload
	// needs this; PostForm's urlencoded body cannot carry file content.
	PostMultipart(url string, fields map[string]string, fileField, fileName string, fileContent []byte) (*http.Response, error)
}

// realHTTPClient adapts *http.Client to httpDoer. Get and PostForm are
// promoted straight through via the embedded client; PostMultipart is added
// here because http.Client has no multipart-upload method of its own.
type realHTTPClient struct {
	*http.Client
}

// PostMultipart builds a multipart/form-data request body from fields and one
// file part, then posts it to url via the embedded client.
func (c realHTTPClient) PostMultipart(url string, fields map[string]string, fileField, fileName string, fileContent []byte) (*http.Response, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			return nil, err
		}
	}
	part, err := w.CreateFormFile(fileField, fileName)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(fileContent); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	return c.Do(req)
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
	rawAllowlist, err := loadOptionalSecret(home, "AGENT_TEAMS_TELEGRAM_DM_ALLOWLIST", "telegram/dm-allowlist")
	if err != nil {
		return nil, fmt.Errorf("telegram: dm-allowlist: %w", err)
	}
	dmAllowlist, err := parseDMAllowlist(rawAllowlist)
	if err != nil {
		return nil, fmt.Errorf("telegram: dm-allowlist: %w", err)
	}
	if client == nil {
		client = realHTTPClient{&http.Client{Timeout: 45 * time.Second}}
	}
	return &Telegram{
		token:       token,
		chatID:      chatID,
		httpClient:  client,
		baseURL:     "https://api.telegram.org",
		logOut:      os.Stderr,
		dmAllowlist: dmAllowlist,
	}, nil
}

// Name returns "telegram".
func (t *Telegram) Name() string { return "telegram" }

// Send delivers msg to the human.
//
// Two shapes, and the first one wins:
//
//   - msg.ChatRef non-empty, or msg.General true: one message, no forum topic,
//     no message_thread_id, "" returned. ChatRef names the conversation to
//     post into (a DM, or the configured supergroup's General channel);
//     General means the configured supergroup's General channel. ChatRef is
//     tested FIRST because transport.OutboundMessage makes it beat General
//     when both are set: downgrading a reply meant for one conversation into
//     the shared channel is a data-leak-shaped failure, not a routing
//     preference. The destination it names is validated by chatFromRef before
//     anything is sent.
//   - otherwise the forum-topic path, unchanged and group-only: msg.ThreadRef
//     == "" opens a new topic via createForumTopic and returns its id as
//     threadRef; a non-empty msg.ThreadRef is passed to sendMessage as
//     message_thread_id.
func (t *Telegram) Send(msg transport.OutboundMessage) (string, error) {
	if msg.ChatRef != "" || msg.General {
		chatID := t.chatID
		if msg.ChatRef != "" {
			var err error
			if chatID, err = t.chatFromRef(msg.ChatRef); err != nil {
				return "", fmt.Errorf("telegram: chat ref: %w", err)
			}
		}
		// Empty thread ref: sendMessage/sendPhoto omits message_thread_id
		// entirely, which is required for a private chat and is already what
		// a General send does. No new branch, and no empty-string thread id.
		if err := t.sendMessageOrAttachment(chatID, "", msg); err != nil {
			return "", err
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

	// On reuse of an existing thread, msg.Title is deliberately NOT
	// prepended to the body: the forum topic (opened above, or on a prior
	// call for this thread) is already named after that same title, so
	// restating it under a heading that says the same thing is noise, not
	// an aid to scanning. Send msg.Body verbatim.
	//
	// t.chatID, not a resolved chat: this path is reachable only with an
	// empty ChatRef, and forum topics exist only in the configured
	// supergroup — createForumTopic and CloseTopic stay group-only for the
	// same reason.
	if err := t.sendMessageOrAttachment(t.chatID, threadRef, msg); err != nil {
		return "", err
	}
	return threadRef, nil
}

// sendMessageOrAttachment posts msg into chatID/threadRef: sendPhoto (with a
// caption) when msg.ImagePath is set, else sendDocument (with a caption) when
// msg.DocumentPath is set, else sendMessage (text-only). ImagePath is checked
// first — the two attachment fields are mutually exclusive in practice (see
// OutboundMessage.DocumentPath), but this ordering is what a caller that sets
// both would get. All three Bot API calls are mutually exclusive per send: an
// attachment paired with a body is delivered as one photo/document message
// with the body as its caption, never an attachment followed by a redundant
// text message (agent-teams-bfw3.1, extended for documents by
// agent-teams-n0jt.6).
func (t *Telegram) sendMessageOrAttachment(chatID, threadRef string, msg transport.OutboundMessage) error {
	if msg.ImagePath != "" {
		if err := t.sendPhoto(chatID, threadRef, mediaCaption(msg), msg.ImagePath); err != nil {
			return fmt.Errorf("telegram: sendPhoto: %w", err)
		}
		return nil
	}
	if msg.DocumentPath != "" {
		if err := t.sendDocument(chatID, threadRef, mediaCaption(msg), msg.DocumentPath); err != nil {
			return fmt.Errorf("telegram: sendDocument: %w", err)
		}
		return nil
	}
	if err := t.sendMessage(chatID, threadRef, msg.Body); err != nil {
		return fmt.Errorf("telegram: sendMessage: %w", err)
	}
	return nil
}

// telegramCaptionMaxChars is the Bot API's caption cap, shared by sendPhoto
// and sendDocument.
const telegramCaptionMaxChars = 1024

// mediaCaption derives the sendPhoto/sendDocument caption for msg: Body,
// falling back to Title when Body is empty (so an attachment sent with only
// a title still gets a caption), truncated to telegramCaptionMaxChars.
// Caption is optional — msg with neither Body nor Title yields "".
func mediaCaption(msg transport.OutboundMessage) string {
	caption := msg.Body
	if caption == "" {
		caption = msg.Title
	}
	return truncateChars(caption, telegramCaptionMaxChars)
}

// chatFromRef decodes and AUTHORIZES the destination named by an
// OutboundMessage.ChatRef, returning the chat id to post to.
//
// The ref is the composite "<chat_id>:<message_id>" Receive emits as
// Reply.MessageRef — one format, one parse, the same shape in both
// directions. Only the chat half is used here; the message half is carried
// along for the ref to stay a single opaque value outside this package, and
// is deliberately not required to be present.
//
// The authorization is not redundant with the inbound gate. This value has
// round-tripped through the STEWARD — an LLM rendering text a human wrote —
// and `ateam notify --to <ref>` passes it through without parsing or
// validating it, by design: the transport is the layer entitled to reject a
// destination. Whatever arrives here becomes a chat_id on a live Bot API
// call, so a garbled or manipulated ref would otherwise turn the bot into a
// sender to an arbitrary Telegram chat. Permitted destinations are exactly
// the configured chat plus whatever allowsDirectChat admits — the same
// predicate the inbound gate uses, so the two directions cannot drift apart.
// A rejection is an error and never a fallback to some other chat: silently
// redirecting a message meant for one conversation is the failure this
// guards against, not the recovery from it.
func (t *Telegram) chatFromRef(ref string) (string, error) {
	chatID, _, ok := strings.Cut(ref, ":")
	if !ok || chatID == "" {
		return "", fmt.Errorf("malformed ref %q (want \"<chat_id>:<message_id>\")", ref)
	}
	if chatID == t.chatID {
		return chatID, nil
	}
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil || !t.allowsDirectChat(id) {
		return "", fmt.Errorf("refusing to send to chat %s: neither the configured chat nor in the dm-allowlist", chatID)
	}
	return chatID, nil
}

// Receive long-polls Telegram for updates, invoking handler for each inbound
// message. Messages whose chat id does not match the configured supergroup
// are not passed to handler (unless admitted as a DM, below); a message from
// the configured chat that is not a topic message — the General channel — IS
// passed, as Reply{ThreadRef: ""}, and the relay routes it by the addressing
// fields rather than by its (absent) thread ref.
//
// A message from a private chat (a DM to the bot) is admitted only when
// allowsDirectChat accepts its chat id — which, for a private chat, IS the
// sender's own Telegram user id — and is then emitted with Reply.Direct ==
// true and an empty ThreadRef (a DM has no forum topic).
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
	// One-time startup config line — chat id only, never the token. Emitted
	// here (not in New) because New is also the registered transport
	// factory: transport.Enabled and transport.For construct a Telegram to
	// merely probe config (notify.go, dispatch.go, gate auto-notify) and
	// must never print relay-startup noise. Receive is only ever called by
	// the relay poller, so this fires exactly once per relay startup.
	t.logf(0, "config: chat_id=%s, token configured (%d bytes)", t.chatID, len(t.token))

	var offset int
	for {
		if t.ownUsername == "" {
			if username, err := t.getMe(); err != nil {
				t.logf(0, "getMe: %v", err)
			} else {
				t.ownUsername = username
				t.logf(0, "bot identity resolved: @%s", username)
			}
		}

		before := offset

		updates, err := t.getUpdates(offset)
		if err != nil {
			// Transient network errors: log and retry.
			t.logf(0, "poll error (getUpdates): %v — retrying in 2s", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, upd := range updates {
			offset = upd.UpdateID + 1

			msg := upd.Message
			if msg == nil {
				continue
			}

			// Admission: the configured chat, or a private chat (DM) whose
			// sender allowsDirectChat accepts. Everything else is rejected.
			// The configured-chat branch is checked FIRST on purpose — the
			// ordering must not depend on the fact that the configured id is
			// a -100… supergroup and so can never be a private chat.
			var direct bool
			chatIDStr := strconv.FormatInt(msg.Chat.ID, 10)
			switch {
			case chatIDStr == t.chatID:
				// The configured supergroup. Log a sender's numeric id the
				// first time it's seen here — the dm-allowlist bootstrap
				// (agent-teams-ncn5.15): a Telegram private chat's chat.id
				// IS the sender's own user id, so the number this line
				// names is exactly the one to add to dm-allowlist. Once per
				// sender per process lifetime (seenSenders), not once per
				// message, so an active General topic doesn't spam the
				// relay log with information that's only useful once, at
				// setup.
				if msg.From != nil && !t.seenSenders[msg.From.ID] {
					if t.seenSenders == nil {
						t.seenSenders = make(map[int64]bool)
					}
					t.seenSenders[msg.From.ID] = true
					t.logf(1, "update %d: sender id %d%s (add to dm-allowlist to permit a DM)", upd.UpdateID, msg.From.ID, senderUsername(msg))
				}
			case msg.Chat.Type == "private":
				if !t.allowsDirectChat(msg.Chat.ID) {
					// Sender id (and @username, when Telegram supplies one)
					// — NEVER the message text: an unauthorized sender's
					// content does not belong in our logs. For a private
					// chat chat.id IS the Telegram user id, so this line is
					// the whole bootstrap path for getting a legitimate
					// sender into the allow-list.
					t.logf(1, "update %d: rejected DM (sender %d%s not in dm-allowlist)", upd.UpdateID, msg.Chat.ID, senderUsername(msg))
					continue
				}
				direct = true
			default:
				t.logf(1, "update %d: rejected (chat %s != configured %s)", upd.UpdateID, chatIDStr, t.chatID)
				continue
			}

			// STRICT (Eric, at-gqqd): content-less reply (no text, no
			// caption) — drop at the relay; never forward to the steward.
			if isContentLess(msg) {
				t.logf(1, "update %d: dropped (no text/caption)", upd.UpdateID)
				continue
			}

			var reply transport.Reply
			reply.Text = messageBody(msg)
			reply.Mentions, reply.MentionsSelf = t.parseMentions(msg)
			// Composite "<chat_id>:<message_id>": a Telegram message id is
			// unique only WITHIN its chat, so the chat has to travel with it
			// or a ref fired against the wrong chat addresses an unrelated
			// message there. Emitted in the same shape for every message,
			// group and DM alike — one format, one parse, no branch. The ref
			// is opaque outside this package (transport.Reply.MessageRef), so
			// uniformity costs nothing and a field with two shapes is the
			// thing that bites later.
			reply.MessageRef = chatIDStr + ":" + strconv.Itoa(msg.MessageID)
			reply.Direct = direct

			if msg.IsTopicMessage && msg.MessageThreadID != 0 {
				reply.ThreadRef = strconv.Itoa(msg.MessageThreadID)
			}
			// ThreadRef stays "" for the two producers of a topic-less
			// reply — a non-topic (General channel) message and a DM. The
			// relay routes both off the addressing fields; this package
			// makes no routing decision about either.

			if err := handler(reply); err != nil {
				return err
			}
		}

		// One depth-0 heartbeat per poll cycle — REQUIRED #2 (a zero-update
		// poll and a normal poll share this same line) and SUGGESTED #1
		// (offset before/after) combined. Emitted after the per-update loop
		// so offset reflects any advance.
		t.logf(0, "poll: offset=%d -> %d, %d update(s)", before, offset, len(updates))
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

// allowsDirectChat reports whether the bot will hold a 1:1 conversation with
// chatID. For a Telegram private chat, chat.id IS the human's own user id, so
// the allow-list is a list of Telegram user ids. This is the single, isolated
// authorization decision point for direct chats (Eric, agent-teams-ncn5),
// built the way isContentLess isolates the content-less policy above:
// flipping the policy is a one-function edit.
//
// It takes a chat id rather than a message because BOTH directions run
// through it — inbound it decides whether a DM is admitted at all, outbound it
// validates the chat decoded from an OutboundMessage.ChatRef before the
// transport will send there — so the two directions cannot drift apart.
//
// A nil or empty allow-list admits nobody. That is byte-identical to the
// behavior before DMs were admitted at all, and it is the correct default for
// a gate whose failure mode is "a stranger reaches an LLM with shell access":
// an operator who upgrades without configuring anything sees no change.
func (t *Telegram) allowsDirectChat(chatID int64) bool {
	return t.dmAllowlist[chatID]
}

// senderUsername renders msg's sender @username as a leading-space-separated
// suffix for a log line, or "" when the message carries no from or no
// username. The rejected-DM line uses it so the record says WHO was rejected
// as well as which id to allow-list — a bare numeric id is not something a
// human recognizes.
func senderUsername(msg *message) string {
	if msg.From == nil || msg.From.Username == "" {
		return ""
	}
	return " @" + msg.From.Username
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

// Ack marks reply's originating message with the read-receipt reaction via
// setMessageReaction. It satisfies the verbs package's optional
// relayAcker-shaped interface (asserted at Run(), mirroring the CloseTopic /
// topicCloser precedent above) rather than being added to transport.Transport,
// which stays initiative/relay-agnostic.
//
// reply.MessageRef is the composite "<chat_id>:<message_id>" Receive emits;
// both halves are required, because acking a DM's message id against the
// configured supergroup would react to whatever unrelated group message
// happens to hold that id.
func (t *Telegram) Ack(reply transport.Reply) error {
	if reply.MessageRef == "" {
		return errors.New("empty message ref")
	}
	chatID, messageID, ok := strings.Cut(reply.MessageRef, ":")
	if !ok || chatID == "" || messageID == "" {
		return fmt.Errorf("malformed message ref %q (want \"<chat_id>:<message_id>\")", reply.MessageRef)
	}
	return t.setMessageReaction(chatID, messageID)
}

// setMessageReaction sets ackReactionEmoji as the reaction on messageID in
// chatID via the Bot API setMessageReaction method. The chat id is a
// parameter rather than the configured t.chatID because Telegram message ids
// are unique only within a chat: a read receipt has to be fired at the chat
// the message actually came from. The reaction param is built with
// json.Marshal (not a hand-built string) so the emoji is correctly JSON-
// encoded regardless of content.
func (t *Telegram) setMessageReaction(chatID, messageID string) error {
	reaction, err := json.Marshal([]map[string]string{
		{"type": "emoji", "emoji": ackReactionEmoji},
	})
	if err != nil {
		return err
	}
	resp, err := t.httpClient.PostForm(t.apiURL("setMessageReaction"), url.Values{
		"chat_id":    {chatID},
		"message_id": {messageID},
		"reaction":   {string(reaction)},
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

// sendMessage posts text into chatID — a forum topic within it when
// threadRef is non-empty, otherwise the chat itself (the General channel of
// a supergroup, or a private chat). Telegram posts without a topic when
// message_thread_id is omitted from the request entirely; an empty-string
// value is not equivalent and must not be sent, which is also what makes a
// private-chat send work with no extra branch.
//
// The chat id is a parameter rather than the configured t.chatID so a reply
// can land in the conversation it was addressed to; every caller resolves it
// through Send, which is the single chokepoint all outbound message text
// funnels through (tests/sent-log.test.sh case9 pins that).
func (t *Telegram) sendMessage(chatID, threadRef, text string) error {
	values := url.Values{
		"chat_id": {chatID},
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

// sendPhoto posts the local image file at imagePath into chatID as a photo —
// a forum topic within it when threadRef is non-empty, otherwise the chat
// itself — with an optional caption. Mirrors sendMessage's chat_id/
// message_thread_id handling: a non-empty threadRef sets message_thread_id,
// an empty one omits the key entirely (see sendMessage).
func (t *Telegram) sendPhoto(chatID, threadRef, caption, imagePath string) error {
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return fmt.Errorf("read image %s: %w", imagePath, err)
	}

	fields := map[string]string{"chat_id": chatID}
	if caption != "" {
		fields["caption"] = caption
	}
	if threadRef != "" {
		fields["message_thread_id"] = threadRef
	}

	resp, err := t.httpClient.PostMultipart(t.apiURL("sendPhoto"), fields, "photo", filepath.Base(imagePath), data)
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

// sendDocument posts the local file at documentPath into chatID as a
// document attachment — a forum topic within it when threadRef is
// non-empty, otherwise the chat itself — with an optional caption. Mirrors
// sendPhoto's chat_id/message_thread_id handling and multipart upload,
// swapping the file-part field name ("document" vs "photo") and the Bot API
// endpoint (agent-teams-n0jt.6: non-image proof — JSON, logs, HAR — rides as
// a document instead of inline, avoiding the ~4096-char text limit).
func (t *Telegram) sendDocument(chatID, threadRef, caption, documentPath string) error {
	data, err := os.ReadFile(documentPath)
	if err != nil {
		return fmt.Errorf("read document %s: %w", documentPath, err)
	}

	fields := map[string]string{"chat_id": chatID}
	if caption != "" {
		fields["caption"] = caption
	}
	if threadRef != "" {
		fields["message_thread_id"] = threadRef
	}

	resp, err := t.httpClient.PostMultipart(t.apiURL("sendDocument"), fields, "document", filepath.Base(documentPath), data)
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
	From            *user           `json:"from"`

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
	// Type is the Telegram chat type: "private" (a 1:1 DM with the bot),
	// "group", "supergroup", or "channel". Only "private" is read — see the
	// admission switch in Receive.
	Type string `json:"type"`
}

// user is the minimal Telegram User shape decoded from a message's from field
// — modeled like sticker/document above: only what is actually read. ID is
// the sender's numeric Telegram user id: for a DM the allow-list is keyed on
// chat.id, not this field, but a GROUP message carries no other source for
// it, and it's exactly what the dm-allowlist bootstrap line in Receive needs
// (agent-teams-ncn5.15) — a Telegram private chat's chat.id equals the same
// sender's id here, so the number surfaced from ordinary group traffic is
// copy-pasteable straight into dm-allowlist. Username is the @username both
// that line and the rejected-DM line report alongside it.
type user struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
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

// loadOptionalSecret is loadSecret's sibling for config that is ALLOWED to be
// absent: env var → file at <home>/<relPath> → "". An absent file is not an
// error — the feature is simply unconfigured. Any other read failure still is:
// a file that exists but cannot be read is a misconfiguration, not an opt-out,
// and treating it as "unconfigured" would silently disable the feature.
//
// Unlike loadSecret, the returned value is NOT trimmed (agent-teams-ncn5.16):
// its one caller, parseDMAllowlist, reports parse errors by line number
// against exactly the text it's given, and that number has to index the file
// a human opens to fix it. Trimming here shifted every reported line number
// whenever the file started with a blank line. parseDMAllowlist already
// trims each field it parses, so nothing is lost by leaving the raw text
// alone.
//
// loadSecret must keep erroring on absence and is deliberately left alone: the
// token and chat id are required, and their absence has to fail loudly.
func loadOptionalSecret(home, envKey, relPath string) (string, error) {
	if v := os.Getenv(envKey); v != "" {
		return v, nil
	}
	p := filepath.Join(home, relPath)
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w", p, err)
	}
	return string(data), nil
}

// parseDMAllowlist parses the DM allow-list into the set allowsDirectChat
// consults: Telegram user ids, one per line or comma-separated, with #
// comments and blank lines ignored. Empty input yields a nil map, which admits
// nobody.
//
// An entry that is not a positive integer is an ERROR naming the offending
// line, never a skipped entry. A silently dropped id means the human believes
// they are allow-listed when they are not, and the only symptom is a DM that
// vanishes with no diagnostic — loud at startup beats silent at runtime. A
// non-positive id is rejected for the same reason: Telegram user ids are
// positive, so a negative one (e.g. a pasted -100… supergroup id) is an entry
// that could never match.
func parseDMAllowlist(raw string) (map[int64]bool, error) {
	var allow map[int64]bool
	for i, line := range strings.Split(raw, "\n") {
		if idx := strings.IndexByte(line, '#'); idx >= 0 {
			line = line[:idx]
		}
		for _, field := range strings.Split(line, ",") {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			id, err := strconv.ParseInt(field, 10, 64)
			if err != nil || id <= 0 {
				return nil, fmt.Errorf("line %d: %q is not a Telegram user id", i+1, field)
			}
			if allow == nil {
				allow = make(map[int64]bool)
			}
			allow[id] = true
		}
	}
	return allow, nil
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

// truncateChars truncates s to at most maxChars runes. Telegram's sendPhoto
// caption cap is stated in characters, not bytes (contrast truncateUTF8,
// used for the byte-bounded topic name), so this counts runes to avoid
// cutting a caption short for non-ASCII text.
func truncateChars(s string, maxChars int) string {
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	return string(runes[:maxChars])
}
