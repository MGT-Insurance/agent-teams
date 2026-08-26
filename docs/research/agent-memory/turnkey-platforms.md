# Turnkey agent-memory platforms

**Research date:** 2026-08-26

**Evidence access date:** 2026-08-26
**Scope:** turnkey memory platforms that could replace part of the current
agent-teams memory implementation. This report does not select a winner.

## Method and frozen requirements

Hosted and self-hosted products are separate deployment propositions. A feature
in one edition is not credited to another. The evaluation uses only official
documentation, official repositories and release tags, SDK/API references,
official pricing or legal pages, and primary papers. Product marketing is not
used to support a gate result.

The frozen requirements are:

| Gate | Required behavior |
| --- | --- |
| R1 | Role-scoped shared memory. |
| R2 | Repeated role instances work across sessions and machines, with defined concurrency and synchronization. |
| R3 | Memory is automatically available at startup, resume, compaction, and to subagents. |
| R4 | Curation is bounded and covers deduplication, conflicts, provenance, recoverability, promotion, and demotion. |
| R5 | A token-budgeted hot set is available while a larger pool remains searchable. |
| R6 | A neutral, stable CLI, API, or protocol works across Claude Code, Codex, and at least one other client. |
| R7 | An applied/usefulness event records provenance and outcome and can inform retention or promotion. |

Gate values mean `Pass` (the edition supplies the requirement), `Partial`
(useful product behavior exists, but retained agent-teams code is necessary),
`Fail` (the documented behavior conflicts with or omits the requirement), and
`Unknown` (primary evidence is insufficient). An unknown is not a pass.

Evidence grades:

- **A:** reproduced at a pinned version and supported by a current primary source.
- **B:** supported by current primary documentation, API/source, release, or a
  primary paper, but not reproduced here.
- **C:** maintainer roadmap/discussion or an unapplied primary paper.
- **D:** marketing, secondary reporting, or inference. Grade D is excluded from
  gate decisions.

## Candidate screen

| Candidate | Editions screened | Pinned/current reference | Deep evaluation decision |
| --- | --- | --- | --- |
| Mem0 | Mem0 Platform; Mem0 OSS | OSS `v2.0.19`, commit `dc82354e143c2581d505d581a00286d6ef8c3605` | Deep. It exposes scoped memory, search, history, export, REST, CLI, and MCP surfaces. The hosted and OSS APIs differ. |
| Zep / Graphiti | Zep Cloud; Enterprise BYOC; Graphiti OSS | Graphiti `v0.29.3`, commit `021d3a57d511f21b10adaf7fa923bd5c1fce5e9d` | Deep. Zep Cloud and Graphiti share concepts but differ materially in engine, operations, and governance. |
| Letta | Letta Cloud; local App Server / Letta Code | Letta Code `v0.26.2`, commit `c033cc0b86cf2e9d2fe8a34b714de66ce644ae48` | Deep. It provides the strongest lifecycle ownership in this set, but as an agent runtime rather than a memory-only service. |
| Supermemory | Hosted API; local server/SMFS | server `v0.0.8`, commit `5d2b5855fe492a3682a1cde4a255e2db0c4db595` | Screened, not deep. The API, local binary, and filesystem interface are credible, but the server release is young and current primary evidence does not establish full-fidelity export/restore, conflict handling, or R7. |
| Cognee | Cognee Cloud; OSS REST/library | `v1.5.3`, commit `25200a548fc6d96aa58d5663603f7c4b6b3f7621` | Screened, not deep. It is a credible knowledge engine, but role delivery, a bounded hot set, and application/usefulness telemetry remain custom. Its Markdown export is not evidence of a full graph round trip. |

### Screen exclusions and boundaries

- **Zep Community Edition is excluded.** Zep's official FAQ says that it is
  deprecated and unsupported. Zep Cloud, proprietary Enterprise BYOC, and
  Apache-2.0 Graphiti OSS are evaluated separately; Graphiti is not a
  self-hosted edition of Zep Cloud.
- **Legacy Letta server APIs are excluded from current scores.** The old
  `letta-ai/letta` Python/Docker server reached `0.16.8`, but current Letta docs
  say that deployment is no longer actively maintained or supported. Its `.af`
  export and legacy block/archive endpoints are not credited to Letta Code.
- **Supermemory remains a validation lead.** Official docs establish tagged
  scoping, local storage, SDK operations, profile generation, and SMFS sync.
  They do not establish deterministic conflict resolution for simultaneous
  mounts, full hosted export/import, or an applied-memory outcome event.
- **Cognee is better aligned with the component track.** Datasets and
  permissions can become role namespaces, and `cognify`/`improve` can curate a
  graph, but the agent-teams lifecycle and hot-context contract would still be
  built outside Cognee.

## Deep evaluation 1: Mem0

### Edition profile

Mem0 Platform is a managed API with memory export jobs, CLI/MCP clients,
webhooks, feedback, filters, and plan-based limits. Mem0 OSS is an Apache-2.0
Python library and FastAPI server backed by operator-selected vector, graph,
LLM, and embedding providers. The OSS reference compose stack uses PostgreSQL
with pgvector and enables Mem0 telemetry by default unless disabled.

