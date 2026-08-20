package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
)

// The worktreePath unit tests that lived here are gone with the helper —
// worktree resolution is initiative.Of's, and internal/initiative owns its
// tests. What is still verbs' own is that resume READS the field; that is
// TestResume_NoWorktreeLine below.

// ---- resumeKong: nil context -----------------------------------------------

func TestResume_NilContext(t *testing.T) {
	err := (&resumeKong{ID: "at-abc"}).Run(nil)
	if err == nil {
		t.Fatal("expected error for nil context, got nil")
	}
}

// ---- resumeKong: missing arg -----------------------------------------------

func TestResume_MissingArg(t *testing.T) {
	err := (&resumeKong{}).Validate()
	if err == nil {
		t.Fatal("expected UsageError for missing arg, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestResume_EmptyArg(t *testing.T) {
	err := (&resumeKong{ID: ""}).Validate()
	if err == nil {
		t.Fatal("expected UsageError for empty arg, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// ---- resumeKong: unknown id ------------------------------------------------

func TestResume_UnknownID(t *testing.T) {
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			return "", fmt.Errorf("bd show: not found")
		},
	}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())

	err := (&resumeKong{ID: "at-nosuchid"}).Run(ctx)
	if err == nil {
		t.Fatal("expected error for unknown id, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "no such initiative") {
		t.Errorf("expected 'no such initiative' in stderr, got: %s", stderr.String())
	}
}

// ---- resumeKong: closed initiative -----------------------------------------

func TestResume_ClosedInitiative(t *testing.T) {
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{
				ID:          "at-closed1",
				Status:      "closed",
				Description: "worktree: /some/path\n",
			}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())

	err := (&resumeKong{ID: "at-closed1"}).Run(ctx)
	if err == nil {
		t.Fatal("expected error for closed initiative, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "closed") {
		t.Errorf("expected 'closed' in stderr, got: %s", stderr.String())
	}
}

// ---- resumeKong: missing worktree line -------------------------------------

func TestResume_NoWorktreeLine(t *testing.T) {
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{
				ID:          "at-nowt1",
				Status:      "open",
				Description: "problem: no worktree here\n",
			}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())

	err := (&resumeKong{ID: "at-nowt1"}).Run(ctx)
	if err == nil {
		t.Fatal("expected error for missing worktree line, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "no worktree") {
		t.Errorf("expected 'no worktree' in stderr, got: %s", stderr.String())
	}
}

// ---- resumeKong: worktree path does not exist ------------------------------

func TestResume_MissingWorktreePath(t *testing.T) {
	missingPath := "/no/such/worktree/path/ever"
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{
				ID:          "at-nowt2",
				Status:      "open",
				Description: "worktree: " + missingPath + "\n",
			}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())

	err := (&resumeKong{ID: "at-nowt2"}).Run(ctx)
	if err == nil {
		t.Fatal("expected error for missing worktree path, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), missingPath) {
		t.Errorf("expected path %q in stderr, got: %s", missingPath, stderr.String())
	}
}

// ---- resumeKong: claude not in PATH ----------------------------------------

func TestResume_MissingClaude(t *testing.T) {
	if _, err := exec.LookPath("claude"); err == nil {
		t.Skip("claude is in PATH; skipping missing-claude test")
	}
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{
				ID:          "at-noclaude",
				Status:      "open",
				Description: "worktree: " + dir + "\n",
			}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	cmd := &resumeKong{ID: "at-noclaude", launch: launchBGSession}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected DepError, got nil")
	}
	if code := cli.ExitCode(err); code != 3 {
		t.Errorf("expected exit 3 (DepError), got %d", code)
	}
}

// ---- resumeKong: happy path (stubbed launch) --------------------------------

func TestResume_HappyPath(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{
				ID:          "at-happy1",
				Status:      "open",
				Description: "worktree: " + dir + "\n",
			}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}

	var launchedDir, launchedArg, launchedRole, launchedInitiative string
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &resumeKong{
		ID: "at-happy1",
		launch: func(_ *cli.Context, d, arg, role, initiativeID string) error {
			launchedDir = d
			launchedArg = arg
			launchedRole = role
			launchedInitiative = initiativeID
			return nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if launchedDir != dir {
		t.Errorf("launch dir = %q, want %q", launchedDir, dir)
	}
	if launchedArg != "at-happy1" {
		t.Errorf("launch driArg = %q, want %q", launchedArg, "at-happy1")
	}
	if launchedRole != "dri" {
		t.Errorf("launch role = %q, want %q", launchedRole, "dri")
	}
	if launchedInitiative != "at-happy1" {
		t.Errorf("launch initiativeID = %q, want %q", launchedInitiative, "at-happy1")
	}

	out := stdout.String()
	basename := filepath.Base(dir)
	checks := []string{
		"initiative_id: at-happy1",
		"worktree: " + dir,
		"Background session launched: " + basename,
		"claude attach " + basename,
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q\ngot:\n%s", want, out)
		}
	}
}

func TestResume_CodexUsesLastSessionAndRuntimeControls(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{runFn: func(args ...string) (string, error) {
		issues := []bd.Issue{{
			ID:     "at-codex-resume",
			Status: "open",
			Description: "worktree: " + dir + "\n" +
				"runtime: codex\n" +
				"session: old-thread\n" +
				"session: active-thread\n",
		}}
		raw, _ := json.Marshal(issues)
		return string(raw), nil
	}}
	var started runtimeStartRequest
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &resumeKong{
		ID: "at-codex-resume",
		launch: func(*cli.Context, string, string, string, string) error {
			t.Fatal("Claude launcher called")
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
	if started.Runtime != sessionruntime.Codex || started.ResumeID != "active-thread" || started.Prompt != codexDRIPrompt("at-codex-resume") {
		t.Fatalf("runtime start = %+v", started)
	}
	if !strings.Contains(stdout.String(), "ateam runtime open codex") || strings.Contains(stdout.String(), "claude attach") {
		t.Fatalf("monitoring output is not Codex-specific:\n%s", stdout.String())
	}
}

func TestResume_RuntimeFailures(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name, description, assertion, want string
	}{
		{name: "unknown stored runtime", description: "runtime: other\n", want: "unknown runtime"},
		{name: "assertion mismatch", description: "runtime: codex\nsession: thread-1\n", assertion: "claude", want: "does not match"},
		{name: "codex missing session", description: "runtime: codex\n", want: "no session"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fbd := &fakeBD{runFn: func(args ...string) (string, error) {
				raw, _ := json.Marshal([]bd.Issue{{ID: "at-r", Status: "open", Description: "worktree: " + dir + "\n" + tt.description}})
				return string(raw), nil
			}}
			ctx, _, stderr := makeCtx(fbd, t.TempDir())
			err := (&resumeKong{ID: "at-r", Runtime: tt.assertion}).Run(ctx)
			combined := stderr.String()
			if err != nil {
				combined += err.Error()
			}
			if err == nil || !strings.Contains(combined, tt.want) {
				t.Fatalf("err=%v stderr=%q want %q", err, stderr.String(), tt.want)
			}
		})
	}
}

func TestResumeCodexCompatibilityFailurePreventsLaunch(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{runFn: func(...string) (string, error) {
		raw, _ := json.Marshal([]bd.Issue{{ID: "at-r", Status: "open", Description: "worktree: " + dir + "\nruntime: codex\nsession: thread-1\n"}})
		return string(raw), nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	cmd := &resumeKong{
		ID: "at-r",
		codexCheck: func(context.Context, string) error {
			return fmt.Errorf("official standalone installer required")
		},
		runtimeStart: func(*cli.Context, runtimeStartRequest) error {
			t.Fatal("runtime launched")
			return nil
		},
	}
	err := cmd.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "official standalone") {
		t.Fatalf("error = %v", err)
	}
}

// ---- resumeKong: --launch-prompt -------------------------------------------

func TestResume_CustomLaunchPromptUsesRawLaunch(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: "at-rr1", Status: "open", Description: "worktree: " + dir + "\n"}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	var gotDir, gotPrompt, gotModel, gotRole, gotInitiative string
	cmd := &resumeKong{
		ID:           "at-rr1",
		LaunchPrompt: "/agent-teams:review-pr at-rr1",
		Model:        "sonnet",
		launch: func(_ *cli.Context, _, _, _, _ string) error {
			t.Fatal("launch called; want launchRaw for --launch-prompt")
			return nil
		},
		launchRaw: func(_ *cli.Context, d, p, m, _, role, initiativeID string) error {
			gotDir, gotPrompt, gotModel, gotRole, gotInitiative = d, p, m, role, initiativeID
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotDir != dir {
		t.Errorf("launchRaw dir = %q, want %q", gotDir, dir)
	}
	if gotPrompt != "/agent-teams:review-pr at-rr1" {
		t.Errorf("launchRaw prompt = %q", gotPrompt)
	}
	if gotModel != "sonnet" {
		t.Errorf("launchRaw model = %q, want sonnet", gotModel)
	}
	if gotRole != "dri" {
		t.Errorf("launchRaw role = %q, want %q", gotRole, "dri")
	}
	if gotInitiative != "at-rr1" {
		t.Errorf("launchRaw initiativeID = %q, want %q", gotInitiative, "at-rr1")
	}
}

func TestResume_NoLaunchPromptUsesDriLaunch(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: "at-rr2", Status: "open", Description: "worktree: " + dir + "\n"}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	var gotArg, gotRole, gotInitiative string
	cmd := &resumeKong{
		ID: "at-rr2",
		launch: func(_ *cli.Context, _, arg, role, initiativeID string) error {
			gotArg, gotRole, gotInitiative = arg, role, initiativeID
			return nil
		},
		launchRaw: func(_ *cli.Context, _, _, _, _, _, _ string) error {
			t.Fatal("launchRaw called; want launch for default path")
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotArg != "at-rr2" {
		t.Errorf("launch driArg = %q, want at-rr2", gotArg)
	}
	if gotRole != "dri" {
		t.Errorf("launch role = %q, want %q", gotRole, "dri")
	}
	if gotInitiative != "at-rr2" {
		t.Errorf("launch initiativeID = %q, want %q", gotInitiative, "at-rr2")
	}
}

func TestResume_ModelWithoutLaunchPromptRejected(t *testing.T) {
	err := (&resumeKong{ID: "at-x", Model: "sonnet"}).Validate()
	if err == nil {
		t.Fatal("expected UsageError for --model without --launch-prompt, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// ---- resumeKong: duplicate-live-session guard (agent-teams-ndr4.1) --------

// livePID is a placeholder PID for a fake live agentSession in the tests
// below; only presence (non-nil), never the value, is meaningful.
var livePID = 4242

func TestResume_NoLiveSession_Launches(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: "at-nolive", Status: "open", Description: "worktree: " + dir + "\n"}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	var launched bool
	cmd := &resumeKong{
		ID:         "at-nolive",
		agentsFunc: func() ([]agentSession, error) { return nil, nil },
		launch: func(_ *cli.Context, _, _, _, _ string) error {
			launched = true
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !launched {
		t.Fatal("expected launch to be called when no live session exists")
	}
}

func TestResume_LiveSessionNoSupersede_RefusesAndNamesID(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: "at-live1", Status: "open", Description: "worktree: " + dir + "\n"}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())

	cmd := &resumeKong{
		ID: "at-live1",
		agentsFunc: func() ([]agentSession, error) {
			return []agentSession{{Name: filepath.Base(dir), ID: "sess-abc", PID: &livePID}}, nil
		},
		launch: func(_ *cli.Context, _, _, _, _ string) error {
			t.Fatal("launch called; want refusal when a live session exists")
			return nil
		},
	}
	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error refusing to resume, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "sess-abc") {
		t.Errorf("expected live session id %q in stderr, got: %s", "sess-abc", stderr.String())
	}
	if !strings.Contains(stderr.String(), "--supersede") {
		t.Errorf("expected --supersede mentioned in stderr, got: %s", stderr.String())
	}
}

func TestResume_LiveSessionWithSupersede_StopsThenLaunches(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: "at-live2", Status: "open", Description: "worktree: " + dir + "\n"}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	var stoppedID string
	var stopCalledBeforeLaunch, launched bool
	cmd := &resumeKong{
		ID:        "at-live2",
		Supersede: true,
		agentsFunc: func() ([]agentSession, error) {
			return []agentSession{{Name: filepath.Base(dir), ID: "sess-xyz", PID: &livePID}}, nil
		},
		stopSession: func(id string) error {
			stoppedID = id
			stopCalledBeforeLaunch = !launched
			return nil
		},
		launch: func(_ *cli.Context, _, _, _, _ string) error {
			launched = true
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stoppedID != "sess-xyz" {
		t.Errorf("stopSession id = %q, want %q", stoppedID, "sess-xyz")
	}
	if !launched {
		t.Fatal("expected launch to be called after superseding the live session")
	}
	if !stopCalledBeforeLaunch {
		t.Fatal("expected stopSession to be called before launch (stop-then-spawn)")
	}
}
