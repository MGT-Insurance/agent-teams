---
name: resume-dri
description: Resolve a natural-language description to an open initiative and relaunch its background DRI. Use when asked to "resume the initiative about X", "restart the <topic> work", "resume an initiative", "pick up where we left off on <description>", or when invoked as /resume-dri <description-or-id>. Accepts a free-text description (fuzzy-matches open initiatives) or an explicit id. Does NOT create a new initiative — use /dispatch-dri for that.
---

You relaunch a background DRI for an existing initiative — you do not register anything. This skill resolves a description (or explicit id) to an open initiative, then calls `ateam resume <id>`, which looks up the registered worktree, validates the initiative is still open, and fires a new background `/dri` session in it. The current session stays free.

Use this when:

- A background DRI session ended (crashed, was stopped, or drifted idle) but the initiative is still open and needs to continue.
- You parked an initiative to wait on a dependency and want to restart it now that the blocker is cleared.
- Any parked-or-interrupted initiative surfaces in `ateam human-list` or `/initiatives` and needs a new DRI session.

For *dispatching a brand-new initiative*, use `/dispatch-dri` instead. For *becoming* the DRI in this session, use `/agent-teams:dri`.

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

`ateam resume` looks up the registered worktree for the initiative, validates the initiative is still open (non-zero exit with a clear error if it is closed, the worktree is missing, or its repo is not opted in — exit 6), and launches a background `/dri <id>` session in the registered worktree. It is prompt-free itself — the launch happens inside the binary (`Bash(ateam:*)`); recovering from exit 6, if needed, is this skill's job (below).

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

**Recovery: repo not opted in (exit 6).** If `ateam resume` exits 6 (stderr contains `agent-teams is not enabled for <repo>`), the initiative's registered repo has no `.agent-teams` opt-in marker, or its marker carries `disabled: true`. Opting a repo in is consent to run unattended bypass-permissions agents there — that decision belongs to the human, never to this skill.

- **Human attached:** ask whether to opt the repo in — say plainly that this lets agent-teams create worktrees and run background sessions with bypassed permissions there. On yes, run `ateam enable-repo <repo>` using the absolute path the refusal printed (don't re-derive it — resume gates on the initiative's registered `repo:` field, which can differ from cwd in a worktree). Relay the `enabled: ...` line verbatim so the human knows whether it created the marker or removed an existing `disabled: true`. If `enable-repo` itself fails (e.g. a permissions error), report that and stop — don't retry the original command against an unchanged repo. Otherwise, retry the original `ateam resume <id>` call ONCE. A second refusal is a real failure — report it and stop, do not loop. On no: stop and tell the human nothing was created.
- **No human attached:** do NOT create the marker and do NOT prompt. Report the refusal verbatim to the caller and stop; a blocking prompt would hang a headless session forever.
- Either way, `.agent-teams` is a real file in the target repo — it will show up in `git status`. Whether to commit it is the human's call, not this skill's.

### 4. Report and hand off

The watch and control commands are printed by `ateam resume` — relay them to the human as-is. No need to look up the session name separately.

When the background DRI finishes, it ends its turn and the session stays idle — it does NOT self-stop. It appears as idle in `claude agents`; use `claude stop <session-id>` to stop it when you are done with it.

Any human gate the background DRI parks on surfaces through `ateam human-list` and the `/initiatives` dashboard — so a needed decision is discoverable without tailing logs.

## Permissions

The relaunched background DRI runs with `--permission-mode bypassPermissions` (set by `ateam resume` when it launches the session). This requires a **one-time interactive acceptance** of bypass mode on the machine first.

Reference: https://code.claude.com/docs/en/agent-view
