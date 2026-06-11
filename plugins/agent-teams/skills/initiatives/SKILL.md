---
name: initiatives
description: Machine-wide dashboard of agent-teams initiatives. Shows every registered initiative (one line each) with phase and highlights the ones parked waiting on a human, with their questions. Use when asked "what's running", "what needs me", "initiative status", or /initiatives.
---

Render the initiative dashboard from the global workspace. If the launcher is absent or `~/.agent-teams/bin/at ws` fails, say so and point at /setup-agent-teams.

1. Open initiatives:
   ```bash
   ~/.agent-teams/bin/at list-json
   ```
   Each element includes `id`, `title`, `description` (the full line-oriented registry schema), `labels`, and notes. The `description` field contains the `branch:`, `team:`, and latest phase state.

2. Parked gates — `~/.agent-teams/bin/at human-list` is the canonical needs-human view:
   ```bash
   ~/.agent-teams/bin/at human-list
   ```
   Issues carrying the `human` label (set by the gate protocol via `~/.agent-teams/bin/at gate`) appear here. The `labels` field in the JSON output also shows it (look for `"human"` in the array).

3. For each initiative output ONE line: `<id> <title> — <branch> — <latest phase note>`, ordered needs-human first.

4. For each needs-human initiative, add an indented line with the parked question(s) from its latest gate note, so the human can answer without digging.

5. If nothing is open: say exactly that, one line.

Read-only: this skill never modifies the registry.
