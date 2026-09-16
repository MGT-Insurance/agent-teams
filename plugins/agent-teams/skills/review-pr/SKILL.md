---
name: review-pr
description: "Lightweight PR review using agent-teams reviewer subagents. Use when invoked as /agent-teams:review-pr <initiative-id> [comment-reply], or when a background session is launched by route-pr-event for a review_requested, re_review, or comment_reply event. Self-detects re-reviews (prior review by this identity); the comment-reply argument switches to answering replies in review-comment threads."
---

You are the PR review orchestrator for one initiative: read the initiative, then either review the PR (check it out, spawn a reviewer subagent, post findings as inline comments) or, in comment-reply mode, answer replies in threads we participated in.

**THIS SESSION IS A SINGLE-PURPOSE REVIEW ORCHESTRATOR.**

Do NOT:
- Create work beads, plan decompositions, or ring epics.
- Spawn implementers, planners, or testers.
- Fix code, push commits, or merge PRs.
- Become a DRI or take on scope beyond posting the review.

## The `ateam` tool

`ateam` is on PATH as a prebuilt binary in the plugin's `bin/` (installed/verified by `/setup-agent-teams`). Call it bare everywhere shown here. One allowlist entry covers all subcommands: `Bash(ateam:*)`.

**CARDINAL RULE.** The GLOBAL workspace (ONLY via `ateam`) holds ONLY initiative-tracking beads and role memories. NEVER create a work bead there, and NEVER touch it with a raw `bd -C`.

## Steps

**Wake invariant (read first if resuming).** If this session is woken (mail,
resume, or heartbeat) believing the review is already done, re-run `ateam
show <id>` before trusting that. If OPEN, you were reopened — re-derive the
pending work from GitHub (comment-reply or re-review) and re-close
idempotently exactly as step 11 / comment-reply step 4 (note + `ateam
close`). NEVER end a turn with the initiative OPEN and no gate.

### 1. Parse the argument

First argument: an initiative id (e.g. `at-xxx`). Optional second argument
`comment-reply` selects that mode. If no id was given, stop and tell the
caller to re-invoke with one.

- No second argument → normal flow (steps 2–10).
- `comment-reply` → read the initiative fields (step 2), then follow the
  **Comment-reply mode** section at the end of this document and skip steps
  3–10 entirely.

### 2. Read initiative details

Run:

```bash
ateam show <id>
```

Parse these structured fields (one per line, `key: value`):

- `pr-number:` — the integer PR number
- `pr-repo:` — owner/repo (e.g. `acme-org/myrepo`)
- `pr-url:` — full https GitHub PR URL

If any is missing, stop and report which. Split `pr-repo` into `<owner>`/`<repo>` for later GitHub API calls.

### 3. Pin the reviewed commit

At the start of **every** review round, capture the PR head exactly once and
keep its full SHA as `<reviewed-sha>` for the whole round:

```bash
gh pr view <pr-number> --repo <owner>/<repo> --json headRefOid,baseRefOid
```

Record `.headRefOid` as `<reviewed-sha>` and `.baseRefOid` as `<base-sha>`.
They are immutable inputs, not branch names. If either is absent, stop, note
the failure, and close without posting. Do not silently refresh either value
later: if the PR advances before a review can be posted, restart the round
from this step.

### 4. Determine authorship

Compare the PR's author against the current GitHub identity:

```bash
gh pr view <pr-number> --repo <owner>/<repo> --json author,title
gh api user -q .login
```

Holds `.author.login` (compared below) and `.title` (step 11's completion
line needs it — empty if this call fails).

This drives **two independent decisions** with **opposite safe defaults** —
do not collapse them into one boolean:

- **Approve gate (step 10):** matching logins = self-review, never
  auto-approve (stay `COMMENT`). A failed check also defaults to
  self-review.
- **Design-commentary phrasing (step 8):** matching logins = the operator's
  own work, so state design/approach findings directly. Differing logins OR
  a failed check = someone else's work → curious-question phrasing. A
  failed check must NOT flip phrasing to "the operator's own" — the exact
  opposite default from the approve gate.

### 5. Detect re-review

Check whether the current identity has already reviewed this PR:

```bash
gh pr view <pr-number> --repo <owner>/<repo> --json reviews \
  -q '[.reviews[] | select(.author.login == "<our-login>")] | length'
```

(`<our-login>`: step 4's `gh api user -q .login` result; if that failed,
treat this as a first review.)

