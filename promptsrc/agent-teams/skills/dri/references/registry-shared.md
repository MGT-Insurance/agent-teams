# Shared initiative-registry contract

The global registry contains one initiative issue and is accessed only through `ateam`; `ateam audit` must remain clean. The line-oriented description records the problem, repository, DRI worktree, branch, runtime/mode, root project `epic:`, and repeatable `track-worktree:` paths. It carries no DRI-maintained phase or status field.

Every initiative owns a project-repository root epic. Every contract, implementation, test, and discovery bead uses `--parent <epic-id>`. Repair a legacy initiative with no `epic:` by creating the project epic and recording `epic: <id>` in an initiative note before delegation.

Standby is active only when `standby: true` exists and neither description nor notes contains `standby: released`. When active, raise the exact QUESTION gate `Standby — waiting for direction` and park before clarify. Direction records `standby: released`, clears the gate, and then enters the normal lifecycle. Never write `standby: false`.

Opening a PR does not close an initiative. Record it on the `pr` rail, note delivery, leave the initiative open in `awaiting-merge`, and close only after merge or explicit human closure.
