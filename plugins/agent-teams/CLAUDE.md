# agent-teams

This plugin hard-requires **beads** (`bd`) — all work tracking is beads-first. Never use TodoWrite/TaskCreate/markdown TODO lists in agent-teams workflows.

**Global workspace:** `~/.agent-teams` — a git-backed beads workspace holding role learnings and the initiative registry (one bd issue per initiative). Access is via `ateam`, which ships as prebuilt per-platform binaries in the plugin `bin/` (auto-added to PATH by Claude Code); `bin/ateam` is the POSIX dispatch wrapper that selects the right binary for the current platform. Skills call bare `ateam`; the single allowlist entry is `Bash(ateam:*)`. If the workspace does not exist or `ateam` is not found, run `/setup-agent-teams`.

**DEV:** after editing `cmd/ateam`, regenerate the binaries with `scripts/build-binaries.sh` and commit `plugins/agent-teams/bin/`.

**DEV — bump the version on EVERY plugin-content change.** Any change to plugin contents (skills, agents, hooks, commands, `.mcp.json`, binaries) MUST bump the version in BOTH `plugins/agent-teams/.claude-plugin/plugin.json` and `.claude-plugin/marketplace.json` (keep them identical). `claude plugin update` keys off the version: if it doesn't change, installed sessions keep the cached old copy and silently never pick up your change — the edit looks merged but is dead. No bump = not shipped.

## 🚨 CARDINAL RULE — two beads databases, NEVER confuse them

There are **two separate beads databases**, and putting the wrong beads in the wrong one is a serious, recurring error:

1. **The PROJECT repo's `.beads`** — holds **ALL work beads**: the planner's decomposition, contract beads, feature/task beads, `--label=discovery` beads, test and review beads. This is where every agent's `bd create` lands, because every agent's cwd is inside the project worktree.
2. **The GLOBAL workspace (`~/.agent-teams`)** — holds **ONLY** two things: the **initiative-tracking beads** (one per initiative, created exclusively by `ateam register`) and **role memories** (via `ateam learn` — see Memory routing below). Nothing else. Ever.

**The rules, non-negotiable:**

- **NEVER** create a feature/work/plan/discovery bead in the global workspace. Work beads live in the project repo, full stop.
- **NEVER** touch the global workspace with a raw `bd -C ~/.agent-teams …` command. The **only** sanctioned interface is the `ateam` script. `ateam` deliberately exposes **no generic issue-create verb** — `register` (initiative-tracking schema) is the only thing that writes an issue there, and that is by design. If you reach for `bd -C <global> create`, you are about to make the mistake this rule exists to prevent.
- Plain `bd create` (no `-C`) is correct for project work — it targets the project repo because that is your cwd. Keep it that way; do not redirect it at the global workspace.
- **Audit:** `ateam audit` lists any issue in the global workspace that lacks the tracking schema (a leaked work bead) and exits non-zero. `/initiatives` and DRI wind-down run it; the workspace must always audit clean.

**Beads runtime:** embedded mode (no server daemon needed). Agent isolation uses git **worktrees** of the project repo, not independent clones — worktrees share the project's single `.beads/` issue DB via git-common-dir discovery; clones each get a separate, fragmented beads workspace.

**Skills:** `/dri <problem>` — run/resume an initiative as DRI. `/dri-dispatch <problem>` — register a new initiative and hand it to a hands-off background DRI. `/initiatives` — machine-wide initiative dashboard. `/setup-agent-teams` — one-time machine setup.

## Debugging hooks, watchers & messaging

**Read this before reading the hook scripts** — it captures what they do and how to diagnose them from logs alone.

**Hooks & cwd.** `plugins/agent-teams/hooks/hooks.json` wires per-event scripts: SessionStart → `session-start-pull.sh` (`ateam pull`), `prime-user-memories.sh` (`ateam prime`), `session-start-inbox.sh` (drain mail); UserPromptSubmit → `inbox-drain.sh`; SubagentStart → `subagent-prime-learnings.sh`; compact → `compact-recovery.sh`; **Stop → `wake-watcher.sh`** (`async`, `asyncRewake`, 24h timeout). The harness runs each hook by spawning `/bin/sh` with the child **cwd set to the session's worktree**.

