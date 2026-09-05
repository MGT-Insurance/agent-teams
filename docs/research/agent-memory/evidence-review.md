# Independent evidence review: agent memory research

**Review date:** 2026-08-26

**Reviewer bead:** `agent-teams-ird0.9`

**Initiative:** `at-jo7h` under `agent-teams-ird0`

**Reviewed range:** `e028082` through `20dd426b22fb3640281dc47878683847aad75d47`

**Approved contract:** [agent memory research plan](../../2026-08-26-agent-memory-research-plan.html)

**Initial result:** **FAIL. Two blocking findings remained at `20dd426`.**

**Final re-review result:** **PASS at `f91277ff3268a8dc6c4258c2284ecff3f09ec5eb`.**

## Initial findings

### ER-01 - Blocking, high: scores exist for candidates that fail the required gate

**Artifact and section:**

- Approved plan, `Primary-source evidence and scoring > Non-gating score`, line 477.
- [decision-matrix.md](decision-matrix.md), `R1-R7 gate before scoring`, lines 45-50.
- [decision-matrix.md](decision-matrix.md), `Frozen weighted score`, lines 60-143.

**Evidence:** The approved plan permits a weighted score only after a candidate clears all gates. An explicit hybrid can also receive a score after it fills every gap. The matrix marks all four architectures as blocked. It then assigns raw scores, weighted totals or intervals, and seven sensitivity results to all four.

The `diagnostic audit arithmetic` label does not change the approved rule. The plan does not define a diagnostic exception for blocked candidates.

**Impact:** The matrix gives numerical position and sensitivity to ineligible architectures. These values can affect the final recommendation despite the gate. The `A3` ceiling language is one example of this risk. The current draft does not declare an adoption winner, but the method still violates the frozen contract.

**Required disposition:** Remove raw scores, weighted totals, intervals, and sensitivity results for all blocked candidates. Alternatively, obtain an explicit human amendment to the approved scoring contract. After an amendment, recompute the matrix under the amended rule. Keep the gate results and qualitative comparisons visible.

### ER-02 - Blocking, high: maintenance-reduction ranges lack a reproducible basis

**Artifact and section:**

- [decision-matrix.md](decision-matrix.md), `Raw 0-4 matrix`, lines 88-102.
- [decision-matrix.md](decision-matrix.md), `Architecture A1-A4 > Retained custom code and maintenance estimate`, lines 151-153, 173-175, 201-203, and 229-231.
- [decision-matrix.md](decision-matrix.md), `Remaining unknowns`, line 268.

**Evidence:** The matrix gives these recurring maintenance-reduction estimates:

| Architecture | Published estimate | Published raw `M` score |
| --- | ---: | ---: |
| A1 current unchanged | 0% | 0 |
| A2 bounded incremental | 5-15% | 1 |
| A3 Mem0 Platform | 25-40% | 2 |
| A4 neutral hybrid | 10-25% | `U` |

No artifact supplies baseline labor, incident load, release work, service operations, or a formula for retired and retained work. The matrix also states that engineering time by layer is unknown. The A4 percentage conflicts with its explicit unknown raw maintenance score.

**Impact:** Expected maintenance reduction has the largest approved weight. Unsupported percentages can become a load-bearing basis for shortlist or recommendation language. Low-confidence labels do not make the ranges reproducible.

**Required disposition:** Replace A2-A4 percentages and maintenance raw scores with `U` or qualitative statements. The 0% value for unchanged A1 is valid by definition. A quantitative replacement must include the baseline, workload, retired layers, retained adapters, new operations, time horizon, formula, and uncertainty basis. Then recompute all eligible scores.

### ER-03 - Non-blocking, minor: current Mem0 entity documentation resolves the cited wording ambiguity

**Artifact and section:**

- [turnkey-platforms.md](turnkey-platforms.md), `Mem0 > R1-R7 matrix`, line 92.
- [validation-and-migration.md](validation-and-migration.md), `Migration-risk register`, line 213.
- [decision-matrix.md](decision-matrix.md), `Remaining unknowns`, line 266.

