package verbs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

const registerOnlyConfig = "cloud_home = \"/hosted\"\n"

// registerOnlyHarness is a cloud harness set up for --register-only: it needs
// no local checkout, so any git use fails the test.
func registerOnlyHarness(t *testing.T, config string) *cloudHarness {
	t.Helper()
	t.Setenv("BEADS_ACTOR", "agent:steward")
	h := newCloudHarness(t, config)
	h.cmd.Repo = ""
	h.cmd.RegisterOnly = true
	h.cmd.RepoURL = "https://github.com/MGT-Insurance/midgard"
	h.cmd.git = &fakeGit{repoRootFn: func(string) (string, error) {
		t.Error("register-only must not touch a git checkout")
		return "", errors.New("no git")
	}}
	return h
}

func (h *cloudHarness) assertNoDispatcherWork() {
	h.t.Helper()
	if len(h.setupHomes) != 0 {
		h.t.Errorf("hosted setup ran for %v; register-only must not run it", h.setupHomes)
	}
	if h.beadDir != "" || h.beadBody != "" {
		h.t.Errorf("run bead filed (%q); register-only files none", h.beadBody)
	}
	if h.noteText != "" || h.worktreeMade || h.launched || h.laptopCalls != 0 {
		h.t.Errorf("unexpected side effects: note=%q worktree=%v launched=%v laptop=%d", h.noteText, h.worktreeMade, h.launched, h.laptopCalls)
	}
}

