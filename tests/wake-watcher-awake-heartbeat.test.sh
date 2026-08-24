#!/usr/bin/env bash
# wake-watcher-awake-heartbeat.test.sh — awake-time heartbeat coverage for
# wake-watcher.sh (agent-teams-bq9y.5).
#
# Bug: the heartbeat deadline was wall-clock (deadline=now+HEARTBEAT_SECS,
# default 4h), so machine sleep counted toward it. On wake, many watchers
# armed around the same time crossed their deadline together — a re-arm
# burst (the Steward's re-arm is a full context reload, the expensive one).
#
# Fix: each poll iteration compares "now" to the previous iteration's tick;
# a gap far past the ~1s cadence (HEARTBEAT_SLEEP_GAP_SECS) is treated as
# machine sleep and its excess beyond one second is pushed onto the
# deadline, so the heartbeat window counts only awake seconds. An
# independent wall-clock safety cap (HEARTBEAT_SAFETY_SECS) still fires
# regardless of awake-time accounting, so a mostly-asleep machine still
# re-arms at least once every ~HEARTBEAT_SAFETY_SECS of wall-clock.
#
# This test drives wake-watcher.sh directly (not through `ateam`) against a
# temp AGENT_TEAMS_HOME, in the Steward's own session dir — same rig as
# wake-watcher-singleton.test.sh — so no CLAUDE_PLUGIN_ROOT/ateam binary is
# needed: the Steward branch's still_open_and_enabled() always returns true.
# HEARTBEAT_SECS/HEARTBEAT_SAFETY_SECS/HEARTBEAT_SLEEP_GAP_SECS are the
# env-overridable knobs this test relies on to avoid waiting real hours.
#
# Case A simulates a machine-sleep gap with SIGSTOP/SIGCONT on the watcher's
# own process — a real wall-clock pause the process cannot distinguish from
# a suspend/resume — and asserts the deadline is pushed forward (no early
# fire) yet the watcher still eventually re-arms once the extended deadline
# is reached. Case B asserts the wall-clock safety fires on its own even
# when the (huge) heartbeat deadline is nowhere close.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HOOKS="$ROOT/plugins/agent-teams/hooks/scripts"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME"
git -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && bd init --prefix at --non-interactive >/dev/null)

STEWARD_DIR="$AGENT_TEAMS_HOME/steward/session"
mkdir -p "$STEWARD_DIR"
: > "$STEWARD_DIR/.steward-session"

MAILBOX="$AGENT_TEAMS_HOME/mailbox"
mkdir -p "$MAILBOX"
PIDFILE="$MAILBOX/steward.watcher.pid"
HOOKS_LOG="$AGENT_TEAMS_HOME/debug/hooks.log"

# log_has_for <session_id> <detail-substring> — unlike wake-watcher-
# singleton's log_has (script-name scoped), this scopes by session id so
# Case A and Case B (sharing one hooks.log) can't cross-contaminate.
log_has_for() {
  awk -F'\t' -v s="$1" -v pat="$2" '$2==s && index($6,pat){f=1} END{exit !f}' \
    "$HOOKS_LOG" 2>/dev/null
}

# wait_until <max_ticks> <cmd...> — polls every 0.1s (up to max_ticks*0.1s)
# until cmd succeeds, returning as soon as it does; returns 1 on timeout.
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

# launch_watcher <cwd> <stdin-json> <logfile> — starts wake-watcher.sh with
# the given cwd and stdin (mimicking Claude Code's {session_id,...} hook
# payload), backgrounded via `exec` so $! is the script's own $$ — exactly
# what it writes to the pidfile and logs. Echoes that pid. Inherits whatever
# HEARTBEAT_* env vars are exported in the caller at call time.
launch_watcher() {
  local cwd="$1" stdin="$2" log="$3"
  bash -c 'cd "$1" && exec bash "$2" <<<"$3"' _ "$cwd" "$HOOKS/wake-watcher.sh" "$stdin" \
    >"$log" 2>&1 &
  echo $!
}