The current docs contain an important version ambiguity. The generic add-memory
overview describes LLM extraction, deduplication, and conflict resolution with
newer truth replacing older truth. The v3 Platform algorithm/API describes an
add-only pipeline with exact-hash deduplication and no `UPDATE` or `DELETE`
decision. Neither behavior can be assumed across both versions or editions.

### R1-R7 matrix

| Gate | Mem0 Platform | Mem0 OSS |
| --- | --- | --- |
| R1 | **Partial, B.** `user_id`, `agent_id`, `run_id`, and `app_id` provide namespaces, but current filter docs disagree on whether entity types combine or identify independent entity stores. A role mapping and isolation test remain necessary. | **Pass for basic isolation, A.** A local `v2.0.19` probe stored and retrieved separate `agent_id=implementer` and `agent_id=tester` records. Role identity policy remains adapter code. |
| R2 | **Partial, B.** The managed API is reachable across machines and offers rate limits and asynchronous operations. Primary docs do not define convergence, serialization, or lost-update behavior for concurrent contradictory writes. | **Unknown, B.** A central deployment can serve many clients, but no primary source defines same-memory write ordering or multi-writer convergence. The reference stack is not evidence of distributed synchronization. |
| R3 | **Partial, B.** CLI and MCP can expose memory to agents, but neither proves automatic delivery at all four agent-teams lifecycle points. | **Partial, B.** REST/SDK access is available; Claude Code, Codex, compaction, resume, and subagent hooks remain retained adapters. |
| R4 | **Partial, B.** History, CRUD, export, decay, and some algorithm-specific dedupe exist. Version-dependent conflict behavior, bounded promotion/demotion, and reversible curation are not one documented contract. | **Partial, A/B.** Local CRUD and history were reproduced. Full inferred dedupe/conflict behavior was not. Manual deletion/history does not provide bounded automated curation or rollback. |
| R5 | **Partial, B.** Search supports filters, `top_k`, threshold, reranking, and recency weighting. There is no documented token-budgeted hot tier or deterministic injection contract. | **Partial, B.** Search can bound result count; token measurement, startup injection, hot/cold promotion, and overflow behavior remain custom. |
| R6 | **Pass for access boundary, B.** REST, official SDKs, CLI, and MCP are client-neutral. The lifecycle contract still belongs to agent-teams under R3. | **Pass for access boundary, A/B.** The local REST surface and Python SDK were exercised; the API is usable from arbitrary clients. Hosted `/v1` paths and OSS paths are not drop-in compatible. |
| R7 | **Partial, B.** The feedback API accepts positive/negative feedback, while webhooks report add/update/delete/categorize. Docs do not show an automatic memory-applied event that binds provenance, task outcome, and retention action. | **Fail, B.** The reference server exposes request logging and memory history, but no applied/usefulness endpoint or retention feedback loop. |

### Operational assessment

| Dimension | Mem0 Platform | Mem0 OSS |
| --- | --- | --- |
| Deployment | Managed multi-tenant service; enterprise documentation advertises on-premise separately. | Python package or FastAPI server/dashboard; reference compose includes Mem0, PostgreSQL/pgvector, and the dashboard. External model and embedding services may still be required. |
| Data ownership and export | Structured, filterable export jobs are documented. Full-fidelity re-import of memories plus history, feedback, and webhook state is not documented. | The operator owns configured stores. A portable, supported full-state export/import across vector, graph, history, and provider configuration is not documented. |
| Concurrency | Rate limits and async operations are documented; write conflict semantics are unknown. | API concurrency exists, but transaction and contradictory-write semantics across stores are unknown. |
| Hot context and search | Semantic/filter search, threshold, rerank, graph memory, and memory decay. Result count is not a token budget. | Semantic search and configurable stores; hybrid behavior depends on optional dependencies/configuration. No native hot-context delivery. |
| Curation and conflicts | History and feedback help review. Generic and v3 algorithm docs describe different update/dedupe behavior, so a pinned API contract is mandatory. | CRUD/history are available. Inference behavior depends on model and release; manual rollback must reconstruct prior values. |
| Telemetry | Webhooks and feedback are product events, not a complete R7 outcome chain. | Anonymous telemetry can be disabled with `MEM0_TELEMETRY=false`; request logs are operational, not usefulness evidence. |
| Privacy | Hosted privacy terms permit processing customer content and use of service providers, including AI infrastructure. Enterprise controls require contract review. | Storage can remain operator-controlled, but configured LLM/embedding/reranking providers may receive memory content. Secrets, network egress, retention, and deletion remain operator duties. |
| License and cost | Hobby: free, 10,000 adds/1,000 retrievals monthly; Starter: $19/month, 50,000/5,000; Pro: $249/month, 500,000/50,000; enterprise is custom as of access date. Usage definitions and overages require a quote/plan check. | Apache-2.0 at `v2.0.19`; costs include databases, graph/vector services, inference, embeddings, backups, monitoring, and operations. |
| Maturity and operations | Managed backups/scaling reduce operator load, but quotas, vendor availability, API-version drift, and export completeness are dependencies. | Active project with tagged releases. The operator must pin provider combinations, run migrations, secure auth, monitor multiple stores, and test backup restoration. |

