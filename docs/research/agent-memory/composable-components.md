# Composable memory components and protocol surfaces

Research date and access date: **2026-08-26**
Repository baseline from the approved plan: `e028082e9f8de49342dc6d5b9106af2b7c4b44da`
Scope: storage, retrieval, graph, metadata, synchronization, embedding, reranking, and neutral interface components only. This artifact does not select a winner.

## Method and evidence grades

This track treats a component as a replaceable layer, not as an agent-memory system. It asks which maintenance seam the component can own and which agent-teams behavior must remain above it. All cited sources are current primary sources: official specifications, product documentation, source repositories, release records, and observed disposable probes.

- **A, reproduced:** behavior reproduced from a pinned release in a disposable environment and paired with official documentation or source.
- **B, primary documented:** explicit current official documentation, source, release notes, or specification, but not reproduced here.
- **C, directional:** maintainer issue, discussion, or roadmap. It identifies risk or a lead but does not pass a requirement.
- **D, discovery only:** marketing, secondary, or community material. No D-grade claim supports this artifact.

`Unknown` means that current primary evidence was not sufficient or the behavior was not exercised. It does not mean absent.

## Executive findings without a selection

1. Storage and retrieval components can materially own indexing, filtered retrieval, transactions, and backup mechanics. None owns role semantics, hot-context budgeting, lifecycle injection, conflict policy, or usefulness attribution end to end.
2. The lightest reproduced local path was SQLite FTS5 plus `sqlite-vec` 0.1.9: one added package, one database file, relational metadata, lexical search, filtered exact vector search, and SQLite-native backup. Its write-concurrency and brute-force vector-search limits remain architectural constraints.
3. LanceDB 0.37.1 and Chroma 1.5.9 both reproduced metadata-filtered local vector search and logical export/restore. Their isolated Python environments were about 310 MiB and 315 MiB respectively, versus 260 KiB for the `sqlite-vec` environment. These figures measure the disposable Python environments, not process RSS or production images.
4. PostgreSQL 18 plus pgvector 0.8.6 and Qdrant 1.19.0 expose stronger multi-process/service behavior than embedded stores, with corresponding service operation, backup, security, monitoring, and upgrade obligations. Qdrant's default distributed point-write semantics require deliberate consistency settings for concurrent updates to the same memory.
5. Graph stores and provenance schemas can represent derivation, contradiction, revision, and responsibility. They do not decide whether two memories conflict, whether a merge is semantically valid, or which fact should enter the hot set.
6. A neutral boundary can improve cross-harness reliability only when one canonical contract is exercised independently of each harness. MCP is useful as an adapter and discovery surface, but MCP alone does not guarantee automatic startup delivery, tool invocation, retry, or identical host behavior.

## Category landscape and exclusions

| Category | Credible current exemplars | Deep representative(s) | Layer it can own | Exclusions and boundary |
|---|---|---|---|---|
| Neutral CLI/API/MCP | MCP specification `2026-07-28`; OpenAPI 3.2.0; official `@modelcontextprotocol/server-memory` 2026.7.4 | MCP and OpenAPI contract analysis | Typed calls, discovery, transport, generic client access | Memory policy and lifecycle injection are not protocol behavior. Feature-rich memory servers are bundled platforms and are not profiled here. |
| Embedded local full-text/vector | SQLite 3.50.4 FTS5 plus `sqlite-vec` 0.1.9; LanceDB 0.37.1; Chroma 1.5.9 local | All three received disposable probes | Local persistence, metadata filters, lexical/vector retrieval, some versioning/export | No component receives credit for role ontology, curation, hot selection, or telemetry interpretation. |
| Hosted/self-hosted vector search | PostgreSQL 18 plus pgvector 0.8.6; Qdrant 1.19.0; Chroma 1.5.9 server/distributed | pgvector/PostgreSQL and Qdrant | Concurrent service access, filtered vector retrieval, service backup/replication | Hosted and self-hosted behavior remain separate. No paid or credentialed probe was run. |
| Graph stores | Neo4j 2026.07.1; Apache AGE 1.7.0 for PostgreSQL 18 | Neo4j data/operation model; AGE as relational co-location alternative | Nodes, relationships, traversal, transactional graph persistence | Graph storage is not contradiction detection or curation. Neo4j edition differences are material. |
| Metadata/provenance models | W3C PROV-O Recommendation; OpenTelemetry specification 1.60.0 and semantic conventions 1.44.0 | W3C PROV-O mapping | Stable identities, derivation/revision/invalidation, actors, retrieval events | These are schemas/signals, not stores or retention policies. GenAI semantic conventions do not define memory application success. |
| Synchronization/export | SQLite Online Backup API; Litestream 0.5.16; PostgreSQL 18 `pg_dump` and logical replication; Arrow 25.0.1/Parquet | SQLite backup, PostgreSQL mechanisms, Arrow/Parquet interchange | Snapshots, continuous replica streams, logical export, portable tabular interchange | Backup is not multi-writer merge. Arrow/Parquet does not coordinate mutations. |
| Embeddings/rerankers | Sentence Transformers 6.0.0; Hugging Face Text Embeddings Inference 1.9.0 | Local library versus self-hosted inference API | Vector generation and second-stage relevance scoring | Model selection, evaluation corpus, thresholds, re-embedding, and failure policy remain custom. |

Explicit exclusions:

