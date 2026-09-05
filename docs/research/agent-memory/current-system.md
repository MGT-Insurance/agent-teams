# Agent memory current system and internal candidates

## Scope and evidence baseline

This artifact covers only the agent-teams implementation and the two internal candidates required by `agent-teams-ird0.2`. It does not research external products, select a winner, or propose an implementation plan.

- **Pinned repository:** `3bb52fad8c23ed5f9149946977f2c997a2cd4310` (2026-08-26). The memory implementation is byte-identical to source baseline `e028082e9f8de49342dc6d5b9106af2b7c4b44da`; the only intervening repository file is the approved research plan.
- **Observed on:** 2026-08-26, macOS arm64, against the live store in `/Users/ericlloyd/.agent-teams`. All live commands in this artifact were read-only. No live memory was learned, forgotten, applied, drained, condensed, synchronized, or locked.
- **Evidence grades:** A = reproduced behavior from the pinned source or shipped artifact, paired with source/tests; B = pinned repository source or test evidence not reproduced live; C = maintainer discussion/roadmap; D = inference or unbuilt design. A projected result for the incremental candidate is not treated as observed evidence.
- **Status terms:** pass, partial, fail, and unknown use the approved R1-R7 gate. A partial result blocks a standalone recommendation unless a later hybrid supplies the missing behavior.

## Shipped artifact identity

| Artifact | Recorded evidence |
| --- | --- |
| Repository and local-main source | Worktree `HEAD` is `3bb52fa`; local main is `e028082`. No memory-system source changed between them. |
| Plugin manifests | Repository, installed Claude plugin, and installed Codex plugin all report version `0.60.0`. |
| PATH command | `which ateam` is `~/.local/bin/ateam`, a symlink to `/Users/ericlloyd/Code/agent-teams/plugins/agent-teams/bin/ateam`. The wrapper selects `ateam-<os>-<arch>`; see `plugins/agent-teams/bin/ateam`. |
| Claude native executable | SHA-256 `6d1c4060666383ce6552bee90c965793e4f3a68cba9dc04eb41b662e9b7e505d`; identical in the pinned worktree, local main, and installed Claude `0.60.0` cache. |
| Codex native executable | SHA-256 `e4a96db6ecff6fd332f86ecdaf17bfcca8bf0a60e32a349dc742d86593242803`; different from the Claude/repository native executable despite the same version. Its inspected memory commands produced the same results. Both binaries identify the same Go module and dependencies, but neither carries a VCS revision in `go version -m`, so byte provenance of the Codex binary remains unproven. |
| Installed Codex agents | The four installed files in `~/.codex/agents/` are SHA-identical to `internal/verbs/codex_agents/*.toml`; `internal/verbs/setup.go` embeds and installs them with drift detection. |

`ateam` has no `--version` flag. Plugin manifests, hashes, `go version -m`, command help, and observed behavior are therefore the reproducible artifact identity.

## Architecture and data flow

```text
role or DRI
  |
  | learn <role> <slug> --file        applied <role> <bare-slug>
  v                                     |
ateam CLI ------------------------------+
  |  workspace.Home() -> AGENT_TEAMS_HOME or ~/.agent-teams
  |  bd.Client -> bd -C <home> ...
  v
global Beads memory KV in Dolt
  |  <role>:hot:<slug>     curated, served automatically
  |  <role>:fresh:<slug>   new/default writes, served automatically
  |  <role>:<slug>         cold, searched on demand
  |  applied:<role>:<slug> JSON count + timestamp
  |
  +-- learnings <role> ------> sorted hot block + sorted fresh block
  |                            (all role keys only when both are empty)
  +-- recall <role> <query> -> tokenized substring rank over key + body
  +-- memories-json ---------> tier/applied normalized dashboard contract
  +-- condense <role> -------> read-only packet; full hot/fresh, cold summaries
  +-- condense-check --------> read-only fresh threshold and size measurements
  |
  +-- condense skill: advisory lock -> read packet -> model decisions
  |                   -> learn/forget batch -> fresh-drain -> verify -> sync
  |
  +-- ateam pull/sync -------> Dolt remote through refs/dolt/data
                               (commit -> pull -> push; one non-FF retry)

Claude Code adapters                         Codex adapters
  role/DRI definitions self-fetch              DRI skill self-fetches
  SessionStart pulls store                      generated agents self-fetch
  SubagentStart pulls, stdout discarded         lifecycle hook handles mail/tie
  clear/compact reload for DRI/steward           no memory-specific hook reload
```

