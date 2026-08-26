# Agent memory validation and migration join

**Validation date:** 2026-08-26

**Repository HEAD:** `1c29b10fb57a87cecbb46f7f32b1513ec0897292`

**Current agent-teams surface:** plugin `0.60.0`; live store read only
**Evidence inputs:** [approved plan](../../2026-08-26-agent-memory-research-plan.html), [current system](current-system.md), [turnkey platforms](turnkey-platforms.md), [framework libraries](framework-libraries.md), [composable components](composable-components.md), and [quality/observability](quality-observability.md)

This is the validation join, not the final recommendation. It applies the approved R1-R7 gates and V1-V10 scenarios without weights, scores, or winner selection. No production memory, hosted account, provider credential, deployment, or long-running service was mutated. Local probes used synthetic data and process- or Python-managed temporary storage.

## Evidence and status rules

- **A:** reproduced at a pinned artifact and supported by source or current official documentation.
- **B:** current primary documentation, source, release, or specification, not reproduced here.
- **C:** directional maintainer or research evidence only.
- **D:** inference or unbuilt design; D cannot pass a gate.
- **Pass:** the complete scenario or requirement is evidenced. **Partial:** a useful part exists but a retained adapter or unverified step is material. **Fail:** observed or source-defined behavior cannot meet the scenario as shipped. **Unknown:** evidence is insufficient.
- A baseline observation is not transferred to a changed or unbuilt path. The incremental and hybrid options therefore retain D-grade deltas until implemented and rerun.

## Shortlist and witnessed eliminations

### Four validation options

| Option | Why it reached the join | Boundary and edition |
| --- | --- | --- |
| **O1 - Current agent-teams unchanged** | It is the control, has observed role isolation and a neutral CLI, and preserves local/offline ownership. | Exact shipped `ateam` `0.60.0` behavior at the pinned source; no hypothetical fixes. |
| **O2 - Bounded incremental agent-teams** | It is the smallest coherent internal design that addresses observable delivery, retrieval, curation, export, telemetry, and artifact-provenance gaps while retaining Dolt and the CLI. | The six changes in `current-system.md`: canonical contract/export, receipt-bearing context, bounded lexical retrieval, reviewable curation, append-only events, and binary provenance. It is unbuilt. |
| **O3 - Mem0 Platform** | Among managed platforms it has the broadest documented combination of scoped memory, filtered/reranked search, history, export jobs, CLI/MCP/SDK/REST access, feedback, webhooks, and managed operation. This is capability coverage, not a preference or final selection. | Hosted Mem0 Platform only, using current Platform documentation accessed 2026-08-26. Mem0 OSS is not credited as the same edition. No hosted credential was available. |
| **O4 - Neutral agent-teams hybrid** | The component evidence identifies PostgreSQL as the strongest documented multi-client transactional store, pgvector as a co-located retrieval primitive, LangGraph `BaseStore` as the closest CRUD/search mapping, LangMem as a curation mechanism, and OTel-compatible append-only events as the clearest telemetry transport. | Agent-teams retains role/tier policy, the versioned CLI/API, hot-set assembly, Claude/Codex lifecycle adapters, curation authority, and outcome semantics. PostgreSQL 18 + pgvector 0.8.6, LangGraph 1.2.11/checkpoint 4.2.0, LangMem 0.0.30, and project-owned `agent_teams.memory.*` events are the pinned parts. The integrated architecture is unbuilt. |

### Eliminated-candidate blockers

No candidate was removed because of category, hosting preference, language, or novelty. Each blocker below is present in primary/source evidence or in a reproduced probe.

