package verbs

import (
	"bytes"
	"encoding/base64"
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
	for _, want := range []string{"<title>", "<h1>", `class="lead"`, "Questions for Eric", `class="example"`, "<h2>Beads</h2>", `class="revlog"`, "sequenceDiagram", "figure img"} {
		if !strings.Contains(got, want) {
			t.Errorf("template missing %q", want)
		}
	}
	visible := pageCommentRe.ReplaceAllString(got, "")
	if strings.Contains(visible, "<script") {
		t.Error("template markup contains a <script>")
	}
	// A stray "-->" inside the usage comment would end it early and render the rest as text.
	if strings.Contains(visible, "PRIMITIVES") || strings.Contains(visible, "EVERYTHING ELSE") || strings.Contains(visible, "participant P") {
		t.Error("usage comment ends early; visible text leaks")
	}
	for _, c := range pageCommentRe.FindAllString(got, -1) {
		body := strings.TrimSuffix(strings.TrimPrefix(c, "<!--"), "-->")
		if strings.Contains(body, "-->") || strings.Contains(body, "--!>") {
			t.Errorf("comment body contains a comment terminator: %.60q", body)
		}
	}
	if first := pageCommentRe.FindString(got); !strings.Contains(first, "RULES:") {
		t.Error("first comment does not reach the end of the usage text")
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

func TestNormalizePageHTML_MermaidLoaderHasScrollFloor(t *testing.T) {
	got := normalizePageHTML(`<title>T</title><pre class="mermaid">flowchart TD; A-->B</pre>`)
	for _, want := range []string{"mermaid@11.4.1", "startOnLoad:false", "mermaid.run()", "style.minWidth=(v.width*0.65)"} {
		if !strings.Contains(got, want) {
			t.Errorf("loader missing %q: %s", want, got)
		}
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
}

func TestPagePublish_SizeGuard(t *testing.T) {
	pageInRun(t)
	// A full document is posted unchanged, so the page length is exactly the file length.
	const head, tail = "<html><body>", "</body></html>"
	fill := func(n int) string { return head + strings.Repeat("a", n-len(head)-len(tail)) + tail }
	calls := 0
	do := func(*http.Request) (*http.Response, error) {
		calls++
		return pageResp(200, `{"id":"a","url":"https://d.example/pages/a","version":1,"created":true}`), nil
	}
	if _, err := pageRunFile(t, &pagePublishKong{httpDo: do}, fill(pageHTMLMax)); err != nil || calls != 1 {
		t.Fatalf("exactly %d should post: err=%v calls=%d", pageHTMLMax, err, calls)
	}
	_, err := pageRunFile(t, &pagePublishKong{httpDo: do}, fill(pageHTMLMax+1))
	if err == nil || !strings.Contains(err.Error(), "900000") || calls != 1 {
		t.Fatalf("over the limit: err=%v calls=%d", err, calls)
	}
	// Dry-run still prints, then warns on stderr and exits 0.
	f := filepath.Join(t.TempDir(), "big.html")
	if werr := os.WriteFile(f, []byte(fill(pageHTMLMax+1)), 0o644); werr != nil {
		t.Fatal(werr)
	}
	var out, errb bytes.Buffer
	if err := (&pagePublishKong{File: f, DryRun: true}).Run(&cli.Context{Stdout: &out, Stderr: &errb}); err != nil {
		t.Fatal(err)
	}
	if out.Len() != pageHTMLMax+1 || !strings.Contains(errb.String(), "warning: page publish: page is") {
		t.Errorf("stdout=%d bytes stderr=%q", out.Len(), errb.String())
	}
}

func pageImageRun(t *testing.T, name string, data []byte, alt, caption string) (string, error) {
	t.Helper()
	f := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(f, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := (&pageImageKong{File: f, Alt: alt, Caption: caption}).Run(&cli.Context{Stdout: &out, Stderr: &bytes.Buffer{}})
	return out.String(), err
}

var (
	pagePNG  = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")
	pageJPEG = []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")
)

func TestPageImage_PNGSnippetRoundTripsAndEscapes(t *testing.T) {
	out, err := pageImageRun(t, "a.png", pagePNG, `a "b" <c>`, "x & y")
	if err != nil {
		t.Fatal(err)
	}
	const prefix = `<figure><img src="data:image/png;base64,`
	if !strings.HasPrefix(out, prefix) {
		t.Fatalf("out = %q", out)
	}
	b64 := out[len(prefix):strings.Index(out, `"`+` alt=`)]
	if dec, derr := base64.StdEncoding.DecodeString(b64); derr != nil || !bytes.Equal(dec, pagePNG) {
		t.Errorf("decoded bytes differ: %v", derr)
	}
	if !strings.Contains(out, `alt="a &#34;b&#34; &lt;c&gt;"`) || !strings.Contains(out, "<figcaption>x &amp; y</figcaption></figure>") {
		t.Errorf("alt/caption not escaped: %q", out)
	}
}

func TestPageImage_JPEGDetectedAndGuards(t *testing.T) {
	out, err := pageImageRun(t, "a.bin", pageJPEG, "alt", "")
	if err != nil || !strings.Contains(out, "data:image/jpeg;base64,") || strings.Contains(out, "figcaption") {
		t.Errorf("jpeg: out=%q err=%v", out, err)
	}
	if _, err := pageImageRun(t, "a.png", pagePNG, "", ""); err == nil || !strings.Contains(err.Error(), "--alt") {
		t.Errorf("missing alt: err=%v", err)
	}
	big := append(append([]byte{}, pagePNG...), make([]byte, pageImageMax)...)
	if _, err := pageImageRun(t, "big.png", big, "alt", ""); err == nil || !strings.Contains(err.Error(), "300000") {
		t.Errorf("oversize: err=%v", err)
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