### Component map

| Responsibility | Concrete source owners |
| --- | --- |
| Workspace and storage adapter | `internal/workspace/workspace.go` (`Home`, `Initialized`); `internal/bd/bd.go` (`Client.Run`, `RunJSON`); `cmd/ateam/main.go` binds every verb to the global workspace. |
| Key model and write caps | `internal/verbs/write.go` (`learnKey`, `learnCap`, `learnCapError`); `internal/verbs/kong_converted.go` (`learnKong.Run`, `forgetKong.Run`). |
| Hot/fresh delivery and cold retrieval | `internal/verbs/query.go` (`runLearnings`, `runRecall`, `recallTokenize`, `recallMatchedTokens`). |
| Tier-independent use signal | `internal/verbs/kong_converted.go` (`appliedKey`, `appliedKong.Run`, `lookupApplied`). |
| Curation packet and drain | `internal/verbs/kong_converted.go` (`condenseKong.Run`, `condenseInstructionContract`, `freshDrainKong.Run`). |
| Trigger and serialization | `internal/verbs/condense_check.go` (`condenseCheckKong.Run`, `condenseCheckForRole`); `internal/verbs/lock.go` (`condenseLockCmd`). |
| Curation policy | `plugins/agent-teams/skills/condense/SKILL.md` and its `references/` files. |
| Claude delivery | `plugins/agent-teams/roles/*.md`, `plugins/agent-teams/skills/dri/SKILL.md`, `plugins/agent-teams/hooks/hooks.json`, and memory-related scripts in `plugins/agent-teams/hooks/scripts/`. |
| Codex delivery | `plugins/agent-teams-codex/skills/dri/SKILL.md`, `plugins/agent-teams-codex/hooks/hooks.json`, `internal/verbs/codex_agents/*.toml`, `internal/verbs/setup.go`, and `internal/verbs/codex_hook.go`. |
| Dashboard telemetry/read model | `internal/verbs/query.go` (`runMemoriesJSON`); `dashboard/server/src/cli.ts`; `dashboard/server/src/memories.ts`; `dashboard/shared/types.ts`; `dashboard/web/src/views/memories/index.tsx`. |

## Lifecycle flows

### Storage and synchronization

The global workspace resolves from `AGENT_TEAMS_HOME`, falling back to `~/.agent-teams`. Every memory operation shells `bd -C <workspace>`, so the CLI is a neutral process boundary rather than a harness SDK. The observed workspace uses Beads' embedded Dolt mode and has a configured Git remote. Cross-machine data travels through the Dolt remote ref, not the code branch or `.beads/issues.jsonl`.

`ateam pull` runs `bd dolt pull`. `ateam sync` first commits the Dolt working set, pulls, then pushes; a non-fast-forward push gets one pull/push retry. Claude session and subagent hooks pull before reads, and DRI wind-down calls sync. This track observed embedded mode but did not reproduce simultaneous local or cross-machine writes or a same-key conflict. There is no memory-level optimistic version, merge callback, or conflict record.

### Write, delete, and tier transitions

`ateam learn` is an upsert by computed key. A bare slug writes fresh; `hot:` stays hot; `cold:` is stripped and stored at the bare cold key. Pinned `learnCap` enforces 900-byte hot/fresh entries and 1,500-byte cold entries after trimming trailing newlines. `ateam forget` directly forms `<role>:<argument>`, so cold rewrite and cold delete deliberately use asymmetric arguments (`cold:<slug>` for learn, bare `<slug>` for forget).

`fresh-drain` enumerates fresh keys in sorted order, writes each body to its cold key, then forgets the fresh key. It is deterministic but is a sequence of two writes per entry, not a transaction; an error can leave a partial drain. Deletion is described as recoverable from Dolt history, but no dedicated `ateam memory restore` command or documented tested restore exercise exists.

### Read, injection, and retrieval