- Mem0, Zep/Graphiti, Letta, Supermemory, Cognee, and broad memory MCP products are handled as turnkey platforms elsewhere. They are not re-profiled.
- LangGraph/LangMem, LlamaIndex, Semantic Kernel, AutoGen, and harness lifecycle APIs are framework-native and outside this track.
- The official MCP memory reference server is screened only to test what the protocol boundary does and does not standardize. `doobidoo/mcp-memory-service` v11.8.2 and `danielsimonjr/memory-mcp` v12.8.2 were current and maintained at access time, but their consolidation, lifecycle, dashboard, and large tool surfaces make them bundled platforms rather than neutral components. Their product capabilities are therefore excluded from this component comparison.
- SQLite VSS, `sqlite-vss`, and Vectorlite were not deep-evaluated. The current `sqlite-vec` release has a direct maintained release and broad bindings, while adding another SQLite vector extension would not add a distinct layer-level architecture to this screen.
- Kuzu is excluded because its [upstream repository is archived](https://github.com/kuzudb/kuzu); an archived graph engine cannot be treated as a current maintained exemplar.

## R1-R7 contribution matrix

Legend: **C** directly contributes a primitive; **P** can participate but requires substantial custom semantics; **-** does not own the requirement.

| Component category | R1 role scope | R2 shared contribution | R3 automatic availability | R4 bounded curation | R5 hot + searchable | R6 portability | R7 applied/usefulness | Orchestration it cannot own |
|---|---:|---:|---:|---:|---:|---:|---:|---|
| SQLite FTS5 + vector extension | C | P | - | P | P | P | P | Role naming, cross-machine merge, injection hooks, token budget, semantic conflict decisions, outcome meaning |
| LanceDB / Chroma embedded | C | P | - | P | P | P | P | Same as SQLite; embedded process ownership and file distribution remain custom |
| PostgreSQL + pgvector | C | C | - | P | P | P | P | Harness lifecycle, curation policy, retrieval evaluation, hot-set assembly, telemetry interpretation |
| Qdrant / Chroma service | C | C | - | P | P | C | P | Role ontology, application-level transactions across events, hot delivery, contradiction policy |
| Graph store | C | C | - | P | P | P | P | Conflict detection, truth choice, ranking, token budgeting, automatic host delivery |
| PROV-O / OTel metadata | P | P | - | C | - | C | C | Persistence, concurrency, retrieval, retention decisions, automatic capture unless instrumented |
| Backup/replication/export | - | P | - | P | - | C | - | Active-active conflict merge, schema semantics, index rebuild policy, application cutover |
| Embedding/reranking runtime | - | P | - | P | C | C | Access control, persistence, corpus construction, thresholds, context assembly, outcome capture |
| Neutral CLI/API/MCP boundary | P | P | P | - | P | C | P | Store implementation, lifecycle policy, host invocation guarantees, semantic decisions |

### Requirement implications

- **R1:** Every screened store can represent `role` as a column, payload, partition key, label, or collection. Isolation passes only when every write, read, search, export, telemetry query, and destructive operation applies the role predicate. A metadata feature is necessary but does not prove complete role isolation.
- **R2:** Service databases have defined concurrent-client behavior. Embedded stores can serve repeated local processes, but file ownership, writer serialization, remote synchronization, and same-ID conflict handling remain deployment-specific. Backup replication is not an active-active merge protocol.
- **R3:** No store or retrieval engine knows when Claude Code or Codex starts, resumes, compacts, or spawns a subagent. A boundary can expose `hot(role, budget)` but harness adapters must call it and surface failure.
- **R4:** Transactions, version history, graph edges, and provenance make curation reversible and auditable. Duplicate detection, semantic contradiction judgment, promotion/demotion, and destructive eviction policy remain application behavior.
- **R5:** FTS/vector search and rerankers can own cold retrieval. The hot-set token budget, deterministic truncation, ordering, and injection are not database concerns.
- **R6:** HTTP/OpenAPI, MCP, SQL, and portable exports reduce coupling. Portability requires conformance tests from generic clients; the mere existence of an SDK does not prove equivalent behavior across harnesses.
- **R7:** Stores can persist application events, PROV-O can relate them, and OTel can transport them. Only agent-teams can define what `applied`, `exposed`, `accepted`, and `successful outcome` mean and prevent popularity feedback loops.

## Detailed component assessment

### 1. Embedded local full-text and vector stores

#### SQLite FTS5 plus `sqlite-vec` 0.1.9

Pin: `sqlite-vec` `v0.1.9`, commit `e9f598abfa0c06b328d8fe5da9c3760cce74be10`, released 2026-03-31. The probe used CPython 3.14.5, SQLite 3.50.4, and the PyPI wheel `sqlite-vec==0.1.9`.

- **Search and filtering:** FTS5 is an SQLite virtual table module with BM25 ranking and configurable tokenization. `sqlite-vec` `vec0` supports typed metadata columns, partition keys, auxiliary columns, and KNN constraints. The probe used `role TEXT PARTITION KEY` and `tier TEXT` in the KNN query, returning only the intended role/tier row. `sqlite-vec` 0.1.9 remains exact/brute-force by default; the 0.1.10 ANN work is pre-release and is not credited.
- **Local/offline:** Fully in-process and offline after installation. Data, FTS index, metadata, provenance tables, events, and vectors can share one SQLite file.
- **Concurrency:** SQLite WAL allows readers and a writer to proceed concurrently, but there is still one writer at a time. Same-row update semantics must be implemented with transactions, compare-and-swap columns, or application locks. A database file should not be opened over an unsupported network filesystem.
- **Export/backup:** The Online Backup API creates a consistent snapshot while allowing brief interleaved source access. `VACUUM INTO` and `sqlite3_rsync` are additional official mechanisms. Copying only the main database file while WAL is live is not the documented backup path.
- **License/maturity:** SQLite is public domain. `sqlite-vec` is dual MIT/Apache-2.0 and has a stable 0.1.x release, but its own 0.1.7 release notes acknowledge stale documentation after a maintenance hiatus. This is active but still a young extension, not SQLite-core maturity.
- **Dependency footprint:** The isolated environment added one package and measured 260 KiB across 25 files. The Python interpreter and standard library were outside the environment and are not included.
- **Migration risk:** SQL tables and FTS text are transparent. The `vec0` shadow-table representation and extension ABI require the same compatible extension on restore. A current open maintainer issue reports a cross-platform first-write corruption case after copying a 0.1.9 `vec0` database; that issue is grade C and needs reproduction before any cross-architecture migration claim. Logical row export is the conservative architecture-neutral path.
- **Cannot own:** Multi-machine write convergence, semantic deduplication, contradictions, hot token selection, harness hooks, or usefulness meaning.

Evidence: SQLite [FTS5](https://www.sqlite.org/fts5.html), [WAL](https://www.sqlite.org/wal.html), and [Online Backup API](https://www.sqlite.org/backup.html); `sqlite-vec` [0.1.9 release](https://github.com/asg017/sqlite-vec/releases/tag/v0.1.9), [Python loading](https://alexgarcia.xyz/sqlite-vec/python.html), [metadata/partition design](https://alexgarcia.xyz/blog/2024/sqlite-vec-metadata-release/), and [cross-platform issue #297](https://github.com/asg017/sqlite-vec/issues/297). Grades A for the probed write/filter/search/backup; B for documented WAL and metadata behavior; C for the unresolved migration report.

#### LanceDB 0.37.1

Pin: `v0.37.1`, annotated-tag commit `b89f87f206abd79a069833cee01f0c029ddb1397`, released 2026-08-10.

- **Search and filtering:** SQL/DataFusion predicates can pre-filter or post-filter vector search. The probe used pre-filtering on role and tier and returned only `m1`.
- **Local/offline versus service:** LanceDB OSS is an embedded database connected by local path or object-store URI. Enterprise exposes remote table and REST namespace surfaces. Remote and local types are not fully behavior-identical: remote Python tables do not expose table-wide `to_arrow()` or `to_pandas()`.
- **Concurrency:** The Lance table format provides ACID transactions, MVCC, schema evolution, and versions. LanceDB documents concurrent writers with bounded commit retries; excessive concurrent writers can fail and require caller retry. This is materially better than an unversioned vector file but does not define business conflict resolution.
- **Export/backup:** Local tables are Arrow-native and can materialize to Arrow/Parquet. Every mutation creates a dataset version until cleanup. Version history is recoverability, not an independent off-site backup. Object-store catalog/manifest consistency and retention need an operational design.
- **License/maturity:** Apache-2.0. The release is current and the table format is separately specified, but the product/API is changing quickly and local/enterprise feature differences create migration surface.
- **Dependency footprint:** The isolated Python environment resolved 16 packages and measured 310,768 KiB across 2,690 files. Large installed artifacts included `lancedb`, PyArrow 25.0.1, and NumPy 2.5.2.
- **Migration risk:** Arrow schemas help preserve structured metadata and vectors. Index definitions, distance metric, table versions, object-store catalog semantics, and remote-only features still require explicit export/rebuild plans. A Parquet export does not preserve every Lance index or historical version.
- **Cannot own:** Role policy, curation, hot selection, harness delivery, applied-event semantics, or retry policy for logical conflicts.

Evidence: LanceDB [quickstart](https://docs.lancedb.com/quickstart), [metadata filtering](https://docs.lancedb.com/search/filtering), [table operations/export caveat](https://docs.lancedb.com/tables), [concurrent-write FAQ](https://docs.lancedb.com/faq/faq-oss), and [Lance table format](https://lance.org/format/table/). Grade A for probed local filtered write/search/Parquet round trip; B for concurrency, MVCC, and remote behavior.

#### Chroma 1.5.9 local

Pin: tag/commit `1.5.9` / `11f3c7435e71024aa0a2b53710a28d3289d922d1`, released 2026-05-05.

- **Search and filtering:** Chroma supports structured `where` metadata filters and document filters. The probe supplied vectors directly, combined role and tier with `$and`, and returned only `m1`.
- **Local/offline versus service:** `PersistentClient` embeds a local store; the same product also has single-node, distributed, and cloud modes. Chroma's official OSS page states that distributed and local modes currently use different storage subsystems and may not have full feature/behavior parity. Treat each mode as a separate candidate surface.
- **Concurrency:** This probe did not exercise multi-process writers. Official architecture docs position local mode for prototyping/experimentation and single-node/server modes for shared workloads. Exact same-record concurrent-update guarantees for local mode remain unknown here.
- **Export/backup:** The probe performed a logical `get()` of IDs, documents, metadata, and embeddings to JSON, then restored all three rows. This proves a small logical round trip, not a canonical production backup. Current official docs located for this track did not define a complete local backup/export contract that preserves collection configuration and indexes, so production backup fidelity is unknown.
- **License/maturity:** Apache-2.0. Current 1.x releases, three deployment modes, and official Python/TypeScript/Rust surfaces indicate active maturity, but storage-subsystem divergence is an explicit migration risk.
- **Dependency footprint:** The isolated Python environment resolved 79 packages and measured 315,436 KiB across 7,010 files. It included ONNX Runtime, gRPC, Kubernetes, OpenTelemetry, tokenizers, and HTTP/server dependencies even though the probe supplied embeddings and used only local mode.
- **Migration risk:** Logical records are portable, but collection configuration, embedding function identity, HNSW/index state, and local/distributed storage differences need explicit capture and rebuild. A client-level JSON loop can be slow and may not be snapshot-consistent under concurrent writes unless coordinated.
- **Cannot own:** Curation, hot-set assembly, lifecycle injection, outcome telemetry semantics, or cross-mode equivalence.

Evidence: Chroma [metadata filtering](https://docs.trychroma.com/docs/querying-collections/metadata-filtering), [architecture](https://docs.trychroma.com/reference/architecture/overview), [client/server mode](https://docs.trychroma.com/guides/deploy/client-server-mode), and [local/distributed parity statement](https://docs.trychroma.com/docs/overview/oss). Grade A for the probed local filtered write/search/logical round trip; B for documented deployment surfaces; unknown for production-consistent local backup and concurrent same-record writes.

### 2. Hosted and self-hosted vector search

#### PostgreSQL 18 plus pgvector 0.8.6

Pin: pgvector `v0.8.6`, commit `8ee86c96f0fd72390f890aa8a336fda6d3ab4c6c`, released 2026-07-29; PostgreSQL documentation version 18.

- **Metadata filtering:** Vectors live beside ordinary typed columns, constraints, JSON, full-text indexes, and joins. R1 can use a mandatory role column, row-level security, list partitioning, or separate tables. pgvector warns that approximate search filtering occurs after index scan; iterative scans added in 0.8.0 can scan farther, and highly selective tenants may need partitioning or separate tables.
- **Concurrency:** PostgreSQL MVCC, row/table locks, transaction isolation, advisory locks, and uniqueness constraints provide defined multi-client behavior. This is the strongest general transactional surface screened in this category, but an application must still choose isolation and retry serialization failures.
- **Export/backup/sync:** `pg_dump` creates consistent logical exports without blocking normal readers/writers; physical backup and point-in-time recovery are separate PostgreSQL facilities. Logical replication provides ordered changes per subscription, but DDL and sequence data are not automatically replicated. Multi-primary conflict policy is not supplied by pgvector.
- **Local/offline versus service:** It can run on one local machine without an external vendor or as a self-hosted/managed service. It is never an in-process dependency: server lifecycle, credentials, upgrades, vacuum, monitoring, and recovery remain operational work.
- **License/maturity:** pgvector uses the PostgreSQL License. PostgreSQL is mature infrastructure; pgvector 0.8.x is actively maintained and supports PostgreSQL 13 through 18. Extension and major-server compatibility must be pinned together.
- **Dependency footprint:** A PostgreSQL service plus extension, client driver, backup tooling, and monitoring. No local server probe was run because this worktree had no container runtime and installing a database service would exceed a disposable docs probe.
- **Migration risk:** Standard SQL and logical dumps are strong exit paths. Approximate indexes must be rebuilt; vector dimensions, types, operators, distance metric, tenant partitioning, RLS, and generated/search columns must be migrated deliberately.
- **Cannot own:** Embedding generation, semantic curation, hot budget, harness startup, or usefulness evaluation.

Evidence: pgvector [README and filtering guidance](https://github.com/pgvector/pgvector/tree/v0.8.6), [0.8.6 changelog](https://github.com/pgvector/pgvector/blob/v0.8.6/CHANGELOG.md); PostgreSQL 18 [MVCC](https://www.postgresql.org/docs/18/mvcc-intro.html), [`pg_dump`](https://www.postgresql.org/docs/18/app-pgdump.html), and [logical replication](https://www.postgresql.org/docs/18/logical-replication.html). Grade B; not reproduced here.

#### Qdrant 1.19.0

Pin: annotated tag `v1.19.0`, commit `74f3e85b9473c62560006c043e13737ce6b48412`, released 2026-08-05.

- **Metadata filtering:** Points carry JSON payloads. Boolean filter clauses, typed payload indexes, text indexes, nested fields, and filter-aware HNSW support role/tier/provenance predicates. Official guidance says payload indexes should be created before ingestion for filter-aware HNSW edges.
- **Concurrency:** Qdrant documents that default distributed concurrent updates to the same point can produce temporarily inconsistent replica states. `write_consistency_factor`, read consistency, and weak/medium/strong write ordering tune the trade-off. Strong ordering can reduce availability when the leader is unavailable. These settings must be explicit for R2.
- **Export/backup:** Collection snapshots include data, configuration, points, payloads, and indexes, but exclude aliases. Distributed snapshots are per node; snapshot restore compatibility is version-bounded. Qdrant's own docs state that snapshots cannot be created in Python SDK local mode, so local-client behavior is not a substitute for server backup validation.
- **Local/offline versus service:** The official local quickstart is a network service in Docker exposing REST and gRPC. Self-hosted distributed and Qdrant Cloud are distinct operational editions. Qdrant Edge is now documented as an embedded/offline product, but it was not released/probed deeply enough here to credit parity with server Qdrant.
- **License/maturity:** Apache-2.0, active 1.x releases, official clients, REST, gRPC, cloud, and distributed operation. Service hardening is required: the local quickstart warns that default startup has no encryption or authentication.
- **Dependency footprint:** One Rust service/container plus storage, clients, ports, snapshots, and cluster operations. No server probe was run because Docker was unavailable.
- **Migration risk:** Scroll/export can move logical points and payloads, while snapshots preserve indexes but have tighter version/deployment constraints. Collection aliases, consistency settings, payload-index schema, shard layout, quantization, and index configuration must be captured separately.
- **Cannot own:** Cross-collection transactions, role ontology, hot selection, harness injection, conflict truth, or application outcome semantics.

Evidence: Qdrant [payloads](https://qdrant.tech/documentation/concepts/payload/), [filtering](https://qdrant.tech/documentation/search/filtering/), [indexing](https://qdrant.tech/documentation/manage-data/indexing/), [consistency guarantees](https://qdrant.tech/documentation/scaling/consistency-guarantees/), [distributed deployment](https://qdrant.tech/documentation/scaling/distributed_deployment/), [snapshots](https://qdrant.tech/documentation/operations/snapshots/), and [local quickstart/security warning](https://qdrant.tech/documentation/quick-start/). Grade B; not reproduced here.

#### Chroma service modes

Chroma 1.5.9 single-node exposes an HTTP client/server mode and distributed Chroma uses a separate storage subsystem. This gives a common product API but not proven storage/behavior identity. Metadata filters are viable, while complete backup, concurrency, and migration evidence must be established separately for each deployment mode. The local grade-A probe cannot be promoted to a service-mode grade. License is Apache-2.0; operational dependency ranges from one Chroma process to its distributed/cloud services. Grade B for documented modes, unknown for round-trip backup fidelity and local-to-distributed parity beyond the official caveat.

### 3. Graph stores

#### Neo4j 2026.07.1

- **Model:** A transactional property graph can represent memories, roles, sessions, sources, applications, revisions, conflicts, and derivations as nodes/relationships. Cypher can traverse provenance and contradiction neighborhoods more directly than joins assembled ad hoc.
- **Concurrency:** Neo4j documents ACID transactions, read-committed default isolation, node/relationship write locks, explicit locks for stronger isolation, and deadlock detection. Non-repeatable reads and lost-update patterns still require transaction design.
- **Local/service:** Community and Enterprise self-managed editions exist, plus Aura. Enterprise adds clustering, failover, and online backup capabilities not present in Community. Edition-specific scoring is mandatory.
- **Export/backup:** Community supports offline backup/dump workflows; Enterprise supports online full and differential backup chains. Aura has different backup behavior. A logical Cypher/CSV export is more portable but may lose constraints, indexes, identities, and transaction history.
- **License/maturity/dependency:** Neo4j Community source is GPL-3.0; Enterprise/Aura use commercial terms. It is a mature dedicated JVM/service stack, significantly heavier than embedding graph edges in an existing relational store.
- **Migration risk:** Cypher is broadly recognizable but implementation features, procedures, index types, and edition operations create lock-in. Stable application IDs are needed so graph export does not substitute internal node IDs.
- **Cannot own:** Whether text is a duplicate or contradiction, which revision is authoritative, retrieval ranking, token budget, or lifecycle delivery.

Evidence: Neo4j [current operations manual and version](https://neo4j.com/docs/operations-manual/current/), [transactional behavior](https://neo4j.com/docs/operations-manual/current/database-internals/), [concurrent access](https://neo4j.com/docs/operations-manual/current/database-internals/concurrent-data-access/), [edition differences](https://neo4j.com/docs/operations-manual/current/introduction/), and [backup/restore](https://neo4j.com/docs/operations-manual/current/backup-restore/). Grade B.

#### Apache AGE 1.7.0 for PostgreSQL 18

Pin: `PG18/v1.7.0-rc0`, commit `806fa2ebdb300b3e76ef30cdba61803babbf2683`; official release notes identify 1.7.0 as released 2026-01-21 for PostgreSQL 18.

- **Model/operation:** AGE adds openCypher graph queries and `agtype` to PostgreSQL, allowing graph and vector/relational data to share PostgreSQL transactions, backup, and access controls.
- **Local/service and concurrency:** It inherits PostgreSQL service operation and ACID behavior. Driver/session setup, extension loading, PostgreSQL-version branches, and Cypher coverage are added dependencies.
- **Export/backup:** PostgreSQL mechanisms can capture extension tables, but restore requires compatible AGE binaries and extension setup. Logical graph exports provide a safer cross-engine exit at the cost of rebuilding indexes and schema.
- **License/maturity:** Apache-2.0. The project is active with current PG18 releases, but release artifacts are PostgreSQL-major-specific and release notes warn about upgrade-script gaps in some lines. This is less operationally mature than PostgreSQL core.
- **Migration risk:** PostgreSQL and AGE upgrades are coupled. Cypher compatibility should be tested against the actual query subset; do not assume Neo4j procedure or index parity.
- **Cannot own:** Semantic graph extraction, conflict decisions, curation, ranking, or lifecycle injection.

Evidence: Apache AGE [overview](https://age.apache.org/overview/), [downloads/version matrix](https://age.apache.org/download/), [release notes](https://age.apache.org/release-notes/), and [source](https://github.com/apache/age/tree/PG18/v1.7.0-rc0). Grade B.

### 4. Metadata and provenance models

#### W3C PROV-O

The 2013 W3C Recommendation is stable rather than recently revised. Its core `Entity`, `Activity`, and `Agent` classes and relations such as `wasGeneratedBy`, `used`, `wasDerivedFrom`, `wasRevisionOf`, `wasInvalidatedBy`, and `wasAttributedTo` map well to memory records, curation runs, role agents, source sessions, merges, revisions, and evictions.

A minimal application profile could map:

| Agent-memory concept | PROV-O representation |
|---|---|
| Immutable memory revision | `prov:Entity` with stable application ID and content hash |
| Agent/session that wrote it | `prov:Agent` and `prov:wasAttributedTo` |
| Extraction/curation run | `prov:Activity` with `prov:used` and `prov:generated` relations |
| Merge or rewrite | New entity `prov:wasRevisionOf` and/or `prov:wasDerivedFrom` old entities |
| Eviction/tombstone | `prov:wasInvalidatedBy` a curation activity; retain entity identity |
| Export bundle | `prov:Bundle` containing the provenance description |

PROV-O supplies vocabulary, not validation rules, physical schema, storage, query API, or curation policy. RDF is optional: the concepts can be projected into relational/JSON fields as long as export semantics remain explicit. Grade B from the [PROV-O Recommendation](https://www.w3.org/TR/prov-o/) and [PROV constraints](https://www.w3.org/TR/prov-constraints/).

#### OpenTelemetry 1.60.0 / semantic conventions 1.44.0

Pin: OpenTelemetry specification `v1.60.0`, commit `29ae8c7710d2ea52e21a5ff81fb1cd657bcd3306`; GenAI semantic conventions reported separately as 1.44.0 in current official docs.

OpenTelemetry can transport spans, metrics, logs, and events across CLI/API/MCP adapters. Current GenAI attributes include retrieval query text and retrieval documents with IDs and relevance scores, with explicit sensitive-data warnings. That is useful for R7 exposure and retrieval traces. It does not define a memory-applied event, successful outcome, retention decision, or causal attribution. Agent-teams would need a small namespaced semantic convention, for example separate events for `memory.exposed`, `memory.applied`, `memory.outcome`, and `memory.curated`, each carrying stable memory revision, role, session, harness, query, rank/score, and provenance IDs.

Telemetry is evidence only if capture is automatic at the boundary and write failure is observable. Agent self-report alone remains partial. Sampling, redaction, retention, and exporter outage behavior are part of the design. Grade B from the [OpenTelemetry specifications](https://opentelemetry.io/docs/specs/) and [GenAI attribute registry](https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/).

### 5. Synchronization and export

#### SQLite Online Backup plus Litestream 0.5.16

Pin: Litestream `v0.5.16`, commit `6d61ef5d007756d62e473daee4c760ac395a55c6`, released 2026-08-05, Apache-2.0.

- SQLite Online Backup produced the grade-A local snapshot in this track. It is a point-in-time backup, not cross-machine concurrent-write synchronization.
- Litestream continuously replicates SQLite changes to object/file storage and restores a database. Current 0.5.x also exposes one-shot sync, forced snapshots, retention, integrity-check restore, stable JSON CLI output, and status/metrics surfaces. This can reduce backup maintenance for a SQLite-backed component.
- Litestream does not make two independently writable SQLite databases converge. A restored replica is a recovery/cutover artifact. R2 still needs a single-writer topology, a service boundary, or an application merge protocol.
- Binary SQLite backup retains all local tables/indexes but couples restore to SQLite/extension compatibility. A periodic logical JSON/CSV/Parquet export is still needed for vendor-neutral migration and audit.

Evidence: Litestream [replicate command](https://litestream.io/reference/replicate/), [restore command](https://litestream.io/reference/restore/), [stable JSON output](https://litestream.io/reference/json-output/), and [0.5.16 release](https://github.com/benbjohnson/litestream/releases/tag/v0.5.16). Grade A for SQLite Online Backup; B for Litestream behavior.

#### PostgreSQL export and logical replication

- `pg_dump` is a consistent logical export while the database remains in use. Custom/directory archives support selective and parallel restore. It does not include cluster-global roles/tablespaces unless paired with `pg_dumpall`, and official docs caution that it is not by itself the routine production-backup answer for every deployment.
- Logical replication sends initial table snapshots followed by ordered row changes. PostgreSQL 18 documents row filters and cross-version/platform use, but DDL and sequences are not replicated automatically. Other writers on subscribers can create conflicts.
- Physical backup/PITR owns disaster recovery; logical dump owns portability; logical replication owns selected change propagation. Treating one as all three creates migration gaps.

Grade B from PostgreSQL 18 [`pg_dump`](https://www.postgresql.org/docs/18/app-pgdump.html), [logical replication](https://www.postgresql.org/docs/18/logical-replication.html), and [restrictions](https://www.postgresql.org/docs/18/logical-replication-restrictions.html).

#### Arrow 25.0.1 and Parquet interchange

Pin: Apache Arrow `apache-arrow-25.0.1`, annotated-tag commit `beccec0d0c451b7aa3e4530416ac431b3c035c69`, released 2026-08-10, Apache-2.0.

Arrow defines a language-independent typed columnar format and IPC. Parquet defines a durable columnar file with schema and file/page metadata. They are strong logical interchange formats for memories, metadata, provenance edges, application events, and vectors. They do not preserve a database's ANN graph, FTS index, transaction log, access control, trigger behavior, or conflict semantics. Export should therefore include a manifest with schema version, embedding model/revision, dimension, normalization, metric, content hash, event schema, row counts, and checksums, followed by index rebuild and validation after import.

Grade B from the [Arrow 25.0.1 columnar format](https://arrow.apache.org/docs/format/Columnar.html), [Parquet file format](https://parquet.apache.org/docs/file-format/), and [format-version compatibility notes](https://parquet.apache.org/docs/file-format/versions/).

### 6. Embeddings and rerankers

#### Sentence Transformers 6.0.0

Pin: `v6.0.0`, commit `56c9548fd8dbef8ba492f57ac04366900e36f5d4`, Apache-2.0, Python 3.10+.

Sentence Transformers supplies local bi-encoder embeddings, sparse encoders, cross-encoder rerankers, and multi-vector encoders. Its official retrieval pattern uses a fast first-stage retriever and a slower cross-encoder over the top candidates. It can run offline after model artifacts are cached, but model licenses are separate from the library license. Python, PyTorch/ONNX, model weights, CPU/GPU requirements, tokenization, and native dependencies make it much heavier than a lexical-only store.

Migration risk is high if the store treats vectors as timeless. Every record needs embedding model ID/revision, dimension, normalization, pooling/query-document mode, and creation timestamp. Changing any of those may require dual indexes and complete re-embedding. Cross-encoder scores are model-specific and should not be compared as stable absolute values across versions.

Evidence: [Sentence Transformers 6.0 documentation](https://www.sbert.net/), [usage](https://www.sbert.net/docs/sentence_transformer/usage/usage.html), and [retrieve/rerank pattern](https://www.sbert.net/examples/sentence_transformer/applications/retrieve_rerank/README.html). Grade B; model inference was not run.

#### Hugging Face Text Embeddings Inference 1.9.0

Pin: `v1.9.0`, commit `5699247f57e46aa09eb4f8c4cf74114099372fe7`, Apache-2.0.

TEI packages embedding, sequence-classification, and reranker models behind HTTP endpoints, including an OpenAI-compatible embeddings endpoint. It supports CPU and multiple GPU families and documents air-gapped deployment after weights are downloaded. This makes inference language-neutral and isolates model dependencies from each harness, at the cost of a service/container, model cache, accelerator compatibility, batching/latency operation, and endpoint failure handling.

TEI owns inference serving, not model quality, record metadata, backfill, retry semantics, or how reranked results become a token-bounded hot set. A neutral memory API should record model identity in every response and fail visibly if the configured model changes dimension.

Evidence: TEI [quick tour, embedding and rerank endpoints](https://huggingface.co/docs/text-embeddings-inference/quick_tour) and [supported models/hardware](https://huggingface.co/docs/text-embeddings-inference/en/supported_models). Grade B; no model image was pulled.

## Neutral CLI/API/MCP boundary assessment

### What the current standards provide

**MCP `2026-07-28`.** Pin: release/tag commit `5f5440bb26a62e2cf3440b92da5a667efa03b267`, released 2026-07-28. The current protocol uses self-contained stateless requests, `server/discover`, typed tool input/output schemas based on JSON Schema 2020-12, resources, and opt-in notification subscriptions. Standard transports include stdio and HTTP. Tier-1 TypeScript, Python, Go, and C# SDKs supported this revision at release. This is a credible cross-harness adapter surface.

**OpenAPI 3.2.0.** Published 2025-09-19. It is a language-neutral HTTP interface description with mature generic client/server tooling. It has no model-facing resource/tool semantics, but it is straightforward to test from `curl` and generate clients from.

**Generic CLI.** A CLI with JSON input/output and explicit exit codes is not a formal specification, but it remains the lowest-common-denominator local adapter. It is directly scriptable, can be called by hooks and humans, and makes failure visible without requiring a host to decide to invoke a model tool.

Evidence: MCP [2026-07-28 release](https://blog.modelcontextprotocol.io/posts/2026-07-28/), [transports](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports), [tools](https://modelcontextprotocol.io/specification/2026-07-28/server/tools), and [resources](https://modelcontextprotocol.io/specification/2026-07-28/server/resources); [OpenAPI 3.2.0](https://spec.openapis.org/oas/v3.2.0.html). Grade B.

### Memory-server surface evidence

The official `@modelcontextprotocol/server-memory` package was current at **2026.7.4**, npm `gitHead` `6dd0a683e198783e30feabf7abaf42f925bd18b1`. It exposes entity/relation/observation CRUD, graph search, full-graph read, and a `memory://knowledge-graph` resource backed by JSONL. This proves that memory CRUD and a readable memory resource fit MCP. It does not supply role isolation, vector ranking, multi-writer transactions, complete backup policy, hot-context budgeting, automatic invocation, or application telemetry. The repository is in an MIT-to-Apache-2.0 licensing transition, so exact-file license provenance matters.

Current maintainer issues request safer persistence defaults and note risks when the default data path lands in package/cache directories. That issue evidence is grade C and reinforces the need for explicit paths, quotas, atomic writes, and backup tests; it does not by itself prove corruption.

Evidence: official [memory server README](https://github.com/modelcontextprotocol/servers/tree/6dd0a683e198783e30feabf7abaf42f925bd18b1/src/memory), [npm registry metadata](https://registry.npmjs.org/@modelcontextprotocol%2fserver-memory/2026.7.4), [source at package commit](https://github.com/modelcontextprotocol/servers/tree/6dd0a683e198783e30feabf7abaf42f925bd18b1/src/memory), and [persistence hardening issue #4117](https://github.com/modelcontextprotocol/servers/issues/4117). Grade B for the source/package surface; C for unresolved operational issues.

### Reliability conclusion

A neutral boundary **contributes strongly to R6 but does not pass R3 by itself**. The reliable shape is a canonical application contract with three thin bindings:

```text
canonical memory service contract
  write/upsert(role, id, content, metadata, provenance, expected_revision)
  get/list(role, filters, cursor)
  search(role, query, filters, limit, model_revision)
  hot(role, token_budget, context)
  record_event(memory_revision, event_type, context, outcome)
  export/import(schema_version, cursor/manifest)
  health/capabilities
        |
        +-- CLI JSON adapter (hooks, scripts, generic fixture)
        +-- OpenAPI HTTP adapter (remote/shared service)
        +-- MCP tool/resource adapter (model-facing discovery and use)
```

Reliability conditions:

1. Define request, response, error, pagination, idempotency, revision, and schema-version semantics once. Generate or mechanically verify all adapters from that contract.
2. Keep role/tier/provenance predicates server-side. A harness must not be trusted to remember an isolation filter.
3. Require optimistic concurrency (`expected_revision`) or an idempotency key for writes and events. Define duplicate retries and same-ID conflicts.
4. Return model and index revision with search results. Detect dimension/metric mismatch before write.
5. Make `hot(role, token_budget)` deterministic and return actual token/byte counts, truncation, data revision, and stale/error state. Do not represent an outage as an empty successful hot set.
6. Exercise the same conformance corpus through CLI, raw HTTP/OpenAPI, MCP, Claude Code, and Codex. Compare normalized outputs, errors, role isolation, and retry behavior.
7. Keep automatic startup/resume/compaction/subagent calls in small harness adapters. MCP tools are generally model-controlled; they cannot guarantee that a host invokes memory before the first turn.
8. Treat MCP protocol revisions as pinned compatibility contracts. The 2026-07-28 release removed prior handshake/session assumptions, so adapter tests must cover exact supported revisions and downgrade behavior.

The boundary reduces duplicated storage/retrieval logic and provides a generic fixture. It does not eliminate the small, unavoidable lifecycle adapters for each harness.

## Reproducible disposable probes

All probes ran outside the repository under `/tmp` on 2026-08-26. They used synthetic vectors to test storage/filter semantics without downloading or crediting an embedding model. Commands below create new temporary directories and do not modify the current store.

### Probe A: SQLite FTS5 + `sqlite-vec` filtered search and backup

```bash
probe_dir=$(mktemp -d /tmp/agent-memory-sqlite-probe.XXXXXX)
uv venv "$probe_dir/venv" --python python3
uv pip install --python "$probe_dir/venv/bin/python" sqlite-vec==0.1.9
"$probe_dir/venv/bin/python" - "$probe_dir" <<'PY'
import json, pathlib, sqlite3, sys
import sqlite_vec
from sqlite_vec import serialize_float32

root = pathlib.Path(sys.argv[1])
db = sqlite3.connect(root / "memory.db")
db.enable_load_extension(True)
sqlite_vec.load(db)
db.enable_load_extension(False)
db.executescript("""
PRAGMA journal_mode=WAL;
CREATE TABLE memories(
  id INTEGER PRIMARY KEY, role TEXT NOT NULL, tier TEXT NOT NULL,
  content TEXT NOT NULL, source TEXT NOT NULL
);
CREATE VIRTUAL TABLE memories_fts
  USING fts5(content, content='memories', content_rowid='id');
CREATE VIRTUAL TABLE memories_vec USING vec0(
  role TEXT PARTITION KEY, tier TEXT, +content TEXT, embedding FLOAT[3]
);
""")
rows = [
 (1,"implementer","hot","Use targeted tests for changed files","session-a",[1.,0.,0.]),
 (2,"reviewer","hot","Inspect changed files for regression risk","session-b",[.95,.05,0.]),
 (3,"implementer","cold","Preserve unrelated dirty worktree files","session-c",[.8,.2,0.]),
]
for ident, role, tier, content, source, vector in rows:
    db.execute("INSERT INTO memories VALUES (?,?,?,?,?)",
               (ident, role, tier, content, source))
    db.execute("INSERT INTO memories_fts(rowid,content) VALUES (?,?)",
               (ident, content))
    db.execute("INSERT INTO memories_vec(rowid,role,tier,content,embedding) VALUES (?,?,?,?,?)",
               (ident, role, tier, content, serialize_float32(vector)))
db.commit()
lexical = db.execute("""
  SELECT m.id,m.role FROM memories_fts f JOIN memories m ON m.id=f.rowid
  WHERE memories_fts MATCH ? AND m.role=? ORDER BY bm25(memories_fts)
""", ("changed", "implementer")).fetchall()
semantic = db.execute("""
  SELECT rowid,role,tier,content,distance FROM memories_vec
  WHERE embedding MATCH ? AND role=? AND tier=? AND k=5 ORDER BY distance
""", (serialize_float32([1.,0.,0.]), "implementer", "hot")).fetchall()
backup = sqlite3.connect(root / "memory.backup.db")
db.backup(backup)
backup.close()
restored = sqlite3.connect(root / "memory.backup.db")
print(json.dumps({
  "sqlite": sqlite3.sqlite_version,
  "sqlite_vec": db.execute("select vec_version()").fetchone()[0],
  "lexical": lexical,
  "semantic": semantic,
  "restored_rows": restored.execute("select count(*) from memories").fetchone()[0],
}, indent=2))
PY
du -sk "$probe_dir/venv"
find "$probe_dir/venv" -type f | wc -l
```

Observed normalized result:

```json
{
  "sqlite": "3.50.4",
  "sqlite_vec": "v0.1.9",
  "lexical": [[1, "implementer"]],
  "semantic": [[1, "implementer", "hot", "Use targeted tests for changed files", 0.0]],
  "restored_rows": 3,
  "backup_bytes": 147456,
  "environment_kib": 260,
  "environment_files": 25
}
```

### Probe B: LanceDB filtered search and Parquet round trip

```bash
probe_dir=$(mktemp -d /tmp/agent-memory-lancedb-probe.XXXXXX)
uv venv "$probe_dir/venv" --python python3
uv pip install --python "$probe_dir/venv/bin/python" lancedb==0.37.1
"$probe_dir/venv/bin/python" - "$probe_dir" <<'PY'
import json, pathlib, sys
import lancedb
import pyarrow.parquet as pq

root = pathlib.Path(sys.argv[1])
db = lancedb.connect(root / "db")
rows = [
 {"id":"m1","role":"implementer","tier":"hot","content":"Use targeted tests for changed files","source":"session-a","vector":[1.,0.,0.]},
 {"id":"m2","role":"reviewer","tier":"hot","content":"Inspect changed files for regression risk","source":"session-b","vector":[.95,.05,0.]},
 {"id":"m3","role":"implementer","tier":"cold","content":"Preserve unrelated dirty worktree files","source":"session-c","vector":[.8,.2,0.]},
]
table = db.create_table("memories", data=rows)
result = table.search([1.,0.,0.]).where(
    "role = 'implementer' AND tier = 'hot'", prefilter=True
).limit(5).to_list()
export_path = root / "memories.parquet"
pq.write_table(table.to_arrow(), export_path)
restored_db = lancedb.connect(root / "restored")
restored = restored_db.create_table("memories", data=pq.read_table(export_path))
print(json.dumps({
  "lancedb": lancedb.__version__,
  "search_ids": [row["id"] for row in result],
  "export_bytes": export_path.stat().st_size,
  "restored_rows": restored.count_rows(),
}, indent=2))
PY
du -sk "$probe_dir/venv"
find "$probe_dir/venv" -type f | wc -l
```

Observed: `search_ids=["m1"]`, 2,096-byte Parquet export, three restored rows, 310,768 KiB and 2,690 files in the isolated environment.

### Probe C: Chroma local filtered search and logical round trip

```bash
probe_dir=$(mktemp -d /tmp/agent-memory-chroma-probe.XXXXXX)
uv venv "$probe_dir/venv" --python python3
uv pip install --python "$probe_dir/venv/bin/python" chromadb==1.5.9
"$probe_dir/venv/bin/python" - "$probe_dir" <<'PY'
import json, pathlib, sys
import chromadb

root = pathlib.Path(sys.argv[1])
client = chromadb.PersistentClient(path=str(root / "db"))
collection = client.create_collection("memories", embedding_function=None)
collection.add(
  ids=["m1","m2","m3"],
  documents=["Use targeted tests for changed files",
             "Inspect changed files for regression risk",
             "Preserve unrelated dirty worktree files"],
  metadatas=[{"role":"implementer","tier":"hot","source":"session-a"},
             {"role":"reviewer","tier":"hot","source":"session-b"},
             {"role":"implementer","tier":"cold","source":"session-c"}],
  embeddings=[[1.,0.,0.],[.95,.05,0.],[.8,.2,0.]],
)
result = collection.query(
  query_embeddings=[[1.,0.,0.]],
  where={"$and":[{"role":"implementer"},{"tier":"hot"}]},
  n_results=5, include=["documents","metadatas","distances"],
)
export = collection.get(include=["documents","metadatas","embeddings"])
payload = {"ids":export["ids"], "documents":export["documents"],
           "metadatas":export["metadatas"],
           "embeddings":export["embeddings"].tolist()}
export_path = root / "memories.json"
export_path.write_text(json.dumps(payload, sort_keys=True))
restored_client = chromadb.PersistentClient(path=str(root / "restored"))
restored = restored_client.create_collection("memories", embedding_function=None)
restored.add(**payload)
print(json.dumps({
  "chromadb": chromadb.__version__,
  "search_ids": result["ids"][0],
  "export_bytes": export_path.stat().st_size,
  "restored_rows": restored.count(),
}, indent=2))
PY
du -sk "$probe_dir/venv"
find "$probe_dir/venv" -type f | wc -l
```

Observed: `search_ids=["m1"]`, 499-byte logical JSON export, three restored rows, 315,436 KiB and 7,010 files in the isolated environment. This is explicitly a logical application export, not a validated production backup.

## Cross-component operational comparison

| Primitive | Local/offline | Metadata filtering | Concurrency | Export/backup | License | Maturity/dependency footprint | Principal migration risk |
|---|---|---|---|---|---|---|---|
| SQLite FTS5 + sqlite-vec 0.1.9 | Yes, in-process | SQL join + vec metadata/partition keys | WAL readers + one writer | Online backup; logical SQL/rows | Public domain + MIT/Apache-2.0 | SQLite mature; vector extension young; one 260 KiB probe package | Extension ABI/shadow tables, cross-architecture vector file concern, no ANN stable release credited |
| LanceDB 0.37.1 OSS | Yes, in-process/object store | SQL/DataFusion pre/post filters | MVCC; concurrent commits with bounded retries | Versions; Arrow/Parquet logical export | Apache-2.0 | Active 0.x; 16 packages/310,768 KiB probe env | Local/enterprise API differences, catalog/version/index rebuild |
| Chroma 1.5.9 local | Yes, embedded | Structured metadata/document filters | Shared-writer guarantees unknown in this probe | Logical get/restore reproduced; canonical backup unknown | Apache-2.0 | Active 1.x; 79 packages/315,436 KiB probe env | Local/distributed storage divergence and collection/index config fidelity |
| PostgreSQL 18 + pgvector 0.8.6 | Local service or remote | Full SQL, joins, partitions, RLS | MVCC, locks, isolation, transactions | `pg_dump`, physical backup/PITR, logical replication | PostgreSQL License | Mature DB + active extension; full service | Server/extension upgrade matrix, ANN rebuild, schema/RLS operation |
| Qdrant 1.19.0 | Local service, self-hosted cluster, cloud; Edge separate | JSON payload and filter-aware indexes | Configurable; default concurrent same-point replica inconsistency possible | Collection/node snapshots; logical point export | Apache-2.0 | Mature active 1.x service | Snapshot version/edition limits, aliases/index/shard config outside simple row export |
| Chroma 1.5.9 service | Network service/cloud | Same API family; parity not assumed | Mode-specific; not exercised | Mode-specific; unknown here | Apache-2.0 | Single-node/distributed/cloud operation | Different local/distributed storage subsystems |
| Neo4j 2026.07.1 | Local/server/cloud by edition | Property predicates and indexes | ACID, locks, read-committed default | Edition-specific offline/online backup | GPL-3.0 Community; commercial Enterprise/Aura | Mature dedicated graph service/JVM | Edition, procedures, index/constraint, internal ID lock-in |
| Apache AGE 1.7.0 | PostgreSQL service | Cypher + SQL | PostgreSQL transactions | PostgreSQL backup plus extension compatibility | Apache-2.0 | Active, PG-major-specific extension | Coupled PostgreSQL/AGE upgrades and Cypher subset |
| Litestream 0.5.16 | Sidecar CLI/daemon | N/A | Replicates one SQLite history; not multi-writer merge | Continuous/object-store restore, snapshots, integrity checks | Apache-2.0 | Active single binary/service | Mistaking recovery replication for active-active sync |
| Arrow/Parquet 25.0.1 | Yes | Consumer-dependent predicate pushdown | No mutation coordinator | Portable logical files | Apache-2.0 | Mature multi-language interchange | Loss of indexes, constraints, event semantics, and transaction history |
| Sentence Transformers 6.0 | Offline after model cache | N/A | Caller/runtime-managed | Model-specific artifact export | Apache-2.0 library; model-specific licenses | Heavy Python/ML runtime and weights | Re-embedding, dimension/metric/normalization and score drift |
| TEI 1.9.0 | Air-gapped after model download; service process | N/A | Batched inference service | Model cache/container, not memory export | Apache-2.0 runtime; model-specific licenses | Container/CPU/GPU/model operation | Endpoint/model revision drift and hardware compatibility |

## Migration and rollback invariants

Any hybrid assembled from these components should preserve the following independently of the chosen store:

1. Stable memory ID and immutable revision ID; role, tier, content, content hash, source, timestamps, and tombstone state.
2. Full provenance edges, conflict/merge decisions, application/exposure/outcome events, and actor/session identity.
3. Embedding model repository and immutable revision, dimension, dtype, normalization, query/document mode, distance metric, and generated-at time.
4. Store schema version, filter semantics, index definitions, tokenizer configuration, payload/partition/RLS policy, and consistency settings.
5. Logical export manifest with counts and checksums plus a documented physical backup. Neither replaces the other.
6. Restore into a clean target, rebuild indexes, and run role-isolation, retrieval, provenance, and event-count checks before cutover.
7. Dual-read comparison before dual-write where possible. Dual-write needs idempotency and a reconciliation ledger; it is not safe merely because both writes returned success.
8. Rollback must retain source writes until the target's export and reverse-import have been exercised. A target snapshot that cannot be read by the source is not a rollback plan.

## Explicit unknowns and required follow-up validation

- Production-scale retrieval quality and latency were not benchmarked. The three-record synthetic probes prove filter and round-trip mechanics only.
- No concurrent-writer probe was run against LanceDB, Chroma, PostgreSQL, Qdrant, Neo4j, or AGE. Documented guarantees remain grade B until the common V2 scenario measures this workload.
- No hosted service or paid API was invoked. Qdrant Cloud, Chroma Cloud/distributed, LanceDB Enterprise, Neo4j Aura, and managed PostgreSQL remain edition-specific grade-B/unknown surfaces.
- Chroma's complete snapshot-consistent local export/backup path was not established from current official docs. The logical client export does not preserve all collection/index configuration.
- Qdrant Edge was announced in current official docs but was not pinned or probed; parity with Qdrant server is unknown.
- Cross-platform `sqlite-vec` database portability has an open issue and was not reproduced. Logical export/restore across macOS/Linux architectures is required before relying on physical file migration.
- MCP conformance through the exact current Claude Code and Codex hosts was not exercised in this track. The validation join must test protocol revision negotiation, tool schema fidelity, resource updates, errors, and automatic lifecycle adapters.
- Embedding and reranker quality, memory-specific calibration, latency, hardware load, and model license compatibility were not tested. These belong to the quality/validation tracks.
- No graph query corpus established that a dedicated graph engine retires enough custom code to offset its operational surface. Graph stores remain viable primitives, not presumed necessities.
- Data-at-rest encryption, tenant authorization, secret distribution, audit retention, and deletion compliance depend on deployment edition and were not exhaustively evaluated here.

## Evidence ledger

All sources were accessed 2026-08-26.

| ID | Pin | Primary source and supported claim | Grade | Uncertainty |
|---|---|---|---|---|
| E1 | SQLite 3.50.4 probe | [FTS5](https://www.sqlite.org/fts5.html), [WAL](https://www.sqlite.org/wal.html), [backup](https://www.sqlite.org/backup.html): lexical index, concurrency mode, consistent backup | A/B | Exact production filesystem and load not tested |
| E2 | sqlite-vec 0.1.9 / `e9f598a...` | [release](https://github.com/asg017/sqlite-vec/releases/tag/v0.1.9), [metadata](https://alexgarcia.xyz/blog/2024/sqlite-vec-metadata-release/): filtered vec0 search | A | ANN pre-release excluded; large-scale performance unknown |
| E3 | LanceDB 0.37.1 / `b89f87f...` | [filtering](https://docs.lancedb.com/search/filtering), [format](https://lance.org/format/table/), local probe | A/B | Concurrent conflict/retry and object-store operation not reproduced |
| E4 | Chroma 1.5.9 / `11f3c74...` | [filtering](https://docs.trychroma.com/docs/querying-collections/metadata-filtering), [modes](https://docs.trychroma.com/reference/architecture/overview), local probe | A/B | Canonical backup and mode parity unknown |
| E5 | pgvector 0.8.6 / `8ee86c9...`; PostgreSQL 18 | [pgvector](https://github.com/pgvector/pgvector/tree/v0.8.6), [MVCC](https://www.postgresql.org/docs/18/mvcc-intro.html), [`pg_dump`](https://www.postgresql.org/docs/18/app-pgdump.html) | B | No disposable server probe |
| E6 | Qdrant 1.19.0 / `74f3e85...` | [filtering](https://qdrant.tech/documentation/search/filtering/), [consistency](https://qdrant.tech/documentation/scaling/consistency-guarantees/), [snapshots](https://qdrant.tech/documentation/operations/snapshots/) | B | No server/cloud probe; Edge excluded |
| E7 | Neo4j 2026.07.1 | [operations](https://neo4j.com/docs/operations-manual/current/), [transactions](https://neo4j.com/docs/operations-manual/current/database-internals/), [backup](https://neo4j.com/docs/operations-manual/current/backup-restore/) | B | Edition behavior not reproduced |
| E8 | AGE 1.7.0 PG18 / `806fa2e...` | [release notes](https://age.apache.org/release-notes/), [source](https://github.com/apache/age/tree/PG18/v1.7.0-rc0) | B | Cypher subset and upgrade path not probed |
| E9 | W3C Recommendation 2013 | [PROV-O](https://www.w3.org/TR/prov-o/), [constraints](https://www.w3.org/TR/prov-constraints/) | B | Agent-memory application profile proposed, not standardized |
| E10 | OTel spec 1.60.0 / `29ae8c7...`; semconv 1.44.0 | [specs](https://opentelemetry.io/docs/specs/), [GenAI registry](https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/) | B | Memory outcome convention remains custom |
| E11 | Litestream 0.5.16 / `6d61ef5...` | [replicate](https://litestream.io/reference/replicate/), [restore](https://litestream.io/reference/restore/) | B | Remote replica probe not run |
| E12 | Arrow 25.0.1 / `beccec0...` | [Arrow format](https://arrow.apache.org/docs/format/Columnar.html), [Parquet](https://parquet.apache.org/docs/file-format/) | B | Store-specific semantics require a manifest |
| E13 | Sentence Transformers 6.0.0 / `56c9548...` | [docs](https://www.sbert.net/), [retrieve/rerank](https://www.sbert.net/examples/sentence_transformer/applications/retrieve_rerank/README.html) | B | No model selected or evaluated |
| E14 | TEI 1.9.0 / `5699247...` | [quick tour](https://huggingface.co/docs/text-embeddings-inference/quick_tour), [hardware/models](https://huggingface.co/docs/text-embeddings-inference/en/supported_models) | B | No image/model probe |
| E15 | MCP 2026-07-28 / `5f5440b...` | [release](https://blog.modelcontextprotocol.io/posts/2026-07-28/), specification tools/resources/transports | B | Harness implementation parity must be tested |
| E16 | OpenAPI 3.2.0 | [normative specification](https://spec.openapis.org/oas/v3.2.0.html) | B | Describes HTTP, not memory semantics |
| E17 | MCP server-memory 2026.7.4 / `6dd0a68...` | [source](https://github.com/modelcontextprotocol/servers/tree/6dd0a683e198783e30feabf7abaf42f925bd18b1/src/memory), [npm registry](https://registry.npmjs.org/@modelcontextprotocol%2fserver-memory/2026.7.4) | B | Reference implementation, not a production R1-R7 solution |

## Bottom line for synthesis

The evidence supports several viable component families but no standalone component that passes R1-R7. Embedded primitives minimize service operation and preserve offline behavior; service databases strengthen shared concurrency and centralized access; graph/provenance components strengthen auditability; embedding/reranking components strengthen retrieval; neutral adapters reduce harness coupling. Every composition still retains agent-teams-owned role policy, curation, hot-context assembly, lifecycle delivery, and usefulness semantics. The validation and decision tracks should compare complete hybrid architectures by that retained maintenance surface rather than rank the components in isolation.
