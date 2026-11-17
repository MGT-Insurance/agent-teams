package verbs

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

const uiKindDesc = "repo: https://github.com/acme/widgets.git\nbranch: feat-x\nsha: " + uiTestSHA + "\ntype: ui-test\nkind: api\nagent: tester\nfix: none\nscope: mithril\n---\nitems\n"

func (h *uiHarness) fileKind(kind, items string) error {
	cmd := &uiTestFileKong{Worktree: "/wt", Scope: "mithril", Kind: kind, ItemsFile: h.itemsFile(items), deps: h.deps}
	return cmd.Run(h.ctx)
}

func TestLiveTest_BodyKindLine(t *testing.T) {
	const items = "items\n"
	plain := uiTestBeadBody("https://github.com/acme/widgets.git", "feat-x", uiTestSHA, "mithril", "", "", items)
	for _, k := range []string{"", "ui"} {
		if got := uiTestBeadBody("https://github.com/acme/widgets.git", "feat-x", uiTestSHA, "mithril", "", k, items); got != plain {
			t.Errorf("kind %q changed the ui body:\n%s", k, got)
		}
	}
	if strings.Contains(plain, "kind:") {
		t.Errorf("ui body has a kind line:\n%s", plain)
	}
	want := strings.Replace(plain, "type: ui-test\n", "type: ui-test\nkind: api\n", 1)
	if got := uiTestBeadBody("https://github.com/acme/widgets.git", "feat-x", uiTestSHA, "mithril", "", "api", items); got != want {
		t.Errorf("api body:\n%s\nwant:\n%s", got, want)
	}
}

func TestLiveTest_FileAPIKindAddsHeaderAndLabel(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	if err := h.fileKind("api", "- call it\n"); err != nil {
		t.Fatal(err)
	}
	f := h.filed[0]
	if strings.Join(f.labels, ",") != "type:ui-test,agent:tester,repo:widgets,kind:api" {
		t.Errorf("labels = %v", f.labels)
	}
	if f.title != "ui-test: feat-x" || !strings.Contains(f.body, "type: ui-test\nkind: api\nagent: tester\n") {
		t.Errorf("title %q body:\n%s", f.title, f.body)
	}
}

func TestLiveTest_FileExplicitUIMatchesDefault(t *testing.T) {
	a, b := newUIHarness(t, uiFullConfig), newUIHarness(t, uiFullConfig)
	if err := a.fileKind("", "x\n"); err != nil {
		t.Fatal(err)
	}
	if err := b.fileKind("ui", "x\n"); err != nil {
		t.Fatal(err)
	}
	if a.filed[0].body != b.filed[0].body || strings.Join(a.filed[0].labels, ",") != strings.Join(b.filed[0].labels, ",") {
		t.Errorf("explicit ui differs: %+v vs %+v", a.filed[0], b.filed[0])
	}
}

func TestLiveTest_UnknownKindIsUsageError(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	err := h.fileKind("doc-upload", "x\n")
	if err == nil || !strings.Contains(err.Error(), "--kind must be ui or api") || exitOf(err) != 2 {
		t.Errorf("err = %v, exit %d", err, exitOf(err))
	}
	if len(h.filed) != 0 {
		t.Error("filed a bead")
	}
}

func TestLiveTest_NotConfiguredExits5BeforeKindLogic(t *testing.T) {
	h := newUIHarness(t, "")
	err := h.fileKind("bogus", "x\n")
	if exitOf(err) != uiExitIneligible {
		t.Fatalf("exit = %d, want 5 (err %v)", exitOf(err), err)
	}
	if got := h.stderr.String(); got != uiNotConfiguredMsg+"\n" {
		t.Errorf("stderr = %q", got)
	}
	if len(h.gitCalls) != 0 || len(h.filed) != 0 {
		t.Error("touched git or bd")
	}
}

