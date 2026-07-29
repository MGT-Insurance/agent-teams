---
name: condense
description: Triggered manually or at wind-down to drain fresh memories then condense hot/cold learnings for each over-threshold role. Lock-guarded; skips cleanly if another condense is already running.
---

**The `ateam` tool.** On PATH via the plugin's `bin/` (installed/verified by `/setup-agent-teams`). Call it as bare `ateam` everywhere.

## Parse the argument

- **`/agent-teams:condense <role>`** — condense ONLY that named role (e.g. `dri`, `implementer`). Lock-guarded (same try-acquire/skip semantics as the all-roles form); NO gate — an explicit single-role invocation always condenses regardless of what `ateam condense-check` would report. See **Single-role form** below.
- **`/agent-teams:condense` (no arg)** — all-roles sweep (see below).

---

## Single-role form (`/agent-teams:condense <role>`)

### Step 0 — Acquire the condense lock

```bash
ateam condense-lock acquire
```

If this exits with **code 5** (lock held by another session), log:

```
condense in progress elsewhere — skipping, fresh flushes next run
```

Then **exit cleanly** — nothing was acquired, so nothing to release. Do NOT block or retry.

### Step 1 — Condense

On successful lock acquisition, emit the packet for the ONE named role (no gate — an explicit invocation always condenses):

```bash
ateam condense <role>
```

Then apply the condense procedure (Design hot set → Apply batch → **Drain fresh** → Verify → Emit summary) exactly as described in **Condense procedure** below.

**Do NOT run `ateam fresh-drain <role>` here.** The drain is a stage INSIDE that procedure, after the batch write. Running it before `ateam condense` silently blinds the promotion decision — see **Ordering is load-bearing** in the all-roles Step 2 for the mechanism.

### Step 2 — Release the lock

```bash
ateam condense-lock release
```

Release in ALL exit paths (success and error). The held-skip path (Step 0 exit-5) never acquired the lock, so no release is needed there.

---

## All-roles sweep (no-arg form)

### Step 0 — Acquire the condense lock

```bash
ateam condense-lock acquire
```

Same code-5 handling as **Step 0** in the single-role form above: on exit code 5, log the line shown there and exit cleanly — do NOT block or retry.

If acquisition succeeds, proceed and ensure the lock is released in every exit path (success, error). The lock window covers all role processing and any `ateam sync` at the end.

### Step 1 — Gate every role with ONE call

```bash
ateam condense-check
```

That single read-only call enumerates every learning role — skipping `user` and `applied` unconditionally — and prints an aligned table, one row per role. The verdict, `FIRE` or `SKIP`, is in the `VERDICT` column; read the trailing free-text `REASON` for what tripped. `--json` emits the same fields machine-readably. Exit code is 0 regardless of verdict: **the verdict is data, not an exit status.** The verb writes nothing.

(`user:` is served by `ateam prime`, not part of the hot/cold learnings model; `applied:` holds per-slug counters, not learnings, and must never be condensed.)

**Defer to the printed verdict. Do NOT recompute it.** The trigger and its threshold are defined exactly ONCE, in Go — see contract `agent-teams-0yd3.1`, SEAM 2. This file deliberately does not restate the arithmetic, and neither should you: no `wc -c`, no divisor, no threshold comparison of your own. **Prose restatements of a constant desynchronise from it; a printed verdict cannot.** Every token number here is a CLI-computed approximation (divisor frozen by contract `agent-teams-b2xr.2`) — read them off the tool, never re-derive them. (Why hand-recomputing this has already failed in practice: references/trigger-design.md.)

**What the gate measures: NEW MATERIAL, not total size.** A role fires on accumulation in its **fresh tier** — un-curated learnings written since the last condense. Total `hot ∪ fresh` size is **NOT** a trigger. It survives only as a reported number (see **Emit summary line** in the condense procedure below) and must never be branched on. A role whose reported union sits persistently high is an aggregate-hot-set problem, not a condense-frequency problem: surface it, do not condense at it. (Why total size cannot be a trigger — the clearability test, for anyone reconsidering this: references/trigger-design.md.)

> **STATED ASSUMPTION — the fresh-tier trigger is complete only because normal contribution routes to fresh; a direct `hot:` write bypasses it invisibly.** `ateam learn <role> hot:<slug>` writes straight to hot, bypassing fresh entirely — a first-class, publicly documented affordance, not an internal path, so nothing in the CLI restricts it to condense. The gate holds by convention, not by construction: add a code path that writes `hot:` directly and you have silently broken it, with no failure to observe. Full mechanism and code pointers: references/trigger-design.md.

