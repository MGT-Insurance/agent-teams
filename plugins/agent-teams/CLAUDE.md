# agent-teams

This plugin hard-requires **beads** (`bd`) — all work tracking is beads-first. Never use TodoWrite/TaskCreate/markdown TODO lists in agent-teams workflows.

**Global workspace:** `${AGENT_TEAMS_HOME:-$HOME/.agent-teams}` — a git-backed beads workspace holding role learnings (`bd remember` memories with keys `<role>:<slug>`) and the initiative registry (bd issues, one per initiative). If it does not exist, run `/setup-agent-teams`.

**Skills:** `/dri <problem>` — run/resume an initiative as DRI. `/initiatives` — machine-wide initiative dashboard. `/setup-agent-teams` — one-time machine setup.