### Retained adapters and custom code

- Map agent-teams roles and repeated instances to a tested entity convention;
  reject unscoped searches.
- Preserve `ateam learn`, `learnings`, `recall`, `applied`, and `forget` as a
  stable compatibility layer over edition-specific APIs.
- Select and inject a deterministic token-budgeted hot set at startup, resume,
  compaction, and subagent spawn.
- Implement provenance-bearing application/outcome events and use them in a
  reviewable promotion, demotion, and deletion policy.
- Implement dual-write, reconciliation, full export validation, rollback, and
  an outage cache. Platform and OSS API shapes require separate adapters.

### Migration, rollback, and unknowns

- A Platform export may not round-trip history, feedback, identifiers, graph
  edges, or timestamps. Validate schema and import before any cutover.
- OSS storage ownership does not by itself guarantee a coherent backup across
  relational, vector, and graph stores. Snapshot ordering and restore drills are
  required.
- Scope-filter ambiguity can cause leakage or false-empty retrieval. Test every
  identifier combination against the exact API version.
- Algorithm drift can alter extracted facts and conflict behavior. Pin both the
  Mem0 release/API version and model configuration; retain raw source events for
  deterministic rebuild.
- Concurrent contradictory update semantics, hosted deletion completion, graph
  export fidelity, and feedback-driven retention are explicit unknowns.

## Deep evaluation 2: Zep Cloud and Graphiti OSS

### Edition profile

Zep Cloud builds temporal user and group graphs from episodes and returns a
prompt-ready context block. It adds managed infrastructure, access controls,
logs, webhooks, and a proprietary graph engine. Enterprise BYOC is a commercial
deployment of Zep, not Graphiti.

Graphiti is an Apache-2.0 Python framework for temporal knowledge graphs. It
supports Neo4j, FalkorDB, Kuzu, and Amazon Neptune according to current project
documentation, with model/provider dependencies. Its MCP server is documented
as experimental. A `group_id` namespaces graph data; cross-group retrieval is an
application fan-out concern.

### R1-R7 matrix

| Gate | Zep Cloud | Graphiti OSS |
| --- | --- | --- |
| R1 | **Pass, B.** Per-user graphs and group graphs are first-class. Agent-teams must map role identity and authorize access. | **Pass, B.** `group_id` isolates nodes and edges and can represent a role namespace. Cross-role search requires explicit queries. |
| R2 | **Partial, B.** A hosted graph is shared across threads and machines. Rate limits are defined; ordering and convergence for simultaneous contradictory episodes are not. | **Partial, B.** Async ingestion and a configurable provider semaphore support concurrency. No primary contract defines distributed multi-writer convergence or same-fact ordering. |
| R3 | **Partial, B.** `memory.get` returns a prompt-ready context block, but invocation at agent-teams lifecycle points and subagent inheritance remain adapters. | **Partial, B.** Library/search and experimental MCP access exist. Startup, resume, compaction, and subagent injection are not supplied. |
| R4 | **Partial, B.** Temporal invalidation and episode provenance are strengths. Deleting an episode can preserve shared derived information, and the docs do not provide bounded promotion/demotion or full rollback. | **Partial, B.** Episodes retain provenance and facts can be invalidated. Bulk ingestion skips edge invalidation, and deterministic bounded curation/rollback remains custom. |
| R5 | **Partial, B.** Prompt-ready context and graph search bound results, but no documented agent-supplied token budget governs a hot tier. | **Partial, B.** Hybrid semantic/BM25/graph retrieval, RRF, MMR, reranking, and result limits are available; token accounting and hot injection are not. |
| R6 | **Pass for access boundary, B.** REST and official Python, TypeScript, and Go SDKs are client-neutral. | **Partial, B.** The Python library is stable enough to integrate, but the turnkey MCP server is experimental and there is no documented stable general-purpose service contract equivalent to Zep Cloud. |
| R7 | **Fail, B.** API logs, audit logs, debug mode, and ingestion webhooks observe system operations. None is a documented memory-applied/usefulness event with task outcome and retention action. | **Fail, B.** Anonymous product telemetry and graph provenance do not record whether retrieved memory was applied or useful. |

### Operational assessment

