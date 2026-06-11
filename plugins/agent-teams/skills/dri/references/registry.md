# Initiative registry — schema and commands

The registry lives in the global workspace: one bd ISSUE per initiative (not per session).

## Description schema (line-oriented; the compaction hook greps `worktree:`)

    problem: <one-line problem statement>
    repo: <abs path to main repo>
    worktree: <abs path of the checkout the DRI owns>
    branch: <branch name>
    team: <team slug>
    mode: interactive|bg

## Commands

Write the body to a temp file first (avoids the newline-# safety prompt), then:

    bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" create \
      --title="<problem statement, short>" --type=task --priority=2 \
      --body-file=/tmp/initiative-body.txt

- Resume match: `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" list --status=open --json` and select where description contains `worktree: $PWD` — use exact-line matching to avoid prefix collisions:

      jq -r --arg wt "worktree: $PWD" \
        '.[] | select((.description // "") | split("\n") | any(. == $wt)) | .id'

  Note: `bd search "<text>"` does NOT search description body content — it only matches titles. Do not use it as a fallback; always use `list --json | jq`.

- Phase changes and session starts: `bd note <id> ...` (file-based for multi-line).
- Close on delivery: `bd close <id> --reason="delivered: <PR URL>"`.

Project-repo beads may also be human-flagged for local detail, but the GLOBAL initiative flag is the canonical "waiting on a human" signal — always raise gates there.
