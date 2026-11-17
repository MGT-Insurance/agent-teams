// cloud_boot_restore.go — the restore step of `ateam cloud-boot`. A restarted
// run starts in a fresh sandbox, so work that an earlier attempt saved to
// wip/<initiative>/* refs (see cloud_boot_wip.go) is not on disk. restoreWIP
// recreates each saved worktree at its recorded path and branch before the
// session launches, and returns a summary for the session's system prompt.
// It never merges, resets, forces, or deletes a ref: any save it cannot place
// by a strict fast-forward is listed for the model instead.
package verbs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

type wipRestored struct{ Branch, Where, Ref, SHA string }

type wipNeedsYou struct{ Label, Ref, SHA, Reason, Advice string }

// wipNeed is a reason a save was not restored plus what the model can do about
// it. Errors that are not a wipNeed are git failures and carry no advice.
type wipNeed struct{ reason, advice string }

func (w *wipNeed) Error() string { return w.reason }

// briefErr renders a git failure on one line of at most 200 characters.
func briefErr(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// wipRestoredKey is the sandbox-local git config key that records the sha
// restored for ref. The record lives in .git/config, is never pushed, and a
// fresh sandbox has none.
func wipRestoredKey(ref string) string { return "ateam.restored." + ref + ".sha" }

// wipAlreadyRestored reports whether this sandbox already restored ref at sha.
// It stays true after the model rebases or amends the restored branch.
func (c *cloudBootCommon) wipAlreadyRestored(bctx context.Context, git cloudGitFunc, cwd, ref, sha string) bool {
	got, err := git(bctx, cwd, "config", "--local", "--get", wipRestoredKey(ref))
	return err == nil && got == sha
}

// restoreWIP restores every wip ref saved for initiative id into the sandbox
// whose run checkout is cwd. Failures are logged to stderr and listed in the
// returned summary; it never fails the boot. The summary is empty when there is
// nothing to report.
func (c *cloudBootCommon) restoreWIP(ctx *cli.Context, id, cwd string) string {
	git := c.Git
	if git == nil {
		git = runGitBounded
	}
	bctx, cancel := context.WithTimeout(context.Background(), cloudBootWIPBudget)
	defer cancel()

	out, err := git(bctx, cwd, "ls-remote", "origin", "refs/heads/wip/"+id+"/*")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "cloud-boot: restore wip: %v\n", err)
		return ""
	}
	type wipRef struct{ ref, sha string }
	var refs []wipRef
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.HasPrefix(f[1], "refs/heads/") {
			refs = append(refs, wipRef{strings.TrimPrefix(f[1], "refs/heads/"), f[0]})
		}
	}
	if len(refs) == 0 {
		return ""
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ref < refs[j].ref })

	var restored []wipRestored
	var needs []wipNeedsYou
	for _, r := range refs {
		if err := bctx.Err(); err != nil {
			needs = append(needs, wipNeedsYou{r.ref, r.ref, short7(r.sha), "out of time: " + err.Error(), ""})
			continue
		}
		if c.wipAlreadyRestored(bctx, git, cwd, r.ref, r.sha) {
			wipSkipped(ctx, r.ref, "this sandbox already restored "+short7(r.sha))
			continue
		}
		done, n, err := c.restoreWIPRef(bctx, ctx, git, id, cwd, r.ref, r.sha)
		switch {
		case err != nil:
			fmt.Fprintf(ctx.Stderr, "cloud-boot: restore wip %s: %v\n", r.ref, err)
			label := n.Label
			if label == "" {
				label = r.ref
			}
			var need *wipNeed
			if !errors.As(err, &need) {
				need = &wipNeed{reason: briefErr(err)}
			}
			needs = append(needs, wipNeedsYou{label, r.ref, short7(r.sha), need.reason, need.advice})
		case done != nil:
			restored = append(restored, *done)
		}
	}
	return wipSummary(restored, needs)
}

// wipRefNeeds carries the branch name found for a ref so an error can name it.
type wipRefNeeds struct{ Label string }

