package verbs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

const testCloudSHA = "0123456789abcdef0123456789abcdef01234567"

// cloudHarness wires a --cloud dispatch to fakes and records every call.
type cloudHarness struct {
	t       *testing.T
	cmd     *dispatchKong
	ctx     *cli.Context
	stdout  fmtBuf
	cloudBD *fakeBD

	cloudHomes   []string
	cloudCalls   [][]string
	createBody   string
	beadDir      string
	beadActor    string
	beadTitle    string
	beadBody     string
	noteText     string
	launched     bool
	worktreeMade bool
	laptopCalls  int
}

type fmtBuf interface{ String() string }

func newCloudHarness(t *testing.T, config string) *cloudHarness {
	t.Helper()
	t.Setenv(dispatcherActorEnv, "")
	home := t.TempDir()
	if config != "" {
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repoDir := newEnabledRepoDir(t)
	h := &cloudHarness{t: t}

	laptop := &fakeBD{
		runFn:     func(...string) (string, error) { h.laptopCalls++; return "", nil },
		runJSONFn: func(any, ...string) error { h.laptopCalls++; return nil },
	}
	h.cloudBD = &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			h.cloudCalls = append(h.cloudCalls, args)
			for _, a := range args {
				if v, ok := strings.CutPrefix(a, "--body-file="); ok {
					data, err := os.ReadFile(v)
					if err != nil {
						return err
					}
					h.createBody = string(data)
				}
			}
			return json.Unmarshal([]byte(`{"id":"at-cloud1"}`), dst.(*bd.Issue))
		},
		runFn: func(args ...string) (string, error) {
			h.cloudCalls = append(h.cloudCalls, args)
			if args[0] == "note" {
				data, err := os.ReadFile(strings.TrimPrefix(args[2], "--file="))
				if err != nil {
					return "", err
				}
				h.noteText = string(data)
			}
			return "", nil
		},
	}
	ctx, stdout, _ := makeCtx(laptop, home)
	h.ctx, h.stdout = ctx, stdout
	h.cmd = &dispatchKong{
		Problem: "Fix the widget",
		Repo:    repoDir,
		Cloud:   true,
		git: &fakeGit{
			repoRootFn:    func(string) (string, error) { return repoDir, nil },
			addWorktreeFn: func(_, _, _, _ string) error { h.worktreeMade = true; return nil },
		},
		launch: func(*cli.Context, string, string, string, string) error { h.launched = true; return nil },
		launchRaw: func(*cli.Context, string, string, string, string, string, string) error {
			h.launched = true
			return nil
		},
		createEpic: func(string, string) (string, error) {
			h.t.Error("cloud dispatch must skip the project epic")
			return "", nil
		},
		cloudBD: func(home string) cli.BDRunner {
			h.cloudHomes = append(h.cloudHomes, home)
			return h.cloudBD
		},
		fileRunBead: func(dir, actor, title, body string) (string, error) {
			h.beadDir, h.beadActor, h.beadTitle, h.beadBody = dir, actor, title, body
			return "dsp-run1", nil
		},
	}
	return h
}

const fullCloudConfig = "cloud_home = \"/hosted\"\ndispatcher_beads = \"/dsp\"\ndispatcher_actor = \"eric@example.com\"\n"

func isUsage(err error) bool {
	var u *cli.UsageError
	return errors.As(err, &u)
}