Log one line for each `SKIP` role — the verb's own line is the note — and do no further work for it:

```
<role>: SKIP (<reason>)
```

Most wind-down sweeps skip every role and exit after the lock release with zero LLM work done.

### Step 2 — Condense (per FIRE role)

For each role whose verdict was `FIRE`:

```bash
ateam condense <role>
```

> **⚠️ Ordering is load-bearing — `ateam condense` runs FIRST, and `ateam fresh-drain` runs LATER, inside the procedure, after the batch write.**
>
> The packet ships FULL bodies only for entries still tagged `hot:`/`fresh:`; cold entries arrive as key + summary only. So draining first destroys the one signal that separates just-served, un-curated material from long-settled archive: those entries would arrive as summaries, shape-identical to cold, and you would be making the promote-vs-archive call on the highest-stakes entries in the packet without ever seeing their bodies. That is drain-then-stop — the exact failure the promotion rule below exists to prevent. Do not "tidy" this ordering back.
>
> Full mechanism and why printing the drain's key list wouldn't rescue a drain-first order: references/drain-ordering.md.

This emits a JSON packet to stdout:

```json
{
  "role": "<role>",
  "memories": [
    {"key": "hot:<slug>", "body": "...", "applied_count": 3, "last_applied": "2026-07-20"},
    {"key": "fresh:<slug>", "body": "..."},
    {"key": "<slug>", "summary": "..."}
  ],
  "hot_budget_tokens": 6000,
  "instruction_contract": "..."
}
```

Field semantics (`instruction_contract` is the schema authority; the sample above is illustrative, one per tier, not exhaustive): `key` is always present and role-relative (`hot:`/`fresh:` prefix, or a bare slug for cold — never the full `<role>:...` form). `body` appears on hot/fresh, never on cold; `summary` appears on cold only — **do not assume every entry carries a body**, and if a promotion decision turns on an elided body, fetch it on demand — `ateam recall <role> <term>`, passing that entry's own `key` verbatim as `<term>` (recall is ONE literal substring match, so a descriptive multi-word query silently returns nothing at exit 0; empty output is never evidence the body is missing) — rather than deciding blind or promoting a summary as if it were the learning. `applied_count`/`last_applied` can appear on ANY tier but are each omitted — not zero-valued — when absent: a missing `applied_count` means 0, a missing `last_applied` means never applied.

`hot_budget_tokens` is **the** hot-set budget: one number, one unit (TOKENS), from the Go constant — use the value the packet actually prints, not the illustrative one above. Nothing else in this skill restates the budget or converts it to bytes; any byte figure you meet here is a per-ENTRY write-time cap, a different limit at a different scope — never convert between the two.

The keys still tagged `fresh:` in the packet are the **primary promotion candidates** — they are being served to every session (hot ∪ fresh) right now. Apply the condense procedure below autonomously for this role.

### Step 3 — Release the lock

After ALL role processing is complete (whether roles were skipped or condensed), release the lock:

```bash
ateam condense-lock release
```

Release on error paths too — do not leave the lock held. (Exception: the held-skip path in Step 0 never acquired the lock, so no release is needed there.)

If you performed an `ateam sync` (Dolt push) at any point, that sync must also occur within the lock window, before release.

---

## Condense procedure (for each gated role)

This procedure is autonomous — NO human-review gate. Safety rests on Dolt history recoverability and the per-role change-summary line you emit.

### Design the hot set (BEFORE writing anything)

IMPORTANT ORDERING: do not create any `<role>:hot:*` key until the full hot set is decided, then create them as a batch. Design the complete hot set first, then write all hot keys as a batch.

Kept for two reasons: mid-design edits change the set you are reasoning about while you reason about it, and an interrupted partial batch leaves hot half-restructured. (The original justification no longer applies — drain now runs after the batch write, not before; full history: references/drain-ordering.md.)

