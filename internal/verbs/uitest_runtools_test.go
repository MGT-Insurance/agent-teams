package verbs

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// c0Server plays the C0 run-tools route. handler gets the decoded request body.
type c0Server struct {
	*httptest.Server
	requests []map[string]any
	headers  []http.Header
}

func newC0Server(t *testing.T, handler func(w http.ResponseWriter, body map[string]any)) *c0Server {
	t.Helper()
	s := &c0Server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/run-tools" {
			t.Errorf("request %s %s, want POST /api/run-tools", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("body is not JSON: %s", raw)
		}
		s.requests = append(s.requests, body)
		s.headers = append(s.headers, r.Header.Clone())
		handler(w, body)
	}))
	t.Cleanup(s.Close)
	return s
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// inRun puts the process in a Dispatcher run served by srv.
func inRun(t *testing.T, srv *c0Server) {
	t.Helper()
	t.Setenv("DSP_BEAD", "dsp-run1")
	t.Setenv("DSP_API_URL", srv.URL+"/")
}

func TestRunTools_FileSendsC0BodyWithoutAuthAndPrintsID(t *testing.T) {
	srv := newC0Server(t, func(w http.ResponseWriter, _ map[string]any) {
		writeJSON(w, 200, map[string]any{"id": "dsp-ui9", "url": "https://dsp.example/beads/dsp-ui9", "created": true})
	})
	h := newUIHarness(t, "")
	inRun(t, srv)
	h.deps.httpDo = nil
	cmd := &uiTestFileKong{Worktree: "/wt", Scope: "mithril", ItemsFile: h.itemsFile("- check home\n"), TestUser: "qa@example.com", deps: h.deps}
	if err := cmd.Run(h.ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := strings.TrimSpace(h.stdout.String()); got != "dsp-ui9" {
		t.Errorf("stdout = %q, want the bare id", got)
	}
	if !strings.Contains(h.stderr.String(), "https://dsp.example/beads/dsp-ui9") {
		t.Errorf("stderr lacks the url: %q", h.stderr.String())
	}
	if len(h.filed) != 0 || len(h.bdReads) != 0 {
		t.Errorf("bd path used: filed=%v reads=%v", h.filed, h.bdReads)
	}
	want := map[string]any{"op": "ui_test_file", "branch": "feat-x", "sha": uiTestSHA, "scope": "mithril", "test_user": "qa@example.com", "items": "- check home\n"}
	got := srv.requests[0]
	if len(got) != len(want) {
		t.Errorf("body = %v, want exactly %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("body[%s] = %v, want %v", k, got[k], v)
		}
	}
	if a := srv.headers[0].Get("Authorization"); a != "" {
		t.Errorf("ateam sent Authorization %q; the firewall adds it", a)
	}
}

func TestRunTools_FileStillChecksPushedBeforeCalling(t *testing.T) {
	srv := newC0Server(t, func(w http.ResponseWriter, _ map[string]any) { t.Error("server called") })
	h := newUIHarness(t, "")
	inRun(t, srv)
	h.ancestor = 1
	if err := h.file("x"); exitOf(err) != uiExitNotPushed {
		t.Errorf("exit = %d, want %d", exitOf(err), uiExitNotPushed)
	}
}

func TestRunTools_FileHTTPErrors(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{401, "401 not authorized"},
		{409, "409 ui-test cap 20 reached for this run"},
		{400, "HTTP 400: bad body"},
	}
	for _, c := range cases {
		msg := map[int]string{401: "nope", 409: "ui-test cap 20 reached for this run", 400: "bad body"}[c.code]
		srv := newC0Server(t, func(w http.ResponseWriter, _ map[string]any) { writeJSON(w, c.code, map[string]any{"error": msg}) })
		h := newUIHarness(t, "")
		inRun(t, srv)
		err := h.file("x")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("code %d: err = %v, want containing %q", c.code, err, c.want)
		}
		if exitOf(err) != 1 {
			t.Errorf("code %d: exit = %d, want 1", c.code, exitOf(err))
		}
		if len(srv.requests) != 1 {
			t.Errorf("code %d: %d requests, want 1 (no retry on 4xx)", c.code, len(srv.requests))
		}
	}
}