`learnings` fetches the complete memory map, filters to one role, and emits sorted hot entries before sorted fresh entries. If both tiers are empty it serves every role key for pre-tier compatibility. Matching header/trailer records count and byte length so truncation can be noticed. The observed implementer result had matching framing with 36 entries and 31,260 payload characters.

`recall` searches every tier for one role. It lowercases whitespace-separated query tokens and scores a row by how many tokens appear as substrings in the key or body. Any one token makes a match; ties sort by key. Zero-match "nearest" rows are merely the first five alphabetical keys because every score is zero. Exact-key recall was reproduced with one result. There is no stemming, stop-word handling, phrase semantics, relevance threshold, top-k cap on successful matches, score in output, or retrieval evaluation corpus.

### Tiering and curation

The `condense` verb itself is a pure read. Its packet carries full bodies for hot/fresh entries, one-line summaries for cold entries, tier-independent applied fields, an instruction contract, and the `hot_budget_tokens` target. `condense-check` is also read-only and fires only when the fresh tier exceeds the source-owned threshold; total hot+fresh size is reported but never triggers.

The skill acquires a machine-local advisory lock, reads the packet before draining, asks the model to promote/merge/demote/rewrite/evict, writes the chosen hot set, cleans cold entries, drains all fresh entries, remeasures, and optionally syncs before releasing. The order preserves full fresh bodies and makes a pre-curation crash mutation-free. Semantic duplicate/conflict decisions are prompt-driven. There is no staged diff, transaction, hard hot-budget write gate, provenance field, conflict object, or human review gate. Recoverability relies on Dolt history plus a free-text summary line.

The live read-only check reported every role below the fresh trigger, but five of seven curation roles had `hot_approx_tokens` above the packet's target. This is direct evidence that the hot target is a procedural postcondition, not an enforced storage invariant.

### Telemetry and dashboard

`ateam applied` performs a non-atomic read-modify-write of JSON at `applied:<role>:<bare-slug>`, incrementing a count and storing UTC RFC3339 time. Role prompts ask the agent to call it after acting on a memory. It has no session, harness, task, exposure, query, outcome, or success fields. Concurrent calls can lose increments; malformed records reset to zero; slug merges/renames reset the signal; deleting a memory does not delete its applied sibling.

`memories-json` joins that sibling signal onto every role memory and emits a stable camelCase array. The dashboard shells that command every 20 seconds, normalizes fields, groups by role, filters by tier/text, sorts by applied count, displays full bodies and last-applied time, and can show the raw injected subset from `learnings` (or capped `prime` output for `user`). It is visibility, not event capture or curation control.

## Current R1-R7 behavior

| Requirement | Result | Grade | Evidence and limit |
| --- | --- | --- | --- |
| **R1 role scope** | **Pass** | **A** | Key prefix filters isolate reads; `TestLearnings_FullBodyNoCrossRoleBleed`, `TestRecall_RolePrefixIsolation`, dashboard role-scoping tests, and the observed role-specific reads agree. All instances using the same global workspace and role share the namespace. |
| **R2 shared contribution** | **Partial** | **B** | All instances write the shared Dolt workspace; pull/sync and retry behavior are source-tested (`TestSync_*`). Same-key writes are last-write upserts, and no reproduced cross-machine concurrent-write scenario proves convergence without silent overwrite or describes semantic conflict resolution. |
| **R3 automatic availability** | **Partial** | **A** | This Codex implementer run executed the generated startup self-fetch and got complete framing. Claude/Codex role definitions and DRI skills require startup reads. Claude pulls before subagent reads and reloads DRI/steward on clear/compact. Delivery still depends on the agent obeying a tool instruction; Claude SubagentStart stdout cannot inject; Codex has no memory-specific lifecycle reload; most Claude role subagents have no clear/compact recovery path. |
| **R4 bounded curation** | **Partial** | **A** | Entry caps, fresh trigger, target budget, lock, drain ordering, conservative prompt rules, and Dolt history exist and have tests. The live hot-target overages, autonomous direct writes, non-transactional drain, no structured provenance/conflicts, no staged review, and untested restore path prevent a pass. |
| **R5 hot plus searchable pool** | **Partial** | **A** | Hot+fresh delivery and cold search were reproduced; exact-key cold retrieval is deterministic. The hot budget is not enforced, fresh can make injected context large, and substring ranking has known false positives and no quality evaluation, so the required token-bounded/usefully-ranked behavior is incomplete. |
| **R6 harness portability** | **Pass** | **A** | Claude, Codex, and a generic shell use the same `ateam` CLI and schema. Explicit Claude and Codex `0.60.0` binaries returned identical read summaries. Runtime delivery adapters remain separately maintained, and Codex native-byte provenance is unresolved, but the functional boundary is neutral. |
| **R7 applied/usefulness telemetry** | **Partial** | **A** | Applied count/time is joined into condense packets and the dashboard; 285 live rows had positive counts. The signal is voluntary, non-atomic, context-free, outcome-free, and reset/retention behavior is weak. It cannot distinguish exposure, application, or usefulness. |

