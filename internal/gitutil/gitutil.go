// Package gitutil provides small, injectable helpers around git operations
// and string slugification for the ateam CLI.
package gitutil

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

// ExecFunc is the signature for running an external command. Swap via
// NewRunner for unit-testing without real git.
type ExecFunc func(name string, args ...string) (stdout []byte, stderr []byte, err error)

// Runner wraps an ExecFunc for git calls so callers can inject a fake in tests.
type Runner struct {
	exec ExecFunc
}

// New returns a Runner using the real git binary.
func New() *Runner { return &Runner{exec: defaultExec} }

// NewWithExec returns a Runner using execFn. Use in tests.
func NewWithExec(execFn ExecFunc) *Runner { return &Runner{exec: execFn} }

func defaultExec(name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.Command(name, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// RepoRoot returns the absolute path to the git repository root for dir.
// Equivalent to: git -C <dir> rev-parse --show-toplevel
func (r *Runner) RepoRoot(dir string) (string, error) {
	out, errOut, err := r.exec("git", "-C", dir, "rev-parse", "--show-toplevel")
	if err != nil {
		msg := strings.TrimSpace(string(errOut))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("not inside a git repo: %s (%s)", dir, msg)
	}
	return strings.TrimSpace(string(out)), nil
}

// CommonDir returns the path to the shared .git directory for dir. When dir is
// inside a worktree, this resolves to the common repo's .git dir; for a normal
// checkout it equals the repo root's .git directory. Callers use this to
// identify the canonical repo when dir may be a worktree.
// Equivalent to: git -C <dir> rev-parse --git-common-dir
func (r *Runner) CommonDir(dir string) (string, error) {
	out, errOut, err := r.exec("git", "-C", dir, "rev-parse", "--git-common-dir")
	if err != nil {
		msg := strings.TrimSpace(string(errOut))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git-common-dir: %s (%s)", dir, msg)
	}
	raw := strings.TrimSpace(string(out))
	// --git-common-dir may return a relative path; resolve it against dir.
	if !filepath.IsAbs(raw) {
		raw = filepath.Join(dir, raw)
	}
	return filepath.Clean(raw), nil
}

// DefaultBranch detects the default remote branch for repoRoot by reading
// refs/remotes/origin/HEAD. Returns "main" if the ref is absent or unreadable.
// Equivalent to: git -C <repoRoot> symbolic-ref refs/remotes/origin/HEAD
func (r *Runner) DefaultBranch(repoRoot string) string {
	out, _, err := r.exec("git", "-C", repoRoot, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err != nil {
		return "main"
	}
	// Output is e.g. "refs/remotes/origin/main\n"; extract the branch name.
	ref := strings.TrimSpace(string(out))
	// Strip "refs/remotes/origin/" prefix.
	const pfx = "refs/remotes/origin/"
	if strings.HasPrefix(ref, pfx) {
		return strings.TrimPrefix(ref, pfx)
	}
	return "main"
}

// WorktreeExists reports true if wtPath is already registered as a worktree of
// repoRoot OR if the directory already exists on disk (collision check).
func (r *Runner) WorktreeExists(repoRoot, wtPath string) bool {
	// Quick filesystem check first.
	if _, err := os.Stat(wtPath); err == nil {
		return true
	}
	// Also check git's worktree list in case the directory was removed manually
	// but git still tracks it.
	out, _, err := r.exec("git", "-C", repoRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			registered := strings.TrimPrefix(line, "worktree ")
			if filepath.Clean(registered) == filepath.Clean(wtPath) {
				return true
			}
		}
	}
	return false
}

// AddWorktree creates a new worktree at wtPath on a new branch named branch
// branching from base.
// Equivalent to: git -C <repoRoot> worktree add <wtPath> -b <branch> <base>
func (r *Runner) AddWorktree(repoRoot, wtPath, branch, base string) error {
	_, errOut, err := r.exec("git", "-C", repoRoot, "worktree", "add", wtPath, "-b", branch, base)
	if err != nil {
		msg := strings.TrimSpace(string(errOut))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("git worktree add: %s", msg)
	}
	return nil
}

// RemoveWorktree removes the worktree at wtPath from repoRoot with --force so
// that it works even when the worktree has uncommitted changes.
// Equivalent to: git -C <repoRoot> worktree remove <wtPath> --force
func (r *Runner) RemoveWorktree(repoRoot, wtPath string) error {
	_, errOut, err := r.exec("git", "-C", repoRoot, "worktree", "remove", wtPath, "--force")
	if err != nil {
		msg := strings.TrimSpace(string(errOut))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("git worktree remove: %s", msg)
	}
	return nil
}

// Slugify converts s to a kebab-case slug: lowercase, runs of non-alphanumeric
// characters become a single "-", leading/trailing "-" are trimmed, and the
// result is capped at 50 characters (on a word boundary where possible).
// Returns "" when the input produces no alphanumeric characters.
func Slugify(s string) string {
	var b strings.Builder
	inSep := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if inSep && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			inSep = false
		} else {
			inSep = true
		}
	}
	slug := b.String()
	// Cap at 50 characters, trimming at the last '-' if possible.
	const maxLen = 50
	if len(slug) > maxLen {
		slug = slug[:maxLen]
		if i := strings.LastIndexByte(slug, '-'); i > 0 {
			slug = slug[:i]
		}
	}
	return slug
}
