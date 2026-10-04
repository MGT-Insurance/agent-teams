
# Human gates

Every human dependency must be recorded in the global initiative and then
parked. Prefer:

```bash
ateam gate <id> --decision "<one line>" \
  --recommendation "<short default>" \
  --alternative "<key alternative>" [--context-file <file>]
```

Use `--file` for prose that does not fit. Delivery uses
`--file <file> --kind=review`; decisions use the default `--kind=question`; a
tester's live pass that closes the engineering loop uses
`--kind=live-test-review --attach <path> [--attach <path> ...] --file <summary-file>`
— DRI-owned, raised before any reviewer spawn or Phase 5 PR prep. `review`
outranks `live-test-review`, which outranks `question`, when the dashboard
resolves an initiative's displayed kind. All three add `human` and a
`gate:*` label atomically. Never substitute raw `bd` human commands. None of
these three kinds is ever an ask to merge — the ask is always review; merge
is the human's own later call.

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
