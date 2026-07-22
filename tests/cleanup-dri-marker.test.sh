#!/usr/bin/env bash
# cleanup-dri-marker.test.sh — coverage for the SessionEnd DRI-marker
# cleanup hook (agent-teams-7ew5.2.5).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HOOKS="$ROOT/plugins/agent-teams/hooks/scripts"
SCRIPT="$HOOKS/cleanup-dri-marker.sh"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME"

run_hook() {
  # run_hook <session_id>
  local sid="$1"
  printf '{"session_id":"%s"}' "$sid" | "$SCRIPT" 2>/dev/null
}

# ── Case: a dri marker exists for the given session_id -> removed ───────────
SID="dri-sess-cleanup-0001"
mkdir -p "$AGENT_TEAMS_HOME/dri-sessions"
: > "$AGENT_TEAMS_HOME/dri-sessions/$SID"

run_hook "$SID"

if [ -f "$AGENT_TEAMS_HOME/dri-sessions/$SID" ]; then
  fail "marker still present after cleanup-dri-marker.sh"
else
  pass "marker removed by cleanup-dri-marker.sh"
fi

# ── Case: no marker exists for the given session_id -> still exits 0, no error ─
set +e
printf '{"session_id":"no-such-session"}' | "$SCRIPT" 2>/dev/null
rc=$?
set -e
if [ "$rc" -eq 0 ]; then
  pass "no marker present -> exits 0"
else
  fail "no marker present -> expected exit 0, got $rc"
fi

# ── Case: .steward-session marker untouched regardless of session_id ────────
STEWARD_DIR="$AGENT_TEAMS_HOME/steward/session"
mkdir -p "$STEWARD_DIR"
: > "$STEWARD_DIR/.steward-session"

run_hook "$SID"
run_hook "some-other-session-id"

if [ -f "$STEWARD_DIR/.steward-session" ]; then
  pass ".steward-session marker untouched"
else
  fail ".steward-session marker was removed (should never happen)"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
