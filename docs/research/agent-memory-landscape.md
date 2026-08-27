# Agent Memory Landscape Recommendation

**Initiative:** `at-jo7h`

**Research date:** 2026-08-26

**Repository baseline:** `e028082e9f8de49342dc6d5b9106af2b7c4b44da`

**Plan:** [Agent memory research plan](../2026-08-26-agent-memory-research-plan.html)

**Independent review:** [Passed after correction](agent-memory/evidence-review.md)

## Executive Recommendation

Keep the current Dolt/Beads memory system as the production authority and rollback path.

Do not replace it with a platform or a new service stack now. No evaluated architecture clears all seven required capability gates with sufficient evidence.

Run a bounded incremental validation program inside the current architecture. The program must address four measured weaknesses first:

1. Retrieval returns broad false positives and has no reliable abstention behavior.
2. Hot-memory budgets are not enforced in current role sets.
3. Applied counters cannot prove exposure, application, outcome, or causal usefulness.
4. Claude Code and Codex use different lifecycle paths, with weaker recovery evidence in Codex.

The incremental program must preserve Dolt/Beads as the source of truth. New indexes, events, and curation proposals must be rebuildable or reversible.

Use two external architectures as research comparators during the pilot:

- **Mem0 Platform** is the strongest screened managed option.
- **PostgreSQL/pgvector with LangGraph, LangMem, and OpenTelemetry** is the strongest screened component hybrid.

Neither comparator is an adoption recommendation. Both retain substantial agent-teams-specific adapters and have unresolved required-capability evidence.

## Decision

The decision is to validate bounded incremental improvement, not to approve a replacement architecture.

This decision has three boundaries:

- The current system remains authoritative during validation.
- No destructive curation or data migration occurs during the research follow-up.
- Architecture selection resumes only after a candidate clears R1-R7 and has measurable maintenance inputs.

The approved scoring weights remain dormant. The research did not calculate a winner because every candidate failed the gate-before-score rule.

## Required Capabilities

The research evaluated each option against the same contract:

| ID | Required behavior |
| --- | --- |
| **R1** | Role-scoped memory shared by all instances of that role. |
| **R2** | Contributions across sessions and machines with defined concurrency and synchronization. |
| **R3** | Automatic availability at startup, resume, compaction, clear recovery, and subagent creation. |
| **R4** | Bounded curation with duplicate handling, conflict handling, provenance, and recovery. |
| **R5** | A token-budgeted hot set and a larger searchable pool. |
| **R6** | One stable CLI, API, or protocol contract works through Claude Code, Codex, and a generic client or fixture. |
| **R7** | Evidence that a memory was exposed, applied, and associated with an independent outcome. |

A partial, failed, or unknown required capability blocks adoption. Weighted strengths cannot cancel a required-capability gap.

## Current System

The current implementation stores role memories in the global Dolt-backed Beads workspace. Dolt remote references synchronize that workspace across machines.

The system uses three memory tiers:

- `fresh` contains new, uncurated memories.
- `hot` contains curated memories that load automatically.
- `cold` contains searchable memories that do not load automatically.

The `ateam` CLI owns writes, reads, recall, applied counters, and curation checks. Claude Code and Codex add runtime-specific hooks, skills, and role definitions.

The current system has two strong properties:

- Role isolation is concrete and directly reproduced.
- The CLI provides a neutral cross-harness access boundary.

The [current-system baseline](agent-memory/current-system.md) found partial behavior for R2-R5 and R7. Important observed results include:

- The live snapshot contained 1,842 memories during the research run.
- All seven role sets exceeded the procedural hot-memory budget during validation.
- A representative lexical recall query matched all 64 tester memories.
- The mutable applied counter can lose concurrent increments.
- The current store lacks a complete, tested logical export and clean-target restore path.
- Claude Code has stronger pull and recovery hooks than the current Codex path.

These weaknesses are reasons to improve the system. They are not evidence that an external replacement is safer.

## Landscape Findings

### Turnkey Platforms

The [turnkey-platform study](agent-memory/turnkey-platforms.md) screened Mem0, Zep/Graphiti, Letta, Supermemory, and Cognee.

