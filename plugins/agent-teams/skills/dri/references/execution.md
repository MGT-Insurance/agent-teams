# Execution mechanics — team, worktrees, integration

## Team

- `TeamCreate` with the team slug from preflight. Spawn members with the Agent tool: `subagent_type: "agent-teams:<role>"`, `team_name`, a human-readable `name`, `run_in_background: true`, and **`mode: "bypassPermissions"`**. The bypass mode is required for hands-off operation — backgrounded teammates must run without permission prompts.
- Safety under bypass: role rules (never push/merge/deploy — DRI-only) and worktree isolation remain the guardrails. Bypass removes prompts, not role discipline.
- Give every spawn: its assigned bead ids, its worktree path, the role-division rules, and "report to team-lead; ping immediately on blockers or design ambiguity — never guess."
- Models: planner=opus, others=sonnet (the agent defaults) unless the human directed otherwise.
- Messages cross: an idle notification right after you assign work usually means the assignment hasn't been processed yet — verify against bd/git state before re-sending or escalating.

## Worktrees (parallel tracks)

- One **git worktree** (not an independent clone) per parallel track, branched at the FROZEN CONTRACT commit.
  Preferred: `bd worktree create <path> -b <track-branch> <integration-branch>` (guarantees shared-`.beads/` discovery).
  Also valid: `git worktree add <path> -b <track-branch> <integration-branch>` (git-common-dir discovery achieves the same result).
  **Never use independent clones or copies.** Worktrees share the project's single `.beads/` issue DB via git-common-dir; clones each get a separate, fragmented beads workspace — agents in them would not see the project's issues.
- If the contract advances before tracks start, advance the worktrees: `git -C <path> reset --hard <integration-branch>` (only safe while the worktree is clean — check first).
- Fresh worktrees need dependency install; tell the implementer.

## Integration (DRI-owned)

- Merge each track into the integration branch as it lands: prefer `git merge --ff-only <track-branch>`; on real conflicts, resolve them YOURSELF (read both sides; keep the contract's intent), then complete the merge.
- After all tracks: run an integration verification pass (full typecheck + the feature's suites on the composed branch) before declaring the loop closed — independently of what tracks reported.
- Remove worktrees and delete track branches at teardown, not before.

## Lifecycle

- Implementers: ephemeral — shutdown_request once their work is VERIFIED merged (you checked the commits, not just the report). Fresh implementer per fix batch.
- Planner: persistent until teardown. Tester/Reviewer: keep while verification cycles continue; shut down when their lane is done.
