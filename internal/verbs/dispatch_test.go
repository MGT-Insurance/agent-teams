package verbs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/repoconfig"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
	"github.com/mgt-insurance/agent-teams/internal/transport"
)

// ---- fakes -----------------------------------------------------------------

// fakeGit implements gitRunner for tests. All fields default to happy-path
// behaviour; override per-test.
type fakeGit struct {
	repoRootFn       func(dir string) (string, error)
	defaultBranchFn  func(repoRoot string) string
	worktreeExistsFn func(repoRoot, wtPath string) bool
	addWorktreeFn    func(repoRoot, wtPath, branch, base string) error
	removeWorktreeFn func(repoRoot, wtPath string) error
}

func (f *fakeGit) RepoRoot(dir string) (string, error) {
	if f.repoRootFn != nil {
		return f.repoRootFn(dir)
	}
	return dir, nil
}
func (f *fakeGit) DefaultBranch(repoRoot string) string {
	if f.defaultBranchFn != nil {
		return f.defaultBranchFn(repoRoot)
	}
	return "main"
}
func (f *fakeGit) WorktreeExists(repoRoot, wtPath string) bool {
	if f.worktreeExistsFn != nil {
		return f.worktreeExistsFn(repoRoot, wtPath)
	}
	return false
}
func (f *fakeGit) AddWorktree(repoRoot, wtPath, branch, base string) error {
	if f.addWorktreeFn != nil {
		return f.addWorktreeFn(repoRoot, wtPath, branch, base)
	}
	return nil
}
func (f *fakeGit) RemoveWorktree(repoRoot, wtPath string) error {
	if f.removeWorktreeFn != nil {
		return f.removeWorktreeFn(repoRoot, wtPath)
	}
	return nil
}

// fakeBD implements cli.BDRunner for tests.
type fakeBD struct {
	runFn     func(args ...string) (string, error)
	runJSONFn func(dst any, args ...string) error
}

func (f *fakeBD) Run(args ...string) (string, error) {
	if f.runFn != nil {
		return f.runFn(args...)
	}
	return "", nil
}
func (f *fakeBD) RunJSON(dst any, args ...string) error {
	if f.runJSONFn != nil {
		return f.runJSONFn(dst, args...)
	}
	return nil
}

// makeCtx builds a cli.Context with captured stdout/stderr and the supplied BD.
func makeCtx(bd cli.BDRunner, home string) (*cli.Context, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	return &cli.Context{
		Home:   home,
		BD:     bd,
		Stdout: &stdout,
		Stderr: &stderr,
	}, &stdout, &stderr
}

// newEnabledRepoDir returns a fresh temp dir seeded with a .agent-teams
// marker file, so fixtures that expect dispatch/resume to reach past the
// repo-enabled gate don't need to re-derive that setup individually.
func newEnabledRepoDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, repoconfig.FileName), nil, 0o644); err != nil {
		t.Fatalf("newEnabledRepoDir: write %s: %v", repoconfig.FileName, err)
	}
	return dir
}

// ---- dispatch happy path (--no-launch) -------------------------------------

func TestDispatch_NoLaunch_HappyPath(t *testing.T) {
	// Create a real temp dir to act as the "repo root" so WorktreeExists can
	// stat it, and a sub-dir for the worktree target that does NOT exist yet.
	repoDir := newEnabledRepoDir(t)
	home := t.TempDir()

	// The worktree path is <home>-worktrees/<slug>; it must not exist yet.
	wtRoot := home + "-worktrees"
	expectedSlug := "add-undo-stack"
	expectedWt := filepath.Join(wtRoot, expectedSlug)

	var capturedBodyFile string
	var capturedBody string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			// Find the --body-file arg and record it.
			for _, a := range args {
				if strings.HasPrefix(a, "--body-file=") {
					capturedBodyFile = strings.TrimPrefix(a, "--body-file=")
					if data, err := os.ReadFile(capturedBodyFile); err == nil {
						capturedBody = string(data)
					}
				}
			}
			// Populate the issue by unmarshalling JSON into *bd.Issue.
			if issue, ok := dst.(*bd.Issue); ok {
				return json.Unmarshal([]byte(`{"id":"at-test1","title":"Add undo stack"}`), issue)
			}
			return nil
		},
	}

	fg := &fakeGit{
		repoRootFn: func(dir string) (string, error) { return repoDir, nil },
	}

	ctx, stdout, _ := makeCtx(fbd, home)
	cmd := &dispatchKong{
		Problem:  "Add undo stack",
		Repo:     repoDir,
		NoLaunch: true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	err := cmd.Run(ctx)
	if err != nil {
		t.Fatalf("dispatch --no-launch: unexpected error: %v", err)
	}

	// Verify initiative_id, slug, base_branch, and worktree path in stdout.
	out := stdout.String()
	if !strings.Contains(out, "initiative_id: at-test1") {
		t.Errorf("stdout missing 'initiative_id: at-test1':\n%s", out)
	}
	if !strings.Contains(out, "slug: "+expectedSlug) {
		t.Errorf("stdout missing 'slug: %s':\n%s", expectedSlug, out)
	}
	if !strings.Contains(out, "base_branch: main") {
		t.Errorf("stdout missing 'base_branch: main':\n%s", out)
	}
	if !strings.Contains(out, expectedWt) {
		t.Errorf("stdout missing worktree path %q:\n%s", expectedWt, out)
	}

	// Verify the body file was written with the worktree line.
	if capturedBodyFile != "" {
		body, err := os.ReadFile(capturedBodyFile)
		if err == nil && !strings.Contains(string(body), "worktree: "+expectedWt) {
			t.Errorf("body file missing 'worktree: %s':\n%s", expectedWt, string(body))
		}
	}
	if !strings.Contains(capturedBody, "runtime: claude\n") {
		t.Errorf("new dispatch must persist its concrete default runtime:\n%s", capturedBody)
	}
}

func TestDispatch_WorktreeSetupFailureContinuesLifecycle(t *testing.T) {
	tests := []struct {
		name       string
		runtime    string
		noLaunch   bool
		idOnly     bool
		outcome    string
		wantLaunch bool
	}{
		{name: "claude missing hook", outcome: "missing", wantLaunch: true},
		{name: "codex failed hook", runtime: "codex", outcome: "exit-42", wantLaunch: true},
		{name: "no launch missing hook", noLaunch: true, idOnly: true, outcome: "missing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			repoDir := newEnabledRepoDir(t)
			slug := "setup-order"
			wtPath := filepath.Join(home+"-worktrees", slug)
			result := worktreeSetupResult{Path: wtPath, Hook: "/configured/setup.sh", Outcome: tt.outcome}
			warning := result.warningLine()
			var events []string
			var body string
			var removed bool
			fbd := &fakeBD{runJSONFn: func(dst any, args ...string) error {
				events = append(events, "register")
				for _, arg := range args {
					if strings.HasPrefix(arg, "--body-file=") {
						data, err := os.ReadFile(strings.TrimPrefix(arg, "--body-file="))
						if err != nil {
							return err
						}
						body = string(data)
					}
				}
				issue := dst.(*bd.Issue)
				issue.ID = "at-setup-order"
				issue.Title = "setup order"
				return nil
			}}
			fg := &fakeGit{
				repoRootFn: func(string) (string, error) { return repoDir, nil },
				addWorktreeFn: func(_, _, _, _ string) error {
					events = append(events, "add-worktree")
					return nil
				},
				removeWorktreeFn: func(_, _ string) error {
					removed = true
					return nil
				},
			}
			ctx, stdout, stderr := makeCtx(fbd, home)
			cmd := &dispatchKong{
				Problem:  "setup order",
				Slug:     slug,
				Repo:     repoDir,
				Runtime:  tt.runtime,
				NoLaunch: tt.noLaunch,
				IDOnly:   tt.idOnly,
				git:      fg,
				setup: func(_ *cli.Context, gotPath string) (worktreeSetupResult, error) {
					if gotPath != wtPath {
						t.Fatalf("setup path = %q, want %q", gotPath, wtPath)
					}
					events = append(events, "setup")
					return result, &cli.SilentError{Code: 1}
				},
				createEpic: func(_, _ string) (string, error) {
					events = append(events, "epic")
					return "project-epic", nil
				},
				transportEnabled: func(string) bool { return true },
				transportFor: func(string) (transport.Transport, error) {
					events = append(events, "transport")
					return &fakeTransport{returnRef: "setup-topic"}, nil
				},
				labelAdd: func(cli.BDRunner, string, string) error {
					events = append(events, "topic-recorded")
					return nil
				},
				launch: func(*cli.Context, string, string, string, string) error {
					events = append(events, "launch")
					return nil
				},
				runtimeStart: func(*cli.Context, runtimeStartRequest) error {
					events = append(events, "launch")
					return nil
				},
			}

			if err := cmd.Run(ctx); err != nil {
				t.Fatalf("dispatch should continue after configured hook failure: %v", err)
			}
			wantEvents := []string{"add-worktree", "setup", "epic", "register", "transport", "topic-recorded"}
			if tt.wantLaunch {
				wantEvents = append(wantEvents, "launch")
			}
			if strings.Join(events, ",") != strings.Join(wantEvents, ",") {
				t.Fatalf("lifecycle order = %v, want %v", events, wantEvents)
			}
			if removed {
				t.Fatal("configured setup failure must retain the worktree")
			}
			if !strings.Contains(stderr.String(), "WARNING") || !strings.Contains(stderr.String(), warning) {
				t.Fatalf("stderr must contain loud normalized warning %q:\n%s", warning, stderr.String())
			}
			if !strings.Contains(body, warning) {
				t.Fatalf("registered initiative body missing normalized warning %q:\n%s", warning, body)
			}
			if tt.idOnly && stdout.String() != "at-setup-order\n" {
				t.Fatalf("--id-only stdout = %q, want initiative id only", stdout.String())
			}
		})
	}
}

func TestDispatch_WorktreeSetupUnexpectedErrorStopsLifecycle(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)
	var events []string
	ctx, _, _ := makeCtx(&fakeBD{runJSONFn: func(any, ...string) error {
		events = append(events, "register")
		return nil
	}}, home)
	cmd := &dispatchKong{
		Problem:  "unexpected setup error",
		Repo:     repoDir,
		NoLaunch: true,
		git: &fakeGit{
			repoRootFn: func(string) (string, error) { return repoDir, nil },
			addWorktreeFn: func(string, string, string, string) error {
				events = append(events, "add-worktree")
				return nil
			},
		},
		setup: func(*cli.Context, string) (worktreeSetupResult, error) {
			events = append(events, "setup")
			return worktreeSetupResult{}, errors.New("hook registry is a directory")
		},
		createEpic: func(string, string) (string, error) {
			events = append(events, "epic")
			return "", nil
		},
		launch: func(*cli.Context, string, string, string, string) error {
			events = append(events, "launch")
			return nil
		},
	}

	err := cmd.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "worktree setup") {
		t.Fatalf("Run error = %v, want unexpected setup error", err)
	}
	if got, want := strings.Join(events, ","), "add-worktree,setup"; got != want {
		t.Fatalf("lifecycle events = %q, want %q (must not register or launch)", got, want)
	}
}

