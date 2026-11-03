package verbs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

const uiTestSHA = "0123456789abcdef0123456789abcdef01234567"

// uiHarness wires ui-test to fakes and records every seam call.
type uiHarness struct {
	t      *testing.T
	ctx    *cli.Context
	stdout fmtBuf
	stderr fmtBuf
	deps   *uiTestDeps

	gitCalls  [][]string
	bdReads   [][]string
	filed     []uiFiled
	slept     []time.Duration
	clock     time.Time
	origin    string
	fetchCode int
	ancestor  int

	// polls is the sequence of bead states served by `bd show`; the last one
	// repeats. comments is served by `bd comments`.
	polls    []uiTestBead
	comments []uiTestComment
}

type uiFiled struct {
	dir, actor, title string
	labels            []string
	body              string
}

func newUIHarness(t *testing.T, config string) *uiHarness {
	t.Helper()
	t.Setenv(dispatcherActorEnv, "")
	home := t.TempDir()
	if config != "" {
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h := &uiHarness{t: t, origin: "git@github.com:acme/widgets.git", clock: time.Unix(1_800_000_000, 0)}
	ctx, stdout, stderr := makeCtx(&fakeBD{}, home)
	h.ctx, h.stdout, h.stderr = ctx, stdout, stderr
	h.deps = &uiTestDeps{
		git: func(_ string, args ...string) (string, int, error) {
			h.gitCalls = append(h.gitCalls, args)
			switch args[0] {
			case "remote":
				return h.origin + "\n", 0, nil
			case "branch":
				return "feat-x\n", 0, nil
			case "rev-parse":
				return uiTestSHA + "\n", 0, nil
			case "fetch":
				return "", h.fetchCode, nil
			case "merge-base":
				return "", h.ancestor, nil
			}
			t.Errorf("unexpected git call %v", args)
			return "", 0, nil
		},
		fileBead: func(dir, actor, title string, labels []string, body string) (string, error) {
			h.filed = append(h.filed, uiFiled{dir, actor, title, labels, body})
			return "dsp-ui1", nil
		},
		bdRead: func(_ string, args ...string) ([]byte, error) {
			h.bdReads = append(h.bdReads, args)
			if args[0] == "show" {
				bead := h.polls[0]
				if len(h.polls) > 1 {
					h.polls = h.polls[1:]
				}
				return json.Marshal([]uiTestBead{bead})
			}
			return json.Marshal(h.comments)
		},
		now:   func() time.Time { return h.clock },
		sleep: func(d time.Duration) { h.slept = append(h.slept, d); h.clock = h.clock.Add(d) },
	}
	return h
}

const uiFullConfig = "dispatcher_beads = \"/dsp\"\ndispatcher_actor = \"eric@example.com\"\n"

func (h *uiHarness) itemsFile(content string) string {
	h.t.Helper()
	p := filepath.Join(h.t.TempDir(), "items.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
	return p
}

func (h *uiHarness) file(items string) error {
	cmd := &uiTestFileKong{Worktree: "/wt", Scope: "mithril", ItemsFile: h.itemsFile(items), deps: h.deps}
	return cmd.Run(h.ctx)
}

func (h *uiHarness) wait(timeout time.Duration) (string, error) {
	out := filepath.Join(h.t.TempDir(), "out")
	cmd := &uiTestWaitKong{ID: "dsp-ui1", Out: out, Timeout: timeout, deps: h.deps}
	return out, cmd.Run(h.ctx)
}

// Real eri-kd2w thread (abridged text): launch and pass comments from
// job:ui-test, then the agent:tester verdict.
func eriKD2WComments() []uiTestComment {
	return []uiTestComment{
		{Author: "job:ui-test", CreatedAt: "2026-11-02T13:49:40Z", Text: "attempt 1: launching ui-test on https://github.com/MGT-Insurance/midgard.git @ a596ac48"},
		{Author: "job:ui-test", CreatedAt: "2026-11-02T13:53:14Z", Text: "attempt 1: passed (exit 0)\n\n✓ build:packages (7s)\n✓ server (23s)\n✓ test (48s)"},
		{Author: "agent:tester", CreatedAt: "2026-11-02T13:59:00Z", Text: "run: cmvzbm7eh00070fl8c73fiwxa\n\n| # | Verdict | Evidence | Screenshot |\n|---|---|---|---|\n| 1 | PASS | Home rendered signed-in | 01-home.png |"},
	}
}

const eriKD2WDescription = "repo: https://github.com/MGT-Insurance/midgard.git\nbranch: main\nsha: a596ac48d7db3de795ecda54184bde858c44eac5\ntype: ui-test\nagent: tester\nfix: none\nscope: mithril\n---\nTiming probe.\n"

func closedBead(reason string) uiTestBead {
	return uiTestBead{ID: "dsp-ui1", Description: eriKD2WDescription, Status: "closed", CloseReason: reason}
}

func exitOf(err error) int { return cli.ExitCode(err) }

func TestUITest_NotConfiguredExits5WithoutTouchingAnything(t *testing.T) {
	// An explicit empty dispatcher_beads never reaches ui-test: workspaceconfig
	// rejects it while reading the file ("must not be empty").
	for name, config := range map[string]string{
		"no config file":       "",
		"config without a key": "cloud_home = \"/hosted\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			h := newUIHarness(t, config)
			for verb, run := range map[string]func() error{
				"file": func() error { return h.file("- check it\n") },
				"wait": func() error { _, err := h.wait(time.Minute); return err },
			} {
				err := run()
				if exitOf(err) != 5 {
					t.Errorf("%s: exit = %d, want 5 (err %v)", verb, exitOf(err), err)
				}
			}
			if !strings.Contains(h.stderr.String(), "Dispatcher not configured; test locally") {
				t.Errorf("stderr = %q", h.stderr.String())
			}
			if len(h.gitCalls)+len(h.bdReads)+len(h.filed) != 0 {
				t.Errorf("seams called: git=%v bd=%v filed=%v", h.gitCalls, h.bdReads, h.filed)
			}
		})
	}
}

