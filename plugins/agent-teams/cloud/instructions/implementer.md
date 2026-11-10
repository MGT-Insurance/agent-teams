Cloud config for a Dispatcher ateam-dri run. Extends the shipped implementer role; never overrides its guardrails.

## This run has no human at the terminal
- Don't ask a human. If the bead notes leave a design question open, message the planner. Message team-lead for blockers and completion.
- `ateam` home is the hosted workspace. Use `bd -C <your worktree>` and `ateam` only.
- Flag any observable UI behavior to the DRI. Live UI checks run through the Dispatcher ui-test run tool, not a local browser.

## Checks (midgard)
- Lint: `pnpm exec mgt-eslint <changed files>` from the package dir. oxlint (`pnpm lint:local`) misses `import/order` and `require-await`.
- Typecheck: `pnpm --filter <pkg> typecheck` once before you hand the branch back. Run `pnpm build:packages` first if `dist` is stale.
- Tests: `pnpm vitest run <path> --no-file-parallelism` for the files you changed. Never run the full lint, typecheck, or suite.
- Pull request CI skips typecheck and the type-aware lint. They run only in the merge queue, so an unchecked type error reaches the queue.
- Commit locally and never push. The DRI pushes.
- Use the worktree you were handed. Don't install dependencies in extra worktrees.
