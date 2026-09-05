# Agent memory architecture decision matrix

**Synthesis date:** 2026-08-26

**Status:** DRAFT corrected for independent evidence review findings ER-01 through ER-03

**Approved contract:** [agent memory research plan](../../2026-08-26-agent-memory-research-plan.html)

**Decision scope:** research synthesis only; no implementation, purchase, migration, or deployment decision is made here.

## Decision result

No architecture currently clears all R1-R7 gates with grade A or B evidence. The unchanged system has five partial gates, the bounded incremental architecture contains unbuilt D-grade paths, Mem0 Platform has unresolved required behavior outside its neutral access boundary, and the neutral hybrid is an unbuilt integration. Therefore, **no candidate is score-eligible, and no weighted or sensitivity winner exists**. Unknowns remain `U`; they do not become scores, zeroes, or numerical bounds. [N01] [N06] [N07]

The draft recommendation is a **validation sequence, not an architecture-selection winner**: retain the current Dolt-backed path as the production authority and rollback path while validating the bounded incremental contracts for neutral export, context receipts, retrieval, reviewable curation changesets, and append-only events. This sequence makes no finding that the incremental design passes the gate or reduces maintenance. Mem0 Platform and the PostgreSQL/pgvector plus LangGraph/LangMem and OTel composition remain the strongest screened external and hybrid **research comparators**, respectively, not adoption recommendations. [N02] [N03] [N04] [N05] [N07]

### Reviewer finding dispositions

- **ER-01 - Resolved:** removed all candidate raw scores, weighted totals, uncertainty intervals, and sensitivity calculations. The frozen weights and method remain only as a future template; no candidate is currently eligible to execute it.
- **ER-02 - Resolved:** removed the unsupported A2-A4 maintenance-reduction percentages and numerical maintenance judgments. Their net maintenance effect is unknown pending a measured baseline, workload, retired and retained work, added operations, formula, time horizon, and uncertainty basis. A1 retains only the definitional 0% statement for an unchanged boundary.
- **ER-03 - Resolved:** replaced the stale Mem0 entity-combination ambiguity with the current documented default-extraction, direct-import, and `AND` filter semantics, accessed 2026-08-26 and linked in N04. R1 remains `Partial, B` until a credentialed authorization and role-isolation probe passes.

## Normalized evidence ledger

These rows are the only claim sources used by this matrix. Evidence grades and `Pass`, `Partial`, `Fail`, and `Unknown` retain the meanings frozen in the approved plan. A design projection is grade D until built and reproduced. Newer observed behavior overrides conflicting prose. [N01]

