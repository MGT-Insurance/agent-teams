#!/usr/bin/env bash
# Tests for plugins/agent-teams/hooks/scripts/update-local-main.sh
# Edge-case coverage per bead agent-teams-iwwf.
# All cases assert exit 0 (fail-soft invariant from contract agent-teams-ovkj).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/update-local-main.sh"
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

# Helper: assert exit 0 and print output, fail with message if not.
assert_exit0() {
  local label="$1"; shift
  local ec=0
  local out
  out=$("$@" 2>&1) || ec=$?
  if [ "$ec" -ne 0 ]; then
    echo "FAIL $label: script exited $ec (want 0); output: $out"
    exit 1
  fi
  echo "$out"
}

# Helper: set up a bare remote + clone pair inside $T.
# Usage: setup_repo <name>  -> sets BARE_REMOTE, MAIN_CLONE
setup_repo() {
  local name="$1"
  BARE_REMOTE="$T/$name-remote.git"
  MAIN_CLONE="$T/$name-clone"
  git init --bare -q "$BARE_REMOTE"
  git clone -q "$BARE_REMOTE" "$MAIN_CLONE" 2>/dev/null
  git -C "$MAIN_CLONE" config user.email "test@example.com"
  git -C "$MAIN_CLONE" config user.name "Test"
  git -C "$MAIN_CLONE" commit -q --allow-empty -m "initial"
  git -C "$MAIN_CLONE" push -q origin main 2>/dev/null
}

# Helper: add a commit to origin (bare remote) without touching the local clone.
# Simulates a PR merge to remote main.
advance_origin() {
  local clone="$1"
  # Push a new empty commit directly via a temp clone.
  local tmp_clone="$T/tmp-advance"
  git clone -q "$BARE_REMOTE" "$tmp_clone" 2>/dev/null
  git -C "$tmp_clone" config user.email "test@example.com"
  git -C "$tmp_clone" config user.name "Test"
  git -C "$tmp_clone" commit -q --allow-empty -m "remote advance"
  git -C "$tmp_clone" push -q origin main 2>/dev/null
  rm -rf "$tmp_clone"
}

# ── Case 1: main NOT checked out (linked worktree), origin ahead -> fast-forward ─
setup_repo "c1"
# Record initial local main sha.
sha_before=$(git -C "$MAIN_CLONE" rev-parse main)
# Add a commit to origin.
advance_origin "$MAIN_CLONE"
sha_origin=$(git -C "$BARE_REMOTE" rev-parse HEAD)
# Create a linked worktree on a different branch so main is NOT checked out.
git -C "$MAIN_CLONE" checkout -q -b feature-c1 2>/dev/null
git -C "$MAIN_CLONE" worktree add -q "$T/c1-wt" -b wt-c1 2>/dev/null
# Invoke script from the linked worktree (main is not checked out there either,
# and the clone's HEAD is now on feature-c1 so main is not checked out in MAIN_CLONE).
out=$(assert_exit0 "case1" "$SCRIPT" "$T/c1-wt")
echo "$out" | grep -q "updated" \
  || { echo "FAIL case1: expected 'updated' in output, got: $out"; exit 1; }
sha_after=$(git -C "$MAIN_CLONE" rev-parse main)
[ "$sha_after" = "$sha_origin" ] \
  || { echo "FAIL case1: local main sha ($sha_after) != origin sha ($sha_origin)"; exit 1; }
echo "  case1 OK: local main fast-forwarded, exit 0"
git -C "$MAIN_CLONE" worktree remove -f "$T/c1-wt" 2>/dev/null || true

# ── Case 2: main NOT checked out, local main DIVERGED (non-fast-forward) ─────
setup_repo "c2"
# Create divergent history: add a local commit on main without pushing.
git -C "$MAIN_CLONE" checkout -q main 2>/dev/null
git -C "$MAIN_CLONE" commit -q --allow-empty -m "local diverge"
sha_local=$(git -C "$MAIN_CLONE" rev-parse main)
# Also advance origin so both sides have new commits.
advance_origin "$MAIN_CLONE"
# Switch to a different branch so main is NOT checked out.
git -C "$MAIN_CLONE" checkout -q -b feature-c2 2>/dev/null
# Invoke script from MAIN_CLONE path (main not checked out).
out=$(assert_exit0 "case2" "$SCRIPT" "$MAIN_CLONE")
echo "$out" | grep -q "fast-forward" \
  || { echo "FAIL case2: expected 'fast-forward' skip notice, got: $out"; exit 1; }
# Local main must be unchanged.
sha_after=$(git -C "$MAIN_CLONE" rev-parse main)
[ "$sha_after" = "$sha_local" ] \
  || { echo "FAIL case2: local main changed (was $sha_local, now $sha_after)"; exit 1; }
