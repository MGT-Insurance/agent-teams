// This file is owned by the predicates track (agent-teams-5y8a.2).
package verbs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── claimsInitiativeLocally ───────────────────────────────────────────────────

// runGit runs a git command with dir as its working directory, failing the
// test on error. Mirrors gitutil_test.go's initTempRepo helper.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v (dir=%s): %v\n%s", args, dir, err, out)
	}
}

// initRepoWithWorktree creates a temp git repo with an initial commit, then
// adds a worktree checked out on a new branch cut from that commit. Returns
// the repo root's canonical (symlink-resolved) path — what claimsLocally's
// repo: field comparison is checked against — and the worktree path.
func initRepoWithWorktree(t *testing.T, branch string) (repoRoot, wtPath string) {
	t.Helper()
	repoRoot = t.TempDir()
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "Test")
	runGit(t, repoRoot, "commit", "--allow-empty", "-m", "init")

	wtPath = filepath.Join(t.TempDir(), "wt")
	runGit(t, repoRoot, "worktree", "add", wtPath, "-b", branch)
	return canonicalPath(repoRoot), wtPath
}

// descBody formats a minimal initiative description with repo:/worktree:/
// branch: lines, mirroring dispatch.go's body layout (dispatch.go:163-166).
func descBody(repo, wt, branch string) string {
	return "problem: test\nrepo: " + repo + "\nworktree: " + wt + "\nbranch: " + branch + "\n"
}

func TestClaimsInitiativeLocally_Matches(t *testing.T) {
	repoRoot, wtPath := initRepoWithWorktree(t, "feat-x")
	iss := bd.Issue{ID: "at-1", Description: descBody(repoRoot, wtPath, "feat-x")}
	if !claimsInitiativeLocally(iss) {
		t.Error("expected true for a matching worktree/branch/repo triple")
	}
}

func TestClaimsInitiativeLocally_PathAbsent(t *testing.T) {
	repoRoot, wtPath := initRepoWithWorktree(t, "feat-x")
	missing := filepath.Join(filepath.Dir(wtPath), "does-not-exist")
	iss := bd.Issue{ID: "at-1", Description: descBody(repoRoot, missing, "feat-x")}
	if claimsInitiativeLocally(iss) {
		t.Error("expected false when the worktree path doesn't exist locally")
	}
}

func TestClaimsInitiativeLocally_NotGitCheckout(t *testing.T) {
	repoRoot, _ := initRepoWithWorktree(t, "feat-x")
	plainDir := t.TempDir()
	iss := bd.Issue{ID: "at-1", Description: descBody(repoRoot, plainDir, "feat-x")}
	if claimsInitiativeLocally(iss) {
		t.Error("expected false when the worktree path isn't a git checkout")
	}
}

func TestClaimsInitiativeLocally_WrongBranch(t *testing.T) {
	repoRoot, wtPath := initRepoWithWorktree(t, "feat-x")
	iss := bd.Issue{ID: "at-1", Description: descBody(repoRoot, wtPath, "some-other-branch")}
	if claimsInitiativeLocally(iss) {
		t.Error("expected false when branch: doesn't match the checked-out branch")
	}
}

func TestClaimsInitiativeLocally_WrongRepo(t *testing.T) {
	_, wtPath := initRepoWithWorktree(t, "feat-x")
	otherRepo := t.TempDir()
	iss := bd.Issue{ID: "at-1", Description: descBody(otherRepo, wtPath, "feat-x")}
	if claimsInitiativeLocally(iss) {
		t.Error("expected false when repo: doesn't match the worktree's actual repo")
	}
}

func TestClaimsInitiativeLocally_MissingFields(t *testing.T) {
	iss := bd.Issue{ID: "at-1", Description: "problem: test\n"}
	if claimsInitiativeLocally(iss) {
		t.Error("expected false when worktree/branch/repo fields are all missing")
	}
}

// ── isFallbackResponder ───────────────────────────────────────────────────────

func touchStewardFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestIsFallbackResponder_MarkerAndStewardPresent(t *testing.T) {
	ctx := &cli.Context{Home: t.TempDir()}
	touchStewardFile(t, StewardFallbackMarkerPath(ctx))
	touchStewardFile(t, StewardSessionMarkerPath(ctx))
	if !isFallbackResponder(ctx) {
		t.Error("expected true when both the fallback and live steward markers are present")
	}
}

func TestIsFallbackResponder_NoFallbackMarker(t *testing.T) {
	ctx := &cli.Context{Home: t.TempDir()}
	touchStewardFile(t, StewardSessionMarkerPath(ctx))
	if isFallbackResponder(ctx) {
		t.Error("expected false when the fallback marker is absent")
	}
}

func TestIsFallbackResponder_NoStewardMarker(t *testing.T) {
	ctx := &cli.Context{Home: t.TempDir()}
	touchStewardFile(t, StewardFallbackMarkerPath(ctx))
	if isFallbackResponder(ctx) {
		t.Error("expected false when the fallback marker is present but there's no live steward session")
	}
}

func TestIsFallbackResponder_NeitherPresent(t *testing.T) {
	ctx := &cli.Context{Home: t.TempDir()}
	if isFallbackResponder(ctx) {
		t.Error("expected false when neither marker is present")
	}
}
