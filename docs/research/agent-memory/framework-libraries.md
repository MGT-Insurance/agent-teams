# Framework-native and harness-native memory

Access date: **2026-08-26**

This track evaluates memory that ships with an agent framework, provider, or coding harness. It does not evaluate turnkey memory platforms or databases.

The profiles do not select a winner. They identify shipped behavior, application-owned behavior, adapter work, risks, and unknowns.

## Method and evidence

The required capabilities are the approved R1-R7 contract:

- **R1:** role-scoped memory shared by all instances of that role.
- **R2:** concurrent contribution across sessions and machines, with defined convergence behavior.
- **R3:** automatic availability at startup, resume, compaction recovery, and subagent start.
- **R4:** bounded curation with deduplication, conflicts, provenance, and recovery.
- **R5:** a token-budgeted hot set plus a larger searchable pool.
- **R6:** a stable boundary across Claude Code, Codex, and a neutral client.
- **R7:** application and outcome telemetry that can guide retention and promotion.

Gate values use `pass`, `partial`, `fail`, or `unknown`. A partial result names the application code that must close the gap.

Evidence grades follow the approved plan:

- **A:** behavior or an interface was reproduced from a pinned artifact and paired with primary documentation or source.
- **B:** current official documentation, source, release notes, generated types, or a primary paper supports the claim.
- **C:** a maintainer issue, discussion, or roadmap supports only a directional claim.
- **D:** marketing or secondary material identifies a lead only. No D evidence supports a result in this file.

Package metadata identifies versions and files. The evaluation uses published code, generated interfaces, official documentation, and local probes for behavior.

## Version ledger

All package files came from the official PyPI file service. The SHA-256 values pin the exact wheels inspected locally.

| Ecosystem | Published artifacts inspected | Pin | License | Maturity signal |
|---|---|---|---|---|
| LangGraph and LangMem | `langgraph` 1.2.11, `langgraph-checkpoint` 4.2.0, `langgraph-checkpoint-postgres` 3.1.2, `langmem` 0.0.30 | Wheel SHA-256 prefixes `8bab70de`, `0547fd22`, `6a7e38ef`, `142f0400`. LangGraph tag commit `644815f9`. LangMem main commit `29cbe41e`. | MIT | LangGraph is 1.x. LangMem remains 0.0.x and has no GitHub release record. |
| LlamaIndex | `llama-index-core` 0.14.24 | Wheel `a12f39b8`. Tag commit `9ba74b86`. | MIT | Current memory API is pre-1.0. The older `ChatMemoryBuffer` is deprecated in its published source. |
| Semantic Kernel | `semantic-kernel` 1.44.1 | Wheel `95bd49c3`. Tag commit `6e102255`. | MIT | The Python legacy memory layer is deprecated. Vector stores and C# agent memory have separate preview status. |
| AutoGen | `autogen-core`, `autogen-agentchat`, and `autogen-ext` 0.7.5 | Wheels `4f4a0d3b`, `d19ca8e`, `18cecc8a`. Tag commit `83afbf58`. | MIT | The Python memory protocol is pre-1.0. Redis memory first appeared in the 0.7 line. |
| OpenAI managed sessions | `openai` Python SDK 3.4.0 generated resources and types | Wheel `b641bb1e` | Apache-2.0 for the SDK | The HTTP API is managed. Data retention and feature availability depend on account controls. |
| Claude Code auto memory | Claude Code documentation, feature floor 2.1.59 | Documentation pin is the access date | Proprietary harness | Auto memory is a current harness feature and uses machine-local Markdown files. |

Full hashes and upload times appear in the evidence ledger.

## Candidate screening

| Candidate | Shipped memory surface | Screen result | Deep profile | Reason |
|---|---|---|---|---|
| LangGraph plus LangMem | Namespaced JSON store, checkpointed threads, semantic search, memory managers, and background reflection | Included | Yes | It can own persistence, retrieval, and part of curation while preserving application-defined namespaces. |
| LlamaIndex | SQL-backed session history, token-waterfall `Memory`, and composable static, fact, and vector blocks | Included | Yes | It can own context budgeting and memory-block orchestration. |
| Semantic Kernel | Vector-store abstractions, deprecated memory stores, and experimental C# agent context providers | Included with edition and language separation | Yes | It has broad runtime support, but its memory surfaces are in migration. |
| AutoGen | `Memory` protocol, automatic agent context updates, list, Chroma, Redis, Mem0, and canvas implementations | Included | Yes | It exposes a small adapter contract and an explicit retrieval event. |
| OpenAI Conversations and Responses | Provider-managed conversation items, response chaining, compaction, and server-side state | Screen only | No | This is session state, not a portable role-memory lifecycle. |
| Claude Code auto memory | Per-repository Markdown index, topic files, startup loading, and compaction reload | Screen only | No | This is useful harness memory, but it is machine-local and Claude Code-specific. |

### Exclusions and boundaries

- Mem0 behavior is excluded here. Semantic Kernel, AutoGen, and LlamaIndex only receive credit for their Mem0 adapter surfaces.
- PostgreSQL, Redis, Chroma, and other raw stores are excluded. This file evaluates only the framework contract layered above them.
- LangSmith and generic OpenTelemetry backends are excluded as quality platforms. Native events and trace seams still count as framework interfaces.
- Legacy OpenAI Assistants threads are excluded because current documentation labels Assistants as legacy.
- Legacy LlamaIndex `ChatMemoryBuffer` is excluded from the deep result. The current `Memory` class replaces it.
- Semantic Kernel `MemoryStoreBase` and `SemanticTextMemory` are migration evidence only. Their published Python classes carry deprecation markers.
- Provider-managed conversation history does not receive R6 credit without a neutral export and behavior boundary.
- No roadmap claim changes a gate. Roadmap-only behavior remains unknown.

