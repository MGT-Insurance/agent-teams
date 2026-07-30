# Memory mechanics — three-tier model

The core routing rule (route by kind; default `ateam learn`; never MEMORY.md; contribute the moment a learning forms) lives inline in SKILL.md under "Memory routing". This file holds the tier mechanics — reference-only detail the DRI reaches when curating memories. The condense procedure itself lives in the `/agent-teams:condense` skill (also the Phase 6 wind-down touchpoint — see references/wind-down.md); it is not restated here.

## Learning body shape (all tiers)

Store the learning itself, not the story of how it was found — include only enough context to signal WHEN it's relevant, not a history lesson. Shape the body as RULE (one sentence — the transferable learning itself), TRIGGER (when it fires / how to recognize relevance), APPLY (what to do about it), with PROVENANCE as a bare initiative-id parenthetical only, e.g. `(agent-teams-2n1w)` — no narrative retelling of how it was discovered. Writing the same `<slug>` again overwrites the previous body (upsert-by-key).

## Three-tier memory model (fresh / hot / cold)

Role memories use a three-tier key convention — the tier is encoded in the key, not in metadata:

- **Fresh:** `<role>:fresh:<slug>` — the default write tier. `ateam learn <role> <slug> --file <f>` (bare slug, no prefix) writes here automatically. Fresh accumulates between condense runs; `ateam learnings <role>` serves it alongside hot. It's the "just written, not yet curated" tier, periodically drained into cold by `ateam fresh-drain <role>`.
- **Hot:** `<role>:hot:<slug>` — curated, auto-injected into every session via `ateam learnings <role>`. Write explicitly with `ateam learn <role> hot:<slug> --file <f>`. Hot bodies are deliberately succinct; target budget ~6000 tokens (~15–25 learnings) across all hot keys for a role.
- **Cold:** `<role>:<slug>` — searchable on demand, NOT auto-injected. Write explicitly with `ateam learn <role> cold:<slug> --file <f>` (the `cold:` prefix is stripped to produce the bare `role:<slug>` key). Pre-tier `dri:<slug>` memories are already cold, no migration needed.

`ateam learnings <role>` serves the **hot ∪ fresh** union, falling back to all `role:` keys only when both are empty. All three tiers are living; cold is not a frozen archive.

**Key conventions:** `ateam learn <role> <slug>` → `role:fresh:<slug>` (default, same as explicit `fresh:<slug>`); `ateam learn <role> hot:<slug>` → `role:hot:<slug>`; `ateam learn <role> cold:<slug>` → `role:<slug>` (tier tag stripped).

**Search/remove:** `ateam recall <role> <query>` substring-searches a role's memories (key+body) on demand — surface cold context before a task or when a hot hint points at cold detail. `ateam forget <role> <slug>|hot:<slug>|fresh:<slug>` removes the matching tier; every removal is recoverable from Dolt history (`refs/dolt/data`).

**Promoting to hot:** `ateam learn <role> hot:<slug> --file <tmpfile>`, same RULE/TRIGGER/APPLY/PROVENANCE shape (write-time cap: 900 bytes for hot/fresh, 1500 for cold) — hot is injected whole every session, so a history lesson directly costs context, not just bytes.
