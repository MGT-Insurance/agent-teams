# agent-teams

This plugin hard-requires **beads** (`bd`) — all work tracking is beads-first. Never use TodoWrite/TaskCreate/markdown TODO lists in agent-teams workflows.

**Global workspace:** `~/.agent-teams` — a git-backed beads workspace holding role learnings and the initiative registry (one bd issue per initiative). Access is via the `~/.agent-teams/bin/at` launcher (installed by `/setup-agent-teams`); never call `bd -C "${AGENT_TEAMS_HOME…}"` directly from skills or agents — use `~/.agent-teams/bin/at <verb>` instead. If the workspace does not exist, run `/setup-agent-teams`.

**Skills:** `/dri <problem>` — run/resume an initiative as DRI. `/initiatives` — machine-wide initiative dashboard. `/setup-agent-teams` — one-time machine setup.
