# Shared execution contract

The DRI owns integration. Give every delegated track the initiative id, project `EPIC_ID`, exact bead ids, absolute worktree, file-disjoint ownership, role boundary, stop conditions, and required verification. Work beads use `--parent <EPIC_ID>`. Role configuration owns the model; do not override it.

Use one git worktree per implementation track, never an independent clone. Record each `track-worktree:` before spawning, provision live environment only through `ateam worktree-setup <absolute-path>`, and operate across checkouts by absolute path or `git -C`/`bd -C` without changing the DRI's session cwd.

Implementers add code and core-path tests but never push, merge, or deploy. Testers own edge cases and live verification. Reviewers never fix. The DRI verifies artifacts, integrates the composed branch, routes findings to fresh implementers, and repeats integration verification after every ring. Loop closure requires both integrated code and an observable end-to-end exercise; tests alone are insufficient.

# Codex execution mechanics

## Child contract

Codex custom agents are bounded children. Spawn `agent-teams-planner`,
`agent-teams-implementer`, `agent-teams-tester`, `agent-teams-reviewer`, or
`agent-teams-investigator` with `fork_turns="none"`; do not override their model
or reasoning settings. Use the investigator only for bounded, evidence-only
questions; the planner retains design authority and owns decomposition. A child
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

## Live-test-review gate

After the tester's live exercise passes, the tester — not the DRI — raises a
`live-test-review` gate carrying proof: screenshots or files via `--attach`, a
short summary via `--file`. Treat it like any other human gate: it must clear
(steward-forwarded to the human, human's go received) before the PR opens in
Phase 5. Never detect steward presence or fall back to a direct Telegram send
yourself — with no steward running, the gate simply waits, as a review or
question gate would.