- **0** → first review. Proceed with the normal flow.
- **1+** → **re-review mode.** The author addressed our prior findings and
  review was re-requested. Fetch the prior findings:

```bash
gh api repos/<owner>/<repo>/pulls/<pr-number>/reviews    # review bodies
gh api repos/<owner>/<repo>/pulls/<pr-number>/comments --paginate   # inline review comments
```

Collect every finding from our most recent review (body + inline comments by
`<our-login>`) as file:line + original severity/label + description. Label
recovery: an inline comment prefixed `<severity>:` carries that severity
(`critical`/`high`/`medium`); an unprefixed body finding is a `question`.
Preserve it — step 10's gate keys off original severity, so a `question`
never blocks on re-review. Re-review mode replaces the reviewer instructions
in step 8 and changes the no-findings wording in step 10; checkout, diff,
posting mechanics, and close are unchanged.

### 6. Checkout the pinned PR code

Run:

```bash
gh pr checkout <pr-number>
git rev-parse HEAD
```

The checked-out `HEAD` must equal `<reviewed-sha>` exactly. If it does not,
the PR advanced: stop this round, note it, and restart from step 3. Do not use
the mutable worktree as citation evidence. If checkout fails (fork with a
non-writable ref or unavailable local repo), continue only with the immutable
API reads in steps 7–8.

### 7. Get the pinned diff

Fetch the diff for the captured SHAs, never a moving PR-head diff:

```bash
gh api repos/<owner>/<repo>/compare/<base-sha>...<reviewed-sha> \
  -H 'Accept: application/vnd.github.diff'
```

Capture the full output. If it is empty or fails, stop, note the error, and
close without posting. This diff and every cited source read must remain tied
to `<reviewed-sha>`.

### 8. Spawn the reviewer subagent

Spawn one `agent-teams-reviewer` subagent (`mode: bypassPermissions`, `run_in_background: true`). It self-fetches learnings on spawn — `ateam learnings reviewer`, step 1 of `roles/reviewer.md` — run BARE, never piped through `head`/`tail` (drops the fresh tier). SubagentStart can't do this for it (why: `references/mechanics-notes.md`).

Include in the reviewer's prompt:

- The PR URL (`<pr-url>`) and PR number (`<pr-number>`)
- `Reviewed commit: <reviewed-sha>` (the full captured `headRefOid`) and the
  full pinned diff from step 7
- **Whose work this is** — step 4's phrasing determination, stated explicitly: "This is the operator's own work" or "This is someone else's work" (frames design commentary). Pass the phrasing value, NOT the approve-gate value.
- For every reported `file:line`, require an immutable source read at
  `<reviewed-sha>` before citing it: `git show <reviewed-sha>:<path>`, or,
  when that object is unavailable locally, `gh api 'repos/<owner>/<repo>/contents/<path>?ref=<reviewed-sha>'`. A diff, search result, or mutable worktree
  alone is never citation evidence.
- The review instructions, verbatim from references/reviewer-prompt.md —
  normal mode by default, or its re-review variant if step 5 detected a
  prior review.

### 9. Collect findings

Wait for the reviewer's SendMessage with its findings list.

If none arrives within a reasonable time, note the timeout in the initiative and proceed to step 11 (record and close) without posting a review — cite `<pr-url>` (no review URL exists in this path) and note the timeout in the close reason.

### 10. Post the review to GitHub

Post the review using the GitHub API. Build the inline comments from the reviewer's findings (one comment per finding at the reported `file:line`).

**If the body is long or multiline, don't fight shell quoting — write it to a temp file and post its CONTENTS**, not its path: `gh pr review <pr-number> --body-file <file>`, or `gh api …/reviews -F body=@<file>`/`-F body=@-` (stdin). **Never** `-f body=@<file>`, `--raw-field body=@<file>`, or `--body @<file>` — those post the literal path text (why + the guard: `references/mechanics-notes.md`).

**Every successful review body opens with `## Summary` and contains its own
`Reviewed commit: <reviewed-sha>` line.** Use the full captured SHA, never an
abbreviation. Normal reviews then carry the reviewer's parity/overlap
enumeration and identifiability answer verbatim, even when "none". Re-reviews
carry the same risk-scaled audit record: its compact audit line when eligible,
or its full per-path rows otherwise.

#### Handle the no-findings case

If the reviewer reported no substantive findings, post:

