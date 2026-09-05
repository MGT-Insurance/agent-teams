
# Wind-down

1. Confirm no child is still running; wait or interrupt only this initiative's
   children.
2. Remove this initiative's track worktrees, prune, and delete their branches.
3. Stop only leaked processes started by this initiative, using explicit PIDs.
4. Close completed project beads and annotate or file remaining work under the
   root epic.
5. Commit and push the project branch.
6. Run `ateam audit`, then `ateam sync`.
7. Route any durable learning through `ateam learn` or `bd remember`, never a
   harness memory file.
8. Invoke `agent-teams-codex:condense` with no role argument.
9. Record the final initiative note. Leave delivered work open until merge;
   on merge clear its gate before closing it.
10. End the turn. Do not stop the managed app-server daemon or delete the Codex
   thread.