# ── Case A: a sleep gap extends the deadline — no early fire, still re-arms ──
# HEARTBEAT_SECS=8 with the machine "asleep" (SIGSTOPped) for ~6s starting
# ~1s after arm means naive wall-clock would already be past the original
# 8s deadline by the time we resume (~7s in) — if the fix were absent (or
# broken), the watcher would fire almost immediately on resume. With the
# fix, the ~6s gap (>= HEARTBEAT_SLEEP_GAP_SECS=3) pushes the deadline out
# by ~5s (gap-1), so it must NOT have fired yet at resume+~2s, and it must
# still eventually fire once the extended deadline arrives.
export HEARTBEAT_SECS=8
export HEARTBEAT_SLEEP_GAP_SECS=3
export HEARTBEAT_SAFETY_SECS=1000
a_log="$T/a-watcher.out"
a_pid=$(launch_watcher "$STEWARD_DIR" '{"session_id":"session-awake-a"}' "$a_log")

if ! wait_until 20 pid_alive "$a_pid"; then
  fail "A: setup failed — watcher never started; log: $(cat "$a_log" 2>/dev/null)"
else
  sleep 1   # let it complete at least one tick so prev_tick is set near "now"
  kill -STOP "$a_pid" 2>/dev/null || true
  sleep 6   # simulated machine-sleep gap while the watcher is frozen
  kill -CONT "$a_pid" 2>/dev/null || true

  sleep 2   # give it time to resume and run one iteration past the naive deadline
  if pid_alive "$a_pid"; then
    pass "A1: watcher still running after the sleep gap (deadline was extended, no early fire)"
  else
    fail "A1: watcher exited early — sleep gap did not extend the deadline as expected; log: $(cat "$a_log" 2>/dev/null)"
  fi

  if wait_until 200 pid_dead "$a_pid"; then
    pass "A2: watcher eventually re-armed once the extended deadline was reached"
  else
    fail "A2: watcher never re-armed within the extended window; log: $(cat "$a_log" 2>/dev/null)"
    kill "$a_pid" 2>/dev/null || true
    wait "$a_pid" 2>/dev/null || true
  fi

  if log_has_for "session-awake-a" "sleep-gap-detected"; then
    pass "A3: sleep-gap-detected note logged"
  else
    fail "A3: sleep-gap-detected note not logged; log tail: $(tail -5 "$HOOKS_LOG" 2>/dev/null)"
  fi

  if log_has_for "session-awake-a" "code=2 reason=heartbeat-rearm"; then
    pass "A4: watcher exited 2 with reason=heartbeat-rearm (normal re-arm path, not a crash)"
  else
    fail "A4: expected exit code=2 reason=heartbeat-rearm not found; log tail: $(tail -5 "$HOOKS_LOG" 2>/dev/null)"
  fi
fi
rm -f "$PIDFILE"
unset HEARTBEAT_SECS HEARTBEAT_SLEEP_GAP_SECS HEARTBEAT_SAFETY_SECS

# ── Case B: the wall-clock safety fires even when the deadline is nowhere close ──
# HEARTBEAT_SECS is enormous (never reachable in this test); only the small
# HEARTBEAT_SAFETY_SECS should cause the re-arm.
export HEARTBEAT_SECS=100000
export HEARTBEAT_SLEEP_GAP_SECS=100000
export HEARTBEAT_SAFETY_SECS=4
b_log="$T/b-watcher.out"
b_pid=$(launch_watcher "$STEWARD_DIR" '{"session_id":"session-awake-b"}' "$b_log")

if ! wait_until 20 pid_alive "$b_pid"; then
  fail "B: setup failed — watcher never started; log: $(cat "$b_log" 2>/dev/null)"
else
  if wait_until 150 pid_dead "$b_pid"; then
    pass "B1: watcher re-armed via the wall-clock safety cap despite a far-off heartbeat deadline"
  else
    fail "B1: watcher never re-armed within the safety window; log: $(cat "$b_log" 2>/dev/null)"
    kill "$b_pid" 2>/dev/null || true
    wait "$b_pid" 2>/dev/null || true
  fi

  if log_has_for "session-awake-b" "code=2 reason=heartbeat-rearm"; then
    pass "B2: watcher exited 2 with reason=heartbeat-rearm"
  else
    fail "B2: expected exit code=2 reason=heartbeat-rearm not found; log tail: $(tail -5 "$HOOKS_LOG" 2>/dev/null)"
  fi
fi
rm -f "$PIDFILE"
unset HEARTBEAT_SECS HEARTBEAT_SLEEP_GAP_SECS HEARTBEAT_SAFETY_SECS

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
