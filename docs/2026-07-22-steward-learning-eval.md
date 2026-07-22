# Can the Steward learn to make Eric's decisions? — Evaluation

**Initiative:** at-f307 · **Date:** 2026-07-22 · **Repo:** `agent-teams`

> **The ask (Eric, verbatim):** "Evaluate the ability of the steward to learn to be like me. Over time I want the steward to make more and more of the decisions I would. Analyze how well this currently happens and what we should improve. For example, I'm not sure if the steward is storing its learnings anywhere or recalling them."

---

## TL;DR

**The binding constraint is CAPTURE, not recall.** The steward recalls its learnings fine at startup (two independent paths load them). What's broken is the other end: when Eric *corrects* a recommendation — the single most valuable learning signal — the steward writes only a `corrected` boolean to its ledger and moves on. It never records *what Eric actually decided*. You cannot recall what was never written.

By explicit v1 design the steward also has **zero autonomous authority** and **no feedback loop**: nothing reads the ledger to change behavior. So today the steward can *score* how often it agrees with Eric, but it has no mechanism to *become* more like him.

**This PR ships the clear-cut quick-win** (Eric's flagged gap): wire correction → learning capture, and add decision-time recall. It deliberately does **not** grant autonomy — that's the larger, gated build, filed as beads below for Eric's decision.

---

## The intended learning loop, and where it breaks

For the steward to "learn to make Eric's decisions," three things must work in sequence:

```
  CAPTURE ────────────▶ RECALL ────────────▶ ACT
  store what Eric        resurface the         make (or better-
  actually decided       relevant past         recommend) the
  + the situation        decision at a          call Eric would
                         similar gate
```

| Stage | State today | Verdict |
|---|---|---|
| **CAPTURE** | Ledger stores `accepted\|corrected` only — never Eric's actual decision. Learnings are generic ("as they form"), **not** correction-triggered. | ❌ **Broken** — the real break |
| **RECALL** | Startup loads `ateam learnings steward` + ledger stats (SKILL.md:42-45); `bd prime` also dumps them. But nothing recalls at *decision* time, and a long-running steward compacts repeatedly. | ⚠️ **Fragile** — startup-only |
| **ACT** | By explicit design, no ledger content changes behavior: "Escalate every gate to Eric regardless of your ledger stats" (SKILL.md:200-202). | ⛔ **Absent by design** (v1) |

The loop is open at both ends. RECALL being fragile barely matters while CAPTURE writes nothing worth recalling and ACT is disabled by design.

---

## Q1 — STORAGE: does the steward capture what it learns?

**The ledger.** `<workspace>/steward/ledger.jsonl`, one JSON line per escalated decision (`internal/verbs/steward_seams.go:72,99-103`). Schema — exactly five fields (`steward_seams.go:651-657`): `ts`, `category`, `initiative`, `recommendation`, `verdict`.

- `category` ∈ {plan-approval, scope-call, merge-approval, design-fork, unblock-action} (`steward_seams.go:617-623`)
- `verdict` ∈ **{accepted, corrected} only** (`steward_seams.go:639-642`)

**Written manually, at verdict time.** No escalation-path code auto-records; the only writer is the `ateam steward ledger record` verb (`internal/verbs/steward.go:265-300`), invoked because SKILL.md tells the steward to (SKILL.md:83-89, 157-159). An escalation Eric never answers produces **no ledger entry at all**.

**The critical gap — the schema cannot represent Eric's decision.** On a `corrected` verdict the record stores the fact of correction plus *the steward's own* recommendation. There is **no field for what Eric actually decided**. The ledger records "the steward was wrong here," never "and here's what right looked like." **It is an accuracy tally, not learnable material.**

**Role learnings** (`ateam learn steward`) store as bd memories, default `fresh` tier, 900-byte cap (`internal/verbs/write.go:71-94`). But the trigger is vague — "contribute learnings as they form" (SKILL.md:167) — with **no instruction that a correction must produce a learning**, and no hook enforcing any write. The highest-signal moment was wired to write nothing learnable.

---

## Q2 — RECALL: does the steward recall its learnings?

**Startup: yes, via prose (two paths).** SKILL.md:42-45 instructs the steward to run `ateam learnings steward` + `ateam steward ledger stats` at startup. Separately, the beads plugin's `bd prime` dumps all global-workspace bd memories (the steward's cwd resolves `~/.agent-teams/.beads`), so `steward:*` learnings surface there too. So Eric's worry "not recalling them" is **half right**: startup recall works, but only incidentally and only at startup.

**No dedicated hook.** No SessionStart hook injects steward learnings or ledger stats (`hooks/hooks.json:3-45`); `ateam prime` filters to `user:` only (`internal/verbs/query.go:467,477,488`). SubagentStart's `subagent-prime-learnings.sh` matches `implementer|planner|tester|reviewer` — excluding both `dri` and `steward` (`hooks.json:83`) — and can't fire for the steward anyway, since it launches as a top-level `claude --bg` session, not a subagent (SKILL.md:28). Recall is 100% prose-dependent.

**Decision-time: no.** The gate handler (SKILL.md:57-71, pre-PR) enriched with `ateam show` + `execution-status` only — it did not re-pull learnings at decision time. For a persistent singleton that runs for days and compacts repeatedly, whatever loaded at startup is long gone by the time a relevant gate arrives.

