# Project Instructions for AI Agents

This file provides instructions and context for AI coding agents working on this project.

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:7510c1e2 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/SYNC_CONCEPTS.md for details and anti-patterns.

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->


## Build & Test

```bash
go build ./...                     # compile everything
go vet ./...                       # static checks
go test ./...                      # Go unit tests
go test -race ./...                # Go unit tests, race-detected
gofmt -l <files>                   # must be empty (formatting gate)
sh scripts/build-binaries.sh       # rebuild the 4 committed ateam binaries (see Release protocol)

# Shell-level hook/CLI tests live in tests/ — run individually:
bash tests/<name>.test.sh
```

`internal/verbs` runs a goroutine (the relay's tick loop) alongside shared mutable package state (the hung-config tunables) — run `go test -race` on any change there, since a plain `go test` pass does not rule out a data race.

Note: `tests/ateam.test.sh` case10 (bd dolt sync against an empty remote) is a known pre-existing failure unrelated to most changes — confirm it also fails at your merge-base before treating it as a regression.

Run a single Go test with `go test ./internal/verbs/... -run TestName`.

### Dashboard (Node/TS, in `dashboard/`)

```bash
cd dashboard && pnpm install   # first time only
pnpm dev                       # server (:4823) + web (:5173) together
pnpm test                      # vitest across all workspaces (115 tests)
pnpm typecheck                 # strict tsc across packages
```

`dashboard/README.md` is the authoritative spec for this package (API endpoints, SSE event catalog, `ateam`/`bd`/`claude` CLI JSON shapes it depends on) — read it before touching `dashboard/server` or `dashboard/web`.

## Eval suite — costs real money

`eval/` (`cmd/eval`, `internal/eval`) is a live-dispatch A/B eval harness:
`eval run` dispatches a **real, autonomous agent-teams DRI session**, and
`eval collect` calls a **real LLM judge** — both spend real API dollars and
real wall-clock time (observed: ~$9 and 13 min for one bugfix run; runs can
take hours). **Never invoke either as part of building/testing/verifying a
change, and never add them to CI or a script/loop.** `go test ./...` on
`internal/eval` is the free path — it fakes every dispatch/clone/judge/push
seam. The supported entry point is `scripts/eval` (a `go run ./cmd/eval`
wrapper anchored at the repo root). See `eval/README.md` for the full cost
model and lifecycle.

## Architecture Overview

Two shipped artifacts in one repo:

- **`ateam` CLI** (Go). Entry point `cmd/ateam/`; verbs in `internal/verbs/` (each as a kong struct with `Run(*cli.Context) error`, wired via `RegisterAllKong` in `internal/verbs/kong_converted.go`); shared CLI plumbing in `internal/cli/`; beads access in `internal/bd/`; the global workspace in `internal/workspace/`. `ateam` is the ONLY sanctioned interface to the global `~/.agent-teams` workspace. Uses [kong](https://github.com/alecthomas/kong) for flag/arg parsing and help generation.
- **The `agent-teams` Claude Code plugin** under `plugins/agent-teams/` — the `/dri` playbook, role agents, hooks, and skills. It ships the CLI as **prebuilt per-platform binaries** committed in `plugins/agent-teams/bin/` (`ateam-{darwin,linux}-{amd64,arm64}`); `bin/ateam` is a POSIX wrapper that execs the right one. **These committed binaries — not your local `go build` — are what run when the plugin is installed.** The four role agent definitions live in `plugins/agent-teams/roles/`, not `agents/` — a deliberate workaround for an open Claude Code bug, not an oversight; see `plugins/agent-teams/roles/README.md` before moving them back.

- **`internal/initiative`** owns reading and writing an initiative's routing data (repo/worktree/branch/... and the session/track ties) — the `key: value` lines inside an initiative bead's description. One matching rule, one read seam (`Of`), one write seam (`New`/`WithSession`/`WithTrack`), used instead of each call site re-implementing its own line scanner. See the package doc comment (`internal/initiative/doc.go`) for the frozen format contract before touching this data anywhere.

There is also a `dashboard/` (Node/TS, pnpm workspace: `shared`/`server`/`web`) initiative dashboard — see `dashboard/README.md` for its API surface and CLI-dependency contracts.

**Two beads databases — never confuse them** (see `plugins/agent-teams/CLAUDE.md` for the cardinal rule): the PROJECT repo's `.beads` holds ALL work beads (plain `bd create`); the GLOBAL `~/.agent-teams` holds ONLY initiative-tracking beads + role memories, reached ONLY via `ateam`.

## Conventions & Patterns

- **Beads-first** for all task tracking; `bd remember` for project facts; never MEMORY.md. Memory routing for role/user learnings goes through `ateam learn` (see `plugins/agent-teams/CLAUDE.md`).
- **🚨 Release protocol — rebuild binaries + bump version on ANY CLI change.** The committed binaries in `plugins/agent-teams/bin/` are what run at install time, and `claude plugin update` only picks up changes when the version changes. So whenever you change `ateam`'s behavior (new/changed/removed verb, flags, or output) OR any plugin content (skills, agents, hooks):
  1. `sh scripts/build-binaries.sh` and commit the updated `plugins/agent-teams/bin/`.
  2. Bump the version in BOTH `.claude-plugin/marketplace.json` and `plugins/agent-teams/.claude-plugin/plugin.json` (keep them identical).
  3. For a new verb, add the kong struct and wire it in `RegisterAllKong`. Help text is generated from kong struct tags — no separate `UsageText` entry required.

  **No rebuild = the deployed `ateam` silently lacks your change; no version bump = installed sessions never pick it up.** A source-only PR that adds a verb is INCOMPLETE. (Detailed rationale in `cmd/ateam/CLAUDE.md` and `plugins/agent-teams/CLAUDE.md`.)