func TestRunWorktreeSetup_SuppressesHookOutputAtDispatchBoundary(t *testing.T) {
	home := t.TempDir()
	repoRoot := initGitWorktree(t)
	scriptPath := filepath.Join(t.TempDir(), "leaky-setup.sh")
	const stdoutMarker = "dispatch-hook-stdout-secret"
	const stderrMarker = "dispatch-hook-stderr-secret"
	writeTinyScript(t, scriptPath, "#!/bin/sh\nprintf '%s\\n' "+stdoutMarker+"\nprintf '%s\\n' "+stderrMarker+" >&2\nexit 17\n")
	writeHookFile(t, home, slugifyBasename(repoRoot), scriptPath)

	ctx, stdout, stderr := makeCtx(&fakeBD{}, home)
	result, err := runWorktreeSetup(ctx, repoRoot)
	if err == nil || result.Outcome != "exit-17" {
		t.Fatalf("runWorktreeSetup result=%+v err=%v, want exit-17 failure from real hook runner", result, err)
	}
	if strings.Contains(stdout.String(), stdoutMarker) || strings.Contains(stdout.String(), stderrMarker) ||
		strings.Contains(stderr.String(), stdoutMarker) || strings.Contains(stderr.String(), stderrMarker) {
		t.Fatalf("hook output leaked through primary dispatch boundary: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestDispatch_CodexPersistsRuntimeAndStartsWorker(t *testing.T) {
	repoDir := newEnabledRepoDir(t)
	home := t.TempDir()
	var body string
	fbd := &fakeBD{runJSONFn: func(dst any, args ...string) error {
		for _, arg := range args {
			if strings.HasPrefix(arg, "--body-file=") {
				data, err := os.ReadFile(strings.TrimPrefix(arg, "--body-file="))
				if err != nil {
					return err
				}
				body = string(data)
			}
		}
		if issue, ok := dst.(*bd.Issue); ok {
			issue.ID = "at-codex1"
		}
		return nil
	}}
	var started runtimeStartRequest
	ctx, stdout, _ := makeCtx(fbd, home)
	cmd := &dispatchKong{
		Problem: "Codex work",
		Repo:    repoDir,
		Runtime: "codex",
		Model:   "gpt-test",
		git:     &fakeGit{repoRootFn: func(string) (string, error) { return repoDir, nil }},
		launch: func(*cli.Context, string, string, string, string) error {
			t.Fatal("Claude launcher called for Codex runtime")
			return nil
		},
		runtimeStart: func(_ *cli.Context, req runtimeStartRequest) error {
			started = req
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(body, "runtime: codex\n") {
		t.Fatalf("initiative body missing runtime: codex:\n%s", body)
	}
	if started.Runtime != sessionruntime.Codex || started.InitiativeID != "at-codex1" || started.Prompt != codexDRIPrompt("at-codex1") || started.Model != "gpt-test" {
		t.Fatalf("runtime start = %+v", started)
	}
	if !strings.Contains(stdout.String(), sessionruntime.EventLogPath(home, "at-codex1")) || strings.Contains(stdout.String(), "claude attach") {
		t.Fatalf("monitoring output is not Codex-specific:\n%s", stdout.String())
	}
}

func TestDispatch_RuntimeResolutionAndValidation(t *testing.T) {
	t.Run("machine default is persisted without launch", func(t *testing.T) {
		t.Setenv("ATEAM_RUNTIME", "codex")
		repoDir := newEnabledRepoDir(t)
		var body string
		fbd := &fakeBD{runJSONFn: func(dst any, args ...string) error {
			for _, arg := range args {
				if strings.HasPrefix(arg, "--body-file=") {
					data, _ := os.ReadFile(strings.TrimPrefix(arg, "--body-file="))
					body = string(data)
				}
			}
			dst.(*bd.Issue).ID = "at-auto1"
			return nil
		}}
		ctx, _, _ := makeCtx(fbd, t.TempDir())
		cmd := &dispatchKong{Problem: "auto", Repo: repoDir, NoLaunch: true, git: &fakeGit{repoRootFn: func(string) (string, error) { return repoDir, nil }}}
		if err := cmd.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, "runtime: codex\n") {
			t.Fatalf("body = %s", body)
		}
	})
	t.Run("unknown runtime fails before worktree", func(t *testing.T) {
		added := false
		cmd := &dispatchKong{Problem: "bad", Runtime: "other", git: &fakeGit{addWorktreeFn: func(string, string, string, string) error { added = true; return nil }}}
		err := cmd.Run(&cli.Context{})
		if err == nil || cli.ExitCode(err) != 2 || added {
			t.Fatalf("err=%v added=%v", err, added)
		}
	})
	t.Run("incompatible Codex fails before repo mutation", func(t *testing.T) {
		gitCalled := false
		cmd := &dispatchKong{
			Problem: "codex",
			Runtime: "codex",
			codexCheck: func(context.Context, string) error {
				return fmt.Errorf("official standalone installer required")
			},
			git: &fakeGit{repoRootFn: func(string) (string, error) {
				gitCalled = true
				return "", nil
			}},
		}
		err := cmd.Run(&cli.Context{})
		if err == nil || !strings.Contains(err.Error(), "official standalone") || gitCalled {
			t.Fatalf("err=%v gitCalled=%v", err, gitCalled)
		}
	})
}

func TestDispatch_ConfigRuntimeDefaultsByClass(t *testing.T) {
	t.Setenv("ATEAM_RUNTIME", "")
	tests := []struct {
		name, topic, explicit, want string
		withConfig                  bool
	}{
		{name: "ordinary work uses work runtime", explicit: "auto", want: "codex", withConfig: true},
		{name: "review uses review runtime", topic: ReviewsHandle, want: "claude", withConfig: true},
		{name: "review without config preserves Claude", topic: ReviewsHandle, want: "claude"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoDir := newEnabledRepoDir(t)
			home := t.TempDir()
			if tt.withConfig {
				if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("work_runtime = \"codex\"\nreview_runtime = \"claude\"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var body string
			fbd := &fakeBD{runJSONFn: func(dst any, args ...string) error {
				for _, arg := range args {
					if strings.HasPrefix(arg, "--body-file=") {
						data, err := os.ReadFile(strings.TrimPrefix(arg, "--body-file="))
						if err != nil {
							return err
						}
						body = string(data)
					}
				}
				dst.(*bd.Issue).ID = "at-config1"
				return nil
			}}
			ctx, _, _ := makeCtx(fbd, home)
			cmd := &dispatchKong{
				Problem:  "configured runtime",
				Repo:     repoDir,
				NoLaunch: true,
				Topic:    tt.topic,
				Runtime:  tt.explicit,
				git:      &fakeGit{repoRootFn: func(string) (string, error) { return repoDir, nil }},
			}
			if err := cmd.Run(ctx); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !strings.Contains(body, "runtime: "+tt.want+"\n") {
				t.Fatalf("initiative body does not persist %s runtime:\n%s", tt.want, body)
			}
		})
	}
}

func TestDispatch_HigherRuntimeTiersBypassInvalidConfig(t *testing.T) {
	tests := []struct {
		name, explicit, environment, want string
	}{
		{name: "explicit beats environment and config", explicit: "claude", environment: "codex", want: "claude"},
		{name: "environment beats config", environment: "codex", want: "codex"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ATEAM_RUNTIME", tt.environment)
			repoDir := newEnabledRepoDir(t)
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("not valid = ["), 0o600); err != nil {
				t.Fatal(err)
			}
			var body string
			fbd := &fakeBD{runJSONFn: func(dst any, args ...string) error {
				for _, arg := range args {
					if strings.HasPrefix(arg, "--body-file=") {
						data, err := os.ReadFile(strings.TrimPrefix(arg, "--body-file="))
						if err != nil {
							return err
						}
						body = string(data)
					}
				}
				dst.(*bd.Issue).ID = "at-bypass1"
				return nil
			}}
			ctx, _, _ := makeCtx(fbd, home)
			cmd := &dispatchKong{
				Problem:  "higher tier",
				Repo:     repoDir,
				NoLaunch: true,
				Runtime:  tt.explicit,
				git:      &fakeGit{repoRootFn: func(string) (string, error) { return repoDir, nil }},
			}
			if err := cmd.Run(ctx); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !strings.Contains(body, "runtime: "+tt.want+"\n") {
				t.Fatalf("initiative body does not persist %s runtime:\n%s", tt.want, body)
			}
		})
	}
}

func TestDispatch_InvalidConfigFailsBeforeSideEffects(t *testing.T) {
	t.Setenv("ATEAM_RUNTIME", "")
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte("work_runtime = \"other\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sideEffects := 0
	cmd := &dispatchKong{
		Problem: "invalid config",
		git: &fakeGit{repoRootFn: func(string) (string, error) {
			sideEffects++
			return "", nil
		}},
		createEpic: func(string, string) (string, error) {
			sideEffects++
			return "", nil
		},
		transportEnabled: func(string) bool {
			sideEffects++
			return true
		},
		codexCheck: func(context.Context, string) error {
			sideEffects++
			return nil
		},
		runtimeStart: func(*cli.Context, runtimeStartRequest) error {
			sideEffects++
			return nil
		},
	}
	ctx, _, _ := makeCtx(&fakeBD{
		runFn: func(...string) (string, error) {
			sideEffects++
			return "", nil
		},
		runJSONFn: func(any, ...string) error {
			sideEffects++
			return nil
		},
	}, home)
	err := cmd.Run(ctx)
	if err == nil || cli.ExitCode(err) != 2 || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "work_runtime") {
		t.Fatalf("Run() error = %v, want usage error naming path and key", err)
	}
	if sideEffects != 0 {
		t.Fatalf("invalid config invoked %d dispatch side effects", sideEffects)
	}
}

// ---- dispatch: not a repo --------------------------------------------------

func TestDispatch_NotARepo(t *testing.T) {
	home := t.TempDir()
	fg := &fakeGit{
		repoRootFn: func(dir string) (string, error) {
			return "", fmt.Errorf("not inside a git repo: %s", dir)
		},
	}
	ctx, _, stderr := makeCtx(&fakeBD{}, home)
	cmd := &dispatchKong{
		Problem:  "Some work",
		NoLaunch: true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error for non-repo, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "not inside a git repo") {
		t.Errorf("expected 'not inside a git repo' in stderr, got: %s", stderr.String())
	}
}

// ---- dispatch: repo not enabled ---------------------------------------------

// TestDispatch_RepoNotEnabled: a real git repo with no .agent-teams file is
// refused — the opt-in gate, not the "not a repo" gate above.
func TestDispatch_RepoNotEnabled(t *testing.T) {
	home := t.TempDir()
	repoDir := t.TempDir() // deliberately NOT newEnabledRepoDir — no .agent-teams file
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, stderr := makeCtx(&fakeBD{}, home)
	cmd := &dispatchKong{
		Problem:  "Some work",
		Repo:     repoDir,
		NoLaunch: true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error for a repo with no .agent-teams file, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "agent-teams is not enabled") {
		t.Errorf("expected 'agent-teams is not enabled' in stderr, got: %s", stderr.String())
	}
}

// TestDispatch_RepoDisabled: a .agent-teams file carrying "disabled: true"
// refuses dispatch exactly like a missing file.
func TestDispatch_RepoDisabled(t *testing.T) {
	home := t.TempDir()
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, repoconfig.FileName), []byte("disabled: true\n"), 0o644); err != nil {
		t.Fatalf("write .agent-teams: %v", err)
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, stderr := makeCtx(&fakeBD{}, home)
	cmd := &dispatchKong{
		Problem:  "Some work",
		Repo:     repoDir,
		NoLaunch: true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error for a repo with disabled: true, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "agent-teams is not enabled") {
		t.Errorf("expected 'agent-teams is not enabled' in stderr, got: %s", stderr.String())
	}
}

// ---- dispatch: empty slug --------------------------------------------------

func TestDispatch_EmptySlug(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	// A problem that slugifies to empty (pure punctuation).
	cmd := &dispatchKong{
		Problem:  "!@#$%",
		Repo:     repoDir,
		NoLaunch: true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error for empty slug, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2 (UsageError), got %d", code)
	}
}

// ---- dispatch: worktree collision ------------------------------------------

func TestDispatch_WorktreeCollision(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)
	fg := &fakeGit{
		repoRootFn:       func(dir string) (string, error) { return repoDir, nil },
		worktreeExistsFn: func(repoRoot, wtPath string) bool { return true }, // collision
	}
	ctx, _, stderr := makeCtx(&fakeBD{}, home)
	cmd := &dispatchKong{
		Problem:  "Some work",
		Repo:     repoDir,
		NoLaunch: true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error for collision, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	msg := stderr.String()
	if !strings.Contains(msg, "worktree already exists") {
		t.Errorf("expected collision message, got: %s", msg)
	}
	if !strings.Contains(msg, "pick a different --slug") {
		t.Errorf("expected pick-a-different-slug hint, got: %s", msg)
	}
}

// ---- dispatch: registration failure removes the worktree -------------------

// TestDispatch_RegisterFailure_RemovesWorktree verifies FIX 2: when bd create
// fails after the worktree was created, dispatch removes the worktree so the
// command is cleanly retryable.
func TestDispatch_RegisterFailure_RemovesWorktree(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	var removedRepo, removedWt string
	setupRan := false
	fg := &fakeGit{
		repoRootFn: func(dir string) (string, error) { return repoDir, nil },
		removeWorktreeFn: func(repoRoot, wtPath string) error {
			removedRepo = repoRoot
			removedWt = wtPath
			return nil
		},
	}

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			return fmt.Errorf("bd create: simulated failure")
		},
	}

	ctx, _, _ := makeCtx(fbd, home)
	cmd := &dispatchKong{
		Problem:  "Some feature",
		Slug:     "some-feature",
		Repo:     repoDir,
		NoLaunch: true,
		git:      fg,
		setup: func(_ *cli.Context, _ string) (worktreeSetupResult, error) {
			setupRan = true
			return worktreeSetupResult{}, nil
		},
		launch: func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error from registration failure, got nil")
	}
	if !strings.Contains(err.Error(), "register initiative") {
		t.Errorf("error missing 'register initiative': %v", err)
	}

	// Worktree removal must have been invoked.
	expectedWt := filepath.Join(home+"-worktrees", "some-feature")
	if removedWt != expectedWt {
		t.Errorf("RemoveWorktree called with wt=%q, want %q", removedWt, expectedWt)
	}
	if removedRepo != repoDir {
		t.Errorf("RemoveWorktree called with repo=%q, want %q", removedRepo, repoDir)
	}
	if !setupRan {
		t.Error("successful setup must run before an independent registration failure")
	}
}

// ---- dispatch: missing --problem -------------------------------------------

