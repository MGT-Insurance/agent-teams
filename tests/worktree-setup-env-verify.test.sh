#!/usr/bin/env bash
# worktree-setup-env-verify.test.sh — coverage for the env-verification pass
# added to scripts/midgard-worktree-setup.sh (agent-teams-xtac.1).
#
# Drives the real script against a synthetic source checkout + real git
# worktree (no midgard, no vercel CLI). Proves the "did the expected env
# files actually land" predicate both ways:
#   RED:   a required file is missing/empty -> no "complete" line, a clear
#          not-provisioned verdict naming the file, nonzero exit.
#   GREEN: all required files present and non-empty -> "complete", exit 0.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/scripts/midgard-worktree-setup.sh"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

# ── synthetic source checkout ────────────────────────────────────────────────
SRC="$T/src"
mkdir -p "$SRC"
git -C "$SRC" init -q
git -C "$SRC" config user.email test@example.com
git -C "$SRC" config user.name test
echo placeholder > "$SRC/placeholder.txt"
git -C "$SRC" add placeholder.txt
git -C "$SRC" commit -q -m init

# .vercel link (ids only) + the local-only env files. These are untracked on
# purpose: the whole point of the hook is copying files git worktrees don't
# inherit.
mkdir -p "$SRC/.vercel"
echo '{"projectId":"x"}' > "$SRC/.vercel/project.json"
mkdir -p "$SRC/apps/shadowfax"
echo "DEV=1" > "$SRC/apps/shadowfax/.env.development.local"
mkdir -p "$SRC/packages/socotra-config"
echo "X=1" > "$SRC/packages/socotra-config/.env.local"
mkdir -p "$SRC/packages/ngrok"
echo "Y=1" > "$SRC/packages/ngrok/.env.local"

run_script() {
  # run_script <worktree-path> -> sets OUT and RC.
  set +e
  OUT="$("$SCRIPT" "$1" 2>&1)"
  RC=$?
  set -e
}

# ── RED: fresh worktree, no node_modules, no apps/shadowfax/.env.local ──────
WT1="$T/wt1"
git -C "$SRC" worktree add -q "$WT1" -b wt1 >/dev/null

run_script "$WT1"

if echo "$OUT" | grep -q "worktree setup complete"; then
  fail "RED (fresh): expected no 'complete' line, got: $OUT"
else
  pass "RED (fresh): no 'complete' line printed"
fi
if echo "$OUT" | grep -q "NOT provisioned" && echo "$OUT" | grep -q "apps/shadowfax/.env.local"; then
  pass "RED (fresh): not-provisioned verdict names apps/shadowfax/.env.local"
else
  fail "RED (fresh): expected not-provisioned verdict naming apps/shadowfax/.env.local, got: $OUT"
fi
if [ "$RC" -ne 0 ]; then
  pass "RED (fresh): exit code nonzero ($RC)"
else
  fail "RED (fresh): expected nonzero exit, got 0"
fi

# ── RED: 0-byte apps/shadowfax/.env.local is treated as missing ─────────────
WT2="$T/wt2"
git -C "$SRC" worktree add -q "$WT2" -b wt2 >/dev/null
mkdir -p "$WT2/apps/shadowfax"
: > "$WT2/apps/shadowfax/.env.local"

run_script "$WT2"

if echo "$OUT" | grep -q "worktree setup complete"; then
  fail "RED (0-byte): expected no 'complete' line, got: $OUT"
else
  pass "RED (0-byte): no 'complete' line printed"
fi
if echo "$OUT" | grep -q "apps/shadowfax/.env.local"; then
  pass "RED (0-byte): verdict names apps/shadowfax/.env.local"
else
  fail "RED (0-byte): expected verdict to name apps/shadowfax/.env.local, got: $OUT"
fi
if [ "$RC" -ne 0 ]; then
  pass "RED (0-byte): exit code nonzero ($RC)"
else
  fail "RED (0-byte): expected nonzero exit, got 0"
fi

# ── GREEN: apps/shadowfax/.env.local pre-placed non-empty (simulated pull) ──
WT3="$T/wt3"
git -C "$SRC" worktree add -q "$WT3" -b wt3 >/dev/null
mkdir -p "$WT3/apps/shadowfax"
echo "PULLED=1" > "$WT3/apps/shadowfax/.env.local"

run_script "$WT3"

if echo "$OUT" | grep -q "worktree setup complete"; then
  pass "GREEN: 'complete' line printed"
else
  fail "GREEN: expected 'complete' line, got: $OUT"
fi
if [ "$RC" -eq 0 ]; then
  pass "GREEN: exit code 0"
else
  fail "GREEN: expected exit 0, got $RC"
fi
# Copied LOCAL_ENV_FILES land alongside the pre-placed pull output.
for rel in apps/shadowfax/.env.development.local packages/socotra-config/.env.local packages/ngrok/.env.local; do
  if [ -s "$WT3/$rel" ]; then
    pass "GREEN: $rel copied"
  else
    fail "GREEN: expected $rel to be copied and non-empty"
  fi
done

# ── secrets discipline: never print env file contents ────────────────────────
if echo "$OUT" | grep -qE "PULLED=1|DEV=1|X=1|Y=1"; then
  fail "output leaked env file contents"
else
  pass "no env file contents in output"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