```bash
REVIEW_URL=$(gh api repos/<owner>/<repo>/pulls/<pr-number>/reviews \
  --method POST \
  -f event=<APPROVE|COMMENT> \
  -f body="## Summary

No substantive findings.

Reviewed commit: <reviewed-sha>

<parity/overlap enumeration and identifiability answer, verbatim from the reviewer>" \
  --jq .html_url)
```

`event=APPROVE` — unless step 4 found a self-review, then `event=COMMENT`
(never auto-approve our own work).

#### Handle findings

Inline comments only work on diff lines. Two kinds belong in the body instead: parity/overlap findings referencing a consumer line **outside the diff**, and `question`-labeled findings (post verbatim, no severity prefix — a prefix would read as a defect). Everything else posts inline.

Build the `## Summary` list first — one line for **every** finding, including the `question` and out-of-diff parity/overlap findings that go in the body rather than inline: `` `file:line` — <severity|question> — <one clause> ``, no flowery language, no restating the finding's full detail. Then construct the inline comments (the diff-line subset only) and collect everything into a single review POST:

```bash
REVIEW_URL=$(gh api repos/<owner>/<repo>/pulls/<pr-number>/reviews \
  --method POST \
  -f event=COMMENT \
  -f body="## Summary

- \`<file:line>\` — <severity|question> — <one clause>
- \`<file:line>\` — <severity|question> — <one clause>

Reviewed commit: <reviewed-sha>

<parity/overlap enumeration and identifiability answer, verbatim from the reviewer>" \
  -F 'comments[][path]=<file-path>' \
  -F 'comments[][line]=<line-number>' \
  -F 'comments[][body]=<severity>: <finding description>\n\n<suggestion>' \
  --jq .html_url)
```

The Summary carries a `-` line for **every** finding; the `-F 'comments[]…'` flags cover only the inline (diff-line) subset. Post as `COMMENT`, never `REQUEST_CHANGES`; findings never create a merge warning, unresolved-at-merge mechanism, or other enforcement.

Every review POST above (every variant, including retry and re-review) must
append `--jq .html_url` into `REVIEW_URL`; step 11 cites it, falling back to
`<pr-url>` if empty.

**One-round event invariant:** assemble the complete body and every eligible
inline comment before one top-level `/reviews` POST. Never post one review per
finding or a partial review. A failed atomic POST may be retried only after
confirming it did not create a successful review event; preserve all body rows
in their original order and move rejected inline content into that same retry
body. An acknowledgement in an existing thread uses the review-comment reply
endpoint, never another top-level review event.

**Re-review mode:** the gate keys off each finding's ORIGINAL severity, not
its resolution. Only `critical`/`high`/`medium` AND `not addressed` forces
event=`COMMENT`. A `question` (or other non-blocking label) never forces
`COMMENT`, regardless of resolution — it was never blocking.

Post any blocking `not addressed` finding inline where its current line is in
the pinned diff. The body opens with `## Summary`, then the tally line (`Re-review: N
of M prior findings resolved`, N = non-blocking: `addressed`, `out of
scope`, every `question` — or `Re-review: all blocking findings resolved`
once none remain), then one line per PRIOR finding, same order as step 5:
`` `file:line` — <original label> — <addressed|out of scope|not
addressed>: <one clause> ``, then `Reviewed commit: <reviewed-sha>`. This
restatement covers every carried finding, in original order, body-only except
for eligible blocking inline comments. Append any new required F1–F5 defect
after the carried rows, with its severity and current anchor. The original
severity gate remains: any original `critical`/`high`/`medium` that is `not
addressed` forces COMMENT; any newly confirmed correctness defect also keeps
the event COMMENT. Otherwise event is APPROVE unless self-review. Use the same
one-round event invariant and capture `REVIEW_URL`.

### 11. Record the outcome and close the initiative

Closing is part of delivering the review — same turn as the post, one atomic
act with the outcome note. Re-reviews and comment replies spawn FRESH
sessions via route-pr-event (matches the CLOSED initiative and reopens it),
so nothing requires staying open. A review-delivered-but-open initiative is
a defect the hung-scan flags for hand-triage.

```bash
printf 'review-posted: PR #<pr-number> — <N> finding(s), event=<APPROVE|COMMENT>\nreviewed-sha: <reviewed-sha>\n' \
  > "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt"
ateam note <id> --file "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt"
ateam close <id> --reason "Review posted: <review-html-url>"

TITLE_SEG=" — <pr-title>"   # exactly "" if step 4's title lookup failed
printf 'Review complete · #%s %s%s\n%s' \
  "<pr-number>" "<repo>" "$TITLE_SEG" "<review-html-url>" \
  > "${CLAUDE_JOB_DIR}/tmp/review-notify-<id>.txt"
ateam notify reviews --file "${CLAUDE_JOB_DIR}/tmp/review-notify-<id>.txt"
```

