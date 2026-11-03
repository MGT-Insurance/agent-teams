package verbs

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// timeoutErr is a net.Error whose Timeout() is true.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "request timed out" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// waitVerdict runs wait against a closed bead whose thread is withFinalizer(links...)
// and returns the out dir, the exit code, and verdict.md.
func (h *uiHarness) waitVerdict(links ...string) (out string, exit int, verdict string) {
	h.t.Helper()
	h.polls = []uiTestBead{closedBead("done")}
	h.comments = withFinalizer(links...)
	out, err := h.wait(time.Minute)
	data, rerr := os.ReadFile(filepath.Join(out, "verdict.md"))
	if rerr != nil {
		h.t.Fatalf("verdict.md: %v (wait err %v)", rerr, err)
	}
	return out, exitOf(err), string(data)
}

func TestUITestShots_ExpiredLinkIsNotFetchedAndExits7(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.fakePNG()
	_, exit, verdict := h.waitVerdict(uiSignedLink("01-ok.png", uiFarExp), uiSignedLink("02-old.png", uiPastExp))
	if exit != 7 {
		t.Fatalf("exit = %d, want 7", exit)
	}
	if !strings.Contains(verdict, "missing:\n02-old.png expired\n") {
		t.Errorf("verdict.md:\n%s", verdict)
	}
	if len(h.httpGets) != 1 || !strings.Contains(h.httpGets[0].URL.Path, "01-ok.png") {
		t.Errorf("GETs = %v, want only the unexpired link", h.httpGets)
	}
}

func TestUITestShots_HTTPFailuresExit7WithCode(t *testing.T) {
	for code, want := range map[int]string{404: "http-404", 401: "http-401", 403: "http-403"} {
		h := newUIHarness(t, uiFullConfig)
		h.deps.httpDo = func(*http.Request) (*http.Response, error) { return pngResponse(code, "text/plain", "no"), nil }
		_, exit, verdict := h.waitVerdict(uiSignedLink("a.png", uiFarExp))
		if exit != 7 || !strings.Contains(verdict, "a.png "+want+"\n") {
			t.Errorf("code %d: exit = %d, verdict:\n%s", code, exit, verdict)
		}
	}
}

func TestUITestShots_RetriesOnce(t *testing.T) {
	// 503 then 200 downloads.
	h := newUIHarness(t, uiFullConfig)
	calls := 0
	h.deps.httpDo = func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return pngResponse(503, "", ""), nil
		}
		return pngResponse(200, "image/png", "PNGDATA"), nil
	}
	if _, exit, _ := h.waitVerdict(uiSignedLink("a.png", uiFarExp)); exit != 0 || calls != 2 {
		t.Errorf("503 then 200: exit = %d, calls = %d", exit, calls)
	}

	// A timeout then 200 downloads.
	h = newUIHarness(t, uiFullConfig)
	calls = 0
	h.deps.httpDo = func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, timeoutErr{}
		}
		return pngResponse(200, "image/png", "PNGDATA"), nil
	}
	if _, exit, _ := h.waitVerdict(uiSignedLink("a.png", uiFarExp)); exit != 0 || calls != 2 {
		t.Errorf("timeout then 200: exit = %d, calls = %d", exit, calls)
	}

	// Two 503s give up after one retry; a 404 is never retried.
	h = newUIHarness(t, uiFullConfig)
	calls = 0
	h.deps.httpDo = func(*http.Request) (*http.Response, error) { calls++; return pngResponse(503, "", ""), nil }
	if _, exit, verdict := h.waitVerdict(uiSignedLink("a.png", uiFarExp)); exit != 7 || calls != 2 || !strings.Contains(verdict, "a.png http-503") {
		t.Errorf("503,503: exit = %d, calls = %d", exit, calls)
	}
	h = newUIHarness(t, uiFullConfig)
	calls = 0
	h.deps.httpDo = func(*http.Request) (*http.Response, error) { calls++; return pngResponse(404, "", ""), nil }
	if _, exit, _ := h.waitVerdict(uiSignedLink("a.png", uiFarExp)); exit != 7 || calls != 1 {
		t.Errorf("404: exit = %d, calls = %d, want one call", exit, calls)
	}
}

func TestUITestShots_NetworkErrorAfterRetryExit7(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.deps.httpDo = func(*http.Request) (*http.Response, error) { return nil, errors.New("connection refused") }
	_, exit, verdict := h.waitVerdict(uiSignedLink("a.png", uiFarExp))
	if exit != 7 || !strings.Contains(verdict, "a.png network\n") {
		t.Errorf("exit = %d, verdict:\n%s", exit, verdict)
	}
	if strings.Contains(verdict, "SECRETSIG") {
		t.Error("verdict.md leaked a signature")
	}
}

