---
name: dispatch-dri
description: Dispatch a new agent-teams initiative to a managed background Codex DRI. Use for /dispatch-dri, starting a separable initiative in the background, or handing work to a fresh DRI without occupying the current session. Creates a worktree, registers the initiative, and launches the Codex-native dri skill.
---

# Dispatch a Codex DRI

This session is a dispatcher, not the new DRI. Do not investigate the codebase,
grep files, design a solution, or answer open questions for the human. Capture
their framing verbatim and launch unconditionally once a problem statement and
target repo are known.

## Preflight

Run `ateam ws`, `ateam runtime check codex`, and `ateam audit`. If the tool,
standalone managed Codex runtime, plugin hooks, or role definitions are not
ready, direct the human to `agent-teams-codex:setup-agent-teams` and stop. If no
problem statement was supplied, ask only for that statement.

## Capture

- Use the human's one-line problem statement without embellishment.
- Put their constraints, decisions, background, and unanswered questions
  verbatim in a temporary body file. Omit it only when there is no extra
  context.
- Default the target repo to the unambiguous current git repository. Pass
  `--repo <absolute-path>` only when another target was named or cwd is not the
  target.
- Let dispatch detect the default branch unless the human named another.
- Pass `--standby` only when the human explicitly asked to park or wait.

## Dispatch

Run exactly one creation call:

```bash
ateam dispatch --runtime codex --problem "<one-line statement>" \
  [--body-file <file>] [--repo <absolute-path>] \
  [--base-branch <branch>] [--standby]
```

Do not use `--no-launch`. `ateam dispatch` creates the project worktree and
root epic, writes the one sanctioned global registry bead, and submits the
Codex DRI prompt through the managed app-server. Work beads are the DRI's job,
not the dispatcher's.

## Report

Relay the printed initiative id, worktree, slug, base branch, and event-log
path. Useful controls are:

```bash
tail -f <event-log-path>
ateam runtime open codex
ateam show <initiative-id>
ateam human-list
ateam resume <initiative-id> --runtime codex
```

The managed daemon may outlive any terminal process. The durable thread id is
stored as `session:` on the initiative. Mail first persists in Beads, then
wakes or steers that same thread; if submission fails it remains unread and
retryable.
