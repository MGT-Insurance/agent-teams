Cloud config for a Dispatcher ateam-dri run. Extends the shipped dri role; never overrides its guardrails.

## This run has no human at the terminal
- `AskUserQuestion` is disabled. To get any human decision, raise `ateam gate`. The steward forwards the gate to the human and relays the answer.
- `ateam` home is the hosted workspace, not a laptop `~/.agent-teams`. Read and write beads, learnings, and mail through `ateam` and `bd` only.
- When you have nothing to do until a gate clears or a teammate reports, end your turn. After consecutive idle polls the run parks. It resumes with a wake event or new mail: run `ateam mail inbox` to read it.
- The run ends when the initiative is closed. Closing it stops cloud-boot and the session.

## Restore saved work
When a run fails, cloud-boot pushes its unpushed work to a wip branch and adds the note `wip saved: <ref> at <sha> from run <run bead>` to the initiative. Before any other work, restore that work:
1. Run `ateam show <initiative>`. The output wraps long note lines, so join a wrapped line before you read it.
2. For each `wip saved: <ref> at <sha>` note that has no later `wip restored: <ref> at <sha>` note, run `git fetch origin <ref>`.
3. If `git merge-base --is-ancestor <sha> HEAD` fails on the working branch, run `git merge <sha>`. If the merge conflicts, resolve it so that both sides' work stays. If you can't, raise a gate.
4. Write `wip restored: <ref> at <sha>` to a file and run `ateam note <initiative> --file=<path>`.

Don't delete the wip branch.

## Live UI checks
- Use the Dispatcher ui-test run tool (`ateam ui-test file`), not a local browser. Push the integration branch first, then brief the tester with the initiative id.
- `--attach` isn't delivered from a cloud run. Put `screenshot: <ui-test bead id>` in the gate text, and the steward posts the image.
- Don't start a ui-test watcher. The Dispatcher sends the `ui-test <id> <kind>` events.

## Learnings
- Learnings you write go to the hosted store. A laptop reads the same store when its config sets `cloud_home`.
- Run the wind-down condense as usual. A lock in the hosted store keeps a laptop and a cloud run from condensing at the same time.

## Checks (midgard)
- Give implementers and testers these rules. Use `pnpm lint:changed --typed` and `pnpm --filter <pkg> typecheck` once. Run tests with `pnpm vitest run <path> --no-file-parallelism`. Never run the full lint, typecheck, or suite.
- Pull request CI skips typecheck and the type-aware lint. They run only in the merge queue, so the local targeted check is the pre-merge signal.
- Commit locally and batch fixes into one push. After reviews resolve, comment `/test` once before queueing. Use one shared worktree per cohesive PR.
