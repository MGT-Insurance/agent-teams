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

func TestPagePublish_CreateDoesNotRetry5xx(t *testing.T) {
	pageInRun(t)
	calls := 0
	cmd := &pagePublishKong{httpDo: func(*http.Request) (*http.Response, error) {
		calls++
		return pageResp(503, `{"error":"down"}`), nil
	}}
	_, _, err := pageTestRun(t, cmd)
	if err == nil {
		t.Fatal("want error")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestPagePublish_RepublishRetries5xxOnce(t *testing.T) {
	pageInRun(t)
	calls := 0
	cmd := &pagePublishKong{URL: "abc", httpDo: func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return pageResp(503, `{"error":"down"}`), nil
		}
		return pageResp(200, `{"id":"abc","url":"https://d.example/pages/abc","version":2,"created":false}`), nil
	}}
	if _, _, err := pageTestRun(t, cmd); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
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

func pageRunFile(t *testing.T, cmd *pagePublishKong, html string) (stdout string, err error) {
	t.Helper()
	f := filepath.Join(t.TempDir(), "p.html")
	if werr := os.WriteFile(f, []byte(html), 0o644); werr != nil {
		t.Fatal(werr)
	}
	cmd.File = f
	var out, errb bytes.Buffer
	err = cmd.Run(&cli.Context{Home: t.TempDir(), Stdout: &out, Stderr: &errb})
	return out.String(), err
}

func TestPageTemplate_PrintsKitWithRequiredParts(t *testing.T) {
	var out bytes.Buffer
	if err := (&pageTemplateKong{}).Run(&cli.Context{Stdout: &out, Stderr: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"<title>", `class="lead"`, "Questions for Eric", "<h2>Beads</h2>", `class="revlog"`, "<h1>"} {
		if !strings.Contains(got, want) {
			t.Errorf("template missing %q", want)
		}
	}
	visible := pageCommentRe.ReplaceAllString(got, "")
	if strings.Contains(visible, "<script") {
		t.Error("template markup contains a <script>")
	}
	// A stray "-->" inside the usage comment would end it early and render the rest as text.
	if strings.Contains(visible, "PRIMITIVES") || strings.Contains(visible, "EVERYTHING ELSE") {
		t.Error("usage comment ends early; visible text leaks")
	}
}

func TestPageTemplate_OutRefusesOverwriteWithoutForce(t *testing.T) {
	f := filepath.Join(t.TempDir(), "plan.html")
	ctx := &cli.Context{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if err := (&pageTemplateKong{Out: f}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (&pageTemplateKong{Out: f}).Run(ctx); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want overwrite refusal", err)
	}
	if b, _ := os.ReadFile(f); string(b) != "mine" {
		t.Errorf("file was overwritten: %q", b)
	}
	if err := (&pageTemplateKong{Out: f, Force: true}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(f); string(b) != planPageTemplate {
		t.Error("--force did not write the kit")
	}
}

func TestNormalizePageHTML_WrapsFragment(t *testing.T) {
	got := normalizePageHTML("<title>T</title>\n<style>p{}</style>\n<main>hi</main>\n")
	for _, want := range []string{"<!doctype html><html lang=\"en\"><head>", "viewport-fit=cover", "<title>T</title>\n<style>p{}</style>\n</head><body>\n<main>hi</main>", "</body></html>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "mermaid") {
		t.Error("loader injected without a diagram")
	}
}

func TestNormalizePageHTML_MermaidLoaderOnlyWhenNeeded(t *testing.T) {
	frag := `<title>T</title><figure><pre class="mermaid">flowchart LR; A-->B</pre></figure>`
	got := normalizePageHTML(frag)
	if strings.Count(got, "mermaid@11.4.1") != 1 || !strings.Contains(got, "</script></body></html>") {
		t.Errorf("loader not injected once before </body>: %s", got)
	}
	if again := normalizePageHTML(got); again != got {
		t.Error("normalize is not idempotent")
	}
	// Usage text inside a comment is not a diagram.
	if strings.Contains(normalizePageHTML(`<title>T</title><!-- <pre class="mermaid"> --><p>x</p>`), "mermaid@") {
		t.Error("loader injected for a commented-out example")
	}
	// The kit itself has no diagram.
	if strings.Contains(normalizePageHTML(planPageTemplate), "mermaid@") {
		t.Error("loader injected for the unfilled kit")
	}
}

func TestNormalizePageHTML_FullDocWithMermaidPassesThrough(t *testing.T) {
	doc := `<!doctype html><html><body><pre class="mermaid">A-->B</pre><script src="https://x/mermaid.min.js"></script></body></html>`
	if got := normalizePageHTML(doc); got != doc {
		t.Errorf("changed:\n%s", got)
	}
}

func TestPagePublish_DryRunPrintsAndMakesNoHTTPCall(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	t.Setenv("DSP_API_URL", "")
	cmd := &pagePublishKong{DryRun: true, httpDo: func(*http.Request) (*http.Response, error) {
		t.Error("network used in --dry-run")
		return nil, nil
	}}
	out, err := pageRunFile(t, cmd, `<title>T</title><pre class="mermaid">A-->B</pre>`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "<!doctype html>") || !strings.Contains(out, "mermaid@11.4.1") {
		t.Errorf("out = %q", out)
	}
}

func TestPagePublish_BodyCarriesNormalizedHTML(t *testing.T) {
	pageInRun(t)
	var body string
	cmd := &pagePublishKong{httpDo: func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		return pageResp(200, `{"id":"a","url":"https://d.example/pages/a","version":1,"created":true}`), nil
	}}
	if _, err := pageRunFile(t, cmd, "<title>T</title><main>x</main>"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "<!doctype html><html lang=") {
		t.Errorf("body not normalized: %s", body)
	}
}