| Candidate not taken into the four-option join | Witnessed blocker |
| --- | --- |
| **Mem0 OSS as the external finalist** | R7 is absent in the reference server and R2 convergence is undefined. More importantly, the published local probe in `turnkey-platforms.md` was not reproducible as written: `mem0ai==2.0.19` rejects `embedder.provider="mock"`, and `search(..., agent_id=...)` rejects the top-level entity parameter. A synthetic probe passed only after an explicit unsupported mock-provider registration/substitution and use of `filters={"agent_id": role}`. Discovery: `agent-teams-ird0.11`. |
| **Zep Cloud / Graphiti** | Both lack an applied/outcome usefulness event (R7 fail). Neither proves a token-budgeted hot tier, same-fact concurrent convergence, or full-fidelity export and supported re-import. Graphiti's service/MCP surface is experimental and was not run without a graph service and model provider. |
| **Letta Cloud / local App Server** | The strongest lifecycle behavior is tied to replacing or adopting the Letta agent runtime, not a memory-only neutral contract. R6 is partial, R7 fails, local shared repositories are not equivalent to Cloud shared memory, and legacy `.af` export does not apply to the current App Server. |
| **Supermemory** | Current primary evidence does not establish full hosted export/import, deterministic simultaneous-mount conflict behavior, or R7. The pinned server release is young and the proposed local probe was not run. |
| **Cognee** | Dataset scope and graph curation are useful, but automatic hot delivery, a deterministic token budget, application/outcome telemetry, and full graph round-trip export remain custom or unproved. |
| **LlamaIndex** | It owns a strong in-framework token budget, but per-memory CRUD/export is block-specific, external lifecycle delivery remains custom, concurrency is unverified, and R7 fails as shipped. |
| **Semantic Kernel** | Memory surfaces are split among deprecated Python APIs, preview/release-candidate vector connectors, and experimental C# agent memory. Cross-language parity and a stable memory contract are not established. |
| **AutoGen** | Its small memory protocol and retrieval event are useful, but stable update/delete, conflict/curation, outcome telemetry, and a neutral external lifecycle boundary are absent. |
| **Provider/harness memory** | OpenAI Conversations is provider session state with retention/export coupling, not portable role memory. Claude Code auto memory is machine-local and Claude-specific. Each fails the neutral cross-harness source-of-truth requirement. |
| **SQLite/sqlite-vec as hybrid source of truth** | The local probe is light and reproducible, but SQLite has one writer, no cross-machine merge, and `sqlite-vec` 0.1.9 has an unresolved cross-platform physical-file concern. It remains a viable derived local index/cache, not the strongest shared source-of-truth candidate. |
| **LanceDB, Chroma, Qdrant, or graph store alone** | Each can own storage/search, but none owns R3, semantic curation authority, hot budgeting, or R7. LanceDB/Chroma add substantial dependencies and edition/mode differences; Qdrant's default same-point replica semantics require explicit consistency choices; a graph store adds operation without proving enough policy retirement. |

## Normalized R1-R7 gate

| Option | R1 role scope | R2 shared contribution | R3 automatic availability | R4 bounded curation | R5 hot + searchable | R6 portability | R7 usefulness telemetry |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **O1 current unchanged** | **Pass A** | **Partial B** | **Partial A** | **Partial A** | **Partial A** | **Pass A** | **Partial A** |
| **O2 incremental** | **Partial A/D** | **Partial B/D** | **Unknown A/D** | **Unknown A/D** | **Unknown A/D** | **Partial A/D** | **Partial A/D** |
| **O3 Mem0 Platform** | **Partial B** | **Partial B** | **Partial B** | **Partial B** | **Partial B** | **Pass B** for access boundary | **Partial B** |
| **O4 hybrid** | **Partial A/B/D** | **Partial B/D** | **Partial A/D** | **Partial A/B/D** | **Partial A/B/D** | **Partial A/B/D** | **Partial A/B/D** |

The gate blocks every option as a standalone recommendation at this evidence stage. O2 and O4 are designs rather than shipped systems; projected behavior is not converted into a pass. O3's R6 pass covers client-neutral access only and does not transfer to automatic Claude/Codex lifecycle delivery.

## V1-V10 scenario results

Each retained-adapter entry names code or operation that remains agent-teams-owned even if the scenario passes.

### O1 - Current agent-teams unchanged

