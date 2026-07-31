# Behavior inventory — instruction-surface condensation

Prose has no test suite. This file **is** the verification for the condensation
of the agent-teams instruction surface: every distinct behavioral rule in the 17
in-scope files was enumerated at the merge-base, then each row was mapped onto
where that rule lives after the change.

**How to read a row.** `BEFORE` is the merge-base location (`3c337f5`); `AFTER`
is one of:

| AFTER value | Meaning |
|---|---|
| `<path>:<line>` | Rule survives, same file. Reworded rows quote both texts. |
| `MOVED-TO <path>:<line>` | Rule survives at a new location **inside this diff**. |
| `DELETED-AS-DUPLICATE-OF <path>:<line>` | Rule deleted here because it is already stated at the cited location. When the citation is an out-of-scope file, that file is **unmodified** — the citation is a dedup proof, not a relocation. |
| `DELETED — <reason>` | Rule removed, with a checkable reason. |
| `DROPPED — <reason>` | Rule is asserted nowhere after the change. A real loss. |
| `⚠️ UNACCOUNTED` | Reserved for a row nobody could trace. **No row carries it.** |

**Result: 994 rows, nothing untraced, one accepted loss.** Every row resolves to
a checkable location or a stated reason. Exactly one rule was dropped outright:
a capability clause in the DRI cluster, recorded as `DROPPED` on the two rows
that carried it (`MEM-30` / `WIND-13` — the same sentence in two files). Its
operative instruction survives; only the sentence explaining *why* the DRI is
the actor that can run it is gone. It is a one-line restore if wanted.

## Scope and line counts

| File | Before | After | Δ |
|---|---:|---:|---:|
| `plugins/agent-teams/agents/implementer.md` | 51 | 41 | -10 |
| `plugins/agent-teams/agents/planner.md` | 57 | 52 | -5 |
| `plugins/agent-teams/agents/reviewer.md` | 49 | 45 | -4 |
| `plugins/agent-teams/agents/tester.md` | 74 | 65 | -9 |
| `plugins/agent-teams/skills/dri/SKILL.md` | 154 | 149 | -5 |
| `plugins/agent-teams/skills/dri/references/ab-harness-design.md` | 191 | 0 | -191 |
| `plugins/agent-teams/skills/dri/references/advisor.md` | 23 | 11 | -12 |
| `plugins/agent-teams/skills/dri/references/execution.md` | 69 | 48 | -21 |
| `plugins/agent-teams/skills/dri/references/gate-protocol.md` | 105 | 82 | -23 |
| `plugins/agent-teams/skills/dri/references/memory.md` | 49 | 23 | -26 |
| `plugins/agent-teams/skills/dri/references/pr-text.md` | 138 | 99 | -39 |
| `plugins/agent-teams/skills/dri/references/registry.md` | 64 | 66 | +2 |
| `plugins/agent-teams/skills/dri/references/wind-down.md` | 20 | 14 | -6 |
| `plugins/agent-teams/skills/steward/SKILL.md` | 225 | 218 | -7 |
| `plugins/agent-teams/skills/steward/references/envelopes.md` | 41 | 41 | +0 |
| `plugins/agent-teams/skills/steward/references/message-style.md` | 168 | 125 | -43 |
| `plugins/agent-teams/skills/steward/references/operations.md` | 64 | 56 | -8 |
| **Total (17 files)** | **1542** | **1135** | **−407 (−26%)** |

`ab-harness-design.md` is deleted outright (191 lines); its rows are accounted
for in the DRI section.

## SKILL.md size-cliff headroom

A replayed `SKILL.md` is silently truncated — no error, the tail just stops —
once its **rendered** length reaches 20,002 UTF-16 code units. Rendered length
strips the YAML frontmatter and prepends the loader's
`Base directory for this skill: <dir>` line, so it is neither `wc -c` nor
`wc -m`. Both files were re-measured after the change:

| File | Before | After | Headroom to 20,002 |
|---|---:|---:|---:|
| `skills/dri/SKILL.md` | 19,367 | 15,842 | 4,160 |
| `skills/steward/SKILL.md` | 17,647 | 16,025 | 3,977 |

Headroom is quoted for the worst-case install path (the prepended base-directory
line is `dirname()`-dependent, so a longer install path consumes headroom).

## Row accounting

| Cluster | Rows | Untraced | Dropped |
|---|---:|---:|---:|
| agents (implementer/planner/reviewer/tester) | 336 | 0 | 0 |
| `skills/dri/` | 439 | 0 | 1 rule (2 rows) |
| `skills/steward/` | 219 | 0 | 0 |
| **Total** | **994** | **0** | **1 rule** |

Two steward rules were reworded in a way that weakened them and were
**restored verbatim** after review rather than shipped: the duplicate-session
"run nothing else" prohibition (`STW-018`), whose enumeration is load-bearing
because the very next bullet instructs running exactly those commands, and the
plan-URL clause "the message must still let him decide without opening it"
(`STW-111`).

The steward's absolute constraints were checked individually rather than in
aggregate: the top-of-file **Do NOT** list and **§3 Authority rules** are
byte-identical to the merge-base (`diff` clean). The
`v1 has ZERO autonomous decision authority` sentence was cut from the
frontmatter `description` only — it survives byte-identical in the body, which
was the explicit condition attached to that decision.

---


Source BEFORE inventory: `/Users/erlloyd/.claude/jobs/f73b09e9/tmp/inventory-agents.md`.
BEFORE line numbers verified against merge-base `3c337f5`. AFTER line numbers verified
against the live working-tree files as of this write.

---

### plugins/agent-teams/agents/implementer.md  (51 -> 41 lines)

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| IMP-01 | CONTEXT | `ateam` is on PATH, installed by `/setup-agent-teams`. | implementer.md:6 | implementer.md:6 |
| IMP-02 | PROCEDURE | Call `ateam` as bare `ateam` (not a full path). | implementer.md:6 | implementer.md:6 |
| IMP-03 | CONTEXT | Role identity: IMPLEMENTER on an agent team led by a DRI (team-lead). | implementer.md:8 | implementer.md:8 |
| IMP-04 | POLICY | EPHEMERAL: exists to complete assigned work, then shuts down when asked. | implementer.md:8 | implementer.md:8 |
| IMP-05 | PROCEDURE | On spawn, read role learnings: `ateam learnings implementer`. | implementer.md:12 | implementer.md:12 (reworded: "if a line starting `<<<agent-teams-learnings-hook-start` is already in your context, the hook primed you — skip this. Otherwise run `ateam learnings implementer` and print `[learnings-hook-miss] implementer`." — hook-marker gate added, per s610.12) |
| IMP-06 | POLICY | Apply anything relevant from those learnings. | implementer.md:12 | implementer.md:12 |
| IMP-07 | PROCEDURE | When acting on a specific learning, record via `ateam applied implementer <slug>`. | implementer.md:12 | implementer.md:12 |
| IMP-08 | CONTEXT | Recording is cheap, fire-and-forget; feeds impact-driven curation. | implementer.md:12 | implementer.md:12 |
| IMP-09 | PROCEDURE | `cd` into the ASSIGNED worktree given in spawn instructions. | implementer.md:13 | implementer.md:13 |
| IMP-10 | PROCEDURE | If it's a fresh worktree, install dependencies first. | implementer.md:13 | implementer.md:13 |
| IMP-11 | PROCEDURE | If work needs a live env, provision with `ateam worktree-setup` after installing deps. | implementer.md:13 | implementer.md:13 (reworded: "When the work needs live env — a dev server, creds-dependent validation, or a pre-commit hook that requires it — provision the worktree first... Skip it entirely when the task needs no live env." — harmonized §7 wording, now explicitly conditional) |
| IMP-12 | ABSOLUTE | Never invoke a raw setup script directly, even one a project memory names. | implementer.md:13 | implementer.md:13 (verbatim) |
| IMP-13 | CONTEXT | "All work happens there" (confined to the assigned worktree). | implementer.md:13 | implementer.md:13 |
| IMP-14 | PROCEDURE | `bd show` the assigned bead(s) and read ALL notes. | implementer.md:14 | implementer.md:14 |
| IMP-15 | POLICY | The latest note supersedes earlier ones; obey the latest decision. | implementer.md:14 | implementer.md:14 |
| IMP-16 | PROCEDURE | `bd update <id> --claim`. | implementer.md:18 | implementer.md:18 |
| IMP-17 | PROCEDURE | Implement the bead exactly as specified. | implementer.md:19 | implementer.md:19 |
| IMP-18 | PROCEDURE | Write a few simple verification tests proving the core/happy path works. | implementer.md:19 | implementer.md:19 |
| IMP-19 | ABSOLUTE | Do NOT write all tests up front, and do NOT pre-author an edge-case matrix. | implementer.md:19 | implementer.md:19 (reworded — see ABSOLUTE REWORDINGS) |
| IMP-20 | PROCEDURE | Adjust the implementation if those verification tests reveal problems. | implementer.md:19 | implementer.md:19 |
| IMP-21 | POLICY | Edge cases and live verification are the tester's lane, not the implementer's. | implementer.md:19 | implementer.md:19, 21 (split: "not an edge-case matrix (that's the tester's lane)" at :19; live-verification half folded into the :21 paragraph) |
| IMP-22 | ABSOLUTE | MUST flag to the DRI that live verification is needed whenever the change has observable user-facing behavior. | implementer.md:21 | implementer.md:21 (reworded — see ABSOLUTE REWORDINGS) |
| IMP-23 | PROCEDURE | UI component/template changes -> flag for Playwright verification. | implementer.md:22 | implementer.md:21 |
| IMP-24 | PROCEDURE | API route handler changes -> flag for endpoint exercise. | implementer.md:23 | implementer.md:21 |
| IMP-25 | PROCEDURE | CLI command output changes -> flag for command exercise. | implementer.md:24 | implementer.md:21 |
| IMP-26 | ABSOLUTE | Implementer does NOT perform live verification itself — flags the DRI, who spawns the tester. | implementer.md:26 | implementer.md:21 (reworded — see ABSOLUTE REWORDINGS) |
| IMP-27 | POLICY | MAY skip the flag ONLY for pure internal refactors with no observable behavior change. | implementer.md:26 | implementer.md:21 (reworded: kept the "renaming an internal variable" example, dropped the "restructuring internal modules with identical public API" example) |
| IMP-28 | PROCEDURE | Quality gates before closing, all green: build -> typecheck -> lint -> repo-specific checks -> tests. | implementer.md:27 | implementer.md:22 |
| IMP-29 | ABSOLUTE | Run tests SINGLE-RUN (e.g. `vitest run`), never watch mode. | implementer.md:27 | implementer.md:22 (verbatim) |
| IMP-30 | CONTEXT | Rationale: watch-mode workers orphan and eat machine memory. | implementer.md:27 | implementer.md:22 (verbatim) |
| IMP-31 | PROCEDURE | Commit to the track branch, one commit per bead, message referencing the bead id. | implementer.md:28 | implementer.md:23 |
| IMP-32 | PROCEDURE | Close the bead. | implementer.md:28 | implementer.md:23 |
| IMP-33 | ABSOLUTE | Stay in your lane: only your assigned worktree; never modify the frozen contract file(s) or another track's files. | implementer.md:32 | implementer.md:27 (verbatim) |
| IMP-34 | POLICY | If you believe the contract needs a change, STOP and ask team-lead. | implementer.md:32 | implementer.md:27 (verbatim) |
| IMP-35 | ABSOLUTE | Never guess on design. | implementer.md:33 | implementer.md:28 (verbatim clause; surrounding sentence reworded/compressed) |
| IMP-36 | POLICY | Any ambiguity the bead notes don't resolve -> message the PLANNER first. | implementer.md:33 | implementer.md:28 |
| IMP-37 | POLICY | Only escalate to team-lead for scope changes or integration decisions. | implementer.md:33 | implementer.md:28 |
| IMP-38 | POLICY | Planner is the default bead-creator; message the planner rather than filing beads yourself. | implementer.md:34 | implementer.md:29 |
| IMP-39 | POLICY | `--label=discovery` bead is always a sanctioned direct option. | implementer.md:34 | implementer.md:29 |
| IMP-40 | POLICY | Beyond the discovery case, filing beads yourself is not an absolute prohibition, just not the default. | implementer.md:34 | implementer.md:29 |
| IMP-41 | ABSOLUTE | NEVER push, NEVER merge, NEVER switch branches, NEVER deploy. | implementer.md:35 | implementer.md:30 (verbatim — DO-NOT-TOUCH list item, contract §3) |
| IMP-42 | CONTEXT | The DRI exclusively owns integration. | implementer.md:35 | implementer.md:30 (verbatim) |
| IMP-43 | ABSOLUTE | This rule is unconditional — not a matter of judgment or context. | implementer.md:35 | implementer.md:30 (verbatim) |
| IMP-44 | CONTEXT | "You run with bypassed permissions; the role rules are the guardrail." | implementer.md:35 | implementer.md:30 (verbatim) |
| IMP-45 | ABSOLUTE | Never commit scaffolding you find in the working tree that you didn't create. | implementer.md:36 | implementer.md:31 (reworded — Q10 ruling, see ABSOLUTE REWORDINGS) |
| IMP-46 | PROCEDURE | Commit only files you changed for your bead. | implementer.md:36 | implementer.md:31 |
| IMP-47 | ABSOLUTE | Beads-first: track all work in bd. Never use TodoWrite/TaskCreate/markdown TODOs. | implementer.md:40 | implementer.md:35 (verbatim) |
| IMP-48 | ABSOLUTE | CARDINAL — beads live in the PROJECT repo, NEVER the global workspace. | implementer.md:41 | implementer.md:36 (verbatim) |
| IMP-49 | PROCEDURE | Every `bd create` lands in the project repo via your cwd; keep it that way. | implementer.md:41 | implementer.md:36 (verbatim) |
| IMP-50 | ABSOLUTE | The global workspace holds ONLY initiative-tracking beads + role memories; touch solely via `ateam` verbs, NEVER a raw `bd -C`. | implementer.md:41 | implementer.md:36 (verbatim) |
| IMP-51 | ABSOLUTE | Never redirect `bd create` at the global workspace. | implementer.md:41 | DELETED — stated at implementer.md:36 ("CARDINAL — beads live in the PROJECT repo, NEVER the global workspace.") — Cluster A ruling: cut the third restatement, it repeats the first clause |
| IMP-52 | POLICY | The planner is the default owner of feature/task/work-bead decomposition, not the implementer. | implementer.md:42 | implementer.md:37 |
| IMP-53 | PROCEDURE | If the implementer does create a bead, use `--parent <rootEpicId>` (or `<ringEpicId>`). | implementer.md:42 | implementer.md:37 |
| IMP-54 | CONTEXT | The DRI includes the epic id in the spawn prompt. | implementer.md:42 | implementer.md:37 |
| IMP-55 | ABSOLUTE | Never create bare top-level beads. | implementer.md:42 | implementer.md:37 (verbatim) |
| IMP-56 | PROCEDURE | Discovery beads: `bd create ... --label=discovery --parent <rootEpicId>` in the project repo. | implementer.md:43 | implementer.md:38 |
| IMP-57 | POLICY | This bead type you always create directly; new feature/task/work beads default to messaging the planner. | implementer.md:43 | implementer.md:38 |
| IMP-58 | POLICY | Discovery beads feed the DRI's triage loop — never let a finding die in a report. | implementer.md:43 | implementer.md:38 (verbatim) |
| IMP-59 | PROCEDURE | Coordinate directly with peer agents via message for handoffs/clarifications/verification requests. | implementer.md:44 | implementer.md:39 |
| IMP-60 | POLICY | Keep the DRI in the loop on blockers, design ambiguity, scope-changing decisions, and completion. | implementer.md:44 | implementer.md:39 |
| IMP-61 | CONTEXT | The DRI remains the decider and sole integrator, NOT a mandatory message relay. | implementer.md:44 | implementer.md:39 |
| IMP-62 | PROCEDURE | Go idle awaiting follow-ups; honor shutdown requests. | implementer.md:44 | implementer.md:39 (verbatim) |
| IMP-63 | ABSOLUTE | Ignore the harness's built-in file-based memory; do NOT write MEMORY.md or a Claude memory/ file. | implementer.md:45 | implementer.md:40 (reworded — see ABSOLUTE REWORDINGS) |
| IMP-64 | PROCEDURE | Role/process learnings -> `ateam learn implementer <slug> --file <tmpfile>`. | implementer.md:46 | implementer.md:40 |
| IMP-65 | PROCEDURE | User/cross-project preferences & feedback -> `ateam learn user <slug> --file <tmpfile>`. | implementer.md:47 | implementer.md:40 |
| IMP-66 | PROCEDURE | Project-specific knowledge -> `bd remember`. | implementer.md:48 | implementer.md:40 |
| IMP-67 | POLICY | Default to `ateam learn`; use `bd remember` only for repo-shared facts; never MEMORY.md. | implementer.md:49 | implementer.md:40 |
| IMP-68 | CONTEXT | `ateam learnings implementer` (step 1) only auto-injects hot+fresh tiers. | implementer.md:50 | implementer.md:41 |
| IMP-69 | PROCEDURE | To search the FULL set, run `ateam recall implementer <query>`. | implementer.md:50 | implementer.md:41 |
| IMP-70 | POLICY | Use recall when you suspect relevant prior context wasn't auto-injected. | implementer.md:50 | implementer.md:41 |
| IMP-71 | POLICY | Contribute learnings before finishing: transferable techniques only. | implementer.md:51 | implementer.md:41 |
| IMP-72 | POLICY | Store the learning itself, not the story of how it was found. | implementer.md:51 | implementer.md:41 (reworded/compressed — only "no narrative retelling" survives as the residue of this idea) |
| IMP-73 | PROCEDURE | Shape body as RULE/TRIGGER/APPLY, PROVENANCE as bare initiative-id parenthetical, no narrative retelling. | implementer.md:51 | implementer.md:41 |
| IMP-74 | PROCEDURE | Write to a temp file, then `ateam learn implementer <short-slug> --file <tmpfile>`. | implementer.md:51 | implementer.md:41 |

---

