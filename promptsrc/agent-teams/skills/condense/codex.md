---
name: condense
description: Curate role learnings manually or at wind-down. Condense before draining fresh; this order is load-bearing. A lock prevents concurrent runs.
---

Use bare `ateam`; `agent-teams-codex:setup-agent-teams` installs and verifies it.

## Invocation

- `agent-teams-codex:condense <role>` always processes exactly that role after acquiring the lock. It is lock-guarded but bypasses the gate.
- `agent-teams-codex:condense` runs one all-role, gate-controlled sweep.

For either form, acquire the lock first:

```bash
ateam condense-lock acquire
```

If it exits with code 5, log `condense in progress elsewhere — skipping, fresh flushes next run` and exit cleanly. Do not retry or release an unacquired lock. Once acquired, you MUST release it on every success and error exit:

```bash
ateam condense-lock release
```

Any `ateam sync` stays inside that lock window.

## Select roles and read packets

For an explicit role, immediately read its packet:

```bash
ateam condense <role>
```

For the no-argument sweep, run this once after acquiring the lock:

```bash
ateam condense-check
```

It is read-only, owns the verdict, and excludes `user` and `applied`. Process only `FIRE` rows; log each other row as `<role>: SKIP (<reason>)`. Never recompute its threshold or branch on total `hot ∪ fresh` size. For each fired role, you MUST read the packet with `ateam condense <role>` before any drain. A zero-memory packet logs `<role>: no memories — skipped`.

The packet's `instruction_contract` is authoritative. `hot:` and `fresh:` entries include bodies; cold entries are summaries. When a cold body is needed, use `ateam recall <role> <key>` with that exact key. `hot_budget_tokens` and all reported token fields are CLI-computed; do not estimate them.

## Curate each packet

Design the complete hot set before writing. You MUST decide every served `fresh:` item explicitly: promote concise, self-contained net-new signal; retain raw sources and the long tail in cold; merge overlapping themes, with more than two candidates per theme becoming one umbrella entry. Prefer applied impact only after the net-new-signal bar. Keep entries concise and complete in RULE / TRIGGER / APPLY form with bare initiative-id provenance. Target the packet's hot budget; if needed, merge or move surplus to cold, never discard useful signal.

Batch-write the decided hot keys in a unique temporary directory:

```bash
DIR=$(mktemp -d)
printf '%s' "<hot body>" > "$DIR/<slug>.txt"
ateam learn <role> hot:<slug> --file "$DIR/<slug>.txt"
```

After all hot writes, demote stale hot keys with `ateam learn <role> cold:<slug> --file <file>` then `ateam forget <role> hot:<slug>`; rewrite or merge cold entries with `learn cold:<slug>`; evict only exact duplicates or clear supersessions with bare-key `ateam forget <role> <slug>`. Remove every old hot key absent from a restructured set.

Only after the complete hot batch and cleanup, you MUST drain fresh:

```bash
ateam fresh-drain <role>
```

Never drain first or fold draining into `ateam condense`: fresh bodies are required for promotion decisions, and `ateam condense` is a PURE READ so failed curation can retry safely. See [drain-ordering.md](references/drain-ordering.md).

## Verify and report

```bash
ateam condense-check <role>
ateam learnings <role>
ateam recall <role> <key-of-an-entry-you-just-demoted>
```

Compare CLI `hot_approx_tokens` with packet `hot_budget_tokens`; if over, merge and repeat rather than report success. Confirm the served output is complete and fresh is empty. The recall proof is exactly `1 matches` for the exact key, not merely output; see [recall-verification.md](references/recall-verification.md). Emit:

```
<role>: promoted N / merged M / evicted K / hot now X tokens / hot∪fresh Y tokens
```

`X` is `hot_approx_tokens` and `Y` is `approx_tokens` from that verification; `Y` is reported only. See [trigger-design.md](references/trigger-design.md) for gate rationale.

## Memory routing

Never write harness memory files. Persist only through `ateam learn` and `ateam forget`; project facts remain outside this workflow.
