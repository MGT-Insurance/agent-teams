package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mgt-insurance/agent-teams/internal/transport"
)

// ── test helpers ─────────────────────────────────────────────────────────────

// newTestTelegram builds a Telegram pointed at srv with a fake token and
// chat. logOut defaults to io.Discard so the many existing Receive-driving
// tests don't spam test stderr with a heartbeat line per loop iteration;
// tests that need to assert on log output set tg.logOut to a *bytes.Buffer
// directly after construction.
func newTestTelegram(t *testing.T, srv *httptest.Server, chatID string) *Telegram {
	t.Helper()
	tg := &Telegram{
		token:      "test-token",
		chatID:     chatID,
		httpClient: &http.Client{},
		baseURL:    srv.URL,
		logOut:     io.Discard,
	}
	return tg
}

// pinTelegramEnv pins EVERY env-var-then-file secret New consults — token,
// chat id, DM allow-list — for the duration of the test. Every test that
// calls New must go through it.
//
// One helper rather than a t.Setenv per test because the leak it prevents is
// specifically the one an UNPINNED variable causes: New reads
// AGENT_TEAMS_TELEGRAM_DM_ALLOWLIST before falling back to the file, so on a
// machine where the operator has exported it — exactly what running this
// feature invites — an unpinned test builds a transport with a REAL
// allow-list and then passes or fails on the developer's environment instead
// of on the code. That failure surfaces on one machine, later, looking like
// flakiness. Routing all three through one helper means the next optional
// secret added to New is pinned by editing one place (agent-teams-ncn5.17).
//
// An empty value is not "unset": loadSecret/loadOptionalSecret treat it as
// absent and fall through to <home>/telegram/<name>, which is how a test asks
// for the file path to be exercised.
func pinTelegramEnv(t *testing.T, token, chatID, dmAllowlist string) {
	t.Helper()
	t.Setenv("AGENT_TEAMS_TELEGRAM_TOKEN", token)
	t.Setenv("AGENT_TEAMS_TELEGRAM_CHAT_ID", chatID)
	t.Setenv("AGENT_TEAMS_TELEGRAM_DM_ALLOWLIST", dmAllowlist)
}

// logLineDepth returns the transport.Logf indentation depth of line — the
// number of two-space indent groups immediately after the fixed-width
// "YYYY-MM-DD HH:MM:SS " timestamp prefix (20 chars: 19-char timestamp + 1
// separator space).
func logLineDepth(t *testing.T, line string) int {
	t.Helper()
	const prefixLen = len("2006-01-02 15:04:05 ")
	if len(line) < prefixLen {
		t.Fatalf("log line too short to carry a timestamp prefix: %q", line)
	}
	rest := line[prefixLen:]
	spaces := 0
	for _, r := range rest {
		if r != ' ' {
			break
		}
		spaces++
	}
	return spaces / 2
}

// newUpdatesServer answers getUpdates with updates on the first call and an
// empty batch on every call after — the fixture shape the Receive tests below
// share.
func newUpdatesServer(updates []map[string]any) *httptest.Server {
	callCount := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			http.NotFound(w, r)
			return
		}
		callCount++
		if callCount == 1 {
			jsonResponse(w, 200, map[string]any{"ok": true, "result": updates})
		} else {
			jsonResponse(w, 200, map[string]any{"ok": true, "result": []any{}})
		}
	}))
}

// jsonResponse writes a JSON body with the given status code.
func jsonResponse(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ── config loading ────────────────────────────────────────────────────────────

func TestLoadSecret_Env(t *testing.T) {
	t.Setenv("AGENT_TEAMS_TELEGRAM_TOKEN", "tok-from-env")
	val, err := loadSecret(t.TempDir(), "AGENT_TEAMS_TELEGRAM_TOKEN", "telegram/token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "tok-from-env" {
		t.Errorf("got %q, want %q", val, "tok-from-env")
	}
}

func TestLoadSecret_File(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "telegram")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("tok-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_TEAMS_TELEGRAM_TOKEN", "") // ensure env is unset

	val, err := loadSecret(home, "AGENT_TEAMS_TELEGRAM_TOKEN", "telegram/token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "tok-from-file" {
		t.Errorf("got %q, want %q", val, "tok-from-file")
	}
}

func TestLoadSecret_Missing(t *testing.T) {
	t.Setenv("AGENT_TEAMS_TELEGRAM_TOKEN", "")
	_, err := loadSecret(t.TempDir(), "AGENT_TEAMS_TELEGRAM_TOKEN", "telegram/token")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLoadSecret_EnvTakesPriorityOverFile(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "telegram")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("file-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_TEAMS_TELEGRAM_TOKEN", "env-token")

	val, err := loadSecret(home, "AGENT_TEAMS_TELEGRAM_TOKEN", "telegram/token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "env-token" {
		t.Errorf("got %q, want %q", val, "env-token")
	}
}

// ── Send: new thread (ThreadRef == "") ───────────────────────────────────────

func TestSend_NewThread_CallsCreateForumTopicThenSendMessage(t *testing.T) {
	const wantThreadID = 42
	const chatID = "-100123456789"

	var gotCreateTopic, gotSendMessage bool
	var gotTopicName, gotSendChatID, gotSendThreadID, gotSendText string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/createForumTopic"):
			gotCreateTopic = true
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			gotTopicName = r.FormValue("name")
			jsonResponse(w, 200, map[string]any{
				"ok":     true,
				"result": map[string]any{"message_thread_id": wantThreadID},
			})
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			gotSendMessage = true
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			gotSendChatID = r.FormValue("chat_id")
			gotSendThreadID = r.FormValue("message_thread_id")
			gotSendText = r.FormValue("text")
			jsonResponse(w, 200, map[string]any{"ok": true, "result": map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	threadRef, err := tg.Send(transport.OutboundMessage{
		InitiativeID: "at-00o",
		ThreadRef:    "",
		Title:        "Blocked on review",
		Body:         "Need your approval.",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if !gotCreateTopic {
		t.Error("createForumTopic was not called")
	}
	if !gotSendMessage {
		t.Error("sendMessage was not called")
	}
	wantName := "Blocked on review"
	if gotTopicName != wantName {
		t.Errorf("topic name: got %q, want %q", gotTopicName, wantName)
	}
	if gotSendChatID != chatID {
		t.Errorf("sendMessage chat_id: got %q, want %q", gotSendChatID, chatID)
	}
	wantThreadRef := strconv.Itoa(wantThreadID)
	if threadRef != wantThreadRef {
		t.Errorf("returned threadRef: got %q, want %q", threadRef, wantThreadRef)
	}
	if gotSendThreadID != wantThreadRef {
		t.Errorf("sendMessage thread_id: got %q, want %q", gotSendThreadID, wantThreadRef)
	}
	// Body sent directly (no title prefix) on new-thread path.
	if gotSendText != "Need your approval." {
		t.Errorf("sendMessage text: got %q, want %q", gotSendText, "Need your approval.")
	}
}

// TestSend_NewThread_TopicNameCappedAt64Chars confirms the defensive
// maxTopicNameLen backstop: a Title longer than 64 chars is truncated before
// createForumTopic is called, and carries no [<InitiativeID>] prefix.
func TestSend_NewThread_TopicNameCappedAt64Chars(t *testing.T) {
	const chatID = "-100123456789"
	longTitle := strings.Repeat("x", 100)

	var gotTopicName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/createForumTopic"):
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			gotTopicName = r.FormValue("name")
			jsonResponse(w, 200, map[string]any{
				"ok":     true,
				"result": map[string]any{"message_thread_id": 1},
			})
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			jsonResponse(w, 200, map[string]any{"ok": true, "result": map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	if _, err := tg.Send(transport.OutboundMessage{
		InitiativeID: "at-00o",
		ThreadRef:    "",
		Title:        longTitle,
		Body:         "body",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(gotTopicName) != maxTopicNameLen {
		t.Errorf("topic name length: got %d, want %d", len(gotTopicName), maxTopicNameLen)
	}
	if gotTopicName != longTitle[:maxTopicNameLen] {
		t.Errorf("topic name: got %q, want %q", gotTopicName, longTitle[:maxTopicNameLen])
	}
}

// TestSend_NewThread_TopicNameTruncationIsRuneSafe confirms
// agent-teams-6rru.14's fix: a Title whose multi-byte rune straddles byte 64
// (the maxTopicNameLen cap) is truncated at the last full rune boundary at
// or below the cap, never mid-rune. 62 ASCII 'x' bytes are followed by a
// 4-byte emoji occupying bytes 62-65 — straddling the byte-64 cut point —
// followed by more ASCII that would never survive truncation either way.
func TestSend_NewThread_TopicNameTruncationIsRuneSafe(t *testing.T) {
	const chatID = "-100123456789"
	const asciiPrefixLen = 62
	title := strings.Repeat("x", asciiPrefixLen) + "😀" + strings.Repeat("y", 20)

	var gotTopicName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/createForumTopic"):
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			gotTopicName = r.FormValue("name")
			jsonResponse(w, 200, map[string]any{
				"ok":     true,
				"result": map[string]any{"message_thread_id": 1},
			})
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			jsonResponse(w, 200, map[string]any{"ok": true, "result": map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	if _, err := tg.Send(transport.OutboundMessage{
		InitiativeID: "at-00o",
		ThreadRef:    "",
		Title:        title,
		Body:         "body",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if !utf8.ValidString(gotTopicName) {
		t.Errorf("topic name is not valid UTF-8: %q", gotTopicName)
	}
	if len(gotTopicName) > maxTopicNameLen {
		t.Errorf("topic name length: got %d bytes, want <= %d", len(gotTopicName), maxTopicNameLen)
	}
	wantTopicName := strings.Repeat("x", asciiPrefixLen)
	if gotTopicName != wantTopicName {
		t.Errorf("topic name: got %q, want %q (emoji should be dropped, not split)", gotTopicName, wantTopicName)
	}
}

// ── Send: existing thread (ThreadRef != "") ───────────────────────────────────

func TestSend_ExistingThread_SkipsCreateForumTopic(t *testing.T) {
	const chatID = "-100123456789"

	var createTopicCalled bool
	var gotSendThreadID, gotSendText string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/createForumTopic"):
			createTopicCalled = true
			jsonResponse(w, 200, map[string]any{"ok": true, "result": map[string]any{"message_thread_id": 99}})
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			gotSendThreadID = r.FormValue("message_thread_id")
			gotSendText = r.FormValue("text")
			jsonResponse(w, 200, map[string]any{"ok": true, "result": map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	threadRef, err := tg.Send(transport.OutboundMessage{
		InitiativeID: "at-00o",
		ThreadRef:    "7",
		Title:        "Status update",
		Body:         "All good.",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if createTopicCalled {
		t.Error("createForumTopic should not have been called for existing thread")
	}
	if threadRef != "7" {
		t.Errorf("threadRef: got %q, want %q", threadRef, "7")
	}
	if gotSendThreadID != "7" {
		t.Errorf("sendMessage thread_id: got %q, want %q", gotSendThreadID, "7")
	}
	wantText := "All good."
	if gotSendText != wantText {
		t.Errorf("sendMessage text:\ngot  %q\nwant %q", gotSendText, wantText)
	}
}

// ── CloseTopic ────────────────────────────────────────────────────────────────

func TestCloseTopic_Success(t *testing.T) {
	const chatID = "-100123456789"
	var gotChatID, gotThreadID string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/closeForumTopic") {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		gotChatID = r.FormValue("chat_id")
		gotThreadID = r.FormValue("message_thread_id")
		jsonResponse(w, 200, map[string]any{"ok": true, "result": true})
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	if err := tg.CloseTopic("42"); err != nil {
		t.Fatalf("CloseTopic: %v", err)
	}
	if gotChatID != chatID {
		t.Errorf("chat_id: got %q, want %q", gotChatID, chatID)
	}
	if gotThreadID != "42" {
		t.Errorf("message_thread_id: got %q, want %q", gotThreadID, "42")
	}
}

func TestCloseTopic_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{"ok": false, "description": "topic already closed"})
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, "-100123456789")
	err := tg.CloseTopic("42")
	if err == nil {
		t.Fatal("expected error for API-level failure")
	}
	if !strings.Contains(err.Error(), "topic already closed") {
		t.Errorf("error = %q, want it to mention API description", err.Error())
	}
}

func TestCloseTopic_ConnectionFailure_NoTokenInError(t *testing.T) {
	tg := newConnFailureTelegram(t)
	err := tg.CloseTopic("42")
	assertErrorHasNoToken(t, err)
}

// ── Ack (read receipts, agent-teams-a0ml.3) ──────────────────────────────────

func TestAck_Success(t *testing.T) {
	const chatID = "-100123456789"
	var gotChatID, gotMessageID string
	var gotReaction []map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/setMessageReaction") {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		gotChatID = r.FormValue("chat_id")
		gotMessageID = r.FormValue("message_id")
		if err := json.Unmarshal([]byte(r.FormValue("reaction")), &gotReaction); err != nil {
			t.Errorf("unmarshal reaction param: %v", err)
		}
		jsonResponse(w, 200, map[string]any{"ok": true, "result": true})
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	if err := tg.Ack(transport.Reply{MessageRef: chatID + ":42"}); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if gotChatID != chatID {
		t.Errorf("chat_id: got %q, want %q", gotChatID, chatID)
	}
	if gotMessageID != "42" {
		t.Errorf("message_id: got %q, want %q", gotMessageID, "42")
	}
	if len(gotReaction) != 1 || gotReaction[0]["type"] != "emoji" || gotReaction[0]["emoji"] != ackReactionEmoji {
		t.Errorf("reaction: got %+v, want [{type:emoji emoji:%q}]", gotReaction, ackReactionEmoji)
	}
}

func TestAck_EmptyMessageRef_NoHTTPCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		jsonResponse(w, 200, map[string]any{"ok": true, "result": true})
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, "-100123456789")
	err := tg.Ack(transport.Reply{MessageRef: ""})
	if err == nil {
		t.Fatal("expected error for empty MessageRef")
	}
	if called {
		t.Error("expected no HTTP call for empty MessageRef")
	}
}

func TestAck_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{"ok": false, "description": "REACTION_INVALID"})
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, "-100123456789")
	err := tg.Ack(transport.Reply{MessageRef: "-100123456789:42"})
	if err == nil {
		t.Fatal("expected error for API-level failure")
	}
	if !strings.Contains(err.Error(), "REACTION_INVALID") {
		t.Errorf("error = %q, want it to mention API description", err.Error())
	}
}

func TestAck_ConnectionFailure_NoTokenInError(t *testing.T) {
	tg := newConnFailureTelegram(t)
	err := tg.Ack(transport.Reply{MessageRef: "-100123456789:42"})
	assertErrorHasNoToken(t, err)
}

// TestAck_MalformedMessageRef_NoHTTPCall pins the same error discipline the
// empty-ref check above has, for a ref that carries no usable chat: reject
// before any HTTP call rather than post a half-formed request. The bare
// integer case is the pre-composite shape — a ref in that shape must never
// reach the API, because there is nothing to say WHICH chat it belongs to.
func TestAck_MalformedMessageRef_NoHTTPCall(t *testing.T) {
	for _, ref := range []string{"42", "-100123456789:", ":42", ":"} {
		t.Run(ref, func(t *testing.T) {
			called := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				jsonResponse(w, 200, map[string]any{"ok": true, "result": true})
			}))
			defer srv.Close()

			tg := newTestTelegram(t, srv, "-100123456789")
			if err := tg.Ack(transport.Reply{MessageRef: ref}); err == nil {
				t.Errorf("Ack(%q): expected an error", ref)
			}
			if called {
				t.Errorf("Ack(%q): expected no HTTP call", ref)
			}
		})
	}
}

// TestAck_ChatIDComesFromMessageRefNotConfiguredChat is the regression guard
// for agent-teams-ncn5.3: setMessageReaction must post the chat encoded in
// the ref, not the configured supergroup it used to close over. Both rows run
// against the SAME transport config (a supergroup chat id), so the private-
// chat row fails the moment the chat id goes back to t.chatID.
func TestAck_ChatIDComesFromMessageRefNotConfiguredChat(t *testing.T) {
	const configuredChatID = "-100111222333"

	cases := []struct {
		name          string
		ref           string
		wantChatID    string
		wantMessageID string
	}{
		{
			name:          "group message acks in the supergroup",
			ref:           configuredChatID + ":77",
			wantChatID:    configuredChatID,
			wantMessageID: "77",
		},
		{
			name:          "DM acks in the private chat, not the supergroup",
			ref:           "12345678:9",
			wantChatID:    "12345678",
			wantMessageID: "9",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotChatID, gotMessageID string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/setMessageReaction") {
					http.NotFound(w, r)
					return
				}
				if err := r.ParseForm(); err != nil {
					t.Errorf("ParseForm: %v", err)
				}
				gotChatID = r.FormValue("chat_id")
				gotMessageID = r.FormValue("message_id")
				jsonResponse(w, 200, map[string]any{"ok": true, "result": true})
			}))
			defer srv.Close()

			tg := newTestTelegram(t, srv, configuredChatID)
			if err := tg.Ack(transport.Reply{MessageRef: tc.ref}); err != nil {
				t.Fatalf("Ack: %v", err)
			}
			if gotChatID != tc.wantChatID {
				t.Errorf("chat_id: got %q, want %q", gotChatID, tc.wantChatID)
			}
			if gotMessageID != tc.wantMessageID {
				t.Errorf("message_id: got %q, want %q", gotMessageID, tc.wantMessageID)
			}
		})
	}
}

