// cloud_boot_wip.go — the save-WIP step of `ateam cloud-boot`. When a cloud run
// ends in an error, Dispatcher deletes the sandbox, so unpushed commits and
// uncommitted edits would be lost. saveWIP pushes them to wip/ branches on
// origin and notes each ref on the initiative. It is reachable only from
// cloud-boot's idleLoop, which requires a Dispatcher run.
package verbs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

const (
	// cloudBootGitTimeout bounds each git call so a stuck push cannot hold the
	// run's exit.
	cloudBootGitTimeout = 60 * time.Second
	cloudBootWIPMessage = "WIP: unsaved work from a failed cloud run"
)

// runGitBounded runs git in dir with a timeout and no credential prompt, and
// returns its trimmed stdout.
func runGitBounded(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cloudBootGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, stderr)
	}
	return strings.TrimSpace(string(out)), nil
}

var wipNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// wipRefName returns the wip/ ref for a worktree: wip/<initiative>/<name>,
// where name is the sanitized branch, or the directory name when detached.
// The initiative in the path keeps refs from colliding across initiatives.
func wipRefName(initiativeID, branch, dir string) string {
	name := branch
	if name == "" {
		name = filepath.Base(dir)
	}
	name = strings.Trim(wipNameUnsafe.ReplaceAllString(name, "-"), "-.")
	if name == "" {
		name = "worktree"
	}
	return "wip/" + initiativeID + "/" + name
}

// cloudWorktree is one entry of `git worktree list --porcelain`.
type cloudWorktree struct{ Dir, Branch string }

func parseWorktreeList(porcelain string) []cloudWorktree {
	var out []cloudWorktree
	for _, line := range strings.Split(porcelain, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			out = append(out, cloudWorktree{Dir: strings.TrimPrefix(line, "worktree ")})
		case strings.HasPrefix(line, "branch ") && len(out) > 0:
			out[len(out)-1].Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "bare" && len(out) > 0:
			out = out[:len(out)-1]
		}
	}
	return out
}

// saveWIP preserves the run checkout's unsaved work: for each worktree with
// uncommitted changes or commits on no remote branch, it commits the changes
// (git add -A, so .gitignore applies) and force-pushes HEAD to a wip/ ref on
// origin, then notes the ref on the initiative. Failures are logged to stderr
// and skipped; it never changes the caller's exit.
func (c *cloudBootCommon) saveWIP(ctx *cli.Context, id, cwd string) {
	git := c.Git
	if git == nil {
		git = runGitBounded
	}
	list, err := git(cwd, "worktree", "list", "--porcelain")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "cloud-boot: save wip: %v\n", err)
		return
	}
	for _, wt := range parseWorktreeList(list) {
		if err := c.saveWorktreeWIP(ctx, git, id, wt); err != nil {
			fmt.Fprintf(ctx.Stderr, "cloud-boot: save wip %s: %v\n", wt.Dir, err)
		}
	}
}

func (c *cloudBootCommon) saveWorktreeWIP(ctx *cli.Context, git func(string, ...string) (string, error), id string, wt cloudWorktree) error {
	dirty, err := git(wt.Dir, "status", "--porcelain")
	if err != nil {
		return err
	}
	unpushed, err := git(wt.Dir, "rev-list", "HEAD", "--not", "--remotes", "--max-count=1")
	if err != nil {
		return err
	}
	if dirty == "" && unpushed == "" {
		return nil
	}
	if dirty != "" {
		if _, err := git(wt.Dir, "add", "-A"); err != nil {
			return err
		}
		if _, err := git(wt.Dir, "-c", "user.name=ateam cloud-boot", "-c", "user.email=ateam@localhost",
			"-c", "commit.gpgsign=false", "commit", "--no-verify", "-m", cloudBootWIPMessage); err != nil {
			return err
		}
	}
	sha, err := git(wt.Dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	ref := wipRefName(id, wt.Branch, wt.Dir)
	if _, err := git(wt.Dir, "push", "--no-verify", "--force", "origin", "HEAD:refs/heads/"+ref); err != nil {
		return err
	}
	fmt.Fprintf(ctx.Stderr, "cloud-boot: saved wip %s at %s\n", ref, sha)
	dsp, _, _ := dispatcherRun()
	return c.noteWIP(ctx, id, fmt.Sprintf("wip saved: %s at %s from run %s", ref, sha, dsp))
}

// noteWIP adds a bd note on the initiative through ctx.BD, the same path
// reap and hung-tick use.
func (c *cloudBootCommon) noteWIP(ctx *cli.Context, id, text string) error {
	tmp, err := os.CreateTemp("", "ateam-cloud-boot-wip-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(text + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if _, err := ctx.BD.Run("note", id, "--file="+tmp.Name()); err != nil {
		return fmt.Errorf("bd note: %w", err)
	}
	return nil
}