`<review-html-url>` is `$REVIEW_URL` from step 10 — cite `<pr-url>` instead
if it's empty (POST failed, no fallback captured).

#### The completion line

Posts to the shared **Reviews** topic (one topic for all reviews). Text is
frozen — reproduce exactly (rationale: `references/mechanics-notes.md`).
`<repo>` is the **basename** (`midgard`, never `acme/midgard`). `TITLE_SEG`
is `" — "` (space, em dash **U+2014**, space) plus step 4's title, or
**empty string** if that failed — copy the separator from the block above,
don't retype it. Two lines: text, then the bare URL.

**Nothing else goes in it** — no finding count, no severity, no
`APPROVE`/`COMMENT` verdict. Do NOT pass `--to`; `--title` defaults to
`Reviews`. Post it **last, after the close** — a notify failure must never
strand the initiative open.

**Step-9 timeout path**: swap the wording — note `review-timeout: PR
#<pr-number> — reviewer subagent did not respond`, close `--reason "Review
not posted (reviewer timeout): <pr-url>"`. That note IS step 9's timeout
note; don't write a second one, and emit **no** completion line — no review
happened.

**Re-review rounds end the same way** — route-pr-event reopened this
initiative to run the round; once it posts, rerun this note+close+notify
step, citing the new review's URL in both places.

**Rare carve-out:** deliberately waiting on a same-session follow-up? Never
idle with the initiative open and gateless — raise a question gate naming
what it's waiting for:

```bash
ateam gate <id> --file <note> --kind=question
```

## Comment-reply mode

Someone replied in an inline review-comment thread this identity
participated in; pr-shepherd reopened this initiative to respond. Reply
text may arrive as mail via the normal hook flow — treat it as context if
present, but do NOT run `ateam mail inbox` yourself. Re-derive the work from
GitHub directly, every time (wake invariant above).

1. **Find the threads.** Fetch all inline review comments:

   ```bash
   gh api repos/<owner>/<repo>/pulls/<pr-number>/comments --paginate
   gh api user -q .login
   ```

   Group into threads by root id (`in_reply_to_id` if set, else `id`). Select
   threads where our login authored a comment AND someone else's later
   comment exists — those await a response.

2. **Respond to each thread — evaluate before agreeing.** Read the thread
   plus enough surrounding code/diff to judge the reply on its merits (`gh pr
   diff <pr-number>`, the file at the thread's `path` if needed). Run `gh pr
   checkout <pr-number>` first so the worktree matches the PR's head; if that
   fails, rely on `gh pr diff`/`gh api` file contents instead. The reply is a
   claim, not a verdict:

   - Verified correct → concede: "You're right — <what the code shows>."
   - The original finding still stands → hold position plainly, citing the
     evidence (file:line, the behavior the code exhibits).
   - A question → answer it concretely.

   Agreement without verification is a defect. Post exactly one reply per
   thread:

   ```bash
   gh api repos/<owner>/<repo>/pulls/<pr-number>/comments \
     --method POST \
     -f body="<the response>" \
     -F in_reply_to=<root comment id>
   ```

   No new findings, no new threads, no code changes, no review posting, no
   APPROVE/REQUEST_CHANGES events.

3. **Nothing to answer?** Reached only after step 1's fresh fetch, never
   from memory. If no qualifying threads exist (already handled, stale
   notification), note that, close, and skip the completion line — nothing
   happened to report.

4. **Note, close, and post the completion line:**

   ```bash
   printf 'comment-replies: PR #<pr-number> — <k> thread(s) answered\n' \
     > "${CLAUDE_JOB_DIR}/tmp/reply-note-<id>.txt"
   ateam note <id> --file "${CLAUDE_JOB_DIR}/tmp/reply-note-<id>.txt"
   ateam close <id> --reason "Comment replies posted: <pr-url>"
   ```

   Then run step 11's completion-line block unchanged. Two differences: URL
   is `<pr-url>` (no review posted); and it skips step 4, so fetch the title
   with `gh pr view <pr-number> --repo <owner>/<repo> --json title -q
   .title`.

## Key constraints

The steps above carry the constraints; this one is stated nowhere else.

- The reviewer subagent runs with `bypassPermissions` — its role guardrails (no push, no merge, no fix) are enforced by the reviewer agent definition, not by permission prompts.
