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

// ── parseDescriptionFields ────────────────────────────────────────────────────

func TestParseDescriptionFields_Basic(t *testing.T) {
	desc := "repo: /Users/eric/Code/myapp\nworktree: /tmp/wt\nbranch: feat-x\nmode: bg\n"
	fields := parseDescriptionFields(desc)
	if fields["repo"] != "/Users/eric/Code/myapp" {
		t.Errorf("repo: got %q", fields["repo"])
	}
	if fields["worktree"] != "/tmp/wt" {
		t.Errorf("worktree: got %q", fields["worktree"])
	}
	if fields["branch"] != "feat-x" {
		t.Errorf("branch: got %q", fields["branch"])
	}
	if fields["mode"] != "bg" {
		t.Errorf("mode: got %q", fields["mode"])
	}
}

func TestParseDescriptionFields_SkipsEmptyKeyOrValue(t *testing.T) {
	desc := ": value-no-key\nkey-no-value:\nnormal: ok\n"
	fields := parseDescriptionFields(desc)
	// Only "normal" should survive.
	if fields["normal"] != "ok" {
		t.Errorf("normal: got %q", fields["normal"])
	}
	if len(fields) != 1 {
		t.Errorf("expected 1 field, got %d: %v", len(fields), fields)
	}
}

func TestParseDescriptionFields_ColonInValue(t *testing.T) {
	// Colons in the VALUE must not split further (only first colon is the key separator).
	desc := "url: https://github.com/owner/repo/pull/42\n"
	fields := parseDescriptionFields(desc)
	if fields["url"] != "https://github.com/owner/repo/pull/42" {
		t.Errorf("url: got %q", fields["url"])
	}
}

func TestParseDescriptionFields_KeyLowercased(t *testing.T) {
	desc := "REPO: /some/path\nBranch: main\n"
	fields := parseDescriptionFields(desc)
	if fields["repo"] != "/some/path" {
		t.Errorf("REPO not lowercased: got %q", fields["repo"])
	}
	if fields["branch"] != "main" {
		t.Errorf("Branch not lowercased: got %q", fields["branch"])
	}
}

func TestParseDescriptionFields_EmptyInput(t *testing.T) {
	fields := parseDescriptionFields("")
	if len(fields) != 0 {
		t.Errorf("expected empty map, got %v", fields)
	}
}

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
	// Notes has PR #10 (owner/notes-repo), Description has PR #20 (owner/desc-repo).
	// Event matches the notes PR → should be MatchPRField.
	desc := descLines("/repo", "/wt/at-ccc", "br") +
		"pr: https://github.com/owner/desc-repo/pull/20\n"
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