func TestUITest_MissingActorExits1NamingKey(t *testing.T) {
	h := newUIHarness(t, "dispatcher_beads = \"/dsp\"\n")
	for _, err := range []error{h.file("x"), func() error { _, e := h.wait(time.Minute); return e }()} {
		if exitOf(err) != 1 || err == nil || !strings.Contains(err.Error(), "dispatcher_actor") {
			t.Errorf("err = %v (exit %d), want exit 1 naming dispatcher_actor", err, exitOf(err))
		}
	}
	if len(h.gitCalls)+len(h.filed) != 0 {
		t.Errorf("seams called before the actor check")
	}
}

func TestUITest_ActorEnvBeatsConfig(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	t.Setenv(dispatcherActorEnv, "env@example.com")
	if err := h.file("- item\n"); err != nil {
		t.Fatal(err)
	}
	if got := h.filed[0].actor; got != "env@example.com" {
		t.Errorf("actor = %q", got)
	}
}

func TestUITest_NormalizeGitHubOrigin(t *testing.T) {
	for in, wantRepo := range map[string]string{
		"git@github.com:acme/widgets.git":      "widgets",
		"https://github.com/acme/widgets":      "widgets",
		"https://github.com/acme/widgets.git":  "widgets",
		"https://github.com/acme/widgets.git/": "widgets",
	} {
		url, repo, ok := normalizeGitHubOrigin(in)
		if !ok || url != "https://github.com/acme/widgets.git" || repo != wantRepo {
			t.Errorf("normalizeGitHubOrigin(%q) = %q, %q, %v", in, url, repo, ok)
		}
	}
	for _, bad := range []string{"https://gitlab.com/acme/widgets.git", "git@bitbucket.org:acme/widgets.git", "/local/path", ""} {
		if _, _, ok := normalizeGitHubOrigin(bad); ok {
			t.Errorf("normalizeGitHubOrigin(%q) accepted a non-GitHub origin", bad)
		}
	}
}

