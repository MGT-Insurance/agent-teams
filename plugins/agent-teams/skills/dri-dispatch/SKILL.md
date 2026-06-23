---
name: dri-dispatch
description: Create a NEW agent-teams initiative and hand it to a background DRI. Use when asked to "start a new initiative in the background", "kick off / dispatch a background DRI", "spin off separable work as its own initiative", or when invoked as /dri-dispatch <problem statement>. Creates a dedicated worktree, registers the initiative, and launches a hands-off background DRI session that drives it to a PR. Does NOT make THIS session the DRI — use /dri for that.
---

You dispatch a new initiative; you do not become its DRI. This skill sets up a fresh, isolated checkout, registers the initiative in the global workspace, and launches a **background** `/dri` session that owns it end-to-end. The current session stays free.

Use this when:

- A human wants to kick off an initiative without tying up this session.
- A DRI (or any session) surfaces **separable** work — a discovery bead that is really its own feature, infra/tooling work, anything that would balloon the current scope. Do NOT absorb it; dispatch it here. The current initiative stays focused; the new one gets its own DRI, checkout, team, and PR.

For *becoming* the DRI in this session, use `/agent-teams:dri` instead.

---

**THIS SESSION IS A HAND-OFF, NOT AN INVESTIGATION.**

Your job is preflight → capture the human's framing → dispatch. Nothing more.

Do NOT:
- Grep the codebase or measure current state.
- Research the topic or form a mechanism opinion.
- Design a solution or write an "agreed design direction" into the context block.
- Answer clarifying questions on the human's behalf.

All of that is the background DRI's job in its clarify/plan phase. The background DRI has no human attached — it recovers context from the initiative bead and then investigates and plans. Doing that work here does not help; it contaminates the context block with the dispatcher's assumptions instead of the human's framing.

---

## The `ateam` tool

`ateam` is on PATH — it ships as a prebuilt binary in the plugin's `bin/` (auto-added to PATH; installed/verified by `/setup-agent-teams`). Call it as bare `ateam` everywhere this document shows `ateam`. One allowlist entry covers all subcommands: `Bash(ateam:*)`.

**🚨 CARDINAL RULE.** The GLOBAL workspace (reached ONLY via `ateam`) holds ONLY initiative-tracking beads and role memories. Registering an initiative is the ONE write you make there, and `ateam register` is the only sanctioned way to do it — NEVER `bd -C <global> create`. All work beads (the planner's decomposition, feature/task/discovery beads) live in the PROJECT repo and are created by the background DRI and its team, not here.

## Steps

### 1. Preflight

- Verify `ateam` is on PATH: run `ateam ws`. If it errors or is not found, tell the human to run `/setup-agent-teams` and stop.
- Run `ateam audit`. It must report clean before you add anything.

### 2. Capture the human's framing

The dispatcher's only judgment here is whether the hand-off inputs are present. Do NOT research the topic or analyze the codebase to fill gaps you perceive.

- **Problem statement.** Take it verbatim from the invocation. If none was given, ask the human — this is the only load-bearing input you should ask for. Do not rephrase or embellish it.
- **Context block.** Copy the human's framing as-is: their stated constraints, background, decisions they've already made, and any open questions they've raised that the background DRI should answer before planning. This mirrors the old "CONTEXT FROM ERIC" block. Write it verbatim — do not add your own analysis, mechanism opinions, or design assumptions. If the human has open questions they haven't answered, pass them through as open questions; do not answer them. Write the context to a temp file (e.g. `/tmp/ateam-ctx-<slug>.txt`) using the Write tool. If there is genuinely no additional context from the human, skip the file and omit `--body-file`.
- **Target repo.** The initiative may target a repo OTHER than the one this dispatcher session is sitting in — do not blindly assume cwd. Identify the target directory the human means (explicit in the invocation if they named one, otherwise the current directory). If that yields a single unambiguous repo, pass nothing (dispatch defaults to cwd). Pass `--repo <abs-path>` ONLY when you are not confident: cwd is not inside any repo, the problem clearly refers to a different project you cannot locate, or more than one repo plausibly fits.
- **Base branch.** Default is the repo's detected default branch (dispatch auto-detects). Pass `--base-branch <b>` only when the human implies a non-default base or there is genuine ambiguity — a wrong base is expensive to unwind.

### 3. Dispatch

Run a single call. Everything deterministic (slugify, git worktree add, initiative register, background DRI launch) is handled inside `ateam dispatch`:

```bash
ateam dispatch --problem "<one-line problem statement>" --body-file <tmpfile> [--repo <abs-path>] [--base-branch <branch>]
```

`--problem` is the one-line title. `--body-file` carries the context block you wrote in step 2 (schema lines come first automatically; the context is appended after them). Omit `--body-file` only when there is truly no additional context from the human to pass.

`dispatch` fail-fasts (non-zero exit) on: not-a-git-repo, empty slug, worktree-slug collision, or a `--body-file` path that cannot be read. It never prompts. On success it prints:

```
initiative_id: <id>
worktree: <abs-path>
slug: <slug>
base_branch: <branch>
```

**🚨 CARDINAL RULE.** `ateam dispatch` performs the ONE write to the global workspace (initiative registration via the same `register` path). All work beads (planner's decomposition, feature/task/discovery beads) live in the PROJECT repo and are created by the background DRI and its team, not here.

### 4. Report and hand off

Relay the output `dispatch` printed. Tell the human:

- The initiative id and the worktree path (from dispatch output).
- How to watch and control it:

```bash
claude agents                   # list background sessions
claude logs <session-id>        # recent output without attaching
claude attach <session-id>      # open it in this terminal
claude stop <session-id>        # abort early OR reap a finished idle session
```

When the background DRI finishes, it ends its turn and the session stays idle — it does NOT self-stop. It appears as idle in `claude agents`; use `claude stop <session-id>` to stop it when you are done with it.

Any human gate the background DRI parks on surfaces through `ateam human-list` and the `/initiatives` dashboard — so a needed decision is discoverable without tailing logs.

## Permissions

A backgrounded DRI has **no human attached to answer prompts**, so it runs with `--permission-mode bypassPermissions` (what `ateam dispatch` sets when launching the background DRI). This requires a **one-time interactive acceptance** of bypass mode on the machine first. A bypass-mode DRI edits a real repo unattended — only dispatch one for well-scoped work, and confirm with the human first when the initiative touches sensitive tooling or infrastructure.

Reference: https://code.claude.com/docs/en/agent-view