## Claude Code and Codex differences

| Surface | Claude Code | Codex | Consequence |
| --- | --- | --- | --- |
| DRI startup | Claude DRI skill explicitly runs `learnings dri` and `instructions dri`, then marks the session role. | Codex DRI skill explicitly runs the same reads during preflight. | Equivalent self-fetch contract, different skill implementation. |
| Role subagent startup | Five role markdown definitions (including investigator) self-fetch learnings and instructions. | Four generated TOML agents (planner, implementer, tester, reviewer) self-fetch; no generated investigator. | Codex has a smaller custom-agent roster. |
| Store freshness | SessionStart pulls; SubagentStart performs a throttled pull before the role self-fetch. Hook stdout is deliberately discarded. | Codex lifecycle hook does not pull memory; DRI wind-down syncs, and reads use the locally available store. | Claude has explicit pre-read freshness; Codex can read stale local data until another sync path runs. |
| Compaction/clear | `role-recall-recovery.sh` reloads DRI/steward learnings on clear/compact; `compact-recovery.sh` separately restores initiative context. | SessionStart matches compact/clear but `codex-hook` only ties the thread and surfaces mail. | No Codex memory-specific reload, and no general Claude role-subagent reload. |
| User preferences | Claude SessionStart runs `ateam prime`; dashboard mirrors its cap/truncation. | Codex hook has no equivalent user-memory injection in source inspected here. | Cross-project user preference delivery is not runtime-parity evidence. |
| Artifact installation | Claude plugin ships role definitions, hooks, skill, wrapper, and native binaries. | Codex plugin ships skills/hooks/binaries; `ateam setup codex` separately installs generated agents under `~/.codex/agents`. | More installation and drift surfaces on Codex, partly covered by exact-file setup checks. |
| Hook payload | Claude memory hooks emit raw text into context where supported. | Codex hooks emit structured hook JSON with `additionalContextLimit`, but memory is absent from that payload. | A future shared delivery path must account for different hook contracts and limits. |

Known documentation drift at the pinned commit: `plugins/agent-teams/CLAUDE.md` says only reviewer loads machine-local instructions, while all five Claude role files and all four installed Codex agent files do so. The executable definitions are the stronger evidence.

## Maintenance inventory and replacement seams

| Owned layer | Current maintenance duty | Replacement seam |
| --- | --- | --- |
| Dolt/Beads workspace | Schema-by-key convention, local mode, remote setup, pull/push, conflict/recovery operations. | Keep `ateam` contract and replace the `bd.Client` storage adapter, or retain storage while replacing higher layers. |
| Go memory verbs | Key parsing, caps, read selection, lexical ranking, applied join, packet shape, trigger, drain. | A versioned internal memory package/CLI schema can isolate storage, retrieval, telemetry, or curation replacements. |
| Curation prompt/skill | Model selection rules, ordering, direct mutations, summary, verification. | Replace with a proposal/apply engine while retaining role semantics and CLI adapters. |
| Claude delivery | Role/DRI prompt clauses, five hooks/scripts, role marker/recovery behavior. | Retain a thin runtime adapter consuming one canonical context command. |
| Codex delivery | DRI skill, plugin hooks, embedded/generated agent TOMLs, setup drift detection. | Retain a thin runtime adapter consuming the same context command and generated contract fixtures. |
| Dashboard | CLI wrapper, duplicated TypeScript schema, normalization, polling, filters and display. | Keep UI and replace only its versioned memory API, or retire UI with an equivalent consumer. |
| Tests and fixtures | Go verb tests, hook shell tests, generated-definition tests, dashboard server/web tests, end-to-end role recovery probes. | Any replacement must preserve scenario fixtures at the CLI boundary, not only unit tests of its own store. |
| Release artifacts | Two plugin manifests, wrappers, four platform binaries per runtime, generated local definitions, cache/update paths. | Reproducible builds and embedded revision metadata can reduce provenance/debug burden even if runtime packaging remains. |