## Deep profile: LangGraph plus LangMem

### Published interfaces

LangGraph separates thread state from cross-thread memory. A checkpointer owns thread snapshots. A `BaseStore` owns long-term JSON documents.

`BaseStore` exposes sync and async operations:

```text
batch / abatch
get / aget
search / asearch
put / aput
delete / adelete
list_namespaces / alist_namespaces
```

Each item uses a hierarchical `tuple[str, ...]` namespace, a key, a JSON value, timestamps, and an optional search score.

The current PostgreSQL store adds setup migrations, vector search, TTL configuration, and a TTL sweeper. The application selects and operates that backend.

LangGraph injects the store through `Runtime.store` inside graph nodes and tools. This injection removes framework-local plumbing but does not serve external harnesses.

LangMem adds two curation paths:

- Memory tools let an agent manage and search memory in the active request path.
- `create_memory_store_manager` extracts, inserts, updates, and optionally deletes structured memories.
- `ReflectionExecutor` schedules the manager in a local thread or a remote LangGraph run.
- Manager schemas can represent profiles, collections, episodic records, or application-defined models.

The memory manager uses model judgment. Inserts and updates are enabled by default. Deletes are disabled by default.

### R1-R7 profile

| Requirement | Result | Evidence and retained work |
|---|---|---|
| R1 role scope | **Partial** | Namespace tuples can encode `("roles", role, tier)`. The application must derive the role and enforce namespace authorization. Local namespace isolation was reproduced. Grade A. |
| R2 shared contribution | **Partial** | PostgreSQL can provide a shared process boundary. Backend transactions, update conflicts, retries, and cross-machine failure behavior remain application and backend concerns. Grade B. |
| R3 automatic availability | **Partial** | LangGraph injects stores into nodes and resumes thread state through checkpointers. Claude Code and Codex startup, compaction, and subagent delivery need `ateam` hooks. Grade B. |
| R4 bounded curation | **Partial** | LangMem can extract, update, delete, and schedule background reflection. It does not ship the required provenance ledger, reversible conflict policy, or hot-budget transaction. Grade B. |
| R5 hot plus searchable | **Partial** | `BaseStore.search` supports filters, limits, and optional vector search. The application must select a hot set by tokens and preserve a cold tier. Grade A/B. |
| R6 harness portability | **Partial** | Python and JavaScript expose similar stores. `BaseStore` is an in-process library interface, not a neutral wire contract. A CLI or service adapter remains necessary. Grade B. |
| R7 usefulness telemetry | **Partial** | LangSmith traces can observe manager and tool execution. No inspected interface records a stable memory identity, application outcome, and retention decision together. Grade B. |

### Lifecycle, retrieval, and observability

The framework owns store CRUD, filtering, optional vector ranking, thread checkpoints, runtime injection, and manager scheduling. LangMem owns model-driven extraction and consolidation.

The application owns role identity, tier policy, token accounting, source provenance, conflict review, recovery, and outcome attribution. It also owns backend availability.

Retrieval control is explicit. Callers provide a namespace, structured filter, query, limit, and offset. Indexed fields are selected during `put` or store configuration.

Observability is framework-coupled. LangMem imports LangSmith tracing for prompt optimization. The store contract itself has no applied-memory event.

### Exact `ateam` adapter surface

| `ateam` contract | LangGraph or LangMem mapping | Additional adapter behavior |
|---|---|---|
| `learn <role> <slug>` | `store.put(("roles", role, "fresh"), slug, value)` | Add source, author, host, revision, and stable identity fields. Reject role mismatch before the store call. |
| `learnings <role>` | `store.search(("roles", role), filter={"tier": ["fresh", "hot"]})` or two searches | Apply deterministic tier order and a token budget. Emit plain text for harness injection. |
| `recall <role> <query>` | `store.search(("roles", role), query=query, limit=n)` | Apply a token budget after ranking. Preserve IDs and scores for later application events. |
| `applied <role> <id>` | No first-party mapping | Write an atomic application-event record with run, query, exposure, outcome, and timestamp. |
| `forget <role> <tier> <slug>` | `store.delete(("roles", role, tier), slug)` | Record a tombstone or history pointer before deletion. |
| `condense-check` and condense | LangMem manager plus `ReflectionExecutor` | Add an advisory lock, snapshot, deterministic budget check, conflict ledger, and reversible tier transaction. |
| startup and subagent load | No external-harness mapping | Keep the current CLI as the stable boundary. Claude Code and Codex hooks call it and detect empty or truncated output. |

Using LangGraph directly inside each harness would couple startup to Python or JavaScript runtime setup. A small service or CLI preserves the current harness boundary.

### Licensing, maturity, and migration risk

The code is MIT licensed. LangGraph is 1.x, but its checkpoint and store packages have separate major versions.

LangMem remains 0.0.30. Its package requires LangGraph, LangChain, provider SDKs, LangSmith, and `trustcall`, which increases upgrade coordination.

Migration can preserve current role and tier fields in namespace segments. Rollback can dual-write the current Dolt store and LangGraph store before read cutover.

The main rollback risk is semantic drift in model-driven consolidation. Store snapshots do not prove that a merge preserved all prior meaning.