| Scenario | Result / grade | Source or observed probe | Retained adapter or unresolved behavior |
| --- | --- | --- | --- |
| **V1 role isolation** | **Pass A** | Source tests cover `learnings` and `recall` role prefixes. Live `memories-json` returned eight role namespaces; tester reads were framed for tester only. | Role parsing, tier-key convention, and deny-by-prefix filtering remain custom. |
| **V2 repeated/concurrent writers** | **Partial B** | Dolt sync/retry is source-tested, but no disposable cross-machine same-key write was run and no memory-level revision/conflict object exists. | Dolt operation, sync retry, same-key policy, and reconciliation. |
| **V3 automatic hot delivery** | **Partial A** | Live tester read had matching first/last framing: 33 entries, 27,978 chars. Claude/Codex definitions self-fetch, but Codex has no memory-specific compact reload and most Claude role subagents lack recovery. | Separate Claude and Codex startup/recovery clauses plus truncation handling. |
| **V4 cold retrieval quality** | **Fail A** for the representative query; full corpus unrun | `ateam recall tester 'targeted own test files only'` reported **64 matches**, the entire tester namespace, because any one common substring token qualifies. Exact-key lookup still works. | Custom lexical ranker, threshold/abstention, corpus, metrics, and token cap. |
| **V5 duplicate/contradiction lifecycle** | **Partial A/B** | Lock, packet ordering, caps, and history exist. Live `condense-check` reported all seven roles over the hot target while every verdict was `SKIP`; budget is not enforced. Generic SQLite probe detected duplicate and conflict but current curation does not store those objects. | Prompt policy, advisory lock, non-transactional drain, review, provenance, and restore. |
| **V6 applied/outcome telemetry** | **Fail A** | Source records only a non-atomic count/time RMW. It has no exposure, version, context, run, harness, or outcome and can lose concurrent increments. | Capture points, append-only events, idempotency, outcome join, and bias control. |
| **V7 neutral boundary** | **Pass A** | `ateam` is the same process boundary for Claude, Codex, and generic shell clients; the baseline track observed matching CLI summaries. | Runtime-specific invocation and artifact distribution remain custom. |
| **V8 export/restore** | **Fail A/B** | `memories-json` is a read model, not lossless export/import. No current command round-trips history, conflicts, tombstones, or events, and no restore drill was reproduced. | Versioned schema, importer, manifest/checksums, quarantine, and Dolt restore procedure. |
| **V9 migration/rollback** | **Partial B** | Ten migration invariants and an old-read-path rollback rule are documented, but no backfill, dual-run, or cutover was executed. | Migration controller, mismatch ledger, idempotency, cutover gate, reverse replay. |
| **V10 failure/offline** | **Partial B** | Local reads work without a hosted service. Stale Codex sync, network partition, lock contention, and malformed/truncated delivery were not exercised end to end; empty and failed reads are not uniformly distinct. | Store freshness, degraded-state envelope, local recovery, alerting, and restore drill. |

### O2 - Bounded incremental agent-teams

