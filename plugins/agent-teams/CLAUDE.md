# agent-teams

This plugin hard-requires **beads** (`bd`) — all work tracking is beads-first. Never use TodoWrite/TaskCreate/markdown TODO lists in agent-teams workflows.

**Global workspace:** `~/.agent-teams` — a git-backed beads workspace holding role learnings and the initiative registry (one bd issue per initiative). Access is via the bundled `scripts/ateam` script, invoked by its absolute path resolved from the plugin directory each session (no symlink, no install step). Skills resolve `<plugin-root>/scripts/ateam` at load time; agents receive the resolved absolute path in their spawn instructions. If the workspace does not exist, run `/setup-agent-teams`.

**Beads runtime:** embedded mode (no server daemon needed). Agent isolation uses git **worktrees** of the project repo, not independent clones — worktrees share the project's single `.beads/` issue DB via git-common-dir discovery; clones each get a separate, fragmented beads workspace.

**Skills:** `/dri <problem>` — run/resume an initiative as DRI. `/initiatives` — machine-wide initiative dashboard. `/setup-agent-teams` — one-time machine setup.