// restoreWIPRef places one saved ref. It returns (nil, _, nil) when the work is
// already present, and an error (the reason) when the model must handle it.
func (c *cloudBootCommon) restoreWIPRef(bctx context.Context, ctx *cli.Context, git cloudGitFunc, id, cwd, ref, sha string) (*wipRestored, wipRefNeeds, error) {
	var n wipRefNeeds
	if _, err := git(bctx, cwd, "fetch", "--no-tags", "origin", "+refs/heads/"+ref+":refs/remotes/origin/"+ref); err != nil {
		return nil, n, err
	}
	trailer := func(key string) (string, error) {
		v, err := git(bctx, cwd, "log", "-1", "--format=%(trailers:key="+key+",valueonly)", sha)
		return strings.TrimSpace(v), err
	}
	where, err := trailer(wipTrailerWorktree)
	if err != nil {
		return nil, n, err
	}
	if where == "" {
		return nil, n, &wipNeed{
			reason: fmt.Sprintf("saved before cloud-boot recorded where it came from (no %s trailer), so there is no restore record", wipTrailerWorktree),
			advice: "Inspect " + ref + " (git log origin/" + ref + ") and bring over what you need.",
		}
	}
	branch, err := trailer(wipTrailerBranch)
	if err != nil {
		return nil, n, err
	}
	n.Label = branch
	divergedAdvice := "Merge or rebase it, or raise a gate."
	byHand := "Inspect " + ref + " and bring over what you need."
	if branch != "" {
		remote, err := git(bctx, cwd, "ls-remote", "origin", "refs/heads/"+branch)
		if err != nil {
			return nil, n, err
		}
		if remote != "" {
			if _, err := git(bctx, cwd, "fetch", "--no-tags", "origin", "+refs/heads/"+branch+":refs/remotes/origin/"+branch); err != nil {
				return nil, n, err
			}
		}
	}

	// Already pushed or merged: reachable from a non-wip origin branch.
	heads, err := git(bctx, cwd, "for-each-ref", "--contains", sha, "--format=%(refname)", "refs/remotes/origin/")
	if err != nil {
		return nil, n, err
	}
	for _, h := range strings.Split(heads, "\n") {
		h = strings.TrimSpace(h)
		if h != "" && h != "refs/remotes/origin/HEAD" && !strings.HasPrefix(h, "refs/remotes/origin/wip/") {
			wipSkipped(ctx, ref, short7(sha)+" is already on "+strings.TrimPrefix(h, "refs/remotes/"))
			return nil, n, nil
		}
	}
	if branch != "" {
		if local, err := git(bctx, cwd, "rev-parse", "--verify", "-q", "refs/heads/"+branch); err == nil && local != "" {
			if _, err := git(bctx, cwd, "merge-base", "--is-ancestor", sha, local); err == nil {
				wipSkipped(ctx, ref, "local "+branch+" already contains "+short7(sha))
				return nil, n, nil
			}
			if _, err := git(bctx, cwd, "merge-base", "--is-ancestor", local, sha); err != nil {
				return nil, n, &wipNeed{reason: "diverged from local " + branch, advice: divergedAdvice}
			}
		}
		if remote, err := git(bctx, cwd, "rev-parse", "--verify", "-q", "refs/remotes/origin/"+branch); err == nil && remote != "" {
			if _, err := git(bctx, cwd, "merge-base", "--is-ancestor", remote, sha); err != nil {
				return nil, n, &wipNeed{reason: "diverged from origin/" + branch, advice: divergedAdvice}
			}
		}
	}

	var place string
	if where == wipRunCheckout {
		place = "the run checkout"
		if branch == "" {
			skip, err := detachGuard(bctx, git, cwd, sha, ref)
			if err != nil {
				return nil, n, err
			}
			if skip {
				wipSkipped(ctx, ref, "HEAD already contains "+short7(sha))
				return nil, n, nil
			}
		}
		dirty, err := git(bctx, cwd, "status", "--porcelain", "--", ".", wipExcludeClaude)
		if err != nil {
			return nil, n, err
		}
		if dirty != "" {
			return nil, n, &wipNeed{
				reason: fmt.Sprintf("the run checkout has uncommitted changes, so %s was not checked out", short7(sha)),
				advice: "Commit or stash that work, then check out " + ref + " by hand.",
			}
		}
		args := []string{"checkout", "-B", branch, sha}
		if branch == "" {
			args = []string{"checkout", "--detach", sha}
		}
		if _, err := git(bctx, cwd, args...); err != nil {
			return nil, n, err
		}
	} else {
		place = where
		if !filepath.IsAbs(where) {
			return nil, n, &wipNeed{reason: fmt.Sprintf("recorded worktree path %q is not absolute", where), advice: byHand}
		}
		if entries, err := os.ReadDir(where); err == nil && len(entries) > 0 {
			if branch == "" {
				if skip, err := detachGuard(bctx, git, where, sha, ref); err == nil && skip {
					wipSkipped(ctx, ref, "HEAD in "+where+" already contains "+short7(sha))
					return nil, n, nil
				}
			}
			return nil, n, &wipNeed{reason: fmt.Sprintf("%s already has content, so %s was not checked out there", where, short7(sha)), advice: byHand}
		}
		args := []string{"worktree", "add", "-B", branch, where, sha}
		if branch == "" {
			args = []string{"worktree", "add", "--detach", where, sha}
		}
		if _, err := git(bctx, cwd, args...); err != nil {
			return nil, n, err
		}
	}
	fmt.Fprintf(ctx.Stderr, "cloud-boot: restored wip %s at %s into %s\n", ref, sha, place)
	if _, err := git(bctx, cwd, "config", "--local", wipRestoredKey(ref), sha); err != nil {
		fmt.Fprintf(ctx.Stderr, "cloud-boot: restore wip %s: record: %v\n", ref, err)
	}
	if err := c.noteWIP(bctx, ctx, id, fmt.Sprintf("wip restored: %s at %s", ref, sha)); err != nil {
		fmt.Fprintf(ctx.Stderr, "cloud-boot: restore wip %s: note: %v\n", ref, err)
	}
	if branch == "" {
		branch = "(detached)"
	}
	return &wipRestored{Branch: branch, Where: place, Ref: ref, SHA: sha}, n, nil
}

