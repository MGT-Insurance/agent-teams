---
name: dri-resume
description: Resolve a natural-language description to an open initiative and relaunch its background DRI. Use when asked to "resume the initiative about X", "restart the <topic> work", "resume an initiative", "pick up where we left off on <description>", or when invoked as /dri-resume <description-or-id>. Accepts a free-text description (fuzzy-matches open initiatives) or an explicit id. Does NOT create a new initiative — use /dri-dispatch for that.
---

You relaunch a background DRI for an existing initiative — you do not register anything. This skill resolves a description (or explicit id) to an open initiative, then calls `ateam resume <id>`, which looks up the registered worktree, validates the initiative is still open, and fires a new background `/dri` session in it. The current session stays free.

Use this when:

- A background DRI session ended (crashed, was stopped, or drifted idle) but the initiative is still open and needs to continue.
- You parked an initiative to wait on a dependency and want to restart it now that the blocker is cleared.
- Any parked-or-interrupted initiative surfaces in `ateam human-list` or `/initiatives` and needs a new DRI session.

For *dispatching a brand-new initiative*, use `/dri-dispatch` instead. For *becoming* the DRI in this session, use `/agent-teams:dri`.

## The `ateam` tool

`ateam` is on PATH — it ships as a prebuilt binary in the plugin's `bin/` (auto-added to PATH; installed/verified by `/setup-agent-teams`). Call it as bare `ateam` everywhere this document shows `ateam`. One allowlist entry covers all subcommands: `Bash(ateam:*)`.

## Steps

### 1. Preflight

Verify `ateam` is on PATH: run `ateam ws`. If it errors or is not found, tell the human to run `/setup-agent-teams` and stop.

### 2. Resolve the initiative

This is the core step — identifying which initiative to resume.

**Case A — no argument given.** Run `ateam list-json`, filter to `status == "open"`, and present the list (id + title each). Ask the human which one to resume.

**Case B — explicit id given** (arg matches the `at-xxx` pattern and resolves in `ateam list-json`). Use it directly; skip to step 3.

**Case C — free-text description given** (everything else). Run `ateam list-json`, filter to `status == "open"`, and match the description against each initiative's `title` and `description` fields. Rank candidates by relevance.

- **One clear match:** show it (`<id> — <title>`) and confirm with the human before launching. Resuming starts a background DRI session — don't silently launch on a guess.
- **Multiple plausible matches:** present the shortlist (id + title each) and ask the human to pick.
- **No match:** say so and show the full open list (id + title) so the human can pick or clarify.

Note: slug-based lookup and cwd-inference are separate gated features; description-resolution is what this skill adds.

### 3. Resume

Once an id is settled, run a single call:

```bash
ateam resume <id>
```

`ateam resume` looks up the registered worktree for the initiative, validates the initiative is still open (non-zero exit with a clear error if it is closed or the worktree is missing), and launches a background `/dri <id>` session in the registered worktree. It is prompt-free — the launch happens inside the binary (`Bash(ateam:*)`).

On success, `ateam resume` prints a confirmation block to stdout:

```
initiative_id: <id>
worktree: <abs-path>

Background session launched: <session-name>

Watch and control:
  claude agents          # list background sessions
  claude logs <session-name>         # recent output without attaching
  claude attach <session-name>       # open it in this terminal
  claude stop <session-name>         # abort it early
```

The session name is the basename of the registered worktree directory.

### 4. Report and hand off

The watch and control commands are printed by `ateam resume` — relay them to the human as-is. No need to look up the session name separately.

When the background DRI finishes, it ends its turn and the session stays idle — it does NOT self-stop. It appears as idle in `claude agents`; use `claude stop <session-id>` to stop it when you are done with it.

Any human gate the background DRI parks on surfaces through `ateam human-list` and the `/initiatives` dashboard — so a needed decision is discoverable without tailing logs.

## Permissions

The relaunched background DRI runs with `--permission-mode bypassPermissions` (set by `ateam resume` when it launches the session). This requires a **one-time interactive acceptance** of bypass mode on the machine first.

Reference: https://code.claude.com/docs/en/agent-view
