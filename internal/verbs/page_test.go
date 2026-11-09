package verbs

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

const pageTestHTML = "<html><body><p>a & b</p></body></html>\n"

func pageTestRun(t *testing.T, cmd *pagePublishKong) (stdout, stderr string, err error) {
	t.Helper()
	f := filepath.Join(t.TempDir(), "plan.html")
	if werr := os.WriteFile(f, []byte(pageTestHTML), 0o644); werr != nil {
		t.Fatal(werr)
	}
	cmd.File = f
	var out, errb bytes.Buffer
	err = cmd.Run(&cli.Context{Home: t.TempDir(), Stdout: &out, Stderr: &errb})
	return out.String(), errb.String(), err
}

func pageInRun(t *testing.T) {
	t.Helper()
	t.Setenv("DSP_BEAD", "dsp-run1")
	t.Setenv("DSP_API_URL", "https://d.example")
}

func TestPagePublish_OutsideRunExits5(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	t.Setenv("DSP_API_URL", "")
	cmd := &pagePublishKong{httpDo: func(*http.Request) (*http.Response, error) {
		t.Error("network used outside a run")
		return nil, nil
	}}
	stdout, stderr, err := pageTestRun(t, cmd)
	if exitOf(err) != 5 {
		t.Errorf("exit = %d, want 5", exitOf(err))
	}
	if stdout != "" || strings.TrimSpace(stderr) != "page publish: not in a Dispatcher run; use the Artifact tool" {
		t.Errorf("stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestPagePublish_CreatePrintsOnlyURLAndSendsUnescapedBody(t *testing.T) {
	pageInRun(t)
	var body string
	cmd := &pagePublishKong{Title: "My plan", httpDo: func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		return pageResp(200, `{"id":"abc","url":"https://d.example/pages/abc","version":1,"created":true}`), nil
	}}
	stdout, stderr, err := pageTestRun(t, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "https://d.example/pages/abc\n" {
		t.Errorf("stdout = %q", stdout)
	}
	if strings.TrimSpace(stderr) != "page publish: created v1" {
		t.Errorf("stderr = %q", stderr)
	}
	if !strings.Contains(body, "<html><body><p>a & b</p>") || strings.Contains(body, "\\u003c") || strings.Contains(body, "\\u0026") {
		t.Errorf("body is HTML-escaped: %s", body)
	}
	if !strings.Contains(body, `"op":"page_publish"`) || !strings.Contains(body, `"title":"My plan"`) || strings.Contains(body, `"page"`) {
		t.Errorf("body = %s", body)
	}
}

func TestPagePublish_URLAndBareIDSendSamePage(t *testing.T) {
	pageInRun(t)
	for _, ref := range []string{"https://d.example/pages/abc123", "https://d.example/pages/abc123/", "abc123"} {
		var body string
		cmd := &pagePublishKong{URL: ref, httpDo: func(r *http.Request) (*http.Response, error) {
			b, _ := io.ReadAll(r.Body)
			body = string(b)
			return pageResp(200, `{"id":"abc123","url":"https://d.example/pages/abc123","version":3,"created":false}`), nil
		}}
		_, stderr, err := pageTestRun(t, cmd)
		if err != nil {
			t.Fatalf("%s: %v", ref, err)
		}
		if !strings.Contains(body, `"page":"abc123"`) {
			t.Errorf("%s: body = %s", ref, body)
		}
		if strings.TrimSpace(stderr) != "page publish: updated v3" {
			t.Errorf("%s: stderr = %q", ref, stderr)
		}
	}
}

func TestPagePublish_401NamesPagesTool(t *testing.T) {
	pageInRun(t)
	cmd := &pagePublishKong{httpDo: func(*http.Request) (*http.Response, error) { return pageResp(401, ``), nil }}
	_, _, err := pageTestRun(t, cmd)
	if err == nil || !strings.Contains(err.Error(), "this run's job has no pages tool, or the run is finished") {
		t.Errorf("err = %v", err)
	}
}

func TestPagePublish_Registered(t *testing.T) {
	parser, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterPageKong(parser)
	kctx, err := parser.Parse([]string{"page", "publish", "/p.html", "--title", "T", "--url", "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if got := kctx.Command(); got != "page publish <file>" {
		t.Errorf("command = %q", got)
	}
}

func pageResp(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body))}
}
