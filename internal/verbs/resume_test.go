package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
)

// The worktreePath unit tests that lived here are gone with the helper —
// worktree resolution is initiative.Of's, and internal/initiative owns its
// tests. What is still verbs' own is that resume READS the field; that is
// TestResume_NoWorktreeLine below.

// ---- resumeKong: nil context -----------------------------------------------

func TestResume_NilContext(t *testing.T) {
	err := (&resumeKong{ID: "at-abc"}).Run(nil)
	if err == nil {
		t.Fatal("expected error for nil context, got nil")
	}
}

// ---- resumeKong: missing arg -----------------------------------------------

func TestResume_MissingArg(t *testing.T) {
	err := (&resumeKong{}).Validate()
	if err == nil {
		t.Fatal("expected UsageError for missing arg, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestResume_EmptyArg(t *testing.T) {
	err := (&resumeKong{ID: ""}).Validate()
	if err == nil {
		t.Fatal("expected UsageError for empty arg, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// ---- resumeKong: unknown id ------------------------------------------------

func TestResume_UnknownID(t *testing.T) {
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			return "", fmt.Errorf("bd show: not found")
		},
	}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())

	err := (&resumeKong{ID: "at-nosuchid"}).Run(ctx)
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

// ---- resumeKong: closed initiative -----------------------------------------

func TestResume_ClosedInitiative(t *testing.T) {
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{
				ID:          "at-closed1",
				Status:      "closed",
				Description: "worktree: /some/path\n",
			}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())

	err := (&resumeKong{ID: "at-closed1"}).Run(ctx)
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

// ---- resumeKong: missing worktree line -------------------------------------

func TestResume_NoWorktreeLine(t *testing.T) {
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{
				ID:          "at-nowt1",
				Status:      "open",
				Description: "problem: no worktree here\n",
			}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())

	err := (&resumeKong{ID: "at-nowt1"}).Run(ctx)
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

// ---- resumeKong: worktree path does not exist ------------------------------

func TestResume_MissingWorktreePath(t *testing.T) {
	missingPath := "/no/such/worktree/path/ever"
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{
				ID:          "at-nowt2",
				Status:      "open",
				Description: "worktree: " + missingPath + "\n",
			}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())

	err := (&resumeKong{ID: "at-nowt2"}).Run(ctx)
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

// ---- resumeKong: claude not in PATH ----------------------------------------

func TestResume_MissingClaude(t *testing.T) {
	if _, err := exec.LookPath("claude"); err == nil {
		t.Skip("claude is in PATH; skipping missing-claude test")
	}
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{
				ID:          "at-noclaude",
				Status:      "open",
				Description: "worktree: " + dir + "\n",
			}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	cmd := &resumeKong{ID: "at-noclaude", launch: launchBGSession}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected DepError, got nil")
	}
	if code := cli.ExitCode(err); code != 3 {
		t.Errorf("expected exit 3 (DepError), got %d", code)
	}
}

// ---- resumeKong: happy path (stubbed launch) --------------------------------

func TestResume_HappyPath(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{
				ID:          "at-happy1",
				Status:      "open",
				Description: "worktree: " + dir + "\n",
			}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}

	var launchedDir, launchedArg, launchedRole, launchedInitiative string
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &resumeKong{
		ID: "at-happy1",
		launch: func(_ *cli.Context, d, arg, role, initiativeID string) error {
			launchedDir = d
			launchedArg = arg
			launchedRole = role
			launchedInitiative = initiativeID
			return nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if launchedDir != dir {
		t.Errorf("launch dir = %q, want %q", launchedDir, dir)
	}
	if launchedArg != "at-happy1" {
		t.Errorf("launch driArg = %q, want %q", launchedArg, "at-happy1")
	}
	if launchedRole != "dri" {
		t.Errorf("launch role = %q, want %q", launchedRole, "dri")
	}
	if launchedInitiative != "at-happy1" {
		t.Errorf("launch initiativeID = %q, want %q", launchedInitiative, "at-happy1")
	}

	out := stdout.String()
	basename := filepath.Base(dir)
	checks := []string{
		"initiative_id: at-happy1",
		"worktree: " + dir,
		"Background session launched: " + basename,
		"claude attach " + basename,
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q\ngot:\n%s", want, out)
		}
	}
}

func TestResume_CodexUsesLastSessionAndRuntimeControls(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{runFn: func(args ...string) (string, error) {
		issues := []bd.Issue{{
			ID:     "at-codex-resume",
			Status: "open",
			Description: "worktree: " + dir + "\n" +
				"runtime: codex\n" +
				"session: old-thread\n" +
				"session: active-thread\n",
		}}
		raw, _ := json.Marshal(issues)
		return string(raw), nil
	}}
	var started runtimeStartRequest
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &resumeKong{
		ID: "at-codex-resume",
		launch: func(*cli.Context, string, string, string, string) error {
			t.Fatal("Claude launcher called")
			return nil
		},
		runtimeStart: func(_ *cli.Context, req runtimeStartRequest) error {
			started = req
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if started.Runtime != sessionruntime.Codex || started.ResumeID != "active-thread" || started.Prompt != codexDRIPrompt("at-codex-resume") {
		t.Fatalf("runtime start = %+v", started)
	}
	if !strings.Contains(stdout.String(), "ateam runtime open codex") || strings.Contains(stdout.String(), "claude attach") {
		t.Fatalf("monitoring output is not Codex-specific:\n%s", stdout.String())
	}
}

func TestResume_RuntimeFailures(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name, description, assertion, want string
	}{
		{name: "unknown stored runtime", description: "runtime: other\n", want: "unknown runtime"},
		{name: "assertion mismatch", description: "runtime: codex\nsession: thread-1\n", assertion: "claude", want: "does not match"},
		{name: "codex missing session", description: "runtime: codex\n", want: "no session"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fbd := &fakeBD{runFn: func(args ...string) (string, error) {
				raw, _ := json.Marshal([]bd.Issue{{ID: "at-r", Status: "open", Description: "worktree: " + dir + "\n" + tt.description}})
				return string(raw), nil
			}}
			ctx, _, stderr := makeCtx(fbd, t.TempDir())
			err := (&resumeKong{ID: "at-r", Runtime: tt.assertion}).Run(ctx)
			combined := stderr.String()
			if err != nil {
				combined += err.Error()
			}
			if err == nil || !strings.Contains(combined, tt.want) {
				t.Fatalf("err=%v stderr=%q want %q", err, stderr.String(), tt.want)
			}
		})
	}
}

// ---- resumeKong: missing worktree recreation (agent-teams-8st0.28) --------

// fakeResumeGit is a resumeWorktreeGit fake recording every call, keyed off
// a plain set of refs that "exist" — the same shape the real
// gitutil.Runner.BranchExists reports (a fully-qualified ref such as
// "refs/heads/foo" or "refs/remotes/origin/foo").
type fakeResumeGit struct {
	refs map[string]bool

	attachCalls []attachCall
	addCalls    []addCall
	removeCalls []removeCall
	attachErr   error
	addErr      error
	removeErr   error
}

type attachCall struct{ repoRoot, wtPath, branch string }
type addCall struct{ repoRoot, wtPath, branch, base string }
type removeCall struct{ repoRoot, wtPath string }

func (g *fakeResumeGit) BranchExists(_, ref string) bool { return g.refs[ref] }

func (g *fakeResumeGit) AttachWorktree(repoRoot, wtPath, branch string) error {
	g.attachCalls = append(g.attachCalls, attachCall{repoRoot, wtPath, branch})
	if g.attachErr != nil {
		return g.attachErr
	}
	return os.MkdirAll(wtPath, 0o755)
}

func (g *fakeResumeGit) AddWorktree(repoRoot, wtPath, branch, base string) error {
	g.addCalls = append(g.addCalls, addCall{repoRoot, wtPath, branch, base})
	if g.addErr != nil {
		return g.addErr
	}
	return os.MkdirAll(wtPath, 0o755)
}

// RemoveWorktree backs recreateReviewWorktree's cleanup of a stray detached
// worktree when the gh pr checkout fallback fails (agent-teams-8st0.29 fix
// 2). It removes the directory addDetached/AddWorktree created, mirroring
// what `git worktree remove --force` does to the real checkout.
func (g *fakeResumeGit) RemoveWorktree(repoRoot, wtPath string) error {
	g.removeCalls = append(g.removeCalls, removeCall{repoRoot, wtPath})
	if g.removeErr != nil {
		return g.removeErr
	}
	return os.RemoveAll(wtPath)
}

// noopSetup stubs worktreeSetupFunc for tests that only care about the
// worktree-recreation decision, not the (separately tested) setup hook.
func noopSetup(*cli.Context, string) (worktreeSetupResult, error) {
	return worktreeSetupResult{}, nil
}

// reviewIssue returns a bd.Issue shaped like a review-shaped initiative
// whose worktree does NOT exist on disk yet (missingWorktreePath is never
// created by the test) — repo is a real temp git repo so BranchExists/
// AttachWorktree fakes can be swapped for the real gitutil.Runner where a
// test wants that instead.
func resumeReviewIssue(id, repo, branch, missingWorktreePath string, prNumber int, ownerRepo string) bd.Issue {
	return bd.Issue{
		ID:     id,
		Status: "open",
		Description: "repo: " + repo + "\n" +
			"worktree: " + missingWorktreePath + "\n" +
			"branch: " + branch + "\n" +
			"pr-number: " + strconv.Itoa(prNumber) + "\n" +
			"pr-repo: " + ownerRepo + "\n" +
			"pr-url: https://github.com/" + ownerRepo + "/pull/" + strconv.Itoa(prNumber) + "\n",
	}
}

func resumeDriIssue(id, repo, branch, missingWorktreePath string) bd.Issue {
	return bd.Issue{
		ID:     id,
		Status: "open",
		Description: "repo: " + repo + "\n" +
			"worktree: " + missingWorktreePath + "\n" +
			"branch: " + branch + "\n",
	}
}

// TestResume_MissingWorktree_Review_LocalBranchExists_Attaches covers the
// acceptance criterion "Worktree gone, local branch present -> recreated at
// the same path, launch called with the review-pr prompt": the PR-state
// probe reports OPEN, a local branch already exists, so resume attaches it
// (no fetch, no gh checkout) and launches the review-pr skill at sonnet.
func TestResume_MissingWorktree_Review_LocalBranchExists_Attaches(t *testing.T) {
	repoDir := newEnabledRepoDir(t)
	missing := filepath.Join(t.TempDir(), "gone")
	fbd := &fakeBD{runFn: func(args ...string) (string, error) {
		raw, _ := json.Marshal([]bd.Issue{resumeReviewIssue("at-rev1", repoDir, "review-branch", missing, 42, "mgt-insurance/midgard")})
		return string(raw), nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	git := &fakeResumeGit{refs: map[string]bool{"refs/heads/review-branch": true}}
	var pruned bool
	var setupCalled bool
	var gotDir, gotPrompt, gotModel string
	cmd := &resumeKong{
		ID:  "at-rev1",
		git: git,
		gitPrune: func(repoRoot string) error {
			pruned = true
			if repoRoot != repoDir {
				t.Errorf("gitPrune repo = %q, want %q", repoRoot, repoDir)
			}
			return nil
		},
		fetchPRHead: func(string, string, int) error {
			t.Fatal("fetchPRHead called; local branch exists, should attach directly")
			return nil
		},
		addDetached:  func(string, string) error { t.Fatal("addDetached called; local branch exists"); return nil },
		ghPRCheckout: func(string, string, int) error { t.Fatal("ghPRCheckout called; local branch exists"); return nil },
		setup: func(_ *cli.Context, dir string) (worktreeSetupResult, error) {
			setupCalled = true
			return worktreeSetupResult{}, nil
		},
		prState: func(ownerRepo string, prNumber int) (string, error) {
			if ownerRepo != "mgt-insurance/midgard" || prNumber != 42 {
				t.Errorf("prState(%q, %d), want mgt-insurance/midgard, 42", ownerRepo, prNumber)
			}
			return ghPRStateOpen, nil
		},
		launchRaw: func(_ *cli.Context, d, p, m, _, _, _ string) error {
			gotDir, gotPrompt, gotModel = d, p, m
			return nil
		},
		launch: func(_ *cli.Context, _, _, _, _ string) error {
			t.Fatal("launch called; review-shaped resume must use launchRaw with the review-pr prompt")
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !pruned {
		t.Error("expected git worktree prune before recreation")
	}
	if len(git.attachCalls) != 1 || git.attachCalls[0] != (attachCall{repoDir, missing, "review-branch"}) {
		t.Errorf("attachCalls = %+v, want one attach(%s, %s, review-branch)", git.attachCalls, repoDir, missing)
	}
	if len(git.addCalls) != 0 {
		t.Errorf("addCalls = %+v, want none (local branch existed)", git.addCalls)
	}
	if !setupCalled {
		t.Error("expected runWorktreeSetup to run after recreating the worktree")
	}
	if gotDir != missing {
		t.Errorf("launchRaw dir = %q, want %q", gotDir, missing)
	}
	if gotPrompt != "/agent-teams:review-pr at-rev1" {
		t.Errorf("launchRaw prompt = %q, want the review-pr skill prompt", gotPrompt)
	}
	if gotModel != "sonnet" {
		t.Errorf("launchRaw model = %q, want sonnet", gotModel)
	}
}

// TestResume_MissingWorktree_Review_FetchesPRHead covers "Worktree gone,
// branch gone, review PR OPEN -> branch created from pull/<n>/head (its tip
// equals the stubbed PR head SHA), launched": no local branch, fetch
// succeeds, resume attaches the freshly-fetched branch and never falls back
// to gh pr checkout.
func TestResume_MissingWorktree_Review_FetchesPRHead(t *testing.T) {
	repoDir := newEnabledRepoDir(t)
	missing := filepath.Join(t.TempDir(), "gone")
	fbd := &fakeBD{runFn: func(args ...string) (string, error) {
		raw, _ := json.Marshal([]bd.Issue{resumeReviewIssue("at-rev2", repoDir, "pr-branch", missing, 7, "mgt-insurance/midgard")})
		return string(raw), nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	git := &fakeResumeGit{refs: map[string]bool{}} // no local branch, no origin branch
	var fetchedBranch string
	var fetchedPR int
	cmd := &resumeKong{
		ID:       "at-rev2",
		git:      git,
		gitPrune: func(string) error { return nil },
		setup:    noopSetup,
		prState:  func(string, int) (string, error) { return ghPRStateOpen, nil },
		fetchPRHead: func(repoRoot, branch string, prNumber int) error {
			fetchedBranch, fetchedPR = branch, prNumber
			return nil
		},
		addDetached: func(string, string) error { t.Fatal("addDetached called; fetch succeeded"); return nil },
		ghPRCheckout: func(string, string, int) error {
			t.Fatal("ghPRCheckout called; fetch succeeded, no fallback needed")
			return nil
		},
		launchRaw: func(_ *cli.Context, _, _, _, _, _, _ string) error { return nil },
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fetchedBranch != "pr-branch" || fetchedPR != 7 {
		t.Errorf("fetchPRHead(branch=%q, pr=%d), want (pr-branch, 7)", fetchedBranch, fetchedPR)
	}
	if len(git.attachCalls) != 1 || git.attachCalls[0].branch != "pr-branch" {
		t.Errorf("attachCalls = %+v, want one attach of pr-branch after fetch", git.attachCalls)
	}
}

// TestResume_MissingWorktree_Review_FetchFails_FallsBackToGHCheckout covers
// the fork-PR fallback: fetch fails, so resume adds a detached worktree and
// runs `gh pr checkout --branch`.
func TestResume_MissingWorktree_Review_FetchFails_FallsBackToGHCheckout(t *testing.T) {
	repoDir := newEnabledRepoDir(t)
	missing := filepath.Join(t.TempDir(), "gone")
	fbd := &fakeBD{runFn: func(args ...string) (string, error) {
		raw, _ := json.Marshal([]bd.Issue{resumeReviewIssue("at-rev3", repoDir, "fork-branch", missing, 9, "mgt-insurance/midgard")})
		return string(raw), nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	git := &fakeResumeGit{refs: map[string]bool{}}
	var detachedAt string
	var checkedOutBranch string
	var checkedOutPR int
	cmd := &resumeKong{
		ID:          "at-rev3",
		git:         git,
		gitPrune:    func(string) error { return nil },
		setup:       noopSetup,
		prState:     func(string, int) (string, error) { return ghPRStateOpen, nil },
		fetchPRHead: func(string, string, int) error { return fmt.Errorf("fetch: couldn't find remote ref pull/9/head") },
		addDetached: func(_, wtPath string) error { detachedAt = wtPath; return nil },
		ghPRCheckout: func(wtPath, branch string, prNumber int) error {
			checkedOutBranch, checkedOutPR = branch, prNumber
			return nil
		},
		launchRaw: func(_ *cli.Context, _, _, _, _, _, _ string) error { return nil },
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if detachedAt != missing {
		t.Errorf("addDetached path = %q, want %q", detachedAt, missing)
	}
	if checkedOutBranch != "fork-branch" || checkedOutPR != 9 {
		t.Errorf("ghPRCheckout(branch=%q, pr=%d), want (fork-branch, 9)", checkedOutBranch, checkedOutPR)
	}
	if len(git.attachCalls) != 0 {
		t.Errorf("attachCalls = %+v, want none (gh pr checkout handles the branch itself)", git.attachCalls)
	}
}

// TestResume_MissingWorktree_Review_BothStrategiesFail_ErrorsNoLaunch covers
// "Both fail -> error, no launch": fetch and the gh checkout fallback both
// fail, so resume reports a loud error and never calls launch.
func TestResume_MissingWorktree_Review_BothStrategiesFail_ErrorsNoLaunch(t *testing.T) {
	repoDir := newEnabledRepoDir(t)
	missing := filepath.Join(t.TempDir(), "gone")
	fbd := &fakeBD{runFn: func(args ...string) (string, error) {
		raw, _ := json.Marshal([]bd.Issue{resumeReviewIssue("at-rev4", repoDir, "dead-branch", missing, 11, "mgt-insurance/midgard")})
		return string(raw), nil
	}}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())

	git := &fakeResumeGit{refs: map[string]bool{}}
	cmd := &resumeKong{
		ID:           "at-rev4",
		git:          git,
		gitPrune:     func(string) error { return nil },
		setup:        noopSetup,
		prState:      func(string, int) (string, error) { return ghPRStateOpen, nil },
		fetchPRHead:  func(string, string, int) error { return fmt.Errorf("fetch: no such ref") },
		addDetached:  func(string, string) error { return nil },
		ghPRCheckout: func(string, string, int) error { return fmt.Errorf("gh: pull request not found") },
		launchRaw: func(_ *cli.Context, _, _, _, _, _, _ string) error {
			t.Fatal("launchRaw called; both recreation strategies failed")
			return nil
		},
	}
	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "at-rev4") {
		t.Errorf("expected initiative id in stderr, got: %s", stderr.String())
	}
}

// TestResume_MissingWorktree_Review_BothStrategiesFail_RemovesStrayDetachedWorktree
// covers agent-teams-8st0.29 fix 2: fetch fails, addDetached stages a stray
// branchless worktree at wtPath, and the gh pr checkout fallback also
// fails — resume must remove that stray worktree (git worktree remove
// --force) and prune git's bookkeeping before returning the error, so the
// next resume finds wtPath missing and retries recreation from scratch
// instead of finding an existing (wrong-checkout) dir and skipping it.
func TestResume_MissingWorktree_Review_BothStrategiesFail_RemovesStrayDetachedWorktree(t *testing.T) {
	repoDir := newEnabledRepoDir(t)
	missing := filepath.Join(t.TempDir(), "gone")
	fbd := &fakeBD{runFn: func(args ...string) (string, error) {
		raw, _ := json.Marshal([]bd.Issue{resumeReviewIssue("at-rev4b", repoDir, "dead-branch", missing, 12, "mgt-insurance/midgard")})
		return string(raw), nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	pruneCalls := 0
	git := &fakeResumeGit{refs: map[string]bool{}}
	cmd := &resumeKong{
		ID:           "at-rev4b",
		git:          git,
		gitPrune:     func(string) error { pruneCalls++; return nil },
		setup:        noopSetup,
		prState:      func(string, int) (string, error) { return ghPRStateOpen, nil },
		fetchPRHead:  func(string, string, int) error { return fmt.Errorf("fetch: no such ref") },
		addDetached:  func(_, wtPath string) error { return os.MkdirAll(wtPath, 0o755) },
		ghPRCheckout: func(string, string, int) error { return fmt.Errorf("gh: pull request not found") },
		launchRaw: func(_ *cli.Context, _, _, _, _, _, _ string) error {
			t.Fatal("launchRaw called; both recreation strategies failed")
			return nil
		},
	}
	if err := cmd.Run(ctx); err == nil {
		t.Fatal("expected an error, got nil")
	}

	if len(git.removeCalls) != 1 {
		t.Fatalf("RemoveWorktree calls = %d, want 1: %+v", len(git.removeCalls), git.removeCalls)
	}
	if got := git.removeCalls[0]; got.repoRoot != repoDir || got.wtPath != missing {
		t.Errorf("RemoveWorktree call = %+v, want repoRoot=%q wtPath=%q", got, repoDir, missing)
	}
	if pruneCalls < 2 {
		t.Errorf("gitPrune calls = %d, want at least 2 (the up-front best-effort prune plus the post-cleanup prune)", pruneCalls)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("expected the stray detached worktree dir %s to be removed, stat err = %v", missing, err)
	}
}

// TestResume_MissingWorktree_Review_Merged_ReturnsErrNothingToReview covers
// "Review PR MERGED -> errNothingToReview, nothing created, no launch":
// the probe fires before any git call, so nothing about the worktree is
// touched at all.
func TestResume_MissingWorktree_Review_Merged_ReturnsErrNothingToReview(t *testing.T) {
	repoDir := newEnabledRepoDir(t)
	missing := filepath.Join(t.TempDir(), "gone")
	fbd := &fakeBD{runFn: func(args ...string) (string, error) {
		raw, _ := json.Marshal([]bd.Issue{resumeReviewIssue("at-rev5", repoDir, "merged-branch", missing, 5, "mgt-insurance/midgard")})
		return string(raw), nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	git := &fakeResumeGit{refs: map[string]bool{"refs/heads/merged-branch": true}}
	cmd := &resumeKong{
		ID:  "at-rev5",
		git: git,
		gitPrune: func(string) error {
			t.Fatal("gitPrune called; PR is already MERGED, nothing should be created")
			return nil
		},
		setup:   noopSetup,
		prState: func(string, int) (string, error) { return ghPRStateMerged, nil },
		launchRaw: func(_ *cli.Context, _, _, _, _, _, _ string) error {
			t.Fatal("launchRaw called; PR is MERGED, resume must not launch")
			return nil
		},
		launch: func(_ *cli.Context, _, _, _, _ string) error {
			t.Fatal("launch called; PR is MERGED, resume must not launch")
			return nil
		},
	}
	err := cmd.Run(ctx)
	if !errors.Is(err, errNothingToReview) {
		t.Fatalf("Run() error = %v, want errNothingToReview", err)
	}
	if len(git.attachCalls) != 0 || len(git.addCalls) != 0 {
		t.Errorf("expected no git worktree calls, got attach=%+v add=%+v", git.attachCalls, git.addCalls)
	}
}

// TestResume_MissingWorktree_DRI_TracksOriginBranch covers "DRI worktree
// gone, only origin/<branch> exists -> tracked, launched": no local branch,
// but origin/<branch> exists, so resume tracks it via AddWorktree(base =
// "origin/<branch>") and launches the normal DRI default, not the review-pr
// prompt.
func TestResume_MissingWorktree_DRI_TracksOriginBranch(t *testing.T) {
	repoDir := newEnabledRepoDir(t)
	missing := filepath.Join(t.TempDir(), "gone")
	fbd := &fakeBD{runFn: func(args ...string) (string, error) {
		raw, _ := json.Marshal([]bd.Issue{resumeDriIssue("at-dri1", repoDir, "dri-branch", missing)})
		return string(raw), nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	git := &fakeResumeGit{refs: map[string]bool{"refs/remotes/origin/dri-branch": true}}
	var launchedArg string
	cmd := &resumeKong{
		ID:       "at-dri1",
		git:      git,
		gitPrune: func(string) error { return nil },
		setup:    noopSetup,
		launch: func(_ *cli.Context, _, arg, _, _ string) error {
			launchedArg = arg
			return nil
		},
		launchRaw: func(_ *cli.Context, _, _, _, _, _, _ string) error {
			t.Fatal("launchRaw called; non-review resume should use the default DRI launch")
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(git.addCalls) != 1 || git.addCalls[0] != (addCall{repoDir, missing, "dri-branch", "origin/dri-branch"}) {
		t.Errorf("addCalls = %+v, want one AddWorktree(%s, %s, dri-branch, origin/dri-branch)", git.addCalls, repoDir, missing)
	}
	if launchedArg != "at-dri1" {
		t.Errorf("launch driArg = %q, want at-dri1", launchedArg)
	}
}

// TestResume_MissingWorktree_NonReview_NoLocalNoRemote_Errors covers
// "Non-review, branch gone everywhere -> error, no launch": the initiative
// isn't review-shaped, so no PR probe runs, but with no local or remote
// branch left resume must refuse rather than fabricate a fresh branch off
// the default branch.
func TestResume_MissingWorktree_NonReview_NoLocalNoRemote_Errors(t *testing.T) {
	repoDir := newEnabledRepoDir(t)
	missing := filepath.Join(t.TempDir(), "gone")
	fbd := &fakeBD{runFn: func(args ...string) (string, error) {
		raw, _ := json.Marshal([]bd.Issue{resumeDriIssue("at-dri2", repoDir, "gone-branch", missing)})
		return string(raw), nil
	}}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())

	git := &fakeResumeGit{refs: map[string]bool{}}
	cmd := &resumeKong{
		ID:       "at-dri2",
		git:      git,
		gitPrune: func(string) error { return nil },
		setup:    noopSetup,
		launch: func(_ *cli.Context, _, _, _, _ string) error {
			t.Fatal("launch called; branch is gone everywhere")
			return nil
		},
	}
	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "gone-branch") {
		t.Errorf("expected branch name in stderr, got: %s", stderr.String())
	}
}

func TestResumeCodexCompatibilityFailurePreventsLaunch(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{runFn: func(...string) (string, error) {
		raw, _ := json.Marshal([]bd.Issue{{ID: "at-r", Status: "open", Description: "worktree: " + dir + "\nruntime: codex\nsession: thread-1\n"}})
		return string(raw), nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	cmd := &resumeKong{
		ID: "at-r",
		codexCheck: func(context.Context, string) error {
			return fmt.Errorf("official standalone installer required")
		},
		runtimeStart: func(*cli.Context, runtimeStartRequest) error {
			t.Fatal("runtime launched")
			return nil
		},
	}
	err := cmd.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "official standalone") {
		t.Fatalf("error = %v", err)
	}
}

// ---- resumeKong: --launch-prompt -------------------------------------------

func TestResume_CustomLaunchPromptUsesRawLaunch(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: "at-rr1", Status: "open", Description: "worktree: " + dir + "\n"}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	var gotDir, gotPrompt, gotModel, gotRole, gotInitiative string
	cmd := &resumeKong{
		ID:           "at-rr1",
		LaunchPrompt: "/agent-teams:review-pr at-rr1",
		Model:        "sonnet",
		launch: func(_ *cli.Context, _, _, _, _ string) error {
			t.Fatal("launch called; want launchRaw for --launch-prompt")
			return nil
		},
		launchRaw: func(_ *cli.Context, d, p, m, _, role, initiativeID string) error {
			gotDir, gotPrompt, gotModel, gotRole, gotInitiative = d, p, m, role, initiativeID
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotDir != dir {
		t.Errorf("launchRaw dir = %q, want %q", gotDir, dir)
	}
	if gotPrompt != "/agent-teams:review-pr at-rr1" {
		t.Errorf("launchRaw prompt = %q", gotPrompt)
	}
	if gotModel != "sonnet" {
		t.Errorf("launchRaw model = %q, want sonnet", gotModel)
	}
	if gotRole != "dri" {
		t.Errorf("launchRaw role = %q, want %q", gotRole, "dri")
	}
	if gotInitiative != "at-rr1" {
		t.Errorf("launchRaw initiativeID = %q, want %q", gotInitiative, "at-rr1")
	}
}

func TestResume_NoLaunchPromptUsesDriLaunch(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: "at-rr2", Status: "open", Description: "worktree: " + dir + "\n"}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	var gotArg, gotRole, gotInitiative string
	cmd := &resumeKong{
		ID: "at-rr2",
		launch: func(_ *cli.Context, _, arg, role, initiativeID string) error {
			gotArg, gotRole, gotInitiative = arg, role, initiativeID
			return nil
		},
		launchRaw: func(_ *cli.Context, _, _, _, _, _, _ string) error {
			t.Fatal("launchRaw called; want launch for default path")
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotArg != "at-rr2" {
		t.Errorf("launch driArg = %q, want at-rr2", gotArg)
	}
	if gotRole != "dri" {
		t.Errorf("launch role = %q, want %q", gotRole, "dri")
	}
	if gotInitiative != "at-rr2" {
		t.Errorf("launch initiativeID = %q, want %q", gotInitiative, "at-rr2")
	}
}

func TestResume_ModelWithoutLaunchPromptRejected(t *testing.T) {
	err := (&resumeKong{ID: "at-x", Model: "sonnet"}).Validate()
	if err == nil {
		t.Fatal("expected UsageError for --model without --launch-prompt, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// ---- resumeKong: duplicate-live-session guard (agent-teams-ndr4.1) --------

// livePID is a placeholder PID for a fake live agentSession in the tests
// below; only presence (non-nil), never the value, is meaningful.
var livePID = 4242

func TestResume_NoLiveSession_Launches(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: "at-nolive", Status: "open", Description: "worktree: " + dir + "\n"}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	var launched bool
	cmd := &resumeKong{
		ID:         "at-nolive",
		agentsFunc: func() ([]agentSession, error) { return nil, nil },
		launch: func(_ *cli.Context, _, _, _, _ string) error {
			launched = true
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !launched {
		t.Fatal("expected launch to be called when no live session exists")
	}
}

func TestResume_LiveSessionNoSupersede_RefusesAndNamesID(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: "at-live1", Status: "open", Description: "worktree: " + dir + "\n"}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())

	cmd := &resumeKong{
		ID: "at-live1",
		agentsFunc: func() ([]agentSession, error) {
			return []agentSession{{Name: filepath.Base(dir), ID: "sess-abc", PID: &livePID}}, nil
		},
		launch: func(_ *cli.Context, _, _, _, _ string) error {
			t.Fatal("launch called; want refusal when a live session exists")
			return nil
		},
	}
	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error refusing to resume, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "sess-abc") {
		t.Errorf("expected live session id %q in stderr, got: %s", "sess-abc", stderr.String())
	}
	if !strings.Contains(stderr.String(), "--supersede") {
		t.Errorf("expected --supersede mentioned in stderr, got: %s", stderr.String())
	}
}

func TestResume_LiveSessionWithSupersede_StopsThenLaunches(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: "at-live2", Status: "open", Description: "worktree: " + dir + "\n"}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	var stoppedID string
	var stopCalledBeforeLaunch, launched bool
	var agentsCalls int
	cmd := &resumeKong{
		ID:        "at-live2",
		Supersede: true,
		agentsFunc: func() ([]agentSession, error) {
			agentsCalls++
			if agentsCalls == 1 {
				// Initial query: the old session is still live.
				return []agentSession{{Name: filepath.Base(dir), ID: "sess-xyz", PID: &livePID}}, nil
			}
			// Re-query after the stop: it's gone.
			return nil, nil
		},
		stopSession: func(id string) error {
			stoppedID = id
			stopCalledBeforeLaunch = !launched
			return nil
		},
		launch: func(_ *cli.Context, _, _, _, _ string) error {
			launched = true
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stoppedID != "sess-xyz" {
		t.Errorf("stopSession id = %q, want %q", stoppedID, "sess-xyz")
	}
	if !launched {
		t.Fatal("expected launch to be called after superseding the live session")
	}
	if !stopCalledBeforeLaunch {
		t.Fatal("expected stopSession to be called before launch (stop-then-spawn)")
	}
	if agentsCalls != 2 {
		t.Fatalf("expected agentsFunc to be called twice (initial query + re-query after stop), got %d", agentsCalls)
	}
}

// TestResume_AgentsFuncError_RefusesAndDoesNotLaunch covers Fix 1: when the
// initial live-session query itself fails, resume must fail CLOSED (refuse)
// rather than proceed as if no live session existed — with or without
// --supersede, since a failed query means there's nothing to enumerate or
// stop either way.
func TestResume_AgentsFuncError_RefusesAndDoesNotLaunch(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: "at-qerr", Status: "open", Description: "worktree: " + dir + "\n"}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())

	cmd := &resumeKong{
		ID: "at-qerr",
		agentsFunc: func() ([]agentSession, error) {
			return nil, fmt.Errorf("agents: connection refused")
		},
		launch: func(_ *cli.Context, _, _, _, _ string) error {
			t.Fatal("launch called; want refusal when the live-session query fails")
			return nil
		},
	}
	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error refusing to resume, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "at-qerr") {
		t.Errorf("expected initiative id in stderr, got: %s", stderr.String())
	}
}

// TestResume_AgentsFuncError_SupersedeStillRefuses covers Fix 1's other half:
// --supersede cannot help when the query itself errors, since there is
// nothing enumerable to stop — the guard must still refuse.
func TestResume_AgentsFuncError_SupersedeStillRefuses(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: "at-qerr2", Status: "open", Description: "worktree: " + dir + "\n"}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	cmd := &resumeKong{
		ID:        "at-qerr2",
		Supersede: true,
		agentsFunc: func() ([]agentSession, error) {
			return nil, fmt.Errorf("agents: connection refused")
		},
		launch: func(_ *cli.Context, _, _, _, _ string) error {
			t.Fatal("launch called; want refusal when the live-session query fails even with --supersede")
			return nil
		},
	}
	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error refusing to resume, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

// TestResume_SupersedeStopFails_StillLiveOnRequery_Refuses covers Fix 2's
// abort path: stopSession reporting an error is not itself proof the session
// died — but here the re-query confirms it's still live, so resume must
// abort rather than launch a duplicate.
func TestResume_SupersedeStopFails_StillLiveOnRequery_Refuses(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: "at-stilllive", Status: "open", Description: "worktree: " + dir + "\n"}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, stderr := makeCtx(fbd, t.TempDir())

	cmd := &resumeKong{
		ID:        "at-stilllive",
		Supersede: true,
		agentsFunc: func() ([]agentSession, error) {
			// Both the initial query and the re-query see the session live.
			return []agentSession{{Name: filepath.Base(dir), ID: "sess-stuck", PID: &livePID}}, nil
		},
		stopSession: func(id string) error {
			return fmt.Errorf("stop: session busy")
		},
		launch: func(_ *cli.Context, _, _, _, _ string) error {
			t.Fatal("launch called; want abort when the session is still live after supersede stop")
			return nil
		},
	}
	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error aborting resume, got nil")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "sess-stuck") {
		t.Errorf("expected still-live session id %q in stderr, got: %s", "sess-stuck", stderr.String())
	}
}

// TestResume_SupersedeStopErrors_ButGoneOnRequery_Launches covers Fix 2's
// benign-race path: stopSession errors (e.g. the session already exited on
// its own between the initial query and the stop call), but the re-query
// shows it gone — resume should proceed and launch.
func TestResume_SupersedeStopErrors_ButGoneOnRequery_Launches(t *testing.T) {
	dir := t.TempDir()
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: "at-benignrace", Status: "open", Description: "worktree: " + dir + "\n"}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	var agentsCalls int
	var launched bool
	cmd := &resumeKong{
		ID:        "at-benignrace",
		Supersede: true,
		agentsFunc: func() ([]agentSession, error) {
			agentsCalls++
			if agentsCalls == 1 {
				return []agentSession{{Name: filepath.Base(dir), ID: "sess-raced", PID: &livePID}}, nil
			}
			// Re-query: already gone despite the stop call erroring.
			return nil, nil
		},
		stopSession: func(id string) error {
			return fmt.Errorf("stop: session %s: not found", id)
		},
		launch: func(_ *cli.Context, _, _, _, _ string) error {
			launched = true
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !launched {
		t.Fatal("expected launch to be called: stop errored but the session was already gone on re-query")
	}
}
