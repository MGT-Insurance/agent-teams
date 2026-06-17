package verbs

import (
	"fmt"
	"os/exec"
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

// ---- resumeCommand: nil context --------------------------------------------

func TestResume_NilContext(t *testing.T) {
	cmd := &resumeCommand{}
	err := cmd.Run(nil, []string{"at-abc"})
	if err == nil {
		t.Fatal("expected error for nil context, got nil")
	}
}

// ---- resumeCommand: missing arg --------------------------------------------

func TestResume_MissingArg(t *testing.T) {
	ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
	cmd := &resumeCommand{}

	err := cmd.Run(ctx, []string{})
	if err == nil {
		t.Fatal("expected UsageError for missing arg, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestResume_EmptyArg(t *testing.T) {
	ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
	cmd := &resumeCommand{}

	err := cmd.Run(ctx, []string{""})
	if err == nil {
		t.Fatal("expected UsageError for empty arg, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// ---- resumeCommand: unknown id ---------------------------------------------

func TestResume_UnknownID(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			return fmt.Errorf("bd show: not found")
		},
	}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())
	cmd := &resumeCommand{}

	err := cmd.Run(ctx, []string{"at-nosuchid"})
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

// ---- resumeCommand: closed initiative --------------------------------------

func TestResume_ClosedInitiative(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-closed1"
				issue.Status = "closed"
				issue.Description = "worktree: /some/path\n"
			}
			return nil
		},
	}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())
	cmd := &resumeCommand{}

	err := cmd.Run(ctx, []string{"at-closed1"})
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

// ---- resumeCommand: missing worktree line ----------------------------------

func TestResume_NoWorktreeLine(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-nowt1"
				issue.Status = "open"
				issue.Description = "problem: no worktree here\n"
			}
			return nil
		},
	}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())
	cmd := &resumeCommand{}

	err := cmd.Run(ctx, []string{"at-nowt1"})
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

// ---- resumeCommand: worktree path does not exist ---------------------------

func TestResume_MissingWorktreePath(t *testing.T) {
	missingPath := "/no/such/worktree/path/ever"
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-nowt2"
				issue.Status = "open"
				issue.Description = "worktree: " + missingPath + "\n"
			}
			return nil
		},
	}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())
	cmd := &resumeCommand{}

	err := cmd.Run(ctx, []string{"at-nowt2"})
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

// ---- resumeCommand: claude not in PATH -------------------------------------

func TestResume_MissingClaude(t *testing.T) {
	if _, err := exec.LookPath("claude"); err == nil {
		t.Skip("claude is in PATH; skipping missing-claude test")
	}
	dir := t.TempDir()
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-noclaude"
				issue.Status = "open"
				issue.Description = "worktree: " + dir + "\n"
			}
			return nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	cmd := &resumeCommand{}

	err := cmd.Run(ctx, []string{"at-noclaude"})
	if err == nil {
		t.Fatal("expected DepError, got nil")
	}
	if code := cli.ExitCode(err); code != 3 {
		t.Errorf("expected exit 3 (DepError), got %d", code)
	}
}

// ---- resumeCommand: happy path (claude present) ----------------------------

func TestResume_HappyPath(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not in PATH; skipping happy-path test")
	}
	dir := t.TempDir()
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-happy1"
				issue.Status = "open"
				issue.Description = "worktree: " + dir + "\n"
			}
			return nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	cmd := &resumeCommand{}

	// We can't prevent claude --bg from running in a test, so only confirm
	// that all pre-flight guardrails pass (no exit-1/2/3 before the launch).
	// The actual launch will either succeed or fail depending on the environment;
	// we only care that the guardrail path produced no silent error.
	err := cmd.Run(ctx, []string{"at-happy1"})
	if err != nil {
		code := cli.ExitCode(err)
		if code == 1 || code == 2 {
			t.Errorf("guardrail fired unexpectedly (exit %d): %v", code, err)
		}
		// exit 3 (claude not found) is skipped above, so any remaining error
		// is from the actual launch — acceptable in a unit test environment.
	}
}
