// This file is owned by the predicates track (agent-teams-5y8a.2). It
// implements the two default ownership predicates frozen by the contract
// (steward_seams.go's claimsLocallyFunc / isFallbackResponderFunc) that
// relay-gating (agent-teams-5y8a.5) wires onto relayKong as DI seams. Does
// not touch relay.go, steward_seams.go, steward_start.go, or notify.go.
package verbs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/gitutil"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// claimsInitiativeLocally is the default claimsLocallyFunc (steward_seams.go):
// reports whether THIS machine holds iss's registered worktree/branch/repo
// triple as a live local git checkout — the local, distributed,
// no-registry-machine-field exactly-once test relay-gating
// (agent-teams-5y8a.5) consults for a tied reply (thread resolves to an open
// initiative). Reads worktree/branch/repo through initiative.Of — the one
// shared first-wins reader, the same seam dispatch.go writes through at Run.
// This site used a last-wins scanner until agent-teams-ully: on 2026-07-28 a
// briefing line redefined the repo path, this predicate went false for a live
// initiative, and every human reply in its topic was silently dropped.
// Returns true iff ALL hold:
//
//  1. all three fields are present.
//  2. the worktree path exists locally.
//  3. it's a git checkout (git rev-parse --is-inside-work-tree == "true").
//  4. its checked-out branch equals the branch: field.
//  5. its repo matches the repo: field — checked against either the
//     worktree's canonical repo root (gitRepoMatches) or its origin remote
//     URL, since repo: is written as a local path by dispatch.go on the
//     machine that created the initiative, which may not be this machine.
//
// Any failure (missing field, absent path, git error) resolves to false —
// this predicate never errors, matching claimsLocallyFunc's bool-only
// signature.
func claimsInitiativeLocally(iss bd.Issue) bool {
	f := initiative.Of(iss)
	wt := f.Worktree
	branch := f.Branch
	repo := f.Repo
	if wt == "" || branch == "" || repo == "" {
		return false
	}
	if _, err := os.Stat(wt); err != nil {
		return false
	}
	if !gitIsWorkTree(wt) {
		return false
	}
	if gitCurrentBranch(wt) != branch {
		return false
	}
	return gitRepoMatches(wt, repo)
}

// gitIsWorkTree reports whether dir is inside a git working tree.
// Equivalent to: git -C <dir> rev-parse --is-inside-work-tree
func gitIsWorkTree(dir string) bool {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// gitCurrentBranch returns dir's checked-out branch name, or "" when dir
// isn't a git checkout or HEAD is detached.
// Equivalent to: git -C <dir> rev-parse --abbrev-ref HEAD
func gitCurrentBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitRepoMatches reports whether dir's repo matches repo, checked two ways:
// dir's canonical repo root — via gitutil.Runner.CommonDir, whose doc
// comment establishes that <git-common-dir>'s parent resolves to the main
// repo root for both a worktree and a plain checkout — compared against
// repo, or dir's origin remote URL compared against repo. Either match
// counts, since repo: (dispatch.go) is a local filesystem path on the
// machine that dispatched but may be a different clone location — or a bare
// origin URL for callers that populate it that way — on this machine. Both
// sides are symlink-normalised (canonicalPath) so /tmp-vs-/private/tmp style
// differences don't produce false negatives.
func gitRepoMatches(dir, repo string) bool {
	if commonDir, err := gitutil.New().CommonDir(dir); err == nil {
		root := filepath.Dir(commonDir)
		if canonicalPath(root) == canonicalPath(repo) {
			return true
		}
	}
	if out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output(); err == nil {
		if strings.TrimSpace(string(out)) == repo {
			return true
		}
	}
	return false
}

// isFallbackResponder is the default isFallbackResponderFunc
// (steward_seams.go): reports whether THIS machine is the designated
// fallback responder for untied/unrouted traffic — static-primary for v1,
// per StewardFallbackMarkerPath's doc comment. Requires BOTH: the fallback
// marker file is present (this machine was designated primary) AND a live
// steward marker (StewardSessionMarkerPath) is present — the same
// any-stat-error-means-absent guard notifyToSteward (steward_route.go) uses
// — so a machine flagged primary that has no running steward session never
// routes untied traffic into a dead mailbox.
func isFallbackResponder(ctx *cli.Context) bool {
	if _, err := os.Stat(StewardFallbackMarkerPath(ctx)); err != nil {
		return false
	}
	if _, err := os.Stat(StewardSessionMarkerPath(ctx)); err != nil {
		return false
	}
	return true
}
