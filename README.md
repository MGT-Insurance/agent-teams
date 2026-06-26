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
- **Role-memory model:** memories use a three-tier key convention — fresh (default write tier; accumulates between condense runs), hot (curated, auto-injected into every role session via `ateam learnings <role>`), and cold (searchable on demand via `ateam recall <role> <query>`; not auto-injected). `ateam learn <role> <slug>` writes fresh; `ateam condense <role>` curates hot; `ateam fresh-drain <role>` moves uncurated fresh into cold. Full mechanics in `plugins/agent-teams/CLAUDE.md`.

## Cross-session messaging

Sessions message each other through a durable, Dolt-synced mailbox — a message survives a crash and reaches a recipient on another machine after `bd dolt pull`.

- **Send** — `ateam send <recipient-initiative-id> --file <body>` (the recipient is addressed by its **initiative id**) writes the message and rings a doorbell that wakes the recipient if it has gone idle. If no live session exists, it escalates to `ateam resume`.
- **Receive** — `ateam inbox` drains unread messages, but you don't run it by hand: a hook fires it every turn and at session start, so incoming mail just appears in context.
- **Debug** — `ateam debug-mail` prints a read-only table of every initiative's recent mail. It's an observability view for humans or agents debugging the system — it does not mark anything read, and is not how a session reads its own mail.

## Worktree setup hooks

When an agent creates a fresh track worktree, gitignored files (env files, creds, local config) are not present. Most work doesn't need them. When a worktree does need live env (running a dev server, creds-dependent validation), run:

```bash
ateam worktree-setup <abs-worktree-path>
```

The hook is registered once per repo; the reference implementation is `scripts/midgard-worktree-setup.sh`. A missing or failing hook is non-fatal.

## Development / Contributing

The repo ships two artifacts:

- **`ateam` Go CLI** (`cmd/ateam/`): the workspace CLI. Shipped as committed per-platform binaries in `plugins/agent-teams/bin/` (`ateam-{darwin,linux}-{amd64,arm64}`); `bin/ateam` is a POSIX wrapper that selects the right one.
- **Claude Code plugin** (`plugins/agent-teams/`): the `/dri` playbook, role agents, hooks, and skills.

### ateam command surface

The plugin's slash commands wrap these `ateam` verbs; agents and the DRI also call them directly.

| verb | run by | purpose |
|------|--------|---------|
| `dispatch`, `resume`, `new-initiative` | human / DRI | launch or relaunch a background DRI session |
| `list`, `show`, `human-list` | human / agent | inspect open initiatives and parked gates |
| `register`, `gate`, `clear-gate`, `note`, `close` | DRI | initiative lifecycle |
| `send`, `inbox` | agent | cross-session mail (`inbox` drains automatically each turn) |
| `debug-mail` | human / agent | read-only audit table of every initiative's recent mail (debugging) |
| `learn`, `learnings`, `recall`, `forget` | role agents | role-memory read and write |
| `condense`, `fresh-drain` | DRI | role-memory curation |
| `sync`, `pull` | DRI | sync the global workspace |
| `worktree-setup` | agent | hydrate a fresh track worktree |

**Build and test:**

```bash
go build ./...
go vet ./...
go test ./...
gofmt -l <files>                   # must produce no output
sh scripts/build-binaries.sh       # rebuild the committed per-platform binaries
bash tests/<name>.test.sh          # run individual shell-level tests
```

**Release protocol — required on ANY CLI or plugin change:**

1. Run `sh scripts/build-binaries.sh` and commit the updated `plugins/agent-teams/bin/`.
2. Bump the version in BOTH `plugins/agent-teams/.claude-plugin/plugin.json` and `.claude-plugin/marketplace.json` (currently **0.31.0** — keep both files identical).

`claude plugin update` keys off the version: no bump means installed sessions silently keep the old copy. A source-only PR that changes `ateam` behavior or plugin content without rebuilding binaries and bumping the version is incomplete.
