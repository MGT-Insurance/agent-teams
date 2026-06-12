# Spawning a sibling initiative as a background DRI

When separable work surfaces mid-initiative — a discovery bead that is really its
own feature, infra/tooling work, anything that would balloon the current
initiative's scope — do NOT expand this initiative to absorb it. Register it and
dispatch a **fresh background DRI session** for it. The current session stays
focused; the new initiative gets its own DRI, checkout, team, and PR.

The mechanism is the Claude Code CLI `--bg` flag: it launches a detached session,
supervised by a daemon, that keeps running after you close the terminal. It is the
same primitive behind any backgrounded Claude session (a bg DRI is just `--bg`
running the `/dri` slash command).

## Launch

1. **Give the new initiative its own checkout** (the DRI owns its checkout
   exclusively). Create a worktree under the canonical DRI worktree root so it is
   already inside the pre-approved `additionalDirectories` (see the permission
   profile in `/setup-agent-teams`):

   ```bash
   git -C <project-repo> worktree add "${AGENT_TEAMS_HOME}-worktrees/<slug>" -b <branch> <base-branch>
   ```

2. **Dispatch the background DRI**, passing the problem statement as a `/dri`
   slash command. The new session reads its settings + permission mode from
   `--cwd` and registers/owns its own initiative on startup (Phase 1):

   ```bash
   claude --bg --name <slug> --cwd "${AGENT_TEAMS_HOME}-worktrees/<slug>" \
     "/dri '<problem statement>'"
   ```

   It prints a session id and the management commands.

## Permissions

A backgrounded DRI has **no human attached to answer prompts**. Either:

- `--permission-mode bypassPermissions` (alias `--dangerously-skip-permissions`)
  for hands-off operation. This needs a **one-time interactive acceptance** of
  bypass mode on the machine first. A bypass-mode bg DRI edits a real repo
  unattended — only dispatch one for well-scoped work, and confirm with the human
  when it touches sensitive tooling.
- Otherwise it inherits `--cwd`'s settings and will **park on the first prompt** it
  cannot answer — fine only if you are watching its output.

## Monitor and control

```bash
claude agents          # list background sessions (add --json for scripting)
claude logs <id>       # recent output without attaching
claude attach <id>     # open the session in this terminal
claude stop <id>       # stop it
claude respawn <id>    # restart it
```

Any human gate the bg DRI parks on also surfaces through `<ateam> human-list` and
the `/initiatives` dashboard — so you discover "it needs a decision" without
tailing its logs.

From inside a running session you can background the current task mid-conversation
with the `/bg "<next task>"` slash command.

Reference: https://code.claude.com/docs/en/agent-view

## Note

This is the raw CLI mechanism. It is expected to be wrapped by the `ateam` CLI in
a later workstream (e.g. an `ateam spawn <slug> "<problem>"` that creates the
worktree, registers the initiative, and dispatches the bg DRI in one step). Until
then, use the commands above.