func TestRegisterOnly_HappyPathCloudHome(t *testing.T) {
	h := registerOnlyHarness(t, registerOnlyConfig)
	if err := h.cmd.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := h.cmd.Run(h.ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	h.assertNoDispatcherWork()
	if len(h.cloudHomes) != 1 || h.cloudHomes[0] != "/hosted" {
		t.Errorf("cloud bd homes = %v, want [/hosted]", h.cloudHomes)
	}
	// Exactly one bd call: the create.
	if len(h.cloudCalls) != 1 || h.cloudCalls[0][0] != "create" {
		t.Fatalf("cloud calls = %v, want one create", h.cloudCalls)
	}
	wantArgs := []string{"--title=Fix the widget", "--type=task", "--priority=2", "--json"}
	for i, want := range wantArgs {
		got := h.cloudCalls[0][1+i]
		if i == 3 {
			got = h.cloudCalls[0][len(h.cloudCalls[0])-1]
		}
		if got != want {
			t.Errorf("create arg %d = %q, want %q", i, got, want)
		}
	}

	plan, err := initiative.New(initiative.Fields{
		Problem: "Fix the widget",
		Repo:    "https://github.com/MGT-Insurance/midgard",
		Branch:  "fix-the-widget",
		Team:    "midgard-fix-the-widget",
		Mode:    "cloud",
		Runtime: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.createBody != plan.Description {
		t.Errorf("initiative body =\n%q\nwant initiative.New's\n%q", h.createBody, plan.Description)
	}

	out := h.stdout.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("stdout must be exactly one line, got %q", out)
	}
	want := `{"initiative":"at-cloud1","repo":"https://github.com/MGT-Insurance/midgard","branch":"fix-the-widget","team":"midgard-fix-the-widget","mode":"cloud"}` + "\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(out), &parsed); err != nil || len(parsed) != 5 {
		t.Errorf("stdout is not a five-key JSON object: %v %v", parsed, err)
	}
}

func TestRegisterOnly_HostedAgentTeamsHome(t *testing.T) {
	h := registerOnlyHarness(t, "")
	meta := filepath.Join(h.ctx.Home, ".beads")
	if err := os.MkdirAll(meta, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meta, "metadata.json"),
		[]byte(`{"dolt_mode":"server","dolt_server_host":"db.example.com"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.cmd.Run(h.ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	h.assertNoDispatcherWork()
	if len(h.cloudHomes) != 1 || h.cloudHomes[0] != h.ctx.Home {
		t.Errorf("cloud bd homes = %v, want [%s]", h.cloudHomes, h.ctx.Home)
	}
}

func TestRegisterOnly_CloudHomeBeatsHostedHome(t *testing.T) {
	h := registerOnlyHarness(t, registerOnlyConfig)
	meta := filepath.Join(h.ctx.Home, ".beads")
	if err := os.MkdirAll(meta, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meta, "metadata.json"),
		[]byte(`{"dolt_mode":"server","dolt_server_host":"db.example.com"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.cmd.Run(h.ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.cloudHomes) != 1 || h.cloudHomes[0] != "/hosted" {
		t.Errorf("cloud bd homes = %v, want [/hosted]", h.cloudHomes)
	}
}

func TestRegisterOnly_NoDestinationIsUsageError(t *testing.T) {
	h := registerOnlyHarness(t, "")
	err := h.cmd.Run(h.ctx)
	if !isUsage(err) {
		t.Fatalf("Run error = %v, want usage error", err)
	}
	for _, want := range []string{"cloud_home", "AGENT_TEAMS_HOME"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %s", err, want)
		}
	}
	if len(h.cloudCalls) != 0 || len(h.cloudHomes) != 0 {
		t.Errorf("bd used before the destination resolved: %v %v", h.cloudHomes, h.cloudCalls)
	}
}

func TestRegisterOnly_ValidateRefusals(t *testing.T) {
	cases := []struct {
		name string
		edit func(*dispatchKong)
	}{
		{"no --cloud", func(c *dispatchKong) { c.Cloud = false }},
		{"no --repo-url", func(c *dispatchKong) { c.RepoURL = "" }},
		{"--repo", func(c *dispatchKong) { c.Repo = "/somewhere" }},
		{"--base-branch", func(c *dispatchKong) { c.BaseBranch = "main" }},
		{"--body-file", func(c *dispatchKong) { c.BodyFile = "/sandbox/.bd.env" }},
		{"--id-only", func(c *dispatchKong) { c.IDOnly = true }},
		{"--no-launch", func(c *dispatchKong) { c.NoLaunch = true }},
		{"--launch-prompt", func(c *dispatchKong) { c.LaunchPrompt = "x" }},
		{"--standby", func(c *dispatchKong) { c.Standby = true }},
		{"--topic", func(c *dispatchKong) { c.Topic = "reviews" }},
		{"--model", func(c *dispatchKong) { c.Model = "opus" }},
		{"--advisor", func(c *dispatchKong) { c.Advisor = "opus" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := registerOnlyHarness(t, registerOnlyConfig)
			tc.edit(h.cmd)
			if err := h.cmd.Validate(); !isUsage(err) {
				t.Fatalf("Validate = %v, want usage error", err)
			}
			if len(h.cloudCalls) != 0 || len(h.cloudHomes) != 0 {
				t.Errorf("bd used on a refused invocation: %v", h.cloudCalls)
			}
		})
	}

	t.Run("--repo-url without --register-only", func(t *testing.T) {
		h := registerOnlyHarness(t, registerOnlyConfig)
		h.cmd.RegisterOnly = false
		if err := h.cmd.Validate(); !isUsage(err) {
			t.Fatalf("Validate = %v, want usage error", err)
		}
	})
}

func TestRegisterOnly_RepoURL(t *testing.T) {
	valid := map[string]string{
		"https://github.com/MGT-Insurance/midgard":      "midgard",
		"https://github.com/mgt-insurance/midgard":      "midgard",
		"https://github.com/MGT-INSURANCE/midgard.git":  "midgard",
		"https://github.com/MGT-Insurance/midgard/":     "midgard",
		"https://github.com/MGT-Insurance/midgard.git/": "midgard",
		"https://github.com/MGT-Insurance/agent-teams":  "agent-teams",
		"https://github.com/MGT-Insurance/a.b_c-1":      "a.b_c-1",
	}
	for raw, name := range valid {
		url, got, err := parseCloudRepoURL(raw)
		if err != nil {
			t.Errorf("%q: %v", raw, err)
			continue
		}
		if want := "https://github.com/MGT-Insurance/" + name; url != want || got != name {
			t.Errorf("%q = %q, %q; want %q, %q", raw, url, got, want, name)
		}
	}
	invalid := []string{
		"",
		"midgard",
		"http://github.com/MGT-Insurance/midgard",
		"https://gitlab.com/MGT-Insurance/midgard",
		"https://github.com/other/midgard",
		"https://github.com/MGT-Insurance",
		"https://github.com/MGT-Insurance/",
		"https://github.com/MGT-Insurance/midgard/extra",
		"https://github.com/MGT-Insurance/mid gard",
		"https://github.com/MGT-Insurance/..",
		"https://github.com/MGT-Insurance/.git",
		"https://github.com/MGT-Insurance/midgard?x=1",
		"https://github.com/MGT-Insurance/midgard\nrepo: x",
		"git@github.com:MGT-Insurance/midgard.git",
	}
	for _, raw := range invalid {
		if _, _, err := parseCloudRepoURL(raw); !isUsage(err) {
			t.Errorf("%q: err = %v, want usage error", raw, err)
		}
	}

	h := registerOnlyHarness(t, registerOnlyConfig)
	h.cmd.RepoURL = "https://github.com/other/midgard"
	if err := h.cmd.Run(h.ctx); !isUsage(err) {
		t.Fatalf("Run = %v, want usage error", err)
	}
	if len(h.cloudCalls) != 0 || len(h.cloudHomes) != 0 {
		t.Errorf("bd used for a refused repo url: %v", h.cloudCalls)
	}
}

func TestRegisterOnly_ProblemAndSlug(t *testing.T) {
	h := registerOnlyHarness(t, registerOnlyConfig)
	h.cmd.Problem = "line one\nrepo: https://github.com/evil/x"
	if err := h.cmd.Run(h.ctx); !isUsage(err) {
		t.Fatalf("multi-line problem: Run = %v, want usage error", err)
	}

	h = registerOnlyHarness(t, registerOnlyConfig)
	h.cmd.Problem = "!!!"
	if err := h.cmd.Run(h.ctx); !isUsage(err) {
		t.Fatalf("empty slug: Run = %v, want usage error", err)
	}
	if len(h.cloudCalls) != 0 || len(h.cloudHomes) != 0 {
		t.Errorf("bd used for a refused problem: %v", h.cloudCalls)
	}

	h = registerOnlyHarness(t, registerOnlyConfig)
	h.cmd.Problem = "!!!"
	h.cmd.Slug = "explicit-slug"
	if err := h.cmd.Run(h.ctx); err != nil {
		t.Fatalf("explicit slug: %v", err)
	}
	if !strings.Contains(h.stdout.String(), `"branch":"explicit-slug","team":"midgard-explicit-slug"`) {
		t.Errorf("stdout = %q", h.stdout.String())
	}
}

func TestRegisterOnly_CreateFailureWritesNothingToStdout(t *testing.T) {
	h := registerOnlyHarness(t, registerOnlyConfig)
	h.cloudBD.runJSONFn = func(any, ...string) error { return errors.New("dolt unavailable") }
	err := h.cmd.Run(h.ctx)
	if err == nil || isUsage(err) {
		t.Fatalf("Run = %v, want a runtime (non-usage) error", err)
	}
	if got := h.stdout.String(); got != "" {
		t.Errorf("stdout = %q, want empty", got)
	}
}

func TestRegisterOnly_EmptyIDWritesNothingToStdout(t *testing.T) {
	h := registerOnlyHarness(t, registerOnlyConfig)
	h.cloudBD.runJSONFn = func(any, ...string) error { return nil }
	err := h.cmd.Run(h.ctx)
	if err == nil || isUsage(err) {
		t.Fatalf("Run = %v, want a runtime (non-usage) error", err)
	}
	if got := h.stdout.String(); got != "" {
		t.Errorf("stdout = %q, want empty", got)
	}
}