func TestUITest_FileNonGitHubOriginExits5AndFilesNothing(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.origin = "https://gitlab.com/acme/widgets.git"
	if err := h.file("- item\n"); exitOf(err) != 5 {
		t.Fatalf("exit = %d, want 5 (%v)", exitOf(err), err)
	}
	if len(h.filed) != 0 {
		t.Error("filed a bead for a non-GitHub origin")
	}
	for _, c := range h.gitCalls {
		if c[0] == "fetch" {
			t.Error("fetched before the origin form check")
		}
	}
}

func TestUITest_FileNotPushedExits6AndFilesNothing(t *testing.T) {
	for name, set := range map[string]func(*uiHarness){
		"fetch fails":       func(h *uiHarness) { h.fetchCode = 128 },
		"head not ancestor": func(h *uiHarness) { h.ancestor = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			h := newUIHarness(t, uiFullConfig)
			set(h)
			if err := h.file("- item\n"); exitOf(err) != 6 {
				t.Fatalf("exit = %d, want 6 (%v)", exitOf(err), err)
			}
			if len(h.filed) != 0 {
				t.Error("filed a bead for an unpushed HEAD")
			}
		})
	}
}

func TestUITest_FileSuccessFilesBeadAndPrintsID(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	if err := h.file("- verify home\n"); err != nil {
		t.Fatal(err)
	}
	if got := h.stdout.String(); got != "dsp-ui1\n" {
		t.Errorf("stdout = %q, want the bead id only", got)
	}
	f := h.filed[0]
	if f.dir != "/dsp" || f.actor != "eric@example.com" || f.title != "ui-test: feat-x" {
		t.Errorf("filed = %+v", f)
	}
	if strings.Join(f.labels, ",") != "type:ui-test,agent:tester,repo:widgets" {
		t.Errorf("labels = %v", f.labels)
	}
	// Order matters: merge-base runs against the fetched ref.
	var sawFetch, sawBase bool
	for _, c := range h.gitCalls {
		switch c[0] {
		case "fetch":
			sawFetch = strings.Join(c, " ") == "fetch origin feat-x"
		case "merge-base":
			sawBase = sawFetch && strings.Join(c, " ") == "merge-base --is-ancestor "+uiTestSHA+" origin/feat-x"
		}
	}
	if !sawBase {
		t.Errorf("git calls = %v", h.gitCalls)
	}
}

func TestUITest_BeadBodyGolden(t *testing.T) {
	const items = "What to verify:\n1. Home loads. Screenshot it.\n"
	want := "repo: https://github.com/acme/widgets.git\nbranch: feat-x\nsha: " + uiTestSHA +
		"\ntype: ui-test\nagent: tester\nfix: none\nscope: mithril\n---\n" + items
	if got := uiTestBeadBody("https://github.com/acme/widgets.git", "feat-x", uiTestSHA, "mithril", "", items); got != want {
		t.Errorf("without test_user:\n%s\nwant:\n%s", got, want)
	}
	want = strings.Replace(want, "scope: mithril\n", "scope: mithril\ntest_user: qa@example.com\n", 1)
	if got := uiTestBeadBody("https://github.com/acme/widgets.git", "feat-x", uiTestSHA, "mithril", "qa@example.com", items); got != want {
		t.Errorf("with test_user:\n%s\nwant:\n%s", got, want)
	}
}

