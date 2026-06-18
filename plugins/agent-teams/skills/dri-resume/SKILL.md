---
name: dri-resume
description: Relaunch a background DRI for an ALREADY-REGISTERED, still-open initiative by id. Use when asked to "resume an initiative", "resume initiative <id>", "re-launch a parked or interrupted background DRI", "restart a background initiative", or when invoked as /dri-resume <id>. Does NOT create a new initiative — use /dri-dispatch for that. Requires an initiative id.
---

You relaunch a background DRI for an existing initiative — you do not register anything. This skill calls `ateam resume <id>`, which looks up the registered worktree, validates the initiative is still open, and fires a new background `/dri` session in it. The current session stays free.

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

### 2. Get the initiative id

Take it from the invocation (e.g. `/dri-resume at-abc`). If none was given, ask the human for the id. You may run `ateam human-list` to show open initiatives and help them pick, or mention that `/initiatives` gives a machine-wide dashboard.

Note: slug-based lookup and no-argument cwd-inference are not yet supported. An explicit id is required.

### 3. Resume

Run a single call:

```bash
ateam resume <id>
```

`ateam resume` looks up the registered worktree for the initiative, validates the initiative is still open (non-zero exit with a clear error if it is closed or the worktree is missing), and launches a background `/dri <id>` session in the worktree. It is prompt-free — the launch happens inside the binary (`Bash(ateam:*)`).

On success it prints:

```
initiative_id: <id>
worktree: <abs-path>
session_id: <claude-session-id>
```

### 4. Report and hand off

Relay the output `resume` printed. Tell the human:

- The initiative id and the worktree path (from resume output).
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

The relaunched background DRI runs with `--permission-mode bypassPermissions` (set by `ateam resume` when it launches the session). This requires a **one-time interactive acceptance** of bypass mode on the machine first.

Reference: https://code.claude.com/docs/en/agent-view