Unknowns:

- Atomic compare-and-set behavior for concurrent updates was not found in the inspected `BaseStore` contract.
- No local PostgreSQL probe measured duplicate writes, conflicting writes, or TTL interaction.
- JavaScript parity for LangMem curation was not established. LangMem itself is published as a Python package.
- No native export preserves an application-defined conflict ledger because that ledger does not exist by default.

## Deep profile: LlamaIndex

### Published interfaces

The current `Memory` class combines a SQL-backed message queue with ordered memory blocks. Its default SQL store uses in-memory SQLite.

`Memory.from_defaults` accepts these relevant controls:

```text
session_id
token_limit
token_flush_size
chat_history_token_ratio
memory_blocks
chat_store or SQLAlchemy connection values
insert_method
```

The memory keeps active chat messages in `SQLAlchemyChatStore`. When the active queue exceeds its reserved token share, the memory archives older messages.

Archived messages flow to each block with `accept_short_term_memory=True`. Blocks can return text, content blocks, or chat messages.

Current core blocks are:

- `StaticMemoryBlock`, for fixed injected context.
- `FactExtractionMemoryBlock`, for model-extracted facts and fact condensation.
- `VectorMemoryBlock`, for vector insertion and query-time retrieval.

Block priority controls truncation. Priority zero means that the framework does not truncate that block.

The SQL chat store exposes session-keyed add, list, archive, delete, and dump operations. It does not define role semantics.

### R1-R7 profile

| Requirement | Result | Evidence and retained work |
|---|---|---|
| R1 role scope | **Partial** | `session_id` and store keys can encode a role. The framework does not enforce role identity or share one role across chosen sessions automatically. Grade A/B. |
| R2 shared contribution | **Partial** | A shared SQL store can serve multiple processes. Queue ordering, concurrent archive operations, and cross-machine convergence were not specified or reproduced. Grade B. |
| R3 automatic availability | **Partial** | `Memory.aget` injects blocks into a system or user message for LlamaIndex agents. External harness startup and compaction delivery remain application-owned. Grade A. |
| R4 bounded curation | **Partial** | Token pressure archives messages. Fact blocks can extract and condense facts. Required provenance, duplicate policy, conflict review, and reversible eviction are absent. Grade B. |
| R5 hot plus searchable | **Partial** | The memory enforces a total token limit and can query vector blocks. It does not expose a stable hot-tier and cold-pool contract for external clients. Grade A/B. |
| R6 harness portability | **Partial** | Python exposes the verified `Memory` surface. TypeScript exists, but equivalent block behavior was not established. There is no neutral memory wire protocol. Grade B. |
| R7 usefulness telemetry | **Fail as shipped** | The inspected memory and chat-store modules emit no memory application event. Applications can wrap `aget`, but the feature is not provided. Grade A/B. |

### Lifecycle, retrieval, and observability

LlamaIndex owns token counting, queue pressure, message archiving, block ordering, block truncation, block injection, and SQL session storage.

The application owns role namespaces, shared-session policy, durable database operation, stable memory IDs, provenance, conflicts, and outcome events.

Retrieval differs by block. `VectorMemoryBlock` controls vector-store query mode, similarity count, metadata filters, and formatting.

`FactExtractionMemoryBlock` owns a fact list and a model prompt. When the fact count grows, the block can condense the list. This is not a conflict ledger.

No callback, dispatcher, trace, or event seam appeared in the inspected `core/memory` modules. Agent-level instrumentation can still wrap memory calls.

### Exact `ateam` adapter surface

| `ateam` contract | LlamaIndex mapping | Additional adapter behavior |
|---|---|---|
| `learn <role> <slug>` | No uniform per-memory CRUD mapping in `Memory` | Use a custom `BaseMemoryBlock` or separate record store. Preserve the slug and provenance outside chat messages. |
| `learnings <role>` | `Memory.aget` with role-derived `session_id` | Filter the returned memory block content and enforce the startup token budget. Do not return ordinary chat history as role guidance. |
| `recall <role> <query>` | `VectorMemoryBlock.aget(messages, query=...)` through `Memory.aget` | Return stable IDs and scores. The built-in formatted context hides part of this boundary. |
| `applied <role> <id>` | No first-party mapping | Emit a separate atomic event after a returned memory enters a harness prompt. |
| `forget <role> <tier> <slug>` | No uniform block method | Add block-specific delete by stable ID. `Memory.areset` is too broad for one memory. |
| `condense-check` and condense | Queue waterfall plus fact condensation | Add duplicate and conflict policy, provenance, review records, and transactional tier movement. |
| startup and subagent load | No external-harness mapping | Keep the `ateam` CLI and serialize selected block output as plain text. |

The current `Memory` interface is useful inside a LlamaIndex agent. A direct CLI adapter must bypass or extend it for stable per-entry management.

### Licensing, maturity, and migration risk

LlamaIndex core is MIT licensed and currently 0.14.24. The package contains a broad dependency set and separately versioned integrations.

The source marks `ChatMemoryBuffer` as deprecated and directs callers to `Memory`. This active migration raises rollback and compatibility risk.

The SQL chat store can dump its records. Memory blocks have different storage and export behavior, so one complete export requires block-specific code.

Dual-write can preserve the current Dolt entries while a custom block receives new events. Read cutover must compare token selection and stable IDs.

Unknowns:

