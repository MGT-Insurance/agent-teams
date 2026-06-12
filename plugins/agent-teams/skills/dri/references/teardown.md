# Teardown checklist — run when the initiative reaches delivered (or is paused long-term)

In order; do not skip items because the session is long — this list exists precisely because context is thinnest now.

1. Teammates: SendMessage shutdown_request to every live member; confirm terminations.
2. Worktrees: `git worktree remove` each track worktree; `git worktree prune`; delete track branches.
3. Orphaned processes: check for leaked test runners/dev servers (`ps` for watch-mode workers; free known ports). Kill by explicit PID.
4. Project beads: close finished, annotate in-progress, file discovery beads for anything unresolved.
5. Push the PROJECT repo (the branch backing the PR) — work is not done until pushed.
6. Audit + sync the GLOBAL workspace: run `<ateam> audit` first — it must be clean (no work beads leaked into the registry; any offender belongs in a project repo, move it and delete it from the workspace). Then `<ateam> sync` (Dolt remote configured at setup via `bd dolt remote add origin <url>` — done by /setup-agent-teams; if push fails with "no remote", re-run that command against the workspace).
7. Learnings: contribute `dri:<slug>` entries for transferable orchestration insights — write to a temp file, then `<ateam> learn dri <slug> --file <tmpfile>`.
8. Registry: final status note. If delivered, record `awaiting-merge` with the PR link and leave the initiative **OPEN** — teardown dismantles the working team, but an opened-not-merged PR is not completion, so the initiative must stay resumable. Close ONLY when the PR is merged or a human explicitly closes it (e.g. `<ateam> close <id> --reason "merged: <PR URL>"`); a long-term pause is also annotated, not closed.
9. **Background sessions only — self-stop.** If `$CLAUDE_JOB_DIR` is set (bg mode) AND the terminal state is DONE (PR delivered with teardown complete; or a resume that just ran the close step; or a resume where awaiting-merge is still open and the human did not ask for more) AND no parked gate is pending: this is the very last action. Read `$CLAUDE_JOB_DIR`, take its final path segment as the session id (e.g. `/Users/x/.claude/jobs/c7b8e7c0` → `c7b8e7c0`), and run:
   ```
   claude stop c7b8e7c0
   ```
   with that literal id inlined. Do NOT use command substitution (`$(basename ...)` or backticks) — it trips an unsilenceable safety prompt. Interactive DRIs skip this step entirely; the human ends the session.