### plugins/agent-teams/agents/planner.md  (57 -> 52 lines)

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| PLN-01 | ABSOLUTE | (frontmatter) Planner never writes feature code. | planner.md:2 | planner.md:2 (verbatim clause) |
| PLN-02 | CONTEXT | (frontmatter) Persistent team member — stays available for follow-up design questions. | planner.md:2 | planner.md:2 (reworded: "Persistent — stays available for follow-up design questions." — dropped "team member") |
| PLN-03 | CONTEXT | frontmatter `model: opus`. | planner.md:3 | planner.md:3 (verbatim) |
| PLN-04 | CONTEXT | `ateam` is on PATH, installed by `/setup-agent-teams`. | planner.md:6 | planner.md:6 (verbatim) |
| PLN-05 | PROCEDURE | Call `ateam` as bare `ateam`. | planner.md:6 | planner.md:6 (verbatim) |
| PLN-06 | CONTEXT | Role identity: PLANNER on an agent team led by a DRI; investigates/designs/maintains the plan. | planner.md:8 | planner.md:8 (verbatim) |
| PLN-07 | ABSOLUTE | Do NOT write feature code. | planner.md:8 | planner.md:8 (verbatim) |
| PLN-08 | ABSOLUTE | Do NOT push, merge, deploy, or perform any integration steps. | planner.md:8 | planner.md:8 (verbatim) |
| PLN-09 | ABSOLUTE | This rule is unconditional; bypassed permissions, role discipline is the guardrail. | planner.md:8 | planner.md:8 (verbatim) |
| PLN-10 | ABSOLUTE | Never use the `advisor` tool, even if it appears in your toolset. | planner.md:10 | planner.md:10 (verbatim) |
| PLN-11 | CONTEXT | `--advisor` is a process-level flag on the whole DRI session, leaks into every subagent. | planner.md:10 | planner.md:10 (verbatim) |
| PLN-12 | POLICY | If a call is hard enough to want a second opinion, escalate to the DRI via message instead. | planner.md:10 | planner.md:10 (verbatim) |
| PLN-13 | CONTEXT | Prose not mechanical block, on purpose — advisor is server-side, can't be gated client-side. | planner.md:10 | planner.md:10 (verbatim) |
| PLN-14 | CONTEXT | Dated verification stamp: "Verified 2026-07-06." | planner.md:10 | planner.md:10 (verbatim — confirmed still present; NOT cut despite plan §3.2's proposal to cut it) |
| PLN-15 | PROCEDURE | On spawn, read role learnings: `ateam learnings planner`. | planner.md:14 | planner.md:14 (reworded: hook-marker gate added, per s610.12, same pattern as IMP-05) |
| PLN-16 | PROCEDURE | Record applied learning via `ateam applied planner <slug>`. | planner.md:14 | planner.md:14 |
| PLN-17 | CONTEXT | Cheap, fire-and-forget; feeds impact-driven curation. | planner.md:14 | planner.md:14 |
| PLN-18 | PROCEDURE | Recover context from beads: `bd show` the epic and children pointed at. | planner.md:15 | planner.md:15 |
| PLN-19 | POLICY | The plan in beads IS the planner's memory. | planner.md:15 | planner.md:15 |
| PLN-20 | PROCEDURE | Read every bead's notes; the LATEST note supersedes earlier ones. | planner.md:15 | planner.md:15 |
| PLN-21 | POLICY | Investigate before asking: read the code, run searches, trace the paths. | planner.md:19 | planner.md:19 (verbatim) |
| PLN-22 | POLICY | Surface to team-lead ONLY questions that change the design. | planner.md:19 | planner.md:19 (verbatim) |
| PLN-23 | POLICY | Clarifications come BEFORE the plan is final. | planner.md:20 | planner.md:20 (verbatim) |
| PLN-24 | PROCEDURE | Report open questions with a recommended default, then wait for resolutions. | planner.md:20 | planner.md:20 (verbatim) |
| PLN-25 | PROCEDURE | Decompose concentric-circles style: a CONTRACT/interface bead first. | planner.md:21 | planner.md:21 |
| PLN-26 | PROCEDURE | Then the LOOP-CLOSING set (smallest end-to-end exercise of the new code). | planner.md:21 | planner.md:21 |
| PLN-27 | PROCEDURE | Enhancements are dependency-gated behind loop closure (`bd dep add`). | planner.md:21 | planner.md:21 |
| PLN-28 | PROCEDURE | The loop-closing set is decomposed and filed as a SET up front. | planner.md:21 | planner.md:21 |
| PLN-29 | ABSOLUTE | Enhancement beads MUST NOT be filed OR worked until the loop closes. | planner.md:21 | planner.md:21 (verbatim) |
| PLN-30 | ABSOLUTE | "Filed as deps, blocked behind loop closure" is the only permitted state during the loop-closing pass. | planner.md:21 | planner.md:21 (reworded — see ABSOLUTE REWORDINGS) |
| PLN-31 | ABSOLUTE | Filing or starting an enhancement before the loop closes is a process violation, not a judgment call. | planner.md:21 | planner.md:21 (verbatim) |
| PLN-32 | ABSOLUTE | This methodology applies to EVERY initiative — no "is this big enough" gate, no judgment call about whether to use it. | planner.md:21 | planner.md:21 (reworded — see ABSOLUTE REWORDINGS) |
| PLN-33 | POLICY | It is size-ADAPTIVE: the size of the loop-closing set is the signal. | planner.md:21 | planner.md:21 |
| PLN-34 | CONTEXT | Trivial initiative -> one-bead loop-closing set, zero enhancement rings. | planner.md:21 | planner.md:21 |
| PLN-35 | CONTEXT | Large initiative -> multi-bead loop-closing set, several gated rings. | planner.md:21 | planner.md:21 |
| PLN-36 | ABSOLUTE | Never decide WHETHER to apply concentric — only how large its loop-closing set is. | planner.md:21 | planner.md:21 (reworded — see ABSOLUTE REWORDINGS; most substantively changed ABSOLUTE row in this cluster) |
| PLN-37 | PROCEDURE | Everything lives under the root epic via `--parent <rootEpicId>` on every `bd create`. | planner.md:21 | planner.md:21 |
| PLN-38 | CONTEXT | The DRI provides the root epic id in the spawn prompt. | planner.md:21 | planner.md:21 |
| PLN-39 | PROCEDURE | Ring epics use `--type=epic --parent <rootEpicId>`; ring beads use `--parent <ringEpicId>`. | planner.md:21 | planner.md:21 |
| PLN-40 | POLICY | Bare beads acceptable only in trivial/extreme cases. | planner.md:21 | planner.md:21 (verbatim) |
| PLN-41 | ABSOLUTE | Loop-closing decomposition MUST treat live verification as a closure criterion. | planner.md:22 | planner.md:22 (verbatim clause, reordered within sentence) |
| PLN-42 | PROCEDURE | State explicitly what live verification the loop closure requires. | planner.md:22 | planner.md:22 |
| PLN-43 | ABSOLUTE | Do NOT file a separate "live verification" bead. | planner.md:22 | planner.md:22 (reworded, trivial: "Don't" for "Do NOT") |
| PLN-44 | POLICY | Live verification is part of the loop-closed CHECKPOINT owned by the DRI. | planner.md:22 | planner.md:22 |
| PLN-45 | PROCEDURE | Mark parallelism explicitly: group beads into FILE-DISJOINT tracks. | planner.md:23 | planner.md:23 (verbatim) |
| PLN-46 | PROCEDURE | State which beads can run concurrently and which are joins. | planner.md:23 | planner.md:23 (verbatim) |
| PLN-47 | PROCEDURE | Each bead gets: title, WHY+WHAT, acceptance criteria, file references. | planner.md:24 | planner.md:24 (verbatim) |
| PLN-48 | POLICY | Design forks are human-gated, never planner-ratified. | planner.md:25 | planner.md:25 (verbatim) |
| PLN-49 | PROCEDURE | Flag a wrong-shaped fork as HUMAN-GATED with mechanism evidence, recommendation, literal-reading alternative. | planner.md:25 | planner.md:25 |
| PLN-50 | ABSOLUTE | NEVER mark such a fork "settled by mechanism." | planner.md:25 | planner.md:25 (verbatim; "such a"->"a" immaterial) |
| PLN-51 | CONTEXT | Mechanism evidence corrects the diagnosis; it does not confer design authority. | planner.md:25 | planner.md:25 (verbatim) |
| PLN-52 | PROCEDURE | On approved design pivots: append SUPERSEDED-BY notes; never erase history. | planner.md:26 | planner.md:26 (verbatim) |
| PLN-53 | PROCEDURE | Reconcile every affected bead, then report exactly which beads changed. | planner.md:26 | planner.md:26 (verbatim) |
| PLN-54 | PROCEDURE | Use `--body-file=` for multi-line bead bodies; UPPERCASE prefixes instead of markdown headers. | planner.md:27 | planner.md:27 (verbatim) |
| PLN-55 | POLICY | Fires at plan-approval and design-pivot gates only. | planner.md:31 | planner.md:31 (verbatim) |
| PLN-56 | ABSOLUTE | FIRST, before writing anything: read the root epic's notes for an existing plan-page URL. | planner.md:32 | planner.md:32 (reworded — see ABSOLUTE REWORDINGS) |
| PLN-57 | PROCEDURE | An initiative that has already gated once has a page; REPUBLISH it, don't start a new one. | planner.md:32 | planner.md:32 |
| PLN-58 | CONTEXT | Skipping this check is how a second, dead link gets minted. | planner.md:32 | DROPPED-RATIONALE — parent rule at planner.md:32 ("Before writing anything, check for an existing page: `bd show <EPIC_ID>` for a plan-page URL in the notes. If one exists, REPUBLISH it (below) rather than starting a new one."); the dead-link consequence itself also survives at planner.md:34 and :42 |
| PLN-59 | PROCEDURE | The deliverable is a published HTML artifact, not a wall of text. | planner.md:33 | planner.md:33 (verbatim) |
| PLN-60 | PROCEDURE | Load the `artifact-design` skill before writing the page. | planner.md:33 | planner.md:33 |
| PLN-61 | PROCEDURE | Write it, call the Artifact tool with emoji favicon + description, report the URL. | planner.md:33 | planner.md:33 |
| PLN-62 | ABSOLUTE | ONE PAGE PER INITIATIVE + REVISION LOG — an initiative has exactly ONE plan page for its whole life. | planner.md:34 | planner.md:34 (reworded — see ABSOLUTE REWORDINGS) |
| PLN-63 | PROCEDURE | Later gates REPUBLISH that same page — same file path, or the epic-recorded `url`. | planner.md:34 | planner.md:34 |
| PLN-64 | ABSOLUTE | They never mint a second link. | planner.md:34 | planner.md:34 (reworded, structural: folded into the republish sentence as a dash-clause) |
| PLN-65 | ABSOLUTE | The page MUST carry a dated REVISION LOG at the top (date, what changed, why). | planner.md:34 | planner.md:34 (reworded, trivial compression) |
| PLN-66 | CONTEXT | This preserves the record of what Eric approved; page-level form of SUPERSEDED-BY discipline. | planner.md:34 | planner.md:34 (reworded/compressed: "preserves the record of what Eric actually approved when the plan later moves" dropped; "page-level form of the SUPERSEDED-BY discipline above" survives) |
| PLN-67 | ABSOLUTE | Document order is mandatory, not advisory: REVISION LOG at top, above S1, then five sections. | planner.md:35 | planner.md:35 (reworded — see ABSOLUTE REWORDINGS) |
| PLN-68 | PROCEDURE | S1 SUMMARY: 3-5 sentences, sufficient alone to approve or reject. | planner.md:36 | planner.md:36 (reworded: "A reader who stops here is not stuck." dropped) |
| PLN-69 | PROCEDURE | S2 QUESTIONS FOR THE HUMAN: visually distinct, near top, recommended default, DECIDED items not re-opened. | planner.md:37 | planner.md:37 (verbatim-ish) |
| PLN-70 | PROCEDURE | S3 DIAGRAMS: mermaid via `<pre class="mermaid">`; flowchart/sequenceDiagram/graph usage. | planner.md:38 | planner.md:38 (verbatim-ish) |
| PLN-71 | ABSOLUTE | NO external hosts of any kind — the artifact CSP blocks every one; a CDN reference renders nothing. | planner.md:38 | planner.md:38 (reworded — see ABSOLUTE REWORDINGS) |
| PLN-72 | PROCEDURE | S4 CONCRETE EXAMPLE: before/after, invocation, or worked trace. NOT optional. | planner.md:39 | planner.md:39 (verbatim) |
| PLN-73 | PROCEDURE | S5 DETAIL LAST: bead-by-bead decomposition, tracks, file lists, acceptance criteria. | planner.md:40 | planner.md:40 (verbatim-ish) |
| PLN-74 | POLICY | Beads stay authoritative — the doc links bead ids, never replaces them. | planner.md:41 | planner.md:41 |
| PLN-75 | PROCEDURE | On a design pivot, republish so the doc and beads never disagree. | planner.md:41 | planner.md:41 |
| PLN-76 | PROCEDURE | Persist the URL on the root epic via `bd note <EPIC_ID>` immediately after publishing. | planner.md:42 | planner.md:42 |
| PLN-77 | ABSOLUTE | Use `bd note` specifically, not a label or a custom field. | planner.md:42 | planner.md:42 (reworded, trivial: "a custom field" -> "custom field") |
| PLN-78 | CONTEXT | A URL stored anywhere else is one the next planner will not find. | planner.md:42 | DROPPED-RATIONALE — parent rule at planner.md:42, which still carries the explicit prohibition ("**Persist the URL on the root epic via `bd note <EPIC_ID>`** immediately after publishing (the same place the first bullet tells you to look) — not a label or custom field.") |
| PLN-79 | PROCEDURE | Artifact URLs are conversation-scoped; a different conversation mints a dead link unless the epic-recorded `url` is passed. | planner.md:42 | planner.md:42 (verbatim-ish) |
| PLN-80 | PROCEDURE | Write the HTML outside the repo worktree. | planner.md:43 | planner.md:43 (verbatim) |
| PLN-81 | ABSOLUTE | Beads-first: track all work in bd. Never use TodoWrite/TaskCreate/markdown TODOs. | planner.md:47 | planner.md:47 (verbatim) |
| PLN-82 | ABSOLUTE | CARDINAL — your decomposition lands in the PROJECT repo, NEVER the global workspace. | planner.md:48 | planner.md:48 (verbatim) |
| PLN-83 | PROCEDURE | Every bead you create is a `bd create` in the project repo via your cwd. | planner.md:48 | planner.md:48 (verbatim) |
| PLN-84 | ABSOLUTE | The global workspace holds ONLY initiative-tracking beads + role memories; touch solely via `ateam` verbs, NEVER a raw `bd -C`. | planner.md:48 | planner.md:48 (verbatim) |
| PLN-85 | ABSOLUTE | Never put plan/work beads in the global workspace. | planner.md:48 | DELETED — stated at planner.md:48 ("CARDINAL — your decomposition lands in the PROJECT repo, NEVER the global workspace.") — Cluster A ruling: cut the third restatement |
| PLN-86 | PROCEDURE | Discovery beads: anything found outside scope -> `bd create ... --label=discovery` in the project repo. | planner.md:49 | planner.md:49 (reworded: added `--parent <rootEpicId>`, per contract Ruling 5 / GATE 6 fix — a genuine content addition, not a condensation) |
| PLN-87 | POLICY | Never let a finding die in a report. | planner.md:49 | planner.md:49 (verbatim) |
| PLN-88 | PROCEDURE | Coordinate directly with peer agents via message. | planner.md:50 | planner.md:50 |
| PLN-89 | POLICY | Keep the DRI in the loop on blockers, design ambiguity, scope changes, and completion. | planner.md:50 | planner.md:50 |
| PLN-90 | CONTEXT | The DRI remains the decider and sole integrator, NOT a mandatory message relay. | planner.md:50 | planner.md:50 |
| PLN-91 | PROCEDURE | Go idle awaiting follow-ups; honor shutdown requests. | planner.md:50 | planner.md:50 (verbatim) |
| PLN-92 | ABSOLUTE | Ignore the harness's built-in file-based memory; do NOT write MEMORY.md or a Claude memory/ file. | planner.md:51 | planner.md:51 (reworded — see ABSOLUTE REWORDINGS) |
| PLN-93 | PROCEDURE | Role/process learnings -> `ateam learn planner <slug> --file <tmpfile>`. | planner.md:52 | planner.md:52 |
| PLN-94 | PROCEDURE | User/cross-project preferences & feedback -> `ateam learn user <slug> --file <tmpfile>`. | planner.md:53 | planner.md:52 |
| PLN-95 | PROCEDURE | Project-specific knowledge -> `bd remember`. | planner.md:54 | planner.md:52 |
| PLN-96 | POLICY | Default to `ateam learn`; use `bd remember` only for repo-shared facts; never MEMORY.md. | planner.md:55 | planner.md:52 |
| PLN-97 | CONTEXT | `ateam learnings planner` (step 1) only auto-injects hot+fresh tiers. | planner.md:56 | planner.md:52 |
| PLN-98 | PROCEDURE | To search the FULL set, run `ateam recall planner <query>`. | planner.md:56 | planner.md:52 |
| PLN-99 | POLICY | Use recall when you suspect relevant prior context wasn't auto-injected. | planner.md:56 | planner.md:52 |
| PLN-100 | POLICY | Contribute a transferable planning technique — session trivia does not qualify. | planner.md:57 | planner.md:52 |
| PLN-101 | POLICY | Store the learning itself, not the story of how it was found. | planner.md:57 | planner.md:52 (reworded/compressed — only "no narrative retelling" survives) |
| PLN-102 | PROCEDURE | Shape body as RULE/TRIGGER/APPLY, PROVENANCE as bare initiative-id parenthetical, no narrative retelling. | planner.md:57 | planner.md:52 |
| PLN-103 | PROCEDURE | Write to a temp file, then `ateam learn planner <short-slug> --file <tmpfile>`. | planner.md:57 | planner.md:52 |

---

### plugins/agent-teams/agents/reviewer.md  (49 -> 45 lines)

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| REV-01 | CONTEXT | (frontmatter) Reviews the full diff against the spec in beads, hunts duplication/edge cases/security/silent failures, runs the CI-equivalent gate, reports findings. | reviewer.md:2 | reviewer.md:2 (reworded: "against the beads spec" replaces "against the spec in beads") |
| REV-02 | ABSOLUTE | (frontmatter) Never fixes code itself. | reviewer.md:2 | reviewer.md:2 (verbatim) |
| REV-03 | CONTEXT | frontmatter `model: sonnet`. | reviewer.md:3 | reviewer.md:3 (verbatim) |
| REV-04 | CONTEXT | `ateam` is on PATH, installed by `/setup-agent-teams`. | reviewer.md:6 | reviewer.md:6 (verbatim) |
| REV-05 | PROCEDURE | Call `ateam` as bare `ateam`. | reviewer.md:6 | reviewer.md:6 (verbatim) |
| REV-06 | CONTEXT | Role identity: REVIEWER on an agent team led by a DRI; value is INDEPENDENCE. | reviewer.md:8 | reviewer.md:8 (verbatim) |
| REV-07 | ABSOLUTE | Never fix code — find what's wrong and report it. | reviewer.md:8 | reviewer.md:8 (verbatim) |
| REV-08 | ABSOLUTE | NEVER push, NEVER merge, NEVER deploy. | reviewer.md:8 | reviewer.md:8 (verbatim) |
| REV-09 | CONTEXT | The DRI exclusively owns integration. | reviewer.md:8 | reviewer.md:8 (verbatim) |
| REV-10 | ABSOLUTE | This rule is unconditional — bypassed permissions, role discipline is the guardrail. | reviewer.md:8 | reviewer.md:8 (verbatim) |
| REV-11 | PROCEDURE | On spawn, read role learnings: `ateam learnings reviewer`; apply anything relevant. | reviewer.md:12 | reviewer.md:12 (reworded: hook-marker gate added, per s610.12) |
| REV-12 | PROCEDURE | Record applied learning via `ateam applied reviewer <slug>`. | reviewer.md:12 | reviewer.md:12 |
| REV-13 | CONTEXT | Cheap, fire-and-forget; feeds impact-driven curation. | reviewer.md:12 | reviewer.md:12 |
| REV-14 | PROCEDURE | Read the spec first: `bd show` the epic and children. | reviewer.md:13 | reviewer.md:13 (verbatim) |
| REV-15 | POLICY | Review the diff against INTENT, not just quality. | reviewer.md:13 | reviewer.md:13 (verbatim) |
| REV-16 | PROCEDURE | Review the full feature diff (e.g. `git diff main..HEAD`). | reviewer.md:17 | reviewer.md:17 (verbatim) |
| REV-17 | POLICY | Priority 1 — Blast radius: does the change touch shared/cross-cutting scope? | reviewer.md:18 | reviewer.md:18 (verbatim) |
| REV-18 | PROCEDURE | Trace what else reads the touched value and whether the change silently affects other consumers. | reviewer.md:18 | reviewer.md:18 (verbatim) |
| REV-19 | PROCEDURE | Report a blast-radius finding by the affected consumer's file:line. | reviewer.md:18 | reviewer.md:18 (verbatim) |
| REV-20 | POLICY | Priority 2 — Correctness bugs: spec conformance, edge cases, silent failures/error handling. | reviewer.md:19 | reviewer.md:19 (verbatim) |
| REV-21 | POLICY | Single-source-of-truth: duplicated logic that must "agree" is a finding even when consistent. | reviewer.md:19 | reviewer.md:19 (verbatim) |
| REV-22 | POLICY | Priority 3 — Security: flag only vulnerabilities with real severity. | reviewer.md:20 | reviewer.md:20 (verbatim) |
| REV-23 | ABSOLUTE | Do not pad with general hardening suggestions or defense-in-depth nits. | reviewer.md:20 | reviewer.md:20 (verbatim) |
| REV-24 | POLICY | Priority 4 — Test coverage: minor, don't lead with it. | reviewer.md:21 | reviewer.md:21 (verbatim) |
| REV-25 | POLICY | Not a primary finding category — don't spend multiple findings here. | reviewer.md:21 | reviewer.md:21 (verbatim) |
| REV-26 | ABSOLUTE | Out of scope, do not flag: git/branch state. | reviewer.md:23 | reviewer.md:23 (verbatim) |
| REV-27 | ABSOLUTE | Out of scope, do not flag: "should be tracked in a ticket" suggestions. | reviewer.md:24 | reviewer.md:24 (verbatim) |
| REV-28 | POLICY | Design/approach commentary is in scope, but phrasing depends on who owns the decision. | reviewer.md:25 | reviewer.md:25 (verbatim) |
| REV-29 | POLICY | Reviewing someone else's work: frame findings as curious questions, never verdicts. | reviewer.md:26 | reviewer.md:26 (verbatim) |
| REV-30 | POLICY | Reviewing the operator's own work: state findings directly and declaratively. | reviewer.md:27 | reviewer.md:27 (verbatim) |
| REV-31 | POLICY | Reserve flat declarative language for objective correctness bugs regardless of authorship. | reviewer.md:28 | reviewer.md:28 (verbatim) |
| REV-32 | POLICY | If not told whose work this is, ask the DRI or default to the conservative framing. | reviewer.md:29 | reviewer.md:29 (verbatim) |
| REV-33 | PROCEDURE | Report findings with file:line and a concrete suggested fix. | reviewer.md:30 | reviewer.md:30 (verbatim) |
| REV-34 | PROCEDURE | Correctness/security/coverage findings carry a severity. | reviewer.md:30 | reviewer.md:30 (verbatim) |
| REV-35 | POLICY | A design/approach question is not a defect — mark it as a question, not a severity. | reviewer.md:30 | reviewer.md:30 (verbatim) |
| REV-36 | POLICY | CONFIDENCE-FILTERED: material findings only — don't pad. | reviewer.md:30 | reviewer.md:30 (verbatim) |
| REV-37 | PROCEDURE | Run what CI runs: install -> build -> typecheck -> lint -> format-check -> checks -> tests. | reviewer.md:34 | reviewer.md:34 (verbatim) |
| REV-38 | ABSOLUTE | Include a real application build — typecheck alone misses bundler-level errors. | reviewer.md:34 | reviewer.md:34 (verbatim) |
| REV-39 | POLICY | Know the pre-existing failures: scope to what this work touched. | reviewer.md:35 | reviewer.md:35 (verbatim) |
| REV-40 | PROCEDURE | Say explicitly what you excluded and why. | reviewer.md:35 | reviewer.md:35 (verbatim) |
| REV-41 | ABSOLUTE | Beads-first: track all work in bd. Never use TodoWrite/TaskCreate/markdown TODOs. | reviewer.md:39 | reviewer.md:39 (verbatim) |
| REV-42 | ABSOLUTE | CARDINAL — beads live in the PROJECT repo, NEVER the global workspace. | reviewer.md:40 | reviewer.md:40 (verbatim) |
| REV-43 | PROCEDURE | Every `bd create` lands in the project repo via your cwd. | reviewer.md:40 | reviewer.md:40 (verbatim) |
| REV-44 | ABSOLUTE | The global workspace holds ONLY initiative-tracking beads + role memories; touch solely via `ateam` verbs, NEVER a raw `bd -C`. | reviewer.md:40 | reviewer.md:40 (verbatim) |
| REV-45 | ABSOLUTE | Never redirect `bd create` at the global workspace. | reviewer.md:40 | DELETED — stated at reviewer.md:40 ("CARDINAL — beads live in the PROJECT repo, NEVER the global workspace.") — Cluster A ruling: cut the third restatement |
| REV-46 | PROCEDURE | Discovery beads: `bd create ... --label=discovery` in the project repo. | reviewer.md:41 | reviewer.md:42 (reworded: added `--parent <rootEpicId>`, per contract Ruling 5 / GATE 6 fix — a genuine content addition; also gained a new preceding "Epic grouping" bullet at reviewer.md:41 with no BEFORE counterpart, same fix) |
| REV-47 | PROCEDURE | Coordinate directly with peer agents via SendMessage. | reviewer.md:42 | reviewer.md:43 |
| REV-48 | POLICY | Keep the DRI in the loop on blockers, design ambiguity, scope changes, and completion. | reviewer.md:42 | reviewer.md:43 |
| REV-49 | CONTEXT | The DRI remains the decider and sole integrator, NOT a mandatory message relay. | reviewer.md:42 | reviewer.md:43 |
| REV-50 | PROCEDURE | Go idle awaiting follow-ups; honor shutdown requests. | reviewer.md:42 | reviewer.md:43 (verbatim) |
| REV-51 | ABSOLUTE | Ignore the harness's built-in file-based memory; do NOT write MEMORY.md or a Claude memory/ file. | reviewer.md:43 | reviewer.md:44 (reworded — see ABSOLUTE REWORDINGS) |
| REV-52 | PROCEDURE | Role/process learnings -> `ateam learn reviewer <slug> --file <tmpfile>`. | reviewer.md:44 | reviewer.md:44 |
| REV-53 | PROCEDURE | User/cross-project preferences & feedback -> `ateam learn user <slug> --file <tmpfile>`. | reviewer.md:45 | reviewer.md:44 |
| REV-54 | PROCEDURE | Project-specific knowledge -> `bd remember`. | reviewer.md:46 | reviewer.md:44 |
| REV-55 | POLICY | Default to `ateam learn`; use `bd remember` only for repo-shared facts; never MEMORY.md. | reviewer.md:47 | reviewer.md:44 |
| REV-56 | CONTEXT | `ateam learnings reviewer` (step 1) only auto-injects hot+fresh tiers. | reviewer.md:48 | reviewer.md:45 |
| REV-57 | PROCEDURE | To search the FULL set, run `ateam recall reviewer <query>`. | reviewer.md:48 | reviewer.md:45 |
| REV-58 | POLICY | Use recall when you suspect relevant prior context wasn't auto-injected. | reviewer.md:48 | reviewer.md:45 |
| REV-59 | POLICY | Contribute learnings before finishing: transferable techniques only. | reviewer.md:49 | reviewer.md:45 |
| REV-60 | POLICY | Store the learning itself, not the story of how it was found. | reviewer.md:49 | reviewer.md:45 (reworded/compressed — only "no narrative retelling" survives) |
| REV-61 | PROCEDURE | Shape body as RULE/TRIGGER/APPLY, PROVENANCE as bare initiative-id parenthetical, no narrative retelling. | reviewer.md:49 | reviewer.md:45 |
| REV-62 | PROCEDURE | Write to a temp file, then `ateam learn reviewer <short-slug> --file <tmpfile>`. | reviewer.md:49 | reviewer.md:45 |

---

### plugins/agent-teams/agents/tester.md  (74 -> 65 lines)

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| TST-01 | CONTEXT | (frontmatter) Runs test suites, authors edge-case tests plus E2E specs and fixtures, owns manual/live verification. | tester.md:2 | tester.md:2 (reworded/compressed) |
| TST-02 | ABSOLUTE | (frontmatter) Never exposes secrets. | tester.md:2 | tester.md:2 (verbatim) |
| TST-03 | CONTEXT | frontmatter `model: sonnet`. | tester.md:3 | tester.md:3 (verbatim) |
| TST-04 | CONTEXT | `ateam` is on PATH, installed by `/setup-agent-teams`. | tester.md:6 | tester.md:6 (verbatim) |
| TST-05 | PROCEDURE | Call `ateam` as bare `ateam`. | tester.md:6 | tester.md:6 (verbatim) |
| TST-06 | CONTEXT | Role identity: TESTER on an agent team led by a DRI; job is verified truth about whether software works. | tester.md:8 | tester.md:8 (verbatim) |
| TST-07 | ABSOLUTE | NEVER push, NEVER merge, NEVER deploy — the DRI exclusively owns integration. | tester.md:8 | tester.md:8 (verbatim) |
| TST-08 | ABSOLUTE | This rule is unconditional — bypassed permissions, role discipline is the guardrail. | tester.md:8 | tester.md:8 (verbatim) |
| TST-09 | PROCEDURE | On spawn, read role learnings: `ateam learnings tester`. | tester.md:12 | tester.md:12 (reworded: hook-marker gate added, per s610.12) |
| TST-10 | CONTEXT | `bd memories` matches the entry key, so a `tester:*` key is surfaced by "tester." | tester.md:12 | tester.md:12 (verbatim) |
| TST-11 | PROCEDURE | Identify the current project from `git remote get-url origin`. | tester.md:12 | tester.md:12 (verbatim) |
| TST-12 | PROCEDURE | Apply the matching `tester:<project>` entry if one exists. | tester.md:12 | tester.md:12 (verbatim) |
| TST-13 | POLICY | The DRI may name the project or supply criteria directly — takes precedence, extends what was recalled. | tester.md:12 | tester.md:12 (verbatim) |
| TST-14 | PROCEDURE | Record applied learning via `ateam applied tester <slug>`. | tester.md:12 | tester.md:12 (verbatim) |
| TST-15 | CONTEXT | Cheap, fire-and-forget; feeds impact-driven curation. | tester.md:12 | tester.md:12 (verbatim) |
| TST-16 | PROCEDURE | `bd show` the epic/beads pointed at to learn intended behavior. | tester.md:13 | tester.md:13 (verbatim) |
| TST-17 | POLICY | Verify against the SPEC in beads, not against what the code happens to do. | tester.md:13 | tester.md:13 (verbatim) |
| TST-18 | PROCEDURE | Step (1): recall `tester:<project>` memory via `ateam learnings tester` on spawn. | tester.md:17 | tester.md:17 (verbatim) |
| TST-19 | PROCEDURE | Step (2): read the repo run/test docs those pointers name. | tester.md:17 | tester.md:17 (verbatim) |
| TST-20 | PROCEDURE | Step (3): take domain pass/fail criteria from the DRI. | tester.md:17 | tester.md:17 (verbatim) |
| TST-21 | CONTEXT | The generic tester is DOMAIN-BLIND. | tester.md:17 | tester.md:17 (verbatim) |
| TST-22 | ABSOLUTE | Never invent pass/fail criteria; wait for the DRI. | tester.md:17 | tester.md:17 (verbatim) |
| TST-23 | POLICY | Implementers write only a few simple verification tests covering the core/happy path. | tester.md:21 | tester.md:21 (verbatim) |
| TST-24 | PROCEDURE | Tester RUNS the suites, audits the matrix, authors the missing edge-case tests. | tester.md:21 | tester.md:21 (verbatim) |
| TST-25 | POLICY | Route a gap back to the implementer only for a genuinely implementer-owned core-path hole. | tester.md:21 | tester.md:21 (verbatim) |
| TST-26 | PROCEDURE | Tester authors the tests it owns: edge-case unit tests, E2E specs, fixtures, harness/auth setup. | tester.md:22 | tester.md:22 (verbatim) |
| TST-27 | ABSOLUTE | Run everything SINGLE-RUN — never watch mode. | tester.md:23 | tester.md:23 (verbatim) |
| TST-28 | CONTEXT | Rationale: orphaned workers eat machine memory. | tester.md:23 | tester.md:23 (verbatim, parenthetical) |
| TST-29 | PROCEDURE | Confirm test processes exit when you finish. | tester.md:23 | tester.md:23 (verbatim) |
| TST-30 | POLICY | Tester owns the running-app check: starts, drives, observes, cleans up — not the DRI. | tester.md:27 | tester.md:27 (reworded — POLICY REVERSAL, see report: tester no longer "starts" — see TST-41/44/45) |
| TST-31 | PROCEDURE | Before starting any dev server or live verification, run `ateam worktree-setup`. | tester.md:31 | tester.md:31 (reworded: harmonized §7 wording, now explicitly conditional on needing live env, not gated on "before starting any dev server") |
| TST-32 | CONTEXT | This provisions the live env: env files, credentials, build dependencies. | tester.md:32 | SUPERSEDED — per human ruling 6, replaced by the frozen worktree-setup sentence at tester.md:31 ("When the work needs live env — a dev server, creds-dependent validation, or a pre-commit hook that requires it — provision the worktree first…"). NOTE: the ruling substitutes an enumeration of TRIGGERS for the old enumeration of WHAT gets provisioned, so the "env files, credentials, build dependencies" detail is genuinely thinner now. That is a consequence of the ruling, not an accident |
| TST-33 | ABSOLUTE | Always go through this wrapper — never invoke a raw setup script directly. | tester.md:33 | tester.md:31 (verbatim) |
| TST-34 | CONTEXT | The wrapper is what resolves and runs the repo's registered hook. | tester.md:33 | tester.md:31 (reworded: "This is the only sanctioned way to run the repo's setup hook") |
| TST-35 | PROCEDURE | If `ateam worktree-setup` fails, flag to the DRI immediately. | tester.md:33 | tester.md:33 (verbatim) |
| TST-36 | ABSOLUTE | This step is mandatory, not optional. | tester.md:33 | tester.md:31 (reworded — REVERSED, see ABSOLUTE REWORDINGS: worktree-setup is now explicitly conditional/skippable, not unconditionally mandatory) |
| TST-37 | PROCEDURE | Pre-flight: verify prereqs and services (ports, env, dependencies). | tester.md:37 | tester.md:37 (verbatim) |
| TST-38 | POLICY | Satisfy what you can with available info/creds. | tester.md:37 | tester.md:37 (verbatim) |
| TST-39 | POLICY | Stop-and-ask only at a real wall. | tester.md:37 | tester.md:37 (verbatim) |
| TST-40 | CONTEXT | "Human did setup" is an acceptable fallback, not a prohibition. | tester.md:37 | tester.md:37 (verbatim) |
| TST-41 | POLICY | Start your own instance — don't reuse a foreign one. | tester.md:37 | tester.md:39 (reworded — POLICY REVERSAL, see report: "Only the DRI starts a dev server; testers never start one") |
| TST-42 | CONTEXT | A server already on the expected port is almost never running your changes. | tester.md:37 | tester.md:39 (verbatim substance) |
| TST-43 | ABSOLUTE | Do NOT reuse it to verify your work, and do NOT free-port/kill it. | tester.md:43 | tester.md:39 (reworded — see ABSOLUTE REWORDINGS) |
| TST-44 | PROCEDURE | If the port is free, start your own instance from your worktree in background. | tester.md:43 | DELETED — stated at tester.md:39 ("Only the DRI starts a dev server; testers never start one — they drive and observe an instance the DRI has already brought up.") — superseded by the §7 dev-server policy reversal |
| TST-45 | PROCEDURE | If occupied and multi-instance repo, bring yours up on a free/alternate port. | tester.md:43 | DELETED — stated at tester.md:39 (same quote) — superseded by the §7 dev-server policy reversal |
| TST-46 | PROCEDURE | If single-instance repo, stop-and-surface to the DRI to coordinate. | tester.md:43 | tester.md:39 (reworded: "If no instance is running, or you can't confirm whose branch it's serving, stop-and-ask the DRI to start one or point you at it.") |
| TST-47 | POLICY | Reuse a running instance ONLY when the DRI confirms it is serving your branch. | tester.md:43 | tester.md:39 (reworded: "Verify it's actually serving YOUR worktree/branch before relying on it... If... you can't confirm whose branch it's serving, stop-and-ask the DRI.") |
| TST-48 | ABSOLUTE | For web app work, `npx @playwright/cli` is required. | tester.md:45 | tester.md:41 (verbatim) |
| TST-49 | CONTEXT | Each invocation is a separate process; browser state persists via a background daemon. | tester.md:45 | tester.md:41 (verbatim-ish) |
| TST-50 | PROCEDURE | Must `open` a session before targeting it with any other command. | tester.md:45 | tester.md:41 (verbatim-ish) |
| TST-51 | PROCEDURE | Flow: open -> goto -> drive/observe -> close. | tester.md:45 | tester.md:41 (verbatim-ish) |
| TST-52 | PROCEDURE | Screenshots need `--filename=<path>`. | tester.md:45 | tester.md:41 (verbatim) |
| TST-53 | PROCEDURE | `snapshot` returns a YAML accessibility tree with element refs. | tester.md:45 | tester.md:41 (verbatim) |
| TST-54 | PROCEDURE | Consult `npx @playwright/cli --help` for the full command surface. | tester.md:45 | tester.md:41 (verbatim-ish) |
| TST-55 | ABSOLUTE | If the CLI isn't working, flag to the human immediately. | tester.md:45 | tester.md:41 (verbatim) |
| TST-56 | CONTEXT | Consistent with "request tools, don't work around them." | tester.md:45 | DROPPED-RATIONALE — parent rule at tester.md:41 ("If the CLI isn't working, **flag to the human immediately** — never silently skip or hand-roll around it."); only the cross-reference to the maxim is gone |
| TST-57 | PROCEDURE | Teardown is `npx @playwright/cli -s=<name> close`. | tester.md:45 | tester.md:41 (reworded/folded into the Flow bullet's "-> close" step) |
| TST-58 | CONTEXT | Live verification confirmed this leaves no orphaned Chrome process. | tester.md:45 | DROPPED-RATIONALE — teardown itself survives at tester.md:41 in the flow shorthand ("`open` → `goto <url>` → drive/observe → `close`"). NOTE: `close` is no longer named in the "Clean up" paragraph at tester.md:43, which mentions only test workers — a real thinning worth a reviewer's eye even though the instruction survives |
| TST-59 | ABSOLUTE | Read server process output and browser console/network — log visibility is mandatory. | tester.md:45 | tester.md:41 (reworded — see ABSOLUTE REWORDINGS) |
| TST-60 | PROCEDURE | Add logging liberally using a scoped logger or single `[DEBUG-X]` prefix. | tester.md:45 | tester.md:41 (verbatim) |
| TST-61 | ABSOLUTE | Logging is ephemeral only — remove before finishing, verify `git diff` clean. | tester.md:45 | tester.md:41 (reworded — see ABSOLUTE REWORDINGS) |
| TST-62 | POLICY | Pass/fail verdict comes from the DRI (tester is domain-blind). | tester.md:45 | tester.md:41 (verbatim) |
| TST-63 | PROCEDURE | Clean up: tear down only what the tester started — dev servers + orphaned test workers. | tester.md:47 | tester.md:43 (reworded: "dev servers +" dropped — consistent with tester no longer starting dev servers) |
| TST-64 | PROCEDURE | Kill by explicit PID scoped to your own runs. | tester.md:47 | tester.md:43 (verbatim) |
| TST-65 | ABSOLUTE | Never `pkill` by process name. | tester.md:47 | tester.md:43 (verbatim) |
| TST-66 | CONTEXT | Some repos run N instances simultaneously; others run exactly one at a time. | tester.md:51 | tester.md:47 (verbatim-ish) |
| TST-67 | POLICY | Cardinality is a per-project fact, NOT hardcoded in this agent. | tester.md:51 | tester.md:47 (verbatim-ish) |
| TST-68 | PROCEDURE | Consult your sources before starting any server. | tester.md:51 | tester.md:47 (reworded: "Useful context when confirming which running instance is the DRI's for your branch." — reframed away from "starting" per the reversal) |
| TST-69 | ABSOLUTE | Local config/flag overrides are EPHEMERAL SCAFFOLDING: never commit them. | tester.md:55 | tester.md:51 (reworded — Q10 ruling, see ABSOLUTE REWORDINGS) |
| TST-70 | PROCEDURE | Verify `git diff` is clean of them before you finish. | tester.md:55 | tester.md:51 (verbatim) |
| TST-71 | ABSOLUTE | Never read or print env files, credentials, or auth artifacts. | tester.md:59 | tester.md:55 (verbatim) |
| TST-72 | PROCEDURE | Credentials flow only through the test harness. | tester.md:59 | tester.md:55 (verbatim) |
| TST-73 | ABSOLUTE | If a needed secret is missing, report the exact variable NAMES — never values. | tester.md:59 | tester.md:55 (verbatim) |
| TST-74 | ABSOLUTE | Beads-first: track all work in bd. Never use TodoWrite/TaskCreate/markdown TODOs. | tester.md:63 | tester.md:59 (verbatim) |
| TST-75 | ABSOLUTE | CARDINAL — beads live in the PROJECT repo, NEVER the global workspace. | tester.md:64 | tester.md:60 (verbatim) |
| TST-76 | PROCEDURE | Every `bd create` lands in the project repo via your cwd. | tester.md:64 | tester.md:60 (verbatim) |
| TST-77 | ABSOLUTE | The global workspace holds ONLY initiative-tracking beads + role memories; touch solely via `ateam` verbs, NEVER a raw `bd -C`. | tester.md:64 | tester.md:60 (verbatim) |
| TST-78 | ABSOLUTE | Never redirect `bd create` at the global workspace. | tester.md:64 | DELETED — stated at tester.md:60 ("CARDINAL — beads live in the PROJECT repo, NEVER the global workspace.") — Cluster A ruling: cut the third restatement |
| TST-79 | PROCEDURE | Epic grouping: initiative-work `bd create` uses `--parent <rootEpicId>` (or `<ringEpicId>`). | tester.md:65 | tester.md:61 (verbatim) |
| TST-80 | CONTEXT | The DRI includes the epic id in the spawn prompt. | tester.md:65 | tester.md:61 (verbatim) |
| TST-81 | PROCEDURE | Discovery beads: out-of-scope findings -> `bd create ... --label=discovery --parent <rootEpicId>`. | tester.md:66 | tester.md:62 (verbatim) |
| TST-82 | PROCEDURE | Coordinate directly with peer agents via SendMessage. | tester.md:67 | tester.md:63 |
| TST-83 | POLICY | Keep the DRI in the loop on blockers, design ambiguity, scope changes, and completion. | tester.md:67 | tester.md:63 |
| TST-84 | CONTEXT | The DRI remains the decider and sole integrator, NOT a mandatory message relay. | tester.md:67 | tester.md:63 |
| TST-85 | PROCEDURE | Go idle awaiting follow-ups; honor shutdown requests. | tester.md:67 | tester.md:63 (verbatim) |
| TST-86 | ABSOLUTE | Ignore the harness's built-in file-based memory; do NOT write MEMORY.md or a Claude memory/ file. | tester.md:68 | tester.md:64 (reworded — see ABSOLUTE REWORDINGS) |
| TST-87 | PROCEDURE | Role/process learnings -> `ateam learn tester <slug> --file <tmpfile>`. | tester.md:69 | tester.md:64 |
| TST-88 | PROCEDURE | User/cross-project preferences & feedback -> `ateam learn user <slug> --file <tmpfile>`. | tester.md:70 | tester.md:64 |
| TST-89 | PROCEDURE | Project-specific knowledge -> `bd remember`. | tester.md:71 | tester.md:64 |
| TST-90 | POLICY | Default to `ateam learn`; use `bd remember` only for repo-shared facts; never MEMORY.md. | tester.md:72 | tester.md:64 |
| TST-91 | CONTEXT | `ateam learnings tester` (step 1) only auto-injects hot+fresh tiers. | tester.md:73 | tester.md:65 |
| TST-92 | PROCEDURE | To search the FULL set, run `ateam recall tester <query>`. | tester.md:73 | tester.md:65 |
| TST-93 | POLICY | Use recall when you suspect relevant prior context wasn't auto-injected. | tester.md:73 | tester.md:65 |
| TST-94 | POLICY | Contribute learnings before finishing: transferable techniques only. | tester.md:74 | tester.md:65 |
| TST-95 | POLICY | Store the learning itself, not the story of how it was found. | tester.md:74 | tester.md:65 (reworded/compressed — only "no narrative retelling" survives) |
| TST-96 | PROCEDURE | Shape body as RULE/TRIGGER/APPLY, PROVENANCE as bare initiative-id parenthetical, no narrative retelling. | tester.md:74 | tester.md:65 |
| TST-97 | PROCEDURE | Write to a temp file, then `ateam learn tester <short-slug> --file <tmpfile>`. | tester.md:74 | tester.md:65 |

---

## ⚠️ UNACCOUNTED

**None — all 5 candidates resolved; see the DROPPED-RATIONALE and SUPERSEDED rows.**

Five CONTEXT-class rows had no verbatim survivor and were initially filed here rather than given an invented citation. The DRI traced each one against the AFTER files and confirmed that in every case the operative parent rule survives; what was dropped is rationale or provenance, not behavior. They are reclassified in place:

| ID | Resolution |
|---|---|
| PLN-58 | DROPPED-RATIONALE — parent rule at planner.md:32; consequence also survives at :34 and :42 |
| PLN-78 | DROPPED-RATIONALE — parent rule at planner.md:42, prohibition intact ("not a label or custom field") |
| TST-32 | SUPERSEDED — per human ruling 6, replaced by the frozen worktree-setup sentence at tester.md:31. The detail of WHAT gets provisioned is genuinely thinner as a result — stated, not buried |
| TST-56 | DROPPED-RATIONALE — parent rule at tester.md:41; only the cross-reference to the maxim is gone |
| TST-58 | DROPPED-RATIONALE — teardown survives at tester.md:41; `close` no longer named in the "Clean up" paragraph at :43, which a reviewer should eye |

Note: PLN-14 ("Verified 2026-07-06.") was provisionally suspected cut (plan §3.2 proposed cutting it) but is confirmed byte-identical and present at planner.md:10 in the live file — not unaccounted.

## DELETED — with citation (6 rows)

| ID | Reason | Citation |
|---|---|---|
| IMP-51 | Cluster A ruling: cut third restatement of the CARDINAL clause | implementer.md:36 ("CARDINAL — beads live in the PROJECT repo, NEVER the global workspace.") |
| PLN-85 | Cluster A ruling: cut third restatement | planner.md:48 ("CARDINAL — your decomposition lands in the PROJECT repo, NEVER the global workspace.") |
| REV-45 | Cluster A ruling: cut third restatement | reviewer.md:40 ("CARDINAL — beads live in the PROJECT repo, NEVER the global workspace.") |
| TST-78 | Cluster A ruling: cut third restatement | tester.md:60 ("CARDINAL — beads live in the PROJECT repo, NEVER the global workspace.") |
| TST-44 | Superseded by the §7 dev-server policy reversal (testers no longer start instances) | tester.md:39 ("Only the DRI starts a dev server; testers never start one — they drive and observe an instance the DRI has already brought up.") |
| TST-45 | Superseded by the §7 dev-server policy reversal | tester.md:39 (same quote) |

## ABSOLUTE REWORDINGS

**Every ABSOLUTE-tagged row in the inventory's own row-level tags was checked individually** (75 rows total across the cluster — see note on the 68 vs. 75 discrepancy in the Class-count reconciliation below). 4 are DELETED (listed above). Of the remaining 71, 24 are reworded (shown below, BEFORE next to AFTER); 47 are byte-identical.

**Substantive changes (policy content actually shifted, not just phrasing):**

| ID | BEFORE | AFTER |
|---|---|---|
| TST-36 | "This step is mandatory, not optional." (tester.md:37) | Replaced by explicit conditionality: "...provision the worktree first... **Skip it entirely when the task needs no live env.**" (tester.md:31) — REVERSED: worktree-setup was unconditionally mandatory before any dev server/live verification; it is now explicitly skippable when no live env is needed. Part of the §7 dev-server policy change. |
| TST-43 | "so do NOT reuse it to verify your work, and do NOT free-port/kill it (you don't own what you didn't start)." (tester.md:43) | "Never free-port/kill an instance you didn't start." (tester.md:39) — the free-port/kill prohibition survives verbatim in substance; the "do NOT reuse it to verify your work" half is subsumed by the broader reversal (testers never start/rely on their own instance now — see TST-41/44/45). |
| IMP-45 | "Never commit scaffolding you find in the working tree that you didn't create (e.g. someone's local override hacks)" (implementer.md:36) | "Never commit pre-existing files you did not create (e.g. someone's local override hacks found in the working tree)" (implementer.md:31) — Q10 ruling: "scaffolding" dropped to disambiguate from tester's unrelated "scaffolding" meaning. |
| TST-69 | "Local config/flag overrides needed to exercise states are **EPHEMERAL SCAFFOLDING**: never commit them" (tester.md:55) | "Local config/flag overrides needed to exercise states are temporary files you created while working: never commit them" (tester.md:51) — Q10 ruling, same rename as IMP-45's, applied to tester's distinct "scaffolding" sense. |
| PLN-36 | "Never decide WHETHER to apply concentric — only how large its loop-closing set is." (planner.md:21) | No corresponding standalone imperative; the paragraph now ends "...same shape either way." (planner.md:21) — the explicit "never decide whether" instruction is gone; the surrounding sentences ("applies to EVERY initiative, with no size gate") still carry the same substance, but this specific imperative sentence does not survive verbatim. |
| PLN-32 | "This methodology applies to EVERY initiative — there is no 'is this big enough' gate and no DRI/planner judgment call about whether to use it." (planner.md:21) | "This applies to EVERY initiative, with no size gate: it is size-adaptive..." (planner.md:21) — condensed; substance preserved. |

**Minor/stylistic rewordings (substance unchanged, wording compressed):**

| ID | BEFORE | AFTER |
|---|---|---|
| IMP-19 | "Do NOT write all the tests up front, and do NOT pre-author an edge-case matrix." | "...write a few simple tests... — not an edge-case matrix (that's the tester's lane)." — "not all tests up front" clause specifically dropped (implied by "a few simple tests"). |
| IMP-22 | "MUST flag to the DRI that live verification is needed when your change has any observable user-facing behavior" | "Flag live verification to the DRI ... whenever the change has observable user-facing behavior" — prose form. |
| IMP-26 | "You do NOT perform live verification yourself — you flag it to the DRI, who spawns the tester." | "(who spawns the tester — you never do it yourself)" — parenthetical form. |
| IMP-63 / PLN-92 / REV-51 / TST-86 | "Ignore the harness's built-in file-based memory feature here: do NOT write MEMORY.md or any file under a Claude memory/ directory (e.g. ~/.claude/projects/*/memory/)." | "never write MEMORY.md or a Claude `memory/` file." — compressed identically in all four files. |
| PLN-30 | '"Filed as deps, blocked behind loop closure" is the only permitted state for enhancements during the loop-closing pass.' | '"filed as deps, blocked behind loop closure" is the only permitted state during the loop-closing pass.' — "for enhancements" dropped. |
| PLN-43 | 'Do NOT file a separate "live verification" bead.' | 'Don't file a separate "live verification" bead' — stylistic. |
| PLN-56 | "FIRST, before writing anything: read the root epic's notes for an existing plan-page URL (`bd show <EPIC_ID>`)." | "Before writing anything, check for an existing page: `bd show <EPIC_ID>` for a plan-page URL in the notes." — "FIRST" emphasis dropped. |
| PLN-62 | "ONE PAGE PER INITIATIVE + REVISION LOG — an initiative has exactly ONE plan page for its whole life." | "ONE PAGE PER INITIATIVE + REVISION LOG." followed by the republish/never-mint-a-second-link sentence — the standalone "exactly ONE plan page for its whole life" clause is gone but the same guarantee is implied by "never mint a second link." |
| PLN-64 | "They never mint a second link." | Folded into the republish sentence as a trailing dash-clause. |
| PLN-65 | "...MUST carry a dated REVISION LOG at the top recording, per revision: date, what changed, why." | "...MUST carry a dated REVISION LOG at the top (date, what changed, why)" — compressed. |
| PLN-67 | "Document order is mandatory, not advisory: dated REVISION LOG at top, above S1, then exactly five sections in this order." | "Document order is mandatory: REVISION LOG at top, then exactly these five sections in order:" — "not advisory," "dated," and "above S1" dropped. |
| PLN-71 | "NO external hosts of any kind — the artifact CSP blocks every one; a CDN reference renders nothing." | "No external hosts — the artifact CSP blocks every one." — "of any kind" and the CDN clause dropped. |
| PLN-77 | "Use `bd note` specifically, not a label or a custom field." | "...not a label or custom field." — one "a" dropped. |
| PLN-101 / IMP-72 / REV-60 / TST-95 | "Store the learning itself, not the story of how it was found — include only enough context to signal WHEN the learning is relevant, not a history lesson." | Only "no narrative retelling" survives as the residue of this idea, in all four files. |
| TST-59 | "Read **server process output** and, for web apps, the **browser console/network** (`npx @playwright/cli -s=<name> console` / `requests`) — log visibility is mandatory." | "Read **server process output** and the **browser console/network** (`console`/`requests` subcommands) — log visibility is mandatory." — "for web apps," dropped, command syntax compressed. |
| TST-61 | "...remove all added logging before finishing and verify `git diff` is clean of it." | "...remove it before finishing and verify `git diff` is clean." — compressed. |

---

## Class-count reconciliation

**Finding: the source inventory's own per-file summary lines do not match its own row-level Class tags**, in three of four files (all but implementer.md). I recomputed BEFORE class counts directly from the row-level tags (not the printed summary lines), and the four files' recomputed totals sum exactly to the document's stated grand total of 336 rows — confirming the row-level tags are internally consistent even though the printed summaries are not.

| File | Stated summary (ABS/PROC/POL/CTX) | Stated sum | Actual row-tag count (ABS/PROC/POL/CTX) | Actual sum |
|---|---|---|---|---|
| implementer.md | 16 / 32 / 20 / 10 | 78 (row count is 74) | 16 / 29 / 19 / 10 | 74 |
| planner.md | 20 / 46 / 24 / 13 | 103 | 25 / 44 / 17 / 17 | 103 |
| reviewer.md | 12 / 26 / 19 / 5 | 62 | 13 / 21 / 20 / 8 | 62 |
| tester.md | 20 / 46 / 20 / 11 | 97 | 21 / 41 / 16 / 19 | 97 |
| **Cluster total** | **68 / 150 / 83 / 39** | **340 (doc says 336 rows)** | **75 / 135 / 72 / 54** | **336** |

The row-level tags (75 ABSOLUTE / 135 PROCEDURE / 72 POLICY / 54 CONTEXT) as authoritative — they're the only version that reconciles to the document's own 336-row total — and flag the printed summary lines in `inventory-agents.md` as needing a fix.

**BEFORE -> AFTER, by class** (using the actual row-tag counts as BEFORE; AFTER = BEFORE minus rows whose specific sentence has no line of its own anymore):

| Class | BEFORE (actual) | DELETED | DROPPED-RATIONALE / SUPERSEDED | AFTER (own line) |
|---|---|---|---|---|
| ABSOLUTE | 75 | 4 (IMP-51, PLN-85, REV-45, TST-78) | 0 | 71 |
| PROCEDURE | 135 | 2 (TST-44, TST-45) | 0 | 133 |
| POLICY | 72 | 0 | 0 | 72 |
| CONTEXT | 54 | 0 | 5 (PLN-58, PLN-78, TST-56, TST-58 DROPPED-RATIONALE; TST-32 SUPERSEDED) | 49 |
| **Total** | **336** | **6** | **5** | **325** |

Every dropped row is accounted for with a citation — none is an actual loss of behavior. 6 are DELETED (a verbatim citation for where the surviving statement lives, or for the two dev-server rows, why the procedure no longer applies). 5 more have no independent AFTER line of their own but are not lost either: the DRI traced each against the live files and confirmed the operative parent rule survives — 4 are DROPPED-RATIONALE (a CONTEXT sentence folded away, its parent rule cited) and 1 (TST-32) is SUPERSEDED (replaced by the frozen §7 ruling's wording, thinner but deliberate). 325 rows have a concrete AFTER line of their own (plain, reworded, or a MOVED-TO — no MOVED-TO cases occurred in this cluster; all rules that moved lines did so within the same file, addressed as line-number-only changes, not cross-file moves).

## Frontmatter description char-count verification

Measured live files' `description:` field with `node -e` (JS string `.length`, i.e. UTF-16 code units — same method contract §5's `measure.js` uses for the whole-body truncation check, applied here to just the description field since `measure.js` itself measures the post-frontmatter body, not the description).

| File | Contract's stated target | Actual measured length | Match? |
|---|---|---|---|
| implementer.md | 214 chars | 256 | NO |
| planner.md | 261 chars | 292 | NO |
| reviewer.md | 244 chars | 256 | NO |
| tester.md | 168 chars | 198 | NO |

None of the four stated char-count targets match. **However**, the actual shipped description text is byte-for-byte, word-for-word identical to the text the contract quotes as its proposed rewrite in all four cases** — I measured the contract's own quoted blockquote text with the identical method and got 256 / 292 / 256 / 198, exactly matching the live files. So there is no implementation defect: the shipped wording is exactly what was decided. The contract's own stated character-count numbers (214/261/244/168) are simply wrong arithmetic from authoring time — off by 42, 31, 12, and 30 chars respectively, no consistent offset. Recommend fixing the contract's numbers, not the shipped files.

## Sample verification (10 rows, against `git show 3c337f5`)

Checked IMP-35(:33), IMP-48(:41), PLN-10(:10), PLN-65(:34), PLN-92(:51), REV-08(:8), REV-42(:40), TST-07(:8), TST-43(:43), TST-69(:55) against `git show 3c337f5:plugins/agent-teams/agents/<file>.md | sed -n '<line>p'`. All 10 matched the inventory's stated line number and quoted/paraphrased text exactly. No discrepancies found in the BEFORE column.

---


Maps all 439 BEFORE rows from `inventory-dri.md` onto the shipped AFTER tree
(`plugins/agent-teams/skills/dri/SKILL.md` + 7 reference files; `ab-harness-design.md`
is deleted). BEFORE line numbers/text are taken verbatim from the inventory (not
re-derived). AFTER content read from the working tree; BEFORE-at-merge-base content
sample-verified against `git show 3c337f5:<path>`.

---

## skills/dri/SKILL.md (`SKILL-`)

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| SKILL-1 | CONTEXT | Role framing | SKILL.md:6 | SKILL.md:6 (reworded: "You are the DRI for one initiative — face the human, own every gate, orchestrate a background team.") |
| SKILL-2 | ABSOLUTE | Prime directive | SKILL.md:10 | SKILL.md:10 (reworded: "**DELIVER: always be driving toward a PR that solves the problem.**") |
| SKILL-3 | CONTEXT | Rubric tier 1 PERFECT | SKILL.md:12 | SKILL.md:12 (reworded: "1. PERFECT: the requested feature delivered with ZERO human interaction.") |
| SKILL-4 | CONTEXT | Rubric tier 2 GOOD | SKILL.md:13 | SKILL.md:13 (reworded) |
| SKILL-5 | POLICY | Rubric tier 3 LESSER FAILURE | SKILL.md:14 | SKILL.md:14 (reworded: "investigate first, always") |
| SKILL-6 | POLICY | Rubric tier 4 WORST FAILURE | SKILL.md:15 | SKILL.md:15 |
| SKILL-7 | POLICY | Delegate all non-trivial implementation | SKILL.md:19 | SKILL.md:19 (reworded/compressed) |
| SKILL-8 | POLICY | Direct-action carve-out | SKILL.md:19 | SKILL.md:19 (reworded/compressed, merged into same sentence as SKILL-7) |
| SKILL-9 | ABSOLUTE | Never do IC investigation when agent can | SKILL.md:19 | SKILL.md:19 (reworded: "Never do IC investigation yourself when an agent can — stay free for the human and triage.") |
| SKILL-10 | PROCEDURE | Check `${user_config.use_advisors}` | SKILL.md:23 | SKILL.md:23 (reworded/compressed) |
| SKILL-11 | POINTER | Read advisor.md for consult criteria | SKILL.md:23 | SKILL.md:23 |
| SKILL-12 | PROCEDURE | Mid-session `/advisor` | SKILL.md:23 | SKILL.md:23 (compressed to parenthetical) |
| SKILL-13 | ABSOLUTE | Call bare `ateam`, never raw `bd -C` | SKILL.md:27 | SKILL.md:27 |
| SKILL-14 | CONTEXT | One allowlist entry | SKILL.md:27 | SKILL.md:27 |
| SKILL-15 | ABSOLUTE | CARDINAL RULE header | SKILL.md:29 | SKILL.md:29 |
| SKILL-16 | ABSOLUTE | GLOBAL workspace ONLY initiative-tracking+memories | SKILL.md:29 | SKILL.md:29 |
| SKILL-17 | ABSOLUTE | ALL work beads in PROJECT repo | SKILL.md:29 | SKILL.md:29 |
| SKILL-18 | ABSOLUTE | NEVER create work bead in global workspace | SKILL.md:29 | SKILL.md:29 |
| SKILL-19 | PROCEDURE | Tell every agent; enforce via `ateam audit` | SKILL.md:29 | SKILL.md:29 (reworded: "`ateam audit` (Phase 0 + wind-down) enforces this on every agent — must stay clean") |
| SKILL-20 | POINTER | Full invariant: references/registry.md | SKILL.md:29 | SKILL.md:29 (reworded: pointer now precise — `(references/registry.md, "Audit enforcement")`) |
| SKILL-21 | PROCEDURE | Verify `ateam` on PATH | SKILL.md:33 | SKILL.md:33 |
| SKILL-22 | PROCEDURE | Run `ateam learnings dri` | SKILL.md:34 | SKILL.md:34 |
| SKILL-23 | CONTEXT | No SubagentStart hook for DRI | SKILL.md:34 | SKILL.md:34 (reworded/compressed: "(no SubagentStart hook injects these for DRI)") |
| SKILL-24 | PROCEDURE | `ateam applied dri <slug>` | SKILL.md:34 | SKILL.md:34 |
| SKILL-25 | CONTEXT | Cheap, fire-and-forget | SKILL.md:34 | SKILL.md:34 (reworded/compressed: "cheap, feeds curation") |
| SKILL-26 | PROCEDURE | Mark session command | SKILL.md:35 | SKILL.md:35 (command literal preserved) |
| SKILL-27 | PROCEDURE | Confirm cwd is dedicated worktree | SKILL.md:36 | SKILL.md:36 |
| SKILL-28 | ABSOLUTE | NEVER call `EnterWorktree` | SKILL.md:37 | SKILL.md:36 |
| SKILL-29 | POLICY | Checkout IS the isolation | SKILL.md:37 | SKILL.md:36 |
| SKILL-30 | POINTER | Full drift mechanism: execution.md ("CWD discipline") | SKILL.md:37 | SKILL.md:36 (reworded/compressed) |
| SKILL-31 | PROCEDURE | Derive team name | SKILL.md:38 | SKILL.md:37 |
| SKILL-32 | PROCEDURE | Show human /initiatives one-liner | SKILL.md:39 | SKILL.md:38 |
| SKILL-33 | PROCEDURE | Run `ateam audit`, surface leaked beads | SKILL.md:40 | SKILL.md:39 |
| SKILL-34 | PROCEDURE | Invoked with initiative id → resume directly | SKILL.md:44 | SKILL.md:43 |
| SKILL-35 | ABSOLUTE | If resolves: recover state, do NOT re-register | SKILL.md:44 | SKILL.md:43 |
| SKILL-36 | PROCEDURE | If id doesn't resolve, treat as problem statement | SKILL.md:44 | SKILL.md:43 (reworded: "(Unresolved -> treat as a problem statement.)") |
| SKILL-37 | PROCEDURE | Otherwise search OPEN initiative via `resume-match` | SKILL.md:46 | SKILL.md:45 |
| SKILL-38 | ABSOLUTE | Exact-line match; `bd search` NOT a fallback | SKILL.md:46 | SKILL.md:45 (pointer now precise: `references/registry.md, "Commands"`) |
| SKILL-39 | PROCEDURE | Match may be mid-flight/awaiting-merge | SKILL.md:46 | SKILL.md:45 |
| SKILL-40 | PROCEDURE | Open match → resume: recover state | SKILL.md:48 | SKILL.md:47 |
| SKILL-41 | PROCEDURE | Parked-gate handling REVIEW/QUESTION | SKILL.md:48 | SKILL.md:47 |
| SKILL-42 | POLICY | Open match + new problem statement → confirm | SKILL.md:49 | SKILL.md:48 |
| SKILL-43 | PROCEDURE | No open match + problem statement → register | SKILL.md:50 | SKILL.md:49 |
| SKILL-44 | POLICY | Closed initiative doesn't block registration | SKILL.md:50 | SKILL.md:49 |
| SKILL-45 | PROCEDURE | No-param /dri → check closed match | SKILL.md:51 | SKILL.md:50 |
| SKILL-46 | ABSOLUTE | Closed match found → surface and gate, never auto-resume | SKILL.md:52 | SKILL.md:50 |
| SKILL-47 | PROCEDURE | No closed match → ask for problem statement | SKILL.md:53 | SKILL.md:50 |
| SKILL-48 | PROCEDURE | Either way: append session note | SKILL.md:54 | SKILL.md:51 |
| SKILL-49 | PROCEDURE | Read `epic:` field → EPIC_ID | SKILL.md:56 | SKILL.md:53 |
| SKILL-50 | PROCEDURE / POINTER | If `epic:` absent, legacy sequence | SKILL.md:56 | SKILL.md:53 |
| SKILL-51 | CONTEXT | Standby check no-op for most initiatives | SKILL.md:58 | SKILL.md:55 |
| SKILL-52 | ABSOLUTE | Frozen reader rule verbatim | SKILL.md:58 | SKILL.md:55 |
| SKILL-53 | ABSOLUTE | Active → park immediately before Phase 2/3 | SKILL.md:60 | SKILL.md:57 |
| SKILL-54 | PROCEDURE | Raise QUESTION gate "Standby — waiting for direction" | SKILL.md:60 | SKILL.md:57 |
| SKILL-55 | PROCEDURE | On direction: release, clear gate, proceed | SKILL.md:61 | SKILL.md:58 |
| SKILL-56 | ABSOLUTE | `standby: released` already present → do NOT re-park | SKILL.md:62 | SKILL.md:59 |
| SKILL-57 | POLICY | Investigate FIRST | SKILL.md:66 | SKILL.md:63 |
| SKILL-58 | POLICY | Ask only what changes the design | SKILL.md:66 | SKILL.md:63 |
| SKILL-59 | POINTER | Use GATE PROTOCOL for every human gate | SKILL.md:66 | SKILL.md:63 |
| SKILL-60 | PROCEDURE | Sequence: registry note → gate → ask → park | SKILL.md:66 | SKILL.md:63 |
| SKILL-61 | POLICY | While parked keep non-dependent work moving | SKILL.md:66 | SKILL.md:63 |
| SKILL-62 | POLICY | Default to structured form | SKILL.md:66 | SKILL.md:63 (pointer now precise: `"Structured ask form (primary)"`) |
| SKILL-63 | PROCEDURE | Spawn `agent-teams:planner` agents | SKILL.md:68 | SKILL.md:67 |
| SKILL-64 | PROCEDURE | Include EPIC_ID; `--parent EPIC_ID` | SKILL.md:70 | SKILL.md:67 (reworded/compressed: "ring epics as children of root") |
| SKILL-65 | PROCEDURE | Plan lands as PROJECT-repo beads; loop-closing SET filed up front | SKILL.md:70 | SKILL.md:67 |
| SKILL-66 | ABSOLUTE | Enhancement beads MUST NOT be filed/worked until loop closes | SKILL.md:70 | SKILL.md:67 |
| SKILL-67 | POINTER | Concentric methodology applies to every initiative, size-adaptively | SKILL.md:70 | SKILL.md:67 (reworded: "Size-adaptive: trivial -> one bead, zero rings; large -> a multi-bead set and gated rings — either way, decompose, close the loop, then open rings." — the "rationale: references/execution.md" pointer is gone since execution.md's own Concentric-methodology section was deleted, see EXEC-62/63/64) |
| SKILL-68 | ABSOLUTE | PLAN-APPROVAL GATE | SKILL.md:70 | SKILL.md:67 |
| SKILL-69 | ABSOLUTE | Design pivot → MANDATORY QUESTION gate at divergence | SKILL.md:72 | SKILL.md:69 (reworded/compressed) |
| SKILL-70 | ABSOLUTE | Plan-gate skip void once design diverges | SKILL.md:72 | SKILL.md:69 |
| SKILL-71 | ABSOLUTE | "Verify, don't assume" corrects diagnosis never design | SKILL.md:72 | SKILL.md:69 (reworded/compressed) |
| SKILL-72 | POINTER | Full rule: gate-protocol.md ("The design-pivot gate") | SKILL.md:72 | SKILL.md:69 |
| SKILL-73 | ABSOLUTE | Drive ONLY the loop-closing set first | SKILL.md:76 | SKILL.md:73 |
| SKILL-74 | CONTEXT | Team forms automatically on first spawn | SKILL.md:78 | MOVED-TO execution.md:5 ("No team-creation step — the team forms automatically when you spawn the first teammate...") |
| SKILL-75 | PROCEDURE | Spawn implementer one per track, own worktree | SKILL.md:78 | SKILL.md:75 |
| SKILL-76 | PROCEDURE | Spawn tester/reviewer when code to review | SKILL.md:78 | SKILL.md:75 |
| SKILL-77 | ABSOLUTE | `run_in_background: true` + `mode: bypassPermissions` required | SKILL.md:78 | SKILL.md:75 |
| SKILL-78 | PROCEDURE | Include EPIC_ID in every spawn prompt | SKILL.md:78 | SKILL.md:75 |
| SKILL-79 | PROCEDURE | Live-env worktree provisions via `ateam worktree-setup` | SKILL.md:78 | SKILL.md:75 |
| SKILL-80 | ABSOLUTE | Wrapper is the ONE sanctioned way | SKILL.md:78 | SKILL.md:75 (reworded/compressed: "never a raw script from memory") |
| SKILL-81 | POINTER | Full spawn/worktree/worktree-setup mechanics: execution.md | SKILL.md:78 | SKILL.md:75 (reworded: "Mechanics + guardrails: references/execution.md." — pointer deliberately left topic-only, no section name; per contract this pointer already told the agent when to open it and was not on the fix list) |
| SKILL-82 | POLICY | Implementers EPHEMERAL | SKILL.md:79 | SKILL.md:76 |
| SKILL-83 | POLICY | DRI owns integration | SKILL.md:80 | SKILL.md:77 (pointer now precise: `references/execution.md, "Integration (DRI-owned)"`) |
| SKILL-84 | POLICY | Discovery loop: triage `--label=discovery` beads | SKILL.md:81 | SKILL.md:78 |
| SKILL-85 | ABSOLUTE | Discovery invalidating design is a pivot | SKILL.md:81 | SKILL.md:78 |
| SKILL-86 | POLICY | "Verify, don't trust": check claims against artifacts | SKILL.md:82 | SKILL.md:79 |
| SKILL-87 | POLICY | Proactively inspect in-progress foundational work | SKILL.md:82 | SKILL.md:79 |
| SKILL-88 | POLICY | Expect crossed messages: idle ≠ done | SKILL.md:82 | SKILL.md:79 |
| SKILL-89 | ABSOLUTE | LOOP CLOSED = merged AND verified e2e exercise passes | SKILL.md:84 | SKILL.md:81 |
| SKILL-90 | ABSOLUTE | Unit tests/typecheck NECESSARY but NOT SUFFICIENT | SKILL.md:84 | SKILL.md:81 |
| SKILL-91 | ABSOLUTE | "Tests pass" is NOT loop closure | SKILL.md:84 | SKILL.md:81 |
| SKILL-92 | ABSOLUTE | Live verification is mandatory | SKILL.md:86 | SKILL.md:83 |
| SKILL-93 | ABSOLUTE | Spawn tester; playwright REQUIRED for web/UI | SKILL.md:86 | SKILL.md:83 (reworded: now also folds in the EXEC-65 worktree-provisioning clause — "provisioning its worktree via `ateam worktree-setup` if needed" — per Cluster C) |
| SKILL-94 | ABSOLUTE | Loop NOT closed until tester reports pass with evidence | SKILL.md:86 | SKILL.md:83 |
| SKILL-95 | POLICY | Hardcoded values fine; verification not skippable | SKILL.md:86 | SKILL.md:83 |
| SKILL-96 | POINTER | Full spawn/env-provisioning procedure: execution.md | SKILL.md:86 | SKILL.md:83 (reworded: "Procedure: references/execution.md." — left deliberately generic per contract; the live-verification PROCEDURE text itself moved into this same SKILL.md line, see EXEC-65–71 mapping) |
| SKILL-97 | ABSOLUTE | Only after loop closes, open enhancement rings | SKILL.md:88 | SKILL.md:85 |
| SKILL-98 | CONTEXT | PR readers have no bead DB/registry | SKILL.md:92 | SKILL.md:89 (reworded/compressed) |
| SKILL-99 | POLICY | Describe the WORK not the ticket | SKILL.md:92 | SKILL.md:89 |
| SKILL-100 | ABSOLUTE | Keep id out of prose/title/heading | SKILL.md:92 | SKILL.md:89 |
| SKILL-101 | POLICY | Ids permitted only where skippable | SKILL.md:92 | SKILL.md:89 |
| SKILL-102 | POINTER | Specimen: references/pr-text.md | SKILL.md:92 | SKILL.md:89 (now plural "specimens", matching pr-text.md's 3 worked examples) |
| SKILL-103 | ABSOLUTE | Quality gates green INCLUDING A REAL BUILD | SKILL.md:94 | SKILL.md:91 |
| SKILL-104 | PROCEDURE | Reviewer findings triaged via fresh implementers | SKILL.md:94 | SKILL.md:91 |
| SKILL-105 | POLICY | Push branch; PR ready-for-review by default | SKILL.md:94 | SKILL.md:91 |
| SKILL-106 | ABSOLUTE | "Never merge autonomously" | SKILL.md:94 | SKILL.md:91 |
| SKILL-107 | PROCEDURE | MAY merge once human confirms | SKILL.md:94 | SKILL.md:91 |
| SKILL-108 | PROCEDURE | `ateam clear-gate <id>` before closing | SKILL.md:94 | SKILL.md:91 |
| SKILL-109 | PROCEDURE | Run local-main update helper, fail-soft | SKILL.md:96-98 | SKILL.md:93-95 |
| SKILL-110 | ABSOLUTE | Absent confirmation: status note `delivered`, leave OPEN | SKILL.md:100 | SKILL.md:97 |
| SKILL-111 | ABSOLUTE | MANDATORY: raise a REVIEW gate | SKILL.md:100-106 | SKILL.md:97,99-102 |
| SKILL-112 | CONTEXT | Review gate is "ready for you" intent bit | SKILL.md:108 | SKILL.md:104 |
| SKILL-113 | POINTER | Full execution-state model: gate-protocol.md | SKILL.md:108 | SKILL.md:104 |
| SKILL-114 | ABSOLUTE | "Opening a PR without setting this gate is incomplete" | SKILL.md:110 | SKILL.md:106 |
| SKILL-115 | ABSOLUTE | Stays open until merged or human explicitly closes | SKILL.md:110 | SKILL.md:106 |
| SKILL-116 | CONTEXT | Close happens later on resume/direction | SKILL.md:110 | SKILL.md:106 |
| SKILL-117 | ABSOLUTE | MANDATORY: record `pr:` field before wind-down | SKILL.md:112 | SKILL.md:108 |
| SKILL-118 | ABSOLUTE | pr-shepherd greps exact line format | SKILL.md:112 | SKILL.md:108 |
| SKILL-119 | PROCEDURE | Combine with delivery note, one `ateam note` call | SKILL.md:114-117 | SKILL.md:108,110-113 |
| SKILL-120 | ABSOLUTE | "Do NOT skip this step..." | SKILL.md:119 | SKILL.md:115 |
| SKILL-121 | PROCEDURE | "...proceed to Phase 6 wind-down." | SKILL.md:121 | DROPPED-RATIONALE — ordering conveyed at SKILL.md:108 ("right after opening the PR, before wind-down") plus heading order |
| SKILL-122 | PROCEDURE / POINTER | "Follow references/wind-down.md exactly" 6-clause summary | SKILL.md:125 | SKILL.md:119 (reworded — now includes the drain+condense clause the inventory's Task 5 flag #4 noted was missing: "drain+condense learnings (`/agent-teams:condense`, lock-guarded, all roles)") |
| SKILL-123 | ABSOLUTE | Terminal DONE, no parked gate → post note, report, END TURN | SKILL.md:127 | SKILL.md:121 |
| SKILL-124 | ABSOLUTE | "Do NOT call `claude stop` to stop yourself." | SKILL.md:127 | SKILL.md:121 |
| SKILL-125 | CONTEXT | Process stays idle; human ends/reaps | SKILL.md:127 | SKILL.md:121 |
| SKILL-126 | ABSOLUTE | Ignore harness's built-in file memory | SKILL.md:131 | SKILL.md:125 |
| SKILL-127 | PROCEDURE | Role/process learnings → `ateam learn` | SKILL.md:133 | SKILL.md:127 |
| SKILL-128 | POINTER | Body shape: references/memory.md | SKILL.md:133 | SKILL.md:127 |
| SKILL-129 | PROCEDURE | User/cross-project preferences → `ateam learn user` | SKILL.md:134 | SKILL.md:128 |
| SKILL-130 | PROCEDURE | Project-specific → `bd remember` | SKILL.md:135 | SKILL.md:129 |
| SKILL-131 | ABSOLUTE | Default `ateam learn`; never MEMORY.md | SKILL.md:137 | SKILL.md:131 (reworded: "never MEMORY.md" dropped here as redundant restatement of SKILL-126, which still states it) |
| SKILL-132 | POLICY | Contribute the moment a learning forms | SKILL.md:137 | SKILL.md:131 |
| SKILL-133 | POINTER | Tier mechanics + condense flow: references/memory.md | SKILL.md:137 | SKILL.md:131 (reworded: "Tier mechanics (fresh/hot/cold): references/memory.md." — "+ condense flow" dropped since memory.md no longer covers condense flow, moved to skills/condense/SKILL.md) |
| SKILL-134 | ABSOLUTE / POLICY | Role-division summary | SKILL.md:141 | SKILL.md:135 |
| SKILL-135 | POINTER | Full per-role detail: execution.md ("Role-division rules") | SKILL.md:141 | SKILL.md:135 (reworded: "Per-role detail: references/execution.md." — quoted section name dropped even though execution.md:42 still carries that heading) |
| SKILL-136 | POLICY | Separable ballooning work → dispatch as sibling | SKILL.md:145 | SKILL.md:139 |
| SKILL-137 | ABSOLUTE | Invoke with problem statement; don't hand-roll `claude --bg` | SKILL.md:145 | SKILL.md:139 |
| SKILL-138 | PROCEDURE | Re-launch parked initiative: `ateam resume <id>` | SKILL.md:145 | SKILL.md:139 |
| SKILL-139 | POINTER | references/registry.md — schema + commands | SKILL.md:149 | SKILL.md:143 (reworded: "initiative schema, standby field, audit enforcement, registry commands") |
| SKILL-140 | POINTER | references/gate-protocol.md — parked-gate sequence | SKILL.md:150 | SKILL.md:144 (reworded: "every gate's exact sequence (never varies) + the review/execution-state model") |
| SKILL-141 | POINTER | references/execution.md — spawn/worktree/merge mechanics | SKILL.md:151 | SKILL.md:145 (reworded: "spawn/worktree/merge/integration mechanics, role-division detail") |
| SKILL-142 | POINTER | references/wind-down.md — checklist incl. close-out | SKILL.md:152 | SKILL.md:146 (reworded: "wind-down checklist (close-out + condense sweep)") |
| SKILL-143 | POINTER | references/advisor.md — consult criteria | SKILL.md:153 | SKILL.md:147 |
| SKILL-144 | POINTER | references/memory.md — three-tier + condense flow | SKILL.md:154 | SKILL.md:148 (reworded: "three-tier memory mechanics" — "+ condense flow" dropped, consistent with SKILL-133/Cluster D) |

**Frontmatter:** `name: dri` survives byte-identical (SKILL.md:2). `description:` (SKILL.md:3) — see verification note below; it did **not** get the 404-char trim the contract's §4 table claims — it is still the original 406-char text, byte-identical to BEFORE.

---

## references/ab-harness-design.md (`AB-`) — file deleted in full

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| AB-1 | CONTEXT | Harness measures concentric vs waterfall concurrently | ab-harness-design.md:1-5 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-2 | CONTEXT | Harness build is bead agent-teams-7r5, deferred | ab-harness-design.md:7-9 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-3 | CONTEXT | Goal: run old/new /dri on same prompt | ab-harness-design.md:13-18 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-4 | ABSOLUTE | Old/new MUST run concurrently | ab-harness-design.md:23-26 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-5 | CONTEXT | Rationale: wall-clock meaningless in shared window | ab-harness-design.md:24-26 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-6 | ABSOLUTE | Single version-pinned install is REJECTED | ab-harness-design.md:28-31 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-7 | POLICY | Mechanism decision deferred until at-vlh returns | ab-harness-design.md:37-38 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-8 | PROCEDURE | Mechanism (a): per-session git worktree plugin load | ab-harness-design.md:41-52 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-9 | CONTEXT | Open capability question re: at-vlh | ab-harness-design.md:54-58 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-10 | POINTER | Version machinery refs | ab-harness-design.md:60-64 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-11 | PROCEDURE | Mechanism (b): two named plugin packages | ab-harness-design.md:66-75 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-12 | CONTEXT | (b) works today; costs two marketplace entries | ab-harness-design.md:76-79 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-13 | POLICY | Recommendation: (a) if at-vlh confirms, else (b) | ab-harness-design.md:81 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-14 | CONTEXT/PROCEDURE | Speed design: parse `timestamp`, elapsed = max-min | ab-harness-design.md:89-98 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-15 | PROCEDURE | Proposed impl: extend `recordJSON`, `SpeedReport` type | ab-harness-design.md:100-103 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-16 | PROCEDURE | Proposed verb `ateam speed` / `--include-speed` | ab-harness-design.md:105-107 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-17 | CONTEXT | Cost axis already delivered by `ateam cost` | ab-harness-design.md:111-118 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-18 | POLICY | Correctness axis deliberately cheap, no eval framework | ab-harness-design.md:122-123 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-19 | PROCEDURE | Step 1: define per-feature rubric | ab-harness-design.md:126 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-20 | CONTEXT | Example rubric questions | ab-harness-design.md:127-128 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-21 | PROCEDURE | Step 2: Eric reviews both runs, scores manually | ab-harness-design.md:129 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-22 | PROCEDURE | Step 3: record scores in markdown table | ab-harness-design.md:130 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-23 | CONTEXT | Objective floor (future): PR gates+bead AC = pass bar | ab-harness-design.md:132-135 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-24 | POLICY | Do not over-build correctness axis | ab-harness-design.md:137-139 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-25 | CONTEXT | Proposed `compare`/`bench` side-by-side table | ab-harness-design.md:143-152 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-26 | PROCEDURE | Implementation is a thin formatter | ab-harness-design.md:154-155 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-27 | PROCEDURE | Proposed command `ateam bench` | ab-harness-design.md:157-159 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-28 | CONTEXT | Harness build is agent-teams-7r5, gated on R2 | ab-harness-design.md:165-167 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-29 | PROCEDURE | Build sequence (1)-(4) | ab-harness-design.md:169-174 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-30 | CONTEXT | "This doc is the design input for that build." | ab-harness-design.md:176 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-31 | POINTER | Key file ref: internal/cost/attribute.go | ab-harness-design.md:184 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-32 | POINTER | Key file ref: internal/cost/pricing.go | ab-harness-design.md:185 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-33 | POINTER | Key file ref: internal/verbs/cost.go | ab-harness-design.md:186 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-34 | POINTER | Key file ref: plugin.json version | ab-harness-design.md:187 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-35 | POINTER | Key file ref: CLAUDE.md two-bump rule | ab-harness-design.md:188 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-36 | POINTER | Key file ref: execution.md worktree setup | ab-harness-design.md:189 | DELETED — per human ruling 1 (whole-file deletion) |
| AB-37 | POINTER | Key file refs: job/transcript discovery sources | ab-harness-design.md:190-191 | DELETED — per human ruling 1 (whole-file deletion) |

**Verification note (per team-lead's instruction to check for surviving content):** grepped the entire `plugins/agent-teams/skills/dri/` tree for `ateam bench`, `ateam speed`, `SpeedReport`, `waterfall`, `agent-teams-waterfall`, `agent-teams-concentric`, `include-speed`, and `ab-harness-design` — **zero hits**. None of AB-1 through AB-37's content survives anywhere in the 7 remaining reference files or SKILL.md; all 37 are clean whole-file deletions with no partial migration. Nothing here looks load-bearing for the DRI's *current* operational behavior — the file was forward-looking design documentation for an unbuilt, deferred harness (bead `agent-teams-7r5`, gated on `at-vlh`), already flagged stale in the inventory's Task 5 (AB-STALE: its `SpeedReport`/`recordJSON` extension plan predates and conflicts with the design of `internal/cost/timestamps.go`, which already ships a differently-named `Timeline` type covering similar ground). Deleting it removed stale/superseded planning prose, not a live behavioral rule.

---

## references/pr-text.md (`PRT-`)

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| PRT-1 | POINTER | Rule lives in SKILL.md; this file holds reasoning + specimen | pr-text.md:3-5 | pr-text.md:3-5 (reworded: "specimens" now plural) |
| PRT-2 | ABSOLUTE | "If this file and SKILL.md ever disagree, SKILL.md wins." | pr-text.md:5 | pr-text.md:4-5 |
| PRT-3 | CONTEXT | Every other artifact is for an in-context reader | pr-text.md:9-14 | pr-text.md:9-12 (reworded/expanded) |
| PRT-4 | CONTEXT | Failure mode is "register bleed," not carelessness | pr-text.md:16-20 | pr-text.md:12-16 (reworded/expanded) |
| PRT-5 | ABSOLUTE | "NEVER in a PR title." | pr-text.md:24 | pr-text.md:20 |
| PRT-6 | ABSOLUTE | "NEVER in a heading, at any level." | pr-text.md:25 | pr-text.md:20 (merged into same bullet as PRT-5) |
| PRT-7 | ABSOLUTE | "NOT woven through prose... not as its subject, not as a possessive, not in passing." | pr-text.md:26-27 | pr-text.md:21-22 |
| PRT-8 | POLICY | "FINE in a trailer, a footnote, a table cell..." | pr-text.md:28-29 | pr-text.md:23-24 |
| PRT-9 | POLICY | "Describe the WORK, not the ticket" | pr-text.md:30-31 | pr-text.md:25-26 |
| PRT-10 | CONTEXT / POINTER | Permitted shape matches memory-body convention at 7 sites | pr-text.md:33-37 | pr-text.md:28-32 |
| PRT-11 | POLICY | "An id is never load-bearing..." | pr-text.md:39-41 | pr-text.md:34-36 |
| PRT-12 | CONTEXT | Worked specimen is PR #139 | pr-text.md:45-50 | pr-text.md:38-43 (reworded) |
| PRT-13 | CONTEXT | Example 1: id as bolded sentence subject | pr-text.md:54-64 | pr-text.md:45-55 |
| PRT-14 | CONTEXT | Example 2: id opening a sentence | pr-text.md:66-78 | DELETED — stated at pr-text.md:21-22 ("not as its subject, not as a possessive, not in passing") — worked example cut for length during condensation; the general rule it illustrated still covers this position, but this specific specimen is gone with no separate restatement |
| PRT-15 | CONTEXT | Example 3: id as mid-sentence possessive | pr-text.md:80-88 | pr-text.md:57-66 (this became the AFTER file's "2." example) |
| PRT-16 | CONTEXT | Example 4: id in passing inside a conclusion | pr-text.md:91-100 | DELETED — stated at pr-text.md:21-22 ("not in passing") — same as PRT-14, worked example cut, general rule still covers it |
| PRT-17 | ABSOLUTE | Example 5: bare suffix shorthand as subject | pr-text.md:102-114 | pr-text.md:68-79 (this became the AFTER file's "3." example) |
| PRT-18 | POLICY | The one COMPLIANT occurrence | pr-text.md:116-127 | pr-text.md:81-88 (reworded/shortened) |
| PRT-19 | ABSOLUTE | Borderline case: closing follow-up list | pr-text.md:129-138 | pr-text.md:90-99 (reworded) |

**Note:** the file went from 5 numbered worked examples to 3 (bolded-subject, mid-sentence-possessive, bare-suffix). PRT-14 and PRT-16's specific specimens (opening-sentence subject; in-passing-inside-a-conclusion) are gone, but the general rule they illustrated (PRT-7, byte-identical survivor) already states "not as subject... not in passing," so no distinct normative content is lost — only two of five illustrations.

---

## references/gate-protocol.md (`GATE-`)

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| GATE-1 | CONTEXT | Definition of a "gate" | gate-protocol.md:3 | gate-protocol.md:3 |
| GATE-2 | PROCEDURE | `ateam gate` accepts `--kind` | gate-protocol.md:7-11 | gate-protocol.md:7-10 |
| GATE-3 | ABSOLUTE | Kind-resolution rule | gate-protocol.md:12-16 | gate-protocol.md:12-16 |
| GATE-4 | CONTEXT | `--kind=review` at Phase 5 is the intent bit | gate-protocol.md:20-21 | gate-protocol.md:20 |
| GATE-5 | CONTEXT | Dashboard joins initiative to live session by cwd | gate-protocol.md:22 | gate-protocol.md:22 |
| GATE-6 | ABSOLUTE | Priority 1: NEEDS-DECISION | gate-protocol.md:24 | gate-protocol.md:24 |
| GATE-7 | ABSOLUTE | Priority 2: IN-PROGRESS overrides review gate | gate-protocol.md:25 | gate-protocol.md:25 |
| GATE-8 | ABSOLUTE | Priority 3: REVIEWABLE | gate-protocol.md:26 | gate-protocol.md:26 |
| GATE-9 | ABSOLUTE | Priority 4: IN-PROGRESS default | gate-protocol.md:27 | gate-protocol.md:27 |
| GATE-10 | POLICY | Conservative rule: running session reads IN-PROGRESS | gate-protocol.md:29 | gate-protocol.md:29 |
| GATE-11 | ABSOLUTE | "The DRI sets NO phase field..." | gate-protocol.md:31 | gate-protocol.md:31 |
| GATE-12 | PROCEDURE | Structured ask form literal command | gate-protocol.md:35-43 | gate-protocol.md:37-39 |
| GATE-13 | ABSOLUTE | `--decision` ≤120 chars, required | gate-protocol.md:47 | gate-protocol.md:42 (reworded/compressed) |
| GATE-14 | PROCEDURE | `--recommendation` one short line | gate-protocol.md:48 | gate-protocol.md:42 |
| GATE-15 | PROCEDURE | `--alternative` one short line | gate-protocol.md:49 | gate-protocol.md:42 |
| GATE-16 | ABSOLUTE | `--context-file` optional, ≤280 chars | gate-protocol.md:50 | gate-protocol.md:42 |
| GATE-17 | POLICY | `--file` prose is fallback only | gate-protocol.md:52 | gate-protocol.md:44 |
| GATE-18 | POLICY | If `--decision` can't fit one line, refine first | gate-protocol.md:54 | gate-protocol.md:44 |
| GATE-19 | CONTEXT | Only plan-approval/design-pivot gates carry plan-doc URL | gate-protocol.md:58 | gate-protocol.md:46 |
| GATE-20 | ABSOLUTE | Planner authors/publishes the plan page | gate-protocol.md:60 | gate-protocol.md:48 (reworded/compressed) |
| GATE-21 | ABSOLUTE | URL goes as FIRST line of `--context-file` | gate-protocol.md:61 | gate-protocol.md:48 |
| GATE-22 | POLICY | Budget: 280 cap, ~68 for URL, ~210 for prose | gate-protocol.md:61 | gate-protocol.md:48 |
| GATE-23 | ABSOLUTE | Ask must stand alone regardless of the link | gate-protocol.md:62 | gate-protocol.md:48 |
| GATE-24 | CONTEXT | Plan-doc URL is enrichment, never a dependency | gate-protocol.md:62 | gate-protocol.md:48 |
| GATE-25 | PROCEDURE | Design-pivot `--file` fallback: URL still near the top | gate-protocol.md:63 | gate-protocol.md:48 |
| GATE-26 | ABSOLUTE | Definition of a design pivot | gate-protocol.md:67 | gate-protocol.md:52 (reworded) |
| GATE-27 | ABSOLUTE | ANY pivot is MANDATORY, raised at moment of divergence | gate-protocol.md:67 | gate-protocol.md:52 |
| GATE-28 | ABSOLUTE | Gate note MUST carry 3 elements | gate-protocol.md:67-71 | gate-protocol.md:52-56 |
| GATE-29 | ABSOLUTE | "The skip is void on pivot." | gate-protocol.md:73 | gate-protocol.md:58 (reworded/expanded) |
| GATE-30 | ABSOLUTE | "'Verify, don't assume' corrects diagnosis, not design." | gate-protocol.md:75 | gate-protocol.md:60 |
| GATE-31 | ABSOLUTE | Neither may self-ratify a pivot | gate-protocol.md:75 | gate-protocol.md:60 |
| GATE-32 | PROCEDURE | Raise with structured form if fits, else `--file` | gate-protocol.md:77 | gate-protocol.md:62 |
| GATE-33 | CONTEXT | Provenance at-9qfb | gate-protocol.md:79 | gate-protocol.md:64 |
| GATE-34 | PROCEDURE | Step 1: record question/note AND flag needs-human | gate-protocol.md:83-84 | gate-protocol.md:68-76 |
| GATE-35 | CONTEXT | Notes message + labels atomically | gate-protocol.md:96 | gate-protocol.md:68 |
| GATE-36 | CONTEXT | `bd human respond`/`dismiss` broken in bd 1.0.4 | gate-protocol.md:97 | gate-protocol.md:76 (reworded — content updated: now confirms broken on "1.0.4 AND 1.1.0" and adds "Re-test before assuming a newer bd has fixed it," resolving the inventory's Task 5 flag #2 about the version pin being possibly stale) |
| GATE-37 | POINTER | `bd human list` still works | gate-protocol.md:97 | gate-protocol.md:76 |
| GATE-38 | PROCEDURE | Step 2: Ask and park | gate-protocol.md:99 | gate-protocol.md:78 |
| GATE-39 | POLICY | Step 3: parking the question never parks the team | gate-protocol.md:100 | gate-protocol.md:79 |
| GATE-40 | PROCEDURE | Step 4: clear the flag via `ateam clear-gate` | gate-protocol.md:101-102 | gate-protocol.md:80 |
| GATE-41 | CONTEXT | `clear-gate` removes `human` + any `gate:*` label | gate-protocol.md:103 | gate-protocol.md:80 |
| GATE-42 | PROCEDURE | Note the resolution, then proceed | gate-protocol.md:103 | gate-protocol.md:80 |
| GATE-43 | CONTEXT | (repeat of GATE-36/37, within-file duplication) | gate-protocol.md:103 | gate-protocol.md:80 (reworded — the within-file duplication flagged in Task 5 #6 is now a short backreference, "see step 1," instead of the full repeated explanation: condensed, not fully deduplicated to a single occurrence) |
| GATE-44 | ABSOLUTE | "Why this must never vary..." | gate-protocol.md:105 | gate-protocol.md:82 |

---

## references/execution.md (`EXEC-`)

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| EXEC-1 | CONTEXT | No team-creation step; team forms on first spawn | execution.md:5 | execution.md:5 |
| EXEC-2 | PROCEDURE | Spawn via Agent tool params | execution.md:5 | execution.md:5 |
| EXEC-3 | ABSOLUTE | Do NOT pass `team_name` | execution.md:5 | execution.md:5 |
| EXEC-4 | ABSOLUTE | Do not pass `model` | execution.md:5 | execution.md:5 |
| EXEC-5 | ABSOLUTE | Bypass required for hands-off operation | execution.md:5 | execution.md:5 |
| EXEC-6 | ABSOLUTE | Bypass removes prompts, not role discipline | execution.md:6 | execution.md:6 |
| EXEC-7 | PROCEDURE | Give every spawn: bead ids, worktree, role-division | execution.md:7 | execution.md:7 |
| EXEC-8 | PROCEDURE / ABSOLUTE | Live-env agent provisions via `ateam worktree-setup` | execution.md:7 | execution.md:7 |
| EXEC-9 | PROCEDURE | Coordinate with peers; escalate to team-lead | execution.md:7 | execution.md:7 |
| EXEC-10 | ABSOLUTE | Tell every spawned agent: NEVER call `EnterWorktree` | execution.md:7 | execution.md:7 |
| EXEC-11 | ABSOLUTE | Helpers spawned without `model`; wrong-model spawn rejected | execution.md:8 | execution.md:8 |
| EXEC-12 | POLICY | Messages cross: idle right after assigning ≠ unprocessed | execution.md:9 | execution.md:9 |
| EXEC-13 | ABSOLUTE | Never call `EnterWorktree` (DRI) | execution.md:13 | execution.md:13 |
| EXEC-14 | CONTEXT | Pin dangles, falls back to $HOME | execution.md:13 | execution.md:13 |
| EXEC-15 | CONTEXT | DRI checkout IS its isolation | execution.md:13 | execution.md:13 |
| EXEC-16 | PROCEDURE | Recovery: `ExitWorktree` with `action: keep` | execution.md:13 | execution.md:13 |
| EXEC-17 | ABSOLUTE | Ignore bootstrap nudge to call `EnterWorktree` | execution.md:14 | execution.md:14 |
| EXEC-18 | CONTEXT | DRI worktree doesn't match nudge's skip-condition | execution.md:14 | execution.md:14 |
| EXEC-19 | ABSOLUTE | Stay cwd-immune | execution.md:15 | execution.md:15 |
| EXEC-20 | CONTEXT | Drifted/dangling pin silently miss-targets | execution.md:15 | execution.md:15 (reworded: "Global policy, but load-bearing for the DRI" framing dropped, consequence clause kept) |
| EXEC-21 | ABSOLUTE | Operate on track worktrees via `-C`/absolute paths | execution.md:16 | execution.md:16 |
| EXEC-22 | CONTEXT | Non-isolated agents inherit lead's cwd | execution.md:17 | execution.md:17 |
| EXEC-23 | CONTEXT | Provenance at-9iq | execution.md:18 | execution.md:18 |
| EXEC-24 | CONTEXT | Provenance at-ps11, session tie | execution.md:19 | execution.md:19 |
| EXEC-25 | ABSOLUTE | "The tie catches the drift... does not make drift safe." | execution.md:19 | execution.md:19 |
| EXEC-26 | ABSOLUTE | Canonical worktree root | execution.md:23 | execution.md:23 |
| EXEC-27 | CONTEXT | Deliberately outside workspace + project repo | execution.md:23 | execution.md:23 |
| EXEC-28 | CONTEXT | Predictable root lets `/setup-agent-teams` pre-approve | execution.md:23 | execution.md:23 |
| EXEC-29 | CONTEXT | `.beads/` discovery unaffected | execution.md:23 | execution.md:23 |
| EXEC-30 | ABSOLUTE | One git worktree, not clone, at FROZEN CONTRACT | execution.md:24 | execution.md:24 |
| EXEC-31 | PROCEDURE | `bd worktree create` preferred | execution.md:25 | execution.md:24 |
| EXEC-32 | PROCEDURE | `git worktree add` also valid | execution.md:26 | execution.md:24 |
| EXEC-33 | ABSOLUTE | Never use independent clones | execution.md:27 | execution.md:24 |
| EXEC-34 | PROCEDURE | Reset --hard if contract advances | execution.md:28 | execution.md:25 |
| EXEC-35 | PROCEDURE | Fresh worktrees need dependency install | execution.md:29 | execution.md:26 |
| EXEC-36 | ABSOLUTE | Env setup is on-demand, not routine | execution.md:30 | execution.md:27 |
| EXEC-37 | POLICY | Run `worktree-setup` only when actually needed | execution.md:30 | execution.md:27 |
| EXEC-38 | ABSOLUTE | Run it AFTER `pnpm install` | execution.md:30 | execution.md:27 |
| EXEC-39 | CONTEXT | Non-fatal either way | execution.md:30 | execution.md:27 |
| EXEC-40 | ABSOLUTE | `ateam worktree-setup` is the ONLY sanctioned entry point | execution.md:31 | execution.md:28 |
| EXEC-41 | CONTEXT | Wrapper resolves the repo's registered hook | execution.md:31 | execution.md:28 |
| EXEC-42 | POLICY | Memory naming raw script path SHADOWS the wrapper | execution.md:31 | execution.md:28 |
| EXEC-43 | POINTER | Registration is the one correct place a raw path appears | execution.md:31 | execution.md:28 |
| EXEC-44 | ABSOLUTE | Record `track-worktree:` line for every implementer worktree | execution.md:32 | execution.md:29 |
| EXEC-45 | CONTEXT | Extends at-ps11 pattern; hung-scan unions the set | execution.md:32 | execution.md:29 |
| EXEC-46 | PROCEDURE | Do this before spawning: append line, update-description | execution.md:33-35 | execution.md:29 |
| EXEC-47 | POLICY | Skip for the DRI's own worktree | execution.md:36 | execution.md:29 |
| EXEC-48 | ABSOLUTE | Legacy fallback is not a substitute | execution.md:36 | execution.md:29 |
| EXEC-49 | PROCEDURE / ABSOLUTE | Merge each track; resolve conflicts yourself | execution.md:40 | execution.md:33 |
| EXEC-50 | ABSOLUTE | Integration verification pass after loop-closing tracks merge | execution.md:41 | execution.md:34 (reworded — now explicitly named as "Step 1 of the two-step gate at SKILL.md's LOOP CLOSED checkpoint," resolving the Task 3 DIVERGENT flag by deferring to one canonical definition) |
| EXEC-51 | PROCEDURE | Step 2 is the live verification procedure | execution.md:41 | DELETED — stated at SKILL.md:83 ("Live verification is mandatory — tests alone never substitute. Spawn an `agent-teams:tester`...") — per Cluster C, the live-verification procedure was folded wholesale into SKILL.md rather than restated here |
| EXEC-52 | ABSOLUTE | "Loop closed = automated gates green AND tester confirms." (Task 3 DIVERGENT flag vs SKILL-89) | execution.md:41 | DELETED — stated at SKILL.md:81 ("LOOP CLOSED = the loop-closing bead set is fully merged into the integration branch AND a verified end-to-end exercise of the new code passes on that branch.") — the two competing formal equations flagged in the inventory's Task 3 are now unified into SKILL.md's single canonical definition; execution.md:34 explicitly defers to it by name instead of restating its own version |
| EXEC-53 | PROCEDURE | Re-run same pass after each subsequent ring | execution.md:41 | execution.md:34 |
| EXEC-54 | POLICY | Remove worktrees/delete branches at wind-down | execution.md:42 | execution.md:35 |
| EXEC-55 | ABSOLUTE | Implementers ephemeral: shutdown once verified merged | execution.md:46 | execution.md:39 |
| EXEC-56 | POLICY | Planner persistent; tester/reviewer kept while cycling | execution.md:47 | execution.md:40 |
| EXEC-57 | ABSOLUTE | Planner plans; never writes feature code | execution.md:51 | execution.md:44 |
| EXEC-58 | ABSOLUTE | Implementers write code + core-path tests; never push/merge | execution.md:52 | execution.md:45 |
| EXEC-59 | POLICY | Tester runs suites, authors edge-case/E2E, owns live verification | execution.md:53 | execution.md:46 (reworded — now also carries the frozen dev-server sentence appended per contract §7: "Only the DRI starts a dev server; testers never start one — they drive and observe an instance the DRI has already brought up.") |
| EXEC-60 | ABSOLUTE | Reviewer never fixes | execution.md:54 | execution.md:47 |
| EXEC-61 | POLICY | All roles file discovery beads; DRI triages | execution.md:55 | execution.md:48 |
| EXEC-62 | ABSOLUTE | Concentric applies to EVERY initiative, no size-gate | execution.md:59 | DELETED — stated at SKILL.md:67 ("Size-adaptive: trivial -> one bead, zero rings; large -> a multi-bead set and gated rings — either way, decompose, close the loop, then open rings.") — per Cluster B, the whole "## Concentric methodology" section was deleted from execution.md as pure duplicate of the SKILL.md keeper |
| EXEC-63 | CONTEXT | Size-adaptive examples (trivial/large) | execution.md:59 | DELETED — stated at SKILL.md:67 (same quote as EXEC-62) |
| EXEC-64 | ABSOLUTE | "Never decide whether to apply concentric — only how large." | execution.md:59 | DELETED — stated at SKILL.md:67 (same quote; the explicit "never decide whether to apply" phrasing itself is softened/implied by "either way" rather than restated verbatim) |
| EXEC-65 | PROCEDURE | Step 1: provision tester's worktree if lacking live env | execution.md:63 | DELETED — stated at SKILL.md:83 ("Spawn an `agent-teams:tester` (provisioning its worktree via `ateam worktree-setup` if needed)...") |
| EXEC-66 | PROCEDURE | Step 2: spawn tester with explicit instructions | execution.md:64 | DELETED — stated at SKILL.md:83 (same line) |
| EXEC-67 | ABSOLUTE | Web/UI: `npx @playwright/cli` REQUIRED | execution.md:65 | DELETED — stated at SKILL.md:83 ("`npx @playwright/cli` for web/UI (REQUIRED)") |
| EXEC-68 | PROCEDURE | API: hit the endpoint | execution.md:66 | DELETED — stated at SKILL.md:83 ("an endpoint hit for API") |
| EXEC-69 | PROCEDURE | CLI: run the command | execution.md:67 | DELETED — stated at SKILL.md:83 ("a command run for CLI") |
| EXEC-70 | PROCEDURE | Step 3: tester reports pass/fail with evidence | execution.md:68 | DELETED — stated at SKILL.md:83 ("The loop is NOT closed until the tester reports pass with evidence") |
| EXEC-71 | ABSOLUTE | Step 4: act on result, never on tests-passing alone | execution.md:69 | DELETED — stated at SKILL.md:83 ("never on tests-passing alone") |

**Note on EXEC-62–71 (the entire old "## Concentric methodology" and "Live verification procedure" sections):** confirmed via direct read of `git show 3c337f5:...execution.md` lines 57-69 that both sections existed exactly where the contract's Cluster B/C describe, and confirmed via the current file that both sections are now absent — their content folded into SKILL.md:67 and SKILL.md:83 respectively, per contract instruction. Nothing here is lost; it moved and was de-duplicated against the (previously divergent) SKILL.md copies.

---

## references/registry.md (`REG-`)

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| REG-1 | CONTEXT | Registry lives in global workspace | registry.md:3 | registry.md:3 |
| REG-2 | ABSOLUTE | Invariant: global workspace ONLY tracking+memories | registry.md:5 | DELETED — stated at SKILL.md:29 ("GLOBAL workspace (`~/.agent-teams`, via `ateam` only) = initiative-tracking beads + role memories ONLY.") — per Cluster J, registry.md keeps only the audit-mechanics restatement (registry.md:5-7), the invariant statement itself is now sole-sourced at SKILL.md |
| REG-3 | ABSOLUTE | Never holds work beads | registry.md:5 | DELETED — stated at SKILL.md:29 ("ALL work beads (planner decomposition, contract, feature/task, `--label=discovery`) live in the PROJECT repo's `.beads` (plain `bd create`).") |
| REG-4 | PROCEDURE | `ateam audit` enforces, exits non-zero | registry.md:5 | registry.md:5-7 (new "## Audit enforcement" heading — Cluster J keeper) |
| REG-5 | PROCEDURE | Schema field `problem:` | registry.md:9 | registry.md:11 |
| REG-6 | PROCEDURE | Schema field `repo:` | registry.md:10 | registry.md:12 |
| REG-7 | PROCEDURE | Schema field `worktree:` | registry.md:11 | registry.md:13 |
| REG-8 | PROCEDURE | Schema field `branch:` | registry.md:12 | registry.md:14 |
| REG-9 | PROCEDURE | Schema field `team:` | registry.md:13 | registry.md:15 |
| REG-10 | PROCEDURE | Schema field `mode:` | registry.md:14 | registry.md:16 |
| REG-11 | PROCEDURE | Schema field (optional) `standby: true` | registry.md:15 | registry.md:17 |
| REG-12 | PROCEDURE | Schema field `epic:` | registry.md:16 | registry.md:18 |
| REG-13 | ABSOLUTE | No `phase:`/`status:` field | registry.md:18 | registry.md:20 |
| REG-14 | PROCEDURE | `track-worktree:` repeatable | registry.md:20-22 | registry.md:22-24 |
| REG-15 | ABSOLUTE | Accumulates; never remove an old one | registry.md:24 | registry.md:26 |
| REG-16 | POINTER | See execution.md's "Worktrees" section for append recipe | registry.md:24 | registry.md:26 |
| REG-17 | CONTEXT | hung-scan unions set; path-substring fallback weaker | registry.md:24 | registry.md:26 |
| REG-18 | ABSOLUTE | `--standby` initiative: DRI parks on startup | registry.md:27 | registry.md:29 |
| REG-19 | CONTEXT | Keeps dispatch mechanical | registry.md:28 | registry.md:29 |
| REG-20 | PROCEDURE | Field written by `ateam dispatch --standby` | registry.md:30-32 | registry.md:31-33 |
| REG-21 | ABSOLUTE | Off state: line absent; never write `standby: false` | registry.md:36 | registry.md:37 |
| REG-22 | ABSOLUTE | Release marker `standby: released` | registry.md:37 | registry.md:38 |
| REG-23 | ABSOLUTE | Reader rule verbatim (matches SKILL-52) | registry.md:38 | registry.md:39 |
| REG-24 | ABSOLUTE | Release marker lands in notes, not description | registry.md:38 | registry.md:39 |
| REG-25 | PROCEDURE | When active, parks with QUESTION gate | registry.md:38 | registry.md:39 |
| REG-26 | PROCEDURE | When human provides direction, DRI releases | registry.md:38 | registry.md:39 |
| REG-27 | CONTEXT | Rationale for explicit release marker | registry.md:39 | registry.md:41 |
| REG-28 | ABSOLUTE | Epic invariant at-e3m | registry.md:41 | registry.md:43 |
| REG-29 | CONTEXT | `ateam register` auto-creates the epic | registry.md:41 | registry.md:43 |
| REG-30 | ABSOLUTE | All work beads MUST use `--parent <epicId>` | registry.md:41 | registry.md:43 |
| REG-31 | POLICY | Multiple ring/phase epics permitted as children | registry.md:41 | registry.md:43 |
| REG-32 | POLICY | Bare unparented beads only for trivial cases | registry.md:41 | registry.md:43 |
| REG-33 | PROCEDURE | `epic:` written to notes when absent (legacy) | registry.md:41 | registry.md:45 |
| REG-34 | CONTEXT | Dashboard reads `epic:` to filter drill-in list | registry.md:41 | registry.md:43 |
| REG-35 | PROCEDURE | Legacy step (1): create root epic | registry.md:43 | registry.md:45 |
| REG-36 | PROCEDURE | Legacy step (2): record via ateam note | registry.md:43 | registry.md:45 |
| REG-37 | PROCEDURE | Legacy step (3): use as EPIC_ID | registry.md:43 | registry.md:45 |
| REG-38 | PROCEDURE | Write body to temp file, `ateam register` | registry.md:47-51 | registry.md:49-51 |
| REG-39 | PROCEDURE | Resume match (open) | registry.md:53 | registry.md:55 |
| REG-40 | PROCEDURE | Resume match (closed) | registry.md:54 | registry.md:56 |
| REG-41 | PROCEDURE | Resolve initiative (ancestor-or-self) | registry.md:55 | registry.md:57 |
| REG-42 | CONTEXT | Ancestor-matching sibling of resume-match | registry.md:55 | registry.md:57 |
| REG-43 | ABSOLUTE | `bd search` does NOT search description bodies | registry.md:57 | registry.md:59 (reworded slightly: "does NOT search description bodies, only titles") |
| REG-44 | PROCEDURE | Phase changes: `ateam note` | registry.md:59 | registry.md:61 |
| REG-45 | PROCEDURE / ABSOLUTE | On delivery: status note, leave OPEN, record `pr:` | registry.md:60 | registry.md:62 |
| REG-46 | POLICY | Opened is not done | registry.md:60 | registry.md:62 |
| REG-47 | ABSOLUTE | Close ONLY when merged | registry.md:61 | registry.md:63 |
| REG-48 | PROCEDURE | Reopen command | registry.md:62 | registry.md:64 |
| REG-49 | ABSOLUTE | Project beads may be human-flagged; global is canonical | registry.md:64 | registry.md:66 |

---

## references/memory.md (`MEM-`)

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| MEM-1 | POINTER | Core routing rule lives in SKILL.md; this file holds tier mechanics | memory.md:3 | memory.md:3 (reworded/expanded — now explicitly notes the condense procedure moved to the `/agent-teams:condense` skill, per Cluster D) |
| MEM-2 | POLICY | Store the learning, not the discovery story | memory.md:7 | memory.md:7 |
| MEM-3 | ABSOLUTE | Body shape: RULE/TRIGGER/APPLY/PROVENANCE | memory.md:7 | memory.md:7 |
| MEM-4 | CONTEXT | Same slug overwrites (upsert-by-key) | memory.md:7 | memory.md:7 |
| MEM-5 | PROCEDURE | Fresh tier, default write | memory.md:13 | memory.md:13 |
| MEM-6 | CONTEXT | Fresh accumulates between condense runs | memory.md:13 | memory.md:13 |
| MEM-7 | CONTEXT | Fresh drained by `ateam fresh-drain` | memory.md:13 | memory.md:13 |
| MEM-8 | PROCEDURE | Hot tier, auto-injected | memory.md:14 | memory.md:14 |
| MEM-9 | POLICY | Hot budget ~6000 tokens | memory.md:14 | memory.md:14 |
| MEM-10 | PROCEDURE | Cold tier, searchable | memory.md:15 | memory.md:15 |
| MEM-11 | CONTEXT | Pre-tier `dri:<slug>` already cold | memory.md:15 | memory.md:15 |
| MEM-12 | CONTEXT | `ateam learnings` serves hot∪fresh | memory.md:17 | memory.md:17 |
| MEM-13 | CONTEXT | Falls back to all `role:` keys when both empty | memory.md:17 | memory.md:17 |
| MEM-14 | CONTEXT | All tiers living, cold not frozen | memory.md:17 | memory.md:17 |
| MEM-15 | PROCEDURE | Key convention table | memory.md:20-23 | memory.md:19 (reworded: table converted to prose) |
| MEM-16 | PROCEDURE | `ateam recall` substring search | memory.md:25 | memory.md:21 |
| MEM-17 | PROCEDURE | `ateam forget` removes tier | memory.md:27 | memory.md:21 |
| MEM-18 | ABSOLUTE | Write-time cap 900/1500 bytes | memory.md:29 | memory.md:23 |
| MEM-19 | CONTEXT | Hot injected whole every session | memory.md:29 | memory.md:23 |
| MEM-20 | ABSOLUTE | Condensing lock-guarded via `ateam condense-lock` | memory.md:33 | DELETED-AS-DUPLICATE-OF skills/condense/SKILL.md:17-21 ("Step 0 — Acquire the condense lock ... `ateam condense-lock acquire`") — per Cluster D, the condense procedure itself moved out of this cluster entirely into the `/agent-teams:condense` skill; memory.md:3 now only points to it |
| MEM-21 | ABSOLUTE | Use `/agent-teams:condense` skill, not `ateam condense` directly | memory.md:33 | DELETED-AS-DUPLICATE-OF skills/condense/SKILL.md:8-11 ("## Parse the argument") |
| MEM-22 | PROCEDURE | Flow: fresh-drain then condense packet | memory.md:35 | DELETED-AS-DUPLICATE-OF skills/condense/SKILL.md:98-127 (all-roles Steps 2-3) |
| MEM-23 | PROCEDURE | Condense agent applies changes via `ateam learn`/`forget` | memory.md:35-39 | DELETED-AS-DUPLICATE-OF skills/condense/SKILL.md:165-188 ("### Apply (batch write, then cleanup)") |
| MEM-24 | ABSOLUTE | "No human-review gate and no staged diff..." | memory.md:41 | DELETED-AS-DUPLICATE-OF skills/condense/SKILL.md:145 ("This procedure is autonomous — NO human-review gate. Safety rests on Dolt history recoverability and the per-role change-summary line you emit.") |
| MEM-25 | CONTEXT | Safety backstop: Dolt history | memory.md:44 | DELETED-AS-DUPLICATE-OF skills/condense/SKILL.md:196-202 ("### Verify" — Dolt-history recoverability framing carried at skills/condense/SKILL.md:145) |
| MEM-26 | PROCEDURE | Change-summary log line | memory.md:45 | DELETED-AS-DUPLICATE-OF skills/condense/SKILL.md:204-210 ("### Emit summary line") |
| MEM-27 | CONTEXT | v1 has no per-run eviction floor | memory.md:47 | DELETED-AS-DUPLICATE-OF skills/condense/SKILL.md:184 ("Conservative: NO eviction floor, but evict little.") |
| MEM-28 | PROCEDURE | Wind-down touchpoint: run `/agent-teams:condense` | memory.md:49 | DELETED — stated at wind-down.md:12 ("Run the `/agent-teams:condense` skill (no arg) — lock-guarded, skips cleanly if another session holds the lock, skips cheaply per-role when nothing is over threshold. See the skill for the full procedure.") |
| MEM-29 | PROCEDURE | Skip role only if both thresholds clear | memory.md:49 | DELETED — stated at wind-down.md:12 (compressed clause "skips cheaply per-role when nothing is over threshold"; full ~8000/~1500 thresholds now live only at skills/condense/SKILL.md:90-96, out of this cluster's scope) |
| MEM-30 | CONTEXT | DRI is a LOCAL agent, can run LLM curation | memory.md:49 | DROPPED — capability assertion, stated nowhere after this change. Operative instruction survives at wind-down.md:12 ("Drain + condense all roles: run the `/agent-teams:condense` skill (no arg)"). What is gone is the sentence explaining WHY the DRI is the actor that can do it — that it is a LOCAL agent with access to the local `~/.agent-teams` Dolt store and so can run the LLM curation step. CONTEXT-class in both source files; the DRI still gets an unambiguous instruction to run the skill. Flagged for the human as the single place in this cluster where a reviewer might want the clause restored. |
| MEM-31 | CONTEXT | Most wind-downs find neither check tripped | memory.md:49 | DELETED — stated at wind-down.md:12 ("skips cheaply per-role when nothing is over threshold" — the "most wind-downs" framing itself is gone, but the underlying cheap-skip behavior is stated) |
| MEM-32 | PROCEDURE | If lock held, log and exit cleanly | memory.md:49 | DELETED — stated at wind-down.md:12 ("skips cleanly if another session holds the lock") |

**Note on MEM-20–27's `DELETED-AS-DUPLICATE-OF skills/condense/SKILL.md` citations:** `skills/condense/` is out of scope for this initiative and unmodified by this PR (confirmed against `git diff --name-only 3c337f5..HEAD` — not in the changed-file list). Nothing was moved there; that content already existed before this change, and what happened here is that memory.md's duplicate restatement of it was deleted. The cited lines/quotes point at pre-existing content a reviewer can verify identically at either revision (3c337f5 or HEAD).

---

## references/advisor.md (`ADV-`)

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| ADV-1 | POLICY | "Read this only when the advisor is enabled..." | advisor.md:1 | advisor.md:3 |
| ADV-2 | CONTEXT | Session runs on sonnet with advisor via `--advisor` | advisor.md:3 | advisor.md:3 |
| ADV-3 | CONTEXT / PROCEDURE | Model from `dri_model` plugin-config, default opus | advisor.md:3 | advisor.md:3 |
| ADV-4 | ABSOLUTE | "The advisor informs; it does not decide..." | advisor.md:3 | advisor.md:3 |
| ADV-5 | POLICY | Consult for: architectural decisions | advisor.md:6 | advisor.md:5 |
| ADV-6 | POLICY | Consult for: cross-system changes | advisor.md:7 | advisor.md:5 |
| ADV-7 | POLICY | Consult for: ambiguous requirements | advisor.md:8 | advisor.md:5 |
| ADV-8 | POLICY | Consult for: unfamiliar domains | advisor.md:9 | advisor.md:5 |
| ADV-9 | POLICY | Consult for: risky refactors | advisor.md:10 | advisor.md:5 |
| ADV-10 | POLICY | Consult for: design tradeoffs, multiple viable approaches | advisor.md:11 | advisor.md:5 (reworded: "two-plus approaches are defensible and the choice matters") |
| ADV-11 | POLICY | Consult for: performance-critical hot-path | advisor.md:12 | advisor.md:5 |
| ADV-12 | POLICY | Consult for: security-sensitive changes | advisor.md:13 | advisor.md:5 |
| ADV-13 | POLICY | Do NOT consult: trivial/mechanical edits | advisor.md:16 | advisor.md:7 |
| ADV-14 | POLICY | Do NOT consult: well-specified single-file changes | advisor.md:17 | advisor.md:7 |
| ADV-15 | POLICY | Do NOT consult: decisions already settled | advisor.md:18 | advisor.md:7 |
| ADV-16 | POLICY | Do NOT consult: resolvable by reading code/investigator | advisor.md:19 | advisor.md:7 |
| ADV-17 | CONTEXT | Advisor exists for genuine judgment forks | advisor.md:21 | advisor.md:9 (reworded) |
| ADV-18 | POLICY | Heuristic: "would a wrong guess be expensive..." | advisor.md:21 | advisor.md:9 |
| ADV-19 | PROCEDURE | Mid-session `/advisor` sends specific question | advisor.md:23 | advisor.md:11 |

---

## references/wind-down.md (`WIND-`)

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| WIND-1 | ABSOLUTE | "In order; do not skip items..." | wind-down.md:3 | wind-down.md:3 |
| WIND-2 | PROCEDURE | 1. Teammates: shutdown_request | wind-down.md:5 | wind-down.md:5 |
| WIND-3 | PROCEDURE | 2. Worktrees: remove/prune/delete branches | wind-down.md:6 | wind-down.md:6 |
| WIND-4 | PROCEDURE | 3. Orphaned processes: check, free ports, kill by PID | wind-down.md:7 | wind-down.md:7 |
| WIND-5 | PROCEDURE | 4. Project beads: close/annotate/file discovery | wind-down.md:8 | wind-down.md:8 |
| WIND-6 | ABSOLUTE | 5. Push the PROJECT repo | wind-down.md:9 | wind-down.md:9 |
| WIND-7 | PROCEDURE / ABSOLUTE | 6. `ateam audit` first, must be clean | wind-down.md:10 | wind-down.md:10 |
| WIND-8 | PROCEDURE | Then `ateam sync` | wind-down.md:10 | wind-down.md:10 |
| WIND-9 | PROCEDURE | 7. Learnings: contribute `dri:<slug>` | wind-down.md:11 | wind-down.md:11 |
| WIND-10 | PROCEDURE | 8. Drain + condense all roles: run `/agent-teams:condense` | wind-down.md:12 | wind-down.md:12 (reworded/compressed per Cluster D — one-line checklist slot, full mechanics moved to skills/condense/SKILL.md) |
| WIND-11 | PROCEDURE | Acquires lock, enumerates roles (skip `user`), measures both | wind-down.md:12 | DELETED-AS-DUPLICATE-OF skills/condense/SKILL.md:70-88 ("### Step 1 — Enumerate roles" + "### Step 2 — Per-role size gate") |
| WIND-12 | PROCEDURE | Skips role only if both thresholds clear | wind-down.md:12 | DELETED-AS-DUPLICATE-OF skills/condense/SKILL.md:90-96 |
| WIND-13 | CONTEXT | DRI is LOCAL agent with access to local Dolt store | wind-down.md:12 | DROPPED — capability assertion, stated nowhere after this change. Operative instruction survives at wind-down.md:12 ("Drain + condense all roles: run the `/agent-teams:condense` skill (no arg)"). What is gone is the sentence explaining WHY the DRI is the actor that can do it — that it is a LOCAL agent with access to the local `~/.agent-teams` Dolt store and so can run the LLM curation step. CONTEXT-class in both source files; the DRI still gets an unambiguous instruction to run the skill. Flagged for the human as the single place in this cluster where a reviewer might want the clause restored. |
| WIND-14 | CONTEXT | Most wind-downs find neither check tripped | wind-down.md:12 | DELETED-AS-DUPLICATE-OF skills/condense/SKILL.md:96 ("Most wind-down runs will find neither check tripped and exit after the release with zero LLM work done.") |
| WIND-15 | PROCEDURE | Concurrent wind-downs serialized by lock | wind-down.md:12 | DELETED-AS-DUPLICATE-OF skills/condense/SKILL.md:23-29 (lock-held skip-log path, also restated inline at wind-down.md:12: "skips cleanly if another session holds the lock") |
| WIND-16 | CONTEXT | Lock window covers `ateam sync` too | wind-down.md:12 | DELETED-AS-DUPLICATE-OF skills/condense/SKILL.md:68,139 ("If you performed an `ateam sync` (Dolt push) at any point, that sync must also occur within the lock window, before release.") |
| WIND-17 | ABSOLUTE | 9. Registry: final status note, awaiting-merge if delivered | wind-down.md:13 | wind-down.md:13 |
| WIND-18 | PROCEDURE / ABSOLUTE | Confirm `pr:` field recorded; do it now if not | wind-down.md:13 | wind-down.md:13 (reworded/compressed) |
| WIND-19 | ABSOLUTE | Close ONLY when merged or human closes | wind-down.md:13 | wind-down.md:13 |
| WIND-20 | PROCEDURE | On merge: clear gate, then close | wind-down.md:13 | wind-down.md:13 |
| WIND-21 | PROCEDURE | Then run local-main update helper, fail-soft | wind-down.md:14-17 | wind-down.md:13 (folded into the same sentence as WIND-17/20 — the close-out sequence line: "On merge, run the Phase 5 close-out sequence (clear-gate → close → update-local-main.sh).") |
| WIND-22 | POLICY | "A long-term pause is annotated, not closed." | wind-down.md:19 | wind-down.md:13 |
| WIND-23 | ABSOLUTE | 10. End the turn, do not self-stop | wind-down.md:20 | wind-down.md:14 |
| WIND-24 | ABSOLUTE | "Do NOT call `claude stop` to stop yourself." | wind-down.md:20 | wind-down.md:14 (reworded/compressed — collapsed into a one-line pointer form: "10. End the turn — do not self-stop (SKILL.md Phase 6, "End-state").") |

**Note on WIND-11/12/14/15/16's `DELETED-AS-DUPLICATE-OF skills/condense/SKILL.md` citations:** same as the note under the memory.md table above — `skills/condense/` is out of scope and unmodified by this PR (`git diff --name-only 3c337f5..HEAD` confirms it's not in the changed-file list). Nothing was relocated there; wind-down.md's duplicate restatement of pre-existing condense-skill content was deleted, and the cited lines/quotes are verifiable at either revision.

---

## UNACCOUNTED

**None remain open — all 3 original candidates were traced to a resolution.** Two turned out not to be losses at all; the third is a genuine drop, and is recorded as a drop rather than left as an unexplained gap. The point of this section is to show the question was asked and answered, not skipped:

1. **SKILL-121** — RESOLVED, not lost. The ordering survives at SKILL.md:108: "**MANDATORY — record the structured `pr:` field** right after opening the PR, **before wind-down**." That, plus the `## Phase 6 — Wind-down` heading at SKILL.md:117 immediately following, conveys the transition the deleted sentence spelled out. Reclassified `DROPPED-RATIONALE`.

2. **MEM-30 / WIND-13** (the same sentence in two files) — **a real drop, confirmed and accepted.** The clause "the DRI is a LOCAL agent with access to the local `~/.agent-teams` Dolt store, so it can run the LLM curation step" is now asserted nowhere: a grep for "LOCAL agent", "local … Dolt store", and "LLM curation" across `plugins/agent-teams/skills/dri/` and `plugins/agent-teams/skills/condense/` returns zero hits at the merged revision.

   What survives is the operative instruction, at wind-down.md:12 — "Drain + condense all roles: run the `/agent-teams:condense` skill (no arg) — lock-guarded, skips cleanly if another session holds the lock, skips cheaply per-role when nothing is over threshold. See the skill for the full procedure." A DRI reaching wind-down step 8 still gets an unambiguous instruction.

   What is gone is the sentence explaining WHY the DRI is the actor that can run it. CONTEXT-class in both source files. Recorded as `DROPPED` rather than resolved away: this is the single row in this cluster where a reviewer may reasonably want the clause restored, and it should stay visible.

---

## ABSOLUTE REWORDINGS

The 8 team-lead-named critical absolutes were checked byte-for-byte and all 8 survive with **zero wording change**:
- CARDINAL RULE (SKILL.md:29) — byte-identical
- "NEVER call `EnterWorktree`" (SKILL.md:36) — byte-identical
- "Never merge autonomously" (SKILL.md:91) — byte-identical
- The `pr:` field format block (SKILL.md:110-113) — byte-identical command
- `--kind=review` command (SKILL.md:99-102) — byte-identical command
- `update-local-main.sh` command (SKILL.md:93-95) — byte-identical command
- `standby: released` reader rule (SKILL.md:55, registry.md:39) — byte-identical in both places
- LOOP CLOSED checkpoint (SKILL.md:81) — byte-identical, and now the SOLE canonical definition (execution.md's competing EXEC-52 equation was deleted in favor of deferring to this one)

Beyond those 8, every other ABSOLUTE-class row was compared for wording change. The great majority survive byte-identical or with only cosmetic punctuation/connective changes (noted inline above as "byte close" in my working notes, shown as plain BEFORE/AFTER citations with no "(reworded: ...)" tag where truly unchanged). Rows where the actual normative wording was compressed or restructured (not just moved) are tagged `(reworded: ...)` inline in the tables above; the ones worth calling out here because the compression touches how the rule reads, not just its length:

- **SKILL-2** (prime directive) — BEFORE: "Prime directive" header + "always be driving toward a PR that solves the problem." AFTER: "**DELIVER: always be driving toward a PR that solves the problem.**" — same rule, now front-loaded with a one-word imperative label.
- **SKILL-67** (concentric methodology) — BEFORE: "Concentric methodology applies to every initiative, size-adaptively — rationale in references/execution.md." AFTER: "Size-adaptive: trivial -> one bead, zero rings; large -> a multi-bead set and gated rings — either way, decompose, close the loop, then open rings." The explicit universality clause ("applies to every initiative") is now only implied by "either way," not stated as its own absolute clause. Low risk — same effective meaning — but it's the one ABSOLUTE-adjacent row where the softening is worth a human's eyes.
- **EXEC-62/63/64** (concentric, no size-gate / never decide whether to apply) — same softening as above, since both point at the same SKILL.md:67 sentence.
- **GATE-36/43** (bd 1.0.4 workaround) — wording changed substantively for the better: now confirms broken on both 1.0.4 and 1.1.0 and adds "re-test before assuming a newer bd has fixed it," resolving the version-staleness flag from the inventory's Task 5.
- **EXEC-50/EXEC-52 / SKILL-89** (loop-closed definition) — resolved from two divergent formal equations to one; execution.md now explicitly defers to SKILL.md's definition by name rather than restating a second one.

No ABSOLUTE row was found reworded in a way that weakens or contradicts its original safety meaning.

---

## Verification summary (see final report to team-lead for the full write-up)

- Sample-verified BEFORE quotes/line numbers against `git show 3c337f5:<path>` for all 8 files — no discrepancies.
- Pointer integrity: all `references/<x>.md` pointers in AFTER SKILL.md resolve to existing files whose content matches what the pointer promises; zero surviving references to `ab-harness-design.md` anywhere outside `docs/condense-contract.md` (confirmed via grep).
- Frontmatter: `description:` in the shipped file is **406 characters, byte-identical to BEFORE** — NOT the 404-char trim that `docs/condense-contract.md` §4 claims shipped. Confirmed via direct byte-for-byte diff of the `description:` line between `git show 3c337f5:...SKILL.md` and the current file: zero diff, both 419 chars including the `description: ` prefix (406 chars of description text). This is a discrepancy between the contract document and what was actually implemented, though the contract's own note ("effectively unchanged from current... not worth cutting further") suggests the 404 figure may have been a drafting artifact rather than a shipped requirement.

---


Merge-base (BEFORE) commit: `3c337f5`. AFTER = current tree at `plugins/agent-teams/skills/steward/`.

### plugins/agent-teams/skills/steward/SKILL.md  (225 -> 218 lines)

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| STW-001 | POINTER | Frontmatter fields present: `name`, `description`. | SKILL.md:1-4 | SKILL.md:1-4 |
| STW-002 | ABSOLUTE | Frontmatter, verbatim: "v1 has ZERO autonomous decision authority..." | SKILL.md:3 | DELETED — removed from the frontmatter `description` field per condensation ruling 2 (611→401 raw chars); the sentence "v1 has ZERO autonomous decision authority: every gate is escalated to Eric with a recommendation and an alternative; only mechanical nudges, anomaly flags, and unambiguous orphan reaping happen without asking." no longer appears at SKILL.md:3. Ruling 2's explicit condition — the constraint stays verbatim in the body — holds: it survives byte-identical at SKILL.md:10-14 (Do NOT list) and SKILL.md:155 (§3 Authority). |
| STW-003 | CONTEXT | Identity: one long-running session, not tied to any initiative, watches every DRI. | SKILL.md:6 | SKILL.md:6 |
| STW-004 | ABSOLUTE | "You are Eric's single conversational counterpart across all initiatives — not a DRI yourself." | SKILL.md:6 | SKILL.md:6 |
| STW-005 | ABSOLUTE | "You never implement, plan, or drive a feature to a PR; you watch, digest, escalate, and record." | SKILL.md:6 | SKILL.md:6 |
| STW-006 | CONTEXT | Banner: "THIS SESSION IS A SINGLE-PURPOSE WATCHER/ESCALATOR." | SKILL.md:8 | SKILL.md:8 |
| STW-007 | ABSOLUTE | Do NOT: "Answer a gate on Eric's behalf, under any circumstance." | SKILL.md:11 | SKILL.md:11 |
| STW-008 | ABSOLUTE | Do NOT: "Merge, push, or close initiatives." | SKILL.md:12 | SKILL.md:12 |
| STW-009 | ABSOLUTE | Do NOT: "Modify code, open PRs, or spawn implementers/planners/testers." | SKILL.md:13 | SKILL.md:13 |
| STW-010 | ABSOLUTE | Do NOT: "Invent capabilities this playbook doesn't describe..." | SKILL.md:14 | SKILL.md:14 |
| STW-011 | PROCEDURE | `ateam` is on PATH — invoke as bare `ateam` everywhere. | SKILL.md:16 | SKILL.md:16 (reworded: "call it as bare `ateam`" replaces "invoke as bare `ateam`") |
| STW-012 | ABSOLUTE | "Exactly ONE steward session may run per machine." | SKILL.md:20 | SKILL.md:20 |
| STW-013 | POINTER | Launch and orphan-watcher mechanics → references/operations.md. | SKILL.md:20 | SKILL.md:20 (reworded: "Launch/orphan-watcher mechanics: references/operations.md.") |
| STW-014 | PROCEDURE | Step 0, before ledger/learnings/execution-status and before ANY inbox drain: confirm not a duplicate. | SKILL.md:21 | SKILL.md:21 (reworded punctuation only) |
| STW-015 | PROCEDURE | Literal duplicate-check `jq` command. | SKILL.md:22-25 | SKILL.md:23-24 (reworded: line-wrapped with `\`, same command) |
| STW-016 | POLICY | Non-empty result = another session already live = you are the duplicate. | SKILL.md:26 | SKILL.md:26 (reworded: "Non-empty = another session owns this dir; you're the duplicate.") |
| STW-017 | ABSOLUTE | Verbatim required output on duplication: "Looks like I'm a duplicate steward session..." | SKILL.md:27-28 | SKILL.md:28 |
| STW-018 | ABSOLUTE | "Then end the turn immediately — run nothing else, not ledger stats, not learnings, not execution-status, not `ateam mail inbox`..." | SKILL.md:30 | SKILL.md:30 (reworded, condensed: "Then end the turn — draining mail as a duplicate consumes the incumbent's unread messages." Drops "immediately" and the four-item enumeration.) |
| STW-019 | PROCEDURE | Load prior context before doing anything else. | SKILL.md:31 | SKILL.md:31 (reworded/merged with STW-020/021/022 into one line) |
| STW-020 | PROCEDURE | `ateam steward ledger stats`. | SKILL.md:32 | SKILL.md:31 (merged) |
| STW-021 | PROCEDURE | `ateam learnings steward`. | SKILL.md:33 | SKILL.md:31 (merged) |
| STW-022 | PROCEDURE | `ateam execution-status`. | SKILL.md:34 | SKILL.md:31 (merged) |
| STW-023 | CONTEXT | Wake triggers: mail at `steward` handle, or periodic heartbeat. | SKILL.md:38 | SKILL.md:35 (reworded: "Wake on mail at the reserved `steward` handle, or the periodic heartbeat.") |
| STW-024 | PROCEDURE | Drain the inbox: `ateam mail inbox`. | SKILL.md:40-42 | SKILL.md:35,38 |
| STW-025 | ABSOLUTE | "Use the canonical `ateam mail send` / `ateam mail inbox` — never the deprecated flat `send`/`inbox` aliases." | SKILL.md:44 | SKILL.md:41 (reworded, minor: "Use canonical...never the deprecated flat...") |
| STW-026 | POLICY | Each unread body is a self-contained, sentinel-delimited envelope — never guess the format. | SKILL.md:44 | SKILL.md:41 |
| STW-027 | PROCEDURE | Classify by envelope type and dispatch. | SKILL.md:44 | SKILL.md:41 (reworded: "Classify by type and dispatch") |
| STW-028 | POINTER | Why each envelope kind exists, failure modes behind it, frozen format contract → references/envelopes.md. | SKILL.md:44 | SKILL.md:41 (reworded, condensed: drops "the failure modes behind it" clause — "why each kind exists and the frozen format contract: references/envelopes.md.") |
| STW-029 | PROCEDURE | steward-gate step 1: enrich the ask with `ateam show <id>` and `ateam execution-status`. | SKILL.md:50 | SKILL.md:47 (reworded: "Enrich with...") |
| STW-030 | POLICY | Enrichment is INBOUND-ONLY: shapes judgment, never forwarded to Eric. | SKILL.md:50 | SKILL.md:47 |
| STW-031 | PROCEDURE | Recall prior similar calls: ledger recall (most recent first) and `ateam recall steward` (distilled learnings). | SKILL.md:50 | SKILL.md:47 (reworded, condensed: drops the "(most recent first)" and "(distilled learnings)" parentheticals) |
| STW-032 | ABSOLUTE | "pull both at decision time, never from the startup load." | SKILL.md:50 | SKILL.md:47 (reworded: "never from startup" replaces "never from the startup load") |
| STW-033 | PROCEDURE | steward-gate step 2: compose per §5's gate-escalation spec and orienting clause. | SKILL.md:51 | SKILL.md:48 (reworded, condensed) |
| STW-034 | POLICY | Compose assuming he remembers nothing and doesn't want it restored — no situation narrative. | SKILL.md:51 | SKILL.md:48 |
| STW-035 | PROCEDURE | steward-gate step 3: temp file, then `ateam notify <id> --file <msg-file>`. | SKILL.md:52-56 | SKILL.md:49 (reworded/condensed to one line) |
| STW-036 | ABSOLUTE | "Nothing goes to the ledger yet — the verdict is pending until Eric replies." | SKILL.md:57 | SKILL.md:50 (reworded: "Nothing to the ledger yet — pending until Eric replies.") |
| STW-037 | PROCEDURE | Keep full working notes on what was recommended and why. | SKILL.md:57 | SKILL.md:50 (reworded: "Keep notes on the recommendation; the reply handler depends on them.") |
| STW-038 | PROCEDURE | steward-reply step 1: interpret the reply against the pending recommendation. | SKILL.md:63 | SKILL.md:56 (reworded, condensed) |
| STW-039 | PROCEDURE | steward-reply step 2: act on the DRI via `ateam mail send`; unblocks the DRI. | SKILL.md:64-67 | SKILL.md:57-60 (reworded: drops "write the answer as a message" framing) |
| STW-040 | ABSOLUTE | "Clearing the gate stays the DRI's own job — you never call `ateam clear-gate`." | SKILL.md:68 | SKILL.md:61 (reworded: "Clearing the gate is the DRI's own job — never call `ateam clear-gate`.") |
| STW-041 | PROCEDURE | Literal `ateam steward ledger record` command. | SKILL.md:69-74 | SKILL.md:63-66 |
| STW-042 | POLICY | `<category>` values, matching the gate's kind. | SKILL.md:75 | SKILL.md:68 |
| STW-043 | POLICY | `accepted` only on exact match; `corrected` if it diverges at all. | SKILL.md:75 | SKILL.md:68 |
| STW-044 | ABSOLUTE | "`--decision` is REQUIRED on `corrected`, optional on `accepted`." | SKILL.md:75 | SKILL.md:68 (reworded: drops "is" — "`--decision` REQUIRED on `corrected`, optional on `accepted`.") |
| STW-045 | PROCEDURE | If `corrected`, distill into a learning immediately. | SKILL.md:76 | SKILL.md:69 |
| STW-046 | POLICY | Shape: RULE/TRIGGER/APPLY — a reusable rule, not a transcript. | SKILL.md:76 | SKILL.md:69 (reworded, condensed) |
| STW-047 | ABSOLUTE | steward-hung-wake: "Do NOT interpret it against a pending recommendation, route anything back into the initiative, or write a ledger verdict." | SKILL.md:80 | SKILL.md:73 |
| STW-048 | PROCEDURE | Instead just proceed to the every-wake scan, which escalates it normally. | SKILL.md:80 | SKILL.md:73 (reworded: "Proceed to the every-wake scan below...") |
| STW-049 | CONTEXT | steward-direct reaches you two ways: 1:1 DM carries `reply-to:<ref>`; @mention in General carries nothing. | SKILL.md:84 | SKILL.md:77 (reworded, condensed: drops "reaches you one of two ways and the header says which" framing) |
| STW-050 | POLICY | No initiative to enrich; optionally pull execution-status, otherwise just answer. | SKILL.md:86 | SKILL.md:79 |
| STW-051 | PROCEDURE | Answer in the conversation used; write reply to temp file. | SKILL.md:87 | SKILL.md:80 (reworded) |
| STW-052 | PROCEDURE | Literal DM-reply command (`--to 8675309:42`). | SKILL.md:90-91 | SKILL.md:83-84 |
| STW-053 | PROCEDURE | Literal @mention-reply command (`--to general`). | SKILL.md:94-95 | SKILL.md:87-88 |
| STW-054 | ABSOLUTE | "Never omit `--to`." Not on either branch, not when General is obviously right. | SKILL.md:98 | SKILL.md:91 (reworded, condensed: "**Never omit `--to`** — it's the only record of which conversation you believed you were answering." Drops "Not on either branch, not when General is obviously right.") — see ABSOLUTE REWORDINGS |
| STW-055 | CONTEXT | Rationale: `--to` is the only surviving record of which conversation you believed you were answering. | SKILL.md:98 | MOVED-TO SKILL.md:91 (folded into STW-054's sentence; "surviving" dropped) |
| STW-056 | ABSOLUTE | "Copy the ref verbatim: everything between `reply-to:` and the closing `>>>`, byte for byte." | SKILL.md:100 | SKILL.md:93 (reworded, meaningfully: drops the explicit delimiter description "everything between `reply-to:` and the closing `>>>`", replaced by "one opaque token" framing merged from STW-057) — see ABSOLUTE REWORDINGS |
| STW-057 | POLICY | It's one opaque token, not a structure — never split, trim, reformat, or retype from memory. | SKILL.md:100 | MOVED-TO SKILL.md:93 (merged into STW-056's sentence) |
| STW-058 | ABSOLUTE | "Never invent one." A header with no `reply-to:` means `--to general` — the right destination, not a blank to fill. | SKILL.md:102 | SKILL.md:95 (reworded, condensed) — see ABSOLUTE REWORDINGS |
| STW-059 | ABSOLUTE | "never carry a ref over from an earlier envelope; each one addresses only its own conversation." | SKILL.md:102 | SKILL.md:95 (reworded/merged into STW-058's sentence; "each one addresses only its own conversation" clause dropped) |
| STW-060 | POLICY | No initiative id attached — interpret against recent briefing context and execution-status. | SKILL.md:108 | SKILL.md:101 (reworded: added "(last posted via `ateam notify briefing`)" parenthetical) |
| STW-061 | ABSOLUTE | "Post ONE briefing-ack (T-ACK) into Briefings... carrying the substance — a routing confirmation, not courtesy. Don't skip it even when the substance also goes elsewhere." | SKILL.md:109 | SKILL.md:102 (reworded, meaningfully condensed: drops "carrying the substance" and "not courtesy" and "Don't skip it") — see ABSOLUTE REWORDINGS |
| STW-062 | PROCEDURE | If the reply names a specific initiative, route the substance there, shrinking the ack to a pointer. | SKILL.md:110 | SKILL.md:103 (reworded/merged with STW-063) |
| STW-063 | PROCEDURE | Use `ateam notify direct --to general` if the reply is an aside. | SKILL.md:110 | SKILL.md:103 (reworded/merged) |
| STW-064 | PROCEDURE | steward-closed-initiative step 1: enrich with `ateam show <id>`. | SKILL.md:116 | SKILL.md:109 (reworded, condensed) |
| STW-065 | POLICY | Not a DRI gate — usually a stray message: answer Eric directly. | SKILL.md:117 | SKILL.md:110 (reworded/merged) |
| STW-066 | POLICY | If it reads as wanting the initiative back, "Want me to reopen it?" is the whole message. | SKILL.md:117 | SKILL.md:110 (reworded/merged) |
| STW-067 | PROCEDURE | Send via `ateam notify <id> --file <msg-file>` — lands back in its own topic. | SKILL.md:117 | SKILL.md:110 (reworded/merged) |
| STW-068 | PROCEDURE | steward-unrouted step 1: read `Reason`; if obvious, act directly. | SKILL.md:123 | SKILL.md:116 (reworded, minor) |
| STW-069 | PROCEDURE | steward-unrouted step 2: otherwise tell Eric directly, including `Reason`/`Body`. | SKILL.md:124 | SKILL.md:117 (reworded) |
| STW-070 | POLICY | steward-unrouted step 3, multi-machine: stay silent or minimal if it's sync lag. | SKILL.md:125 | SKILL.md:118 (reworded, significantly condensed: "Multi-machine: a reply that looks like it belongs to another machine's topic is sync lag, not yours — stay silent or minimal.") |
| STW-071 | PROCEDURE | Every wake, run `execution-status`, `hung-scan`, `claude agents --all --json`. | SKILL.md:129-132 | SKILL.md:123-125 |
| STW-072 | CONTEXT | `ateam hung-scan` classifies WORKING/AWAITING-HUMAN/DEAD/STUCK — ground truth. Full field list pointer. | SKILL.md:135 | SKILL.md:128 (reworded, minor: "Full field list: references/operations.md." replaces "Full field list and how each is computed: ...") |
| STW-073 | POLICY | STUCK/`hung:true` → escalate a DIGESTED message to the initiative's own topic. | SKILL.md:137 | SKILL.md:130 (reworded/condensed into one bullet) |
| STW-074 | POLICY | That escalation is a judgment call, never autonomous. | SKILL.md:137 | SKILL.md:130 (merged into bullet) |
| STW-075 | PROCEDURE | Reply comes back as an ordinary steward-reply; record under `unblock-action`. | SKILL.md:137 | SKILL.md:130 (merged into bullet) |
| STW-076 | ABSOLUTE | DEAD/`cwd_present:false`: "the one autonomous cleanup allowed is `ateam reap-orphans`." | SKILL.md:138 | SKILL.md:131 (reworded: "**DEAD, `cwd_present:false`** — orphan. Only autonomous cleanup: `ateam reap-orphans`.") |
| STW-077 | POLICY | DEAD/`cwd_present:true`/`dead_hung:true` → escalate like STUCK; no autonomous revive. | SKILL.md:139 | SKILL.md:132 (reworded/condensed) |
| STW-078 | CONTEXT | Cross-reference `claude agents --all --json` by worktree; also wakes mechanically at 15 min. | SKILL.md:139 | MOVED-TO operations.md:40 (the worktree cross-reference instruction relocated there, with elaboration: "For these, `claude respawn <shortid>`'s argument isn't in this JSON — cross-reference `claude agents --all --json` by worktree to find it."); a short trace pointer remains at SKILL.md:132 ("finding the shortid: references/operations.md"); the "wakes...at 15 min" half survives at SKILL.md:132 too. |
| STW-079 | POLICY | WORKING/`wp_trip_eligible:true`: busy + recent command + no failure tokens → "healthy, watching". | SKILL.md:140 | SKILL.md:133 (reworded/condensed) |
| STW-080 | POLICY | Otherwise → nudge the DRI like STUCK; record under `unblock-action`. | SKILL.md:140 | SKILL.md:133 (merged) |
| STW-081 | CONTEXT | At 1h flat an automatic direct alert fires — not yours to trigger. | SKILL.md:140 | SKILL.md:133 (merged) |
| STW-082 | POLICY | STUCK under threshold, AWAITING-HUMAN, or WORKING without `wp_trip_eligible` → no action. | SKILL.md:141 | SKILL.md:134 |
| STW-083 | POLICY | `mode:interactive` excluded from every mechanical wake path. | SKILL.md:142 | SKILL.md:135 (reworded, minor) |
| STW-084 | POLICY | Flag other anomalies — zombie sessions, missing watcher — a note to Eric, not autonomous action. | SKILL.md:144 | SKILL.md:137 (reworded, condensed) |
| STW-085 | ABSOLUTE | Attribution: "never answer from your own session state or memory." | SKILL.md:148 | SKILL.md:141 |
| STW-086 | CONTEXT | Rationale: context compacts, and any record of what was sent compacts with it — that is exactly how this failed once already. | SKILL.md:148 | SKILL.md:141 (reworded, partial loss: keeps "context compacts, and any record of what you sent compacts with it" but drops the trailing "that is exactly how this failed once already" historical justification) |
| STW-087 | PROCEDURE | Literal `ateam sent --since <window> --json ...` command. | SKILL.md:150-151 | SKILL.md:144 |
| STW-088 | ABSOLUTE | "answer from the records, not recollection." | SKILL.md:154 | SKILL.md:147 (reworded: "and answer from the records." drops "not recollection") — see ABSOLUTE REWORDINGS |
| STW-089 | CONTEXT | `sender` is one of six constants, names the verb not a session. | SKILL.md:156 | SKILL.md:149 (reworded/condensed into a bullet) |
| STW-090 | POLICY | `relay-hung`'s `session_id` may look like yours but isn't proof; other fields corroborate. | SKILL.md:156 | SKILL.md:149 (merged into bullet) |
| STW-091 | POLICY | `UNDECLARED` means a call site didn't identify itself — say so, don't guess. | SKILL.md:157 | SKILL.md:150 (reworded, condensed) |
| STW-092 | POLICY | No matching record ≠ "I didn't send it" — absence never proves non-authorship. | SKILL.md:158 | SKILL.md:151 (reworded: drops "on its own") |
| STW-093 | ABSOLUTE | "The Do NOT list at the top of this file is absolute: a recommendation is a suggestion, never a decision." | SKILL.md:162 | SKILL.md:155 |
| STW-094 | ABSOLUTE | "The only autonomous actions are status nudges, anomaly flags, and unambiguous `ateam reap-orphans`." | SKILL.md:162 | SKILL.md:155 |
| STW-095 | ABSOLUTE | "Everything else escalates to Eric with a recommendation and an alternative, and waits." | SKILL.md:162 | SKILL.md:155 |
| STW-096 | PROCEDURE | One ledger record per escalated decision, written at verdict time, never at recommendation time. | SKILL.md:166 | SKILL.md:159 |
| STW-097 | POINTER | Categories, verdict rules, and the command → §2's steward-reply handler. | SKILL.md:166 | SKILL.md:159 |
| STW-098 | POLICY | Nothing else reaches the ledger: direct chat, briefing reply, closed-initiative, unrouted are not gated decisions. | SKILL.md:166 | SKILL.md:159 (reworded, minor conjunction change) |
| STW-099 | POLICY | "Green gates are silent. Only failures get words." | SKILL.md:170 | SKILL.md:163 (reworded/merged with STW-100/101 into one sentence) |
| STW-100 | ABSOLUTE | "Never report that unit or gate tests passed." | SKILL.md:170 | SKILL.md:163 (reworded/merged: "Green gates are silent — never report a passing test.") — see ABSOLUTE REWORDINGS |
| STW-101 | POLICY | Exception: if LIVE verification was actually run, say so in one line. | SKILL.md:170 | SKILL.md:163 (reworded, condensed) |
| STW-102 | PROCEDURE | If four hours pass with no message, post one briefing line confirming green. | SKILL.md:172 | SKILL.md:165 (reworded, condensed) |
| STW-103 | POINTER | Why silence and this heartbeat are one rule → references/message-style.md. | SKILL.md:172 | SKILL.md:165 (reworded, minor) |
| STW-104 | POLICY | gate-escalation shape (verbatim spec): "One line of what it buys. One line of what it costs. Your recommendation. ~88 words." | SKILL.md:174 | SKILL.md:167 |
| STW-105 | POLICY | Orienting clause required for gate-escalation, hung-escalation, reply-ack, anomaly-flag: ≤12 words or folded into first line. | SKILL.md:176 | SKILL.md:169 (reworded, condensed) |
| STW-106 | POLICY | REQUIRED = the thing named plainly; BANNED = title/bead id/topic-name copy. | SKILL.md:176 | SKILL.md:169 (merged into same paragraph) |
| STW-107 | POLICY | Terse: no process narration, no restating what he already knows, no back-references. | SKILL.md:178 | SKILL.md:171 (reworded: "what he knows" replaces "what he already knows") |
| STW-108 | POLICY | Terseness governs the outbound message only; internal record-keeping stays full. | SKILL.md:178 | SKILL.md:171 |
| STW-109 | ABSOLUTE | Plan-URL carve-out: "reproduce it VERBATIM on its own line: never summarized away, never wrapped in markdown, never truncated, and it does not count against the word budget." | SKILL.md:180 | SKILL.md:173 (reworded, condensed) — see ABSOLUTE REWORDINGS |
| STW-110 | POINTER | Why bare, not markdown → references/message-style.md. | SKILL.md:180 | SKILL.md:173 (reworded, condensed) |
| STW-111 | POLICY | The link is an ADDITION, never a replacement — the message must still let him decide without opening it. | SKILL.md:180 | SKILL.md:173 (reworded; trailing clause RESTORED verbatim after review — no loss) |
| STW-112 | POLICY | Disclosure: a mistake that changed the work gets one plain line to Eric — no apology, no retrospective. | SKILL.md:182 | SKILL.md:175 (reworded, condensed: drops "to Eric" and "on how it happened") |
| STW-113 | POLICY | The learning capture (§6) is not a substitute for telling him, and vice versa. | SKILL.md:182 | SKILL.md:175 (reworded, condensed) |
| STW-114 | PROCEDURE | Message-kind table header. | SKILL.md:184-193 | SKILL.md:177-178 |
| STW-115 | PROCEDURE | Table row gate-escalation. | SKILL.md:186 | SKILL.md:179 |
| STW-116 | PROCEDURE | Table row hung-escalation. | SKILL.md:187 | SKILL.md:180 |
| STW-117 | PROCEDURE | Table row reply-ack. | SKILL.md:188 | SKILL.md:181 |
| STW-118 | PROCEDURE | Table row direct-answer. | SKILL.md:189 | SKILL.md:182 |
| STW-119 | PROCEDURE | Table row briefing-post. | SKILL.md:190 | SKILL.md:183 |
| STW-120 | PROCEDURE | Table row briefing-ack. | SKILL.md:191 | SKILL.md:184 |
| STW-121 | PROCEDURE | Table row anomaly-flag. | SKILL.md:192 | SKILL.md:185 |
| STW-122 | PROCEDURE | Table row status-change. | SKILL.md:193 | SKILL.md:186 |
| STW-123 | CONTEXT | A ninth kind, `topic-open`, is machine-authored, not Steward prose → references/message-style.md. | SKILL.md:195 | SKILL.md:188 |
| STW-124 | POINTER | Worked before/after specimens per kind → references/message-style.md. | SKILL.md:195 | SKILL.md:188 |
| STW-125 | POLICY | Contribute role learnings as they form — the Steward never winds down. | SKILL.md:199 | SKILL.md:192 |
| STW-126 | PROCEDURE | Literal `ateam learn steward <slug> --file <tmpfile>`. | SKILL.md:201-202 | SKILL.md:195 |
| STW-127 | POLICY | Shape: RULE/TRIGGER/APPLY, PROVENANCE as bare initiative-id parenthetical. | SKILL.md:205 | SKILL.md:198 (reworded, minor) |
| STW-128 | POLICY | "The highest-value moment to capture a learning is when Eric CORRECTS a recommendation." | SKILL.md:207 | SKILL.md:200 (reworded, condensed from full sentence to bolded fragment) |
| STW-129 | CONTEXT | `ateam learnings steward` auto-injects hot+fresh only; `recall steward <query>` searches the FULL set, cold included. | SKILL.md:209 | SKILL.md:202 (reworded, condensed) |
| STW-130 | POINTER | Tier mechanics → references/operations.md. | SKILL.md:209 | DELETED — stated at SKILL.md (BEFORE) L209 ("Tier mechanics: references/operations.md."); no equivalent pointer sentence remains at AFTER SKILL.md:202. The dedicated "Learnings tiers" section in operations.md this pointer named is also gone (see OPS-021/022/023) — the underlying fact itself is still stated inline at SKILL.md:202, but the pointer instruction is gone. |
| STW-131 | POLICY | Cross-initiative material goes to the dedicated briefing topic, never one initiative's. | SKILL.md:213 | SKILL.md:206 (reworded, minor: drops "dedicated") |
| STW-132 | PROCEDURE | Literal `ateam notify briefing --file <msg-file>`. | SKILL.md:215-217 | SKILL.md:209 |
| STW-133 | POLICY | Reach for `briefing` only when the message genuinely doesn't belong to one initiative. | SKILL.md:219 | SKILL.md:212 (reworded: drops "genuinely") |
| STW-134 | CONTEXT | Other two targets: `ateam notify <id>` and `ateam notify direct` (always with `--to`). | SKILL.md:219 | SKILL.md:212 (reworded, condensed; adds cross-ref "see steward-direct §2") |
| STW-135 | ABSOLUTE | "No confidence graduation: the ledger grants no autonomous authority — escalate every gate to Eric regardless of ledger stats." | SKILL.md:223 | SKILL.md:216 |
| STW-136 | POINTER | Full mechanics → references/operations.md. | SKILL.md:225 | SKILL.md:218 (reworded, resolves the Task-5 VAGUE rating: "Launch/singleton mechanics, hung-scan's full field list, and ledger CLI: references/operations.md.") |
| STW-137 | POINTER | Why envelope kinds exist → references/envelopes.md. | SKILL.md:225 | SKILL.md:218 |
| STW-138 | POINTER | Worked specimens → references/message-style.md. | SKILL.md:225 | SKILL.md:218 |

### plugins/agent-teams/skills/steward/references/message-style.md  (168 -> 126 lines)

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| MSG-001 | CONTEXT | Title: "Steward message style — worked specimens." | message-style.md:1 | message-style.md:1 |
| MSG-002 | ABSOLUTE | "Every rule...lives in `SKILL.md`. If this file and `SKILL.md` ever disagree, `SKILL.md` wins." | message-style.md:3-5 | message-style.md:3-5 |
| MSG-003 | CONTEXT | One section per outbound kind; two calibrated, seven empty on purpose. | message-style.md:7-9 | message-style.md:7-9 |
| MSG-004 | ABSOLUTE | "Do not fill them in from taste, inference, or a draft that has not come back from him..." | message-style.md:9-11 | message-style.md:9-11 |
| MSG-005 | CONTEXT | Rationale: silence + four-hour heartbeat are one rule. | message-style.md:15 | message-style.md:15 |
| MSG-006 | CONTEXT | Rationale: LIVE verification is evidence Eric can't get another way. | message-style.md:17 | message-style.md:17 |
| MSG-007 | CONTEXT | Rationale: Telegram `sendMessage` has no `parse_mode`, so a plan URL goes in bare. | message-style.md:19 | message-style.md:19 |
| MSG-008 | CONTEXT | Restates the gate-escalation rule being illustrated (verbatim quote). | message-style.md:25-27 | message-style.md:25-27 |
| MSG-009 | CONTEXT | BEFORE specimen: 310-word message actually sent (opening quoted), analyzed. | message-style.md:29-39 | message-style.md:29-39 |
| MSG-010 | CONTEXT | That BEFORE message also had the initiative title mechanically prepended by `telegram.go:167`, deleted in this same initiative. | message-style.md:41-44 | DELETED — described removed behavior; see `internal/transport/telegram/telegram.go:233-236`, whose current comment reads: "On reuse of an existing thread, msg.Title is deliberately NOT prepended to the body: the forum topic (opened above, or on a prior call for this thread) is already named after that same title, so restating it under a heading that says the same thing is noise, not an aid to scanning." Per instruction, explicitly NOT ⚠️ UNACCOUNTED — this documents the removed behavior the deleted paragraph described. (Note: `telegram.go:167`, the line the deleted paragraph cited, is unrelated DM-allowlist code today — the paragraph's own text already anticipated this, calling the behavior "deleted in this same initiative.") |
| MSG-011 | CONTEXT | AFTER specimen B (the one Eric picked), quoted in full. | message-style.md:46-56 | message-style.md:41-51 |
| MSG-012 | CONTEXT | "What changed": decision moved to first line; status/provenance/green-test cut. | message-style.md:58-61 | message-style.md:53-56 |
| MSG-013 | CONTEXT | Where the orientation comes from; don't stack a further orienting sentence; rejected specimen C shared the same opening two lines. | message-style.md:63-69 | message-style.md:58-64 |
| MSG-014 | CONTEXT | status-change discovered in round 2 — the one kind the Steward sends on its own. | message-style.md:73-77 | message-style.md:68-72 |
| MSG-015 | ABSOLUTE | "Budget: 35 words — not T-ACK's 25... Do not correct it down to 25 later..." | message-style.md:79-81 | message-style.md:74-76 |
| MSG-016 | CONTEXT | AFTER specimen 1 (34 words, landed as-is), quoted in full. | message-style.md:83-87 | message-style.md:78-82 |
| MSG-017 | POLICY | Folding an anomaly detail into status-change instead of a separate anomaly-flag is deliberate. | message-style.md:89-91 | message-style.md:84-86 |
| MSG-018 | CONTEXT | AFTER specimen 2 (32 words, RE-CUT), quoted in full. | message-style.md:93-97 | message-style.md:88-92 |
| MSG-019 | CONTEXT | Worked example of the no-back-references rule: rejected 25-word "your N asks" cut, quoted verbatim. | message-style.md:99-106 | message-style.md:94-101 |
| MSG-020 | CONTEXT | AFTER specimen 3 (30 words, landed as-is), quoted in full. | message-style.md:108-112 | message-style.md:103-107 |
| MSG-021 | PROCEDURE | Round 2 takes real sent messages back to Eric one kind at a time; answers land in the empty slots. | message-style.md:116-117 | message-style.md:111-114 (reworded, restructured: now also names the six empty kinds and points to SKILL.md §5's table, replacing what were six standalone subsections) |
| MSG-022 | CONTEXT | hung-escalation description: Eric must choose unblock/restart/kill. No calibrated specimen yet. | message-style.md:119-124 | DELETED — the standalone subsection ("### hung-escalation" + description + "No calibrated specimen yet") no longer exists in message-style.md; the kind is now only named in the one-line list at message-style.md:112-113 with a pointer to "SKILL.md §5's table for what triggers each and what Eric must do." The Trigger/Eric-must/Budget/Banned facts for this kind are independently and pre-existingly stated in SKILL.md's message-kind table, row hung-escalation (SKILL.md:180 / STW-116), unchanged by this condensation pass. |
| MSG-023 | CONTEXT | reply-ack description: confirms the answer was received and routed. No calibrated specimen yet. | message-style.md:126-131 | DELETED — same pattern as MSG-022; kind named at message-style.md:112; SKILL.md's table row reply-ack (SKILL.md:181 / STW-117) independently covers Trigger/Eric-must/Budget/Banned. |
| MSG-024 | CONTEXT | direct-answer description: answers a question Eric asked directly. No calibrated specimen yet. | message-style.md:133-138 | DELETED — same pattern; kind named at message-style.md:112; SKILL.md table row direct-answer (SKILL.md:182 / STW-118). |
| MSG-025 | CONTEXT | briefing-post description: cross-initiative sweep posted to the briefing topic. No calibrated specimen yet. | message-style.md:140-145 | DELETED — same pattern; kind named at message-style.md:113; SKILL.md table row briefing-post (SKILL.md:183 / STW-119). |
| MSG-026 | CONTEXT | briefing-ack description: a short reply on the briefing thread. No calibrated specimen yet. | message-style.md:147-151 | DELETED — same pattern; kind named at message-style.md:113; SKILL.md table row briefing-ack (SKILL.md:184 / STW-120). |
| MSG-027 | CONTEXT | anomaly-flag description: something looks wrong, nothing asked of him, no decision gated. No calibrated specimen yet. | message-style.md:153-158 | DELETED — same pattern; kind named at message-style.md:113; SKILL.md table row anomaly-flag (SKILL.md:185 / STW-121). |
| MSG-028 | POLICY | topic-open: machine-authored at `dispatch.go:331`, not Steward-authored; the one kind absent from SKILL.md §5's table. | message-style.md:160-168 | message-style.md:118-119 (reworded: citation fixed `dispatch.go:331` → `dispatch.go:376 (createInitialTopic)`, correcting the Task-6 stale-citation flag) |

### plugins/agent-teams/skills/steward/references/operations.md  (64 -> 56 lines)

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| OPS-001 | CONTEXT | Scope: how the Steward is launched, kept singleton, and removed. | operations.md:3 | operations.md:3 |
| OPS-002 | CONTEXT | `ateam` ships as a prebuilt per-platform binary, auto-added to PATH; `/setup-agent-teams` installs it — why SKILL.md calls it bare. | operations.md:7 | DELETED — the entire "## The `ateam` binary" section (operations.md BEFORE L5-7: "`ateam` ships as a prebuilt per-platform binary in the plugin's `bin/`, which is auto-added to PATH; `/setup-agent-teams` installs and verifies it. That is why SKILL.md calls it as a bare `ateam` everywhere and never as a path.") is gone. The base fact (prebuilt per-platform binaries, auto-added to PATH) survives at `plugins/agent-teams/CLAUDE.md:5`. The specific "why SKILL.md calls it bare" rationale clause has no surviving citation anywhere in the steward cluster — partial loss, flagged. |
| OPS-003 | ABSOLUTE | "Exactly ONE steward session may run per machine." (restates SKILL.md:20) | operations.md:11 | operations.md:7 |
| OPS-004 | PROCEDURE | The sanctioned launch: `ateam steward start`. | operations.md:14 | operations.md:10 |
| OPS-005 | PROCEDURE | `steward start` = init, singleton pre-flight, orphan-watcher hygiene, then launch. | operations.md:17 | operations.md:13 |
| OPS-006 | PROCEDURE | Literal underlying launch command. | operations.md:19-21 | operations.md:15-17 |
| OPS-007 | ABSOLUTE | "`--permission-mode bypassPermissions` is required — a background steward launched without it hangs invisibly..." | operations.md:23 | operations.md:19 |
| OPS-008 | CONTEXT | Running `ateam steward init` BEFORE the session starts ensures the marker exists before any SessionStart hook fires. | operations.md:23 | operations.md:19 |
| OPS-009 | POLICY | `ateam steward init` is idempotent — pure backstop. | operations.md:25 | operations.md:21 |
| OPS-010 | POLICY | The duplicate check comes before the inbox drain; the guard is a backstop, not the mechanism. | operations.md:29 | operations.md:25 |
| OPS-011 | CONTEXT | Why all three context loads happen before anything else. | operations.md:31 | operations.md:27 |
| OPS-012 | CONTEXT | Why the startup load isn't enough alone: compaction, so recall must be re-run at decision time. | operations.md:33 | operations.md:29 (reworded: inserts "— the same one the SubagentStart hook injects —") |
| OPS-013 | CONTEXT | Wake plumbing: doorbell + wake-watcher machinery, or periodic heartbeat. | operations.md:35 | operations.md:31 |
| OPS-014 | CONTEXT | `hung-scan` field list intro. | operations.md:39 | operations.md:35 |
| OPS-015 | CONTEXT | Field `hung` — live session idle past threshold. | operations.md:41 | operations.md:37 |
| OPS-016 | CONTEXT | Fields `cwd_present`/`pid_present`. | operations.md:42 | operations.md:38 |
| OPS-017 | POLICY | Field `mode` — `bg` or `interactive`; `interactive` excluded from every mechanical wake path. | operations.md:43 | operations.md:39 |
| OPS-018 | CONTEXT | Field `dead_hung` — DEAD-with-worktree past 15 min. | operations.md:44 | operations.md:40 (reworded: appends the relocated STW-078 cross-reference sentence — "For these, `claude respawn <shortid>`'s argument isn't in this JSON — cross-reference `claude agents --all --json` by worktree to find it.") |
| OPS-019 | CONTEXT | Work-product fields list. | operations.md:45 | operations.md:41 |
| OPS-020 | POLICY | `wp_trip_eligible:true` means all three conditions; the mechanical wake carries the evidence. | operations.md:47 | operations.md:43 |
| OPS-021 | CONTEXT | `ateam learnings steward` auto-injects only hot+fresh tiers. | operations.md:51 | MOVED-TO SKILL.md:202 (the "## Learnings tiers" section header and this sentence are gone from operations.md; the fact is a pre-existing, independent restatement at SKILL.md:202 / STW-129, itself reworded but present both before and after this pass) |
| OPS-022 | CONTEXT | `ateam recall steward <query>` is a substring search over key+body across the FULL set, cold/archived included, printing matches directly. | operations.md:51 | DELETED — the "## Learnings tiers" section (operations.md BEFORE L49-51) is gone entirely. SKILL.md:202 preserves only the higher-level claim ("searches the full set, cold included"); the specific mechanism detail — substring search over key+body, "archived" entries, printing matches directly — has no surviving citation anywhere in the cluster. Flagged as a genuine (if minor) content loss. |
| OPS-023 | POLICY | Use `recall` at decision time; `learnings` is the ambient tier. | operations.md:51 | DELETED — same deleted section. A related but not identical statement survives at SKILL.md:47 ("pull both at decision time, never from startup"), which is about the steward-gate flow specifically, not this general decision-time-vs-ambient-tier framing rule. |
| OPS-024 | ABSOLUTE | "`ateam steward ledger record` REJECTS a `corrected` verdict submitted without `--decision`." | operations.md:55 | operations.md:47 |
| OPS-025 | CONTEXT | The rejection is deliberate: a `corrected` row with no decision record teaches nothing. | operations.md:55 | operations.md:47 |
| OPS-026 | CONTEXT | Gate→Steward routing guarded on `StewardSessionMarkerPath` existing. | operations.md:59 | operations.md:51 |
| OPS-027 | CONTEXT | This keeps a steward-less machine from accumulating unread steward-message beads forever. | operations.md:59 | operations.md:51 |
| OPS-028 | PROCEDURE | Manual disable: delete `<workspace>/steward/session`. | operations.md:63 | operations.md:55 |
| OPS-029 | PROCEDURE | `ateam steward remove`: supported way to de-steward a machine. | operations.md:64 | operations.md:56 |
| OPS-030 | POLICY | Keeps ledger.jsonl and briefing-thread by default; prints their paths. | operations.md:64 | operations.md:56 |
| OPS-031 | PROCEDURE | Pass `--purge` to delete those too. | operations.md:64 | operations.md:56 |
| OPS-032 | PROCEDURE | Reports (never modifies) unread messages still assigned to `steward`. | operations.md:64 | operations.md:56 |

### plugins/agent-teams/skills/steward/references/envelopes.md  (41 -> 41 lines)

| ID | Class | Rule | BEFORE | AFTER |
|---|---|---|---|---|
| ENV-001 | POINTER | "Rationale only — never the dispatch rules themselves. SKILL.md §2 is self-sufficient without this file." | envelopes.md:3 | envelopes.md:3 |
| ENV-002 | ABSOLUTE | "`internal/verbs/steward_seams.go` is the frozen contract for the envelope format itself..." | envelopes.md:5 | envelopes.md:5 |
| ENV-003 | CONTEXT | Reopening a Telegram topic doesn't reopen the initiative; the closed-initiative safety net routes here instead. | envelopes.md:9 | envelopes.md:9 |
| ENV-004 | CONTEXT | Without this envelope kind, a reply to an old closed initiative's topic would get no response at all. | envelopes.md:9 | envelopes.md:9 (reworded, condensed: "a reply...gets no response at all" replaces "a human replying...would get no response at all"; "hand it to" replaces "hand the message to"; "a Steward judgment call" replaces "routing it to the Steward for a judgment call") |
| ENV-005 | CONTEXT | steward-unrouted fires on three failure modes. | envelopes.md:13 | envelopes.md:13 (reworded: drops "distinct") |
| ENV-006 | CONTEXT | No concrete identified target to act on — no initiative id, no clean reply surface. | envelopes.md:13 | envelopes.md:13 (reworded, condensed: drops "identified", "you can", "Telegram") |
| ENV-007 | CONTEXT | Multi-machine sync-lag caveat: each machine syncs on its own schedule. | envelopes.md:15 | envelopes.md:15 (reworded, condensed) |
| ENV-008 | CONTEXT | Why stay silent/minimal: reacting on stale state produces confusing double-replies. | envelopes.md:15 | envelopes.md:15 (reworded: drops "to") |
| ENV-009 | CONTEXT | The parked DRI is waiting on the answer message itself — that's what it resumes on. | envelopes.md:19 | envelopes.md:19 |
| ENV-010 | CONTEXT | Clearing the gate is a separate DRI step — why the Steward never calls `clear-gate`. | envelopes.md:19 | envelopes.md:19 |
| ENV-011 | CONTEXT | Learning-capture rationale: a distilled RULE/TRIGGER/APPLY turns a correction into a reusable rule. | envelopes.md:23 | envelopes.md:23 |
| ENV-012 | CONTEXT | "The ledger is the tally; the learning is the lesson." | envelopes.md:23 | envelopes.md:23 |
| ENV-013 | CONTEXT | steward-direct: two sources (@mention or DM), one envelope kind. | envelopes.md:27 | envelopes.md:27 |
| ENV-014 | ABSOLUTE | "The DM path additionally admits only allow-listed senders...That gate is not yours to reason about..." | envelopes.md:29 | envelopes.md:29 |
| ENV-015 | CONTEXT | Why the header carries a reply-to ref anyway: @mention answered publicly, DM answered privately. | envelopes.md:31 | envelopes.md:31 |
| ENV-016 | CONTEXT | The ref is the transport's own opaque handle — why the Steward copies rather than interprets it. | envelopes.md:31 | envelopes.md:31 |
| ENV-017 | ABSOLUTE | "The briefing-ack is never optional." A briefing reply has no initiative to route to. | envelopes.md:35 | envelopes.md:35 |
| ENV-018 | POLICY | Holds even when the substance is routed elsewhere — the ack shrinks to a pointer but still gets sent. | envelopes.md:35 | envelopes.md:35 |
| ENV-019 | CONTEXT | No bead lives behind the Briefings topic — cross-initiative by construction. | envelopes.md:37 | envelopes.md:37 |
| ENV-020 | CONTEXT | Not a fixed "always bounce back to Briefings" case — no beads state to check "open." | envelopes.md:37 | envelopes.md:37 |
| ENV-021 | CONTEXT | Briefing topic identity (cross-ref SKILL.md §7): no initiative bead backs the `briefing` handle. | envelopes.md:41 | envelopes.md:41 |

## ABSOLUTE REWORDINGS

Every ABSOLUTE-class row was individually checked against its BEFORE text (39 total, minus STW-002 DELETED = 38 checked for wording). 24 are byte-identical after the post-review restores (STW-004, 005, 007, 008, 009, 010, 012, 017, 047, 085 (clause), 093, 094, 095, 135; MSG-002, 004, 015; OPS-003, 007, 024; ENV-002, 014, 017 — and STW-018, restored; 24 rows). The following 15 entries (all in SKILL.md) were reworded in the condensation pass — both texts quoted in full. STW-018 appears here for the record but now ships byte-identical:

**STW-018** (SKILL.md:30)
- BEFORE: "Then end the turn immediately — run nothing else, not ledger stats, not learnings, not execution-status, not `ateam mail inbox`: draining mail as a duplicate consumes the incumbent's unread messages."
- AFTER: byte-identical to BEFORE (an earlier draft dropped the "run nothing else" enumeration; RESTORED after review — the next bullet instructs running exactly those commands, so the prohibition is load-bearing).

**STW-025** (SKILL.md:41)
- BEFORE: "Use the canonical `ateam mail send` / `ateam mail inbox` — never the deprecated flat `send`/`inbox` aliases."
- AFTER: "Use canonical `ateam mail send`/`ateam mail inbox`, never the deprecated flat `send`/`inbox` aliases."

**STW-032** (SKILL.md:47)
- BEFORE: "pull both at decision time, never from the startup load."
- AFTER: "pull both at decision time, never from startup."

**STW-036** (SKILL.md:50)
- BEFORE: "Nothing goes to the ledger yet — the verdict is pending until Eric replies."
- AFTER: "Nothing to the ledger yet — pending until Eric replies."

**STW-040** (SKILL.md:61)
- BEFORE: "Clearing the gate stays the DRI's own job — you never call `ateam clear-gate`."
- AFTER: "Clearing the gate is the DRI's own job — never call `ateam clear-gate`."

**STW-044** (SKILL.md:68)
- BEFORE: "`--decision` is REQUIRED on `corrected`, optional on `accepted`."
- AFTER: "`--decision` REQUIRED on `corrected`, optional on `accepted`."

**STW-054** (SKILL.md:91) — meaningful condensation
- BEFORE: "Never omit `--to`." Not on either branch, not when General is obviously right. [rationale, STW-055:] `--to` is the only surviving record of which conversation you believed you were answering.
- AFTER: "**Never omit `--to`** — it's the only record of which conversation you believed you were answering."
- Lost: "Not on either branch, not when General is obviously right" (the explicit no-exceptions framing) and "surviving."

**STW-056** (SKILL.md:93) — meaningful condensation, flagged
- BEFORE: "Copy the ref verbatim: everything between `reply-to:` and the closing `>>>`, byte for byte." [STW-057:] It's one opaque token, not a structure (often contains its own colons) — never split it, trim it, reformat it, or retype it from memory.
- AFTER: "**Copy the ref verbatim**, byte for byte — one opaque token (`8675309:42` is a single ref, not two fields), never split, trimmed, reformatted, or retyped from memory."
- Lost: the explicit delimiter description ("everything between `reply-to:` and the closing `>>>`") — replaced by an "opaque token" framing with a worked example instead. The behavioral instruction (copy verbatim, byte for byte, never split/trim/reformat/retype) is fully preserved.

**STW-058/STW-059** (SKILL.md:95)
- BEFORE: "Never invent one." A header with no `reply-to:` means `--to general` — the right destination for that message, not a blank to fill. [STW-059:] never carry a ref over from an earlier envelope; each one addresses only its own conversation.
- AFTER: "**Never invent one.** No `reply-to:` means `--to general` IS the destination, not a blank to fill — never carry a ref over from an earlier envelope."
- Lost: "each one addresses only its own conversation" (trailing clause).

**STW-061** (SKILL.md:102) — meaningful condensation
- BEFORE: "Post ONE briefing-ack (T-ACK) into Briefings... carrying the substance — a routing confirmation, not courtesy. Don't skip it even when the substance also goes elsewhere (step 3)."
- AFTER: "Post ONE briefing-ack (T-ACK) into Briefings (`ateam notify briefing --file <reply-file>`) — a routing confirmation, even when step 3 also routes the substance elsewhere."
- Lost: "carrying the substance" and "not courtesy"; "Don't skip it" softened to the "even when" clause.

**STW-076** (SKILL.md:131)
- BEFORE: "the one autonomous cleanup allowed is `ateam reap-orphans`."
- AFTER: "Only autonomous cleanup: `ateam reap-orphans`."

**STW-088** (SKILL.md:147)
- BEFORE: "answer from the records, not recollection."
- AFTER: "and answer from the records."
- Lost: "not recollection" (the explicit contrast).

**STW-100** (SKILL.md:163)
- BEFORE: "Never report that unit or gate tests passed."
- AFTER: (folded into STW-099) "Green gates are silent — never report a passing test."

**STW-109** (SKILL.md:173)
- BEFORE: "reproduce it VERBATIM on its own line: never summarized away, never wrapped in markdown, never truncated, and it does not count against the word budget."
- AFTER: "Reproduce a plan-page URL VERBATIM on its own line — never summarized, markdown-wrapped, or truncated, and it doesn't count against the word budget"

Not shown above but worth noting: STW-013, STW-136 are POINTER-class (not ABSOLUTE) reworded pointers — listed in the main table only.

## UNACCOUNTED

None. Zero rows required this marking. Every one of the 13 DELETED rows (STW-002, STW-130, MSG-010, MSG-022, MSG-023, MSG-024, MSG-025, MSG-026, MSG-027, OPS-002, OPS-021 [moved], OPS-022, OPS-023) has a real, checkable citation for either its removal or its survival elsewhere; STW-078 and OPS-021 are MOVED-TO with a genuine relocated citation.
