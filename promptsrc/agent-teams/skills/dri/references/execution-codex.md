
# Codex execution mechanics

## Child contract

Codex custom agents are bounded children. Spawn `agent-teams-planner`,
`agent-teams-implementer`, `agent-teams-tester`, or `agent-teams-reviewer` with
`fork_turns="none"`; do not override their model or reasoning settings. A child
does not own the initiative mailbox and need not survive past its result.

Every prompt includes:

- initiative id and project `EPIC_ID`;
- repo, branch, and absolute assigned worktree;
- exact bead ids and a file-disjoint ownership lane;
- the applicable role boundary;
- required tests/artifacts and stop conditions;
- instructions to load `ateam learnings <role>` and
  `ateam instructions <role>`;
- project work beads must use `--parent <EPIC_ID>`;
- report through the final response; urgent blockers may message the parent.

Wait for the child, then independently inspect its Beads, commits, diff, and
test evidence. A final response is a claim, not durable proof. If a child dies,
reconstruct from those artifacts and spawn a fresh worker for remaining work.

## Worktrees and integration

The DRI never changes its session cwd. Create track worktrees below
`${AGENT_TEAMS_HOME:-$HOME/.agent-teams}-worktrees/`, use `bd worktree create`
when available, and operate through absolute paths or `git -C` / `bd -C`.
Never create independent clones.

Append `track-worktree: <absolute-path>` to the initiative before spawning its
implementer. Provision live environment only when needed, through
`ateam worktree-setup <absolute-path>`.

Implementers never push, merge, or deploy. The DRI inspects and integrates,
preferring `git merge --ff-only <track-branch>`. The tester owns edge cases and
live verification; the reviewer never fixes. Route findings to fresh
implementers.

After every integration ring, run the composed branch's full verification.
Loop closure requires both integrated code and observable end-to-end behavior.