The highest-coupling seams are tier/key semantics, the `learnings` text envelope, condense packet contract, dashboard JSON contract, and runtime startup clauses. A candidate that replaces storage alone leaves almost all policy, injection, curation, telemetry, dashboard, and release work custom. A candidate that replaces curation/retrieval still needs the role model and both harness adapters unless it exposes the same neutral boundary.

## Candidate A: unchanged current system

### Profile

This candidate is exactly version `0.60.0` at `3bb52fa`, with no credit for hypothetical fixes.

| R1 | R2 | R3 | R4 | R5 | R6 | R7 |
| --- | --- | --- | --- | --- | --- | --- |
| Pass A | Partial B | Partial A | Partial A | Partial A | Pass A | Partial A |

- **Retained custom responsibilities:** every layer in the maintenance inventory: storage/sync operations, Go verbs and schemas, lexical retrieval, curation prompt/lock/drain, Claude and Codex delivery, dashboard, tests, binary builds, and release parity.
- **Maintenance estimate:** high and distributed. Eight listed layers remain project-owned; no major component or adapter is retired. Operational service cost is low because the store is local, but code/prompt/harness coordination cost remains.
- **Primary risks:** same-key/concurrent-write ambiguity; procedural hot budget; weak lexical relevance; autonomous destructive curation without structured audit; partial-drain states; incomplete restore procedure; voluntary lost-update telemetry; runtime delivery asymmetry; binary provenance drift.
- **Benefits preserved:** local/offline operation, direct data ownership, no hosted dependency, stable generic CLI, role isolation, searchable long tail, and Dolt history.
- **Standalone gate:** blocked by partial R2, R3, R4, R5, and R7 under the approved contract.

## Candidate B: bounded incremental improvement

### Proposed boundary

This is a first-class candidate, not assumed follow-up work for Candidate A. It keeps Dolt/Beads and the `ateam` process boundary, and changes only existing memory-owned layers:

1. **Canonical versioned contract.** Move key/tier parsing, record identity, caps, packet/export schemas, and applied joins into one internal memory package. Add a versioned lossless `ateam memory-export` plus dry-run validation/import contract. Generate or contract-test Claude clauses, Codex TOMLs, and dashboard fixtures from that owner rather than maintaining literals independently.
2. **One context command with delivery receipts.** Add `ateam memory-context <role>` returning entries, store revision, measured size, target status, complete/truncated flag, and payload hash in text/JSON. Keep Claude subagent self-fetch because its hook stdout cannot inject. Make both DRI skills, generated agents, Claude recovery, and a Codex memory recovery adapter consume the same command; missing or truncated delivery must be visible.
3. **Bounded lexical retrieval.** Preserve exact-key lookup, but normalize punctuation, remove stop words, distinguish whole-token/phrase hits from substrings, return scores, and cap results. Add a checked-in distractor corpus and quality/latency assertions. This is an internal deterministic change, not an external platform dependency.
4. **Reviewable curation transaction.** Split condense into proposal and apply. The proposal records source revision, input identities, duplicate/conflict decisions, before/after tiers, and projected budget. Apply rejects stale revisions, enforces the hot target, writes an audit record, retains drain-after-curation ordering, and requires explicit confirmation for semantic conflict resolution or destructive eviction. Keep the advisory lock until a storage transaction replaces it.
5. **Append-only telemetry events.** Replace the counter RMW with unique events for delivery/exposure, recall, and explicit application. Include stable memory identity, role, session, harness, context/task reference, and optional outcome. Aggregate for condense/dashboard; preserve a compatibility count. Delivery and recall events can be automatic. Actual application remains explicit until a trustworthy runtime signal exists.
6. **Artifact provenance.** Embed plugin version and source revision in native binaries, expose `ateam version --json`, and verify Claude/Codex native hashes or reproducible-build metadata during setup.

