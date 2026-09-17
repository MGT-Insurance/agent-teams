package promptsync

import (
	"fmt"
	"os"
	"os/exec"
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

const (
	reviewerF7Heading                  = "F7 — Executable caller compatibility"
	reviewerF7Trigger                  = "Trigger when a diff changes a documented or default executable entrypoint, launcher, or startup configuration."
	reviewerF7Trace                    = "Enumerate every retained public invocation affected by that change, including default scripts/commands and documented aliases. For each, trace the exact argv, cwd, and required environment through each launcher/wrapper to the new target."
	reviewerF7Comparison               = "Compare each caller's produced contract with the target's accepted contract."
	reviewerF7TraceRecord              = "Report a terse explicit F7 trace record for every affected retained public caller in this exact form: `caller=... | invocation=... | argv=... | cwd=... | required-env/state=... | target=... | accepted-contract=... | verdict=...`."
	reviewerF7EmptyArgv                = "Render an empty argv as `argv=[]`."
	reviewerF7RequiredEnvState         = "In `required-env/state`, state what environment/state is required or present and whether any environment/state is read or consumed before an early rejection."
	reviewerF7Evidence                 = "The caller and target-contract evidence must each be anchored at the pinned reviewed commit; the record's caller, target, and accepted-contract values carry those pinned anchors."
	reviewerF7CallerTraceReconcile     = "Reconcile final F7 output against every affected retained public caller in the candidate/audit enumeration: each has exactly one trace record."
	reviewerF7RejectedFindingReconcile = "Every trace with a target-rejected verdict has exactly one structured finding labeled at least medium."
	reviewerF7CompletenessLine         = "End F7 with `F7 completeness: callers=traces=<count> | rejected=findings=<count>`."
	reviewerF7RecordModes              = "Require this record in normal review and when re-verifying a carried F7 finding."
	reviewerF7RejectedCaller           = "A retained default/public caller that the target rejects is a confirmed correctness finding labeled at least medium, unless removal or deprecation is explicit and verified across the retained public surfaces."
	reviewerF7AntiRationalization      = "A caller still present in a package manifest, command registry, retained documentation, or equivalent public surface is not removed/deprecated for F7. Intentional fail-closed rejection, PR disclosure, or tests that encode/assert the mismatch do not waive the correctness finding. The exception requires actual caller removal or an explicit deprecation/migration whose retained public surfaces no longer advertise an invocation the target rejects."
	reviewerF7Probe                    = "When execution adds evidence safely, use a bounded early-fail probe or a fake-child/spawn-capture boundary. Do not start or require a long-running server."
	reviewerF7ReviewPRModes            = "Apply F7 in normal review and when re-verifying a carried F7 finding."
)

var reviewerF7TraceLabels = []string{
	"caller=...",
	"invocation=...",
	"argv=...",
	"cwd=...",
	"required-env/state=...",
	"target=...",
	"accepted-contract=...",
	"verdict=...",
}

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

func TestReviewerF7ExecutableCallerContract(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, path := range []string{
		"promptsrc/agent-teams/roles/reviewer/shared-core.md",
		"plugins/agent-teams/roles/reviewer.md",
		"internal/verbs/codex_agents/agent-teams-reviewer.toml",
	} {
		if err := reviewerF7SharedContractError(path, readReviewerRefinementFile(t, root, path)); err != nil {
			t.Error(err)
		}
	}

	reviewPRPath := "plugins/agent-teams/skills/review-pr/references/reviewer-prompt.md"
	if err := reviewerF7ReviewPRContractError(reviewPRPath, readReviewerRefinementFile(t, root, reviewPRPath)); err != nil {
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

func TestReviewerPrePostHeadCheckFailsClosedWhenNotePersistenceFails(t *testing.T) {
	root := filepath.Join("..", "..")
	skill := readReviewerRefinementFile(t, root, "plugins/agent-teams/skills/review-pr/SKILL.md")
	block, err := reviewerPrePostHeadCheckScript(skill)
	if err != nil {
		t.Fatalf("extract pre-POST head-check script: %v", err)
	}
	block = strings.NewReplacer(
		"<pr-number>", "1",
		"<owner>/<repo>", "owner/repo",
		"<id>", "initiative",
		"<reviewed-sha>", "reviewed-sha",
	).Replace(block)

	for _, tt := range []struct {
		name      string
		jobDir    string
		ghStatus  string
		ghOutput  string
		wantAteam bool
	}{
		{
			name:     "note-file write fails after lookup failure",
			jobDir:   filepath.Join(t.TempDir(), "missing-job-dir"),
			ghStatus: "1",
		},
		{
			name:      "note persistence fails after lookup failure",
			jobDir:    t.TempDir(),
			ghStatus:  "1",
			wantAteam: true,
		},
		{
			name:      "note persistence fails after head changes",
			jobDir:    t.TempDir(),
			ghStatus:  "0",
			ghOutput:  "different-head",
			wantAteam: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantAteam {
				if err := os.Mkdir(filepath.Join(tt.jobDir, "tmp"), 0o755); err != nil {
					t.Fatalf("create job temp directory: %v", err)
				}
			}
			postMarker := filepath.Join(t.TempDir(), "review-posted")
			ateamMarker := filepath.Join(t.TempDir(), "ateam-note-called")
			script := "gh() { if [ \"$GH_STATUS\" -eq 0 ]; then printf '%s\\n' \"$GH_OUTPUT\"; fi; return \"$GH_STATUS\"; }\n" +
				"ateam() { touch \"$ATEAM_MARKER\"; return 1; }\n" +
				block +
				"\nprintf 'review POST reached\\n' > \"$POST_MARKER\"\n"
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(),
				"CLAUDE_JOB_DIR="+tt.jobDir,
				"GH_STATUS="+tt.ghStatus,
				"GH_OUTPUT="+tt.ghOutput,
				"ATEAM_MARKER="+ateamMarker,
				"POST_MARKER="+postMarker,
			)
			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatal("failed restart-note persistence exited successfully")
			}
			if _, err := os.Stat(postMarker); !os.IsNotExist(err) {
				t.Fatalf("failed restart-note persistence reached a review POST: %v", err)
			}
			_, statErr := os.Stat(ateamMarker)
			if tt.wantAteam != !os.IsNotExist(statErr) {
				t.Fatalf("ateam note invocation = %t, want %t (stat error: %v; shell output: %s)", !os.IsNotExist(statErr), tt.wantAteam, statErr, output)
			}
		})
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
		{
			name: "lookup-failure note-file write may restart successfully",
			body: strings.Replace(skill, `> "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt" || exit 1`, `> "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt"`, 1),
		},
		{
			name: "lookup-failure note persistence may restart successfully",
			body: strings.Replace(skill, `ateam note <id> --file "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt" || exit 1`, `ateam note <id> --file "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt"`, 1),
		},
		{
			name: "different-head note persistence may restart successfully",
			body: strings.Replace(skill, `ateam note <id> --file "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt" || exit 1`, `ateam note <id> --file "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt"`, 2),
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

func TestReviewerF7ExecutableCallerMutationGuards(t *testing.T) {
	root := filepath.Join("..", "..")
	sharedPath := "promptsrc/agent-teams/roles/reviewer/shared-core.md"
	shared := readReviewerRefinementFile(t, root, sharedPath)
	if err := reviewerF7SharedContractError(sharedPath, shared); err != nil {
		t.Fatalf("control F7 executable-caller contract rejected: %v", err)
	}

	for _, tt := range []struct {
		name string
		body string
	}{
		{
			name: "retained public caller enumeration removed",
			body: strings.Replace(shared,
				"Enumerate every retained public invocation affected by that change, including default scripts/commands and documented aliases.",
				"", 1),
		},
		{
			name: "explicit trace record omits target contract evidence",
			body: strings.Replace(shared, reviewerF7Evidence, "", 1),
		},
		{
			name: "caller trace reconciliation removed",
			body: strings.Replace(shared, reviewerF7CallerTraceReconcile, "", 1),
		},
		{
			name: "rejected trace finding reconciliation removed",
			body: strings.Replace(shared, reviewerF7RejectedFindingReconcile, "", 1),
		},
		{
			name: "completeness equality line removed",
			body: strings.Replace(shared, reviewerF7CompletenessLine, "", 1),
		},
		{
			name: "rejected retained default caller weakened below medium",
			body: strings.Replace(shared, "labeled at least medium", "labeled low", 1),
		},
		{
			name: "retained manifest caller is rationalized as intentionally deprecated",
			body: strings.Replace(shared, reviewerF7AntiRationalization, "A retained package-manifest caller may be treated as deprecated when its target intentionally rejects it.", 1),
		},
		{
			name: "PR disclosure or mismatch test waives retained caller finding",
			body: strings.Replace(shared, reviewerF7AntiRationalization, "PR disclosure or tests that encode/assert a retained caller mismatch waive the correctness finding.", 1),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := reviewerF7SharedContractError(sharedPath, tt.body); err == nil {
				t.Fatal("F7 executable-caller contract mutation was accepted")
			}
		})
	}

	for _, label := range reviewerF7TraceLabels {
		t.Run("literal trace schema omits "+label, func(t *testing.T) {
			mutated := strings.Replace(shared, label, "omitted-"+label, 1)
			if err := reviewerF7SharedContractError(sharedPath, mutated); err == nil {
				t.Fatalf("F7 executable-caller contract accepted trace schema without %q", label)
			}
		})
	}

	for _, tt := range []struct {
		name string
		body string
	}{
		{
			name: "empty argv is not rendered as an empty list",
			body: strings.Replace(shared, reviewerF7EmptyArgv, "Render an empty argv as `argv=none`.", 1),
		},
		{
			name: "required environment state omits pre-rejection consumption",
			body: strings.Replace(shared, reviewerF7RequiredEnvState, "In `required-env/state`, state what environment/state is required or present.", 1),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := reviewerF7SharedContractError(sharedPath, tt.body); err == nil {
				t.Fatal("F7 executable-caller contract accepted missing trace-state detail")
			}
		})
	}

	reviewPRPath := "plugins/agent-teams/skills/review-pr/references/reviewer-prompt.md"
	reviewPR := readReviewerRefinementFile(t, root, reviewPRPath)
	if err := reviewerF7ReviewPRContractError(reviewPRPath, reviewPR); err != nil {
		t.Fatalf("control F7 review-pr contract rejected: %v", err)
	}
	for _, label := range reviewerF7TraceLabels {
		for _, occurrence := range []int{1, 2} {
			t.Run(fmt.Sprintf("review-pr trace schema omits %s in occurrence %d", label, occurrence), func(t *testing.T) {
				mutatedRecord := strings.Replace(reviewerF7TraceRecord, label, "omitted-"+label, 1)
				mutated := replaceNth(reviewPR, reviewerF7TraceRecord, mutatedRecord, occurrence)
				if err := reviewerF7ReviewPRContractError(reviewPRPath, mutated); err == nil {
					t.Fatalf("F7 review-pr contract accepted trace schema without %q in occurrence %d", label, occurrence)
				}
			})
		}
	}
	for _, clause := range []string{reviewerF7EmptyArgv, reviewerF7RequiredEnvState, reviewerF7CallerTraceReconcile, reviewerF7RejectedFindingReconcile, reviewerF7CompletenessLine} {
		if err := reviewerF7ReviewPRContractError(reviewPRPath, strings.Replace(reviewPR, clause, "", 1)); err == nil {
			t.Fatalf("F7 review-pr contract accepted a normal-review deletion of %q", clause)
		}
		if err := reviewerF7ReviewPRContractError(reviewPRPath, replaceNth(reviewPR, clause, "", 2)); err == nil {
			t.Fatalf("F7 review-pr contract accepted a re-review deletion of %q", clause)
		}
	}
	if err := reviewerF7ReviewPRContractError(reviewPRPath, strings.Replace(reviewPR, reviewerF7AntiRationalization, "", 1)); err == nil {
		t.Fatal("F7 review-pr contract accepted an anti-rationalization deletion in normal or re-review")
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

func reviewerF7SharedContractError(path, body string) error {
	for _, clause := range []string{
		reviewerF7Heading,
		reviewerF7Trigger,
		reviewerF7Trace,
		reviewerF7Comparison,
		reviewerF7TraceRecord,
		reviewerF7EmptyArgv,
		reviewerF7RequiredEnvState,
		reviewerF7Evidence,
		reviewerF7CallerTraceReconcile,
		reviewerF7RejectedFindingReconcile,
		reviewerF7CompletenessLine,
		reviewerF7RecordModes,
		reviewerF7RejectedCaller,
		reviewerF7AntiRationalization,
		reviewerF7Probe,
	} {
		if !strings.Contains(strings.Join(strings.Fields(body), " "), strings.Join(strings.Fields(clause), " ")) {
			return fmt.Errorf("%s is missing F7 executable-caller contract: %q", path, clause)
		}
	}
	return nil
}

func replaceNth(body, old, replacement string, occurrence int) string {
	start := 0
	for i := 0; i < occurrence; i++ {
		index := strings.Index(body[start:], old)
		if index < 0 {
			return body
		}
		start += index
		if i == occurrence-1 {
			return body[:start] + replacement + body[start+len(old):]
		}
		start += len(old)
	}
	return body
}

func reviewerF7ReviewPRContractError(path, body string) error {
	if err := reviewerF7SharedContractError(path, body); err != nil {
		return err
	}
	if strings.Count(strings.Join(strings.Fields(body), " "), strings.Join(strings.Fields(reviewerF7TraceRecord), " ")) != 2 {
		return fmt.Errorf("%s must require the explicit F7 trace record in both normal review and re-review", path)
	}
	for _, clause := range []string{reviewerF7EmptyArgv, reviewerF7RequiredEnvState, reviewerF7CallerTraceReconcile, reviewerF7RejectedFindingReconcile, reviewerF7CompletenessLine} {
		if strings.Count(strings.Join(strings.Fields(body), " "), strings.Join(strings.Fields(clause), " ")) != 2 {
			return fmt.Errorf("%s must require %q in both normal review and re-review", path, clause)
		}
	}
	if strings.Count(strings.Join(strings.Fields(body), " "), strings.Join(strings.Fields(reviewerF7AntiRationalization), " ")) != 2 {
		return fmt.Errorf("%s must require F7 anti-rationalization in both normal review and re-review", path)
	}
	if !strings.Contains(strings.Join(strings.Fields(body), " "), strings.Join(strings.Fields(reviewerF7ReviewPRModes), " ")) {
		return fmt.Errorf("%s is missing F7 normal-review and re-review contract: %q", path, reviewerF7ReviewPRModes)
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
    > "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt" || exit 1
  ateam note <id> --file "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt" || exit 1
  exit 0
fi`
	const differentHeadRestart = `if [ "$CURRENT_HEAD" != "<reviewed-sha>" ]; then
  printf 'review-round-restarted: PR #<pr-number> — reviewed-sha: <reviewed-sha>; current-sha: %s\n' "$CURRENT_HEAD" \
    > "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt" || exit 1
  ateam note <id> --file "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt" || exit 1
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

func reviewerPrePostHeadCheckScript(body string) (string, error) {
	const heading = "#### Recheck the PR head immediately before posting"
	const nextSection = "#### Handle the no-findings case"
	sectionStart := strings.Index(body, heading)
	sectionEnd := strings.Index(body, nextSection)
	if sectionStart < 0 || sectionEnd < 0 || sectionEnd <= sectionStart {
		return "", fmt.Errorf("review-pr pre-POST head check section is missing")
	}
	section := body[sectionStart:sectionEnd]
	start := strings.Index(section, "```bash\n")
	if start < 0 {
		return "", fmt.Errorf("review-pr pre-POST head check shell template is missing")
	}
	start += len("```bash\n")
	end := strings.Index(section[start:], "\n```")
	if end < 0 {
		return "", fmt.Errorf("review-pr pre-POST head check shell template is unterminated")
	}
	return section[start : start+end], nil
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
