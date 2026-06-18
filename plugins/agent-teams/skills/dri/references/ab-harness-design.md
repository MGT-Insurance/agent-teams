# A/B Effectiveness Harness — Design

Measures whether the **concentric** methodology beats **waterfall** on three axes
(speed, cost, correctness) by running both on the same prompt concurrently and
comparing the results.

The harness BUILD is bead **agent-teams-7r5**, deferred to a sibling initiative
(to be `/dri-dispatch`ed) after **at-vlh** research resolves the version-coexistence
mechanism. This doc is its design input.

---

## Goal

Run old (waterfall) and new (concentric) `/dri` sessions against the same real
prompt. Collect speed, cost, and correctness data. Answer: does concentric beat
waterfall, and by how much?

---

## Hard Requirement — Parallel Execution

Old and new MUST run **concurrently**. Sequential runs are rejected: wall-clock
comparison is meaningless when runs share the same time window, and sequential
runs inflate elapsed time for the second run due to system contention and context
effects.

A single version-pinned plugin install is **sequential-only** and is REJECTED.
Both background `/dri` sessions would read from the one installed plugin copy,
so both sessions run the same methodology — no comparison is possible. Even if
scheduling were staggered, the sequential nature invalidates the speed axis.

---

## Two Candidate Run Mechanisms

Decision deferred until **at-vlh** research returns. Both mechanisms achieve
true concurrent execution; they differ in plugin identity approach.

### (a) Per-checkout local plugin source — PREFERRED IF at-vlh confirms

Each A/B `/dri` session loads the `agent-teams` plugin from its **own git
worktree**:

- **Waterfall session:** plugin loaded from a worktree pinned to the last
  waterfall baseline tag (e.g. `v0.waterfall-baseline`).
- **Concentric session:** plugin loaded from a worktree at `main` (concentric
  methodology).

The worktree IS the version. No duplicate plugin name, no separate marketplace
entry. Each session's `cwd` points to its own worktree; Claude Code picks up
the local plugin from there.

**Open capability question (at-vlh dependency):** does Claude Code support
per-session or project-scoped local plugin loading — i.e. can two concurrent
`/dri` sessions each have a DIFFERENT local plugin source? This is what at-vlh
is researching. If yes, mechanism (a) is the clean path. If no, fall through
to (b).

Plugin version machinery reference:
- `plugins/agent-teams/.claude-plugin/plugin.json` — version field; `claude
  plugin update` keys off this.
- `plugins/agent-teams/CLAUDE.md` — documents the two-bump rule
  (plugin.json + marketplace.json must stay identical on every change).

### (b) Two named installs side-by-side — FALLBACK, works today

Install two distinct plugin packages:

- `agent-teams-waterfall` — packages the baseline tag's plugin content.
- `agent-teams-concentric` — packages the main branch's plugin content.

Both are installed concurrently in the test environment. Each A/B `/dri` session
is launched with its plugin name explicitly, routing it to the right methodology.

Works with current Claude Code capabilities. Costs:
- Two marketplace entries with distinct plugin names.
- Release management friction: both packages need version bumps and publishes
  when the methodology diverges.

**Recommendation:** use (a) if at-vlh confirms the capability; otherwise (b).

---

## Three Measurement Axes

### Speed — wall-clock duration

`ateam cost` does **not** capture elapsed time. However, the JSONL transcripts
it already walks (`internal/cost/attribute.go: parseJSONL`) carry per-record
timestamps. A cheap wall-clock extractor reuses the same walk:

- Parse the `timestamp` field (or equivalent top-level time field) from each
  JSONL record in `~/.claude/projects/<slug>/<session>.jsonl`.
- Track `min(timestamp)` (session start) and `max(timestamp)` (session end)
  across all transcripts for the initiative (same session discovery as
  `discoverSessions` in `internal/cost/attribute.go`).
- `elapsed = max - min`.

This is small additive work on top of `internal/cost/attribute.go`. No new
infrastructure — extend `recordJSON` with a timestamp field, pass min/max
accumulators through `parseJSONL`, expose via a new `SpeedReport` type alongside
`Report`.

New verb: `ateam speed <initiative-id> [--json]` mirrors the `cost` verb
(`internal/verbs/cost.go`). Alternatively, add `--include-speed` to the
existing `cost` verb so both are printed in one pass.

### Cost — tokens and estimated $

Already delivered by `ateam cost <initiative-id> [--json]`.

Implementation:
- `internal/cost/attribute.go` — session discovery + JSONL walk.
- `internal/cost/pricing.go` — hardcoded per-model rate table ($/M tokens).
- `internal/verbs/cost.go` — CLI verb, tabwriter output, `--json` flag.

Reuse as-is. No changes needed for the cost axis.

### Correctness — low-fidelity human rubric

**Deliberately cheap.** No automated eval framework — deferred indefinitely.
"Do whatever is easiest" is the stated requirement.

Process:
1. Define a per-feature rubric before each A/B run (3–5 yes/no questions).
   Example: "Did the DRI produce a PR?", "Did CI pass?", "Were acceptance
   criteria met?", "Did the DRI correctly use peer comms / concentric loop?".
2. Eric reviews both `/dri` runs against the rubric and scores manually.
3. Record scores in a markdown table alongside speed/cost numbers.

Objective floor (future refinement, not required now): PR gates green + all
bead acceptance criteria closed = minimum passing bar. This is a binary signal
already available from CI and `bd` status — could be auto-checked without a
full eval framework.

Do not over-build the correctness axis. Speed and cost are the primary
quantitative signals; correctness is qualitative signal to confirm the faster/
cheaper run didn't cut corners.

---

## Compare / Bench Verb

A `compare` or `bench` sub-command prints a side-by-side table:

```
AXIS         WATERFALL          CONCENTRIC         DELTA
Speed        4h 12m             2h 48m             -33%
Cost         $3.42              $1.87              -45%
Correctness  4/5 (human)        5/5 (human)        +1
```

Implementation is a thin formatter over `ateam cost` + `ateam speed` output —
no new data collection. Takes two initiative IDs as arguments:

```
ateam bench <waterfall-id> <concentric-id> [--json]
```

---

## Build Deferral

The harness build — speed extractor, `bench` verb, correctness capture flow,
and the two-version runner launch script — is **bead agent-teams-7r5**, gated
on **R2** (after at-vlh research closes).

Sequence:
1. at-vlh research completes → capability confirmed or rejected.
2. Mechanism (a) or (b) selected.
3. agent-teams-7r5 `/dri-dispatch`ed as a sibling initiative.
4. Build implements: `internal/cost/attribute.go` timestamp extension,
   `ateam speed` verb, `ateam bench` formatter, runner docs or script.

This doc is the design input for that build.

---

## Key File References

| Path | Role |
|------|------|
| `internal/cost/attribute.go` | Session discovery + JSONL walk; extend for timestamps |
| `internal/cost/pricing.go` | Per-model rate table |
| `internal/verbs/cost.go` | `ateam cost` verb implementation; model for new verbs |
| `plugins/agent-teams/.claude-plugin/plugin.json` | Plugin version (mechanism a/b) |
| `plugins/agent-teams/CLAUDE.md` | Two-bump rule for plugin version changes |
| `plugins/agent-teams/skills/dri/references/execution.md` | Worktree setup; `/dri` launch path |
| `~/.claude/jobs/*/state.json` | Per-initiative session discovery (intent = `/dri <id>`) |
| `~/.claude/projects/<slug>/<session>.jsonl` | JSONL transcripts; token + timestamp source |
