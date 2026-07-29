---
name: condense
description: Triggered manually or at wind-down to drain fresh memories then condense hot/cold learnings for each over-threshold role. Lock-guarded; skips cleanly if another condense is already running.
---

**The `ateam` tool.** `ateam` is on PATH — it ships as a prebuilt binary in the plugin's `bin/` (auto-added to PATH; installed/verified by `/setup-agent-teams`). Call it as bare `ateam` everywhere.

## Parse the argument

- **`/agent-teams:condense <role>`** — condense ONLY that named role (e.g. `dri`, `implementer`). Lock-guarded (same try-acquire/skip semantics as the all-roles form); NO gate — an explicit single-role invocation always condenses that role regardless of what `ateam condense-check` would report for it. See **Single-role form** below.
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

### Step 1 — Drain fresh then condense

On successful lock acquisition, run the drain+condense procedure for the ONE named role (no gate — an explicit invocation always condenses):

```bash
ateam fresh-drain <role>
ateam condense <role>
```

Apply the condense procedure (Design hot set → Apply batch → Verify → Emit summary) exactly as described in **Condense procedure** below.

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

If this exits with **code 5** (lock held by another session), log:

```
condense in progress elsewhere — skipping, fresh flushes next run
```

Then **exit cleanly** — nothing was acquired, so nothing to release. Do NOT block or retry.

If acquisition succeeds, proceed and ensure the lock is released in every exit path (success, error). The lock window covers all role processing and any `ateam sync` at the end.

### Step 1 — Gate every role with ONE call

```bash
ateam condense-check
```

That single read-only call enumerates every learning role — skipping `user` and `applied` unconditionally — and prints one line per role ending in a verdict, `FIRE` or `SKIP`, with a short `reason` naming what tripped. `--json` emits the same per-role fields machine-readably. Exit code is 0 regardless of verdict: **the verdict is data, not an exit status.** The verb writes nothing.

(For why those two namespaces are excluded: `user:` is served by `ateam prime`, capped and truncated at read time, and is not part of the hot/cold learnings model; `applied:` holds per-slug applied-signal counters, not learnings, and must never be condensed.)

**Defer to the printed verdict. Do NOT recompute it.** The trigger and its threshold are defined exactly ONCE, in Go — see contract `agent-teams-0yd3.1`, SEAM 2. This file deliberately does not restate the arithmetic, and neither should you: no `wc -c`, no divisor, no threshold comparison of your own. Hand-recomputation is the defect this verb exists to remove — sweeps in the measured window computed the old prose gate with a looser divisor than the skill mandated, gating ~33% looser than intended and making the fire decision non-deterministic across runs. Every token number in this skill is a CLI-computed approximation (the bytes-per-token divisor is frozen by contract `agent-teams-b2xr.2`); read them off the tool, never re-derive them.

**What the gate measures: NEW MATERIAL, not total size.** A role fires on accumulation in its **fresh tier** — un-curated learnings written since the last condense. Total `hot ∪ fresh` size is **NOT** a trigger. It survives only as a reported number (see **Emit summary line** in the condense procedure below) and must never be branched on.

Why total size cannot be a trigger, stated so it is not relitigated: **a trigger has to be CLEARABLE by the action it triggers.** A condense run does drain fresh and re-curate hot, but it lands only about a thousand tokens under the old union ceiling and re-arms within roughly two sweeps — so that ceiling fired at every wind-down, on material that had already been curated, forever. Apply the same clearability test to any future proposal to reinstate a size-based trigger. A role whose reported union sits persistently high is an aggregate-hot-set problem, not a condense-frequency problem: surface it, do not condense at it.

