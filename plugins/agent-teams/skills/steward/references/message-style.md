# Steward message style — worked specimens

Specimens only. Every rule — the kind vocabulary, the word budgets, the required
orienting clause, the silence convention — lives in `SKILL.md`. If this file and
`SKILL.md` ever disagree, `SKILL.md` wins.

One section per outbound kind. Exactly one is calibrated: Eric ruled on
`gate-escalation` in round 1. The other seven are empty on purpose. Do not fill
them in from taste, inference, or a draft that has not come back from him — a
fabricated specimen that later contradicts his taste is worse than a blank slot.

## Calibrated

### gate-escalation

The rule this specimen illustrates:

> One line of what it buys. One line of what it costs. Your recommendation. ~88 words.

**BEFORE** — a message actually sent, 310 words. It opened:

> P2 is ready — PR #123: the durable recall you greenlit, built as ONE shared
> pipeline for both the steward and every DRI (exactly the constraint you set).
> No autonomy grant. Independent tester passed all 9 cases…

Only the opening is preserved; the remaining ~270 words are not reproduced here.
The opening alone is enough to see the failure. Three clauses in, it has spent
its words on status, on provenance ("exactly the constraint you set"), on a
scope disclaimer, and on a green test report — and it has not yet asked Eric
anything. The decision he was being asked to make is not in the opening at all.

That message also arrived in the topic with the initiative title mechanically
prepended by `telegram.go:167`, deleted in this same initiative. So the real
delta is larger than 310 → 88 words suggests: a line of machine restatement
disappears on top of the cut.

**AFTER** — specimen B, the one Eric picked:

> Merge PR #123 now, or make it cheaper first?
>
> What it buys: background agents keep your corrections after their context gets compacted.
>
> The cost: it re-sends those learnings — a few thousand tokens — on every turn of every background session. They're only lost at a compaction, so ~99% of the sends buy nothing. That repeats in every session, forever.
>
> The cheaper version sends them only after a compaction. It's filed and the hook it needs already ships in this PR.
>
> My rec: cheaper first, then merge.

**What changed:** the decision moved to the first line, phrased as the question
he answers; status, provenance and green-test reporting were cut entirely; what
survives is one line of what it buys, one line of what it costs, and a
recommendation.

**Where the orientation comes from.** Eric's own instruction is "assume I don't
remember the details," so the message has to name the concrete thing at stake in
his terms — here the first line plus the "what it buys" line do it, without
re-adding the title dump this initiative deletes. Do not stack a further
orienting sentence on top of those two; it blows the budget. And do not read any
single line above as *the* reason B won: the rejected specimen C carried the same
opening two lines.

## Not yet calibrated

Round 2 (`agent-teams-ubbz.9`) takes real sent messages back to Eric, one kind at
a time; his answers land in these slots. Until then they stay empty.

### hung-escalation

A session has stalled or gone quiet and Eric has to choose: unblock, restart, or
kill it.

_(No calibrated specimen yet — awaiting Eric.)_

### reply-ack

Confirms back to Eric that the answer he just gave was received and routed, and
names what it was routed to.

_(No calibrated specimen yet — awaiting Eric.)_

### direct-answer

Answers a question Eric asked the Steward directly — he already holds the
context, because he raised it.

_(No calibrated specimen yet — awaiting Eric.)_

### briefing-post

The cross-initiative sweep of what is running, posted into the briefing topic
rather than into any one initiative's topic.

_(No calibrated specimen yet — awaiting Eric.)_

### briefing-ack

A short reply on the briefing thread, acknowledging something Eric said there.

_(No calibrated specimen yet — awaiting Eric.)_

### anomaly-flag

Something looks wrong and Eric should know, but nothing is being asked of him —
no decision is gated on the flag.

_(No calibrated specimen yet — awaiting Eric.)_

### topic-open

The first message in a new initiative topic. Machine-authored, not
Steward-authored, and unchanged in this pass by Eric's decision — listed here
only so the eight-kind vocabulary stays complete.

_(No calibrated specimen yet — awaiting Eric.)_
