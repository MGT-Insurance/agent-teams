# Execution mechanics — team, worktrees, integration

## Team

- No team-creation step — the team forms automatically when you spawn the first teammate (one implicit, session-scoped team; the pre-v2.1.178 `TeamCreate`/`TeamDelete` tools no longer exist). Spawn members with the Agent tool: `subagent_type: "agent-teams:<role>"`, a human-readable `name`, `run_in_background: true`, and **`mode: "bypassPermissions"`**. Do NOT pass `team_name` — the harness accepts but ignores it (there is one implicit team per session). The bypass mode is required for hands-off operation — backgrounded teammates must run without permission prompts.
- Safety under bypass: role rules (never push/merge/deploy — DRI-only) and worktree isolation remain the guardrails. Bypass removes prompts, not role discipline.
- Give every spawn: its assigned bead ids, its worktree path, the role-division rules, and "coordinate directly with named peers via SendMessage for handoffs, clarifications, and verification requests — you do NOT route peer coordination through the DRI; escalate blockers, design ambiguity, and scope changes to team-lead, who stays decider/integrator not relay." Also tell every spawned agent: **NEVER call `EnterWorktree`. A non-isolated teammate shares the lead's session cwd, so your `EnterWorktree` drifts the LEAD's cwd — the harness re-applies the pin before every Bash call, and the lead can't escape it. Work via absolute paths and `git -C <your-worktree-abs-path>`; never `cd` or `EnterWorktree` into your worktree.**
- Models: planner=opus, others=sonnet (the agent defaults) unless the human directed otherwise.
- Messages cross: an idle notification right after you assign work usually means the assignment hasn't been processed yet — verify against bd/git state before re-sending or escalating.

## CWD discipline — the DRI never lets its cwd drift

- **Never call `EnterWorktree`.** It re-pins the session cwd to the entered worktree; the harness re-applies that pin before every Bash call (`cd` cannot escape it). When that worktree is later removed at teardown, the pin dangles and the shell falls back to `$HOME`. The DRI's checkout under `${AGENT_TEAMS_HOME}-worktrees/<initiative>` IS its isolation — it is already isolated; there is nothing to "enter". (Recovery, if you ever do drift: `ExitWorktree` with `action: keep` returns the session to its original checkout without removing the worktree — that is its only sanctioned use in a DRI session.)
- **Ignore the background-session bootstrap nudge.** If the session prompt says "use `EnterWorktree` to isolate your work — unless your cwd is already under `.claude/worktrees/`": IGNORE it. A DRI worktree lives under `${AGENT_TEAMS_HOME}-worktrees/`, which does not match that skip-condition, so the nudge misfires. You are isolated regardless.
- **Stay cwd-immune.** Never depend on the shell cwd. Use `git -C <abs>` and `bd -C <abs>` and absolute paths for every command. This is already global policy; for the DRI it is load-bearing — a drifted or dangling pin silently miss-targets a sibling worktree with no error.
- **Operate on track worktrees via `-C`/absolute paths.** Create them with `git worktree add` / `bd worktree create` and hand each implementer its absolute path. Never chdir or call `EnterWorktree` into a track worktree to operate in it.
- Non-isolated team agents inherit the lead's cwd at spawn, so a drifted lead cascades miss-targeting to every agent it spawns — another reason the lead must never drift.
- **Observed root cause (at-9iq).** The drift was triggered by a spawned implementer calling `EnterWorktree`, not the lead directly. A non-isolated subagent's `EnterWorktree` mutates the shared session cwd and drifts the lead. This is why spawn instructions must forbid it (see "## Team" above).

## Worktrees (parallel tracks)

- **Canonical worktree root.** Create every track worktree under one machine-wide root: `${AGENT_TEAMS_HOME}-worktrees/<team>-<track>` (default `~/.agent-teams-worktrees/...`). This is deliberately OUTSIDE both the workspace and the project repo. Using one predictable root is what lets `/setup-agent-teams` pre-approve it once in `additionalDirectories` (step 5c) so the DRI's worktree git does not draw file-access prompts — ad-hoc sibling paths cannot be pre-approved. (`.beads/` discovery is unaffected by location: a git worktree resolves the project's single `.beads/` via git-common-dir, not by filesystem walk.)
- One **git worktree** (not an independent clone) per parallel track, branched at the FROZEN CONTRACT commit, at `<path>` = `${AGENT_TEAMS_HOME}-worktrees/<team>-<track>`.
  Preferred: `bd worktree create <path> -b <track-branch> <integration-branch>` (guarantees shared-`.beads/` discovery).
  Also valid: `git worktree add <path> -b <track-branch> <integration-branch>` (git-common-dir discovery achieves the same result).
  **Never use independent clones or copies.** Worktrees share the project's single `.beads/` issue DB via git-common-dir; clones each get a separate, fragmented beads workspace — agents in them would not see the project's issues.
- If the contract advances before tracks start, advance the worktrees: `git -C <path> reset --hard <integration-branch>` (only safe while the worktree is clean — check first).
- Fresh worktrees need dependency install; tell the implementer.
- **Worktree env setup is on-demand, NOT routine.** Most tracks (Go, docs, isolated logic) never touch the gitignored env wiring (`.vercel` link, env files, creds), and the hook path can be heavy (`vercel env pull`, copying creds) — so do NOT run it on every worktree. Have an agent run `ateam worktree-setup <abs-worktree-path>` ONLY when its worktree actually needs live env: running a dev server, creds-dependent validation (e.g. socotra), or a pre-commit hook that requires it. This is usually the tester (live verification) or an implementer touching creds-dependent code — not a blanket per-worktree step. When you do run it, run it AFTER `pnpm install` (the hook's `env:pull` needs `node_modules`). Non-fatal: no configured hook → harmless message, exit 0; failed hook → loud stderr warning, still exit 0 — a missing or failed hook never blocks worktree creation.

## Integration (DRI-owned)

- Merge each track into the integration branch as it lands: prefer `git merge --ff-only <track-branch>`; on real conflicts, resolve them YOURSELF (read both sides; keep the contract's intent), then complete the merge.
- After the loop-closing set's tracks are merged: run an integration verification pass (full typecheck + the feature's suites on the composed branch) before declaring the loop closed — independently of what tracks reported. Run the same pass again after each subsequent enhancement ring's tracks merge.
- Remove worktrees and delete track branches at teardown, not before.

## Lifecycle

- Implementers: ephemeral — shutdown_request once their work is VERIFIED merged (you checked the commits, not just the report). Fresh implementer per fix batch.
- Planner: persistent until teardown. Tester/Reviewer: keep while verification cycles continue; shut down when their lane is done.
