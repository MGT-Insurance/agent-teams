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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

type wipRestored struct{ Branch, Where, Ref, SHA string }

type wipNeedsYou struct{ Label, Ref, SHA, Reason string }

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

	// Bring every origin branch and wip ref up to date so reachability and
	// divergence checks see origin as it is now.
	_, fetchErr := git(bctx, cwd, "fetch", "--no-tags", "origin",
		"+refs/heads/*:refs/remotes/origin/*")
	var restored []wipRestored
	var needs []wipNeedsYou
	for _, r := range refs {
		if err := bctx.Err(); err != nil {
			needs = append(needs, wipNeedsYou{r.ref, r.ref, short7(r.sha), "out of time: " + err.Error()})
			continue
		}
		if fetchErr != nil {
			needs = append(needs, wipNeedsYou{r.ref, r.ref, short7(r.sha), "fetch failed: " + fetchErr.Error()})
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
			needs = append(needs, wipNeedsYou{label, r.ref, short7(r.sha), err.Error()})
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
	trailer := func(key string) (string, error) {
		v, err := git(bctx, cwd, "log", "-1", "--format=%(trailers:key="+key+",valueonly)", sha)
		return strings.TrimSpace(v), err
	}
	where, err := trailer(wipTrailerWorktree)
	if err != nil {
		return nil, n, err
	}
	if where == "" {
		return nil, n, fmt.Errorf("saved before cloud-boot recorded where it came from (no %s trailer)", wipTrailerWorktree)
	}
	branch, err := trailer(wipTrailerBranch)
	if err != nil {
		return nil, n, err
	}
	n.Label = branch

	// Already pushed or merged: reachable from a non-wip origin branch.
	heads, err := git(bctx, cwd, "for-each-ref", "--contains", sha, "--format=%(refname)", "refs/remotes/origin/")
	if err != nil {
		return nil, n, err
	}
	for _, h := range strings.Split(heads, "\n") {
		h = strings.TrimSpace(h)
		if h != "" && h != "refs/remotes/origin/HEAD" && !strings.HasPrefix(h, "refs/remotes/origin/wip/") {
			return nil, n, nil
		}
	}
	if branch != "" {
		if local, err := git(bctx, cwd, "rev-parse", "--verify", "-q", "refs/heads/"+branch); err == nil && local != "" {
			if _, err := git(bctx, cwd, "merge-base", "--is-ancestor", sha, local); err == nil {
				return nil, n, nil
			}
			if _, err := git(bctx, cwd, "merge-base", "--is-ancestor", local, sha); err != nil {
				return nil, n, fmt.Errorf("diverged from local %s", branch)
			}
		}
		if remote, err := git(bctx, cwd, "rev-parse", "--verify", "-q", "refs/remotes/origin/"+branch); err == nil && remote != "" {
			if _, err := git(bctx, cwd, "merge-base", "--is-ancestor", remote, sha); err != nil {
				return nil, n, fmt.Errorf("diverged from origin/%s", branch)
			}
		}
	}

	var place string
	if where == wipRunCheckout {
		place = "the run checkout"
		dirty, err := git(bctx, cwd, "status", "--porcelain", "--", ".", wipExcludeClaude)
		if err != nil {
			return nil, n, err
		}
		if dirty != "" {
			return nil, n, fmt.Errorf("the run checkout has uncommitted changes, so %s was not checked out", short7(sha))
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
			return nil, n, fmt.Errorf("recorded worktree path %q is not absolute", where)
		}
		if entries, err := os.ReadDir(where); err == nil && len(entries) > 0 {
			return nil, n, fmt.Errorf("%s already has content, so %s was not checked out there", where, short7(sha))
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
	if err := c.noteWIP(bctx, ctx, id, fmt.Sprintf("wip restored: %s at %s", ref, sha)); err != nil {
		fmt.Fprintf(ctx.Stderr, "cloud-boot: restore wip %s: note: %v\n", ref, err)
	}
	if branch == "" {
		branch = "(detached)"
	}
	return &wipRestored{Branch: branch, Where: place, Ref: ref, SHA: sha}, n, nil
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
			fmt.Fprintf(&b, "- %s: %s %s %s. Merge or rebase it, or raise a gate.\n", n.Label, n.Ref, n.SHA, n.Reason)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
