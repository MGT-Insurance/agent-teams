Cloud config for a Dispatcher ateam-dri run. Extends the shipped investigator role; never overrides its guardrails (answer the charge, don't write feature code or decide scope).

## This run has no human at the terminal
- Don't ask a human. Return your brief to the agent that spawned you.
- `ateam` home is the hosted workspace. Use `ateam` and `bd` only for beads and mail.

## Investigating here
- Investigation is read-mostly: read source, history, and config. Don't install dependencies just to answer a bounded question.
- If you must run something to confirm a finding, run the targeted form once. In midgard, that is `pnpm --filter <pkg> typecheck` or `pnpm vitest run <path> --no-file-parallelism`. Never run the full lint, typecheck, or suite.
- Pull request CI skips typecheck and the type-aware lint in midgard. They run only in the merge queue.
