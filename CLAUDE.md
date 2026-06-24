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
gofmt -l <files>                   # must be empty (formatting gate)
sh scripts/build-binaries.sh       # rebuild the 4 committed ateam binaries (see Release protocol)

# Shell-level hook/CLI tests live in tests/ — run individually:
bash tests/<name>.test.sh
```

Note: `tests/ateam.test.sh` case10 (bd dolt sync against an empty remote) is a known pre-existing failure unrelated to most changes — confirm it also fails at your merge-base before treating it as a regression.

## Architecture Overview

Two shipped artifacts in one repo:

- **`ateam` CLI** (Go). Entry point `cmd/ateam/`; verbs in `internal/verbs/` (each `RegisterX(reg)` wired in `cmd/ateam/main.go`); shared CLI plumbing in `internal/cli/`; beads access in `internal/bd/`; the global workspace in `internal/workspace/`. `ateam` is the ONLY sanctioned interface to the global `~/.agent-teams` workspace.
- **The `agent-teams` Claude Code plugin** under `plugins/agent-teams/` — the `/dri` playbook, role agents, hooks, and skills. It ships the CLI as **prebuilt per-platform binaries** committed in `plugins/agent-teams/bin/` (`ateam-{darwin,linux}-{amd64,arm64}`); `bin/ateam` is a POSIX wrapper that execs the right one. **These committed binaries — not your local `go build` — are what run when the plugin is installed.**

There is also a `dashboard/` (Node/TS) initiative dashboard.

**Two beads databases — never confuse them** (see `plugins/agent-teams/CLAUDE.md` for the cardinal rule): the PROJECT repo's `.beads` holds ALL work beads (plain `bd create`); the GLOBAL `~/.agent-teams` holds ONLY initiative-tracking beads + role memories, reached ONLY via `ateam`.

## Conventions & Patterns

- **Beads-first** for all task tracking; `bd remember` for project facts; never MEMORY.md. Memory routing for role/user learnings goes through `ateam learn` (see `plugins/agent-teams/CLAUDE.md`).
- **🚨 Release protocol — rebuild binaries + bump version on ANY CLI change.** The committed binaries in `plugins/agent-teams/bin/` are what run at install time, and `claude plugin update` only picks up changes when the version changes. So whenever you change `ateam`'s behavior (new/changed/removed verb, flags, or output) OR any plugin content (skills, agents, hooks):
  1. `sh scripts/build-binaries.sh` and commit the updated `plugins/agent-teams/bin/`.
  2. Bump the version in BOTH `.claude-plugin/marketplace.json` and `plugins/agent-teams/.claude-plugin/plugin.json` (keep them identical).
  3. For a new/renamed verb, add it to `UsageText` in `internal/cli/cli.go`.

  **No rebuild = the deployed `ateam` silently lacks your change; no version bump = installed sessions never pick it up.** A source-only PR that adds a verb is INCOMPLETE. (Detailed rationale in `cmd/ateam/CLAUDE.md` and `plugins/agent-teams/CLAUDE.md`.)
