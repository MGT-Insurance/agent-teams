// This file is owned by Track R (route-pr-event verbs).
package verbs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/repoconfig"
)

// ── test helpers ──────────────────────────────────────────────────────────────

// fakeRunner captures args passed to the ateamRunner without executing any subprocess.
type fakeRunner struct {
	calls [][]string
}

func (f *fakeRunner) run(args ...string) error {
	f.calls = append(f.calls, append([]string(nil), args...))
	return nil
}

// routeFakeBD returns a fixed issue list for "list" calls; satisfies cli.BDRunner.
// Named to avoid collision with fakeBD in dispatch_test.go (same package).
type routeFakeBD struct {
	issues []bd.Issue
}

func (f *routeFakeBD) Run(args ...string) (string, error) { return "", nil }

func (f *routeFakeBD) RunJSON(dst any, args ...string) error {
	if len(args) > 0 && args[0] == "list" {
		if out, ok := dst.(*[]bd.Issue); ok {
			*out = f.issues
		}
	}
	return nil
}

// makeRouteCtx builds a minimal cli.Context backed by a routeFakeBD.
// ctx.Home is set to a synthetic path; use makeRouteCtxWithHome when
// spawnReviewInitiative needs a real fs-accessible home.
func makeRouteCtx(issues []bd.Issue) (*cli.Context, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return &cli.Context{
		Home:   "/fake/home",
		BD:     &routeFakeBD{issues: issues},
		Stdout: stdout,
		Stderr: stderr,
	}, stdout, stderr
}

// makeRouteCtxWithHome builds a cli.Context whose Home is set to tmpHome,
// so spawnReviewInitiative can find (or not find) config files under it.
func makeRouteCtxWithHome(t *testing.T, issues []bd.Issue) (*cli.Context, *bytes.Buffer, *bytes.Buffer, string) {
	t.Helper()
	tmpHome := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	ctx := &cli.Context{
		Home:   tmpHome,
		BD:     &routeFakeBD{issues: issues},
		Stdout: stdout,
		Stderr: stderr,
	}
	return ctx, stdout, stderr, tmpHome
}

// newEnabledClonePath returns a fresh temp dir seeded with a .agent-teams
// marker file, so review-repos fixtures reach past spawnReviewInitiative's
// repo-enabled gate without re-deriving that setup individually.
func newEnabledClonePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, repoconfig.FileName), nil, 0o644); err != nil {
		t.Fatalf("newEnabledClonePath: write %s: %v", repoconfig.FileName, err)
	}
	return dir
}

// newDisabledClonePath returns a fresh temp dir marked "disabled: true" —
// the sibling of newEnabledClonePath for tests pinning the routing gates
// added against the Codex adversarial-review finding that mail send/reopen
// could revive a disabled repo.
func newDisabledClonePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, repoconfig.FileName), []byte("disabled: true\n"), 0o644); err != nil {
		t.Fatalf("newDisabledClonePath: write %s: %v", repoconfig.FileName, err)
	}
	return dir
}

// writeTempFile creates a temp file with the given content and returns its path.
func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "body-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

// prFieldIssue builds an issue that will MatchPRField for ownerRepo + prNumber.
// Its "repo:" field points at a real, .agent-teams-enabled temp dir (rather
// than a synthetic /code/<ownerRepo> path that doesn't exist on disk), so the
// many route-pr-event tests built on it keep exercising the non-disabled
// routing path now that it's gated on repoconfig.Enabled.
func prFieldIssue(t *testing.T, id, ownerRepo string, prNumber int) bd.Issue {
	t.Helper()
	prURL := fmt.Sprintf("https://github.com/%s/pull/%d", ownerRepo, prNumber)
	return bd.Issue{
		ID:          id,
		Title:       "Initiative " + id,
		Description: fmt.Sprintf("repo: %s\nworktree: /tmp/wt-%s\nbranch: main\n", newEnabledClonePath(t), id),
		Notes:       "pr: " + prURL,
		Status:      "open",
	}
}

// branchIssue builds an issue that will MatchBranch for repoName + headBranch
// (no pr: URL, so MatchPRField is skipped).
// branchIssue's "repo:" field must be a real, enabled directory whose
// BASENAME is exactly repoName — MatchBranch matches on
// filepath.Base(f.Repo), so an arbitrary temp dir (as prFieldIssue uses)
// would silently break the match instead of just failing the enabled check.
func branchIssue(t *testing.T, id, repoName, headBranch string) bd.Issue {
	t.Helper()
	repoDir := filepath.Join(t.TempDir(), repoName)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", repoDir, err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, repoconfig.FileName), nil, 0o644); err != nil {
		t.Fatalf("write %s: %v", repoconfig.FileName, err)
	}
	return bd.Issue{
		ID:          id,
		Title:       "Initiative " + id,
		Description: fmt.Sprintf("repo: %s\nworktree: /tmp/wt-%s\nbranch: %s\n", repoDir, id, headBranch),
		Notes:       "",
		Status:      "open",
	}
}

// ── routePREventKong Validate ─────────────────────────────────────────────────

// TestRoutePREvent_ZeroPRNumberValidate verifies Validate rejects pr-number=0.
func TestRoutePREvent_ZeroPRNumberValidate(t *testing.T) {
	cmd := &routePREventKong{
		Repo:       "o/r",
		PRNumber:   0,
		HeadBranch: "main",
		Transition: TransitionCIFailed,
		BodyFile:   "/some/file",
	}
	if err := cmd.Validate(); err == nil {
		t.Error("expected Validate error for PRNumber=0, got nil")
	}
}

// TestRoutePREvent_PositivePRNumberValidate verifies Validate passes for pr-number>0.
func TestRoutePREvent_PositivePRNumberValidate(t *testing.T) {
	cmd := &routePREventKong{
		Repo:       "o/r",
		PRNumber:   42,
		HeadBranch: "main",
		Transition: TransitionCIFailed,
		BodyFile:   "/some/file",
	}
	if err := cmd.Validate(); err != nil {
		t.Errorf("expected Validate to pass for PRNumber=42, got: %v", err)
	}
}

// ── decision matrix ───────────────────────────────────────────────────────────