// TestReceive_PopulatesMessageRefFromMessageID verifies that Receive fills
// reply.MessageRef with the composite "<chat_id>:<message_id>", driven
// through the real getUpdates -> Receive path (not by constructing a
// transport.Reply directly), so the ack seam has something to ack downstream
// that identifies both the message and the chat it lives in.
func TestReceive_PopulatesMessageRefFromMessageID(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)

	updates := []map[string]any{
		{
			"update_id": 1,
			"message": map[string]any{
				"message_id":        77,
				"is_topic_message":  true,
				"message_thread_id": 5,
				"text":              "hi",
				"chat":              map[string]any{"id": chatIDInt},
			},
		},
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			http.NotFound(w, r)
			return
		}
		callCount++
		if callCount == 1 {
			jsonResponse(w, 200, map[string]any{"ok": true, "result": updates})
		} else {
			jsonResponse(w, 200, map[string]any{"ok": true, "result": []any{}})
		}
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		return sentinel
	})

	if len(received) != 1 {
		t.Fatalf("got %d replies, want 1", len(received))
	}
	if want := chatID + ":77"; received[0].MessageRef != want {
		t.Errorf("MessageRef: got %q, want %q", received[0].MessageRef, want)
	}
	if received[0].ThreadRef != "5" {
		t.Errorf("ThreadRef: got %q, want %q", received[0].ThreadRef, "5")
	}
}

// ── Receive: is_topic_message filter ─────────────────────────────────────────

func TestReceive_FiltersIsTopicMessage(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)

	// Two updates: one topic message, one non-topic message.
	updates := []map[string]any{
		{
			"update_id": 1,
			"message": map[string]any{
				"message_id":        10,
				"message_thread_id": 5,
				"is_topic_message":  true,
				"text":              "topic reply",
				"chat":              map[string]any{"id": chatIDInt},
			},
		},
		{
			"update_id": 2,
			"message": map[string]any{
				"message_id": 11,
				"text":       "general message",
				"chat":       map[string]any{"id": chatIDInt},
			},
		},
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			http.NotFound(w, r)
			return
		}
		callCount++
		if callCount == 1 {
			jsonResponse(w, 200, map[string]any{"ok": true, "result": updates})
		} else {
			// Stop after first batch.
			jsonResponse(w, 200, map[string]any{"ok": true, "result": []any{}})
		}
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		if len(received) >= 2 {
			return sentinel
		}
		return nil
	})

	if len(received) != 2 {
		t.Fatalf("got %d replies, want 2", len(received))
	}
	// First: topic message — ThreadRef populated.
	if received[0].ThreadRef != "5" {
		t.Errorf("topic reply ThreadRef: got %q, want %q", received[0].ThreadRef, "5")
	}
	if received[0].Text != "topic reply" {
		t.Errorf("topic reply Text: got %q", received[0].Text)
	}
	// Second: non-topic — ThreadRef empty, leaving the routing decision to
	// the relay's addressing fields.
	if received[1].ThreadRef != "" {
		t.Errorf("non-topic reply ThreadRef: got %q, want empty", received[1].ThreadRef)
	}
	if received[1].Text != "general message" {
		t.Errorf("non-topic reply Text: got %q", received[1].Text)
	}
}

// ── Receive: chat-id allowlist ────────────────────────────────────────────────

func TestReceive_RejectsDifferentChatID(t *testing.T) {
	const allowedChatID = "-100111222333"
	allowedInt, _ := strconv.ParseInt(allowedChatID, 10, 64)
	wrongInt := allowedInt + 1

	updates := []map[string]any{
		{
			"update_id": 1,
			"message": map[string]any{
				"message_id":        10,
				"message_thread_id": 5,
				"is_topic_message":  true,
				"text":              "from wrong chat",
				"chat":              map[string]any{"id": wrongInt},
			},
		},
		{
			"update_id": 2,
			"message": map[string]any{
				"message_id":        11,
				"message_thread_id": 6,
				"is_topic_message":  true,
				"text":              "from right chat",
				"chat":              map[string]any{"id": allowedInt},
			},
		},
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			http.NotFound(w, r)
			return
		}
		callCount++
		if callCount == 1 {
			jsonResponse(w, 200, map[string]any{"ok": true, "result": updates})
		} else {
			jsonResponse(w, 200, map[string]any{"ok": true, "result": []any{}})
		}
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, allowedChatID)

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		return sentinel
	})

	if len(received) != 1 {
		t.Fatalf("got %d replies, want 1 (only from allowed chat)", len(received))
	}
	if received[0].Text != "from right chat" {
		t.Errorf("received wrong message: %q", received[0].Text)
	}
}

// ── Receive: offset advances ──────────────────────────────────────────────────