echo "  case2 OK: diverged local main unchanged, exit 0"

# ── Case 3: no origin remote configured ──────────────────────────────────────
NO_ORIGIN="$T/no-origin"
mkdir -p "$NO_ORIGIN"
git -C "$NO_ORIGIN" init -q
git -C "$NO_ORIGIN" config user.email "test@example.com"
git -C "$NO_ORIGIN" config user.name "Test"
git -C "$NO_ORIGIN" commit -q --allow-empty -m "no origin"
out=$(assert_exit0 "case3" "$SCRIPT" "$NO_ORIGIN")
echo "$out" | grep -q "no origin" \
  || { echo "FAIL case3: expected 'no origin' notice, got: $out"; exit 1; }
echo "  case3 OK: no origin remote, notice printed, exit 0"

# ── Case 4: path pointing at a non-git directory ─────────────────────────────
NONGIT="$T/not-a-repo"
mkdir -p "$NONGIT"
out=$(assert_exit0 "case4" "$SCRIPT" "$NONGIT")
echo "$out" | grep -q "not a git repository" \
  || { echo "FAIL case4: expected 'not a git repository' notice, got: $out"; exit 1; }
echo "  case4 OK: non-git directory, notice printed, exit 0"

# ── Case 5: main IS checked out, dirty working tree, behind origin → skip ────
setup_repo "c5"
advance_origin "$MAIN_CLONE"
# Working tree is on main (checkout is clean so far, but behind origin).
# Add an uncommitted file to make it dirty.
touch "$MAIN_CLONE/dirty.txt"
git -C "$MAIN_CLONE" add "dirty.txt"   # staged = dirty
sha_before=$(git -C "$MAIN_CLONE" rev-parse main)
out=$(assert_exit0 "case5" "$SCRIPT" "$MAIN_CLONE")
echo "$out" | grep -q "dirty" \
  || { echo "FAIL case5: expected 'dirty' skip notice, got: $out"; exit 1; }
sha_after=$(git -C "$MAIN_CLONE" rev-parse main)
[ "$sha_after" = "$sha_before" ] \
  || { echo "FAIL case5: local main changed despite dirty tree (was $sha_before, now $sha_after)"; exit 1; }
echo "  case5 OK: dirty main checkout skipped, exit 0"

# ── Case 6: main IS checked out, clean, behind origin → pull --ff-only ───────
setup_repo "c6"
advance_origin "$MAIN_CLONE"
sha_origin=$(git -C "$BARE_REMOTE" rev-parse HEAD)
# main IS checked out (clone default) and clean.
out=$(assert_exit0 "case6" "$SCRIPT" "$MAIN_CLONE")
echo "$out" | grep -q "updated" \
  || { echo "FAIL case6: expected 'updated' in output, got: $out"; exit 1; }
sha_after=$(git -C "$MAIN_CLONE" rev-parse main)
[ "$sha_after" = "$sha_origin" ] \
  || { echo "FAIL case6: local main sha ($sha_after) != origin sha ($sha_origin)"; exit 1; }
echo "  case6 OK: clean main checkout fast-forwarded, exit 0"

# ── Case 7: NO arg, invoked from inside a worktree → derives main correctly ───
setup_repo "c7"
advance_origin "$MAIN_CLONE"
sha_origin=$(git -C "$BARE_REMOTE" rev-parse HEAD)
# Switch to a non-main branch so main is not checked out.
git -C "$MAIN_CLONE" checkout -q -b feature-c7 2>/dev/null
# Create a linked worktree.
git -C "$MAIN_CLONE" worktree add -q "$T/c7-wt" -b wt-c7 2>/dev/null
# Invoke script with NO arg from inside the linked worktree (script uses CWD).
out=$(cd "$T/c7-wt" && assert_exit0 "case7" "$SCRIPT")
echo "$out" | grep -q "updated" \
  || { echo "FAIL case7: expected 'updated' in output, got: $out"; exit 1; }
sha_after=$(git -C "$MAIN_CLONE" rev-parse main)
[ "$sha_after" = "$sha_origin" ] \
  || { echo "FAIL case7: local main sha ($sha_after) != origin sha ($sha_origin)"; exit 1; }
echo "  case7 OK: no-arg from worktree, main updated correctly, exit 0"
git -C "$MAIN_CLONE" worktree remove -f "$T/c7-wt" 2>/dev/null || true

# ── Case 8: already up to date (main checked out, clean, at same sha as origin) ─
setup_repo "c8"
# No advance_origin call; local and origin are at the same sha.
out=$(assert_exit0 "case8" "$SCRIPT" "$MAIN_CLONE")
echo "$out" | grep -qi "already up to date" \
  || { echo "FAIL case8: expected 'already up to date', got: $out"; exit 1; }
echo "  case8 OK: already up to date, exit 0"

echo "PASS"