### Projected R1-R7 profile

| Requirement | Projected result | Evidence status and required validation |
| --- | --- | --- |
| **R1** | **Pass** | Retains the observed role namespace and adds stable identity. Baseline A; delta D until role-isolation/export round trips pass. |
| **R2** | **Partial** | Append-only telemetry removes one lost-update path and stale-revision apply protects curation, but general concurrent memory upserts and Dolt sync conflicts remain. Baseline B; delta D. |
| **R3** | **Projected pass** | One receipt-bearing context command and memory-specific recovery adapters cover startup/clear/compact/subagent paths by design. Baseline A plus unbuilt D; requires real Claude/Codex lifecycle probes before the gate can pass. |
| **R4** | **Projected pass** | Enforced budget, proposal/apply audit, stale-revision rejection, structured conflicts, and reviewed destructive actions fill current gaps. Baseline A plus unbuilt D; requires duplicate/contradiction/crash/restore scenarios. |
| **R5** | **Projected pass** | Enforced hot target and evaluated bounded ranking fill the two current gaps. Baseline A plus unbuilt D; requires a retrieval corpus and delivery-size assertions. |
| **R6** | **Pass** | Retains and versions the neutral CLI while reducing adapter drift. Baseline A; delta D for version negotiation and generic-client export/import. |
| **R7** | **Partial** | Atomic append-only events, exposure, context, and outcome fields materially improve the signal, but actual use is still self-reported unless a harness supplies trustworthy automatic application evidence. Baseline A plus unbuilt D. |

- **Retained custom responsibilities:** Dolt/Beads operations and sync; role and tier policy; `ateam` CLI; lexical retrieval implementation; curation policy/model invocation; Claude/Codex adapters; dashboard; telemetry aggregation; tests; packaging and release.
- **Maintenance estimate:** medium-high after stabilization. It consolidates duplicated contracts and makes failures/audits observable, but retires no major layer and adds event, export, proposal/apply, and retrieval-evaluation code. Expected recurring reduction is modest and concentrated in parity/debug work; short-term implementation and migration maintenance increases.
- **Primary risks:** scope expansion across six coordinated changes; event-volume growth; privacy exposure from session/context/outcome metadata; stale-revision UX; generated artifacts drifting from hand-authored prose; stricter ranking regressing niche queries; review gates making curation stall; old clients bypassing new invariants.
- **Standalone gate:** still blocked by partial R2 and R7. The design must not inherit Candidate A's R1/R6 evidence for its changed paths without rerunning migration and lifecycle scenarios.

## Migration invariants

Any internal or external successor must preserve these before cutover:

1. Role remains part of identity and cannot leak across reads, search, curation, export, or telemetry.
2. A memory keeps one stable bare identity across hot, fresh, and cold transitions; telemetry follows that identity rather than a tier-specific key.
3. Fresh remains the default contribution tier; hot precedes fresh in automatic context; cold remains available on demand.
4. Context delivery is complete or visibly incomplete. A missing trailer, exceeded budget, stale revision, adapter failure, or truncation cannot look like an empty healthy result.
5. Existing bodies, role/tier metadata, timestamps, applied data, and Dolt history are exported before any destructive transform. Unknown or malformed records are quarantined, not silently coerced or dropped.
6. Dual-read prefers one authoritative result per identity and records mismatches. Dual-write has idempotency keys and an explicit rollback window; it does not union divergent stores silently.
7. Curation ordering preserves full fresh bodies until decisions are made. An interrupted run cannot silently demote unreviewed fresh material or leave an unidentifiable partial batch.
8. Semantic conflict resolution and destructive eviction leave a reviewable record. Rollback restores both content and metadata.
9. Claude, Codex, and a generic CLI fixture exercise the same versioned contract before cutover. Machine-local human instructions remain outside the replicated/curated memory store.
10. The old `ateam` read path remains available until export/import counts, per-role hashes, injected-context hashes, and representative recall results agree.

