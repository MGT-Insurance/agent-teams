# agent-teams

This plugin hard-requires **beads** (`bd`) — all work tracking is beads-first. Never use TodoWrite/TaskCreate/markdown TODO lists in agent-teams workflows.

**Global workspace:** `~/.agent-teams` — a git-backed beads workspace holding role learnings and the initiative registry (one bd issue per initiative). Access is via `ateam`, which ships as prebuilt per-platform binaries in the plugin `bin/` (auto-added to PATH by Claude Code); `bin/ateam` is the POSIX dispatch wrapper that selects the right binary for the current platform. Skills call bare `ateam`; the single allowlist entry is `Bash(ateam:*)`. If the workspace does not exist or `ateam` is not found, run `/setup-agent-teams`. A SessionStart hook (`ensure-ateam-link.sh`) keeps `~/.local/bin/ateam` pinned to the loaded install rather than a fixed version, so a stale symlink from an older plugin install self-heals on the next session instead of shadowing the current one.

**DEV:** after editing `cmd/ateam`, regenerate the binaries with `scripts/build-binaries.sh` and commit `plugins/agent-teams/bin/`.

**DEV — bump the version on EVERY plugin-content change.** Any change to plugin contents (skills, agents, hooks, commands, `.mcp.json`, binaries) MUST bump the version in BOTH `plugins/agent-teams/.claude-plugin/plugin.json` and `.claude-plugin/marketplace.json` (keep them identical). `claude plugin update` keys off the version: if it doesn't change, installed sessions keep the cached old copy and silently never pick up your change — the edit looks merged but is dead. No bump = not shipped.

**DEV — keep SKILL.md under the post-compact replay cap.** Claude Code re-injects an invoked skill's full SKILL.md after compaction, truncating anything over the cap with a `[... skill content truncated for compaction ...]` marker — silently losing behavioral continuity mid-replay (e.g. a truncated DRI skill losing its wind-down instructions). There is also a ~25000-token aggregate cap across all replayed skills; a skill that would push past it is dropped entirely.

The cap is **exact arithmetic, not a tokenizer** (read out of the CLI bundle, 2.1.219): truncated iff `Math.round(len / 4) > 5000`, i.e. `len >= 20002`. Content mix is irrelevant — code fences and paths cost the same as prose.

**`wc -c SKILL.md` is the wrong measurement** and is wrong in three directions at once. `len` is the **rendered prompt** in **UTF-16 code units**: frontmatter is stripped (not counted), multibyte UTF-8 runes count as 1 rather than their byte width, and a `Base directory for this skill: <abs path>` line (~115 chars) is prepended. For `steward/SKILL.md` the two differ by ~700: 18385 bytes on disk renders to 17683 → 4421, comfortably under. Measure the rendered value; treat `wc -c` as a loose upper bound only.

Relocate reference-only/duplicate prose into `references/*.md`, keeping **all decision logic inline** — decision-heavy skills legitimately need most of the budget.

**ACCEPTED COST — the beads plugin double-fires `bd prime` around every compaction; there is no local lever on the *firing*.** The third-party `beads` plugin registers `bd prime` under BOTH `SessionStart` (matcher `""`, which includes `reason=compact`) and `PreCompact`, so around a compaction it runs twice. That much is NOT fixable from this repo: Claude Code has no per-hook/per-plugin disable (only a global `disableAllHooks`, which would also kill agent-teams' own hooks), and a project `.claude/settings.json` cannot override a hook contributed by another installed plugin — verified. The only path to stopping the second fire is upstream (drop/scope one hook in gastownhall/beads' `plugin.json`); do not re-investigate a local one, there isn't one.

**What IS controllable locally is what prime emits — and that, not the double-fire, is where the cost lives.** The size depends entirely on which workspace resolves. In a project repo it is modest — ~20KB measured (`bd prime | wc -c`), and it scales with that repo's own memory count, so the figure drifts upward over time: re-measure rather than trust it. That clears the 10,240-byte truncation threshold, so the `SessionStart` emission becomes a ~2KB preview + file pointer; `PreCompact` still injects whole. In the **global workspace** (`~/.agent-teams`) it is not modest — `bd prime` appends the entire memory store, measured at 392,864 bytes across 473 memories, **98.5% of the output**, and `PreCompact` injects all of it into context. One steward session paid that four times: ~1.5 MB. So the global workspace carries a `.beads/PRIME.md`, which on installed beads (v1.1.0) is a *total* override of prime's output — see initiative at-mudn. It trades away nothing, because role memories never reached sessions through `bd prime` in the first place: `ateam` serves them role-scoped via `role-recall-recovery.sh` (SessionStart `clear|compact`) and `prime-user-memories.sh` (`user:` keys), and role subagents self-fetch via `ateam learnings <role>` on spawn (`subagent-prime-learnings.sh` is sync-only — SubagentStart stdout never renders into agent context). Prime's dump is redundant on every axis.

