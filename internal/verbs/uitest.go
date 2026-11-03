package verbs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/workspaceconfig"
)

// Exit codes of `ateam ui-test`. The numbers are frozen by the contract bead
// agent-teams-fwhv.4; callers branch on them.
const (
	uiExitFailed     = 2 // bead closed without a verdict, or closed failed
	uiExitPending    = 3 // timeout reached, bead still open
	uiExitQuestion   = 4 // bead blocked on a question: comment
	uiExitIneligible = 5 // feature off, non-GitHub origin, or Dispatcher rejected the repo
	uiExitNotPushed  = 6 // HEAD is not on origin
	uiExitNoImages   = 7 // verdict exists but screenshots are missing
)

const (
	uiNotConfiguredMsg = "Dispatcher not configured; test locally"
	uiBeadURLPrefix    = "https://dispatcher.mgtinsurance.ai/beads/"
	uiPollInterval     = 30 * time.Second
	uiDefaultTimeout   = 9 * time.Minute
	uiMaxTimeout       = 9*time.Minute + 30*time.Second
	// uiHeaderEnd separates the routing header from the items in the bead
	// description, as in the Dispatcher's own ui-test beads.
	uiHeaderEnd = "---"
)

// RegisterUITestKong registers the `ui-test` parent verb onto p.
func RegisterUITestKong(p *cli.Parser) {
	p.AddVerb("ui-test", "Optional: file a Dispatcher ui-test bead and wait for its verdict.", &uiTestCmd{})
}

// uiTestCmd is the kong parent struct for `ateam ui-test <subcommand>`. It has
// no Run method: kong would run it for every subcommand (see mail_register.go).
type uiTestCmd struct {
	File uiTestFileKong `cmd:"" name:"file" help:"File a ui-test bead for a pushed branch in the Dispatcher's beads database; prints the bead id."`
	Wait uiTestWaitKong `cmd:"" name:"wait" help:"Wait up to --timeout for the bead's verdict, then write it (and screenshots) under --out."`
}

// uiTestDeps are the seams that keep `ui-test` off the network and the clock
// in tests. A nil field takes the production default.
type uiTestDeps struct {
	// git runs `git -C dir args...`, returning stdout and the exit status. err
	// is set only when git could not run at all.
	git func(dir string, args ...string) (stdout string, code int, err error)
	// fileBead runs `bd create` against the Dispatcher database and returns the id.
	fileBead func(dir, actor, title string, labels []string, body string) (string, error)
	// bdRead runs `bd -C dir args...` and returns stdout only; stderr is kept
	// out of the JSON and surfaces in err.
	bdRead func(dir string, args ...string) ([]byte, error)
	now    func() time.Time
	sleep  func(time.Duration)
	// httpDo performs one screenshot GET.
	httpDo func(*http.Request) (*http.Response, error)
}

func (d *uiTestDeps) resolved() uiTestDeps {
	var r uiTestDeps
	if d != nil {
		r = *d
	}
	if r.git == nil {
		r.git = defaultUITestGit
	}
	if r.fileBead == nil {
		r.fileBead = defaultUITestFileBead
	}
	if r.bdRead == nil {
		r.bdRead = defaultUITestBDRead
	}
	if r.now == nil {
		r.now = time.Now
	}
	if r.sleep == nil {
		r.sleep = time.Sleep
	}
	if r.httpDo == nil {
		r.httpDo = defaultUITestHTTP
	}
	return r
}

// uiTestSettings is the resolved Dispatcher configuration.
type uiTestSettings struct {
	beadsDir, actor string
}

// resolveUITestSettings reads dispatcher_beads and the dispatcher actor (env
// ATEAM_DISPATCHER_ACTOR beats the dispatcher_actor key). An unset or empty
// dispatcher_beads is "not configured" (ok=false, nil error): the feature is
// an optional add-on, so that is never a failure. A set dispatcher_beads with
// no actor is an error naming the key.
func resolveUITestSettings(home string) (s uiTestSettings, ok bool, err error) {
	beads, set, err := workspaceconfig.DispatcherBeads(home)
	if err != nil {
		return s, false, fmt.Errorf("ui-test: %w", err)
	}
	s.beadsDir = strings.TrimSpace(beads)
	if !set || s.beadsDir == "" {
		return s, false, nil
	}
	if s.actor = strings.TrimSpace(os.Getenv(dispatcherActorEnv)); s.actor == "" {
		actor, set, err := workspaceconfig.DispatcherActor(home)
		if err != nil {
			return s, false, fmt.Errorf("ui-test: %w", err)
		}
		if s.actor = strings.TrimSpace(actor); !set || s.actor == "" {
			return s, false, fmt.Errorf("ui-test: dispatcher_beads is set but dispatcher_actor is not set in %s/%s (or set $%s)",
				home, workspaceconfig.FileName, dispatcherActorEnv)
		}
	}
	if strings.HasPrefix(s.actor, "agent:") {
		return s, false, fmt.Errorf("ui-test: dispatcher actor %q is an agent actor, which the Dispatcher never autostarts; use your own account email", s.actor)
	}
	return s, true, nil
}

