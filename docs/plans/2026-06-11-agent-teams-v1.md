# Agent Teams v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the agent-teams Claude plugin marketplace: four role agents, the `/dri` playbook skill, `/setup-agent-teams`, `/initiatives`, and the compaction-recovery hook — per the approved spec at `docs/2026-06-11-agent-teams-framework-design.md`.

**Architecture:** A plugin marketplace repo (`agent-teams`) containing one plugin (`plugins/agent-teams/`). Deliverables are prompt artifacts (markdown agents + skills), JSON config, and one shell script (the hook — the only TDD-able code). All runtime state lives in beads: the project repo (plans, discovery beads) and a global git-backed workspace at `${AGENT_TEAMS_HOME:-$HOME/.agent-teams}` (role learnings + initiative registry).

**Tech Stack:** Claude Code plugins (marketplace.json / plugin.json / agents / skills / hooks), beads (`bd`, hard dependency), bash + jq for the hook.

**Conventions used throughout:**
- `$ATH` in prose means `${AGENT_TEAMS_HOME:-$HOME/.agent-teams}` — always written out in full in artifact files.
- Registry issue description schema (machine-parsed by the hook; line-oriented):
  ```
  problem: <one-line problem statement>
  repo: <abs path to main repo>
  worktree: <abs path of the checkout the DRI owns>
  branch: <branch name>
  team: <team slug>
  mode: interactive|bg
  ```
- Task 2 produces `docs/verifications.md` recording **exact verified bd syntax** (init flags, sync commands, `bd human` subcommands, `bd list --json` field shape). Tasks 3 and 8–11 embed commands per the primary assumptions below and include a reconcile step against `docs/verifications.md`. If a verified command differs, the reconcile step fixes the artifact — do not skip it.

---

### Task 1: Repo scaffold — marketplace + plugin manifests

**Files:**
- Create: `.claude-plugin/marketplace.json`
- Create: `plugins/agent-teams/.claude-plugin/plugin.json`
- Create: `plugins/agent-teams/CLAUDE.md`

- [ ] **Step 1: Write marketplace.json**

```json
{
  "name": "agent-teams",
  "owner": { "name": "Eric Lloyd", "email": "erlloyd@gmail.com" },
  "metadata": {
    "description": "DRI-led multi-agent software delivery: role agents, the /dri playbook, and a shared learning workspace",
    "version": "0.1.0"
  },
  "plugins": [
    {
      "name": "agent-teams",
      "source": "./plugins/agent-teams",
      "description": "Role agents (planner, implementer, tester, reviewer), the /dri skill, initiative registry, and learning-store conventions. Hard-requires beads (bd)."
    }
  ]
}
```

- [ ] **Step 2: Write plugin.json**

```json
{
  "name": "agent-teams",
  "version": "0.1.0",
  "description": "DRI-led agent teams: the /dri playbook, four role agents, a global learning workspace and initiative registry. Hard-requires beads (bd).",
  "author": { "name": "Eric Lloyd" }
}
```

- [ ] **Step 3: Write the thin plugin CLAUDE.md**

```markdown
# agent-teams

This plugin hard-requires **beads** (`bd`) — all work tracking is beads-first. Never use TodoWrite/TaskCreate/markdown TODO lists in agent-teams workflows.

**Global workspace:** `${AGENT_TEAMS_HOME:-$HOME/.agent-teams}` — a git-backed beads workspace holding role learnings (`bd remember` memories with keys `<role>:<slug>`) and the initiative registry (bd issues, one per initiative). If it does not exist, run `/setup-agent-teams`.

**Skills:** `/dri <problem>` — run/resume an initiative as DRI. `/initiatives` — machine-wide initiative dashboard. `/setup-agent-teams` — one-time machine setup.
```

- [ ] **Step 4: Validate JSON parses**