No platform provides the full agent-teams R1-R7 lifecycle without custom adapters.

**Mem0 Platform** provides the broadest documented managed surface. It includes scoped memory, filtered search, reranking, history, export jobs, SDKs, CLI access, MCP access, feedback, and webhooks.

Mem0 still needs project-owned controls for role authorization, lifecycle delivery, token budgets, conflict authority, usefulness lineage, migration, rollback, and outage behavior. Credentialed role-isolation and complete export/re-import checks were not available.

Mem0 OSS `v2.0.19` is not equivalent to Mem0 Platform. Its public mock configuration and documented search call did not reproduce cleanly. The final evidence grade is B, not A.

**Zep/Graphiti** has strong temporal provenance and graph behavior. Its export, concurrency, hot-context, and complete lifecycle evidence did not satisfy the contract.

**Letta** owns more agent lifecycle behavior than the other platforms. That strength comes with full-runtime coupling and material differences between cloud and local memory behavior.

Supermemory and Cognee remained screened candidates. Their evidence did not justify deep adoption scoring.

### Framework Libraries

The [framework study](agent-memory/framework-libraries.md) evaluated LangGraph/LangMem, LlamaIndex, Semantic Kernel, AutoGen, and provider-native memory.

**LangGraph/LangMem** has the broadest library surface for CRUD, namespaces, search, and background reflection. Applications still own concurrency policy, provenance, curation authority, lifecycle delivery, and usefulness telemetry.

**LlamaIndex** has explicit context budgets and block orchestration. It lacks a uniform memory-entry lifecycle and applied-memory events.

**AutoGen** has a compact protocol and useful retrieval events. Its base contract lacks update, selective deletion, and bounded curation.

**Semantic Kernel** has broad connector support. Its APIs differ by language and maturity.

OpenAI Conversations and Claude Code auto memory provide session or harness continuity. They do not provide portable role memory for agent-teams.

### Composable Components

The [component study](agent-memory/composable-components.md) evaluated local search, vector stores, graph stores, shared databases, protocols, embeddings, and rerankers.

SQLite FTS5 with `sqlite-vec` had the smallest reproduced local footprint. It retains single-writer and vector-migration constraints.

LanceDB and Chroma reproduced filtered local search and export behavior. Their Python dependency footprints are materially larger.

PostgreSQL/pgvector and Qdrant provide stronger shared-service concurrency. They add backup, upgrade, monitoring, and consistency responsibilities.

A canonical CLI or API improves portability. MCP is useful as an adapter to that boundary. MCP does not guarantee lifecycle calls or generic-client conformance.

No component owns agent-teams role authority, lifecycle delivery, curation judgment, hot-context budgeting, or usefulness semantics.

### Curation And Usefulness

The [quality and observability study](agent-memory/quality-observability.md) found that usefulness needs separate immutable events:

- Exposure: the memory was available to the agent.
- Retrieval: the system selected the memory.
- Application: the agent used the memory in an action or decision.
- Outcome: an independent signal described what happened next.
- Feedback: a human or evaluator assessed the result.
- Curation: the system promoted, demoted, merged, or removed the memory.

One mutable applied counter cannot provide this evidence.

Exact duplicates and typed temporal conflicts can use deterministic rules. Semantic merges and ambiguous conflicts need model assistance plus human approval.

Promotion based only on use count creates popularity bias. Reliable evaluation needs exposure denominators, holdouts or randomized eligibility, immutable events, and structured application receipts.

OpenTelemetry provides a useful transport surface. Its current memory conventions do not define role, tier, revision, exposure, application, outcome, or retention semantics for agent-teams.

## Gate Results

| Architecture | R1 | R2 | R3 | R4 | R5 | R6 | R7 | Result |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Current unchanged | Pass | Partial | Partial | Partial | Partial | Pass | Partial | Blocked |
| Bounded incremental | Partial | Partial | Unknown | Unknown | Unknown | Partial | Partial | Blocked |
| Mem0 Platform | Partial | Partial | Partial | Partial | Partial | Pass for access | Partial | Blocked |
| PostgreSQL/LangGraph hybrid | Partial | Partial | Partial | Partial | Partial | Partial | Partial | Blocked |

