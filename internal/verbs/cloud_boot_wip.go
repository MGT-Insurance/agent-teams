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

const cloudBootWIPMessage = "WIP: unsaved work from a failed cloud run"

// Tunables, vars so tests can shrink them. cloudBootGitTimeout bounds each git
// call and cloudBootWIPBudget the whole save, so a stuck remote cannot hold the
// run's exit. cloudBootWaitDelay is how long after a timeout git's pipes may
// stay open (a git-remote-https or ssh grandchild can hold them) before the
// call gives up.
var (
	cloudBootGitTimeout = 60 * time.Second
	cloudBootWIPBudget  = 2 * time.Minute
	cloudBootWaitDelay  = 3 * time.Second
)

// cloudGitFunc runs git in dir, bounded by ctx, and returns its trimmed stdout.
type cloudGitFunc func(ctx context.Context, dir string, args ...string) (string, error)

// runGitBounded runs git with no credential prompt, bounded by the shorter of
// ctx and cloudBootGitTimeout.
func runGitBounded(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, cloudBootGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.WaitDelay = cloudBootWaitDelay
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
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
// (git add -A, so .gitignore applies; .claude/ is excluded) and pushes HEAD to a
// wip/ ref on origin without force, then notes the ref on the initiative. The
// whole step shares one deadline. Failures are logged to stderr and skipped; it
// never changes the caller's exit.
func (c *cloudBootCommon) saveWIP(ctx *cli.Context, id, cwd string) {
	git := c.Git
	if git == nil {
		git = runGitBounded
	}
	bctx, cancel := context.WithTimeout(context.Background(), cloudBootWIPBudget)
	defer cancel()
	list, err := git(bctx, cwd, "worktree", "list", "--porcelain")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "cloud-boot: save wip: %v\n", err)
		return
	}
	used := map[string]bool{}
	for _, wt := range parseWorktreeList(list) {
		if err := bctx.Err(); err != nil {
			fmt.Fprintf(ctx.Stderr, "cloud-boot: save wip: out of time, skipping %s: %v\n", wt.Dir, err)
			continue
		}
		if err := c.saveWorktreeWIP(bctx, ctx, git, used, id, wt); err != nil {
			fmt.Fprintf(ctx.Stderr, "cloud-boot: save wip %s: %v\n", wt.Dir, err)
		}
	}
}

// wipExcludeClaude keeps a Claude session's local settings out of the save.
const wipExcludeClaude = ":(exclude).claude"

func (c *cloudBootCommon) saveWorktreeWIP(bctx context.Context, ctx *cli.Context, git cloudGitFunc, used map[string]bool, id string, wt cloudWorktree) error {
	dirty, err := git(bctx, wt.Dir, "status", "--porcelain", "--", ".", wipExcludeClaude)
	if err != nil {
		return err
	}
	unpushed, err := git(bctx, wt.Dir, "rev-list", "HEAD", "--not", "--remotes", "--max-count=1")
	if err != nil {
		return err
	}
	if dirty == "" && unpushed == "" {
		return nil
	}
	if dirty != "" {
		if _, err := git(bctx, wt.Dir, "add", "-A", "--", ".", wipExcludeClaude); err != nil {
			return err
		}
		// Dirty submodule content shows in status but stages nothing.
		staged, err := git(bctx, wt.Dir, "diff", "--cached", "--name-only")
		if err != nil {
			return err
		}
		if staged != "" {
			if _, err := git(bctx, wt.Dir, "-c", "user.name=ateam cloud-boot", "-c", "user.email=ateam@localhost",
				"-c", "commit.gpgsign=false", "commit", "--no-verify", "-m", cloudBootWIPMessage); err != nil {
				return err
			}
		} else if unpushed == "" {
			return nil
		}
	}
	sha, err := git(bctx, wt.Dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	ref := wipRefName(id, wt.Branch, wt.Dir)
	if used[ref] {
		ref += "-" + sha[:min(7, len(sha))]
	}
	push := func(ref string) error {
		_, err := git(bctx, wt.Dir, "push", "--no-verify", "origin", "HEAD:refs/heads/"+ref)
		return err
	}
	if err := push(ref); err != nil {
		// A rejected push means the ref exists with other work; never overwrite it.
		ref += "-" + sha[:min(7, len(sha))]
		if err2 := push(ref); err2 != nil {
			return err2
		}
	}
	used[ref] = true
	fmt.Fprintf(ctx.Stderr, "cloud-boot: saved wip %s at %s\n", ref, sha)
	dsp, _, _ := dispatcherRun()
	return c.noteWIP(bctx, ctx, id, fmt.Sprintf("wip saved: %s at %s from run %s", ref, sha, dsp))
}

// noteWIP adds a bd note on the initiative through ctx.BD, the same path
// reap and hung-tick use.
// The BD seam takes no context, so the call runs in a goroutine and is
// abandoned when bctx expires.
func (c *cloudBootCommon) noteWIP(bctx context.Context, ctx *cli.Context, id, text string) error {
	tmp, err := os.CreateTemp("", "ateam-cloud-boot-wip-*")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(text + "\n"); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	done := make(chan error, 1)
	go func() {
		_, err := ctx.BD.Run("note", id, "--file="+tmp.Name())
		os.Remove(tmp.Name())
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("bd note: %w", err)
		}
		return nil
	case <-bctx.Done():
		return fmt.Errorf("bd note: %w", bctx.Err())
	}
}
