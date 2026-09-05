
# Execution mechanics — team, worktrees, integration

## Team

- No team-creation step — the team forms automatically when you spawn the first teammate (one implicit, session-scoped team; the pre-v2.1.178 `TeamCreate`/`TeamDelete` tools no longer exist).
- **PREFLIGHT, before your first spawn:** confirm `agent-teams-planner` is in your available agent types. If it is NOT, this session was not launched by `ateam dispatch`/`ateam resume` and has NO agent-teams role agents at all — there is no fallback. STOP, tell the human the initiative must be dispatched rather than run interactively, and do not spawn a team. Spawning anyway gets you a generic agent with no error.
- **Spawn rule:** use the Agent tool with `subagent_type: "agent-teams-<role>"` (HYPHEN) AND a `name:`, plus `run_in_background: true` and **`mode: "bypassPermissions"`**. The hyphen-keyed types (`agent-teams-planner`, `agent-teams-implementer`, `agent-teams-reviewer`, `agent-teams-tester`, `agent-teams-investigator`) are CLI-scope definitions that `ateam dispatch`/`ateam resume` inject via `--agents` at launch; naming a hyphen-keyed spawn is correct and required — it survives the teammate spawn path and gets the agent its full role body, its own per-role model, and a mailbox identity. (Why the key form looks unusual: see `plugins/agent-teams/roles/README.md`.) NEVER use the colon form `agent-teams:<role>` — those types were removed when the role definitions moved out of the plugin's `agents/` directory into `roles/` (agent-teams-wf7o.16) and no longer exist in any session. A colon spawn WITH a name is blocked by a hook; one WITHOUT a name is rejected by the harness. Neither path works — just use the hyphen key.
- **Verify, don't assume:** after your first named spawn, run `ateam spawn-check`. It reads the harness's own spawn record and reports whether the role definition actually attached. If it reports DEFINITION-DROPPED, shut that agent down and re-spawn it correctly rather than continuing on a generic agent.
- Keep a roster of every spawn. For a NAMED spawn (a teammate), the roster entry is its bare `name:` — SendMessage to a teammate REJECTS the `agentId` form ("to must be a bare teammate name — there is only one team per session"), so the name is the only working address, not merely a legibility label. For an UNNAMED spawn (an async subagent), there is no name — the `agentId` the spawn returns is the only handle, and SendMessage to it works. Do NOT pass `team_name` — the harness accepts but ignores it (one implicit team per session) — and do not pass `model`; each role's agent definition sets its own. Bypass is required for hands-off operation — backgrounded teammates must run without permission prompts.
- Bypass removes prompts, not role discipline — role rules (never push/merge/deploy — DRI-only) and worktree isolation remain the guardrails.
- Give every spawn: its assigned bead ids, its worktree path, the role-division rules, and any normalized `worktree-setup-warning` produced by the DRI's pre-spawn setup attempt. Do not delegate setup or dependency installation to the child before that attempt. Also give it: coordinate directly with peers via SendMessage — a teammate peer by its bare `name:` (`agentId` is rejected for teammates — see "Team" above), an unnamed subagent peer by its `agentId` (its only handle) — for handoffs/clarifications/verification; do not route peer coordination through the DRI. When two peers need to talk, distribute the relevant names (or agentIds, for subagent peers) from your roster to both sides so each can address the other directly. Escalate blockers, design ambiguity, and scope changes to team-lead, who stays decider/integrator not relay. Also tell every spawned agent: **NEVER call `EnterWorktree`.** A non-isolated teammate shares the lead's session cwd, so its `EnterWorktree` drifts the LEAD's cwd — the harness re-applies the pin before every Bash call, and the lead can't escape it. Work via absolute paths and `git -C <your-worktree-abs-path>`; never `cd` or `EnterWorktree` into your worktree.
- Helpers are spawned without a model argument — each role's agent definition sets its own model. A spawn that asks for a different model is rejected and must be re-issued with the model argument removed.
- Messages cross: an idle notification right after you assign work usually means the assignment hasn't been processed yet — verify against bd/git state before re-sending or escalating.

