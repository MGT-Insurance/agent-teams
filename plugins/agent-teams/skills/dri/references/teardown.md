# Teardown checklist — run when the initiative reaches delivered (or is paused long-term)

In order; do not skip items because the session is long — this list exists precisely because context is thinnest now.

1. Teammates: SendMessage shutdown_request to every live member; confirm terminations.
2. Worktrees: `git worktree remove` each track worktree; `git worktree prune`; delete track branches.
3. Orphaned processes: check for leaked test runners/dev servers (`ps` for watch-mode workers; free known ports). Kill by explicit PID.
4. Project beads: close finished, annotate in-progress, file discovery beads for anything unresolved.
5. Push the PROJECT repo (the branch backing the PR) — work is not done until pushed.
6. Sync the GLOBAL workspace: `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" dolt push` (Dolt remote configured at setup via `bd dolt remote add origin <url>` — done by /setup-agent-teams; if push fails with "no remote", re-run that command against the workspace).
7. Learnings: contribute `dri:<slug>` entries for transferable orchestration insights (`bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" remember --key "dri:<slug>" ...`).
8. Registry: final status note; close the initiative with the PR link (if delivered).