- Transaction behavior during concurrent queue archive and block writes was not reproduced.
- A crash between message archive and block insertion can create a recovery question. The inspected code does not expose one transaction across both operations.
- TypeScript parity for current memory blocks is unknown.
- Fact condensation quality and recoverability require a model-based validation corpus.

## Deep profile: Semantic Kernel

### Published interfaces

Semantic Kernel currently has three distinct memory-related surfaces. They must not be treated as one stable API.

1. Python legacy memory stores expose collection CRUD and vector nearest-match operations. Published classes carry deprecation markers.
2. Current vector-store abstractions expose typed collections, upsert, get, delete, vector search, hybrid search, and schema controls.
3. Experimental C# agent memory adds `AIContextProvider` implementations to an `AgentThread`.

The C# `Mem0Provider` sends messages to Mem0 and queries Mem0 before agent invocation. This profile credits only the provider adapter.

The C# `WhiteboardProvider` extracts requirements, proposals, decisions, and actions. It injects the whiteboard into later agent calls.

Official documentation marks agent memory experimental. It marks vector-store support as release candidate or preview, depending on language and connector.

Current Python wheel 1.44.1 does not contain `WhiteboardProvider`, `Mem0Provider`, or an `AIContextProvider` path. The documented agent-memory feature is C#-specific.

### R1-R7 profile

| Requirement | Result | Evidence and retained work |
|---|---|---|
| R1 role scope | **Partial** | Collections and typed fields can encode roles. C# Mem0 options expose user and thread scopes, but that storage belongs to Mem0. Role enforcement remains application-owned. Grade B. |
| R2 shared contribution | **Partial** | Connectors can use shared databases. Their concurrency and consistency differ by backend. The framework does not define cross-backend convergence. Grade B. |
| R3 automatic availability | **Partial** | C# `AgentThread.AIContextProviders` update agent context automatically. Python and external harnesses need separate adapters. Grade B. |
| R4 bounded curation | **Partial** | Whiteboard extraction retains selected conversational facts. It is experimental and does not establish provenance, conflicts, recovery, or a durable role pool. Grade B. |
| R5 hot plus searchable | **Partial** | Context providers can inject selected context. Vector stores can search larger collections. No unified token-budgeted two-tier lifecycle was found. Grade B. |
| R6 harness portability | **Partial** | Vector connectors can run outside the core kernel. Agent-memory behavior differs across C#, Python, and Java, and no neutral wire contract exists. Grade B. |
| R7 usefulness telemetry | **Unknown** | Semantic Kernel supports observability, but the inspected memory surfaces do not establish a stable applied-memory outcome event. Grade B. |

### Lifecycle, retrieval, and observability

Vector-store abstractions own typed record CRUD, vector generation hooks, filtering, vector search, and hybrid search. Connector capabilities remain backend-specific.

The application owns role schemas, curation, tier budgets, conflict handling, export policy, and harness delivery. C# context providers own only framework-local injection.

General Semantic Kernel observability does not equal usefulness telemetry. A trace can show an agent call without proving which memory changed the outcome.

### Exact `ateam` adapter surface

| `ateam` contract | Semantic Kernel mapping | Additional adapter behavior |
|---|---|---|
| `learn <role> <slug>` | `VectorStoreCollection.upsert` with a typed record | Define stable key, role, tier, text, source, revision, and timestamps as filterable fields. |
| `learnings <role>` | `search` or `get` from a role collection | Select deterministic fresh and hot records by tokens. Format them for CLI output. |
| `recall <role> <query>` | `VectorStoreCollection.search` or `hybrid_search` | Normalize connector scores and return stable record IDs. |
| `applied <role> <id>` | No established memory mapping | Add a separate event collection with atomic increments or append-only events. |
| `forget <role> <tier> <slug>` | `VectorStoreCollection.delete(key)` | Write a tombstone and preserve export history before deletion. |
| `condense-check` and condense | No portable built-in mapping | A C# Whiteboard provider cannot replace durable role curation. Add the complete curation transaction. |
| startup and subagent load | C# only through `AIContextProviders` | Keep the CLI boundary for Claude Code and Codex. Python must use custom context plumbing. |

### Licensing, maturity, and migration risk

Semantic Kernel is MIT licensed and has C#, Python, and Java SDKs. Feature parity differs by language.

The Python wheel contains deprecated memory stores and current vector stores together. Official migration guidance recommends vector stores over legacy memory stores.

The C# agent-memory feature is experimental. It also delegates long-term behavior to Mem0, which creates a separate platform dependency.

When records use an application-owned schema and backend, rollback is possible. When behavior depends on provider-managed Mem0 extraction, rollback is harder.

Unknowns:

- A stable Python equivalent for C# agent memory was not found in version 1.44.1.
- Connector compatibility and status differ by language and database.
- Whiteboard export, revision history, and conflict behavior were not established.
- The current transition from legacy memory stores to vector stores can require schema migration or re-embedding.

## Deep profile: AutoGen

### Published interfaces

AutoGen defines a small Python `Memory` abstract class:

```text
update_context(model_context) -> UpdateContextResult
query(query, cancellation_token, **kwargs) -> MemoryQueryResult
add(content, cancellation_token)
clear()
close()
```

`MemoryContent` carries content, MIME type, and optional metadata. `MemoryQueryResult` returns selected memory content.

`AssistantAgent` calls each memory implementation before model inference. When an update returns memory results, the agent emits a `MemoryQueryEvent`.

Core includes `ListMemory`, which appends all entries in chronological order. Extensions include persistent Chroma and Redis implementations.

