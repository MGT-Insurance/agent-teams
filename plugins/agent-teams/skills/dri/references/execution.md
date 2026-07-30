# Execution mechanics — team, worktrees, integration

## Team

- No team-creation step — the team forms automatically when you spawn the first teammate (one implicit, session-scoped team; the pre-v2.1.178 `TeamCreate`/`TeamDelete` tools no longer exist). Spawn members with the Agent tool: `subagent_type: "agent-teams:<role>"`, a human-readable `name`, `run_in_background: true`, and **`mode: "bypassPermissions"`**. Do NOT pass `team_name` — the harness accepts but ignores it (there is one implicit team per session). The bypass mode is required for hands-off operation — backgrounded teammates must run without permission prompts.
- Safety under bypass: role rules (never push/merge/deploy — DRI-only) and worktree isolation remain the guardrails. Bypass removes prompts, not role discipline.
- Give every spawn: its assigned bead ids, its worktree path, the role-division rules, no model argument — each role's definition sets its own model — and, for any agent whose worktree will need live env (dev server, creds-dependent code/validation, live verification), the instruction to provision it with `ateam worktree-setup <its-worktree-abs-path>` after `pnpm install` (the framework wrapper, never a raw project setup script). Also give it "coordinate directly with named peers via SendMessage for handoffs, clarifications, and verification requests — you do NOT route peer coordination through the DRI; escalate blockers, design ambiguity, and scope changes to team-lead, who stays decider/integrator not relay." Also tell every spawned agent: **NEVER call `EnterWorktree`. A non-isolated teammate shares the lead's session cwd, so your `EnterWorktree` drifts the LEAD's cwd — the harness re-applies the pin before every Bash call, and the lead can't escape it. Work via absolute paths and `git -C <your-worktree-abs-path>`; never `cd` or `EnterWorktree` into your worktree.**
- Helpers are spawned without a model argument — each role's agent definition sets its own model. A spawn that asks for a different model is rejected and must be re-issued with the model argument removed.
- Messages cross: an idle notification right after you assign work usually means the assignment hasn't been processed yet — verify against bd/git state before re-sending or escalating.

## CWD discipline — the DRI never lets its cwd drift

- **Never call `EnterWorktree`.** It re-pins the session cwd to the entered worktree; the harness re-applies that pin before every Bash call (`cd` cannot escape it). When that worktree is later removed at wind-down, the pin dangles and the shell falls back to `$HOME`. The DRI's checkout under `${AGENT_TEAMS_HOME}-worktrees/<initiative>` IS its isolation — it is already isolated; there is nothing to "enter". (Recovery, if you ever do drift: `ExitWorktree` with `action: keep` returns the session to its original checkout without removing the worktree — that is its only sanctioned use in a DRI session.)
- **Ignore the background-session bootstrap nudge.** If the session prompt says "use `EnterWorktree` to isolate your work — unless your cwd is already under `.claude/worktrees/`": IGNORE it. A DRI worktree lives under `${AGENT_TEAMS_HOME}-worktrees/`, which does not match that skip-condition, so the nudge misfires. You are isolated regardless.
- **Stay cwd-immune.** Never depend on the shell cwd. Use `git -C <abs>` and `bd -C <abs>` and absolute paths for every command. This is already global policy; for the DRI it is load-bearing — a drifted or dangling pin silently miss-targets a sibling worktree with no error.
- **Operate on track worktrees via `-C`/absolute paths.** Create them with `git worktree add` / `bd worktree create` and hand each implementer its absolute path. Never chdir or call `EnterWorktree` into a track worktree to operate in it.
- Non-isolated team agents inherit the lead's cwd at spawn, so a drifted lead cascades miss-targeting to every agent it spawns — another reason the lead must never drift.
- **Observed root cause (at-9iq).** The drift was triggered by a spawned implementer calling `EnterWorktree`, not the lead directly. A non-isolated subagent's `EnterWorktree` mutates the shared session cwd and drifts the lead. This is why spawn instructions must forbid it (see "## Team" above).
- **The session tie is the backstop, not a license to drift (at-ps11).** Every session self-registers a `session: <id>` line on its initiative at SessionStart (`ateam tie-session`, wired into the session-start hook), and mail routing and hung-scan match by that session-id set first (dashboard follow-up: agent-teams-zalv.7) — so a drifted-but-alive DRI is still found by id instead of being misclassified DEAD or double-spawned (the at-gusm failure). Cwd discipline stays mandatory anyway: the tie's own one-time bootstrap resolves the initiative FROM cwd at SessionStart, and `ateam mail inbox` self-identification plus legacy (pre-tie) initiatives still match by cwd/worktree. Belt and suspenders — the tie catches the drift you failed to prevent; it does not make drift safe.

