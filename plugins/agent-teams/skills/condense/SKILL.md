---
name: condense
description: Manual trigger to condense a role's learnings memory into the succinct hot layer; no-arg discovers and condenses all learning roles.
---

**The `ateam` tool.** `ateam` is on PATH — it ships as a prebuilt binary in the plugin's `bin/` (auto-added to PATH; installed/verified by `/setup-agent-teams`). Call it as bare `ateam` everywhere.

## Parse the argument

- **`/agent-teams:condense <role>`** — condense ONLY that role (e.g. `dri`, `implementer`).
- **`/agent-teams:condense` (no arg)** — run `ateam roles` to discover all role namespaces, then condense each EXCEPT `user`. The `user:` namespace is served by `ateam prime` (capped and truncated at read time) and is NOT part of the hot/cold learnings model — skip it unconditionally. Roles to condense are the learning roles: `dri`, `planner`, `implementer`, `tester`, `reviewer` (and any others returned by `ateam roles` that are not `user`).

## Condense procedure (repeat for each target role)

This procedure is autonomous — NO human-review gate. Safety rests on Dolt history recoverability and the per-role change-summary line you emit.

### Step 1 — Gather

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

Read ALL memory bodies from this packet. These are the full cold + hot contents for the role.

### Step 2 — Design the hot set (BEFORE writing anything)

IMPORTANT ORDERING: do not create any `<role>:hot:*` key until the full hot set is decided, then create them as a batch. The moment one hot key exists, `ateam learnings <role>` serves ONLY hot keys — a partial hot set would under-serve the next session.

Design principles:
- Select the highest-signal learnings: recurring process rules, hard-won gotchas, ship constraints, cardinal rules — anything whose loss causes a wrong or expensive action.
- MERGE overlapping learnings into single succinct entries. This is where most token reduction comes from.
- Write each entry "as succinct as possible while still COMPLETE" — keep every load-bearing detail (file paths, exact commands, the WHY).
- Target <= 6000 tokens (~24KB) / ~15-25 items total across all hot keys.
- Assign each hot entry a meaningful slug (e.g. `hot:cardinal-rule`, `hot:ship-constraint`).

### Step 3 — Apply (batch write, then cleanup)

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
- For each cold `<role>:<slug>` whose content is NOW FULLY CAPTURED in a hot entry: `ateam forget <role> <slug>` (removes the redundant original — recoverable via Dolt history).
- LEAVE IN COLD any learning not promoted (the long tail stays searchable, not injected).
- EVICT (forget) ONLY exact duplicates or clearly-superseded items. When in doubt, keep in cold. Conservative: NO eviction floor, but evict little.

If you are refreshing an existing hot key, `ateam learn <role> hot:<slug>` is an UPSERT — it overwrites in place.

If you restructure the hot set (e.g. merge several old hot entries into fewer new ones), you MUST `ateam forget <role> hot:<old-slug>` for every old hot key that is NOT present in the new hot set. Skipping this step leaves stale hot entries that linger and bloat the injected layer.

### Step 4 — Verify

```bash
ateam learnings <role>
```

Confirm output shows only the hot set and is <= ~24KB. Then spot-check cold:

```bash
ateam recall <role> <term>
```

Confirm cold memories are still reachable for a representative term.

### Step 5 — Emit summary line

Emit one line to the user:

```
<role>: promoted N / merged M / evicted K / hot now X tokens
```

Where:
- N = number of net-new hot entries (keys that did not previously have `hot:` form)
- M = number of cold entries merged into a single hot entry (count source entries collapsed)
- K = number of cold entries removed via `ateam forget`
- X = approximate token count of the current hot set (estimate from character count / 4)

## Handling multiple roles (no-arg form)

Process roles sequentially. After each role, emit its summary line before starting the next. If a role returns zero memories from `ateam condense <role>`, skip it with a one-line note: `<role>: no memories — skipped`.

## Memory routing reminder

Do NOT write any MEMORY.md files or Claude file-based memories here. All persistence goes through `ateam learn` / `ateam forget`.
