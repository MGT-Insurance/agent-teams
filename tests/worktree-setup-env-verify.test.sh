#!/usr/bin/env bash
# worktree-setup-env-verify.test.sh — coverage for scripts/midgard-worktree-setup.sh
# (agent-teams-xtac.1: the env-verification safety net; agent-teams-xtac.2: the
# script now runs `pnpm install` itself when node_modules is absent, before
# pulling).
#
# The synthetic source checkouts below have no real pnpm workspace, so a real
# `pnpm install`/`pnpm env:pull` can't run here. A fake `pnpm` stub is placed on
# PATH to exercise the script's control flow (did it call install? did it fall
# through to pull? did it surface failures correctly?) — this does NOT exercise
# a real install or a real vercel pull.
#
# Drives the real script against a synthetic source checkout + real git
# worktree (no midgard, no vercel CLI). Proves:
#   - a fresh worktree gets `pnpm install` run for it, then pulls, and reports
#     "complete" (GREEN);
#   - an install failure fails loudly, before any pull is attempted (RED);
#   - install+pull "succeeding" with no file actually landing still trips the
#     landed-file safety net (RED);
#   - exclusions (file absent in source; no .vercel at all) are unaffected.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/scripts/midgard-worktree-setup.sh"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

# ── fake pnpm on PATH ─────────────────────────────────────────────────────────
# Dispatches on $1: `install` mkdir's node_modules (or fails, per
# FAKE_PNPM_INSTALL=fail); `env:pull` writes a non-empty .env.local (or writes
# nothing, per FAKE_PNPM_PULL=empty). Never echoes any env content it wasn't
# told to write literally.
FAKEBIN="$T/fakebin"
mkdir -p "$FAKEBIN"
cat > "$FAKEBIN/pnpm" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  install)
    if [ "${FAKE_PNPM_INSTALL:-ok}" = "fail" ]; then
      echo "fake pnpm install: forced failure" >&2
      exit 1
    fi
    mkdir -p "$PWD/node_modules"
    exit 0
    ;;
  env:pull)
    if [ "${FAKE_PNPM_PULL:-ok}" = "empty" ]; then
      exit 0
    fi
    mkdir -p "$PWD/apps/shadowfax"
    echo "PULLED=1" > "$PWD/apps/shadowfax/.env.local"
    exit 0
    ;;
  *)
    echo "fake pnpm: unknown command $1" >&2
    exit 1
    ;;
esac
STUB
chmod +x "$FAKEBIN/pnpm"
export PATH="$FAKEBIN:$PATH"

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

# ── 1. GREEN: fresh worktree, fake pnpm installs then pulls successfully ────
WT1="$T/wt1"
git -C "$SRC" worktree add -q "$WT1" -b wt1 >/dev/null

export FAKE_PNPM_INSTALL=ok FAKE_PNPM_PULL=ok
run_script "$WT1"
unset FAKE_PNPM_INSTALL FAKE_PNPM_PULL

if echo "$OUT" | grep -q "installing dependencies"; then
  pass "1 (fresh install+pull): install line shown"
else
  fail "1 (fresh install+pull): expected install line, got: $OUT"
fi
if echo "$OUT" | grep -q "pulled vercel env"; then
  pass "1 (fresh install+pull): pull succeeded"
else
  fail "1 (fresh install+pull): expected pull-succeeded line, got: $OUT"
fi
if echo "$OUT" | grep -q "worktree setup complete"; then
  pass "1 (fresh install+pull): 'complete' line printed"
else
  fail "1 (fresh install+pull): expected 'complete' line, got: $OUT"
fi
if [ "$RC" -eq 0 ]; then
  pass "1 (fresh install+pull): exit code 0"
else
  fail "1 (fresh install+pull): expected exit 0, got $RC"
fi
GREEN_OUT="$OUT"

# ── 2. RED: fresh worktree, fake pnpm install fails ──────────────────────────
WT2="$T/wt2"
git -C "$SRC" worktree add -q "$WT2" -b wt2 >/dev/null

export FAKE_PNPM_INSTALL=fail
run_script "$WT2"
unset FAKE_PNPM_INSTALL

if echo "$OUT" | grep -q "pnpm install failed"; then
  pass "2 (install fails): install-failed message shown"
else
  fail "2 (install fails): expected install-failed message, got: $OUT"
fi
if echo "$OUT" | grep -q "worktree setup complete"; then
  fail "2 (install fails): expected no 'complete' line, got: $OUT"
else
  pass "2 (install fails): no 'complete' line printed"
fi
if echo "$OUT" | grep -q "pulled vercel env"; then
  fail "2 (install fails): pull must not have been attempted, got: $OUT"
else
  pass "2 (install fails): pull was not attempted"
fi
if [ "$RC" -ne 0 ]; then
  pass "2 (install fails): exit code nonzero ($RC)"
else
  fail "2 (install fails): expected nonzero exit, got 0"
fi

# ── 3. RED: install+pull "succeed" but env:pull produces no file ────────────
# The landed-file safety net (step 4) must still catch this.
WT3="$T/wt3"
git -C "$SRC" worktree add -q "$WT3" -b wt3 >/dev/null