| Scenario | Result / grade | Source or reproducible validation | Retained adapter or unresolved behavior |
| --- | --- | --- | --- |
| **V1 role isolation** | **Partial A/D** | Baseline prefixes pass, but the proposed canonical export/context/event paths do not exist and were not role-isolation tested. | Role authorization and generated contract tests. |
| **V2 repeated/concurrent writers** | **Partial B/D** | Append-only events and stale-revision curation would close two races, but ordinary Dolt memory upserts and cross-machine conflicts remain unchanged. | Dolt sync, expected revisions for all writes, and reconciliation. |
| **V3 automatic hot delivery** | **Unknown A/D** | Receipt-bearing `memory-context` and Codex recovery are design text only. | Claude/Codex adapters, receipts, payload hashes, lifecycle conformance fixtures. |
| **V4 cold retrieval quality** | **Unknown A/D** | Whole-token/phrase scoring, stop words, caps, and the distractor corpus are unimplemented. The current control failed the representative query. | Ranker, frozen corpus, score calibration, latency/token metrics. |
| **V5 duplicate/contradiction lifecycle** | **Unknown A/D** | Proposal/apply, source revision, structured decisions, enforced budget, and reviewed destruction are unimplemented. | Curation policy/model, transaction/lock, reviewer queue, audit and recovery. |
| **V6 applied/outcome telemetry** | **Partial A/D** | Event schema would fix count races and add exposure/context, but trustworthy application capture and independent outcome adapters remain unresolved. | Event store/spool, harness receipts, outcome owners, aggregate and bias controls. |
| **V7 neutral boundary** | **Partial A/D** | The current CLI is neutral; version negotiation and new context/export/import operations are not built. | Versioned CLI/API and per-harness thin adapters. |
| **V8 export/restore** | **Unknown D** | Lossless export, dry-run import, hashes, malformed-record quarantine, and restore verification are proposal only. | Export/import engine, manifests, compatibility and reverse transforms. |
| **V9 migration/rollback** | **Partial D** | The design gives a reproducible sequence, but no fixture has passed backfill, dual-write/read, cutover, or rollback. | Migration ledger, checkpoints, rollback replay, old-client fencing. |
| **V10 failure/offline** | **Partial B/D** | Retaining Dolt preserves local operation. Receipt/error states improve design coverage, but no outage, stale revision, event-spool, or unavailable-model probe exists. | Degraded-state contract, spool/replay, cache age, model-free fallback. |

### O3 - Mem0 Platform

| Scenario | Result / grade | Source or hosted gap | Retained adapter or unresolved behavior |
| --- | --- | --- | --- |
| **V1 role isolation** | **Partial B** | Platform entity scopes and filters are documented, but current docs differ on entity combination and no credentialed isolation probe was run. The OSS probe cannot validate hosted semantics. | Mandatory role/principal mapping, server-side filter enforcement, authorization tests. |
| **V2 repeated/concurrent writers** | **Unknown B** | Managed multi-client and asynchronous APIs exist, but same-memory contradictory-write ordering, serialization, and lost-update behavior are undocumented and unprobed. | Idempotency, expected revisions if available, reconciliation ledger, conflict quarantine. |
| **V3 automatic hot delivery** | **Partial B** | CLI/MCP/SDK/REST expose memory, but none proves startup, resume, compaction, and subagent delivery in both harnesses. | Agent-teams context selector and Claude/Codex lifecycle adapters with receipts. |
| **V4 cold retrieval quality** | **Partial B** | Filters, `top_k`, thresholds, reranking, recency, and graph search are documented. No repository-shaped corpus, latency, token, or abstention run was possible. | Evaluation corpus, token-budget post-selection, thresholds, model/index pins. |
| **V5 duplicate/contradiction lifecycle** | **Partial B** | History, CRUD, export, decay, and dedupe mechanisms exist, but generic and v3 algorithm docs disagree and no bounded/reversible conflict workflow was run. | Raw-source ledger, pinned algorithm/model, reviewable promotion/demotion and rollback. |
| **V6 applied/outcome telemetry** | **Partial B** | Feedback and webhooks exist, but they do not form automatic exposure -> application -> independent outcome -> retention lineage. | Project event schema, injector/retrieval receipts, outcome join, bias controls. |
| **V7 neutral boundary** | **Partial B** | REST, SDKs, CLI, and MCP pass the access-boundary requirement. The same versioned agent-teams schema was not exercised in Claude, Codex, and a generic fixture. | Compatibility facade and lifecycle adapters; hosted and OSS path normalization. |
| **V8 export/restore** | **Unknown B** | Filterable export jobs are documented. Full-fidelity re-import of IDs, role/tier, history, feedback, graph, conflicts, tombstones, and timestamps is not documented or run. | Neutral export manifest, importer, completeness hashes, deletion and restore verification. |
| **V9 migration/rollback** | **Partial B** | Backfill/shadow/dual-read controls are known, but no account-mutating pilot occurred. API-version and extraction drift create replay risk. | Migration controller, raw-event retention, mismatch ledger, old backend fallback. |
| **V10 failure/offline** | **Unknown B** | Hosted availability and quotas are documented surfaces, but outage, account loss, rate limit, and offline behavior were not exercised. Mem0 Platform has no demonstrated local read path. | Bounded encrypted hot cache, stale/error marker, validated exports, credential rotation and fallback. |

