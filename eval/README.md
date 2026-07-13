# agent-teams eval suite

## ⚠️ This costs real money — read before running anything

`eval run` dispatches a **real, autonomous agent-teams DRI session** against
a fixture repo, and `eval collect` calls a **real `claude -p` LLM judge**.
Both spend real API tokens/dollars and real wall-clock time — observed: one
`opus-noadvisor` bugfix run cost **$8.86 and took 13 minutes**; runs can take
hours.

**Never run `eval run` or `eval collect`:**
- as part of building, testing, or verifying a code change ("just to check")
- from CI
- in a loop or script that retries automatically

This is not a unit test suite or an e2e suite. It is a human-operated,
serial harness for comparing agent-teams configurations against each other.
See [What is safe to run](#what-is-safe-to-run) below for the actual free path.

## What this is

`eval` (package `internal/eval`, CLI entry `cmd/eval`) is an A/B comparison
harness: it runs the same fixture task under two or more agent-teams
configurations (currently `opus-noadvisor` and `sonnet-advisor`, the frozen
v1 registry in `cmd/eval/main.go`) and compares them on cost, latency, tool
calls, turns, and correctness. It is **not** a pass/fail gate — `eval collect`
always produces descriptive metrics plus a correctness score, never a
binary "the build passed."

## Lifecycle

Run all commands below via `scripts/eval` — a thin wrapper that execs
`go run ./cmd/eval` from the repo root, so the CLI is always current with
the source tree (no `go build -o eval-bin` step to remember, and no stale
binary to trip over) and resolves `eval/runs` / `eval/tasks` correctly
regardless of your cwd.

1. **`scripts/eval run --task <path> --config <name>`** (or, for an ad hoc
   pair not in the frozen registry, **`scripts/eval run --task <path>
   --model <m> [--advisor <a>]`** — see [CLI reference](#cli-reference) for
   how the two forms interact) — loads a `TaskSpec` (see below), resolves its
   fixture repo to a local clone, and shells `ateam dispatch --repo <clone>
   --base-branch <FixtureRef> --problem <Problem> --slug <RunID> --model
   <cfg.DRIModel> [--advisor <cfg.Advisor>] --launch-prompt "/dri {id}"`.
   This launches a real DRI agent team that works autonomously — minutes to
   hours — against the fixture. `eval run` itself returns quickly once
   dispatch has started; it prints the `RunID` and writes
   `eval/runs/<RunID>/manifest.json`.
2. **Wait.** The dispatched team drives the task to a pushed branch on its
   own; there is nothing to poll from this CLI. This is why the harness is
   human-operated and serial by design — `eval run` refuses to reuse a
   `RunID` that already exists rather than trying to coordinate concurrent
   dispatches.
3. **`scripts/eval collect <RunID>`** — once the dispatched run has
   finished, this extracts cost/token/wall-clock/tool-call/turn metrics from
   the local Claude Code session data for the run's initiative (the same
   accounting `ateam cost` uses), runs the task's `buildCheck` in the run's
   worktree for an objective pass/fail floor, and calls a real LLM judge
   (`claude -p`, fixed to the `opus` model regardless of the run's own
   config) to score the produced diff against the task's
   `acceptanceCriteria`. This also spends money — a second real LLM call per
   `eval collect` invocation. Results are written to
   `eval/runs/<RunID>/result.json`. If Langfuse credentials are set (see
   below), the result is also pushed; otherwise `eval collect` prints
   `push skipped (no LANGFUSE_HOST set)` and exits successfully.
4. **`scripts/eval push <RunID>`** — backfills a Langfuse push for a run
   that was collected before credentials existed, reading the persisted
   `result.json`. Idempotent (trace id = RunID, dataset item id = TaskID).
5. **`scripts/eval clean <RunID>`** — removes the per-run git worktree and
   branch that `ateam dispatch` left behind under the fixture clone.
   Best-effort, not idempotent: re-running `clean` on an already-cleaned run
   is an error, not a no-op.

## Where results land

Every run gets a directory `eval/runs/<RunID>/`:

- `manifest.json` — written by `eval run`: `RunID`, `TaskID`, `Config`, the
  `ateam dispatch` initiative id, branch, worktree path, start time.
- `result.json` — written by `eval collect`: `RunID`, `TaskID`, `Config`,
  `Metrics` (cost/tokens/wall-clock/tool calls/turns), `Judge`
  (objective-floor pass + correctness score + per-criterion results +
  rationale).

`RunID` has the form `eval-<TaskID>-<ConfigHash>-<unixTimestamp>` — the
`eval-` prefix is deliberate and flows through `--slug` into the dispatched
session's worktree, branch, and `claude agents` identity, so every
eval-owned artifact on the machine (worktrees, branches, background
sessions) is identifiable at a glance as belonging to this harness.

`eval/runs/` is empty in a fresh checkout; run directories are created by
`eval run`/`eval collect`, not checked in.

## Task specs & fixtures

Task specs live in `eval/tasks/*.json` (one file, e.g.
`eval/tasks/webapp-bugfix-1.json`). Each `TaskSpec` has: `id`, `archetype`,
`runShape`, `fixtureRepo`, `fixtureRef` (the pinned tag/branch/commit the run
starts from), `problem` (handed to `/dri` verbatim), `acceptanceCriteria`
(consumed by the LLM judge), and `buildCheck` (a shell command run against
the produced diff — exit 0 is the objective-floor pass).

`fixtureRepo` is resolved to a local clone via `EVAL_FIXTURES_DIR` (defaults
to `~/.agent-teams-eval-fixtures/`), and may be:
- an existing local directory, used in place;
- a bare name (e.g. `webapp-medium`), resolved directly to
  `EVAL_FIXTURES_DIR/<name>`;
- a git URL, cloned once into `EVAL_FIXTURES_DIR/clones/<name>` and reused
  on every later run.

Fixture repos themselves live **outside** this repo — `fixtureRepo` must
never point inside `agent-teams`.

`webapp-bugfix-1`'s fixture baseline originally carried an undetected
frontend refetch-loop defect (grft.17) that made it a two-defect task,
contaminating seeded-bug isolation. That's fixed: `fixtureRef` now points to
`v1-bug-1` (the loop-fixed `v1-baseline` plus the same seeded project-filter
bug). No real runs had been pushed to Langfuse under this id yet, so it was
repointed in place rather than versioned.

## Langfuse push is optional

`eval collect` only pushes to Langfuse if `LANGFUSE_HOST`,
`LANGFUSE_PUBLIC_KEY`, and `LANGFUSE_SECRET_KEY` are all set. With none set,
collection still succeeds and `result.json` is written — **`result.json` is
the source of truth**, Langfuse is a convenience for cross-run comparison.
If credentials are present, a push failure is a hard error even though
`result.json` was already durably written by that point.

## What is safe to run

`go test ./...` against `internal/eval` (and `cmd/eval`) is free. The test
suite fakes every seam that touches something real — `runGitClone`,
`runDispatch` (the `ateam dispatch` shell-out), `runExtractMetrics`,
`runJudge` (the `claude -p` call), and `runPush` (the Langfuse HTTP client,
exercised against `httptest`) are all package-level vars swapped for fakes in
tests. No test in this repo clones a real fixture, dispatches a real DRI
session, calls a real LLM, or hits a real Langfuse instance.

**Only `eval run` and `eval collect` spend money.** `eval push` and
`eval clean` operate on already-persisted local data / local git state and
don't dispatch anything new, but still assume a run that itself cost money
already happened.

## CLI reference

```
scripts/eval run --task <path> --config <name>             dispatch a DRI run under a frozen preset, print its RunID
scripts/eval run --task <path> --model <m> [--advisor <a>]  dispatch a DRI run under an ad hoc model/advisor pair
scripts/eval collect <RunID>                                assemble metrics+judge, push to Langfuse if configured
scripts/eval push <RunID>                                   push a previously collected result to Langfuse
scripts/eval clean <RunID>                                  remove the run's leftover fixture worktree/branch
```

`eval run` takes exactly one of two mutually exclusive forms for selecting a
config — using both is an error:

- **`--config <name>`** — one of the frozen v1 registry in
  `cmd/eval/main.go`: `opus-noadvisor`, `sonnet-advisor`.
- **`--model <m> [--advisor <a>]`** — sets the DRI model and advisor axes
  independently, for any pair not in the frozen registry. `--model` alone
  turns the advisor off; `--advisor` without `--model` is an error (explicit
  beats a hidden default). `<m>`/`<a>` are passed through to `ateam dispatch`
  unvalidated — dispatch owns rejecting an unknown model. The resulting
  `ConfigFingerprint.Name` is derived as `<model>-noadvisor` or
  `<model>-advisor:<advisor>`.
