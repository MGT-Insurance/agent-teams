#!/usr/bin/env bash
# inbox-drain-mail-without-doorbell.test.sh — inbox-drain.sh logs a
# "mail-without-doorbell" line to hooks.log only when a cache-expiry or
# untrusted-cache hook-scan rescan found unread mail AND no doorbell was
# consumed on that prompt (agent-teams-c4i9.20.34.7). It also drops the
# hook-scan cache entry whenever it consumes a doorbell.
#
# Drives inbox-drain.sh directly against a temp AGENT_TEAMS_HOME with a fake
# ateam whose hook-scan prints $FAKE_SCAN_OUT on stdout and $FAKE_SCAN_ERR on
# stderr.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HOOKS="$ROOT/plugins/agent-teams/hooks/scripts"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

FAKE_PLUGIN_ROOT="$T/plugin-root"
mkdir -p "$FAKE_PLUGIN_ROOT/bin"
cat > "$FAKE_PLUGIN_ROOT/bin/ateam" <<'SHIM'
#!/usr/bin/env bash
[ -n "${FAKE_SCAN_OUT:-}" ] && printf '%b' "$FAKE_SCAN_OUT"
[ -n "${FAKE_SCAN_ERR:-}" ] && printf '%b' "$FAKE_SCAN_ERR" >&2
exit 0
SHIM
chmod +x "$FAKE_PLUGIN_ROOT/bin/ateam"

export AGENT_TEAMS_HOME="$T/ws"
export CLAUDE_PLUGIN_ROOT="$FAKE_PLUGIN_ROOT"
mkdir -p "$AGENT_TEAMS_HOME/.beads" "$AGENT_TEAMS_HOME/mailbox" "$AGENT_TEAMS_HOME/cache/hookscan"
HOOKS_LOG="$AGENT_TEAMS_HOME/debug/hooks.log"
DOORBELL="$AGENT_TEAMS_HOME/mailbox/at-1.wake"
CACHE="$AGENT_TEAMS_HOME/cache/hookscan/s1.json"
WORK="$T/work"; mkdir -p "$WORK"

# run_case <scan-out> <scan-err> <doorbell:0|1> — runs the hook; sets $HOOK_OUT
# and $NEW_LOG (only the hooks.log lines this run appended).
run_case() {
  local before=0
  [ -f "$HOOKS_LOG" ] && before=$(wc -l < "$HOOKS_LOG")
  rm -f "$DOORBELL"; [ "$3" = 1 ] && : > "$DOORBELL"
  echo '{}' > "$CACHE"
  HOOK_OUT=$( cd "$WORK" && FAKE_SCAN_OUT="$1" FAKE_SCAN_ERR="$2" \
    bash "$HOOKS/inbox-drain.sh" <<<'{"session_id":"s1"}' 2>/dev/null ) || true
  NEW_LOG=$(tail -n +"$((before + 1))" "$HOOKS_LOG" 2>/dev/null || true)
}

SCAN2='id: at-1\nunread: 2\n'
NOTE='scan: full reason=ttl cache_age=612s\n'

run_case "$SCAN2" "$NOTE" 0
if printf '%s\n' "$NEW_LOG" | grep -q 'mail-without-doorbell initiative=at-1 unread=2 cache_age=612s'; then
  pass "A: TTL rescan, mail, no doorbell -> logged"
else fail "A: expected mail-without-doorbell line; got: $NEW_LOG"; fi
[ -f "$CACHE" ] && pass "A: cache kept when no doorbell consumed" || fail "A: cache removed without a doorbell"
printf '%s' "$HOOK_OUT" | grep -q 'unread message' && pass "A: signal still emitted" || fail "A: no signal"

run_case "$SCAN2" "$NOTE" 1
if printf '%s\n' "$NEW_LOG" | grep -q 'mail-without-doorbell'; then fail "B: logged although a doorbell was consumed"
else pass "B: doorbell consumed -> no line"; fi
printf '%s\n' "$NEW_LOG" | grep -q 'doorbell-consumed' && pass "B: doorbell-consumed logged" || fail "B: no doorbell-consumed line"
[ ! -f "$CACHE" ] && pass "B: cache dropped on doorbell consume" || fail "B: cache survived doorbell consume"

run_case "$SCAN2" "" 0
if printf '%s\n' "$NEW_LOG" | grep -q 'mail-without-doorbell'; then fail "C: logged on a nag/non-discovery scan"
else pass "C: no scan note (nag) -> no line"; fi

run_case 'id: at-1\nunread: 0\n' "" 0
if printf '%s\n' "$NEW_LOG" | grep -q 'mail-without-doorbell'; then fail "D: logged with no unread mail"
else pass "D: no unread mail -> no line"; fi

echo "passed=$PASS failed=$FAIL"
[ "$FAIL" -eq 0 ]