Chroma configuration exposes a collection name, persistence path or HTTP client, `k`, score threshold, and embedding configuration. Redis exposes an index and prefix.

The base protocol omits update, delete-one, list, export, namespace, transaction, and version methods. Implementations can add their own operations.

### R1-R7 profile

| Requirement | Result | Evidence and retained work |
|---|---|---|
| R1 role scope | **Partial** | A memory instance, Chroma collection, or Redis prefix can represent one role. The protocol does not enforce role identity or shared-instance naming. Grade A/B. |
| R2 shared contribution | **Partial** | Redis and HTTP Chroma can serve multiple processes. The protocol does not define conflict, ordering, or convergence behavior. Grade B. |
| R3 automatic availability | **Partial** | `AssistantAgent` calls `update_context` before inference and emits query events. Harness startup, resume, compaction, and subagent delivery remain external. Grade A. |
| R4 bounded curation | **Fail as shipped** | The protocol provides add and clear, but no update, delete-one, deduplication, conflict, provenance, promotion, or recovery contract. Grade A. |
| R5 hot plus searchable | **Partial** | Chroma and Redis support top-k retrieval and thresholds. Model-context classes can limit chat context. No first-party hot and cold tier lifecycle connects them. Grade B. |
| R6 harness portability | **Partial** | The Python ABC is implementation-neutral but in-process. AutoGen supports Python and .NET runtimes, but memory-interface parity was not established. Grade B. |
| R7 usefulness telemetry | **Partial** | `MemoryQueryEvent` records retrieved content and agent source. OpenTelemetry traces agents and tools. Outcome attribution and retention feedback remain application-owned. Grade A/B. |

### Lifecycle, retrieval, and observability

AutoGen owns the call point before inference, the memory result envelope, and the `MemoryQueryEvent`. Each implementation owns storage and ranking.

The application owns memory extraction, role scope, stable IDs, updates, deletes, curation, tier budgets, provenance, conflicts, and outcome events.

Retrieval control is implementation-specific through `**kwargs`. Chroma exposes top-k and threshold controls. This flexibility weakens portability between memory implementations.

AutoGen supports OpenTelemetry for agent and tool execution. The memory query event is more direct than a generic trace, but it has no outcome field.

### Exact `ateam` adapter surface

| `ateam` contract | AutoGen mapping | Additional adapter behavior |
|---|---|---|
| `learn <role> <slug>` | `Memory.add(MemoryContent(...))` | Store the role, tier, slug, provenance, and stable ID in metadata. Reject implementations that discard metadata. |
| `learnings <role>` | `Memory.query` or a role-specific memory instance | Add deterministic hot selection and a token budget. `ListMemory` alone returns all entries. |
| `recall <role> <query>` | `Memory.query(query, **backend_controls)` | Normalize scores, IDs, filters, and limits across implementations. |
| `applied <role> <id>` | Start from `MemoryQueryEvent` | Add run identity, exposure, selected memory ID, outcome, and an atomic event sink. |
| `forget <role> <tier> <slug>` | No base protocol method | Extend the adapter with delete-one. Do not map this command to `clear()`. |
| `condense-check` and condense | No base protocol method | Implement extraction, merge, conflict review, budget, lock, and recovery outside AutoGen. |
| startup and subagent load | `AssistantAgent` only | Keep the `ateam` CLI for Claude Code, Codex, and neutral clients. AutoGen agents can consume the same service. |

### Licensing, maturity, and migration risk

AutoGen Python packages are MIT licensed and currently 0.7.5. Memory configuration serializes as AutoGen components, which adds framework coupling.

The minimal protocol is easy to wrap, but it does not retire the high-maintenance curation layer. Backend-specific `**kwargs` can also leak through adapters.

A migration can use one role-specific memory collection while dual-writing current Dolt records. Rollback requires a neutral export of content and metadata.

Unknowns:

- The .NET memory API was not shown to match Python `Memory`.
- Redis and Chroma consistency under concurrent update and deletion was not tested.
- Stable update and delete operations are not part of the base protocol.
- `MemoryQueryEvent` shows retrieval, not successful use or outcome quality.

## Provider-managed and harness-managed facilities

These facilities are screened separately because their ownership and portability differ from libraries.

### OpenAI Conversations and Responses

The generated Python SDK 3.4.0 exposes conversation create, retrieve, metadata update, delete, and item resources. Responses accept either `conversation` or `previous_response_id`.

When a response uses `conversation`, the API prepends existing items. It adds new input and output items after completion.

Official data controls state that `/v1/conversations` retains application state until deletion. The endpoint is not Zero Data Retention eligible.

This facility owns durable conversation state and server operation. The application owns role scope, semantic extraction, curation, hot selection, search, and usefulness events.

The facility is language-neutral through HTTP but provider-specific in behavior. It does not satisfy the neutral cross-harness boundary in R6.

Export can list conversation items as provider objects. A complete neutral role-memory export still needs an application schema and transformation.

Screen result: useful for provider session continuity, but not a standalone replacement for role memory.

### Claude Code auto memory

Claude Code has user-authored `CLAUDE.md` files and model-authored auto memory. Both load at the start of a conversation.

Auto memory stores plain Markdown under a per-repository machine-local directory. Worktrees share that directory, but different machines do not.

The harness loads the first 200 lines or 25 KB of `MEMORY.md`, whichever comes first. It reads topic files on demand.

Project-root instructions and auto memory reload after compaction. Subagents can maintain separate persistent memory.

