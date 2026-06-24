#!/usr/bin/env bash
# Edge-case tests for plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
# Authored by tester (implementer wrote core-path tests in hook-debug-log.test.sh).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh"

PASS=0; FAIL=0
pass() { echo "PASS $1"; PASS=$((PASS+1)); }
fail() { echo "FAIL $1: $2"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

# ── Case 4: malformed / non-JSON stdin → session_id 'unknown', no crash ────────
(
  ATH="$T/case4"
  export ATH
  stdin_bad="this is not json at all }{{}}"
  HOOK_SESSION_ID=$(printf '%s' "$stdin_bad" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
  export HOOK_SESSION_ID
  . "$LIB"
  hook_debug_log "test-script.sh" "at-edge01"
)
log4="$T/case4/debug/hooks.log"
if [ ! -f "$log4" ]; then
  fail "case4" "log file not created"
else
  sid4=$(cut -f2 < "$log4")
  if [ "$sid4" = "unknown" ]; then
    pass "case4: malformed stdin -> session_id 'unknown'"
  else
    fail "case4" "expected 'unknown', got '$sid4'"
  fi
fi

# ── Case 5: stdin JSON missing session_id field → 'unknown' ────────────────────
(
  ATH="$T/case5"
  export ATH
  stdin_no_sid='{"hook_event_name":"Stop","cwd":"/some/path"}'
  HOOK_SESSION_ID=$(printf '%s' "$stdin_no_sid" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
  export HOOK_SESSION_ID
  . "$LIB"
  hook_debug_log "wake-watcher.sh" "at-edge02"
)
log5="$T/case5/debug/hooks.log"
if [ ! -f "$log5" ]; then
  fail "case5" "log file not created"
else
  sid5=$(cut -f2 < "$log5")
  if [ "$sid5" = "unknown" ]; then
    pass "case5: JSON missing session_id field -> 'unknown'"
  else
    fail "case5" "expected 'unknown', got '$sid5'"
  fi
fi

# ── Case 6: missing jq on PATH → graceful 'unknown', no crash ──────────────────
# Simulate by using a PATH that has no jq. The hook pattern is:
#   HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '...' 2>/dev/null || echo "unknown")
# The '|| echo unknown' guard must absorb the jq-not-found error.
(
  ATH="$T/case6"
  export ATH
  stdin_json='{"session_id":"should-not-appear"}'
  ORIGINAL_PATH="$PATH"
  # Strip real PATH; provide only minimal builtins dir (printf, echo etc. are builtins so PATH irrelevant)
  PATH="$T/empty-bin"
  /bin/mkdir -p "$T/empty-bin"
  HOOK_SESSION_ID=$(printf '%s' "$stdin_json" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
  PATH="$ORIGINAL_PATH"
  export HOOK_SESSION_ID
  . "$LIB"
  hook_debug_log "inbox-drain.sh" "at-edge03"
)
log6="$T/case6/debug/hooks.log"
if [ ! -f "$log6" ]; then
  fail "case6" "log file not created when jq absent"
else
  sid6=$(cut -f2 < "$log6")
  if [ "$sid6" = "unknown" ]; then
    pass "case6: missing jq -> session_id 'unknown', no crash"
  else
    fail "case6" "expected 'unknown' when jq absent, got '$sid6'"
  fi
fi

# ── Case 7: log is append-only — two invocations produce two lines ──────────────
(
  ATH="$T/case7"
  export ATH
  HOOK_SESSION_ID="session-first"
  export HOOK_SESSION_ID
  . "$LIB"
  hook_debug_log "wake-watcher.sh" "at-edge04"
)
(
  ATH="$T/case7"
  export ATH
  HOOK_SESSION_ID="session-second"
  export HOOK_SESSION_ID
  . "$LIB"
  hook_debug_log "wake-watcher.sh" "at-edge04"
)
log7="$T/case7/debug/hooks.log"
if [ ! -f "$log7" ]; then
  fail "case7" "log file not created"
else
  count7=$(wc -l < "$log7" | tr -d ' ')
  if [ "$count7" = "2" ]; then
    pass "case7: two invocations -> two lines (append-only)"
  else
    fail "case7" "expected 2 lines, got $count7"
  fi
  # Verify first session id appears
  if grep -q "session-first" "$log7" && grep -q "session-second" "$log7"; then
    pass "case7b: both session ids present in log"
  else
    fail "case7b" "missing session ids; log:\n$(cat "$log7")"
  fi
fi

# ── Case 8: debug/ dir is created if absent ──────────────────────────────────────
# The ATH dir exists but debug/ subdirectory does NOT — mkdir -p must create it.
mkdir -p "$T/case8/agent-teams"  # ATH dir without debug/ subdir
(
  ATH="$T/case8/agent-teams"
  export ATH
  HOOK_SESSION_ID="dir-creation-test"
  export HOOK_SESSION_ID
  . "$LIB"
  hook_debug_log "session-start-inbox.sh" "at-edge05"
)
log8="$T/case8/agent-teams/debug/hooks.log"
if [ -f "$log8" ]; then
  pass "case8: debug/ dir created if absent, log file written"
else
  fail "case8" "log file not found at $log8 (debug/ dir not created)"
fi

# ── Case 9: HOOK_SESSION_ID explicitly empty string → falls back to 'unknown' ───
(
  ATH="$T/case9"
  export ATH
  HOOK_SESSION_ID=""
  export HOOK_SESSION_ID
  . "$LIB"
  hook_debug_log "wake-watcher.sh" "at-edge06"
)
log9="$T/case9/debug/hooks.log"
if [ ! -f "$log9" ]; then
  fail "case9" "log file not created"
else
  sid9=$(cut -f2 < "$log9")
  if [ "$sid9" = "unknown" ]; then
    pass "case9: empty HOOK_SESSION_ID -> 'unknown'"
  else
    fail "case9" "expected 'unknown' for empty session id, got '$sid9'"
  fi
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
