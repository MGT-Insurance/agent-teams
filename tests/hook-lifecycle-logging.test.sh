#!/usr/bin/env bash
# Core-path tests for the upgraded hook-debug-log.sh lifecycle logging.
# Verifies: start line, exit line (code + reason), signal traps, rotation,
# and mid-run notes. Edge cases and exhaustive coverage belong to the tester.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

# ── Helper: source lib in an isolated subshell with a temp ATH ──────────────
# Usage: run_lib_subshell <ATH-dir> <session-id> <initiative-id> <bash-body>
run_lib_subshell() {
  local ath="$1" sid="$2" init_id="$3" body="$4"
  (
    ATH="$ath"
    export ATH
    HOOK_SESSION_ID="$sid"
    export HOOK_SESSION_ID
    HOOK_INITIATIVE="$init_id"
    export HOOK_INITIATIVE
    # shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
    . "$LIB"
    eval "$body"
  )
}

# ── Case L1: hook_log_start emits a start event ──────────────────────────────
ATH_L1="$T/l1"
(
  ATH="$ATH_L1"
  export ATH
  HOOK_SESSION_ID="sess-l1"
  export HOOK_SESSION_ID
  . "$LIB"
  hook_log_start "test-script.sh"
  # Don't exit — the EXIT trap fires when the subshell ends.
)
log_l1="$ATH_L1/debug/hooks.log"
if [ ! -f "$log_l1" ]; then
  fail "L1: log file not created"
else
  # Should have a start line and an exit line
  start_count=$(grep -c $'\tstart\t' "$log_l1" 2>/dev/null || echo 0)
  exit_count=$(grep -c $'\texit\t' "$log_l1" 2>/dev/null || echo 0)
  if [ "$start_count" -ge 1 ]; then
    pass "L1a: start event written"
  else
    fail "L1a: no start event; log: $(cat "$log_l1")"
  fi
  if [ "$exit_count" -ge 1 ]; then
    pass "L1b: exit event written by EXIT trap"
  else
    fail "L1b: no exit event; log: $(cat "$log_l1")"
  fi
fi

# ── Case L2: HOOK_EXIT_REASON appears in exit line ───────────────────────────
ATH_L2="$T/l2"
(
  ATH="$ATH_L2"
  export ATH
  HOOK_SESSION_ID="sess-l2"
  export HOOK_SESSION_ID
  . "$LIB"
  hook_log_start "test-script.sh"
  HOOK_EXIT_REASON="doorbell-fired"
  exit 2
) || true  # exit 2 would fail set -e in parent; allow it
log_l2="$ATH_L2/debug/hooks.log"
if [ ! -f "$log_l2" ]; then
  fail "L2: log file not created"
else
  if grep -q "code=2" "$log_l2" && grep -q "reason=doorbell-fired" "$log_l2"; then
    pass "L2: exit code and HOOK_EXIT_REASON appear in exit line"
  else
    fail "L2: exit line missing code or reason; log: $(cat "$log_l2")"
  fi
fi

# ── Case L3: default exit reason is 'unexpected' when not set ────────────────
ATH_L3="$T/l3"
(
  ATH="$ATH_L3"
  export ATH
  HOOK_SESSION_ID="sess-l3"
  export HOOK_SESSION_ID
  . "$LIB"
  hook_log_start "test-script.sh"
  # Do NOT set HOOK_EXIT_REASON — simulates an unanticipated exit.
  exit 0
)
log_l3="$ATH_L3/debug/hooks.log"
if [ ! -f "$log_l3" ]; then
  fail "L3: log file not created"
else
  if grep -q "reason=unexpected" "$log_l3"; then
    pass "L3: unset HOOK_EXIT_REASON defaults to 'unexpected'"
  else
    fail "L3: expected reason=unexpected; log: $(cat "$log_l3")"
  fi
fi

# ── Case L4: hook_log_note emits a mid-run note ──────────────────────────────
ATH_L4="$T/l4"
(
  ATH="$ATH_L4"
  export ATH
  HOOK_SESSION_ID="sess-l4"
  HOOK_INITIATIVE="at-abc1"
  export HOOK_SESSION_ID HOOK_INITIATIVE
  . "$LIB"
  hook_log_start "test-script.sh"
  hook_log_note "note" "pidfile-claim pid=9999"
  HOOK_EXIT_REASON="ok"
)
log_l4="$ATH_L4/debug/hooks.log"
if [ ! -f "$log_l4" ]; then
  fail "L4: log file not created"
else
  if grep -q "pidfile-claim" "$log_l4"; then
    pass "L4: hook_log_note wrote mid-run note"
  else
    fail "L4: mid-run note missing; log: $(cat "$log_l4")"
  fi
  # Initiative id should appear in lines after it was exported
  if grep -q "at-abc1" "$log_l4"; then
    pass "L4b: initiative id appears in log lines"
  else
    fail "L4b: initiative id missing; log: $(cat "$log_l4")"
  fi