func TestReceive_OffsetAdvances(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)

	var capturedOffsets []string
	callCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			http.NotFound(w, r)
			return
		}
		rawURL := r.URL.String()
		parsed, _ := url.Parse(rawURL)
		capturedOffsets = append(capturedOffsets, parsed.Query().Get("offset"))

		callCount++
		switch callCount {
		case 1:
			jsonResponse(w, 200, map[string]any{
				"ok": true,
				"result": []map[string]any{
					{
						"update_id": 100,
						"message": map[string]any{
							"message_id":        1,
							"message_thread_id": 3,
							"is_topic_message":  true,
							"text":              "msg1",
							"chat":              map[string]any{"id": chatIDInt},
						},
					},
				},
			})
		case 2:
			jsonResponse(w, 200, map[string]any{
				"ok": true,
				"result": []map[string]any{
					{
						"update_id": 200,
						"message": map[string]any{
							"message_id":        2,
							"message_thread_id": 3,
							"is_topic_message":  true,
							"text":              "msg2",
							"chat":              map[string]any{"id": chatIDInt},
						},
					},
				},
			})
		default:
			jsonResponse(w, 200, map[string]any{"ok": true, "result": []any{}})
		}
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)

	received := 0
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received++
		if received >= 2 {
			return sentinel
		}
		return nil
	})

	// After first batch (update_id=100) offset should advance to 101.
	// After second batch (update_id=200) offset should advance to 201.
	if len(capturedOffsets) < 2 {
		t.Fatalf("expected at least 2 getUpdates calls, got %d", len(capturedOffsets))
	}
	if capturedOffsets[0] != "0" {
		t.Errorf("first call offset: got %q, want %q", capturedOffsets[0], "0")
	}
	if capturedOffsets[1] != "101" {
		t.Errorf("second call offset: got %q, want %q", capturedOffsets[1], "101")
	}
}

// ── General topic (id 1) filter ───────────────────────────────────────────────

func TestReceive_GeneralTopicID1_EmitsEmptyThreadRef(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)

	// A message with message_thread_id=1 (General topic) and is_topic_message=false
	// (General topic doesn't set is_topic_message). Also test one with
	// is_topic_message=false explicitly.
	updates := []map[string]any{
		{
			"update_id": 1,
			"message": map[string]any{
				"message_id":       10,
				"is_topic_message": false,
				"text":             "general msg",
				"chat":             map[string]any{"id": chatIDInt},
			},
		},
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			http.NotFound(w, r)
			return
		}
		callCount++
		if callCount == 1 {
			jsonResponse(w, 200, map[string]any{"ok": true, "result": updates})
		} else {
			jsonResponse(w, 200, map[string]any{"ok": true, "result": []any{}})
		}
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		return sentinel
	})

	if len(received) != 1 {
		t.Fatalf("got %d replies, want 1", len(received))
	}
	if received[0].ThreadRef != "" {
		t.Errorf("General topic ThreadRef: got %q, want empty", received[0].ThreadRef)
	}
	if received[0].Text != "general msg" {
		t.Errorf("General topic Text: got %q", received[0].Text)
	}
}

// ── Receive: media-only messages get a non-empty placeholder body ────────────
//
// TestReceive_MessageBody_MediaPlaceholders drives synthetic getUpdates JSON
// through the REAL Receive path (not messageBody directly) for the core
// media types plus a plain-text control and the fallback case, proving an
// actionable, non-empty reply.Text reaches the handler for every one of
// them — closing the empty-envelope bug end-to-end at the mock-HTTP level.
// TestReceive_MessageBody_MediaPlaceholders drives synthetic getUpdates JSON
// through the real Receive path and asserts, per STRICT (Eric, at-gqqd,
// isContentLess): a reply with no text and no caption is dropped before the
// handler ever runs, while a reply carrying text or a caption reaches the
// handler with messageBody's usual formatting. Cases set `want` (forwards,
// asserted against reply.Text) xor `wantDrop` (never reaches the handler).
func TestReceive_MessageBody_MediaPlaceholders(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)

	baseMsg := func(id int, extra map[string]any) map[string]any {
		m := map[string]any{
			"message_id":        id,
			"message_thread_id": 5,
			"is_topic_message":  true,
			"chat":              map[string]any{"id": chatIDInt},
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	type testCase struct {
		name     string
		msg      map[string]any
		want     string
		wantDrop bool
	}
	cases := []testCase{
		{
			name: "plain text passes through unchanged",
			msg:  baseMsg(1, map[string]any{"text": "hello world"}),
			want: "hello world",
		},
		{
			name:     "bare sticker with emoji drops (no caption)",
			msg:      baseMsg(2, map[string]any{"sticker": map[string]any{"emoji": "😀"}}),
			wantDrop: true,
		},
		{
			name:     "bare sticker without emoji drops (no caption)",
			msg:      baseMsg(3, map[string]any{"sticker": map[string]any{}}),
			wantDrop: true,
		},
		{
			name: "photo with caption forwards",
			msg: baseMsg(4, map[string]any{
				"photo":   []map[string]any{{"file_id": "AAA", "width": 100, "height": 100}},
				"caption": "beach sunset",
			}),
			want: "[photo] beach sunset",
		},
		{
			name: "bare photo without caption drops",
			msg: baseMsg(15, map[string]any{
				"photo": []map[string]any{{"file_id": "III", "width": 100, "height": 100}},
			}),
			wantDrop: true,
		},
		{
			name:     "bare voice note drops (no caption)",
			msg:      baseMsg(5, map[string]any{"voice": map[string]any{"file_id": "BBB", "duration": 5}}),
			wantDrop: true,
		},
		{
			name: "bare document with file_name drops (no caption)",
			msg: baseMsg(6, map[string]any{
				"document": map[string]any{"file_id": "CCC", "file_name": "report.pdf"},
			}),
			wantDrop: true,
		},
		{
			name: "bare animation drops (no caption; animation checked before document — GIF sends both)",
			msg: baseMsg(7, map[string]any{
				"animation": map[string]any{"file_id": "DDD"},
				"document":  map[string]any{"file_id": "DDD", "file_name": "gif.mp4"},
			}),
			wantDrop: true,
		},
		{
			name:     "reaction-like empty-body message drops (unrecognized, no text/caption/media)",
			msg:      baseMsg(8, map[string]any{}),
			wantDrop: true,
		},
		{
			// A `"photo": null` message with no text and no caption is
			// content-less under isContentLess, so Receive drops it before
			// messageBody (and its null-photo decode branch) is ever
			// reached — this case is drop coverage only. The
			// *json.RawMessage null-vs-absent decode guard itself (the
			// json.RawMessage vs *json.RawMessage bug, at-gqqd) is
			// exercised directly by
			// TestMessageBody_ExplicitNullPhotoDecodesAsAbsent below.
			name:     "explicit JSON null for photo drops (content-less: no text/caption)",
			msg:      baseMsg(9, map[string]any{"photo": nil}),
			wantDrop: true,
		},
		{
			name:     "bare video message drops (no caption)",
			msg:      baseMsg(10, map[string]any{"video": map[string]any{"file_id": "EEE", "duration": 12}}),
			wantDrop: true,
		},
		{
			name:     "bare audio message drops (no caption)",
			msg:      baseMsg(11, map[string]any{"audio": map[string]any{"file_id": "FFF", "duration": 30}}),
			wantDrop: true,
		},
		{
			name:     "video_note message drops (Telegram never captions video_note)",
			msg:      baseMsg(12, map[string]any{"video_note": map[string]any{"file_id": "GGG", "duration": 3}}),
			wantDrop: true,
		},
		{
			name: "voice message with caption forwards",
			msg: baseMsg(13, map[string]any{
				"voice":   map[string]any{"file_id": "HHH", "duration": 4},
				"caption": "quick update",
			}),
			want: "[voice message] quick update",
		},
		{
			// This fixture forges a Caption on a sticker to exercise
			// messageBody's sticker branch, which ignores caption entirely
			// — a state that is IMPOSSIBLE in real Telegram traffic (real
			// stickers are never captioned, so real sticker replies always
			// have Caption=="" and always drop, per the bare-sticker cases
			// above). The frozen predicate keys only off the raw
			// Text/Caption fields with no media-type branching, so this
			// forged non-empty Caption alone makes the message
			// content-bearing and it forwards; messageBody then still
			// strips the caption for stickers, producing "[sticker 🎉]".
			// Do not read this case as "captioned stickers survive the
			// filter in production" — they don't, because they don't exist.
			name: "sticker with forged caption forwards (Caption non-empty keeps it content-bearing); messageBody still ignores the caption text",
			msg: baseMsg(14, map[string]any{
				"sticker": map[string]any{"emoji": "🎉"},
				"caption": "should be ignored",
			}),
			want: "[sticker 🎉]",
		},
	}

	// Trailing marker message: guaranteed to forward (plain text), used
	// solely to deterministically stop Receive once the whole batch has
	// been processed, regardless of how many preceding cases dropped.
	const markerText = "END OF BATCH MARKER"
	updates := make([]map[string]any, 0, len(cases)+1)
	for i, tc := range cases {
		updates = append(updates, map[string]any{
			"update_id": 100 + i,
			"message":   tc.msg,
		})
	}
	updates = append(updates, map[string]any{
		"update_id": 100 + len(cases),
		"message":   baseMsg(999, map[string]any{"text": markerText}),
	})

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			http.NotFound(w, r)
			return
		}
		callCount++
		if callCount == 1 {
			jsonResponse(w, 200, map[string]any{"ok": true, "result": updates})
		} else {
			jsonResponse(w, 200, map[string]any{"ok": true, "result": []any{}})
		}
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		if r.Text == markerText {
			return sentinel
		}
		return nil
	})

	var wantForward []testCase
	for _, tc := range cases {
		if !tc.wantDrop {
			wantForward = append(wantForward, tc)
		}
	}

	// received must be exactly the forwarding cases, in order, followed by
	// the trailing marker — proving every dropped case's handler call never
	// happened (a stray call would shift this count and the per-position
	// assertions below).
	if len(received) != len(wantForward)+1 {
		t.Fatalf("got %d replies, want %d (forwarding cases) + 1 (marker)", len(received), len(wantForward)+1)
	}
	for i, tc := range wantForward {
		if received[i].Text != tc.want {
			t.Errorf("%s: reply.Text = %q, want %q", tc.name, received[i].Text, tc.want)
		}
		if received[i].Text == "" {
			t.Errorf("%s: reply.Text is empty — messageBody must never return \"\"", tc.name)
		}
	}
	if last := received[len(received)-1].Text; last != markerText {
		t.Errorf("final reply.Text = %q, want marker %q", last, markerText)
	}
}

// ── messageBody: direct decode-level regression test ─────────────────────────

// TestMessageBody_ExplicitNullPhotoDecodesAsAbsent exercises the JSON decode
// itself — not the Receive drop filter, which now intercepts a null-photo
// message before messageBody ever runs since it carries no text/caption
// (see case 9 of TestReceive_MessageBody_MediaPlaceholders above). This test
// guards the *json.RawMessage fix (at-gqqd) directly: a bare json.RawMessage
// captures the literal bytes of an explicit JSON null, decoding "photo": null
// to a non-nil 4-byte "null" slice that would wrongly satisfy messageBody's
// `case msg.Photo != nil`. The pointer form must decode explicit null to a
// nil pointer, same as an absent key.
func TestMessageBody_ExplicitNullPhotoDecodesAsAbsent(t *testing.T) {
	var msg message
	if err := json.Unmarshal([]byte(`{"photo": null}`), &msg); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if msg.Photo != nil {
		t.Fatalf("msg.Photo = %v, want nil after decoding explicit JSON null (a bare json.RawMessage would leave it non-nil)", msg.Photo)
	}

	// Observable consequence: messageBody must not treat this as a photo —
	// it falls through to the default placeholder instead of "[photo]".
	if got := messageBody(&msg); got == "[photo]" {
		t.Errorf("messageBody with explicit-null photo returned %q, want it NOT to match the photo case", got)
	}
}