This facility owns startup loading, compaction reload, local file access, and the hot index limit. Markdown supports direct inspection, edit, delete, and export.

The facility does not define cross-machine sync, role-shared namespaces, concurrent merge, cold search ranking, or applied-memory outcomes.

It is Claude Code-specific. Codex and neutral clients need a separate path, so it cannot satisfy R6 alone.

Screen result: useful as a harness cache or delivery path, but not as the portable source of truth.

## Cross-candidate ownership matrix

| Behavior | LangGraph and LangMem | LlamaIndex | Semantic Kernel | AutoGen | OpenAI sessions | Claude Code auto memory |
|---|---|---|---|---|---|---|
| Durable store contract | Framework store plus chosen backend | SQL chat store plus block-specific stores | Vector connector plus chosen backend | Implementation-specific | Provider-owned | Local Markdown files |
| Role namespace | Application | Application | Application | Application | Application metadata | Repository and subagent scope only |
| Automatic framework injection | LangGraph runtime | `Memory.aget` | C# context providers | `AssistantAgent` | Response conversation | Harness startup and compaction |
| External harness injection | Application | Application | Application | Application | Application | Claude Code only |
| Token budget | Application for long-term memory | Framework total memory budget | Application | Application | Provider context management, not role hot set | Fixed startup index limit |
| Search control | Namespace, filter, query, limit | Block-specific | Connector-specific | Implementation-specific | Conversation item access, not semantic role search | File tools, no ranking contract |
| Background curation | LangMem reflection | Queue waterfall and block processing | Experimental whiteboard path | Application | None for semantic memory | Model-authored local notes |
| Conflict and recovery ledger | Application | Application | Application | Application | Application | Git or file backup if added by user |
| Applied event | Application trace only | None | Unknown | `MemoryQueryEvent` | API usage metadata only | UI indication, no stable outcome event |
| Neutral export | Application JSON | Block-specific | Application schema | Application schema | Provider item transformation | Markdown copy |

No candidate owns the complete R1-R7 lifecycle. Each framework moves a different subset of current maintenance behind an in-process interface.

## Stable adapter required by `ateam`

A framework change must preserve one neutral contract. Harnesses must not import framework packages directly.

The minimum adapter operations are:

```text
upsert(role, tier, slug, content, provenance, expected_revision?) -> memory_id, revision
get(role, tier, slug) -> memory_record?
select_hot(role, token_budget) -> ordered memory_record[]
search(role, query, token_budget, filters?) -> ranked memory_record[]
record_application(role, memory_id, run_id, query_id, outcome?) -> event_id
delete(role, tier, slug, expected_revision?) -> tombstone
curation_status(role) -> pending_count, lock_state, last_revision
curate(role, policy_revision, dry_run) -> reviewable changeset
export(role?) -> versioned neutral stream
import(stream, mode) -> validation report
```

The existing CLI remains the initial wire boundary:

```text
ateam learn <role> <slug>
ateam learnings <role>
ateam recall <role> <query>
ateam applied <role> <memory-id>
ateam forget <role> <tier> <slug>
ateam condense-check <role>
```

The adapter must preserve these startup properties:

- Output is deterministic plain text with a measured token budget.
- An empty result differs from a failed read.
- Every returned memory carries a stable identity outside the injected text.
- Claude Code and Codex hooks detect truncation and missing delivery.
- The source of truth does not depend on either harness process.
- Writes use revision checks or append-only events to avoid silent lost updates.
- Curation produces a reviewable changeset before destructive deletion.

LangGraph has the closest direct mapping for CRUD and search. AutoGen has the clearest framework-local retrieval event.

LlamaIndex owns the strongest explicit context budget in this set. Semantic Kernel offers broad connector and language coverage, with significant maturity caveats.

Those observations describe interface coverage. They are not a product ranking or recommendation.

## Disposable local verification

The probes used isolated Python 3.12 environments. They did not use provider credentials or modify the repository.

### LangGraph namespaced CRUD and search

Command:

```bash
uv run --isolated --python 3.12 --with langgraph-checkpoint==4.2.0 python probe.py
```

The disposable script called `put`, `get`, filtered `search`, `list_namespaces`, and `delete` on `InMemoryStore`.

Observed result:

```text
value={'text': 'Use rg for file search', 'source': 'probe'}
found_keys=['prefer-rg']
namespaces=[('roles', 'implementer', 'hot')]
after_delete=None
```

This probe supports interface and namespace claims only. It does not support persistent concurrency or recovery claims.

### LlamaIndex memory injection

Command:

```bash
uv run --isolated --python 3.12 --with llama-index-core==0.14.24 python probe.py
```

The script created `Memory` with `session_id="role:implementer"`, a 128-token limit, and one `StaticMemoryBlock`.

Observed result:

```text
roles=['MessageRole.SYSTEM', 'MessageRole.USER']
system='<memory><hot>Use rg for file search</hot></memory>'
stored_count=1
```

Whitespace was normalized in the displayed system message. The actual output used line breaks around the XML-like tags.

This probe supports SQL session storage and memory-block injection. It does not support fact extraction, vector quality, or concurrent archive claims.

### AutoGen context update

Command:

```bash
uv run --isolated --python 3.12 --with autogen-core==0.7.5 python probe.py
```

The script added one `MemoryContent` item to `ListMemory`. It called `update_context` on `BufferedChatCompletionContext` and then cleared memory.

Observed result:

```text
query_result_count=1
context=['Relevant memory content (in chronological order): 1. Use rg for file search']
after_clear=0
```

