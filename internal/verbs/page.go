package verbs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
	p.AddVerb("page", "Cloud runs: publish an HTML page through the Dispatcher.", &pageCmd{})
}

// pageCmd is the kong parent struct for `ateam page <subcommand>`. It has no
// Run method: kong would run it for every subcommand (see mail_register.go).
type pageCmd struct {
	Publish pagePublishKong `cmd:"" name:"publish" help:"Publish an HTML file as a Dispatcher page (cloud run only); prints the page URL."`
}

// pagePublishKong implements `ateam page publish`.
type pagePublishKong struct {
	File  string `arg:"" name:"file" help:"The self-contained HTML file to publish."`
	Title string `name:"title" help:"Page title."`
	URL   string `name:"url" help:"A page URL or id from an earlier publish; republishes that page in place."`

	httpDo runtools.Do
}

type pagePublishReq struct {
	Op    string `json:"op"`
	HTML  string `json:"html"`
	Title string `json:"title,omitempty"`
	Page  string `json:"page,omitempty"`
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
	client, ok := runtools.FromEnv()
	if !ok {
		fmt.Fprintln(ctx.Stderr, pageNotInRunMsg)
		return cli.Silent(pageExitNotInRun)
	}
	b, err := os.ReadFile(c.File)
	if err != nil {
		return fmt.Errorf("page publish: %v", err)
	}
	do := c.httpDo
	if do == nil {
		do = http.DefaultClient.Do
	}
	req := pagePublishReq{Op: "page_publish", HTML: string(b), Title: c.Title}
	if c.URL != "" {
		if req.Page = pageIDFromURL(c.URL); req.Page == "" {
			return fmt.Errorf("page publish: --url %q has no page id", c.URL)
		}
	}
	data, err := client.Call(do, "page_publish", req, 1)
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