| Dimension | Zep Cloud / Enterprise BYOC | Graphiti OSS |
| --- | --- | --- |
| Deployment | Managed Cloud. Enterprise offers BYOC/BYOK and contractual controls. Community Edition is not a supported option. | Python library plus a graph database and model/embedding providers. Official quickstarts use Neo4j or FalkorDB; the MCP compose stack is experimental. |
| Data ownership and export | APIs can paginate nodes, edges, episodes, and observations. No primary source establishes one-shot, full-fidelity export and supported re-import of the complete graph and service metadata. | Operator owns graph storage and can query the graph directly. Portable cross-backend backup/restore and schema migration are operator concerns. |
| Concurrency | Hosted rate limits are published. Same-subject write serialization and contradiction ordering are unknown. | A semaphore limits provider operations (default 10 in quickstart guidance). Database transactions, retries, and multi-process episode ordering require workload tests. |
| Hot context and search | `memory.get` returns a composed context block; graph search supports user/group data. Exact token-budget guarantees are not documented. | Hybrid search and recipes provide strong retrieval control. Result limits, not token budgets, are native. |
| Curation and conflicts | Temporal facts can become invalid while retaining history. Cascading deletes and shared summaries mean source deletion is not guaranteed semantic erasure from every derivative. | Episode `MENTIONS` edges preserve provenance. Temporal invalidation is available, but bulk ingestion intentionally omits invalidation and can change curation semantics. |
| Telemetry | API logs, dashboard audit logs, temporary debug traces, and processing/provider webhooks. Sensitive debug data requires handling controls. | Anonymous PostHog system/configuration telemetry is enabled unless `GRAPHITI_TELEMETRY_ENABLED=false`; this is operational telemetry only. |
| Privacy | Zep documents SOC 2, HIPAA BAA availability, BYOK, ABAC on eligible plans, and a US West 2 data location. Contract and tenant configuration determine actual controls. | Data residency is operator-controlled, but prompts and graph content can leave the environment through LLM, embedder, or reranker providers. Database access control is operator-owned. |
| License and cost | Flex is listed at $1,250/year (or $125 monthly) with 50,000 credits; Flex Plus at $3,750/year (or $375 monthly) with 200,000 credits. Enterprise/BYOC is custom. Credits are tied to episode processing, so replay/rebuild cost matters. | Graphiti `v0.29.3` is Apache-2.0. Database licensing, inference, embeddings, storage, observability, backup, and engineering are additional costs. |
| Maturity and operations | Managed operational surface and documented limits; service/API availability, credit economics, export completeness, and proprietary engine behavior remain dependencies. | Active tagged project, but the operator owns graph/database compatibility, indexes, provider rate limits, migrations, backups, telemetry policy, and MCP hardening. |

### Retained adapters and custom code

- Map role scope to Zep user/group graphs or Graphiti `group_id`; prevent
  accidental unscoped and cross-group retrieval.
- Convert graph/context results into a deterministic token-budgeted hot set and
  inject it through every Claude Code and Codex lifecycle hook.
- Add an R7 event store linking retrieved node/edge/episode IDs to application,
  task outcome, and later promotion/demotion.
- Add bounded curation rules over temporal invalidation, merge, source deletion,
  and bulk ingestion; preserve raw episodes for replay.
- Build export normalization, completeness checks, graph-backend restore tests,
  dual-read comparison, and rollback tooling.

### Migration, rollback, and unknowns

- Reading all Cloud graph objects is not proof that a supported round-trip
  import preserves identity, temporal validity, observations, and derived
  context. A representative export/import proof is mandatory.
- Deleting an episode may leave information in shared nodes, edges, or summaries.
  Privacy deletion and rollback must verify derivatives, not only source rows.
- Graphiti bulk ingestion has different invalidation behavior from incremental
  ingestion. Rebuild and migration paths can therefore yield different graphs.
- Model/extractor changes can produce graph drift. Pin Graphiti, models,
  prompts, database versions, and episode timestamps; retain immutable inputs.
- Zep same-subject concurrency semantics, BYOC operational responsibility,
  full export/import, deterministic context token bounds, and usefulness-driven
  retention are explicit unknowns.

## Deep evaluation 3: Letta Cloud and local App Server

### Edition profile

Current Letta is a stateful agent runtime. Letta Code `v0.26.2` and the App
Server expose an Agent SDK over WebSocket, an OpenAI-compatible API, and ACP.
The local backend stores state on the device; each agent has a Git-backed MemFS.
The App Server can keep agents alive, queue work, replay events after reconnect,
and accept multiple clients.

Cloud shared-memory repositories are Git repositories owned by an organization
and attached to agents. Agents must commit and push; peers must pull or sync.
Current docs explicitly distinguish this Cloud feature from local agents, which
have their own MemFS/project files. This gives Cloud an inspectable
synchronization mechanism but also exposes normal Git conflict and stale-read
risks.

### R1-R7 matrix