## CWD discipline — the DRI never lets its cwd drift

- **Never call `EnterWorktree`.** It re-pins the session cwd to the entered worktree; the harness re-applies that pin before every Bash call (`cd` cannot escape it). When that worktree is later removed at wind-down, the pin dangles and the shell falls back to `$HOME`. The DRI's checkout under `${AGENT_TEAMS_HOME}-worktrees/<initiative>` IS its isolation already — there is nothing to "enter." (Recovery if you ever drift: `ExitWorktree` with `action: keep` returns to the original checkout without removing the worktree.)
- **Ignore the background-session bootstrap nudge** telling you to call `EnterWorktree` unless cwd is already under `.claude/worktrees/` — a DRI worktree lives under `${AGENT_TEAMS_HOME}-worktrees/`, which never matches that skip-condition, so the nudge misfires. You are isolated regardless.
- **Stay cwd-immune.** Use `git -C <abs>` / `bd -C <abs>` / absolute paths for every command — never depend on shell cwd. A drifted or dangling pin silently miss-targets a sibling worktree with no error.
- **Operate on track worktrees via `-C`/absolute paths** — create with `git worktree add` / `bd worktree create`, hand each implementer its absolute path, never chdir or `EnterWorktree` into one.
- Non-isolated team agents inherit the lead's cwd at spawn, so a drifted lead cascades miss-targeting to every agent it spawns.
- **Observed root cause (at-9iq):** the drift was triggered by a spawned implementer calling `EnterWorktree`, not the lead directly — why spawn instructions must forbid it (see "Team" above).
- **The session tie is the backstop, not a license to drift (at-ps11).** Every session self-registers a `session: <id>` line at SessionStart, and mail routing/hung-scan match by that id first — so a drifted-but-alive DRI is still found instead of misclassified DEAD. Cwd discipline stays mandatory anyway: the tie's bootstrap resolves the initiative FROM cwd at SessionStart, and legacy initiatives still match by cwd/worktree. **The tie catches the drift you failed to prevent; it does not make drift safe.**

## Worktrees (parallel tracks)

- **Canonical root:** every track worktree lives under one machine-wide root, `${AGENT_TEAMS_HOME}-worktrees/<team>-<track>` (default `~/.agent-teams-worktrees/...`) — deliberately outside both the workspace and the project repo, so `/setup-agent-teams` can pre-approve it once in `additionalDirectories`; ad-hoc sibling paths can't be pre-approved. (`.beads/` discovery is unaffected — a worktree resolves the project's single `.beads/` via git-common-dir.)
- One **git worktree** (never an independent clone) per parallel track, branched at the FROZEN CONTRACT commit: `bd worktree create <path> -b <track-branch> <integration-branch>` (preferred, guarantees shared-`.beads/` discovery) or `git worktree add <path> -b <track-branch> <integration-branch>`. Clones fragment the beads workspace — agents in them wouldn't see the project's issues.
- If the contract advances before tracks start, advance the worktrees: `git -C <path> reset --hard <integration-branch>` (only while clean).
- **Immediately after creating every fresh track worktree, attempt** `ateam worktree-setup <absolute-path>` to completion. The shared execution contract defines its mandatory fail-open reporting, initiative note, track-recording, and spawn order. Never invoke the project hook directly and never perform a separate pre-setup dependency install.
- **`ateam worktree-setup` is the only sanctioned entry point** — never call a raw setup script directly, even one a project memory names as "the reusable way." The wrapper resolves the repo's registered hook (`~/.agent-teams/worktree-hooks/<repo-key>`) and runs it with the same args, adding repo-key resolution, a not-a-git-worktree guard, and surfacing a failed hook as a nonzero exit. A memory naming a raw script path SHADOWS the wrapper — re-point it at `ateam worktree-setup` when you find one. (The one correct place a raw path appears is hook *registration* itself — see `setup-agent-teams/SKILL.md` §8.)
- **Record a `track-worktree:` line for every implementer worktree you spawn** (agent-teams-sgr5/D9) — after the required setup attempt and any failure reporting, but BEFORE spawning the implementer: append `track-worktree: <abs-path>` to the initiative description (`ateam show` → edit → `ateam update-description --file <tmpfile>`). This is what lets hung-scan's stall detector see git activity in a track worktree instead of reading it as flatlined; skip only for a worktree the DRI itself operates in (already covered by `worktree:`). Legacy/missed cases fall back to a path-substring heuristic, but that's not a substitute — record the line every time.