// ── @mention parsing ──────────────────────────────────────────────────────────

// newMentionTestServer builds a server that answers getMe with username and
// getUpdates with the given updates (once; empty thereafter). Returns the
// server and a pointer to the getMe call count.
func newMentionTestServer(username string, updates []map[string]any) (*httptest.Server, *int) {
	getMeCalls := 0
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			getMeCalls++
			jsonResponse(w, 200, map[string]any{
				"ok":     true,
				"result": map[string]any{"username": username},
			})
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			callCount++
			if callCount == 1 {
				jsonResponse(w, 200, map[string]any{"ok": true, "result": updates})
			} else {
				jsonResponse(w, 200, map[string]any{"ok": true, "result": []any{}})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	return srv, &getMeCalls
}

func TestReceive_MentionsSelf_CallsGetMeExactlyOnce(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)

	updates := []map[string]any{
		{
			"update_id": 1,
			"message": map[string]any{
				"message_id":       10,
				"is_topic_message": false,
				"text":             "hey @StewardBot need help",
				"entities": []map[string]any{
					{"type": "mention", "offset": 4, "length": 11},
				},
				"chat": map[string]any{"id": chatIDInt},
			},
		},
	}

	srv, getMeCalls := newMentionTestServer("stewardbot", updates)
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		return sentinel
	})

	if *getMeCalls != 1 {
		t.Errorf("getMe calls: got %d, want 1", *getMeCalls)
	}
	if len(received) != 1 {
		t.Fatalf("got %d replies, want 1", len(received))
	}
	if !received[0].MentionsSelf {
		t.Error("MentionsSelf: got false, want true (mention case-folded to own username)")
	}
	if len(received[0].Mentions) != 1 || received[0].Mentions[0] != "stewardbot" {
		t.Errorf("Mentions: got %v, want [stewardbot]", received[0].Mentions)
	}
}

// TestReceive_GetMeFailsThenSucceeds_MentionsSelfMatchesAfterRecovery proves
// a transient getMe failure on Receive's first loop iteration doesn't
// permanently disable MentionsSelf: getMe is retried at the top of each
// subsequent iteration (paced by the existing getUpdates poll, no separate
// retry loop) until it succeeds, after which mentions of this bot resolve
// correctly.
func TestReceive_GetMeFailsThenSucceeds_MentionsSelfMatchesAfterRecovery(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)

	updates := []map[string]any{
		{
			"update_id": 1,
			"message": map[string]any{
				"message_id":       10,
				"is_topic_message": false,
				"text":             "hey @StewardBot need help",
				"entities": []map[string]any{
					{"type": "mention", "offset": 4, "length": 11},
				},
				"chat": map[string]any{"id": chatIDInt},
			},
		},
	}

	getMeCalls := 0
	pollCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			getMeCalls++
			if getMeCalls == 1 {
				// Transient failure on the first attempt.
				jsonResponse(w, 200, map[string]any{"ok": false, "description": "temporary failure"})
				return
			}
			jsonResponse(w, 200, map[string]any{
				"ok":     true,
				"result": map[string]any{"username": "stewardbot"},
			})
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			pollCalls++
			if pollCalls == 1 {
				// First poll: no updates yet, giving getMe a second loop
				// iteration to retry before any message arrives.
				jsonResponse(w, 200, map[string]any{"ok": true, "result": []any{}})
				return
			}
			jsonResponse(w, 200, map[string]any{"ok": true, "result": updates})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		return sentinel
	})

	if getMeCalls != 2 {
		t.Fatalf("getMe calls: got %d, want 2 (fails once, retried and succeeds on the next iteration)", getMeCalls)
	}
	if len(received) != 1 {
		t.Fatalf("got %d replies, want 1", len(received))
	}
	if !received[0].MentionsSelf {
		t.Error("MentionsSelf: got false, want true — getMe should have recovered by the time the mention arrived")
	}
}

func TestReceive_MentionsOtherBot_NotSelf(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)

	updates := []map[string]any{
		{
			"update_id": 1,
			"message": map[string]any{
				"message_id":       10,
				"is_topic_message": false,
				"text":             "hey @xbot handle this",
				"entities": []map[string]any{
					{"type": "mention", "offset": 4, "length": 5},
				},
				"chat": map[string]any{"id": chatIDInt},
			},
		},
	}

	srv, _ := newMentionTestServer("stewardbot", updates)
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		return sentinel
	})

	if len(received) != 1 {
		t.Fatalf("got %d replies, want 1", len(received))
	}
	if received[0].MentionsSelf {
		t.Error("MentionsSelf: got true, want false (mentions another bot, not me)")
	}
	if len(received[0].Mentions) != 1 || received[0].Mentions[0] != "xbot" {
		t.Errorf("Mentions: got %v, want [xbot]", received[0].Mentions)
	}
}

func TestReceive_NoEntities_EmptyMentions(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)

	updates := []map[string]any{
		{
			"update_id": 1,
			"message": map[string]any{
				"message_id":       10,
				"is_topic_message": false,
				"text":             "just a plain message",
				"chat":             map[string]any{"id": chatIDInt},
			},
		},
	}

	srv, _ := newMentionTestServer("stewardbot", updates)
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		return sentinel
	})

	if len(received) != 1 {
		t.Fatalf("got %d replies, want 1", len(received))
	}
	if len(received[0].Mentions) != 0 {
		t.Errorf("Mentions: got %v, want empty", received[0].Mentions)
	}
	if received[0].MentionsSelf {
		t.Error("MentionsSelf: got true, want false")
	}
}

// TestReceive_UTF16Offset_MultiByteCharBeforeMention proves entity
// offset/length are consumed as UTF-16 code units, not runes: "😀" is one
// rune but two UTF-16 code units, so a rune-indexed extraction would
// misalign and miss the mention entirely.
func TestReceive_UTF16Offset_MultiByteCharBeforeMention(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)

	text := "😀 @stewardbot hi"
	updates := []map[string]any{
		{
			"update_id": 1,
			"message": map[string]any{
				"message_id":       10,
				"is_topic_message": false,
				"text":             text,
				"entities": []map[string]any{
					// offset 3 = 2 UTF-16 units for the emoji + 1 for the space.
					{"type": "mention", "offset": 3, "length": 11},
				},
				"chat": map[string]any{"id": chatIDInt},
			},
		},
	}

	srv, _ := newMentionTestServer("stewardbot", updates)
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		return sentinel
	})

	if len(received) != 1 {
		t.Fatalf("got %d replies, want 1", len(received))
	}
	if !received[0].MentionsSelf {
		t.Error("MentionsSelf: got false, want true — offset must be decoded as UTF-16 code units")
	}
	if len(received[0].Mentions) != 1 || received[0].Mentions[0] != "stewardbot" {
		t.Errorf("Mentions: got %v, want [stewardbot]", received[0].Mentions)
	}
}

// ── parseMentions: direct unit tests ──────────────────────────────────────────
//
// These call parseMentions directly (no HTTP round trip through Receive) —
// exercising entity offset/length edge cases the mention tests above don't
// reach: a mention entity flush against either edge of msg.Text, and
// multiple entities (bot + human, or multiple bots) in a single message.

// TestParseMentions_MentionAtOffsetZero verifies a mention entity starting at
// offset 0 (no leading text) is extracted correctly — the boundary opposite
// the one TestReceive_UTF16Offset_MultiByteCharBeforeMention covers.
func TestParseMentions_MentionAtOffsetZero(t *testing.T) {
	tg := &Telegram{ownUsername: "stewardbot"}
	msg := &message{
		Text: "@stewardbot please help",
		Entities: []messageEntity{
			{Type: "mention", Offset: 0, Length: 11},
		},
	}

	mentions, mentionsSelf := tg.parseMentions(msg)

	if !mentionsSelf {
		t.Error("MentionsSelf: got false, want true (mention entity starts at offset 0)")
	}
	if len(mentions) != 1 || mentions[0] != "stewardbot" {
		t.Errorf("Mentions: got %v, want [stewardbot]", mentions)
	}
}

// TestParseMentions_MentionAtEndOfText verifies a mention entity whose
// Offset+Length lands EXACTLY at len(units) — the trailing boundary — is not
// rejected by the "> len(units)" bounds check (an off-by-one there would
// drop a mention with no trailing text).
func TestParseMentions_MentionAtEndOfText(t *testing.T) {
	tg := &Telegram{ownUsername: "stewardbot"}
	text := "please help @stewardbot"
	msg := &message{
		Text: text,
		Entities: []messageEntity{
			// offset 12 = len("please help "); length 11 = len("@stewardbot"),
			// so offset+length == len(text) exactly.
			{Type: "mention", Offset: 12, Length: 11},
		},
	}

	mentions, mentionsSelf := tg.parseMentions(msg)

	if !mentionsSelf {
		t.Error("MentionsSelf: got false, want true (mention entity ends exactly at text length)")
	}
	if len(mentions) != 1 || mentions[0] != "stewardbot" {
		t.Errorf("Mentions: got %v, want [stewardbot]", mentions)
	}
}

// TestParseMentions_MultipleMentionsMixedHumanAndBot verifies a message with
// THREE mention entities — human, this bot, and another bot — extracts all
// of them in order and still correctly flags MentionsSelf, proving multiple
// bot mentions plus a mixed human mention don't interfere with each other
// (relay's firstBotMention/MentionsSelf logic depends on Mentions preserving
// every entity, not just the first).
func TestParseMentions_MultipleMentionsMixedHumanAndBot(t *testing.T) {
	tg := &Telegram{ownUsername: "stewardbot"}
	text := "@eric @stewardbot @otherbot"
	msg := &message{
		Text: text,
		Entities: []messageEntity{
			{Type: "mention", Offset: 0, Length: 5},  // "@eric"
			{Type: "mention", Offset: 6, Length: 11}, // "@stewardbot"
			{Type: "mention", Offset: 18, Length: 9}, // "@otherbot"
		},
	}

	mentions, mentionsSelf := tg.parseMentions(msg)

	if !mentionsSelf {
		t.Error("MentionsSelf: got false, want true (stewardbot is one of three mentions)")
	}
	want := []string{"eric", "stewardbot", "otherbot"}
	if len(mentions) != len(want) {
		t.Fatalf("Mentions: got %v, want %v", mentions, want)
	}
	for i, m := range want {
		if mentions[i] != m {
			t.Errorf("Mentions[%d] = %q, want %q", i, mentions[i], m)
		}
	}
}

// ── Send: General channel ─────────────────────────────────────────────────────

