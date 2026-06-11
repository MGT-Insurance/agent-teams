---
name: initiatives
description: Machine-wide dashboard of agent-teams initiatives. Shows every registered initiative (one line each) with phase and highlights the ones parked waiting on a human, with their questions. Use when asked "what's running", "what needs me", "initiative status", or /initiatives.
---

**The `ateam` tool.** Your plugin directory is injected at load time. The workspace tool is at `<plugin-root>/scripts/ateam` (from a skill at `plugins/agent-teams/skills/initiatives/SKILL.md`, that's two levels up from the skill dir, then `scripts/ateam`). Resolve this to its absolute path once and write that LITERAL absolute path wherever this document shows `<ateam>` below. Do NOT assign it to a shell variable (a `$VAR` re-introduces the unsilenceable expansion prompt) — write the literal path each time.

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

3. For each initiative output ONE line: `<id> <title> — <branch> — <latest phase note>`, ordered needs-human first.

4. For each needs-human initiative, add an indented line with the parked question(s) from its latest gate note, so the human can answer without digging.

5. If nothing is open: say exactly that, one line.

Read-only: this skill never modifies the registry.
