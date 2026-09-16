package promptsync

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"
)

var requestChangesEvent = regexp.MustCompile("(?m)\\bevent\\s*=\\s*[`\\\"]?REQUEST_CHANGES[`\\\"]?")
var mergeEnforcementInstruction = regexp.MustCompile(`(?is)(?:\b(?:require|must|shall|ensure|enforce|block|gate|prevent)\b.{0,120}\b(?:unresolved-at-merge|merge)\b|\bunresolved-at-merge\b.{0,120}\b(?:require|must|shall|ensure|enforce|block|gate|prevent)\b|\b(?:add|create|post|emit)\b.{0,120}\b(?:unresolved-at-merge|merge warning)\b)`)

func TestReviewerRefinementSharedContract(t *testing.T) {
	root := filepath.Join("..", "..")
	paths := []string{
		"promptsrc/agent-teams/roles/reviewer/shared-core.md",
		"plugins/agent-teams/roles/reviewer.md",
		"internal/verbs/codex_agents/agent-teams-reviewer.toml",
	}
	clauses := []string{
		"Before reporting any file:line, read that exact file at the pinned reviewed commit, using `git show <reviewed-sha>:<path>`",
		"A diff hunk, mutable worktree read, or stale search result alone is insufficient evidence for a citation.",
		"every confirmed correctness defect is at least `medium` and appears in the structured findings list, never only in narrative or audit prose.",
		"If evidence suggests a higher impact than the selected label but does not justify promotion, add one terse sentence naming that possible higher impact.",
		"*Guard/invariant parity*: for every new or materially changed code path, compare established sibling paths for termination/cycle protection, authorization, idempotency, validation, and error handling.",
		"A sibling-established guard missing from the changed path is a correctness finding.",
		"This reverse comparison complements the inbound check for sibling surfaces that need the PR's fix; do both.",
		"Compact output is allowed only for a small, low-risk, clean change with no affected sibling or consumer requiring a row and no substantive finding.",
		"Use full per-path audit rows whenever a candidate sibling or affected consumer exists, the change touches shared state/config, automated actions, authorization/security, persistence/data writes, or the review has a substantive finding.",
	}
	for _, path := range paths {
		if err := reviewerRefinementClausesError(path, readReviewerRefinementFile(t, root, path), clauses...); err != nil {
			t.Error(err)
		}
	}

	if _, err := Check(Config{Root: root}); err != nil {
		t.Fatalf("prompt-sync exact render comparison: %v", err)
	}
}

func TestReviewerRefinementReviewPRContract(t *testing.T) {
	root := filepath.Join("..", "..")
	skillPath := "plugins/agent-teams/skills/review-pr/SKILL.md"
	referencePath := "plugins/agent-teams/skills/review-pr/references/reviewer-prompt.md"
	skill := readReviewerRefinementFile(t, root, skillPath)
	reference := readReviewerRefinementFile(t, root, referencePath)

	for _, check := range []struct {
		path    string
		body    string
		clauses []string
	}{
		{
			path: skillPath,
			body: skill,
			clauses: []string{
				"At the start of **every** review round, capture the PR head exactly once",
				"keep its full SHA as `<reviewed-sha>` for the whole round",
				"Record `.headRefOid` as `<reviewed-sha>`",
				"`Reviewed commit: <reviewed-sha>` (the full captured `headRefOid`)",
				"Every successful review body opens with `## Summary` and contains its own `Reviewed commit: <reviewed-sha>` line.",
				"Re-reviews carry the same risk-scaled audit record: its compact audit line when eligible, or its full per-path rows otherwise.",
				"one line per PRIOR finding, same order as step 5",
				"restatement covers every carried finding, in original order",
				"reviewed-sha: <reviewed-sha>",
				"**One-round event invariant:** assemble the complete body and every eligible inline comment before one top-level `/reviews` POST.",
				"Never post one review per finding or a partial review.",
				"An acknowledgement in an existing thread uses the review-comment reply endpoint, never another top-level review event.",
				"Post as `COMMENT`, never `REQUEST_CHANGES`; findings never create a merge warning, unresolved-at-merge mechanism, or other enforcement.",
			},
		},
		{
			path: referencePath,
			body: reference,
			clauses: []string{
				"The supplied full `Reviewed commit: <reviewed-sha>` and diff are this round's immutable scope",
				"Before every reported `file:line`, open that path at `<reviewed-sha>` with `git show <reviewed-sha>:<path>`",
				"Give its **current** `file:line`, or explicitly say `construct no longer exists` if no current anchor remains.",
				"Report via SendMessage one line per prior finding, in original order",
				"Carry each original label unchanged",
				"sibling-guard parity (both directions)",
				"risk-scaled parity/identifiability audit as normal mode",
				"Separately include the same labeled audit-record section as normal mode: the compact audit line when eligible, or the full per-path parity/overlap rows plus identifiability answer otherwise.",
				"The orchestrator renders this section verbatim in the re-review body.",
			},
		},
	} {
		if err := reviewerRefinementClausesError(check.path, check.body, check.clauses...); err != nil {
			t.Error(err)
		}
	}

	if err := reviewerRefinementAdvisoryError(skill); err != nil {
		t.Error(err)
	}
}

