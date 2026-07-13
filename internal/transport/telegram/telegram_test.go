package telegram

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/transport"
)

// ── test helpers ─────────────────────────────────────────────────────────────

// newTestTelegram builds a Telegram pointed at srv with a fake token and chat.
func newTestTelegram(t *testing.T, srv *httptest.Server, chatID string) *Telegram {
	t.Helper()
	tg := &Telegram{
		token:      "test-token",
		chatID:     chatID,
		httpClient: &http.Client{},
		baseURL:    srv.URL,
	}
	return tg
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
	wantName := "[at-00o] Blocked on review"
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
	wantText := "[at-00o] Status update\n\nAll good."
	if gotSendText != wantText {
		t.Errorf("sendMessage text:\ngot  %q\nwant %q", gotSendText, wantText)
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
	// Second: non-topic — ThreadRef empty so relay can bounce.
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