### O4 - Neutral agent-teams hybrid

| Scenario | Result / grade | Source or observed probe | Retained adapter or unresolved behavior |
| --- | --- | --- | --- |
| **V1 role isolation** | **Partial A/B/D** | LangGraph checkpoint `4.2.0` in-memory probe returned only `('roles','implementer','hot')` and `reviewer_leak=false`. PostgreSQL RLS/partition behavior is documented, but the integrated server-side policy is unbuilt. | Role/principal derivation, RLS/schema policy, deny-unscoped facade tests. |
| **V2 repeated/concurrent writers** | **Partial B/D** | PostgreSQL MVCC, transactions, locks, and uniqueness are grade B. No PostgreSQL/pgvector service or concurrent same-ID probe ran; LangGraph `BaseStore` has no documented compare-and-set contract. | Expected revisions, transaction boundaries, idempotency, retry and conflict ledger. |
| **V3 automatic hot delivery** | **Partial A/D** | Existing agent-teams lifecycle adapters are observed, but the hybrid `hot(role,budget)` response, receipts, timeout, and compact/subagent paths are unbuilt. | Versioned facade and separate thin Claude/Codex lifecycle adapters. |
| **V4 cold retrieval quality** | **Partial A/B/D** | SQLite FTS5/sqlite-vec 0.1.9 representative probe proved role/tier-filtered lexical/vector mechanics and clean backup; it is not the selected PostgreSQL backend and no 12-record metric run exists. | Hybrid retrieval/fusion/rerank policy, corpus, token cap, abstention, model/index pins. |
| **V5 duplicate/contradiction lifecycle** | **Partial A/B/D** | SQLite 3.51.0 deterministic probe reproduced `DUP|m1|m2` and role-scoped `CONFLICT|...|2,4`. LangMem curation and reversible PostgreSQL changesets were not run. | Canonicalization, provenance, model proposal, human gate, transactional activation, rollback. |
| **V6 applied/outcome telemetry** | **Partial A/B/D** | OTel GenAI memory fields were source-inspected and the project event contract is specified, but no adapter emitted events or proved 100 concurrent idempotent applications. | `agent_teams.memory.*` schema, outbox/spool, receipts, independent outcomes and bias analysis. |
| **V7 neutral boundary** | **Partial A/B/D** | `ateam` supplies an observed neutral process boundary; LangGraph is in-process and the new HTTP/OpenAPI/MCP conformance facade is unbuilt. | Canonical CLI/API schema, protocol pins, harness conformance and compatibility negotiation. |
| **V8 export/restore** | **Partial A/B/D** | PostgreSQL `pg_dump`/logical mechanisms and Arrow/Parquet are documented. The SQLite representative backup restored three rows, but no hybrid export preserves versions, provenance, conflicts, events, and index/model manifests. | Neutral logical export/import plus physical backup, checksums and clean-target restore test. |
| **V9 migration/rollback** | **Partial B/D** | Export -> dual-write -> shadow-read -> cutover -> replay rollback is specified by the source tracks, not executed. | Migration controller, source-event retention, per-role hashes, mismatch and reverse-replay ledgers. |
| **V10 failure/offline** | **Partial B/D** | PostgreSQL is a service dependency. A local signed event spool and bounded hot cache are specified but unbuilt; model-free lexical fallback and stale/error behavior were not run. | Cache/spool, degraded-state envelope, service/model timeouts, restore and failback operation. |

## Reproduced probes and observed evidence

### Current read surfaces

All commands below were read only. They returned and exited; no worker or server remained.

