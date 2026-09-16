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

var mergeEnforcementInstructions = []*regexp.Regexp{
	regexp.MustCompile(`(?is)\b(?:require|must|shall|ensure|enforce|add|create|post|emit)\b.{0,120}\bunresolved-at-merge\b`),
	regexp.MustCompile(`(?is)\bunresolved-at-merge\b.{0,120}\b(?:require|must|shall|ensure|enforce|add|create|post|emit)\b`),
	regexp.MustCompile(`(?i)\bdo not (?:allow|permit)\s+(?:a\s+|the\s+)?merg(?:e|ing)\b`),
	regexp.MustCompile(`(?i)\b(?:block|prevent|gate|forbid|prohibit|deny)\s+(?:a\s+|the\s+)?merg(?:e|ing)\b`),
	regexp.MustCompile(`(?i)\bmerg(?:e|ing)\s+(?:is\s+)?(?:forbidden|prohibited|denied)\b`),
	regexp.MustCompile(`(?is)\b(?:require|must|shall|ensure)\b.{0,120}\bfindings?\b.{0,80}\bresolved\b.{0,80}\b(?:before|prior to)\s+merg(?:e|ing)\b`),
	regexp.MustCompile(`(?i)\b(?:create|post|emit)\s+(?:a\s+)?merge warning\b`),
}
var negatedMergeInstruction = regexp.MustCompile(`(?i)(?:\bnever|\bno|\bdo not|\bdon't)\s*$`)

const reviewerSkillMinimumHeadroom = 1300

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
				"`Reviewed commit: <reviewed-sha>` (full `headRefOid`)",
				"Every successful review body opens with `## Summary` and contains its own `Reviewed commit: <reviewed-sha>` line.",
				"Normal and re-review bodies carry the reviewer's risk-scaled parity/overlap and identifiability record verbatim: compact line when eligible, otherwise full per-path rows.",
				"one line per PRIOR finding, same order as step 5",
				"restatement covers every carried finding, in original order",
				"reviewed-sha: <reviewed-sha>",
				"**One-round event invariant:** assemble the complete body and every eligible inline comment before one top-level `/reviews` POST.",
				"Never post one review per finding or a partial review.",
				"A thread acknowledgement uses the review-comment reply endpoint, never another top-level review event.",
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
	if err := reviewerReviewPostBindingError(skill); err != nil {
		t.Error(err)
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
	if units >= SkillUTF16Limit || !reviewerSkillHasRequiredHeadroom(headroom) {
		t.Fatalf("%s rendered prompt = %d UTF-16 units, headroom = %d; want < %d and >= %d headroom (frontmatter stripped; loader base-directory line included)", path, units, headroom, SkillUTF16Limit, reviewerSkillMinimumHeadroom)
	}
}