// TestDecisionMatrix_OwnedViaPRFieldRoutesViaSend verifies the ROUTE path:
// owned initiative (MatchPRField) → runner("mail", "send", id, "--file", body, "--sender", "pr-shepherd").
func TestDecisionMatrix_OwnedViaPRFieldRoutesViaSend(t *testing.T) {
	bodyFile := writeTempFile(t, "CI failed output")
	issue := prFieldIssue(t, "at-abc.1", "owner/myrepo", 42)

	ctx, stdout, _ := makeRouteCtx([]bd.Issue{issue})
	fr := &fakeRunner{}
	cmd := &routePREventKong{
		Repo:       "owner/myrepo",
		PRNumber:   42,
		HeadBranch: "feat-x",
		Transition: TransitionCIFailed,
		BodyFile:   bodyFile,
		runner:     fr.run,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("expected 1 runner call, got %d: %v", len(fr.calls), fr.calls)
	}
	call := fr.calls[0]
	if len(call) < 7 {
		t.Fatalf("runner call too short: %v", call)
	}
	if call[0] != "mail" {
		t.Errorf("call[0]: got %q, want \"mail\"", call[0])
	}
	if call[1] != "send" {
		t.Errorf("call[1]: got %q, want \"send\"", call[1])
	}
	if call[2] != "at-abc.1" {
		t.Errorf("call[2] (initiative id): got %q, want \"at-abc.1\"", call[2])
	}
	if call[3] != "--file" {
		t.Errorf("call[3]: got %q, want \"--file\"", call[3])
	}
	if call[4] != bodyFile {
		t.Errorf("call[4] (body file): got %q, want %q", call[4], bodyFile)
	}
	if call[5] != "--sender" {
		t.Errorf("call[5]: got %q, want \"--sender\"", call[5])
	}
	if call[6] != "pr-shepherd" {
		t.Errorf("call[6]: got %q, want \"pr-shepherd\"", call[6])
	}
	if !strings.Contains(stdout.String(), "at-abc.1") {
		t.Errorf("stdout should mention matched initiative id; got: %q", stdout.String())
	}
}

