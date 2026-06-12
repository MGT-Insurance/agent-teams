---
name: initiatives
description: Machine-wide dashboard of agent-teams initiatives. Renders every registered initiative as a compact table (status icon, id, title, phase, where) and footnotes the questions for any parked waiting on a human. Use when asked "what's running", "what needs me", "initiative status", or /initiatives.
---

**The `ateam` tool.** Your plugin directory is injected at load time. The workspace tool is at `<plugin-root>/scripts/ateam` (from a skill at `plugins/agent-teams/skills/initiatives/SKILL.md`, that's two levels up from the skill dir, then `scripts/ateam`). Resolve this to its absolute path once and write that LITERAL absolute path wherever this document shows `<ateam>` below. Do not assign it to a shell variable — write the literal path each time.

Render the initiative dashboard from the global workspace. If the `<ateam>` script is absent or `<ateam> ws` fails, say so and point at /setup-agent-teams.

1. Open initiatives:
   ```bash
   <ateam> list-json
   ```
   Each element includes `id`, `title`, `description` (the full line-oriented registry schema), `labels`, and notes. The `description` field contains the `branch:`, `team:`, and latest phase state.

2. Parked gates — `<ateam> human-list` is the canonical needs-human view:
   ```bash
   <ateam> human-list
   ```
   Issues carrying the `human` label (set by the gate protocol via `<ateam> gate`) appear here. The `labels` field in the JSON output also shows it (look for `"human"` in the array).

3. Render ONE markdown table, ordered needs-human first. Columns, in order:

   | column | content |
   |--------|---------|
   | (icon) | status at a glance: `⚠` needs human · `✅` delivered (PR open/awaiting merge) · `🔍` investigating/root-causing · `▶` executing · `📋` planning · `⏸` parked/blocked (non-human). Pick the one that fits the latest phase note; when in doubt use the closest. |
   | ID | the initiative id (e.g. `at-zot`) |
   | Title | the initiative title, untruncated unless absurdly long |
   | Phase | a SHORT phase token distilled from the latest note (e.g. `planning`, `executing`, `root-caused`, `delivered`) — NOT a sentence |
   | Where | `PR #N` if a PR exists, else the `branch:` name |

   Keep cells terse — the table is the at-a-glance view, not the full story. Do not add prose commentary per row.

4. Below the table, for each needs-human initiative ONLY, add one footnote line: `⚠ <id>: <parked question(s) from the latest gate note>`. This is the one place full question text belongs — cells stay short.

5. If nothing is open: say exactly that, one line. (No empty table.)

6. Workspace health: run `<ateam> audit`. The global workspace must contain ONLY initiative-tracking beads + role memories. On a clean result, add a single terse line under the table (e.g. `_audit clean · no leaked work beads_`). If audit reports leaked work beads (feature/plan/discovery beads that belong in a project repo), surface them under a `⚠ leaked work beads` heading so they get cleaned up — this is a recurring drift worth catching every time the dashboard runs.

Read-only: this skill never modifies the registry (the audit is read-only too — it reports, it does not delete).
