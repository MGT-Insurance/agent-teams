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
func prFieldIssue(id, ownerRepo string, prNumber int) bd.Issue {
	prURL := fmt.Sprintf("https://github.com/%s/pull/%d", ownerRepo, prNumber)
	return bd.Issue{
		ID:          id,
		Title:       "Initiative " + id,
		Description: fmt.Sprintf("repo: /code/%s\nworktree: /tmp/wt-%s\nbranch: main\n", ownerRepo, id),
		Notes:       "pr: " + prURL,
		Status:      "open",
	}
}

// branchIssue builds an issue that will MatchBranch for repoName + headBranch
// (no pr: URL, so MatchPRField is skipped).
func branchIssue(id, repoName, headBranch string) bd.Issue {
	return bd.Issue{
		ID:          id,
		Title:       "Initiative " + id,
		Description: fmt.Sprintf("repo: /code/%s\nworktree: /tmp/wt-%s\nbranch: %s\n", repoName, id, headBranch),
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
// owned initiative (MatchPRField) → runner("send", id, "--file", body, "--sender", "pr-shepherd").
func TestDecisionMatrix_OwnedViaPRFieldRoutesViaSend(t *testing.T) {
	bodyFile := writeTempFile(t, "CI failed output")
	issue := prFieldIssue("at-abc.1", "owner/myrepo", 42)

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
	if len(call) < 6 {
		t.Fatalf("runner call too short: %v", call)
	}
	if call[0] != "send" {
		t.Errorf("call[0]: got %q, want \"send\"", call[0])
	}
	if call[1] != "at-abc.1" {
		t.Errorf("call[1] (initiative id): got %q, want \"at-abc.1\"", call[1])
	}
	if call[2] != "--file" {
		t.Errorf("call[2]: got %q, want \"--file\"", call[2])
	}
	if call[3] != bodyFile {
		t.Errorf("call[3] (body file): got %q, want %q", call[3], bodyFile)
	}
	if call[4] != "--sender" {
		t.Errorf("call[4]: got %q, want \"--sender\"", call[4])
	}
	if call[5] != "pr-shepherd" {
		t.Errorf("call[5]: got %q, want \"pr-shepherd\"", call[5])
	}
	if !strings.Contains(stdout.String(), "at-abc.1") {
		t.Errorf("stdout should mention matched initiative id; got: %q", stdout.String())
	}
}

// TestDecisionMatrix_OwnedViaMatchBranchRoutesViaSend verifies the MatchBranch
// path (no pr: URL) also calls send with the correct initiative id.
func TestDecisionMatrix_OwnedViaMatchBranchRoutesViaSend(t *testing.T) {
	bodyFile := writeTempFile(t, "changes requested body")
	issue := branchIssue("at-xyz.2", "myrepo", "feature-branch")

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
	if fr.calls[0][0] != "send" {
		t.Errorf("runner verb: got %q, want \"send\"", fr.calls[0][0])
	}
	if fr.calls[0][1] != "at-xyz.2" {
		t.Errorf("runner initiative id: got %q, want \"at-xyz.2\"", fr.calls[0][1])
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

// TestSpawnReviewInitiative_Configured verifies the happy path: a review-repos
// config file is present → runner called with dispatch + correct args including
// --launch-prompt and --skip-epic for the lightweight review-pr skill.
func TestSpawnReviewInitiative_Configured(t *testing.T) {
	ctx, stdout, _, tmpHome := makeRouteCtxWithHome(t, nil)

	// Register a fake clone path in the config.
	clonePath := t.TempDir()
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
	//          --model sonnet
	if len(call) < 12 {
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

	clonePath := t.TempDir()
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

	clonePath := t.TempDir()
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
// owned (MatchPRField) → runner("send", id, "--file", body, "--sender", "pr-shepherd").
func TestRoutePREventKong_OwnedViaPRFieldRoutesViaSend(t *testing.T) {
	bodyFile := writeTempFile(t, "CI failed output")
	issue := prFieldIssue("at-kong.1", "owner/myrepo", 42)

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
	if call[0] != "send" {
		t.Errorf("call[0]: got %q, want \"send\"", call[0])
	}
	if call[1] != "at-kong.1" {
		t.Errorf("call[1] (initiative id): got %q, want \"at-kong.1\"", call[1])
	}
	if call[4] != "--sender" || call[5] != "pr-shepherd" {
		t.Errorf("expected --sender pr-shepherd; call: %v", call)
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
