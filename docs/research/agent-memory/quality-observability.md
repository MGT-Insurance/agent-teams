# Agent memory quality, curation, and observability

Status: research evidence for track T5 / Bead `agent-teams-ird0.6`
Repository baseline: `3bb52fad8c23ed5f9149946977f2c997a2cd4310` (`agent-teams` plugin `0.60.0`)
Research and source access date: **2026-08-26**
Scope: methods and maintained integration surfaces for R4, R5, and R7. This
artifact does not select a storage engine, platform, framework, or overall
architecture winner.

## Executive findings

1. **A safe curation pipeline is evidence-preserving.** Exact duplicate
   detection, role filtering, token-budget enforcement, event counting, and
   version checks can be deterministic. Free-text extraction, near-duplicate
   grouping, and contradiction classification can be model-assisted, but a
   model-generated merge is a new derived artifact rather than a replacement
   for its sources. Ambiguous conflict resolution and destructive deletion
   should remain reviewable and reversible.
2. **Retrieval quality and task usefulness are separate measurements.** R5
   needs ranked-retrieval metrics against human-labeled memory IDs, while R7
   needs exposure, application, and independently observed outcome events.
   Neither a retrieval hit nor the current `applied` counter proves that a
   memory improved an outcome.
3. **The reliable cross-harness capture points are outside model prose.** The
   injector can record what it actually delivered, the retrieval boundary can
   record ordered IDs and scores, and the orchestrator can record test/gate or
   task outcomes. Agent self-report is useful as a sparse annotation, not as
   the denominator or sole promotion signal.
4. **Popularity must not become destiny.** Rank position controls exposure;
   exposure controls opportunities to be applied; and naive promotion based on
   raw counts increases future exposure. Retention decisions therefore need an
   exposure denominator, exploration or holdouts, role/task stratification,
   capped count influence, and a novelty/diversity constraint.
5. **Current standards cover operations, not usefulness.** The inspected
   OpenTelemetry GenAI memory span defines create/search/update/upsert/delete
   operations and record identifiers, counts, optional query text, and optional
   record payloads. All memory-specific fields are still marked Development,
   and the schema does not define role, tier, version lineage, exposure,
   application, outcome, or curation-decision semantics. A small project
   namespace is still required even if OTLP is the transport.

## Evidence method

Evidence grades follow the approved contract:

- **A - reproduced:** behavior reproduced from a pinned source, executable, or
  disposable probe and paired with current source or official documentation.
- **B - primary documented:** current official documentation, source, release
  notes, or a primary paper, pinned where possible, but not reproduced here.
- **C - directional:** maintainer discussion/roadmap, or a paper whose transfer
  to this operational-memory use case has not been demonstrated.
- **D - discovery only:** marketing, secondary commentary, or inference; not
  used to pass a requirement.

The report separates observed outcomes from mechanism/design claims. A library
API or telemetry schema proves that a mechanism exists; it does not prove that
the mechanism improves memory quality. A paper result is reported only for the
paper's evaluated task and is not treated as a product benchmark.

## Current memory shape and baseline observations

The source pin above defines the local contract. `memoryRecord` in
[`internal/verbs/query.go`](../../../internal/verbs/query.go) emits:

```text
role, key, slug, tier, body, appliedCount, lastApplied
```

`learnKey` in [`internal/verbs/write.go`](../../../internal/verbs/write.go)
stores hot and fresh as `<role>:hot:<slug>` and `<role>:fresh:<slug>`; cold is
the bare `<role>:<slug>`. `runRecall` tokenizes on whitespace, matches each
lower-cased token as a substring of key or body, ranks by the count of distinct
matched tokens, and breaks ties by key. `appliedKong.Run` in
[`internal/verbs/kong_converted.go`](../../../internal/verbs/kong_converted.go)
stores `{"count":N,"last_applied":"<RFC3339>"}` at
`applied:<role>:<slug>` using non-atomic read-modify-write. The condense packet
joins that counter by bare slug across tiers.

Read-only `ateam memories-json` inspection on 2026-08-26, using the shipped
binary at `/Users/ericlloyd/.local/bin/ateam` (SHA-256
`85977625e9f8c3150d8eb692a5ca3a16798e604a9cfc4a0d10383ca3305e07f8`),
returned these aggregate facts without retaining memory bodies:

| Observation | Result | Evidence |
| --- | ---: | --- |
| Records across eight namespaces | 1,842 | A |
| Cold / fresh / hot records | 1,611 / 83 / 148 | A |
| Records with non-zero applied count | 285 (15.5%) | A |
| Maximum observed applied count | 92 | A |
| Body size, min / mean / max bytes | 32 / 817 / 3,570 | A |
| Bodies with RULE / TRIGGER / APPLY markers | 1,748 / 1,716 / 1,721 | A |
| Same role and bare slug present in multiple tiers | 11 groups | A |

These are shape and coverage observations, not quality judgments. The global
snapshot is mutable, contains private operational knowledge, and is not a
shareable evaluation corpus. It motivates a synthetic corpus with the same
wire shape and failure patterns.

## R4, R5, and R7 method map

