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

## Cross-kind rule demonstrations

These three pairs demonstrate the four cross-kind rules (SKILL.md §5) against real
sent messages. They are NOT calibrated per-kind specimens awaiting Eric's ruling —
do not read them as filling any empty slot below. Every BEFORE is quoted verbatim
from a real sent message; every AFTER is new prose written to obey the rules,
built only from facts already present in its BEFORE.

### Bare ids, under budget

**BEFORE** — a `briefing-post`, 134 words, well under its 176-word budget:

> Quiet window — nothing new from me, and nothing is wrong. No initiative is
> executing: at-jno7 is waiting, at-xm7q and at-ig53 idle. Everything is parked
> on you.
>
> Two PRs open: **#140** (at-jno7, ready — merge then restart the relay) and
> **#134** (at-ig53, still below main at 0.48.0).
>
> Machine-local decisions waiting: at-jno7's merge, at-xm7q's plan — that one
> carries the finding that passing `name:` to a spawn silently drops the role
> definition, including the never-push rule, across ~66% of July's role spawns
> — and at-ig53's condense plan.
>
> Twelve initiatives now show on `human-list`, several synced in from your MGT
> laptop (at-nk90, at-7q4j, at-6nj, at-3ed, at-y7l9, at-o0v). I don't have
> visibility into those sessions from here, so I'm not tracking their state —
> flagging only that the queue is longer than what this machine is running.

**AFTER** (117 words, rule 1 did the cutting — this message was already under
budget, so a budget alone never reaches it; rule 2's ban on restating and
pointing-back binds at any length, which is the half that does):

> Quiet window — nothing new from me, nothing wrong. Nothing is executing: the
> PR #140 merge decision (at-jno7) is waiting on you, the spawn role-drop plan
> (at-xm7q) and the condense plan (at-ig53) are idle.
>
> Two PRs open: #140 is ready — merge, then restart the relay; #134 is still
> below main at 0.48.0.
>
> Also waiting on you: the spawn role-drop finding — passing name: to a spawn
> silently drops the role definition, including the never-push rule, on about
> two-thirds of July's role spawns.
>
> Twelve initiatives now show on human-list, six synced in from your MGT
> laptop. I have no visibility into those from here — flagging only that the
> queue is longer than what this machine runs.

Every bare id now leads with the work it names; where no name exists (the six
synced-in initiatives), the id is dropped rather than dumped bare. The inline
bold on `#140`/`#134` is also gone (rule 3) — plain prose, not decoration.

### Verification receipts

**BEFORE** — a `gate-escalation`, 385 words against an 88-word budget. It opened:

> **PR #140 reworked and ready — recommend merge.**
> https://github.com/MGT-Insurance/agent-teams/pull/140
>
> It cleared its gate on your answer; the record confirms it, so no concern
> from me there.
>
> **The thing I was most worried about is clean.** I flagged that resolving
> the three prose conflicts carelessly would silently revert #144 and put both
> SKILL.md files back over the truncation cliff. I checked it directly rather
> than take its word: of the substantive lines #144 deleted — 54 in
> dri/SKILL.md, 66 in steward/SKILL.md, 26 in gate-protocol.md — **zero
> reappear on this branch.** …

The remaining ~300 words are not reproduced here; they are a five-item green
checklist (**Everything else I checked holds:** MERGEABLE/CLEAN, version
bump, binaries rebuilt, merge-detection removal, diff size) followed by an
operational caveat and the recommendation.

**AFTER** (74 words, rules 2 and 3 did the cutting):

> Merge PR #140 and restart the relay now, or later?
>
> What it buys: the condensation stays fully intact, and the Reviews-topic
> reply bug is fixed.
>
> What it costs: you have to restart the relay by hand for the fix to take
> effect — skip it and replies stay broken exactly as now, the same trap as
> #143.
>
> My rec: merge, then restart. Alternative: merge now, restart later if you'd
> rather batch it with at-xm7q.

The five-item green checklist is gone entirely — it set the Steward's own
confidence to recommend merging, never Eric's message. What survives is the
one thing that isn't assumable: the relay needs a manual restart, and skipping
it reproduces the exact bug #143 already burned him on. The plan URL is cut
too — nothing here is a plan Eric needs to open to decide.

### Mechanism before effect

**BEFORE** — a message to at-zrjt's topic, 641 words, reaching the actual ask at
word 341, under bold headers naming a hook filename, a `SubagentStart` event,
and four `file:line` citations before ever stating what breaks:

> **4 — what the "something else" is**
>
> Two things run the identical command.
>
> First, the hook: `hooks.json` registers `SubagentStart` →
> `subagent-prime-learnings.sh`, which runs `ateam pull` then `ateam learnings
> <role>`.
>
> Second — and this is the "something else" — the agent's own .md body. Step 1
> of each agent's numbered workflow literally says "Read role learnings: `ateam
> learnings <role>`". It's at implementer.md:12, planner.md:14, reviewer.md:12,
> tester.md:12. …

**AFTER** (84 words, rule 4 did the cutting):

> Widen this initiative's scope by one file — the hook that primes subagent
> memory — or file it separately?
>
> It buys: a subagent currently looks the same whether it loaded role memory
> or not. One line of shell in that hook fixes that.
>
> It costs: touching one file outside the scope you gave this initiative
> (agents/ + dri/ + steward/ only).
>
> My rec: widen it here — cheaper than a bead that waits. Alternative: keep
> scope clean, file separately, leave the duplicate call as backstop.

The hook registration, the agent-file line numbers, and the four-way failure
enumeration are all mechanism — none of it is what Eric needs to answer the
actual question, which is a scope call. What would be lost: if Eric later
wants to know exactly *why* the hook can fail silently, this AFTER doesn't
carry that — it's recoverable by asking, which is the point of rule 2.

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
a time; his answers land here. Until then, empty: `hung-escalation`,
`reply-ack`, `direct-answer`, `briefing-post`, `briefing-ack`, `anomaly-flag` —
see SKILL.md §5's table for what triggers each and what Eric must do.

### topic-open

The first message in a new initiative topic. Machine-authored at
`dispatch.go:376` (`createInitialTopic`), not Steward-authored, and not
shortened this pass by Eric's decision (§F) — it is the one kind absent from
SKILL.md §5's table, listed here only so the nine-kind vocabulary stays
complete and so machine prose is never mistaken for Steward prose. Nothing
about it is the Steward's to compose.

_(No calibrated specimen yet — awaiting Eric.)_
