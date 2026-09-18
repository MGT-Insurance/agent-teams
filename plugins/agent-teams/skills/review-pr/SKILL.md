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

**Wake invariant.** On a mail, resume, or heartbeat that says review is done,
run `ateam show <id>`. If it is OPEN, re-derive GitHub work and re-close it
idempotently under step 11 / comment-reply step 4. NEVER end OPEN without a
gate.

### 1. Parse the argument

First argument: initiative id (e.g. `at-xxx`). Optional `comment-reply`
selects that mode. If no id was given, stop and request one.

- No second argument → normal flow (steps 2–11).
- `comment-reply` → read step 2, follow **Comment-reply mode**, skip 3–10.

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
They are immutable inputs, not branch names. If either is absent, note and
close without posting. If the PR advances before posting, restart here; never
silently refresh either value.

### 4. Determine authorship

Compare the PR's author against the current GitHub identity:

```bash
gh pr view <pr-number> --repo <owner>/<repo> --json author,title
gh api user -q .login
```

This yields `.author.login` and `.title` (empty on failure for step 11).

This drives **two independent decisions** with **opposite safe defaults** —
do not collapse them into one boolean:

- **Approve gate (step 10):** matching or failed identity = self-review;
  stay `COMMENT`.
- **Design commentary (step 8):** matching = operator's work, state findings
  directly. Different or failed identity = someone else's work, use curious
  questions. Its failed-check default is the opposite of the approve gate.

### 5. Detect re-review

Check whether the current identity has already reviewed this PR:

```bash
gh pr view <pr-number> --repo <owner>/<repo> --json reviews \
  -q '[.reviews[] | select(.author.login == "<our-login>")] | length'
```

(`<our-login>`: step 4's `gh api user -q .login` result; if that failed,
treat this as a first review.)

- **0** → first review. Proceed with the normal flow.
- **1+** → **re-review mode.** Fetch prior findings:

```bash
gh api repos/<owner>/<repo>/pulls/<pr-number>/reviews    # review bodies
gh api repos/<owner>/<repo>/pulls/<pr-number>/comments --paginate   # inline review comments
```

Collect our latest body and inline findings as file:line, original label, and
description. A `<severity>:` inline prefix is `critical`/`high`/`medium`; an
unprefixed body finding is `question`. Preserve it: step 10 gates on original
severity, so `question` never blocks. Re-review changes step 8 instructions
and step 10 no-findings wording only.

### 6. Checkout the pinned PR code

Run:

```bash
gh pr checkout <pr-number>
git rev-parse HEAD
```

`HEAD` must equal `<reviewed-sha>` exactly; otherwise note and restart at 3.
Never cite the mutable worktree. If checkout fails, use only immutable API
reads in steps 7–8.

### 7. Get the pinned diff

Fetch the diff for the captured SHAs, never a moving PR-head diff:

```bash
gh api repos/<owner>/<repo>/compare/<base-sha>...<reviewed-sha> \
  -H 'Accept: application/vnd.github.diff'
```

If empty or failed, note and close without posting. The diff and cited reads
stay tied to `<reviewed-sha>`.

### 8. Spawn the reviewer subagent

Spawn one `agent-teams-reviewer` (`mode: bypassPermissions`,
`run_in_background: true`). It runs its own bare `ateam learnings reviewer`
(step 1 of `roles/reviewer.md`); do not pipe it through `head`/`tail`.

Include in the reviewer's prompt:

- PR URL/number; `Reviewed commit: <reviewed-sha>` (full `headRefOid`); and
  the full step-7 diff.
- Step 4's explicit phrasing value — "This is the operator's own work" or
  "This is someone else's work" — not its approve-gate value.
- Before every `file:line`, an immutable `<reviewed-sha>` read: `git show
  <reviewed-sha>:<path>`, or unavailable locally, `gh api
  'repos/<owner>/<repo>/contents/<path>?ref=<reviewed-sha>'`. A diff, search,
  or worktree alone is never evidence.
- The verbatim normal/re-review instructions from `references/reviewer-prompt.md`.

### 9. Collect findings

Wait for the reviewer's SendMessage with its findings list.

On timeout, note it and perform step 11 without posting; cite `<pr-url>` and
include timeout in the close reason.

### 10. Post the review to GitHub

Post through GitHub API; build one inline comment per reported `file:line`.

