#!/usr/bin/env bash
# wake-watcher-singleton.test.sh — pidfile-claim coverage for wake-watcher.sh
# (agent-teams-e3mq.29).
#
# Bug: two background Steward sessions raced over one watcher pidfile slot.
# The old claim logic was last-one-wins — it unconditionally killed whatever
# pid was already in the pidfile, so a duplicate steward session could steal
# the real steward's watcher (and, observed live, freeze on a permission
# prompt while holding it — the real steward session never saw the wake).
#
# Fix: the pidfile now holds "pid<TAB>session_id" instead of just "pid".
# First-one-wins: an ALIVE incumbent from a DIFFERENT session is left running
# untouched (refuse); a session re-arming its OWN watcher (same session_id,
# e.g. after a Stop) still supersedes cleanly; a DEAD incumbent is always
# claimed regardless of session, old-format-or-not.
#
# This test drives wake-watcher.sh directly (not through `ateam`) against a
# temp AGENT_TEAMS_HOME, in the Steward's own session dir — the pidfile-claim
# logic under test is shared code, identical for the Steward and any regular
# initiative watcher, so exercising it via the Steward branch is sufficient.
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

log_has() {
  awk -F'\t' -v s="$1" -v pat="$2" '$3==s && index($6,pat){f=1} END{exit !f}' \
    "$HOOKS_LOG" 2>/dev/null
}

# wait_until <max_ticks> <cmd...> — polls every 0.1s (up to max_ticks*0.1s)
# until cmd succeeds, returning as soon as it does; returns 1 on timeout.
# Used instead of a fixed sleep for cross-process state (pid death, pidfile
# writes) whose timing varies with system load — a fixed sleep either wastes
# time or flakes, a bounded poll does neither.
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
pidfile_is() { [ "$(cat "$PIDFILE" 2>/dev/null || true)" = "$1" ]; }

# launch_watcher <cwd> <stdin-json> <logfile> — starts wake-watcher.sh with
# the given cwd and stdin (mimicking Claude Code's {session_id,...} hook
# payload), backgrounded via `exec` so $! is the script's own $$ — exactly
# what it writes to the pidfile. Echoes that pid.
launch_watcher() {
  local cwd="$1" stdin="$2" log="$3"
  bash -c 'cd "$1" && exec bash "$2" <<<"$3"' _ "$cwd" "$HOOKS/wake-watcher.sh" "$stdin" \
    >"$log" 2>&1 &
  echo $!
}

# ── Case A: LIVE foreign pid+session → newcomer refuses, incumbent untouched ─
sleep 100 &
incumbent_pid=$!
printf '%s\t%s' "$incumbent_pid" "session-incumbent" > "$PIDFILE"

a_log="$T/a-newcomer.out"
newcomer_pid=$(launch_watcher "$STEWARD_DIR" '{"session_id":"session-newcomer"}' "$a_log")

if wait_until 20 pid_dead "$newcomer_pid"; then
  pass "A1: newcomer refused (exited without taking over)"
else
  fail "A1: newcomer should have refused and exited; still running. log: $(cat "$a_log")"
  kill "$newcomer_pid" 2>/dev/null || true
  wait "$newcomer_pid" 2>/dev/null || true
fi

if kill -0 "$incumbent_pid" 2>/dev/null; then
  pass "A2: incumbent left running (not killed)"
else
  fail "A2: incumbent was killed; should have been left alone"
fi

pidfile_content=$(cat "$PIDFILE" 2>/dev/null || true)
want_content=$(printf '%s\t%s' "$incumbent_pid" "session-incumbent")
if [ "$pidfile_content" = "$want_content" ]; then
  pass "A3: pidfile unchanged by the refused newcomer"
else
  fail "A3: pidfile changed; got=$pidfile_content want=$want_content"
fi

if log_has wake-watcher.sh "duplicate-watcher"; then
  pass "A4: duplicate-watcher reason logged"
else
  fail "A4: duplicate-watcher reason not logged; log tail: $(tail -5 "$HOOKS_LOG" 2>/dev/null)"
fi

kill "$incumbent_pid" 2>/dev/null || true
wait "$incumbent_pid" 2>/dev/null || true
rm -f "$PIDFILE"

# ── Case B: DEAD pid (old-format, no session — backward compat) → claim ─────
printf '99999' > "$PIDFILE"

b_log="$T/b-claim.out"
claimant_pid=$(launch_watcher "$STEWARD_DIR" '{"session_id":"session-claim"}' "$b_log")

if wait_until 20 pidfile_is "$(printf '%s\t%s' "$claimant_pid" "session-claim")"; then
  pass "B1: claimant entered the poll loop (claimed dead old-format pidfile)"
else
  fail "B1: claimant exited unexpectedly; log: $(cat "$b_log")"
fi

pidfile_content=$(cat "$PIDFILE" 2>/dev/null || true)
want_content=$(printf '%s\t%s' "$claimant_pid" "session-claim")
if [ "$pidfile_content" = "$want_content" ]; then
  pass "B2: pidfile rewritten in new pid<TAB>session format"
else
  fail "B2: unexpected pidfile content; got=$pidfile_content want=$want_content"
fi

kill "$claimant_pid" 2>/dev/null || true
wait "$claimant_pid" 2>/dev/null || true
rm -f "$PIDFILE"

# ── Case C: same session id, live pid → supersede (existing behavior) ───────
c_incumbent_log="$T/c-incumbent.out"
incumbent2_pid=$(launch_watcher "$STEWARD_DIR" '{"session_id":"session-same"}' "$c_incumbent_log")

if ! wait_until 20 pidfile_is "$(printf '%s\t%s' "$incumbent2_pid" "session-same")"; then
  fail "C: setup failed — incumbent (session-same) did not start; log: $(cat "$c_incumbent_log")"
else
  c_rearm_log="$T/c-rearm.out"
  rearm_pid=$(launch_watcher "$STEWARD_DIR" '{"session_id":"session-same"}' "$c_rearm_log")

  # Poll (bounded) for the rearm to finish its claim — evidenced by the
  # pidfile flipping to the rearm's pid+session — rather than guessing a
  # fixed sleep for how long the kill+0.1s-tick+write takes under load.
  rearm_claimed=$(printf '%s\t%s' "$rearm_pid" "session-same")
  wait_until 50 pidfile_is "$rearm_claimed" || true

  if wait_until 20 pid_alive "$rearm_pid"; then
    pass "C1: re-arm (same session id) entered the poll loop"
  else
    fail "C1: re-arm exited unexpectedly; log: $(cat "$c_rearm_log")"
  fi

  if wait_until 50 pid_dead "$incumbent2_pid"; then
    pass "C2: old incumbent (same session id) was superseded"
  else
    fail "C2: old incumbent (same session id) should have been superseded"
  fi

  pidfile_content=$(cat "$PIDFILE" 2>/dev/null || true)
  if [ "$pidfile_content" = "$rearm_claimed" ]; then
    pass "C3: pidfile holds the re-arm's pid+session"
  else
    fail "C3: unexpected pidfile content after supersede; got=$pidfile_content want=$rearm_claimed"
  fi

  kill "$rearm_pid" 2>/dev/null || true
  wait "$rearm_pid" 2>/dev/null || true
fi
rm -f "$PIDFILE" "$MAILBOX/steward.wake"

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