func TestDispatch_MissingProblem(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	// Problem: "" slugifies to "" → UsageError (exit 2).
	cmd := &dispatchKong{
		Problem:  "",
		Repo:     repoDir,
		NoLaunch: true,
		git:      &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }},
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected UsageError, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// ---- dispatch: --id-only output --------------------------------------------

func TestDispatch_IDOnly(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-idonly1"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, stdout, _ := makeCtx(fbd, home)
	cmd := &dispatchKong{
		Problem:  "some work",
		Repo:     repoDir,
		NoLaunch: true,
		IDOnly:   true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	err := cmd.Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// --id-only with an empty (zero-value) issue id should just print a blank
	// line (the zero value of issue.ID). The key assertion is that the full
	// report block is NOT present.
	out := stdout.String()
	if strings.Contains(out, "worktree:") {
		t.Errorf("--id-only should not print worktree line, got:\n%s", out)
	}
	if strings.Contains(out, "base_branch:") {
		t.Errorf("--id-only should not print base_branch line, got:\n%s", out)
	}
}

// ---- dispatch: registry body contains worktree line -----------------------

func TestDispatch_RegistryBodyWorktreeLine(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	expectedSlug := "my-work"
	expectedWt := filepath.Join(home+"-worktrees", expectedSlug)

	var gotBody string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			for _, a := range args {
				if strings.HasPrefix(a, "--body-file=") {
					path := strings.TrimPrefix(a, "--body-file=")
					b, err := os.ReadFile(path)
					if err == nil {
						gotBody = string(b)
					}
				}
			}
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-body1"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(fbd, home)
	cmd := &dispatchKong{
		Problem:  "My work",
		Slug:     expectedSlug,
		Repo:     repoDir,
		NoLaunch: true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	err := cmd.Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantLine := "worktree: " + expectedWt
	if !strings.Contains(gotBody, wantLine) {
		t.Errorf("body missing %q:\n%s", wantLine, gotBody)
	}
	if !strings.Contains(gotBody, "mode: bg") {
		t.Errorf("body missing 'mode: bg':\n%s", gotBody)
	}
}

// ---- dispatch: --standby ---------------------------------------------------

// TestDispatch_Standby_WritesMarker verifies that --standby appends the frozen
// "standby: true" line to the initiative body, positioned immediately after
// "mode: bg" (contract: agent-teams-yl6t.1).
func TestDispatch_Standby_WritesMarker(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	var gotBody string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			for _, a := range args {
				if strings.HasPrefix(a, "--body-file=") {
					path := strings.TrimPrefix(a, "--body-file=")
					b, err := os.ReadFile(path)
					if err == nil {
						gotBody = string(b)
					}
				}
			}
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-standby1"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(fbd, home)

	cmd := &dispatchKong{
		Problem:  "Standby work",
		Slug:     "standby-work",
		Repo:     repoDir,
		NoLaunch: true,
		Standby:  true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	modeIdx := strings.Index(gotBody, "mode: bg\n")
	standbyIdx := strings.Index(gotBody, "standby: true\n")
	if modeIdx < 0 {
		t.Fatalf("body missing 'mode: bg':\n%s", gotBody)
	}
	if standbyIdx < 0 {
		t.Fatalf("body missing 'standby: true':\n%s", gotBody)
	}
	if standbyIdx != modeIdx+len("mode: bg\n") {
		t.Errorf("'standby: true' must appear immediately after 'mode: bg':\n%s", gotBody)
	}
}

// TestDispatch_NoStandby_OmitsMarker verifies that without --standby, the body
// contains no "standby" line at all (never "standby: false").
func TestDispatch_NoStandby_OmitsMarker(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	var gotBody string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			for _, a := range args {
				if strings.HasPrefix(a, "--body-file=") {
					path := strings.TrimPrefix(a, "--body-file=")
					b, err := os.ReadFile(path)
					if err == nil {
						gotBody = string(b)
					}
				}
			}
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-nostandby1"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(fbd, home)

	cmd := &dispatchKong{
		Problem:  "Normal work",
		Slug:     "normal-work",
		Repo:     repoDir,
		NoLaunch: true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(gotBody, "standby") {
		t.Errorf("body must not contain any 'standby' line when --standby is omitted:\n%s", gotBody)
	}
}

// ---- dispatch: --body-file appends context after schema lines ---------------

func TestDispatch_BodyFile_AppendsContext(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	expectedSlug := "add-feature"
	expectedWt := filepath.Join(home+"-worktrees", expectedSlug)

	// Write context to a temp file.
	ctxFile := filepath.Join(t.TempDir(), "context.txt")
	contextText := "CONTEXT FROM ERIC\nThis is the full framing.\nKey constraint: must be fast."
	if err := os.WriteFile(ctxFile, []byte(contextText), 0o600); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	var gotBody string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			for _, a := range args {
				if strings.HasPrefix(a, "--body-file=") {
					path := strings.TrimPrefix(a, "--body-file=")
					b, err := os.ReadFile(path)
					if err == nil {
						gotBody = string(b)
					}
				}
			}
			if issue, ok := dst.(*bd.Issue); ok {
				return json.Unmarshal([]byte(`{"id":"at-bf1","title":"Add feature"}`), issue)
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(fbd, home)
	cmd := &dispatchKong{
		Problem:  "Add feature",
		Slug:     expectedSlug,
		Repo:     repoDir,
		NoLaunch: true,
		BodyFile: ctxFile,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	err := cmd.Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Schema lines must come first.
	worktreeLine := "worktree: " + expectedWt
	worktreeIdx := strings.Index(gotBody, worktreeLine)
	contextIdx := strings.Index(gotBody, contextText)

	if worktreeIdx < 0 {
		t.Errorf("body missing worktree line %q:\n%s", worktreeLine, gotBody)
	}
	if contextIdx < 0 {
		t.Errorf("body missing context text:\n%s", gotBody)
	}
	if worktreeIdx >= 0 && contextIdx >= 0 && worktreeIdx > contextIdx {
		t.Errorf("schema worktree line must appear before context block; worktree at %d, context at %d", worktreeIdx, contextIdx)
	}
}

// TestDispatch_BodyFile_Missing errors when the file does not exist.
func TestDispatch_BodyFile_Missing(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	cmd := &dispatchKong{
		Problem:  "Some work",
		Repo:     repoDir,
		NoLaunch: true,
		BodyFile: "/no/such/file/ever.txt",
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error for missing --body-file, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2 (UsageError), got %d", code)
	}
}

// TestDispatch_BodyFile_Missing_RemovesWorktree verifies that when --body-file
// cannot be read (worktree already created), the worktree is cleaned up before
// returning the usage error.
func TestDispatch_BodyFile_Missing_RemovesWorktree(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	var removedWt string
	fg := &fakeGit{
		repoRootFn: func(dir string) (string, error) { return repoDir, nil },
		removeWorktreeFn: func(repoRoot, wtPath string) error {
			removedWt = wtPath
			return nil
		},
	}
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	cmd := &dispatchKong{
		Problem:  "Some work",
		Slug:     "some-work",
		Repo:     repoDir,
		NoLaunch: true,
		BodyFile: "/no/such/file/ever.txt",
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error for missing --body-file, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2 (UsageError), got %d", code)
	}

	expectedWt := filepath.Join(home+"-worktrees", "some-work")
	if removedWt != expectedWt {
		t.Errorf("RemoveWorktree called with wt=%q, want %q", removedWt, expectedWt)
	}
}

// TestDispatch_EmptyID_RemovesWorktree verifies that when bd create returns
// JSON with an empty id, dispatch removes the just-created worktree and returns
// an error — no launch is attempted, no initiative_id line is printed.
func TestDispatch_EmptyID_RemovesWorktree(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	var removedWt string
	fg := &fakeGit{
		repoRootFn: func(dir string) (string, error) { return repoDir, nil },
		removeWorktreeFn: func(repoRoot, wtPath string) error {
			removedWt = wtPath
			return nil
		},
	}

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			// Return JSON with an empty id field.
			if issue, ok := dst.(*bd.Issue); ok {
				return json.Unmarshal([]byte(`{"id":"","title":"Some work"}`), issue)
			}
			return nil
		},
	}

	ctx, stdout, _ := makeCtx(fbd, home)
	cmd := &dispatchKong{
		Problem: "Some work",
		Slug:    "some-work",
		Repo:    repoDir,
		// NoLaunch intentionally false: the error must fire before any launch.
		git:    fg,
		launch: func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error for empty issue id, got nil")
	}
	if !strings.Contains(err.Error(), "bd create returned no id") {
		t.Errorf("error missing 'bd create returned no id': %v", err)
	}

	expectedWt := filepath.Join(home+"-worktrees", "some-work")
	if removedWt != expectedWt {
		t.Errorf("RemoveWorktree called with wt=%q, want %q", removedWt, expectedWt)
	}

	// No initiative_id line must have been printed.
	if strings.Contains(stdout.String(), "initiative_id:") {
		t.Errorf("stdout must not contain 'initiative_id:' on empty-id error:\n%s", stdout.String())
	}
}

// TestDispatch_BodyFile_Omitted verifies that omitting --body-file produces the
// schema-only body unchanged (backward-compat).
func TestDispatch_BodyFile_Omitted(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	expectedSlug := "schema-only"
	expectedWt := filepath.Join(home+"-worktrees", expectedSlug)

	var gotBody string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			for _, a := range args {
				if strings.HasPrefix(a, "--body-file=") {
					path := strings.TrimPrefix(a, "--body-file=")
					b, err := os.ReadFile(path)
					if err == nil {
						gotBody = string(b)
					}
				}
			}
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-omit1"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(fbd, home)
	cmd := &dispatchKong{
		Problem:  "Schema only",
		Slug:     expectedSlug,
		Repo:     repoDir,
		NoLaunch: true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	err := cmd.Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Schema lines must be present.
	if !strings.Contains(gotBody, "worktree: "+expectedWt) {
		t.Errorf("body missing worktree line:\n%s", gotBody)
	}
	if !strings.Contains(gotBody, "mode: bg") {
		t.Errorf("body missing 'mode: bg':\n%s", gotBody)
	}
	// No extra blank line at the end from a missing body-file.
	if strings.Contains(gotBody, "\n\n") {
		t.Errorf("schema-only body should not have double newline:\n%q", gotBody)
	}
}

// ---- dispatch: --body-file field-redefinition warning ----------------------
//
// dispatch warns to stderr when a --body-file line collides with a routing
// field the header already wrote. The rule is initiative.Fields.CollisionsIn's
// — the SAME rule initiative.Of reads by, not a second one that happens to
// agree (agent-teams-ully.7). The redefining lines below are built by
// concatenation rather than as a literal line in a raw string block, per the
// bead's own hazard note: a source line literally starting with a routing key
// would reproduce the bug this change guards against.

// TestDispatch_BodyFileWarnsOnFieldRedefinition covers case 1: a body-file
// line that redefines the "repo" field the header wrote produces a stderr
// warning naming the 1-based line number and the field, and says the line is
// IGNORED — under first-wins the header's value is the one that survives, so
// the old "last-wins — this value replaces the header's" wording was false.
func TestDispatch_BodyFileWarnsOnFieldRedefinition(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	redefineLine := "rep" + "o: /bogus/other/repo"
	bodyContent := "Some prose framing the task.\n" + redefineLine + "\nMore prose after.\n"

	bfPath := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(bfPath, []byte(bodyContent), 0o600); err != nil {
		t.Fatalf("write body file: %v", err)
	}

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-warn1"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, stderr := makeCtx(fbd, home)
	cmd := &dispatchKong{
		Problem:  "Some work",
		Slug:     "some-work",
		Repo:     repoDir,
		NoLaunch: true,
		BodyFile: bfPath,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stderr.String()
	if !strings.Contains(got, "line 2") {
		t.Errorf("stderr missing 1-based line number 2:\n%s", got)
	}
	if !strings.Contains(got, `"repo"`) {
		t.Errorf("stderr missing redefined field name %q:\n%s", "repo", got)
	}
	if !strings.Contains(got, redefineLine) {
		t.Errorf("stderr missing offending line text:\n%s", got)
	}
	if !strings.Contains(got, "IGNORED") {
		t.Errorf("stderr must say the body-file line is IGNORED under first-wins:\n%s", got)
	}
	if strings.Contains(got, "last-wins") {
		t.Errorf("stderr still claims last-wins, which is false under the frozen rule:\n%s", got)
	}
}

// TestDispatch_MultiLineProblemRejectedBeforeWorktree pins that a --problem
// carrying a line break is refused as a USAGE error, and refused early enough
// that no worktree is ever created. The second line of such a value would sit
// in the routing header looking like a canonical field, which is the exact
// shape of the bug this initiative closes. initiative.New would also reject
// it, but only after step 6 has already built a worktree that would then have
// to be unwound.
func TestDispatch_MultiLineProblemRejectedBeforeWorktree(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	addCalled := false
	fg := &fakeGit{
		repoRootFn: func(dir string) (string, error) { return repoDir, nil },
		addWorktreeFn: func(_, _, _, _ string) error {
			addCalled = true
			return nil
		},
	}
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	cmd := &dispatchKong{
		Problem:  "Fix the thing\n" + "wor" + "ktree: /bogus/injected",
		Slug:     "fix-the-thing",
		Repo:     repoDir,
		NoLaunch: true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected an error for a multi-line --problem")
	}
	if !strings.Contains(err.Error(), "single line") {
		t.Errorf("error should tell the caller --problem must be a single line, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--body-file") {
		t.Errorf("error should point the caller at --body-file, got: %v", err)
	}
	if addCalled {
		t.Error("worktree must not be created before --problem is validated")
	}
}

// TestDispatch_BodyFileNoWarningWithoutCollision covers case 2: a body file
// with no field-shaped line produces no stderr output at all.
func TestDispatch_BodyFileNoWarningWithoutCollision(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	bodyContent := "Just some framing prose.\nNo colons here that matter.\nDone.\n"
	bfPath := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(bfPath, []byte(bodyContent), 0o600); err != nil {
		t.Fatalf("write body file: %v", err)
	}

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-warn2"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, stderr := makeCtx(fbd, home)
	cmd := &dispatchKong{
		Problem:  "Some work",
		Slug:     "some-work",
		Repo:     repoDir,
		NoLaunch: true,
		BodyFile: bfPath,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stderr.String() != "" {
		t.Errorf("expected no stderr output, got:\n%s", stderr.String())
	}
}

// TestDispatch_BodyFileNoWarningOnHyphenPrefixedLine covers case 3, the one
// that matters most: a briefing line beginning with a hyphen and a space
// before the capitalised word parses to a DIFFERENT key ("- repo", not
// "repo") and must NOT warn. This is the exact one-character reason sibling
// initiative at-ig53 escaped the original bug; a warning firing here is a
// false positive on the most common briefing style.
func TestDispatch_BodyFileNoWarningOnHyphenPrefixedLine(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	// Safe to write literally: this line does not begin with a routing-field
	// word followed by a colon — it begins with "- ".
	bodyContent := "- Repo: use the conventional commit style for this one\nOther prose.\n"
	bfPath := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(bfPath, []byte(bodyContent), 0o600); err != nil {
		t.Fatalf("write body file: %v", err)
	}

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-warn3"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, stderr := makeCtx(fbd, home)
	cmd := &dispatchKong{
		Problem:  "Some work",
		Slug:     "some-work",
		Repo:     repoDir,
		NoLaunch: true,
		BodyFile: bfPath,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stderr.String() != "" {
		t.Errorf("hyphen-prefixed line must not warn (false positive), got:\n%s", stderr.String())
	}
}

// TestDispatch_BodyFileNoWarningOnWrongCaseLine covers case 4, INVERTED by
// agent-teams-ully.7. It previously asserted the warning folded case, because
// the warning mirrored a lenient reader that also folded case. The frozen
// matching rule (internal/initiative/doc.go, frozen item 1) does not fold
// case: a wrong-case key is not a field line at all, so no reader will ever
// take this value and warning about it is a false positive. The warning's rule
// and the reader's rule are now one rule, so this shape must stay silent.
func TestDispatch_BodyFileNoWarningOnWrongCaseLine(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	bodyContent := "BRAN" + "CH: totally-wrong-branch" + "\n"
	bfPath := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(bfPath, []byte(bodyContent), 0o600); err != nil {
		t.Fatalf("write body file: %v", err)
	}

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-warn4"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, stderr := makeCtx(fbd, home)
	cmd := &dispatchKong{
		Problem:  "Some work",
		Slug:     "some-work",
		Repo:     repoDir,
		NoLaunch: true,
		BodyFile: bfPath,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stderr.String() != "" {
		t.Errorf("wrong-case line must not warn (false positive under the frozen rule), got:\n%s", stderr.String())
	}
}

// ---- new-initiative: arg validation ----------------------------------------

func TestNewInitiative_MissingDirectory(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}
	// Dir: "" triggers the empty-directory UsageError in newInitiativeKong.Run.
	cmd := &newInitiativeKong{}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected UsageError, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestNewInitiative_MissingDRIArg(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}
	cmd := &newInitiativeKong{Dir: dir}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected UsageError for missing dri-arg, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// ---- bgSessionArgs: argv shape and memory-routing flag ---------------------

func TestBGSessionArgs_ContainsAppendSystemPrompt(t *testing.T) {
	args := bgSessionArgs("my-session", "/dri at-abc123", "", "", "", "", "{}", "")

	// Locate --append-system-prompt and verify it is immediately followed by
	// the canonical driSystemPromptAppend const (memoryRoutingRule +
	// driGuardrails, concatenated into one string — agent-teams-kxlb.2). A
	// "/dri " prompt is what a true DRI launch always carries
	// (launchBGSession prepends it), which is what earns the guardrail
	// digest — see TestBGSessionArgs_ReviewPromptOmitsGuardrails for the
	// negative case.
	found := false
	for i, a := range args {
		if a == "--append-system-prompt" {
			if i+1 >= len(args) {
				t.Fatal("--append-system-prompt has no following value in argv")
			}
			val := args[i+1]
			if val != driSystemPromptAppend {
				t.Errorf("value after --append-system-prompt does not match driSystemPromptAppend const:\ngot:  %q\nwant: %q", val, driSystemPromptAppend)
			}
			if !strings.Contains(val, "ateam learn") {
				t.Errorf("append-system-prompt missing 'ateam learn': %q", val)
			}
			if !strings.Contains(val, "Never MEMORY.md") {
				t.Errorf("append-system-prompt missing 'Never MEMORY.md': %q", val)
			}
			if !strings.Contains(val, "DRI HARD GUARDRAILS") {
				t.Errorf("append-system-prompt missing DRI guardrail digest: %q", val)
			}
			if !strings.Contains(val, "re-invoke it via the Skill tool") {
				t.Errorf("append-system-prompt missing the re-invoke-the-skill floor bullet: %q", val)
			}
			if !strings.Contains(val, "Never merge without explicit human confirmation") {
				t.Errorf("append-system-prompt missing the never-merge-without-confirmation guardrail: %q", val)
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("argv missing --append-system-prompt; got: %v", args)
	}
}

// TestBGSessionArgs_ReviewPromptOmitsGuardrails is the negative case for the
// "/dri " gate above: a non-DRI bg session launched with a custom
// --launch-prompt (e.g. review-pr, which hardcodes role "dri" — route.go ->
// launchRaw — but never prefixes its prompt with "/dri ") must still get
// memoryRoutingRule (role-agnostic, always wanted) but NOT driGuardrails
// (DRI-orchestration rules that don't apply and would just add per-turn
// bloat on a session that isn't a DRI).
func TestBGSessionArgs_ReviewPromptOmitsGuardrails(t *testing.T) {
	args := bgSessionArgs("my-session", "/agent-teams:review-pr at-x", "", "", "dri", "at-x", "{}", "")

	found := false
	for i, a := range args {
		if a == "--append-system-prompt" {
			if i+1 >= len(args) {
				t.Fatal("--append-system-prompt has no following value in argv")
			}
			val := args[i+1]
			if val != memoryRoutingRule {
				t.Errorf("value after --append-system-prompt for a non-/dri prompt = %q, want memoryRoutingRule alone: %q", val, memoryRoutingRule)
			}
			if strings.Contains(val, "DRI HARD GUARDRAILS") {
				t.Errorf("append-system-prompt for a review-pr (non-DRI) session must not contain the DRI guardrail digest: %q", val)
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("argv missing --append-system-prompt; got: %v", args)
	}
}

func TestBGSessionArgs_StandardArgsPresent(t *testing.T) {
	name := "my-session"
	// bgSessionArgs now takes a raw prompt; the /dri prefix is added by the
	// caller (launchBGSession), not by bgSessionArgs itself.
	prompt := "/dri at-abc123"
	args := bgSessionArgs(name, prompt, "", "", "", "", "{}", "")

	// Required flags and their values must be present in correct positions.
	checks := []struct {
		flag string
		val  string
	}{
		{"--bg", ""},
		{"-n", name},
		{"--model", "claude-opus-4-8"},
		{"--permission-mode", "bypassPermissions"},
	}
	for _, c := range checks {
		if c.val == "" {
			found := false
			for _, a := range args {
				if a == c.flag {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("argv missing flag %q; got: %v", c.flag, args)
			}
			continue
		}
		found := false
		for i, a := range args {
			if a == c.flag && i+1 < len(args) && args[i+1] == c.val {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("argv missing %q %q pair; got: %v", c.flag, c.val, args)
		}
	}

	// Prompt must be the last element exactly as passed (bgSessionArgs does not
	// add any /dri prefix; that is the caller's responsibility).
	last := args[len(args)-1]
	if last != prompt {
		t.Errorf("last argv element = %q, want %q", last, prompt)
	}
}

// TestBGSessionArgs_ModelOverride verifies that a non-empty model argument
// replaces the "claude-opus-4-8" default in the --model flag.
func TestBGSessionArgs_ModelOverride(t *testing.T) {
	args := bgSessionArgs("my-session", "/some-prompt", "sonnet", "", "", "", "{}", "")

	found := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) {
			if args[i+1] != "sonnet" {
				t.Errorf("--model value = %q, want %q", args[i+1], "sonnet")
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("argv missing --model flag; got: %v", args)
	}
	// The default "claude-opus-4-8" must NOT appear anywhere when overridden.
	for _, a := range args {
		if a == "claude-opus-4-8" {
			t.Errorf("argv should not contain default \"claude-opus-4-8\" when model override is set; got: %v", args)
		}
	}
}

// TestBGSessionArgs_EmptyModelDefaultsToOpus verifies that an empty model
// argument falls back to the "claude-opus-4-8" default.
func TestBGSessionArgs_EmptyModelDefaultsToOpus(t *testing.T) {
	args := bgSessionArgs("my-session", "/some-prompt", "", "", "", "", "{}", "")

	found := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "claude-opus-4-8" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("argv missing --model claude-opus-4-8 pair for empty override; got: %v", args)
	}
}

// TestBGSessionArgs_AdvisorEnabled verifies the enabled branch: model
// "sonnet" and advisor "opus" both appear as flag/value pairs in argv.
// Per contract decision 7 (agent-teams-wvx2.1), this must be asserted at the
// bgSessionArgs level (pure, no env reads).
func TestBGSessionArgs_AdvisorEnabled(t *testing.T) {
	args := bgSessionArgs("my-session", "/dri at-abc123", "sonnet", "opus", "", "", "{}", "")

	hasPair := func(flag, val string) bool {
		for i, a := range args {
			if a == flag && i+1 < len(args) && args[i+1] == val {
				return true
			}
		}
		return false
	}
	if !hasPair("--model", "sonnet") {
		t.Errorf("argv missing \"--model\" \"sonnet\" pair; got: %v", args)
	}
	if !hasPair("--advisor", "opus") {
		t.Errorf("argv missing \"--advisor\" \"opus\" pair; got: %v", args)
	}
}

// TestBGSessionArgs_AdvisorDisabled verifies the disabled/unset branch: model
// defaults to "claude-opus-4-8" and no "--advisor" flag appears anywhere in argv.
func TestBGSessionArgs_AdvisorDisabled(t *testing.T) {
	args := bgSessionArgs("my-session", "/dri at-abc123", "", "", "", "", "{}", "")

	hasPair := func(flag, val string) bool {
		for i, a := range args {
			if a == flag && i+1 < len(args) && args[i+1] == val {
				return true
			}
		}
		return false
	}
	if !hasPair("--model", "claude-opus-4-8") {
		t.Errorf("argv missing \"--model\" \"claude-opus-4-8\" pair; got: %v", args)
	}
	for _, a := range args {
		if a == "--advisor" {
			t.Errorf("argv must not contain \"--advisor\" when advisor is disabled; got: %v", args)
		}
	}
}

// ---- driAdvisorSettings: config.toml-reading helper ------------------------

// TestDriAdvisorSettings verifies driAdvisorSettings(home) returns ("sonnet",
// claudeDriModel) only when config.toml's use_advisors key is true, and
// (claudeDriModel, "") otherwise — including when config.toml is absent
// entirely, which must resolve to the hardcoded defaults (claude-opus-4-8,
// no advisor) with NO env involved. Cases with an explicit non-default
// claude_dri_model ("haiku") in both the advisor-on and advisor-off branches
// prove the config key actually threads through, not just the default.
func TestDriAdvisorSettings(t *testing.T) {
	cases := []struct {
		name        string
		config      string // "" means no config.toml file at all
		wantModel   string
		wantAdvisor string
	}{
		{name: "absent_config_defaults", config: "", wantModel: "claude-opus-4-8", wantAdvisor: ""},
		{name: "advisors_true_default_model", config: "use_advisors = true\n", wantModel: "sonnet", wantAdvisor: "claude-opus-4-8"},
		{name: "advisors_false_default_model", config: "use_advisors = false\n", wantModel: "claude-opus-4-8", wantAdvisor: ""},
		{name: "advisors_true_nondefault_model", config: "use_advisors = true\nclaude_dri_model = \"haiku\"\n", wantModel: "sonnet", wantAdvisor: "haiku"},
		{name: "advisors_false_nondefault_model", config: "use_advisors = false\nclaude_dri_model = \"haiku\"\n", wantModel: "haiku", wantAdvisor: ""},
		{name: "model_only_no_advisors_key", config: "claude_dri_model = \"haiku\"\n", wantModel: "haiku", wantAdvisor: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.config != "" {
				if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			gotModel, gotAdvisor, err := driAdvisorSettings(home)
			if err != nil {
				t.Fatalf("driAdvisorSettings(%q) unexpected error: %v", home, err)
			}
			if gotModel != tc.wantModel || gotAdvisor != tc.wantAdvisor {
				t.Errorf("driAdvisorSettings(%q) = (%q, %q), want (%q, %q)", home, gotModel, gotAdvisor, tc.wantModel, tc.wantAdvisor)
			}
		})
	}
}

// TestDriAdvisorSettings_InvalidConfigPropagatesError verifies a malformed
// config.toml surfaces as an error rather than silently falling back to
// defaults — readers must never swallow a reader error (agent-teams-qox8.2).
func TestDriAdvisorSettings_InvalidConfigPropagatesError(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("not valid = ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := driAdvisorSettings(home); err == nil {
		t.Fatal("driAdvisorSettings: want error for invalid config.toml, got nil")
	}
}

// ---- driAutoCompactWindow: config.toml-reading helper ----------------------

// TestDriAutoCompactWindow verifies driAutoCompactWindow(home) resolves
// config.toml's auto_compact_window key (a positive integer token count) to
// its decimal string form, and returns "" when the key or the file is
// absent — the unchanged empty-default contract. No env var is read at all:
// the free-form CLI shorthand ("500k", "auto") the old env-backed helper
// passed through verbatim no longer applies, since config.toml only ever
// carries a plain positive int64.
func TestDriAutoCompactWindow(t *testing.T) {
	cases := []struct {
		name   string
		config string // "" means no config.toml file at all
		want   string
	}{
		{name: "absent_config", config: "", want: ""},
		{name: "configured", config: "auto_compact_window = 450000\n", want: "450000"},
		{name: "regression_witness_300k", config: "auto_compact_window = 300000\n", want: "300000"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.config != "" {
				if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			got, err := driAutoCompactWindow(home)
			if err != nil {
				t.Fatalf("driAutoCompactWindow(%q) unexpected error: %v", home, err)
			}
			if got != tc.want {
				t.Errorf("driAutoCompactWindow(%q) = %q, want %q", home, got, tc.want)
			}
		})
	}
}

// TestDriAutoCompactWindow_InvalidConfigPropagatesError verifies a malformed
// config.toml surfaces as an error rather than silently resolving to "".
func TestDriAutoCompactWindow_InvalidConfigPropagatesError(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("not valid = ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := driAutoCompactWindow(home); err == nil {
		t.Fatal("driAutoCompactWindow: want error for invalid config.toml, got nil")
	}
}

// TestDriAutoCompactWindow_RegressionWitness_ArgvCarriesFormattedTokens is the
// end-to-end regression witness (agent-teams-qox8.2): a bare, no-env temp
// home whose config.toml sets auto_compact_window = 300000 must produce
// "--autocompact 300000" (strconv.FormatInt of the configured int) in BOTH
// the dispatch producer's argv (bgSessionArgs) and the steward producer's
// argv (stewardLaunchArgs) — proving config.toml alone, with no plugin-option
// env var present, drives the flag on a bare-terminal launch.
func TestDriAutoCompactWindow_RegressionWitness_ArgvCarriesFormattedTokens(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("auto_compact_window = 300000\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	window, err := driAutoCompactWindow(home)
	if err != nil {
		t.Fatalf("driAutoCompactWindow(%q) unexpected error: %v", home, err)
	}
	if window != "300000" {
		t.Fatalf("driAutoCompactWindow(%q) = %q, want %q", home, window, "300000")
	}

	dispatchArgs := bgSessionArgs("sess", "/dri at-abc", "", "", "", "", "{}", window)
	if !argvContainsSequence(dispatchArgs, "--autocompact", "300000") {
		t.Errorf("bgSessionArgs argv missing \"--autocompact 300000\": %v", dispatchArgs)
	}

	stewardArgs := stewardLaunchArgs(window)
	if !argvContainsSequence(stewardArgs, "--autocompact", "300000") {
		t.Errorf("stewardLaunchArgs argv missing \"--autocompact 300000\": %v", stewardArgs)
	}
}

// argvContainsSequence reports whether args contains flag immediately
// followed by value at some position i, i+1.
func argvContainsSequence(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

// ---- parseAutoCompactWindowTokens: value parser ---------------------------

// TestParseAutoCompactWindowTokens covers every accepted form the claude
// CLI's own --autocompact flag documents (plain integer, k/m suffix, bare
// 100-1000 thousands shorthand, the literal "auto") plus the fail-closed
// cases (empty, unparseable) that must all resolve to ok=false ("no window").
func TestParseAutoCompactWindowTokens(t *testing.T) {
	cases := []struct {
		name       string
		value      string
		wantTokens int
		wantOK     bool
	}{
		{name: "k_suffix", value: "300k", wantTokens: 300000, wantOK: true},
		{name: "bare_thousands_shorthand", value: "200", wantTokens: 200000, wantOK: true},
		{name: "plain_integer", value: "300000", wantTokens: 300000, wantOK: true},
		{name: "m_suffix", value: "1m", wantTokens: 1000000, wantOK: true},
		{name: "literal_auto", value: "auto", wantOK: false},
		{name: "auto_uppercase", value: "AUTO", wantOK: false},
		{name: "empty", value: "", wantOK: false},
		{name: "zero", value: "0", wantOK: false},
		{name: "negative", value: "-1", wantOK: false},
		{name: "overflow", value: "9223372036854775807m", wantOK: false},
		{name: "garbage", value: "banana", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotTokens, gotOK := parseAutoCompactWindowTokens(tc.value)
			if gotOK != tc.wantOK {
				t.Fatalf("parseAutoCompactWindowTokens(%q) ok = %v, want %v", tc.value, gotOK, tc.wantOK)
			}
			if gotOK && gotTokens != tc.wantTokens {
				t.Errorf("parseAutoCompactWindowTokens(%q) tokens = %d, want %d", tc.value, gotTokens, tc.wantTokens)
			}
		})
	}
}

func TestParseAutoCompactWindowValueDistinguishesAutoFromInvalid(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		want      int64
		wantAuto  bool
		wantError bool
	}{
		{name: "plain tokens", value: "300000", want: 300000},
		{name: "thousands shorthand", value: "300", want: 300000},
		{name: "k suffix", value: "300k", want: 300000},
		{name: "m suffix", value: "1M", want: 1000000},
		{name: "auto", value: " AUTO ", wantAuto: true},
		{name: "empty", value: " ", wantError: true},
		{name: "wrong suffix", value: "300g", wantError: true},
		{name: "zero", value: "0", wantError: true},
		{name: "negative", value: "-300k", wantError: true},
		{name: "parse overflow", value: "9223372036854775808", wantError: true},
		{name: "multiply overflow", value: "9223372036854775807m", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, automatic, err := parseAutoCompactWindowValue(tt.value)
			if (err != nil) != tt.wantError {
				t.Fatalf("parseAutoCompactWindowValue(%q) error = %v, wantError %v", tt.value, err, tt.wantError)
			}
			if got != tt.want || automatic != tt.wantAuto {
				t.Fatalf("parseAutoCompactWindowValue(%q) = %d, %v; want %d, %v", tt.value, got, automatic, tt.want, tt.wantAuto)
			}
		})
	}
}

func TestNewInitiative_MissingClaude(t *testing.T) {
	// Only run when 'claude' is NOT in PATH.
	if _, err := exec.LookPath("claude"); err == nil {
		t.Skip("claude is in PATH; skipping missing-claude test")
	}
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}
	cmd := &newInitiativeKong{Dir: dir, DriArgs: []string{"some-initiative-id"}}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected DepError, got nil")
	}
	if code := cli.ExitCode(err); code != 3 {
		t.Errorf("expected exit 3 (DepError), got %d", code)
	}
}

func TestNewInitiative_NonExistentDirectory(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}
	cmd := &newInitiativeKong{Dir: "/no/such/directory/exists/ever", DriArgs: []string{"arg"}}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected UsageError for non-existent dir, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestNewInitiative_RegularFileNotDirectory(t *testing.T) {
	// Create a real file (not a directory) and pass it as the <directory> arg.
	f, err := os.CreateTemp(t.TempDir(), "not-a-dir-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()

	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}
	cmd := &newInitiativeKong{Dir: f.Name(), DriArgs: []string{"some-initiative"}}

	runErr := cmd.Run(ctx)
	if runErr == nil {
		t.Fatal("expected UsageError for regular file, got nil")
	}
	if code := cli.ExitCode(runErr); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
	if !strings.Contains(runErr.Error(), "not a directory") {
		t.Errorf("expected 'not a directory' in error, got: %v", runErr)
	}
}

// ---- kong structs: core-path tests -----------------------------------------

// TestDispatchKong_FlagsRoundtrip verifies that dispatchKong.Run passes all
// seven flags through to the underlying dispatchCommand correctly.
func TestDispatchKong_FlagsRoundtrip(t *testing.T) {
	repoDir := newEnabledRepoDir(t)
	home := t.TempDir()

	var capturedSlug string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-kong1"
			}
			return nil
		},
	}
	fg := &fakeGit{
		repoRootFn: func(dir string) (string, error) { return repoDir, nil },
		addWorktreeFn: func(repoRoot, wtPath, branch, base string) error {
			capturedSlug = branch
			return nil
		},
	}

	ctx, stdout, _ := makeCtx(fbd, home)
	cmd := &dispatchKong{
		Problem:    "Add feature X",
		Repo:       repoDir,
		BaseBranch: "develop",
		Slug:       "add-feature-x",
		IDOnly:     false,
		NoLaunch:   true,
		git:        fg,
		launch:     func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedSlug != "add-feature-x" {
		t.Errorf("slug = %q, want %q", capturedSlug, "add-feature-x")
	}
	out := stdout.String()
	if !strings.Contains(out, "base_branch: develop") {
		t.Errorf("stdout missing 'base_branch: develop':\n%s", out)
	}
	if !strings.Contains(out, "initiative_id: at-kong1") {
		t.Errorf("stdout missing 'initiative_id: at-kong1':\n%s", out)
	}
}

// TestDispatchKong_IDOnly verifies --id-only routes through dispatchKong correctly.
func TestDispatchKong_IDOnly(t *testing.T) {
	repoDir := newEnabledRepoDir(t)
	home := t.TempDir()

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-idonly-kong"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, stdout, _ := makeCtx(fbd, home)

	cmd := &dispatchKong{
		Problem:  "Work item",
		Repo:     repoDir,
		IDOnly:   true,
		NoLaunch: true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if strings.Contains(out, "worktree:") {
		t.Errorf("--id-only must not print worktree line:\n%s", out)
	}
	if !strings.Contains(out, "at-idonly-kong") {
		t.Errorf("--id-only must print the id:\n%s", out)
	}
}

// TestNewInitiativeKong_DriArgJoined verifies that multiple DriArgs words are
// joined as a single space-separated string before being passed to launchBGSession.
func TestNewInitiativeKong_DriArgJoined(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}

	// Inject a stub launcher so the test NEVER execs a real `claude --bg`
	// session (an un-stubbed launch leaks a detached session into dir, which
	// t.TempDir() then deletes — orphaning it; see agent-teams-wwyd).
	var gotDir, gotArg, gotRole, gotInitiative string
	cmd := &newInitiativeKong{
		Dir:     dir,
		DriArgs: []string{"the", "problem", "statement"},
		launch: func(_ *cli.Context, d, arg, role, initiativeID string) error {
			gotDir, gotArg, gotRole, gotInitiative = d, arg, role, initiativeID
			return nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotArg != "the problem statement" {
		t.Errorf("driArg = %q, want %q", gotArg, "the problem statement")
	}
	if gotDir != dir {
		t.Errorf("dir = %q, want %q", gotDir, dir)
	}
	if gotRole != "dri" {
		t.Errorf("role = %q, want %q", gotRole, "dri")
	}
	// A multi-word DriArgs is a free-text problem statement, not an id — the
	// launcher doesn't know an initiative id yet, so it must be omitted.
	if gotInitiative != "" {
		t.Errorf("initiativeID = %q, want empty (multi-word DriArgs is a problem statement, not an id)", gotInitiative)
	}
}

// TestNewInitiativeKong_IDShapedDriArgIsInitiativeID verifies that a DriArgs
// value matching the registry's id shape (initiativeIDPattern, e.g.
// "at-1ldm") is passed through as ATEAM_INITIATIVE — the "driArg is an id"
// case from agent-teams-142k.2.
func TestNewInitiativeKong_IDShapedDriArgIsInitiativeID(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}

	var gotRole, gotInitiative string
	cmd := &newInitiativeKong{
		Dir:     dir,
		DriArgs: []string{"at-1ldm"},
		launch: func(_ *cli.Context, _, _, role, initiativeID string) error {
			gotRole, gotInitiative = role, initiativeID
			return nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotRole != "dri" {
		t.Errorf("role = %q, want %q", gotRole, "dri")
	}
	if gotInitiative != "at-1ldm" {
		t.Errorf("initiativeID = %q, want %q", gotInitiative, "at-1ldm")
	}
}

// TestNewInitiativeKong_SingleWordNonIDShapedDriArgOmitsInitiativeID verifies
// the false-negative bias explicitly: a single-word DriArgs value that does
// NOT match the id shape (e.g. a bare one-word problem statement) must NOT be
// passed through as ATEAM_INITIATIVE. Token count alone would misclassify
// this; only the id-shape regex (initiativeIDPattern) tells them apart.
func TestNewInitiativeKong_SingleWordNonIDShapedDriArgOmitsInitiativeID(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}

	var gotRole, gotInitiative string
	cmd := &newInitiativeKong{
		Dir:     dir,
		DriArgs: []string{"authentication"},
		launch: func(_ *cli.Context, _, _, role, initiativeID string) error {
			gotRole, gotInitiative = role, initiativeID
			return nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotRole != "dri" {
		t.Errorf("role = %q, want %q", gotRole, "dri")
	}
	if gotInitiative != "" {
		t.Errorf("initiativeID = %q, want empty (single word, but not id-shaped)", gotInitiative)
	}
}

// TestResumeKong_DelegatesLaunch verifies that resumeKong.Run passes the
// injected launchFunc through to the underlying resumeCommand.
func TestResumeKong_DelegatesLaunch(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{
				ID:          "at-rk1",
				Status:      "open",
				Description: "worktree: " + dir + "\n",
			}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}

	var launchedID, launchedRole, launchedInitiative string
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	cmd := &resumeKong{
		ID: "at-rk1",
		launch: func(_ *cli.Context, _, arg, role, initiativeID string) error {
			launchedID, launchedRole, launchedInitiative = arg, role, initiativeID
			return nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if launchedID != "at-rk1" {
		t.Errorf("launch driArg = %q, want %q", launchedID, "at-rk1")
	}
	if launchedRole != "dri" {
		t.Errorf("launch role = %q, want %q", launchedRole, "dri")
	}
	if launchedInitiative != "at-rk1" {
		t.Errorf("launch initiativeID = %q, want %q", launchedInitiative, "at-rk1")
	}
}

// TestResumeKong_RepoEnabled_DelegatesLaunch verifies a "repo:" field pointing
// at an enabled repo does not block resume — the positive counterpart to
// TestResumeKong_RepoDisabled_Refuses below.
func TestResumeKong_RepoEnabled_DelegatesLaunch(t *testing.T) {
	dir := t.TempDir()
	repoDir := newEnabledRepoDir(t)
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{
				ID:          "at-rk2",
				Status:      "open",
				Description: "worktree: " + dir + "\nrepo: " + repoDir + "\n",
			}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}

	var launchedID string
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	cmd := &resumeKong{
		ID: "at-rk2",
		launch: func(_ *cli.Context, _, arg, _, _ string) error {
			launchedID = arg
			return nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if launchedID != "at-rk2" {
		t.Errorf("launch driArg = %q, want %q", launchedID, "at-rk2")
	}
}

// TestResumeKong_RepoDisabled_Refuses verifies resume refuses to relaunch a
// session whose initiative's "repo:" field points at a repo with no (or a
// disabled) .agent-teams file — the same gate as dispatch, applied to an
// already-registered initiative.
func TestResumeKong_RepoDisabled_Refuses(t *testing.T) {
	dir := t.TempDir()
	repoDir := t.TempDir() // no .agent-teams file
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{
				ID:          "at-rk3",
				Status:      "open",
				Description: "worktree: " + dir + "\nrepo: " + repoDir + "\n",
			}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}

	launched := false
	ctx, _, stderr := makeCtx(fbd, t.TempDir())
	cmd := &resumeKong{
		ID: "at-rk3",
		launch: func(_ *cli.Context, _, _, _, _ string) error {
			launched = true
			return nil
		},
	}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error for a disabled repo, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if launched {
		t.Error("launch was called despite the repo being disabled")
	}
	if !strings.Contains(stderr.String(), "agent-teams is not enabled") {
		t.Errorf("expected 'agent-teams is not enabled' in stderr, got: %s", stderr.String())
	}
}

// ── dispatch: epic creation ───────────────────────────────────────────────────

// TestDispatch_EpicCreatedAndAppendedToBody verifies that when createEpic
// succeeds, "epic: <id>" is written into the initiative body before bd create.
func TestDispatch_EpicCreatedAndAppendedToBody(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	expectedSlug := "epic-work"

	var gotBody string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			for _, a := range args {
				if strings.HasPrefix(a, "--body-file=") {
					path := strings.TrimPrefix(a, "--body-file=")
					b, err := os.ReadFile(path)
					if err == nil {
						gotBody = string(b)
					}
				}
			}
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-epic-dispatch"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(fbd, home)

	var epicRepoGot, epicTitleGot string
	cmd := &dispatchKong{
		Problem:  "Epic work",
		Slug:     expectedSlug,
		Repo:     repoDir,
		NoLaunch: true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
		createEpic: func(repoPath, title string) (string, error) {
			epicRepoGot = repoPath
			epicTitleGot = title
			return "at-root-epic", nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The epic creator must have been called with the resolved repo root.
	if epicRepoGot != repoDir {
		t.Errorf("createEpic repo = %q, want %q", epicRepoGot, repoDir)
	}
	if epicTitleGot != "Epic work" {
		t.Errorf("createEpic title = %q, want %q", epicTitleGot, "Epic work")
	}
	// The body passed to bd create must contain the epic: line.
	if !strings.Contains(gotBody, "epic: at-root-epic") {
		t.Errorf("body missing 'epic: at-root-epic':\n%s", gotBody)
	}
	// The repo: line must still be present.
	if !strings.Contains(gotBody, "repo: "+repoDir) {
		t.Errorf("body missing 'repo:' line:\n%s", gotBody)
	}
}

// ── bgSessionArgs: --settings argument ─────────────────────────────────────

// TestBGSessionArgs_SettingsAutoCompactWindow pins agent-teams-4pc5.3's fix:
// the auto-compact window now DOES belong in --settings (inverting the older
// decision this test used to guard — the earlier default-safety argument
// still holds for the UNSET case, just not for a value the caller actually
// configured). Nearly all background launches are served by the daemon's
// pre-warmed spare pool, which claims a session via IPC and only honors
// --settings, never a startup-time --autocompact flag — so a window pinned
// only on argv silently never reaches those sessions. Every parseable window
// must appear as autoCompactEnabled:true + autoCompactWindow:<int> (the
// global default for autoCompactEnabled is false, so the window alone would
// still leave compaction off); "auto", empty, and unparseable values must
// still omit both keys, keeping today's default byte-identical. Unmarshaled
// into a map rather than string-compared, so key ordering can't make this
// brittle.
func TestBGSessionArgs_SettingsAutoCompactWindow(t *testing.T) {
	for _, tc := range []struct {
		role, initiativeID, window string
		wantWindow                 int
		wantKeys                   bool
	}{
		{role: "dri", initiativeID: "at-abc123", window: "", wantKeys: false},
		{role: "dri", initiativeID: "", window: "", wantKeys: false},
		{role: "steward", initiativeID: "", window: "", wantKeys: false},
		{role: "steward", initiativeID: "", window: "auto", wantKeys: false},
		{role: "dri", initiativeID: "", window: "banana", wantKeys: false},
		{role: "dri", initiativeID: "at-abc123", window: "450000", wantWindow: 450000, wantKeys: true},
		{role: "dri", initiativeID: "", window: "500k", wantWindow: 500000, wantKeys: true},
		{role: "dri", initiativeID: "at-abc123", window: "1m", wantWindow: 1000000, wantKeys: true},
		{role: "dri", initiativeID: "", window: "200", wantWindow: 200000, wantKeys: true},
	} {
		args := bgSessionArgs("my-session", "/dri at-abc123", "", "", tc.role, tc.initiativeID, "{}", tc.window)
		got := settingsValue(t, args)

		var parsed map[string]any
		if err := json.Unmarshal([]byte(got), &parsed); err != nil {
			t.Fatalf("role=%q window=%q: --settings = %q is not valid JSON: %v", tc.role, tc.window, got, err)
		}

		if !tc.wantKeys {
			if _, ok := parsed["autoCompactEnabled"]; ok {
				t.Errorf("role=%q window=%q: --settings must omit autoCompactEnabled; got %q", tc.role, tc.window, got)
			}
			if _, ok := parsed["autoCompactWindow"]; ok {
				t.Errorf("role=%q window=%q: --settings must omit autoCompactWindow; got %q", tc.role, tc.window, got)
			}
			continue
		}
		enabled, ok := parsed["autoCompactEnabled"].(bool)
		if !ok || !enabled {
			t.Errorf("role=%q window=%q: --settings missing autoCompactEnabled:true; got %q", tc.role, tc.window, got)
		}
		winVal, ok := parsed["autoCompactWindow"].(float64)
		if !ok {
			t.Errorf("role=%q window=%q: --settings missing numeric autoCompactWindow; got %q", tc.role, tc.window, got)
		} else if int(winVal) != tc.wantWindow {
			t.Errorf("role=%q window=%q: autoCompactWindow = %v, want %d", tc.role, tc.window, winVal, tc.wantWindow)
		}
	}
}

// TestBGSessionArgs_OmitsAutocompactFlagWhenEmpty is the guard that actually
// protects the empty default now that the auto-compact window rides argv
// instead of --settings (see TestBGSessionArgs_SettingsOmitsAutoCompactWindow
// above): with autoCompactWindow == "", argv must not contain "--autocompact"
// at all, so today's launches stay byte-identical.
func TestBGSessionArgs_OmitsAutocompactFlagWhenEmpty(t *testing.T) {
	args := bgSessionArgs("my-session", "/dri at-abc123", "", "", "dri", "at-abc123", "{}", "")
	for _, a := range args {
		if a == "--autocompact" {
			t.Fatalf("argv must omit --autocompact when the window is empty; got: %v", args)
		}
	}
}

// TestBGSessionArgs_AutocompactFlagWhenSet pins the pass-through-verbatim
// contract: when autoCompactWindow is non-empty, bgSessionArgs appends
// "--autocompact" followed by the exact string given, exactly once — no
// parsing, no normalization, no range check. Cases include a non-numeric CLI
// shorthand ("500k") and the literal "auto" to prove neither is rejected or
// altered here; the claude CLI itself owns validation.
func TestBGSessionArgs_AutocompactFlagWhenSet(t *testing.T) {
	for _, window := range []string{"450000", "500k", "1m", "200", "auto"} {
		args := bgSessionArgs("my-session", "/dri at-abc123", "", "", "dri", "at-abc123", "{}", window)

		count := 0
		for i, a := range args {
			if a != "--autocompact" {
				continue
			}
			count++
			if i+1 >= len(args) {
				t.Fatalf("--autocompact has no following value in argv: %v", args)
			}
			if args[i+1] != window {
				t.Errorf("--autocompact value = %q, want %q", args[i+1], window)
			}
		}
		if count != 1 {
			t.Errorf("argv must contain --autocompact exactly once for window %q; got %d occurrences in %v", window, count, args)
		}
	}
}

// settingsValue extracts the value following "--settings" in args, or fails
// the test if the flag is missing.
func settingsValue(t *testing.T, args []string) string {
	t.Helper()
	for i, a := range args {
		if a == "--settings" {
			if i+1 >= len(args) {
				t.Fatal("--settings has no following value in argv")
			}
			return args[i+1]
		}
	}
	t.Fatalf("argv missing --settings; got: %v", args)
	return ""
}

// TestBGSessionArgs_SettingsEnv_RoleAndInitiative verifies the default /dri
// path's merged --settings JSON: role and initiative id both present, in the
// contract's documented field order (agent-teams-142k.1, PLAN.md §1).
func TestBGSessionArgs_SettingsEnv_RoleAndInitiative(t *testing.T) {
	args := bgSessionArgs("my-session", "/dri at-abc123", "", "", "dri", "at-abc123", "{}", "")
	got := settingsValue(t, args)
	want := `{"env":{"ATEAM_ROLE":"dri","ATEAM_INITIATIVE":"at-abc123"}}`
	if got != want {
		t.Errorf("--settings = %q, want %q", got, want)
	}
}

// TestBGSessionArgs_SettingsEnv_RoleOnly verifies the new-initiative-with-a-
// bare-problem-statement case: role present, ATEAM_INITIATIVE omitted
// entirely (not an empty string) when the launcher doesn't know the id.
func TestBGSessionArgs_SettingsEnv_RoleOnly(t *testing.T) {
	args := bgSessionArgs("my-session", "/dri a problem statement", "", "", "dri", "", "{}", "")
	got := settingsValue(t, args)
	want := `{"env":{"ATEAM_ROLE":"dri"}}`
	if got != want {
		t.Errorf("--settings = %q, want %q", got, want)
	}
	if strings.Contains(got, "ATEAM_INITIATIVE") {
		t.Errorf("--settings must omit ATEAM_INITIATIVE when initiative id is unknown: %q", got)
	}
}

// TestBGSessionArgs_SettingsEnv_Absent verifies that when both role and
// initiative id are empty (a hypothetical bare launch), the "--settings" flag
// is left off the argv entirely rather than carrying an empty object. The env
// map is the only thing ateam configures, so with nothing to say it says
// nothing — an empty "{}" would read like an intent that went missing.
func TestBGSessionArgs_SettingsEnv_Absent(t *testing.T) {
	args := bgSessionArgs("my-session", "/dri at-abc123", "", "", "", "", "{}", "")
	for _, a := range args {
		if a == "--settings" {
			t.Fatalf("argv must omit --settings when role and initiative id are both empty; got: %v", args)
		}
	}
	// The flag it precedes must still be intact.
	found := false
	for i, a := range args {
		if a == "--append-system-prompt" && i+1 < len(args) && args[i+1] == driSystemPromptAppend {
			found = true
		}
	}
	if !found {
		t.Errorf("argv missing --append-system-prompt pair; got: %v", args)
	}
}

// ── dispatch: --skip-epic ─────────────────────────────────────────────────────

// TestDispatch_SkipEpic verifies that when --skip-epic is set, createEpic is
// never called (even when it is injected).
func TestDispatch_SkipEpic(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-se1"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(fbd, home)

	epicCalled := false
	cmd := &dispatchKong{
		Problem:  "Skip epic test",
		Repo:     repoDir,
		NoLaunch: true,
		SkipEpic: true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
		createEpic: func(_, _ string) (string, error) {
			epicCalled = true
			return "at-should-not-be-created", nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if epicCalled {
		t.Errorf("createEpic must not be called when --skip-epic is set")
	}
}

// ── dispatch: --launch-prompt ─────────────────────────────────────────────────

// TestDispatch_LaunchPrompt verifies that when --launch-prompt is set, the bg
// session receives the custom prompt verbatim (not /dri <id>).
func TestDispatch_LaunchPrompt(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-lp1"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(fbd, home)

	var capturedPrompt string
	cmd := &dispatchKong{
		Problem:      "Do a review",
		Repo:         repoDir,
		LaunchPrompt: "/review-skill at-lp1",
		git:          fg,
		launch:       func(_ *cli.Context, _, _, _, _ string) error { return nil },
		launchRaw: func(_ *cli.Context, _, p, _, _, _, _ string) error {
			capturedPrompt = p
			return nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Prompt must be the LaunchPrompt verbatim (static, no {id} in this test).
	if capturedPrompt != "/review-skill at-lp1" {
		t.Errorf("bg session prompt = %q, want %q", capturedPrompt, "/review-skill at-lp1")
	}
	// Must NOT be the default /dri prefix.
	if strings.HasPrefix(capturedPrompt, "/dri ") {
		t.Errorf("bg session must not use /dri prefix when --launch-prompt is set: %q", capturedPrompt)
	}
}

// TestDispatch_LaunchPrompt_Substitution verifies that {id} in --launch-prompt
// is replaced with the actual initiative id returned by bd create.
func TestDispatch_LaunchPrompt_Substitution(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-sub1"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(fbd, home)

	var capturedPrompt, capturedRole, capturedInitiative string
	cmd := &dispatchKong{
		Problem:      "Substitution test",
		Repo:         repoDir,
		LaunchPrompt: "/review {id}",
		git:          fg,
		launch:       func(_ *cli.Context, _, _, _, _ string) error { return nil },
		launchRaw: func(_ *cli.Context, _, p, _, _, role, initiativeID string) error {
			capturedPrompt, capturedRole, capturedInitiative = p, role, initiativeID
			return nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// {id} must be replaced with the actual initiative id.
	want := "/review at-sub1"
	if capturedPrompt != want {
		t.Errorf("prompt = %q, want %q ({id} must be substituted with initiative id)", capturedPrompt, want)
	}
	// The --launch-prompt path is role=dri, initiative id known (agent-teams-142k.2).
	if capturedRole != "dri" {
		t.Errorf("launchRaw role = %q, want %q", capturedRole, "dri")
	}
	if capturedInitiative != "at-sub1" {
		t.Errorf("launchRaw initiativeID = %q, want %q", capturedInitiative, "at-sub1")
	}
}

// TestDispatch_LaunchPrompt_ModelOverride verifies that --model is threaded
// through to c.launchRaw alongside the substituted --launch-prompt, and that
// advisor defaults to "" when --advisor is not set.
func TestDispatch_LaunchPrompt_ModelOverride(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-model1"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(fbd, home)

	var capturedModel, capturedAdvisor string
	cmd := &dispatchKong{
		Problem:      "Review with sonnet",
		Repo:         repoDir,
		LaunchPrompt: "/review {id}",
		Model:        "sonnet",
		git:          fg,
		launch:       func(_ *cli.Context, _, _, _, _ string) error { return nil },
		launchRaw: func(_ *cli.Context, _, _, m, adv, _, _ string) error {
			capturedModel = m
			capturedAdvisor = adv
			return nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default behavior is unchanged: advisor is "" unless the caller explicitly
	// sets --advisor, and model passes through as given via --model.
	if capturedAdvisor != "" {
		t.Errorf("launchRaw advisor = %q, want empty (default when --advisor is omitted)", capturedAdvisor)
	}
	if capturedModel != "sonnet" {
		t.Errorf("launchRaw model = %q, want %q", capturedModel, "sonnet")
	}
}

// TestDispatch_LaunchPrompt_AdvisorOverride verifies that --advisor is
// threaded through to c.launchRaw when explicitly set, opting the
// --launch-prompt path into advisor mode.
func TestDispatch_LaunchPrompt_AdvisorOverride(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-advisor1"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(fbd, home)

	var capturedModel, capturedAdvisor string
	cmd := &dispatchKong{
		Problem:      "Review with advisor",
		Repo:         repoDir,
		LaunchPrompt: "/review {id}",
		Model:        "sonnet",
		Advisor:      "opus",
		git:          fg,
		launch:       func(_ *cli.Context, _, _, _, _ string) error { return nil },
		launchRaw: func(_ *cli.Context, _, _, m, adv, _, _ string) error {
			capturedModel = m
			capturedAdvisor = adv
			return nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedAdvisor != "opus" {
		t.Errorf("launchRaw advisor = %q, want %q (explicit --advisor override)", capturedAdvisor, "opus")
	}
	if capturedModel != "sonnet" {
		t.Errorf("launchRaw model = %q, want %q", capturedModel, "sonnet")
	}
}

// TestDispatch_EpicCreation_FailSoft verifies that when createEpic returns an
// error, dispatch still succeeds and registers the initiative without epic:.
func TestDispatch_EpicCreation_FailSoft(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	var gotBody string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			for _, a := range args {
				if strings.HasPrefix(a, "--body-file=") {
					path := strings.TrimPrefix(a, "--body-file=")
					b, err := os.ReadFile(path)
					if err == nil {
						gotBody = string(b)
					}
				}
			}
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-no-epic"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, stderr := makeCtx(fbd, home)

	cmd := &dispatchKong{
		Problem:  "Some work",
		Slug:     "some-work",
		Repo:     repoDir,
		NoLaunch: true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
		createEpic: func(_, _ string) (string, error) {
			return "", fmt.Errorf("bd: simulated epic failure")
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("dispatch should succeed despite epic failure, got: %v", err)
	}
	// The body must NOT contain an epic: line.
	if strings.Contains(gotBody, "epic:") {
		t.Errorf("body must not contain 'epic:' when epic creation fails:\n%s", gotBody)
	}
	// A warning must be written to stderr.
	if !strings.Contains(stderr.String(), "fail-soft") {
		t.Errorf("stderr missing 'fail-soft' warning: %q", stderr.String())
	}
}

// ── dispatch: eager Telegram topic creation (agent-teams-6rru.1) ────────────

// TestDispatch_EagerTopic_CreatesAndLabelsThread confirms dispatch eagerly
// calls Send with ThreadRef="" right after the initiative bead is created,
// and records the returned ref as "thread:<ref>" on that bead.
func TestDispatch_EagerTopic_CreatesAndLabelsThread(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-eager1"
				issue.Title = "Eager topic test"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(fbd, home)

	ft := &fakeTransport{returnRef: "555"}
	var recordedID, recordedLabel string
	cmd := &dispatchKong{
		Problem:          "Eager topic test",
		Slug:             "eager-topic-test",
		Repo:             repoDir,
		NoLaunch:         true,
		SkipEpic:         true,
		git:              fg,
		launch:           func(_ *cli.Context, _, _, _, _ string) error { return nil },
		transportEnabled: func(home string) bool { return true },
		transportFor:     fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			recordedID = id
			recordedLabel = label
			return nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	if ft.calls[0].ThreadRef != "" {
		t.Errorf("expected ThreadRef empty on eager creation, got %q", ft.calls[0].ThreadRef)
	}
	if ft.calls[0].InitiativeID != "at-eager1" {
		t.Errorf("InitiativeID = %q, want at-eager1", ft.calls[0].InitiativeID)
	}
	if ft.calls[0].Title != "Eager topic test" {
		t.Errorf("Title = %q, want %q", ft.calls[0].Title, "Eager topic test")
	}
	wantBody := "Initiative registered: Eager topic test"
	if ft.calls[0].Body != wantBody {
		t.Errorf("Body = %q, want %q", ft.calls[0].Body, wantBody)
	}

	if recordedID != "at-eager1" {
		t.Errorf("labelAdd id = %q, want at-eager1", recordedID)
	}
	if recordedLabel != "thread:555" {
		t.Errorf("labelAdd label = %q, want thread:555", recordedLabel)
	}
}

// TestDispatch_EagerTopic_ThenNotify_ReusesThread confirms a later `ateam
// notify` on the same initiative reuses the thread dispatch already opened,
// rather than opening a second topic.
func TestDispatch_EagerTopic_ThenNotify_ReusesThread(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-eager2"
				issue.Title = "Reuse thread test"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(fbd, home)

	ft := &fakeTransport{returnRef: "777"}
	var recordedLabel string
	cmd := &dispatchKong{
		Problem:          "Reuse thread test",
		Slug:             "reuse-thread-test",
		Repo:             repoDir,
		NoLaunch:         true,
		SkipEpic:         true,
		git:              fg,
		launch:           func(_ *cli.Context, _, _, _, _ string) error { return nil },
		transportEnabled: func(home string) bool { return true },
		transportFor:     fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			recordedLabel = label
			return nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("dispatch: unexpected error: %v", err)
	}
	if recordedLabel != "thread:777" {
		t.Fatalf("expected thread label thread:777 recorded, got %q", recordedLabel)
	}

	// A later notify reuses the stored thread; no second topic is created.
	bodyFile := makeTempBodyFile(t, "follow-up")
	nbd := &notifyFakeBD{
		issue: bd.Issue{
			ID:     "at-eager2",
			Title:  "Reuse thread test",
			Labels: []string{recordedLabel},
		},
	}
	notifyCmd := &notifyKong{
		ID:           "at-eager2",
		File:         bodyFile,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
	}
	notifyCtx, _, _ := newNotifyCtx(nbd)
	if err := notifyCmd.Run(notifyCtx); err != nil {
		t.Fatalf("notify: unexpected error: %v", err)
	}

	if len(ft.calls) != 2 {
		t.Fatalf("expected 2 total Send calls (dispatch + notify), got %d", len(ft.calls))
	}
	if ft.calls[1].ThreadRef != "777" {
		t.Errorf("notify Send ThreadRef = %q, want 777 (reused, no second topic)", ft.calls[1].ThreadRef)
	}
}

// TestDispatch_EagerTopic_TransportDisabled_NoSend confirms a machine with no
// transport configured skips eager topic creation entirely (silent skip, no
// stderr warning — the normal state for installs without Telegram set up).
func TestDispatch_EagerTopic_TransportDisabled_NoSend(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-notrans"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, stderr := makeCtx(fbd, home)

	ft := &fakeTransport{returnRef: "should-not-be-used"}
	cmd := &dispatchKong{
		Problem:          "No transport test",
		Slug:             "no-transport-test",
		Repo:             repoDir,
		NoLaunch:         true,
		SkipEpic:         true,
		git:              fg,
		launch:           func(_ *cli.Context, _, _, _, _ string) error { return nil },
		transportEnabled: func(home string) bool { return false },
		transportFor:     fakeTransportFor(ft, nil),
		labelAdd:         func(b cli.BDRunner, id, label string) error { return nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("dispatch should succeed when transport disabled, got: %v", err)
	}
	if len(ft.calls) != 0 {
		t.Errorf("Send should not be called when transportEnabled returns false, got %d calls", len(ft.calls))
	}
	if stderr.String() != "" {
		t.Errorf("expected no stderr warning when transport is simply not configured, got: %q", stderr.String())
	}
}

// TestDispatch_EagerTopic_SendError_FailSoft confirms a transport error
// during the eager Send warns to stderr but does not fail dispatch, and does
// not record a thread label (nothing to label — the topic never opened).
func TestDispatch_EagerTopic_SendError_FailSoft(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-senderr"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, stderr := makeCtx(fbd, home)

	ft := &fakeTransport{returnErr: fmt.Errorf("simulated telegram outage")}
	labelAddCalled := false
	cmd := &dispatchKong{
		Problem:          "Send error test",
		Slug:             "send-error-test",
		Repo:             repoDir,
		NoLaunch:         true,
		SkipEpic:         true,
		git:              fg,
		launch:           func(_ *cli.Context, _, _, _, _ string) error { return nil },
		transportEnabled: func(home string) bool { return true },
		transportFor:     fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			labelAddCalled = true
			return nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("dispatch should succeed despite topic Send failure, got: %v", err)
	}
	if labelAddCalled {
		t.Error("labelAdd must not be called when Send fails")
	}
	if !strings.Contains(stderr.String(), "fail-soft") {
		t.Errorf("stderr missing 'fail-soft' warning: %q", stderr.String())
	}
}

// ── dispatch --topic: the shared Reviews topic (agent-teams-p9dm.10) ─────────

const (
	testPROwnerRepo = "MGT-Insurance/midgard"
	testPRURL       = "https://github.com/MGT-Insurance/midgard/pull/4517"
)

// reviewBodyFile writes the pr-number/pr-repo/pr-url metadata block that
// route.go's spawnReviewInitiative and the dispatch-review-pr skill both
// hand dispatch via --body-file.
func reviewBodyFile(t *testing.T) string {
	t.Helper()
	return makeTempBodyFile(t, "pr-number: 4517\npr-repo: "+testPROwnerRepo+"\npr-url: "+testPRURL+"\n")
}

// reviewDispatch builds a --topic reviews dispatchKong wired to ft, with the
// PR metadata body and a stubbed prTitle.
func reviewDispatch(t *testing.T, repoDir, slug string, ft *fakeTransport, prTitle prTitleFunc, labelAdd labelAddFunc) *dispatchKong {
	t.Helper()
	return &dispatchKong{
		Problem:          "Review PR #4517",
		Slug:             slug,
		Repo:             repoDir,
		BodyFile:         reviewBodyFile(t),
		NoLaunch:         true,
		SkipEpic:         true,
		Topic:            ReviewsHandle,
		git:              &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }},
		launch:           func(_ *cli.Context, _, _, _, _ string) error { return nil },
		transportEnabled: func(home string) bool { return true },
		transportFor:     fakeTransportFor(ft, nil),
		labelAdd:         labelAdd,
		prTitle:          prTitle,
	}
}

// TestDispatch_TopicReviews_PostsSharedLine_NoThreadLabel is the core of
// agent-teams-p9dm.10: with --topic reviews, dispatch posts the frozen
// ReviewsStartLineFormat line into the shared, file-backed Reviews topic
// (persisting the returned ref to StewardReviewsThreadPath) and writes NO
// "thread:" label on the initiative bead — the no-label part is load-bearing
// per the contract, not an omission.
func TestDispatch_TopicReviews_PostsSharedLine_NoThreadLabel(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-rev1"
				issue.Title = "Review PR #4517 (MGT-Insurance/midgard)"
			}
			return nil
		},
	}
	ctx, _, stderr := makeCtx(fbd, home)

	ft := &fakeTransport{returnRef: "901"}
	labelAddCalled := false
	cmd := reviewDispatch(t, repoDir, "review-pr-4517", ft,
		func(ownerRepo string, prNumber int) (string, error) {
			if ownerRepo != testPROwnerRepo || prNumber != 4517 {
				t.Errorf("prTitle(%q, %d), want (%q, 4517)", ownerRepo, prNumber, testPROwnerRepo)
			}
			return "Fix flaky retry logic", nil
		},
		func(b cli.BDRunner, id, label string) error {
			labelAddCalled = true
			return nil
		})

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if labelAddCalled {
		t.Error("labelAdd must NEVER be called on the --topic path (a shared thread label breaks relay routing and close)")
	}
	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	sent := ft.calls[0]
	if sent.ThreadRef != "" {
		t.Errorf("ThreadRef = %q, want empty on first send (topic not yet open)", sent.ThreadRef)
	}
	if sent.InitiativeID != ReviewsHandle {
		t.Errorf("InitiativeID = %q, want %q", sent.InitiativeID, ReviewsHandle)
	}
	if sent.Title != ReviewsTopicTitle {
		t.Errorf("Title = %q, want %q (the shared topic's name, not the initiative's)", sent.Title, ReviewsTopicTitle)
	}
	wantBody := "Review started · #4517 midgard — Fix flaky retry logic\n" + testPRURL
	if sent.Body != wantBody {
		t.Errorf("Body = %q, want %q", sent.Body, wantBody)
	}

	// The returned ref persists to the shared thread-ref file, not a label.
	ref, err := os.ReadFile(StewardReviewsThreadPath(ctx))
	if err != nil {
		t.Fatalf("read %s: %v", StewardReviewsThreadPath(ctx), err)
	}
	if string(ref) != "901" {
		t.Errorf("persisted thread ref = %q, want %q", string(ref), "901")
	}
	if stderr.String() != "" {
		t.Errorf("expected no stderr on the happy path, got: %q", stderr.String())
	}
}

// TestDispatch_TopicReviews_TitleFetchFails_StillDispatches pins the
// contract's mandatory fail-soft: a prTitleFunc error yields no title
// segment (and no dangling " — " separator), and never fails the dispatch.
func TestDispatch_TopicReviews_TitleFetchFails_StillDispatches(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-rev2"
			}
			return nil
		},
	}
	ctx, _, stderr := makeCtx(fbd, home)

	ft := &fakeTransport{returnRef: "902"}
	cmd := reviewDispatch(t, repoDir, "review-pr-4517-no-title", ft,
		func(ownerRepo string, prNumber int) (string, error) {
			return "", fmt.Errorf("gh: not authenticated")
		},
		func(b cli.BDRunner, id, label string) error { return nil })

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("a title-fetch failure must never fail dispatch, got: %v", err)
	}

	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	wantBody := "Review started · #4517 midgard\n" + testPRURL
	if ft.calls[0].Body != wantBody {
		t.Errorf("Body = %q, want %q", ft.calls[0].Body, wantBody)
	}
	if !strings.Contains(stderr.String(), "fail-soft") {
		t.Errorf("stderr missing 'fail-soft' warning: %q", stderr.String())
	}
}

// TestDispatch_TopicReviews_SecondDispatch_ReusesRef confirms the second
// review dispatch sends into the topic the first one opened instead of
// creating another — the whole point of the initiative.
func TestDispatch_TopicReviews_SecondDispatch_ReusesRef(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-rev3"
			}
			return nil
		},
	}
	ctx, _, _ := makeCtx(fbd, home)

	ft := &fakeTransport{returnRef: "903"}
	prTitle := func(ownerRepo string, prNumber int) (string, error) { return "Fix flaky retry logic", nil }
	noLabel := func(b cli.BDRunner, id, label string) error {
		t.Error("labelAdd must never be called on the --topic path")
		return nil
	}

	if err := reviewDispatch(t, repoDir, "review-one", ft, prTitle, noLabel).Run(ctx); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	if err := reviewDispatch(t, repoDir, "review-two", ft, prTitle, noLabel).Run(ctx); err != nil {
		t.Fatalf("second dispatch: %v", err)
	}

	if len(ft.calls) != 2 {
		t.Fatalf("expected 2 Send calls, got %d", len(ft.calls))
	}
	if ft.calls[1].ThreadRef != "903" {
		t.Errorf("second Send ThreadRef = %q, want 903 (reused, no second topic)", ft.calls[1].ThreadRef)
	}
}

// TestDispatch_NoTopic_LeavesEagerPathAlone confirms the flag is inert when
// absent: the per-initiative topic and its thread label are unchanged, and
// prTitle is never called (so a plain dispatch never spawns gh).
func TestDispatch_NoTopic_LeavesEagerPathAlone(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-rev4"
				issue.Title = "Plain dispatch"
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(dir string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(fbd, home)

	ft := &fakeTransport{returnRef: "904"}
	var recordedLabel string
	cmd := &dispatchKong{
		Problem:          "Plain dispatch",
		Slug:             "plain-dispatch",
		Repo:             repoDir,
		BodyFile:         reviewBodyFile(t),
		NoLaunch:         true,
		SkipEpic:         true,
		git:              fg,
		launch:           func(_ *cli.Context, _, _, _, _ string) error { return nil },
		transportEnabled: func(home string) bool { return true },
		transportFor:     fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			recordedLabel = label
			return nil
		},
		prTitle: func(ownerRepo string, prNumber int) (string, error) {
			t.Error("prTitle must not be called without --topic")
			return "", nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	if ft.calls[0].Title != "Plain dispatch" {
		t.Errorf("Title = %q, want the initiative's own title", ft.calls[0].Title)
	}
	if ft.calls[0].Body != "Initiative registered: Plain dispatch" {
		t.Errorf("Body = %q, want the unchanged eager-topic body", ft.calls[0].Body)
	}
	if recordedLabel != "thread:904" {
		t.Errorf("labelAdd label = %q, want thread:904", recordedLabel)
	}
	if _, err := os.Stat(StewardReviewsThreadPath(ctx)); !os.IsNotExist(err) {
		t.Errorf("the shared reviews thread-ref file must not be touched without --topic (stat err = %v)", err)
	}
}

// TestDispatch_TopicReviews_MissingPRMetadata_NamesTheKey confirms a body
// without the PR metadata posts nothing rather than a half-rendered line,
// and that the warning NAMES the absent keys — otherwise a caller that
// forgot one sees a dispatch that succeeded with no line in the topic and
// nothing to chase.
func TestDispatch_TopicReviews_MissingPRMetadata_NamesTheKey(t *testing.T) {
	home := t.TempDir()
	repoDir := newEnabledRepoDir(t)

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-rev5"
			}
			return nil
		},
	}
	ctx, _, stderr := makeCtx(fbd, home)

	ft := &fakeTransport{returnRef: "905"}
	cmd := reviewDispatch(t, repoDir, "review-no-metadata", ft,
		func(ownerRepo string, prNumber int) (string, error) { return "Fix flaky retry logic", nil },
		func(b cli.BDRunner, id, label string) error { return nil })
	// pr-number and pr-url present, pr-repo absent.
	cmd.BodyFile = makeTempBodyFile(t, "pr-number: 4517\npr-url: "+testPRURL+"\n")

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("a body missing PR metadata must never fail dispatch, got: %v", err)
	}

	if len(ft.calls) != 0 {
		t.Fatalf("expected no Send, got %d (a half-rendered line must not reach the shared feed)", len(ft.calls))
	}
	if !strings.Contains(stderr.String(), "pr-repo") {
		t.Errorf("warning must name the missing key, got: %q", stderr.String())
	}
	for _, present := range []string{"pr-number", "pr-url"} {
		if strings.Contains(stderr.String(), present) {
			t.Errorf("warning names %s, which was present: %q", present, stderr.String())
		}
	}
}

// TestDispatch_UnknownTopic_UsageError pins the contract's "unrecognized
// --topic is a usage error, never a silent fallback to per-initiative topic
// creation". Validate runs before Run, so it costs no worktree and no bead.
func TestDispatch_UnknownTopic_UsageError(t *testing.T) {
	cmd := &dispatchKong{Problem: "Review PR #4517", Topic: "briefing"}
	err := cmd.Validate()
	if err == nil {
		t.Fatal("expected a usage error for an unknown --topic value")
	}
	var usage *cli.UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("error = %T (%v), want *cli.UsageError", err, err)
	}

	if err := (&dispatchKong{Problem: "p", Topic: ReviewsHandle}).Validate(); err != nil {
		t.Errorf("--topic %s must validate, got: %v", ReviewsHandle, err)
	}
	if err := (&dispatchKong{Problem: "p"}).Validate(); err != nil {
		t.Errorf("no --topic must validate, got: %v", err)
	}
}