func TestSend_General_OmitsThreadIDAndReturnsEmpty(t *testing.T) {
	const chatID = "-100123456789"

	var gotCreateTopic, gotSendMessage, sawThreadIDKey bool
	var gotText string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/createForumTopic"):
			gotCreateTopic = true
			jsonResponse(w, 200, map[string]any{"ok": true, "result": map[string]any{"message_thread_id": 1}})
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			gotSendMessage = true
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			_, sawThreadIDKey = r.PostForm["message_thread_id"]
			gotText = r.FormValue("text")
			jsonResponse(w, 200, map[string]any{"ok": true, "result": map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	threadRef, err := tg.Send(transport.OutboundMessage{
		InitiativeID: "at-00o",
		Title:        "Steward",
		Body:         "hello from general",
		General:      true,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if threadRef != "" {
		t.Errorf("threadRef: got %q, want empty", threadRef)
	}
	if gotCreateTopic {
		t.Error("createForumTopic should not be called for General sends")
	}
	if !gotSendMessage {
		t.Error("sendMessage was not called")
	}
	if sawThreadIDKey {
		t.Error("message_thread_id key must be omitted from PostForm values for General sends")
	}
	if gotText != "hello from general" {
		t.Errorf("sendMessage text: got %q, want %q", gotText, "hello from general")
	}
}

// ── Token never leaks into transport-level errors ─────────────────────────────

const fakeToken = "1234567:FAKE-token-must-not-appear"

// closedPortBaseURL returns an http:// base URL pointing at a TCP port with
// no listener, so PostForm/Get fail with a connection-refused *url.Error —
// whose unsanitized Error() string would include the full request URL, and
// therefore the bot token embedded in it (see apiURL).
func closedPortBaseURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("ln.Close: %v", err)
	}
	return "http://" + addr
}

func newConnFailureTelegram(t *testing.T) *Telegram {
	t.Helper()
	return &Telegram{
		token:      fakeToken,
		chatID:     "-100123456789",
		httpClient: &http.Client{Timeout: 2 * time.Second},
		baseURL:    closedPortBaseURL(t),
	}
}

func assertErrorHasNoToken(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a connection error, got nil")
	}
	if strings.Contains(err.Error(), fakeToken) {
		t.Fatalf("error leaked the bot token: %v", err)
	}
}

func TestCreateForumTopic_ConnectionFailure_NoTokenInError(t *testing.T) {
	tg := newConnFailureTelegram(t)
	_, err := tg.createForumTopic("topic")
	assertErrorHasNoToken(t, err)
}

func TestSendMessage_ConnectionFailure_NoTokenInError(t *testing.T) {
	tg := newConnFailureTelegram(t)
	err := tg.sendMessage("-100123456789", "7", "hello")
	assertErrorHasNoToken(t, err)
}

func TestGetUpdates_ConnectionFailure_NoTokenInError(t *testing.T) {
	tg := newConnFailureTelegram(t)
	_, err := tg.getUpdates(0)
	assertErrorHasNoToken(t, err)
}

// ── logging (agent-teams-a0ml.1) ─────────────────────────────────────────────

// TestReceive_PollHeartbeat_LogsOffsetAndUpdateCount proves REQUIRED #2 (a
// heartbeat every poll cycle, even a zero-update one) and SUGGESTED #1
// (offset before/after in the same line). The heartbeat is emitted after the
// per-update loop, so a poll whose handler call returns the sentinel never
// logs its own heartbeat (Receive returns immediately) — the fixture
// therefore lets the first real message's handler return nil (so its poll's
// heartbeat logs normally) and only stops on a later marker message.
func TestReceive_PollHeartbeat_LogsOffsetAndUpdateCount(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)

	msg := func(id int, extra map[string]any) map[string]any {
		m := map[string]any{
			"message_id":        id,
			"message_thread_id": 3,
			"is_topic_message":  true,
			"chat":              map[string]any{"id": chatIDInt},
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			http.NotFound(w, r)
			return
		}
		callCount++
		switch callCount {
		case 1:
			// Zero updates — heartbeat must still fire.
			jsonResponse(w, 200, map[string]any{"ok": true, "result": []any{}})
		case 2:
			jsonResponse(w, 200, map[string]any{
				"ok": true,
				"result": []map[string]any{
					{"update_id": 100, "message": msg(1, map[string]any{"text": "msg1"})},
				},
			})
		case 3:
			jsonResponse(w, 200, map[string]any{
				"ok": true,
				"result": []map[string]any{
					{"update_id": 200, "message": msg(2, map[string]any{"text": "marker"})},
				},
			})
		default:
			jsonResponse(w, 200, map[string]any{"ok": true, "result": []any{}})
		}
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	var logBuf bytes.Buffer
	tg.logOut = &logBuf

	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		if r.Text == "marker" {
			return sentinel
		}
		return nil
	})

	got := logBuf.String()
	if !strings.Contains(got, "poll: offset=0 -> 0, 0 update(s)") {
		t.Errorf("expected zero-update heartbeat line, got:\n%s", got)
	}
	if !strings.Contains(got, "poll: offset=0 -> 101, 1 update(s)") {
		t.Errorf("expected offset-advance heartbeat line, got:\n%s", got)
	}
}

// TestReceive_BotIdentityLoggedExactlyOnce reuses the
// TestReceive_MentionsSelf_CallsGetMeExactlyOnce fixture and additionally
// asserts the "bot identity resolved" line appears exactly once — the
// existing if t.ownUsername == "" guard means it must not repeat on later
// iterations after resolution.
func TestReceive_BotIdentityLoggedExactlyOnce(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)

	updates := []map[string]any{
		{
			"update_id": 1,
			"message": map[string]any{
				"message_id":       10,
				"is_topic_message": false,
				"text":             "hey @StewardBot need help",
				"entities": []map[string]any{
					{"type": "mention", "offset": 4, "length": 11},
				},
				"chat": map[string]any{"id": chatIDInt},
			},
		},
	}

	srv, _ := newMentionTestServer("stewardbot", updates)
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	var logBuf bytes.Buffer
	tg.logOut = &logBuf

	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(transport.Reply) error { return sentinel })

	got := logBuf.String()
	if n := strings.Count(got, "bot identity resolved: @stewardbot"); n != 1 {
		t.Errorf("expected bot identity log line exactly once, got %d in:\n%s", n, got)
	}
}

// TestReceive_ChatIDRejectLogged proves the chat-id-reject filtering
// decision (previously silent) now produces a depth-1 log line, reusing the
// TestReceive_RejectsDifferentChatID fixture.
func TestReceive_ChatIDRejectLogged(t *testing.T) {
	const allowedChatID = "-100111222333"
	allowedInt, _ := strconv.ParseInt(allowedChatID, 10, 64)
	wrongInt := allowedInt + 1

	updates := []map[string]any{
		{
			"update_id": 1,
			"message": map[string]any{
				"message_id":        10,
				"message_thread_id": 5,
				"is_topic_message":  true,
				"text":              "from wrong chat",
				"chat":              map[string]any{"id": wrongInt},
			},
		},
		{
			"update_id": 2,
			"message": map[string]any{
				"message_id":        11,
				"message_thread_id": 6,
				"is_topic_message":  true,
				"text":              "from right chat",
				"chat":              map[string]any{"id": allowedInt},
			},
		},
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			http.NotFound(w, r)
			return
		}
		callCount++
		if callCount == 1 {
			jsonResponse(w, 200, map[string]any{"ok": true, "result": updates})
		} else {
			jsonResponse(w, 200, map[string]any{"ok": true, "result": []any{}})
		}
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, allowedChatID)
	var logBuf bytes.Buffer
	tg.logOut = &logBuf

	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(transport.Reply) error { return sentinel })

	var found string
	for _, line := range strings.Split(logBuf.String(), "\n") {
		if strings.Contains(line, "update 1: rejected (chat") {
			found = line
			break
		}
	}
	if found == "" {
		t.Fatalf("expected chat-id-reject log line, got:\n%s", logBuf.String())
	}
	if depth := logLineDepth(t, found); depth != 1 {
		t.Errorf("expected depth-1 log line, got depth %d: %q", depth, found)
	}
}

// TestReceive_ContentLessDropLogged proves the content-less-drop decision
// (STRICT, at-gqqd) now produces a depth-1 log line instead of the old
// unstamped fmt.Fprintln.
func TestReceive_ContentLessDropLogged(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)

	updates := []map[string]any{
		{
			"update_id": 1,
			"message": map[string]any{
				"message_id":        10,
				"message_thread_id": 5,
				"is_topic_message":  true,
				"chat":              map[string]any{"id": chatIDInt},
				// no text, no caption, no media — content-less.
			},
		},
		{
			"update_id": 2,
			"message": map[string]any{
				"message_id":        11,
				"message_thread_id": 5,
				"is_topic_message":  true,
				"text":              "forwards",
				"chat":              map[string]any{"id": chatIDInt},
			},
		},
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			http.NotFound(w, r)
			return
		}
		callCount++
		if callCount == 1 {
			jsonResponse(w, 200, map[string]any{"ok": true, "result": updates})
		} else {
			jsonResponse(w, 200, map[string]any{"ok": true, "result": []any{}})
		}
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	var logBuf bytes.Buffer
	tg.logOut = &logBuf

	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(transport.Reply) error { return sentinel })

	var found string
	for _, line := range strings.Split(logBuf.String(), "\n") {
		if strings.Contains(line, "update 1: dropped (no text/caption)") {
			found = line
			break
		}
	}
	if found == "" {
		t.Fatalf("expected content-less-drop log line, got:\n%s", logBuf.String())
	}
	if depth := logLineDepth(t, found); depth != 1 {
		t.Errorf("expected depth-1 log line, got depth %d: %q", depth, found)
	}
}

// TestNew_WritesNothingToLog is the regression guard for the bug this test
// was reworked to catch: the one-time startup config line used to live in
// New(), but New is also the registered transport factory — transport.
// Enabled and transport.For call it to merely PROBE config resolvability
// (internal/verbs/notify.go's gate auto-notify, internal/verbs/dispatch.go's
// transportEnabled) — so every ateam notify/gate/dispatch call was printing
// relay-startup noise to stderr. New() must write nothing to the log;
// captured by temporarily redirecting the package-level os.Stderr (New's
// default logOut) to a pipe.
func TestNew_WritesNothingToLog(t *testing.T) {
	pinTelegramEnv(t, fakeToken, "-100999888777", "")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w

	_, newErr := New(t.TempDir(), &http.Client{})

	w.Close()
	os.Stderr = orig
	if newErr != nil {
		t.Fatalf("New: %v", newErr)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("New() must write nothing to the log (it's also transport.Enabled/transport.For's config-probe path), got: %q", buf.String())
	}
}

// ── Direct chats / DM allow-list (agent-teams-ncn5.2) ────────────────────────

// dmUpdate builds a private-chat (DM) update: no is_topic_message, no
// message_thread_id, no entities, and a POSITIVE chat id which — per the Bot
// API — is the sender's own Telegram user id.
func dmUpdate(updateID, messageID int, senderID int64, text string) map[string]any {
	return map[string]any{
		"update_id": updateID,
		"message": map[string]any{
			"message_id": messageID,
			"text":       text,
			"chat":       map[string]any{"id": senderID, "type": "private"},
			"from":       map[string]any{"id": senderID, "username": "eric", "is_bot": false},
		},
	}
}

// groupUpdate builds a topic message in the configured supergroup — the
// existing, unchanged inbound path.
func groupUpdate(updateID, messageID int, chatID int64, text string) map[string]any {
	return map[string]any{
		"update_id": updateID,
		"message": map[string]any{
			"message_id":        messageID,
			"message_thread_id": 5,
			"is_topic_message":  true,
			"text":              text,
			"chat":              map[string]any{"id": chatID, "type": "supergroup"},
		},
	}
}