Enabling the beads MCP server does NOT shrink prime either (Claude Code loads MCP from `~/.claude.json`, while `bd prime`'s MCP-mode heuristic reads only `~/.claude/settings.json` — disjoint mechanisms).

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

**Skills:** `/dri <problem>` — run/resume an initiative as DRI. `/dispatch-dri <problem>` — register a new initiative and hand it to a hands-off background DRI. `/initiatives` — machine-wide initiative dashboard. `/setup-agent-teams` — one-time machine setup.

## Debugging hooks, watchers & messaging

**Read this before reading the hook scripts** — it captures what they do and how to diagnose them from logs alone.

**Hooks & cwd.** `plugins/agent-teams/hooks/hooks.json` wires per-event scripts: SessionStart → `session-start-pull.sh` (`ateam pull`), `prime-user-memories.sh` (`ateam prime`), `session-start-inbox.sh` (drain mail), **and `wake-watcher.sh`** (`startup`/`resume` only — a freshly-launched or respawned session re-arms its own watcher); UserPromptSubmit → `inbox-drain.sh`; SubagentStart → `subagent-prime-learnings.sh`; compact → `compact-recovery.sh`; **Stop → `wake-watcher.sh`** (`async`, `asyncRewake`, 24h timeout). The watcher is now armed from BOTH ends of a turn — SessionStart and Stop — closing the gap where Claude's aggressive idle-process reaping kills the Stop-armed watcher and leaves a session deaf until its next Stop. The harness runs each hook by spawning `/bin/sh` with the child **cwd set to the session's worktree**.

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

**Wake/messaging mechanism.** `ateam mail send <id>` creates a `type=message` bead (assignee=`<id>`) in the global workspace **and** touches a doorbell `~/.agent-teams/mailbox/<id>.wake`. A live `wake-watcher.sh` poll-loop (one per initiative, singleton-guarded by `mailbox/<id>.watcher.pid`) checks the doorbell every 1s and `exit 2`s to wake the session — **the watcher fires without consuming the doorbell.** The doorbell is consumed only by `inbox-drain.sh`, at the START of the turn the wake produced. That makes doorbell presence unambiguous: **present = undelivered** (no turn has seen it yet), **absent = a turn saw it**. A lost rewake (e.g. the woken session's first worker attempt crashes before its next turn starts) self-heals: the doorbell is still there, so the next armed watcher fires again. **Mail is beads, not files** — reading is bead-driven (`inbox-drain.sh`/`session-start-inbox.sh`); the doorbell only controls *waking*.

**Check watcher health:** `ateam watchers` — per-initiative state. `MISSING-WATCHER` = no pidfile/poll-loop; `STALE-PIDFILE` = pidfile names a dead pid. A live session with `MISSING-WATCHER` has a **dead doorbell** (nothing is polling it).

**Known gotchas:**
- **`ENOENT … posix_spawn '/bin/sh'`** on any hook = the session's cwd (its git worktree) was **deleted while the session is still alive**. The harness then can't spawn the shell, so *no* hook for that session runs — it is NOT a bug in the script. Remedy: don't delete a live session's worktree; reap orphans (`claude agents --json --all` → `claude stop <id>` for entries whose `cwd` no longer exists).

**`ateam mail send` escalation.** `ateam mail send` queries `claude agents --all --json`, matches the recipient by symlink-normalised worktree cwd, and branches on what it finds — it no longer needs `ateam watchers`/watcher-liveness at all, since the doorbell probe below subsumes that check:

| Match state | Action |
|---|---|
| No entry at all | `ateam resume <id>` — fresh dispatch, creates the only entry |
| `status: busy` or `status: waiting` | Wait — never touch it. Respawn would interrupt an in-flight tool call or drop a pending permission/AskUserQuestion dialog; the Stop-armed watcher picks up the doorbell the instant the turn ends |
| `status: idle`, or no `pid` at all (tracked-but-dead) | Wait ~5s, then re-check the doorbell: gone means a turn already consumed it (done); still present means the recipient is deaf, so `claude respawn <shortid>` revives it **in place** — same `sessionId`, same single `claude agents` entry, full conversation preserved |

Respawn failure (e.g. `claude` not in PATH) only prints a warning — the mail is already a written bead, so a broken respawn never loses it.

## Tuning stall detection (`hung-config.json`)

The relay's stall-detection tick is tunable without a rebuild, via **one JSON object at `~/.agent-teams/hung-config.json`** (`$AGENT_TEAMS_HOME/hung-config.json`). Every key is optional; omit one and it keeps its default. Durations are Go duration strings (`"45m"`, `"2h"`, `"90s"`); `wake_attempts_before_alert` is a bare integer. All values must be positive.

```json
{
  "tick_interval":                  "20m",
  "stuck_threshold":                "2h",
  "wake_attempts_before_alert":     2,
  "workproduct_flat_threshold":     "2h",
  "workproduct_alert_threshold":    "4h",
  "dead_worktree_threshold":        "2h",
  "transcript_corroborator_window": "2h"
}
```

Those are also the defaults. What each one gates: `tick_interval` — how often the relay re-runs the scan. `stuck_threshold` — how long a live-but-idle session must sit before it counts as HUNG. `wake_attempts_before_alert` — Steward nudges tried before escalating to a direct Telegram alert. `workproduct_flat_threshold` / `workproduct_alert_threshold` — flatline durations that make the work-product path eligible to trip, and that fire the direct alert. `dead_worktree_threshold` — how long a DEAD-with-worktree initiative waits before joining the ladder. `transcript_corroborator_window` — how far back the transcript tail is read for real work turns; defaults to match the flat threshold so both are judged over the same window.

**🚨 The relay reads this ONCE, at process start.** Editing the file does nothing to a running relay — you must restart it. Confirm what a relay actually picked up from its startup stderr, which reports every resolved value:

```
hung config: tick_interval=20m0s stuck_threshold=2h0m0s wake_attempts_before_alert=2 …
```

**Per-run override:** each key also has an env var (`AGENT_TEAMS_HUNG_TICK_INTERVAL`, `AGENT_TEAMS_HUNG_STUCK_THRESHOLD`, `AGENT_TEAMS_HUNG_WAKE_ATTEMPTS_BEFORE_ALERT`, `AGENT_TEAMS_HUNG_WORKPRODUCT_FLAT_THRESHOLD`, `AGENT_TEAMS_HUNG_WORKPRODUCT_ALERT_THRESHOLD`, `AGENT_TEAMS_HUNG_DEAD_WORKTREE_THRESHOLD`, `AGENT_TEAMS_HUNG_TRANSCRIPT_CORROBORATOR_WINDOW`) which beats the file. Reach for the file for real config; the env vars are for one-off runs.

**Bad config degrades, it never crashes the relay** — which matters, because the same process routes all your messages. Malformed JSON → warning naming the file, all defaults. One bad value in otherwise-valid JSON → warning naming the key, that value defaults, the rest still apply. A missing file is normal and silent.

`ateam hung-scan` resolves the same config the same way, so it reports against the thresholds the relay is acting on.

## Memory routing

**MEMORY ROUTING (agent-teams).** Ignore the harness's built-in file-based memory feature here: do NOT write MEMORY.md or any file under a Claude memory/ directory (e.g. `~/.claude/projects/*/memory/`). Persistent memory routes by kind:

- Role/process learnings (transferable across repos) → `ateam learn <role> <slug> --file <tmpfile>`, where `<role>` is `dri | planner | implementer | tester | reviewer | investigator`. This is an UPSERT-by-key: writing the same `<slug>` again overwrites the previous body. **A bare `<slug>` (no prefix) defaults to the fresh tier** (`role:fresh:<slug>`); use `hot:<slug>` or `cold:<slug>` to target those tiers explicitly. See the three-tier model below.
- User/cross-project preferences & feedback → `ateam learn user <slug> --file <tmpfile>`.
- Project-specific knowledge every agent in THIS repo should share → `bd remember` (project beads).
- Durable, human-authored instruction for a role, on THIS MACHINE only → a file at `$AGENT_TEAMS_HOME/instructions/<role>.md`, served by `ateam instructions <role>`. See "Machine-local instructions" below.

Default to `ateam learn`. Use `bd remember` only for repo-shared project facts. Never MEMORY.md.

### Machine-local instructions (`ateam instructions <role>`)

This is the route for a human who wants to give a role standing custom direction and does NOT want it to replicate or be autonomously edited. `ateam learn <role> …` is the wrong tool for that job even though it also produces "durable direction to an agent": it writes to the global workspace's beads DB, which syncs to every machine via `refs/dolt/data`, and the condense agent may demote, merge, reword, or evict it with no human gate (see Condensing below). A machine-local instruction file has neither property, and that is the whole reason it exists:

- **It does not replicate.** The instructions file lives at `$AGENT_TEAMS_HOME/instructions/<role>.md`, outside the beads DB entirely. It stays on the machine it was written on unless a human deliberately `git add`s it into the `~/.agent-teams` workspace repo.
- **It is never condensed.** Being a plain file rather than a bd memory, it is structurally invisible to `bd memories --json` — and therefore to `condense`, `condense-check`, `fresh-drain`, `recall`, and `forget`. Nothing autonomous can touch it.

Cap: 4096 bytes. A file over cap is refused, not truncated — silently cutting a human's instruction mid-sentence is worse than not applying it, so `ateam instructions <role>` prints a loud marker naming the file and its size instead of a partial body.

It is additive only: the instructions extend a role's shipped definition and cannot override the role's hard guardrails (e.g. the reviewer's never-fix, never-push, never-merge rules). Tool output arrives after the agent definition has already become the system prompt, so it cannot revoke what that prompt already established.

Today only the reviewer fetches this on spawn (`plugins/agent-teams/roles/reviewer.md`); the other roles don't yet self-fetch it. This is not a loophole in "Never MEMORY.md" above — it is a separate, machine-local channel with different guarantees, not a place to dump project or role knowledge that belongs in `bd remember` or `ateam learn`.

### Three-tier memory model (fresh / hot / cold)

Role memories use a three-tier key convention — the tier is encoded in the key, not in metadata:

- **Fresh:** `<role>:fresh:<slug>` — the default write tier. `ateam learn <role> <slug> --file <f>` (bare slug, no prefix) writes here automatically. Fresh memories accumulate between condense runs; `ateam learnings <role>` serves them alongside hot. Fresh is the "just written, not yet curated" tier and is periodically drained into cold by `ateam fresh-drain <role>`.
- **Hot:** `<role>:hot:<slug>` — curated, auto-injected into every session via `ateam learnings <role>`. Write explicitly with `ateam learn <role> hot:<slug> --file <f>`. Hot bodies are deliberately succinct. The budget for a role's whole hot set (~15–25 learnings) is stated ONCE, in TOKENS, by the `hot_budget_tokens` field of the `ateam condense <role>` packet — read it there rather than restating it here or carrying a number in your head. There is no byte equivalent of that budget; the only byte limits in this model are the per-entry write-time caps, frozen by contract `agent-teams-b2xr.2` (900 bytes hot and fresh, 1500 cold).
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

Condensing is **lock-guarded** via `ateam condense-lock`. Use the `/agent-teams:condense` skill (no arg = all roles; `<role>` arg = single role) — do not call `ateam condense <role>` directly. The skill acquires the lock, skips cleanly if another session holds it, emits the condense packet FIRST (`ateam condense <role>`) for agent curation, drains fresh into cold only afterward (`ateam fresh-drain <role>`, deterministic, no LLM, once the curated hot set has been written), and releases the lock on all exit paths.

**That order is load-bearing — do not "tidy" it back.** The packet marks tiers by the prefix a key carries at read time, and `ateam fresh-drain` prints only a count, never the key list. Draining first would leave the just-served, un-curated entries shape-identical to long-settled cold in the packet, and those are precisely the entries the run must judge for promotion. Keeping the drain last also keeps `ateam condense` a pure read: a run that dies before curation has mutated nothing and retries clean.

The condense agent applies changes directly via `ateam learn` and `ateam forget` — cold writes use `ateam learn <role> cold:<slug> --file <f>` (since bare `ateam learn` now writes fresh). There is NO human-review gate and NO staged diff — the agent acts autonomously.

New verbs introduced by the three-tier model:
- `ateam fresh-drain <role>` — deterministic drain of `role:fresh:*` into cold (no LLM).
- `ateam condense-lock acquire` / `ateam condense-lock release` — advisory lock for condense serialization.

Safety backstops:
- **Dolt history** — every write, including eviction, is recoverable via `refs/dolt/data`. A bad run is revertible.
- **Change-summary log** — the condense agent emits one line per run: `promoted N / merged M / evicted K / hot now X tokens`.

v1 has no per-run eviction floor — trust the agent and Dolt-history recoverability.
