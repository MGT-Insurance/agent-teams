#!/usr/bin/env bash
# Tests for plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh"

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
export AGENT_TEAMS_HOME="$T/agent-teams"

# ── Case 1: helper writes a correctly-formatted TSV line ─────────────────────
# Source the helper in a subshell with a known session id and initiative id.
(
  ATH="$T/agent-teams"
  export ATH
  HOOK_SESSION_ID="test-session-abc"
  export HOOK_SESSION_ID
  . "$LIB"
  hook_debug_log "test-script.sh" "at-test01"
)
log_file="$T/agent-teams/debug/hooks.log"
[ -f "$log_file" ] || { echo "FAIL case1: log file not created at $log_file"; exit 1; }
line=$(cat "$log_file")
# Verify TSV has 4 fields: timestamp, session_id, script-name, initiative-id
field2=$(printf '%s' "$line" | cut -f2)
field3=$(printf '%s' "$line" | cut -f3)
field4=$(printf '%s' "$line" | cut -f4)
[ "$field2" = "test-session-abc" ] || { echo "FAIL case1: session_id field wrong, got: $field2"; exit 1; }
[ "$field3" = "test-script.sh"   ] || { echo "FAIL case1: script-name field wrong, got: $field3"; exit 1; }
[ "$field4" = "at-test01"        ] || { echo "FAIL case1: initiative-id field wrong, got: $field4"; exit 1; }
# Timestamp should look like ISO-8601 UTC (starts with 20 and contains T and Z)
field1=$(printf '%s' "$line" | cut -f1)
echo "$field1" | grep -qE '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$' \
  || { echo "FAIL case1: timestamp not ISO-8601 UTC, got: $field1"; exit 1; }
echo "PASS case1: correctly-formatted TSV line written"

# ── Case 2: JSON stdin with session_id is parsed and logged correctly ─────────
# Simulate how hook scripts extract HOOK_SESSION_ID from stdin JSON.
T2="$T/case2"
mkdir -p "$T2"
(
  ATH="$T2/agent-teams"
  export ATH
  stdin_json='{"session_id":"live-session-xyz","hook_event_name":"UserPromptSubmit","cwd":"/some/path"}'
  HOOK_SESSION_ID=$(printf '%s' "$stdin_json" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
  export HOOK_SESSION_ID
  . "$LIB"
  hook_debug_log "inbox-drain.sh" "at-abc1"
)
log2="$T2/agent-teams/debug/hooks.log"
[ -f "$log2" ] || { echo "FAIL case2: log file not created"; exit 1; }
line2=$(cat "$log2")
sid=$(printf '%s' "$line2" | cut -f2)
[ "$sid" = "live-session-xyz" ] || { echo "FAIL case2: session_id not parsed from JSON, got: $sid"; exit 1; }
echo "PASS case2: session_id from JSON stdin parsed and logged"

# ── Case 3: empty stdin (direct invocation via </dev/null) exits normally ─────
# The hook scripts guard stdin capture with `cat 2>/dev/null || true`.
# This test sources the helper with HOOK_SESSION_ID defaulting to 'unknown'.
T3="$T/case3"
mkdir -p "$T3"
(
  ATH="$T3/agent-teams"
  export ATH
  # Simulate what the hook scripts do with empty stdin
  HOOK_STDIN=$(cat 2>/dev/null </dev/null || true)
  HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
  export HOOK_SESSION_ID
  . "$LIB"
  hook_debug_log "wake-watcher.sh" "unknown"
)
log3="$T3/agent-teams/debug/hooks.log"
[ -f "$log3" ] || { echo "FAIL case3: log file not created for empty-stdin case"; exit 1; }
sid3=$(cat "$log3" | cut -f2)
[ "$sid3" = "unknown" ] || { echo "FAIL case3: expected session_id 'unknown' for empty stdin, got: $sid3"; exit 1; }
echo "PASS case3: empty stdin exits cleanly and logs session_id 'unknown'"

echo "PASS"
