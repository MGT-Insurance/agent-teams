# Shared gate contract

Every human dependency follows one sequence: record the gate atomically on the global initiative with `ateam gate`, state the decision, then park the DRI while independent work may continue. Prefer the structured `--decision`/`--recommendation`/`--alternative` form; use `--file` when the ask cannot fit. Question gates use `--kind=question`; delivery uses `--kind=review`. Clear a resolved gate with `ateam clear-gate <id> [--file <response-file>]`. Never substitute raw global-workspace `bd` commands.

Any departure from the human's named mechanism, reuse path, or scope class is a mandatory QUESTION gate at the moment of divergence. Include mechanism evidence, the recommended design, and the literal-reading alternative. Prior approval or a skipped plan gate never approves a new mechanism.

A review gate means the PR is ready for the human. Only the human may assert that review is finished: a DRI never runs `ateam handoff`. Never merge without explicit human confirmation.
