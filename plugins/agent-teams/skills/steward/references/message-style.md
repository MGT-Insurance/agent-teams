# Steward message style — worked specimens

Specimens only. Every rule — the kind vocabulary, the word budgets, the required
orienting clause, the silence convention — lives in `SKILL.md`. If this file and
`SKILL.md` ever disagree, `SKILL.md` wins.

One section per outbound kind. Two are calibrated: `gate-escalation` (Eric ruled
in round 1) and `status-change` (round 2 — see below; this kind didn't exist
until round 2 surfaced it). The other seven are empty on purpose. Do not fill
them in from taste, inference, or a draft that has not come back from him — a
fabricated specimen that later contradicts his taste is worse than a blank slot.

## Why the cross-kind rules read the way they do

**Silence and the four-hour heartbeat are one rule, not two.** Green gates being silent is only safe while something still proves the Steward is alive. Silence alone removes the only evidence of that — and detecting dead things is the Steward's own job, so a dead Steward is the failure nobody else is watching for. The heartbeat ships with the silence convention, never separately.

**Why LIVE verification gets its one line.** A report that unit or gate tests passed tells Eric nothing he could not assume. Someone actually driving the real thing and watching it work is evidence he cannot get any other way, which is why it is the single exception to green-is-silent.

**Why a plan-document URL goes in bare, not as markdown.** Telegram's `sendMessage` is called with no `parse_mode`, so a markdown link renders as literal text and a bare URL is what Telegram auto-links into something tappable on a phone. That is also why the URL is reproduced verbatim, never truncated, and never counted against the word budget.

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

### status-change

**Discovered in round 2, not a pre-existing slot.** Every other kind is triggered
by something arriving — an envelope, a scan result. `status-change` is the one
the Steward decides to send on its own: a thing of Eric's changed state and
nothing is being asked of him. That case had no home, and these three messages
are the evidence it needed one.

**Budget: 35 words** — not T-ACK's 25. Derived from the two as-is specimens
below, which Eric approved at this length. Do not correct it down to 25 later;
it is a deliberately separate allowance for this kind only.

**AFTER — specimen 1** (34 words, landed as-is):

> The scheduling A/B is on hold — the DRI is checking Claude Code's built-in
> schedulers first, then re-raises it once with the full comparison. (A duplicate
> session spawned during the handoff; stood down, nothing touched.)

This folds an anomaly detail (the duplicate session, stood down) into the
status-change instead of sending a separate `anomaly-flag`. That composition is
deliberate — one situation gets one message, not two.

**AFTER — specimen 2** (32 words, RE-CUT):

> Merged: #119 — eager Telegram topics, hung-session detection with an escalation
> ladder, and human-friendly topic titles. Plus your single-writer fix: the
> hung-state race is gone by construction, no lock needed. Version 0.42.27.

**Why this is the worked example of the no-back-references rule.** The first cut
of this specimen was 25 words: "Merged: #119. All three of your asks are on
main, plus your single-writer fix — the race is gone by construction, no lock
needed. Version 0.42.27." Eric rejected it: "your N asks" sends him back to find
out what those asks were — the exact searching this initiative exists to remove.
Naming eager topic creation, hung-session detection, and human-friendly titles
costs seven more words. A short message he has to research is worse than a
longer one he does not.

**AFTER — specimen 3** (30 words, landed as-is):

> A new initiative for the wrong-machine reply bug is parked and won't move
> until you release it. Worth knowing: the @mention change that just merged may
> have already fixed it.

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

The first message in a new initiative topic. Machine-authored at
`dispatch.go:331`, not Steward-authored, and not shortened this pass by Eric's
decision (§F) — it is the one kind absent from SKILL.md §5's table, listed here
only so the nine-kind vocabulary stays complete and so machine prose is never
mistaken for Steward prose. Nothing about it is the Steward's to compose.

_(No calibrated specimen yet — awaiting Eric.)_