| Gate | Letta Cloud | Local App Server / Letta Code |
| --- | --- | --- |
| R1 | **Partial, B.** Shared memory repositories can attach to multiple agents, but role membership, authorization, and reliable commit/pull behavior are policies outside the repository. | **Partial, B.** Reusing one long-lived agent shares its MemFS, but separate role instances do not receive a native shared-memory repository. Project Git/custom synchronization is required. |
| R2 | **Partial, B.** Git gives explicit commit/push/pull and recoverability across machines. Concurrent edits, stale pulls, merge conflicts, and agents that fail to commit are not automatically resolved. | **Partial, B.** A central App Server supports multiple clients, queued turns, reconnect sync, and replay. Cross-machine shared state requires that central server; distinct-agent memory synchronization remains custom. |
| R3 | **Partial, B.** Letta owns agent resume, compaction, background dreaming, and subagent behavior within its runtime. Existing Claude Code/Codex processes still need integration, or must be replaced by the Letta runtime. | **Partial, B.** The local runtime supplies the same internal lifecycle behavior. Automatic delivery into external agent-teams runtimes is not supplied. |
| R4 | **Partial, B.** MemFS history, `/doctor`, dreaming, optional second-agent review, and pre-restructure backups provide curation and recoverability. They do not establish a deterministic bounded policy or human conflict workflow. | **Partial, B.** Git-backed MemFS supports inspection and rollback. Dreaming is model-driven, backup is manual outside selected operations, and shared conflict handling is absent. |
| R5 | **Partial, B.** MemFS organization and system-prompt token audits support context management. No current source defines an agent-teams token budget with deterministic hot selection and larger searchable cold storage. | **Partial, B.** Same mechanisms; local file/search tools can retrieve more context, but the hot-set contract remains runtime-specific and custom. |
| R6 | **Partial, B.** WebSocket Agent SDK, OpenAI-compatible API, and ACP are broad interfaces, but they expose a full Letta agent runtime rather than a stable memory-only protocol for three existing clients. | **Partial, B.** The interfaces are network/client neutral. Adopting them either couples agent-teams to Letta's harness or requires a custom memory facade. The direct protocol is documented as evolving. |
| R7 | **Fail, B.** Runtime events and traces show agent activity, but current primary docs do not define a memory-applied/usefulness event that drives retention or promotion. | **Fail, B.** Local event replay and Git history are not outcome-linked memory telemetry. |

### Operational assessment

| Dimension | Letta Cloud | Local App Server / Letta Code |
| --- | --- | --- |
| Deployment | Managed agent runtime with plan limits and optional BYOK. Shared-memory repositories are Cloud-only in current docs. | Node `>=22.19.0`, persistent local backend, model/provider setup, and an App Server listening on WebSocket. Official deployment examples persist `/root/.letta` and workspace data. |
| Data ownership and export | Shared memory is Git-backed and cloneable at that layer. A documented full export/import of all current agent state, messages, runtime queues, credentials, and metadata was not found. | MemFS and local backend state are operator-controlled. Git preserves memory-file history, but current docs warn that local state is not automatically backed up. A full supported server restore contract remains unproven. |
| Concurrency | Shared repositories use Git synchronization. Agent turns and memory-repository edits are distinct concurrency domains. | The App Server accepts multiple clients and owns queues/event streams. The application owns users, task durability, retry policy, and durable results. Idempotent retry can reuse `client_message_id`. |
| Hot context and search | MemFS, memory tools, `/doctor`, compaction, and dreaming operate inside Letta agents. No external-agent token-budget guarantee is documented. | Same, with local filesystem and search access. External Claude/Codex prompt injection remains custom. |
| Curation and conflicts | Dreaming consolidates lessons after step thresholds or compaction; optional second-agent review and Git backup aid recovery. Git merge conflicts and model-driven errors require policy. | Per-agent Git history aids rollback. No native shared repository means cross-agent conflict curation must be added. |
| Telemetry | Rich runtime events are useful for operations, but no R7 semantic is documented. | Event streams and reconnect replay support client state; the operator must add logs, metrics, retention, and R7. |
| Privacy | Hosted state and provider use are governed by Cloud terms and BYOK configuration. Full data-flow and deletion review remains a contracting task. | Agent state stays on device under the local backend, but provider calls can transmit context. The runtime has shell/filesystem capabilities, so token protection, network isolation, least privilege, and workspace sandboxing are material controls. |
| License and cost | Free plan: 3 agents; Pro: $20/month, up to 20 agents; API Plan: $20/month plus $0.10 per active agent/month and $0.00015 per tool-execution second, plus model tokens; enterprise is custom as of access date. | Letta Code `v0.26.2` is Apache-2.0. Costs include model use, Node/runtime hosts, persistent disks, backups, monitoring, and the security burden of a host-capable agent runtime. |
| Maturity and operations | Current product surface is actively evolving. Cloud reduces server operations but creates runtime and data-model coupling. | The current App Server replaces the unsupported legacy Docker server. Protocol evolution, manual backups, provider compatibility, process supervision, and broad host permissions increase operational scope. |

### Retained adapters and custom code

- Decide whether Letta is the agent runtime or only a memory backend. A
  memory-only use requires a facade; a runtime replacement changes agent-teams
  execution semantics and is outside this research track.
- Map roles to Cloud repository membership or local long-lived agents, with
  explicit commit/push/pull, stale-read, merge-conflict, and authorization rules.
- Preserve current CLI semantics and lifecycle hooks for Claude Code and Codex.
- Enforce an independent token budget and provenance envelope around selected
  MemFS content.
- Add R7 application/outcome events, review queues, bounded promotion/demotion,
  backup verification, and export normalization.

### Migration, rollback, and unknowns

- Legacy `.af` export is not evidence for the current App Server. Migration must
  prove current-state coverage rather than depend on deprecated endpoints.
