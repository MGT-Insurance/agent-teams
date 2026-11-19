Cloud config for a Dispatcher ateam-dri run. Extends the shipped dri role; never overrides its guardrails.

## This run has no human at the terminal
- `AskUserQuestion` is disabled. To get any human decision, raise `ateam gate`. The steward forwards the gate to the human and relays the answer.
- `ateam` home is the hosted workspace, not a laptop `~/.agent-teams`. Read and write beads, learnings, and mail through `ateam` and `bd` only.
- When you have nothing to do until a gate clears or a teammate reports, end your turn. After consecutive idle polls the run parks. It resumes with a wake event or new mail: run `ateam mail inbox` to read it.
- The run ends when the initiative is closed. Closing it stops cloud-boot and the session.

## Ownership and judgment
- Built means merged. You own your PRs through merge under Eric's standing bar: every finding addressed, plus 2 human approvals, or Canary auto-approve, or 1 approval after an hour. A relayed "merge under Eric's bar" is his authorization; don't wait for fresh confirmation.
- Never park with an owned unmerged PR or a live teammate. A parked run wakes only on mail or a comment, and its background teammate dies on park. If you must park, mail the steward the exact event you wait on.
- If a required reviewer holds a PR that has other approvals and green checks, ask Canary to drop that reviewer at once. Don't ask Eric first.
- Before asking Eric an open config or design question, find the existing convention in the codebase. Otherwise decide on the merits and say where you departed. Escalate only taste, risk acceptance, or things only Eric can do.
- Eric measures trust by how little he has to redirect you.

## Restore saved work
Cloud-boot restores saved work before the session starts. You don't restore it yourself.
- If the system prompt has a "Needs you" list from cloud-boot, handle each item before other work. Cloud-boot did not touch those items.
- Worktrees under "Restored at boot" are already in place. Their work is committed but not pushed.

Don't delete a wip branch.

## Live UI checks
- Use the Dispatcher ui-test run tool (`ateam live-test file`), not a local browser. Push the integration branch first, then brief the tester with the initiative id.
- `--attach` isn't delivered from a cloud run. Put `screenshot: <ui-test bead id>` in the gate text, and the steward posts the image.
- Don't start a ui-test watcher. The Dispatcher sends the `ui-test <id> <kind>` events.

## Learnings
- Learnings you write go to the hosted store. A laptop reads the same store when its config sets `cloud_home`.
- Condense right before you close the initiative, on every close path, including the merge close-out. Closing ends this run, so run `/agent-teams:condense` (no argument) after `ateam clear-gate` and before `ateam close`. It takes the shared lock in the hosted store and skips in seconds when no role has enough new learnings.

## Checks (midgard)
- Give implementers and testers these rules. Use `pnpm lint:changed --typed` and `pnpm --filter <pkg> typecheck` once. Run tests with `pnpm vitest run <path> --no-file-parallelism`. Never run the full lint, typecheck, or suite.
- Pull request CI skips typecheck and the type-aware lint. They run only in the merge queue, so the local targeted check is the pre-merge signal.
- Commit locally and batch fixes into one push. After reviews resolve, comment `/test` once before queueing. Use one shared worktree per cohesive PR.
