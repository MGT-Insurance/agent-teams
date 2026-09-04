# Shared execution contract

The DRI owns integration. Give every delegated track the initiative id, project `EPIC_ID`, exact bead ids, absolute worktree, file-disjoint ownership, role boundary, stop conditions, and required verification. Work beads use `--parent <EPIC_ID>`. Role configuration owns the model; do not override it.

Use one git worktree per implementation track, never an independent clone. For **every fresh delegated track worktree**, the DRI must complete this fail-open sequence before spawning: create -> setup attempt -> failure report -> `track-worktree:` -> spawn. Setup is unconditional: do not run a project script directly or ask the child to install dependencies first; the registered hook owns that work. No registered hook exits 0 and needs no warning. A configured missing or failing hook exits 1; it never removes the worktree or blocks recording or spawning. Operate across checkouts by absolute path or `git -C`/`bd -C` without changing the DRI's session cwd.

After creation, use this POSIX-shell-safe procedure (including under `set -e`). `track_worktree` is its absolute path; on failure determine `hook_path` and `outcome` without hook output or credentials: `missing` for an absent configured script, otherwise `exit-N` from its status (or `exit-1` if unavailable).

```sh
setup_status=0
ateam worktree-setup "$track_worktree" || setup_status=$?
if [ "$setup_status" -ne 0 ]; then
  warning_file=$(mktemp)
  printf 'worktree-setup-warning: path="%s" hook="%s" outcome="%s" lifecycle=continued\n' \
    "$track_worktree" "$hook_path" "$outcome" > "$warning_file"
  cat "$warning_file"
  ateam note "$initiative_id" --file "$warning_file" || \
    printf 'worktree-setup-note-warning: initiative="%s" lifecycle=continued\n' "$initiative_id" >&2
fi
```

The line above is the exact normalized warning; append its temp file to the existing initiative, include it in the spawn brief, then record `track-worktree:` and spawn. A note failure is visibly reported but nonblocking. A successful setup skips this reporting path and continues directly to track recording and spawn.

Implementers add code and core-path tests but never push, merge, or deploy. Testers own edge cases and live verification. Reviewers never fix. The DRI verifies artifacts, integrates the composed branch, routes findings to fresh implementers, and repeats integration verification after every ring. Loop closure requires both integrated code and an observable end-to-end exercise; tests alone are insufficient.