func TestUITestShots_WrongTypeAndTooLarge(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.deps.httpDo = func(*http.Request) (*http.Response, error) { return pngResponse(200, "text/html", "<html>"), nil }
	if _, exit, verdict := h.waitVerdict(uiSignedLink("a.png", uiFarExp)); exit != 7 || !strings.Contains(verdict, "a.png not-png\n") {
		t.Errorf("html: exit = %d, verdict:\n%s", exit, verdict)
	}

	h = newUIHarness(t, uiFullConfig)
	big := strings.Repeat("x", uiShotMaxBytes+1)
	h.deps.httpDo = func(*http.Request) (*http.Response, error) { return pngResponse(200, "image/png", big), nil }
	if _, exit, verdict := h.waitVerdict(uiSignedLink("a.png", uiFarExp)); exit != 7 || !strings.Contains(verdict, "a.png too-large\n") {
		t.Errorf("11MB: exit = %d", exit)
	}

	// Exactly the cap is accepted.
	h = newUIHarness(t, uiFullConfig)
	ok := strings.Repeat("x", uiShotMaxBytes)
	h.deps.httpDo = func(*http.Request) (*http.Response, error) { return pngResponse(200, "image/png", ok), nil }
	if _, exit, _ := h.waitVerdict(uiSignedLink("a.png", uiFarExp)); exit != 0 {
		t.Errorf("10MB: exit = %d, want 0", exit)
	}
}

func TestUITestShots_NoLinksAndSigningDisabledExit7(t *testing.T) {
	for name, tc := range map[string]struct{ finalizer, want string }{
		"screenshots: 0":        {"attempt 1: passed\nrun: " + uiRunID + "\nscreenshots: 0", "screenshots no-links"},
		"screenshots: disabled": {"attempt 1: passed\nrun: " + uiRunID + "\nscreenshots: disabled (no signing secret)", "screenshots signing-disabled"},
		"no finalizer comment":  {"", "screenshots no-links"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newUIHarness(t, uiFullConfig)
			h.fakePNG()
			h.polls = []uiTestBead{closedBead("done")}
			h.comments = eriKD2WComments()
			if tc.finalizer != "" {
				h.comments = append(h.comments, uiTestComment{Author: "job:ui-test", CreatedAt: "2026-11-02T13:53:15Z", Text: tc.finalizer})
			}
			out, err := h.wait(time.Minute)
			if exitOf(err) != 7 {
				t.Fatalf("exit = %d, want 7 (%v)", exitOf(err), err)
			}
			data, _ := os.ReadFile(filepath.Join(out, "verdict.md"))
			if !strings.Contains(string(data), "missing:\n"+tc.want+"\n") {
				t.Errorf("verdict.md:\n%s", data)
			}
			if len(h.httpGets) != 0 {
				t.Error("fetched with no links")
			}
		})
	}
}

func TestUITestShots_OtherRunsLinksAreIgnored(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.fakePNG()
	h.polls = []uiTestBead{closedBead("done")}
	h.comments = append(eriKD2WComments(), uiTestComment{Author: "job:ui-test", CreatedAt: "2026-11-02T13:53:15Z",
		Text: "attempt 1: passed\nrun: some-other-run\nscreenshots: 1\nartifact: " + uiSignedLink("x.png", uiFarExp)})
	if _, err := h.wait(time.Minute); exitOf(err) != 7 || len(h.httpGets) != 0 {
		t.Errorf("exit = %d, GETs = %d; links from another run must not be used", exitOf(err), len(h.httpGets))
	}
}

func TestUITestShots_NamesStayInsideScreenshotsDir(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.fakePNG()
	out, exit, _ := h.waitVerdict(
		uiSignedLink("../../etc/passwd", uiFarExp),
		uiSignedLink("a b.png", uiFarExp),
		uiSignedLink("dup.png", uiFarExp),
		uiSignedLink("dup.png", uiFarExp))
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	entries, err := os.ReadDir(filepath.Join(out, "screenshots"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	want := "a_b.png dup-2.png dup.png passwd.png"
	if strings.Join(got, " ") != want {
		t.Errorf("files = %v, want %s", got, want)
	}
	if _, err := os.Stat(filepath.Join(out, "..", "etc")); err == nil {
		t.Error("wrote outside --out")
	}
}

func TestUITestShots_FileNameSanitizing(t *testing.T) {
	used := map[string]bool{}
	for in, want := range map[string]string{
		"../../etc/passwd": "passwd.png",
		"a b.png":          "a_b.png",
		"shot":             "shot.png",
		`C:\x\y z.png`:     "y_z.png",
		"..":               "...png",
		"":                 "screenshot.png",
	} {
		if got := uiShotFileName(in, map[string]bool{}); got != want {
			t.Errorf("uiShotFileName(%q) = %q, want %q", in, got, want)
		}
	}
	if a, b := uiShotFileName("x.png", used), uiShotFileName("x.png", used); a != "x.png" || b != "x-2.png" {
		t.Errorf("duplicates = %q, %q", a, b)
	}
}
