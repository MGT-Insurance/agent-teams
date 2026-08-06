# Retrieving a past PR review

The human asks what a review found, or asks for a deeper one, after seeing a line in the shared Reviews topic. This file is the whole answer path: read the findings back from GitHub, never from beads.

## What the Reviews topic lines carry — and what they deliberately omit

Two lines per review, frozen in `internal/verbs/steward_seams.go` (`ReviewsStartLineFormat`, :997, and the completion line as a doc comment just below it). Rendered:

```
Review started · #4408 midgard — feat(mithril/aragorn): migrate /transactions to Aragorn - BUG-874
https://github.com/MGT-Insurance/midgard/pull/4408
```

```
Review complete · #4408 midgard — feat(mithril/aragorn): migrate /transactions to Aragorn - BUG-874
https://github.com/MGT-Insurance/midgard/pull/4408#pullrequestreview-1234567
```

PR number, repo basename, PR title, URL. Nothing else — no finding count, no severity, no APPROVE/COMMENT verdict. That omission is Eric's own call (agent-teams-p9dm.7), verbatim:

> Most of the time I don't care about the review content. I just want to know a review happened, and then if the PR title intrigues me I'd like to ask the steward for more info, or maybe even to dispatch a more focused review.

So the title is the entire basis on which he decides to dig, and "ask the steward for more info" is a designed-for follow-up, not an off-script request. The ask can arrive as any envelope kind — a direct message, a briefing reply, an unrouted reply from the Reviews topic itself (which has no owning initiative, so the relay cannot place it). Answer it wherever it lands; do not treat the sparse line as all there is.

## Retrieving the findings — from GitHub, always

The findings survive in full on GitHub. Two endpoints, and you need both:

```bash
# 1. Review bodies — one summarizing sentence per review, plus any finding
#    that could not be attached to a diff line.
gh api repos/<owner>/<repo>/pulls/<pr-number>/reviews \
  --jq '.[] | "\(.user.login) \(.state)\n\(.body)\n"'

# 2. Inline review comments — where the substance actually lives.
gh api repos/<owner>/<repo>/pulls/<pr-number>/comments --paginate \
  --jq '.[] | "\(.user.login) \(.path):\(.line // .original_line)\n\(.body)\n"'
```

**Run both.** The reviews endpoint alone will make a substantive review look thin: `review-pr` posts one comment per finding at its reported `file:line` and sets the review body to a single overall sentence (`skills/review-pr/SKILL.md`:191-202). Verified live on `MGT-Insurance/midgard#4408` — the reviews endpoint returned four bodies, but the critical finding on that PR (server actions shipped with no auth wrapper) exists only as an inline comment.

Both endpoints return every review by every author, ours and the humans'. That is a feature, not noise: "what did that review find" usually means the whole conversation on the PR, not just our bot's part. Attribute by `.user.login`; ours is whatever `gh api user -q .login` returns.

### Resolving the repo

The topic line renders the repo BASENAME (`midgard`), not `owner/repo` — but the URL on the line's second line carries the owner. Read it off the message rather than reconstructing it: `https://github.com/MGT-Insurance/midgard/pull/4408` gives owner `MGT-Insurance`, repo `midgard`, PR `4408`.

When the human names a bare basename with no URL in reach ("what did that midgard review say?"), assume owner `MGT-Insurance` — every repo in play is under that org, this one included (`git@github.com:MGT-Insurance/agent-teams.git`) — but confirm rather than firing a request at a guessed path:

```bash
gh repo view MGT-Insurance/<basename> --json nameWithOwner -q .nameWithOwner
```

If that 404s, `gh search repos <basename> --owner=MGT-Insurance --json fullName`. Near-miss names are real (`midgard` and `midgard-e2e` both exist), and each repo has its own PR-number space, so a wrong owner or a wrong sibling repo returns a confidently wrong review for a completely different PR. If neither command resolves it, ask the human for the URL — one short question beats an answer about the wrong repo.

### What to send back

A direct-answer, under SKILL.md §5: the answer as the first word, sized to the question, never over T-DECIDE, no preamble. Summarize — do **not** paste raw bodies into Telegram. The comment bodies above run to thousands of characters each; one dump wrecks the topic and buries the point. Name the findings that matter in the human's terms, keep the severity words the reviewer used, and let the PR URL carry anything deeper.

## NOT via `ateam` — do not go hunting for the initiative

Every review does have an initiative bead, and `ateam show <id>` still prints its note after it closes. Ignore that path anyway: nothing maps a PR number to the id.

- `ateam list` is hardcoded to open initiatives — `ctx.BD.Run("list", "--status=open")`, `internal/verbs/query.go`:52, on a `listKong` struct with zero fields, so there is no flag that changes it. A review initiative closes as soon as the review posts, so the one you want is exactly the one `list` cannot see.
- The two functions that DO map a PR to an initiative — `matchInitiative` (`internal/verbs/route_match.go`:89) and `matchClosedReviewInitiative` (:175) — are internals of `route-pr-event`, not exposed as a verb.
- Nothing else in the verb inventory searches by PR.

This is deliberate, not a gap to file as a bug. GitHub answers the question better than the bead does: every review by every author, full text, no id resolution needed. The bead note is a one-line tally by design.

## Dispatching a deeper review

Eric's second option — "maybe even to dispatch a more focused review" — already exists as a skill. Nothing new to build:

```
/agent-teams:dispatch-review-pr https://github.com/<owner>/<repo>/pull/<pr-number>
```

Pass the full URL, or `owner/repo#<pr-number>`. **Never a bare number.** The bare form infers the repo from the current directory's git remote (`skills/dispatch-review-pr/SKILL.md`), and this session's cwd is `~/.agent-teams/steward/session` — inside the memory workspace, whose origin is `agent-teams-memory`. A bare number there resolves silently to the wrong repo.

Running it is a hand-off, not a review: it parses the reference, registers a review initiative, and launches a background session that does the reading. You still never read the diff or form an opinion on the code yourself.

**Authority is unchanged (SKILL.md §3).** Deciding a PR deserves another look is the human's call, never yours. If a finding warrants it, recommend it in one clause and wait for him to say go.