func TestLiveTest_RunToolsSendsKindAndRequiresEcho(t *testing.T) {
	srv := newC0Server(t, func(w http.ResponseWriter, body map[string]any) {
		resp := map[string]any{"id": "dsp-ui9", "url": "u", "created": true}
		if title, _ := body["title"].(string); title == "echo" {
			resp["kind"] = body["kind"]
		}
		writeJSON(w, 200, resp)
	})
	h := newUIHarness(t, "")
	inRun(t, srv)
	cmd := &uiTestFileKong{Worktree: "/wt", Scope: "mithril", Kind: "api", Title: "echo", ItemsFile: h.itemsFile("x"), deps: h.deps}
	if err := cmd.Run(h.ctx); err != nil {
		t.Fatalf("echoed kind: %v", err)
	}
	if srv.requests[0]["kind"] != "api" {
		t.Errorf("body = %v, want kind api", srv.requests[0])
	}
	if strings.TrimSpace(h.stdout.String()) != "dsp-ui9" {
		t.Errorf("stdout = %q", h.stdout.String())
	}

	h = newUIHarness(t, "")
	inRun(t, srv)
	cmd = &uiTestFileKong{Worktree: "/wt", Scope: "mithril", Kind: "api", Title: "no echo", ItemsFile: h.itemsFile("x"), deps: h.deps}
	err := cmd.Run(h.ctx)
	if exitOf(err) != uiExitIneligible {
		t.Fatalf("exit = %d, want 5 (err %v)", exitOf(err), err)
	}
	if !strings.Contains(h.stderr.String(), "Dispatcher does not support --kind api yet; test locally\n") {
		t.Errorf("stderr = %q", h.stderr.String())
	}
	if h.stdout.String() != "" {
		t.Errorf("stdout = %q, want empty on failure", h.stdout.String())
	}
}

func TestLiveTest_RunToolsUIOmitsKindAndNeedsNoEcho(t *testing.T) {
	srv := newC0Server(t, func(w http.ResponseWriter, _ map[string]any) {
		writeJSON(w, 200, map[string]any{"id": "dsp-ui9", "url": "u", "created": true})
	})
	h := newUIHarness(t, "")
	inRun(t, srv)
	if err := h.fileKind("ui", "x"); err != nil {
		t.Fatal(err)
	}
	if _, has := srv.requests[0]["kind"]; has {
		t.Errorf("body = %v, want no kind key", srv.requests[0])
	}
}

const uiTranscriptJSON = `{"version":1,"calls":[{"id":"c1","method":"GET","status":200}]}`

// kindHarness serves description desc and the given artifacts: names ending in
// .json answer with transcriptBody, any other name answers with a PNG.
func kindHarness(t *testing.T, desc, transcriptBody string, names ...string) *uiHarness {
	t.Helper()
	h := newUIHarness(t, uiFullConfig)
	bead := closedBead("done")
	bead.Description = desc
	h.polls = []uiTestBead{bead}
	var links []string
	for _, n := range names {
		links = append(links, uiSignedLink(n, uiFarExp))
	}
	h.comments = withFinalizer(links...)
	h.deps.httpDo = func(req *http.Request) (*http.Response, error) {
		h.httpGets = append(h.httpGets, req)
		if strings.Contains(req.URL.Path, ".json") {
			return pngResponse(200, "application/json", transcriptBody), nil
		}
		return pngResponse(200, "image/png", "PNGDATA"), nil
	}
	return h
}

