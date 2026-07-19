#!/usr/bin/env bash
# inbox-drain-disarm.test.sh — session-aware disarm coverage for
# inbox-drain.sh (agent-teams-e3mq.30).
#
# Bug: wake-watcher.sh's claim path (agent-teams-e3mq.29) became
# session-aware — a LIVE incumbent watcher from a DIFFERENT session is left
# running untouched — but inbox-drain.sh's disarm (the per-turn "kill the
# pending watcher" block) was never updated to match. It read the pidfile as
# a bare pid, so the new "pid<TAB>session_id" format made `kill -0` fail on
# the tab-containing string, and `rm -f "$PIDFILE"` ran UNCONDITIONALLY
# regardless of whether the watcher belonged to this session. A duplicate
# session's first turn deleted the incumbent's pidfile, the incumbent's
# watcher self-terminated via its own "superseded" loop guard, and the
# duplicate's next Stop-hook watcher then claimed the now-empty slot —
# defeating first-one-wins through the release side instead of the claim
# side.
#
# Fix: inbox-drain.sh's disarm now parses the same pid<TAB>session_id format
# (via the shared lib/watcher-pidfile.sh) and mirrors wake-watcher.sh's claim
# rules: only kill/rm a watcher this session owns, or one whose pid is
# already dead. A live watcher owned by a different (or old-format,
# unattributable) session is left completely untouched, and the drain exits
# early with reason=foreign-watcher-live BEFORE consuming the doorbell or
# peeking at mail — since this session is not the session-of-record for this
# mailbox.
#
# Also covers the dead-exit-reason regression fixed in the same change:
# hook_log_start pre-seeds HOOK_EXIT_REASON="unexpected", so inbox-drain.sh's
# final "${HOOK_EXIT_REASON:-ok}" never applied — every no-mail drain logged
# reason=unexpected. Now fixed to check for the "unexpected" sentinel
# explicitly.
#
# This test drives inbox-drain.sh directly (not through `ateam`) against a
# temp AGENT_TEAMS_HOME, with a fake ateam binary that only satisfies the
# scripts' dependency guard and reports "no unread mail" — matching
# tests/steward-inbox-resolve.test.sh's approach.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HOOKS="$ROOT/plugins/agent-teams/hooks/scripts"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

# ── Fake ateam binary — dependency-guard satisfier + fixed "no mail" peek ───
FAKE_PLUGIN_ROOT="$T/plugin-root"
mkdir -p "$FAKE_PLUGIN_ROOT/bin"
cat > "$FAKE_PLUGIN_ROOT/bin/ateam" <<'SHIM'
#!/usr/bin/env bash
echo "no unread mail"
exit 0
SHIM
chmod +x "$FAKE_PLUGIN_ROOT/bin/ateam"

export AGENT_TEAMS_HOME="$T/ws"
export CLAUDE_PLUGIN_ROOT="$FAKE_PLUGIN_ROOT"
mkdir -p "$AGENT_TEAMS_HOME"
git -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && bd init --prefix at --non-interactive >/dev/null)

STEWARD_DIR="$AGENT_TEAMS_HOME/steward/session"
mkdir -p "$STEWARD_DIR"
: > "$STEWARD_DIR/.steward-session"

MAILBOX="$AGENT_TEAMS_HOME/mailbox"
mkdir -p "$MAILBOX"
PIDFILE="$MAILBOX/steward.watcher.pid"
DOORBELL="$MAILBOX/steward.wake"
HOOKS_LOG="$AGENT_TEAMS_HOME/debug/hooks.log"

# wait_until <max_ticks> <cmd...> — polls every 0.1s (up to max_ticks*0.1s)
# until cmd succeeds; returns 1 on timeout. See wake-watcher-singleton.test.sh.
wait_until() {
  local max_ticks="$1"; shift
  local i=0
  until "$@" 2>/dev/null; do
    i=$((i+1))
    [ "$i" -lt "$max_ticks" ] || return 1
    sleep 0.1
  done
  return 0
}

pid_alive() { kill -0 "$1" 2>/dev/null; }
pid_dead() { ! kill -0 "$1" 2>/dev/null; }

