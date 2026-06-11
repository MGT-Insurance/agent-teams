---
name: initiatives
description: Machine-wide dashboard of agent-teams initiatives. Shows every registered initiative (one line each) with phase and highlights the ones parked waiting on a human, with their questions. Use when asked "what's running", "what needs me", "initiative status", or /initiatives.
---

Render the initiative dashboard from the global workspace (`${AGENT_TEAMS_HOME:-$HOME/.agent-teams}`). If the workspace doesn't exist, say so and point at /setup-agent-teams.

1. Open initiatives:
   ```bash
   bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" list --status=open --json
   ```
   Each element includes `id`, `title`, `description` (the full line-oriented registry schema), `labels`, and notes. The `description` field contains the `branch:`, `team:`, and latest phase state.

2. Parked gates — `bd human list` is the canonical needs-human view:
   ```bash
   bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" human list
   ```
   Issues carrying the `human` label (set by the gate protocol via `bd label add <id> human`) appear here. The `labels` field in the JSON output also shows it (look for `"human"` in the array).

3. For each initiative output ONE line: `<id> <title> — <branch> — <latest phase note>`, ordered needs-human first.

4. For each needs-human initiative, add an indented line with the parked question(s) from its latest gate note, so the human can answer without digging.

5. If nothing is open: say exactly that, one line.

Read-only: this skill never modifies the registry.
