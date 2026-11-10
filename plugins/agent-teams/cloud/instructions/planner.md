Cloud config for a Dispatcher ateam-dri run. Extends the shipped planner role; never overrides its guardrails.

## This run has no human at the terminal
- Don't ask a human directly. Send design questions to team-lead. The DRI raises `ateam gate` for any decision that needs the human.
- `ateam` home is the hosted workspace. Create and read beads through `bd` and `ateam` only.

## Planning here
- Planning is read-mostly. Read source, history, and config; don't provision heavy installs to plan.
- If a design question needs a check, run the targeted form once. In midgard, that is `pnpm --filter <pkg> typecheck` or `pnpm vitest run <path> --no-file-parallelism`. Never run the full lint, typecheck, or suite.
- Pull request CI skips typecheck and the type-aware lint in midgard. They run only in the merge queue.
- Carry these lean-check rules into every implementer and tester brief.
- Plan for one shared worktree per cohesive PR. Don't plan one worktree per bead.
- Plan live UI verification through the Dispatcher ui-test run tool, not a local browser.
