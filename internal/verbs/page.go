package verbs

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/runtools"
)

// pageExitNotInRun is the exit code outside a Dispatcher run. The number is
// frozen by the page_publish contract bead (dsp-kb8.1).
const pageExitNotInRun = 5

const pageNotInRunMsg = "page publish: not in a Dispatcher run; use the Artifact tool"

// RegisterPageKong registers the `page` parent verb onto p.
func RegisterPageKong(p *cli.Parser) {
	p.AddVerb("page", "Plan-page kit (template) and cloud-run page publishing through the Dispatcher.", &pageCmd{})
}

// pageCmd is the kong parent struct for `ateam page <subcommand>`. It has no
// Run method: kong would run it for every subcommand (see mail_register.go).
type pageCmd struct {
	Template pageTemplateKong `cmd:"" name:"template" help:"Print the shared plan-page kit (HTML); works anywhere. --out writes it to a file."`
	Image    pageImageKong    `cmd:"" name:"image" help:"Print a <figure> snippet that embeds an image file as a data: URI; works anywhere."`
	Publish  pagePublishKong  `cmd:"" name:"publish" help:"Publish an HTML file as a Dispatcher page (cloud run only); prints the page URL."`
}

// pagePublishKong implements `ateam page publish`.
type pagePublishKong struct {
	File  string `arg:"" name:"file" help:"The self-contained HTML file to publish."`
	Title string `name:"title" help:"Page title."`
	URL   string `name:"url" help:"A page URL or id from an earlier publish; republishes that page in place."`

	DryRun bool `name:"dry-run" help:"Print the normalized HTML that would be published and exit; makes no network call and works outside a run."`

	httpDo runtools.Do
}

type pagePublishReq struct {
	Op    string `json:"op"`
	HTML  string `json:"html"`
	Title string `json:"title,omitempty"`
	Page  string `json:"page,omitempty"`
}

//go:embed embedded/plan_page.html
var planPageTemplate string

// pageTemplateKong implements `ateam page template`.
type pageTemplateKong struct {
	Out   string `name:"out" help:"Write the kit to this file instead of stdout."`
	Force bool   `name:"force" help:"With --out, overwrite an existing file."`
}

// Run prints the plan-page kit, or writes it to --out. It never needs a
// Dispatcher run.
func (c *pageTemplateKong) Run(ctx *cli.Context) error {
	if c.Out == "" {
		_, err := fmt.Fprint(ctx.Stdout, planPageTemplate)
		return err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if c.Force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(c.Out, flags, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("page template: %s already exists; pass --force to overwrite", c.Out)
		}
		return fmt.Errorf("page template: %v", err)
	}
	if _, err := f.WriteString(planPageTemplate); err != nil {
		f.Close()
		return fmt.Errorf("page template: %v", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("page template: %v", err)
	}
	return nil
}

const pageMermaidLoader = `<script src="https://cdn.jsdelivr.net/npm/mermaid@11.4.1/dist/mermaid.min.js"></script>` +
	`<script>mermaid.initialize({startOnLoad:false,theme:window.matchMedia('(prefers-color-scheme: dark)').matches?'dark':'default'});` +
	// After rendering, a diagram that Mermaid shrank below 0.65 of its natural
	// width gets a floor of 0.65 so it scrolls in its box instead of going illegible.
	`window.addEventListener('load',function(){mermaid.run().then(function(){` +
	`document.querySelectorAll('pre.mermaid svg').forEach(function(s){` +
	`var v=s.viewBox&&s.viewBox.baseVal;if(!v||!v.width)return;` +
	`if(s.getBoundingClientRect().width<v.width*0.65)s.style.minWidth=(v.width*0.65)+'px';});});});</script>`

var (
	pageCommentRe     = regexp.MustCompile(`(?s)<!--.*?-->`)
	pageMermaidUseRe  = regexp.MustCompile(`(?i)class\s*=\s*["']mermaid["']`)
	pageMermaidLoadRe = regexp.MustCompile(`(?is)<script[^>]*\bsrc\s*=[^>]*mermaid`)
	pageHeadItemRe    = regexp.MustCompile(`(?is)^\s*(<!--.*?-->|<title[^>]*>.*?</title>|<meta\b[^>]*>|<style[^>]*>.*?</style>)`)
	pageDocRe         = regexp.MustCompile(`(?i)<html|<!doctype`)
)

// normalizePageHTML wraps a fragment in a document skeleton and adds the
// pinned Mermaid loader when the page has a diagram and no loader. A full
// document that already loads Mermaid, or has no diagram, is returned
// byte-identical.
func normalizePageHTML(s string) string {
	if !pageDocRe.MatchString(s) {
		var head strings.Builder
		rest := s
		for {
			m := pageHeadItemRe.FindString(rest)
			if m == "" {
				break
			}
			head.WriteString(strings.TrimLeft(m, " \t\r\n"))
			head.WriteString("\n")
			rest = rest[len(m):]
		}
		s = `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
			`<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">` +
			"\n" + head.String() + "</head><body>" + rest + "</body></html>\n"
	}
	// The kit's usage comment mentions class="mermaid" as text; it is not a diagram.
	visible := pageCommentRe.ReplaceAllString(s, "")
	if pageMermaidUseRe.MatchString(visible) && !pageMermaidLoadRe.MatchString(visible) {
		if i := strings.LastIndex(strings.ToLower(s), "</body>"); i >= 0 {
			s = s[:i] + pageMermaidLoader + s[i:]
		} else {
			s += pageMermaidLoader
		}
	}
	return s
}

// pageHTMLMax mirrors PAGE_HTML_MAX in the Dispatcher run-tools contract: the
// most characters (UTF-16 code units, as zod counts them) one page may hold.
const pageHTMLMax = 900_000

// pageSizeError returns an error naming the limit when s is over it.
func pageSizeError(s string) error {
	n := 0
	for _, r := range s {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	if n <= pageHTMLMax {
		return nil
	}
	return fmt.Errorf("page publish: page is %d characters; a Dispatcher page holds at most %d. Re-export images as smaller JPEG or WebP, or drop some.", n, pageHTMLMax)
}

// pageImageMax is the largest image file `page image` embeds, in bytes.
const pageImageMax = 300_000

// pageImageKong implements `ateam page image`.
type pageImageKong struct {
	File    string `arg:"" name:"file" help:"The image file (png, jpeg, gif, webp, or svg)."`
	Alt     string `name:"alt" help:"Alt text describing the image (required)."`
	Caption string `name:"caption" help:"Optional figure caption."`
}

// Run prints a <figure> snippet that embeds the image as a data: URI.
func (c *pageImageKong) Run(ctx *cli.Context) error {
	if strings.TrimSpace(c.Alt) == "" {
		return fmt.Errorf("page image: --alt is required")
	}
	b, err := os.ReadFile(c.File)
	if err != nil {
		return fmt.Errorf("page image: %v", err)
	}
	if len(b) > pageImageMax {
		return fmt.Errorf("page image: %s is %d bytes; the limit is %d. Downscale it or convert it to JPEG or WebP.", c.File, len(b), pageImageMax)
	}
	mime := http.DetectContentType(b)
	if strings.EqualFold(filepath.Ext(c.File), ".svg") {
		mime = "image/svg+xml"
	}
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/svg+xml":
	default:
		return fmt.Errorf("page image: %s looks like %s; use png, jpeg, gif, webp, or svg", c.File, mime)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, `<figure><img src="data:%s;base64,%s" alt="%s">`, mime, base64.StdEncoding.EncodeToString(b), html.EscapeString(c.Alt))
	if c.Caption != "" {
		fmt.Fprintf(&sb, "<figcaption>%s</figcaption>", html.EscapeString(c.Caption))
	}
	sb.WriteString("</figure>\n")
	_, err = fmt.Fprint(ctx.Stdout, sb.String())
	return err
}

// pageIDFromURL returns the last path segment of a page URL, or ref itself
// when it is a bare id.
func pageIDFromURL(ref string) string {
	ref = strings.TrimSpace(ref)
	if i := strings.IndexAny(ref, "?#"); i >= 0 {
		ref = ref[:i]
	}
	ref = strings.TrimRight(ref, "/")
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		ref = ref[i+1:]
	}
	return ref
}