## Integration (DRI-owned)

- Merge each track into the integration branch as it lands: prefer `git merge --ff-only <track-branch>`; on real conflicts, resolve them YOURSELF (read both sides; keep the contract's intent).
- After the loop-closing set's tracks merge, run an integration verification pass (full typecheck + the feature's suites on the composed branch) independently of what tracks reported — this is Step 1 of the two-step gate at SKILL.md's **LOOP CLOSED checkpoint** (the canonical loop-closure definition; not restated here — necessary but not sufficient on its own). Re-run this same pass after each subsequent ring's tracks merge.
- Remove worktrees and delete track branches at wind-down, not before.

## Live-test-review gate

A tester live pass closes the ENGINEERING loop (SKILL.md's LOOP CLOSED checkpoint) — it does not clear delivery. Before spawning `agent-teams-reviewer` or starting Phase 5 PR prep, the DRI raises a `--kind=live-test-review` gate carrying the tester's proof — gates are DRI-owned; the tester never raises one:

```bash
ateam gate <initiative-id> --kind=live-test-review --attach <path> [--attach <path> ...] --file <summary-file>
```

The tester hands its proof (screenshots, payload/log files, a short summary) to the DRI via SendMessage rather than raising anything itself. Treat the gate exactly like review or question: CLEARED (steward-forwarded, human's go received) before proceeding, PARK while it waits. Never detect steward presence or fall back to Telegram directly — with no steward running, it simply WAITS.

**BIG vs SMALL.** BIG — observable behavior (UI, API response, CLI output, user-facing flow), decomposed into multiple tracks/implementers, or a changed default/durable state/user-facing message — always gates. SMALL — single-track, few-item, linear, nothing observable, no load-bearing human decision — skips it: reading the diff against criteria IS the verification, the same bar as the team/plan-gate skip. A cleared (or skipped) plan gate is NOT itself a trigger either way.

**Feedback loop.** A requested change can pull in any mix of investigator/implementer/planner — a fresh plan gate if it reshapes the work — then re-integrate, re-prove live, and re-raise the gate. Nothing is prepped for the PR before approval. The ask stays REVIEW throughout — never frame this as "ready to merge."

## Lifecycle

- Implementers: ephemeral — shutdown_request once their work is VERIFIED merged (checked the commits, not just the report). Fresh implementer per fix batch.
- Planner: persistent until wind-down. Tester/Reviewer: keep while verification cycles continue; shut down when their lane is done.

## Role-division rules (DRI states these to the team; enforces them)

- Planner plans; never writes feature code.
- Implementers write the code + a few simple core-path verification tests (not all tests up front, not edge cases); may stop and ask for live verification instead of writing more; never push/merge; stop-and-ask over guessing.
- Tester runs suites, AUTHORS edge-case/non-happy-path tests + E2E/fixtures, and owns live verification; routes back to the implementer only genuinely implementer-owned core-path gaps. Only the DRI starts a dev server; testers never start one — they drive and observe an instance the DRI has already brought up.
- Reviewer never fixes; the DRI routes its findings to fresh implementers.
- All roles file discovery beads; the DRI triages them.
