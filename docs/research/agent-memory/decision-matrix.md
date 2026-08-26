# Agent memory architecture decision matrix

**Synthesis date:** 2026-08-26

**Status:** DRAFT for independent evidence review

**Approved contract:** [agent memory research plan](../../2026-08-26-agent-memory-research-plan.html)

**Decision scope:** research synthesis only; no implementation, purchase, migration, or deployment decision is made here.

## Decision result

No architecture currently clears all R1-R7 gates with grade A or B evidence. The unchanged system has five partial gates, the bounded incremental architecture contains unbuilt D-grade paths, Mem0 Platform has unresolved required behavior outside its neutral access boundary, and the neutral hybrid is an unbuilt integration. Weighted arithmetic therefore does not produce an adoption winner. Unknowns remain `U` and produce score intervals rather than point values. [N01] [N06] [N07]

The draft recommendation is to keep the current Dolt-backed path authoritative while advancing the **bounded incremental architecture** as the lowest-migration validation target. This is not a finding that the incremental design passes the gate or maximizes maintenance reduction. It is a conditional preference for proving the neutral export, context receipt, retrieval, curation changeset, and append-only event contracts without first introducing vendor lock-in or a new database and model-service operating stack. Mem0 Platform remains the strongest screened buy candidate, and the PostgreSQL/pgvector plus LangGraph/LangMem and OTel composition remains the strongest screened hybrid. Neither is adoption-ready. [N02] [N03] [N04] [N05] [N07]

## Normalized evidence ledger

These rows are the only claim sources used by this matrix. Evidence grades and `Pass`, `Partial`, `Fail`, and `Unknown` retain the meanings frozen in the approved plan. A design projection is grade D until built and reproduced. Newer observed behavior overrides conflicting prose. [N01]