Run: `jq . .claude-plugin/marketplace.json plugins/agent-teams/.claude-plugin/plugin.json`
Expected: both documents echoed, exit 0.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: marketplace + plugin scaffold"
```

---

### Task 2: Verify bd mechanics (spec §10 V3 + V4) and record them

Everything downstream embeds bd commands; verify them against a throwaway workspace FIRST.

**Files:**
- Create: `docs/verifications.md`

- [ ] **Step 1: Create a throwaway home-level workspace and verify init + memories**

```bash
T=$(mktemp -d) && mkdir "$T/ws" && git -C "$T/ws" init
bd -C "$T/ws" init --prefix at
bd -C "$T/ws" remember --key "planner:test-slug" "test insight. WHY: test. HOW TO APPLY: test."
bd -C "$T/ws" memories planner
```
Expected: init succeeds non-interactively (if `--prefix` is wrong/interactive, find the working non-interactive form via `bd init --help` and record it); memories output contains `planner:test-slug`.

- [ ] **Step 2: Verify `bd human` syntax on a registry-shaped issue**

```bash
printf 'problem: test\nrepo: %s\nworktree: %s\nbranch: t\nteam: t\nmode: interactive\n' "$T" "$T" > "$T/body.md"
bd -C "$T/ws" create --title="Test initiative" --type=task --priority=2 --body-file="$T/body.md"
bd -C "$T/ws" list --status=open
```
Then with the created id (call it `at-1`): run `bd human --help`; verify the flag/list/respond/dismiss forms (expected shapes: `bd -C "$T/ws" human at-1`, `bd -C "$T/ws" human list`, dismiss/respond variants). Record exact working syntax, including how question text attaches best (`bd note` vs `bd comment` — try `bd -C "$T/ws" note at-1 --file=...` with a question line, confirm it renders in `bd show at-1`).

- [ ] **Step 3: Verify `bd list --json` shape for hook matching**

Run: `bd -C "$T/ws" list --status=open --json | jq .`
Record: does each element include the full `description`? If yes, the hook matches on `description | contains("worktree: $PWD")`. If no, verify fallback: `bd -C "$T/ws" search "$T" --json` (search by worktree path) — record which strategy works and the exact jq path to `id`.

- [ ] **Step 4: Verify dolt sync against a local bare remote (V3)**

```bash
git init --bare "$T/remote.git"
git -C "$T/ws" remote add origin "$T/remote.git"
bd -C "$T/ws" dolt push
git -C "$T/ws" ls-remote origin
```
Expected: push succeeds and a `refs/dolt/data`-style ref appears on the remote. Then verify the clone-side: `git clone "$T/remote.git" "$T/ws2" && bd -C "$T/ws2" dolt pull && bd -C "$T/ws2" memories planner` shows the test memory. If `bd dolt push/pull` isn't the verb, find it (`bd dolt --help`, `bd sync --help`) and record.

- [ ] **Step 5: Write `docs/verifications.md`**

Record, with the exact commands that worked: (1) non-interactive workspace init; (2) memory write/read; (3) `bd human` flag/list/respond/dismiss + question-attachment method; (4) the hook's working match strategy + jq path; (5) sync push/pull verbs and the clone-side bootstrap. Plus two rows left `UNVERIFIED (Task 13)`: slash-command-in---bg (V1) and AskUserQuestion-detached (V2).

- [ ] **Step 6: Clean up and commit**

```bash
rm -rf "$T"
git add docs/verifications.md && git commit -m "docs: verified bd mechanics for workspace, registry, human-flags, sync"
```

---

### Task 3: Compaction-recovery hook (TDD)

**Files:**
- Create: `tests/hook-compact-recovery.test.sh`
- Create: `plugins/agent-teams/hooks/scripts/compact-recovery.sh`
- Create: `plugins/agent-teams/hooks/hooks.json`

- [ ] **Step 1: Write the failing test**

```bash
#!/usr/bin/env bash
# Tests for the compact-recovery SessionStart hook script.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/compact-recovery.sh"
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME" "$T/wt"
git -C "$AGENT_TEAMS_HOME" init -q
bd -C "$AGENT_TEAMS_HOME" init --prefix at   # adjust per docs/verifications.md
printf 'problem: test problem\nrepo: %s\nworktree: %s\nbranch: feat/x\nteam: test-team\nmode: interactive\n' "$T/wt" "$T/wt" > "$T/body.md"
bd -C "$AGENT_TEAMS_HOME" create --title="Hook test initiative" --type=task --priority=2 --body-file="$T/body.md" >/dev/null

# Case 1: cwd matches a registered open initiative -> emits context
out=$(cd "$T/wt" && "$SCRIPT")
echo "$out" | grep -q "Hook test initiative" || { echo "FAIL case1: no context for matching cwd"; exit 1; }
echo "$out" | grep -q "/dri skill governs" || { echo "FAIL case1: missing governance reminder"; exit 1; }

# Case 2: non-matching cwd -> silent, exit 0
out=$(cd "$T" && "$SCRIPT")
[ -z "$out" ] || { echo "FAIL case2: output for non-matching cwd"; exit 1; }

# Case 3: workspace absent -> silent, exit 0
out=$(env AGENT_TEAMS_HOME="$T/nope" sh -c "cd '$T/wt' && '$SCRIPT'")
[ -z "$out" ] || { echo "FAIL case3: output without workspace"; exit 1; }

# Case 4: closed initiatives never match
id=$(bd -C "$AGENT_TEAMS_HOME" list --status=open --json | jq -r '.[0].id')
bd -C "$AGENT_TEAMS_HOME" close "$id" >/dev/null
out=$(cd "$T/wt" && "$SCRIPT")
[ -z "$out" ] || { echo "FAIL case4: matched a closed initiative"; exit 1; }

echo "PASS"
```

- [ ] **Step 2: Run it to verify it fails**

Run: `chmod +x tests/hook-compact-recovery.test.sh && tests/hook-compact-recovery.test.sh`
Expected: FAIL — script not found.

- [ ] **Step 3: Implement the script**

```bash
#!/usr/bin/env bash
# SessionStart(source=compact) recovery for agent-teams.
# If cwd is the worktree of a registered OPEN initiative in the global
# workspace, re-inject that initiative's context. Silent no-op otherwise —
# never broadcasts machine-wide state (teammate sessions also fire hooks).
set -euo pipefail
ATH="${AGENT_TEAMS_HOME:-$HOME/.agent-teams}"
command -v bd >/dev/null 2>&1 || exit 0
[ -d "$ATH/.beads" ] || exit 0

# Match strategy per docs/verifications.md (primary: description in list --json)
match_id=$(bd -C "$ATH" list --status=open --json 2>/dev/null \
  | jq -r --arg wt "worktree: $PWD" \
      '[.[] | select((.description // "") | contains($wt))][0].id // empty')
[ -n "$match_id" ] || exit 0

echo "## agent-teams: initiative context (post-compaction recovery)"
bd -C "$ATH" show "$match_id" 2>/dev/null || true
cat <<'EOF'

This session is the DRI for the initiative above. The /dri skill governs it —
re-read the dri skill if its guidance is no longer in context. Recover working
state from: this initiative's notes, `bd human list` in the global workspace
(parked gates), and the project repo's beads (plan, discovery beads).
EOF
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `chmod +x plugins/agent-teams/hooks/scripts/compact-recovery.sh && tests/hook-compact-recovery.test.sh`
Expected: `PASS`. If case 1 fails because `bd list --json` lacks `description`, switch to the fallback strategy recorded in `docs/verifications.md` (`bd search "$PWD" --json`, filtered to open status) and re-run until PASS.