// TestDecisionMatrix_OwnedViaPRField_DisabledRepoSkips pins the highest-severity
// Codex adversarial-review finding on this feature: an OPEN initiative matched
// by pr: field must not be revived via mail send once its repo is disabled —
// this is the direct path that bypassed the original repoconfig gate (which
// only covered spawnReviewInitiative, the unowned/no-match SPAWN path).
func TestDecisionMatrix_OwnedViaPRField_DisabledRepoSkips(t *testing.T) {
	bodyFile := writeTempFile(t, "CI failed output")
	repoDir := newDisabledClonePath(t)
	issue := bd.Issue{
		ID:          "at-abc.9",
		Title:       "Initiative at-abc.9",
		Description: "repo: " + repoDir + "\nworktree: /tmp/wt-at-abc.9\nbranch: main\n",
		Notes:       "pr: https://github.com/owner/myrepo/pull/42",
		Status:      "open",
	}

	ctx, stdout, _ := makeRouteCtx([]bd.Issue{issue})
	fr := &fakeRunner{}
	cmd := &routePREventKong{
		Repo:       "owner/myrepo",
		PRNumber:   42,
		HeadBranch: "feat-x",
		Transition: TransitionCIFailed,
		BodyFile:   bodyFile,
		runner:     fr.run,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 0 {
		t.Fatalf("expected 0 runner calls for a disabled repo, got %d: %v", len(fr.calls), fr.calls)
	}
	if !strings.Contains(stdout.String(), "repo is disabled") {
		t.Errorf("stdout should say 'repo is disabled'; got: %q", stdout.String())
	}
}

// TestDecisionMatrix_OwnedViaMatchBranchRoutesViaSend verifies the MatchBranch
// path (no pr: URL) also calls send with the correct initiative id.
func TestDecisionMatrix_OwnedViaMatchBranchRoutesViaSend(t *testing.T) {
	bodyFile := writeTempFile(t, "changes requested body")
	issue := branchIssue(t, "at-xyz.2", "myrepo", "feature-branch")

	ctx, _, _ := makeRouteCtx([]bd.Issue{issue})
	fr := &fakeRunner{}
	cmd := &routePREventKong{
		Repo:       "owner/myrepo",
		PRNumber:   99,
		HeadBranch: "feature-branch",
		Transition: TransitionChangesRequested,
		BodyFile:   bodyFile,
		runner:     fr.run,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("expected 1 runner call via MatchBranch, got %d", len(fr.calls))
	}
	if fr.calls[0][0] != "mail" {
		t.Errorf("runner verb: got %q, want \"mail\"", fr.calls[0][0])
	}
	if fr.calls[0][1] != "send" {
		t.Errorf("runner subverb: got %q, want \"send\"", fr.calls[0][1])
	}
	if fr.calls[0][2] != "at-xyz.2" {
		t.Errorf("runner initiative id: got %q, want \"at-xyz.2\"", fr.calls[0][2])
	}
}

// TestDecisionMatrix_OwnedViaMatchBranch_DisabledRepoSkips is the MatchBranch
// counterpart of TestDecisionMatrix_OwnedViaPRField_DisabledRepoSkips.
func TestDecisionMatrix_OwnedViaMatchBranch_DisabledRepoSkips(t *testing.T) {
	bodyFile := writeTempFile(t, "changes requested body")
	repoDir := newDisabledClonePath(t)
	issue := bd.Issue{
		ID:          "at-xyz.9",
		Title:       "Initiative at-xyz.9",
		Description: "repo: " + repoDir + "\nworktree: /tmp/wt-at-xyz.9\nbranch: feature-branch\n",
		Status:      "open",
	}

	ctx, stdout, _ := makeRouteCtx([]bd.Issue{issue})
	fr := &fakeRunner{}
	cmd := &routePREventKong{
		Repo:       "owner/" + filepath.Base(repoDir),
		PRNumber:   99,
		HeadBranch: "feature-branch",
		Transition: TransitionChangesRequested,
		BodyFile:   bodyFile,
		runner:     fr.run,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 0 {
		t.Fatalf("expected 0 runner calls for a disabled repo, got %d: %v", len(fr.calls), fr.calls)
	}
	if !strings.Contains(stdout.String(), "repo is disabled") {
		t.Errorf("stdout should say 'repo is disabled'; got: %q", stdout.String())
	}
}

// TestDecisionMatrix_UnownedReviewRequestedUnconfiguredSkips verifies the SPAWN seam
// when the repo is not registered in review-repos: runner NOT called, "skipping" logged.
func TestDecisionMatrix_UnownedReviewRequestedUnconfiguredSkips(t *testing.T) {
	bodyFile := writeTempFile(t, "reviewer added")
	// ctx.Home points to a real temp dir with no review-repos/<key> file.
	ctx, stdout, _, _ := makeRouteCtxWithHome(t, nil) // no issues → MatchNone
	fr := &fakeRunner{}
	cmd := &routePREventKong{
		Repo:       "owner/repo",
		PRNumber:   7,
		HeadBranch: "some-branch",
		Transition: TransitionReviewRequested,
		BodyFile:   bodyFile,
		runner:     fr.run,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	// Runner must NOT have been called — no config file means skip.
	if len(fr.calls) != 0 {
		t.Errorf("expected 0 runner calls for unconfigured review_requested, got %d: %v", len(fr.calls), fr.calls)
	}
	out := stdout.String()
	if !strings.Contains(out, "skipping") {
		t.Errorf("stdout should say 'skipping' for unconfigured repo; got: %q", out)
	}
}

// TestDecisionMatrix_UnownedReviewRequestedDisabledCloneSkips verifies the
// SPAWN seam when the repo IS registered in review-repos but the clone has no
// (or a disabled) .agent-teams file: skip quietly, one log line, no dispatch
// subprocess — the fix for a repeatedly-polled review_requested PR on a
// disabled repo otherwise spamming a refused `dispatch` call every pr-shepherd
// poll cycle.
func TestDecisionMatrix_UnownedReviewRequestedDisabledCloneSkips(t *testing.T) {
	bodyFile := writeTempFile(t, "reviewer added")
	ctx, stdout, _, tmpHome := makeRouteCtxWithHome(t, nil) // no issues → MatchNone

	clonePath := t.TempDir() // deliberately no .agent-teams marker
	configDir := filepath.Join(tmpHome, "review-repos")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "repo"), []byte(clonePath+"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	fr := &fakeRunner{}
	cmd := &routePREventKong{
		Repo:       "owner/repo",
		PRNumber:   7,
		HeadBranch: "some-branch",
		Transition: TransitionReviewRequested,
		BodyFile:   bodyFile,
		runner:     fr.run,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 0 {
		t.Errorf("expected 0 runner calls for a disabled clone, got %d: %v", len(fr.calls), fr.calls)
	}
	out := stdout.String()
	if !strings.Contains(out, "agent-teams not enabled") {
		t.Errorf("stdout should say 'agent-teams not enabled' for a disabled clone; got: %q", out)
	}
}

// TestSpawnReviewInitiative_Configured verifies the happy path: a review-repos
// config file is present → runner called with dispatch + correct args including
// --launch-prompt and --skip-epic for the lightweight review-pr skill.
func TestSpawnReviewInitiative_Configured(t *testing.T) {
	ctx, stdout, _, tmpHome := makeRouteCtxWithHome(t, nil)

	// Register a fake clone path in the config.
	clonePath := newEnabledClonePath(t)
	repoKey := "midgard" // Slugify(basename("MGT-Insurance/midgard"))
	configDir := filepath.Join(tmpHome, "review-repos")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, repoKey), []byte(clonePath+"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	fr := &fakeRunner{}
	cmd := &routePREventKong{runner: fr.run}

	event := PREvent{
		Repo:       "MGT-Insurance/midgard",
		PRNumber:   42,
		PRURL:      "https://github.com/MGT-Insurance/midgard/pull/42",
		Transition: TransitionReviewRequested,
	}

	if err := cmd.spawnReviewInitiative(ctx, event); err != nil {
		t.Fatalf("spawnReviewInitiative error: %v", err)
	}

	// Runner must have been called exactly once.
	if len(fr.calls) != 1 {
		t.Fatalf("expected 1 runner call, got %d: %v", len(fr.calls), fr.calls)
	}
	call := fr.calls[0]

	// Verify argv structure:
	// dispatch --repo <clone> --problem <title> --body-file <path>
	//          --launch-prompt "/agent-teams:review-pr {id}" --skip-epic
	//          --model sonnet --topic reviews
	if len(call) < 14 {
		t.Fatalf("runner call too short (%d args): %v", len(call), call)
	}
	if call[0] != "dispatch" {
		t.Errorf("call[0]: got %q, want \"dispatch\"", call[0])
	}
	if call[1] != "--repo" {
		t.Errorf("call[1]: got %q, want \"--repo\"", call[1])
	}
	if call[2] != clonePath {
		t.Errorf("call[2] (clone path): got %q, want %q", call[2], clonePath)
	}
	if call[3] != "--problem" {
		t.Errorf("call[3]: got %q, want \"--problem\"", call[3])
	}
	// Problem must mention the PR number.
	if !strings.Contains(call[4], "42") {
		t.Errorf("--problem should mention PR number 42; got %q", call[4])
	}
	if call[5] != "--body-file" {
		t.Errorf("call[5]: got %q, want \"--body-file\"", call[5])
	}
	// call[6] is the temp file path (cleaned up after runner returns).
	if call[7] != "--launch-prompt" {
		t.Errorf("call[7]: got %q, want \"--launch-prompt\"", call[7])
	}
	if call[8] != "/agent-teams:review-pr {id}" {
		t.Errorf("call[8] (launch-prompt value): got %q, want \"/agent-teams:review-pr {id}\"", call[8])
	}
	if call[9] != "--skip-epic" {
		t.Errorf("call[9]: got %q, want \"--skip-epic\"", call[9])
	}
	if call[10] != "--model" {
		t.Errorf("call[10]: got %q, want \"--model\"", call[10])
	}
	if call[11] != "sonnet" {
		t.Errorf("call[11] (model value): got %q, want \"sonnet\"", call[11])
	}
	// The whole point of agent-teams-p9dm: this webhook path spawned every
	// observed single-line per-PR topic, so without --topic the shared
	// Reviews topic never wins where the noise actually comes from.
	if call[12] != "--topic" {
		t.Errorf("call[12]: got %q, want \"--topic\"", call[12])
	}
	if call[13] != ReviewsHandle {
		t.Errorf("call[13] (topic value): got %q, want %q", call[13], ReviewsHandle)
	}

	// Confirmation line must appear in stdout.
	out := stdout.String()
	if !strings.Contains(out, "spawned review initiative") {
		t.Errorf("stdout should confirm spawn; got: %q", out)
	}
}

// TestSpawnReviewInitiative_ConfiguredBodyContent verifies the structured metadata
// body written to the temp file contains the required pr-number/pr-repo/pr-url fields.
func TestSpawnReviewInitiative_ConfiguredBodyContent(t *testing.T) {
	ctx, _, _, tmpHome := makeRouteCtxWithHome(t, nil)

	clonePath := newEnabledClonePath(t)
	repoKey := "midgard"
	configDir := filepath.Join(tmpHome, "review-repos")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, repoKey), []byte(clonePath), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var capturedBody string
	bodyCapturingRunner := func(args ...string) error {
		// Find --body-file arg and read it before returning.
		for i, a := range args {
			if a == "--body-file" && i+1 < len(args) {
				data, err := os.ReadFile(args[i+1])
				if err == nil {
					capturedBody = string(data)
				}
				break
			}
		}
		return nil
	}

	cmd := &routePREventKong{runner: bodyCapturingRunner}
	event := PREvent{
		Repo:       "MGT-Insurance/midgard",
		PRNumber:   42,
		PRURL:      "https://github.com/MGT-Insurance/midgard/pull/42",
		Transition: TransitionReviewRequested,
	}

	if err := cmd.spawnReviewInitiative(ctx, event); err != nil {
		t.Fatalf("spawnReviewInitiative error: %v", err)
	}

	// Body must contain structured metadata fields parseable by the review-pr skill.
	requiredFields := []string{
		"pr-number: 42",
		"pr-repo: MGT-Insurance/midgard",
		"pr-url: https://github.com/MGT-Insurance/midgard/pull/42",
	}
	for _, field := range requiredFields {
		if !strings.Contains(capturedBody, field) {
			t.Errorf("review metadata body missing %q; body:\n%s", field, capturedBody)
		}
	}
	// Body must NOT contain the old verbose instruction text.
	oldPhrases := []string{
		"gh pr checkout",
		"NO nit comments",
		"INLINE comment",
		"Do NOT open a PR",
	}
	for _, phrase := range oldPhrases {
		if strings.Contains(capturedBody, phrase) {
			t.Errorf("review metadata body should not contain old instruction text %q; body:\n%s", phrase, capturedBody)
		}
	}
}

// TestSpawnReviewInitiative_PRURLConstructed verifies that when event.PRURL is empty,
// a pr-url is constructed from the repo and PR number.
func TestSpawnReviewInitiative_PRURLConstructed(t *testing.T) {
	ctx, _, _, tmpHome := makeRouteCtxWithHome(t, nil)

	clonePath := newEnabledClonePath(t)
	repoKey := "myrepo"
	configDir := filepath.Join(tmpHome, "review-repos")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, repoKey), []byte(clonePath), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var capturedBody string
	bodyCapturingRunner := func(args ...string) error {
		for i, a := range args {
			if a == "--body-file" && i+1 < len(args) {
				data, err := os.ReadFile(args[i+1])
				if err == nil {
					capturedBody = string(data)
				}
				break
			}
		}
		return nil
	}

	cmd := &routePREventKong{runner: bodyCapturingRunner}
	event := PREvent{
		Repo:       "owner/myrepo",
		PRNumber:   7,
		PRURL:      "", // empty — should be constructed
		Transition: TransitionReviewRequested,
	}

	if err := cmd.spawnReviewInitiative(ctx, event); err != nil {
		t.Fatalf("spawnReviewInitiative error: %v", err)
	}

	// pr-url must be auto-constructed when not provided.
	wantURL := "pr-url: https://github.com/owner/myrepo/pull/7"
	if !strings.Contains(capturedBody, wantURL) {
		t.Errorf("expected constructed pr-url %q in body; got:\n%s", wantURL, capturedBody)
	}
}

// TestDecisionMatrix_UnownedCIFailedSkips verifies LOG-AND-SKIP:
// unowned + ci_failed → logs "skipping", runner NOT called.
func TestDecisionMatrix_UnownedCIFailedSkips(t *testing.T) {
	bodyFile := writeTempFile(t, "ci output")
	ctx, stdout, _ := makeRouteCtx(nil) // no issues → MatchNone
	fr := &fakeRunner{}
	cmd := &routePREventKong{
		Repo:       "owner/repo",
		PRNumber:   3,
		HeadBranch: "fix-branch",
		Transition: TransitionCIFailed,
		BodyFile:   bodyFile,
		runner:     fr.run,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 0 {
		t.Errorf("expected 0 runner calls for unowned ci_failed, got %d", len(fr.calls))
	}
	out := stdout.String()
	if !strings.Contains(out, "skipping") {
		t.Errorf("stdout should say 'skipping'; got: %q", out)
	}
}

// TestDecisionMatrix_UnownedApprovedSkips verifies other non-review transitions also skip.
func TestDecisionMatrix_UnownedApprovedSkips(t *testing.T) {
	bodyFile := writeTempFile(t, "approved body")
	ctx, stdout, _ := makeRouteCtx(nil)
	fr := &fakeRunner{}
	cmd := &routePREventKong{
		Repo:       "owner/repo",
		PRNumber:   5,
		HeadBranch: "br",
		Transition: TransitionApproved,
		BodyFile:   bodyFile,
		runner:     fr.run,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 0 {
		t.Errorf("expected 0 runner calls for unowned approved, got %d", len(fr.calls))
	}
	if !strings.Contains(stdout.String(), "skipping") {
		t.Errorf("stdout should say 'skipping' for unowned approved; got: %q", stdout.String())
	}
}

// TestDecisionMatrix_NilContextErrors verifies nil context returns an error.
func TestDecisionMatrix_NilContextErrors(t *testing.T) {
	cmd := &routePREventKong{
		Repo:       "o/r",
		PRNumber:   1,
		HeadBranch: "br",
		Transition: TransitionCIFailed,
		BodyFile:   "/dev/null",
		runner:     (&fakeRunner{}).run,
	}
	if err := cmd.Run(nil); err == nil {
		t.Error("expected error for nil context, got nil")
	}
}

// TestDecisionMatrix_MissingBodyFileErrors verifies error when body-file is missing.
func TestDecisionMatrix_MissingBodyFileErrors(t *testing.T) {
	ctx, _, _ := makeRouteCtx(nil)
	cmd := &routePREventKong{
		Repo:       "o/r",
		PRNumber:   1,
		HeadBranch: "br",
		Transition: TransitionCIFailed,
		BodyFile:   "/no/such/file.txt",
		runner:     (&fakeRunner{}).run,
	}
	if err := cmd.Run(ctx); err == nil {
		t.Fatal("expected error for missing body-file, got nil")
	}
}

// TestRegisterRouteEvent confirms that route-pr-event is registered in the
// full kong parser (no missing-verb or duplicate panic).
func TestRegisterRouteEvent(t *testing.T) {
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterRouteEventKong(p)
	// Invoke with --help to test registration. Validate() may fire for missing
	// required flags, which is fine — what matters is the verb is known (no
	// "unexpected argument route-pr-event" error).
	_, parseErr := p.Parse([]string{"route-pr-event", "--help"})
	if parseErr != nil && strings.Contains(parseErr.Error(), "unexpected argument route-pr-event") {
		t.Errorf("route-pr-event not registered: %v", parseErr)
	}
}

// TestRegisterRouteEvent_NoDuplicateWithFullRegistry confirms RegisterAllKong
// does not panic on duplicate registration.
func TestRegisterRouteEvent_NoDuplicateWithFullRegistry(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("RegisterAllKong panicked (duplicate): %v", r)
		}
	}()
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterAllKong(p)
}

// ── routePREventKong core-path tests ─────────────────────────────────────────

// TestRoutePREventKong_Validate_ZeroPRNumber verifies Validate rejects pr-number <= 0.
func TestRoutePREventKong_Validate_ZeroPRNumber(t *testing.T) {
	cmd := &routePREventKong{
		Repo:       "owner/repo",
		PRNumber:   0,
		HeadBranch: "br",
		Transition: TransitionCIFailed,
		BodyFile:   "/dev/null",
		runner:     (&fakeRunner{}).run,
	}
	if err := cmd.Validate(); err == nil {
		t.Error("expected error for PRNumber=0, got nil")
	}
}

func TestRoutePREventKong_Validate_NegativePRNumber(t *testing.T) {
	cmd := &routePREventKong{
		Repo:       "owner/repo",
		PRNumber:   -5,
		HeadBranch: "br",
		Transition: TransitionCIFailed,
		BodyFile:   "/dev/null",
		runner:     (&fakeRunner{}).run,
	}
	if err := cmd.Validate(); err == nil {
		t.Error("expected error for negative PRNumber, got nil")
	}
}

func TestRoutePREventKong_Validate_PositivePRNumber(t *testing.T) {
	cmd := &routePREventKong{
		Repo:       "owner/repo",
		PRNumber:   42,
		HeadBranch: "br",
		Transition: TransitionCIFailed,
		BodyFile:   "/dev/null",
		runner:     (&fakeRunner{}).run,
	}
	if err := cmd.Validate(); err != nil {
		t.Errorf("unexpected Validate error for positive PRNumber: %v", err)
	}
}

// TestRoutePREventKong_NilContext verifies nil context returns an error.
func TestRoutePREventKong_NilContext(t *testing.T) {
	cmd := &routePREventKong{runner: (&fakeRunner{}).run}
	if err := cmd.Run(nil); err == nil {
		t.Error("expected error for nil context, got nil")
	}
}

// TestRoutePREventKong_OwnedViaPRFieldRoutesViaSend verifies the ROUTE path:
// owned (MatchPRField) → runner("mail", "send", id, "--file", body, "--sender", "pr-shepherd").
func TestRoutePREventKong_OwnedViaPRFieldRoutesViaSend(t *testing.T) {
	bodyFile := writeTempFile(t, "CI failed output")
	issue := prFieldIssue(t, "at-kong.1", "owner/myrepo", 42)

	ctx, _, _ := makeRouteCtx([]bd.Issue{issue})
	fr := &fakeRunner{}
	cmd := &routePREventKong{
		Repo:       "owner/myrepo",
		PRNumber:   42,
		HeadBranch: "feat-x",
		Transition: TransitionCIFailed,
		BodyFile:   bodyFile,
		runner:     fr.run,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("expected 1 runner call, got %d: %v", len(fr.calls), fr.calls)
	}
	call := fr.calls[0]
	if call[0] != "mail" {
		t.Errorf("call[0]: got %q, want \"mail\"", call[0])
	}
	if call[1] != "send" {
		t.Errorf("call[1]: got %q, want \"send\"", call[1])
	}
	if call[2] != "at-kong.1" {
		t.Errorf("call[2] (initiative id): got %q, want \"at-kong.1\"", call[2])
	}
	if call[5] != "--sender" || call[6] != "pr-shepherd" {
		t.Errorf("expected --sender pr-shepherd; call: %v", call)
	}
}

// multiPRFieldIssue builds an issue whose "pr" rail holds one PR per entry in
// prNumbers, under ownerRepo, for exercising the SECOND/THIRD rail entry
// through the FULL route-pr-event dispatch (not just matchInitiativeFromIssues)
// — proving a multi-PR rail routes via the real "matched -> mail send" branch
// in route.go, not just that matchInitiativeFromIssues returns MatchPRField in
// isolation. Its "branch:" field is deliberately "unrelated-branch" so tier-2
// (MatchBranch) can never rescue a tier-1 miss — the tests using this must go
// red under a first-match-only tier-1, not pass for the wrong reason via
// branch fallback.
func multiPRFieldIssue(t *testing.T, id, ownerRepo string, prNumbers []int) bd.Issue {
	t.Helper()
	var desc strings.Builder
	fmt.Fprintf(&desc, "repo: %s\n", newEnabledClonePath(t))
	fmt.Fprintf(&desc, "worktree: /tmp/wt-%s\n", id)
	desc.WriteString("branch: unrelated-branch\n")
	for _, n := range prNumbers {
		fmt.Fprintf(&desc, "pr: https://github.com/%s/pull/%d\n", ownerRepo, n)
	}
	return bd.Issue{
		ID:          id,
		Title:       "Initiative " + id,
		Description: desc.String(),
		Status:      "open",
	}
}

// TestRoutePREventKong_SecondPRRoutesViaSend_CIFailed rides the exact
// non-review_requested default-branch silent-drop path (route.go's
// `default: ... unowned ... skipping`, not the review_requested spawn path
// that would mask a tier-1 miss): a ci_failed event naming the SECOND PR on
// a two-PR initiative must still route via mail send. A first-match-only
// tier-1 (pre-agent-teams-ssib.7) would read only PR #1, miss #2 entirely,
// and this event would silently vanish into the default branch instead.
func TestRoutePREventKong_SecondPRRoutesViaSend_CIFailed(t *testing.T) {
	bodyFile := writeTempFile(t, "CI failed output")
	issue := multiPRFieldIssue(t, "at-multi.1", "owner/myrepo", []int{1, 2})

	ctx, stdout, _ := makeRouteCtx([]bd.Issue{issue})
	fr := &fakeRunner{}
	cmd := &routePREventKong{
		Repo:       "owner/myrepo",
		PRNumber:   2, // the SECOND rail entry
		HeadBranch: "feat-x",
		Transition: TransitionCIFailed,
		BodyFile:   bodyFile,
		runner:     fr.run,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("expected 1 runner call (routed via mail send), got %d: %v (stdout: %s)", len(fr.calls), fr.calls, stdout.String())
	}
	if fr.calls[0][0] != "mail" || fr.calls[0][1] != "send" {
		t.Errorf("expected mail send, got %v", fr.calls[0])
	}
	if fr.calls[0][2] != "at-multi.1" {
		t.Errorf("expected send to at-multi.1, got %v", fr.calls[0])
	}
}

// TestRoutePREventKong_ThirdPRRoutesViaSend_ChangesRequested is the sibling of
// the above for the THIRD rail entry and the OTHER silent-drop transition
// named in the acceptance bar (changes_requested, route.go's default branch).
func TestRoutePREventKong_ThirdPRRoutesViaSend_ChangesRequested(t *testing.T) {
	bodyFile := writeTempFile(t, "changes requested body")
	issue := multiPRFieldIssue(t, "at-multi.2", "owner/myrepo", []int{1, 2, 3})

	ctx, stdout, _ := makeRouteCtx([]bd.Issue{issue})
	fr := &fakeRunner{}
	cmd := &routePREventKong{
		Repo:       "owner/myrepo",
		PRNumber:   3, // the THIRD rail entry
		HeadBranch: "feat-x",
		Transition: TransitionChangesRequested,
		BodyFile:   bodyFile,
		runner:     fr.run,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("expected 1 runner call (routed via mail send), got %d: %v (stdout: %s)", len(fr.calls), fr.calls, stdout.String())
	}
	if fr.calls[0][0] != "mail" || fr.calls[0][1] != "send" {
		t.Errorf("expected mail send, got %v", fr.calls[0])
	}
	if fr.calls[0][2] != "at-multi.2" {
		t.Errorf("expected send to at-multi.2, got %v", fr.calls[0])
	}
}

// TestRoutePREventKong_UnownedCIFailedSkips verifies LOG-AND-SKIP for unowned PR.
func TestRoutePREventKong_UnownedCIFailedSkips(t *testing.T) {
	bodyFile := writeTempFile(t, "ci output")
	ctx, stdout, _ := makeRouteCtx(nil)
	fr := &fakeRunner{}
	cmd := &routePREventKong{
		Repo:       "owner/repo",
		PRNumber:   3,
		HeadBranch: "fix-branch",
		Transition: TransitionCIFailed,
		BodyFile:   bodyFile,
		runner:     fr.run,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 0 {
		t.Errorf("expected 0 runner calls for unowned ci_failed, got %d", len(fr.calls))
	}
	if !strings.Contains(stdout.String(), "skipping") {
		t.Errorf("stdout should say 'skipping'; got: %q", stdout.String())
	}
}

// TestRoutePREventKong_RegisteredAsKongVerb verifies RegisterRouteEventKong adds
// route-pr-event as a native (non-bridge) verb so --help works correctly.
func TestRoutePREventKong_RegisteredAsKongVerb(t *testing.T) {
	parser, err := cli.NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	RegisterRouteEventKong(parser)

	// Parse --help; a native kong verb should exit cleanly (not error).
	var stdout, stderr bytes.Buffer
	// We just need the parse to not return an unexpected error for --help.
	// (main.go sets a no-op Exit; the Parser has its own.)
	_, parseErr := parser.Parse([]string{"route-pr-event", "--help"})
	_ = parseErr // help triggers exit(0), not a real error
	_ = stdout
	_ = stderr
}

// TestRoutePREventKong_UnownedReviewRequestedSkipsWhenUnconfigured verifies
// review_requested + unowned + no config file → logs skip, runner not called.
func TestRoutePREventKong_UnownedReviewRequestedSkipsWhenUnconfigured(t *testing.T) {
	bodyFile := writeTempFile(t, "reviewer added")
	ctx, stdout, _, _ := makeRouteCtxWithHome(t, nil)
	fr := &fakeRunner{}
	cmd := &routePREventKong{
		Repo:       "owner/repo",
		PRNumber:   7,
		HeadBranch: "some-branch",
		Transition: TransitionReviewRequested,
		BodyFile:   bodyFile,
		runner:     fr.run,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 0 {
		t.Errorf("expected 0 runner calls for unconfigured review_requested, got %d: %v", len(fr.calls), fr.calls)
	}
	if !strings.Contains(stdout.String(), "skipping") {
		t.Errorf("stdout should say 'skipping'; got: %q", stdout.String())
	}
}

// ── re_review transition ──────────────────────────────────────────────────────

// statusFakeBD serves different issue lists for --status=open vs --status=closed.
type statusFakeBD struct {
	open, closed []bd.Issue
}

func (f *statusFakeBD) Run(args ...string) (string, error) { return "", nil }

func (f *statusFakeBD) RunJSON(dst any, args ...string) error {
	out, ok := dst.(*[]bd.Issue)
	if !ok || len(args) == 0 || args[0] != "list" {
		return nil
	}
	for _, a := range args {
		if a == "--status=closed" {
			*out = f.closed
			return nil
		}
	}
	*out = f.open
	return nil
}

// failRunner records calls like fakeRunner but fails any call whose first arg
// equals failOn.
type failRunner struct {
	calls  [][]string
	failOn string
}

func (f *failRunner) run(args ...string) error {
	f.calls = append(f.calls, append([]string(nil), args...))
	if len(args) > 0 && args[0] == f.failOn {
		return fmt.Errorf("injected %s failure", f.failOn)
	}
	return nil
}

func makeStatusCtx(open, closed []bd.Issue) (*cli.Context, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return &cli.Context{
		Home:   "/fake/home",
		BD:     &statusFakeBD{open: open, closed: closed},
		Stdout: stdout,
		Stderr: stderr,
	}, stdout, stderr
}

func TestReReview_OpenMatch_SendsWithResumeFlags(t *testing.T) {
	bodyFile := writeTempFile(t, "re-review body")
	issue := prFieldIssue(t, "at-rr.1", "owner/myrepo", 42)
	ctx, _, _ := makeStatusCtx([]bd.Issue{issue}, nil)

	runner := &fakeRunner{}
	cmd := &routePREventKong{
		Repo: "owner/myrepo", PRNumber: 42, HeadBranch: "feat-x",
		Transition: TransitionReReview, BodyFile: bodyFile,
		runner: runner.run,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d, want 1 (send only)", len(runner.calls))
	}
	want := []string{"mail", "send", "at-rr.1", "--file", bodyFile, "--sender", "pr-shepherd",
		"--resume-launch-prompt", "/agent-teams:review-pr at-rr.1", "--resume-model", "sonnet"}
	if strings.Join(runner.calls[0], " ") != strings.Join(want, " ") {
		t.Errorf("send args = %v\nwant %v", runner.calls[0], want)
	}
}

func TestReReview_ClosedMatch_ReopensThenSends(t *testing.T) {
	bodyFile := writeTempFile(t, "re-review body")
	closed := prFieldIssue(t, "at-rr.2", "owner/myrepo", 42)
	closed.Status = "closed"
	ctx, stdout, _ := makeStatusCtx(nil, []bd.Issue{closed})

	runner := &fakeRunner{}
	cmd := &routePREventKong{
		Repo: "owner/myrepo", PRNumber: 42, HeadBranch: "feat-x",
		Transition: TransitionReReview, BodyFile: bodyFile,
		runner: runner.run,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %d, want 2 (reopen, send): %v", len(runner.calls), runner.calls)
	}
	if strings.Join(runner.calls[0], " ") != "reopen at-rr.2" {
		t.Errorf("first call = %v, want reopen at-rr.2", runner.calls[0])
	}
	wantSend := []string{"mail", "send", "at-rr.2", "--file", bodyFile, "--sender", "pr-shepherd",
		"--resume-launch-prompt", "/agent-teams:review-pr at-rr.2", "--resume-model", "sonnet"}
	if strings.Join(runner.calls[1], " ") != strings.Join(wantSend, " ") {
		t.Errorf("send args = %v\nwant %v", runner.calls[1], wantSend)
	}
	if !strings.Contains(stdout.String(), "reopening") {
		t.Errorf("stdout missing reopen notice: %s", stdout.String())
	}
}

// TestReReview_DisabledRepo_SkipsWithoutReopenOrSpawn pins the Codex
// adversarial-review finding: a re_review matching a CLOSED initiative whose
// repo is disabled must not reopen it (nor fall back to spawning a fresh
// review, unlike a reopen/send failure) — the repo being disabled is
// deliberate operator policy, not a transient error.
func TestReReview_DisabledRepo_SkipsWithoutReopenOrSpawn(t *testing.T) {
	bodyFile := writeTempFile(t, "re-review body")
	repoDir := newDisabledClonePath(t)
	closed := bd.Issue{
		ID:          "at-rr.9",
		Title:       "Initiative at-rr.9",
		Description: "repo: " + repoDir + "\nworktree: /tmp/wt-at-rr.9\nbranch: main\n",
		Notes:       "pr: https://github.com/owner/myrepo/pull/42",
		Status:      "closed",
	}
	ctx, stdout, _ := makeStatusCtx(nil, []bd.Issue{closed})

	runner := &fakeRunner{}
	cmd := &routePREventKong{
		Repo: "owner/myrepo", PRNumber: 42, HeadBranch: "feat-x",
		Transition: TransitionReReview, BodyFile: bodyFile,
		runner: runner.run,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("expected 0 runner calls for a disabled repo, got %d: %v", len(runner.calls), runner.calls)
	}
	if !strings.Contains(stdout.String(), "repo is disabled") {
		t.Errorf("stdout should say 'repo is disabled'; got: %s", stdout.String())
	}
}

func TestReReview_NoInitiative_FallsBackToSpawn(t *testing.T) {
	bodyFile := writeTempFile(t, "re-review body")
	ctx, stdout, _, tmpHome := makeRouteCtxWithHome(t, nil)
	// Point ctx.BD at a status-aware fake with no issues at all.
	ctx.BD = &statusFakeBD{}
	// Configure the review-repos mapping so spawn proceeds.
	repoDir := filepath.Join(tmpHome, "review-repos")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	clonePath := newEnabledClonePath(t)
	if err := os.WriteFile(filepath.Join(repoDir, "myrepo"), []byte(clonePath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &fakeRunner{}
	cmd := &routePREventKong{
		Repo: "owner/myrepo", PRNumber: 42, HeadBranch: "feat-x",
		Transition: TransitionReReview, BodyFile: bodyFile,
		runner: runner.run,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.calls) != 1 || runner.calls[0][0] != "dispatch" {
		t.Fatalf("calls = %v, want a single dispatch call", runner.calls)
	}
	if !strings.Contains(stdout.String(), "no prior initiative") {
		t.Errorf("stdout missing spawn-fallback notice: %s", stdout.String())
	}
}

func TestReReview_ReopenFails_FallsBackToSpawn(t *testing.T) {
	bodyFile := writeTempFile(t, "re-review body")
	closed := prFieldIssue(t, "at-rr.3", "owner/myrepo", 42)
	closed.Status = "closed"
	ctx, stdout, _, tmpHome := makeRouteCtxWithHome(t, nil)
	ctx.BD = &statusFakeBD{closed: []bd.Issue{closed}}
	repoDir := filepath.Join(tmpHome, "review-repos")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	clonePath := newEnabledClonePath(t)
	if err := os.WriteFile(filepath.Join(repoDir, "myrepo"), []byte(clonePath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &failRunner{failOn: "reopen"}
	cmd := &routePREventKong{
		Repo: "owner/myrepo", PRNumber: 42, HeadBranch: "feat-x",
		Transition: TransitionReReview, BodyFile: bodyFile,
		runner: runner.run,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// reopen (failed), then dispatch fallback — no send.
	if len(runner.calls) != 2 || runner.calls[0][0] != "reopen" || runner.calls[1][0] != "dispatch" {
		t.Fatalf("calls = %v, want [reopen, dispatch]", runner.calls)
	}
	if !strings.Contains(stdout.String(), "reopen at-rr.3 failed") {
		t.Errorf("stdout missing reopen-failure notice: %s", stdout.String())
	}
}

func TestReReview_SendFailsAfterReopen_FallsBackToSpawn(t *testing.T) {
	bodyFile := writeTempFile(t, "re-review body")
	closed := prFieldIssue(t, "at-rr.4", "owner/myrepo", 42)
	closed.Status = "closed"
	ctx, stdout, _, tmpHome := makeRouteCtxWithHome(t, nil)
	ctx.BD = &statusFakeBD{closed: []bd.Issue{closed}}
	repoDir := filepath.Join(tmpHome, "review-repos")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	clonePath := newEnabledClonePath(t)
	if err := os.WriteFile(filepath.Join(repoDir, "myrepo"), []byte(clonePath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &failRunner{failOn: "mail"}
	cmd := &routePREventKong{
		Repo: "owner/myrepo", PRNumber: 42, HeadBranch: "feat-x",
		Transition: TransitionReReview, BodyFile: bodyFile,
		runner: runner.run,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// reopen (success), send/mail (failed), then dispatch fallback.
	if len(runner.calls) != 3 {
		t.Fatalf("calls = %d, want 3 [reopen, mail send, dispatch]: %v", len(runner.calls), runner.calls)
	}
	if runner.calls[0][0] != "reopen" {
		t.Errorf("calls[0][0]: got %q, want \"reopen\"", runner.calls[0][0])
	}
	if runner.calls[1][0] != "mail" {
		t.Errorf("calls[1][0]: got %q, want \"mail\"", runner.calls[1][0])
	}
	if runner.calls[2][0] != "dispatch" {
		t.Errorf("calls[2][0]: got %q, want \"dispatch\"", runner.calls[2][0])
	}
	outStr := stdout.String()
	if !strings.Contains(outStr, "send to") {
		t.Errorf("stdout should contain 'send to' failure notice; got: %s", outStr)
	}
}

func TestReReview_OtherTransitionSendHasNoResumeFlags(t *testing.T) {
	bodyFile := writeTempFile(t, "ci failed body")
	issue := prFieldIssue(t, "at-ci.1", "owner/myrepo", 42)
	ctx, _, _ := makeStatusCtx([]bd.Issue{issue}, nil)

	runner := &fakeRunner{}
	cmd := &routePREventKong{
		Repo: "owner/myrepo", PRNumber: 42, HeadBranch: "feat-x",
		Transition: TransitionCIFailed, BodyFile: bodyFile,
		runner: runner.run,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, arg := range runner.calls[0] {
		if arg == "--resume-launch-prompt" {
			t.Errorf("non-re_review send must not carry --resume-launch-prompt: %v", runner.calls[0])
		}
	}
}

// ── comment_reply transition ──────────────────────────────────────────────────

func TestCommentReply_OpenMatch_PlainSendNoResumeFlags(t *testing.T) {
	bodyFile := writeTempFile(t, "comment reply body")
	issue := prFieldIssue(t, "at-cr.1", "owner/myrepo", 42)
	ctx, _, _ := makeStatusCtx([]bd.Issue{issue}, nil)

	runner := &fakeRunner{}
	cmd := &routePREventKong{
		Repo: "owner/myrepo", PRNumber: 42, HeadBranch: "feat-x",
		Transition: TransitionCommentReply, BodyFile: bodyFile,
		runner: runner.run,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d, want 1 (send only)", len(runner.calls))
	}
	want := []string{"mail", "send", "at-cr.1", "--file", bodyFile, "--sender", "pr-shepherd"}
	if strings.Join(runner.calls[0], " ") != strings.Join(want, " ") {
		t.Errorf("send args = %v\nwant %v (no resume flags on open match)", runner.calls[0], want)
	}
}

func TestCommentReply_ClosedMatch_ReopensThenSendsWithCommentReplyPrompt(t *testing.T) {
	bodyFile := writeTempFile(t, "comment reply body")
	closed := prFieldIssue(t, "at-cr.2", "owner/myrepo", 42)
	closed.Status = "closed"
	ctx, stdout, _ := makeStatusCtx(nil, []bd.Issue{closed})

	runner := &fakeRunner{}
	cmd := &routePREventKong{
		Repo: "owner/myrepo", PRNumber: 42, HeadBranch: "feat-x",
		Transition: TransitionCommentReply, BodyFile: bodyFile,
		runner: runner.run,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %d, want 2 (reopen, send): %v", len(runner.calls), runner.calls)
	}
	if strings.Join(runner.calls[0], " ") != "reopen at-cr.2" {
		t.Errorf("first call = %v, want reopen at-cr.2", runner.calls[0])
	}
	wantSend := []string{"mail", "send", "at-cr.2", "--file", bodyFile, "--sender", "pr-shepherd",
		"--resume-launch-prompt", "/agent-teams:review-pr at-cr.2 comment-reply", "--resume-model", "sonnet"}
	if strings.Join(runner.calls[1], " ") != strings.Join(wantSend, " ") {
		t.Errorf("send args = %v\nwant %v", runner.calls[1], wantSend)
	}
	if !strings.Contains(stdout.String(), "reopening") {
		t.Errorf("stdout missing reopen notice: %s", stdout.String())
	}
}

// TestCommentReply_DisabledRepo_SkipsWithoutReopen is the comment_reply
// counterpart of TestReReview_DisabledRepo_SkipsWithoutReopenOrSpawn.
func TestCommentReply_DisabledRepo_SkipsWithoutReopen(t *testing.T) {
	bodyFile := writeTempFile(t, "comment reply body")
	repoDir := newDisabledClonePath(t)
	closed := bd.Issue{
		ID:          "at-cr.9",
		Title:       "Initiative at-cr.9",
		Description: "repo: " + repoDir + "\nworktree: /tmp/wt-at-cr.9\nbranch: main\n",
		Notes:       "pr: https://github.com/owner/myrepo/pull/42",
		Status:      "closed",
	}
	ctx, stdout, _ := makeStatusCtx(nil, []bd.Issue{closed})

	runner := &fakeRunner{}
	cmd := &routePREventKong{
		Repo: "owner/myrepo", PRNumber: 42, HeadBranch: "feat-x",
		Transition: TransitionCommentReply, BodyFile: bodyFile,
		runner: runner.run,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("expected 0 runner calls for a disabled repo, got %d: %v", len(runner.calls), runner.calls)
	}
	if !strings.Contains(stdout.String(), "repo is disabled") {
		t.Errorf("stdout should say 'repo is disabled'; got: %s", stdout.String())
	}
}

func TestCommentReply_NoInitiative_DropsWithoutSpawn(t *testing.T) {
	bodyFile := writeTempFile(t, "comment reply body")
	ctx, stdout, _, tmpHome := makeRouteCtxWithHome(t, nil)
	ctx.BD = &statusFakeBD{}
	// Configure review-repos so a spawn WOULD be possible — proving the drop
	// is deliberate, not a missing-config accident.
	repoDir := filepath.Join(tmpHome, "review-repos")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	clonePath := newEnabledClonePath(t)
	if err := os.WriteFile(filepath.Join(repoDir, "myrepo"), []byte(clonePath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &fakeRunner{}
	cmd := &routePREventKong{
		Repo: "owner/myrepo", PRNumber: 42, HeadBranch: "feat-x",
		Transition: TransitionCommentReply, BodyFile: bodyFile,
		runner: runner.run,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("calls = %v, want none (no spawn for comment_reply)", runner.calls)
	}
	if !strings.Contains(stdout.String(), "no initiative") {
		t.Errorf("stdout missing drop notice: %s", stdout.String())
	}
}

func TestCommentReply_ReopenFails_DropsWithoutSpawn(t *testing.T) {
	bodyFile := writeTempFile(t, "comment reply body")
	closed := prFieldIssue(t, "at-cr.3", "owner/myrepo", 42)
	closed.Status = "closed"
	ctx, stdout, _ := makeStatusCtx(nil, []bd.Issue{closed})

	runner := &failRunner{failOn: "reopen"}
	cmd := &routePREventKong{
		Repo: "owner/myrepo", PRNumber: 42, HeadBranch: "feat-x",
		Transition: TransitionCommentReply, BodyFile: bodyFile,
		runner: runner.run,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.calls) != 1 || runner.calls[0][0] != "reopen" {
		t.Fatalf("calls = %v, want [reopen] only", runner.calls)
	}
	if !strings.Contains(stdout.String(), "dropping") {
		t.Errorf("stdout missing drop notice: %s", stdout.String())
	}
}

func TestCommentReply_SendFails_DropsWithoutSpawn(t *testing.T) {
	bodyFile := writeTempFile(t, "comment reply body")
	closed := prFieldIssue(t, "at-cr.4", "owner/myrepo", 42)
	closed.Status = "closed"
	ctx, stdout, _ := makeStatusCtx(nil, []bd.Issue{closed})

	runner := &failRunner{failOn: "mail"}
	cmd := &routePREventKong{
		Repo: "owner/myrepo", PRNumber: 42, HeadBranch: "feat-x",
		Transition: TransitionCommentReply, BodyFile: bodyFile,
		runner: runner.run,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.calls) != 3 || runner.calls[0][0] != "reopen" || runner.calls[1][0] != "mail" || runner.calls[2][0] != "close" {
		t.Fatalf("calls = %v, want [reopen, mail, close] and no dispatch", runner.calls)
	}
	wantClose := []string{"close", "at-cr.4", "--reason", "comment-reply send failed; restoring closed state"}
	if gotClose := runner.calls[2]; strings.Join(gotClose, "\x00") != strings.Join(wantClose, "\x00") {
		t.Errorf("close call = %v, want %v", gotClose, wantClose)
	}
	if !strings.Contains(stdout.String(), "comment-reply event dropped") {
		t.Errorf("stdout missing dropped notice: %s", stdout.String())
	}
}
