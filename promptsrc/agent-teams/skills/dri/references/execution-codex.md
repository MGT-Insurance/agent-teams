
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
live verification; the reviewer never fixes and is spawned only in Phase 5,
after the live-test-review gate below clears — never alongside the tester.
Route findings to fresh implementers.

After every integration ring, run the composed branch's full verification.
Loop closure requires both integrated code and observable end-to-end behavior.

## Live-test-review gate

A tester live pass closes the engineering loop — it does not clear delivery.
Gates are DRI-owned: the tester hands its proof (screenshots, payload/log
files, a short summary) to the DRI via its final response or a message; it
never calls `ateam gate` itself. Before spawning `agent-teams-reviewer` or
starting any Phase 5 PR prep, the DRI raises the gate carrying that proof:

```bash
ateam gate <initiative-id> --kind=live-test-review --attach <path> [--attach <path> ...] --file <summary-file>
```

Treat it like any other human gate: it must clear (steward-forwarded to the
human, human's go received) before the PR opens in Phase 5. Never detect
steward presence or fall back to a direct Telegram send yourself — with no
steward running, the gate simply waits, as a review or question gate would.

**BIG vs SMALL.** BIG — observable behavior (UI, API response, CLI output,
user-facing flow), decomposed into multiple tracks/implementers, or a changed
default/durable state/user-facing message — always gates. SMALL —
single-track, few-item, linear, nothing observable, no load-bearing human
decision — skips it: reading the diff against criteria IS the verification,
the same bar as the team/plan-gate skip. A cleared or skipped plan gate is not
itself a trigger either way.

**Feedback loop.** A requested change can pull in any mix of
investigator/implementer/planner — a fresh plan gate if it reshapes the work —
then re-integrate, re-prove live, and re-raise the gate. Nothing is prepped
for the PR before approval. The ask stays REVIEW throughout — never frame this
as "ready to merge."
