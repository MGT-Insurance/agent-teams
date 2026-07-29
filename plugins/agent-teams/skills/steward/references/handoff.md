# "I'm done with that one" — recognizing Eric's handoff

The fact being tracked: *Eric has looked at this PR, he is done, it is now on the team.* It comes into existence the instant he finishes looking, and nothing in GitHub records that instant — so it is **DECLARED, never derived** (`internal/verbs/external_review.go` §0–§2).

## 🚫 You never declare it

`ateam handoff` is Eric's verb. Run it ONLY when he has actually said so, in words, in a message you can point at.

Never on your own initiative — not from a merged-looking PR, not from an initiative that has gone quiet, not from GitHub. §0 of the contract forbids reading `reviewDecision` / `reviewRequests` / `latestReviews` at all: reviewer assignment is automated on the target repos, so those fields say nothing about whether Eric has looked. There is no signal you can compute that means "he is done."

There is also no sweep. A periodic *"these three PRs are sitting — which are you done with?"* was offered to Eric on 2026-07-29 and deferred: "second one for now." Do not build one, do not improvise one, do not re-propose it to him. This page teaches you only to RECOGNIZE what he volunteers.

## Where the declaration arrives: anywhere

He was offered a dedicated command and turned it down in favor of just saying it. Verbatim, 2026-07-29:

> you just say it whenever, in whatever thread you're already in, and it sticks

So it can land inside ANY envelope kind — a steward-reply about something else, a briefing reply, a DM, an @mention in General, a message in a closed initiative's topic, an unrouted reply. Mid-conversation, unprompted, in a thread about a different initiative, appended to an answer you asked for on another subject. There is no channel where it does not count and no prompt it waits for.

Phrasings he actually uses:

- "I've looked at it" / "I reviewed it"
- "done with that one" / "I'm done with that"
- "that's on the team now" / "handed that off"
- "waiting on <name>" / "not waiting on me anymore" / "nothing for me there"

NOT a declaration. Each of these has a real other meaning, so do nothing rather than stretch it:

- "I'll look at it later" / "haven't got to it yet" — the opposite of the declaration.
- "what did that review find?" — a question, answered from GitHub (references/pr-reviews.md).
- an answer to a pending gate ("yes, merge it", "go with the alternative") — that is a steward-reply verdict: route it to the DRI and write the ledger row. If he ALSO says he is done looking, do both; the two are different facts.
- acknowledging a briefing line, with nothing said about his own attention.

## Which initiative? — resolve it, and ask when you can't

He says "the midgard one" or a PR number, never a bead id. Resolve against `ateam execution-status`, which carries `id`, `title` and `pr` for every open initiative:

- **Exactly one match, reporting `REVIEWABLE`** — act. That is the only status the declaration changes.
- **More than one match** — ASK. One short question naming the candidates by title (never bead ids he would have to decode), then wait. Do not guess, do not batch, do not hand off both "to be safe."
- **No match** — say so plainly and ask which he means. Never invent an id.
- **The match already reports `AWAITING-EXTERNAL-REVIEW`** — it is already handed off. `ateam handoff` is idempotent, so re-running costs nothing, but a declaration aimed at an already-handed-off row usually means he is talking about a different one. Ask before assuming he repeated himself.

Ambiguity is worth a question here because the two errors are not symmetric. A question costs one line. A wrong handoff silently removes a PR from the only queue that tells Eric it needs him — no error, no notification, it simply stops appearing — and he finds out when someone asks why nobody merged it.

## Running it

```bash
ateam handoff <initiative-id>
```

Then confirm in ONE line: a status-change under SKILL.md §5 (35w) — what changed, named in his terms.

**No confirmation prompt first.** "It sticks" means the sentence he said IS the whole interaction; asking "shall I mark that one handed off?" before every declaration is exactly the ceremony he rejected. Asking is for a referent you cannot resolve — never for permission.

If the initiative carries no `gate:review` label, the verb still records the declaration and warns on stderr that the reported status will not change until a review gate exists. Pass that on in your one line rather than swallowing it: the label is written but inert.

## Reversal — always available

> "actually I still need to look at that" / "put it back" / "I'm not done with that one after all"

```bash
ateam handoff <initiative-id> --clear
```

Same recognition rules, same resolution rules — resolved against the rows reporting `AWAITING-EXTERNAL-REVIEW`. Treat it as first-class, not an edge case: a declaration he cannot take back is worse than no declaration.

## After it lands

The row reports `AWAITING-EXTERNAL-REVIEW` — not ours, not Eric's, and not idle. Nothing to nudge, nothing to brief. What that status means for your scan: references/operations.md.