# log_count / lines_since — scope log assertions to only the lines a single
# run_hook call appended, so cases sharing one hooks.log can't false-positive
# off an earlier case's entries (several cases legitimately share reasons,
# e.g. both "own session" and "dead pid" cases end in reason=ok).
log_count() { [ -f "$HOOKS_LOG" ] && wc -l < "$HOOKS_LOG" 2>/dev/null || echo 0; }
lines_since() { tail -n +"$(( $1 + 1 ))" "$HOOKS_LOG" 2>/dev/null || true; }

# new_log_has <new-lines> <script> <detail-substring>
new_log_has() {
  printf '%s\n' "$1" | awk -F'\t' -v s="$2" -v pat="$3" \
    '$3==s && index($6,pat){f=1} END{exit !f}'
}

# run_hook <script> <cwd> <stdin-json> — runs the hook, echoing its stdout
# (so callers can check for/against an emitted additionalContext).
run_hook() {
  local script="$1" cwd="$2" stdin="${3:-}"
  ( cd "$cwd" && bash "$HOOKS/$script" <<<"$stdin" ) 2>/dev/null || true
}

# ── Case A: new-format entry, live pid, FOREIGN session → refused, untouched ─
sleep 100 &
watcher_pid=$!
entry_a=$(printf '%s\t%s' "$watcher_pid" "session-incumbent")
printf '%s' "$entry_a" > "$PIDFILE"
: > "$DOORBELL"

before=$(log_count)
out=$(run_hook inbox-drain.sh "$STEWARD_DIR" '{"session_id":"session-newcomer"}')
new_log=$(lines_since "$before")

if [ -f "$PIDFILE" ] && [ "$(cat "$PIDFILE" 2>/dev/null || true)" = "$entry_a" ]; then
  pass "A1: pidfile intact and unchanged"
else
  fail "A1: pidfile changed or missing; got=$(cat "$PIDFILE" 2>/dev/null || echo MISSING)"
fi
if pid_alive "$watcher_pid"; then
  pass "A2: watcher process still alive"
else
  fail "A2: watcher process was killed; should have been left alone"
fi
if [ -f "$DOORBELL" ]; then
  pass "A3: doorbell still present (not consumed)"
else
  fail "A3: doorbell was consumed; should have been left alone"
fi
if new_log_has "$new_log" inbox-drain.sh "reason=foreign-watcher-live"; then
  pass "A4: exited reason=foreign-watcher-live"
else
  fail "A4: expected reason=foreign-watcher-live; new log: $new_log"
fi
if [ -z "$out" ]; then
  pass "A5: no additionalContext emitted (exited before the mail-peek block)"
else
  fail "A5: unexpected stdout: $out"
fi

kill "$watcher_pid" 2>/dev/null || true
wait "$watcher_pid" 2>/dev/null || true
rm -f "$PIDFILE" "$DOORBELL"

# ── Case B: new-format entry, live pid, OWN session → disarmed ──────────────
sleep 100 &
watcher_pid=$!
printf '%s\t%s' "$watcher_pid" "session-mine" > "$PIDFILE"
: > "$DOORBELL"

before=$(log_count)
run_hook inbox-drain.sh "$STEWARD_DIR" '{"session_id":"session-mine"}' >/dev/null
new_log=$(lines_since "$before")

if wait_until 20 pid_dead "$watcher_pid"; then
  pass "B1: own watcher was killed"
else
  fail "B1: own watcher should have been killed; still alive"
fi
if [ ! -f "$PIDFILE" ]; then
  pass "B2: pidfile removed"
else
  fail "B2: pidfile still present: $(cat "$PIDFILE" 2>/dev/null)"
fi
if [ ! -f "$DOORBELL" ]; then
  pass "B3: doorbell consumed"
else
  fail "B3: doorbell still present"
fi
if new_log_has "$new_log" inbox-drain.sh "watcher-disarmed pid=${watcher_pid}"; then
  pass "B4: watcher-disarmed logged"
else
  fail "B4: expected watcher-disarmed log; new log: $new_log"
fi

# ── Case C: new-format entry, DEAD pid → removed, script proceeds ──────────
printf '99999\tsession-anyone' > "$PIDFILE"
: > "$DOORBELL"