The [validation report](agent-memory/validation-and-migration.md) applied ten common scenarios. None of the four options passed the full set.

The [decision matrix](agent-memory/decision-matrix.md) therefore contains no candidate scores, totals, intervals, rankings, or sensitivity winner.

## Recommended Incremental Target

The recommended target is a bounded improvement layer around the current authority.

### 1. Freeze A Neutral Record Contract

Define stable memory IDs, revisions, role scope, tier, provenance, source references, and timestamps.

Add complete logical export and clean-target import before changing storage or retrieval. Round-trip tests must prove fidelity.

The current Dolt records remain authoritative.

### 2. Add Receipt-Bearing Context Delivery

Make each startup, resume, compaction, clear recovery, and subagent load return a receipt. The receipt must identify the harness, role, memory revisions, token budget, and truncation state.

Claude Code and Codex can keep separate lifecycle adapters. Both adapters and one generic client fixture must satisfy the same conformance contract.

### 3. Replace Broad Lexical Recall In Shadow Mode

Build a rebuildable local index from authoritative records. Start with deterministic lexical retrieval and a strict token budget.

SQLite FTS5 is the lowest-footprint initial candidate. `sqlite-vec` can remain an optional experiment until it improves the fixed evaluation corpus.

The shadow index must never become a second writable authority.

### 4. Make Curation Reviewable And Reversible

Separate curation into proposal and apply stages.

Deterministic duplicate rules can apply automatically when provenance remains intact. Semantic merges, conflict resolution, and destructive eviction require reviewable changesets during the pilot.

Each changeset must name source revisions and support rollback.

### 5. Replace Applied Counters With Immutable Events

Record exposure, retrieval, application, outcome, feedback, and curation as separate events.

Use stable IDs and idempotency keys. Do not store raw content in telemetry by default.

Build derived counters from events. Do not update counters through read-modify-write operations.

### 6. Measure Maintenance Before Selecting A Replacement

Record current maintenance work by layer. Include development, debugging, release parity, operations, and incident recovery.

Measure the incremental pilot with the same workload categories. A component counts as an improvement only when it retires measured work.

## External Comparator Conditions

### Mem0 Platform

Do not promote Mem0 Platform beyond comparator status until a credentialed pilot proves:

- Role authorization and isolation under actual entity and filter semantics.
- Concurrent writes, idempotency, history, and conflict behavior.
- Complete export, clean-target import, deletion verification, and reverse replay.
- Token-bounded hot selection and Claude/Codex lifecycle delivery.
- Outage behavior and an encrypted local continuity policy.
- Privacy, retention, processor, pricing, and contractual acceptability.
- Fewer measured project-owned maintenance layers after all adapters are counted.

### PostgreSQL/LangGraph Hybrid

Do not promote the hybrid beyond comparator status until an integrated deployment proves:

- Role isolation through schema and database policy.
- Concurrent revision checks, outbox recovery, and idempotency.
- Backup, restore, logical export, and reverse import.
- Model-free lexical fallback and bounded vector retrieval.
- Proposal-only LangMem curation with human conflict authority.
- Complete event capture and independent outcomes.
- Lower measured maintenance after database, Python, model, and telemetry operations are included.

## Migration And Rollback

The incremental path uses staged, reversible changes:

1. Freeze the neutral schema and prove export/import round trips.
2. Add context receipts, shadow retrieval, and immutable events without changing authoritative reads.
3. Compare shadow results against the fixed evaluation corpus.
4. Run curation in proposal-only mode.
5. Activate one role at a time only after its V1-V8 scenarios pass.
6. Keep old reads and the neutral event journal through the rollback period.

Rollback disables new delivery and curation paths, returns reads to existing `ateam` verbs, and rebuilds derived indexes from Dolt.

Unmatched or malformed records go to quarantine. Rollback must never silently discard them.

## Maintenance And Operating Model

The unchanged system retains eight inventoried maintenance layers. The bounded incremental path adds six improved surfaces while consolidating some current behavior.

No quantitative maintenance reduction is supported yet. The independent review rejected unsupported percentages and numerical maintenance scores.