| ID | Candidate, edition, and pin | Normalized evidence claim | Grade and boundary |
| --- | --- | --- | --- |
| **N01** | Approved plan, repository baseline `e028082e`; approved 2026-08-26 | Freezes R1-R7, A-D evidence grades, gate-before-score rule, exact weights, unknown handling, and sensitivity method. | Contract, not candidate evidence. |
| **N02** | agent-teams plugin `0.60.0`, source `3bb52fa`; live snapshot and executable checks on 2026-08-26 | Current system passes R1 and R6; R2, R3, R4, R5, and R7 are partial. It owns eight maintenance layers and has no lossless memory export/import. | A/B. Snapshot counts are date-specific. [Source](current-system.md) |
| **N03** | Bounded incremental design in `current-system.md` | Six changes are defined: canonical contract/export, receipt-bearing context, bounded lexical retrieval, reviewable curation, append-only events, and artifact provenance. No major layer is retired; R2 and R7 remain partial even if projected changes work. | Baseline A/B plus unbuilt D. No projected pass is credited. [Source](current-system.md#candidate-b-bounded-incremental-improvement) |
| **N04** | Mem0 Platform, hosted edition; documentation accessed 2026-08-26 | Platform supplies managed scoped memory, filtered/reranked search, history, export jobs, REST/SDK/CLI/MCP, feedback, and webhooks. R6 passes only as an access boundary. Role mapping, contradictory-write semantics, lifecycle delivery, token budgeting, bounded conflict policy, usefulness lineage, full re-import, offline behavior, and contract/privacy review remain partial or unknown. | B only; no hosted credentialed probe. OSS behavior is not transferred. [Source](turnkey-platforms.md#deep-evaluation-1-mem0) |
| **N05** | PostgreSQL 18; pgvector `0.8.6` at `8ee86c9`; LangGraph `1.2.11`; checkpoint `4.2.0`; LangMem `0.0.30`; OTel spec `1.60.0` and semconv `1.44.0` | PostgreSQL/pgvector is the strongest documented transactional shared-store primitive screened; LangGraph is the closest CRUD/search mapping; LangMem supplies a model-assisted curation mechanism; OTel can transport events. None supplies role authority, hot delivery, complete curation authority, or usefulness semantics. | A for isolated LangGraph/SQLite/OTel artifact probes where cited; B for PostgreSQL and published interfaces; integrated architecture D. [Component source](composable-components.md) [Framework source](framework-libraries.md) [Quality source](quality-observability.md) |
| **N06** | Validation join at repository `1c29b10`; corrected evidence at HEAD `f7b2511` | Common V1-V10 validation leaves all four options gate-blocked. Current retrieval failed the representative query, current telemetry and export failed their scenarios, Mem0 Platform credentialed scenarios were not run, and the integrated hybrid was not built. | A/B/D by scenario. [Source](validation-and-migration.md) |
| **N07** | Common migration-risk register, 2026-08-26 | Every changed architecture needs stable IDs, role predicates, immutable source evidence, idempotency/revisions, dual-read comparison, complete logical export plus physical backup, staged cutover, and reverse replay. | A/B for mechanisms; migration executions remain D. [Source](validation-and-migration.md#migration-risk-register) |
| **N08** | Quality and observability evidence, agent-teams `0.60.0`; OTel GenAI commit `56d6b11`; OpenInference `0.1.33`; Phoenix `20.4.0` | Exposure, retrieval, application, outcome, feedback, and curation are distinct events. Current mutable applied counts can lose increments. OTel memory fields remain Development and do not define the project-specific outcome or retention semantics. | A for source/schema inspection; B for integration surfaces; end-to-end capture D. [Source](quality-observability.md#minimal-neutral-event-contract) |
| **N09** | Corrected Mem0 OSS `v2.0.19` evidence at HEAD `f7b2511` | Public configuration rejects the documented mock embedder; top-level `agent_id` search is rejected; a patched diagnostic worked only after unsupported runtime substitutions and filter syntax correction. The prior count also measured response keys, not retrieved memories. | **B, not A.** Diagnostic only; no score or Platform gate inherits it. [Source](turnkey-platforms.md#mem0-oss-supported-path-failure-and-patched-diagnostic) |
| **N10** | Dated cost, license, and privacy sources in candidate ledgers | Mem0 Platform public plans and terms are dated snapshots. PostgreSQL/pgvector, LangGraph/LangMem, and the screened telemetry components have documented open-source licenses, but deployment, model, monitoring, storage, and engineering costs remain workload-dependent. | B; no procurement quote, workload bill, or legal approval. [Turnkey ledger](turnkey-platforms.md#primary-evidence-ledger) [Component ledger](composable-components.md#evidence-ledger) |

### Edition and version boundaries

- **Mem0 Platform and Mem0 OSS are separate candidates.** Platform scores use hosted documentation only. The corrected OSS diagnostic is grade B and is excluded from Platform evidence. [N04] [N09]
- **Zep Cloud, Graphiti OSS, Letta Cloud, and Letta local are separate editions or products.** Their behavior is not transferred into the selected buy or hybrid architecture. [N06]
- **LangGraph in-memory reproduction is not PostgreSQL evidence.** PostgreSQL concurrency, pgvector retrieval, integrated transactions, and restore remain unrun for this architecture. [N05] [N06]
- **OTel transport is not R7 semantics.** The `agent_teams.memory.*` event vocabulary, capture completeness, outcome join, privacy policy, and retention use remain project-owned. [N08]

## R1-R7 gate before scoring

The table records the current evidence state. Adapter names show how a changed architecture proposes to close a gap; an unbuilt adapter does not convert a gate to `Pass`.

| Architecture | R1 | R2 | R3 | R4 | R5 | R6 | R7 | Ranking eligibility |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| **A1 current unchanged** | Pass A | Partial B | Partial A | Partial A | Partial A | Pass A | Partial A | **Blocked**: five partial gates. [N02] [N06] |
| **A2 bounded incremental** | Partial A/D (`I1`) | Partial B/D (`I1`, `I5`) | Unknown A/D (`I2`) | Unknown A/D (`I4`) | Unknown A/D (`I3`, `I4`) | Partial A/D (`I1`, `I2`, `I6`) | Partial A/D (`I5`) | **Blocked**: changed paths are unbuilt; R2 and R7 remain incomplete by design. [N03] [N06] |
| **A3 Mem0 Platform-centered adoption** | Partial B (`B1`) | Partial B; V2 unknown (`B2`) | Partial B (`B3`) | Partial B (`B4`) | Partial B (`B3`) | Pass B for access only (`B5`) | Partial B (`B6`) | **Blocked**: required Platform behavior and all gap adapters remain unproved. [N04] [N06] |
| **A4 neutral component hybrid** | Partial A/B/D (`H1`) | Partial B/D (`H2`) | Partial A/D (`H3`) | Partial A/B/D (`H4`) | Partial A/B/D (`H5`) | Partial A/B/D (`H6`) | Partial A/B/D (`H7`) | **Blocked**: integrated architecture and adapters are unbuilt. [N05] [N06] |

### Gap adapters and maintenance count

**A2 adapters, all project-owned:** `I1` canonical versioned record/export contract and revision policy; `I2` receipt-bearing context command plus Claude/Codex lifecycle calls; `I3` bounded lexical ranker and fixed corpus; `I4` proposal/apply curation transaction and review queue; `I5` append-only exposure/application/outcome events plus aggregation; `I6` artifact provenance and contract fixtures. This adds six maintained surfaces while consolidating existing ones. [N03]

**A3 adapters, all project-owned:** `B1` deny-unscoped role/principal mapper; `B2` idempotent write gateway, version/reconciliation ledger, and conflict quarantine; `B3` deterministic token-budget selector, encrypted hot cache, delivery receipts, and Claude/Codex lifecycle adapters; `B4` immutable provenance plus reviewable promotion/demotion/conflict policy; `B5` versioned `ateam` compatibility facade over the exact Platform API; `B6` exposure/application/outcome event store and retention policy; `B7` export/import validator, deletion verifier, dual-run controller, dashboard normalization, and outage rollback. The vendor operates the service, but these seven adapter groups remain owned. [N04] [N06] [N07] [N08]

**A4 adapters, all project-owned:** `H1` role/principal derivation plus PostgreSQL schema/RLS; `H2` expected revisions, idempotency, transaction/outbox, retries, and conflict ledger; `H3` deterministic hot response, cache, receipts, and Claude/Codex lifecycle calls; `H4` immutable provenance, LangMem proposal, human authority gate, transactional changeset, and rollback; `H5` lexical/vector fusion, token cap, abstention, model/index manifest, and evaluation corpus; `H6` versioned CLI/HTTP/OpenAPI/MCP facade and conformance suite; `H7` OTel transport plus project event semantics, spool, independent outcome joins, and bias controls; `H8` logical export/import, physical backup, migration/replay, dashboard normalization, service monitoring, and restore drills. PostgreSQL, Python/LangGraph/LangMem, model, and telemetry operations are additional maintenance, not retired work. [N05] [N06] [N07] [N08]

## Frozen weighted score

Because every architecture is gate-blocked, none receives a ranking-eligible weighted point total. The calculations below are diagnostic audit arithmetic: they expose known dimension judgments and uncertainty spans without allowing a blocked option to win. [N01] [N06]

### Formula and scale

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

Raw scores use `0` unacceptable/absent, `1` major gaps, `2` workable with material burden, `3` strong with bounded caveats, and `4` strong and directly evidenced. `U` means evidence is insufficient. For a known score, contribution is `weight * raw / 4`. For an architecture with unknowns, the displayed interval is:

```text
known contribution + sum(each unknown contribution in [0, weight])
```

The lower interval endpoint is only an uncertainty bound. It does not assign zero to an unknown. No midpoint or point total is calculated for an architecture containing `U`. [N01]

### Raw 0-4 matrix

| Architecture | M | I | P | D | S | R | O | C | L |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| **A1 current unchanged** | 0 [N02] | 1 [N02] | 4 [N02] | 2 [N06] | 3 [N02] | 2 [N06] | 2 [N02] | 2 [N02] | 3 [N02] |
| **A2 bounded incremental** | 1 [N03] | 1 [N03] | 3 [N03] | **U** [N03] | 2 [N08] | **U** [N06] | 1 [N03] | 1 [N03] | 3 [N03] |
| **A3 Mem0 Platform adoption** | 2 [N04] | 2 [N04] | 3 [N04] | **U** [N04] | **U** [N04] | **U** [N04] | 3 [N04] | 2 [N10] | 2 [N04] |
| **A4 neutral hybrid** | **U** [N06] | 1 [N06] | 3 [N05] | 2 [N05] | 2 [N06] | **U** [N06] | 1 [N06] | **U** [N06] | 3 [N10] |

Score rationale is deliberately conservative:

- A1 receives no maintenance-reduction credit because it retires no owned layer. Its neutral CLI is directly evidenced, while export, restore, reliability, and operations have material gaps. [N02] [N06]
- A2 receives only modest maintenance credit because it consolidates contracts but retires no major layer and adds event, export, curation, and evaluation work. Export/rollback and changed-path reliability remain unknown until built. [N03] [N06]
- A3 receives material but not strong maintenance credit because managed service/search/history/export operation moves outside the project while seven adapter groups remain. Export round trip, contract/privacy acceptability, and workload reliability are unknown. [N04] [N06]
- A4 does not receive a maintenance point estimate because the join explicitly leaves exact retained code and operations unknown. It has a strong neutral boundary direction, but it adds a database service, Python worker, model/evaluation, telemetry, backup, and on-call surface. [N05] [N06]

### Weighted arithmetic

Each cell is `weight * raw / 4`; `U[0,w]` is the unresolved contribution range for that dimension.

| Architecture | M | I | P | D | S | R | O | C | L | Total / 100 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| **A1** | 0.00 | 3.75 | 15.00 | 6.00 | 7.50 | 5.00 | 4.00 | 3.00 | 3.00 | **47.25** |
| **A2** | 5.00 | 3.75 | 11.25 | U[0,12] | 5.00 | U[0,10] | 2.00 | 1.50 | 3.00 | **[31.50, 53.50]** |
| **A3** | 10.00 | 7.50 | 11.25 | U[0,12] | U[0,10] | U[0,10] | 6.00 | 3.00 | 2.00 | **[39.75, 71.75]** |
| **A4** | U[0,20] | 3.75 | 11.25 | 6.00 | 5.00 | U[0,10] | 2.00 | U[0,6] | 3.00 | **[31.00, 67.00]** |

Worked checks:

```text
A1 = (20*0 + 15*1 + 15*4 + 12*2 + 10*3 + 10*2 + 8*2 + 6*2 + 4*3) / 4
   = 189 / 4 = 47.25

A3 known = (20*2 + 15*2 + 15*3 + 8*3 + 6*2 + 4*2) / 4 = 159 / 4 = 39.75
A3 unknown span = (12 + 10 + 10) * [0,4] / 4 = [0,32]
A3 total = [39.75, 71.75], not a point score.
```

The arithmetic does not rank the options: A2-A4 have unresolved dimensions, every interval overlaps another option, and every architecture is gate-blocked. [N01] [N06]

## Sensitivity

Equal weights use nine weights of `1`. The six variation cases change one of the three largest weights by 25 percent and normalize by the resulting total: maintenance `15` or `25`, integration `11.25` or `18.75`, and portability `11.25` or `18.75`. Other weights remain at their frozen values before normalization.

| Weight case | A1 | A2 interval | A3 interval | A4 interval |
| --- | ---: | ---: | ---: | ---: |
| Frozen 20/15/15/12/10/10/8/6/4 | 47.25 | [31.50, 53.50] | [39.75, 71.75] | [31.00, 67.00] |
| Equal weights | 52.78 | [33.33, 55.56] | [38.89, 72.22] | [33.33, 66.67] |
| Maintenance -25% | 49.74 | [31.84, 55.00] | [39.21, 72.89] | [32.63, 65.26] |
| Maintenance +25% | 45.00 | [31.19, 52.14] | [40.24, 70.71] | [29.52, 68.57] |
| Integration -25% | 48.12 | [31.75, 54.61] | [39.35, 72.60] | [31.23, 68.64] |
| Integration +25% | 46.45 | [31.27, 52.47] | [40.12, 70.96] | [30.78, 65.48] |
| Portability -25% | 45.19 | [29.81, 52.66] | [38.38, 71.62] | [29.29, 66.69] |
| Portability +25% | 49.16 | [33.07, 54.28] | [41.02, 71.87] | [32.59, 67.29] |

**Sensitivity result:** no case produces a defensible winner. A3 has the highest possible ceiling in every tested weighting, but that ceiling consists partly of unresolved export/rollback, privacy, and reliability evidence. A ceiling is not a score. A1 is the only point total because it is the only fully observed scoring surface, yet its R1-R7 gate is blocked and its score is not comparable to unresolved intervals as a winner. [N01] [N04] [N06]

## Architecture A1: current system unchanged

### Boundary and operating model

Keep agent-teams `0.60.0`: Dolt/Beads source of truth; fresh/hot/cold key convention; Go CLI verbs; lexical recall; prompt-driven condense with lock and drain; Claude and Codex delivery definitions; dashboard; tests; prebuilt binaries; and release parity. It stays local-first and uses Dolt remote synchronization across machines. [N02]

### Retained custom code and maintenance estimate

All eight inventoried layers remain custom. Estimated recurring maintenance reduction is **0%** by definition. Service spend remains low, but engineering, prompt, harness, dashboard, synchronization, and release work remain. This is an analytical estimate from the unchanged boundary, not a measured labor study. [N02]

### Migration, rollback, and operation

There is no data migration. The operational improvement available without changing architecture is to document and rehearse Dolt restore, but that work is not credited to this unchanged candidate. Rollback from any later candidate is to keep this read path available, preserve source writes, and replay neutral post-cutover events only after a reverse import has been proved. [N06] [N07]

### Cost, license, privacy, and risk

No new vendor subscription or external memory processor is introduced. Sensitive operational memory remains in the project-controlled workspace. Engineering cost remains material. Principal risks are same-key convergence ambiguity, unreliable lifecycle recovery, overbroad lexical retrieval, unenforced hot budgets, non-transactional curation, mutable applied counters, incomplete export/restore, and artifact drift. [N02] [N06]

### Revisit triggers

Revisit immediately if V2 measures silent write loss, V3 measures missing context, V4 continues broad false-positive retrieval, V6 shows material event loss, or V8 cannot restore a complete fixture. Also revisit if maintenance time for the eight owned layers is measured and exceeds the bounded cost of a validated alternative. [N06]

## Architecture A2: bounded incremental improvement

### Boundary and operating model

Keep Dolt/Beads and the `ateam` CLI as source of truth and compatibility boundary. Add only adapters `I1-I6`: canonical records/export, context receipts, bounded lexical retrieval, reviewable curation changesets, append-only events, and artifact provenance. Claude and Codex remain thin but separate lifecycle callers. [N03]

### Retained custom code and maintenance estimate

All current major layers remain, and six improved surfaces must be maintained. The estimated steady-state maintenance reduction is **5-15%**, limited to less duplicated contract work and faster diagnosis; short-term maintenance rises during implementation and migration. This range is analytical and low confidence because the source evidence explicitly says no major layer is retired. [N03]

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

### Retained custom code and maintenance estimate

Retain role/principal policy, revision/reconciliation, lifecycle delivery, token budgeting, encrypted outage cache, provenance, curation authority, R7 semantics, migration, rollback, dashboard normalization, and contract tests. Estimated recurring maintenance reduction is **25-40%** if managed storage/search/history/export operation is reliable and the adapters remain bounded. The estimate is low confidence and excludes unresolved contract, export, and outage work. [N04] [N06]

### Migration stages and rollback

1. Obtain a non-production account and pin the exact Platform API/algorithm edition and contractual controls.
2. Run V1, V2, V4, V6, V8, and V10 with synthetic role, conflict, deletion, export/re-import, quota, and outage fixtures.
3. Backfill immutable source records into a shadow namespace; never re-extract silently during migration.
4. Shadow-read and compare IDs, role filters, context budgets, and known queries before dual-write.
5. Dual-write with idempotency and reconciliation; cut over per role only after export and reverse import pass.

Rollback reads from Dolt and replays the neutral journal. The local encrypted hot cache is degraded read-only continuity, not a second writable authority. [N04] [N07]

### Cost, licensing, privacy, and risk

Public pricing observed on 2026-08-26 ranged from free Hobby limits through Starter at `$19/month` and Pro at `$249/month`; enterprise and overage economics require a current quote and workload measurement. Hosted terms, processors, retention, deletion, encryption, tenant controls, and export rights require contract review. Main risks are entity-scope ambiguity, contradictory-write ordering, algorithm/API drift, incomplete re-import, vendor outage/account loss, quota amplification, and adapters recreating the custom system above the service. [N04] [N10]

### Revisit triggers

Promote A3 only if a credentialed pilot passes all R1-R7 scenarios, complete reverse import, deletion verification, bounded outage behavior, privacy/legal review, and measured cost. Reject it if any required role filter is client-only, post-cutover writes cannot be exported and replayed, or the seven adapter groups do not retire a measured maintenance layer. [N04] [N06]

## Architecture A4: neutral PostgreSQL/LangGraph/LangMem/OTel hybrid

### Boundary and operating model

Use PostgreSQL 18 and pgvector 0.8.6 as the transactional record, event, lexical, and vector source; use LangGraph's store mapping behind one service implementation; use LangMem only to propose curation changes; and use OTel-compatible transport for project-defined events. Keep all eight adapter groups `H1-H8`, the `ateam` CLI/API, role and tier semantics, deterministic hot selection, human conflict authority, Claude/Codex lifecycle delivery, dashboard, migration, and rollback. [N05] [N08]

### Retained custom code and maintenance estimate

The hybrid can retire Dolt-specific memory storage/sync code, the mutable applied counter, and part of lexical search plumbing, but it adds PostgreSQL/pgvector operation, a Python/service runtime, model/index management, OTel collection, evaluation, backup, monitoring, and on-call ownership. Estimated recurring maintenance reduction is **10-25%**, but the scoring value remains `U` because exact retained code and operations were not measured. [N05] [N06]

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

**DRAFT recommendation:** choose **A2 bounded incremental improvement as the next validation target**, keep **A1 unchanged as the production authority and rollback path**, and make no buy or hybrid adoption decision yet. This recommendation is based on reversibility and the evidence gaps, not on a weighted winner. A2 directly targets the observed retrieval, telemetry, export, curation, and delivery failures while preserving the only reproduced neutral boundary. Its expected maintenance reduction is modest, so it should be stopped if a bounded pilot does not retire measurable debugging or parity work. [N02] [N03] [N06] [N07]

The strongest buy candidate is A3 because Mem0 Platform has the broadest documented managed memory surface in the screened set, but its required hosted concurrency, export/re-import, privacy, outage, and deletion evidence is missing. The strongest hybrid is A4 because its selected components offer the strongest documented transactional, CRUD/search, curation-mechanism, and telemetry primitives, but the integrated operating burden and maintenance reduction are unknown. [N04] [N05] [N06]

Independent review should decide whether the absence of any gate-clearing architecture makes the draft recommendation appropriately conservative, verify every raw score against N01-N10, and challenge the four analytical maintenance ranges. The recommendation must change if a reviewer finds that A2 cannot materially reduce maintenance or if a credentialed A3/A4 pilot closes all gates with lower measured owned burden. [N01] [N06]

## Remaining unknowns

- Cross-machine same-memory convergence and conflict visibility in the current/Dolt path. [N02] [N06]
- Lifecycle delivery success, truncation, compact/resume behavior, and outage behavior in Claude Code and Codex for every changed path. [N06]
- Complete logical export, clean-target restore, reverse import, and post-cutover replay for all architectures. [N07]
- Representative retrieval quality, latency, token cost, and abstention on the fixed repository-shaped corpus. [N06] [N08]
- Trustworthy application capture, independent outcomes, exposure bias, event loss, and approved telemetry privacy/retention rules. [N08]
- Mem0 Platform entity combinations, write ordering, algorithm edition, deletion completion, export fidelity, quotas, outage behavior, contractual controls, and workload cost. [N04]
- PostgreSQL/pgvector workload behavior, LangGraph compare-and-set semantics, LangMem quality and runtime burden, embedding/reranker choice, transaction/outbox integration, and hybrid maintenance cost. [N05] [N06]
- Measured engineering time by current layer, without which all maintenance-reduction percentages remain analytical estimates. [N02] [N06]
