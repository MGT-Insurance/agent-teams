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

## Never end a turn waiting on work

Three yield states, not two:

- **Nothing pending** -> a clean end of turn; the human reaps the idle session.
- **A pending human gate** -> park; the human is the driver.
- **A pending agent or machine dependency** (a spawned teammate still working, a build/CI/merge still running) -> NOT a clean end and NOT a park. Never end a turn to wait on it.

For that third state, never end a turn to wait on a background command, a build, or a peer's message to finish. You may still launch independent work concurrently, but collect it within the same live turn — one blocking wait, never a turn-ending await. The simplest single-task form: run it in the foreground (one blocking call with an explicit timeout), get the result, then report and stop.

This overrides, for any spawned teammate, the general "background it and wait for the finish notification" habit — that habit is written for a top-level session. A spawned teammate does not get the same re-wake.

### Stall handling

A teammate that has gone idle without delivering its committed artifact is a stall, not progress — never assume it is still working. On being re-woken by its idle notification with work still pending, do not re-yield on "still waiting": verify the artifact first (`bd show`, `git log`, the diff — never the claim alone), nudge the teammate (an `ateam mail` / SendMessage DOES wake it, unlike a background-task finish notification), and replace or take over the work if it stays unresponsive.
