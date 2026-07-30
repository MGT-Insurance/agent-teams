# Wind-down checklist — run when the initiative reaches delivered (or is paused long-term)

In order; do not skip items because the session is long — this list exists precisely because context is thinnest now.

1. Teammates: SendMessage shutdown_request to every live member; confirm terminations.
2. Worktrees: `git worktree remove` each track worktree; `git worktree prune`; delete track branches.
3. Orphaned processes: check for leaked test runners/dev servers (`ps` for watch-mode workers; free known ports). Kill by explicit PID.
4. Project beads: close finished, annotate in-progress, file discovery beads for anything unresolved.
5. Push the PROJECT repo (the branch backing the PR) — work is not done until pushed.
6. Audit + sync the GLOBAL workspace: `ateam audit` first — must be clean (no work beads leaked into the registry; any offender belongs in a project repo, move it and delete it from the workspace). Then `ateam sync` (Dolt remote configured at setup; if push fails with "no remote", re-run `bd dolt remote add origin <url>` against the workspace).
7. Learnings: contribute `dri:<slug>` entries for transferable orchestration insights — write to a temp file, then `ateam learn dri <slug> --file <tmpfile>`.
8. Drain + condense all roles: run the `/agent-teams:condense` skill (no arg) — lock-guarded, skips cleanly if another session holds the lock, skips cheaply per-role when nothing is over threshold. See the skill for the full procedure.
9. Registry: final status note. Close ONLY when the PR is merged or a human explicitly closes it — a delivered-but-unmerged PR stays `awaiting-merge` and **OPEN** so a future no-parameter /dri can resume it. On merge, run the Phase 5 close-out sequence (clear-gate → close → update-local-main.sh). A long-term pause is annotated, not closed.
10. End the turn — do not self-stop (SKILL.md Phase 6, "End-state").
