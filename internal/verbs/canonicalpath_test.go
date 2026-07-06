// canonicalpath_test.go covers canonicalPath and its use in matchByWorktree /
// matchAllByWorktree (match.go). Lives in package verbs (not verbs_test, like
// match_test.go) because those symbols are unexported.
package verbs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

func TestCanonicalPath_ResolvesSymlink(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	wantReal, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("EvalSymlinks(real): %v", err)
	}
	if got := canonicalPath(link); got != wantReal {
		t.Errorf("canonicalPath(%q) = %q, want %q", link, got, wantReal)
	}
	if got := canonicalPath(real); got != wantReal {
		t.Errorf("canonicalPath(%q) = %q, want %q", real, got, wantReal)
	}
}

func TestCanonicalPath_NonexistentFallsBackToClean(t *testing.T) {
	p := "/no/such/path/../path2/"
	want := filepath.Clean(p)
	if got := canonicalPath(p); got != want {
		t.Errorf("canonicalPath(%q) = %q, want %q", p, got, want)
	}
}

func TestMatchByWorktree_SymlinkedPath(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Initiative registered with the resolved path, looked up via the symlink.
	issues := []bd.Issue{{ID: "at-1", Description: "worktree: " + real + "\n"}}
	if got := matchByWorktree(issues, link); got == nil || got.ID != "at-1" {
		t.Errorf("matchByWorktree(real-registered, symlink-lookup) = %v, want at-1", got)
	}

	// Initiative registered with the symlink, looked up via the resolved path.
	issuesLink := []bd.Issue{{ID: "at-2", Description: "worktree: " + link + "\n"}}
	if got := matchByWorktree(issuesLink, real); got == nil || got.ID != "at-2" {
		t.Errorf("matchByWorktree(symlink-registered, real-lookup) = %v, want at-2", got)
	}
}

func TestMatchAllByWorktree_SymlinkedPath(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	issues := []bd.Issue{{ID: "at-1", Description: "worktree: " + real + "\n"}}
	got := matchAllByWorktree(issues, link)
	if len(got) != 1 || got[0].ID != "at-1" {
		t.Errorf("matchAllByWorktree(real-registered, symlink-lookup) = %v, want [at-1]", got)
	}
}
