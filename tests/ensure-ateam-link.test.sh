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

run_hook() {
  local plugin_root="$1" link="$2"
  CLAUDE_PLUGIN_ROOT="$plugin_root" AGENT_TEAMS_ATEAM_LINK="$link" "$SCRIPT"
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
echo "PASS T5 (no-clobber — a hand-placed regular file is left byte-identical)"

# ── T6: IDEMPOTENT NO-OP — running twice against an already-current link ───
LINK="$T/case6/local-bin/ateam"; assert_scratch_path "$LINK"
mkdir -p "$(dirname "$LINK")"
out1=$(run_hook "$VERSION_B" "$LINK") || fail "T6 (idempotent): first hook run failed"
[ -z "$out1" ] || fail "T6 (idempotent): first run printed to stdout: '$out1' — SessionStart stdout is injected into every session's context and must stay silent"
target1="$(readlink "$LINK")" || fail "T6 (idempotent): link missing after first run"
out2=$(run_hook "$VERSION_B" "$LINK") || fail "T6 (idempotent): second hook run failed"
[ -z "$out2" ] || fail "T6 (idempotent): second run printed to stdout: '$out2'"
target2="$(readlink "$LINK")" || fail "T6 (idempotent): link missing after second run"
[ "$target1" = "$target2" ] || fail "T6 (idempotent): link target changed between runs ('$target1' -> '$target2')"
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
echo "PASS T7 (directory guard — a directory at the link path is never touched)"

echo "PASS"