The pilot must measure:

- Time spent on memory contract changes.
- Time spent maintaining Claude and Codex parity.
- Retrieval defects and evaluation work.
- Curation review and recovery work.
- Synchronization and concurrency incidents.
- Release, binary, dashboard, and migration work.
- New index, event, model, database, or vendor operations.

Stop the pilot when added work exceeds retired work or when a required capability remains structurally unresolved.

## Cost, Licensing, And Privacy

The incremental path has no required vendor subscription. Its costs are engineering, event storage, evaluation, optional embeddings, model-assisted proposals, and human review.

Mem0 public pricing is a dated snapshot. The research observed free limits, a `$19/month` Starter tier, and a `$249/month` Pro tier. Enterprise, overage, retention, and contractual costs need a current workload quote.

The screened hybrid components use documented open-source licenses. Self-hosting still adds database, backup, monitoring, model, telemetry, migration, and on-call costs.

Telemetry must use IDs by default. Session content, memory text, model inputs, and outcomes require explicit retention, deletion, and access policies.

## Risks And Unknowns

The recommendation retains these unresolved risks:

- Same-memory convergence across machines.
- Reliable delivery during resume, compaction, clear recovery, and subagent creation.
- Complete export, restore, and reverse replay.
- Retrieval precision, latency, token cost, and abstention on representative data.
- Trustworthy application capture and independent outcome joins.
- Exposure bias and popularity feedback loops.
- Model-assisted curation quality and review workload.
- Mem0 hosted authorization, privacy, outage, export, deletion, and cost behavior.
- Integrated PostgreSQL/LangGraph reliability and operating burden.
- A measured maintenance baseline for every current layer.

Unknown remains unknown. It does not count as failure, absence, zero, or an optimistic midpoint.

## Rejected Decisions

### Keep The Current System Unchanged Indefinitely

Rejected as the improvement direction. It preserves known retrieval, budget, telemetry, lifecycle, and export weaknesses.

It remains the production authority and rollback path during validation.

### Adopt Mem0 Platform Now

Rejected for current adoption. The hosted service has the strongest managed feature surface, but required credentialed and migration evidence is missing.

### Replace Memory With Letta

Rejected for current adoption. Letta owns more lifecycle behavior, but replacing agent-teams runtime boundaries creates substantial coupling and migration scope.

### Build The Full PostgreSQL/LangGraph Hybrid Now

Rejected for current adoption. The stack is unintegrated and adds several operating systems before proving that it retires measured maintenance.

### Use MCP As The Memory Architecture

Rejected as a complete architecture. MCP is an interface adapter. It does not guarantee lifecycle invocation, curation policy, role authority, storage, or usefulness semantics.

## Reconsideration Triggers

Reopen architecture selection when one option satisfies all of these conditions:

- R1-R7 pass with grade A or B evidence.
- Export, clean-target import, and rollback are reproduced.
- Concurrent writes and role isolation are reproduced.
- Claude Code, Codex, and a generic client fixture pass the same access contract.
- Claude Code and Codex lifecycle delivery passes startup, resume, compaction, clear recovery, and subagent scenarios.
- Usefulness events include exposure and independent outcomes.
- Destructive curation remains reviewable and reversible.
- Maintenance inputs use a measured baseline and a stated workload.
- Privacy, cost, licensing, and operating ownership are accepted.

At that point, execute the frozen weighted matrix and sensitivity analysis. Do not execute it before gate clearance.

## Evidence Index

- [Current system and internal candidates](agent-memory/current-system.md)
- [Turnkey memory platforms](agent-memory/turnkey-platforms.md)
- [Framework and harness libraries](agent-memory/framework-libraries.md)
- [Composable components and protocols](agent-memory/composable-components.md)
- [Curation, evaluation, and observability](agent-memory/quality-observability.md)
- [Validation and migration](agent-memory/validation-and-migration.md)
- [Corrected decision matrix](agent-memory/decision-matrix.md)
- [Independent evidence review](agent-memory/evidence-review.md)

## Final Status

The research supports a specific next direction: retain the current authority and validate bounded incremental improvement.

The research does not support a platform purchase, full replacement, or new service stack today.
