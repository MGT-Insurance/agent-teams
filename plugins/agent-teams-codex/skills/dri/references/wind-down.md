# Shared wind-down contract

Wind down in order: settle delegated work; remove only this initiative's track worktrees and processes; close or annotate project beads; commit and push the project branch; run `ateam audit` then `ateam sync`; route durable knowledge through `ateam learn` or project `bd remember`; and record the final initiative note.

Delivered but unmerged work remains open and review-gated. On confirmed merge, clear the gate before closing. End the turn without terminating the runtime daemon or deleting the owning session/thread.

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
8. Record the final initiative note. Leave delivered work open until merge;
   on merge clear its gate before closing it.
9. End the turn. Do not stop the managed app-server daemon or delete the Codex
   thread.