before=$(log_count)
run_hook inbox-drain.sh "$STEWARD_DIR" '{"session_id":"session-mine"}' >/dev/null
new_log=$(lines_since "$before")

if [ ! -f "$PIDFILE" ]; then
  pass "C1: dead-pid entry removed"
else
  fail "C1: pidfile still present: $(cat "$PIDFILE" 2>/dev/null)"
fi
if [ ! -f "$DOORBELL" ]; then
  pass "C2: doorbell consumed (script proceeded past the disarm block)"
else
  fail "C2: doorbell not consumed; script should have proceeded"
fi
if new_log_has "$new_log" inbox-drain.sh "reason=foreign-watcher-live"; then
  fail "C3: should not have exited foreign-watcher-live for a dead pid"
else
  pass "C3: did not exit foreign-watcher-live"
fi

# ── Case D: OLD-format entry (pid only), LIVE pid → treated as foreign ──────
sleep 100 &
watcher_pid=$!
printf '%s' "$watcher_pid" > "$PIDFILE"
: > "$DOORBELL"

before=$(log_count)
out=$(run_hook inbox-drain.sh "$STEWARD_DIR" '{"session_id":"session-mine"}')
new_log=$(lines_since "$before")

if [ -f "$PIDFILE" ] && [ "$(cat "$PIDFILE" 2>/dev/null || true)" = "$watcher_pid" ]; then
  pass "D1: old-format pidfile intact and unchanged"
else
  fail "D1: pidfile changed or missing; got=$(cat "$PIDFILE" 2>/dev/null || echo MISSING)"
fi
if pid_alive "$watcher_pid"; then
  pass "D2: watcher process still alive"
else
  fail "D2: watcher process was killed; should have been left alone (unattributable session)"
fi
if [ -f "$DOORBELL" ]; then
  pass "D3: doorbell still present (not consumed)"
else
  fail "D3: doorbell was consumed; should have been left alone"
fi
if new_log_has "$new_log" inbox-drain.sh "reason=foreign-watcher-live"; then
  pass "D4: exited reason=foreign-watcher-live"
else
  fail "D4: expected reason=foreign-watcher-live; new log: $new_log"
fi
if [ -z "$out" ]; then
  pass "D5: no additionalContext emitted"
else
  fail "D5: unexpected stdout: $out"
fi

kill "$watcher_pid" 2>/dev/null || true
wait "$watcher_pid" 2>/dev/null || true
rm -f "$PIDFILE" "$DOORBELL"

# ── Case E: OLD-format entry, DEAD pid → removed, script proceeds ───────────
printf '99999' > "$PIDFILE"
: > "$DOORBELL"

before=$(log_count)
run_hook inbox-drain.sh "$STEWARD_DIR" '{"session_id":"session-mine"}' >/dev/null
new_log=$(lines_since "$before")

if [ ! -f "$PIDFILE" ]; then
  pass "E1: dead old-format entry removed"
else
  fail "E1: pidfile still present: $(cat "$PIDFILE" 2>/dev/null)"
fi
if [ ! -f "$DOORBELL" ]; then
  pass "E2: doorbell consumed (script proceeded past the disarm block)"
else
  fail "E2: doorbell not consumed; script should have proceeded"
fi

# ── Case F: plain no-mail drain (no pidfile at all) → reason=ok ─────────────
# Regression check for the dead "${HOOK_EXIT_REASON:-ok}" default: hook_log_start
# pre-seeds HOOK_EXIT_REASON="unexpected", so that default never applied and every
# no-mail drain logged reason=unexpected instead.
rm -f "$PIDFILE" "$DOORBELL"

before=$(log_count)
run_hook inbox-drain.sh "$STEWARD_DIR" '{"session_id":"session-mine"}' >/dev/null
new_log=$(lines_since "$before")

if new_log_has "$new_log" inbox-drain.sh "reason=ok"; then
  pass "F1: plain no-mail drain logs reason=ok"
else
  fail "F1: expected reason=ok; new log: $new_log"
fi
if new_log_has "$new_log" inbox-drain.sh "reason=unexpected"; then
  fail "F2: should not log reason=unexpected"
else
  pass "F2: did not log reason=unexpected"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
