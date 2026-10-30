#!/usr/bin/env bash
# session-start-inbox-single-scan.test.sh — session-start-inbox.sh must make
# exactly ONE ateam call (hook-scan) on the blocking SessionStart path: no
# tie-session, no resolve-initiative, no mail inbox. Uses a fake ateam that
# logs argv and prints canned hook-scan output; real resolution is covered by
# Go tests in internal/verbs/hookscan_test.go.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/session-start-inbox.sh"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

FAKE_PLUGIN_ROOT="$T/plugin-root"
mkdir -p "$FAKE_PLUGIN_ROOT/bin" "$T/wt" "$T/ws/.beads"
CALL_LOG="$T/ateam-calls.log"
cat > "$FAKE_PLUGIN_ROOT/bin/ateam" <<'SHIM'
#!/usr/bin/env bash
echo "$@" >> "$CALL_LOG"
if [ "$1" = "hook-scan" ]; then
  printf '%s' "${SCAN_OUTPUT:-}"
fi
exit 0
SHIM
chmod +x "$FAKE_PLUGIN_ROOT/bin/ateam"

export CLAUDE_PLUGIN_ROOT="$FAKE_PLUGIN_ROOT"
export AGENT_TEAMS_HOME="$T/ws"
export CALL_LOG
unset ATEAM_INITIATIVE || true

HOOKS_LOG="$AGENT_TEAMS_HOME/debug/hooks.log"

# run_hook <cwd> — runs the hook with a session id on stdin; sets out/rc.
run_hook() {
  : > "$CALL_LOG"
  rm -f "$HOOKS_LOG"
  if out=$(cd "$1" && echo '{"session_id":"sess-1"}' | "$SCRIPT"); then rc=0; else rc=$?; fi
}

# ── Case 1: one hook-scan call with --session-id; no hint when unset; unread
# mail becomes additionalContext ────────────────────────────────────────────
SCAN_OUTPUT=$'id: at-x\nunread: 2\n' run_hook "$T/wt"
[ "$rc" -eq 0 ] && pass "case1: exit 0" || fail "case1: exit $rc"
if [ "$(wc -l < "$CALL_LOG" | tr -d ' ')" = "1" ] && grep -qx "hook-scan $T/wt --session-id sess-1" "$CALL_LOG"; then
  pass "case1: exactly one ateam call: hook-scan with --session-id, no --hint-id"
else
  fail "case1: unexpected ateam calls: $(cat "$CALL_LOG")"
fi
if grep -qE 'resolve-initiative|mail inbox|tie-session' "$CALL_LOG"; then
  fail "case1: forbidden call in log: $(cat "$CALL_LOG")"
else
  pass "case1: no resolve-initiative, mail inbox, or tie-session call"
fi
if printf '%s' "$out" | jq -e '.additionalContext | contains("2 unread message(s)")' >/dev/null 2>&1; then
  pass "case1: additionalContext reports 2 unread message(s)"
else
  fail "case1: unexpected stdout: $out"
fi
if grep -q 'initiative-resolved id=at-x' "$HOOKS_LOG" && grep -q 'mail-signaled' "$HOOKS_LOG"; then
  pass "case1: hook log has initiative-resolved note and mail-signaled reason"
else
  fail "case1: hook log: $(cat "$HOOKS_LOG" 2>/dev/null || echo MISSING)"
fi

# ── Case 2: ATEAM_INITIATIVE set -> --hint-id passed ────────────────────────
ATEAM_INITIATIVE=at-launched SCAN_OUTPUT=$'id: at-x\nunread: 0\n' run_hook "$T/wt"
if [ "$(wc -l < "$CALL_LOG" | tr -d ' ')" = "1" ] && grep -qx "hook-scan $T/wt --session-id sess-1 --hint-id at-launched" "$CALL_LOG"; then
  pass "case2: --hint-id passed when ATEAM_INITIATIVE is set"
else
  fail "case2: unexpected ateam calls: $(cat "$CALL_LOG")"
fi

# ── Case 3: unread 0 -> no stdout ───────────────────────────────────────────
SCAN_OUTPUT=$'id: at-x\nunread: 0\n' run_hook "$T/wt"
if [ "$rc" -eq 0 ] && [ -z "$out" ]; then
  pass "case3: unread 0 gives no stdout"
else
  fail "case3: rc=$rc out=$out"
fi

# ── Case 4: empty scan output -> exit 0, no stdout, no-open-match ───────────
SCAN_OUTPUT='' run_hook "$T/wt"
if [ "$rc" -eq 0 ] && [ -z "$out" ] && grep -q 'no-open-match' "$HOOKS_LOG"; then
  pass "case4: empty scan gives exit 0, no stdout, exit reason no-open-match"
else
  fail "case4: rc=$rc out=$out log=$(cat "$HOOKS_LOG" 2>/dev/null || echo MISSING)"
fi

# ── Case 5: steward cwd -> hook-scan --id=steward ───────────────────────────
mkdir -p "$T/ws/steward/session"
: > "$T/ws/steward/session/.steward-session"
SCAN_OUTPUT=$'id: steward\nunread: 1\n' run_hook "$T/ws/steward/session"
if grep -qx 'hook-scan --id=steward' "$CALL_LOG" && [ "$(wc -l < "$CALL_LOG" | tr -d ' ')" = "1" ]; then
  pass "case5: steward cwd makes one call: hook-scan --id=steward"
else
  fail "case5: unexpected ateam calls: $(cat "$CALL_LOG") (is the steward marker name right?)"
fi

echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
