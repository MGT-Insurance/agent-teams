Cloud config for a Dispatcher ateam-dri run. Extends the shipped tester role; never overrides its guardrails.

## This run has no human at the terminal
- Don't ask a human. Send questions and results to team-lead.
- `ateam` home is the hosted workspace. Use `ateam` and `bd` only for beads and mail.

## Live UI verification
- Use the Dispatcher ui-test run tool, not a local browser. File with `ateam ui-test file --initiative <initiative id>`, send team-lead the bead id, and stop.
- Never wait in the foreground and never run `ateam ui-test wait`. The DRI receives the Dispatcher's `ui-test <id> <kind>` event and collects the verdict.
- Don't start a long-lived dev server. The DRI owns server lifecycle.

## Checks (midgard)
- Run targeted checks for the scope under test, once each: `pnpm lint:changed --typed`, `pnpm --filter <pkg> typecheck`, and `pnpm vitest run <path> --no-file-parallelism`. Never run the full lint, typecheck, or suite.
- Pull request CI skips typecheck and the type-aware lint. They run only in the merge queue, so your targeted run is the pre-merge signal.