func TestReviewerRefinementSkillHeadroomBoundary(t *testing.T) {
	if !reviewerSkillHasRequiredHeadroom(reviewerSkillMinimumHeadroom) {
		t.Fatalf("%d headroom was rejected", reviewerSkillMinimumHeadroom)
	}
	if reviewerSkillHasRequiredHeadroom(1200) {
		t.Fatal("hypothetical 1200 headroom was accepted; want it below the required boundary")
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
	if err := reviewerReviewPostBindingError(skill); err != nil {
		t.Fatalf("control review-post SHA binding rejected: %v", err)
	}
	for _, tt := range []struct {
		name string
		body string
	}{
		{
			name: "no-findings commit_id removed",
			body: reviewerReviewPostCommitIDRemoved(skill, 0),
		},
		{
			name: "findings commit_id removed",
			body: reviewerReviewPostCommitIDRemoved(skill, 1),
		},
		{
			name: "failed-head lookup guard removed",
			body: strings.Replace(skill, "if ! CURRENT_HEAD=$(gh pr view <pr-number> --repo <owner>/<repo> --json headRefOid --jq .headRefOid); then", "if false; then", 1),
		},
		{
			name: "head equality guard removed",
			body: strings.Replace(skill, "if [ \"$CURRENT_HEAD\" != \"<reviewed-sha>\" ]; then", "if false; then", 1),
		},
		{
			name: "round restart guard removed",
			body: strings.Replace(skill, "  exit 0\nfi\nif [ \"$CURRENT_HEAD\" != \"<reviewed-sha>\" ]; then", "fi\nif [ \"$CURRENT_HEAD\" != \"<reviewed-sha>\" ]; then", 1),
		},
		{
			name: "lookup-failure restart note removed",
			body: strings.Replace(skill, "review-round-restarted: PR #<pr-number> — reviewed-sha: <reviewed-sha>; current-sha: lookup failed", "", 1),
		},
		{
			name: "different-head restart note removed",
			body: strings.Replace(skill, "review-round-restarted: PR #<pr-number> — reviewed-sha: <reviewed-sha>; current-sha: %s", "", 1),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := reviewerReviewPostBindingError(tt.body); err == nil {
				t.Fatal("review-post SHA binding mutation was accepted")
			}
		})
	}
	for _, tt := range []struct {
		name        string
		instruction string
	}{
		{"unresolved-at-merge requirement", "Require an unresolved-at-merge enforcement mechanism before merge."},
		{"do not permit merging", "Do not permit merging while findings remain."},
		{"block merge", "Block merging until all findings are resolved."},
		{"prevent merge", "Prevent a merge when findings remain unresolved."},
		{"forbid merge", "Forbid merging while findings remain."},
		{"prohibit merge", "Prohibit a merge while findings remain."},
		{"deny merge", "Deny merging while findings remain."},
		{"passive forbidden merge", "Merging is forbidden while findings remain."},
		{"passive prohibited merge", "A merge is prohibited while findings remain."},
		{"passive denied merge", "Merging is denied while findings remain."},
		{"require findings resolved", "Require findings to be resolved before merge."},
		{"create merge warning", "Create a merge warning for unresolved findings."},
		{"post merge warning", "Post a merge warning for unresolved findings."},
		{"emit merge warning", "Emit a merge warning for unresolved findings."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mutated := skill + "\n- " + tt.instruction + "\n"
			if err := reviewerRefinementAdvisoryError(mutated); err == nil {
				t.Fatalf("positive merge enforcement %q was accepted alongside the COMMENT mapping and approved advisory sentence", tt.instruction)
			}
		})
	}

	for _, advisory := range []string{
		"Never use REQUEST_CHANGES for a review event.",
		"Never create a merge warning.",
		"No merge enforcement is permitted.",
		"Do not forbid merging while findings remain.",
	} {
		if err := reviewerRefinementAdvisoryError(skill + "\n- " + advisory + "\n"); err != nil {
			t.Fatalf("approved advisory prose %q was rejected: %v", advisory, err)
		}
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
	if positiveMergeEnforcementInstruction(withoutApprovedAdvisory) {
		return fmt.Errorf("review-pr skill contains a positive merge-enforcement instruction")
	}
	return nil
}

func reviewerReviewPostBindingError(body string) error {
	const lookup = "if ! CURRENT_HEAD=$(gh pr view <pr-number> --repo <owner>/<repo> --json headRefOid --jq .headRefOid); then"
	const guard = "if [ \"$CURRENT_HEAD\" != \"<reviewed-sha>\" ]; then"
	const restart = "# Do not POST; discard this round and restart at step 3."
	if err := reviewerRefinementClausesError("review-pr review-post binding", body,
		"#### Recheck the PR head immediately before posting",
		lookup,
		guard,
		"review-round-restarted: PR #<pr-number> — reviewed-sha: <reviewed-sha>; current-sha: lookup failed",
		"review-round-restarted: PR #<pr-number> — reviewed-sha: <reviewed-sha>; current-sha: %s",
		"ateam note <id> --file \"${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt\"",
		restart,
		"exit 0",
		"Run immediately before the selected POST, with no intervening reviewer work.",
		"On failed/different lookup, including retry, record this durable restart note, discard body/comments, and restart at 3 with a new SHA.",
		"do not add `commit_id` to that reply endpoint.",
	); err != nil {
		return err
	}
	if err := reviewerPrePostHeadCheckError(body); err != nil {
		return err
	}

	for _, template := range []struct {
		name   string
		marker string
	}{
		{"no-findings", "#### Handle the no-findings case"},
		{"findings", "#### Handle findings"},
	} {
		post, err := reviewerTopLevelReviewPostTemplate(body, template.marker)
		if err != nil {
			return fmt.Errorf("%s review POST: %w", template.name, err)
		}
		if !strings.Contains(post, "-f commit_id=<reviewed-sha> \\") {
			return fmt.Errorf("%s review POST is missing -f commit_id=<reviewed-sha>", template.name)
		}
	}
	reply, err := reviewerTopLevelReviewPostTemplate(body, "2. **Respond to each thread")
	if err != nil {
		return fmt.Errorf("review-comment reply POST: %w", err)
	}
	if strings.Contains(reply, "commit_id") {
		return fmt.Errorf("review-comment reply POST must not bind commit_id")
	}
	return nil
}