// Run posts the file through the run's Dispatcher and prints the page URL on
// stdout. Outside a Dispatcher run it exits 5.
func (c *pagePublishKong) Run(ctx *cli.Context) error {
	b, err := os.ReadFile(c.File)
	if err != nil {
		return fmt.Errorf("page publish: %v", err)
	}
	page := normalizePageHTML(string(b))
	sizeErr := pageSizeError(page)
	if c.DryRun {
		if _, err := fmt.Fprint(ctx.Stdout, page); err != nil {
			return err
		}
		if sizeErr != nil {
			fmt.Fprintln(ctx.Stderr, "warning: "+sizeErr.Error())
		}
		return nil
	}
	client, ok := runtools.FromEnv()
	if !ok {
		fmt.Fprintln(ctx.Stderr, pageNotInRunMsg)
		return cli.Silent(pageExitNotInRun)
	}
	if sizeErr != nil {
		return sizeErr
	}
	do := c.httpDo
	if do == nil {
		do = http.DefaultClient.Do
	}
	req := pagePublishReq{Op: "page_publish", HTML: page, Title: c.Title}
	if c.URL != "" {
		if req.Page = pageIDFromURL(c.URL); req.Page == "" {
			return fmt.Errorf("page publish: --url %q has no page id", c.URL)
		}
	}
	// A create retried after a lost response would make a second page, so only
	// a republish, which is idempotent in effect, retries.
	retries := 0
	if req.Page != "" {
		retries = 1
	}
	data, err := client.Call(do, "page_publish", req, retries)
	if err != nil {
		return fmt.Errorf("page publish: %w", err)
	}
	var r struct {
		URL     string `json:"url"`
		Version int    `json:"version"`
		Created bool   `json:"created"`
	}
	if err := json.Unmarshal(data, &r); err != nil || r.URL == "" {
		return fmt.Errorf("page publish: run-tools page_publish: unexpected response")
	}
	fmt.Fprintln(ctx.Stdout, r.URL)
	if r.Created {
		fmt.Fprintf(ctx.Stderr, "page publish: created v%d\n", r.Version)
	} else {
		fmt.Fprintf(ctx.Stderr, "page publish: updated v%d\n", r.Version)
	}
	return nil
}
