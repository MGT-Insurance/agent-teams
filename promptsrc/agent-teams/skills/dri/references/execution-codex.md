
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
- the normalized `worktree-setup-warning` when the DRI's required pre-spawn setup attempt failed;
- return a status — done, blocked, or waiting — through the final response,
  never ending without one; urgent blockers may also message the parent.

Wait for the child, then independently inspect its Beads, commits, diff, and
test evidence. A final response is a claim, not durable proof — a returned
claim that a commit or a bead-state change does not back is a stall signal,
not progress or completion. Inspect the artifacts and spawn a fresh worker for
the remaining work; never assume the claim and never treat it as a clean
turn-end. Reconstruct the same way if a child dies.

## Worktrees and integration

The DRI never changes its session cwd. Create track worktrees below
`${AGENT_TEAMS_HOME:-$HOME/.agent-teams}-worktrees/`, use `bd worktree create`
when available, and operate through absolute paths or `git -C` / `bd -C`.
Never create independent clones.

Immediately after creating every fresh track worktree, attempt the exact command
`ateam worktree-setup <absolute-path>` to completion. Follow the shared
execution contract's fail-open reporting procedure when it exits nonzero, then
append `track-worktree: <absolute-path>` to the initiative and spawn its
implementer. Never call a project setup script directly or install dependencies
before this attempt; the registered hook owns dependency installation.

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

## Optional: live verification via Dispatcher

Applies only when the tester reports a bead id from `ateam ui-test file`. If
the tester reports exit 5, nothing here applies: start a dev server as before.
The same flow, screenshots included, works in a cloud run.

- **Push first.** Before each Dispatcher live test and after each fix batch,
  push the real integration branch: `git push -u origin <branch>`, never force.
  If the tester still reports exit 6, push and re-spawn it. Open no PR until
  Phase 5. In midgard a branch push without a PR deploys nothing: `vercel.json`
  sets `deploymentEnabled` for `main` only, the CI push trigger is `main` only,
  and the branch deploy workflows need an open PR. In other repos, check the
  deploy triggers first.
- **Dev server.** Start one only for exit 5 or for local-only items the tester
  names.
- **Proof.** Raise live-test-review with `--attach <png>` for every PNG the
  tester reports and `--attach <verdict.md>`. Put the bead URL in the summary
  file.
- **Cloud run.** `--attach` is not delivered from a cloud run. Put
  `screenshot: <ui-test bead id>` in the gate text, and the steward posts the
  image.
- **Cloud run: stop events.** A filed ui-test bead is an event-backed
  dependency, an exception to "never end a turn waiting on work": the
  Dispatcher posts `ui-test <id> <kind>` (`question`, `blocked`, `done`, or
  `failed`) to your inbox, or resumes you if you are parked, so you may end
  your turn after the tester files. On that message run
  `ateam ui-test wait <id> --out <dir> --timeout 30s`. Exit 4: if the brief,
  plan, or code answers the question, run
  `ateam ui-test reply <id> --text <answer>` (or `--file`); otherwise raise a
  question gate and reply with the human's answer. Exit 0: raise
  live-test-review with `screenshot: <id>`. Exits 2 and 7, and kind `blocked`,
  are handled as outside a cloud run. Treat the message as a hint, not the
  result, and ack it with `ateam mail inbox` as usual.
- **NO-IMAGES (exit 7).** Re-file once. If images are still missing, raise a
  question gate. Never raise live-test-review without images.