export FAKE_PNPM_INSTALL=ok FAKE_PNPM_PULL=empty
run_script "$WT3"
unset FAKE_PNPM_INSTALL FAKE_PNPM_PULL

if echo "$OUT" | grep -q "worktree setup complete"; then
  fail "3 (empty pull): expected no 'complete' line, got: $OUT"
else
  pass "3 (empty pull): no 'complete' line printed"
fi
if echo "$OUT" | grep -q "NOT provisioned" && echo "$OUT" | grep -q "apps/shadowfax/.env.local"; then
  pass "3 (empty pull): not-provisioned verdict names apps/shadowfax/.env.local"
else
  fail "3 (empty pull): expected not-provisioned verdict naming apps/shadowfax/.env.local, got: $OUT"
fi
if [ "$RC" -ne 0 ]; then
  pass "3 (empty pull): exit code nonzero ($RC)"
else
  fail "3 (empty pull): expected nonzero exit, got 0"
fi

# ── 4a. exclusion: a LOCAL_ENV_FILES entry ABSENT from source is not required ─
# We can't copy what source never had, so its absence must NOT flag the
# worktree as not-provisioned. Source has .vercel + two of three local files
# (ngrok missing); fake pnpm installs + pulls successfully.
SRC2="$T/src2"
mkdir -p "$SRC2"
git -C "$SRC2" init -q
git -C "$SRC2" config user.email test@example.com
git -C "$SRC2" config user.name test
echo placeholder > "$SRC2/placeholder.txt"
git -C "$SRC2" add placeholder.txt
git -C "$SRC2" commit -q -m init
mkdir -p "$SRC2/.vercel"; echo '{"projectId":"x"}' > "$SRC2/.vercel/project.json"
mkdir -p "$SRC2/apps/shadowfax"; echo "DEV=1" > "$SRC2/apps/shadowfax/.env.development.local"
mkdir -p "$SRC2/packages/socotra-config"; echo "X=1" > "$SRC2/packages/socotra-config/.env.local"
# packages/ngrok/.env.local deliberately absent in source.

WT4="$T/wt4"
git -C "$SRC2" worktree add -q "$WT4" -b wt4 >/dev/null

export FAKE_PNPM_INSTALL=ok FAKE_PNPM_PULL=ok
run_script "$WT4"
unset FAKE_PNPM_INSTALL FAKE_PNPM_PULL

if echo "$OUT" | grep -q "worktree setup complete"; then
  pass "4a (source-missing file): reports complete"
else
  fail "4a (source-missing file): expected complete, got: $OUT"
fi
if [ "$RC" -eq 0 ]; then
  pass "4a (source-missing file): exit 0"
else
  fail "4a (source-missing file): expected exit 0, got $RC"
fi
if echo "$OUT" | grep -q "NOT provisioned"; then
  fail "4a (source-missing file): must NOT flag the source-absent ngrok file, got: $OUT"
else
  pass "4a (source-missing file): a file missing in source is not required"
fi

# ── 4b. exclusion: source WITHOUT .vercel — no install attempted, no pull ───
# No .vercel in source → no vercel-backed env is expected, so no install and
# no pull should be attempted, and a missing apps/shadowfax/.env.local must
# NOT flag the worktree. All local files present.
SRC3="$T/src3"
mkdir -p "$SRC3"
git -C "$SRC3" init -q
git -C "$SRC3" config user.email test@example.com
git -C "$SRC3" config user.name test
echo placeholder > "$SRC3/placeholder.txt"
git -C "$SRC3" add placeholder.txt
git -C "$SRC3" commit -q -m init
mkdir -p "$SRC3/apps/shadowfax"; echo "DEV=1" > "$SRC3/apps/shadowfax/.env.development.local"
mkdir -p "$SRC3/packages/socotra-config"; echo "X=1" > "$SRC3/packages/socotra-config/.env.local"
mkdir -p "$SRC3/packages/ngrok"; echo "Y=1" > "$SRC3/packages/ngrok/.env.local"

WT5="$T/wt5"
git -C "$SRC3" worktree add -q "$WT5" -b wt5 >/dev/null

run_script "$WT5"

if echo "$OUT" | grep -q "worktree setup complete"; then
  pass "4b (no-.vercel): reports complete"
else
  fail "4b (no-.vercel): expected complete, got: $OUT"
fi
if [ "$RC" -eq 0 ]; then
  pass "4b (no-.vercel): exit 0"
else
  fail "4b (no-.vercel): expected exit 0, got $RC"
fi
if echo "$OUT" | grep -q "NOT provisioned"; then
  fail "4b (no-.vercel): must NOT require the pulled file when source has no .vercel, got: $OUT"
else
  pass "4b (no-.vercel): the vercel-pulled file is not required without .vercel"
fi
if echo "$OUT" | grep -q "installing dependencies"; then
  fail "4b (no-.vercel): no install should have been attempted, got: $OUT"
else
  pass "4b (no-.vercel): no install attempted"
fi

# ── 5. secrets discipline: never print env file contents ────────────────────
if echo "$GREEN_OUT" | grep -qE "PULLED=1|DEV=1|X=1|Y=1"; then
  fail "output leaked env file contents"
else
  pass "no env file contents in output"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