For a long/multiline body, post temp-file **contents**, not its path: `gh pr
review <pr-number> --body-file <file>` or `gh api …/reviews -F
body=@<file>`/`-F body=@-`. Never `-f body=@<file>`, `--raw-field
body=@<file>`, or `--body @<file>` (they post the path).

**Every successful review body opens with `## Summary` and contains its own
`Reviewed commit: <reviewed-sha>` line.** Use the full SHA. Normal and
re-review bodies carry the reviewer's risk-scaled parity/overlap and
identifiability record verbatim: compact line when eligible, otherwise full
per-path rows.

#### Recheck the PR head immediately before posting

Immediately before every top-level `/reviews` POST, fetch the current head and
compare it exactly to this round's full `<reviewed-sha>`:

```bash
if ! CURRENT_HEAD=$(gh pr view <pr-number> --repo <owner>/<repo> --json headRefOid --jq .headRefOid); then
  printf 'review-round-restarted: PR #<pr-number> — reviewed-sha: <reviewed-sha>; current-sha: lookup failed\n' \
    > "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt" || exit 1
  ateam note <id> --file "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt" || exit 1
  exit 0
fi
if [ "$CURRENT_HEAD" != "<reviewed-sha>" ]; then
  printf 'review-round-restarted: PR #<pr-number> — reviewed-sha: <reviewed-sha>; current-sha: %s\n' "$CURRENT_HEAD" \
    > "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt" || exit 1
  ateam note <id> --file "${CLAUDE_JOB_DIR}/tmp/review-note-<id>.txt" || exit 1
  # Do not POST; discard this round and restart at step 3.
  exit 0
fi
```

Run immediately before the selected POST, with no intervening reviewer work.
On failed/different lookup, including retry, record this durable restart note,
discard body/comments, and restart at 3 with a new SHA. If note-file writing
or `ateam note` fails, exit nonzero: do not silently restart or POST. This
binds the event, not only its body stamp.

#### Handle the no-findings case

If the reviewer reported no substantive findings, post:

```bash
REVIEW_URL=$(gh api repos/<owner>/<repo>/pulls/<pr-number>/reviews \
  --method POST \
  -f commit_id=<reviewed-sha> \
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

Only diff lines support inline comments. Put out-of-diff consumer
parity/overlap and `question` findings in the body (verbatim, no severity
prefix); put the rest inline.

Build `## Summary` first: one terse `` `file:line` — <severity|question> —
<one clause> `` line for **every** finding, including body-only findings. Then
collect the inline diff-line subset into one POST:

```bash
REVIEW_URL=$(gh api repos/<owner>/<repo>/pulls/<pr-number>/reviews \
  --method POST \
  -f commit_id=<reviewed-sha> \
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

Every review POST — retry, re-review, fallback included — passes the exact
head-equality gate, binds `-f commit_id=<reviewed-sha>`, and stores
`--jq .html_url` in `REVIEW_URL`; step 11 falls back to `<pr-url>`. A reply
is not a top-level POST: do not add `commit_id` to that reply endpoint.

**One-round event invariant:** assemble the complete body and every eligible
inline comment before one top-level `/reviews` POST. Never post one review per
finding or a partial review. Retry an atomic failure only after confirming no
successful event; rerun the head-equality gate immediately before retry and
preserve `commit_id=<reviewed-sha>`. Keep body rows ordered and move rejected
inline content into that retry body. A thread acknowledgement uses the
review-comment reply endpoint, never another top-level review event.

**Re-review mode:** the gate keys off each finding's ORIGINAL severity, not
its resolution. Only `critical`/`high`/`medium` AND `not addressed` forces
event=`COMMENT`. A `question` (or other non-blocking label) never forces
`COMMENT`, regardless of resolution — it was never blocking.

Post blocking `not addressed` findings inline at their current pinned-diff
line. Body: `## Summary`; tally (`Re-review: N of M prior findings resolved`,
where N is `addressed`, `out of scope`, or any `question`, or `Re-review: all
blocking findings resolved`); then one line per PRIOR finding, same order as
step 5: `` `file:line` — <original label> — <addressed|out of scope|not
addressed>: <one clause> ``, then `Reviewed commit: <reviewed-sha>`. The
restatement covers every carried finding, in original order; append new F1–F5
defects with severity/current anchor. Original `critical`/`high`/`medium` +
`not addressed`, or a new correctness defect, forces COMMENT; otherwise
APPROVE unless self-review. Use the one-round invariant and `REVIEW_URL`.

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