## Unknowns and limits requiring later validation

- Cross-machine concurrent `learn` to the same key, Dolt merge behavior, conflict visibility, and convergence were not reproduced.
- A complete, tested restoration procedure from Dolt history is not present in the inspected memory surface.
- The fraction of role launches that actually execute startup self-fetch, and the failure rate after resume/clear/compact, is not instrumented.
- Claude and Codex user-preference delivery parity is not established.
- Retrieval precision, recall, latency scaling, and token cost have no representative evaluation corpus.
- Applied counts cannot quantify under-reporting, concurrent loss, popularity bias, or causal usefulness.
- The current live snapshot is mutable. Counts and curation verdicts below are evidence for 2026-08-26 only, not constants.
- The installed Codex native executable differs byte-for-byte from the repository/Claude executable and lacks embedded VCS provenance, despite matching observed memory behavior.
- Embedded-store contention and remote/offline failure behavior were not load-tested because this track was restricted to read-only, targeted checks.
- The source/test claim that Dolt history makes every removal recoverable was not elevated to grade A because no isolated export-delete-restore exercise was run.

## Reproducible verification notes

Run from the pinned worktree. Commands under this heading are read-only unless explicitly marked otherwise; none of the write-path commands described elsewhere were run against the live store.

```bash
git rev-parse HEAD
git diff --name-only e028082..3bb52fa

which ateam
readlink "$(which ateam)"
jq -c '{name,version}' plugins/agent-teams/.claude-plugin/plugin.json
jq -c '{name,version}' plugins/agent-teams-codex/.codex-plugin/plugin.json
shasum -a 256 plugins/agent-teams/bin/ateam-darwin-arm64
go version -m plugins/agent-teams/bin/ateam-darwin-arm64

ateam ws
ateam roles
ateam learnings implementer
ateam recall implementer 'implementer:hot:worktree-path-discipline'
ateam memories-json | jq '{total:length, byRole:(group_by(.role)|map({key:.[0].role,value:length})|from_entries), byTier:(group_by(.tier)|map({key:.[0].tier,value:length})|from_entries)}'
ateam condense-check --json
ateam condense implementer | jq '{role, memories:(.memories|length), hot_budget_tokens}'
```

Observed snapshot summary:

| Check | Result |
| --- | --- |
| Roles from `ateam roles` | `dri`, `implementer`, `investigator`, `planner`, `reviewer`, `steward`, `tester`, `user` |
| `memories-json` | 1,842 role-memory rows: 1,611 cold, 148 hot, 83 fresh; 285 had positive applied counts and non-null last-applied values. Applied sibling records themselves are excluded from the row count. |
| Implementer context | 36 entries, 31,260 payload characters, 22 hot and 14 fresh; header/trailer matched. |
| Exact-key recall | One match for `implementer:hot:worktree-path-discipline`. |
| `condense-check` | Seven curation roles, all `SKIP` on fresh threshold. Reported hot values exceeded the packet target for implementer, investigator, reviewer, steward, and tester. |
| Implementer packet | 385 entries: 22 hot, 14 fresh, 349 cold; 36 full bodies, 349 summaries, 50 positive applied joins. |
| Explicit installed Codex CLI | Same workspace, context framing, total/tier counts, applied-positive count, and implementer condense verdict as the PATH/Claude CLI. |

Targeted repository evidence to rerun:

```bash
go test ./internal/verbs -count=1 -run 'Test(Learnings_|Recall_|Roles_|MemoriesJSON|Learn_|Forget_|Applied_|Condense_|CondenseCheck_|CondenseLock_|SetupCodex|CodexHook)'
bash tests/role-recall-recovery.test.sh
bash tests/hook-subagent-prime-learnings.test.sh
```

Dashboard contracts are covered by `dashboard/server/src/memories.test.ts`, `dashboard/server/src/cli.test.ts`, and `dashboard/web/src/views/memories/memories.test.tsx`. The core source tests include role isolation, tier ordering/fallback, exact retrieval semantics, caps, pure-read condense/check behavior, applied joins, lock behavior, generated Codex definition drift, and hook output contracts.
