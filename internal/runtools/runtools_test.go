package runtools

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFromEnv(t *testing.T) {
	cases := []struct {
		bead, url, want string
		ok              bool
	}{
		{"dsp-1", "https://d.example/", "https://d.example/api/run-tools", true},
		{"dsp-1", " https://d.example ", "https://d.example/api/run-tools", true},
		{"", "https://d.example", "", false},
		{"dsp-1", "", "", false},
		{"dsp-1", "/", "", false},
	}
	for _, c := range cases {
		t.Setenv("DSP_BEAD", c.bead)
		t.Setenv("DSP_API_URL", c.url)
		cl, ok := FromEnv()
		if ok != c.ok || (ok && cl.Endpoint != c.want) {
			t.Errorf("bead=%q url=%q: got %v %v, want %q %v", c.bead, c.url, cl, ok, c.want, c.ok)
		}
	}
}

func resp(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body))}
}

func TestCall_RetriesHonored(t *testing.T) {
	c := &Client{Endpoint: "http://x/api/run-tools"}
	for _, retries := range []int{0, 1, 2} {
		n := 0
		_, err := c.Call(func(*http.Request) (*http.Response, error) { n++; return resp(503, `{"error":"down"}`), nil }, "telegram_post", map[string]string{"op": "x"}, retries)
		if n != retries+1 {
			t.Errorf("retries=%d: %d attempts, want %d", retries, n, retries+1)
		}
		if err == nil || !strings.Contains(err.Error(), "HTTP 503: down") {
			t.Errorf("retries=%d: err = %v", retries, err)
		}
	}
}

func TestCall_NetworkErrorRetriedThenSucceeds(t *testing.T) {
	c := &Client{Endpoint: "http://x/api/run-tools"}
	n := 0
	data, err := c.Call(func(*http.Request) (*http.Response, error) {
		if n++; n == 1 {
			return nil, errors.New("connection refused")
		}
		return resp(200, `{"ok":true}`), nil
	}, "ui_test_file", map[string]string{"op": "x"}, 1)
	if err != nil || string(data) != `{"ok":true}` {
		t.Errorf("got %q, %v", data, err)
	}
}

func TestCall_NoRetryOn4xx(t *testing.T) {
	c := &Client{Endpoint: "http://x/api/run-tools"}
	n := 0
	_, err := c.Call(func(*http.Request) (*http.Response, error) { n++; return resp(400, `{"error":"bad"}`), nil }, "ui_test_file", nil, 3)
	if n != 1 || err == nil {
		t.Errorf("attempts=%d err=%v, want 1 attempt and an error", n, err)
	}
}

func TestError_Mapping(t *testing.T) {
	cases := []struct {
		op   string
		code int
		body string
		want string
	}{
		{"ui_test_file", 401, ``, "run-tools ui_test_file: 401 not authorized; this run's job has no ui-test tool, or the run is finished"},
		{"bead_get", 401, ``, "this run's job has no ui-test tool"},
		{"telegram_post", 401, ``, "run-tools telegram_post: 401 not authorized; this run's job has no telegram tool, or the run is finished"},
		{"telegram_close", 401, ``, "this run's job has no telegram tool"},
		{"ui_test_file", 409, `{"error":"cap 20"}`, "run-tools ui_test_file: 409 cap 20"},
		{"ui_test_file", 409, ``, "409 ui-test cap reached for this run, or budget refused"},
		{"telegram_post", 409, `{"error":"No Telegram chat is paired"}`, "run-tools telegram_post: HTTP 409: No Telegram chat is paired"},
		{"telegram_close", 404, `{"error":"No Telegram topic"}`, "run-tools telegram_close: HTTP 404: No Telegram topic"},
		{"ui_test_file", 500, ``, "run-tools ui_test_file: HTTP 500: no error message"},
	}
	for _, c := range cases {
		if got := Error(c.op, c.code, []byte(c.body)); !strings.Contains(got.Error(), c.want) {
			t.Errorf("%s %d: %q, want containing %q", c.op, c.code, got, c.want)
		}
	}
}
