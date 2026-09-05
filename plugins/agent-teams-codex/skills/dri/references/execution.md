# Shared execution contract

The DRI owns integration. Give every delegated track the initiative id, project `EPIC_ID`, exact bead ids, absolute worktree, file-disjoint ownership, role boundary, stop conditions, and required verification. Work beads use `--parent <EPIC_ID>`. Role configuration owns the model; do not override it.

Use one git worktree per implementation track, never an independent clone. For **every fresh delegated track worktree**, the DRI must complete this fail-open sequence before spawning: create -> setup attempt -> failure report -> `track-worktree:` -> spawn. Setup is unconditional: do not run a project script directly or ask the child to install dependencies first; the registered hook owns that work. No registered hook exits 0 and needs no warning. A configured missing or failing hook exits 1; it never removes the worktree or blocks recording or spawning. Operate across checkouts by absolute path or `git -C`/`bd -C` without changing the DRI's session cwd.

After creation, use this POSIX-shell-safe procedure (including under `set -e`). `track_worktree` is its absolute path; on failure determine `hook_path` and `outcome` without hook output or credentials: `missing` for an absent configured script, otherwise `exit-N` from its status (or `exit-1` if unavailable).

```sh
print_setup_warning() {
  printf 'worktree-setup-warning: path="%s" hook="%s" outcome="%s" lifecycle=continued\n' \
    "$track_worktree" "$hook_path" "$outcome"
}

setup_status=0
ateam worktree-setup "$track_worktree" > /dev/null 2>&1 || setup_status=$?
if [ "$setup_status" -ne 0 ]; then
  if warning_file=$(mktemp); then
    if print_setup_warning > "$warning_file"; then
      if ! cat "$warning_file"; then
        print_setup_warning >&2 || :
        printf 'worktree-setup-display-warning: path="%s" lifecycle=continued\n' \
          "$track_worktree" >&2 || :
      fi
      if ! ateam note "$initiative_id" --file "$warning_file"; then
        printf 'worktree-setup-note-warning: initiative="%s" lifecycle=continued\n' \
          "$initiative_id" >&2 || :
      fi
    else
      print_setup_warning >&2 || :
      printf 'worktree-setup-report-warning: path="%s" lifecycle=continued\n' \
        "$track_worktree" >&2 || :
    fi
    if ! rm -f "$warning_file"; then
      printf 'worktree-setup-cleanup-warning: path="%s" lifecycle=continued\n' \
        "$track_worktree" >&2 || :
    fi
  else
    print_setup_warning >&2 || :
    printf 'worktree-setup-report-warning: path="%s" lifecycle=continued\n' \
      "$track_worktree" >&2 || :
  fi
fi
```

The line from `print_setup_warning` is the exact normalized warning. When its temporary file is available, append that file to the existing initiative; always include the normalized warning in the spawn brief, then record `track-worktree:` and spawn. Every reporting primitive is nonblocking: a temporary-file, write, display, note, or cleanup failure emits a fallback warning without hook output or credentials and still continues. A successful setup skips this reporting path and continues directly to track recording and spawn.

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
- the normalized `worktree-setup-warning` when the DRI's required pre-spawn setup attempt failed;
- report through the final response; urgent blockers may message the parent.

Wait for the child, then independently inspect its Beads, commits, diff, and
test evidence. A final response is a claim, not durable proof. If a child dies,
reconstruct from those artifacts and spawn a fresh worker for remaining work.

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