func TestRunTools_FileRetriesOnce5xx(t *testing.T) {
	n := 0
	srv := newC0Server(t, func(w http.ResponseWriter, _ map[string]any) {
		if n++; n == 1 {
			writeJSON(w, 500, map[string]any{"error": "boom"})
			return
		}
		writeJSON(w, 200, map[string]any{"id": "dsp-ui9", "url": "u", "created": false})
	})
	h := newUIHarness(t, "")
	inRun(t, srv)
	if err := h.file("x"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 2 {
		t.Errorf("requests = %d, want 2", n)
	}
	if !strings.Contains(h.stderr.String(), "reused open bead") {
		t.Errorf("stderr lacks the dedupe note: %q", h.stderr.String())
	}
}

const rtVerdictDesc = "branch: feat-x\nsha: " + uiTestSHA + "\n---\nitems\n"

func rtBead(status, reason string, comments ...uiTestComment) map[string]any {
	return map[string]any{"id": "dsp-ui9", "title": "ui-test: x", "status": status, "close_reason": reason, "description": rtVerdictDesc, "labels": []string{"run-filed:r1"}, "comments": comments}
}

func (h *uiHarness) rtWait(timeout time.Duration) (string, error) {
	out := filepath.Join(h.t.TempDir(), "out")
	cmd := &uiTestWaitKong{ID: "dsp-ui9", Out: out, Timeout: timeout, deps: h.deps}
	return out, cmd.Run(h.ctx)
}

func TestRunTools_WaitVerdictWritesVerdictAndFetchesPNGByOp(t *testing.T) {
	srv := newC0Server(t, func(w http.ResponseWriter, body map[string]any) {
		switch body["op"] {
		case "bead_get":
			writeJSON(w, 200, rtBead("closed", "passed", withFinalizer(uiSignedLink("01-home.png", uiFarExp))...))
		case "artifact":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("PNGDATA"))
		}
	})
	h := newUIHarness(t, "")
	inRun(t, srv)
	out, err := h.rtWait(time.Minute)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.bdReads) != 0 {
		t.Errorf("bd path used: %v", h.bdReads)
	}
	png, err := os.ReadFile(filepath.Join(out, "screenshots", "01-home.png"))
	if err != nil || string(png) != "PNGDATA" {
		t.Errorf("png = %q, %v", png, err)
	}
	verdict, _ := os.ReadFile(filepath.Join(out, "verdict.md"))
	for _, want := range []string{"bead: dsp-ui9", "run: " + uiRunID, "branch: feat-x", "sha: " + uiTestSHA, "01-home.png"} {
		if !strings.Contains(string(verdict), want) {
			t.Errorf("verdict.md lacks %q:\n%s", want, verdict)
		}
	}
	art := srv.requests[len(srv.requests)-1]
	if art["op"] != "artifact" || art["id"] != "dsp-ui9" || art["runId"] != uiRunID || art["name"] != "01-home.png" {
		t.Errorf("artifact request = %v", art)
	}
	for _, hd := range srv.headers {
		if hd.Get("Authorization") != "" {
			t.Errorf("ateam sent an Authorization header")
		}
	}
}

func TestRunTools_WaitMissingScreenshotExit7(t *testing.T) {
	srv := newC0Server(t, func(w http.ResponseWriter, body map[string]any) {
		if body["op"] == "artifact" {
			writeJSON(w, 404, map[string]any{"error": "not found"})
			return
		}
		writeJSON(w, 200, rtBead("closed", "passed", withFinalizer(uiSignedLink("01-home.png", uiFarExp))...))
	})
	h := newUIHarness(t, "")
	inRun(t, srv)
	out, err := h.rtWait(time.Minute)
	if exitOf(err) != uiExitNoImages {
		t.Fatalf("exit = %d, want %d (err %v)", exitOf(err), uiExitNoImages, err)
	}
	verdict, _ := os.ReadFile(filepath.Join(out, "verdict.md"))
	if !strings.Contains(string(verdict), "01-home.png http-404") {
		t.Errorf("verdict.md missing list:\n%s", verdict)
	}
}

func TestRunTools_WaitExitCodes(t *testing.T) {
	cases := []struct {
		name string
		bead map[string]any
		want int
	}{
		{"closed failed", rtBead("closed", "failed", eriKD2WComments()...), uiExitFailed},
		{"blocked question", rtBead("blocked", "", uiTestComment{Author: "agent:tester", CreatedAt: "2026-11-02T14:00:00Z", Text: "question: which user?"}), uiExitQuestion},
		{"still open", rtBead("open", ""), uiExitPending},
	}
	for _, c := range cases {
		srv := newC0Server(t, func(w http.ResponseWriter, _ map[string]any) { writeJSON(w, 200, c.bead) })
		h := newUIHarness(t, "")
		inRun(t, srv)
		_, err := h.rtWait(time.Minute)
		if exitOf(err) != c.want {
			t.Errorf("%s: exit = %d, want %d (err %v)", c.name, exitOf(err), c.want, err)
		}
	}
}

func TestRunTools_WaitErrorsAreClear(t *testing.T) {
	for _, code := range []int{401, 404} {
		srv := newC0Server(t, func(w http.ResponseWriter, _ map[string]any) { writeJSON(w, code, map[string]any{"error": "x"}) })
		h := newUIHarness(t, "")
		inRun(t, srv)
		_, err := h.rtWait(time.Minute)
		if err == nil || !strings.Contains(err.Error(), "ui-test wait: run-tools bead_get") || exitOf(err) != 1 {
			t.Errorf("code %d: err = %v exit %d", code, err, exitOf(err))
		}
	}
}

func TestRunTools_OffWithoutBothEnvVars(t *testing.T) {
	srv := newC0Server(t, func(w http.ResponseWriter, _ map[string]any) { t.Error("server called") })
	// DSP_BEAD without DSP_API_URL, and DSP_API_URL without DSP_BEAD: both take
	// the old path, which is unconfigured here.
	for _, env := range [][2]string{{"dsp-run1", ""}, {"", srv.URL}} {
		h := newUIHarness(t, "")
		t.Setenv("DSP_BEAD", env[0])
		t.Setenv("DSP_API_URL", env[1])
		if err := h.file("x"); exitOf(err) != uiExitIneligible {
			t.Errorf("env %v: file exit = %d, want 5", env, exitOf(err))
		}
		if _, err := h.rtWait(time.Minute); exitOf(err) != uiExitIneligible {
			t.Errorf("env %v: wait exit = %d, want 5", env, exitOf(err))
		}
	}
}

func TestRunTools_ConfiguredLaptopPathUnchangedWhenEnvUnset(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	if err := h.file("x"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.filed) != 1 || strings.TrimSpace(h.stdout.String()) != "dsp-ui1" {
		t.Errorf("filed=%v stdout=%q", h.filed, h.stdout.String())
	}
}