This probe supports context-update behavior. It does not support persistent storage, ranking, or curation claims.

### Published interface inspection

The current wheels were unpacked without importing dependencies. Source inspection established these additional facts:

- LangGraph `BaseStore` has sync and async CRUD, search, batch, and namespace-list methods.
- LangMem includes local and remote reflection executors and stateful store managers.
- LlamaIndex `Memory` uses SQL chat storage, token pressure, archive status, and memory blocks.
- Semantic Kernel Python 1.44.1 marks legacy memory classes deprecated and includes current vector-store classes.
- AutoGen `AssistantAgent` calls memory before inference and creates `MemoryQueryEvent` values.
- OpenAI SDK 3.4.0 includes generated conversation resources and Response conversation fields.

## Migration and rollback risks

| Risk | Affected candidates | Required control |
|---|---|---|
| In-process framework coupling reaches harness startup | All portable libraries | Keep `ateam` as the neutral CLI or service. Do not import framework packages in hooks. |
| Backend semantics leak through a generic adapter | LangGraph, Semantic Kernel, AutoGen | Freeze score, filter, revision, and error semantics in the neutral contract. |
| Model curation changes meaning | LangMem, LlamaIndex facts, Semantic Kernel whiteboard | Store source records, prompts, model pins, changesets, and reversible revisions. |
| Per-language feature drift | LangGraph, Semantic Kernel, AutoGen, LlamaIndex | Select one server implementation and test every harness against its wire contract. |
| Framework API migration | LlamaIndex, Semantic Kernel | Pin packages, keep dual reads, and test export before each upgrade. |
| Provider retention or outage | OpenAI sessions, Mem0 adapters | Keep provider features outside the source-of-truth path unless a neutral backup exists. |
| Incomplete export | Every block-based or provider-managed option | Round-trip roles, tiers, provenance, revisions, conflicts, tombstones, and application events. |
| Silent lost update | All shared backends | Require expected revisions, append-only events, or backend transactions. |

A reversible migration has four stages:

1. Export current memories to the neutral schema without changing reads.
2. Dual-write new events with stable IDs and compare revisions.
3. Shadow-select hot and cold results while the current output remains authoritative.
4. Change reads only after round-trip, concurrency, delivery, and outcome checks pass.

Rollback returns reads to Dolt and replays neutral events written after dual-write began. Framework-native state must never be the only record during validation.

## Evidence ledger

Every source below was accessed on 2026-08-26.