func TestLiveTest_WaitEvidenceByKind(t *testing.T) {
	cases := []struct {
		name  string
		desc  string
		json  string
		files []string
		exit  int
	}{
		{"api transcript only", uiKindDesc, uiTranscriptJSON, []string{"transcript.json"}, 0},
		{"ui transcript only", eriKD2WDescription, uiTranscriptJSON, []string{"transcript.json"}, uiExitNoEvidence},
		{"api PNGs only", uiKindDesc, uiTranscriptJSON, []string{"01-home.png"}, uiExitNoEvidence},
		{"api empty transcript", uiKindDesc, `{"version":1,"calls":[]}`, []string{"transcript.json"}, uiExitNoEvidence},
		{"api bad transcript", uiKindDesc, `not json`, []string{"transcript.json"}, uiExitNoEvidence},
		{"ui PNG only", eriKD2WDescription, uiTranscriptJSON, []string{"01-home.png"}, 0},
		{"ui PNG plus transcript", eriKD2WDescription, uiTranscriptJSON, []string{"01-home.png", "transcript.json"}, 0},
		{"api transcript plus PNG", uiKindDesc, uiTranscriptJSON, []string{"transcript.json", "01-home.png"}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := kindHarness(t, c.desc, c.json, c.files...)
			out, err := h.wait(time.Minute)
			if exitOf(err) != c.exit {
				t.Fatalf("exit = %d, want %d (err %v)", exitOf(err), c.exit, err)
			}
			verdict, rerr := os.ReadFile(filepath.Join(out, "verdict.md"))
			if rerr != nil {
				t.Fatal(rerr)
			}
			if !strings.Contains(string(verdict), "evidence:\n") || !strings.Contains(string(verdict), "screenshots:\n") {
				t.Errorf("verdict.md lacks the evidence or screenshots block:\n%s", verdict)
			}
		})
	}
}

func TestLiveTest_WaitAPIPrintsTranscriptPathAndKeepsFile(t *testing.T) {
	h := kindHarness(t, uiKindDesc, uiTranscriptJSON, "transcript.json")
	out, err := h.wait(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	tr := filepath.Join(out, "transcript.json")
	if got := h.stdout.String(); got != filepath.Join(out, "verdict.md")+"\n"+tr+"\n" {
		t.Errorf("stdout = %q", got)
	}
	if b, err := os.ReadFile(tr); err != nil || string(b) != uiTranscriptJSON {
		t.Errorf("transcript = %q, %v", b, err)
	}
	verdict, _ := os.ReadFile(filepath.Join(out, "verdict.md"))
	if !strings.Contains(string(verdict), "kind: api\nevidence:\n"+tr+"\nscreenshots:\n") {
		t.Errorf("verdict.md:\n%s", verdict)
	}
}

func TestLiveTest_AliasParsesSameCommandAndHelpListsLiveTest(t *testing.T) {
	var stdout, stderr strings.Builder
	var exitCode *int
	p, err := cli.NewParser(kong.Writers(&stdout, &stderr), kong.Exit(func(c int) { exitCode = &c }))
	if err != nil {
		t.Fatal(err)
	}
	RegisterUITestKong(p)
	for _, args := range [][]string{
		{"file", "--worktree", "/wt", "--scope", "m", "--items-file", "/i"},
		{"wait", "dsp-1", "--out", "/o"},
		{"watch", "dsp-1"},
		{"reply", "dsp-1", "--text", "x"},
	} {
		a, err := p.Parse(append([]string{"ui-test"}, args...))
		if err != nil {
			t.Fatalf("ui-test %v: %v", args, err)
		}
		b, err := p.Parse(append([]string{"live-test"}, args...))
		if err != nil {
			t.Fatalf("live-test %v: %v", args, err)
		}
		if strings.TrimPrefix(a.Command(), "ui-test") != strings.TrimPrefix(b.Command(), "live-test") {
			t.Errorf("%v: %q vs %q", args, a.Command(), b.Command())
		}
	}
	_, _ = p.Parse([]string{"--help"})
	if exitCode == nil || *exitCode != 0 {
		t.Fatalf("--help exit = %v", exitCode)
	}
	help := stdout.String()
	if !strings.Contains(help, "live-test") || strings.Contains(help, "ui-test") {
		t.Errorf("help should list live-test only:\n%s", help)
	}
}