**The debug log.** Every hook writes lifecycle events to `~/.agent-teams/debug/hooks.log` (via `lib/hook-debug-log.sh`), 6 TAB-separated columns:
```
<iso8601-utc>  <session_id>  <script>  <initiative_id>  <event>  <detail>
```
- `start` — logged before any guard, so it appears even if `bd`/`jq` are missing.
- `exit` — `detail="code=<n> reason=<why>"`; covers every exit incl. `set -e` failures (default reason `unexpected`).
- `signal` — `detail=TERM|HUP|INT` (the process was asked to stop).
- `note` — mid-run markers: pidfile claim/takeover, doorbell-seen, `alive elapsed=Ns`.

Auto-rotates to `hooks.log.1` at ~5 MB. Tail it: `tail -f ~/.agent-teams/debug/hooks.log`.

**Reading the log — diagnostic signatures:**
- **`start` with NO later `exit`/`signal`** for that session → the hook was hard-killed (SIGKILL / async-child reap). This is the signature of the watcher-reaping failure.
- **`exit reason=<x>`** → exited on its own; the reason names the path. `wake-watcher` reasons: `missing-deps`, `no-open-match`, `superseded`, `doorbell-fired`, `heartbeat-rearm`, `initiative-closed`.
- **`signal=TERM`** → graceful kill (e.g. the singleton handoff killing a prior watcher).
- **`wake-watcher` `alive elapsed=Ns` ticks** → how long the poll-loop survived; an abrupt stop with no exit pinpoints when it was reaped.

**Wake/messaging mechanism.** `ateam send <id>` creates a `type=message` bead (assignee=`<id>`) in the global workspace **and** touches a doorbell `~/.agent-teams/mailbox/<id>.wake`. A live `wake-watcher.sh` poll-loop (one per initiative, singleton-guarded by `mailbox/<id>.watcher.pid`) checks the doorbell every 1s and `exit 2`s to wake the session, which drains mail via `ateam inbox`. **Mail is beads, not files** — reading is bead-driven (`inbox-drain.sh`/`session-start-inbox.sh`); the doorbell only controls *waking*.

**Check watcher health:** `ateam watchers` — per-initiative state. `MISSING-WATCHER` = no pidfile/poll-loop; `STALE-PIDFILE` = pidfile names a dead pid. A live session with `MISSING-WATCHER` has a **dead doorbell** (nothing is polling it).

**Known gotchas:**
- **`ENOENT … posix_spawn '/bin/sh'`** on any hook = the session's cwd (its git worktree) was **deleted while the session is still alive**. The harness then can't spawn the shell, so *no* hook for that session runs — it is NOT a bug in the script. Remedy: don't delete a live session's worktree; reap orphans (`claude agents --json --all` → `claude stop <id>` for entries whose `cwd` no longer exists).
- **`ateam send` reports session-liveness, not watcher-liveness** — its "doorbell will wake it" can be false when the session is alive but `ateam watchers` shows `MISSING-WATCHER`.

## Memory routing

**MEMORY ROUTING (agent-teams).** Ignore the harness's built-in file-based memory feature here: do NOT write MEMORY.md or any file under a Claude memory/ directory (e.g. `~/.claude/projects/*/memory/`). Persistent memory routes by kind:

- Role/process learnings (transferable across repos) → `ateam learn <role> <slug> --file <tmpfile>`, where `<role>` is `dri | planner | implementer | tester | reviewer`. This is an UPSERT-by-key: writing the same `<slug>` again overwrites the previous body. **A bare `<slug>` (no prefix) defaults to the fresh tier** (`role:fresh:<slug>`); use `hot:<slug>` or `cold:<slug>` to target those tiers explicitly. See the three-tier model below.
- User/cross-project preferences & feedback → `ateam learn user <slug> --file <tmpfile>`.
- Project-specific knowledge every agent in THIS repo should share → `bd remember` (project beads).

