#!/usr/bin/env bash
# Tests for the ensure-ateam-link SessionStart hook script.
#
# WHY THIS TEST EXISTS (see agent-teams-wtf3.1, the frozen contract): a
# marketplace plugin update rewrites installed_plugins.json but never touches
# ~/.local/bin/ateam, which /setup-agent-teams symlinked straight at the OLD
# version's resolved wrapper. ~/.local/bin sorts before the plugin bin/
# directory Claude Code auto-adds to PATH, so the stale link SHADOWS the
# correct, self-updating entry — silently, forever, until this hook re-points
# it on every session start.
#
# T2 below is the load-bearing case: it proves the stale link keeps running
# the OLD version with the hook NOT invoked. If T2 cannot fail, T1 proves
# nothing — a check is only worth what it can witness.
#
# Behavior under test is the frozen contract in agent-teams-wtf3.1. Do not
# read the implementation script to write these assertions; it may not exist
# yet (built in parallel) and reading it would test the implementation
# against itself instead of against the spec.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/ensure-ateam-link.sh"
WRAPPER_SRC="$ROOT/plugins/agent-teams/bin/ateam"

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

fail() { echo "FAIL $1"; exit 1; }

# Every path this test touches must live under $T. Called before any write
# to a case's link path, per the bead's acceptance criterion: never let a
# typo in a case send this test anywhere near the real $HOME.
assert_scratch_path() {
  case "$1" in
    "$T"/*) ;;
    *) echo "FAIL safety: path '$1' is not under scratch dir $T — refusing to touch it"; exit 1 ;;
  esac
}

# lib/hook-debug-log.sh derives its log path from AGENT_TEAMS_HOME (falling
# back to ~/.agent-teams). Pin it to scratch and export once so every
# invocation of $SCRIPT below inherits it — never let the hook read or
# write the real ~/.agent-teams/debug/hooks.log, which live sessions on
# this machine are appending to right now.
AGENT_TEAMS_HOME="$T/ateam-home"; assert_scratch_path "$AGENT_TEAMS_HOME"
export AGENT_TEAMS_HOME
HOOKS_LOG="$AGENT_TEAMS_HOME/debug/hooks.log"

# ── Fake "plugin version" fixtures ───────────────────────────────────────────
# Each version dir mimics a real plugin install root: bin/ateam is a copy of
# the committed POSIX dispatch wrapper, and bin/ateam-<os>-<arch> is a stub
# for the CURRENT platform that just prints which "version" answered — so
# assertions run the resolved binary and check what it PRINTS, never just
# readlink.
PLATFORM_OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in
    x86_64)         PLATFORM_ARCH=amd64 ;;
    aarch64|arm64)  PLATFORM_ARCH=arm64 ;;
    *)              PLATFORM_ARCH="$(uname -m)" ;;
esac

make_version() {
  local dir="$1" label="$2"
  mkdir -p "$dir/bin"
  cp "$WRAPPER_SRC" "$dir/bin/ateam"
  chmod +x "$dir/bin/ateam"
  printf '#!/bin/sh\necho "%s"\n' "$label" > "$dir/bin/ateam-${PLATFORM_OS}-${PLATFORM_ARCH}"
  chmod +x "$dir/bin/ateam-${PLATFORM_OS}-${PLATFORM_ARCH}"
}

# Shared, read-only fixtures reused across cases. T4 needs its own throwaway
# copy of "version A" since it deletes it.
VERSION_A="$T/versionA"; make_version "$VERSION_A" "VERSION-A"
VERSION_B="$T/versionB"; make_version "$VERSION_B" "VERSION-B"

# Contract C7: the hook must NEVER fail a session — every path exits 0.
# Asserted explicitly rather than relying on `set -e` to abort the suite: a
# bare abort IS caught, but reports nothing about which invariant broke. This
# is not hypothetical — an unbound $ATH referenced by lib/hook-debug-log.sh
# made the hook exit 1 on every invocation during development, and stdout
# stayed empty throughout, so an stdout-only check would have called it clean.
run_hook() {
  local plugin_root="$1" link="$2" rc=0
  CLAUDE_PLUGIN_ROOT="$plugin_root" AGENT_TEAMS_ATEAM_LINK="$link" "$SCRIPT" || rc=$?
  [ "$rc" -eq 0 ] || fail "C7 (never fail a session): hook exited $rc, want 0"
}

# assert_reason <label> <expected-reason> — C6 freezes exactly five exit
# reasons (no-wrapper, already-current, not-a-symlink, relinked, created).
# Reads the newest "exit" line from the scratch debug log (HOOKS_LOG, under
# AGENT_TEAMS_HOME above) and checks its reason= matches the contract case
# just exercised. Call immediately after the hook run being checked, since
# the log is a single accumulating file shared by every case in this suite.
assert_reason() {
  local label="$1" want="$2" got
  got="$(awk -F'\t' '$5=="exit"{line=$6} END{print line}' "$HOOKS_LOG" 2>/dev/null | sed -n 's/.*reason=//p')"
  [ "$got" = "$want" ] || fail "$label: HOOK_EXIT_REASON was '${got:-<none>}', want '$want' (C6)"
}

# ── T2: MUTATION PROOF — must be checkable with NO implementation present ───
# Placed first (not T1) so a single run of this file demonstrates the
# implementation-independent case going green even while T1 (which requires
# the hook) fails for lack of a script. Same fixture as T1: link pre-pinned
# to version A, simulating a past /setup-agent-teams run — but the hook is
# never invoked.
LINK="$T/case2/local-bin/ateam"; assert_scratch_path "$LINK"
mkdir -p "$(dirname "$LINK")"
ln -s "$VERSION_A/bin/ateam" "$LINK"
out=$("$LINK") || fail "T2 (mutation proof): pre-pinned link failed to execute"
[ "$out" = "VERSION-A" ] || fail "T2 (mutation proof): stale link ran '$out', want 'VERSION-A' — without the hook the link must NOT self-heal"
echo "PASS T2 (mutation proof — stale link still runs the OLD version without the hook)"

# ── T1: VERSION BUMP — the loop-closing witness ──────────────────────────────
LINK="$T/case1/local-bin/ateam"; assert_scratch_path "$LINK"
mkdir -p "$(dirname "$LINK")"
ln -s "$VERSION_A/bin/ateam" "$LINK"
run_hook "$VERSION_B" "$LINK"
out=$("$LINK") || fail "T1 (version bump): link failed to execute after the hook ran"
[ "$out" = "VERSION-B" ] || fail "T1 (version bump): link ran '$out' after the hook, want 'VERSION-B'"
assert_reason "T1 (version bump)" "relinked"
echo "PASS T1 (version bump — hook re-points a stale link to the current version)"

# ── T3: SHADOWING — proves why this matters at all ──────────────────────────
LINK="$T/case3/local-bin/ateam"; assert_scratch_path "$LINK"
mkdir -p "$(dirname "$LINK")"
ln -s "$VERSION_A/bin/ateam" "$LINK"
SHADOW_PATH="$(dirname "$LINK"):$VERSION_B/bin:/usr/bin:/bin"
out=$(PATH="$SHADOW_PATH" ateam) || fail "T3 (shadowing): bare 'ateam' failed to execute before the hook ran"
[ "$out" = "VERSION-A" ] || fail "T3 (shadowing): bare 'ateam' ran '$out' before the hook, want 'VERSION-A' — the stale link must win over the correct plugin-bin entry"
run_hook "$VERSION_B" "$LINK"
out=$(PATH="$SHADOW_PATH" ateam) || fail "T3 (shadowing): bare 'ateam' failed to execute after the hook ran"
[ "$out" = "VERSION-B" ] || fail "T3 (shadowing): bare 'ateam' ran '$out' after the hook, want 'VERSION-B'"
echo "PASS T3 (shadowing — bare 'ateam' now resolves to the current version)"

# ── T4: DANGLING LINK HEALS ──────────────────────────────────────────────────
VERSION_A_CASE4="$T/case4/versionA"; make_version "$VERSION_A_CASE4" "VERSION-A"
LINK="$T/case4/local-bin/ateam"; assert_scratch_path "$LINK"
mkdir -p "$(dirname "$LINK")"
ln -s "$VERSION_A_CASE4/bin/ateam" "$LINK"
rm -rf "$VERSION_A_CASE4"
[ -L "$LINK" ] || fail "T4 (dangling): setup error — link is not a symlink before the hook ran"
[ ! -e "$LINK" ] || fail "T4 (dangling): setup error — link is not actually dangling"
run_hook "$VERSION_B" "$LINK"
[ -x "$LINK" ] || fail "T4 (dangling): link is not executable after the hook ran"
out=$("$LINK") || fail "T4 (dangling): link failed to execute after the hook ran"
[ "$out" = "VERSION-B" ] || fail "T4 (dangling): link ran '$out' after the hook, want 'VERSION-B'"
assert_reason "T4 (dangling)" "relinked"
echo "PASS T4 (dangling link heals to the current version)"

# ── T5: NO-CLOBBER — a regular file at the link path is never touched ──────
LINK="$T/case5/local-bin/ateam"; assert_scratch_path "$LINK"
mkdir -p "$(dirname "$LINK")"
printf 'a human put this file here by hand\n' > "$LINK"
BEFORE="$T/case5/before.txt"
cp "$LINK" "$BEFORE"
run_hook "$VERSION_B" "$LINK"
[ ! -L "$LINK" ] || fail "T5 (no-clobber): a hand-placed regular file was replaced with a symlink"
cmp -s "$BEFORE" "$LINK" || fail "T5 (no-clobber): file content changed after the hook ran"
assert_reason "T5 (no-clobber)" "not-a-symlink"
echo "PASS T5 (no-clobber — a hand-placed regular file is left byte-identical)"

# ── T6: IDEMPOTENT NO-OP — running twice against an already-current link ───
LINK="$T/case6/local-bin/ateam"; assert_scratch_path "$LINK"
mkdir -p "$(dirname "$LINK")"
out1=$(run_hook "$VERSION_B" "$LINK") || fail "T6 (idempotent): first hook run failed"
[ -z "$out1" ] || fail "T6 (idempotent): first run printed to stdout: '$out1' — SessionStart stdout is injected into every session's context and must stay silent"
target1="$(readlink "$LINK")" || fail "T6 (idempotent): link missing after first run"
assert_reason "T6 (idempotent, first run)" "created"
out2=$(run_hook "$VERSION_B" "$LINK") || fail "T6 (idempotent): second hook run failed"
[ -z "$out2" ] || fail "T6 (idempotent): second run printed to stdout: '$out2'"
target2="$(readlink "$LINK")" || fail "T6 (idempotent): link missing after second run"
[ "$target1" = "$target2" ] || fail "T6 (idempotent): link target changed between runs ('$target1' -> '$target2')"
assert_reason "T6 (idempotent, second run)" "already-current"
out=$("$LINK") || fail "T6 (idempotent): link failed to execute"
[ "$out" = "VERSION-B" ] || fail "T6 (idempotent): link ran '$out', want 'VERSION-B'"
echo "PASS T6 (idempotent no-op — repeat runs are silent and stable)"

# ── T7: DIRECTORY AT THE LINK PATH — regression guard for agent-teams-7ee ──
# ln -sf <target> <existing-dir> exits 0 and silently creates the link INSIDE
# the directory (producing .../ateam/ateam) instead of replacing it. A naive
# [ -f ] guard misses a directory; a naive [ -x ] guard mistakes it for an
# installed link. Both ship this trap green. C4(c)'s frozen test is
# [ -e "$LINK" ] && [ ! -L "$LINK" ], which catches it.
LINK="$T/case7/local-bin/ateam"; assert_scratch_path "$LINK"
mkdir -p "$LINK"
run_hook "$VERSION_B" "$LINK"
[ -d "$LINK" ] || fail "T7 (directory guard): link path is no longer a directory after the hook ran"
[ ! -e "$LINK/ateam" ] || fail "T7 (directory guard): hook created $LINK/ateam inside the directory (the ln -sf-into-a-directory trap)"
assert_reason "T7 (directory guard)" "not-a-symlink"
echo "PASS T7 (directory guard — a directory at the link path is never touched)"

# ── T8: HOME UNSET — regression guard for agent-teams-wtf3.8 (D1) ──────────
# A bare $HOME under set -u aborted the whole hook (C7 "never fail a
# session" violation) when HOME was unset. Case 1: override given, HOME
# unset -> normal operation must still succeed. Case 2: neither given ->
# there is no safe path to act on (an unset HOME must never degrade to
# writing under "/.local/bin/ateam", a root-owned path unrelated to any
# user) so the hook must no-op cleanly rather than crash or write there.
LINK="$T/case8/local-bin/ateam"; assert_scratch_path "$LINK"
rc=0
env -u HOME CLAUDE_PLUGIN_ROOT="$VERSION_B" AGENT_TEAMS_ATEAM_LINK="$LINK" "$SCRIPT" || rc=$?
[ "$rc" -eq 0 ] || fail "T8 (HOME unset, override set): hook exited $rc, want 0"
out=$("$LINK") || fail "T8 (HOME unset, override set): link failed to execute after the hook ran"
[ "$out" = "VERSION-B" ] || fail "T8 (HOME unset, override set): link ran '$out', want 'VERSION-B'"
echo "PASS T8a (HOME unset, override set — hook still self-heals)"

rc=0
env -u HOME -u AGENT_TEAMS_ATEAM_LINK CLAUDE_PLUGIN_ROOT="$VERSION_B" "$SCRIPT" || rc=$?
[ "$rc" -eq 0 ] || fail "T8 (HOME unset, no override): hook exited $rc, want 0"
[ ! -e "/.local/bin/ateam" ] || fail "T8 (HOME unset, no override): hook created /.local/bin/ateam — must never write under an empty HOME"
# Contract amendment (agent-teams-wtf3.1, ratified by DRI): the empty-LINK
# no-op is a SIXTH frozen exit reason, "no-home", added alongside C6's
# original five.
assert_reason "T8b (HOME unset, no override)" "no-home"
echo "PASS T8b (HOME unset, no override — no-op, nothing created)"

# ── T9: SELF-REFERENTIAL LINK — regression guard for agent-teams-wtf3.8 (D2) ─
# resolve_chain() had no cycle guard: a link pointing at itself (the state
# setup-agent-teams/SKILL.md:149 warns `ln -sf` can create) looped forever.
# A hanging SessionStart hook is worse than a failing one — it blocks
# session start. Bounded here to 8s wall-clock so a regression fails this
# test instead of hanging the whole suite.
LINK="$T/case9/local-bin/ateam"; assert_scratch_path "$LINK"
mkdir -p "$(dirname "$LINK")"
ln -sf ateam "$LINK"
( CLAUDE_PLUGIN_ROOT="$VERSION_B" AGENT_TEAMS_ATEAM_LINK="$LINK" "$SCRIPT" ) &
pid=$!
hung=0
for i in $(seq 1 8); do
  if ! kill -0 "$pid" 2>/dev/null; then
    wait "$pid"; rc=$?
    [ "$rc" -eq 0 ] || fail "T9 (self-referential): hook exited $rc, want 0"
    break
  fi
  sleep 1
  if [ "$i" -eq 8 ]; then
    hung=1
    kill -9 "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
done
[ "$hung" -eq 0 ] || fail "T9 (self-referential): hook was still running after 8s — HUNG"
out=$("$LINK") || fail "T9 (self-referential): link failed to execute after the hook ran"
[ "$out" = "VERSION-B" ] || fail "T9 (self-referential): link ran '$out' after the hook, want 'VERSION-B'"
# An unresolvable chain is routed to the existing case (d) relink path (see
# ensure-ateam-link.sh resolve_chain doc comment), so C6's label here is
# "relinked" — the same reason a stale-but-resolvable symlink gets.
assert_reason "T9 (self-referential)" "relinked"
echo "PASS T9 (self-referential link — hook terminates and heals the link)"

# ── T10: LONG LEGAL CHAIN — proves the hop bound doesn't break real chains ─
# A 3+ hop chain that already ends at WRAPPER must still be recognized as
# already-current and left untouched (case b), even with resolve_chain's
# new hop cap in place.
CASE10="$T/case10"
mkdir -p "$CASE10"
ln -sf "$VERSION_B/bin/ateam" "$CASE10/hop3"
ln -sf "$CASE10/hop3" "$CASE10/hop2"
ln -sf "$CASE10/hop2" "$CASE10/hop1"
LINK="$T/case10/local-bin/ateam"; assert_scratch_path "$LINK"
mkdir -p "$(dirname "$LINK")"
ln -sf "$CASE10/hop1" "$LINK"
BEFORE_TARGET="$(readlink "$LINK")"
run_hook "$VERSION_B" "$LINK"
AFTER_TARGET="$(readlink "$LINK")"
[ "$BEFORE_TARGET" = "$AFTER_TARGET" ] || fail "T10 (long legal chain): hook touched an already-current chain (target changed '$BEFORE_TARGET' -> '$AFTER_TARGET')"
out=$("$LINK") || fail "T10 (long legal chain): link failed to execute after the hook ran"
[ "$out" = "VERSION-B" ] || fail "T10 (long legal chain): link ran '$out', want 'VERSION-B'"
assert_reason "T10 (long legal chain)" "already-current"
echo "PASS T10 (long legal chain — already-current chain resolved correctly and left alone)"

# ── T11: WRAPPER NOT EXECUTABLE — regression guard, contract case (a) ──────
# Case (a) [ ! -x "$WRAPPER" ] was never exercised by any prior case: every
# fixture above passes a valid, executable wrapper. A hook that relinked
# anyway — to a nonexistent or non-executable wrapper — would break `ateam`
# for the user entirely, and nothing before this caught it.
# (i) CLAUDE_PLUGIN_ROOT has no bin/ateam at all.
CASE11_NO_BIN="$T/case11/no-bin-root"
mkdir -p "$CASE11_NO_BIN"
LINK="$T/case11/local-bin-a/ateam"; assert_scratch_path "$LINK"
mkdir -p "$(dirname "$LINK")"
ln -sf "$VERSION_A/bin/ateam" "$LINK"
BEFORE_TARGET="$(readlink "$LINK")"
rc=0
out=$(CLAUDE_PLUGIN_ROOT="$CASE11_NO_BIN" AGENT_TEAMS_ATEAM_LINK="$LINK" "$SCRIPT") || rc=$?
[ "$rc" -eq 0 ] || fail "T11a (no bin/ateam): hook exited $rc, want 0"
[ -z "$out" ] || fail "T11a (no bin/ateam): hook printed to stdout: '$out'"
AFTER_TARGET="$(readlink "$LINK")"
[ "$BEFORE_TARGET" = "$AFTER_TARGET" ] || fail "T11a (no bin/ateam): link target changed ('$BEFORE_TARGET' -> '$AFTER_TARGET') — must be left untouched when the wrapper doesn't exist"
assert_reason "T11a (no bin/ateam)" "no-wrapper"
echo "PASS T11a (CLAUDE_PLUGIN_ROOT with no bin/ateam — link left untouched)"

# (ii) bin/ateam exists but is not executable.
CASE11_NOT_X="$T/case11/not-executable-root"
mkdir -p "$CASE11_NOT_X/bin"
printf '#!/bin/sh\necho should-never-run\n' > "$CASE11_NOT_X/bin/ateam"
chmod -x "$CASE11_NOT_X/bin/ateam"
LINK="$T/case11/local-bin-b/ateam"; assert_scratch_path "$LINK"
mkdir -p "$(dirname "$LINK")"
ln -sf "$VERSION_A/bin/ateam" "$LINK"
BEFORE_TARGET="$(readlink "$LINK")"
rc=0
out=$(CLAUDE_PLUGIN_ROOT="$CASE11_NOT_X" AGENT_TEAMS_ATEAM_LINK="$LINK" "$SCRIPT") || rc=$?
[ "$rc" -eq 0 ] || fail "T11b (non-executable bin/ateam): hook exited $rc, want 0"
[ -z "$out" ] || fail "T11b (non-executable bin/ateam): hook printed to stdout: '$out'"
AFTER_TARGET="$(readlink "$LINK")"
[ "$BEFORE_TARGET" = "$AFTER_TARGET" ] || fail "T11b (non-executable bin/ateam): link target changed ('$BEFORE_TARGET' -> '$AFTER_TARGET') — must be left untouched when the wrapper isn't executable"
assert_reason "T11b (non-executable bin/ateam)" "no-wrapper"
echo "PASS T11b (CLAUDE_PLUGIN_ROOT with non-executable bin/ateam — link left untouched)"

echo "PASS"