| ID | Candidate and pin | Primary source | Type | Grade | Claim supported |
|---|---|---|---|---|---|
| E01 | LangGraph 1.2.11 | [PyPI release](https://pypi.org/project/langgraph/1.2.11/) | Published artifact | A | Version, Python floor, dependencies, and MIT expression. Wheel SHA-256 `8bab70de7b2d00b5300fb289bcf38d8b241400f3184c1e95e8ce706fb0e8686b`, uploaded 2026-08-11. |
| E02 | LangGraph checkpoint 4.2.0 | [PyPI release](https://pypi.org/project/langgraph-checkpoint/4.2.0/) | Published artifact | A | Current `BaseStore` artifact. Wheel SHA-256 `0547fd228935a0b758865de3a3d6d7a2537c308895d0f9ab092ce9151b5da942`, uploaded 2026-08-07. |
| E03 | LangGraph | [Python persistence guide](https://docs.langchain.com/oss/python/langgraph/persistence) | Official documentation | B | Checkpointers, stores, cross-thread namespaces, and persistent backend guidance. |
| E04 | LangGraph | [JavaScript persistence guide](https://docs.langchain.com/oss/javascript/langgraph/persistence) | Official documentation | B | JavaScript store parity at the documented operation level. |
| E05 | PostgreSQL store 3.1.2 | [PostgresStore reference](https://reference.langchain.com/python/langgraph.store.postgres/base/PostgresStore) | Official API reference | B | Setup, vector search, TTL, and sweeper behavior. Wheel SHA-256 `6a7e38ef16985b54e356cba7bdaf447943aae33d5aaf290026c593bb6b4a6264`. |
| E06 | LangMem 0.0.30 | [PyPI release](https://pypi.org/project/langmem/0.0.30/) | Published artifact | A | Current package interface and dependencies. Wheel SHA-256 `142f040014493eebd67e1055c0642f9ab38868b5b1fde5c8f2d39add57f4ba5b`. |
| E07 | LangMem, main `29cbe41e` | [Repository](https://github.com/langchain-ai/langmem/tree/29cbe41e58528f92e9efa773c12e15c47be3808c) | Official source | B | Memory tools, native store integration, and license. |
| E08 | LangMem | [Memory API reference](https://langchain-ai.github.io/langmem/reference/memory/) | Official API reference | B | Manager operations, background execution, and delete default. |
| E09 | LlamaIndex core 0.14.24 | [PyPI release](https://pypi.org/project/llama-index-core/0.14.24/) | Published artifact | A | Current wheel and MIT expression. SHA-256 `a12f39b8a777cc526bf400d929016cd244372af61c5e98f6f4186f51a0b9288c`, uploaded 2026-08-19. |
| E10 | LlamaIndex tag `v0.14.24`, `9ba74b86` | [Pinned source](https://github.com/run-llama/llama_index/tree/9ba74b8628712e68d16955d9492b5192bd7e6f00) | Official source | B | `Memory`, block, SQL store, deprecation, and queue behavior. |
| E11 | LlamaIndex | [Artifact editor memory example](https://docs.llamaindex.ai/en/stable/examples/tools/order_completion_agent_with_artifact_editor/) | Official documentation | B | Current `Memory` and external memory-block composition. |
| E12 | Semantic Kernel 1.44.1 | [PyPI release](https://pypi.org/project/semantic-kernel/1.44.1/) | Published artifact | A | Python interfaces, deprecations, and dependencies. Wheel SHA-256 `95bd49c3055710473a46472dfceaeafe11d97a546dc715ee6d5359c2f47ec754`, uploaded 2026-08-06. |
| E13 | Semantic Kernel tag `python-1.44.1`, `6e102255` | [Pinned source](https://github.com/microsoft/semantic-kernel/tree/6e102255f1903916ce97c80f07aae3a771e42ba7) | Official source | B | Python vector and legacy memory implementation. |
| E14 | Semantic Kernel | [Agent memory](https://learn.microsoft.com/en-us/semantic-kernel/frameworks/agent/agent-memory) | Official documentation | B | Experimental C# Mem0 and Whiteboard context providers. |
| E15 | Semantic Kernel | [Vector stores](https://learn.microsoft.com/en-us/semantic-kernel/concepts/vector-store-connectors/) | Official documentation | B | Vector-store abstraction, independent use, and preview status. |
| E16 | Semantic Kernel | [Legacy memory stores](https://learn.microsoft.com/en-us/semantic-kernel/concepts/vector-store-connectors/memory-stores) | Official migration guide | B | Legacy replacement, schema differences, and migration paths. |
| E17 | Semantic Kernel | [Supported languages](https://learn.microsoft.com/en-us/semantic-kernel/get-started/supported-languages) | Official documentation | B | C#, Python, Java, and feature-parity caveats. |
| E18 | AutoGen 0.7.5, `83afbf58` | [Release](https://github.com/microsoft/autogen/releases/tag/python-v0.7.5) | Official release and source | B | Current version, Redis memory change, and MIT license. |
| E19 | AutoGen core 0.7.5 | [PyPI release](https://pypi.org/project/autogen-core/0.7.5/) | Published artifact | A | `Memory` interface. Wheel SHA-256 `4f4a0d3b88a36da75b2ef0d40be2d5e3a207cae7f7d951511e498ad1d68f8ef4`. |
| E20 | AutoGen AgentChat 0.7.5 | [PyPI release](https://pypi.org/project/autogen-agentchat/0.7.5/) | Published artifact | A | Agent memory call point and `MemoryQueryEvent`. Wheel SHA-256 `d19ca8ec26cb15e071a56c4269140aea2bf3c718bdc7e06f6677af9a905815ba`. |
| E21 | AutoGen extensions 0.7.5 | [PyPI release](https://pypi.org/project/autogen-ext/0.7.5/) | Published artifact | A | Chroma, Redis, Mem0, and canvas implementations. Wheel SHA-256 `18cecc8aab37c7c4861fbad038a1017f0ef25e35e273aa158066ccf9d93fea4f`. |
| E22 | AutoGen | [Memory protocol reference](https://microsoft.github.io/autogen/stable/reference/python/autogen_core.memory.html) | Official API reference | B | Base methods, result types, and implementation ownership. |
| E23 | AutoGen | [Memory and RAG guide](https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/memory.html) | Official documentation | B | Agent integration and persistent Chroma example. |
| E24 | AutoGen | [Tracing guide](https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/tracing.html) | Official documentation | B | OpenTelemetry support and scope. |
| E25 | OpenAI SDK 3.4.0 | [PyPI release](https://pypi.org/project/openai/3.4.0/) | Generated SDK artifact | A | Conversation resources and Response fields. Wheel SHA-256 `b641bb1e5a9d977530f451bdc8d01e8f4b23df395d49e5428f8fdbce07ac14f0`, uploaded 2026-08-26. |
| E26 | OpenAI Conversations | [Create conversation API](https://developers.openai.com/api/reference/python/resources/conversations/methods/create) | Official API reference | B | Conversation object, initial items, metadata, and identity. |
| E27 | OpenAI Responses | [Create response API](https://developers.openai.com/api/reference/cli/resources/responses/methods/create) | Official API reference | B | Conversation attachment, response chaining, and automatic item updates. |
| E28 | OpenAI platform | [Data controls](https://developers.openai.com/api/docs/guides/your-data) | Official documentation | B | Conversation and response retention and Zero Data Retention behavior. |
| E29 | Claude Code | [Memory documentation](https://code.claude.com/docs/en/memory) | Official harness documentation | B | Auto memory scope, storage, startup limit, worktree sharing, compaction, and machine locality. |

## Remaining unknowns for the validation join

The next join must resolve these unknowns before any candidate can pass R1-R7:

- Concurrent write and conflict behavior against one selected persistent backend per deep candidate.
- Exact token counts and truncation behavior with current agent-teams memory shapes.
- Stable identity preservation through retrieval, prompt injection, and application events.
- Export and restore of role, tier, provenance, revisions, conflicts, tombstones, and outcome events.
- Claude Code and Codex startup behavior during backend outage, timeout, empty result, and truncated output.
- Cross-language parity where a candidate advertises more than one runtime.
- Model-driven curation quality on duplicate and contradiction fixtures.
- Total retained code for locks, sync, CLI delivery, telemetry, migration, and rollback.

These unknowns block a standalone decision. They do not make a candidate absent or unsuitable by assumption.
