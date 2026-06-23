---
name: condense
description: Triggered manually or at teardown to drain fresh memories then condense hot/cold learnings for each over-threshold role. Lock-guarded; skips cleanly if another condense is already running.
---

**The `ateam` tool.** `ateam` is on PATH — it ships as a prebuilt binary in the plugin's `bin/` (auto-added to PATH; installed/verified by `/setup-agent-teams`). Call it as bare `ateam` everywhere.

## Parse the argument

- **`/agent-teams:condense <role>`** — condense ONLY that named role (e.g. `dri`, `implementer`). Lock-guarded (same try-acquire/skip semantics as the all-roles form); no 8K size gate — an explicit single-role invocation always condenses that role regardless of current size. See **Single-role form** below.
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

On successful lock acquisition, run the drain+condense procedure for the ONE named role (no 8K size gate — an explicit invocation always condenses):

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

### Step 1 — Enumerate roles

```bash
ateam roles
```

Skip the `user` role unconditionally. The `user:` namespace is served by `ateam prime` (capped and truncated at read time) and is not part of the hot/cold learnings model. Learning roles to consider: `dri`, `planner`, `implementer`, `tester`, `reviewer`, and any others returned by `ateam roles` that are not `user`.

### Step 2 — Per-role size gate (8K token threshold)

For each role:

```bash
ateam learnings <role>
```

Measure the **byte length** of the output. Approximate token count as `bytes / 4` (rough heuristic: one token ≈ 4 bytes of English text; adjust this divisor if you observe systematic over- or under-counting). If `approx_tokens <= 8000`, **skip this role cheaply** with a one-line note:

```
<role>: under threshold (~<N> tokens) — skipped
```

Only roles where `approx_tokens > 8000` proceed to drain+condense. Most teardown runs will find nothing over 8K and exit after the release with zero LLM work done.

### Step 3 — Drain fresh then condense (per gated role)

For each role that exceeded the 8K gate:

#### 3a — Drain fresh tier

```bash
ateam fresh-drain <role>
```

This is deterministic (no LLM call). It moves all `<role>:fresh:*` keys into bare cold keys (`<role>:<slug>`). After this, the condense agent sees only hot and cold — no third tier.

#### 3b — Condense (emit packet, spawn agent)

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

Read ALL memory bodies from this packet. These are the full cold + hot contents for the role (fresh has already been drained into cold). Apply the condense procedure below autonomously for this role.

### Step 4 — Release the lock

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

IMPORTANT ORDERING: do not create any `<role>:hot:*` key until the full hot set is decided, then create them as a batch. The moment one hot key exists, `ateam learnings <role>` serves ONLY hot keys — a partial hot set would under-serve the next session.

Design principles:
- Select the highest-signal learnings: recurring process rules, hard-won gotchas, ship constraints, cardinal rules — anything whose loss causes a wrong or expensive action.
- MERGE overlapping learnings into single succinct entries. This is where most token reduction comes from.
- Write each entry "as succinct as possible while still COMPLETE" — keep every load-bearing detail (file paths, exact commands, the WHY).
- Target <= 6000 tokens (~24KB) / ~15-25 items total across all hot keys.
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

### Verify

```bash
ateam learnings <role>
```

Confirm output shows only the hot set and is <= ~24KB. Then spot-check cold:

```bash
ateam recall <role> <term>
```

Confirm cold memories are still reachable for a representative term.

### Emit summary line

Emit one line per role:

```
<role>: promoted N / merged M / evicted K / hot now X tokens
```

Where:
- N = number of net-new hot entries (keys that did not previously have `hot:` form)
- M = number of cold entries merged into a single hot entry (count source entries collapsed)
- K = number of cold entries removed via `ateam forget`
- X = approximate token count of the current hot set (estimate from character count / 4)

If a role returned zero memories from `ateam condense <role>`, skip it with: `<role>: no memories — skipped`.

---

## Memory routing reminder

Do NOT write any MEMORY.md files or Claude file-based memories here. All persistence goes through `ateam learn` / `ateam forget`.
