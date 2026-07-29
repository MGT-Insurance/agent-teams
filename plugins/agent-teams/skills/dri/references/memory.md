# Memory mechanics — three-tier model + condensing

The core routing rule (route by kind; default `ateam learn`; never MEMORY.md; contribute the moment a learning forms) lives inline in SKILL.md under "Memory routing". This file holds the tier mechanics and the condense flow — reference-only detail the DRI reaches when curating memories or at Phase 6 wind-down.

## Learning body shape (all tiers)

Store the learning itself, not the story of how it was found — include only enough context to signal WHEN it's relevant, not a history lesson. Shape the body as RULE (one sentence — the transferable learning itself), TRIGGER (when it fires / how to recognize relevance), APPLY (what to do about it), with PROVENANCE as a bare initiative-id parenthetical only, e.g. `(agent-teams-2n1w)` — no narrative retelling of how it was discovered. Writing the same `<slug>` again overwrites the previous body (upsert-by-key).

## Three-tier memory model (fresh / hot / cold)

Role memories use a three-tier key convention — the tier is encoded in the key, not in metadata:

- **Fresh:** `<role>:fresh:<slug>` — the default write tier. `ateam learn <role> <slug> --file <f>` (bare slug, no prefix) writes here automatically. Fresh memories accumulate between condense runs; `ateam learnings <role>` serves them alongside hot. Fresh is the "just written, not yet curated" tier and is periodically drained into cold by `ateam fresh-drain <role>`.
- **Hot:** `<role>:hot:<slug>` — curated, auto-injected into every session via `ateam learnings <role>`. Write explicitly with `ateam learn <role> hot:<slug> --file <f>`. Hot bodies are deliberately succinct. The budget for a role's whole hot set (~15–25 learnings) is stated ONCE, in TOKENS, by the `hot_budget_tokens` field of the `ateam condense <role>` packet — read it there rather than carrying a number in your head. There is no byte equivalent of that budget; the only byte limits in this model are the per-entry write-time caps below.
- **Cold:** `<role>:<slug>` — searchable on demand, NOT auto-injected. Write explicitly with `ateam learn <role> cold:<slug> --file <f>` (the `cold:` prefix is stripped to produce the bare `role:<slug>` key). The existing pre-tier `dri:<slug>` memories are already cold with no migration needed.

`ateam learnings <role>` serves the **hot ∪ fresh** union. It falls back to all `role:` keys only when BOTH hot and fresh are empty (preserving pre-tier behavior for roles with no curated set). All three tiers are living; cold is not a frozen archive.

**Key conventions at a glance:**
- `ateam learn <role> <slug>` → writes `role:fresh:<slug>` (default)
- `ateam learn <role> hot:<slug>` → writes `role:hot:<slug>` (explicit hot)
- `ateam learn <role> fresh:<slug>` → writes `role:fresh:<slug>` (explicit fresh, same as default)
- `ateam learn <role> cold:<slug>` → writes `role:<slug>` (explicit cold, no tier tag)

**Caution — a direct `hot:` write bypasses the condense trigger.** The bare-slug default (→ `<role>:fresh:<slug>`, `learnKey` at `internal/verbs/write.go:78`) is what makes the fresh-tier condense gate COMPLETE: every normal contribution lands where the gate can see it. `hot:` skips fresh entirely (`internal/verbs/write.go:75-77`), so anything written that way never registers with the gate. Today the only caller using `hot:` is the condense procedure itself. Keep it that way — the gate holds by convention, not by construction, and breaking it produces no failure to observe.

**Searching cold memories:** `ateam recall <role> <query>` does a substring search over a role's memories (key+body) and prints matching key+body pairs on demand. Use this to surface cold context before starting a task or when a hot hint points to a cold detail.

**Removing a memory:** `ateam forget <role> <slug>` removes a cold memory. `ateam forget <role> hot:<slug>` removes a hot memory. `ateam forget <role> fresh:<slug>` removes a fresh memory. Every removal is recoverable from Dolt history (`refs/dolt/data`).

**Promoting a learning to hot:** write it with `ateam learn <role> hot:<slug> --file <tmpfile>`. Keep the body in the shape above (write-time cap: 900 bytes for hot and fresh, 1500 for cold) — hot memories are injected whole every session, so a history lesson directly costs context, not just extra bytes.

## Condensing (autonomous)

Condensing is **lock-guarded**: the `/agent-teams:condense` skill acquires `ateam condense-lock` before doing any work, skips cleanly if another session holds the lock, and releases on all exit paths. Use the skill (no arg for all roles; `<role>` arg for a single role) rather than calling `ateam condense <role>` directly.

The condense flow per role: `ateam condense <role>` FIRST (emits a read-only structured packet: all memories, hot budget, and instruction contract) to stdout. The condense agent reads that packet and applies changes autonomously via `ateam learn` and `ateam forget`:
- promote/refresh into hot: `ateam learn <role> hot:<slug> --file <f>`
- demote stale hot to cold: `ateam learn <role> cold:<slug> --file <f>`, then `ateam forget <role> hot:<slug>`
- merge/rewrite in cold: `ateam learn <role> cold:<slug> --file <f>`
- evict dead items: `ateam forget <role> <slug>`

Only AFTER that batch write does `ateam fresh-drain <role>` run (deterministic — moves every `role:fresh:*` to a bare cold key, no LLM). **That order is load-bearing, not incidental.** The packet marks tiers by the prefix a key carries at read time, and fresh-drain prints only a count — never the key list. Drain first and the just-served, un-curated entries arrive shape-identical to long-settled cold, so the promote-vs-archive call gets made on exactly the wrong entries with the least information. Condense also stays a pure read this way: a run that dies before curation has mutated nothing and retries clean.

There is NO human-review gate and NO staged diff — the agent acts autonomously.

Safety backstops:
- **Dolt history** — every write, including eviction, is recoverable via `refs/dolt/data`. A bad run is revertible.
- **Change-summary log** — the condense agent emits one line per run: `promoted N / merged M / evicted K / hot now X tokens`.

v1 has no per-run eviction floor — trust the agent and Dolt-history recoverability.

**Wind-down touchpoint:** at Phase 6 wind-down, run the `/agent-teams:condense` skill (no arg) to perform the all-roles, lock-guarded condense sweep. This acquires the condense lock, gates every role with ONE call (`ateam condense-check`, which prints a `FIRE`/`SKIP` verdict per role), runs the condense procedure for each role that FIRED (`ateam condense <role>` FIRST, with `ateam fresh-drain <role>` later, inside the procedure, after the curated hot set is written), and releases the lock. **The verdict turns on NEW MATERIAL** — un-curated `<role>:fresh:*` accumulation since that role's last condense. Total `hot ∪ fresh` size is NOT a trigger; it is reported at run end and never branched on (a size ceiling is not clearable by the run it triggers — the run lands just under it and re-arms within about two sweeps). The threshold lives in Go, defined once: defer to the printed verdict, never recompute it. The DRI is a LOCAL agent with access to the local `~/.agent-teams` Dolt store and can run the LLM curation. Most wind-downs find every role SKIPs and exit cheaply with zero LLM calls. If another session holds the condense lock, the skill logs "condense in progress elsewhere — skipping, fresh flushes next run" and exits cleanly without blocking. See the `/agent-teams:condense` skill for the full procedure.
