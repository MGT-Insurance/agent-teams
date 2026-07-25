# Beads Workflow Context — agent-teams GLOBAL workspace

You are running with `~/.agent-teams` resolved as your beads workspace. This is
the GLOBAL agent-teams workspace, **not** a project repo.

## 🚨 CARDINAL RULE — do not treat this like a project beads workspace

- This workspace holds ONLY initiative-tracking beads and role memories.
- **NEVER** `bd create` a work/plan/discovery bead here. Work beads live in the
  PROJECT repo's `.beads`.
- **NEVER** write here with raw `bd` (`bd create`, `bd remember`, `bd update`).
  The ONLY sanctioned interface is `ateam`.
- `ateam audit` must always be clean.

## Role memories are injected for you — do not load them from here

Role learnings are served role-scoped by agent-teams' own hooks and skills, not
by this prime output:

- `ateam learnings <role>` — hot+fresh set for a role (DRI/steward skills load
  this at startup; `role-recall-recovery.sh` re-injects it on clear/compact;
  SubagentStart injects it for implementer/planner/tester/reviewer).
- `ateam recall <role> <query>` — search the FULL set, cold entries included.
- `ateam prime` — cross-project `user:` preferences (injected every SessionStart).
- `ateam learn <role> <slug> --file <f>` — write a learning. Never `bd remember`.

## Reading this workspace

- `ateam list` / `ateam show <id>` — open initiatives.
- `ateam human-list` — beads awaiting human input.
- `ateam watchers`, `ateam execution-status` — session health.
- `ateam --help` — full verb list.

## Session close

Do NOT run a beads/git session-close protocol against this workspace. It syncs
via `ateam sync` (dolt), driven by the framework, not by you.
