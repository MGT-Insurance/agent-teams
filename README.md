# agent-teams

Multi-agent software delivery for Claude Code. One session acts as the **DRI** (directly responsible individual) for an initiative: it plans the work as beads, runs a background team of role agents — planner, implementers, tester, reviewer, investigators — and drives the initiative to a reviewable pull request. A persistent **Steward** session watches every initiative on the machine and is your single conversational counterpart for gates and decisions; you review and merge.

New to agent-teams? Start with [GETTING-STARTED.md](GETTING-STARTED.md) for an end-to-end walkthrough.

## Requirements

- [beads](https://github.com/gastownhall/beads) (`bd`) — hard dependency, no fallback.
- Claude Code with plugins enabled.

## Install

```
/plugin marketplace add MGT-Insurance/agent-teams
/plugin install agent-teams@agent-teams
/setup-agent-teams        # one-time per machine: creates/clones the global workspace
```

(For local development: `/plugin marketplace add /path/to/agent-teams`.)

The plugin also declares two config options in its manifest (`use_advisors`, `dri_model`; see the `userConfig` block in `plugins/agent-teams/.claude-plugin/plugin.json`) that pick which model a background DRI actually runs on — `dri_model` (default `claude-opus-4-8`) is the DRI's model; `use_advisors` (default off) instead runs the DRI on sonnet with `dri_model` attached as an advisor.

## Enable a repo

A repo is agent-teams-enabled only when a `.agent-teams` file exists at its root — not to be confused with the global `~/.agent-teams` workspace, which is machine-wide and unrelated. Its contents are ignored except for one line: `disabled: true`, with `disabled` at the very start of the line. Everything else — empty file, comments, unrelated prose — leaves the repo enabled.

A missing file has the identical effect to `disabled: true`: not enabled. `disabled: true` is a live kill switch — no commit, no restart, read fresh on every call — and it doesn't close any initiative that's already open.

Commit the file. It's read straight off disk, so an untracked `.agent-teams` enables only the checkout it's sitting in, not the repo. Nothing creates it for you.

When a repo isn't enabled, `ateam dispatch` and `ateam resume` refuse loudly — `agent-teams is not enabled for ...`, non-zero exit. Everything hook-driven goes quiet instead: resolving a session to its initiative, mail wakeups, and PR-event routing all skip without erroring. So a repo switched off mid-flight reads less like a refusal than like agent-teams having quietly stopped.

## Machine-local instructions

A human can give a role standing custom direction by hand-writing a file at `$AGENT_TEAMS_HOME/instructions/<role>.md`, served to a spawning role agent by `ateam instructions <role>`. It's machine-local: the file lives outside the global workspace's beads DB, so it needs no check-in and doesn't sync to other machines the way `ateam learn` memories do.

If cross-machine sharing IS wanted, `git add`-ing the file into the `~/.agent-teams` workspace repo is an available choice — nothing about the mechanism forces machine-locality, it's just the default because nothing creates or commits the file for you.

Content over 4096 bytes is refused rather than truncated: the human sees a loud marker naming the file and its size instead of a silently cut-off instruction. The instructions extend a role's shipped definition — they can't override its hard guardrails.

Today only the reviewer role fetches this on spawn; the other roles don't yet self-fetch it.

## Use

- `/dri <problem statement>` — make the current session the DRI for an initiative and run it end-to-end in the current worktree. Interactive: you approve the plan and answer load-bearing questions.
- `/dri` in a worktree with an open initiative — resume it.
- `/dispatch-dri <problem statement>` — register a *new* initiative in its own worktree and hand it to a hands-off background DRI. Use it to split off separable work without derailing what you're on; the dispatched DRI drives its own initiative to a PR independently.
- `/resume-dri <description-or-id>` — one-command re-launch of a parked or interrupted background initiative. Resolves your description to the matching open initiative and relaunches its background DRI; accepts an explicit id too.
- `/initiatives` — machine-wide dashboard: what's running, what's parked waiting on you.
- `/bg-session [prompt] [dir]` — launch a bare background Claude session with no `ateam`, no beads, no worktree, no initiative registration. A deliberate escape hatch for work that isn't a tracked initiative (e.g. running a dev server); use `/dispatch-dri` for real feature work.
- `/dispatch-review-pr <PR>` — register a review initiative for a GitHub PR (URL, `owner/repo#123`, or a bare number) and launch a background review session.
- `/agent-teams:steward` — start or act as the machine's Steward (see [Steward](#steward)).
- `/condense` — curate hot/cold learnings, then drain what is left of the fresh tier; can be triggered manually or runs automatically at DRI wind-down. A role is skipped unless its fresh tier has accumulated enough NEW material to be worth a pass.

### Headless spawn

`/dispatch-dri` is the easiest way to launch a background initiative; `/resume-dri <description-or-id>` relaunches one. Both wrap CLI verbs you can also call directly:

```bash
ateam dispatch --problem "<problem statement>"    # new initiative: worktree + registration + background DRI
ateam resume <id>                                 # relaunch an existing initiative's background DRI
```

Both launch the background session with `--permission-mode bypassPermissions` for hands-off operation: the DRI runs without permission prompts and spawns teammates with `mode: bypassPermissions`. **Safety note:** bypass means agents run commands unprompted — the guardrails are worktree isolation (each teammate is confined to its own worktree) and role boundaries (teammates only commit to their own track; the DRI owns branch integration and opens the PR; merging stays a human decision). The DRI skill enforces these.

The session shows up in `claude agents`; attach to answer gates (`claude attach <id>` — the short id from that listing, not the session name, which does not resolve), or watch `/initiatives` for parked questions. Parked gates never stop work that doesn't depend on the answer.

## Eval suite

`eval/` is an A/B comparison harness for agent-teams configurations (model, advisor on/off) across cost, latency, tool calls, turns, and correctness. Run it via `scripts/eval` (never installed on agent PATHs by design). See [`eval/README.md`](eval/README.md) for the full lifecycle.

⚠️ **Costs real money:** `eval run` dispatches a real, autonomous agent team, and `eval collect`'s LLM judge call spends real API dollars too. Neither is part of the test suite — never run either casually or in CI.

## Concepts

- **Global workspace** (`${AGENT_TEAMS_HOME:-$HOME/.agent-teams}`): a git-backed beads workspace. Role learnings (`<role>:<slug>` memories — every planner learns from every planner) and the initiative registry (one issue per initiative; a `human` label = "waiting on a human" — enumerate with `bd human list` or `ateam human-list`). Syncs across machines via its git remote.
- **Roles:** planner (opus) plans as beads; implementers (sonnet, ephemeral) write code + unit tests in isolated worktrees; tester runs suites + live verification (including `@playwright/cli` browser automation for UI checks — it's plain Bash, so it works in any session, including `claude --bg`, with no MCP wiring); reviewer reviews independently and runs the CI gate; investigators (opus, ephemeral) each answer one bounded question and return an evidence-backed brief, fanned out in parallel on disjoint charges. All file `discovery` beads; the DRI triages them.
- **Prime directive:** deliver a PR that solves the problem — investigating beats asking; asking beats delivering wrong.
- **Lifecycle:** the DRI drives to an opened PR, then leaves the initiative open in an `awaiting-merge` state. Opening the PR is delivery — merging is yours. Nothing closes it on merge: a `merged` PR event has no handler, so close-out is either a DRI resume that finds the PR merged, or a human running `ateam close <id> --reason "merged: <url>"` directly. The DRI (or a human) can also declare an initiative done with `ateam handoff <id>`, moving it to an `awaiting-external-review` state for third-party reviewers (`ateam handoff <id> --clear` undoes it).
- **Role-memory model:** memories use a three-tier key convention — fresh (default write tier, accumulates between condense runs) and hot (curated) are both auto-injected into every role session via `ateam learnings <role>`; cold is searchable on demand via `ateam recall <role> <query>`, not auto-injected. `ateam learn <role> <slug>` writes fresh; `ateam condense <role>` emits a structured memory packet that curates fresh into hot; `ateam fresh-drain <role>` moves uncurated fresh into cold; `ateam applied <role> <slug>` records that a learning was actually used, feeding impact-driven curation. Full mechanics in `plugins/agent-teams/CLAUDE.md`.

## Steward

A persistent, machine-scoped session — not tied to any single initiative — watches every DRI, gates plan/scope/merge/design-fork/unblock decisions through you, and nudges stalled work. It's your single conversational counterpart across all initiatives, not a DRI itself: every `ateam gate` routes to it via the reserved mail handle `steward` (see [Cross-session messaging](#cross-session-messaging)).

Start or act as it with `/agent-teams:steward`, or manage it directly with `ateam steward init|start|remove` and its decision ledger, `ateam steward ledger record|stats|recall`.

## Cross-session messaging

Sessions message each other through a durable, Dolt-synced mailbox — a message survives a crash and reaches a recipient on another machine after `bd dolt pull`.

- **Send** — `ateam mail send <recipient-id> --file <body>` writes the message and rings a doorbell that wakes the recipient if it has gone idle. The recipient is an **initiative id**, or the reserved handle `steward`. If no live session exists, it escalates to `ateam resume` — except `steward`, which has no resume path and just queues the mail.
- **Receive** — `ateam mail inbox` consumes unread messages for the current initiative; you do run it by hand. A hook peeks (`ateam mail inbox --peek`) on each prompt and at session start and, if mail is waiting, tells you to run it — the hook only signals, it never drains.
- **List / close / purge** — `ateam mail list` is a read-only table of every initiative's recent mail, including closed (does not mark anything read); `ateam mail close <id>` closes a message bead; `ateam mail purge` deletes old closed ones.
- Bare `send` / `inbox` / `debug-mail` still work as deprecated aliases — older role learnings and installed hooks still call them. Each prints a deprecation note to stderr and delegates to the `ateam mail` equivalent.

## Dashboard

A local, single-user web UI for watching every initiative on the machine — an inbox of things needing you, a live constellation of all teams, and drill-in detail with logs and attach. Run it with `cd dashboard && pnpm install && pnpm dev` (backend on :4823, web on :5173). See [`dashboard/README.md`](dashboard/README.md) for its API surface and CLI dependencies.

## Worktree setup hooks

When an agent creates a fresh track worktree, gitignored files (env files, creds, local config) are not present. Most work doesn't need them. When a worktree does need live env (running a dev server, creds-dependent validation), run:

```bash
ateam worktree-setup [abs-worktree-path]   # defaults to cwd
```

The hook is registered once per repo by dropping a file at `$AGENT_TEAMS_HOME/worktree-hooks/<repo-slug>` whose contents are the absolute path to the setup script; the reference implementation is `scripts/midgard-worktree-setup.sh`. A missing or failing hook is non-fatal.

## Development / Contributing

The repo ships two artifacts:

- **`ateam` Go CLI** (`cmd/ateam/`): the workspace CLI. Shipped as committed per-platform binaries in `plugins/agent-teams/bin/` (`ateam-{darwin,linux}-{amd64,arm64}`); `bin/ateam` is a POSIX wrapper that selects the right one.
- **Claude Code plugin** (`plugins/agent-teams/`): the `/dri` playbook, role agents, hooks, and skills.

Two separate beads databases — never confuse them: this repo's `.beads` holds all work beads (plain `bd create`); the global `~/.agent-teams` workspace holds only initiative-tracking beads and role memories, reached only via `ateam`. Agents read `CLAUDE.md` / `AGENTS.md` for the full workflow contract. Design docs and verification notes live in `docs/`.

### ateam command surface

The plugin's slash commands wrap these `ateam` verbs; agents and the DRI also call them directly. This is the subset a human or a DRI actually types by hand:

| verb | run by | purpose |
|------|--------|---------|
| `dispatch`, `resume`, `new-initiative` | human / DRI | launch or relaunch a background DRI session |
| `handoff` | DRI / human | hand an initiative off to external review (see [Lifecycle](#concepts)) |
| `list`, `show`, `human-list` | human / agent | inspect open initiatives and parked gates |
| `register`, `gate`, `clear-gate`, `note`, `close` | DRI | initiative lifecycle |
| `mail send`, `mail inbox`, `mail list`, `mail close`, `mail purge` | agent / human | cross-session mail (see [Cross-session messaging](#cross-session-messaging)) |
| `learn`, `learnings`, `recall`, `forget`, `applied` | role agents | role-memory read and write |
| `instructions` | role agents | read a role's machine-local instructions file (see [Machine-local instructions](#machine-local-instructions)) |
| `condense`, `fresh-drain`, `condense-check` | DRI / human | role-memory curation (`condense-check` is read-only: it reports the per-role fire/skip verdict) |
| `sync`, `pull` | DRI / hooks | sync the global workspace |
| `worktree-setup` | agent | hydrate a fresh track worktree |
| `steward init`, `steward start`, `steward remove` | human | start or stop the machine's Steward session |

The rest — `route-pr-event`, `hung-scan`, `watchers`, `reap-orphans`, `relay`, `tie-session`, `resume-match`, `condense-lock`, and more — are plumbing that hooks and agents call, not verbs you'll type by hand. Run `ateam --help` for the full list.

**Build and test:**

```bash
go build ./...
go vet ./...
go test ./...
go test -race ./...                # required for internal/verbs changes — see below
gofmt -l <files>                   # must produce no output
sh scripts/build-binaries.sh       # rebuild the committed per-platform binaries
bash tests/<name>.test.sh          # run individual shell-level tests
```

`internal/verbs` runs a goroutine (the relay's tick loop) alongside shared mutable package state (the hung-config tunables) — run `go test -race` on any change there, since a plain `go test` pass doesn't rule out a data race. `tests/ateam.test.sh` case10 (bd dolt sync against an empty remote) is a known pre-existing failure unrelated to most changes.

**Dashboard** (Node/TS, `dashboard/`):

```bash
cd dashboard && pnpm install   # first time only
pnpm dev                       # server (:4823) + web (:5173) together
pnpm test                      # vitest across all workspaces (115 tests)
pnpm typecheck                 # strict tsc across packages
```

**Release protocol — required on ANY CLI or plugin change:**

1. Run `sh scripts/build-binaries.sh` and commit the updated `plugins/agent-teams/bin/`.
2. Bump the version in BOTH `plugins/agent-teams/.claude-plugin/plugin.json` and `.claude-plugin/marketplace.json` (keep the two version strings identical).
3. For a new verb, add the kong struct and wire it in `RegisterAllKong`. Help text is generated from kong struct tags — no separate `UsageText` entry required.

`claude plugin update` keys off the version: no bump means installed sessions silently keep the old copy. A source-only PR that changes `ateam` behavior or plugin content without rebuilding binaries and bumping the version is incomplete.
