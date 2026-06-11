# Agent Teams — Framework Design

**Date:** 2026-06-11
**Status:** Approved design, pre-implementation
**Origin:** Distilled from a live DRI-led multi-agent feature delivery session (midgard `specialty-products-v2`, PR #3499), where the roles, the DRI playbook, and the failure modes documented here were developed and battle-tested.

## 1. Vision

Many independent **DRI + agent-team** sessions run on a machine — one per feature/initiative, each in its own worktree or repo copy. Each DRI is a Claude session that owns delivery end-to-end: it plans via beads, runs a background team of role agents, keeps the human responsive-to (never blocked-by), and drives to a pushed branch + PR. Sessions are spawnable headlessly (`claude --bg "/dri <problem>"`) and reachable via agent view. Above them, eventually, sits an **overseer** that watches all initiatives, surfaces what needs the human, and talks to DRIs. Knowledge compounds: every role agent reads and contributes to a shared, synced learning store, so every Planner benefits from every Planner.

**v1 scope:** the role agents, the DRI skill, the global workspace (learnings + initiative registry), setup and status skills, one hook — packaged as a Claude plugin in a personal marketplace repo. Workstreams 1–3 (learning curation, overseer, CLI) are explicitly deferred but have their seams built in.

## 2. Decisions (settled)

| # | Decision | Choice |
|---|----------|--------|
| D1 | Task tracking | **Beads-first, hard dependency.** No fallback to harness task tools. The skill fails fast with an install pointer if `bd` is absent. |
| D2 | Framework home | **Own marketplace repo** (this repo). `claude-config` stays purely personal preferences. |
| D3 | Learning loop in v1 | **Habit + store, no curation.** Every agent reads role learnings on spawn and contributes before finishing. Curation is workstream 1. |
| D4 | Learning store | **Beads, in a global workspace.** A future CLI wraps the same workspace (interface layer over the same persistence — no migration). |
| D5 | Workspace location | **Env var `AGENT_TEAMS_HOME`, default `~/.agent-teams`.** All references use `${AGENT_TEAMS_HOME:-$HOME/.agent-teams}`. Override once in `~/.claude/settings.json` `env` block. |
| D6 | Workspace durability | **Git-repo-backed.** The workspace is a git repo with a private remote (e.g. `erlloyd/agent-teams-memory`); beads syncs via the dolt data refs on that remote. Knowledge is cross-machine by construction. Separate repo from the framework (different lifecycles: shareable framework vs. personal knowledge). |
| D7 | DRI packaging | **A skill, not an agent.** The DRI must be the human-facing main session (owns questions, approvals, interrupts). `/dri <problem>` converts the current session. |
| D8 | Role packaging | **Four plugin agents** with model defaults in frontmatter: planner (opus), implementer (sonnet), tester (sonnet), reviewer (sonnet). Overridable at spawn. |
| D9 | Headless behavior | **No separate autonomy mode.** `--bg` sessions are parked-and-reachable, not headless: human gates stay; a parked gate is made *discoverable* via the registry, and the human (later: overseer) answers via attach. |
| D10 | Registry granularity | **One issue per initiative, not per session.** `/dri` resumes an open initiative matching cwd. |
| D11 | Needs-human signal | **`bd human` on the initiative issue** in the global workspace — the canonical machine-wide "waiting on a human" signal. Question text is a note on the initiative. |
| D12 | Hooks vs prompts | **Hooks for deterministic must-happens; prompts for judgment.** v1 ships exactly one hook: compaction recovery. The dashboard is pull (`/initiatives`), never push — teammate sessions are real sessions and would receive any broadcast hook. |
| D13 | Setup | **Dedicated `setup-agent-teams` skill** (one-time, multi-step: bd check, clone-or-init workspace, remote config, smoke test). The DRI skill keeps only a fast preflight. |

## 3. Repo & plugin layout

```
agent-teams/                          ← this repo; a Claude plugin marketplace
├── .claude-plugin/marketplace.json   ← lists plugins; v1: agent-teams only
├── README.md                         ← install, concepts, spawn recipes (incl. claude --bg)
├── docs/                             ← this design; future specs
└── plugins/
    └── agent-teams/
        ├── .claude-plugin/plugin.json
        ├── CLAUDE.md                 ← thin: beads requirement, AGENT_TEAMS_HOME convention
        ├── agents/
        │   ├── planner.md            (model: opus)
        │   ├── implementer.md        (model: sonnet)
        │   ├── tester.md             (model: sonnet)
        │   └── reviewer.md           (model: sonnet)
        ├── skills/
        │   ├── dri/
        │   │   ├── SKILL.md          ← the playbook (invocable /dri; description auto-triggers on DRI-shaped prompts)
        │   │   └── references/       ← gate protocol, worktree/merge mechanics, teardown checklist
        │   ├── setup-agent-teams/    ← one-time machine setup
        │   └── initiatives/          ← pull dashboard: registry + needs-human list
        └── hooks/                    ← SessionStart(source: compact) recovery hook + script
```

**Reserved, named in README, empty in v1:** `plugins/overseer/` (workstream 2), `cli/` (workstream 3). Growth is additive — new siblings, not surgery.

## 4. The DRI skill (`/dri`)

Converts the current session into the DRI for one initiative. Invocation forms:

- `/dri <problem statement>` — new initiative (interactive or via `claude --bg "/dri …"`).
- `/dri` in a worktree with an open registered initiative — **resume**.
- A problem statement given where an open initiative already matches cwd → pause and confirm: append to the existing initiative vs. start a new one. Closed initiatives never match.

### Phases

1. **Preflight.** `bd` present and global workspace exists (else: "run `/setup-agent-teams`"). Confirm cwd is the dedicated worktree/checkout — the DRI owns its checkout. Derive a unique team name (repo+branch slug). Show the `/initiatives` one-liner once.
2. **Register or resume.** New: create the initiative issue in the global workspace ({repo, worktree, branch, team name, problem statement, spawn mode}). Resume: recover state — registry entry, bd plan in the project repo, branch/PR state — report "here's where this stands," clean/recreate the team (prior members are dead), spawn fresh. Either way: append a session note ("session N, date, interactive/bg"). Keep status current at every phase change.
3. **Clarify.** Investigate first (delegate exploration; never spend the human's answers on grep-able questions). Ask only questions that change the design. **Gate protocol** (used at every human gate): note the question on the initiative issue → `bd human <initiative-id>` → ask and park. While parked, keep any work that doesn't depend on the answer moving; batch questions.
4. **Plan.** Spawn **one or more planner agents** (persistent team members). The plan lands as beads in the *project* repo: loop-closing set first, enhancements dependency-gated, parallel tracks explicitly file-disjoint, a contract/interface bead first so tracks fan out against frozen interfaces. **Plan-approval gate** (gate protocol).
5. **Execute.** `TeamCreate`; spawn role agents in background (team-joined, `run_in_background`). Implementers are **ephemeral**: spawned per work-package, one worktree per parallel track off the frozen contract, merged track-by-track into the integration branch by the DRI, shut down on verified completion. Tester runs suites and owns live/manual verification; Reviewer reviews independently and runs the CI gate.
6. **Verify, don't trust.** The DRI's defining discipline: check every agent claim against artifacts (`bd show`, `git log`, read the diff) before acting on it; proactively inspect in-progress foundational work (don't wait for completion reports); expect crossed messages (idle ≠ done; "fixed" ≠ landed — verify the commit).
7. **Deliver.** Quality gates green — including a **real build**, not just typecheck (bundler-level errors like RSC boundary violations are invisible to tsc). Push branch, open PR, **never merge**. Registry → delivered, PR link recorded.
8. **Teardown.** Shut down teammates; remove worktrees; sweep orphaned processes (watch-mode test runners are repeat offenders); close/annotate beads; push the project repo **and** the global workspace (dolt sync); contribute `dri:*` learnings.

### Role-division rules the skill states explicitly

- Planner plans (beads, parallel tracks, clarifications surfaced *before* the plan is final); never writes feature code.
- Implementers write the code **and the unit tests**; never push, never merge, never guess on design — stop and ask.
- Tester *runs* suites and flags coverage gaps to implementers (who write the unit tests); may author tests where it is the natural owner (E2E specs, fixtures, harness). Owns manual/live verification.
- Reviewer never fixes; reports findings with file:line; DRI routes fixes to fresh implementers.
- The DRI orchestrates and integrates; does not do IC work beyond trivial glue; escalates to the human only when input is load-bearing.

## 5. Agent definitions

Common boilerplate, repeated per file (duplication over a wrong abstraction): beads-first (no harness task tools); learning hooks (read `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" memories <role>` on spawn; contribute `<role>:<slug>` before finishing — only insights a different `<role>` on a different repo would benefit from); team comms (report to team-lead via SendMessage, idle awaiting follow-ups, honor shutdown requests).

**planner.md (opus).** Investigates, designs, decomposes into beads; never writes feature code. Clarifications surfaced to the DRI before the plan is finalized. Loop-closing set first; enhancements gated; parallel tracks file-disjoint; contract bead first. Recovers context from `bd show` on spawn (plan-in-beads makes planner death a non-event). On design pivots: append/supersede notes, never erase history. Persistent.

**implementer.md (sonnet).** Ephemeral. Claim bead → implement → write unit tests → quality gates (build packages → typecheck → lint → repo checks → tests, always single-run/non-watch) → commit per bead referencing the id. Works only in its assigned worktree; never modifies the frozen contract or another track's files; stops and asks on any design ambiguity; no push/merge/branch-switch.

**tester.md (sonnet).** Runs suites; flags coverage gaps; may author E2E/fixture tests. Owns manual/live verification: dev-server lifecycle, role auth via storage states, config overrides as ephemeral uncommitted scaffolding (verify clean diff before finishing). Never reads or prints env/secret files — credentials flow only through the test harness. Single-run test invocations only.

**reviewer.md (sonnet).** Independent: never fixes. Reviews the diff against the spec in beads, not just code quality; runs the CI gate including a real build; reports material, confidence-filtered findings with file:line.

## 6. Global workspace conventions

Two kinds of state, two beads primitives:

**Role learnings — memories.** Keys `planner:<slug>`, `implementer:<slug>`, `tester:<slug>`, `reviewer:<slug>`, `dri:<slug>`. Body: insight + `WHY:` + `HOW TO APPLY:` (uppercase prefixes — markdown `#` after a newline trips the command-safety matcher). Quality bar: transferable technique only.

**Initiative registry — issues.** One per initiative, resumable (D10). Fields in description: repo path, worktree, branch, team name, problem statement, spawn mode. Phase changes and session starts as notes. Needs-human via `bd human` on the issue (D11) — `bd -C … human list` is intrinsically grouped by initiative. Closed on delivery with PR link.

**Sync:** DRI teardown pushes the workspace; `setup-agent-teams` clones/pulls on new machines. A team-shared memory repo is possible later by pointing the remote at a shared repo (collaboration semantics deferred).

## 7. Hooks (v1: one)

**SessionStart, `source: compact` only.** If cwd matches a registered open initiative: re-inject that initiative's context (status, team name, "the DRI skill governs this session"). Self-scoping; never broadcasts machine-wide state. Rationale: teammates are real sessions — any broadcast SessionStart hook would inject machine-level state into every ephemeral implementer (noise + distraction), and there is no trustworthy signal to detect teammate-ness. The dashboard is therefore pull-only: `/initiatives`, plus once at `/dri` preflight, with the overseer as the eventual push channel.

**Deferred hardening (named, not built):** PreToolUse guard blocking `git push` from implementer contexts; Stop-hook registry-freshness check; SubagentStop learning nudges.

## 8. `setup-agent-teams` and `/initiatives`

**setup-agent-teams (one-time per machine):** verify `bd` installed (fail with pointer) → workspace present? clone from the memory remote (knowledge arrives with it) or init fresh + configure remote → env-var guidance if non-default location → smoke test (remember/memories roundtrip + sync push).

**/initiatives (pull dashboard):** formatted view of `bd -C … list` + `bd -C … human list` — one line per initiative, needs-human entries highlighted with their parked questions.

## 9. Deferred workstreams and their v1 seams

| Workstream | What it adds | The seam v1 ships |
|---|---|---|
| **WS1 — Learning curation** | A curator that dedupes, synthesizes, promotes per-role digests, prunes noise | Role-scoped keys in a shared, synced workspace already accumulating data |
| **WS2 — Overseer** | Sibling plugin: watches all initiatives, reports status, prioritizes for the human, attaches/talks to DRIs, water-cooler | The registry + `bd human` flags (its entire data feed), and the spawn recipe |
| **WS3 — CLI** | `at spawn <repo> "<problem>"` (prepare worktree → `claude --bg "/dri …"`), `at status`, `at learn <role> "…"`, messaging | Everything underneath is bd + the claude CLI — the CLI is sugar; zero migration |

## 10. Build-time verifications (assumptions to confirm during implementation)

1. Slash command as a `--bg` initial prompt invokes the skill (`claude --bg "/dri …"`). Fallback already designed: the skill description auto-triggers on DRI-shaped prompts.
2. `AskUserQuestion` behavior when parked detached and attached later (gate is already designed to be robust either way: registry flag + note first, then ask — plain-text question as fallback).
3. Exact beads sync commands for a home-level workspace against a git remote (`bd dolt push/pull` semantics outside a project repo).
4. `bd human` syntax for flag/respond/dismiss and how the question text is best attached (note vs. comment).
5. Plugin hook config format for `SessionStart` with `source: compact` matching.

## 11. Out of scope for v1

Learning curation/synthesis; the overseer; the CLI; team-shared memory semantics; push-based dashboards; fire-and-forget autonomy (no-human gates) — relevant only once the overseer can be the decision-maker; cross-session DRI↔DRI messaging.
