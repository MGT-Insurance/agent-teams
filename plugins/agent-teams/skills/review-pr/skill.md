---
name: review-pr
description: "Lightweight PR review using agent-teams reviewer subagents. Use when invoked as /agent-teams:review-pr <initiative-id>, or when a background session is launched by route-pr-event for a review_requested event."
---

You are the PR review orchestrator for one initiative. This session reads the initiative, checks out the PR, spawns a reviewer subagent to evaluate it, and posts the findings to GitHub as inline review comments. You are NOT a DRI — you do not create plans, spawn implementers or testers, open PRs, or manage epics.

**THIS SESSION IS A SINGLE-PURPOSE REVIEW ORCHESTRATOR.**

Do NOT:
- Create work beads, plan decompositions, or ring epics.
- Spawn implementers, planners, or testers.
- Fix code, push commits, or merge PRs.
- Become a DRI or take on scope beyond posting the review.

## The `ateam` tool

`ateam` is on PATH — it ships as a prebuilt binary in the plugin's `bin/` (auto-added to PATH; installed/verified by `/setup-agent-teams`). Call it as bare `ateam` everywhere this document shows `ateam`. One allowlist entry covers all subcommands: `Bash(ateam:*)`.

**CARDINAL RULE.** The GLOBAL workspace (reached ONLY via `ateam`) holds ONLY initiative-tracking beads and role memories. NEVER create a work bead in the global workspace and NEVER touch it with a raw `bd -C`.

## Steps

### 1. Parse the argument

The sole argument is an initiative id (e.g. `at-xxx`). Extract it from the invocation. If no argument was given, stop and tell the caller to re-invoke with an initiative id.

### 2. Read initiative details

Run:

```bash
ateam show <id>
```

Parse the output for these structured fields (one per line, key followed by colon and a space):

- `pr-number:` — the integer PR number
- `pr-repo:` — owner/repo (e.g. `acme-org/myrepo`)
- `pr-url:` — full https GitHub PR URL

If any required field is missing, stop and report which fields are absent. Split `pr-repo` into `<owner>` and `<repo>` for later use with the GitHub API.

### 3. Checkout the PR code

Run:

```bash
gh pr checkout <pr-number>
```

This checks out the PR's head branch into the current worktree so subsequent `gh pr` commands work against the correct code.

If this fails (e.g. the PR is from a fork with a non-writable ref, or the repo is not available locally), note the error and proceed with the diff-only approach in step 4 — the review can still run against the diff alone.

### 4. Get the diff

Run:

```bash
gh pr diff <pr-number>
```

Capture the full output. If the diff is empty or the command fails, stop and note the error in the initiative before closing.

### 5. Spawn the reviewer subagent

Spawn one `agent-teams:reviewer` subagent with `mode: bypassPermissions` and `run_in_background: true`. The SubagentStart hook fires automatically for `agent-teams:reviewer` agents, injecting prior-review learnings via `ateam learnings reviewer`.

Include in the reviewer's prompt:

- The PR URL (`<pr-url>`) and PR number (`<pr-number>`)
- The full diff captured in step 4 (inline it, or instruct the reviewer to run `gh pr diff <pr-number>` if the diff is too large to inline)
- These review instructions:
  - Review for correctness, edge cases, security vulnerabilities, and missing or inadequate test coverage
  - NO nit-level style comments — report only substantive findings that a maintainer should act on
  - For each finding: severity (critical / high / medium), the file path and line number (`file:line`), a brief description of the problem, and a concrete suggestion
  - Do NOT fix code, do NOT push, do NOT merge
  - When done, report all findings in a structured list via SendMessage back to this session (include severity, file:line, and description for each)
  - If there are no substantive findings, SendMessage back with a single "No substantive findings" message

### 6. Collect findings

Wait for the reviewer to complete. The reviewer will SendMessage its findings back to this session when done. Once the message arrives, capture the findings list.

If no SendMessage arrives within a reasonable time, note the timeout in the initiative and proceed to step 8 (update + close) without posting a review.

### 7. Post the review to GitHub

Post the review using the GitHub API. Build the inline comments from the reviewer's findings (one comment per finding at the reported `file:line`).

#### Handle the no-findings case

If the reviewer reported no substantive findings, post a clean review with no inline comments:

```bash
gh api repos/<owner>/<repo>/pulls/<pr-number>/reviews \
  --method POST \
  -f event=COMMENT \
  -f body="Automated review: no substantive findings."
```

#### Handle findings

For each finding, construct an inline comment. Collect them into a single review POST:

```bash
gh api repos/<owner>/<repo>/pulls/<pr-number>/reviews \
  --method POST \
  -f event=COMMENT \
  -f body="<one-sentence overall summary>" \
  -F 'comments[][path]=<file-path>' \
  -F 'comments[][line]=<line-number>' \
  -F 'comments[][body]=<severity>: <finding description>\n\n<suggestion>'
```

Repeat the `-F 'comments[]…'` flags for each finding. Post as `COMMENT` — not `APPROVE` and not `REQUEST_CHANGES`.

The review body is a single sentence summarizing the overall assessment (e.g. "Two high-severity findings related to error handling and one medium concerning missing test coverage.").

If the `gh api` call fails (e.g. a file:line reference does not correspond to a diff hunk), retry without the failing inline comment(s) and add their content to the review body instead, then note the fallback in the initiative.

### 8. Update the initiative

Write a brief note recording the outcome:

```bash
printf 'review-posted: PR #<pr-number> — <N> finding(s) posted as inline comments\n' \
  > /tmp/review-note-<id>.txt
ateam note <id> --file /tmp/review-note-<id>.txt
```

### 9. Close the initiative

```bash
ateam close <id> --reason "Review posted to PR #<pr-number>"
```

## Key constraints

- This skill does NOT create plans, spawn implementers/testers, open PRs, or manage epics.
- It is a single-purpose review orchestrator — one reviewer, one PR, one outcome.
- Uses `ateam` (not raw `bd -C`) for all global workspace operations.
- CARDINAL RULE: no work beads in the global workspace — all work beads belong in the project repo via plain `bd`.
- The reviewer subagent runs with `bypassPermissions` — its role guardrails (no push, no merge, no fix) are enforced by the reviewer agent definition, not by permission prompts.
