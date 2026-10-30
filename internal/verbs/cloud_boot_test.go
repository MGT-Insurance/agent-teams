package verbs

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// cbEnv is a fake Dispatcher VM: env vars, a hosted workspace, a fake bd, and
// recording tmux/agents seams.
type cbEnv struct {
	t        *testing.T
	ctx      *cli.Context
	common   cloudBootCommon
	cwd      string
	sup      string
	pluginDr string
	desc     string
	status   string
	showN    int
	closeAt  int               // close the initiative at this show call (0 = never)
	showErr  func(n int) error // fails the nth bd show when non-nil
	updates  []string
	tmux     [][]string
	polls    []func() []agentSession
	pollN    int
}

func newCBEnv(t *testing.T) *cbEnv {
	t.Helper()
	e := &cbEnv{t: t, cwd: t.TempDir(), sup: t.TempDir(), pluginDr: t.TempDir(), status: "open"}
	if err := os.MkdirAll(filepath.Join(e.pluginDr, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.pluginDr, ".claude-plugin", "plugin.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".beads", "metadata.json"),
		[]byte(`{"dolt_mode":"server","dolt_server_host":"db.example.com"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DSP_BEAD", "dsp-run1")
	t.Setenv("DSP_SUP", e.sup)
	t.Setenv("AGENT_TEAMS_HOST_FOLLOW", "off")
	t.Setenv("DSP_META_INITIATIVE", "at-ab12")

	exec := func(name string, args ...string) ([]byte, []byte, error) {
		if len(args) >= 2 && args[0] == "-C" {
			args = args[2:]
		}
		switch args[0] {
		case "show":
			e.showN++
			if e.showErr != nil {
				if err := e.showErr(e.showN); err != nil {
					return nil, nil, err
				}
			}
			st := e.status
			if e.closeAt != 0 && e.showN >= e.closeAt {
				st = "closed"
			}
			raw, _ := json.Marshal([]bd.Issue{{ID: "at-ab12", Status: st, Description: e.desc}})
			return raw, nil, nil
		case "update":
			body, err := os.ReadFile(strings.TrimPrefix(args[2], "--body-file="))
			if err != nil {
				t.Fatal(err)
			}
			e.desc = string(body)
			e.updates = append(e.updates, e.desc)
			return nil, nil, nil
		}
		t.Fatalf("unexpected bd call %v", args)
		return nil, nil, nil
	}
	var out, errb bytes.Buffer
	e.ctx = &cli.Context{Home: home, BD: bd.NewClientWithExec(t.TempDir(), exec), Stdout: &out, Stderr: &errb}
	e.common = cloudBootCommon{
		PluginDir: e.pluginDr,
		Poll:      time.Second,
		IdlePolls: 3,
		Tmux:      func(args ...string) error { e.tmux = append(e.tmux, args); return nil },
		Sleep:     func(time.Duration) {},
		NewID:     func() string { return "sid-1" },
		LookPath:  func(f string) (string, error) { return "/bin/" + f, nil },
		Getwd:     func() (string, error) { return e.cwd, nil },
		Agents: func() ([]agentSession, error) {
			i := e.pollN
			if i >= len(e.polls) {
				i = len(e.polls) - 1
			}
			e.pollN++
			return e.polls[i](), nil
		},
	}
	return e
}

func lead(sid, status string) agentSession {
	pid := 1
	return agentSession{SessionID: sid, Status: status, PID: &pid}
}

// leadSeq returns one poll per status, all listing only the lead.
func (e *cbEnv) leadSeq(sid string, statuses ...string) {
	for _, st := range statuses {
		e.polls = append(e.polls, func() []agentSession { return []agentSession{lead(sid, st)} })
	}
}

func (e *cbEnv) start() error  { return (&cloudBootStartKong{e.common}).Run(e.ctx) }
func (e *cbEnv) resume() error { return (&cloudBootResumeKong{e.common}).Run(e.ctx) }

func (e *cbEnv) calls(sub string) int {
	n := 0
	for _, c := range e.tmux {
		if c[0] == sub {
			n++
		}
	}
	return n
}

func wantExit(t *testing.T, err error, code int) {
	t.Helper()
	var s *cli.SilentError
	if !errors.As(err, &s) || s.Code != code {
		t.Fatalf("err = %v, want exit %d", err, code)
	}
}

func TestCloudBootPreconditionsFailClosed(t *testing.T) {
	cases := map[string]func(t *testing.T, e *cbEnv){
		"outside run":        func(t *testing.T, e *cbEnv) { t.Setenv("DSP_BEAD", "") },
		"empty DSP_SUP":      func(t *testing.T, e *cbEnv) { t.Setenv("DSP_SUP", "") },
		"host follow on":     func(t *testing.T, e *cbEnv) { t.Setenv("AGENT_TEAMS_HOST_FOLLOW", "on") },
		"non-hosted home":    func(t *testing.T, e *cbEnv) { _ = os.RemoveAll(filepath.Join(e.ctx.Home, ".beads")) },
		"no meta initiative": func(t *testing.T, e *cbEnv) { t.Setenv("DSP_META_INITIATIVE", "") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := newCBEnv(t)
			mutate(t, e)
			e.leadSeq("sid-1", "idle")
			err := e.start()
			if err == nil {
				t.Fatal("want error")
			}
			if cli.ExitCode(err) == cloudBootParkExit {
				t.Fatalf("precondition failure must not park: %v", err)
			}
			if len(e.tmux) != 0 || len(e.updates) != 0 || e.showN != 0 {
				t.Errorf("side effects: tmux=%v updates=%d shows=%d", e.tmux, len(e.updates), e.showN)
			}
		})
	}
}

func TestCloudBootStartHappyPath(t *testing.T) {
	e := newCBEnv(t)
	e.desc = "repo: x\n"
	e.leadSeq("sid-1", "idle")
	e.common.IdlePolls = 1
	wantExit(t, e.start(), cloudBootParkExit)

	for _, want := range []string{"where: run:dsp-run1", "worktree: " + e.cwd, "session: sid-1"} {
		if !strings.Contains(e.desc, want) {
			t.Errorf("description missing %q:\n%s", want, e.desc)
		}
	}
	model, _, err := driAdvisorSettings(e.ctx.Home)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]string{"new-session", "-d", "-s", "ateam-dri", "-c", e.cwd,
		"-e", "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1", "--", "claude",
		"--session-id", "sid-1", "--plugin-dir", e.pluginDr, "--disallowedTools", "AskUserQuestion"},
		driSessionArgs("/dri at-ab12", driSystemPromptAppend, model, "", "dri", "at-ab12", "")...)
	if !slices.Equal(e.tmux[0], want) {
		t.Errorf("tmux argv:\n got %q\nwant %q", e.tmux[0], want)
	}
	for _, a := range e.tmux[0] {
		if strings.Contains(a, "ANTHROPIC_") {
			t.Errorf("argv mentions ANTHROPIC_*: %q", a)
		}
	}
	if last := e.tmux[0][len(e.tmux[0])-1]; last != "/dri at-ab12" {
		t.Errorf("last arg = %q, want positional prompt", last)
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".ateam-cloud-boot.json"))
	if err != nil || !strings.Contains(string(data), `"session":"sid-1"`) {
		t.Errorf("state file: %q, %v", data, err)
	}
}

func TestCloudBootParksAfterThirdConsecutiveIdle(t *testing.T) {
	e := newCBEnv(t)
	e.leadSeq("sid-1", "busy", "idle", "idle", "busy", "idle", "idle", "idle")
	wantExit(t, e.start(), cloudBootParkExit)
	if e.pollN != 7 {
		t.Errorf("polls = %d, want 7", e.pollN)
	}
	if e.calls("kill-session") != 1 {
		t.Errorf("kill-session calls = %d, want 1", e.calls("kill-session"))
	}
}

func TestCloudBootGraceDelaysPark(t *testing.T) {
	e := newCBEnv(t)
	e.common.Grace = 5 * time.Second
	e.common.IdlePolls = 1
	e.leadSeq("sid-1", "idle")
	wantExit(t, e.start(), cloudBootParkExit)
	if e.pollN != 5 {
		t.Errorf("polls = %d, want 5 (grace 5s at 1s poll)", e.pollN)
	}
}

func TestCloudBootInitiativeClosedMidLoopExitsZero(t *testing.T) {
	e := newCBEnv(t)
	e.leadSeq("sid-1", "busy")
	e.closeAt = 4 // show 1 is the precondition check; 2.. are loop polls
	if err := e.start(); err != nil {
		t.Fatalf("err = %v, want nil (exit 0)", err)
	}
	if e.calls("kill-session") != 1 {
		t.Errorf("kill-session calls = %d, want 1", e.calls("kill-session"))
	}
}

func TestCloudBootTeammateBusyUnderCwdBlocksPark(t *testing.T) {
	e := newCBEnv(t)
	e.common.IdlePolls = 2
	pid := 2
	mate := agentSession{SessionID: "mate", Status: "busy", PID: &pid, CWD: filepath.Join(e.cwd, "sub")}
	outside := agentSession{SessionID: "other", Status: "busy", PID: &pid, CWD: "/elsewhere"}
	for i := 0; i < 3; i++ {
		e.polls = append(e.polls, func() []agentSession { return []agentSession{lead("sid-1", "idle"), mate, outside} })
	}
	idleMate := mate
	idleMate.Status = "idle"
	e.polls = append(e.polls, func() []agentSession { return []agentSession{lead("sid-1", "idle"), idleMate} })
	wantExit(t, e.start(), cloudBootParkExit)
	if e.pollN != 5 {
		t.Errorf("polls = %d, want 5 (3 blocked, then 2 idle)", e.pollN)
	}
}

func TestCloudBootLeadGoneIsError(t *testing.T) {
	e := newCBEnv(t)
	e.polls = []func() []agentSession{func() []agentSession { return nil }}
	e.common.Tmux = func(args ...string) error {
		e.tmux = append(e.tmux, args)
		if args[0] == "has-session" {
			return errors.New("no session")
		}
		return nil
	}
	err := e.start()
	if err == nil || cli.ExitCode(err) == cloudBootParkExit {
		t.Fatalf("err = %v, want a non-park error", err)
	}
}

func TestCloudBootToleratesTransientPollErrors(t *testing.T) {
	e := newCBEnv(t)
	e.common.IdlePolls = 1
	e.leadSeq("sid-1", "busy", "idle")
	// show 1 is the precondition check. Three bd shows fail, one poll succeeds
	// (resetting the count), then five agent polls fail: eight errors in all,
	// never more than five in a row.
	e.showErr = func(n int) error {
		if n >= 2 && n <= 4 {
			return errors.New("bd flake")
		}
		return nil
	}
	calls := 0
	inner := e.common.Agents
	e.common.Agents = func() ([]agentSession, error) {
		calls++
		if calls >= 2 && calls <= 6 {
			return nil, errors.New("agents flake")
		}
		return inner()
	}
	wantExit(t, e.start(), cloudBootParkExit)
	if e.calls("kill-session") != 1 {
		t.Errorf("kill-session calls = %d, want 1 (park only)", e.calls("kill-session"))
	}
}

func TestCloudBootGivesUpAfterConsecutivePollErrorsAndKillsSession(t *testing.T) {
	for name, fail := range map[string]func(e *cbEnv){
		"bd show": func(e *cbEnv) { e.showErr = func(n int) error { return errNilIf(n == 1, errors.New("bd down")) } },
		"agents": func(e *cbEnv) {
			e.common.Agents = func() ([]agentSession, error) { return nil, errors.New("agents down") }
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newCBEnv(t)
			e.leadSeq("sid-1", "busy")
			fail(e)
			err := e.start()
			if err == nil || cli.ExitCode(err) == cloudBootParkExit {
				t.Fatalf("err = %v, want a non-park error", err)
			}
			if e.calls("kill-session") != 1 {
				t.Errorf("kill-session calls = %d, want 1", e.calls("kill-session"))
			}
			if !strings.Contains(err.Error(), "6 consecutive poll errors") {
				t.Errorf("err = %v, want 6 consecutive poll errors", err)
			}
		})
	}
}

// errNilIf returns nil when skip is true, else err.
func errNilIf(skip bool, err error) error {
	if skip {
		return nil
	}
	return err
}

func TestCloudBootLeadGoneKillsSession(t *testing.T) {
	e := newCBEnv(t)
	e.polls = []func() []agentSession{func() []agentSession { return nil }}
	e.common.Tmux = func(args ...string) error {
		e.tmux = append(e.tmux, args)
		if args[0] == "has-session" {
			return errors.New("no session")
		}
		return nil
	}
	if err := e.start(); err == nil {
		t.Fatal("want error")
	}
	if e.calls("kill-session") != 1 {
		t.Errorf("kill-session calls = %d, want 1", e.calls("kill-session"))
	}
}

func TestCloudBootResumeRejectsStateFromAnotherRun(t *testing.T) {
	e := newCBEnv(t)
	e.desc = "repo: x\n"
	state, _ := json.Marshal(cloudBootState{Initiative: "at-ab12", Session: "sid-9", Bead: "dsp-other"})
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".ateam-cloud-boot.json"), state, 0o600); err != nil {
		t.Fatal(err)
	}
	e.leadSeq("sid-9", "idle")
	err := e.resume()
	if err == nil || !strings.Contains(err.Error(), "dsp-other") || cli.ExitCode(err) == cloudBootParkExit {
		t.Fatalf("err = %v, want a mismatch error naming dsp-other", err)
	}
	if len(e.tmux) != 0 || len(e.updates) != 0 {
		t.Errorf("side effects: tmux=%v updates=%d", e.tmux, len(e.updates))
	}
}

func TestCloudBootResumeWithWakeAndState(t *testing.T) {
	e := newCBEnv(t)
	e.desc = "repo: x\nwhere: laptop\n"
	state, _ := json.Marshal(cloudBootState{Initiative: "at-ab12", Session: "sid-9", Bead: "dsp-run1"})
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".ateam-cloud-boot.json"), state, 0o600); err != nil {
		t.Fatal(err)
	}
	wake := `{"op":"comment","author":"eric@example.com","text":"please rebase\nthanks","seq":42}`
	if err := os.WriteFile(filepath.Join(e.sup, "wake.json"), []byte(wake), 0o644); err != nil {
		t.Fatal(err)
	}
	e.common.IdlePolls = 1
	e.leadSeq("sid-9", "idle")
	wantExit(t, e.resume(), cloudBootParkExit)

	argv := e.tmux[0]
	i := slices.Index(argv, "--resume")
	if i < 0 || argv[i+1] != "sid-9" {
		t.Fatalf("argv lacks --resume sid-9: %q", argv)
	}
	prompt := argv[len(argv)-1]
	for _, want := range []string{"seq 42", "op comment", "author eric@example.com", "please rebase\nthanks", "ateam show at-ab12"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if !strings.Contains(e.desc, "where: run:dsp-run1") || strings.Contains(e.desc, "laptop") {
		t.Errorf("where not rewritten:\n%s", e.desc)
	}
	if slices.Contains(argv, "--session-id") {
		t.Errorf("resume must not pass --session-id: %q", argv)
	}
}

func TestCloudBootResumeGenericPromptAndSessionFromInitiative(t *testing.T) {
	e := newCBEnv(t)
	e.desc = "repo: x\nsession: old-1\nsession: sid-last\n"
	e.common.IdlePolls = 1
	e.leadSeq("sid-last", "idle")
	// No state file, no wake.json.
	wantExit(t, e.resume(), cloudBootParkExit)
	argv := e.tmux[0]
	if i := slices.Index(argv, "--resume"); i < 0 || argv[i+1] != "sid-last" {
		t.Fatalf("argv lacks --resume sid-last: %q", argv)
	}
	if prompt := argv[len(argv)-1]; !strings.Contains(prompt, "with no wake event") {
		t.Errorf("prompt = %q, want generic", prompt)
	}

	// Invalid wake.json is also generic.
	if err := os.WriteFile(filepath.Join(e.sup, "wake.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if readCloudWake(e.sup) != nil {
		t.Error("invalid wake.json must yield nil")
	}
}

func TestResolveCloudInitiative(t *testing.T) {
	if id, err := resolveCloudInitiative("", "at-ab12"); err != nil || id != "at-ab12" {
		t.Errorf("meta: %q, %v", id, err)
	}
	if id, err := resolveCloudInitiative("at-flag", "at-ab12"); err != nil || id != "at-flag" {
		t.Errorf("flag must win: %q, %v", id, err)
	}
	for _, meta := range []string{"", "  "} {
		if _, err := resolveCloudInitiative("", meta); err == nil {
			t.Errorf("meta %q: want error", meta)
		}
	}
}

func TestResolveCloudPluginDirFromSymlinkedBinary(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins", "agent-teams")
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin", "ateam-linux-amd64")
	for p, body := range map[string]string{bin: "", filepath.Join(root, ".claude-plugin", "plugin.json"): "{}"} {
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(t.TempDir(), "ateam")
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	got, err := resolveCloudPluginDir("", func() (string, error) { return link, nil })
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(root)
	if gotR, _ := filepath.EvalSymlinks(got); gotR != want {
		t.Errorf("dir = %s, want %s", got, want)
	}
	if _, err := resolveCloudPluginDir(t.TempDir(), nil); err == nil {
		t.Error("dir without plugin.json must error")
	}
}