- Cloud shared Git repositories cover shared memory files, not necessarily all
  agent state. Full account export/import and deletion completeness are unknown.
- A local backup must capture the local backend and every MemFS repository at a
  coherent point. Current docs require manual backup and do not prove live
  snapshot consistency.
- Concurrent Cloud agents can create ordinary Git conflicts or omit commits.
  A failed push can leave peers on stale memory without an application-level
  reconciliation loop.
- Protocol compatibility across Letta releases, deterministic dreaming output,
  multi-client turn ordering, full restore, and usefulness-driven retention are
  explicit unknowns.

## Shared retained agent-teams boundary

No deep candidate satisfies all seven frozen gates without retained code. This
is a gate result, not a product ranking or winner selection.

| Retained capability | Why it remains outside the platform |
| --- | --- |
| Stable agent-teams CLI | Keeps `learn`, `learnings`, `recall`, `applied`, and `forget` consistent across vendors and supports rollback. |
| Role and principal mapping | Vendor users, agents, groups, tags, and repositories have different isolation and authorization semantics. |
| Lifecycle delivery | Claude Code and Codex startup, resume, compaction, and subagent hooks are agent-teams concerns. |
| Token-budget controller | Every candidate can limit search, but none proves the frozen deterministic hot-set contract. |
| Provenance envelope | Source Bead, role, session, machine, timestamps, content hash, and vendor object IDs must survive migration. |
| R7 event and policy | Retrieval is not application. Agent-teams must record use/outcome and apply reviewable retention and promotion rules. |
| Migration controller | Dual-write/read comparison, checkpoints, retries, quarantine, reconciliation, export validation, and rollback must be vendor-neutral. |
| Reliability and security | Outage cache, secrets, redaction, audit policy, deletion verification, monitoring, and restore drills remain operator responsibilities. |

## Migration and rollback risk register

| Risk | Required control before a pilot |
| --- | --- |
| Namespace mismatch or leakage | Golden tests for role, user, run, agent, group, and repository combinations; deny unscoped retrieval. |
| Lossy export | Export and re-import a fixture containing provenance, conflicts, deletions, timestamps, history, and Unicode/binary-adjacent content; compare normalized state. |
| Model-derived drift | Retain immutable source events and extraction configuration; pin models/prompts where possible; rebuild into a shadow namespace. |
| Dual-write divergence | Use idempotency keys and a reconciliation ledger; never infer success from one backend; alert on lag and mismatch. |
| Incomplete deletion | Verify source, indexes, graph derivatives, history, export artifacts, caches, and backups against policy. |
| Concurrent curation | Define ordering, optimistic concurrency, merge/review queues, and quarantine for contradictory memories. |
| Vendor/API change | Pin release/API versions, run contract tests in CI, and keep the current backend readable until rollback expiry. |
| Cost amplification | Measure extraction, embedding, reranking, graph construction, replay, and export costs on actual workloads, including failed/retried writes. |
| Hosted outage or account loss | Maintain bounded local hot cache, regular validated exports, credential rotation, and a tested read-only fallback. |
| Self-host restore failure | Use application-consistent snapshots and scheduled restore drills, including vector/graph indexes and encryption keys. |

## Reproducible local probes

### Mem0 OSS: run result

The following core-path probe was run against `mem0ai==2.0.19` in a temporary
virtual environment with telemetry disabled. It used embedded Qdrant and Mem0's
deterministic mock embedder, so it incurred no model call. It validated scoped
CRUD/search and history plumbing only. It did **not** test semantic quality,
inference, inferred deduplication, contradiction handling, concurrent writes,
hybrid BM25, auth, or server deployment.

Observed result:

```json
{"history_events":["ADD","UPDATE"],"infer":false,"package":"mem0ai==2.0.19","role_isolation":{"implementer":1,"tester":1},"semantic_quality_tested":false}
```

Reproduction outline:

```bash
python3 -m venv /tmp/mem0-probe
/tmp/mem0-probe/bin/pip install 'mem0ai==2.0.19'
MEM0_TELEMETRY=false OPENAI_API_KEY=probe-only /tmp/mem0-probe/bin/python - <<'PY'
import json
import tempfile
from mem0 import Memory

root = tempfile.mkdtemp(prefix="mem0-qdrant-")
config = {
    "vector_store": {
        "provider": "qdrant",
        "config": {"path": root, "collection_name": "probe", "embedding_model_dims": 10},
    },
    "embedder": {"provider": "mock", "config": {"embedding_dims": 10}},
}
memory = Memory.from_config(config)
ids = {}
for role in ("implementer", "tester"):
    result = memory.add(
        "Remember scoped auth checks for this role.", agent_id=role, infer=False
    )
    ids[role] = result["results"][0]["id"]

isolated = {
    role: len(memory.search("scoped auth checks", agent_id=role))
    for role in ids
}
memory.update(ids["implementer"], "Remember scoped auth and rollback checks.")
events = [entry["event"] for entry in memory.history(ids["implementer"])]
print(json.dumps({"history_events": events, "infer": False,
                  "package": "mem0ai==2.0.19",
                  "role_isolation": isolated,
                  "semantic_quality_tested": False}, sort_keys=True))
PY
```

