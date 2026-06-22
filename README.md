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
- `/dri-resume <description-or-id>` — one-command re-launch of a parked or interrupted background initiative. Resolves your description to the matching open initiative and relaunches its background DRI; accepts an explicit id too.
- `/initiatives` — machine-wide dashboard: what's running, what's parked waiting on you.

### Headless spawn

`/dri-dispatch` is the easiest way to launch a background initiative; `/dri-resume <description-or-id>` relaunches one. To spawn either by hand directly via the CLI: for a new initiative use `ateam dispatch`; to resume an existing one use `ateam resume <id>`. To spawn a new initiative entirely by hand:

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

## Memory priming hooks

Role learnings and user preferences live in the global workspace (`ateam learn …`),
but something has to load them into a session. Two mechanisms:

- **SessionStart user-preference prime (shipped).** A `SessionStart` hook
  (`hooks/scripts/prime-user-memories.sh`, matchers `startup|resume|clear|compact`)
  runs `ateam prime`, which emits the `user:`-prefixed memories (cross-project
  working-style preferences) as a concise capped block that the harness injects
  into every session's context. Before this, `ateam learn user …` had no consumer —
  user memories were written but never loaded. The hook coexists with the existing
  `compact`-only recovery hook (a separate entry; Claude Code runs all matching
  entries). `ateam prime` filters by key prefix `user:` (not a substring match),
  caps at 12 memories / ~300 chars each, and emits nothing when there are none.

- **Deterministic agent-start priming (finding + planned wiring).** Role learnings
  are loaded today by *instructing* each role agent, in its prompt, to run
  `ateam learnings <role>` as step 1 — prompt-dependent and non-deterministic.
  Claude Code exposes a **`SubagentStart`** hook event
  ([hooks reference](https://code.claude.com/docs/en/hooks)) that fires before a
  spawned subagent's first prompt, injects via stdout / `additionalContext`, and
  matches by `agent_type` — the deterministic mechanism we want. **Constraint:**
  plugin-shipped subagents *ignore* the frontmatter `hooks:` field for security
  reasons ([sub-agents](https://code.claude.com/docs/en/sub-agents),
  [plugins-reference](https://code.claude.com/docs/en/plugins-reference)), so the
  hook must live in `hooks/hooks.json`, not in an agent's `.md` frontmatter. The
  plan: a `SubagentStart` hook runs `ateam learnings <agent_type>` and injects it,
  letting the prompt-step-1 instruction be removed once live firing is verified.

## Cross-session messaging

Deliver a message to a running session from *outside* it — a PR-shepherd conductor, an overseer, or a peer DRI — including waking a session that has gone idle. A generalization of the human-gate protocol: any sender, plus a wake. Design doc: [`docs/2026-06-19-cross-session-messaging-design.md`](docs/2026-06-19-cross-session-messaging-design.md).

- **Send.** `ateam send <recipient-initiative-id> --file <body> [--sender <id>] [--thread <id>]` writes a beads `type=message` bead (addressed by `assignee` = recipient initiative id; ephemeral, excluded from `bd ready`/`list`) and rings a per-recipient doorbell file `~/.agent-teams/mailbox/<id>.wake`. If `claude agents --json` shows no live session, it escalates to `ateam resume`.
- **Drain.** `ateam inbox` resolves the recipient by `worktree: $PWD`, prints unread messages as a `<system-reminder>` (adds a `read` label, keeps the bead open) and writes a two-phase delivery ack. It runs per-turn via a `UserPromptSubmit` hook and at session start via `SessionStart` — at-least-once, idempotent.
- **Wake (the idle case).** A per-initiative singleton `asyncRewake` `Stop` hook (`hooks/scripts/wake-watcher.sh`) arms a dependency-free poll-loop on the doorbell when a session goes idle. A doorbell write wakes the session within ~1s (the `Stop`-hook process exits 2, which Claude Code treats as a re-wake). A heartbeat re-arms before the hook timeout reaps it (defaults: **4h heartbeat / 24h timeout**), so the session stays reachable while the initiative is open and self-silences once it's closed. The 24h timeout survives normal laptop sleeps; only a continuous >24h close drops the watcher.

The mailbox is durable and git/Dolt-synced, so messages survive crashes and reach a recipient on another machine after `bd dolt pull`. Verified end-to-end against the live plugin (see [`docs/verifications.md`](docs/verifications.md)).

## Roadmap

- `plugins/overseer/` — a meta-orchestrator watching every initiative on the machine (feeds on the registry).
- `cli/` — `ateam` CLI codifying the conventions (gate, register, teardown, spawn) so they can't drift.
- Learning curation — synthesis/dedup over the role memories.