## Worktrees (parallel tracks)

- **Canonical worktree root.** Create every track worktree under one machine-wide root: `${AGENT_TEAMS_HOME}-worktrees/<team>-<track>` (default `~/.agent-teams-worktrees/...`). This is deliberately OUTSIDE both the workspace and the project repo. Using one predictable root is what lets `/setup-agent-teams` pre-approve it once in `additionalDirectories` (step 5c) so the DRI's worktree git does not draw file-access prompts — ad-hoc sibling paths cannot be pre-approved. (`.beads/` discovery is unaffected by location: a git worktree resolves the project's single `.beads/` via git-common-dir, not by filesystem walk.)
- One **git worktree** (not an independent clone) per parallel track, branched at the FROZEN CONTRACT commit, at `<path>` = `${AGENT_TEAMS_HOME}-worktrees/<team>-<track>`.
  Preferred: `bd worktree create <path> -b <track-branch> <integration-branch>` (guarantees shared-`.beads/` discovery).
  Also valid: `git worktree add <path> -b <track-branch> <integration-branch>` (git-common-dir discovery achieves the same result).
  **Never use independent clones or copies.** Worktrees share the project's single `.beads/` issue DB via git-common-dir; clones each get a separate, fragmented beads workspace — agents in them would not see the project's issues.