func TestDispatchCloud_HappyPath(t *testing.T) {
	h := newCloudHarness(t, fullCloudConfig)
	if err := h.cmd.Run(h.ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if h.worktreeMade || h.launched {
		t.Errorf("worktree made = %v, launched = %v; want neither", h.worktreeMade, h.launched)
	}
	if h.laptopCalls != 0 {
		t.Errorf("laptop workspace used %d times; cloud initiatives register in cloud_home", h.laptopCalls)
	}
	if len(h.cloudHomes) != 1 || h.cloudHomes[0] != "/hosted" {
		t.Errorf("cloud bd homes = %v, want [/hosted]", h.cloudHomes)
	}
	for _, want := range []string{
		"repo: https://github.com/MGT-Insurance/agent-teams\n",
		"branch: fix-the-widget\n",
		"mode: cloud\n",
		"runtime: claude\n",
	} {
		if !strings.Contains(h.createBody, want) {
			t.Errorf("initiative description missing %q:\n%s", want, h.createBody)
		}
	}
	for _, banned := range []string{"where:", "worktree:", "epic:"} {
		if strings.Contains(h.createBody, banned) {
			t.Errorf("initiative description must not contain %q:\n%s", banned, h.createBody)
		}
	}

	wantBody := "repo: https://github.com/MGT-Insurance/agent-teams\n" +
		"branch: main\n" +
		"sha: " + testCloudSHA + "\n" +
		"type: ateam-dri\n" +
		"initiative: at-cloud1\n" +
		"meta-initiative: at-cloud1\n" +
		"start: auto\n" +
		"---\n" +
		"Fix the widget\n"
	if h.beadBody != wantBody {
		t.Errorf("run bead body =\n%q\nwant\n%q", h.beadBody, wantBody)
	}
	if h.beadTitle != "Fix the widget" {
		t.Errorf("run bead title = %q, want the plain problem", h.beadTitle)
	}
	if h.beadDir != "/dsp" || h.beadActor != "eric@example.com" {
		t.Errorf("run bead dir/actor = %q/%q", h.beadDir, h.beadActor)
	}
	if !strings.Contains(h.noteText, "cloud run bead dsp-run1 filed in beads_eric_lloyd") {
		t.Errorf("note = %q", h.noteText)
	}
	want := "dispatched at-cloud1 to Dispatcher run bead dsp-run1 (repo https://github.com/MGT-Insurance/agent-teams, base main @ 0123456)\n"
	if got := h.stdout.String(); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestDispatchCloud_IDOnly(t *testing.T) {
	h := newCloudHarness(t, fullCloudConfig)
	h.cmd.IDOnly = true
	if err := h.cmd.Run(h.ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := h.stdout.String(); got != "at-cloud1\n" {
		t.Errorf("stdout = %q, want the initiative id", got)
	}
}

func TestDispatchCloud_ActorResolution(t *testing.T) {
	cases := []struct {
		name, config, env, wantActor, wantErr string
	}{
		{"env beats config", fullCloudConfig, "env@example.com", "env@example.com", ""},
		{"config used when env empty", fullCloudConfig, "", "eric@example.com", ""},
		{"neither set", "cloud_home = \"/hosted\"\ndispatcher_beads = \"/dsp\"\n", "", "", "dispatcher_actor"},
		{"agent actor in env", fullCloudConfig, "agent:x", "", "agent actor"},
		{"agent actor in config", strings.Replace(fullCloudConfig, "eric@example.com", "agent:x", 1), "", "", "agent actor"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newCloudHarness(t, tc.config)
			t.Setenv(dispatcherActorEnv, tc.env)
			err := h.cmd.Run(h.ctx)
			if tc.wantErr != "" {
				if !isUsage(err) || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want usage error containing %q", err, tc.wantErr)
				}
				if len(h.cloudHomes) != 0 || h.beadBody != "" {
					t.Error("usage error must write nothing")
				}
				return
			}
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if h.beadActor != tc.wantActor {
				t.Errorf("actor = %q, want %q", h.beadActor, tc.wantActor)
			}
		})
	}
}

func TestDispatchCloud_MissingConfigKey(t *testing.T) {
	for _, key := range []string{"cloud_home", "dispatcher_beads"} {
		var kept []string
		for _, line := range strings.SplitAfter(fullCloudConfig, "\n") {
			if line != "" && !strings.HasPrefix(line, key) {
				kept = append(kept, line)
			}
		}
		h := newCloudHarness(t, strings.Join(kept, ""))
		err := h.cmd.Run(h.ctx)
		if !isUsage(err) || !strings.Contains(err.Error(), key) {
			t.Errorf("missing %s: err = %v, want usage error naming it", key, err)
		}
		if len(h.cloudHomes) != 0 || h.beadBody != "" {
			t.Errorf("missing %s: wrote something", key)
		}
	}
}

func TestDispatchCloud_UsageErrors(t *testing.T) {
	t.Run("non-MGT owner", func(t *testing.T) {
		h := newCloudHarness(t, fullCloudConfig)
		h.cmd.git.(*fakeGit).remoteURLFn = func(string) (string, error) { return "https://github.com/other/agent-teams", nil }
		if err := h.cmd.Run(h.ctx); !isUsage(err) || !strings.Contains(err.Error(), "MGT-Insurance") {
			t.Fatalf("err = %v", err)
		}
		if len(h.cloudHomes) != 0 || h.beadBody != "" {
			t.Error("wrote something")
		}
	})
	t.Run("codex runtime", func(t *testing.T) {
		h := newCloudHarness(t, fullCloudConfig)
		h.cmd.Runtime = "codex"
		if err := h.cmd.Run(h.ctx); !isUsage(err) || !strings.Contains(err.Error(), "Claude runtime only") {
			t.Fatalf("err = %v", err)
		}
		if len(h.cloudHomes) != 0 {
			t.Error("wrote something")
		}
	})
	for flag, set := range map[string]func(*dispatchKong){
		"--no-launch":     func(c *dispatchKong) { c.NoLaunch = true },
		"--launch-prompt": func(c *dispatchKong) { c.LaunchPrompt = "x" },
		"--standby":       func(c *dispatchKong) { c.Standby = true },
		"--topic":         func(c *dispatchKong) { c.Topic = ReviewsHandle },
		"--model":         func(c *dispatchKong) { c.Model = "m" },
		"--advisor":       func(c *dispatchKong) { c.Advisor = "opus" },
	} {
		cmd := &dispatchKong{Problem: "p", Cloud: true}
		set(cmd)
		if err := cmd.Validate(); !isUsage(err) || !strings.Contains(err.Error(), flag) {
			t.Errorf("%s: Validate = %v, want usage error naming the flag", flag, err)
		}
	}
	if err := (&dispatchKong{Problem: "p", Cloud: true}).Validate(); err != nil {
		t.Errorf("plain --cloud: Validate = %v", err)
	}
}

func TestDispatchCloud_RunBeadFailureClosesInitiative(t *testing.T) {
	h := newCloudHarness(t, fullCloudConfig)
	h.cmd.fileRunBead = func(_, _, _, _ string) (string, error) { return "", errors.New("dolt down") }
	err := h.cmd.Run(h.ctx)
	if err == nil || isUsage(err) || !strings.Contains(err.Error(), "cloud run bead not filed: dolt down") {
		t.Fatalf("err = %v", err)
	}
	var closed []string
	for _, call := range h.cloudCalls {
		if call[0] == "close" {
			closed = call
		}
	}
	if len(closed) != 3 || closed[1] != "at-cloud1" || closed[2] != "--reason=cloud run bead not filed: dolt down" {
		t.Errorf("close call = %v", closed)
	}
}

func TestDispatchCloud_GitFailuresWriteNothing(t *testing.T) {
	for name, set := range map[string]func(*fakeGit){
		"remote url": func(g *fakeGit) { g.remoteURLFn = func(string) (string, error) { return "", errors.New("no origin") } },
		"remote head": func(g *fakeGit) {
			g.remoteHeadFn = func(string, string) (string, error) { return "", errors.New("not pushed") }
		},
	} {
		h := newCloudHarness(t, fullCloudConfig)
		set(h.cmd.git.(*fakeGit))
		if err := h.cmd.Run(h.ctx); err == nil {
			t.Errorf("%s: want error", name)
		}
		if len(h.cloudHomes) != 0 || h.beadBody != "" {
			t.Errorf("%s: wrote something", name)
		}
	}
}
