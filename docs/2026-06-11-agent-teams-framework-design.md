# Agent Teams — Framework Design

> **Historical snapshot.** Records the approved pre-implementation design; on PR/lifecycle specifics the shipped behavior is authoritative in the `/dri` skill (`plugins/agent-teams/skills/dri/`).

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
| D5 | Workspace location | **Env var `AGENT_TEAMS_HOME`, default `~/.agent-teams` — resolved INSIDE the bundled `ateam` script (D14), never inline in command strings.** Originally motivated by `${VAR:-default}` tripping Claude Code's "Contains expansion" permission prompt in interactive sessions. Under bypass (D15), that prompt is moot for backgrounded agents — but resolving the var inside the script remains clean: a stable interface, no prompt risk in interactive mode, and the script's logic lives in one place. Override once in `~/.claude/settings.json` `env` block. |
| D6 | Workspace durability | **Git-repo-backed.** The workspace is a git repo with a private remote (e.g. `erlloyd/agent-teams-memory`); beads syncs via the dolt data refs on that remote. Knowledge is cross-machine by construction. Separate repo from the framework (different lifecycles: shareable framework vs. personal knowledge). |
| D7 | DRI packaging | **A skill, not an agent.** The DRI must be the human-facing main session (owns questions, approvals, interrupts). `/dri <problem>` converts the current session. |
| D8 | Role packaging | **Four plugin agents** with model defaults in frontmatter: planner (opus), implementer (sonnet), tester (sonnet), reviewer (sonnet). Overridable at spawn. |
| D9 | Headless behavior | **Bypass + bg, not a separate autonomy mode.** `--dangerously-skip-permissions` + `--bg` together give hands-off operation: `claude --bg --dangerously-skip-permissions "/dri …"`. Human gates still exist — they park (the session stops at the gate, records it via `bd human`, and waits); a parked gate is *discoverable* via the registry, and the human (later: overseer) answers via attach. Bypass removes the permission-prompt interrupts; gate protocol remains. |
| D10 | Registry granularity | **One issue per initiative, not per session.** `/dri` resumes an open initiative matching cwd. |
| D11 | Needs-human signal | **`bd human` on the initiative issue** in the global workspace — the canonical machine-wide "waiting on a human" signal. Question text is a note on the initiative. |
| D12 | Hooks vs prompts | **Hooks for deterministic must-happens; prompts for judgment.** v1 ships exactly one hook: compaction recovery. The dashboard is pull (`/initiatives`), never push — teammate sessions are real sessions and would receive any broadcast hook. |
| D13 | Setup | **Dedicated `setup-agent-teams` skill** (one-time, multi-step: bd check, clone-or-init workspace, remote config, smoke test). The DRI skill keeps only a fast preflight. |
| D14 | Workspace access | **A bundled script — `plugins/agent-teams/scripts/ateam`** — is the single way skills/agents touch the workspace (verbs: ws, list, list-json, human-list, resume-match, register, note, gate, clear-gate, learn, learnings, show, close, sync). Rationale: (1) one clean workspace interface — command strings become fixed paths, one narrow allowlist entry for interactive sessions; (2) sequenced conventions (gate = note+flag) are implemented once instead of prompt-followed; (3) it is the embryonic WS3 `ateam` CLI, shipped early. **Finalized invocation (verified):** no symlink, no install artifact. Callers invoke `scripts/ateam` by its **absolute path, resolved fresh from the plugin directory each session** — skills from their injected base dir, and the DRI passes the resolved path to each teammate at spawn. The script resolves `AGENT_TEAMS_HOME` internally; `AGENT_TEAMS_HOME` relocates only the DATA workspace. Note: under bypass (D15), backgrounded agents skip all permission prompts, so the allowlist is no longer a prerequisite for hands-off operation — it remains useful for interactive DRI sessions. The remaining friction — a long, version-shifting path — is what **WS3 removes by shipping `ateam` as a real CLI on PATH** (one stable name, no path resolution). The symlink was a poor-man's "put it on PATH"; WS3 is the real version. |
| D15 | Hands-off permission model | **Backgrounded agents run with permissions bypassed.** Verified: `claude --bg --dangerously-skip-permissions "/dri …"` skips all permission prompts for the DRI session itself, including the hardcoded "Contains expansion" check. The DRI spawns teammates via the Agent tool with `mode: bypassPermissions`, which extends the same guarantee to every background teammate. This is the actual mechanism for hands-off operation — not prompt-avoidance command hygiene (which was the pre-bypass workaround). The behavioral guardrails that matter under bypass: role rules (teammates never push/merge/deploy — DRI-only) and worktree isolation; the skill and agent definitions enforce these. The `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS` env var must also be set in `~/.claude/settings.json` `env` block — without it, `TeamCreate` silently no-ops and the team-orchestration model fails at Phase 4. |
| D16 | Beads mode & isolation | **(a) Embedded mode** for both the global `~/.agent-teams` workspace and project repos — NOT server mode. Rationale: agent-teams writes are dominated by independent inserts (`register`/`bd create`) and distinct-key upserts (`ateam learn`), which embedded serializes with zero loss (verified 12-way concurrent; §6 of verifications.md). The one lossy case — concurrent same-record field-RMW, silent exit 0 — does not occur by design: the initiative registry and individual beads each have a single sequential owner (the DRI), and learning keys are distinct per agent. Server mode rejected: adds a daemon to keep running AND does not fix the app-layer RMW race anyway. **(b) Agent isolation uses git WORKTREES of the project repo, never independent clones.** Worktrees share the one `.beads/` (and `.git/`) via git-common-dir discovery, so all agents see the same project issue DB. Separate clones each get their own `.beads/`, fragmenting the issue DB into disconnected copies. Blessed path: `bd worktree create` (guarantees shared-`.beads/` discovery); plain `git worktree add` also works. |

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