// uiTestGate runs the OPTIONAL ADD-ON RULE: it returns the settings, or a
// *cli.SilentError with exit 5 after printing the opt-out message.
func uiTestGate(ctx *cli.Context) (uiTestSettings, error) {
	s, ok, err := resolveUITestSettings(ctx.Home)
	if err != nil {
		return s, err
	}
	if !ok {
		fmt.Fprintln(ctx.Stderr, uiNotConfiguredMsg)
		return s, cli.Silent(uiExitIneligible)
	}
	return s, nil
}

var (
	githubHTTPSRE = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+?)(?:\.git)?/?$`)
	githubSSHRE   = regexp.MustCompile(`^git@github\.com:([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+?)(?:\.git)?/?$`)
)

// normalizeGitHubOrigin returns the https://github.com/<org>/<repo>.git form
// of a GitHub origin URL (https or scp-style ssh) and the repo name. ok is
// false for any other origin.
func normalizeGitHubOrigin(origin string) (url, repo string, ok bool) {
	origin = strings.TrimSpace(origin)
	m := githubHTTPSRE.FindStringSubmatch(origin)
	if m == nil {
		m = githubSSHRE.FindStringSubmatch(origin)
	}
	if m == nil {
		return "", "", false
	}
	return "https://github.com/" + m[1] + "/" + m[2] + ".git", m[2], true
}

// uiTestBeadBody composes the bead description: the routing header the
// Dispatcher parses, a "---" line, then the items verbatim.
func uiTestBeadBody(repoURL, branch, sha, scope, testUser, items string) string {
	lines := []string{
		"repo: " + repoURL,
		"branch: " + branch,
		"sha: " + sha,
		"type: ui-test",
		"agent: tester",
		"fix: none",
		"scope: " + scope,
	}
	if testUser != "" {
		lines = append(lines, "test_user: "+testUser)
	}
	lines = append(lines, uiHeaderEnd)
	return strings.Join(lines, "\n") + "\n" + items
}

// uiTestFileKong implements `ateam ui-test file`.
type uiTestFileKong struct {
	Worktree  string `name:"worktree" required:"" help:"Absolute path of the worktree whose pushed HEAD is tested."`
	Scope     string `name:"scope" required:"" help:"Apps in scope, written to the bead's scope: line."`
	ItemsFile string `name:"items-file" required:"" help:"File holding the items to verify, appended verbatim to the bead body."`
	TestUser  string `name:"test-user" help:"Email of the test user, written to the bead's test_user: line."`
	Title     string `name:"title" help:"Bead title suffix; defaults to the branch name."`

	deps *uiTestDeps
}

// Run files the bead and prints its id. Check order: config (exit 5, or 1 for
// a missing actor), origin form (exit 5), pushed (exit 6), then file.
func (c *uiTestFileKong) Run(ctx *cli.Context) error {
	d := c.deps.resolved()
	settings, err := uiTestGate(ctx)
	if err != nil {
		return err
	}

	originOut, code, err := d.git(c.Worktree, "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("ui-test file: git: %w", err)
	}
	repoURL, repo, ok := normalizeGitHubOrigin(originOut)
	if code != 0 || !ok {
		fmt.Fprintln(ctx.Stderr, "ui-test file: origin is not a GitHub repository; test locally")
		return cli.Silent(uiExitIneligible)
	}

	branch, sha, err := uiTestHead(d, c.Worktree)
	if err != nil {
		return err
	}
	if _, code, err := d.git(c.Worktree, "fetch", "origin", branch); err != nil {
		return fmt.Errorf("ui-test file: git fetch: %w", err)
	} else if code != 0 {
		fmt.Fprintf(ctx.Stderr, "ui-test file: branch %s is not on origin; push it first (git push -u origin %s)\n", branch, branch)
		return cli.Silent(uiExitNotPushed)
	}
	_, code, err = d.git(c.Worktree, "merge-base", "--is-ancestor", sha, "origin/"+branch)
	if err != nil {
		return fmt.Errorf("ui-test file: git merge-base: %w", err)
	}
	if code != 0 {
		fmt.Fprintf(ctx.Stderr, "ui-test file: HEAD %s is not on origin/%s; push it first (git push origin %s)\n", sha, branch, branch)
		return cli.Silent(uiExitNotPushed)
	}

	items, err := os.ReadFile(c.ItemsFile)
	if err != nil {
		return cli.Usagef("ui-test file: --items-file %q: %v", c.ItemsFile, err)
	}
	title := c.Title
	if title == "" {
		title = branch
	}
	id, err := d.fileBead(settings.beadsDir, settings.actor, "ui-test: "+title,
		[]string{"type:ui-test", "agent:tester", "repo:" + repo},
		uiTestBeadBody(repoURL, branch, sha, c.Scope, c.TestUser, string(items)))
	if err != nil {
		return fmt.Errorf("ui-test file: %w", err)
	}
	fmt.Fprintln(ctx.Stdout, id)
	return nil
}

// uiTestHead returns the current branch and full HEAD sha of worktree.
func uiTestHead(d uiTestDeps, worktree string) (branch, sha string, err error) {
	out, code, err := d.git(worktree, "branch", "--show-current")
	if err != nil || code != 0 || strings.TrimSpace(out) == "" {
		return "", "", fmt.Errorf("ui-test file: %s is not on a branch", worktree)
	}
	branch = strings.TrimSpace(out)
	out, code, err = d.git(worktree, "rev-parse", "HEAD")
	if err != nil || code != 0 {
		return "", "", fmt.Errorf("ui-test file: cannot resolve HEAD in %s", worktree)
	}
	return branch, strings.TrimSpace(out), nil
}

func defaultUITestGit(dir string, args ...string) (string, int, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.String(), exitErr.ExitCode(), nil
	}
	return stdout.String(), 0, err
}

// uiTestBDEnv is the environment every Dispatcher bd call runs with. It never
// sets or reads BEADS_DOLT_PASSWORD: bd reads ~/.config/beads/credentials.
func uiTestBDEnv() []string {
	return append(os.Environ(), "BEADS_DOLT_SERVER_TLS=1", "GODEBUG=netdns=cgo")
}

// defaultUITestFileBead runs
//
//	bd -C <dir> create --title <title> --type task --priority 1
//	  --assignee agent:tester --labels <labels> --actor <actor>
//	  --body-file <tmp> --silent
func defaultUITestFileBead(dir, actor, title string, labels []string, body string) (string, error) {
	f, err := os.CreateTemp("", "ateam-ui-test-*.txt")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return "", fmt.Errorf("write temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}
	cmd := exec.Command("bd", "-C", dir, "create",
		"--title", title,
		"--type", "task",
		"--priority", "1",
		"--assignee", "agent:tester",
		"--labels", strings.Join(labels, ","),
		"--actor", actor,
		"--body-file", f.Name(),
		"--silent")
	cmd.Env = uiTestBDEnv()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("bd create: %w: %s", err, msg)
		}
		return "", fmt.Errorf("bd create: %w", err)
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", fmt.Errorf("bd create returned no id")
	}
	return id, nil
}

func defaultUITestBDRead(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("bd", append([]string{"-C", dir}, args...)...)
	cmd.Env = uiTestBDEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

// uiTestBead is the subset of `bd show --json` that wait reads.
type uiTestBead struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"`
	CloseReason string `json:"close_reason"`
}

