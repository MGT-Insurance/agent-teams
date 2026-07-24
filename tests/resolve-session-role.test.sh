#!/usr/bin/env bash
# resolve-session-role.test.sh — unit coverage for the shared session-role
# resolution lib (agent-teams-7ew5.2.1 CONTRACT): valid_session_id,
# dri_mark_session/dri_cleanup_session_marker round-trip, and
# resolve_session_role's steward-first ordering.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/plugins/agent-teams/hooks/scripts/lib"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME"
export ATH="$AGENT_TEAMS_HOME"

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/resolve-steward.sh
. "$LIB/resolve-steward.sh"
# shellcheck source=plugins/agent-teams/hooks/scripts/lib/resolve-session-role.sh
. "$LIB/resolve-session-role.sh"

# ── valid_session_id ──────────────────────────────────────────────────────────
if valid_session_id "a1b2c3d4-e5f6-7890-abcd-ef1234567890"; then
  pass "valid_session_id accepts a UUID-like id"
else
  fail "valid_session_id rejected a UUID-like id"
fi

if valid_session_id "../etc/passwd"; then
  fail "valid_session_id accepted a path-traversal string"
else
  pass "valid_session_id rejects ../etc/passwd"
fi

if valid_session_id ""; then
  fail "valid_session_id accepted an empty string"
else
  pass "valid_session_id rejects empty string"
fi

if valid_session_id "abc/def"; then
  fail "valid_session_id accepted a slash-containing id"
else
  pass "valid_session_id rejects a slash-containing id"
fi

# ── dri_mark_session / dri_cleanup_session_marker round-trip ────────────────
SID="test-session-0001"
marker=$(dri_session_marker_path "$ATH" "$SID")

CLAUDE_CODE_SESSION_ID="$SID" dri_mark_session "$ATH"
if [ -f "$marker" ]; then
  pass "dri_mark_session created the marker"
else
  fail "dri_mark_session did not create the marker at $marker"
fi

dri_cleanup_session_marker "$ATH" "$SID"
if [ -f "$marker" ]; then
  fail "dri_cleanup_session_marker did not remove the marker"
else
  pass "dri_cleanup_session_marker removed the marker"
fi

# dri_mark_session no-ops silently on an invalid/absent env var
rm -rf "${ATH:?}/dri-sessions"
unset CLAUDE_CODE_SESSION_ID || true
dri_mark_session "$ATH"
if [ -d "$ATH/dri-sessions" ] && [ -n "$(ls -A "$ATH/dri-sessions" 2>/dev/null)" ]; then
  fail "dri_mark_session created a marker despite no session id in the environment"
else
  pass "dri_mark_session no-ops silently with no CLAUDE_CODE_SESSION_ID"
fi

# ── resolve_session_role ordering ────────────────────────────────────────────
# Case: no steward marker, no dri marker -> empty
role=$(resolve_session_role "$ATH" "$SID")
if [ -z "$role" ]; then
  pass "resolve_session_role returns empty with no steward cwd and no dri marker"
else
  fail "resolve_session_role returned '$role', expected empty"
fi

# Case: dri marker present for this session_id, not steward cwd -> dri
CLAUDE_CODE_SESSION_ID="$SID" dri_mark_session "$ATH"
role=$(resolve_session_role "$ATH" "$SID")
if [ "$role" = "dri" ]; then
  pass "resolve_session_role returns dri when only the dri marker matches"
else
  fail "resolve_session_role returned '$role', expected dri"
fi

# Case: steward cwd true AND a dri marker also present for this session_id ->
# steward wins (never misidentified as dri).
STEWARD_DIR="$AGENT_TEAMS_HOME/steward/session"
mkdir -p "$STEWARD_DIR"
: > "$STEWARD_DIR/.steward-session"

role=$(cd "$STEWARD_DIR" && resolve_session_role "$ATH" "$SID")
if [ "$role" = "steward" ]; then
  pass "resolve_session_role returns steward even with a dri marker present"
else
  fail "resolve_session_role returned '$role', expected steward"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
