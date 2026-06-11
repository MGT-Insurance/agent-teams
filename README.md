# agent-teams

DRI-led multi-agent software delivery for Claude Code: one session acts as the **DRI** (directly responsible individual) for an initiative, orchestrating a background team of role agents — planner, implementers, tester, reviewer — through a beads-tracked plan to a pushed branch and an opened PR. Never merges.

Distilled from a real delivery session; the playbook's rules (verify-don't-trust, gate protocol, ephemeral implementers, discovery loop) each earned their place by catching a real failure.

## Requirements

- [beads](https://github.com/gastownhall/beads) (`bd`) — hard dependency, no fallback.
- Claude Code with plugins enabled.

## Install

```
/plugin marketplace add erlloyd/agent-teams
/plugin install agent-teams@agent-teams
/setup-agent-teams        # one-time per machine: creates/clones the global workspace
```

(For local development: `/plugin marketplace add /path/to/agent-teams`.)

## Use

- `/dri <problem statement>` — run an initiative end-to-end in the current worktree. Interactive: you'll be asked to approve the plan and answer load-bearing questions.
- `/dri` in a worktree with an open initiative — resume it.
- `/initiatives` — machine-wide dashboard: what's running, what's parked waiting on you.

### Headless spawn (one initiative per worktree)

```bash
git worktree add ../myrepo-featx -b feat/x main
cd ../myrepo-featx
claude --bg "/dri <problem statement>" --permission-mode acceptEdits
```

The session shows up in `claude agents`; attach to answer gates (`claude attach <id>`), or watch `/initiatives` for parked questions. Parked gates never stop work that doesn't depend on the answer.

## Concepts

- **Global workspace** (`${AGENT_TEAMS_HOME:-$HOME/.agent-teams}`): a git-backed beads workspace. Role learnings (`<role>:<slug>` memories — every planner learns from every planner) and the initiative registry (one issue per initiative; `bd human` flags = "waiting on a human"). Syncs across machines via its git remote.
- **Roles:** planner (opus) plans as beads; implementers (sonnet, ephemeral) write code + unit tests in isolated worktrees; tester runs suites + live verification; reviewer reviews independently and runs the CI gate. All file `discovery` beads; the DRI triages them.
- **Prime directive:** deliver a PR that solves the problem — investigating beats asking; asking beats delivering wrong.

## Roadmap

- `plugins/overseer/` — a meta-orchestrator watching every initiative on the machine (feeds on the registry).
- `cli/` — `ateam` CLI codifying the conventions (gate, register, teardown, spawn) so they can't drift.
- Learning curation — synthesis/dedup over the role memories.
