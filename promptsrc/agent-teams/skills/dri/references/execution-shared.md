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
