# Agent-Teams v1 — Test Plan (fresh sessions)

Validate the full v1 flow end-to-end, **fully isolated from your real agent memory**
(`~/.agent-teams`) and its private remote. Every workspace write during testing goes to a
throwaway location; the reset deletes the throwaways and verifies your real memory is untouched.

## Isolation model (why this is safe)

All workspace access goes through the `ateam` script, which resolves the workspace path from
`AGENT_TEAMS_HOME` (default `~/.agent-teams`). Set `AGENT_TEAMS_HOME` to a throwaway path and
**every learning, initiative, and sync lands there — never in your real workspace.** Belt-and-
suspenders: we back up the real workspace first and verify it's byte-unchanged at the end.

`ateam` absolute path (this machine):
`/Users/ericlloyd/Code/agent-teams/plugins/agent-teams/scripts/ateam`

Throwaway locations used below:
- workspace: `/tmp/at-test-workspace`
- workspace remote (local bare, so `sync` has a safe target): `/tmp/at-test-remote.git`
- project repo the DRI will work in: `/tmp/at-test-project`
- real-workspace backup: `/tmp/at-REAL-backup`

---

## Part 0 — One-time setup (do first)

**0.1 Back up the real workspace (insurance) and record its baseline:**
```bash
cp -r ~/.agent-teams /tmp/at-REAL-backup
# baseline (note what's really there — expect near-empty):
/Users/ericlloyd/Code/agent-teams/plugins/agent-teams/scripts/ateam learnings dri
/Users/ericlloyd/Code/agent-teams/plugins/agent-teams/scripts/ateam list
```

**0.2 Create the throwaway workspace remote** (a local bare repo — `ateam sync` will push here, never to GitHub):
```bash
git init --bare /tmp/at-test-remote.git
```

**0.3 Create a throwaway PROJECT repo with a tiny, self-contained feature target.**
NOT midgard, NOT any prod-connected repo — under bypass, teammates run commands unprompted and
must not touch prod MCP tooling. Zero-dependency Node (`node --test`, no npm install):
```bash
mkdir -p /tmp/at-test-project/src /tmp/at-test-project/test
cd /tmp/at-test-project
git init -q
printf '{\n  "name": "at-test-project",\n  "type": "module",\n  "scripts": { "test": "node --test" }\n}\n' > package.json
printf 'export function add(a, b) {\n  return a + b;\n}\n' > src/calc.js
printf "import { test } from 'node:test';\nimport assert from 'node:assert';\nimport { add } from '../src/calc.js';\ntest('add', () => assert.equal(add(2, 3), 5));\n" > test/calc.test.js
git add -A && git commit -qm "initial: calc with add()"
bd init --prefix tt --non-interactive --stealth   # stealth: invisible, local-only
node --test   # sanity: should pass
```
**The feature you'll hand the DRI:** *"Add a `multiply(a, b)` function to src/calc.js, with tests."*
Small, exercises plan → implement(code+tests) → test(`node --test`) → review → branch.

**0.4 In EVERY terminal you launch a test session from, FIRST run:**
```bash
export AGENT_TEAMS_HOME=/tmp/at-test-workspace
```

---

## Part 1 — Isolation gate (DO NOT SKIP)

In your first test session's terminal (after the export):
```bash
/Users/ericlloyd/Code/agent-teams/plugins/agent-teams/scripts/ateam ws
```
**Must print `/tmp/at-test-workspace`.** If it prints `~/.agent-teams` (or
`/Users/ericlloyd/.agent-teams`), the env var isn't set in this shell — **STOP and fix before
running anything else.**