**PROMOTION IS THE POINT — condensing is not just token-reduction. `ateam learnings <role>` serves hot ∪ fresh, so every key the packet shows still tagged `fresh:` is being injected into every session RIGHT NOW — and the drain at the end of this procedure will move it out of that injection unless you promote it. Any such key you leave unpromoted is SILENTLY DEMOTED.** Therefore you MUST explicitly decide, for each `fresh:`-tagged (currently-served) key, hot vs cold — do not let them fall into cold by default; that is the drain-then-stop failure mode Ordering above exists to prevent. **Being UNDER the hot budget (`hot_budget_tokens`) is NOT a reason to skip promotion** — it means there is ROOM; fill it with the highest-signal currently-served learnings. Over budget is handled by the theme-first merge below, never by silently dropping served learnings.

**Promote vs. archive — a `fresh:`-tagged key earns a hot slot only if it is a concise, self-contained learning carrying NET-NEW signal (a RULE/gotcha not already covered by an existing hot entry). Do NOT blind-promote a raw, verbose entry that is the pre-distillation SOURCE of an existing hot entry (tell: a longer body under a near-duplicate slug, e.g. cold `go-advisory-lock-pattern` vs hot `advisory-lock`) — promoting it de-distills hot. Such raw archive stays in cold; merge any nuance it carries into the existing hot entry instead of adding a second one.**

**Applied-impact ranking — `applied_count` / `last_applied` are an ADDITIONAL ranking signal, not a replacement for the net-new-signal bar above.** The packet supplies both per memory, fed by agents self-reporting via `ateam applied <role> <slug>` at the point they act on a learning. Among candidates that already clear the net-new-signal bar, prefer promoting learnings with a high `applied_count` — frequent application is empirical evidence the learning is load-bearing. Conversely, a cold entry that has never been applied (`applied_count` 0 or absent, `last_applied` empty) is an eviction candidate — weigh it against the conservative "evict little" default in Apply below rather than auto-evicting on this signal alone. Treat the count as directional, not precise (self-reported, so undercounting is expected); a slug merge/rename during condense resets it to zero, which is fine.

Design principles:
- Select the highest-signal learnings: recurring process rules, hard-won gotchas, ship constraints, cardinal rules — anything whose loss causes a wrong or expensive action.
- MERGE overlapping learnings into single succinct entries. This is where most token reduction comes from.
- **Theme-first forced merge:** when more than 2 fresh/cold candidates share a theme, they MUST collapse into ONE umbrella hot (or cold) entry with per-nuance bullets — do not leave them as separate entries. Cite at most ONE anecdote or initiative-id per merged entry; the rest of the theme's occurrences just reinforce the same RULE/TRIGGER, they don't each need their own provenance. This is not optional polish — a shared theme with 3+ standalone entries is a design defect.
- Write each entry "as succinct as possible while still COMPLETE" — keep every load-bearing detail (file paths, exact commands, the WHY). Store the learning itself, not the story of how it was found — include only enough context to signal WHEN the learning is relevant, not a history lesson. Shape each entry as RULE (one sentence — the transferable learning itself), TRIGGER (when it fires / how to recognize relevance), APPLY (what to do about it), and PROVENANCE as a bare initiative-id parenthetical only, e.g. `(agent-teams-2n1w)` — no narrative retelling of how it was discovered.
- **Target the packet's `hot_budget_tokens`, in TOKENS**, across all hot keys — roughly 15-25 items. Do not steer to a byte equivalent: there isn't one. The one byte figure that applies here is a different limit at a different scope — each INDIVIDUAL entry is capped at ~900 bytes at write time by `ateam learn` (frozen by contract `agent-teams-b2xr.2`; hot and fresh 900 bytes, cold 1500). A merged umbrella entry must still fit that per-entry cap.
- Assign each hot entry a meaningful slug (e.g. `hot:cardinal-rule`, `hot:ship-constraint`).

### Apply (batch write, then cleanup)

Create a unique session-scoped temp directory so parallel condense runs cannot clobber each other:

```bash
DIR=$(mktemp -d)
```

For each entry in your decided hot set, write to a file under `$DIR` and promote:

```bash
printf '%s' "<hot body>" > "$DIR/<slug>.txt"
ateam learn <role> hot:<slug> --file "$DIR/<slug>.txt"
```

