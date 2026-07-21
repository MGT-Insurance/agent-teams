package telegram

import (
	"encoding/json"
	"fmt"
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
	err := tg.sendMessage("7", "hello")
	assertErrorHasNoToken(t, err)
}

func TestGetUpdates_ConnectionFailure_NoTokenInError(t *testing.T) {
	tg := newConnFailureTelegram(t)
	_, err := tg.getUpdates(0)
	assertErrorHasNoToken(t, err)
}