- If the contract advances before tracks start, advance the worktrees: `git -C <path> reset --hard <integration-branch>` (only safe while the worktree is clean — check first).
- Fresh worktrees need dependency install; tell the implementer.
- **Worktree env setup is on-demand, NOT routine.** Most tracks (Go, docs, isolated logic) never touch the gitignored env wiring (`.vercel` link, env files, creds), and the hook path can be heavy (`vercel env pull`, copying creds) — so do NOT run it on every worktree. Have an agent run `ateam worktree-setup <abs-worktree-path>` ONLY when its worktree actually needs live env: running a dev server, creds-dependent validation (e.g. socotra), or a pre-commit hook that requires it. This is usually the tester (live verification) or an implementer touching creds-dependent code — not a blanket per-worktree step. When you do run it, run it AFTER `pnpm install` (the hook's `env:pull` needs `node_modules`). Non-fatal: no configured hook → harmless message, exit 0; failed hook → loud stderr warning, still exit 0 — a missing or failed hook never blocks worktree creation.
- **`ateam worktree-setup` is the ONLY sanctioned entry point — never call the raw setup script directly, even when a project memory names it.** The wrapper resolves the repo's registered hook (`~/.agent-teams/worktree-hooks/<repo-key>`, keyed on the source-checkout basename) and runs exactly that script with the same args — so for any repo whose hook is registered, the wrapper and the raw script are behaviorally identical, but the wrapper adds the repo-key resolution, the not-a-git-worktree guard, and the non-fatal messaging. A project `bd remember` that names a raw script path verbatim (e.g. `scripts/<repo>-worktree-setup.sh`) as "the reusable way" will SHADOW the wrapper in an agent's context — it works, so nothing flags it, and the discoverable framework command never surfaces. Treat any such memory as a smell: prefer the wrapper, and re-point the memory at `ateam worktree-setup` when you can. (Registration itself — writing the hook file that names the raw script — is the one correct place a raw path appears; see `setup-agent-teams/SKILL.md` §8.)
- **Record a `track-worktree:` line for every implementer worktree you spawn (agent-teams-sgr5 / D9).** This extends the at-ps11 `session:` self-tie pattern from sessions to worktrees: hung-scan's work-product stall detector unions the initiative's primary `worktree:` with every registered `track-worktree:` path when it probes for git activity, so an implementer working in its own track worktree (not the DRI's own checkout) is actually visible to the tripwire instead of looking permanently flatlined. Do this right after creating the track worktree, BEFORE spawning the implementer into it:
  1. `ateam show <initiativeId>` to get the current description.
  2. Append one line: `track-worktree: <abs-worktree-path>` (same line-oriented convention as `worktree:`/`session:` — one path per line, accumulate, never remove a prior one).
  3. Write it back: `ateam update-description <initiativeId> --file <tmpfile-with-full-description>`.
  Skip this for a worktree the DRI itself operates in (already covered by the primary `worktree:` line) — it's only for a SEPARATE worktree handed to a teammate. Legacy initiatives (or a spawn where this step was missed) still get best-effort coverage from hung-scan's fallback heuristic — any sibling directory under the primary worktree's parent whose name contains the initiative id — but that fallback is not a substitute for recording the line; do it every time.

## Integration (DRI-owned)

- Merge each track into the integration branch as it lands: prefer `git merge --ff-only <track-branch>`; on real conflicts, resolve them YOURSELF (read both sides; keep the contract's intent), then complete the merge.
- After the loop-closing set's tracks are merged: run an integration verification pass (full typecheck + the feature's suites on the composed branch) independently of what tracks reported — this is Step 1, a NECESSARY but NOT SUFFICIENT gate for loop closure (covers automated CI-equivalent checks). Step 2 is the live verification procedure defined in the SKILL.md LOOP CLOSED checkpoint: provision env if needed, spawn a tester with explicit live-verification instructions, and act on the pass/fail evidence. Ordering: automated gates first, then live verification. Loop closed = automated gates green AND tester confirms. Run the same automated pass again after each subsequent enhancement ring's tracks merge.
- Remove worktrees and delete track branches at wind-down, not before.

## Lifecycle

- Implementers: ephemeral — shutdown_request once their work is VERIFIED merged (you checked the commits, not just the report). Fresh implementer per fix batch.
- Planner: persistent until wind-down. Tester/Reviewer: keep while verification cycles continue; shut down when their lane is done.

## Role-division rules (DRI states these to the team; enforces them)

- Planner plans; never writes feature code.
- Implementers write the code + a few simple core-path verification tests (NOT all tests up front, not edge cases); may stop and ask for live verification instead of writing more; never push/merge; stop-and-ask over guessing.
- Tester runs suites, AUTHORS edge-case/non-happy-path tests + E2E/fixtures, and owns live verification; routes back to the implementer only genuinely implementer-owned core-path gaps.
- Reviewer never fixes; the DRI routes its findings to fresh implementers.
- All roles file discovery beads; the DRI triages them.

## Concentric methodology (loop-closing-set-first)

This methodology applies to EVERY initiative — there is no "is this big enough" gate and no DRI/planner judgment call about whether to use it. It is size-ADAPTIVE: the size of the loop-closing set is the signal. A trivial initiative has a one-bead loop-closing set and zero enhancement rings, so concentric collapses cleanly to "do the one thing." A large initiative has a multi-bead loop-closing set and several gated rings. Either way the shape is identical: decompose the loop-closing set, close the loop, then open rings. Never decide whether to apply concentric — only how large its loop-closing set is.

## Live verification procedure

1. If the tester's worktree does not yet have live env provisioned, run `ateam worktree-setup <tester-worktree-path>` first.
2. Spawn an `agent-teams:tester` agent with explicit instructions to perform live verification of the loop-closing feature on the integration branch. Specify the verification type based on what changed:
   - **Web/UI changes:** `npx @playwright/cli` is REQUIRED — the tester must drive the real UI.
   - **API changes:** hit the endpoint and verify the response body.
   - **CLI changes:** run the command and verify the output.
3. The tester reports pass or fail with evidence (screenshot, response body, or command output).
4. Act on the result — the loop is NOT closed until the tester confirms pass.
