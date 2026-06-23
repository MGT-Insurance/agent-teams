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

// ── parseTransition ───────────────────────────────────────────────────────────

func TestParseTransition_ValidValues(t *testing.T) {
	cases := []struct {
		in   string
		want PRTransition
	}{
		{"ci_failed", TransitionCIFailed},
		{"changes_requested", TransitionChangesRequested},
		{"review_requested", TransitionReviewRequested},
		{"bot_findings", TransitionBotFindings},
		{"approved", TransitionApproved},
		{"merged", TransitionMerged},
		{"stale", TransitionStale},
		{"other", TransitionOther},
	}
	for _, tc := range cases {
		got, err := parseTransition(tc.in)
		if err != nil {
			t.Errorf("parseTransition(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseTransition(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseTransition_Unknown(t *testing.T) {
	_, err := parseTransition("unknown_transition")
	if err == nil {
		t.Error("expected error for unknown transition, got nil")
	}
}

// ── parseRoutePREventFlags ────────────────────────────────────────────────────

func TestParseRoutePREventFlags_AllRequired(t *testing.T) {
	bodyFile := writeTempFile(t, "test body")
	repo, prNum, head, transition, body, prURL, err := parseRoutePREventFlags([]string{
		"--repo", "owner/repo",
		"--pr-number", "42",
		"--head-branch", "feat-x",
		"--transition", "ci_failed",
		"--body-file", bodyFile,
		"--pr-url", "https://github.com/owner/repo/pull/42",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo != "owner/repo" {
		t.Errorf("repo: got %q", repo)
	}
	if prNum != 42 {
		t.Errorf("prNum: got %d", prNum)
	}
	if head != "feat-x" {
		t.Errorf("headBranch: got %q", head)
	}
	if transition != TransitionCIFailed {
		t.Errorf("transition: got %q", transition)
	}
	if body != bodyFile {
		t.Errorf("bodyFile: got %q", body)
	}
	if prURL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("prURL: got %q", prURL)
	}
}

func TestParseRoutePREventFlags_EqForm(t *testing.T) {
	bodyFile := writeTempFile(t, "body")
	_, prNum, _, _, _, _, err := parseRoutePREventFlags([]string{
		"--repo=owner/repo", "--pr-number=7", "--head-branch=main",
		"--transition=approved", "--body-file=" + bodyFile,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prNum != 7 {
		t.Errorf("prNum (eq-form): got %d", prNum)
	}
}

func TestParseRoutePREventFlags_MissingRepo(t *testing.T) {
	bodyFile := writeTempFile(t, "body")
	_, _, _, _, _, _, err := parseRoutePREventFlags([]string{
		"--pr-number", "1", "--head-branch", "br",
		"--transition", "ci_failed", "--body-file", bodyFile,
	})
	if err == nil {
		t.Error("expected error for missing --repo")
	}
}

func TestParseRoutePREventFlags_MissingPRNumber(t *testing.T) {
	bodyFile := writeTempFile(t, "body")
	_, _, _, _, _, _, err := parseRoutePREventFlags([]string{
		"--repo", "o/r", "--head-branch", "br",
		"--transition", "ci_failed", "--body-file", bodyFile,
	})
	if err == nil {
		t.Error("expected error for missing --pr-number")
	}
}

func TestParseRoutePREventFlags_BadPRNumber(t *testing.T) {
	bodyFile := writeTempFile(t, "body")
	_, _, _, _, _, _, err := parseRoutePREventFlags([]string{
		"--repo", "o/r", "--pr-number", "abc", "--head-branch", "br",
		"--transition", "ci_failed", "--body-file", bodyFile,
	})
	if err == nil {
		t.Error("expected error for non-integer --pr-number")
	}
}

func TestParseRoutePREventFlags_ZeroPRNumber(t *testing.T) {
	bodyFile := writeTempFile(t, "body")
	_, _, _, _, _, _, err := parseRoutePREventFlags([]string{
		"--repo", "o/r", "--pr-number", "0", "--head-branch", "br",
		"--transition", "ci_failed", "--body-file", bodyFile,
	})
	if err == nil {
		t.Error("expected error for --pr-number=0 (must be positive)")
	}
}

func TestParseRoutePREventFlags_UnknownFlag(t *testing.T) {
	bodyFile := writeTempFile(t, "body")
	_, _, _, _, _, _, err := parseRoutePREventFlags([]string{
		"--repo", "o/r", "--pr-number", "1", "--head-branch", "br",
		"--transition", "ci_failed", "--body-file", bodyFile, "--unknown",
	})
	if err == nil {
		t.Error("expected error for unknown flag")
	}
}

func TestParseRoutePREventFlags_BadTransition(t *testing.T) {
	bodyFile := writeTempFile(t, "body")
	_, _, _, _, _, _, err := parseRoutePREventFlags([]string{
		"--repo", "o/r", "--pr-number", "1", "--head-branch", "br",
		"--transition", "not_a_thing", "--body-file", bodyFile,
	})
	if err == nil {
		t.Error("expected error for unknown --transition value")
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
	cmd := &routePREventCommand{runner: fr.run}

	err := cmd.Run(ctx, []string{
		"--repo", "owner/myrepo",
		"--pr-number", "42",
		"--head-branch", "feat-x",
		"--transition", "ci_failed",
		"--body-file", bodyFile,
	})
	if err != nil {
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
	cmd := &routePREventCommand{runner: fr.run}

	err := cmd.Run(ctx, []string{
		"--repo", "owner/myrepo",
		"--pr-number", "99",
		"--head-branch", "feature-branch",
		"--transition", "changes_requested",
		"--body-file", bodyFile,
	})
	if err != nil {
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
	cmd := &routePREventCommand{runner: fr.run}

	err := cmd.Run(ctx, []string{
		"--repo", "owner/repo",
		"--pr-number", "7",
		"--head-branch", "some-branch",
		"--transition", "review_requested",
		"--body-file", bodyFile,
	})
	if err != nil {
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
// config file is present → runner called with dispatch + correct args, body-file
// contains the required review instructions.
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
	cmd := &routePREventCommand{runner: fr.run}

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

	// Verify argv structure: dispatch --repo <clone> --problem <title> --body-file <path>
	if len(call) < 7 {
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
	// Body file path comes from the runner args (temp file is cleaned up after run,
	// but we capture the path from the call before it's removed).
	bodyFilePath := call[6]
	// The temp file is removed after runner returns; we only have the path recorded.
	// Since fakeRunner records args synchronously before returning, we can't read
	// the file after the fact — instead capture content via a custom runner.
	_ = bodyFilePath

	// Confirmation line must appear in stdout.
	out := stdout.String()
	if !strings.Contains(out, "spawned review initiative") {
		t.Errorf("stdout should confirm spawn; got: %q", out)
	}
}

// TestSpawnReviewInitiative_ConfiguredBodyContent verifies the review instructions
// body written to the temp file contains the required phrases.
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

	cmd := &routePREventCommand{runner: bodyCapturingRunner}
	event := PREvent{
		Repo:       "MGT-Insurance/midgard",
		PRNumber:   42,
		PRURL:      "https://github.com/MGT-Insurance/midgard/pull/42",
		Transition: TransitionReviewRequested,
	}

	if err := cmd.spawnReviewInitiative(ctx, event); err != nil {
		t.Fatalf("spawnReviewInitiative error: %v", err)
	}

	requiredPhrases := []string{
		"gh pr checkout 42",
		"gh pr diff 42",
		"NO nit comments",
		"INLINE comment",
		"gh api repos/MGT-Insurance/midgard/pulls/42/reviews",
		"single sentence",
		"must NOT open a new PR",
		"Do NOT open a PR",
		"https://github.com/MGT-Insurance/midgard/pull/42",
		"MGT-Insurance/midgard",
	}
	for _, phrase := range requiredPhrases {
		if !strings.Contains(capturedBody, phrase) {
			t.Errorf("review body missing %q; body:\n%s", phrase, capturedBody)
		}
	}
}

// TestDecisionMatrix_UnownedCIFailedSkips verifies LOG-AND-SKIP:
// unowned + ci_failed → logs "skipping", runner NOT called.
func TestDecisionMatrix_UnownedCIFailedSkips(t *testing.T) {
	bodyFile := writeTempFile(t, "ci output")
	ctx, stdout, _ := makeRouteCtx(nil) // no issues → MatchNone
	fr := &fakeRunner{}
	cmd := &routePREventCommand{runner: fr.run}

	err := cmd.Run(ctx, []string{
		"--repo", "owner/repo",
		"--pr-number", "3",
		"--head-branch", "fix-branch",
		"--transition", "ci_failed",
		"--body-file", bodyFile,
	})
	if err != nil {
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
	cmd := &routePREventCommand{runner: fr.run}

	err := cmd.Run(ctx, []string{
		"--repo", "owner/repo",
		"--pr-number", "5",
		"--head-branch", "br",
		"--transition", "approved",
		"--body-file", bodyFile,
	})
	if err != nil {
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
	cmd := &routePREventCommand{runner: (&fakeRunner{}).run}
	err := cmd.Run(nil, nil)
	if err == nil {
		t.Error("expected error for nil context, got nil")
	}
}

// TestDecisionMatrix_BadArgsMissingRepo verifies UsageError on bad args.
func TestDecisionMatrix_BadArgsMissingRepo(t *testing.T) {
	ctx, _, _ := makeRouteCtx(nil)
	cmd := &routePREventCommand{runner: (&fakeRunner{}).run}
	err := cmd.Run(ctx, []string{
		"--pr-number", "1", "--head-branch", "br",
		"--transition", "ci_failed", "--body-file", "/dev/null",
	})
	if err == nil {
		t.Fatal("expected error for missing --repo, got nil")
	}
}

// TestRegisterRouteEvent confirms the verb registers under "route-pr-event".
func TestRegisterRouteEvent(t *testing.T) {
	reg := make(cli.Registry)
	RegisterRouteEvent(reg)
	cmd, ok := reg.Lookup("route-pr-event")
	if !ok {
		t.Fatal("route-pr-event not registered")
	}
	if cmd.Name() != "route-pr-event" {
		t.Errorf("Name() = %q, want \"route-pr-event\"", cmd.Name())
	}
}

// TestRegisterRouteEvent_NoDuplicateWithFullRegistry confirms no collision
// when route-pr-event is added alongside all existing verbs.
func TestRegisterRouteEvent_NoDuplicateWithFullRegistry(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("registration panicked (duplicate): %v", r)
		}
	}()
	reg := make(cli.Registry)
	verbs_registerAll(reg)
}

// verbs_registerAll mirrors main.go's registration block plus the new verb.
func verbs_registerAll(reg cli.Registry) {
	RegisterQuery(reg)
	RegisterMatch(reg)
	RegisterWrite(reg, nil, nil)
	RegisterDispatch(reg)
	RegisterCost(reg)
	RegisterWorktreeSetup(reg)
	RegisterMessaging(reg)
	RegisterRouteEvent(reg)
}