// groupUpdateFrom is groupUpdate plus a from field — the shape a real
// message from a human sender carries, needed to exercise the dm-allowlist
// bootstrap log line (agent-teams-ncn5.15).
func groupUpdateFrom(updateID, messageID int, chatID, senderID int64, username, text string) map[string]any {
	return map[string]any{
		"update_id": updateID,
		"message": map[string]any{
			"message_id":        messageID,
			"message_thread_id": 5,
			"is_topic_message":  true,
			"text":              text,
			"chat":              map[string]any{"id": chatID, "type": "supergroup"},
			"from":              map[string]any{"id": senderID, "username": username, "is_bot": false},
		},
	}
}

// TestReceive_AllowlistedDM_AdmittedAsDirect proves the admitted half of the
// gate end-to-end through the real Receive path: an allow-listed sender's DM
// reaches the handler with Direct==true and an empty ThreadRef (a DM has
// neither is_topic_message nor message_thread_id), while a message from the
// configured supergroup in the same batch is untouched — Direct==false, its
// ThreadRef still populated.
func TestReceive_AllowlistedDM_AdmittedAsDirect(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)
	const senderID = int64(12345678)

	srv := newUpdatesServer([]map[string]any{
		dmUpdate(1, 10, senderID, "hey, what's up"),
		groupUpdate(2, 11, chatIDInt, "group message"),
	})
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	tg.dmAllowlist = map[int64]bool{senderID: true}

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		if len(received) >= 2 {
			return sentinel
		}
		return nil
	})

	if len(received) != 2 {
		t.Fatalf("got %d replies, want 2 (the DM and the group message)", len(received))
	}
	dm := received[0]
	if !dm.Direct {
		t.Error("DM Direct: got false, want true")
	}
	if dm.ThreadRef != "" {
		t.Errorf("DM ThreadRef: got %q, want empty (a DM has no forum topic)", dm.ThreadRef)
	}
	if dm.Text != "hey, what's up" {
		t.Errorf("DM Text: got %q", dm.Text)
	}
	if want := strconv.FormatInt(senderID, 10) + ":10"; dm.MessageRef != want {
		t.Errorf("DM MessageRef: got %q, want %q (composite, carrying the PRIVATE chat)", dm.MessageRef, want)
	}
	group := received[1]
	if group.Direct {
		t.Error("group message Direct: got true, want false")
	}
	if group.ThreadRef != "5" {
		t.Errorf("group message ThreadRef: got %q, want %q", group.ThreadRef, "5")
	}
	if want := chatID + ":11"; group.MessageRef != want {
		t.Errorf("group MessageRef: got %q, want %q (same composite shape as the DM)", group.MessageRef, want)
	}
}

// TestReceive_ThenAck_DMReadReceiptLandsInThePrivateChat closes the loop the
// composite ref exists for, end-to-end inside this package: an admitted DM's
// ref is carried from Receive straight into Ack (exactly as the relay does)
// and the resulting setMessageReaction must target the SENDER's private chat
// — never the configured supergroup, where that message id belongs to some
// unrelated older message.
func TestReceive_ThenAck_DMReadReceiptLandsInThePrivateChat(t *testing.T) {
	const chatID = "-100111222333"
	const senderID = int64(12345678)
	const dmMessageID = 10

	var gotChatID, gotMessageID string
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			callCount++
			if callCount == 1 {
				jsonResponse(w, 200, map[string]any{
					"ok":     true,
					"result": []map[string]any{dmUpdate(1, dmMessageID, senderID, "hey")},
				})
				return
			}
			jsonResponse(w, 200, map[string]any{"ok": true, "result": []any{}})
		case strings.HasSuffix(r.URL.Path, "/setMessageReaction"):
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			gotChatID = r.FormValue("chat_id")
			gotMessageID = r.FormValue("message_id")
			jsonResponse(w, 200, map[string]any{"ok": true, "result": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	tg.dmAllowlist = map[int64]bool{senderID: true}

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		return sentinel
	})

	if len(received) != 1 {
		t.Fatalf("got %d replies, want 1 (the DM)", len(received))
	}
	if err := tg.Ack(received[0]); err != nil {
		t.Fatalf("Ack: %v", err)
	}

	wantChatID := strconv.FormatInt(senderID, 10)
	if gotChatID != wantChatID {
		t.Errorf("setMessageReaction chat_id: got %q, want %q (the private chat, NOT the configured supergroup %q)", gotChatID, wantChatID, chatID)
	}
	if gotMessageID != strconv.Itoa(dmMessageID) {
		t.Errorf("setMessageReaction message_id: got %q, want %q", gotMessageID, strconv.Itoa(dmMessageID))
	}
}

// TestReceive_NonAllowlistedDM_DroppedAndSenderIDLogged proves the rejected
// half: the DM never reaches the handler, and the depth-1 log line carries the
// sender id — the number Eric copies into the allow-list, and the only
// bootstrap path there is — while carrying NONE of the message text.
func TestReceive_NonAllowlistedDM_DroppedAndSenderIDLogged(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)
	const strangerID = int64(99887766)
	const dmText = "private words that must not be logged"

	srv := newUpdatesServer([]map[string]any{
		dmUpdate(1, 10, strangerID, dmText),
		groupUpdate(2, 11, chatIDInt, "marker"),
	})
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	tg.dmAllowlist = map[int64]bool{12345678: true} // someone else
	var logBuf bytes.Buffer
	tg.logOut = &logBuf

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		return sentinel
	})

	if len(received) != 1 {
		t.Fatalf("got %d replies, want 1 (only the group marker)", len(received))
	}
	if received[0].Text != "marker" {
		t.Errorf("received the DM instead of the marker: %q", received[0].Text)
	}

	got := logBuf.String()
	var found string
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "rejected DM") {
			found = line
			break
		}
	}
	if found == "" {
		t.Fatalf("expected a rejected-DM log line, got:\n%s", got)
	}
	if !strings.Contains(found, strconv.FormatInt(strangerID, 10)) {
		t.Errorf("rejected-DM line must carry the sender id %d: %q", strangerID, found)
	}
	if depth := logLineDepth(t, found); depth != 1 {
		t.Errorf("expected depth-1 log line, got depth %d: %q", depth, found)
	}
	if strings.Contains(got, dmText) {
		t.Fatalf("log leaked the rejected DM's message text:\n%s", got)
	}
}

// TestReceive_GroupMessage_LogsSenderIDOnceForBootstrap proves the
// dm-allowlist bootstrap half of agent-teams-ncn5.15: a sender's numeric id
// is decoded from an ordinary group message and logged, so it's
// discoverable from traffic Eric already generates rather than only from a
// deliberately-triggered DM rejection. A second message from the same
// sender must not log again — the whole point is a one-time lookup, not a
// line per message — and the message text must never appear in the log.
func TestReceive_GroupMessage_LogsSenderIDOnceForBootstrap(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)
	const senderID = int64(55554444)
	const msgText = "words that must not be logged"

	srv := newUpdatesServer([]map[string]any{
		groupUpdateFrom(1, 10, chatIDInt, senderID, "eric", msgText),
		groupUpdateFrom(2, 11, chatIDInt, senderID, "eric", "second message, same sender"),
	})
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	var logBuf bytes.Buffer
	tg.logOut = &logBuf

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		if len(received) >= 2 {
			return sentinel
		}
		return nil
	})

	if len(received) != 2 {
		t.Fatalf("got %d replies, want 2 (bootstrap logging must not filter group messages)", len(received))
	}

	got := logBuf.String()
	var senderLines []string
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "sender id") {
			senderLines = append(senderLines, line)
		}
	}
	if len(senderLines) != 1 {
		t.Fatalf("got %d sender-id log lines, want exactly 1 (once per sender, not once per message):\n%s", len(senderLines), got)
	}
	if !strings.Contains(senderLines[0], strconv.FormatInt(senderID, 10)) {
		t.Errorf("sender-id line must carry the sender id %d: %q", senderID, senderLines[0])
	}
	if !strings.Contains(senderLines[0], "@eric") {
		t.Errorf("sender-id line must carry the @username: %q", senderLines[0])
	}
	if depth := logLineDepth(t, senderLines[0]); depth != 1 {
		t.Errorf("expected depth-1 log line, got depth %d: %q", depth, senderLines[0])
	}
	if strings.Contains(got, msgText) {
		t.Fatalf("log leaked the group message's text:\n%s", got)
	}
}

// TestReceive_EmptyDMAllowlist_AdmitsNobody pins the default: with no
// allow-list configured, a DM is dropped exactly as it was before DMs were
// admitted at all.
func TestReceive_EmptyDMAllowlist_AdmitsNobody(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)

	srv := newUpdatesServer([]map[string]any{
		dmUpdate(1, 10, 12345678, "let me in"),
		groupUpdate(2, 11, chatIDInt, "marker"),
	})
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID) // dmAllowlist left nil
	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		return sentinel
	})

	if len(received) != 1 {
		t.Fatalf("got %d replies, want 1 (only the group marker)", len(received))
	}
	if received[0].Text != "marker" {
		t.Errorf("an empty allow-list admitted a DM: %q", received[0].Text)
	}
}

// TestReceive_AllowlistedDMWithCaptionNoText_Forwards proves isContentLess
// (telegram.go:264-273, in the admission switch) applies to DMs exactly as
// it does to group messages (see TestReceive_MessageBody_MediaPlaceholders):
// a DM carrying a caption but no text is content-bearing and forwards, not
// silently dropped as content-less.
func TestReceive_AllowlistedDMWithCaptionNoText_Forwards(t *testing.T) {
	const chatID = "-100111222333"
	const senderID = int64(12345678)

	update := map[string]any{
		"update_id": 1,
		"message": map[string]any{
			"message_id": 10,
			"photo":      []map[string]any{{"file_id": "AAA", "width": 100, "height": 100}},
			"caption":    "beach sunset",
			"chat":       map[string]any{"id": senderID, "type": "private"},
			"from":       map[string]any{"id": senderID, "username": "eric", "is_bot": false},
		},
	}

	srv := newUpdatesServer([]map[string]any{update})
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	tg.dmAllowlist = map[int64]bool{senderID: true}

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		return sentinel
	})

	if len(received) != 1 {
		t.Fatalf("got %d replies, want 1 (caption makes it content-bearing)", len(received))
	}
	if !received[0].Direct {
		t.Error("Direct: got false, want true")
	}
	if received[0].Text != "[photo] beach sunset" {
		t.Errorf("Text: got %q, want %q", received[0].Text, "[photo] beach sunset")
	}
}

