Cloud config for a Dispatcher ateam-dri run. Extends the shipped dri role; never overrides its guardrails.

## This run has no human at the terminal
- `AskUserQuestion` is disabled. To get any human decision, raise `ateam gate`. The steward forwards the gate to the human and relays the answer.
- `ateam` home is the hosted workspace, not a laptop `~/.agent-teams`. Read and write beads, learnings, and mail through `ateam` and `bd` only.
- When you have nothing to do until a gate clears or a teammate reports, end your turn. After consecutive idle polls the run parks. It resumes with a wake event or new mail: run `ateam mail inbox` to read it.
- The run ends when the initiative is closed. Closing it stops cloud-boot and the session.

## Live UI checks
- Use the Dispatcher ui-test run tool (`ateam ui-test file`), not a local browser. Push the integration branch first, then brief the tester with the initiative id.
- `--attach` isn't delivered from a cloud run. Put `screenshot: <ui-test bead id>` in the gate text, and the steward posts the image.
- Don't start a ui-test watcher. The Dispatcher sends the `ui-test <id> <kind>` events.

## Learnings
- Don't run the condense skill or fresh-drain. A separate cloud curator job does that.
- Learnings you write here reach the laptop when the human runs `ateam learnings-sync` there. At wind-down, skip that step in this run.

## Checks (midgard)
- Give implementers and testers these rules. Use `pnpm exec mgt-eslint <changed files>` and `pnpm --filter <pkg> typecheck` once. Run tests with `pnpm vitest run <path> --no-file-parallelism`. Never run the full lint, typecheck, or suite.
- Pull request CI skips typecheck and the type-aware lint. They run only in the merge queue, so the local targeted check is the pre-merge signal.
- Commit locally and batch fixes into one push. After reviews resolve, comment `/test` once before queueing. Use one shared worktree per cohesive PR.
