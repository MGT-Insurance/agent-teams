# Shared execution contract

The DRI owns integration. Give every delegated track the initiative id, project `EPIC_ID`, exact bead ids, absolute worktree, file-disjoint ownership, role boundary, stop conditions, and required verification. Work beads use `--parent <EPIC_ID>`. Role configuration owns the model; do not override it.

Use one git worktree per implementation track, never an independent clone. For **every fresh delegated track worktree**, the DRI must complete this fail-open sequence before spawning its agent: create the worktree -> attempt the exact command `ateam worktree-setup <absolute-path>` -> if it exits nonzero, visibly and durably report the normalized warning below -> record `track-worktree:` -> spawn. The setup attempt is unconditional: do not run a project setup script directly or ask the child to install dependencies before it. The registered hook owns dependency installation. No configured hook exits 0 and needs no warning; a missing or failing configured hook never removes the worktree or blocks recording or spawning. Operate across checkouts by absolute path or `git -C`/`bd -C` without changing the DRI's session cwd.

On a nonzero setup result, determine the configured hook path and outcome without reproducing hook output or credentials: use `missing` when the configured script is absent; otherwise use `exit-N` from the hook's exit status (or `exit-1` if none is available). Write exactly one normalized line to a temp file and echo it visibly:

```text
worktree-setup-warning: path=<quoted-absolute-path> hook=<quoted-script-path> outcome=<quoted-missing-or-exit-N> lifecycle=continued
```

Append that file to the existing initiative with `ateam note <initiative-id> --file <temp-file>`. If the append fails, visibly report that failure too and still record the track and spawn. Include the same normalized warning in the spawned agent's brief. A successful setup skips this reporting path and continues directly to track recording and spawn.

Implementers add code and core-path tests but never push, merge, or deploy. Testers own edge cases and live verification. Reviewers never fix. The DRI verifies artifacts, integrates the composed branch, routes findings to fresh implementers, and repeats integration verification after every ring. Loop closure requires both integrated code and an observable end-to-end exercise; tests alone are insufficient.
