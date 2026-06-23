---
name: initiatives
description: Machine-wide dashboard of agent-teams initiatives. Renders every registered initiative as a compact table (status icon, id, title, phase, where) and footnotes the questions for any parked waiting on a human. Use when asked "what's running", "what needs me", "initiative status", or /initiatives.
---

**The `ateam` tool.** `ateam` is on PATH — it ships as a prebuilt binary in the plugin's `bin/` (auto-added to PATH; installed/verified by `/setup-agent-teams`). Call it as bare `ateam` everywhere.

Render the initiative dashboard from the global workspace. If `ateam ws` fails or `ateam` is not found, say so and point at /setup-agent-teams.

## Data source

Call `ateam execution-status`. This is the ONLY data source for status. Do NOT infer phase, reviewability, or execution state from labels, notes prose, or any other field — `execution_status` is computed deterministically in Go and you render it as-is. The verb always emits JSON.

```bash
ateam execution-status
```

Each element in the returned array has:

| field | content |
|-------|---------|
| `id` | initiative bead id (e.g. `at-zot`) |
| `title` | initiative title |
| `worktree` | worktree path (informational) |
| `labels` | raw label array |
| `execution_status` | one of `NEEDS-DECISION`, `IN-PROGRESS`, `REVIEWABLE`, `unknown` |
| `ask` | structured ask block or `null` — `{ decision, recommendation, alternative, context? }` |
| `pr` | GitHub PR URL string, or `""` |

## Render: three-tier ranked list

Render a single ranked list in three tiers, ordered by urgency:

### Tier 1 — NEEDS DECISION (`execution_status == "NEEDS-DECISION"`)

Requires immediate human input. Sort first, mark with `⚠`.

For each: show id and title on one line, then the crisp ask block expanded below it:

```
⚠ at-abc  My initiative title
    decide: <ask.decision>
    recommend: <ask.recommendation>
    alternative: <ask.alternative>
    context: <ask.context>    ← omit if empty
```

If `ask` is null (no structured block), show the raw notes in place of the structured fields (backward-compat for pre-sentinel gates). If notes are also empty, just show `(no details)`.

### Tier 2 — REVIEWABLE (`execution_status == "REVIEWABLE"`)

PR is ready and the agent is not actively working. Show id, title, and the PR link. Mark with `✅`.

```
✅ at-def  Another initiative   PR: <pr url>
```

If `pr` is empty, omit the PR field and note `(no PR link)`.

### Tier 3 — IN PROGRESS (`execution_status == "IN-PROGRESS"`)

Agent is working or no gate is set — do not touch. Mark with `▶`. Show id and title only.

```
▶ at-ghi  Third initiative
```

### Unknown status (`execution_status == "unknown"`)

Group under Tier 3 with `(state unknown)` appended.

```
▶ at-xyz  Some initiative   (state unknown)
```

## Format

- If nothing is open: say exactly that, one line. No empty table.
- Keep each row terse — Tier 1 is the only place multi-line content appears.
- Separate tiers with a blank line and a heading (`## Needs decision`, `## Ready to review`, `## In progress`).

## Workspace health

Run `ateam audit`. On a clean result, add a single terse line at the bottom (e.g. `_audit clean · no leaked work beads_`). If audit reports leaked work beads (feature/plan/discovery beads that belong in a project repo), surface them under a `⚠ leaked work beads` heading.

Read-only: this skill never modifies the registry (the audit is read-only too — it reports, it does not delete).