Converts the current session into the DRI for one initiative.

### Prime directive

**The DRI's job is to DELIVER: always driving to a PR that solves the problem.** The skill states the outcome hierarchy explicitly, because it governs every judgment call:

1. **Perfect:** a PR opened that delivers the requested feature with **zero human interaction**.
2. **Good:** a correct PR that needed the human only for genuinely load-bearing decisions.
3. **Lesser failure:** asking the human anything the DRI could have figured out itself — by reading the code, or by spawning agents to investigate. Self-serve before asking, always.
4. **Worst failure:** opening a PR that does not solve the problem. Asking beats delivering wrong; investigating beats asking.

Invocation forms:

- `/dri <problem statement>` — new initiative (interactive or via `claude --bg "/dri …"`).
- `/dri` in a worktree with an open registered initiative — **resume**.
- A problem statement given where an open initiative already matches cwd → pause and confirm: append to the existing initiative vs. start a new one. Closed initiatives never match.

### Phases

1. **Preflight.** `bd` present and global workspace exists (else: "run `/setup-agent-teams`"). Confirm cwd is the dedicated worktree/checkout — the DRI owns its checkout. Derive a unique team name (repo+branch slug). Show the `/initiatives` one-liner once.
2. **Register or resume.** New: create the initiative issue in the global workspace ({repo, worktree, branch, team name, problem statement, spawn mode}). Resume: recover state — registry entry, bd plan in the project repo, branch/PR state — report "here's where this stands," clean/recreate the team (prior members are dead), spawn fresh. Either way: append a session note ("session N, date, interactive/bg"). Keep status current at every phase change.
3. **Clarify.** Investigate first (delegate exploration; never spend the human's answers on grep-able questions). Ask only questions that change the design. **Gate protocol** (used at every human gate): note the question on the initiative issue → `bd human <initiative-id>` → ask and park. While parked, keep any work that doesn't depend on the answer moving; batch questions.
4. **Plan.** Spawn **one or more planner agents** (persistent team members). The plan lands as beads in the *project* repo: loop-closing set first, enhancements dependency-gated, parallel tracks explicitly file-disjoint, a contract/interface bead first so tracks fan out against frozen interfaces. **Plan-approval gate** (gate protocol).
5. **Execute.** `TeamCreate`; spawn role agents in background (team-joined, `run_in_background`). Implementers are **ephemeral**: spawned per work-package, one worktree per parallel track off the frozen contract, merged track-by-track into the integration branch by the DRI, shut down on verified completion. Tester runs suites and owns live/manual verification; Reviewer reviews independently and runs the CI gate. **The discovery loop:** agents file `discovery`-labeled beads for anything they find that needs investigation outside their scope (implementers are the primary source); the DRI continuously triages these and spawns agents to investigate (often a planner) rather than letting findings die in reports — this triage loop, not just the planned beads, is how the team converges on a PR that actually solves the problem.
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

Common boilerplate, repeated per file (duplication over a wrong abstraction): beads-first (no harness task tools); learning hooks (read `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" memories <role>` on spawn; contribute `<role>:<slug>` before finishing — only insights a different `<role>` on a different repo would benefit from); team comms (report to team-lead via SendMessage, idle awaiting follow-ups, honor shutdown requests); **discovery beads** (file a `discovery`-labeled bead in the project repo for anything found that needs investigation outside your scope — never let a finding die in a report).

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
| **WS3 — CLI** | `ateam spawn <repo> "<problem>"` (prepare worktree → `claude --bg "/dri …"`), `ateam status`, `ateam learn <role> "…"`, messaging | Everything underneath is bd + the claude CLI — zero migration. Beyond sugar, installing `ateam` as a real CLI **on PATH** is the clean fix for v1's one wart: callers type `ateam` (stable name, one allowlist entry; naming it `ateam` rather than `at` also avoids colliding with the Unix `at` job-scheduler command on PATH) instead of resolving the script's long, version-shifting plugin path each session. |

### CLI codification candidates (WS3 scoping)

Principle: the CLI should codify **sequenced multi-step conventions and schema-bearing writes** — the things most likely to be forgotten, done differently across sessions, or lost to compaction. Judgment stays in prompts. Each command replaces a paragraph of prompt instruction with one verb, which both shrinks the skill/agent prompts and makes the behavior uniform. v1 ships these as prompt conventions deliberately shaped like CLI calls (exact sequences, exact schemas) so WS3's lift is mechanical.

Ranked by drift-risk × frequency:

| Candidate | Today (prompt convention) | As CLI | Why codify |
|---|---|---|---|
| **Gate protocol** | note question on initiative → `bd human <id>` → park | `ateam gate "<question>"` / `ateam gates` | The visibility lifeline; three commands in exact order; if done differently once, an initiative waits invisibly |
| **Registry lifecycle** | create with field schema; cwd→initiative resume match; phase notes; close with PR link | `ateam register` / `ateam resume` / `ateam phase <p>` / `ateam close <pr>` | Schema-bearing writes + matching logic that belongs in code, not re-derived from prose each session |
| **Teardown** | shutdown teammates, remove worktrees, sweep orphaned processes, push project repo + workspace, contribute learnings | `ateam teardown` | Runs exactly when context is thinnest (end of long session); checklist-as-code |
| **Preflight** | bd present, workspace present, cwd-is-dedicated-checkout, open-initiative match | `ateam preflight` (machine-readable output the skill consumes) | Deterministic checks; resume-matching in code |
| **Workspace sync** | dolt push/pull of `$AGENT_TEAMS_HOME` at teardown/setup | `ateam sync` | Trivially forgettable; silent when skipped |
| **Learning I/O** | `bd -C $ATH memories <role>` / `remember --key <role>:<slug>` | `ateam learnings <role>` / `ateam learn <role> "…"` | Enforces the key schema + quality bar in help text; removes env-var plumbing from every agent prompt |
| **Track worktrees** | worktree-per-parallel-track at the frozen-contract tip; advance all tracks when the contract moves | `ateam track new <name>` / `ateam track sync` | Session evidence: worktrees were twice created/left at a stale contract commit — exactly the class of mechanical error code prevents |
| **Spawn** | prepare worktree → `claude --bg "/dri …"` | `ateam spawn <repo> "<problem>"` | WS3's core; the overseer's hands |
| Discovery beads | `bd create --label=discovery` | (`ateam discover "…"` — marginal) | Near-atomic already; lowest priority |

## 10. Build-time verifications (assumptions to confirm during implementation)

1. Slash command as a `--bg` initial prompt invokes the skill (`claude --bg "/dri …"`). Fallback already designed: the skill description auto-triggers on DRI-shaped prompts.
2. `AskUserQuestion` behavior when parked detached and attached later (gate is already designed to be robust either way: registry flag + note first, then ask — plain-text question as fallback).
3. Exact beads sync commands for a home-level workspace against a git remote (`bd dolt push/pull` semantics outside a project repo).
4. `bd human` syntax for flag/respond/dismiss and how the question text is best attached (note vs. comment).
5. Plugin hook config format for `SessionStart` with `source: compact` matching.

## 11. Out of scope for v1

Learning curation/synthesis; the overseer; the CLI; team-shared memory semantics; push-based dashboards; fire-and-forget autonomy (no-human gates) — relevant only once the overseer can be the decision-maker; cross-session DRI↔DRI messaging.
