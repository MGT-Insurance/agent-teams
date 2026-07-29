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
| `execution_status` | one of `NEEDS-DECISION`, `STALE-MERGED`, `REVIEWABLE`, `AWAITING-EXTERNAL-REVIEW`, `IN-PROGRESS`, `unknown` |
| `ask` | structured ask block or `null` — `{ decision, recommendation, alternative, context? }` |
| `pr` | GitHub PR URL string, or `""` |
| `pr_probe` | `"ok"` (merge state was checked) · `"unreachable"` (a check was attempted and failed) · `"none"` (no PR URL — never checked, not a fault) |

## Render: five-tier ranked list

Render a single ranked list in five tiers, ordered by how much of Eric's attention the row needs:

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

### Tier 2 — MERGED, CLOSE IT OUT (`execution_status == "STALE-MERGED"`)

The PR is merged or closed on GitHub but the initiative is still open. This is a 10-second cleanup, **not a review** — say so, and never render it as work to review. Mark with `☑`. Outranks REVIEWABLE deliberately: it is the cheapest row on the board to clear.

```
☑ at-jkl  Finished initiative   PR merged — close the initiative   PR: <pr url>
```

### Tier 3 — REVIEWABLE (`execution_status == "REVIEWABLE"`)

Genuinely awaiting Eric: a PR is ready, no agent is actively working, and Eric has not said he is done looking at it. Show id, title, and the PR link. Mark with `✅`.

```
✅ at-def  Another initiative   PR: <pr url>
```

If `pr` is empty, omit the PR field and note `(no PR link)`.

Where `pr_probe == "unreachable"`, append `(merge state unknown)`. Do NOT imply the merge state was checked — it was not, and a row here may in fact already be merged:

```
✅ at-def  Another initiative   PR: <pr url>   (merge state unknown)
```

### Tier 4 — IN PROGRESS (`execution_status == "IN-PROGRESS"`)

Agent is working or no gate is set — do not touch. Mark with `▶`. Show id and title only.

```
▶ at-ghi  Third initiative
```

### Tier 5 — WITH REVIEWERS (`execution_status == "AWAITING-EXTERNAL-REVIEW"`)

Eric has **declared** he is done looking (he ran `ateam handoff`); the PR is with the team. **Not in his action queue** — it ranks below REVIEWABLE, at the bottom of the board, and reads as a standing state rather than a request. Mark with `⋯`.

```
⋯ at-mno  Handed-off initiative   you've already looked at this; it's with the team   PR: <pr url>
```

Two things this tier must never do:

- **Never name reviewers.** We do not have them and will not fetch them — `execution-status` never asks GitHub who is assigned, because auto-assignment makes that field meaningless. Any phrasing like "waiting on `<login>`" is wrong.
- **Never suggest the system worked this out from GitHub.** This state exists only because Eric said so; nothing in GitHub records the moment he finished looking.

Because it is declared rather than derived, this tier is **unaffected by `pr_probe`** — it stays correct even when the probe is `"unreachable"`. The asymmetry is deliberate: an unreachable probe degrades only Tier 2 (a merged PR keeps reporting as REVIEWABLE, or as this tier, until the probe recovers).

### Unknown status (`execution_status == "unknown"`)

Group under Tier 4 with `(state unknown)` appended.

```
▶ at-xyz  Some initiative   (state unknown)
```

## Format

- If nothing is open: say exactly that, one line. No empty table.
- Keep each row terse — Tier 1 is the only place multi-line content appears.
- Separate tiers with a blank line and a heading, in tier order: `## Needs decision`, `## Merged — close out`, `## Ready to review`, `## In progress`, `## With reviewers`.
- Omit any tier with no rows — no empty headings.

## Workspace health

Run `ateam audit`. On a clean result, add a single terse line at the bottom (e.g. `_audit clean · no leaked work beads_`). If audit reports leaked work beads (feature/plan/discovery beads that belong in a project repo), surface them under a `⚠ leaked work beads` heading.

Read-only: this skill never modifies the registry (the audit is read-only too — it reports, it does not delete).