func reviewerSkillHasRequiredHeadroom(headroom int) bool {
	return headroom >= reviewerSkillMinimumHeadroom
}

func reviewerPrePostHeadCheckError(body string) error {
	const heading = "#### Recheck the PR head immediately before posting"
	const nextSection = "#### Handle the no-findings case"
	const lookupFailureRestart = `if ! CURRENT_HEAD=$(gh pr view <pr-number> --repo <owner>/<repo> --json headRefOid --jq .headRefOid); then
  printf 'review-round-restarted: PR #<pr-number> — reviewed-sha: <reviewed-sha>; current-sha: lookup failed\n' \
    > "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt"
  ateam note <id> --file "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt"
  exit 0
fi`
	const differentHeadRestart = `if [ "$CURRENT_HEAD" != "<reviewed-sha>" ]; then
  printf 'review-round-restarted: PR #<pr-number> — reviewed-sha: <reviewed-sha>; current-sha: %s\n' "$CURRENT_HEAD" \
    > "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt"
  ateam note <id> --file "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt"
  # Do not POST; discard this round and restart at step 3.
  exit 0
fi`
	start := strings.Index(body, heading)
	end := strings.Index(body, nextSection)
	if start < 0 || end < 0 || end <= start {
		return fmt.Errorf("review-pr pre-POST head check section is missing")
	}
	section := body[start:end]
	if strings.Contains(section, "gh api repos/<owner>/<repo>/pulls/<pr-number>/reviews") {
		return fmt.Errorf("review-pr pre-POST head check must not POST a review")
	}
	if !strings.Contains(section, lookupFailureRestart) {
		return fmt.Errorf("review-pr pre-POST failed-head restart must durably note the lookup failure before exit")
	}
	if !strings.Contains(section, differentHeadRestart) {
		return fmt.Errorf("review-pr pre-POST different-head restart must durably note both SHAs before exit")
	}
	return nil
}

func reviewerReviewPostCommitIDRemoved(body string, occurrence int) string {
	const commitID = "-f commit_id=<reviewed-sha>"
	start := 0
	for i := 0; i <= occurrence; i++ {
		index := strings.Index(body[start:], commitID)
		if index < 0 {
			return body
		}
		index += start
		if i == occurrence {
			return body[:index] + body[index+len(commitID):]
		}
		start = index + len(commitID)
	}
	return body
}

func reviewerTopLevelReviewPostTemplate(body, marker string) (string, error) {
	section := strings.Index(body, marker)
	if section < 0 {
		return "", fmt.Errorf("missing %q section", marker)
	}
	start := strings.Index(body[section:], "```bash\n")
	if start < 0 {
		return "", fmt.Errorf("missing shell template")
	}
	start += section + len("```bash\n")
	end := strings.Index(body[start:], "\n```")
	if end < 0 {
		end = strings.Index(body[start:], "\n   ```")
	}
	if end < 0 {
		return "", fmt.Errorf("unterminated shell template")
	}
	return body[start : start+end], nil
}

func positiveMergeEnforcementInstruction(body string) bool {
	for _, clause := range strings.FieldsFunc(body, func(r rune) bool {
		return r == '.' || r == '!' || r == '?' || r == ';' || r == '\n'
	}) {
		for _, instruction := range mergeEnforcementInstructions {
			for _, match := range instruction.FindAllStringIndex(clause, -1) {
				if !negatedMergeInstruction.MatchString(clause[:match[0]]) {
					return true
				}
			}
		}
	}
	return false
}
