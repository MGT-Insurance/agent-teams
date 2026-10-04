# Shared initiative-registry contract

The global registry contains one initiative issue and is accessed only through `ateam`; `ateam audit` must remain clean. The line-oriented description records the problem, repository, DRI worktree, branch, runtime/mode, root project `epic:`, and repeatable `track-worktree:` paths. It carries no DRI-maintained phase or status field.

Every initiative owns a project-repository root epic. Every contract, implementation, test, and discovery bead uses `--parent <epic-id>`. Repair a legacy initiative with no `epic:` by creating the project epic and recording `epic: <id>` in an initiative note before delegation.

Standby is active only when `standby: true` exists and neither description nor notes contains `standby: released`. When active, raise the exact QUESTION gate `Standby — waiting for direction` and park before clarify. Direction records `standby: released`, clears the gate, and then enters the normal lifecycle. Never write `standby: false`.

Opening a PR does not close an initiative. Record it on the `pr` rail, note delivery, leave the initiative open in `awaiting-merge`, and close only after merge or explicit human closure.

# Initiative registry

The global registry contains one issue per initiative. Access it only through
`ateam`; `ateam audit` enforces that work beads did not leak into it.

The description schema is line-oriented:

```text
problem: <one-line statement>
repo: <absolute main-repo path>
worktree: <absolute DRI checkout>
branch: <branch>
team: <slug>
mode: interactive|bg
runtime: codex
standby: true
epic: <project root epic id>
track-worktree: <additional implementer checkout, repeatable>
```

`standby:` is absent when off; never write `standby: false`. Standby is active
only when `standby: true` exists and neither description nor notes contains
`standby: released`. Direction records that release marker before clearing the
gate.

Every initiative owns a project-repo root epic. Every work/discovery/test bead
uses `--parent <epic-id>`. For a legacy initiative without `epic:`, create a
project epic, write `epic: <id>` as an initiative note, and use it for all work.

For a direct `/dri <problem>` in an already-dedicated checkout, create the root
epic in the project repo, assemble the schema above in a temporary file, then:

```bash
ateam register --title "<short problem>" --file <body-file>
```

The body must identify the main repository, the DRI's current checkout and
branch, `mode: interactive`, `runtime: codex`, and the new project epic. Prefer
`agent-teams-codex:dispatch-dri` when a dedicated checkout does not already
exist; registration must never silently claim a shared primary checkout.

Useful commands:

- `ateam show <id>` — description and notes.
- `ateam resume-match <worktree>` — exact open match.
- `ateam resume-match-closed <worktree>` — newest exact closed match.
- `ateam resolve-initiative <path>` — ancestor-aware hook lookup.
- `ateam note <id> --file <file>` — append durable state.
- `ateam reopen <id>` / `ateam close <id> --reason <reason>`.

Do not use `bd search` as a description lookup. A PR opening does not close an
initiative; it remains open until merge or explicit human closure.
