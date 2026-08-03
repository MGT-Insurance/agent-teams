package verbs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/repoconfig"
)

// fakeEnableRepoGit implements enableRepoGit for tests; scoped to this file
// so it never depends on how other tracks' test files evolve their own
// fakes in the same package.
type fakeEnableRepoGit struct {
	repoRootFn func(dir string) (string, error)
}

func (f *fakeEnableRepoGit) RepoRoot(dir string) (string, error) {
	if f.repoRootFn != nil {
		return f.repoRootFn(dir)
	}
	return dir, nil
}

func TestEnableRepoKong_Run(t *testing.T) {
	tests := []struct {
		name          string
		markerContent *string // nil = no marker file present
		wantSuffix    string
	}{
		{
			name:          "missing marker created",
			markerContent: nil,
			wantSuffix:    "(created)",
		},
		{
			name:          "disabled marker undisabled",
			markerContent: strPtr("disabled: true\n"),
			wantSuffix:    `(removed "disabled: true")`,
		},
		{
			name:          "already enabled marker",
			markerContent: strPtr("disabled: false\n"),
			wantSuffix:    "(already enabled)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.markerContent != nil {
				if err := os.WriteFile(filepath.Join(root, repoconfig.FileName), []byte(*tt.markerContent), 0o644); err != nil {
					t.Fatalf("seed marker: %v", err)
				}
			}

			var stdout, stderr bytes.Buffer
			ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}
			cmd := &enableRepoKong{
				git: &fakeEnableRepoGit{repoRootFn: func(dir string) (string, error) { return root, nil }},
			}

			if err := cmd.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}

			wantLine := "enabled: " + filepath.Join(root, repoconfig.FileName) + " " + tt.wantSuffix + "\n"
			if stdout.String() != wantLine {
				t.Errorf("stdout = %q, want %q", stdout.String(), wantLine)
			}
			if stderr.String() != "" {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
			if !repoconfig.Enabled(root) {
				t.Error("repoconfig.Enabled(root) = false after successful Run, want true")
			}
		})
	}
}

func TestEnableRepoKong_SubdirectoryResolvesToRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}
	cmd := &enableRepoKong{
		Path: sub,
		git: &fakeEnableRepoGit{repoRootFn: func(dir string) (string, error) {
			if dir != sub {
				t.Errorf("RepoRoot called with %q, want %q", dir, sub)
			}
			return root, nil
		}},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if _, err := os.Stat(filepath.Join(root, repoconfig.FileName)); err != nil {
		t.Errorf("marker not created at repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sub, repoconfig.FileName)); err == nil {
		t.Error("marker created in subdirectory, want it only at repo root")
	}
}

func TestEnableRepoKong_NotAGitRepo(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}
	cmd := &enableRepoKong{
		Path: dir,
		git: &fakeEnableRepoGit{repoRootFn: func(string) (string, error) {
			return "", errors.New("not a git repository")
		}},
	}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil for a non-git path")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("ExitCode(err) = %d, want 1", code)
	}
	if stdout.String() != "" {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if stderr.String() == "" {
		t.Error("stderr is empty, want a refusal message")
	}
	if _, err := os.Stat(filepath.Join(dir, repoconfig.FileName)); err == nil {
		t.Error("marker file was written despite RepoRoot failing")
	}
}

func strPtr(s string) *string { return &s }