- [ ] **Step 5: Write hooks.json**

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "compact",
        "hooks": [
          {
            "type": "command",
            "command": "\"${CLAUDE_PLUGIN_ROOT}/hooks/scripts/compact-recovery.sh\""
          }
        ]
      }
    ]
  }
}
```

Validate: `jq . plugins/agent-teams/hooks/hooks.json` exits 0. (Whether `matcher: "compact"` is the correct source-matching form is V5 — verified live in Task 13; if wrong there, fix here.)

- [ ] **Step 6: Commit**

```bash
git add tests plugins/agent-teams/hooks && git commit -m "feat: compaction-recovery hook (TDD)"
```

---

### Task 4: `agents/planner.md`

**Files:**
- Create: `plugins/agent-teams/agents/planner.md`

- [ ] **Step 1: Write the file**

```markdown
---
description: Expert software planner for agent teams. Investigates a codebase, surfaces clarifying questions, and decomposes work into a beads plan with parallel, file-disjoint tracks that implementers can execute cleanly. Never writes feature code. Persistent team member — stays available for follow-up design questions.
model: opus
---

You are the PLANNER on an agent team led by a DRI (team-lead). You investigate, design, and maintain the plan. You do NOT write feature code.

# On spawn

1. Read role learnings: `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" memories planner` — apply anything relevant.
2. Recover context from beads: `bd show` the epic and children you are pointed at. The plan in beads IS your memory — a fresh planner must be able to take over from beads alone. Read every bead's notes; the LATEST note supersedes earlier ones when they conflict.

# Planning method

- **Investigate before asking.** Read the code, run searches, trace the paths. Surface to team-lead ONLY the questions that change the design — never anything answerable from the repo.
- **Clarifications come BEFORE the plan is final.** Report open questions to team-lead with a recommended default for each, then wait for resolutions before filing the full breakdown.
- **Decompose concentric-circles style:** a CONTRACT/interface bead first (frozen types, signatures, schemas) so parallel tracks can fan out against it; then the LOOP-CLOSING set (smallest end-to-end exercise of the new code); enhancements dependency-gated behind loop closure (`bd dep add`).
- **Mark parallelism explicitly.** Group beads into tracks that are FILE-DISJOINT (no shared files across tracks; shared edits are front-loaded into the contract bead). State which beads can run concurrently and which are joins.
- **Each bead** gets: clear title, WHY + WHAT description, acceptance criteria, concrete file references — small enough for one implementer to execute cleanly.
- **On design pivots:** append SUPERSEDED-BY notes; never erase history. Reconcile every affected bead, then report exactly which beads changed.
- Use `--body-file=` for multi-line bead bodies; use UPPERCASE prefixes (WHY:, ACCEPTANCE:) instead of markdown headers inside bodies.

# Conventions (all agent-teams roles)

- **Beads-first:** track all work in bd. Never use TodoWrite/TaskCreate/markdown TODOs.
- **Discovery beads:** anything you find that needs investigation outside your scope -> `bd create ... --label=discovery` in the project repo. Never let a finding die in a report.
- **Team comms:** report to team-lead via SendMessage; go idle awaiting follow-ups; honor shutdown requests.
- **Contribute learnings before finishing:** if you learned a transferable planning technique (one a planner on a DIFFERENT repo would benefit from), save it: `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" remember --key "planner:<short-slug>" "<insight>. WHY: <why>. HOW TO APPLY: <how>"`. Session trivia does not qualify.
```

- [ ] **Step 2: Validate against checklist**

Confirm all present: frontmatter has `description` + `model: opus`; never-writes-code stated; clarifications-before-plan; contract-first + loop-closing + gated enhancements; file-disjoint tracks; supersede-not-erase; latest-note-wins recovery; the four common conventions.

- [ ] **Step 3: Commit**

```bash
git add plugins/agent-teams/agents/planner.md && git commit -m "feat: planner agent"
```

---

### Task 5: `agents/implementer.md`

**Files:**
- Create: `plugins/agent-teams/agents/implementer.md`

- [ ] **Step 1: Write the file**

```markdown
---
description: Ephemeral implementation agent for agent teams. Claims a beads work item, implements it WITH unit tests, runs quality gates, and commits — strictly within its assigned worktree. Stops and asks on any design ambiguity. Never pushes, never merges.
model: sonnet
---

You are an IMPLEMENTER on an agent team led by a DRI (team-lead). You are EPHEMERAL: you exist to complete the work you were spawned for, then shut down when asked.

# On spawn

1. Read role learnings: `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" memories implementer` — apply anything relevant.
2. `cd` into your ASSIGNED worktree (given in your instructions). If it is a fresh worktree, install dependencies first. All work happens there.
3. `bd show` your assigned bead(s) and read ALL notes — the latest note supersedes earlier ones. The design has usually evolved; obey the latest decision.

# Work loop (per bead)

1. `bd update <id> --claim`.
2. Implement the bead exactly as specified. **You write the unit tests for your code** — they are part of the bead, not optional.
3. Quality gates, all green before closing: build packages -> typecheck -> lint -> repo-specific checks -> tests. Run tests SINGLE-RUN (e.g. `vitest run`), never watch mode — watch-mode workers orphan and eat machine memory.
4. Commit to your track branch, one commit per bead, message referencing the bead id. Close the bead.

# Hard rules

- **Stay in your lane:** only your assigned worktree; never modify the frozen contract file(s) or another track's files. If you believe the contract needs a change, STOP and ask team-lead.
- **Never guess on design.** Any ambiguity the bead notes don't resolve -> ask team-lead (or the planner) BEFORE writing code.
- **Never push, never merge, never switch branches.** The DRI owns integration.
- **Never commit scaffolding** you find in the working tree that you didn't create (e.g. someone's local override hacks) — commit only files you changed for your bead.

# Conventions (all agent-teams roles)