func TestUITest_WaitVerdictExit0WritesVerdictMD(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.polls = []uiTestBead{closedBead("done")}
	h.comments = eriKD2WComments()
	out, err := h.wait(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	verdictPath := filepath.Join(out, "verdict.md")
	if got := h.stdout.String(); got != verdictPath+"\n" {
		t.Errorf("stdout = %q", got)
	}
	data, err := os.ReadFile(verdictPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"bead: dsp-ui1\n",
		"url: https://dispatcher.mgtinsurance.ai/beads/dsp-ui1\n",
		"run: cmvzbm7eh00070fl8c73fiwxa\n",
		"branch: main\n",
		"sha: a596ac48d7db3de795ecda54184bde858c44eac5\n",
		"screenshots:\n",
		"missing:\n",
		"\n| 1 | PASS | Home rendered signed-in | 01-home.png |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("verdict.md missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "attempt 1") {
		t.Error("verdict.md includes a job:ui-test comment")
	}
}

func TestUITest_WaitVerdictIsNewestTesterRun(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.polls = []uiTestBead{closedBead("done")}
	h.comments = append(eriKD2WComments(),
		uiTestComment{Author: "agent:tester", CreatedAt: "2026-11-02T14:05:00Z", Text: "run: second-run\n\nsecond verdict"})
	out, err := h.wait(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(out, "verdict.md"))
	if !strings.Contains(string(data), "run: second-run\n") || strings.Contains(string(data), "cmvzbm7eh") {
		t.Errorf("verdict.md did not use the newest run:\n%s", data)
	}
}

func TestUITest_WaitIneligibleExit5(t *testing.T) {
	for name, text := range map[string]string{
		"rejected: unknown type":   "rejected: unknown type \"ui-tset\"",
		"not started: no such job": "not started: no such job",
	} {
		t.Run(name, func(t *testing.T) {
			h := newUIHarness(t, uiFullConfig)
			h.polls = []uiTestBead{closedBead("failed")}
			h.comments = []uiTestComment{{Author: "job:ui-test", CreatedAt: "2026-11-02T13:49:40Z", Text: text}}
			if _, err := h.wait(time.Minute); exitOf(err) != 5 {
				t.Errorf("exit = %d, want 5 (%v)", exitOf(err), err)
			}
		})
	}
}

func TestUITest_WaitOtherNotStartedExit2WritesFailureMD(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.polls = []uiTestBead{closedBead("failed")}
	h.comments = []uiTestComment{{Author: "job:ui-test", CreatedAt: "2026-11-02T13:49:40Z", Text: "not started: budget exceeded"}}
	out, err := h.wait(time.Minute)
	if exitOf(err) != 2 {
		t.Fatalf("exit = %d, want 2 (%v)", exitOf(err), err)
	}
	data, rerr := os.ReadFile(filepath.Join(out, "failure.md"))
	if rerr != nil || !strings.Contains(string(data), "not started: budget exceeded") {
		t.Errorf("failure.md = %q (%v)", data, rerr)
	}
	if got := h.stdout.String(); got != filepath.Join(out, "failure.md")+"\n" {
		t.Errorf("stdout = %q, want the failure.md path", got)
	}
}

func TestUITest_WaitClosedWithoutVerdictExit2(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.polls = []uiTestBead{closedBead("done")}
	h.comments = eriKD2WComments()[:2]
	if _, err := h.wait(time.Minute); exitOf(err) != 2 {
		t.Errorf("exit = %d, want 2 (%v)", exitOf(err), err)
	}
}

func TestUITest_WaitQuestionExit4PrintsQuestion(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.polls = []uiTestBead{{ID: "dsp-ui1", Status: "blocked"}}
	h.comments = []uiTestComment{{Author: "agent:tester", CreatedAt: "2026-11-02T13:59:00Z", Text: "question: which test user?"}}
	_, err := h.wait(time.Minute)
	if exitOf(err) != 4 {
		t.Fatalf("exit = %d, want 4 (%v)", exitOf(err), err)
	}
	if got := h.stdout.String(); got != "question: which test user?\n" {
		t.Errorf("stdout = %q", got)
	}
}

func TestUITest_WaitPendingExit3AfterPollingEvery30s(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.polls = []uiTestBead{{ID: "dsp-ui1", Status: "in_progress"}}
	_, err := h.wait(95 * time.Second)
	if exitOf(err) != 3 {
		t.Fatalf("exit = %d, want 3 (%v)", exitOf(err), err)
	}
	want := []time.Duration{30 * time.Second, 30 * time.Second, 30 * time.Second, 5 * time.Second}
	if len(h.slept) != len(want) {
		t.Fatalf("slept = %v, want %v", h.slept, want)
	}
	for i := range want {
		if h.slept[i] != want[i] {
			t.Fatalf("slept = %v, want %v", h.slept, want)
		}
	}
}

func TestUITest_WaitPollsUntilClosed(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.polls = []uiTestBead{{ID: "dsp-ui1", Status: "open"}, {ID: "dsp-ui1", Status: "in_progress"}, closedBead("done")}
	h.comments = eriKD2WComments()
	if _, err := h.wait(5 * time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(h.slept) != 2 {
		t.Errorf("slept %v, want two poll intervals", h.slept)
	}
}

func TestUITest_WaitRetriesOneTransientReadFailure(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.polls = []uiTestBead{closedBead("done")}
	h.comments = eriKD2WComments()
	inner := h.deps.bdRead
	failed := false
	h.deps.bdRead = func(dir string, args ...string) ([]byte, error) {
		if !failed {
			failed = true
			return nil, errors.New("dial tcp: lookup dolt.example: i/o timeout")
		}
		return inner(dir, args...)
	}
	if _, err := h.wait(time.Minute); err != nil {
		t.Fatalf("transient failure was not retried: %v", err)
	}
	// A non-transient failure is not retried.
	h2 := newUIHarness(t, uiFullConfig)
	calls := 0
	h2.deps.bdRead = func(string, ...string) ([]byte, error) { calls++; return nil, errors.New("access denied") }
	if _, err := h2.wait(time.Minute); exitOf(err) != 1 || calls != 1 {
		t.Errorf("exit = %d, calls = %d, want exit 1 after one call", exitOf(err), calls)
	}
}

// A deprecation warning goes to stderr; stdout stays pure JSON. The seam
// returns stdout only, so a stderr line can never reach the decoder.
func TestUITest_DefaultBDReadKeepsStderrOutOfStdout(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'Warning: dolt_server_port is deprecated' >&2\necho '[{\"id\":\"x\",\"status\":\"open\"}]'\n"
	if err := os.WriteFile(filepath.Join(dir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	d := (&uiTestDeps{}).resolved()
	bead, err := uiTestFetchBead(d, dir, "x")
	if err != nil || bead.ID != "x" {
		t.Fatalf("bead = %+v, err = %v", bead, err)
	}
}

func TestUITest_WaitRejectsTimeoutOverMax(t *testing.T) {
	if err := (&uiTestWaitKong{Timeout: 10 * time.Minute}).Validate(); !isUsage(err) {
		t.Errorf("Validate(10m) = %v, want usage error", err)
	}
	if err := (&uiTestWaitKong{Timeout: 9*time.Minute + 30*time.Second}).Validate(); err != nil {
		t.Errorf("Validate(9m30s) = %v", err)
	}
}

func TestUITest_RegisteredWithFrozenFlags(t *testing.T) {
	parser, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterUITestKong(parser)
	kctx, err := parser.Parse([]string{"ui-test", "file", "--worktree", "/wt", "--scope", "mithril",
		"--items-file", "/i.md", "--test-user", "qa@example.com", "--title", "T"})
	if err != nil {
		t.Fatal(err)
	}
	if got := kctx.Command(); got != "ui-test file" {
		t.Errorf("command = %q", got)
	}
	kctx, err = parser.Parse([]string{"ui-test", "wait", "dsp-1", "--out", "/o", "--timeout", "2m"})
	if err != nil {
		t.Fatal(err)
	}
	if got := kctx.Command(); got != "ui-test wait <bead-id>" {
		t.Errorf("command = %q", got)
	}
}