// TestReceive_ContentLessDM_AuthorizationCheckedBeforeContentCheck pins the
// admission switch's order (telegram.go: the allow-list gate runs first in
// the switch, and the content-less check runs strictly after it, only for
// messages that already passed admission). A non-allow-listed sender's
// content-less DM is dropped for the AUTH reason ("rejected DM") and never
// reaches the content-less check at all, while an allow-listed sender's
// content-less DM passes the gate and is dropped by the content-less check
// instead ("dropped (no text/caption)"). If a later change reordered these
// two checks, one of these two rows would start logging the wrong line.
func TestReceive_ContentLessDM_AuthorizationCheckedBeforeContentCheck(t *testing.T) {
	const chatID = "-100111222333"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)

	for _, tt := range []struct {
		name           string
		senderID       int64
		allowlisted    bool
		wantLogSubstr  string
		dontWantSubstr string
	}{
		{
			name:           "allow-listed sender: passes auth, dropped as content-less",
			senderID:       12345678,
			allowlisted:    true,
			wantLogSubstr:  "dropped (no text/caption)",
			dontWantSubstr: "rejected DM",
		},
		{
			name:           "non-allow-listed sender: dropped at auth, content-less check never reached",
			senderID:       99887766,
			allowlisted:    false,
			wantLogSubstr:  "rejected DM",
			dontWantSubstr: "dropped (no text/caption)",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			contentLessDM := map[string]any{
				"update_id": 1,
				"message": map[string]any{
					"message_id": 10,
					"chat":       map[string]any{"id": tt.senderID, "type": "private"},
					"from":       map[string]any{"id": tt.senderID, "username": "eric", "is_bot": false},
					// no text, no caption — content-less.
				},
			}

			srv := newUpdatesServer([]map[string]any{
				contentLessDM,
				groupUpdate(2, 11, chatIDInt, "marker"),
			})
			defer srv.Close()

			tg := newTestTelegram(t, srv, chatID)
			if tt.allowlisted {
				tg.dmAllowlist = map[int64]bool{tt.senderID: true}
			}
			var logBuf bytes.Buffer
			tg.logOut = &logBuf

			var received []transport.Reply
			sentinel := fmt.Errorf("stop")
			_ = tg.Receive(func(r transport.Reply) error {
				received = append(received, r)
				return sentinel
			})

			if len(received) != 1 {
				t.Fatalf("got %d replies, want 1 (only the trailing marker; the content-less DM must never reach the handler either way)", len(received))
			}
			if received[0].Text != "marker" {
				t.Errorf("received the content-less DM instead of the marker: %q", received[0].Text)
			}

			got := logBuf.String()
			if !strings.Contains(got, tt.wantLogSubstr) {
				t.Errorf("expected log to contain %q, got:\n%s", tt.wantLogSubstr, got)
			}
			if strings.Contains(got, tt.dontWantSubstr) {
				t.Errorf("expected log NOT to contain %q, got:\n%s", tt.dontWantSubstr, got)
			}
		})
	}
}

// TestReceive_DMFromBot_TreatedIdenticallyToHuman documents a real gap: the
// user struct (telegram.go) never decodes from.is_bot, so a DM whose sender
// is itself a bot is indistinguishable from a human DM once it reaches this
// package — an allow-listed bot id is admitted exactly like an allow-listed
// human id. Pinned so that if is_bot decoding is ever added (and someone
// wires up bot-filtering on top of it), the behavior change shows up as an
// intentional diff here, not a silent one.
func TestReceive_DMFromBot_TreatedIdenticallyToHuman(t *testing.T) {
	const chatID = "-100111222333"
	const senderID = int64(55566677)

	update := map[string]any{
		"update_id": 1,
		"message": map[string]any{
			"message_id": 10,
			"text":       "beep boop",
			"chat":       map[string]any{"id": senderID, "type": "private"},
			"from":       map[string]any{"id": senderID, "username": "somebot", "is_bot": true},
		},
	}

	srv := newUpdatesServer([]map[string]any{update})
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	tg.dmAllowlist = map[int64]bool{senderID: true}

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		return sentinel
	})

	if len(received) != 1 {
		t.Fatalf("got %d replies, want 1 (is_bot is not decoded, so an allow-listed bot sender is admitted like any other)", len(received))
	}
	if !received[0].Direct {
		t.Error("Direct: got false, want true")
	}
	if received[0].Text != "beep boop" {
		t.Errorf("Text: got %q", received[0].Text)
	}
}

// TestReceive_PrivateChatIDEqualsConfiguredChatID_GroupBranchWins pins the
// switch's case order (telegram.go: "The configured-chat branch is checked
// FIRST on purpose"): if a private chat's id ever coincidentally matched the
// configured chat id as a STRING, the configured-chat case wins — the
// message is treated as the group, never evaluated as a DM. In production
// this collision cannot happen (a configured chat id is always a negative
// supergroup id and a private chat id is always positive — parseDMAllowlist
// even rejects negative allow-list entries for exactly this reason), but the
// switch itself does not encode that assumption. This test exercises the
// ordering directly rather than trusting it holds by convention.
func TestReceive_PrivateChatIDEqualsConfiguredChatID_GroupBranchWins(t *testing.T) {
	const coincidentID = int64(555000111)
	chatID := strconv.FormatInt(coincidentID, 10)

	update := map[string]any{
		"update_id": 1,
		"message": map[string]any{
			"message_id": 10,
			"text":       "hello",
			"chat":       map[string]any{"id": coincidentID, "type": "private"},
			"from":       map[string]any{"id": coincidentID, "username": "eric", "is_bot": false},
		},
	}

	srv := newUpdatesServer([]map[string]any{update})
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID) // dmAllowlist left nil — irrelevant if the group branch wins

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		return sentinel
	})

	if len(received) != 1 {
		t.Fatalf("got %d replies, want 1 (configured-chat branch must win even with no allow-list entry)", len(received))
	}
	if received[0].Direct {
		t.Error("Direct: got true, want false (the configured-chat case must win over the private-chat case)")
	}
}

// TestReceive_EditedMessageUpdate_SilentlyDropped documents a real gap: the
// update struct (telegram.go) has no EditedMessage field, so an update whose
// JSON carries "edited_message" instead of "message" decodes with
// Message == nil and is dropped by the pre-existing `if msg == nil {
// continue }` guard — with NO log line at all, not even at depth 1. This is
// true for an edited DM exactly as it is for an edited group message;
// nothing about it is DM-specific. Pinned so a later reader does not assume
// edits are inspected anywhere in this package.
func TestReceive_EditedMessageUpdate_SilentlyDropped(t *testing.T) {
	const chatID = "-100111222333"
	const senderID = int64(12345678)

	editedUpdate := map[string]any{
		"update_id": 1,
		"edited_message": map[string]any{
			"message_id": 10,
			"text":       "edited text",
			"chat":       map[string]any{"id": senderID, "type": "private"},
			"from":       map[string]any{"id": senderID, "username": "eric", "is_bot": false},
		},
	}

	srv := newUpdatesServer([]map[string]any{
		editedUpdate,
		dmUpdate(2, 11, senderID, "marker"),
	})
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	tg.dmAllowlist = map[int64]bool{senderID: true}
	var logBuf bytes.Buffer
	tg.logOut = &logBuf

	var received []transport.Reply
	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(r transport.Reply) error {
		received = append(received, r)
		return sentinel
	})

	if len(received) != 1 {
		t.Fatalf("got %d replies, want 1 (only the trailing marker; edited_message is never inspected)", len(received))
	}
	if received[0].Text != "marker" {
		t.Errorf("received the edited-message update instead of the marker: %q", received[0].Text)
	}
	if got := logBuf.String(); strings.Contains(got, "update 1:") {
		t.Errorf("expected NO log line for update 1 (edited_message is dropped silently, not logged), got:\n%s", got)
	}
}

// TestParseDMAllowlist_LinesCommasCommentsBlanks covers the documented format
// in one pass: one-per-line, comma-separated, a trailing comment, a whole-line
// comment, and blank lines.
func TestParseDMAllowlist_LinesCommasCommentsBlanks(t *testing.T) {
	raw := "# Eric's phones\n111, 222\n\n333 # laptop\n"

	allow, err := parseDMAllowlist(raw)
	if err != nil {
		t.Fatalf("parseDMAllowlist: %v", err)
	}
	for _, id := range []int64{111, 222, 333} {
		if !allow[id] {
			t.Errorf("id %d: not allow-listed, want allow-listed (got map %v)", id, allow)
		}
	}
	if len(allow) != 3 {
		t.Errorf("allow-list size: got %d (%v), want 3", len(allow), allow)
	}
}

// TestParseDMAllowlist_EmptyInputYieldsNoEntries pairs with
// TestReceive_EmptyDMAllowlist_AdmitsNobody at the parse level: absent or
// whitespace-only config must not become an entry.
func TestParseDMAllowlist_EmptyInputYieldsNoEntries(t *testing.T) {
	allow, err := parseDMAllowlist("")
	if err != nil {
		t.Fatalf("parseDMAllowlist(\"\"): %v", err)
	}
	if len(allow) != 0 {
		t.Errorf("empty input produced %v, want no entries", allow)
	}
}

// TestNew_MalformedDMAllowlist_FailsLoudlyNamingTheLine is the acceptance
// criterion for the fail-loud rule: a bad entry must abort construction with
// an error naming the offending line, never be skipped. A skipped entry means
// Eric believes he is allow-listed when he is not, and the only symptom is a
// DM that vanishes with no diagnostic.
func TestNew_MalformedDMAllowlist_FailsLoudlyNamingTheLine(t *testing.T) {
	pinTelegramEnv(t, fakeToken, "-100999888777", "111\nnot-an-id\n222")

	_, err := New(t.TempDir(), &http.Client{})
	if err == nil {
		t.Fatal("expected New to fail on a malformed allow-list entry, got nil")
	}
	if !strings.Contains(err.Error(), "not-an-id") {
		t.Errorf("error must name the offending entry: %v", err)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error must name the offending line number: %v", err)
	}
}

// TestNew_DMAllowlistFromFile_LeadingBlankLine_ReportsFileLineNumber is the
// regression for agent-teams-ncn5.16: loadOptionalSecret used to trim the
// file before parseDMAllowlist split it into lines, so a leading blank line
// shifted every reported line number off by one from the file a human
// actually opens to fix it. The fixture below MUST start with a literal
// blank line — not a leading comment, which strings.TrimSpace never
// touched and so never triggered the bug (see the correction on
// agent-teams-ncn5.16's filing).
//
// File, physical lines: 1 blank, 2 "111", 3 "not-an-id". Before the fix,
// trimming line 1 away made "not-an-id" (real line 3) get reported as line
// 2 — the line Eric would "fix" is the wrong one.
func TestNew_DMAllowlistFromFile_LeadingBlankLine_ReportsFileLineNumber(t *testing.T) {
	pinTelegramEnv(t, fakeToken, "-100999888777", "")

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "telegram"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "telegram", "dm-allowlist"), []byte("\n111\nnot-an-id\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := New(home, &http.Client{})
	if err == nil {
		t.Fatal("expected New to fail on a malformed allow-list entry, got nil")
	}
	if !strings.Contains(err.Error(), "not-an-id") {
		t.Errorf("error must name the offending entry: %v", err)
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("error must name the FILE's line 3 (1: blank, 2: 111, 3: not-an-id): %v", err)
	}
	if strings.Contains(err.Error(), "line 2") {
		t.Errorf("error named the trimmed-text line instead of the file line: %v", err)
	}
}