- **Beads-first:** track all work in bd. Never use TodoWrite/TaskCreate/markdown TODOs.
- **Discovery beads:** anything you find that needs investigation outside your scope (suspicious code, latent bugs, missing abstractions) -> `bd create ... --label=discovery` in the project repo. This feeds the DRI's triage loop — never let a finding die in a report.
- **Team comms:** report to team-lead via SendMessage (completion with commit hashes + gate results; blockers immediately); go idle awaiting follow-ups; honor shutdown requests.
- **Contribute learnings before finishing:** transferable techniques only: `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" remember --key "implementer:<short-slug>" "<insight>. WHY: <why>. HOW TO APPLY: <how>"`.
```

- [ ] **Step 2: Validate against checklist**

Confirm: ephemeral framing; assigned-worktree discipline; writes unit tests; single-run tests; stop-and-ask; no push/merge/branch-switch; no foreign scaffolding in commits; commit-per-bead with id; the four common conventions; `model: sonnet`.

- [ ] **Step 3: Commit**

```bash
git add plugins/agent-teams/agents/implementer.md && git commit -m "feat: implementer agent"
```

---

### Task 6: `agents/tester.md`

**Files:**
- Create: `plugins/agent-teams/agents/tester.md`

- [ ] **Step 1: Write the file**

```markdown
---
description: Verification agent for agent teams. Runs test suites and flags coverage gaps (implementers write the unit tests), authors E2E specs and fixtures where it is the natural owner, and owns manual/live verification of the running application. Never exposes secrets.
model: sonnet
---

You are the TESTER on an agent team led by a DRI (team-lead). Your job is verified truth about whether the software works.

# On spawn

1. Read role learnings: `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" memories tester` — apply anything relevant.
2. `bd show` the epic/beads you are pointed at to learn the intended behavior — you verify against the SPEC in beads, not against what the code happens to do.

# Division of test labor

- **Implementers write the unit tests** for their code. You RUN the suites and audit the matrix: report any role/state/edge combination not asserted, as specific named gaps for the implementer to close. Do not silently fix coverage yourself.
- **You may author tests where you are the natural owner:** E2E specs, fixtures, harness/auth setup.
- Run everything SINGLE-RUN (e.g. `vitest run`) — never watch mode (orphaned workers eat machine memory). Confirm test processes exit when you finish.

# Live / manual verification

- You own the running-app check: dev-server lifecycle (free the port, start in background, wait-for-url), driving the UI/API, and the manual test plan (cells: role x flag/config-state x expected outcome) when automation isn't warranted.
- Local config/flag overrides needed to exercise states are EPHEMERAL SCAFFOLDING: never commit them; verify `git diff` is clean of them before you finish.
- **Secrets discipline:** never read or print env files, credentials, or auth artifacts. Credentials flow only through the test harness (e.g. Playwright auth setup minting storage states from an env file the human populated). If a needed secret is missing, report the exact variable NAMES needed — never values.

# Conventions (all agent-teams roles)