| Lifecycle problem | Deterministic method | Model-assisted method | Human-gated method | Maintained tools or primary surfaces | Requirement / grade |
| --- | --- | --- | --- | --- | --- |
| Extraction | Parse explicit structured fields; reject over-cap or malformed records; attach source ID and version before any model call | Extract atomic RULE/TRIGGER/APPLY claims from free text with a schema-constrained model; retain confidence and prompt/model version | Approve new extraction rules for sensitive classes or low-confidence/high-impact facts | LangMem `0.0.30` at commit [`29cbe41`](https://github.com/langchain-ai/langmem/tree/29cbe41e58528f92e9efa773c12e15c47be3808c); Mem0 `2.0.19` at commit [`39bc023`](https://github.com/mem0ai/mem0/blob/39bc02330563764e7d4465f1ecff5f002d94da1a/mem0/configs/prompts.py) has a comparable ADD/UPDATE/DELETE/NONE prompt; neither is outcome evidence | R4 / B |
| Exact deduplication | Normalize only declared non-semantic variation, hash canonical content, and group within role/scope; merge provenance, never discard it | None needed | Review normalization-rule changes because punctuation, paths, case, and numbers can be semantic | Standard hashes plus any transactional store; reproduced SQLite example below | R4 / A |
| Near-duplicate detection | Blocking by role, topic/key tokens, MinHash, or embedding-neighbor candidates; deterministic thresholds only generate candidates | Pairwise entailment or LLM cluster/merge proposal with quoted source spans | Approve merges whose union drops a condition, command, exception, or provenance | LangMem manager; Graphiti `0.29.3` edge resolver at commit [`683a853`](https://github.com/getzep/graphiti/blob/683a8539c8925de69071a1305dc8bf0e52e17c65/graphiti_core/utils/maintenance/edge_operations.py) | R4 / B |
| Contradiction detection | For typed facts, compare `(role, subject, predicate, validity interval)` and flag incompatible values; preserve both versions | Natural-language inference can classify entailment, independence, or contradiction and propose the atomic disputed claims | Resolve ambiguous authority, scope, or time; approve any destructive action | Graphiti's source accepts model-produced duplicate and contradiction indexes and sets temporal invalidation fields; this is mechanism evidence, not proof of correctness | R4 / B |
| Resolution and consolidation | Supersede only when an explicit authority rule and non-overlapping validity interval make the answer mechanical; create a derived version with source links and tombstones | Batch synthesis of a compact candidate from selected sources; evaluate before activation | Approve destructive merges, unresolved conflicts, and changes to cardinal operational rules | Generative Agents reflection design; A-MEM linked-note evolution; current `ateam condense` packet and history | R4 / B |
| Provenance and versioning | Stable memory ID plus immutable version ID; `derived_from`, source artifact/version, observed/event time, actor, model/prompt version, decision, and reversible status | Models may identify source spans, but may not invent provenance | Approve missing-source exceptions | W3C [PROV-O](https://www.w3.org/TR/prov-o/) gives a neutral entity/activity/agent vocabulary; temporal Graphiti fields show one implementation pattern | R4 / B |
| Forgetting | First remove from hot exposure, then archive; enforce retention/legal holds; delete only tombstoned versions after policy delay; recompute eligibility from events | Predict low utility or summarize old clusters only as a proposal | Approve irreversible erasure and policy changes; review low-use but high-severity rules | MemoryBank proposes time/importance reinforcement; the current system supports hot/cold demotion and Dolt recovery | R4/R5 / C for decay transfer |
| Hot-set selection | Hard role filter, exclude unresolved conflicts, enforce token budget exactly, cap per-theme redundancy, and solve a constrained coverage/ranking problem | Estimate semantic coverage, task relevance, and merge quality | Approve changes to weighting, protected classes, or the budget policy | Generative Agents uses recency/relevance/importance; LongMemEval-V2 fixes a context budget at query time; current `condense-check` supplies measured budget fields | R5 / B |
| Cold retrieval and reranking | Role/metadata filter first; lexical and/or vector candidate generation; deterministic reciprocal-rank fusion; stable tie-break; abstain when no score clears a calibrated threshold | Cross-encoder or LLM reranker for the small candidate set; query expansion with original query retained | Adjudicate gold relevance and temporal-answer labels, not individual production queries | Ragas `0.4.3` (`4ecab384`) has ID/non-LLM and LLM context metrics; DeepEval `4.2.0` (`eb61968`) has contextual precision/recall; LongMemEval commits are pinned in the ledger | R5 / B |
| Retrieval evaluation | Recall@k, Precision@k, MRR, nDCG@k, abstention, role leakage, latency, and injected tokens against fixed IDs | LLM judges can label relevance or answer support when no gold label exists; run multiple seeds and retain judge traces | Build and periodically re-adjudicate the gold set | Ragas, DeepEval, Phoenix datasets/experiments `20.4.0` (`a015c6f`) | R5 / B |
| Application capture | Emit a receipt when a memory ID/version is actually handed to the harness or explicitly cited in a tool/action; append immutable events | Infer likely use from a trace only as `capture_mode=model_inferred` | Human annotation for disputed or high-value cases | OpenTelemetry GenAI memory span commit [`56d6b11`](https://github.com/open-telemetry/semantic-conventions-genai/tree/56d6b11a02129319bf371083fa134b7ce989c976); OpenInference semantic conventions `0.1.33` (`d10212b`) | R7 / A for schema inspection, not usefulness |
| Usefulness attribution | Join exposure/application to independently observed task outcomes; compare exposed vs holdout or randomized eligible alternatives; aggregate by role/task/version | Classify outcome or causal contribution only when deterministic outcomes are unavailable; store judge identity/version | Interpret causal or high-stakes retention decisions | Phoenix trace/dataset/experiment surface; counterfactual propensity weighting from Joachims et al. | R7 / B |

### Authority boundary

The following split is testable and avoids treating model confidence as
authority:

| Action | Default authority | Required record |
| --- | --- | --- |
| Reject malformed/over-budget write | Deterministic | Validation result and input hash |
| Collapse byte-identical or declared-normal-form duplicate | Deterministic, reversible | All source/version links and canonicalization version |
| Flag typed incompatible values | Deterministic | Conflict key, both versions, validity intervals |
| Suggest a near-duplicate cluster or merge | Model-assisted | Candidate set, prompt/model, quoted supporting spans, confidence |
| Supersede a fact with explicit authoritative ordering | Deterministic, reversible | Authority rule and old/new validity interval |
| Resolve ambiguous free-text contradiction | Human-gated | Alternatives, evidence, decision actor, reason |
| Demote from hot to searchable cold | Automated, reversible | Selection score components and prior tier |
| Permanently erase or merge away unique source evidence | Human-gated | Policy basis, approval, tombstone, recovery deadline |

## Outcome evidence, separate from design claims

| Primary evidence | Observed outcome in that work | What it supports here | What it does not prove | Grade |
| --- | --- | --- | --- | --- |
| [LongMemEval](https://arxiv.org/abs/2410.10813) and official repo commit [`9e0b455`](https://github.com/xiaowu0162/LongMemEval/tree/9e0b455f4ef0e2ab8f2e582289761153549043fc) | The 500-question benchmark exercises extraction, cross-session reasoning, temporal reasoning, knowledge updates, and abstention; tested systems lost substantial accuracy over long histories | An evaluation suite must include updates, time, multi-memory reasoning, and abstention rather than retrieval-hit rate alone | Performance on operational coding memories or a particular library | B |
| [LongMemEval-V2](https://arxiv.org/abs/2605.12493), repo commit [`2cc8c54`](https://github.com/xiaowu0162/LongMemEval-V2/tree/2cc8c540bdb87fe6761629b585e727e1c4704520) | On its web-agent histories, fixed-budget context gathering measured both answer accuracy and query latency; the coding-agent memory method reached 72.5% average accuracy versus 48.5% for its strongest RAG baseline, with higher latency remaining material | Agent memory evaluation should include workflow knowledge, environment gotchas, premise awareness, failed trajectories, fixed token budgets, and latency | General superiority of coding-agent retrieval, or transfer to agent-teams | B |
| [Useful Memories Become Faulty When Continuously Updated by LLMs](https://arxiv.org/abs/2605.12978) | In its controlled ARC-AGI Stream setup, repeated consolidation eventually degraded utility; agents retaining raw episodes by default doubled accuracy versus forced consolidation, and no-consolidation remained competitive | Preserve raw evidence, gate consolidation, evaluate derived memories before activation, and avoid rewrite-on-every-event | That consolidation is always harmful or that its tested schedule transfers unchanged | B |
| [Unbiased Learning-to-Rank with Biased Feedback](https://www.microsoft.com/en-us/research/publication/unbiased-learning-rank-biased-feedback/) | The paper derives propensity-weighted learning from position-biased implicit feedback and reports improvement in its tested search setting | Exposure propensity and presentation position must be accounted for before learning a memory ranker from applications | That an uncalibrated propensity model makes agent-memory telemetry causal | B |

The following are **design or API claims only**: Generative Agents' reflection
and recency/relevance/importance architecture; MemoryBank's Ebbinghaus-inspired
decay; A-MEM's linked note evolution; LangMem's extraction and CRUD API;
Graphiti's temporal edge invalidation; Ragas/DeepEval metrics; Phoenix
experiments; and OTel/OpenInference attributes. They are useful components or
methods to validate, not outcome evidence for this initiative.

## Minimal evaluation dataset based on current shapes

Use two files in a disposable evaluation run, not production data:

1. `memories.jsonl` uses the exact current seven fields: `role`, `key`, `slug`,
   `tier`, `body`, `appliedCount`, `lastApplied`. Bodies follow the observed
   RULE/TRIGGER/APPLY/provenance style and current byte caps.
2. `oracle.jsonl` is evaluation-only. It adds `memory_id`, `version_id`, source
   artifact/version, event and observation time, valid interval, duplicate
   cluster, conflict set, relevance labels by query, expected tier, and whether
   automated resolution is safe. These fields expose metadata that the current
   shape cannot represent directly.

The minimum 12-record corpus is synthetic but mirrors observed failure modes:

| ID | Current-shape role/key/tier | Body intent | Oracle purpose |
| --- | --- | --- | --- |
| M01 | `implementer:hot:worktree-path`, hot | Use the assigned absolute worktree for each command | Relevant cardinal hot rule |
| M02 | `implementer:worktree-path-copy`, cold | Byte-identical to M01, different source | Exact duplicate with retained provenance |
| M03 | `implementer:fresh:cwd-does-not-persist`, fresh | Paraphrase of M01 with one additional trigger | Near duplicate; merge must retain nuance |
| M04 | `implementer:test-workers`, cold | Worker cap is 2, valid from 2026-07-01 | Older typed fact |
| M05 | `implementer:fresh:test-workers-current`, fresh | Worker cap is 4, valid from 2026-08-01, authoritative config | Temporal conflict with deterministic supersession evidence |
| M06 | `reviewer:hot:test-workers`, hot | Reviewer cap remains 2 | Cross-role lexical distractor; must never leak |
| M07 | `implementer:barrel-mock-discipline`, cold | Preserve original exports in shared barrel mocks | Relevant cold retrieval target |
| M08 | `implementer:marketplace-version-path`, cold | Version is owned by marketplace metadata | Same vocabulary, unrelated intent |
| M09 | `implementer:go-advisory-lock`, cold | Use a process lock for a Go command | Short-token substring distractor (`go`) |
| M10 | `implementer:fresh:test-workers-rumor`, fresh | Unattributed claim that worker cap is 8 | Ambiguous conflict; unsafe to auto-resolve |
| M11 | `implementer:hot:no-suppression-comments`, hot | Fix lint/type errors rather than suppressing them | Protected low-frequency hot rule |
| M12 | `implementer:prettier-markdown-baseline`, cold | Prove unrelated formatting drift against baseline | Unrelated but realistic long-form cold entry |

Set non-zero `appliedCount` on M01, M04, M08, and M11; leave M03, M05, and M07
at zero. This deliberately prevents a count-only policy from selecting all
currently correct or relevant memories. Give M01 a higher count than M11 to
test whether a lower-frequency safety rule remains protected. Include the same
bare slug in hot and cold for one fixture copy to exercise the current
tier-independent applied join.

### Query and curation cases

| Case | Input | Gold behavior |
| --- | --- | --- |
| Q1 exact/key | role `implementer`, `worktree-path` | M01 first; M02/M03 recognized as duplicate/near-duplicate evidence, not three independent recommendations |
| Q2 semantic | role `implementer`, `commands run in the wrong checkout` | M01 or M03 in top 3 despite limited lexical overlap |
| Q3 temporal update | role `implementer`, `test worker cap` at 2026-08-26 | M05 supports the current answer; M04 remains historical; M10 is surfaced as untrusted conflict, not selected as truth |
| Q4 role isolation | role `reviewer`, `test worker cap` | M06 only; zero implementer IDs at every rank |
| Q5 cold gotcha | role `implementer`, `mock a shared auth barrel` | M07 in top 3 |
| Q6 substring trap | role `implementer`, `go` | Abstain or return only intentionally labeled results; do not treat arbitrary substrings as relevance |
| Q7 unknown | role `implementer`, `Kubernetes operator rollback` | Correct abstention; no relevant memory |
| C1 exact dedup | M01 + M02 | One active semantic item, two provenance links, both source versions recoverable |
| C2 merge | M01 + M03 | Proposed merge preserves M03's extra trigger; original bodies remain immutable |
| C3 conflict | M04 + M05 + M10 | M05 active under explicit authority/time rule; M04 historical; M10 unresolved/quarantined |
| H1 hot selection | all 12 under a deliberately tight fixture budget | Budget never exceeded; no unresolved conflict or duplicate exposed; M11 protected; relevant themes covered without count-only sorting |

Scale the set after the first loop by sampling de-identified production shapes,
not bodies: body-length deciles, role/tier proportions, slug-token lengths,
applied-count buckets, and known failed queries. Every added case needs two
independent labels or one label plus adjudication. Freeze dataset and oracle
versions before comparing runs.

## Measurable criteria

### Curation and extraction (R4)

| Metric | Definition | Minimum closed-loop criterion on the fixture |
| --- | --- | --- |
| Atomic extraction precision/recall/F1 | Human-labeled atomic claims versus extracted claims | Report all three; exact structured claims must be 1.0, model extraction remains a measured score |
| Exact-duplicate pair precision/recall | Predicted exact pairs versus oracle pairs | 1.0 / 1.0; no model needed |
| Near-duplicate pair/cluster quality | Pairwise F1 plus B-cubed precision/recall | Report with raw TP/FP/FN; no unlabeled pair treated as negative |
| Conflict detection precision/recall | Predicted conflict sets versus oracle | 1.0 on typed conflicts; model-assisted free text reported separately |
| Unsafe auto-resolution rate | Ambiguous conflicts automatically activated / ambiguous conflicts | 0 |
| Provenance completeness | Active/derived versions with all required source/version links | 100% |
| Reversibility | Decisions restored exactly from version/tombstone history | 100% in delete, merge, and supersede probes |
| Budget compliance | Curated hot sets at or below measured token budget | 100% of runs |
| Redundant/conflicted hot exposure | Hot tokens from duplicate clusters or unresolved conflicts | 0 |

### Retrieval and hot selection (R5)

- Report Recall@1/3/5, Precision@1/3/5, MRR, nDCG@5, MAP, and per-query ranked
  IDs. ID-based measures are primary; LLM relevance is a secondary analysis.
- Report exact role leakage count; the criterion is zero at every cutoff.
- Report abstention precision and recall on positive and no-answer cases.
- Report p50/p95/p99 retrieval latency, candidate count, reranker calls,
  injected tokens, and cost per query. Do not compare quality at unequal token
  budgets without labeling the difference.
- On this small fixture, every gold target must appear by rank 5 and every
  exact/key case by rank 1. Treat that as a smoke gate, not a statistically
  meaningful product benchmark. Publish bootstrap confidence intervals once
  the query set is large enough.
- For hot selection, report oracle task/requirement coverage per 1,000 tokens,
  unique-theme count, protected-rule recall, duplicate-token rate, unresolved
  conflict count, and churn between consecutive hot sets.

### Application and usefulness (R7)

- **Capture completeness:** emitted exposure receipts / injector-delivered
  memory versions. Controlled harness runs require 100%.
- **Event integrity:** 100 concurrent fixture applications must produce 100
  distinct events and the exact aggregate count after retries; duplicate
  idempotency keys must add zero.
- **Attribution join rate:** applications linked to an exposure, memory version,
  trace/run, role, and task type / all applications. Controlled runs require
  100%; production reports the missing strata rather than imputing them.
- **Outcome join rate:** outcome-eligible runs with an independent outcome
  event / outcome-eligible runs. Keep "no observable outcome" distinct from
  failure.
- **Conditional utility:** success rate and cost with an eligible memory
  exposed versus a randomized/held-out eligible alternative, stratified by
  role and task type. Report effect size and interval, not raw count.
- **Negative evidence:** only an exposed memory with a genuine opportunity to
  help can accrue a non-use observation. Absence of self-report is not a
  negative label.

## Reproduced deterministic duplicate/conflict example

This disposable SQLite in-memory probe was run on 2026-08-26 with the system
`sqlite3`. It demonstrates role-scoped exact duplicate detection and typed
conflict detection. It does not resolve the conflict because authority and
validity policy are separate inputs.

```sh
sqlite3 :memory: "
CREATE TABLE memories(
  id TEXT PRIMARY KEY, role TEXT, subject TEXT, predicate TEXT,
  value TEXT, valid_from TEXT, source TEXT
);
INSERT INTO memories VALUES
 ('m1','implementer','test_workers','equals','2','2026-07-01','config@a1'),
 ('m2','implementer','test_workers','equals','2','2026-07-01','runbook@b1'),
 ('m3','implementer','test_workers','equals','4','2026-08-01','config@c1'),
 ('m4','reviewer','test_workers','equals','2','2026-07-01','config@a1');
SELECT 'DUP', a.id, b.id
FROM memories a JOIN memories b
 ON a.id < b.id AND a.role=b.role AND a.subject=b.subject
 AND a.predicate=b.predicate
 AND lower(trim(a.value))=lower(trim(b.value))
 AND a.valid_from=b.valid_from;
SELECT 'CONFLICT', role, subject, predicate,
       group_concat(DISTINCT value)
FROM memories
GROUP BY role,subject,predicate
HAVING count(DISTINCT lower(trim(value))) > 1
ORDER BY role,subject,predicate;"
```

Observed output:

```text
DUP|m1|m2
CONFLICT|implementer|test_workers|equals|2,4
```

The reviewer record is neither duplicate nor conflict because role is part of
the identity. A production equivalent must additionally record both sources,
valid-to intervals, canonicalization version, and the later decision. Exact
duplicate collapse can be automatic and reversible. Choosing `2` or `4`
cannot be automatic unless an explicit authority/validity rule is present.

## Inspected telemetry integration surface

The OpenTelemetry GenAI semantic-conventions repository was shallow-cloned and
inspected at commit
[`56d6b11a02129319bf371083fa134b7ce989c976`](https://github.com/open-telemetry/semantic-conventions-genai/tree/56d6b11a02129319bf371083fa134b7ce989c976).
The generated `gen_ai.memory.client` span and its source YAML are marked
**Development** and define:

- operations `create_memory_store`, `search_memory`, `create_memory`,
  `update_memory`, `upsert_memory`, `delete_memory`, and
  `delete_memory_store`;
- required `gen_ai.operation.name`;
- conditional `gen_ai.memory.record.id`, `gen_ai.memory.store.id`, provider,
  and error/server fields;
- recommended `gen_ai.memory.record.count`;
- opt-in `gen_ai.memory.query.text` and `gen_ai.memory.records`, both explicitly
  gated because they can contain sensitive or PII data;
- a JSON record schema requiring `content`, with optional `id`, `metadata`, and
  search `score`, and allowing provider-specific fields.

The repository's generated support report lists reference scenarios for AWS
Bedrock AgentCore and Google ADK. Inspection verifies a usable operation span,
not end-to-end capture in Claude Code, Codex, or agent-teams.

OpenInference `0.1.33` at commit
[`d10212b`](https://github.com/Arize-ai/openinference/blob/d10212b2049d097e101f7ec0fa5a7dd814a7d9c9/spec/semantic_conventions.md)
provides a second OTLP-compatible surface: `RETRIEVER` and `RERANKER` span
kinds, document ID/content/metadata/score, ordered retrieval/reranker document
attributes, session ID, and evaluator spans. Phoenix release `20.4.0`
documents OTLP/OpenInference ingestion, datasets, experiments, code/LLM/human
evaluators, and trace-linked annotations. These can carry and inspect the
proposed events, but none supplies the missing memory semantics automatically.

### Minimal neutral event contract

Use ordinary OTel spans for store/search operations and append-only OTel log
records or span events for per-memory lifecycle signals. Put project-specific
fields behind a versioned `agent_teams.memory.*` namespace while the standard
is Development.

| Event | Reliable capture point | Required project fields | Interpretation |
| --- | --- | --- | --- |
| `memory.exposed` | Injector after bytes/IDs are actually handed to a harness | event/exposure ID, memory ID+version, role, tier, ordinal, token count, run/trace, harness, adapter version | Opportunity to influence; denominator for application |
| `memory.retrieved` | Shared CLI/API response boundary | query hash or opted-in text, ordered IDs+versions, rank, raw/fused/rerank score, filters, latency, candidate count | Retrieval behavior, not use |
| `memory.applied` | Explicit tool/action citation or structured adapter receipt | source exposure/retrieval event, memory ID+version, action/tool-call ID, capture mode | Claimed or directly observed application, not benefit |
| `memory.outcome` | Orchestrator/test/gate/task boundary independent of memory agent | run/task ID, outcome type/value, verifier, latency/cost, eligibility | Observable result; still not causal alone |
| `memory.feedback` | Human annotation or versioned judge | target event, label/score, annotator type+ID, rubric/judge version | Sparse quality label |
| `memory.curated` | Curation transaction | old/new versions, decision, source IDs, authority mode, reason, model/prompt or human actor, reversible-until | Audit and rollback |

Do not emit raw body, prompt, query, tool result, or user identity by default.
Use opaque stable IDs and content hashes; keep sensitive payloads in the
governed source store. Hashing low-entropy queries is not anonymization, so an
opt-in content mode still needs access control, encryption, retention, and
deletion propagation.

### Cross-harness capture

- Put injection and retrieval instrumentation at the stable `ateam` CLI/API
  boundary, then let Claude Code, Codex, and a generic client adapters add only
  harness/run identity. This avoids depending on model wording or provider
  callbacks.
- An injector emits `memory.exposed` only after it builds the exact delivered
  payload and verifies the expected completion/end marker. A read attempt is
  not a delivery receipt.
- Retrieval emits ordered stable IDs and versions before formatting human
  output. Truncated terminal text must not change the telemetry record.
- Explicit application needs a structured field/tool argument containing the
  memory ID/version. Free-form mentions can be retained as model-inferred
  annotations but cannot be mixed with direct receipts.
- Outcome adapters belong to the task runner: test pass/fail, build result,
  review finding accepted/rejected, human gate, retry, latency, and cost. The
  memory subsystem must not grade its own benefit.
- For offline harnesses, spool signed/idempotent events locally and upload
  later. Report delayed and dropped counts; never silently turn an outage into
  zero applications.

## Concurrency and consistency requirements

1. **Append events, aggregate later.** Replace mutable count read-modify-write
   with one immutable event per application. A materialized count is a cache,
   rebuildable from events.
2. **Idempotency is end to end.** Each event has a globally unique `event_id`
   and a deterministic idempotency key over harness run, event type, memory
   version, and action/tool-call. Retries use the same key.
3. **Separate event and observation time.** Store `occurred_at`, `observed_at`,
   and monotonic sequence when a harness provides one. Never infer causal order
   from wall-clock timestamps across machines alone.
4. **Version every read and write.** Exposure and retrieval cite the exact
   memory version. Curation uses compare-and-swap or a transaction so a stale
   model proposal cannot overwrite a newer version.
5. **Make operation/event gaps observable.** Prefer a transactional outbox
   when memory and event stores share a transaction. Otherwise use a durable
   local write-ahead spool and reconciliation for memory-without-event and
   event-without-memory cases.
6. **Serialize only conflicting curation.** A per-role/cluster lease can guard
   activation, but model work should happen against a snapshot and outside a
   long database lock. Revalidate versions at commit.
7. **Preserve sync provenance.** Cross-machine merge/replay retains original
   event IDs and actors. Last-write-wins is not acceptable for counters,
   conflict decisions, or version lineage.

The current `applied` implementation violates item 1 under concurrent writers:
two readers can observe `N`, both write `N+1`, and lose one application. Its
timestamp also records only the last successful overwrite and carries no run,
exposure, outcome, or memory-version identity. This is grade-A source evidence
for expected undercount, while the actual production loss rate remains unknown.

## Feedback-loop bias and failure modes

| Failure mode | Mechanism | Detection | Mitigation |
| --- | --- | --- | --- |
| Popularity/position bias | High-ranked or hot memories receive more exposure and therefore more applications | Application rate by rank and exposure bucket; compare randomized eligible positions | Use exposure-normalized rates or inverse propensity weighting; cap popularity weight; reserve exploration and protected/novelty slots |
| Self-report undercount | Agent omits `applied`, especially under pressure, failure, compaction, or concurrent overwrite | Compare explicit reports with structured citations/tool receipts and event reconciliation | Treat self-report as partial; capture at adapter boundaries; append events |
| Exposure confounding | Easy/common tasks both retrieve popular memories and succeed | Outcome by task/role difficulty and holdout assignment | Randomized holdout/interleaving among eligible memories; stratify; report effect intervals |
| Non-use misread as harm | A memory was never exposed or had no opportunity to help | Join application to exposure and outcome eligibility | Only score non-use after verified exposure/opportunity; keep unknown separate |
| Consolidation drift | Repeated model rewrites omit conditions or introduce false rules | Source-span coverage, semantic diff, regression tasks, version churn | Preserve raw sources; batch/gate consolidation; activate only after eval; rollback |
| Contradiction flattening | Newer, louder, or more popular text overwrites valid scoped/history facts | Conflict-set audit and temporal/role test cases | Bitemporal/versioned facts; explicit authority; human gate for ambiguity |
| Provenance loss | Merge keeps prose but drops source/version/decision | Provenance completeness and restore probe | Immutable source IDs, derived versions, tombstones |
| Lexical and embedding bias | Substring noise, vocabulary mismatch, hubness, or stale embedding model | Per-query failures, hard distractors, slice metrics | Hybrid candidates, role filter first, reranking, abstention, model/version pin |
| LLM judge bias/variance | Judge preference or prompt change alters relevance/outcome labels | Human sample, multiple seeds/judges, agreement and drift by version | Deterministic ID metrics first; freeze rubric; retain traces; adjudicate disagreements |
| Safety-rule starvation | Rare cardinal rule has fewer applications than common convenience advice | Protected-rule recall and severity slice | Severity/protection constraint independent of count; no pure top-N popularity sort |
| Duplicate/replayed events | Retry or offline flush inflates counts | Duplicate idempotency-key rate and sequence gaps | Unique constraints, idempotent ingest, reconciliation |
| Role leakage | Similar text crosses role filters during candidate generation or telemetry joins | Exact leaked-ID count in every test | Role/scope filter before retrieval and aggregation; include role in identity |
| Privacy replication | Traces copy bodies, prompts, secrets, or deleted content into another store | Payload-field audit, retention/deletion reconciliation | IDs-only default, opt-in content capture, encryption/RBAC, redaction, deletion propagation |
| Telemetry outage bias | Offline runs look like non-use or failure | Per-adapter expected/emitted/accepted counters and spool age | Durable spool, completeness dashboards, exclude incomplete windows from learning |

## Cost and operational model

Report costs by stage and observed volume rather than one blended per-memory
number. Let `W` be new writes, `C` candidate comparisons, `Q` searches, `K`
reranked candidates, `V` retained versions, and `E` telemetry events.

| Cost class | Main drivers | Controls and accounting |
| --- | --- | --- |
| Deterministic compute | Canonicalization/hash `O(W)`; naive all-pairs dedup `O(N^2)`; lexical/vector candidates and metric calculation | Block by role/topic/time, use indexes, cap candidate sets, record CPU/latency per stage |
| Model calls | Extraction, entailment/conflict classification, consolidation, reranking, LLM judges | Batch where semantics allow; use models only after deterministic filtering; pin model/prompt; record input/output/cache tokens and retry cost separately |
| Embeddings | New/changed bodies and query embeddings; re-embedding on model change | Content-hash cache; dual-index migration; record model/dimension/version and amortized re-embed cost |
| Storage | Raw sources + immutable versions + indexes + append events + trace metadata; derived text can duplicate source content | Measure bytes per source/version/event and retention class; compact aggregates without deleting audit events; test export/restore |
| Telemetry transport/backend | `E` events, high-cardinality IDs, trace retention, indexing, offline spool | Sample verbose spans only, never sample audit/application events silently; IDs-only default; tiered retention; publish dropped-event counts |
| Privacy/security | Classification, redaction, key management, RBAC, deletion propagation, incident response | Data inventory and processor map; content opt-in; tenant/role isolation tests; short trace retention; audit access |
| Operations | Schema migrations, model drift, evaluator maintenance, conflict queues, reindexing, backup/restore, standard churn | Version every envelope and evaluator; canary/replay fixture; monitor queue age, event lag, restore success, and unknown-rate |
| Human review | Gold labels, ambiguous conflicts, destructive erasure, high-impact rule changes | Route only uncertain/high-risk cases; measure queue age, agreement, and decisions overturned after review |

Model-assisted quality has both direct inference cost and indirect operational
cost: nondeterministic retries, schema failures, judge drift, and re-validation
when a provider model changes. Local models reduce data egress but do not remove
compute, upgrade, or evaluation cost. Hosted observability reduces some service
operation but creates another data processor and retention/export surface.
These are architecture inputs, not a winner selection.

## Evidence ledger

All links were accessed on 2026-08-26.

| ID | Pinned source | Type | Supported claim | Grade / uncertainty |
| --- | --- | --- | --- | --- |
| E1 | agent-teams `3bb52fad`; plugin manifests `0.60.0`; shipped binary SHA above; local `query.go`, `write.go`, `kong_converted.go` | Source + executable inspection | Current shape, retrieval algorithm, condense join, and non-atomic applied counter | A; global store changes after snapshot |
| E2 | Disposable SQLite probe and observed output above | Reproduced artifact | Role-scoped exact duplicate and typed conflict queries are deterministic | A; toy scale only |
| E3 | OTel GenAI memory conventions commit [`56d6b11`](https://github.com/open-telemetry/semantic-conventions-genai/tree/56d6b11a02129319bf371083fa134b7ce989c976), based on semconv `1.44.0` | Official schema/source inspection | Memory operation span fields, Development status, privacy opt-in, record schema, reference scenarios | A for artifact; no agent-teams integration run |
| E4 | OpenInference semantic conventions `0.1.33`, commit [`d10212b`](https://github.com/Arize-ai/openinference/blob/d10212b2049d097e101f7ec0fa5a7dd814a7d9c9/spec/semantic_conventions.md) | Official schema/source | Retriever/reranker/evaluator span fields and ordered documents | B; not exercised here |
| E5 | Phoenix `20.4.0`, commit [`a015c6f`](https://github.com/Arize-ai/phoenix/tree/a015c6f69ccb23f1eb2d2a31a25097b42f9dba00); [official experiments docs](https://arize.com/docs/phoenix/datasets-and-experiments/how-to-experiments/run-experiments) | Release/source/docs | OTLP/OpenInference observability and code/LLM evaluation over datasets/experiments | B; backend operating profile is outside this track |
| E6 | Ragas `0.4.3`, commit [`4ecab384`](https://github.com/vibrantlabsai/ragas/tree/4ecab384fda829ca50bec3f07cc49589d756e172); [context precision](https://docs.ragas.io/en/stable/concepts/metrics/available_metrics/context_precision/), [context recall](https://docs.ragas.io/en/stable/concepts/metrics/available_metrics/context_recall/) | Release/source/docs | ID/non-LLM and LLM-assisted retrieval metrics | B; LLM metrics inherit judge cost/variance |
| E7 | DeepEval `4.2.0`, commit [`eb61968`](https://github.com/confident-ai/deepeval/tree/eb61968725bb2414c5d8d453e6224f156d470291); [contextual precision](https://deepeval.com/docs/metrics-contextual-precision), [contextual recall](https://deepeval.com/docs/metrics-contextual-recall) | Release/source/docs | Reference-based LLM retriever evaluation and ranked precision | B; not a substitute for ID gold labels |
| E8a | LangMem `0.0.30`, commit [`29cbe41`](https://github.com/langchain-ai/langmem/tree/29cbe41e58528f92e9efa773c12e15c47be3808c); [`knowledge/tools.py`](https://github.com/langchain-ai/langmem/blob/29cbe41e58528f92e9efa773c12e15c47be3808c/src/langmem/knowledge/tools.py) | Maintained source/docs | Model-assisted create/update/delete and background extraction/consolidation surface | B; quality and cross-harness behavior not reproduced |
| E8b | Mem0 `2.0.19`, commit [`39bc023`](https://github.com/mem0ai/mem0/blob/39bc02330563764e7d4465f1ecff5f002d94da1a/mem0/configs/prompts.py) | Maintained source | Schema-constrained extraction plus model-selected ADD/UPDATE/DELETE/NONE operations | B for mechanism; prompt behavior and outcome quality not reproduced |
| E9 | Graphiti `0.29.3`, commit [`683a853`](https://github.com/getzep/graphiti/tree/683a8539c8925de69071a1305dc8bf0e52e17c65); edge resolver linked above | Maintained source | LLM-selected duplicate/contradiction candidates and valid/invalid/expired temporal fields | B for mechanism; model correctness unknown |
| E10 | [Generative Agents](https://doi.org/10.1145/3586183.3606763), UIST 2023 | Peer-reviewed primary paper | Complete episode stream, reflection, and recency/relevance/importance retrieval design | B for paper; simulation transfer unknown |
| E11 | [MemoryBank](https://arxiv.org/abs/2305.10250), arXiv `2305.10250` | Primary paper | Time/importance reinforcement and forgetting design | C for operational-rule retention; transfer and deletion safety unproven |
| E12 | [A-MEM](https://arxiv.org/abs/2502.12110), arXiv `2502.12110` | Primary paper | Structured linked notes and memory evolution | B for method; continuous rewrite risk must be evaluated |
| E13 | [LongMemEval](https://arxiv.org/abs/2410.10813), repo [`9e0b455`](https://github.com/xiaowu0162/LongMemEval/tree/9e0b455f4ef0e2ab8f2e582289761153549043fc) | Primary paper + maintained benchmark | Five long-term memory abilities and evaluation format | B; dialogue domain differs |
| E14 | [LongMemEval-V2](https://arxiv.org/abs/2605.12493), repo [`2cc8c54`](https://github.com/xiaowu0162/LongMemEval-V2/tree/2cc8c540bdb87fe6761629b585e727e1c4704520) | 2026 primary preprint + code | Agent workflow/gotcha/premise benchmark, fixed context budget, accuracy and latency | B; new preprint and web-agent domain |
| E15 | [Useful Memories Become Faulty When Continuously Updated by LLMs](https://arxiv.org/abs/2605.12978), arXiv `2605.12978v1` | 2026 primary preprint | Consolidation schedule can degrade utility; raw episodes are a necessary control | B; ARC-AGI Stream scope |
| E16 | [RAGAs paper](https://aclanthology.org/2024.eacl-demo.16/), EACL 2024, DOI `10.18653/v1/2024.eacl-demo.16` | Peer-reviewed primary paper | Separating retrieval focus/relevance from generation faithfulness and answer quality | B; agent-memory curation not directly evaluated |
| E17 | [Unbiased Learning-to-Rank with Biased Feedback](https://doi.org/10.1145/3018661.3018699), WSDM 2017 | Peer-reviewed primary paper | Counterfactual/propensity treatment for biased implicit feedback | B; propensity estimation for memory exposure is unvalidated |
| E18 | [W3C PROV-O](https://www.w3.org/TR/prov-o/), W3C Recommendation 2013 | Official standard | Neutral entity/activity/agent provenance vocabulary | B; does not prescribe storage or curation policy |

## Explicit unknowns and follow-up validation

- The actual rate of lost concurrent `applied` increments is unknown. Source
  proves the race is possible; only a controlled concurrent write probe on a
  disposable workspace can measure it.
- No current cross-harness adapter emits exposure or exact application receipts.
  Capture completeness across Claude Code, Codex, compaction, resume, and
  subagent startup is unknown until V3/V6 run end to end.
- The best normalization rules, blocking strategy, embedding/reranker, and
  score weights for RULE/TRIGGER/APPLY bodies are unknown. They must be chosen
  from the frozen dataset, not from generic benchmark claims.
- The causal effect of any individual memory on task success is unknown without
  randomized/interleaved exposure or another defensible counterfactual design.
- How often zero-use entries are truly obsolete versus merely unexposed is
  unknown because the current system has no exposure denominator.
- No current evidence establishes a safe autonomous deletion threshold. Start
  with reversible demotion/archive and measure review overturns.
- OTel GenAI memory conventions are Development and do not yet standardize
  role, tier, version lineage, application, outcome, or conflict semantics.
  Attribute names and integration code may churn before stabilization.
- The privacy classification and retention policy for memory IDs, queries,
  event metadata, and traces have not been approved. Raw-content telemetry
  must remain off until that governance exists.
- Model and judge cost at the observed 1,842-record scale is unknown until a
  candidate pipeline records actual candidate counts, tokens, cache hits,
  retries, latency, and price snapshot.
- Benchmark transfer is unknown. LongMemEval and LongMemEval-V2 supply useful
  task categories and harness patterns, but the initiative still needs the
  small repository-shaped dataset above and later de-identified regression
  cases from real failures.

These unknowns do not imply absence. They remain explicit inputs for the
validation join and must not be converted to zero scores or platform claims.
