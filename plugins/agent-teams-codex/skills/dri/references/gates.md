# Shared gate contract

Every human dependency follows one sequence: record the gate atomically on the global initiative with `ateam gate`, state the decision, then park the DRI while independent work may continue. Prefer the structured `--decision`/`--recommendation`/`--alternative` form; use `--file` when the ask cannot fit. Question gates use `--kind=question`; delivery uses `--kind=review`. Clear a resolved gate with `ateam clear-gate <id> [--file <response-file>]`. Never substitute raw global-workspace `bd` commands.

Any departure from the human's named mechanism, reuse path, or scope class is a mandatory QUESTION gate at the moment of divergence. Include mechanism evidence, the recommended design, and the literal-reading alternative. Prior approval or a skipped plan gate never approves a new mechanism.

A review gate means the PR is ready for the human. Only the human may assert that review is finished: a DRI never runs `ateam handoff`. Never merge without explicit human confirmation.

# Human gates

Every human dependency must be recorded in the global initiative and then
parked. Prefer:

```bash
ateam gate <id> --decision "<one line>" \
  --recommendation "<short default>" \
  --alternative "<key alternative>" [--context-file <file>]
```

Use `--file` for prose that does not fit. Delivery uses
`--file <file> --kind=review`; decisions use the default `--kind=question`.
Both add `human` and a `gate:*` label atomically. Never substitute raw `bd`
human commands.

After raising a gate, state the question and end the turn. The managed Codex
thread remains durable; mail delivery or explicit `ateam resume` submits the
answer to it. Independent work may continue before parking, but never invent
the answer.

After a response, record it and run:

```bash
ateam clear-gate <id> --file <response-file>
```

Only the human runs `ateam handoff`. A DRI must never claim on the human's
behalf that review is complete.

A design pivot always gets a QUESTION gate at divergence. Include mechanism
evidence, the recommended design, and the literal implementation of the
original framing. Previous approval or a previous gate skip does not approve a
new mechanism.