```bash
ateam learnings tester | awk 'NR==1{first=$0} {last=$0; lines++} END{...}'
ateam memories-json | jq '{total:length, byRole:..., byTier:..., appliedPositive:...}'
ateam condense-check --json | jq '{roles:length, verdicts:..., overTarget:...}'
ateam recall tester 'targeted own test files only' | sed -n '1p'
```

Observed:

- Tester context first and last framing matched: **33 entries, 27,978 chars, 23 hot, 10 fresh**, 215 output lines.
- The live read model had **1,842** memories: 1,611 cold, 83 fresh, 148 hot; 285 had positive applied counts. No body was retained in probe output.
- `condense-check` returned seven `SKIP` verdicts and all seven curation roles above `hot_budget_tokens`. The snapshot is mutable; this is evidence for the validation time only.
- The representative tester recall query returned **64 matches**, proving the common-token overmatching path. The command intentionally reported only its header, not private memory bodies.

### Mem0 OSS `2.0.19` control for the Platform assessment

The exact probe copied from `turnkey-platforms.md` failed in a clean isolated environment:

```text
MemoryConfig: Unsupported embedding provider: mock
```

The wheel contains `mem0.embeddings.mock.MockEmbeddings`, but `EmbedderConfig` and `EmbedderFactory.provider_to_class` do not list `mock`. After a runtime-only provider registration and post-validation substitution, the documented `memory.search(..., agent_id=role)` then failed:

```text
Top-level entity parameters {'agent_id'} are not supported in search().
Use filters={'user_id': '...'} instead.
```

The temporary synthetic probe finally passed with `filters={'agent_id': role}`:

```json
{"history_events":["ADD","UPDATE"],"package":"mem0ai==2.0.19","role_isolation":{"implementer":1,"tester":1},"semantic_quality_tested":false}
```

The successful run used a fake non-secret API value, `infer=False`, a temporary Qdrant path, and a temporary history database. It validates patched OSS CRUD/filter/history mechanics only. It does not validate a supported mock configuration, semantic quality, concurrency, hosted Platform behavior, or API compatibility. The mismatch is tracked in `agent-teams-ird0.11`.

### LangGraph namespace control

```bash
uv run --isolated --python 3.12 --with langgraph-checkpoint==4.2.0 python -
```

Two same-key records were inserted under implementer and reviewer namespaces. Searching the implementer prefix produced:

```json
{"implementer_keys":["prefer-rg"],"implementer_namespaces":[["roles","implementer","hot"]],"reviewer_leak":false}
{"after_delete":null}
```

This is A-grade interface/namespace evidence only. `InMemoryStore` does not validate persistent PostgreSQL concurrency, export, or recovery.

### Filtered retrieval and backup control

```bash
uv run --isolated --python 3.12 --with sqlite-vec==0.1.9 python -
```

The Python-managed temporary database used FTS5 plus a `vec0` role partition and tier metadata. It returned only implementer row 1 for both lexical and vector paths and restored all three rows through SQLite online backup:

```json
{"lexical":[[1,"implementer"]],"restored_rows":3,"semantic":[[1,"implementer","hot",0.0]],"sqlite":"3.50.4","sqlite_vec":"v0.1.9"}
```

This validates mechanics representative of the hybrid contract, not PostgreSQL/pgvector behavior or retrieval quality.

### Deterministic duplicate/conflict control

The source track's `sqlite3 :memory:` probe was rerun with SQLite 3.51.0 and exited:

```text
DUP|m1|m2
CONFLICT|implementer|test_workers|equals|2,4
```

The reviewer record did not leak into either implementer group. Detection is reproducible; choosing the authoritative value remains policy/human work.

## Migration-risk register

