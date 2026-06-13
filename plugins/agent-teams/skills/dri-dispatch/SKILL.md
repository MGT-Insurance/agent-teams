---
name: dri-dispatch
description: Create a NEW agent-teams initiative and hand it to a background DRI. Use when asked to "start a new initiative in the background", "kick off / dispatch a background DRI", "spin off separable work as its own initiative", or when invoked as /dri-dispatch <problem statement>. Creates a dedicated worktree, registers the initiative, and launches a hands-off background DRI session that drives it to a PR. Does NOT make THIS session the DRI — use /dri for that.
---

You dispatch a new initiative; you do not become its DRI. This skill sets up a fresh, isolated checkout, registers the initiative in the global workspace, and launches a **background** `/dri` session that owns it end-to-end. The current session stays free.

Use this when:

- A human wants to kick off an initiative without tying up this session.
- A DRI (or any session) surfaces **separable** work — a discovery bead that is really its own feature, infra/tooling work, anything that would balloon the current scope. Do NOT absorb it; dispatch it here. The current initiative stays focused; the new one gets its own DRI, checkout, team, and PR.

For *becoming* the DRI in this session, use `/agent-teams:dri` instead.

## The `ateam` tool

`ateam` is on PATH — it ships as a prebuilt binary in the plugin's `bin/` (auto-added to PATH; installed/verified by `/setup-agent-teams`). Call it as bare `ateam` everywhere this document shows `ateam`. One allowlist entry covers all subcommands: `Bash(ateam:*)`.

**🚨 CARDINAL RULE.** The GLOBAL workspace (reached ONLY via `ateam`) holds ONLY initiative-tracking beads and role memories. Registering an initiative is the ONE write you make there, and `ateam register` is the only sanctioned way to do it — NEVER `bd -C <global> create`. All work beads (the planner's decomposition, feature/task/discovery beads) live in the PROJECT repo and are created by the background DRI and its team, not here.

## Steps

### 1. Preflight

- Verify `ateam` is on PATH: run `ateam ws`. If it errors or is not found, tell the human to run `/setup-agent-teams` and stop.
- Run `ateam audit`. It must report clean before you add anything.

### 2. Scope the initiative

The LLM's job here is **judgment only** — everything mechanical is handled by `ateam dispatch` in step 3.

- **Problem statement.** Take it from the invocation. If none was given, ask the human — this is the single load-bearing input.
- **Full context block.** Capture the human's complete framing: constraints, background, relevant decisions already made, and any clarifying-question seeds the background DRI should answer before planning. This mirrors the old "CONTEXT FROM ERIC" block. WHY: the background DRI has no human attached — it recovers all context from the initiative bead via `ateam show <id>`. A bare one-liner starves its clarify phase; a rich context block lets it proceed with the right assumptions. Write the context to a temp file (e.g. `/tmp/ateam-ctx-<slug>.txt`) using the Write tool. If there is genuinely no additional context, skip the file and omit `--body-file`.
- **Target repo.** The initiative may target a repo OTHER than the one this dispatcher session is sitting in — do not blindly assume cwd. Identify the target directory the human means (explicit in the invocation if they named one, otherwise the current directory). If that yields a single unambiguous repo, pass nothing (dispatch defaults to cwd). Pass `--repo <abs-path>` ONLY when you are not confident: cwd is not inside any repo, the problem clearly refers to a different project you cannot locate, or more than one repo plausibly fits.
- **Base branch.** Default is the repo's detected default branch (dispatch auto-detects). Pass `--base-branch <b>` only when the human implies a non-default base or there is genuine ambiguity — a wrong base is expensive to unwind.

### 3. Dispatch

Run a single call. Everything deterministic (slugify, git worktree add, initiative register, background DRI launch) is handled inside `ateam dispatch`:

```bash
ateam dispatch --problem "<one-line problem statement>" --body-file <tmpfile> [--repo <abs-path>] [--base-branch <branch>]
```

`--problem` is the one-line title. `--body-file` carries the full context block you wrote in step 2 (schema lines come first automatically; the context is appended after them). Omit `--body-file` only when there is truly no additional context to pass.

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
claude stop <session-id>        # abort it early (the DRI self-stops when done — you only need this to cancel)
```

The background DRI self-stops its own session when it finishes — after Phase 6 teardown is complete, it runs `claude stop <its-own-id>` as its final action and will appear as `stopped` in `claude agents`. You do NOT need to stop it manually when it completes normally; `claude stop <session-id>` is only needed to abort early.

Any human gate the background DRI parks on surfaces through `ateam human-list` and the `/initiatives` dashboard — so a needed decision is discoverable without tailing logs.

## Permissions

A backgrounded DRI has **no human attached to answer prompts**, so it runs with `--permission-mode bypassPermissions` (what `ateam dispatch` sets when launching the background DRI). This requires a **one-time interactive acceptance** of bypass mode on the machine first. A bypass-mode DRI edits a real repo unattended — only dispatch one for well-scoped work, and confirm with the human first when the initiative touches sensitive tooling or infrastructure.

Reference: https://code.claude.com/docs/en/agent-view