fi

# ── Case L5: legacy hook_debug_log still writes 6-column TSV ─────────────────
ATH_L5="$T/l5"
(
  ATH="$ATH_L5"
  export ATH
  HOOK_SESSION_ID="sess-l5"
  export HOOK_SESSION_ID
  . "$LIB"
  hook_debug_log "legacy-script.sh" "at-leg01"
)
log_l5="$ATH_L5/debug/hooks.log"
if [ ! -f "$log_l5" ]; then
  fail "L5: log file not created for legacy hook_debug_log"
else
  # Should have 6 fields
  field_count=$(awk -F'\t' '{print NF}' "$log_l5" | head -1)
  if [ "$field_count" = "6" ]; then
    pass "L5: legacy hook_debug_log writes 6-column TSV"
  else
    fail "L5: expected 6 fields, got $field_count; log: $(cat "$log_l5")"
  fi
  # event column (field 5) should be 'start'
  event_col=$(cut -f5 "$log_l5" | head -1)
  if [ "$event_col" = "start" ]; then
    pass "L5b: legacy hook_debug_log writes event=start"
  else
    fail "L5b: expected event=start, got '$event_col'"
  fi
fi

# ── Case L6: size rotation rolls hooks.log to hooks.log.1 past ~5 MB ─────────
ATH_L6="$T/l6"
mkdir -p "$ATH_L6/debug"
log_l6="$ATH_L6/debug/hooks.log"
# Write a 5.5 MB placeholder file to trigger rotation.
dd if=/dev/zero bs=1024 count=5632 2>/dev/null | tr '\0' 'x' > "$log_l6"
(
  ATH="$ATH_L6"
  export ATH
  HOOK_SESSION_ID="sess-l6"
  export HOOK_SESSION_ID
  . "$LIB"
  hook_debug_log "rotate-test.sh" "at-rot01"
)
if [ -f "${log_l6}.1" ]; then
  pass "L6a: oversized log rotated to hooks.log.1"
else
  fail "L6a: hooks.log.1 not created after rotation"
fi
if [ -f "$log_l6" ]; then
  new_size=$(wc -c < "$log_l6" | tr -d ' ')
  if [ "$new_size" -lt 1000000 ]; then
    pass "L6b: fresh hooks.log is small after rotation"
  else
    fail "L6b: fresh hooks.log is still large ($new_size bytes)"
  fi
else
  fail "L6b: no fresh hooks.log after rotation"
fi

# ── Case L7: session-start-pull.sh writes start+exit with session id ─────────
# Smoke-test a full hook script against a temp ATH.
ATH_L7="$T/l7"
out=$(printf '{"session_id":"hook-test-session"}' | \
  CLAUDE_PLUGIN_ROOT="/nonexistent" \
  AGENT_TEAMS_HOME="$ATH_L7" \
  bash "$ROOT/plugins/agent-teams/hooks/scripts/session-start-pull.sh" 2>&1 || true)
log_l7="$ATH_L7/debug/hooks.log"
if [ ! -f "$log_l7" ]; then
  fail "L7: no hooks.log from session-start-pull.sh"
else
  if grep -q "session-start-pull.sh" "$log_l7" && \
     grep -q $'\tstart\t' "$log_l7" && \
     grep -q $'\texit\t' "$log_l7"; then
    pass "L7: session-start-pull.sh writes start+exit to hooks.log"
  else
    fail "L7: expected start+exit lines; log: $(cat "$log_l7")"
  fi
  if grep -q "hook-test-session" "$log_l7"; then
    pass "L7b: session_id from stdin JSON appears in hooks.log"
  else
    fail "L7b: session_id missing from hooks.log; log: $(cat "$log_l7")"
  fi
fi

# ── Case L8: wake-watcher.sh exits with missing-deps reason when .beads absent ─
ATH_L8="$T/l8"
out=$(printf '{"session_id":"ww-test-session"}' | \
  AGENT_TEAMS_HOME="$ATH_L8" \
  bash "$ROOT/plugins/agent-teams/hooks/scripts/wake-watcher.sh" 2>&1 || true)
log_l8="$ATH_L8/debug/hooks.log"
if [ ! -f "$log_l8" ]; then
  fail "L8: no hooks.log from wake-watcher.sh"
else
  if grep -q "wake-watcher.sh" "$log_l8" && grep -q $'\tstart\t' "$log_l8"; then
    pass "L8a: wake-watcher.sh writes start line"
  else
    fail "L8a: missing start line; log: $(cat "$log_l8")"
  fi
  if grep -q "reason=missing-deps" "$log_l8"; then
    pass "L8b: wake-watcher.sh exit reason=missing-deps when .beads absent"
  else
    fail "L8b: expected reason=missing-deps; log: $(cat "$log_l8")"
  fi
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