**Evidence:** The current [Mem0 entity-scoped memory documentation](https://docs.mem0.ai/platform/features/entity-scoped-memory) states that inferred memories use either `user_id` or `agent_id` by default. Direct import can associate both identifiers. The current [filter documentation](https://docs.mem0.ai/platform/features/v2-memory-filters) states that multiple filters use `AND` semantics.

The artifacts say that current documentation disagrees about entity combinations. That wording is now stale. No credentialed isolation probe exists, so the role mapping and authorization behavior still remain unproved.

**Impact:** This source drift does not change the `Partial, B` R1 result. It does affect the stated reason for the remaining unknown.

**Required disposition:** Replace the documentation-ambiguity statement with the current documented semantics. Keep `Partial, B` until a credentialed role-isolation and authorization probe passes. Record an access date or a stable documentation snapshot.

## Initial required review results

| Review area | Result | Basis |
| --- | --- | --- |
| Evidence rubric | **FAIL** | ER-01 uses evidence outside the gate-to-score rule. ER-02 assigns unsupported quantitative claims. |
| Candidate-family coverage | **PASS** | All approved internal, turnkey, framework, component, quality, and observability families appear. |
| R1-R7 coverage | **PASS** | Every shortlisted architecture has an explicit grade and gate state. No partial, failure, or unknown is hidden. |
| Scoring reproducibility | **FAIL** | Arithmetic is exact, but score eligibility fails ER-01. Maintenance inputs fail ER-02. |
| Unknown handling | **PASS** | `U` remains explicit. Interval lower bounds are labeled uncertainty bounds, not zero values. No midpoint becomes a hidden score. |
| Current-system fidelity | **PASS** | The architecture, mutable snapshot, retrieval failure, curation behavior, telemetry limits, and executable boundaries reproduce. |
| Current candidate separation | **PASS** | A1 unchanged and A2 incremental remain separate in gates, adapters, maintenance, migration, and recommendation text. |
| Migration and rollback completeness | **PASS for research** | High-risk paths have controls or explicit unknown status. No changed architecture is represented as pilot-ready. |
| Edition and version boundaries | **PASS** | Hosted and OSS products remain separate. Framework, storage, and telemetry probes do not transfer to integrated behavior. |
| Mem0 correction | **PASS** | Mem0 OSS `v2.0.19` remains grade B throughout and is excluded from Platform gates and scores. |
| Links, pins, and paths | **PASS with ER-03 drift** | Local links and anchors resolve. Remote links resolve or reach DOI anti-bot pages. Selected tags and commits match upstream. |
| Unsupported winner or gated failure | **PASS** | The draft names no adoption winner. A2 is a conditional validation target. Every architecture remains gate-blocked. |
| Closure of reviewer bead | **FAIL** | ER-01 and ER-02 require disposition before closure. |

## Plan and R1-R7 coverage

The research covers the plan questions, R1-R7, candidate families, evidence grades, migration risks, and required sensitivities. The score phase does not obey the plan's eligibility rule.

| Gate | Coverage result | Independent result |
| --- | --- | --- |
| R1 role isolation | **PASS** | Current role isolation reproduces. External and hybrid mechanisms remain partial or unbuilt where authorization is not proved. |
| R2 cross-machine synchronization | **PASS** | Current and all alternatives retain explicit convergence, conflict, or integrated-path gaps. |
| R3 automatic hot-context delivery | **PASS** | Current lifecycle surfaces are named. Changed delivery paths remain adapter work and do not receive pass credit. |
| R4 bounded curation | **PASS** | Current lock/drain mechanics reproduce. Reversible proposal, conflict, and authority controls remain partial or unbuilt. |
| R5 cold retrieval | **PASS** | The current false-positive behavior reproduces. External search features do not become a token-budget contract. |
| R6 harness-neutral integration | **PASS** | Neutral access is separate from Claude and Codex lifecycle behavior. Provider-native memory is not treated as cross-harness authority. |
| R7 usefulness feedback | **PASS** | Exposure, application, outcome, feedback, and curation remain separate. No candidate receives a complete feedback-loop pass. |

No weighted result hides a gate failure in the prose. However, ER-01 requires removal of the ineligible numerical results.

## Evidence audit

### Grade-A evidence reopened

The review reopened each load-bearing reproduced artifact and source path:

- Current executable identity and source: the PATH wrapper SHA-256 is `85977625e9f8c3150d8eb692a5ca3a16798e604a9cfc4a0d10383ca3305e07f8`. Its native target SHA-256 is `6d1c4060666383ce6552bee90c965793e4f3a68cba9dc04eb41b662e9b7e505d`.
- Current role recall, startup injection, memory JSON, curation limits, and mutable applied-count implementation.
- LangGraph checkpoint `4.2.0` namespace isolation and deletion behavior.
- `sqlite-vec 0.1.9` lexical and vector filtering, backup, duplicate detection, and conflict detection.
- OTel semantic-convention source at commit `56d6b11`, including Development status and content opt-in fields.
- The corrected Mem0 OSS commit `f7b2511` and its supported-path failure record.

The two executable hashes refer to the wrapper and its native target. They do not conflict.

### Grade-B risk sample reopened

The sample focused on the claims that affect selection, migration, security, cost, and retained ownership:

- Mem0 Platform entity scope, filters, export jobs, direct import, pricing, privacy, and processor terms.
- PostgreSQL 18 MVCC and `pg_dump` consistency behavior.
- LangMem curation defaults and delete behavior.
- Mem0, Graphiti, pgvector, and LangGraph tag or commit pins.
- Export and import boundaries that affect rollback.

The source sample supports the partial and unknown statuses. ER-03 records the only material documentation drift found in this sample.

### Mem0 OSS correction

The isolated `mem0ai 2.0.19` probe reproduced the corrected boundary:

- Public configuration rejects the documented mock embedder.
- The provider factory does not map the mock provider.
- Search scope uses `filters`, not a top-level `agent_id` argument.
- The prior patched path needed unsupported substitutions.
- The prior count measured response keys, not memory results.

All Mem0 OSS claims remain grade B. No Platform score or gate inherits OSS behavior.

## Representative probe results

### Current system

- `ateam learnings tester` returned 33 entries, 27,978 characters, 23 hot entries, 10 fresh entries, and 215 lines.
- `ateam memories-json` returned 1,843 records during review. The research snapshot contains 1,842. The one-record difference is consistent with the documented mutable snapshot boundary.
- The current tier counts were 1,611 cold, 84 fresh, and 148 hot. There were 285 records with positive applied counts.
- `ateam condense-check --json` returned seven `SKIP` results. All seven roles exceeded the hot target.
- The representative recall query returned 64 broad matches. This reproduces the unbounded namespace false-positive result.
- The targeted Go memory tests passed.
- `tests/role-recall-recovery.test.sh` passed 14 checks.
- `tests/hook-subagent-prime-learnings.test.sh` passed.

### External and component paths

- LangGraph isolated the implementer namespace. The reviewer namespace did not leak. Deletion returned no record.
- SQLite and sqlite-vec returned only the implementer match for lexical and vector filters. Backup restoration returned three rows.
- The duplicate probe returned `DUP|m1|m2`.
- The conflict probe returned `CONFLICT|implementer|test_workers|equals|2,4`.
- The reviewer record did not leak into the conflict group.
- Mem0 OSS failed through public configuration before initialization, as the corrected artifact states.

These probes establish component mechanics only. They do not establish integrated A3 or A4 behavior.

## Independent score recomputation

The published arithmetic is exact for its stated inputs. This arithmetic result does not resolve ER-01 or ER-02.

| Weight case | A1 | A2 interval | A3 interval | A4 interval |
| --- | ---: | ---: | ---: | ---: |
| Frozen weights | 47.25 | [31.50, 53.50] | [39.75, 71.75] | [31.00, 67.00] |
| Equal weights | 52.78 | [33.33, 55.56] | [38.89, 72.22] | [33.33, 66.67] |
| Maintenance -25% | 49.74 | [31.84, 55.00] | [39.21, 72.89] | [32.63, 65.26] |
| Maintenance +25% | 45.00 | [31.19, 52.14] | [40.24, 70.71] | [29.52, 68.57] |
| Integration -25% | 48.12 | [31.75, 54.61] | [39.35, 72.60] | [31.23, 68.64] |
| Integration +25% | 46.45 | [31.27, 52.47] | [40.12, 70.96] | [30.78, 65.48] |
| Portability -25% | 45.19 | [29.81, 52.66] | [38.38, 71.62] | [29.29, 66.69] |
| Portability +25% | 49.16 | [33.07, 54.28] | [41.02, 71.87] | [32.59, 67.29] |

The interval implementation preserves unknowns. It does not use zero as an observed value. The sensitivity results do not establish a winner because all candidates fail the gate.

## Candidate and family challenge

### Shortlist and family coverage

The shortlist covers the approved architecture families:

- Internal: A1 current unchanged and A2 bounded incremental.
- Turnkey: Mem0 Platform, with Mem0 OSS, Zep Cloud, Graphiti OSS, Letta Cloud, Letta local, Supermemory, and Cognee screened separately.
- Framework: LangGraph and LangMem, with LlamaIndex, Semantic Kernel, AutoGen, OpenAI Conversations and Responses, and Claude Code memory screened.
- Components: PostgreSQL and pgvector, SQLite FTS and sqlite-vec, LanceDB, Chroma, Qdrant, Neo4j, Apache AGE, and neutral CLI, HTTP, OpenAPI, or MCP boundaries.
- Quality and observability: deterministic curation, provenance, evaluation, OTel, OpenInference, Phoenix, Ragas, and DeepEval.

No approved family is absent. The screened edition boundaries remain explicit. The chosen representatives have stated exclusions and blockers.

### Retained adapters and ownership

The matrix does not treat managed storage or component libraries as complete replacements. It retains role authorization, lifecycle delivery, token budgeting, curation authority, outcome semantics, migration, rollback, and dashboard normalization where required.

A3 retains seven adapter groups. A4 retains eight adapter groups and adds database, Python, model, telemetry, backup, monitoring, and on-call work. This inventory supports the qualitative burden statements. It does not support the percentages in ER-02.

### Security, privacy, cost, and licensing

Hosted processing, model-provider egress, tenant controls, retention, deletion, encryption, and contract review remain explicit for A3. Database indexes, caches, spools, traces, and model providers remain explicit for A4.

Public prices are dated snapshots. Workload cost, overages, engineering cost, and legal approval remain unknown. The artifacts do not use public list prices as total cost of ownership.

## Migration and rollback

The migration register covers schema transforms, identity, concurrency, export fidelity, dual-run, cutover, rollback, data loss, privacy, dependencies, ownership, and offline failure. Each high-risk path has a required control or an explicit unknown status.

The proposed controls include stable IDs, immutable source evidence, revisions, idempotency, a transaction outbox, shadow reads, reconciliation, complete export, physical backup, reverse import, a neutral journal, staged cutover, and restore drills.

No changed architecture has implemented these controls. The report correctly assigns D or unknown status to execution. Thus migration and rollback pass as a research risk analysis, not as a pilot-readiness result.

## Parity inventory

The plausible sibling surfaces are:

- Claude Code and Codex lifecycle adapters.
- Generic CLI, HTTP, OpenAPI, and MCP clients.
- Startup, resume, clear, compaction, and role-subagent initialization.
- DRI, planner, implementer, tester, reviewer, investigator, and steward role namespaces where the current system defines them.
- Current storage, key and tier schema, recall, curation, event counters, dashboard, tests, binary release, and plugin parity.
- Hosted versus OSS, local, enterprise, service, and embedded product editions.
- Lexical, vector, graph, relational, curation, telemetry, evaluation, backup, and restore components.

All these surfaces are in scope or are explicitly excluded. Provider-native memory is a parity reference only. It is not a neutral source of truth. User memory and unrelated machine-local instruction redesign remain outside the approved matrix.

## After-the-fact identifiability

**Current result: FAIL, correctly disclosed.** The current applied counter cannot identify the exact memory revision, exposure, run, session, harness, context, task, or independent outcome. Concurrent increments can also be lost.

Mem0 feedback and webhooks do not supply a complete exposure-to-application-to-outcome chain. The incremental and hybrid designs propose stable IDs and append-only events, but those paths remain unbuilt grade D evidence.

Migration actions also lack complete identifiability today. Stable versions, complete exports, journals, reconciliation records, and reverse import must exist before a cutover can be reconstructed after the fact.

## Initial audit gate results

| Gate | Result |
| --- | --- |
| `git diff --check e028082..HEAD` | PASS |
| Full local and remote link scan | PASS: 22 local references and 196 unique remote URLs checked. Two DOI targets reached ACM anti-bot responses, not missing targets. |
| Upstream pin scan | PASS for Mem0 `v2.0.19`, Graphiti `v0.29.3`, pgvector `v0.8.6`, and LangGraph `1.2.11`. |
| `go vet ./...` | PASS |
| `go test ./...` | FAIL: `TestBuildAgentsJSON_RealRolesStructure` expects `claude-opus-4-8` and reads `claude-sonnet-5`. |
| Base revision reproduction | The same Go test fails at `e028082`. This failure predates the research branch and is not a branch regression. |
| `go build ./...` | PASS |
| Targeted memory and lifecycle tests | PASS |
| Independent arithmetic recomputation | PASS for calculation, FAIL for eligibility and maintenance evidence. |

## Initial closure decision

The initial review left `agent-teams-ird0.9` open. ER-01 and ER-02 blocked a passing evidence review. ER-03 also required disposition before final publication.

## Re-review and final disposition

**Re-review date:** 2026-08-26

**Corrected matrix commit:** `f91277ff3268a8dc6c4258c2284ecff3f09ec5eb`

**Scope:** ER-01, ER-02, ER-03, and regression review of the changed matrix text.

**Final result:** **PASS. No blocking or non-blocking findings remain.**

### Finding dispositions

| Finding | Result | Exact re-review evidence |
| --- | --- | --- |
| ER-01 | **Resolved** | All architecture-specific raw scores, weighted totals, intervals, and sensitivity results are absent. Lines 75-99 retain only the approved future scoring template. Lines 77 and 99 state that the method is dormant while every candidate remains gate-blocked. |
| ER-02 | **Resolved** | A2, A3, and A4 state that net maintenance reduction is `unknown` at lines 131, 159, and 187. No percentage or numerical maintenance judgment remains for these candidates. Line 109 retains A1 `0%` only because an unchanged boundary retires no work. It explicitly states that this value is not a labor estimate. |
| ER-03 | **Resolved** | N04 cites the current Mem0 entity-scope and filter documents. It records speaker-based default attribution, the `infer=False` direct-import exception, and implicit `AND` filter semantics. A3 R1 remains `Partial, B` at line 55 because credentialed authorization and role isolation are unproved. |

### Required checks

| Check | Result | Evidence |
| --- | --- | --- |
| No score for a gate-blocked architecture | **PASS** | The four ranking rows remain `Blocked`. No candidate row contains a raw score, weighted total, interval, or sensitivity result. |
| Frozen weights are dormant | **PASS** | The matrix labels the section `Deferred weighted evaluation template`. It permits execution only after full gate clearance or proof that a hybrid fills every gap. |
| A2-A4 numerical maintenance claims are absent | **PASS** | The three architecture sections use `unknown`. Counts of retained adapters describe scope and are not maintenance-reduction judgments. |
| A1 `0%` is definitional only | **PASS** | The text ties `0%` only to the unchanged architecture boundary and disclaims a labor or operations estimate. |
| Mem0 semantics are current and cited | **PASS** | The cited official pages state that default extraction assigns `user_id` or `agent_id` by speaker. Direct import can populate both. Sibling filter fields use implicit `AND`. |
| Mem0 R1 remains bounded | **PASS** | R1 remains `Partial, B` until a credentialed authorization and role-isolation probe passes. Documented filtering does not become authorization evidence. |
| Recommendation is not a winner | **PASS** | The result and recommendation sections call A2 a shadowed validation sequence. A1 remains the production authority and rollback path. A3 and A4 are research comparators. |
| Unknown handling | **PASS** | Unknowns do not become zeroes, numerical bounds, point scores, or rankings. |
| New inconsistency or unsupported claim | **PASS** | The corrected claims remain within N01-N10 and the prior evidence review. No changed claim grants a gate pass, projected maintenance benefit, or adoption status. |

### Source re-open

The re-review reopened both official Mem0 pages on 2026-08-26:

- [Entity-scoped memory](https://docs.mem0.ai/platform/features/entity-scoped-memory) states that default extraction assigns facts by speaker. It also identifies direct import with `infer=False` as the path that can populate both IDs.
- [V2 memory filters](https://docs.mem0.ai/platform/features/v2-memory-filters) states that sibling top-level fields use implicit `AND`. The flat and explicit `AND` forms are equivalent.

These documents support the corrected B-grade semantics. They do not prove credentialed role authorization or isolation.

### Regression review

The corrected commit changes only [decision-matrix.md](decision-matrix.md). It removes 85 lines and adds 41 lines. The change removes numerical outputs and narrows claims. It does not change evidence grades, gate results, migration controls, edition boundaries, security boundaries, or rollback status.

The future scale and exact weights remain in the matrix because the approved plan freezes them. They are method definitions, not candidate judgments. The `25 percent` sensitivity instruction is also dormant and has no current output.

### Final closure decision

The corrected matrix satisfies the evidence rubric. It contains no unsupported winner, no gated failure hidden by weighting, and no high-risk migration issue without mitigation or unknown status. Close `agent-teams-ird0.9`.
