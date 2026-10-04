package verbs

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/transport"
	"github.com/mgt-insurance/agent-teams/internal/transport/telegram"
)

func TestRelay_ClosedInitiative_LocalOwnerNonFallback(t *testing.T) {
	const threadRef = "reopened-41"
	const body = "the topic was reopened"
	closed := bd.Issue{ID: "at-closed-owner", Status: "closed"}
	open := newFakeBDQuery()
	closedQuery := newFakeBDQuery()
	closedQuery.results["thread:"+threadRef] = []bd.Issue{closed}
	sent := &fakeSendCapture{}
	acked := &fakeAck{}
	ctx := newRelayCtx(t)
	var deliveryOrder []string

	cmd := &relayKong{
		bdQuery:       open.query,
		bdQueryClosed: closedQuery.query,
		send: func(ctx *cli.Context, file string) error {
			deliveryOrder = append(deliveryOrder, "send")
			return sent.send(ctx, file)
		},
		ack: func(ref string) error {
			deliveryOrder = append(deliveryOrder, "ack")
			return acked.ack(ref)
		},
		claimsLocally:       func(got bd.Issue) bool { return got.ID == closed.ID },
		isFallbackResponder: func(*cli.Context) bool { return false },
		knownStewardTopic:   neverKnownStewardTopic,
	}
	if err := cmd.handleReply(ctx, transport.Reply{ThreadRef: threadRef, MessageRef: "-10041:73", Text: body}); err != nil {
		t.Fatalf("handleReply: %v", err)
	}

	if len(sent.bodies) != 1 {
		t.Fatalf("closed local-owner sends = %d, want 1", len(sent.bodies))
	}
	envelope, ok := ParseStewardClosedInitiativeEnvelope(sent.bodies[0])
	if !ok {
		t.Fatalf("sent body is not a closed-initiative envelope: %q", sent.bodies[0])
	}
	if envelope.InitiativeID != closed.ID || envelope.Body != body {
		t.Errorf("closed envelope = %+v, want initiative %q and body %q", envelope, closed.ID, body)
	}
	if got := acked.refs; len(got) != 1 || got[0] != "-10041:73" {
		t.Errorf("acks = %v, want [-10041:73] after successful send", got)
	}
	if got := strings.Join(deliveryOrder, ","); got != "send,ack" {
		t.Errorf("delivery order = %q, want send,ack", got)
	}
	stderr := relayStderr(ctx)
	if !strings.Contains(stderr, "routed message to steward for closed initiative "+closed.ID) {
		t.Errorf("missing closed-route success diagnostic: %q", stderr)
	}
	if strings.Contains(stderr, "no open initiative found") || strings.Contains(stderr, "unrouted") {
		t.Errorf("unexpected generic untied diagnostic: %q", stderr)
	}
}

func TestRelay_ClosedInitiative_FallbackNonOwner(t *testing.T) {
	const threadRef = "reopened-42"
	closed := bd.Issue{ID: "at-closed-not-owner", Status: "closed"}
	open := newFakeBDQuery()
	closedQuery := newFakeBDQuery()
	closedQuery.results["thread:"+threadRef] = []bd.Issue{closed}
	sent := &fakeSendCapture{}
	acked := &fakeAck{}
	ctx := newRelayCtx(t)

	cmd := &relayKong{
		bdQuery:             open.query,
		bdQueryClosed:       closedQuery.query,
		doltPull:            func(string) error { return nil },
		sleeper:             func(time.Duration) {},
		send:                sent.send,
		ack:                 acked.ack,
		claimsLocally:       func(bd.Issue) bool { return false },
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
	}
	if err := cmd.handleReply(ctx, transport.Reply{ThreadRef: threadRef, MessageRef: "-10042:74", Text: "do not duplicate this"}); err != nil {
		t.Fatalf("handleReply: %v", err)
	}

	if len(sent.calls) != 0 {
		t.Errorf("fallback non-owner sends = %d, want 0", len(sent.calls))
	}
	if len(acked.refs) != 0 {
		t.Errorf("fallback non-owner acks = %v, want none", acked.refs)
	}
	stderr := relayStderr(ctx)
	if !strings.Contains(stderr, "not claimed locally") || !strings.Contains(stderr, "closed") {
		t.Errorf("missing closed-reply not-claimed diagnostic: %q", stderr)
	}
}

// relayReopenedTopicHTTPDoer redirects Telegram's production base URL to a
// local httptest.Server while leaving Telegram.New and its JSON decoder intact.
type relayReopenedTopicHTTPDoer struct {
	client *http.Client
	base   string
}

func (d relayReopenedTopicHTTPDoer) rewrite(raw string) string {
	return d.base + strings.TrimPrefix(raw, "https://api.telegram.org")
}

func (d relayReopenedTopicHTTPDoer) Get(raw string) (*http.Response, error) {
	return d.client.Get(d.rewrite(raw))
}

func (d relayReopenedTopicHTTPDoer) PostForm(raw string, values url.Values) (*http.Response, error) {
	return d.client.PostForm(d.rewrite(raw), values)
}

func (d relayReopenedTopicHTTPDoer) PostMultipart(string, map[string]string, string, string, []byte) (*http.Response, error) {
	return nil, errors.New("unexpected multipart Telegram request")
}

