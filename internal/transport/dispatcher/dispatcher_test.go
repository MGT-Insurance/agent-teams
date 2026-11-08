package dispatcher_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/transport"
	"github.com/mgt-insurance/agent-teams/internal/transport/dispatcher"
)

var registerOnce sync.Once

func register() {
	registerOnce.Do(func() { transport.RegisterTransport("dispatcher", dispatcher.Factory) })
}

type recorded struct {
	body   map[string]any
	header http.Header
}

// serve starts an httptest server that records each request and answers with
// the next status/body from replies (the last repeats), and points the
// Dispatcher run env at it.
func serve(t *testing.T, replies ...[2]any) *[]recorded {
	t.Helper()
	var got []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/run-tools" {
			t.Errorf("request %s %s, want POST /api/run-tools", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("body is not JSON: %s", raw)
		}
		n := len(got)
		got = append(got, recorded{body: body, header: r.Header.Clone()})
		if n >= len(replies) {
			n = len(replies) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(replies[n][0].(int))
		_, _ = io.WriteString(w, replies[n][1].(string))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("DSP_BEAD", "dsp-run1")
	t.Setenv("DSP_API_URL", srv.URL+"/")
	return &got
}

func newT(t *testing.T) transport.Transport {
	t.Helper()
	tr, err := dispatcher.Factory(t.TempDir())
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	return tr
}

type closer interface{ CloseTopic(string) error }

func TestFactory_RequiresRunEnv(t *testing.T) {
	const want = "dispatcher transport: DSP_BEAD and DSP_API_URL must be set (only inside a Dispatcher run)"
	for _, c := range []struct{ bead, url string }{{"", ""}, {"dsp-run1", ""}, {"", "http://x"}} {
		t.Setenv("DSP_BEAD", c.bead)
		t.Setenv("DSP_API_URL", c.url)
		if _, err := dispatcher.Factory(t.TempDir()); err == nil || err.Error() != want {
			t.Errorf("bead=%q url=%q: err = %v, want %q", c.bead, c.url, err, want)
		}
	}
}

func TestName(t *testing.T) {
	serve(t, [2]any{200, `{}`})
	if got := newT(t).Name(); got != "dispatcher" {
		t.Errorf("Name = %q", got)
	}
}

func TestSend_PostsExactBodyAndReturnsThreadRef(t *testing.T) {
	got := serve(t, [2]any{200, `{"initiative":"at-1","thread_ref":"77","parts":1}`})
	ref, err := newT(t).Send(transport.OutboundMessage{InitiativeID: "at-1", ThreadRef: "999", Title: "T", Body: "hello"})
	if err != nil || ref != "77" {
		t.Fatalf("Send = %q, %v; want 77, nil", ref, err)
	}
	if len(*got) != 1 {
		t.Fatalf("%d requests, want 1", len(*got))
	}
	want := map[string]any{"op": "telegram_post", "initiative": "at-1", "title": "T", "text": "hello"}
	r := (*got)[0]
	if len(r.body) != len(want) {
		t.Errorf("body = %v, want exactly %v (no thread_ref)", r.body, want)
	}
	for k, v := range want {
		if r.body[k] != v {
			t.Errorf("body[%s] = %v, want %v", k, r.body[k], v)
		}
	}
	if a := r.header.Get("Authorization"); a != "" {
		t.Errorf("Authorization = %q; the firewall adds it", a)
	}
}

func TestSend_OmitsEmptyTitleAndCutsLongOne(t *testing.T) {
	got := serve(t, [2]any{200, `{"thread_ref":"1"}`})
	tr := newT(t)
	if _, err := tr.Send(transport.OutboundMessage{InitiativeID: "at-1", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, has := (*got)[0].body["title"]; has {
		t.Errorf("empty title was sent: %v", (*got)[0].body)
	}
	if _, err := tr.Send(transport.OutboundMessage{InitiativeID: "at-1", Title: strings.Repeat("é", 600), Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune((*got)[1].body["title"].(string))); n != 500 {
		t.Errorf("title runes = %d, want 500", n)
	}
}

func TestSend_RejectsUnsupportedWithoutRequest(t *testing.T) {
	got := serve(t, [2]any{200, `{}`})
	tr := newT(t)
	cases := map[string]struct {
		msg  transport.OutboundMessage
		want string
	}{
		"general":  {transport.OutboundMessage{InitiativeID: "a", Body: "x", General: true}, "posts only to its own initiative's topic"},
		"chatref":  {transport.OutboundMessage{InitiativeID: "a", Body: "x", ChatRef: "c"}, "posts only to its own initiative's topic"},
		"image":    {transport.OutboundMessage{InitiativeID: "a", Body: "x", ImagePath: "/p.png"}, "attachments are not supported"},
		"document": {transport.OutboundMessage{InitiativeID: "a", Body: "x", DocumentPath: "/p.json"}, "attachments are not supported"},
	}
	for name, c := range cases {
		if _, err := tr.Send(c.msg); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want containing %q", name, err, c.want)
		}
	}
	if len(*got) != 0 {
		t.Errorf("%d requests, want none", len(*got))
	}
}

func TestSend_DoesNotRetry502(t *testing.T) {
	got := serve(t, [2]any{502, `{"error":"telegram down"}`})
	_, err := newT(t).Send(transport.OutboundMessage{InitiativeID: "a", Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "run-tools telegram_post: HTTP 502: telegram down") {
		t.Errorf("err = %v", err)
	}
	if len(*got) != 1 {
		t.Errorf("%d requests, want 1", len(*got))
	}
}

func TestCloseTopic_PostsBodyAndRetriesOnce502(t *testing.T) {
	got := serve(t, [2]any{502, `{"error":"boom"}`}, [2]any{200, `{"already_closed":false}`})
	if err := newT(t).(closer).CloseTopic("55"); err != nil {
		t.Fatalf("CloseTopic: %v", err)
	}
	if len(*got) != 2 {
		t.Fatalf("%d requests, want 2", len(*got))
	}
	for i, r := range *got {
		if len(r.body) != 2 || r.body["op"] != "telegram_close" || r.body["thread_ref"] != "55" {
			t.Errorf("request %d body = %v", i, r.body)
		}
	}
}

func TestCloseTopic_GivesUpAfterOneRetry(t *testing.T) {
	got := serve(t, [2]any{500, `{"error":"boom"}`})
	if err := newT(t).(closer).CloseTopic("55"); err == nil {
		t.Error("want error")
	}
	if len(*got) != 2 {
		t.Errorf("%d requests, want 2", len(*got))
	}
}

func TestAuthFailuresSurfaceC6Text(t *testing.T) {
	for _, code := range []int{401, 403} {
		serve(t, [2]any{code, `{"error":"nope"}`})
		tr := newT(t)
		_, sendErr := tr.Send(transport.OutboundMessage{InitiativeID: "a", Body: "x"})
		closeErr := tr.(closer).CloseTopic("1")
		want := "HTTP 403: nope"
		if code == 401 {
			want = "401 not authorized; this run's job has no telegram tool, or the run is finished"
		}
		for _, err := range []error{sendErr, closeErr} {
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("code %d: err = %v, want containing %q", code, err, want)
			}
		}
	}
}

func TestReceive_ErrorsAtOnce(t *testing.T) {
	serve(t, [2]any{200, `{}`})
	err := newT(t).Receive(func(transport.Reply) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "does not receive") {
		t.Errorf("err = %v", err)
	}
}

func TestForFindsCloseTopicThroughDecorator(t *testing.T) {
	register()
	serve(t, [2]any{200, `{}`})
	t.Setenv("AGENT_TEAMS_TRANSPORT", "dispatcher")
	tr, err := transport.For(t.TempDir())
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if _, ok := transport.Capability[closer](tr); !ok {
		t.Error("Capability did not find CloseTopic through the For decorator")
	}
}

func TestForOutsideRunFailsWithC1Message(t *testing.T) {
	register()
	t.Setenv("DSP_BEAD", "")
	t.Setenv("DSP_API_URL", "")
	t.Setenv("AGENT_TEAMS_TRANSPORT", "dispatcher")
	if _, err := transport.For(t.TempDir()); err == nil || !strings.Contains(err.Error(), "DSP_BEAD and DSP_API_URL must be set") {
		t.Errorf("err = %v", err)
	}
}