func TestReviewerRefinementReviewPRRenderedUTF16Budget(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("absolute repository root: %v", err)
	}
	path := filepath.Join(root, "plugins", "agent-teams", "skills", "review-pr", "SKILL.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body, err := stripFrontmatter(content)
	if err != nil {
		t.Fatalf("strip %s frontmatter: %v", path, err)
	}
	rendered := "Base directory for this skill: " + filepath.ToSlash(filepath.Dir(path)) + "\n\n" + string(body)
	units := len(utf16.Encode([]rune(rendered)))
	headroom := SkillUTF16Limit - units
	if units >= SkillUTF16Limit || headroom < 1000 {
		t.Fatalf("%s rendered prompt = %d UTF-16 units, headroom = %d; want < %d and >= 1000 headroom (frontmatter stripped; loader base-directory line included)", path, units, headroom, SkillUTF16Limit)
	}
}

func TestReviewerRefinementMutationGuards(t *testing.T) {
	root := filepath.Join("..", "..")
	sharedPath := "promptsrc/agent-teams/roles/reviewer/shared-core.md"
	shared := readReviewerRefinementFile(t, root, sharedPath)
	openBeforeCite := "Before reporting any file:line, read that exact file at the pinned reviewed commit, using `git show <reviewed-sha>:<path>`"
	if err := reviewerRefinementClausesError(sharedPath, shared, openBeforeCite); err != nil {
		t.Fatalf("control shared contract rejected: %v", err)
	}
	if err := reviewerRefinementClausesError(sharedPath, strings.Replace(shared, openBeforeCite, "", 1), openBeforeCite); err == nil {
		t.Fatal("open-before-cite removal was accepted")
	}

	skillPath := "plugins/agent-teams/skills/review-pr/SKILL.md"
	skill := readReviewerRefinementFile(t, root, skillPath)
	oneEvent := "**One-round event invariant:** assemble the complete body and every eligible inline comment before one top-level `/reviews` POST."
	if err := reviewerRefinementClausesError(skillPath, skill, oneEvent); err != nil {
		t.Fatalf("control one-event contract rejected: %v", err)
	}
	if err := reviewerRefinementClausesError(skillPath, strings.Replace(skill, "**One-round event invariant:**", "", 1), oneEvent); err == nil {
		t.Fatal("one-review-event removal was accepted")
	}
	if err := reviewerRefinementAdvisoryError(strings.Replace(skill, "-f event=COMMENT", "-f event=REQUEST_CHANGES", 1)); err == nil {
		t.Fatal("REQUEST_CHANGES advisory regression was accepted")
	}
	contradictoryMergeEnforcement := skill + "\n- Require an unresolved-at-merge enforcement mechanism before merge.\n"
	if err := reviewerRefinementAdvisoryError(contradictoryMergeEnforcement); err == nil {
		t.Fatal("unresolved-at-merge enforcement was accepted alongside the COMMENT mapping and advisory-only sentence")
	}
}

func readReviewerRefinementFile(t *testing.T, root, path string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func reviewerRefinementClausesError(path, body string, clauses ...string) error {
	normalized := strings.Join(strings.Fields(body), " ")
	for _, clause := range clauses {
		if !strings.Contains(normalized, strings.Join(strings.Fields(clause), " ")) {
			return fmt.Errorf("%s is missing reviewer refinement contract clause %q", path, clause)
		}
	}
	return nil
}

func reviewerRefinementAdvisoryError(body string) error {
	if requestChangesEvent.MatchString(body) {
		return fmt.Errorf("review-pr skill maps a review event to REQUEST_CHANGES")
	}
	advisoryOnly := "Post as `COMMENT`, never `REQUEST_CHANGES`; findings never create a merge warning, unresolved-at-merge mechanism, or other enforcement."
	if err := reviewerRefinementClausesError("review-pr advisory contract", body, advisoryOnly); err != nil {
		return err
	}
	withoutApprovedAdvisory := strings.Replace(body, advisoryOnly, "", 1)
	if mergeEnforcementInstruction.MatchString(withoutApprovedAdvisory) {
		return fmt.Errorf("review-pr skill contains a positive merge-enforcement instruction")
	}
	return nil
}