func TestRelay_ReopenedTopic_FakeBotAPI(t *testing.T) {
	const (
		chatID    = "-1001234500"
		threadRef = "912"
		messageID = "71"
		body      = "reply after reopening"
	)
	chatIDInt := int64(-1001234500)
	closed := newReopenedTopicLocalIssue(t, threadRef)
	var gotGetMe, gotUpdates, gotReaction int
	var reaction url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			gotGetMe++
			writeReopenedTopicJSON(w, map[string]any{"ok": true, "result": map[string]any{"username": "relaybot"}})
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			gotUpdates++
			writeReopenedTopicJSON(w, map[string]any{"ok": true, "result": []any{map[string]any{
				"update_id": 501,
				"message": map[string]any{
					"message_id":        71,
					"is_topic_message":  true,
					"message_thread_id": 912,
					"text":              body,
					"chat":              map[string]any{"id": chatIDInt, "type": "supergroup"},
				},
			}}})
		case strings.HasSuffix(r.URL.Path, "/setMessageReaction"):
			gotReaction++
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			reaction = r.Form
			writeReopenedTopicJSON(w, map[string]any{"ok": true, "result": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "telegram"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "telegram", "token"), []byte("test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "telegram", "chat-id"), []byte(chatID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_TEAMS_TELEGRAM_TOKEN", "")
	t.Setenv("AGENT_TEAMS_TELEGRAM_CHAT_ID", "")
	t.Setenv("AGENT_TEAMS_TELEGRAM_DM_ALLOWLIST", "")
	tg, err := telegram.New(home, relayReopenedTopicHTTPDoer{client: srv.Client(), base: srv.URL})
	if err != nil {
		t.Fatalf("telegram.New: %v", err)
	}

	sent := &fakeSendCapture{}
	ctx := newRelayCtx(t)
	var deliveryOrder []string
	cmd := &relayKong{
		bdQuery: newFakeBDQuery().query,
		bdQueryClosed: func(_, label string) ([]bd.Issue, error) {
			if label != "thread:"+threadRef {
				t.Fatalf("closed label = %q, want thread:%s", label, threadRef)
			}
			return []bd.Issue{closed}, nil
		},
		send: func(ctx *cli.Context, file string) error {
			deliveryOrder = append(deliveryOrder, "send")
			return sent.send(ctx, file)
		},
		ack: func(ref string) error {
			deliveryOrder = append(deliveryOrder, "ack")
			return tg.Ack(transport.Reply{MessageRef: ref})
		},
		claimsLocally:       claimsInitiativeLocally,
		isFallbackResponder: func(*cli.Context) bool { return false },
		knownStewardTopic:   neverKnownStewardTopic,
	}
	sentinel := errors.New("stop fake Bot API poll")
	err = tg.Receive(func(reply transport.Reply) error {
		if err := cmd.handleReply(ctx, reply); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Receive error = %v, want sentinel", err)
	}

	if gotGetMe != 1 || gotUpdates != 1 {
		t.Errorf("Bot API calls getMe=%d getUpdates=%d, want one each", gotGetMe, gotUpdates)
	}
	if len(sent.bodies) != 1 {
		t.Fatalf("mail sends = %d, want one", len(sent.bodies))
	}
	envelope, ok := ParseStewardClosedInitiativeEnvelope(sent.bodies[0])
	if !ok || envelope.InitiativeID != closed.ID || envelope.Body != body {
		t.Errorf("closed envelope = %+v (ok=%v), want id %q body %q", envelope, ok, closed.ID, body)
	}
	if _, unrouted := ParseStewardUnroutedEnvelope(sent.bodies[0]); unrouted {
		t.Errorf("closed reply was sent as an unrouted envelope: %q", sent.bodies[0])
	}
	if gotReaction != 1 {
		t.Fatalf("setMessageReaction calls = %d, want 1", gotReaction)
	}
	if got := strings.Join(deliveryOrder, ","); got != "send,ack" {
		t.Errorf("delivery order = %q, want send,ack", got)
	}
	if reaction.Get("chat_id") != chatID || reaction.Get("message_id") != messageID {
		t.Errorf("reaction target = chat_id=%q message_id=%q, want %q:%q", reaction.Get("chat_id"), reaction.Get("message_id"), chatID, messageID)
	}
}

func writeReopenedTopicJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		panic(err)
	}
}

func newReopenedTopicLocalIssue(t *testing.T, threadRef string) bd.Issue {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	worktree := filepath.Join(filepath.Dir(repo), "reopened-worktree")
	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	runGit(t.TempDir(), "init", "--initial-branch=main", repo)
	runGit(repo, "config", "user.email", "test@example.invalid")
	runGit(repo, "config", "user.name", "relay test")
	runGit(repo, "commit", "--allow-empty", "-m", "initial")
	runGit(repo, "worktree", "add", "-b", "reopened-topic", worktree)
	if !claimsInitiativeLocally(bd.Issue{Description: fmt.Sprintf("repo: %s\nworktree: %s\nbranch: reopened-topic\n", repo, worktree)}) {
		t.Fatal("test git worktree is not recognized by claimsInitiativeLocally")
	}
	return bd.Issue{
		ID:          "at-reopened-topic",
		Status:      "closed",
		Description: fmt.Sprintf("repo: %s\nworktree: %s\nbranch: reopened-topic\n", repo, worktree),
		Labels:      []string{"thread:" + threadRef},
	}
}