> **STATED ASSUMPTION — the fresh-tier trigger is complete only because normal contribution routes to fresh.** `ateam learn <role> <slug>` with a BARE slug falls through to `<role>:fresh:<slug>` (`learnKey`, `internal/verbs/write.go:78`), and that DEFAULT is what makes a fresh-tier trigger see every accumulation. But `ateam learn <role> hot:<slug>` writes straight to hot, bypassing fresh entirely (`internal/verbs/write.go:75-77`), and the tier prefix is ADVERTISED in public help (the `learn` verb's slug flag: "prefix with `hot:`, `fresh:`, or `cold:` to target a tier") — a first-class documented affordance, not an internal path, and nothing in the CLI restricts it to condense. Today the only caller using it is the condense instruction contract in `internal/verbs/kong_converted.go`, i.e. this procedure. **A direct `hot:` write by anything other than condense bypasses the trigger and is invisible to it.** The gate holds by convention, not by construction: add a code path that writes `hot:` directly and you have silently broken it, with no failure to observe.

Log one line for each `SKIP` role — the verb's own line is the note — and do no further work for it:

```
<role>: SKIP (<reason>)
```

Most wind-down sweeps skip every role and exit after the lock release with zero LLM work done.

### Step 2 — Drain fresh then condense (per FIRE role)

For each role whose verdict was `FIRE`:

#### 2a — Drain fresh tier

```bash
ateam fresh-drain <role>
```

This is deterministic (no LLM call). It moves all `<role>:fresh:*` keys into bare cold keys (`<role>:<slug>`). After this, the condense agent sees only hot and cold — no third tier.

#### 2b — Condense (emit packet, spawn agent)

```bash
ateam condense <role>
```

This emits a JSON packet to stdout:

```json
{
  "role": "<role>",
  "memories": [{"key": "<role>:<slug>", "body": "..."}],
  "hot_budget_tokens": 6000,
  "instruction_contract": "..."
}
```

`hot_budget_tokens` is **the** hot-set budget: one number, one unit (TOKENS), emitted from the Go constant. The value above is illustrative — use the one the packet actually prints. Nothing else in this skill restates the budget, and nothing converts it to bytes. Any byte figure you meet here is a per-ENTRY write-time cap, a different limit on a different scope; never convert between the two.

Read ALL memory bodies from this packet. These are the full cold + hot contents for the role (fresh has already been drained into cold). The keys that step 2a JUST moved out of `<role>:fresh:*` are the **primary promotion candidates** — they were being SERVED to every session (via hot ∪ fresh) right up until the drain, so they are not settled cold: they are un-curated served learnings awaiting a hot/cold verdict. Apply the condense procedure below autonomously for this role.

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

IMPORTANT ORDERING: do not create any `<role>:hot:*` key until the full hot set is decided, then create them as a batch. `ateam learnings <role>` serves hot ∪ fresh; because `ateam fresh-drain <role>` already ran, the fresh set is empty here — so a partial hot set would under-serve the next session. Design the complete hot set first, then write all hot keys as a batch.

**PROMOTION IS THE POINT — condensing is not just token-reduction. `ateam learnings <role>` serves hot ∪ fresh, so every key step 2a drained out of fresh was being injected into every session until now. Any drained key you leave in cold is SILENTLY DEMOTED out of that injection. Therefore you MUST explicitly decide, for each drained (previously-served) key, hot vs cold — do not let them settle into cold by default. Drain-then-stop (draining fresh and promoting nothing) is the failure mode this step exists to prevent: it silently strips the session of learnings it was relying on. Being UNDER the hot budget (`hot_budget_tokens`) is NOT a reason to skip promotion — it means there is ROOM; fill it with the highest-signal drained learnings. (Going over budget is handled by the theme-first merge below, never by silently dropping served learnings.)**

**Promote vs. archive — a drained key earns a hot slot only if it is a concise, self-contained learning carrying NET-NEW signal (a RULE/gotcha not already covered by an existing hot entry). Do NOT blind-promote a raw, verbose entry that is the pre-distillation SOURCE of an existing hot entry (tell: a longer body under a near-duplicate slug, e.g. cold `go-advisory-lock-pattern` vs hot `advisory-lock`) — promoting it de-distills hot. Such raw archive stays in cold; if it carries a nuance the hot entry lacks, MERGE that nuance into the existing hot entry instead of adding a second entry.**

**Applied-impact ranking — `applied_count` / `last_applied` are an ADDITIONAL ranking signal, not a replacement for the net-new-signal bar above.** The condense packet supplies `applied_count` (int) and `last_applied` (string) per memory, fed by agents self-reporting via `ateam applied <role> <slug>` at the point they act on a learning. Among candidates that already clear the net-new-signal bar, prefer promoting learnings with a high `applied_count` — frequent application is empirical evidence the learning is load-bearing, not merely plausible. Conversely, a cold entry that has never been applied (`applied_count` 0 or absent, `last_applied` empty) and has fallen to cold is an eviction candidate — weigh it against the conservative "evict little" default in Apply below rather than auto-evicting on this signal alone. Accepted forks: undercounting is expected (this is agent self-report, not auto-detected, so treat the count as directional, not precise) and a slug merge/rename during condense resets its counter to zero — that's fine, since condense is exactly when a learning is re-evaluated.

Design principles:
- Select the highest-signal learnings: recurring process rules, hard-won gotchas, ship constraints, cardinal rules — anything whose loss causes a wrong or expensive action.
- MERGE overlapping learnings into single succinct entries. This is where most token reduction comes from.
- **Theme-first forced merge:** when more than 2 fresh/cold candidates share a theme, they MUST collapse into ONE umbrella hot (or cold) entry with per-nuance bullets — do not leave them as separate entries. Cite at most ONE anecdote or initiative-id per merged entry; the rest of the theme's occurrences just reinforce the same RULE/TRIGGER, they don't each need their own provenance. This is not optional polish — a shared theme with 3+ standalone entries is a design defect, not a stylistic choice.
- Write each entry "as succinct as possible while still COMPLETE" — keep every load-bearing detail (file paths, exact commands, the WHY). Store the learning itself, not the story of how it was found — include only enough context to signal WHEN the learning is relevant, not a history lesson. Shape each entry as RULE (one sentence — the transferable learning itself), TRIGGER (when it fires / how to recognize relevance), APPLY (what to do about it), and PROVENANCE as a bare initiative-id parenthetical only, e.g. `(agent-teams-2n1w)` — no narrative retelling of how it was discovered.
- **Target the packet's `hot_budget_tokens`, in TOKENS**, across all hot keys — roughly 15-25 items. Do not steer to a byte equivalent: there isn't one, and there must not be. The one byte figure that applies here is a different limit at a different scope — each INDIVIDUAL entry is capped at ~900 bytes at write time by `ateam learn` (frozen by contract `agent-teams-b2xr.2`; hot and fresh 900 bytes, cold 1500). A merged umbrella entry must still fit that per-entry cap, which is exactly why per-nuance bullets over separate entries is the only way to absorb a large shared theme.
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

If you are refreshing an existing hot key, `ateam learn <role> hot:<slug>` is an UPSERT — it overwrites in place.

If you restructure the hot set (e.g. merge several old hot entries into fewer new ones), you MUST `ateam forget <role> hot:<old-slug>` for every old hot key that is NOT present in the new hot set. Skipping this step leaves stale hot entries that linger and bloat the injected layer.

### Verify — re-measure, then iterate

```bash
ateam condense-check <role>
```

Compare the reported `hot_approx_tokens` against `hot_budget_tokens` from the packet. **Both are in TOKENS and both come from Go — one budget, one unit, measured by the tool.** Do not measure bytes, and do not certify the landing against any byte figure.

If the role is still over budget, **iterate — do not accept-and-report**: apply the theme-first forced merge above again, rewrite the batch, re-run the check. Re-measure-and-iterate is the backstop; a run that lands over budget and notes it in the summary has not finished.

**Do NOT buy the budget by evicting curated signal.** If merging genuinely cannot fit the hot set inside the budget, the surplus goes to COLD — still searchable via `ateam recall`, just not injected — never dropped. Shrinking the landing below the budget does not buy meaningful condense frequency either: the frequency lever is the fresh-tier trigger, not the landing target. Over-eviction is the failure this section exists to prevent, and it has happened: a run steering to the wrong number dropped entries carrying real signal.

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
- Y = `approx_tokens` (the `hot ∪ fresh` union) from that same output. **REPORTED ONLY — never branched on.** It is here so that a role whose union sits persistently high is visible instead of silent; that condition routes to the aggregate hot-set problem, not to another condense run.

If a role returned zero memories from `ateam condense <role>`, skip it with: `<role>: no memories — skipped`.

---

## Memory routing reminder

Do NOT write any MEMORY.md files or Claude file-based memories here. All persistence goes through `ateam learn` / `ateam forget`.
