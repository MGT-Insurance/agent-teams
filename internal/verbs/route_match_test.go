// This file is owned by Track R (route-pr-event verbs).
package verbs

import (
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

// makeIssue builds a minimal bd.Issue for use in match-engine tests.
func makeIssue(id, description, notes string) bd.Issue {
	return bd.Issue{
		ID:          id,
		Title:       "Initiative " + id,
		Description: description,
		Notes:       notes,
		Status:      "open",
	}
}

// descLines formats key:value lines for an initiative description.
func descLines(repo, worktree, branch string) string {
	return "repo: " + repo + "\nworktree: " + worktree + "\nbranch: " + branch + "\n"
}

// The parseDescriptionFields tests that lived here are gone with the helper.
// Two of them asserted behaviour the frozen contract now forbids — key case
// folding and a bare first-colon split — so they could not be carried over
// even in spirit; the rule they are replaced by is initiative.Of's, tested in
// internal/initiative. The tier-2 branch matching that consumed those fields
// is still tested below.

// ── extractPrURL ──────────────────────────────────────────────────────────────

func TestExtractPrURL_FoundInLine(t *testing.T) {
	text := "DELIVERED — awaiting-merge. PR #3551: https://github.com/MGT-Insurance/midgard/pull/3551"
	got := extractPrURL(text)
	if got != "https://github.com/MGT-Insurance/midgard/pull/3551" {
		t.Errorf("extractPrURL: got %q", got)
	}
}

func TestExtractPrURL_NotPresent(t *testing.T) {
	if got := extractPrURL("no link here"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExtractPrURL_EmptyString(t *testing.T) {
	if got := extractPrURL(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExtractPrURL_MultiLine(t *testing.T) {
	text := "session 1\nsome context\nhttps://github.com/org/repo/pull/42\nmore text"
	if got := extractPrURL(text); got != "https://github.com/org/repo/pull/42" {
		t.Errorf("extractPrURL multiline: got %q", got)
	}
}

// ── parsePrURL ────────────────────────────────────────────────────────────────

func TestParsePrURL_Valid(t *testing.T) {
	ownerRepo, num, ok := parsePrURL("https://github.com/MGT-Insurance/midgard/pull/3551")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if ownerRepo != "mgt-insurance/midgard" {
		t.Errorf("ownerRepo: got %q", ownerRepo)
	}
	if num != 3551 {
		t.Errorf("prNumber: got %d", num)
	}
}

func TestParsePrURL_CaseNormalized(t *testing.T) {
	ownerRepo, _, ok := parsePrURL("https://github.com/Owner/Repo/pull/1")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if ownerRepo != "owner/repo" {
		t.Errorf("ownerRepo not lowercased: got %q", ownerRepo)
	}
}

func TestParsePrURL_Invalid(t *testing.T) {
	_, _, ok := parsePrURL("not-a-url")
	if ok {
		t.Error("expected ok=false for non-url")
	}
}

func TestParsePrURL_MissingNumber(t *testing.T) {
	_, _, ok := parsePrURL("https://github.com/owner/repo/pulls/")
	if ok {
		t.Error("expected ok=false for malformed url")
	}
}

// ── matchInitiativeFromIssues — MatchPRField ──────────────────────────────────

func TestMatchInitiative_PRFieldInNotes(t *testing.T) {
	issues := []bd.Issue{
		makeIssue("at-aaa",
			descLines("/Users/eric/Code/myapp", "/wt/at-aaa", "feat-x"),
			"session 1 — DELIVERED https://github.com/owner/myapp/pull/42"),
	}
	event := PREvent{Repo: "owner/myapp", PRNumber: 42}
	result, err := matchInitiativeFromIssues(issues, event, "feat-x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchPRField {
		t.Errorf("How: got %v, want MatchPRField", result.How)
	}
	if result.InitiativeID != "at-aaa" {
		t.Errorf("InitiativeID: got %q", result.InitiativeID)
	}
	if result.Worktree != "/wt/at-aaa" {
		t.Errorf("Worktree: got %q", result.Worktree)
	}
}

func TestMatchInitiative_PRFieldInDescription(t *testing.T) {
	// pr: in description (no notes).
	desc := descLines("/Users/eric/Code/repo", "/wt/at-bbb", "main") +
		"pr: https://github.com/owner/repo/pull/99\n"
	issues := []bd.Issue{
		makeIssue("at-bbb", desc, ""),
	}
	event := PREvent{Repo: "owner/repo", PRNumber: 99}
	result, err := matchInitiativeFromIssues(issues, event, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchPRField {
		t.Errorf("How: got %v, want MatchPRField", result.How)
	}
	if result.InitiativeID != "at-bbb" {
		t.Errorf("InitiativeID: got %q", result.InitiativeID)
	}
}

func TestMatchInitiative_NotesCheckedBeforeDescription(t *testing.T) {
	// No "pr:" rail line on this issue — the Description PR reference is
	// prose, not a "pr: <url>" field line, so initiative.ResolvedPRs' rail
	// stays empty and it falls back to the free-text scan, Notes checked
	// before Description (docs/multi-pr-contract.md, "read precedence"). Had
	// this been a real rail entry, the rail would win wholesale over Notes
	// regardless of which PR the event names — see
	// TestMatchInitiative_PRFieldInDescription for that (single-PR) rail case.
	desc := descLines("/repo", "/wt/at-ccc", "br") +
		"delivered as https://github.com/owner/desc-repo/pull/20\n"
	issues := []bd.Issue{
		makeIssue("at-ccc", desc, "pr delivered: https://github.com/owner/notes-repo/pull/10"),
	}
	event := PREvent{Repo: "owner/notes-repo", PRNumber: 10}
	result, err := matchInitiativeFromIssues(issues, event, "br")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchPRField {
		t.Errorf("How: got %v, want MatchPRField", result.How)
	}
	if result.InitiativeID != "at-ccc" {
		t.Errorf("InitiativeID: got %q", result.InitiativeID)
	}
}

func TestMatchInitiative_PRFieldCaseInsensitive(t *testing.T) {
	// event.Repo uses different case than the PR URL stored in notes.
	issues := []bd.Issue{
		makeIssue("at-ci",
			descLines("/repo", "/wt/at-ci", "main"),
			"https://github.com/MGT-Insurance/Midgard/pull/3551"),
	}
	event := PREvent{Repo: "mgt-insurance/midgard", PRNumber: 3551}
	result, err := matchInitiativeFromIssues(issues, event, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchPRField {
		t.Errorf("How: got %v, want MatchPRField", result.How)
	}
}

func TestMatchInitiative_PRNumberMustMatch(t *testing.T) {
	// Correct repo but wrong PR number → should not MatchPRField.
	// Use a branch that does NOT match headBranch so tier-2 also misses.
	issues := []bd.Issue{
		makeIssue("at-num",
			descLines("/repo", "/wt/at-num", "stored-branch"),
			"https://github.com/owner/repo/pull/1"),
	}
	event := PREvent{Repo: "owner/repo", PRNumber: 999}
	// headBranch differs from the issue's branch: → no tier-2 match either.
	result, err := matchInitiativeFromIssues(issues, event, "different-branch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchNone {
		t.Errorf("How: got %v, want MatchNone (wrong PR number, branch mismatch)", result.How)
	}
}

// ── matchInitiativeFromIssues — multi-PR rail (agent-teams-ssib.7) ───────────
//
// The bug these guard: reading only the FIRST resolved PR (the pre-fix
// extractPrURL(Notes)-then-extractPrURL(Description) single-URL scan) makes a
// SECOND or THIRD PR opened on the same initiative invisible to tier-1
// matching. Each issue below deliberately mismatches tier-2 (different repo
// basename, or a "branch:" field the test's headBranch does not equal) so a
// tier-1 miss cannot be masked by an accidental branch-fallback match —
// mutating matchInitiativeFromIssues back to first-match-only must turn these
// red, not leave them passing for the wrong reason.

// railIssue builds an issue whose "pr" rail holds prURLs in registration
// order (Description "pr: <url>" lines, the ateam-pr-add write path), with no
// pr: mention in Notes at all.
func railIssue(id string, prURLs []string, repo, worktree, branch string) bd.Issue {
	desc := descLines(repo, worktree, branch)
	for _, u := range prURLs {
		desc += "pr: " + u + "\n"
	}
	return makeIssue(id, desc, "")
}

func TestMatchInitiative_SecondRailEntryMatches(t *testing.T) {
	prURLs := []string{
		"https://github.com/owner/repo/pull/1",
		"https://github.com/owner/second-repo/pull/2",
	}
	issues := []bd.Issue{railIssue("at-multi", prURLs, "/code/repo", "/wt/at-multi", "main")}
	event := PREvent{Repo: "owner/second-repo", PRNumber: 2}
	result, err := matchInitiativeFromIssues(issues, event, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchPRField {
		t.Fatalf("How: got %v, want MatchPRField (second rail entry must match)", result.How)
	}
	if result.InitiativeID != "at-multi" {
		t.Errorf("InitiativeID: got %q", result.InitiativeID)
	}
}

func TestMatchInitiative_ThirdRailEntryMatches(t *testing.T) {
	prURLs := []string{
		"https://github.com/owner/repo/pull/1",
		"https://github.com/owner/repo2/pull/2",
		"https://github.com/owner/third-repo/pull/3",
	}
	issues := []bd.Issue{railIssue("at-multi3", prURLs, "/code/repo", "/wt/at-multi3", "main")}
	event := PREvent{Repo: "owner/third-repo", PRNumber: 3}
	result, err := matchInitiativeFromIssues(issues, event, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchPRField {
		t.Fatalf("How: got %v, want MatchPRField (third rail entry must match)", result.How)
	}
	if result.InitiativeID != "at-multi3" {
		t.Errorf("InitiativeID: got %q", result.InitiativeID)
	}
}

func TestMatchInitiative_RailOnlyPRStillRoutes(t *testing.T) {
	// No Notes at all — the PR is recorded ONLY via the rail (ateam pr add).
	// initiative.ResolvedPRs must still surface it to tier-1 matching.
	issues := []bd.Issue{railIssue("at-rail-only", []string{"https://github.com/owner/repo/pull/5"}, "/code/repo", "/wt/at-rail-only", "main")}
	event := PREvent{Repo: "owner/repo", PRNumber: 5}
	result, err := matchInitiativeFromIssues(issues, event, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchPRField {
		t.Fatalf("How: got %v, want MatchPRField", result.How)
	}
}

// ── matchInitiativeFromIssues — MatchBranch ───────────────────────────────────

func TestMatchInitiative_BranchFallback(t *testing.T) {
	// No pr: line anywhere; match by repo basename + branch.
	issues := []bd.Issue{
		makeIssue("at-br",
			descLines("/Users/eric/Code/myapp", "/wt/at-br", "feat-cool"),
			""),
	}
	event := PREvent{Repo: "owner/myapp", PRNumber: 5}
	result, err := matchInitiativeFromIssues(issues, event, "feat-cool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchBranch {
		t.Errorf("How: got %v, want MatchBranch", result.How)
	}
	if result.InitiativeID != "at-br" {
		t.Errorf("InitiativeID: got %q", result.InitiativeID)
	}
}

func TestMatchInitiative_BranchFallback_RepoBasename(t *testing.T) {
	// repo: field has a full path; only the basename is compared.
	issues := []bd.Issue{
		makeIssue("at-base",
			"repo: /deep/path/to/agent-teams\nworktree: /wt/at-base\nbranch: my-feature\n",
			""),
	}
	event := PREvent{Repo: "erlloyd/agent-teams", PRNumber: 1}
	result, err := matchInitiativeFromIssues(issues, event, "my-feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchBranch {
		t.Errorf("How: got %v, want MatchBranch", result.How)
	}
}

func TestMatchInitiative_BranchFallback_DifferentRepoName(t *testing.T) {
	// Same branch name but different repo basename → must NOT match.
	issues := []bd.Issue{
		makeIssue("at-diff-repo",
			descLines("/Users/eric/Code/other-repo", "/wt/at-diff-repo", "feat-cool"),
			""),
	}
	event := PREvent{Repo: "owner/myapp", PRNumber: 5}
	result, err := matchInitiativeFromIssues(issues, event, "feat-cool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchNone {
		t.Errorf("How: got %v, want MatchNone (repo name differs)", result.How)
	}
}

func TestMatchInitiative_BranchFallback_DifferentBranch(t *testing.T) {
	// Matching repo, but different branch → no match.
	issues := []bd.Issue{
		makeIssue("at-diff-br",
			descLines("/code/myapp", "/wt/at-diff-br", "wrong-branch"),
			""),
	}
	event := PREvent{Repo: "owner/myapp", PRNumber: 5}
	result, err := matchInitiativeFromIssues(issues, event, "feat-cool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchNone {
		t.Errorf("How: got %v, want MatchNone (branch differs)", result.How)
	}
}

func TestMatchInitiative_BranchFallback_EmptyHeadBranch(t *testing.T) {
	// headBranch is "" → tier-2 skipped entirely even if repo name matches.
	issues := []bd.Issue{
		makeIssue("at-nobr",
			descLines("/code/myapp", "/wt/at-nobr", ""),
			""),
	}
	event := PREvent{Repo: "owner/myapp", PRNumber: 5}
	result, err := matchInitiativeFromIssues(issues, event, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchNone {
		t.Errorf("How: got %v, want MatchNone (empty headBranch skips tier-2)", result.How)
	}
}

// ── matchInitiativeFromIssues — MatchNone ─────────────────────────────────────

func TestMatchInitiative_NoMatch(t *testing.T) {
	issues := []bd.Issue{
		makeIssue("at-zzz",
			descLines("/code/other", "/wt/at-zzz", "unrelated"),
			""),
	}
	event := PREvent{Repo: "owner/myapp", PRNumber: 1}
	result, err := matchInitiativeFromIssues(issues, event, "some-branch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchNone {
		t.Errorf("How: got %v, want MatchNone", result.How)
	}
	if result.InitiativeID != "" {
		t.Errorf("InitiativeID: expected empty, got %q", result.InitiativeID)
	}
}

func TestMatchInitiative_EmptyIssueList(t *testing.T) {
	event := PREvent{Repo: "owner/repo", PRNumber: 1}
	result, err := matchInitiativeFromIssues(nil, event, "br")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchNone {
		t.Errorf("How: got %v, want MatchNone", result.How)
	}
}

// ── matchInitiativeFromIssues — ambiguous → error ─────────────────────────────

func TestMatchInitiative_AmbiguousPRField(t *testing.T) {
	prURL := "https://github.com/owner/repo/pull/42"
	issues := []bd.Issue{
		makeIssue("at-a1", descLines("/r", "/wt/a1", "br"), "delivered: "+prURL),
		makeIssue("at-a2", descLines("/r", "/wt/a2", "br"), "also: "+prURL),
	}
	event := PREvent{Repo: "owner/repo", PRNumber: 42}
	_, err := matchInitiativeFromIssues(issues, event, "br")
	if err == nil {
		t.Fatal("expected error for ambiguous MatchPRField, got nil")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error message missing 'ambiguous': %v", err)
	}
}

func TestMatchInitiative_AmbiguousBranch(t *testing.T) {
	issues := []bd.Issue{
		makeIssue("at-b1", descLines("/code/myapp", "/wt/b1", "feat"), ""),
		makeIssue("at-b2", descLines("/code/myapp", "/wt/b2", "feat"), ""),
	}
	event := PREvent{Repo: "owner/myapp", PRNumber: 5}
	_, err := matchInitiativeFromIssues(issues, event, "feat")
	if err == nil {
		t.Fatal("expected error for ambiguous MatchBranch, got nil")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error message missing 'ambiguous': %v", err)
	}
}

// ── precedence: MatchPRField wins over MatchBranch ────────────────────────────

func TestMatchInitiative_PRFieldWinsOverBranch(t *testing.T) {
	// at-pr1: matched by PR field.
	// at-br1: matched by branch (same repo name + branch).
	// PR field must win; no ambiguity error.
	prURL := "https://github.com/owner/myapp/pull/7"
	issues := []bd.Issue{
		makeIssue("at-pr1", descLines("/code/myapp", "/wt/pr1", "feat"), "delivered: "+prURL),
		makeIssue("at-br1", descLines("/code/myapp", "/wt/br1", "feat"), ""),
	}
	event := PREvent{Repo: "owner/myapp", PRNumber: 7}
	result, err := matchInitiativeFromIssues(issues, event, "feat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchPRField {
		t.Errorf("How: got %v, want MatchPRField (pr-field must win over branch)", result.How)
	}
	if result.InitiativeID != "at-pr1" {
		t.Errorf("InitiativeID: got %q, want at-pr1", result.InitiativeID)
	}
}

// ── malformed / missing fields ────────────────────────────────────────────────

func TestMatchInitiative_MalformedDescription(t *testing.T) {
	// No recognisable key:value lines, no pr URL → MatchNone, no panic.
	issues := []bd.Issue{
		makeIssue("at-mal", "this is just random text with no structure", ""),
	}
	event := PREvent{Repo: "owner/repo", PRNumber: 1}
	result, err := matchInitiativeFromIssues(issues, event, "br")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchNone {
		t.Errorf("How: got %v, want MatchNone for malformed description", result.How)
	}
}

func TestMatchInitiative_EmptyNotesAndDescription(t *testing.T) {
	issues := []bd.Issue{
		makeIssue("at-empty", "", ""),
	}
	event := PREvent{Repo: "owner/repo", PRNumber: 1}
	result, err := matchInitiativeFromIssues(issues, event, "br")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.How != MatchNone {
		t.Errorf("How: got %v, want MatchNone for empty issue", result.How)
	}
}

func TestMatchInitiative_WorktreeExtracted(t *testing.T) {
	// Verify the Worktree field is populated from the description's worktree: line.
	issues := []bd.Issue{
		makeIssue("at-wt",
			descLines("/code/myapp", "/Users/eric/.agent-teams-worktrees/wt-one", "main"),
			"https://github.com/owner/myapp/pull/1"),
	}
	event := PREvent{Repo: "owner/myapp", PRNumber: 1}
	result, err := matchInitiativeFromIssues(issues, event, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Worktree != "/Users/eric/.agent-teams-worktrees/wt-one" {
		t.Errorf("Worktree: got %q", result.Worktree)
	}
}

// ── MatchNone zero-value contract ─────────────────────────────────────────────

func TestMatchResult_NoneZeroValue(t *testing.T) {
	// MatchNone must be the zero value so an uninitialised MatchResult means "no match".
	var r MatchResult
	if r.How != MatchNone {
		t.Errorf("zero MatchResult.How: want MatchNone, got %v", r.How)
	}
}

// ── matchClosedFromIssues ─────────────────────────────────────────────────────

func TestMatchClosed_PicksMostRecentlyCreated(t *testing.T) {
	older := prFieldIssue(t, "at-old.1", "owner/myrepo", 42)
	older.Status = "closed"
	older.CreatedAt = "2026-07-01T00:00:00Z"
	newer := prFieldIssue(t, "at-new.1", "owner/myrepo", 42)
	newer.Status = "closed"
	newer.CreatedAt = "2026-07-10T00:00:00Z"

	got := matchClosedFromIssues([]bd.Issue{older, newer}, PREvent{Repo: "owner/myrepo", PRNumber: 42})
	if got.How != MatchPRField {
		t.Fatalf("How = %v, want MatchPRField", got.How)
	}
	if got.InitiativeID != "at-new.1" {
		t.Errorf("InitiativeID = %q, want at-new.1 (most recent)", got.InitiativeID)
	}
	if got.Worktree != "/tmp/wt-at-new.1" {
		t.Errorf("Worktree = %q", got.Worktree)
	}
}

func TestMatchClosed_NoMatchReturnsMatchNone(t *testing.T) {
	other := prFieldIssue(t, "at-other.1", "owner/otherrepo", 7)
	other.Status = "closed"

	got := matchClosedFromIssues([]bd.Issue{other}, PREvent{Repo: "owner/myrepo", PRNumber: 42})
	if got.How != MatchNone {
		t.Errorf("How = %v, want MatchNone", got.How)
	}
}

func TestMatchClosed_IgnoresBranchOnlyIssues(t *testing.T) {
	branchOnly := branchIssue(t, "at-br.1", "myrepo", "feat-x")
	branchOnly.Status = "closed"

	got := matchClosedFromIssues([]bd.Issue{branchOnly}, PREvent{Repo: "owner/myrepo", PRNumber: 42})
	if got.How != MatchNone {
		t.Errorf("How = %v, want MatchNone — closed matching is pr-field only", got.How)
	}
}
