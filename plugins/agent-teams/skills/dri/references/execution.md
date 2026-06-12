# Execution mechanics — team, worktrees, integration

## Team

- `TeamCreate` with the team slug from preflight. Spawn members with the Agent tool: `subagent_type: "agent-teams:<role>"`, `team_name`, a human-readable `name`, `run_in_background: true`, and **`mode: "bypassPermissions"`**. The bypass mode is required for hands-off operation — backgrounded teammates must run without permission prompts.
- Safety under bypass: role rules (never push/merge/deploy — DRI-only) and worktree isolation remain the guardrails. Bypass removes prompts, not role discipline.
- Give every spawn: its assigned bead ids, its worktree path, the role-division rules, and "report to team-lead; ping immediately on blockers or design ambiguity — never guess."
- Models: planner=opus, others=sonnet (the agent defaults) unless the human directed otherwise.
- Messages cross: an idle notification right after you assign work usually means the assignment hasn't been processed yet — verify against bd/git state before re-sending or escalating.

## CWD discipline — the DRI never lets its cwd drift

- **Never call `EnterWorktree`.** It re-pins the session cwd to the entered worktree; the harness re-applies that pin before every Bash call (`cd` cannot escape it). When that worktree is later removed at teardown, the pin dangles and the shell falls back to `$HOME`. The DRI's checkout under `${AGENT_TEAMS_HOME}-worktrees/<initiative>` IS its isolation — it is already isolated; there is nothing to "enter". (Recovery, if you ever do drift: `ExitWorktree` with `action: keep` returns the session to its original checkout without removing the worktree — that is its only sanctioned use in a DRI session.)
- **Ignore the background-session bootstrap nudge.** If the session prompt says "use `EnterWorktree` to isolate your work — unless your cwd is already under `.claude/worktrees/`": IGNORE it. A DRI worktree lives under `${AGENT_TEAMS_HOME}-worktrees/`, which does not match that skip-condition, so the nudge misfires. You are isolated regardless.
- **Stay cwd-immune.** Never depend on the shell cwd. Use `git -C <abs>` and `bd -C <abs>` and absolute paths for every command. This is already global policy; for the DRI it is load-bearing — a drifted or dangling pin silently miss-targets a sibling worktree with no error.
- **Operate on track worktrees via `-C`/absolute paths.** Create them with `git worktree add` / `bd worktree create` and hand each implementer its absolute path. Never chdir or call `EnterWorktree` into a track worktree to operate in it.
- Non-isolated team agents inherit the lead's cwd at spawn, so a drifted lead cascades miss-targeting to every agent it spawns — another reason the lead must never drift.

## Worktrees (parallel tracks)

- **Canonical worktree root.** Create every track worktree under one machine-wide root: `${AGENT_TEAMS_HOME}-worktrees/<team>-<track>` (default `~/.agent-teams-worktrees/...`). This is deliberately OUTSIDE both the workspace and the project repo. Using one predictable root is what lets `/setup-agent-teams` pre-approve it once in `additionalDirectories` (step 5c) so the DRI's worktree git does not draw file-access prompts — ad-hoc sibling paths cannot be pre-approved. (`.beads/` discovery is unaffected by location: a git worktree resolves the project's single `.beads/` via git-common-dir, not by filesystem walk.)
- One **git worktree** (not an independent clone) per parallel track, branched at the FROZEN CONTRACT commit, at `<path>` = `${AGENT_TEAMS_HOME}-worktrees/<team>-<track>`.
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