// uiTestComment is the subset of `bd comments --json` that wait reads.
type uiTestComment struct {
	Author    string `json:"author"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

var transientBDRE = regexp.MustCompile(`(?i)i/o timeout|timed out|timeout|no such host|temporary failure in name resolution|dns`)

// uiTestRead runs one bd read, retrying once on a DNS or i/o timeout, and
// returns its stdout.
func uiTestRead(d uiTestDeps, dir string, args ...string) ([]byte, error) {
	out, err := d.bdRead(dir, args...)
	if err != nil && transientBDRE.MatchString(err.Error()) {
		out, err = d.bdRead(dir, args...)
	}
	if err != nil {
		return nil, fmt.Errorf("bd %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// uiTestFetchBead reads the bead; `bd show --json` prints an array of one.
func uiTestFetchBead(d uiTestDeps, dir, id string) (uiTestBead, error) {
	out, err := uiTestRead(d, dir, "show", id, "--json")
	if err != nil {
		return uiTestBead{}, err
	}
	var beads []uiTestBead
	if err := json.Unmarshal(out, &beads); err != nil {
		return uiTestBead{}, fmt.Errorf("bd show %s: decode: %w", id, err)
	}
	if len(beads) == 0 {
		return uiTestBead{}, fmt.Errorf("bd show %s: no such bead", id)
	}
	return beads[0], nil
}

func uiTestFetchComments(d uiTestDeps, dir, id string) ([]uiTestComment, error) {
	out, err := uiTestRead(d, dir, "comments", id, "--json")
	if err != nil {
		return nil, err
	}
	var comments []uiTestComment
	if err := json.Unmarshal(out, &comments); err != nil {
		return nil, fmt.Errorf("bd comments %s: decode: %w", id, err)
	}
	sort.SliceStable(comments, func(i, j int) bool { return comments[i].CreatedAt < comments[j].CreatedAt })
	return comments, nil
}

var uiRunLineRE = regexp.MustCompile(`^run:\s*(\S+)`)

// uiTestVerdict returns the newest agent:tester comment whose first line is
// "run: <runId>", and that run id.
func uiTestVerdict(comments []uiTestComment) (c uiTestComment, runID string, ok bool) {
	for i := len(comments) - 1; i >= 0; i-- {
		if comments[i].Author != "agent:tester" {
			continue
		}
		first, _, _ := strings.Cut(comments[i].Text, "\n")
		if m := uiRunLineRE.FindStringSubmatch(strings.TrimSpace(first)); m != nil {
			return comments[i], m[1], true
		}
	}
	return c, "", false
}

var ineligibleReasonRE = regexp.MustCompile(`(?i)no such job|job not found|catalog|unknown (job|type)`)

// uiNoCatalogMarker is the Dispatcher log line for a repo with no ui-test job.
const uiNoCatalogMarker = "resolve failed: job not found"

// uiTestIneligible reports whether the repo has no ui-test catalog in the
// Dispatcher: the newest job:ui-test comment shows a failed job resolve, or the
// newest comment starting rejected: or not started: matches the regex.
func uiTestIneligible(comments []uiTestComment) (reason string, ok bool) {
	for i := len(comments) - 1; i >= 0; i-- {
		if comments[i].Author == "job:ui-test" {
			if strings.Contains(comments[i].Text, uiNoCatalogMarker) {
				return comments[i].Text, true
			}
			break
		}
	}
	for i := len(comments) - 1; i >= 0; i-- {
		text := strings.TrimSpace(comments[i].Text)
		switch {
		case strings.HasPrefix(text, "rejected:"):
			return text, true
		case strings.HasPrefix(text, "not started:"):
			return text, ineligibleReasonRE.MatchString(text)
		}
	}
	return "", false
}

// uiTestHeaderField returns the value of key in the bead description's header
// (the lines before the first "---").
func uiTestHeaderField(description, key string) string {
	for _, line := range strings.Split(description, "\n") {
		line = strings.TrimSpace(line)
		if line == uiHeaderEnd {
			return ""
		}
		if v, ok := strings.CutPrefix(line, key+":"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// uiTestWaitKong implements `ateam ui-test wait`.
type uiTestWaitKong struct {
	ID      string        `arg:"" name:"bead-id" help:"The ui-test bead id printed by 'ui-test file'."`
	Out     string        `name:"out" required:"" help:"Directory that receives verdict.md (and failure.md or screenshots/)."`
	Timeout time.Duration `name:"timeout" default:"9m" help:"How long to wait for the verdict (max 9m30s). Exit 3 when it elapses."`

	deps *uiTestDeps
}

// Validate rejects an out-of-range --timeout before any bd call.
func (c *uiTestWaitKong) Validate() error {
	if c.Timeout <= 0 || c.Timeout > uiMaxTimeout {
		return cli.Usagef("ui-test wait: --timeout must be greater than 0 and at most %s, got %s", uiMaxTimeout, c.Timeout)
	}
	return nil
}

// Run polls the bead every 30s until it has a verdict, fails, asks a
// question, or --timeout elapses.
func (c *uiTestWaitKong) Run(ctx *cli.Context) error {
	d := c.deps.resolved()
	settings, err := uiTestGate(ctx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.Out, 0o755); err != nil {
		return fmt.Errorf("ui-test wait: --out: %w", err)
	}
	out, err := filepath.Abs(c.Out)
	if err != nil {
		return fmt.Errorf("ui-test wait: --out: %w", err)
	}

	deadline := d.now().Add(c.Timeout)
	for {
		bead, err := uiTestFetchBead(d, settings.beadsDir, c.ID)
		if err != nil {
			return fmt.Errorf("ui-test wait: %w", err)
		}
		comments, err := uiTestFetchComments(d, settings.beadsDir, c.ID)
		if err != nil {
			return fmt.Errorf("ui-test wait: %w", err)
		}
		switch bead.Status {
		case "closed":
			return c.finish(ctx, d, bead, comments, out)
		case "blocked":
			if n := len(comments); n > 0 && strings.HasPrefix(strings.TrimSpace(comments[n-1].Text), "question:") {
				fmt.Fprintln(ctx.Stdout, strings.TrimSpace(comments[n-1].Text))
				return cli.Silent(uiExitQuestion)
			}
		}
		remaining := deadline.Sub(d.now())
		if remaining <= 0 {
			fmt.Fprintf(ctx.Stderr, "ui-test wait: bead %s is still %s; run the same command again\n", c.ID, bead.Status)
			return cli.Silent(uiExitPending)
		}
		d.sleep(min(uiPollInterval, remaining))
	}
}

// finish maps a closed bead to an exit code and writes the output files.
func (c *uiTestWaitKong) finish(ctx *cli.Context, d uiTestDeps, bead uiTestBead, comments []uiTestComment, out string) error {
	verdict, runID, hasVerdict := uiTestVerdict(comments)
	if hasVerdict && !strings.Contains(strings.ToLower(bead.CloseReason), "fail") {
		return c.writeVerdict(ctx, d, bead, comments, verdict, runID, out)
	}
	if reason, ok := uiTestIneligible(comments); ok {
		fmt.Fprintf(ctx.Stderr, "ui-test wait: Dispatcher cannot run this repo (%s); test locally\n", firstLine(reason))
		return cli.Silent(uiExitIneligible)
	}
	text := "bead " + c.ID + " closed without a verdict comment"
	for i := len(comments) - 1; i >= 0; i-- {
		if strings.HasPrefix(comments[i].Author, "job:") || strings.HasPrefix(strings.TrimSpace(comments[i].Text), "rejected:") {
			text = comments[i].Text
			break
		}
	}
	path := filepath.Join(out, "failure.md")
	if err := os.WriteFile(path, []byte(text+"\n"), 0o644); err != nil {
		return fmt.Errorf("ui-test wait: %w", err)
	}
	fmt.Fprintln(ctx.Stdout, path)
	return cli.Silent(uiExitFailed)
}

// writeVerdict downloads the run's screenshots, writes verdict.md, and prints
// its path followed by one PNG path per line. Exit 0 only when at least one
// screenshot was downloaded and none is missing; otherwise exit 7.
func (c *uiTestWaitKong) writeVerdict(ctx *cli.Context, d uiTestDeps, bead uiTestBead, comments []uiTestComment, verdict uiTestComment, runID, out string) error {
	shots := uiDownloadShots(d, comments, runID, out)
	if shots.Err != nil {
		return fmt.Errorf("ui-test wait: screenshots: %w", shots.Err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "bead: %s\nurl: %s%s\nrun: %s\nbranch: %s\nsha: %s\n",
		bead.ID, uiBeadURLPrefix, bead.ID, runID,
		uiTestHeaderField(bead.Description, "branch"), uiTestHeaderField(bead.Description, "sha"))
	b.WriteString("screenshots:\n")
	for _, p := range shots.Paths {
		b.WriteString(p + "\n")
	}
	b.WriteString("missing:\n")
	for _, m := range shots.Missing {
		b.WriteString(m + "\n")
	}
	b.WriteString("\n" + verdict.Text)
	if !strings.HasSuffix(verdict.Text, "\n") {
		b.WriteString("\n")
	}
	path := filepath.Join(out, "verdict.md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("ui-test wait: %w", err)
	}
	fmt.Fprintln(ctx.Stdout, path)
	for _, p := range shots.Paths {
		fmt.Fprintln(ctx.Stdout, p)
	}
	if len(shots.Missing) > 0 || len(shots.Paths) == 0 {
		return cli.Silent(uiExitNoImages)
	}
	return nil
}

func firstLine(s string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return first
}
