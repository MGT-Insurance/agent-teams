# agent-teams

Multi-agent software delivery for Claude Code. One session acts as the **DRI** (directly responsible individual) for an initiative: it plans the work as beads, runs a background team of role agents — planner, implementers, tester, reviewer — and drives the initiative to a reviewable pull request. You review and merge.

## Requirements

- [beads](https://github.com/gastownhall/beads) (`bd`) — hard dependency, no fallback.
- Claude Code with plugins enabled.

## Install

```
/plugin marketplace add mgt-insurance/agent-teams
/plugin install agent-teams@agent-teams
/setup-agent-teams        # one-time per machine: creates/clones the global workspace
```

(For local development: `/plugin marketplace add /path/to/agent-teams`.)

## Use

- `/dri <problem statement>` — make the current session the DRI for an initiative and run it end-to-end in the current worktree. Interactive: you approve the plan and answer load-bearing questions.
- `/dri` in a worktree with an open initiative — resume it.
- `/dri-dispatch <problem statement>` — register a *new* initiative in its own worktree and hand it to a hands-off background DRI. Use it to split off separable work without derailing what you're on; the dispatched DRI drives its own initiative to a PR independently.
- `ateam resume <id>` — one-command re-launch of a parked or interrupted background initiative by id. Looks up the registered worktree, validates the initiative is still open, and fires a new background DRI session in it.
- `/initiatives` — machine-wide dashboard: what's running, what's parked waiting on you.

### Headless spawn

`/dri-dispatch` is the easiest way to launch a background initiative. To spawn one by hand:

```bash
git worktree add ../myrepo-featx -b feat/x main
cd ../myrepo-featx
claude --bg --dangerously-skip-permissions "/dri <problem statement>"
```

`--dangerously-skip-permissions` is required for hands-off operation: the DRI runs without permission prompts and spawns teammates with `mode: bypassPermissions`. **Safety note:** bypass means agents run commands unprompted — the guardrails are worktree isolation (each teammate is confined to its own worktree) and role boundaries (teammates only commit to their own track; the DRI owns branch integration and opens the PR; merging stays a human decision). The DRI skill enforces these.

The session shows up in `claude agents`; attach to answer gates (`claude attach <id>`), or watch `/initiatives` for parked questions. Parked gates never stop work that doesn't depend on the answer.

## Concepts

- **Global workspace** (`${AGENT_TEAMS_HOME:-$HOME/.agent-teams}`): a git-backed beads workspace. Role learnings (`<role>:<slug>` memories — every planner learns from every planner) and the initiative registry (one issue per initiative; `bd human` flags = "waiting on a human"). Syncs across machines via its git remote.
- **Roles:** planner (opus) plans as beads; implementers (sonnet, ephemeral) write code + unit tests in isolated worktrees; tester runs suites + live verification (including Playwright MCP browser tools for UI checks — available in `claude --bg` sessions via MCP inheritance); reviewer reviews independently and runs the CI gate. All file `discovery` beads; the DRI triages them.
- **Prime directive:** deliver a PR that solves the problem — investigating beats asking; asking beats delivering wrong.
- **Lifecycle:** the DRI drives to an opened PR, then leaves the initiative open in an `awaiting-merge` state; a resume after the PR merges closes it out. Opening the PR is delivery — merging is yours.

## Worktree setup hooks

When an agent creates a fresh track worktree, it's clean by design — gitignored files (`.vercel` project link, environment files, local-only config) are not present. **Most work doesn't need them, and the hook can be heavy** (`vercel env pull`, copying creds) — so this is **on-demand, not routine**. Run it only when a worktree actually needs live env: running a dev server, creds-dependent validation (e.g. socotra), or a pre-commit hook that requires it.

When you do need it, run it after `pnpm install` (the hook's `env:pull` needs `node_modules`):

```bash
ateam worktree-setup <abs-worktree-path>
```

The verb looks up `<AGENT_TEAMS_HOME>/worktree-hooks/<repo-key>` (repo-key = slugified basename of the main checkout (the source checkout behind the worktree)). If a hook file exists, its contents (the absolute path to the repo's setup script) are used to run `<script> <wtPath> <srcCheckout>`. The script receives both the new worktree path and the source checkout path as arguments, so it can copy gitignored files and pull creds without modifying the source repo.

**Non-fatal by design.** A missing hook file prints an informational message and exits 0. A script failure prints a loud warning to stderr but the verb still exits 0 — a broken hook never blocks worktree creation.

**Registering a hook** is one-time per repo: write the absolute path to the repo's setup script into `<AGENT_TEAMS_HOME>/worktree-hooks/<repo-key>`. The reference implementation for midgard is `scripts/midgard-worktree-setup.sh` in this repo — it copies gitignored env files from the source checkout and runs `vercel env pull` to restore creds-dependent tooling. See `/setup-agent-teams` step 8 for the exact registration command.

## Roadmap

- `plugins/overseer/` — a meta-orchestrator watching every initiative on the machine (feeds on the registry).
- `cli/` — `ateam` CLI codifying the conventions (gate, register, teardown, spawn) so they can't drift.
- Learning curation — synthesis/dedup over the role memories.