**No case-wise read-back.** The ledger has two subverbs only: `record` and `stats` (`steward.go:40-41`). `stats` emits per-category counts (`steward.go:339-405`). There is **no verb that reads back individual records**, so "last time a merge-approval like this came up, what did Eric decide?" is impossible in both data (no decision field) and API (aggregates only).

**Compaction: no recovery.** `compact-recovery.sh` only re-injects when `$PWD` matches an OPEN initiative's `worktree:` (`compact-recovery.sh:26-33`). The steward's cwd isn't any initiative's worktree → silent no-op. After a compaction the persistent steward gets nothing back.

---

## Q3 — FEEDBACK LOOP: does "Eric corrected me" change behavior?

**No. The ledger is functionally write-only.** Its sole consumer is `ateam steward ledger stats` (`steward.go:339-407`), and that verb's sole caller is the SKILL.md:43 startup context-load — it prints a stat table into the LLM's prompt. Grep confirms nothing anywhere reads `accept_rate` to make a decision; no dashboard/hook references the ledger at all. This is stated by design: "No confidence graduation … Escalate every gate to Eric regardless of your ledger stats" (SKILL.md:200-202).

So there are two disconnected, entirely manual surfaces — the ledger (counts only, injected as an advisory table) and role learnings (free-form, untriggered) — and **neither reads back into a decision.** There is no durable behavior-change loop. v1 deliberately defers it.

---

## Q4 — GAP TO GOAL, and what's already filed

**In mechanism terms, three things must exist that today do not:**

1. **Store Eric's actual decision** in a recallable, situation-keyed form (today: discarded — only a boolean).
2. **Recall it at decision time** (today: aggregate counts at startup only).
3. **A graduation rule** that flips a category from "escalate" to "decide" once the track record clears a bar.

**Already filed toward the goal:**

| Bead | Status | What it is | Relation |
|---|---|---|---|
| `agent-teams-7ew5` | OPEN (epic) | This evaluation's container | Root |
| `agent-teams-e3mq.11` | OPEN, **gated** | Confidence graduation — per-category autonomy from ledger stats | **The graduation rule (#3).** Engineering blocker already done; gated on an Eric authority decision |
| `agent-teams-6rru.8` | OPEN, P4 | Autonomous stuck-DRI respawn once ledger accept-rate clears a bar | Narrow first instance of #3 |
| `agent-teams-e3mq.12` | OPEN, gated | Richer "session going wrong" judgment | Improves *what* it can detect; complementary |
| `agent-teams-e4r1` | OPEN | Durable, role-scoped learnings injection (survives compaction) | Foundational for robust RECALL (#2) — same gap hits the steward |
| `agent-teams-u71p` (+.5) | OPEN | Track how often each *learning* is applied (impact signal) | Adjacent — instruments learnings, not the ledger |

**The missing middle nobody has filed:** capturing Eric's actual decision content and recalling it case-wise (#1 + case-wise #2). The graduation *rule* is filed; the *storage+recall substrate that would make graduation safe* is not. That's the gap this evaluation surfaces — filed as beads below.

---

## What this PR ships (the quick-win)

Prose-only changes to `skills/steward/SKILL.md` — no code, no autonomy grant, v1 stance preserved:

1. **Capture on correction (CAPTURE fix).** The steward-reply handler now requires, on every `corrected` verdict, distilling *what Eric decided* into a role learning (RULE/TRIGGER/APPLY keyed on the situation) — not just the ledger tally. This is the missing link that turns a correction into a better next recommendation.
2. **Decision-time recall (RECALL fix).** The gate handler now runs `ateam recall steward <keywords>` before composing a recommendation, so a compacted long-running steward pulls the relevant past lesson at decision time instead of relying on a stale startup load.
3. **§6 pointer** making correction the primary learning trigger.

This closes the loop *within the layer the steward already operates in* (prose-instructed behavior). It intentionally stops short of granting autonomy.

---

## Prioritized roadmap (gated — needs Eric)

**P1 — make CAPTURE structured (the missing middle).** Extend the ledger to record Eric's actual decision alongside the verdict, and add a case-wise read-back verb (`ateam steward ledger recall <category>`). Turns the tally into a queryable decision corpus. Filed: **`agent-teams-7ew5.1`**. Prereq for any safe graduation.

**P2 — make RECALL durable.** Hook-level injection of steward learnings + ledger context at session start and after compaction. Coordinate with `agent-teams-e4r1` (same gap, DRI role) rather than build a divergent one. Filed: **`agent-teams-7ew5.2`**.

**P3 — graduation (`agent-teams-e3mq.11`).** Only after 1–2: with a real decision corpus + reliable recall, a per-category threshold can safely flip "escalate" → "decide." This is the step that needs Eric's explicit authority decision, not just engineering. Recommend starting with the narrowest category (`agent-teams-6rru.8`, stuck-DRI respawn) as a trial.

**Sequencing:** capture → recall → graduate. Granting autonomy (P3) before the corpus exists (P1) would let the steward act on an accuracy score with no record of *why* Eric decided as he did — confidently wrong. Build the substrate first.
