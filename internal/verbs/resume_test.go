package verbs

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ---- worktreePath helper ---------------------------------------------------

func TestWorktreePath_PresentFirstLine(t *testing.T) {
	desc := "worktree: /some/path\nbranch: main\n"
	got := worktreePath(desc)
	if got != "/some/path" {
		t.Errorf("got %q, want %q", got, "/some/path")
	}
}

func TestWorktreePath_PresentMidDescription(t *testing.T) {
	desc := "problem: do stuff\nrepo: /r\nworktree: /wt/path\nbranch: feat\n"
	got := worktreePath(desc)
	if got != "/wt/path" {
		t.Errorf("got %q, want %q", got, "/wt/path")
	}
}

func TestWorktreePath_Absent(t *testing.T) {
	desc := "problem: do stuff\nbranch: feat\n"
	got := worktreePath(desc)
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestWorktreePath_EmptyDescription(t *testing.T) {
	got := worktreePath("")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestWorktreePath_TrailingCR(t *testing.T) {
	desc := "worktree: /wt/path\r\nbranch: x\n"
	got := worktreePath(desc)
	if got != "/wt/path" {
		t.Errorf("got %q, want %q", got, "/wt/path")
	}
}

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

	var launchedDir, launchedArg string
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &resumeKong{
		ID: "at-happy1",
		launch: func(_ *cli.Context, d, arg string) error {
			launchedDir = d
			launchedArg = arg
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