| Risk area | O1/O2 exposure | O3 Mem0 Platform exposure | O4 hybrid exposure | Required control before any pilot or cutover |
| --- | --- | --- | --- | --- |
| **Schema transforms** | Tier lives in keys; applied data is a sibling counter; no stable version/provenance schema. | Platform entity, memory, graph, feedback, and history objects may not map one-to-one. | Relational, vector, curation, and telemetry schemas are separately versioned. | Versioned neutral envelope; immutable memory/version IDs; explicit tier, role, timestamps, provenance, tombstone, conflicts, event IDs; quarantine malformed rows. |
| **Identity and namespace mapping** | Bare slug is stable only by convention and role is string-prefix authority. | `user_id`/`agent_id`/`run_id`/`app_id` semantics and combinations are ambiguous across docs. | Namespace tuple, SQL role/RLS, CLI principal, and telemetry identity can diverge. | One mapping table; role in every identity and predicate; reject unscoped operations; golden cross-role tests over reads, search, curation, export, and events. |
| **Sync and concurrency** | Dolt sync has retry but same-key convergence and count RMW loss are unresolved. | Contradictory-write ordering and convergence are unknown. | PostgreSQL is transactional, but facade/LangGraph revisions and model jobs need one conflict contract. | Expected revisions or append-only commands; idempotency; transactional outbox; per-conflict quarantine; concurrent same-ID, same-role, and cross-machine probes. |
| **Import/export fidelity** | `memories-json` omits history and has no importer. | Export jobs exist; full supported re-import is unknown. | `pg_dump` and logical files do not automatically preserve model/index settings or application semantics. | Fixture with Unicode, large bodies, every tier, histories, deletes, conflicts, events, malformed rows; counts, per-role hashes, lineage, model/index manifest, clean-target restore. |
| **Dual-run** | Existing read path is a viable control, but no mismatch ledger exists. | Extraction may produce different facts during backfill and dual-write. | Store, curation, index, and event writes can partially succeed across boundaries. | Backfill immutable source records first; shadow reads before read cutover; stable idempotency keys; operation/result ledger; never silently union divergent results. |
| **Cutover** | Old clients can bypass new invariants. | Quota, latency, account, or API-version behavior can change at cutover. | Service/schema/index migrations and lifecycle adapters must move together. | Freeze schema/version, drain reconciliation lag, compare counts/hashes/context payloads/known queries, fence incompatible clients, staged per-role cutover with abort thresholds. |
| **Rollback** | Dolt history exists but no tested memory restore command. | Target export may not be source-importable; post-cutover writes can be stranded. | Reverse transform can lose graph/index/derived state and event ordering. | Keep old reads and source writes through rollback window; append neutral write journal; prove reverse import; preserve raw sources and target exports; rehearse failback. |
| **Data loss and semantic drift** | Fresh drain is non-transactional; curation rewrites can drop nuance. | Model extraction/version drift and incomplete graph/history export can lose meaning. | LangMem/model consolidation can change meaning; outbox gaps can lose events. | Immutable raw evidence, derived revisions, source-span coverage, stale-revision rejection, review for ambiguous/destructive changes, partial-batch detection and recovery. |
| **Privacy and deletion** | Local store reduces processors, but operational memories and telemetry are sensitive. | Hosted service/model processors, retention, backups, derivatives, and deletion completion need contract review. | PostgreSQL, model providers, OTLP backend, caches, and spools expand processors and copies. | IDs-only telemetry by default; no raw query/body traces; RBAC/RLS, encryption, processor map, retention classes, legal holds, deletion propagation and verification across indexes/backups. |
| **Dependency footprint** | Custom Go/Beads/Dolt, two plugins, binaries, hooks, dashboard. | Vendor API/SDK/MCP plus service availability and API-version dependency. | PostgreSQL, pgvector, Python/LangGraph/LangMem/provider SDKs, model service, OTel collector/backend, migrations and monitoring. | Pin every release/API/model; software bill of materials; upgrade contract tests; supported matrix; failure budget; remove components that do not retire measurable owned work. |
| **Operational ownership** | Agent-teams owns all eight current layers. | Vendor owns service operation; agent-teams still owns policy, lifecycle, token budget, telemetry meaning, migration, cache, and incident fallback. | Agent-teams owns the service, schema, indexes, Python worker, telemetry, model/evaluation, adapters, backup and on-call. | Named owner/runbook per layer; SLOs and alerts; restore drills; capacity/cost accounting; clear vendor support boundary; maintenance inventory compared after pilot. |
| **Failure/offline behavior** | Local reads survive internet loss but can be stale; synchronization and recovery failures are weakly surfaced. | Hosted outage/account/rate-limit failure can remove source reads. | Database, model, or telemetry service can fail independently. | Distinguish empty/stale/partial/error; bounded encrypted hot cache; model-free lexical path; durable event spool; retry/backpressure limits; dropped-event counters; tested read-only fallback. |

