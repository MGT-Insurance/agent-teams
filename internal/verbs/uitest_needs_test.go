package verbs

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func (h *uiHarness) fileNeeds(needs ...string) error {
	cmd := &uiTestFileKong{Worktree: "/wt", Scope: "mithril", ItemsFile: h.itemsFile("- check\n"), Needs: needs, deps: h.deps}
	return cmd.Run(h.ctx)
}

func TestUINeeds_RejectsUnknownAndRatingAlone(t *testing.T) {
	cases := []struct {
		needs []string
		want  string
	}{
		{[]string{"bogus"}, `--needs must be socotra-tenant or rating`},
		{[]string{"rating"}, uiNeedsRatingRefusal},
		{[]string{"rating", "rating"}, uiNeedsRatingRefusal},
	}
	for _, c := range cases {
		h := newUIHarness(t, uiFullConfig)
		err := h.fileNeeds(c.needs...)
		if exitOf(err) != 2 || err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("needs %v: exit %d err %v, want exit 2 containing %q", c.needs, exitOf(err), err, c.want)
		}
		if len(h.filed) != 0 || len(h.gitCalls) != 0 {
			t.Errorf("needs %v: filed %v git %v, want nothing touched", c.needs, h.filed, h.gitCalls)
		}
	}
}

func TestUINeeds_LocalFilingWritesNeedsHeaderOnlyWhenGiven(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	if err := h.fileNeeds("socotra-tenant", "rating"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := uiTestBeadBodyNeeds("https://github.com/acme/widgets.git", "feat-x", uiTestSHA, "mithril", "", "ui-test", []string{"socotra-tenant", "rating"}, "- check\n")
	if h.filed[0].body != want || !strings.Contains(want, "\nneeds: socotra-tenant,rating\n---\n") {
		t.Errorf("body = %q", h.filed[0].body)
	}

	h = newUIHarness(t, uiFullConfig)
	if err := h.fileNeeds(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := h.filed[0].body, uiTestBead2Plain("- check\n"); got != want || strings.Contains(got, "needs") {
		t.Errorf("no-needs body = %q, want byte-identical %q", got, want)
	}
}

func uiTestBead2Plain(items string) string {
	return uiTestBeadBody("https://github.com/acme/widgets.git", "feat-x", uiTestSHA, "mithril", "", "ui-test", items)
}

func TestUINeeds_RunToolsSendsNeedsAndAcceptsEcho(t *testing.T) {
	srv := newC0Server(t, func(w http.ResponseWriter, _ map[string]any) {
		writeJSON(w, 200, map[string]any{"id": "dsp-ui9", "url": "u", "created": true, "type": "ui-test", "needs": []string{"rating", "socotra-tenant"}})
	})
	h := newUIHarness(t, "")
	inRun(t, srv)
	h.deps.httpDo = nil
	if err := h.fileNeeds("socotra-tenant", "rating"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := srv.requests[0]["needs"]; !reflect.DeepEqual(got, []any{"socotra-tenant", "rating"}) {
		t.Errorf("needs sent = %v", got)
	}
	if strings.TrimSpace(h.stdout.String()) != "dsp-ui9" {
		t.Errorf("stdout = %q", h.stdout.String())
	}
}

func TestUINeeds_RunToolsMissingOrWrongEchoExits5(t *testing.T) {
	for name, echo := range map[string]any{"omitted": nil, "partial": []string{"socotra-tenant"}} {
		srv := newC0Server(t, func(w http.ResponseWriter, _ map[string]any) {
			resp := map[string]any{"id": "dsp-ui9", "url": "u", "created": true, "type": "ui-test"}
			if echo != nil {
				resp["needs"] = echo
			}
			writeJSON(w, 200, resp)
		})
		h := newUIHarness(t, "")
		inRun(t, srv)
		h.deps.httpDo = nil
		err := h.fileNeeds("socotra-tenant", "rating")
		if exitOf(err) != uiExitIneligible {
			t.Errorf("%s: exit = %d, want %d (%v)", name, exitOf(err), uiExitIneligible, err)
		}
		if h.stdout.String() != "" || !strings.Contains(h.stderr.String(), "dsp-ui9 was filed without those needs") {
			t.Errorf("%s: stdout %q stderr %q", name, h.stdout.String(), h.stderr.String())
		}
	}
}

func TestUINeeds_WaitPassesPoolFullThroughWithExit8(t *testing.T) {
	const line = "POOL_FULL (100/100 leased)"
	cases := map[string]struct {
		bead     uiTestBead
		comments []uiTestComment
	}{
		"not started comment": {closedBead("failed"), []uiTestComment{{Author: "dispatcher", CreatedAt: "2026-11-02T13:49:40Z", Text: "not started: " + line}}},
		"job comment":         {closedBead("failed"), []uiTestComment{{Author: "job:ui-test", CreatedAt: "2026-11-02T13:49:40Z", Text: "not started: " + line}}},
		"close reason only":   {closedBead("not started: " + line), nil},
	}
	for name, c := range cases {
		h := newUIHarness(t, uiFullConfig)
		h.polls = []uiTestBead{c.bead}
		h.comments = c.comments
		out, err := h.wait(time.Minute)
		if exitOf(err) != uiExitPoolFull {
			t.Fatalf("%s: exit = %d, want %d (%v)", name, exitOf(err), uiExitPoolFull, err)
		}
		if got := strings.TrimSpace(h.stderr.String()); got != line {
			t.Errorf("%s: stderr = %q, want %q verbatim", name, got, line)
		}
		data, rerr := os.ReadFile(filepath.Join(out, "failure.md"))
		if rerr != nil || strings.TrimSpace(string(data)) != line {
			t.Errorf("%s: failure.md = %q (%v)", name, data, rerr)
		}
	}
}

func TestUINeeds_WaitPoolFullOnlyFromTheNewestJobCommentOrCloseReason(t *testing.T) {
	const line = "not started: POOL_FULL (1/1 leased)"
	cases := map[string][]uiTestComment{
		"older comment, newer failure": {
			{Author: "dispatcher", CreatedAt: "2026-11-02T13:00:00Z", Text: line},
			{Author: "job:ui-test", CreatedAt: "2026-11-02T13:30:00Z", Text: "run crashed: browser exited"},
		},
		"human comment quotes the text": {
			{Author: "job:ui-test", CreatedAt: "2026-11-02T13:00:00Z", Text: "run crashed: browser exited"},
			{Author: "eric", CreatedAt: "2026-11-02T13:30:00Z", Text: line},
		},
		"bare line without not started": {
			{Author: "job:ui-test", CreatedAt: "2026-11-02T13:30:00Z", Text: "POOL_FULL (1/1 leased)"},
		},
	}
	for name, comments := range cases {
		h := newUIHarness(t, uiFullConfig)
		h.polls = []uiTestBead{closedBead("failed")}
		h.comments = comments
		_, err := h.wait(time.Minute)
		if exitOf(err) == uiExitPoolFull {
			t.Errorf("%s: exit = %d, want any other failure", name, exitOf(err))
		}
	}
}

func TestUINeeds_WaitPoolFullDoesNotMaskAVerdict(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.polls = []uiTestBead{closedBead("done")}
	h.comments = append([]uiTestComment{{Author: "dispatcher", CreatedAt: "2026-11-02T13:00:00Z", Text: "not started: POOL_FULL (1/1 leased)"}}, eriKD2WComments()...)
	h.fakePNG()
	if _, err := h.wait(time.Minute); exitOf(err) == uiExitPoolFull {
		t.Errorf("a bead with a verdict exited %d", uiExitPoolFull)
	}
}