// TestLoadOptionalSecret_DoesNotTrim pins the mechanism behind the ncn5.16
// fix directly: unlike loadSecret, loadOptionalSecret must return the file
// (or env var) content byte-for-byte, leading/trailing whitespace and all,
// because its one caller (parseDMAllowlist) reports errors by line number
// against exactly what it's handed.
func TestLoadOptionalSecret_DoesNotTrim(t *testing.T) {
	t.Setenv("AGENT_TEAMS_TELEGRAM_DM_ALLOWLIST", "")

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "telegram"), 0o700); err != nil {
		t.Fatal(err)
	}
	const raw = "\n111\n"
	if err := os.WriteFile(filepath.Join(home, "telegram", "dm-allowlist"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := loadOptionalSecret(home, "AGENT_TEAMS_TELEGRAM_DM_ALLOWLIST", "telegram/dm-allowlist")
	if err != nil {
		t.Fatalf("loadOptionalSecret: %v", err)
	}
	if got != raw {
		t.Errorf("got %q, want %q unchanged (loadOptionalSecret must not trim)", got, raw)
	}
}

// TestNew_DMAllowlistFromFile_AdmitsOnlyListedSenders drives the real config
// path Eric will use — <home>/telegram/dm-allowlist — through New and asserts
// the resulting predicate, proving loadOptionalSecret and parseDMAllowlist are
// wired into construction.
func TestNew_DMAllowlistFromFile_AdmitsOnlyListedSenders(t *testing.T) {
	// Empty allow-list env so the file under home is the one exercised.
	pinTelegramEnv(t, fakeToken, "-100999888777", "")

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "telegram"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "telegram", "dm-allowlist"), []byte("12345678\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tg, err := New(home, &http.Client{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !tg.allowsDirectChat(12345678) {
		t.Error("allowsDirectChat(12345678): got false, want true (id is in the file)")
	}
	if tg.allowsDirectChat(99887766) {
		t.Error("allowsDirectChat(99887766): got true, want false (id is not in the file)")
	}
}

// TestNew_AbsentDMAllowlist_ConstructsAndAdmitsNobody proves the optional
// loader does NOT inherit loadSecret's fail-on-absence behavior: an operator
// who never configures a DM allow-list still gets a working transport, one
// that admits no DMs.
func TestNew_AbsentDMAllowlist_ConstructsAndAdmitsNobody(t *testing.T) {
	pinTelegramEnv(t, fakeToken, "-100999888777", "")

	tg, err := New(t.TempDir(), &http.Client{})
	if err != nil {
		t.Fatalf("New with no allow-list configured must succeed, got: %v", err)
	}
	if tg.allowsDirectChat(12345678) {
		t.Error("allowsDirectChat: got true with no allow-list configured, want false")
	}
}

// ── Send: ChatRef — per-message chat id + outbound guard (ncn5.10) ───────────

// sendCapture records what an outbound Send actually put on the wire.
// httptest only, like every test in this file: one bot token permits exactly
// ONE getUpdates consumer, and Eric's production relay holds it.
type sendCapture struct {
	sendCalls      int
	topicCalls     int
	chatID         string
	sawThreadIDKey bool
	text           string
}

// newSendCaptureServer answers sendMessage and createForumTopic into capture.
func newSendCaptureServer(t *testing.T, capture *sendCapture) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			capture.sendCalls++
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			capture.chatID = r.FormValue("chat_id")
			_, capture.sawThreadIDKey = r.PostForm["message_thread_id"]
			capture.text = r.FormValue("text")
			jsonResponse(w, 200, map[string]any{"ok": true, "result": map[string]any{}})
		case strings.HasSuffix(r.URL.Path, "/createForumTopic"):
			capture.topicCalls++
			jsonResponse(w, 200, map[string]any{"ok": true, "result": map[string]any{"message_thread_id": 1}})
		default:
			http.NotFound(w, r)
		}
	}))
}

// TestSend_EmptyChatRef_PostsConfiguredChat is the regression guard on every
// existing caller: with no ChatRef, a General send goes exactly where it went
// before — the configured chat, no message_thread_id, no topic opened.
func TestSend_EmptyChatRef_PostsConfiguredChat(t *testing.T) {
	const chatID = "-100111222333"

	var capture sendCapture
	srv := newSendCaptureServer(t, &capture)
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	threadRef, err := tg.Send(transport.OutboundMessage{
		InitiativeID: "at-00o",
		Title:        "Steward",
		Body:         "hello from general",
		General:      true,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if threadRef != "" {
		t.Errorf("threadRef: got %q, want empty", threadRef)
	}
	if capture.chatID != chatID {
		t.Errorf("chat_id: got %q, want the configured chat %q", capture.chatID, chatID)
	}
	if capture.sawThreadIDKey {
		t.Error("message_thread_id must be omitted entirely on a General send")
	}
	if capture.topicCalls != 0 {
		t.Errorf("createForumTopic calls: got %d, want 0", capture.topicCalls)
	}
}

// TestSend_AllowlistedChatRef_PostsPrivateChatAndOmitsThreadID is the new
// capability: a reply addressed at an allow-listed DM lands in that private
// chat, with message_thread_id omitted entirely (a private chat has no forum
// topic) and no topic creation attempted.
func TestSend_AllowlistedChatRef_PostsPrivateChatAndOmitsThreadID(t *testing.T) {
	const chatID = "-100111222333"
	const senderID = int64(12345678)

	var capture sendCapture
	srv := newSendCaptureServer(t, &capture)
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	tg.dmAllowlist = map[int64]bool{senderID: true}

	threadRef, err := tg.Send(transport.OutboundMessage{
		InitiativeID: "at-00o",
		Title:        "Steward",
		Body:         "answering your DM",
		ChatRef:      "12345678:10",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if threadRef != "" {
		t.Errorf("threadRef: got %q, want empty (a DM has no forum topic)", threadRef)
	}
	if capture.chatID != "12345678" {
		t.Errorf("chat_id: got %q, want the private chat %q (NOT the configured %q)", capture.chatID, "12345678", chatID)
	}
	if capture.sawThreadIDKey {
		t.Error("message_thread_id must be omitted entirely for a private chat")
	}
	if capture.topicCalls != 0 {
		t.Errorf("createForumTopic calls: got %d, want 0 — a topic in a private chat is not a thing", capture.topicCalls)
	}
	if capture.text != "answering your DM" {
		t.Errorf("text: got %q", capture.text)
	}
}

// TestSend_ChatRefBeatsGeneral pins the precedence transport.OutboundMessage
// freezes, which is deliberately the OPPOSITE of General-beats-ThreadRef:
// with both set, the specific conversation wins. Downgrading a reply meant
// for one person into the shared channel is a data-leak-shaped failure, so
// this is a correctness rule, not a preference.
func TestSend_ChatRefBeatsGeneral(t *testing.T) {
	const chatID = "-100111222333"
	const senderID = int64(12345678)

	var capture sendCapture
	srv := newSendCaptureServer(t, &capture)
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID)
	tg.dmAllowlist = map[int64]bool{senderID: true}

	if _, err := tg.Send(transport.OutboundMessage{
		InitiativeID: "at-00o",
		Title:        "Steward",
		Body:         "meant for one person",
		General:      true,
		ChatRef:      "12345678:10",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if capture.chatID != "12345678" {
		t.Errorf("chat_id: got %q, want the ChatRef's private chat %q — ChatRef must beat General", capture.chatID, "12345678")
	}
}

// TestSend_RejectedChatRef_NoHTTPCall is the outbound guard. ChatRef arrives
// having round-tripped through the steward (an LLM) and notify passes it
// through unvalidated by design, so this is the only layer that can refuse a
// destination — and refusing must mean sending nothing, never falling back to
// the configured chat.
func TestSend_RejectedChatRef_NoHTTPCall(t *testing.T) {
	const chatID = "-100111222333"
	const senderID = int64(12345678)

	cases := []struct {
		name    string
		chatRef string
	}{
		{name: "a chat that is not allow-listed", chatRef: "99887766:10"},
		{name: "a plausible-looking foreign supergroup", chatRef: "-100999888777:10"},
		{name: "no chat/message split at all", chatRef: "12345678"},
		{name: "empty chat half", chatRef: ":10"},
		{name: "colon only", chatRef: ":"},
		{name: "non-numeric chat id", chatRef: "not-a-chat:10"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var capture sendCapture
			srv := newSendCaptureServer(t, &capture)
			defer srv.Close()

			tg := newTestTelegram(t, srv, chatID)
			tg.dmAllowlist = map[int64]bool{senderID: true}

			threadRef, err := tg.Send(transport.OutboundMessage{
				InitiativeID: "at-00o",
				Title:        "Steward",
				Body:         "must not be delivered anywhere",
				ChatRef:      tc.chatRef,
			})
			if err == nil {
				t.Errorf("Send(ChatRef=%q): expected an error", tc.chatRef)
			}
			if threadRef != "" {
				t.Errorf("Send(ChatRef=%q): threadRef = %q, want empty", tc.chatRef, threadRef)
			}
			if capture.sendCalls != 0 {
				t.Errorf("Send(ChatRef=%q): sendMessage was called %d time(s) — a rejected destination must send NOTHING, not fall back to the configured chat", tc.chatRef, capture.sendCalls)
			}
			if capture.topicCalls != 0 {
				t.Errorf("Send(ChatRef=%q): createForumTopic was called %d time(s), want 0", tc.chatRef, capture.topicCalls)
			}
		})
	}
}

// TestSend_ConfiguredChatRef_Allowed covers the other admitted destination:
// a ref naming the configured supergroup (the composite ref of a General-
// channel message) posts back into it with no topic — the reply-in-place case
// for group traffic.
func TestSend_ConfiguredChatRef_Allowed(t *testing.T) {
	const chatID = "-100111222333"

	var capture sendCapture
	srv := newSendCaptureServer(t, &capture)
	defer srv.Close()

	tg := newTestTelegram(t, srv, chatID) // dmAllowlist deliberately nil
	if _, err := tg.Send(transport.OutboundMessage{
		InitiativeID: "at-00o",
		Title:        "Steward",
		Body:         "replying in General",
		ChatRef:      chatID + ":77",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if capture.chatID != chatID {
		t.Errorf("chat_id: got %q, want %q", capture.chatID, chatID)
	}
	if capture.sawThreadIDKey {
		t.Error("message_thread_id must be omitted entirely for a ChatRef send")
	}
	if capture.topicCalls != 0 {
		t.Errorf("createForumTopic calls: got %d, want 0", capture.topicCalls)
	}
}

// TestReceive_StartupConfigLine_ContainsChatIDNotToken mirrors the
// assertErrorHasNoToken security rationale, now against the config line's
// actual home: the top of Receive (the relay-poller-only path). Asserted
// via an injected logOut buffer rather than a real stderr capture — simpler
// now that the line no longer fires inside New.
func TestReceive_StartupConfigLine_ContainsChatIDNotToken(t *testing.T) {
	const chatID = "-100999888777"
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			http.NotFound(w, r)
			return
		}
		jsonResponse(w, 200, map[string]any{
			"ok": true,
			"result": []map[string]any{
				{
					"update_id": 1,
					"message": map[string]any{
						"message_id":        1,
						"message_thread_id": 5,
						"is_topic_message":  true,
						"text":              "hi",
						"chat":              map[string]any{"id": chatIDInt},
					},
				},
			},
		})
	}))
	defer srv.Close()

	tg := &Telegram{
		token:      fakeToken,
		chatID:     chatID,
		httpClient: &http.Client{},
		baseURL:    srv.URL,
	}
	var logBuf bytes.Buffer
	tg.logOut = &logBuf

	sentinel := fmt.Errorf("stop")
	_ = tg.Receive(func(transport.Reply) error { return sentinel })

	got := logBuf.String()
	if !strings.Contains(got, chatID) {
		t.Errorf("expected startup config line to contain chat id %q, got:\n%s", chatID, got)
	}
	if strings.Contains(got, fakeToken) {
		t.Fatalf("startup config line leaked the bot token: %s", got)
	}
}
