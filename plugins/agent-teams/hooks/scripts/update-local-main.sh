#!/usr/bin/env bash
# update-local-main.sh — safely fast-forward the local main branch after a PR merges.
#
# Invocation: update-local-main.sh [<repo-or-worktree-path>]
#   Arg optional; defaults to CWD.
#   Works from a linked worktree: derives the main checkout root via --git-common-dir.
#
# FAIL-SOFT: this script ALWAYS exits 0.  Every git error is caught; a one-line
# summary is printed and the script exits 0 so it never blocks teardown or close.
#
# Algorithm (frozen, from contract agent-teams-ovkj):
#   a. Determine current HEAD branch of the main repo.
#   b. main NOT checked out -> fetch origin main:main (ff-only refspec, no working-tree touch).
#   c. main IS checked out  -> if clean: pull --ff-only origin main; if dirty: skip.
#   d. detached/other       -> attempt fetch-refspec form; fail soft.
#
# Output: exactly ONE summary line prefixed with "update-local-main:" describing the outcome.

# NOTE: intentionally NO `set -e` / `set -euo pipefail` — fail-soft requires that
# every git command failure is explicitly caught; set -e would propagate failures
# before our catch logic runs.
# We still set -u to catch typos in variable names (a separate safety), but only
# after we've finished quoting all references properly.
# shellcheck disable=SC2317  # directives below are for clarity; SC doesn't affect runtime.

PREFIX="update-local-main"

# ── Resolve target path ──────────────────────────────────────────────────────
TARGET_PATH="${1:-$PWD}"

# ── Derive MAIN_REPO via --git-common-dir ────────────────────────────────────
# git rev-parse --git-common-dir may return a relative path (e.g. inside a
# worktree it returns "../../.git/worktrees/<name>/..").  We resolve it
# relative to TARGET_PATH so this works from a linked worktree.
COMMON_DIR_RAW=$(git -C "$TARGET_PATH" rev-parse --git-common-dir 2>/dev/null) || {
  printf '%s: error: %s is not a git repository\n' "$PREFIX" "$TARGET_PATH"
  exit 0
}

# Resolve COMMON_DIR to an absolute path.
case "$COMMON_DIR_RAW" in
  /*)
    # Already absolute.
    COMMON_DIR="$COMMON_DIR_RAW"
    ;;
  *)
    # Relative — resolve from TARGET_PATH.
    COMMON_DIR=$(cd "$TARGET_PATH" && cd "$COMMON_DIR_RAW" 2>/dev/null && pwd) || {
      printf '%s: error: could not resolve git-common-dir %s\n' "$PREFIX" "$COMMON_DIR_RAW"
      exit 0
    }
    ;;
esac

# The main worktree root is the parent of the common git dir.
MAIN_REPO=$(cd "$COMMON_DIR" && cd .. && pwd) || {
  printf '%s: error: could not resolve main repo from common dir %s\n' "$PREFIX" "$COMMON_DIR"
  exit 0
}

# ── Check for origin remote ───────────────────────────────────────────────────
if ! git -C "$MAIN_REPO" remote get-url origin >/dev/null 2>&1; then
  printf '%s: skipped: no origin remote in %s\n' "$PREFIX" "$MAIN_REPO"
  exit 0
fi

# ── Determine HEAD branch of the main repo ───────────────────────────────────
HEAD_BRANCH=$(git -C "$MAIN_REPO" symbolic-ref --short HEAD 2>/dev/null) || HEAD_BRANCH=""

# ── Case b/d: main NOT checked out (or detached HEAD / other) ────────────────
if [ "$HEAD_BRANCH" != "main" ]; then
  # Use fetch refspec to update local main without touching the working tree.
  fetch_out=$(git -C "$MAIN_REPO" fetch origin main:main 2>&1) || {
    # Distinguish non-ff from other errors.
    case "$fetch_out" in
      *"would clobber"*|*"rejected"*|*"non-fast-forward"*|*"not a fast-forward"*)
        printf '%s: skipped: not a fast-forward in %s\n' "$PREFIX" "$MAIN_REPO"
        ;;
      *"could not read"*|*"unable to connect"*|*"Could not resolve"*|*"fatal: "*)
        printf '%s: skipped: no origin remote or network error in %s\n' "$PREFIX" "$MAIN_REPO"
        ;;
      *)
        printf '%s: skipped: fetch failed in %s (%s)\n' "$PREFIX" "$MAIN_REPO" "$fetch_out"
        ;;
    esac
    exit 0
  }
  # Check if already up to date.
  case "$fetch_out" in
    *"up to date"*|*"up-to-date"*|"")
      # Fetch returned empty or 'up to date' — check if there was nothing new.
      new_sha=$(git -C "$MAIN_REPO" rev-parse main 2>/dev/null) || new_sha="unknown"
      printf '%s: already up to date (%s main at %s)\n' "$PREFIX" "$MAIN_REPO" "$new_sha"
      ;;
    *)
      new_sha=$(git -C "$MAIN_REPO" rev-parse main 2>/dev/null) || new_sha="unknown"
      printf '%s: updated %s main to %s\n' "$PREFIX" "$MAIN_REPO" "$new_sha"
      ;;
  esac
  exit 0
fi

# ── Case c: main IS checked out ───────────────────────────────────────────────
dirty=$(git -C "$MAIN_REPO" status --porcelain 2>/dev/null) || {
  printf '%s: error: git status failed in %s\n' "$PREFIX" "$MAIN_REPO"
  exit 0
}

if [ -n "$dirty" ]; then
  printf '%s: skipped: main checked out with dirty tree in %s\n' "$PREFIX" "$MAIN_REPO"
  exit 0
fi

# Clean main checkout — pull --ff-only.
pull_out=$(git -C "$MAIN_REPO" pull --ff-only origin main 2>&1) || {
  case "$pull_out" in
    *"not possible to fast-forward"*|*"rejected"*)
      printf '%s: skipped: not a fast-forward in %s\n' "$PREFIX" "$MAIN_REPO"
      ;;
    *"could not read"*|*"unable to connect"*|*"Could not resolve"*|*"fatal: "*)
      printf '%s: skipped: no origin remote or network error in %s\n' "$PREFIX" "$MAIN_REPO"
      ;;
    *)
      printf '%s: skipped: pull failed in %s (%s)\n' "$PREFIX" "$MAIN_REPO" "$pull_out"
      ;;
  esac
  exit 0
}

# Determine outcome from pull output.
case "$pull_out" in
  *"Already up to date"*|*"Already up-to-date"*)
    printf '%s: already up to date (%s main)\n' "$PREFIX" "$MAIN_REPO"
    ;;
  *)
    new_sha=$(git -C "$MAIN_REPO" rev-parse main 2>/dev/null) || new_sha="unknown"
    printf '%s: updated %s main to %s\n' "$PREFIX" "$MAIN_REPO" "$new_sha"
    ;;
esac
exit 0