## Candidate-specific adapters and unresolved unknowns

| Option | Custom adapters retained | Unresolved unknowns that block promotion |
| --- | --- | --- |
| **O1 current unchanged** | Dolt/Beads storage and sync; key/tier schema; Go verbs; lexical retrieval; curation skill/lock/drain; Claude/Codex delivery; dashboard; tests; binaries and release parity. | Same-key convergence, launch/delivery success rate, full restore, representative retrieval metrics, applied-loss rate, cross-harness recovery, contention and stale/offline behavior. |
| **O2 incremental** | All O1 layers plus canonical schemas, context receipts, event aggregation, curation proposal/apply, export/import, corpus/evaluator, generated fixtures and provenance checks. | Whether the six changes reduce rather than increase recurring maintenance; trustworthy application capture; Dolt conflict closure; event/privacy volume; review-queue operation; backward-client fencing. |
| **O3 Mem0 Platform** | Role/principal authorization; `ateam` facade; token-budget hot selector; lifecycle adapters; stable provenance/event schema; outcome capture; curation authority; migration/reconciliation; outage cache; dashboard normalization. | Hosted entity-combination semantics, concurrent contradictory writes, algorithm/API version, full export/re-import, graph/history/feedback fidelity, deletion completion, measured corpus quality/cost, outage and quota behavior, contract/privacy terms. |
| **O4 hybrid** | Role/tier policy and RLS; versioned CLI/API/MCP facade; hot selection; Claude/Codex lifecycle adapters; provenance/conflict policy; LangMem prompt/model review; event semantics/outcomes; migrations, index/model manifests, dashboard and operations. | PostgreSQL/pgvector concurrent workload and latency, LangGraph CAS semantics, LangMem quality and JS parity, integrated transaction/outbox boundary, embedding/reranker choice, exact retained code/operations, service/model/offline behavior, end-to-end export and rollback. |

## Explicit live gaps

- **Mem0 Platform:** no account credential or paid/account-mutating probe was authorized. No role-isolation, concurrency, export/import, deletion, quota, latency, webhook, feedback, or outage call ran. OSS behavior is not transferred to Platform.
- **Zep Cloud, Letta Cloud, Supermemory hosted, Cognee Cloud, Qdrant Cloud, Chroma Cloud, LanceDB Enterprise, Neo4j Aura, and managed PostgreSQL:** no credentials or paid calls were used. Their edition-specific claims remain B or unknown.
- **Graphiti and current Letta local App Server:** no graph database/model provider or host-capable long-running agent service was started. Their proposed probes remain unrun.
- **PostgreSQL/pgvector hybrid:** no repository-provided running instance was handed to the tester, and tester instructions prohibit starting a server. MVCC/export evidence is grade B until a DRI-owned disposable instance is available for V2/V8/V10.
- **LangMem curation and embeddings/rerankers:** no provider/model credential was used, so semantic duplicate, contradiction, consolidation, reranking, latency, token, and price behavior remain unknown.
- **Claude/Codex lifecycle E2E:** the existing CLI framing was observed, but no DRI-provided isolated harness run exercised startup, resume, compaction, subagent delivery, truncation, or backend outage for a changed candidate.

These gaps are blockers, not negative scores. The next decision artifact may compare maintenance and other non-gating dimensions, but it must not convert any unknown above into zero or a pass.