| ID | Candidate, edition, and pin | Normalized evidence claim | Grade and boundary |
| --- | --- | --- | --- |
| **N01** | Approved plan, repository baseline `e028082e`; approved 2026-08-26 | Freezes R1-R7, A-D evidence grades, gate-before-score rule, exact weights, unknown handling, and sensitivity method. | Contract, not candidate evidence. |
| **N02** | agent-teams plugin `0.60.0`, source `3bb52fa`; live snapshot and executable checks on 2026-08-26 | Current system passes R1 and R6; R2, R3, R4, R5, and R7 are partial. It owns eight maintenance layers and has no lossless memory export/import. | A/B. Snapshot counts are date-specific. [Source](current-system.md) |
| **N03** | Bounded incremental design in `current-system.md` | Six changes are defined: canonical contract/export, receipt-bearing context, bounded lexical retrieval, reviewable curation, append-only events, and artifact provenance. No major layer is retired; R2 and R7 remain partial even if projected changes work. | Baseline A/B plus unbuilt D. No projected pass is credited. [Source](current-system.md#candidate-b-bounded-incremental-improvement) |
| **N04** | Mem0 Platform, hosted edition; documentation accessed 2026-08-26 | Platform supplies managed scoped memory, filtered/reranked search, history, export jobs, REST/SDK/CLI/MCP, feedback, and webhooks. Current documentation says default inferred memories carry `user_id` or `agent_id` according to the speaker, not both; direct import with `infer=False` can populate both; and sibling filter fields are implicitly combined with `AND` (equivalent to explicit `AND`). R6 passes only as an access boundary. Credentialed role authorization and isolation, contradictory-write semantics, lifecycle delivery, token budgeting, bounded conflict policy, usefulness lineage, full re-import, offline behavior, and contract/privacy review remain partial or unknown. | B only; no hosted credentialed authorization probe. OSS behavior is not transferred. [Entity scope](https://docs.mem0.ai/platform/features/entity-scoped-memory) [Filter semantics](https://docs.mem0.ai/platform/features/v2-memory-filters) [Source](turnkey-platforms.md#deep-evaluation-1-mem0) |
| **N05** | PostgreSQL 18; pgvector `0.8.6` at `8ee86c9`; LangGraph `1.2.11`; checkpoint `4.2.0`; LangMem `0.0.30`; OTel spec `1.60.0` and semconv `1.44.0` | PostgreSQL/pgvector is the strongest documented transactional shared-store primitive screened; LangGraph is the closest CRUD/search mapping; LangMem supplies a model-assisted curation mechanism; OTel can transport events. None supplies role authority, hot delivery, complete curation authority, or usefulness semantics. | A for isolated LangGraph/SQLite/OTel artifact probes where cited; B for PostgreSQL and published interfaces; integrated architecture D. [Component source](composable-components.md) [Framework source](framework-libraries.md) [Quality source](quality-observability.md) |
| **N06** | Validation join at repository `1c29b10`; corrected evidence at HEAD `f7b2511` | Common V1-V10 validation leaves all four options gate-blocked. Current retrieval failed the representative query, current telemetry and export failed their scenarios, Mem0 Platform credentialed scenarios were not run, and the integrated hybrid was not built. | A/B/D by scenario. [Source](validation-and-migration.md) |
| **N07** | Common migration-risk register, 2026-08-26 | Every changed architecture needs stable IDs, role predicates, immutable source evidence, idempotency/revisions, dual-read comparison, complete logical export plus physical backup, staged cutover, and reverse replay. | A/B for mechanisms; migration executions remain D. [Source](validation-and-migration.md#migration-risk-register) |
| **N08** | Quality and observability evidence, agent-teams `0.60.0`; OTel GenAI commit `56d6b11`; OpenInference `0.1.33`; Phoenix `20.4.0` | Exposure, retrieval, application, outcome, feedback, and curation are distinct events. Current mutable applied counts can lose increments. OTel memory fields remain Development and do not define the project-specific outcome or retention semantics. | A for source/schema inspection; B for integration surfaces; end-to-end capture D. [Source](quality-observability.md#minimal-neutral-event-contract) |
| **N09** | Corrected Mem0 OSS `v2.0.19` evidence at HEAD `f7b2511` | Public configuration rejects the documented mock embedder; top-level `agent_id` search is rejected; a patched diagnostic worked only after unsupported runtime substitutions and filter syntax correction. The prior count also measured response keys, not retrieved memories. | **B, not A.** Diagnostic only; no score or Platform gate inherits it. [Source](turnkey-platforms.md#mem0-oss-supported-path-failure-and-patched-diagnostic) |
| **N10** | Dated cost, license, and privacy sources in candidate ledgers | Mem0 Platform public plans and terms are dated snapshots. PostgreSQL/pgvector, LangGraph/LangMem, and the screened telemetry components have documented open-source licenses, but deployment, model, monitoring, storage, and engineering costs remain workload-dependent. | B; no procurement quote, workload bill, or legal approval. [Turnkey ledger](turnkey-platforms.md#primary-evidence-ledger) [Component ledger](composable-components.md#evidence-ledger) |

### Edition and version boundaries

- **Mem0 Platform and Mem0 OSS are separate candidates.** Platform comparisons use hosted documentation only. The corrected OSS diagnostic is grade B and is excluded from Platform evidence. [N04] [N09]
- **Zep Cloud, Graphiti OSS, Letta Cloud, and Letta local are separate editions or products.** Their behavior is not transferred into the selected buy or hybrid architecture. [N06]
- **LangGraph in-memory reproduction is not PostgreSQL evidence.** PostgreSQL concurrency, pgvector retrieval, integrated transactions, and restore remain unrun for this architecture. [N05] [N06]
- **OTel transport is not R7 semantics.** The `agent_teams.memory.*` event vocabulary, capture completeness, outcome join, privacy policy, and retention use remain project-owned. [N08]

## R1-R7 gate before scoring

The table records the current evidence state. Adapter names show how a changed architecture proposes to close a gap; an unbuilt adapter does not convert a gate to `Pass`.

| Architecture | R1 | R2 | R3 | R4 | R5 | R6 | R7 | Ranking eligibility |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| **A1 current unchanged** | Pass A | Partial B | Partial A | Partial A | Partial A | Pass A | Partial A | **Blocked**: five partial gates. [N02] [N06] |
| **A2 bounded incremental** | Partial A/D (`I1`) | Partial B/D (`I1`, `I5`) | Unknown A/D (`I2`) | Unknown A/D (`I4`) | Unknown A/D (`I3`, `I4`) | Partial A/D (`I1`, `I2`, `I6`) | Partial A/D (`I5`) | **Blocked**: changed paths are unbuilt; R2 and R7 remain incomplete by design. [N03] [N06] |
| **A3 Mem0 Platform-centered adoption** | Partial B: entity/filter semantics are documented, but credentialed role authorization and isolation are unproved (`B1`) | Partial B; V2 unknown (`B2`) | Partial B (`B3`) | Partial B (`B4`) | Partial B (`B3`) | Pass B for access only (`B5`) | Partial B (`B6`) | **Blocked**: required Platform behavior and all gap adapters remain unproved. [N04] [N06] |
| **A4 neutral component hybrid** | Partial A/B/D (`H1`) | Partial B/D (`H2`) | Partial A/D (`H3`) | Partial A/B/D (`H4`) | Partial A/B/D (`H5`) | Partial A/B/D (`H6`) | Partial A/B/D (`H7`) | **Blocked**: integrated architecture and adapters are unbuilt. [N05] [N06] |

### Gap adapters and maintenance count

**A2 adapters, all project-owned:** `I1` canonical versioned record/export contract and revision policy; `I2` receipt-bearing context command plus Claude/Codex lifecycle calls; `I3` bounded lexical ranker and fixed corpus; `I4` proposal/apply curation transaction and review queue; `I5` append-only exposure/application/outcome events plus aggregation; `I6` artifact provenance and contract fixtures. This adds six maintained surfaces while consolidating existing ones. [N03]

**A3 adapters, all project-owned:** `B1` deny-unscoped role/principal mapper; `B2` idempotent write gateway, version/reconciliation ledger, and conflict quarantine; `B3` deterministic token-budget selector, encrypted hot cache, delivery receipts, and Claude/Codex lifecycle adapters; `B4` immutable provenance plus reviewable promotion/demotion/conflict policy; `B5` versioned `ateam` compatibility facade over the exact Platform API; `B6` exposure/application/outcome event store and retention policy; `B7` export/import validator, deletion verifier, dual-run controller, dashboard normalization, and outage rollback. The vendor operates the service, but these seven adapter groups remain owned. [N04] [N06] [N07] [N08]

**A4 adapters, all project-owned:** `H1` role/principal derivation plus PostgreSQL schema/RLS; `H2` expected revisions, idempotency, transaction/outbox, retries, and conflict ledger; `H3` deterministic hot response, cache, receipts, and Claude/Codex lifecycle calls; `H4` immutable provenance, LangMem proposal, human authority gate, transactional changeset, and rollback; `H5` lexical/vector fusion, token cap, abstention, model/index manifest, and evaluation corpus; `H6` versioned CLI/HTTP/OpenAPI/MCP facade and conformance suite; `H7` OTel transport plus project event semantics, spool, independent outcome joins, and bias controls; `H8` logical export/import, physical backup, migration/replay, dashboard normalization, service monitoring, and restore drills. PostgreSQL, Python/LangGraph/LangMem, model, and telemetry operations are additional maintenance, not retired work. [N05] [N06] [N07] [N08]

### Qualitative pre-gate comparison

| Architecture | Evidence-supported direction | Decision-blocking boundary |
| --- | --- | --- |
| **A1 current unchanged** | Preserves the directly reproduced local-first authority, neutral CLI, existing operating model, and immediate rollback path. | Five required gates remain partial; all eight inventoried maintenance layers remain project-owned. [N02] [N06] |
| **A2 bounded incremental** | Targets observed export, delivery, retrieval, curation, telemetry, and artifact-provenance gaps while keeping current authority and migration scope bounded. | Every changed path is unbuilt, no major layer is demonstrated retired, and the net maintenance direction is unknown until a bounded pilot measures current and changed workloads. [N03] [N06] |
| **A3 Mem0 Platform comparator** | Offers the broadest documented managed storage, search/rerank, history, export-job, feedback, webhook, and neutral access surface among screened external options. | Required credentialed behavior is unproved, seven project-owned adapter groups remain, and net maintenance direction is unknown without baseline and pilot workload measurements. [N04] [N06] |
| **A4 neutral hybrid comparator** | Combines the strongest screened transactional store, CRUD/search mapping, curation mechanism, and telemetry transport primitives behind a neutral boundary. | The integration is unbuilt, eight project-owned adapter groups remain, new database/model/telemetry operations are introduced, and net maintenance direction is unknown. [N05] [N06] [N08] |

## Deferred weighted evaluation template

This section preserves the frozen method for a future evaluation; it publishes no current decision output. The frozen weights and scoring method become executable only after a candidate passes every R1-R7 gate with grade A or B evidence, or after an explicit hybrid has filled and proved every gap under the approved contract. All four current candidates are blocked, so there are **no raw scores, weighted totals, intervals, sensitivity calculations, rankings, or winners**. [N01] [N06]

### Frozen weights

The exact approved weights are:

| Code | Dimension | Weight |
| --- | --- | ---: |
| `M` | Expected maintenance reduction | 20 |
| `I` | Integration and retained custom code | 15 |
| `P` | Harness portability and coupling | 15 |
| `D` | Data ownership, export, migration, rollback | 12 |
| `S` | Security and privacy | 10 |
| `R` | Maturity and reliability | 10 |
| `O` | Operational burden | 8 |
| `C` | Cost | 6 |
| `L` | Licensing and project risk | 4 |

### Future execution rules

After gate clearance, assign each eligible architecture a raw value for every dimension using the frozen scale: `0` unacceptable or absent, `1` major gaps, `2` workable with material burden, `3` strong with bounded caveats, and `4` strong and directly evidenced. Every value must cite its evidence row. `U` means evidence is insufficient; any `U` blocks ranking and is never converted to zero, a midpoint, or an interval endpoint. [N01]

For an eligible architecture with no unknown dimensions, calculate each contribution as `weight * raw / 4` and sum the nine contributions for a total out of 100. Then recompute with equal weights and with each of the three largest weights varied by plus or minus 25 percent while normalizing the total. Report a winner only if eligibility remains intact and plausible weight changes do not change the result; otherwise report the decision as sensitive. These rules are dormant while every candidate remains gate-blocked. [N01] [N06]

## Architecture A1: current system unchanged

### Boundary and operating model

Keep agent-teams `0.60.0`: Dolt/Beads source of truth; fresh/hot/cold key convention; Go CLI verbs; lexical recall; prompt-driven condense with lock and drain; Claude and Codex delivery definitions; dashboard; tests; prebuilt binaries; and release parity. It stays local-first and uses Dolt remote synchronization across machines. [N02]

### Retained custom code and maintenance evidence

All eight inventoried layers remain custom. Recurring maintenance reduction is **0% by definition** because the architecture is unchanged; this does not estimate labor or operational effort. Service spend remains low, but engineering, prompt, harness, dashboard, synchronization, and release work remain unmeasured. [N02]

### Migration, rollback, and operation

There is no data migration. The operational improvement available without changing architecture is to document and rehearse Dolt restore, but that work is not credited to this unchanged candidate. Rollback from any later candidate is to keep this read path available, preserve source writes, and replay neutral post-cutover events only after a reverse import has been proved. [N06] [N07]

### Cost, license, privacy, and risk

No new vendor subscription or external memory processor is introduced. Sensitive operational memory remains in the project-controlled workspace. Engineering cost remains material. Principal risks are same-key convergence ambiguity, unreliable lifecycle recovery, overbroad lexical retrieval, unenforced hot budgets, non-transactional curation, mutable applied counters, incomplete export/restore, and artifact drift. [N02] [N06]

### Revisit triggers

Revisit immediately if V2 measures silent write loss, V3 measures missing context, V4 continues broad false-positive retrieval, V6 shows material event loss, or V8 cannot restore a complete fixture. Also revisit if maintenance time for the eight owned layers is measured and exceeds the bounded cost of a validated alternative. [N06]

## Architecture A2: bounded incremental improvement

### Boundary and operating model

Keep Dolt/Beads and the `ateam` CLI as source of truth and compatibility boundary. Add only adapters `I1-I6`: canonical records/export, context receipts, bounded lexical retrieval, reviewable curation changesets, append-only events, and artifact provenance. Claude and Codex remain thin but separate lifecycle callers. [N03]

### Retained custom code and maintenance evidence

All current major layers remain, and six improved surfaces must be maintained. Consolidated contracts and better diagnostics could reduce some duplicated upkeep, while implementation, migration, event storage, curation review, and evaluation add work. Net maintenance reduction is **unknown** because no baseline workload, retired-layer measurement, pilot workload, or calculation exists. [N03] [N06]

### Migration stages and rollback

1. Freeze `I1` neutral record/version/export schema and round-trip the current store without changing reads.
2. Add `I2`, `I3`, and `I5` in shadow mode; compare context hashes, retrieval corpus results, and event completeness while current output remains authoritative.
3. Run `I4` proposal-only against immutable source revisions; do not activate destructive changes.
4. Cut over per role only after V1-V8 pass and old clients are fenced.
5. Retain old reads and the neutral write journal through the rollback window.

Rollback switches reads to the existing verbs, disables new curation activation, and replays only validated neutral events. Malformed or unmatched records remain quarantined. [N03] [N07]

### Cost, license, privacy, and risk

No new memory vendor fee is inherent in the design. Costs are engineering, larger event storage, evaluation, possible model calls, and review queues. IDs-only telemetry is the default; session, context, outcome, and optional content metadata expand privacy scope and need retention and deletion rules. Risks are scope expansion, old-client bypass, event volume, stale changesets, judge/model drift, and added machinery that fails to reduce maintenance. [N03] [N08]

### Revisit triggers

Stop or narrow the architecture if a pilot does not reduce time spent on contract drift/debugging, if R2 same-key convergence or R7 trustworthy application remain unresolved, if the review queue stalls, or if the event/privacy burden exceeds the maintenance it removes. Escalate to A3 or A4 only when a measured owned layer can be retired. [N03] [N06]

## Architecture A3: Mem0 Platform-centered adoption

### Boundary and operating model

Use hosted Mem0 Platform for memory storage, search/rerank, history, managed service operation, export jobs, feedback, and webhooks. Keep the vendor-neutral `ateam` facade and all seven adapter groups `B1-B7`. Do not use Mem0 OSS behavior, paths, or the patched diagnostic as Platform evidence. [N04] [N09]

### Retained custom code and maintenance evidence

Retain role/principal policy, revision/reconciliation, lifecycle delivery, token budgeting, encrypted outage cache, provenance, curation authority, R7 semantics, migration, rollback, dashboard normalization, and contract tests. Managed storage, search, history, and export jobs could move some service operation outside the project, while seven adapter groups and unresolved contract, export, and outage work remain. Net maintenance reduction is **unknown** because no baseline or credentialed pilot workload has been measured. [N04] [N06]

### Migration stages and rollback

1. Obtain a non-production account and pin the exact Platform API/algorithm edition and contractual controls.
2. Run V1, V2, V4, V6, V8, and V10 with synthetic role, conflict, deletion, export/re-import, quota, and outage fixtures.
3. Backfill immutable source records into a shadow namespace; never re-extract silently during migration.
4. Shadow-read and compare IDs, role filters, context budgets, and known queries before dual-write.
5. Dual-write with idempotency and reconciliation; cut over per role only after export and reverse import pass.

Rollback reads from Dolt and replays the neutral journal. The local encrypted hot cache is degraded read-only continuity, not a second writable authority. [N04] [N07]

### Cost, licensing, privacy, and risk

Public pricing observed on 2026-08-26 ranged from free Hobby limits through Starter at `$19/month` and Pro at `$249/month`; enterprise and overage economics require a current quote and workload measurement. Hosted terms, processors, retention, deletion, encryption, tenant controls, and export rights require contract review. Main risks are unvalidated role-to-entity authorization, differing default-extraction and direct-import attribution paths, contradictory-write ordering, algorithm/API drift, incomplete re-import, vendor outage/account loss, quota amplification, and adapters recreating the custom system above the service. [N04] [N10]

### Revisit triggers

Promote A3 only if a credentialed pilot passes all R1-R7 scenarios, complete reverse import, deletion verification, bounded outage behavior, privacy/legal review, and measured cost. Reject it if any required role filter is client-only, post-cutover writes cannot be exported and replayed, or the seven adapter groups do not retire a measured maintenance layer. [N04] [N06]

## Architecture A4: neutral PostgreSQL/LangGraph/LangMem/OTel hybrid

### Boundary and operating model

Use PostgreSQL 18 and pgvector 0.8.6 as the transactional record, event, lexical, and vector source; use LangGraph's store mapping behind one service implementation; use LangMem only to propose curation changes; and use OTel-compatible transport for project-defined events. Keep all eight adapter groups `H1-H8`, the `ateam` CLI/API, role and tier semantics, deterministic hot selection, human conflict authority, Claude/Codex lifecycle delivery, dashboard, migration, and rollback. [N05] [N08]

### Retained custom code and maintenance evidence

The hybrid could retire Dolt-specific memory storage/sync code, the mutable applied counter, and part of lexical search plumbing, but it adds PostgreSQL/pgvector operation, a Python/service runtime, model/index management, OTel collection, evaluation, backup, monitoring, and on-call ownership. Net maintenance reduction is **unknown** because exact current work, retired work, retained adapters, and new operational workload were not measured. [N05] [N06]

### Migration stages and rollback

1. Freeze the neutral schema, RLS policy, revision/CAS contract, outbox, event vocabulary, model/index manifest, and logical export.
2. Prove concurrent same-ID writes, role isolation, transaction/outbox recovery, backup/restore, and model-free lexical fallback in a disposable deployment.
3. Backfill immutable records and source history; build indexes from pinned models; shadow-select hot/cold results against the fixed corpus.
4. Run LangMem proposal-only with human review and source-span checks; activate no destructive change until V5 passes.
5. Dual-write, reconcile, cut over by role, and retain Dolt plus neutral journal through reverse-import expiry.

Rollback stops curation workers, returns reads to Dolt, replays journaled writes in event order, and rebuilds derived indexes. PostgreSQL snapshots alone are not a source-compatible rollback. [N05] [N07]

### Cost, licensing, privacy, and risk

The pinned software stack uses documented open-source licenses, but engineering and operational costs include database hosts, backups, monitoring, Python/model services, embeddings/reranking, telemetry storage, migrations, and review. Self-hosting improves direct data control but expands copies into database indexes, caches, spools, traces, and model providers. IDs-only telemetry, RLS, encryption, deletion propagation, model egress controls, and restore drills are mandatory design work. [N05] [N08] [N10]

### Revisit triggers

Promote A4 only after the integrated V1-V10 suite passes and measured maintenance shows that retired Dolt/retrieval/counter work exceeds the new service and model burden. Remove LangGraph, LangMem, vector search, MCP, or the telemetry backend individually if a component does not retire measurable owned work. Reject the architecture if transaction/outbox gaps, model drift, restore, offline behavior, or privacy copies remain unbounded. [N05] [N06]

## Draft recommendation and review questions

**DRAFT validation sequence:** validate **A2 bounded incremental improvement** through bounded, shadowed work while keeping **A1 unchanged as the production authority and rollback path**. This is not an architecture-selection recommendation and makes no buy or hybrid adoption decision. A2 targets the observed retrieval, telemetry, export, curation, and delivery failures while preserving the only reproduced neutral boundary. Its maintenance effect is unknown, so validation must stop if it does not retire measurable debugging or parity work. [N02] [N03] [N06] [N07]

The strongest screened external comparator is A3 because Mem0 Platform has the broadest documented managed memory surface in the screened set, but its required hosted authorization, concurrency, export/re-import, privacy, outage, and deletion evidence is missing. The strongest screened hybrid comparator is A4 because its selected components offer the strongest documented transactional, CRUD/search, curation-mechanism, and telemetry primitives, but the integrated operating burden and maintenance reduction are unknown. These are research comparisons, not adoption recommendations. [N04] [N05] [N06]

Independent review should verify that no numerical decision output survives before gate clearance, that unknowns remain non-numerical, and that the validation sequence cannot be read as an adoption winner. Architecture selection can begin only after at least one candidate clears R1-R7 and supplies measured maintenance inputs; an eligible comparison must then execute the frozen scoring and sensitivity template. [N01] [N06]

## Remaining unknowns

- Cross-machine same-memory convergence and conflict visibility in the current/Dolt path. [N02] [N06]
- Lifecycle delivery success, truncation, compact/resume behavior, and outage behavior in Claude Code and Codex for every changed path. [N06]
- Complete logical export, clean-target restore, reverse import, and post-cutover replay for all architectures. [N07]
- Representative retrieval quality, latency, token cost, and abstention on the fixed repository-shaped corpus. [N06] [N08]
- Trustworthy application capture, independent outcomes, exposure bias, event loss, and approved telemetry privacy/retention rules. [N08]
- Mem0 Platform credentialed role authorization and isolation under the documented default-extraction, direct-import, and `AND` filter semantics; write ordering, algorithm edition, deletion completion, export fidelity, quotas, outage behavior, contractual controls, and workload cost. [N04]
- PostgreSQL/pgvector workload behavior, LangGraph compare-and-set semantics, LangMem quality and runtime burden, embedding/reranker choice, transaction/outbox integration, and hybrid maintenance cost. [N05] [N06]
- Measured baseline labor and operational workload by current layer, plus retired, retained, and added work for each changed architecture; without these inputs, A2-A4 maintenance reduction remains unknown and cannot be scored. [N02] [N06]
