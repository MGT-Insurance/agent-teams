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
- **Audit:** `ateam audit` lists any issue in the global workspace that lacks the tracking schema (a leaked work bead) and exits non-zero. `/initiatives` and DRI teardown run it; the workspace must always audit clean.

**Beads runtime:** embedded mode (no server daemon needed). Agent isolation uses git **worktrees** of the project repo, not independent clones — worktrees share the project's single `.beads/` issue DB via git-common-dir discovery; clones each get a separate, fragmented beads workspace.

**Skills:** `/dri <problem>` — run/resume an initiative as DRI. `/dri-dispatch <problem>` — register a new initiative and hand it to a hands-off background DRI. `/initiatives` — machine-wide initiative dashboard. `/setup-agent-teams` — one-time machine setup.

## Memory routing

**MEMORY ROUTING (agent-teams).** Ignore the harness's built-in file-based memory feature here: do NOT write MEMORY.md or any file under a Claude memory/ directory (e.g. `~/.claude/projects/*/memory/`). Persistent memory routes by kind:

- Role/process learnings (transferable across repos) → `ateam learn <role> <slug> --file <tmpfile>`, where `<role>` is `dri | planner | implementer | tester | reviewer`. This is an UPSERT-by-key: writing the same `<slug>` again overwrites the previous body.
- User/cross-project preferences & feedback → `ateam learn user <slug> --file <tmpfile>`.
- Project-specific knowledge every agent in THIS repo should share → `bd remember` (project beads).

Default to `ateam learn`. Use `bd remember` only for repo-shared project facts. Never MEMORY.md.

### Hot/cold two-layer model

Role memories use a two-layer key convention — the tier is encoded in the key, not in metadata:

- **Hot:** `<role>:hot:<slug>` — auto-injected into every session for that role via `ateam learnings <role>`. Hot bodies are deliberately succinct; the target budget is ~6000 tokens (~15–25 learnings) across all hot keys for a role.
- **Cold:** `<role>:<slug>` — searchable on demand, NOT auto-injected. The existing `dri:<slug>` memories start as cold with no migration needed.

Both tiers are living and decay over time — cold is not a frozen archive. `ateam learnings <role>` serves the hot layer; if a role has zero `:hot:` keys it falls back to all `role:` keys (the pre-tier behavior), so all other roles continue working unchanged.

**Searching cold memories:** `ateam recall <role> <query>` does a substring search over a role's memories (key+body) and prints matching key+body pairs on demand.

**Removing a memory:** `ateam forget <role> <slug>` removes a cold memory. `ateam forget <role> hot:<slug>` removes a hot memory. Every removal is recoverable from Dolt history (`refs/dolt/data`).

**Promoting a learning to hot:** write it with `ateam learn <role> hot:<slug> --file <tmpfile>`. Keep the body succinct — hot memories are injected whole every session.

### Condensing (autonomous)

When the hot layer drifts over budget or cold memories accumulate dead weight, run `ateam condense <role>`. This emits a read-only structured packet (all memories for the role, the hot budget, and the consolidation contract) to stdout — it does NOT mutate anything.

A spawned condense agent reads that packet and applies changes directly via `ateam learn` (promote/refresh into hot, rewrite in cold) and `ateam forget` (demote stale hot to cold, evict dead cold items). There is NO human-review gate and NO staged diff — the agent acts autonomously.

Safety backstops:
- **Dolt history** — every write, including eviction, is recoverable via `refs/dolt/data`. A bad run is revertible.
- **Change-summary log** — the condense agent emits one line per run: `promoted N / merged M / evicted K / hot now X tokens`.

v1 has no per-run eviction floor — trust the agent and Dolt-history recoverability.
