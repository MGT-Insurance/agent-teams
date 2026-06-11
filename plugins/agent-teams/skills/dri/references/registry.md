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

    ~/.agent-teams/bin/at register --title "<problem statement, short>" --file /tmp/initiative-body.txt

This prints the new issue id on stdout.

- Resume match: `~/.agent-teams/bin/at resume-match "$PWD"` — prints the id of the open initiative whose description contains an exact `worktree: <path>` line, or nothing on no match. Exact-line matching avoids prefix collisions (e.g. `/a/b` matching `worktree: /a/b/c`).

  Note: `bd search "<text>"` does NOT search description body content — it only matches titles. Do not use it as a fallback.

- Phase changes and session starts: `~/.agent-teams/bin/at note <id> --file <file>`.
- Close on delivery: `~/.agent-teams/bin/at close <id> --reason "delivered: <PR URL>"`.

Project-repo beads may also be human-flagged for local detail, but the GLOBAL initiative flag is the canonical "waiting on a human" signal — always raise gates there.