### Graphiti OSS: unrun gap

A meaningful test requires a supported graph database and model/embedder
credentials. Docker was not available in this worktree environment, and no
credential-bearing or paid model probe was authorized. The minimal future probe
is:

```bash
git clone --branch v0.29.3 --depth 1 https://github.com/getzep/graphiti.git /tmp/graphiti
cd /tmp/graphiti
docker compose -f mcp_server/docker-compose.yml up -d
# Add two episodes under distinct group_id values, search each group, add a
# contradictory episode, verify temporal invalidation/provenance, then restart
# the services and verify persistence. Record provider and database versions.
docker compose -f mcp_server/docker-compose.yml down
```

The compose path and environment keys must be checked against the pinned tag
before execution because the MCP server is experimental.

### Letta local App Server: unrun gap

A meaningful current-stack test requires Node `>=22.19.0`, a provider or local
model, and a long-running host-capable agent process. It was not started in this
research task. A future isolated probe should use a disposable backend:

```bash
npm install -g @letta-ai/letta-code@0.26.2
export LETTA_LOCAL_BACKEND_DIR="$(mktemp -d)"
letta server --backend local --listen ws://127.0.0.1:4500
# In a second shell, connect with the pinned Agent SDK. Create one agent, write
# a MemFS memory, reconnect with the same client_message_id, verify replay and
# persistence, connect a second client, and inspect the MemFS Git history.
```

The probe must also test backup/restore while the server is stopped and confirm
that separate agents do not silently share local MemFS state.

### Screened candidates: unrun gaps

- **Supermemory:** run the pinned `server-v0.0.8` binary with a temporary
  `.supermemory`, test two container tags, two concurrent SMFS clients, restart,
  export/restore, deletion, and offline-provider mode. Do not infer commercial
  support terms from the open binary.
- **Cognee:** run `v1.5.3` with authentication enabled and a supported
  relational/vector/graph combination; test dataset isolation, concurrent
  `cognify`/search, `improve`, Markdown export, and graph reconstruction. Treat
  Markdown export as lossy until proved otherwise.

## Primary evidence ledger

All sources below were accessed 2026-08-26. Repository tags pin the OSS source;
hosted documentation and prices can change and must be captured again for a
procurement or pilot decision.

### Mem0

