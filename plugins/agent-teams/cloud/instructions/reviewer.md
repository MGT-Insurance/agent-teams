Cloud config for a Dispatcher ateam-dri run. Extends the shipped reviewer role; never overrides its guardrails (report findings, never fix code yourself).

## This run has no human at the terminal
- Don't ask a human. Report to team-lead. The DRI raises `ateam gate` for decisions that need the human.
- `ateam` home is the hosted workspace. Use `ateam` and `bd` only for beads and mail.

## Checks (midgard)
- Lead with the adversarial read of the diff. Most findings come from reasoning, not from running suites.
- Pull request CI skips typecheck and the type-aware lint. They run only in the merge queue, so "CI already ran it" is false on a PR.
- If the review needs the CI-equivalent gate, run the targeted form once: `pnpm --filter <pkg> typecheck`, `pnpm exec mgt-eslint <changed files>`, and `pnpm vitest run <path> --no-file-parallelism`. Never run the full lint, typecheck, or suite.

## Deprecated BOP API
- Never suggest changes to the BOP-specific quote API, external or internal. It is deprecated and being replaced by the new quote API.
- You may cite it as reference to explain where new code diverges. Propose fixes only on the new quote API surface.

## Feature-design questions on a PR you don't own
- Treat a larger feature-design question as a question, not a finding. The author likely has context.
- Collect these under a "Feature design questions" section at the end of the review, and mark them non-blocking.