During T3, also confirm propagation to spawned teammates: ask the DRI to have one teammate run
its `ateam ... ws` and report the path. It must be `/tmp/at-test-workspace`. (Teammates inherit
the DRI session's env; this proves it.) If a teammate reports `~/.agent-teams`, stop and tell me.

---

## Part 2 — Tests

For each: **Do**, then check **Expect**.

**T1 — setup-agent-teams (fresh path).**
Do: `/setup-agent-teams`. When asked existing-remote-or-fresh, choose **fresh**; give the remote
as `/tmp/at-test-remote.git`.
Expect: throwaway workspace initialized at `/tmp/at-test-workspace`; `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS`
check; smoke test (learn → learnings → sync) passes; `sync` pushes to the local bare remote.
Check: `git -C /tmp/at-test-workspace ls-remote origin` shows `refs/dolt/data`. Real `~/.agent-teams`
untouched (verify at the end).

**T2 — /initiatives (empty).**
Do: `/initiatives`.
Expect: a one-line "nothing open" message (the throwaway registry is empty).

**T3 — /dri full flow (interactive).** The core test.
Do: `cd /tmp/at-test-project`, then `/dri "Add a multiply(a,b) function to src/calc.js with tests"`.
Expect, in order:
- Registers an initiative (Phase 1) — `ateam list` now shows it.
- Investigates, then (Phase 2) may ask a **clarifying gate** — confirm it asks you and waits.
- Plans (Phase 3): spawns a planner; plan lands as **beads in the project repo** (`bd list` in
  `/tmp/at-test-project`); plan-approval gate asks you.
- Executes (Phase 4): spawns implementer/tester/reviewer as **background teammates with bypass**
  — confirm **no permission prompts** fire. Implementer works in a **git worktree** (not a clone).
- Delivers (Phase 5): pushes a branch. (PR creation needs a GitHub remote — the toy repo has none,
  so expect the DRI to report "branch ready, no remote for PR." That's correct; see optional T8.)
- Teardown (Phase 6): shuts down teammates, removes worktrees, writes a `dri:<slug>` learning.
Check:
- `node --test` in the delivered branch passes and `multiply` exists with tests.
- `ateam learnings dri` shows a new contribution; `ateam learnings implementer`/`tester`/`reviewer`
  may too.
- `git -C /tmp/at-test-project worktree list` showed worktrees during the run (and they're gone after teardown).
- The propagation check from Part 1 (a teammate's `ateam ws` = throwaway).

**T4 — /dri resume.**
Do: in `/tmp/at-test-project` (same checkout), run `/dri` again with no problem statement.
Expect: it **resumes** the open initiative ("here's where this stands"), does NOT double-register.

**T5 — backgrounded DRI + gate discovery (hands-off).**
Do: ```export AGENT_TEAMS_HOME=/tmp/at-test-workspace; cd /tmp/at-test-project; \
  claude --bg --dangerously-skip-permissions "/dri 'Add a subtract(a,b) function with tests'"```
Then, in a separate session (also with the export): `/initiatives`, and
`ateam human-list`.
Expect: the bg DRI runs without prompts; if it hits a gate it **parks**; `/initiatives` shows it
needs-human with the parked question; `ateam human-list` lists the initiative.
(Answer it by attaching: `claude attach <id>`.)

**T6 — worktree shares the project `.beads/`.**
Do: during T3/T5 while a worktree exists (or make one: `bd worktree create /tmp/at-test-project-wt`),
run `bd list` from inside the worktree.
Expect: the **same** issues as the main repo — no separate `bd init`, one shared DB.

**T7 — (optional) compaction recovery.**
Do: in a long-running DRI session, `/compact`.
Expect: after compaction the `bd prime` hook re-injects context and the DRI resumes coherently
(knows its initiative, phase, team).

**T8 — (optional) PR creation.**
Only if you want to test Phase 5's `gh pr create`: push the toy repo to a throwaway PRIVATE GitHub
repo (`gh repo create erlloyd/at-test-project --private --source /tmp/at-test-project --push`) before
T3, and delete it after (`gh repo delete erlloyd/at-test-project --yes`). Otherwise skip — branch-ready
is sufficient validation.

---

## Part 3 — Reset (run after testing — restores a clean machine)

**3.1 Stop all test sessions + teammates:**
```bash
claude agents --json | jq -r '.[].id // empty'   # find test session ids
# claude stop <id>   for each test session you started
```
Sweep any orphaned `node`/test processes if you started long runs.

**3.2 Remove test worktrees, then delete the throwaways:**
```bash
git -C /tmp/at-test-project worktree list   # see any leftover worktrees
# git -C /tmp/at-test-project worktree remove <path>   for each
rm -rf /tmp/at-test-workspace /tmp/at-test-project /tmp/at-test-project-wt /tmp/at-test-remote.git
```

**3.3 Unset the env and close the test terminals:**
```bash
unset AGENT_TEAMS_HOME
```

**3.4 VERIFY your real memory is untouched** (new shell, env NOT set, so `ateam` → `~/.agent-teams`):
```bash
/Users/ericlloyd/Code/agent-teams/plugins/agent-teams/scripts/ateam ws        # → /Users/ericlloyd/.agent-teams
/Users/ericlloyd/Code/agent-teams/plugins/agent-teams/scripts/ateam learnings dri
/Users/ericlloyd/Code/agent-teams/plugins/agent-teams/scripts/ateam list
```
These must match the Part 0 baseline — **no `tt-*` initiatives, no test learnings.**

**3.5 If anything leaked into the real workspace** (it shouldn't have), restore from backup:
```bash
rm -rf ~/.agent-teams && cp -r /tmp/at-REAL-backup ~/.agent-teams
```

**3.6 Once satisfied, remove the backup:**
```bash
rm -rf /tmp/at-REAL-backup
```

---

## What each test proves

| Test | Validates |
|------|-----------|
| T1 | setup skill, env-var step, fresh init, smoke, sync to remote |
| T2 | initiatives dashboard (empty state) |
| T3 | the whole DRI lifecycle, bypass spawning, worktree isolation, learnings write, gate |
| T4 | cross-invocation resume by worktree match |
| T5 | hands-off bg operation + parked-gate discoverability |
| T6 | worktrees share one `.beads/` (no fragmentation) |
| T7 | compaction recovery hook |
| T8 | PR creation (optional) |