Default to `ateam learn`. Use `bd remember` only for repo-shared project facts. Never MEMORY.md.

### Three-tier memory model (fresh / hot / cold)

Role memories use a three-tier key convention — the tier is encoded in the key, not in metadata:

- **Fresh:** `<role>:fresh:<slug>` — the default write tier. `ateam learn <role> <slug> --file <f>` (bare slug, no prefix) writes here automatically. Fresh memories accumulate between condense runs; `ateam learnings <role>` serves them alongside hot. Fresh is the "just written, not yet curated" tier and is periodically drained into cold by `ateam fresh-drain <role>`.
- **Hot:** `<role>:hot:<slug>` — curated, auto-injected into every session via `ateam learnings <role>`. Write explicitly with `ateam learn <role> hot:<slug> --file <f>`. Hot bodies are deliberately succinct; target budget is ~6000 tokens (~15–25 learnings) across all hot keys for a role.
- **Cold:** `<role>:<slug>` — searchable on demand, NOT auto-injected. Write explicitly with `ateam learn <role> cold:<slug> --file <f>` (the `cold:` prefix is stripped to produce the bare `role:<slug>` key). The existing pre-tier `dri:<slug>` memories are already cold with no migration needed.

`ateam learnings <role>` serves the **hot ∪ fresh** union. It falls back to all `role:` keys only when BOTH hot and fresh are empty (preserving pre-tier behavior for roles with no curated set). All three tiers are living; cold is not a frozen archive.

**Key conventions:**
- `ateam learn <role> <slug>` → writes `role:fresh:<slug>` (default)
- `ateam learn <role> hot:<slug>` → writes `role:hot:<slug>`
- `ateam learn <role> cold:<slug>` → writes `role:<slug>` (bare cold key, no tier tag)

**Searching cold memories:** `ateam recall <role> <query>` does a substring search over a role's memories (key+body) and prints matching key+body pairs on demand.

**Removing a memory:** `ateam forget <role> <slug>` removes a cold memory. `ateam forget <role> hot:<slug>` removes a hot memory. `ateam forget <role> fresh:<slug>` removes a fresh memory. Every removal is recoverable from Dolt history (`refs/dolt/data`).

**Promoting a learning to hot:** write it with `ateam learn <role> hot:<slug> --file <tmpfile>`. Keep the body succinct — hot memories are injected whole every session.

### Condensing (autonomous)

Condensing is **lock-guarded** via `ateam condense-lock`. Use the `/agent-teams:condense` skill (no arg = all roles; `<role>` arg = single role) — do not call `ateam condense <role>` directly. The skill acquires the lock, skips cleanly if another session holds it, drains fresh into cold first (`ateam fresh-drain <role>`, deterministic, no LLM), then emits the condense packet (`ateam condense <role>`) for agent curation, and releases the lock on all exit paths.

The condense agent applies changes directly via `ateam learn` and `ateam forget` — cold writes use `ateam learn <role> cold:<slug> --file <f>` (since bare `ateam learn` now writes fresh). There is NO human-review gate and NO staged diff — the agent acts autonomously.

New verbs introduced by the three-tier model:
- `ateam fresh-drain <role>` — deterministic drain of `role:fresh:*` into cold (no LLM).
- `ateam condense-lock acquire` / `ateam condense-lock release` — advisory lock for condense serialization.

Safety backstops:
- **Dolt history** — every write, including eviction, is recoverable via `refs/dolt/data`. A bad run is revertible.
- **Change-summary log** — the condense agent emits one line per run: `promoted N / merged M / evicted K / hot now X tokens`.

v1 has no per-run eviction floor — trust the agent and Dolt-history recoverability.