After ALL hot entries are written, handle cold cleanup:
- DEMOTE stale hot items to cold: `ateam learn <role> cold:<slug> --file <f>` then `ateam forget <role> hot:<slug>`.
- Within cold: MERGE duplicates or REWRITE for brevity via `ateam learn <role> cold:<slug> --file <f>`; EVICT truly-dead items via `ateam forget <role> <slug>`.
- LEAVE IN COLD any learning not promoted (the long tail stays searchable, not injected).
- EVICT ONLY exact duplicates or clearly-superseded items. When in doubt, keep in cold. Conservative: NO eviction floor, but evict little.

**Do not "normalize" the two forms above.** `learn` takes `cold:<slug>` while `forget` takes a bare `<slug>` for the same cold entry — that reads like an inconsistency and is not one (`instruction_contract` carries the mechanism). The two errors are not symmetric: adding `cold:` to `forget` fails loudly with `No memory with key` and exit 1, but dropping `cold:` from `learn` **succeeds silently into the wrong tier**, writing a new fresh duplicate and leaving the stale cold entry in place. Only one of these edits announces itself.

If you are refreshing an existing hot key, `ateam learn <role> hot:<slug>` is an UPSERT — it overwrites in place.

If you restructure the hot set (e.g. merge several old hot entries into fewer new ones), you MUST `ateam forget <role> hot:<old-slug>` for every old hot key that is NOT present in the new hot set. Skipping this step leaves stale hot entries that linger and bloat the injected layer.

### Drain fresh — AFTER the batch write, never before

```bash
ateam fresh-drain <role>
```

Deterministic, no LLM call: it rewrites every `<role>:fresh:<slug>` into a bare cold `<role>:<slug>` and prints a count.

It does NOT discriminate, and it does not need to: it drains the whole fresh tier unconditionally. Do not read this step as "sweep up the leftovers" and make it conditional. (Why unconditional draining is still correct for promoted entries too: references/drain-ordering.md.)

Run it HERE, once the hot set has been written. Run it any earlier and you reintroduce the failure described in **Ordering is load-bearing** above.

**Do not "simplify" this by folding the drain into `ateam condense` itself.** `ateam condense` is a PURE READ — giving it a store mutation means a run that dies after the packet emit but before curation has already demoted the entire fresh tier to cold, un-curated: drain-then-stop promoted from accident to systematic. Keeping the drain here, after the batch write, means a failed run mutates nothing and retries clean. That crash-safety property is the main reason this ordering is what it is.

### Verify — re-measure, then iterate

```bash
ateam condense-check <role>
```

Compare the reported `hot_approx_tokens` against `hot_budget_tokens` from the packet. **Both are TOKENS, both come from Go, measured by the tool.** Do not measure bytes, and do not certify the landing against any byte figure.

If the role is still over budget, **iterate — do not accept-and-report**: apply the theme-first forced merge above again, rewrite the batch, re-run the check. Re-measure-and-iterate is the backstop; a run that lands over budget and notes it in the summary has not finished.

**Do NOT buy the budget by evicting curated signal.** If merging genuinely cannot fit the hot set inside the budget, the surplus goes to COLD — still searchable via `ateam recall`, just not injected — never dropped. Shrinking the landing below the budget does not buy meaningful condense frequency either: the frequency lever is the fresh-tier trigger, not the landing target. This has happened before: a run steering to the wrong number dropped entries carrying real signal.

Then confirm the served set and spot-check cold:

```bash
ateam learnings <role>
```

Confirm output shows only the hot entries (the fresh tier is empty after drain).

```bash
ateam recall <role> <term>
```

Confirm cold memories are still reachable for a representative term.

### Emit summary line

Emit one line per role:

```
<role>: promoted N / merged M / evicted K / hot now X tokens / hot∪fresh Y tokens
```

Where:
- N = number of net-new hot entries (keys that did not previously have `hot:` form)
- M = number of cold entries merged into a single hot entry (count source entries collapsed)
- K = number of cold entries removed via `ateam forget`
- X = `hot_approx_tokens` from the `ateam condense-check <role>` you ran in Verify — read it off the tool, do not estimate it
- Y = `approx_tokens` (the `hot ∪ fresh` union) from that same output. **REPORTED ONLY — never branched on.** It is here so a persistently-high union is visible instead of silent — that condition routes to the aggregate hot-set problem, not to another condense run.

If a role returned zero memories from `ateam condense <role>`, skip it with: `<role>: no memories — skipped`.

---

## Memory routing reminder

Do NOT write any MEMORY.md files or Claude file-based memories here. All persistence goes through `ateam learn` / `ateam forget`.