// detachGuard decides whether a detached save may move dir's HEAD to sha. It
// reports skip when dir's HEAD already contains sha, and an error when HEAD has
// commits sha lacks, since detaching would leave them on no ref.
func detachGuard(bctx context.Context, git cloudGitFunc, dir, sha, ref string) (skip bool, err error) {
	head, err := git(bctx, dir, "rev-parse", "--verify", "-q", "HEAD")
	if err != nil || head == "" {
		return false, nil
	}
	if _, err := git(bctx, dir, "merge-base", "--is-ancestor", sha, head); err == nil {
		return true, nil
	}
	if _, err := git(bctx, dir, "merge-base", "--is-ancestor", head, sha); err == nil {
		return false, nil
	}
	return false, &wipNeed{
		reason: fmt.Sprintf("HEAD in %s has commits that %s does not contain", dir, short7(sha)),
		advice: "Merge or rebase it, or raise a gate. Inspect " + ref + " first.",
	}
}

// wipSkipped logs a ref that restore left alone because the work is already
// present. It goes to stderr only, not the model's summary.
func wipSkipped(ctx *cli.Context, ref, reason string) {
	fmt.Fprintf(ctx.Stderr, "cloud-boot: restore skipped %s: %s\n", ref, reason)
}

func short7(sha string) string { return sha[:min(7, len(sha))] }

// wipSummary renders the restore result for --append-system-prompt, or "".
func wipSummary(restored []wipRestored, needs []wipNeedsYou) string {
	var b strings.Builder
	if len(restored) > 0 {
		b.WriteString("\n\nRestored at boot (cloud-boot recreated these from wip refs; committed, not pushed):\n")
		for _, r := range restored {
			fmt.Fprintf(&b, "- %s at %s (%s %s)\n", r.Branch, r.Where, r.Ref, short7(r.SHA))
		}
		b.WriteString("Restored worktrees may need `ateam worktree-setup <path>`.\n")
	}
	if len(needs) > 0 {
		b.WriteString("\n\nNeeds you (cloud-boot did not touch these):\n")
		for _, n := range needs {
			fmt.Fprintf(&b, "- %s: %s %s %s.", n.Label, n.Ref, n.SHA, strings.TrimRight(n.Reason, "."))
			if n.Advice != "" {
				b.WriteString(" " + n.Advice)
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