- **B:** [OSS REST API](https://docs.mem0.ai/open-source/features/rest-api) and
  [self-host setup](https://docs.mem0.ai/open-source/setup) define the server,
  routes, auth, dashboard, and reference storage.
- **A/B:** [Mem0 OSS `v2.0.19`](https://github.com/mem0ai/mem0/tree/v2.0.19),
  [release](https://github.com/mem0ai/mem0/releases/tag/v2.0.19), and
  [Apache-2.0 license](https://github.com/mem0ai/mem0/blob/v2.0.19/LICENSE)
  pin the reproduced library and source.
- **B:** [search](https://docs.mem0.ai/core-concepts/memory-operations/search),
  [entity scope](https://docs.mem0.ai/platform/features/entity-scoped-memory),
  and [v2 filters](https://docs.mem0.ai/platform/features/v2-memory-filters)
  support search/scoping claims and expose the entity-combination ambiguity.
- **B:** [generic add behavior](https://docs.mem0.ai/core-concepts/memory-operations/add),
  [v3 graph memory](https://docs.mem0.ai/platform/features/graph-memory), and
  [v3 add API](https://docs.mem0.ai/api-reference/memory/add-memories) support
  the algorithm-version caveat.
- **B:** [history](https://docs.mem0.ai/api-reference/memory/history-memory),
  [export](https://docs.mem0.ai/platform/features/memory-export),
  [webhooks](https://docs.mem0.ai/platform/features/webhooks), and
  [memory decay](https://docs.mem0.ai/platform/features/memory-decay) support
  curation, portability, telemetry, and recency claims.
- **B:** [CLI](https://docs.mem0.ai/platform/cli) and
  [MCP](https://docs.mem0.ai/platform/mem0-mcp) support the access-boundary result.
- **B:** [pricing](https://mem0.ai/pricing),
  [privacy policy](https://mem0.ai/privacy-policy), and
  [terms](https://mem0.ai/terms) support dated hosted cost and legal statements.

### Zep and Graphiti

- **B:** [Zep overview](https://help.getzep.com/overview),
  [quickstart/context](https://help.getzep.com/v2/quickstart), and
  [users/user graphs](https://help.getzep.com/users-and-user-graphs) define the
  hosted memory and graph model.
- **B:** [reading graph data](https://help.getzep.com/reading-data-from-the-graph)
  and [deleting graph data](https://help.getzep.com/deleting-data-from-the-graph)
  support export and semantic-deletion caveats.
- **B:** [security/compliance](https://help.getzep.com/security-compliance),
  [BYOK](https://help.getzep.com/bring-your-own-key), and
  [ABAC](https://help.getzep.com/attribute-based-access-control) support hosted
  governance statements.
- **B:** [API logging](https://help.getzep.com/api-logging),
  [audit logging](https://help.getzep.com/audit-logging),
  [debug mode](https://help.getzep.com/debug-mode), and
  [webhooks](https://help.getzep.com/v3/webhooks) support the R7 distinction.
- **B:** [pricing](https://www.getzep.com/pricing/),
  [rate limits](https://help.getzep.com/rate-limits), and the
  [official FAQ](https://help.getzep.com/faq) support dated cost, operation, and
  Community Edition exclusion.
- **B:** [Graphiti overview](https://help.getzep.com/graphiti/getting-started/overview),
  [namespacing](https://help.getzep.com/graphiti/core-concepts/graph-namespacing),
  [episodes/provenance](https://help.getzep.com/graphiti/core-concepts/adding-episodes),
  and [search](https://help.getzep.com/graphiti/working-with-data/searching)
  support the OSS graph assessment.
- **B:** [quickstart/concurrency](https://help.getzep.com/graphiti/getting-started/quick-start),
  [MCP server](https://help.getzep.com/graphiti/getting-started/mcp-server), and
  [telemetry](https://help.getzep.com/graphiti/other/telemetry) support the
  operational findings.
- **B:** [Graphiti `v0.29.3`](https://github.com/getzep/graphiti/tree/v0.29.3),
  [release](https://github.com/getzep/graphiti/releases/tag/v0.29.3), and
  [Apache-2.0 license](https://github.com/getzep/graphiti/blob/v0.29.3/LICENSE)
  pin the evaluated source. The [Graphiti primary paper](https://arxiv.org/abs/2501.13956)
  is directional evidence only and does not determine a gate.

### Letta

- **B:** [self-hosting](https://docs.letta.com/self-hosting) defines local
  storage, manual backup, App Server startup, and deployment responsibilities.
- **B:** [shared memory](https://docs.letta.com/concepts/shared-memory) defines
  Cloud Git repositories, agent attachment, and commit/push/pull behavior.
- **B:** [memory and dreaming](https://docs.letta.com/configuration/memory)
  defines MemFS, `/doctor`, background curation, review, and backups.
- **B:** [App Server](https://docs.letta.com/platform/app-server) and
  [quickstart](https://docs.letta.com/platform/app-server/quickstart) define
  protocols, clients, queue ownership, retries, and replay.
- **B:** [pricing](https://docs.letta.com/pricing) supports the dated hosted cost
  statements.
- **B:** [Letta Code `v0.26.2`](https://github.com/letta-ai/letta-code/tree/v0.26.2),
  [release](https://github.com/letta-ai/letta-code/releases/tag/v0.26.2), and
  [Apache-2.0 license](https://github.com/letta-ai/letta-code/blob/v0.26.2/LICENSE)
  pin the current CLI/runtime source. The official
  [deployment repository](https://github.com/letta-ai/letta-app-server-deployment/tree/a12e5076f6ec368b3316ba9a65e8716fc9f499eb)
  pins the deployment example reviewed.
- **B:** [legacy server `0.16.8`](https://github.com/letta-ai/letta/releases/tag/0.16.8)
  identifies the excluded historical implementation; no legacy feature is
  credited to the current runtime.

### Supermemory and Cognee

- **B:** [Supermemory server `v0.0.8`](https://github.com/supermemoryai/supermemory/tree/server-v0.0.8),
  [MIT license](https://github.com/supermemoryai/supermemory/blob/server-v0.0.8/LICENSE),
  [self-host quickstart](https://github.com/supermemoryai/supermemory/blob/main/apps/docs/self-hosting/quickstart.mdx),
  [SDK](https://supermemory.ai/docs/integrations/supermemory-sdk),
  [filtering](https://docs.supermemory.ai/memory-api/features/filtering),
  [profiles](https://supermemory.ai/docs/concepts/user-profiles), and
  [SMFS mount](https://supermemory.ai/docs/smfs/mount) support its screen.
  [Pricing](https://supermemory.ai/pricing/) and
  [privacy](https://supermemory.ai/privacy/) support only the dated commercial
  and legal statements.
- **B:** [Cognee `v1.5.3`](https://github.com/topoteretes/cognee/tree/v1.5.3),
  [Apache-2.0 license](https://github.com/topoteretes/cognee/blob/v1.5.3/LICENSE),
  [datasets](https://docs.cognee.ai/core-concepts/further-concepts/datasets),
  [dataset permissions](https://docs.cognee.ai/core-concepts/multi-user-mode/permissions-system/datasets),
  [cognify API](https://docs.cognee.ai/api-reference/cognify/cognify),
  [memify/improve](https://docs.cognee.ai/core-concepts/main-operations/legacy-operations/memify),
  [REST deployment](https://docs.cognee.ai/guides/deploy-rest-api-server), and
  [security](https://docs.cognee.ai/setup-configuration/security) support its
  screen. [Pricing](https://www.cognee.ai/pricing) supports only the dated
  commercial statements.