- **Beads-first:** track all work in bd. Never use TodoWrite/TaskCreate/markdown TODOs.
- **Discovery beads:** out-of-scope findings (real bugs you can't fix, infra gaps) -> `bd create ... --label=discovery` in the project repo.
- **Team comms:** report to team-lead via SendMessage (per-cell pass/fail with what you actually observed — never "should work"); go idle awaiting follow-ups; honor shutdown requests.
- **Contribute learnings before finishing:** transferable techniques only: `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" remember --key "tester:<short-slug>" "<insight>. WHY: <why>. HOW TO APPLY: <how>"`.
```

- [ ] **Step 2: Validate against checklist**

Confirm: runs-suites/flags-gaps division with implementers-write-unit-tests; may-author E2E/fixtures; single-run; ephemeral-scaffolding + clean-diff; secrets discipline (names not values); observed-evidence reporting; four common conventions; `model: sonnet`.

- [ ] **Step 3: Commit**

```bash
git add plugins/agent-teams/agents/tester.md && git commit -m "feat: tester agent"
```

---

### Task 7: `agents/reviewer.md`

**Files:**
- Create: `plugins/agent-teams/agents/reviewer.md`

- [ ] **Step 1: Write the file**

```markdown
---
description: Independent review agent for agent teams. Reviews the full diff against the spec in beads, hunts duplication, edge cases, security issues, and silent failures, and runs the CI-equivalent gate including a real build. Reports findings — never fixes code itself.
model: sonnet
---

You are the REVIEWER on an agent team led by a DRI (team-lead). Your value is INDEPENDENCE: you never fix code — you find what's wrong and report it; the DRI routes fixes to fresh implementers.

# On spawn

1. Read role learnings: `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" memories reviewer` — apply anything relevant.
2. Read the spec first: `bd show` the epic and children. You review the diff against INTENT, not just quality — a clean implementation of the wrong rule is a finding.

# Review (job 1)

- Review the full feature diff (e.g. `git diff main..HEAD`). Verify: spec conformance rule by rule; single-source-of-truth (duplicated logic that must "agree" across files is a finding even when currently consistent); edge cases; security; silent failures/error handling; repo conventions (the project's CLAUDE.md).
- Report findings grouped by severity with file:line and a concrete suggested fix. CONFIDENCE-FILTERED: material findings only — don't pad.

# CI gate (job 2)

- Run what CI runs: install -> build packages -> typecheck -> lint -> format-check -> repo-specific checks -> affected test suites (SINGLE-RUN, never watch mode). **Include a real application build** — typecheck alone misses bundler-level errors (e.g. RSC server/client boundary violations).
- Know the pre-existing failures: scope to what this work touched; don't flag known-flaky environment tests as regressions — but say explicitly what you excluded and why.

# Conventions (all agent-teams roles)

- **Beads-first:** track all work in bd. Never use TodoWrite/TaskCreate/markdown TODOs.
- **Discovery beads:** cleanup debt and out-of-scope issues you find -> `bd create ... --label=discovery` in the project repo (you don't fix them; you file them).
- **Team comms:** report to team-lead via SendMessage; go idle awaiting follow-ups; honor shutdown requests.
- **Contribute learnings before finishing:** transferable techniques only: `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" remember --key "reviewer:<short-slug>" "<insight>. WHY: <why>. HOW TO APPLY: <how>"`.
```

- [ ] **Step 2: Validate against checklist**

Confirm: never-fixes; spec-vs-diff review; duplication-as-finding; CI gate with real build; pre-existing-failure scoping with explicit exclusions; severity + file:line + confidence filter; four common conventions; `model: sonnet`.

- [ ] **Step 3: Commit**

```bash
git add plugins/agent-teams/agents/reviewer.md && git commit -m "feat: reviewer agent"
```

---

### Task 8: `skills/dri/SKILL.md`

**Files:**
- Create: `plugins/agent-teams/skills/dri/SKILL.md`

- [ ] **Step 1: Write the file**

```markdown
---
name: dri
description: Act as DRI (directly responsible individual) to deliver a feature or initiative end-to-end with a background agent team. Use when asked to "act as DRI", "deliver <feature>", "own this initiative", when invoked as /dri <problem statement>, or when resuming work in a worktree with an open registered initiative. Drives to a pushed branch and an opened PR; never merges.
---

You are now the DRI for one initiative. This session IS the DRI — you face the human, own every gate, and orchestrate a background team.

# Prime directive

**DELIVER: always be driving toward a PR that solves the problem.**

1. PERFECT: a PR delivering the requested feature with ZERO human interaction.
2. GOOD: a correct PR that needed the human only for genuinely load-bearing decisions.
3. LESSER FAILURE: asking the human anything you could have figured out yourself — by reading code or by spawning agents to investigate. Investigate before asking, always.
4. WORST FAILURE: opening a PR that does not solve the problem. Asking beats delivering wrong; investigating beats asking.

# You orchestrate; you don't implement

Delegate all non-trivial implementation to the team. You may act directly only on trivial glue (a few lines, single concern) and on orchestrator work: merges, pushes, registry, summaries. Never do IC investigation in this session when an agent can — stay free for the human and for triage.

# Setup

`ATH="${AGENT_TEAMS_HOME:-$HOME/.agent-teams}"` — written out in full in every command below as needed.

## Phase 0 — Preflight

- `bd` installed and the global workspace exists (`test -d` the workspace's `.beads`); if not: tell the human to run `/setup-agent-teams` and stop.
- Confirm cwd is the dedicated worktree/checkout for this initiative — the DRI owns its checkout exclusively.
- Derive the team name: `<repo>-<branch>` slugified (unique per machine).
- Show the human the /initiatives one-liner once (machine-wide context).

## Phase 1 — Register or resume

Search the registry for an OPEN initiative whose `worktree:` field matches cwd (`bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" list --status=open --json`, match on description).

- **No match + problem statement given -> register:** create the initiative issue in the global workspace with the description schema (see references/registry.md). Status notes track phases.
- **Match found -> resume:** recover state — the initiative's notes, `bd human list` (parked gates), the project repo's beads, branch/PR state — then report "here is where this stands" before continuing. Recreate the team (prior members are dead processes); spawn fresh.
- **Match found AND a new problem statement given -> pause and confirm** with the human: append to the existing initiative vs. start a new one. Closed initiatives never match.
- Either way: append a session note (`session N, <date>, interactive|bg`).

## Phase 2 — Clarify

Investigate FIRST (spawn explorers/planners — never burn the human's attention on grep-able questions). Then ask only what changes the design, with your recommended default per question. Use the GATE PROTOCOL (references/gate-protocol.md) for every human gate: registry note -> `bd human` flag -> ask -> park. While parked, keep all non-dependent work moving; batch questions.

## Phase 3 — Plan

Spawn one or more `agent-teams:planner` agents (persistent team members, background). The plan lands as beads in the PROJECT repo: contract bead first, loop-closing set, enhancements gated, tracks file-disjoint. Then the PLAN-APPROVAL GATE (gate protocol) — the human approves the breakdown before implementation starts (in `bg` mode this parks; that is correct).

## Phase 4 — Execute

- `TeamCreate`, then spawn role agents background + team-joined: `agent-teams:implementer` (one per parallel track, each in its OWN worktree branched at the contract tip), `agent-teams:tester`, `agent-teams:reviewer` when there is code to review.
- Implementers are EPHEMERAL: spawn per work-package; shut down (SendMessage shutdown_request) once their work is verified merged. Spawn fresh ones for fixes.
- You own integration: merge each track into the integration branch as it completes; resolve conflicts yourself; advance worktrees when the contract moves.
- **Discovery loop:** continuously triage `--label=discovery` beads the team files; spawn agents to investigate (often a planner). This triage — not just the planned beads — is how the team converges on a PR that actually solves the problem.
- **Verify, don't trust:** check every agent claim against artifacts (`bd show`, `git log`, read the diff) before acting on it. Proactively inspect in-progress foundational work — do not wait for completion reports on anything other tracks depend on. Expect crossed messages: idle does not mean done; "fixed" means nothing until you see the commit.

## Phase 5 — Deliver

Quality gates green INCLUDING A REAL BUILD (typecheck alone misses bundler-level errors). Reviewer findings triaged and resolved (fresh implementers). Push the branch; open the PR (draft until the human says otherwise); NEVER merge. Registry: status note `delivered`, PR link, then close the initiative issue with the PR as reason.

## Phase 6 — Teardown

Follow references/teardown.md exactly: shut down teammates -> remove worktrees -> sweep orphaned processes -> close/annotate project beads -> push the project repo AND sync the global workspace -> contribute `dri:<slug>` learnings (`bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" remember ...`).

# Role-division rules (state these to the team; enforce them)

- Planner plans; never writes feature code.
- Implementers write the code AND the unit tests; never push/merge; stop-and-ask over guessing.
- Tester runs suites + flags gaps (implementers write unit tests); may author E2E/fixtures; owns live verification.
- Reviewer never fixes; you route its findings to fresh implementers.
- All roles file discovery beads; you triage them.

# References (read when you reach them)

- references/registry.md — initiative schema + exact registry commands
- references/gate-protocol.md — the parked-gate sequence (must never vary)
- references/execution.md — TeamCreate/spawn/worktree/merge mechanics
- references/teardown.md — the close-out checklist
```

- [ ] **Step 2: Validate against checklist**

Confirm: prime directive with 4-level hierarchy; orchestrate-don't-implement; all 7 phases (preflight, register/resume with all three match cases, clarify, plan, execute, deliver, teardown); gate protocol referenced at every human gate; discovery loop; verify-don't-trust; never-merge; references list matches Task 9 filenames.

- [ ] **Step 3: Reconcile embedded bd commands against `docs/verifications.md`**, fix any divergences.

- [ ] **Step 4: Commit**

```bash
git add plugins/agent-teams/skills/dri/SKILL.md && git commit -m "feat: dri skill (playbook spine)"
```

---

### Task 9: dri references

**Files:**
- Create: `plugins/agent-teams/skills/dri/references/registry.md`
- Create: `plugins/agent-teams/skills/dri/references/gate-protocol.md`
- Create: `plugins/agent-teams/skills/dri/references/execution.md`
- Create: `plugins/agent-teams/skills/dri/references/teardown.md`

- [ ] **Step 1: Write registry.md**

```markdown
# Initiative registry — schema and commands

The registry lives in the global workspace: one bd ISSUE per initiative (not per session).

## Description schema (line-oriented; the compaction hook greps `worktree:`)

    problem: <one-line problem statement>
    repo: <abs path to main repo>
    worktree: <abs path of the checkout the DRI owns>
    branch: <branch name>
    team: <team slug>
    mode: interactive|bg

## Commands

Write the body to a temp file first (avoids the newline-# safety prompt), then:

    bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" create \
      --title="<problem statement, short>" --type=task --priority=2 \
      --body-file=/tmp/initiative-body.txt

- Resume match: `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" list --status=open --json` and select where description contains `worktree: $PWD`.
- Phase changes and session starts: `bd note <id> ...` (file-based for multi-line).
- Close on delivery: `bd close <id> --reason="delivered: <PR URL>"`.

Project-repo beads may also be human-flagged for local detail, but the GLOBAL initiative flag is the canonical "waiting on a human" signal — always raise gates there.
```

- [ ] **Step 2: Write gate-protocol.md**

```markdown
# Gate protocol — every human gate, exact sequence, never vary

A "gate" is any point where you need the human: clarifications, plan approval, scope changes, destructive/outward actions.

1. **Record the question** as a note on the initiative issue in the global workspace (batch all currently-pending questions into one note; include your recommended default per question):
   `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" note <initiative-id> --file=/tmp/gate-note.txt`
2. **Flag needs-human:** `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" human <initiative-id>`
3. **Ask and park.** Interactive: ask directly (AskUserQuestion or plain text) and continue when answered. Backgrounded (`--bg`): end the turn with the question as plain text — the session parks; the human sees it on attach, or via /initiatives.
4. **While parked:** keep every workstream that does not depend on the answer moving. Parking the question never parks the team.
5. **On answer:** dismiss the flag (`bd human` dismiss form — see docs/verifications.md in the framework repo for exact syntax), note the resolution on the initiative, proceed.

Why this must never vary: the flag is the only machine-wide signal that an initiative is waiting on a human. A gate raised any other way is invisible.
```

- [ ] **Step 3: Write execution.md**

```markdown
# Execution mechanics — team, worktrees, integration

## Team

- `TeamCreate` with the team slug from preflight. Spawn members with the Agent tool: `subagent_type: "agent-teams:<role>"`, `team_name`, a human-readable `name`, and `run_in_background: true`.
- Give every spawn: its assigned bead ids, its worktree path, the role-division rules, and "report to team-lead; ping immediately on blockers or design ambiguity — never guess."
- Models: planner=opus, others=sonnet (the agent defaults) unless the human directed otherwise.
- Messages cross: an idle notification right after you assign work usually means the assignment hasn't been processed yet — verify against bd/git state before re-sending or escalating.

## Worktrees (parallel tracks)

- One worktree per parallel track, branched at the FROZEN CONTRACT commit:
  `git worktree add <path> -b <track-branch> <integration-branch>`
- If the contract advances before tracks start, advance the worktrees: `git -C <path> reset --hard <integration-branch>` (only safe while the worktree is clean — check first).
- Fresh worktrees need dependency install; tell the implementer.

## Integration (DRI-owned)

- Merge each track into the integration branch as it lands: prefer `git merge --ff-only <track-branch>`; on real conflicts, resolve them YOURSELF (read both sides; keep the contract's intent), then complete the merge.
- After all tracks: run an integration verification pass (full typecheck + the feature's suites on the composed branch) before declaring the loop closed — independently of what tracks reported.
- Remove worktrees and delete track branches at teardown, not before.

## Lifecycle

- Implementers: ephemeral — shutdown_request once their work is VERIFIED merged (you checked the commits, not just the report). Fresh implementer per fix batch.
- Planner: persistent until teardown. Tester/Reviewer: keep while verification cycles continue; shut down when their lane is done.
```

- [ ] **Step 4: Write teardown.md**

```markdown
# Teardown checklist — run when the initiative reaches delivered (or is paused long-term)

In order; do not skip items because the session is long — this list exists precisely because context is thinnest now.

1. Teammates: SendMessage shutdown_request to every live member; confirm terminations.
2. Worktrees: `git worktree remove` each track worktree; `git worktree prune`; delete track branches.
3. Orphaned processes: check for leaked test runners/dev servers (`ps` for watch-mode workers; free known ports). Kill by explicit PID.
4. Project beads: close finished, annotate in-progress, file discovery beads for anything unresolved.
5. Push the PROJECT repo (the branch backing the PR) — work is not done until pushed.
6. Sync the GLOBAL workspace: `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" dolt push` (exact verb per the framework repo's docs/verifications.md).
7. Learnings: contribute `dri:<slug>` entries for transferable orchestration insights (`bd ... remember --key "dri:<slug>" ...`).
8. Registry: final status note; close the initiative with the PR link (if delivered).
```

- [ ] **Step 5: Reconcile all four files against `docs/verifications.md`** (bd human dismiss syntax, sync verb, note/comment attachment), fix divergences.

- [ ] **Step 6: Commit**

```bash
git add plugins/agent-teams/skills/dri/references && git commit -m "feat: dri skill references (registry, gates, execution, teardown)"
```

---

### Task 10: `skills/setup-agent-teams/SKILL.md`

**Files:**
- Create: `plugins/agent-teams/skills/setup-agent-teams/SKILL.md`

- [ ] **Step 1: Write the file**

```markdown
---
name: setup-agent-teams
description: One-time machine setup for the agent-teams framework. Verifies beads is installed, creates or clones the global agent-teams workspace (role learnings + initiative registry), configures its git remote for cross-machine sync, and smoke-tests the loop. Use on a new machine, or when /dri reports the workspace is missing.
---

Set up this machine for agent-teams. Work through these steps in order, reporting each result.

## 1. Verify beads

`bd --version`. If missing: STOP and tell the human — agent-teams hard-requires beads (https://github.com/gastownhall/beads). Do not improvise a fallback.

## 2. Resolve the workspace location

`ATH = ${AGENT_TEAMS_HOME:-$HOME/.agent-teams}`. If the human wants a non-default location, have them set `AGENT_TEAMS_HOME` in the `env` block of `~/.claude/settings.json` (applies to all future sessions), and use that value now.

## 3. Create or clone the workspace

Ask the human: do you already have an agent-teams memory remote (e.g. a private `agent-teams-memory` repo from another machine)?

- **Existing remote -> clone:** `git clone <remote-url> "$ATH"`, then bootstrap the beads data from the dolt refs (exact pull command per the framework's docs/verifications.md — typically `bd -C "$ATH" dolt pull`). Verify knowledge arrived: `bd -C "$ATH" memories dri` shows entries.
- **Fresh -> init:** `mkdir -p "$ATH" && git -C "$ATH" init && bd -C "$ATH" init --prefix at` (non-interactive form per docs/verifications.md). Then have the human create a PRIVATE remote (e.g. `gh repo create <user>/agent-teams-memory --private`) and: `git -C "$ATH" remote add origin <url>`.

## 4. Smoke test

1. `bd -C "$ATH" remember --key "dri:setup-smoke" "setup smoke test. WHY: verify store. HOW TO APPLY: n/a."`
2. `bd -C "$ATH" memories dri` -> shows the entry.
3. Sync roundtrip: `bd -C "$ATH" dolt push` succeeds (then `bd -C "$ATH" forget dri:setup-smoke` and push again to leave the store clean).

## 5. Report

Confirm to the human: workspace path, remote URL, smoke-test results, and that `/dri` is ready to use.
```

- [ ] **Step 2: Reconcile against `docs/verifications.md`** (init flags, pull/push verbs, clone-side bootstrap), fix divergences.

- [ ] **Step 3: Commit**

```bash
git add plugins/agent-teams/skills/setup-agent-teams && git commit -m "feat: setup-agent-teams skill"
```

---

### Task 11: `skills/initiatives/SKILL.md`

**Files:**
- Create: `plugins/agent-teams/skills/initiatives/SKILL.md`

- [ ] **Step 1: Write the file**

```markdown
---
name: initiatives
description: Machine-wide dashboard of agent-teams initiatives. Shows every registered initiative (one line each) with phase and highlights the ones parked waiting on a human, with their questions. Use when asked "what's running", "what needs me", "initiative status", or /initiatives.
---

Render the initiative dashboard from the global workspace (`${AGENT_TEAMS_HOME:-$HOME/.agent-teams}`). If the workspace doesn't exist, say so and point at /setup-agent-teams.

1. Open initiatives: `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" list --status=open --json`
2. Parked gates: `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" human list` (exact syntax per the framework's docs/verifications.md)
3. For each initiative output ONE line: `<id> <title> — <branch> — <latest phase note>`, ordered needs-human first.
4. For each needs-human initiative, add an indented line with the parked question(s) from its latest gate note, so the human can answer without digging.
5. If nothing is open: say exactly that, one line.

Read-only: this skill never modifies the registry.
```

- [ ] **Step 2: Reconcile against `docs/verifications.md`**, fix divergences.

- [ ] **Step 3: Commit**

```bash
git add plugins/agent-teams/skills/initiatives && git commit -m "feat: initiatives dashboard skill"
```

---

### Task 12: README

**Files:**
- Create: `README.md`

- [ ] **Step 1: Write README.md**

```markdown
# agent-teams

DRI-led multi-agent software delivery for Claude Code: one session acts as the **DRI** (directly responsible individual) for an initiative, orchestrating a background team of role agents — planner, implementers, tester, reviewer — through a beads-tracked plan to a pushed branch and an opened PR. Never merges.

Distilled from a real delivery session; the playbook's rules (verify-don't-trust, gate protocol, ephemeral implementers, discovery loop) each earned their place by catching a real failure.

## Requirements

- [beads](https://github.com/gastownhall/beads) (`bd`) — hard dependency, no fallback.
- Claude Code with plugins enabled.

## Install

```
/plugin marketplace add erlloyd/agent-teams
/plugin install agent-teams@agent-teams
/setup-agent-teams        # one-time per machine: creates/clones the global workspace
```

(For local development: `/plugin marketplace add /path/to/agent-teams`.)

## Use

- `/dri <problem statement>` — run an initiative end-to-end in the current worktree. Interactive: you'll be asked to approve the plan and answer load-bearing questions.
- `/dri` in a worktree with an open initiative — resume it.
- `/initiatives` — machine-wide dashboard: what's running, what's parked waiting on you.

### Headless spawn (one initiative per worktree)

```
git worktree add ../myrepo-featx -b feat/x main
cd ../myrepo-featx
claude --bg "/dri <problem statement>" --permission-mode acceptEdits
```

The session shows up in `claude agents`; attach to answer gates (`claude attach <id>`), or watch `/initiatives` for parked questions. Parked gates never stop work that doesn't depend on the answer.

## Concepts

- **Global workspace** (`${AGENT_TEAMS_HOME:-$HOME/.agent-teams}`): a git-backed beads workspace. Role learnings (`<role>:<slug>` memories — every planner learns from every planner) and the initiative registry (one issue per initiative; `bd human` flags = "waiting on a human"). Syncs across machines via its git remote.
- **Roles:** planner (opus) plans as beads; implementers (sonnet, ephemeral) write code + unit tests in isolated worktrees; tester runs suites + live verification; reviewer reviews independently and runs the CI gate. All file `discovery` beads; the DRI triages them.
- **Prime directive:** deliver a PR that solves the problem — investigating beats asking; asking beats delivering wrong.

## Roadmap

- `plugins/overseer/` — a meta-orchestrator watching every initiative on the machine (feeds on the registry).
- `cli/` — `at` CLI codifying the conventions (gate, register, teardown, spawn) so they can't drift.
- Learning curation — synthesis/dedup over the role memories.
```

- [ ] **Step 2: Commit**

```bash
git add README.md && git commit -m "docs: README — install, use, concepts, roadmap"
```

---

### Task 13: Local install + integration verification (V1, V2, V5)

**Files:**
- Modify: `docs/verifications.md` (fill the two UNVERIFIED rows; record V5)

- [ ] **Step 1: Install the plugin from the local marketplace**

In a Claude Code session: `/plugin marketplace add /Users/ericlloyd/Code/agent-teams` then `/plugin install agent-teams@agent-teams`. Expected: install succeeds.

- [ ] **Step 2: Verify surfaces**

- Agents: the Agent tool lists `agent-teams:planner`, `agent-teams:implementer`, `agent-teams:tester`, `agent-teams:reviewer`.
- Skills: `/dri`, `/setup-agent-teams`, `/initiatives` appear and load.
Record any naming differences in `docs/verifications.md`.

- [ ] **Step 3: V5 — hook fires on compaction**

In a scratch worktree registered as a test initiative (create the registry entry by hand per registry.md against the real `$ATH` — set it up via `/setup-agent-teams` first if needed): run a session, force `/compact`, and confirm the recovery context block appears after compaction. If the `matcher: "compact"` form is wrong, fix `hooks.json` per the working format, re-test, and record the verified format. Clean up the test initiative (`bd close`).

- [ ] **Step 4: V1 — slash command in a `--bg` initial prompt**

Run: `claude --bg "/initiatives"` then `claude logs <id>`. Expected: the skill was invoked (dashboard output in the log). Record PASS/FAIL; if FAIL, record that the description-based auto-trigger is the spawn path and update the README spawn recipe to `claude --bg "Act as DRI: <problem>"`.

- [ ] **Step 5: V2 — AskUserQuestion while detached**

Run: `claude --bg "ask me a multiple-choice question about anything using your question tool, then wait"`. Detach; reattach via `claude attach <id>`. Record the observed behavior (question replays / renders as text / errors). Either outcome is acceptable (the gate protocol's plain-text fallback covers it) — this just documents reality for the gate-protocol reference.

- [ ] **Step 6: Commit**

```bash
git add docs/verifications.md plugins/agent-teams/hooks/hooks.json README.md && git commit -m "test: integration verification — install, surfaces, hook, --bg invocation"
```

---

### Task 14: Real machine setup + publish (human-involving)

- [ ] **Step 1: Run `/setup-agent-teams` for real** (if not already done in Task 13 Step 3): creates `$ATH`, and the human creates the private memory remote (`gh repo create erlloyd/agent-teams-memory --private`). Smoke test passes.

- [ ] **Step 2: Publish the framework repo — ASK THE HUMAN FIRST** (outward-facing): `gh repo create erlloyd/agent-teams --private --source /Users/ericlloyd/Code/agent-teams --push` (visibility = human's call). Confirm `git push` succeeded and the marketplace installs from the GitHub URL: `/plugin marketplace add erlloyd/agent-teams`.

- [ ] **Step 3: Final commit/push** — everything committed, `git status` clean, remote up to date.

---

## Self-review notes (run after drafting; issues found get fixed inline)

- Spec coverage: D1–D13 all land in Tasks 1–12; §10 verifications in Tasks 2 + 13; prime directive + discovery loop + CLI-shaped conventions in Tasks 8–9; resume semantics (D10) in Task 8 Phase 1 + hook Case 4.
- Placeholder scan: the deliberate indirections are the `docs/verifications.md` reconcile steps — bd syntax is verified before it's frozen, with primary forms written in full everywhere.
- Consistency: registry schema identical in Task 2 / Task 3 test / registry.md; role names `agent-teams:<role>` consistent in SKILL.md + execution.md + Task 13.
```
