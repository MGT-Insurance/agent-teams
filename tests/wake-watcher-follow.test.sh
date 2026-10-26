#!/usr/bin/env bash
# wake-watcher-follow.test.sh — wake-watcher.sh runs `ateam mail follow` (no
# arguments) in the foreground right after it writes its own pidfile, and never
# stops the follower on exit (docs/hosted-beads.md, F10).
#
# A stub `ateam` on CLAUDE_PLUGIN_ROOT/bin records its argv and whether the
# watcher pidfile already existed, leaves a long-lived stand-in follower
# running, and returns at once like the real verb. The watcher runs in the
# Steward's session dir (the wiring is shared code, identical for any
# initiative watcher).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HOOKS="$ROOT/plugins/agent-teams/hooks/scripts"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d)
cleanup() {
  [ -f "$T/follower.pid" ] && kill "$(cat "$T/follower.pid")" 2>/dev/null || true
  rm -rf "$T"
}
trap cleanup EXIT

export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME"
git -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && bd init --prefix at --non-interactive >/dev/null)

STEWARD_DIR="$AGENT_TEAMS_HOME/steward/session"
mkdir -p "$STEWARD_DIR"
: > "$STEWARD_DIR/.steward-session"
MAILBOX="$AGENT_TEAMS_HOME/mailbox"
mkdir -p "$MAILBOX"

PLUGIN="$T/plugin"
mkdir -p "$PLUGIN/bin"
ARGS="$T/stub.args"
CALLS="$T/stub.calls"
SAW_PIDFILE="$T/stub.saw-pidfile"
FOLLOWER="$T/follower.pid"
cat > "$PLUGIN/bin/ateam" <<'EOF'
#!/usr/bin/env bash
echo "$*" > "$STUB_ARGS"
echo x >> "$STUB_CALLS"
if [ -s "$STUB_MAILBOX/steward.watcher.pid" ]; then echo yes > "$STUB_SAW"; else echo no > "$STUB_SAW"; fi
echo "stub-follow-output"
# Stand-in for the detached follower: outlives this process.
if [ ! -s "$STUB_FOLLOWER" ]; then
  sleep 300 </dev/null >/dev/null 2>&1 &
  echo "$!" > "$STUB_FOLLOWER"
fi
exit 0
EOF
chmod +x "$PLUGIN/bin/ateam"
export STUB_ARGS="$ARGS" STUB_CALLS="$CALLS" STUB_SAW="$SAW_PIDFILE" STUB_FOLLOWER="$FOLLOWER" \
  STUB_MAILBOX="$MAILBOX" CLAUDE_PLUGIN_ROOT="$PLUGIN"

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
file_nonempty() { [ -s "$1" ]; }
pid_dead() { ! kill -0 "$1" 2>/dev/null; }

# launch <env assignments...> -> sets WPID; starts a watcher and waits for the stub.
launch() {
  rm -f "$ARGS" "$CALLS" "$SAW_PIDFILE" "$MAILBOX/steward.wake" "$MAILBOX/steward.watcher.pid"
  env "$@" bash -c 'cd "$1" && exec bash "$2" <<<"$3"' _ "$STEWARD_DIR" "$HOOKS/wake-watcher.sh" \
    '{"session_id":"follow-test"}' >"$T/watcher.out" 2>&1 &
  WPID=$!
}

check_start() {
  local label="$1"
  if wait_until 50 file_nonempty "$FOLLOWER" && wait_until 50 file_nonempty "$SAW_PIDFILE"; then
    pass "$label: stub ateam started"
  else
    fail "$label: stub ateam never started; watcher log: $(cat "$T/watcher.out")"
    return 1
  fi
  if [ "$(cat "$ARGS")" = "mail follow" ]; then
    pass "$label: argv is 'mail follow' (no initiative id, no --parent-pid)"
  else
    fail "$label: argv '$(cat "$ARGS")' want 'mail follow'"
  fi
  if [ "$(cat "$SAW_PIDFILE")" = "yes" ]; then
    pass "$label: watcher pidfile existed before ateam mail follow ran"
  else
    fail "$label: watcher pidfile was not written before ateam mail follow"
  fi
  if [ "$(wc -l < "$CALLS" | tr -d ' ')" = "1" ]; then
    pass "$label: ateam mail follow ran exactly once"
  else
    fail "$label: ateam mail follow ran $(wc -l < "$CALLS" | tr -d ' ') times"
  fi
}

follower_alive() { kill -0 "$(cat "$FOLLOWER")" 2>/dev/null; }

# ── Case A: doorbell exit leaves the follower running ───────────────────────
launch HEARTBEAT_SECS=14400
if check_start A; then
  touch "$MAILBOX/steward.wake"
  rc=0; wait "$WPID" || rc=$?
  [ "$rc" = 2 ] && pass "A: watcher exited 2 on doorbell" || fail "A: watcher exit $rc, want 2"
  sleep 0.3
  follower_alive && pass "A: follower outlives the watcher" || fail "A: watcher exit killed the follower"
  if grep -q 'stub-follow-output' "$AGENT_TEAMS_HOME/debug/mail-follow.log" 2>/dev/null; then
    pass "A: follow output appended to debug/mail-follow.log"
  else
    fail "A: debug/mail-follow.log missing the follow output"
  fi
else
  kill "$WPID" 2>/dev/null || true
fi
kill "$(cat "$FOLLOWER" 2>/dev/null)" 2>/dev/null || true; rm -f "$FOLLOWER"

# ── Case B: heartbeat exit leaves the follower running ──────────────────────
launch HEARTBEAT_SECS=1
if check_start B; then
  rc=0; wait "$WPID" || rc=$?
  [ "$rc" = 2 ] && pass "B: watcher exited 2 on heartbeat" || fail "B: watcher exit $rc, want 2"
  sleep 0.3
  follower_alive && pass "B: follower outlives the watcher" || fail "B: watcher exit killed the follower"
else
  kill "$WPID" 2>/dev/null || true
fi
kill "$(cat "$FOLLOWER" 2>/dev/null)" 2>/dev/null || true; rm -f "$FOLLOWER"

# ── Case C: SIGTERM leaves the follower running and keeps hook_log's trap ───
launch HEARTBEAT_SECS=14400
if check_start C; then
  kill -TERM "$WPID"
  wait "$WPID" 2>/dev/null || true
  sleep 0.3
  follower_alive && pass "C: follower outlives a SIGTERMed watcher" || fail "C: SIGTERM killed the follower"
  # The hook_log_start signal trap must be the one in force (no wrapper trap).
  if grep -q 'signal-TERM' "$AGENT_TEAMS_HOME/debug/hook-debug.log" 2>/dev/null \
     || grep -rqs 'signal-TERM' "$AGENT_TEAMS_HOME/debug" 2>/dev/null; then
    pass "C: hook_log signal trap logged signal-TERM"
  else
    fail "C: no signal-TERM entry from hook_log's trap under $AGENT_TEAMS_HOME/debug"
  fi
else
  kill "$WPID" 2>/dev/null || true
fi
kill "$(cat "$FOLLOWER" 2>/dev/null)" 2>/dev/null || true; rm -f "$FOLLOWER"

# ── Case D: no executable ateam -> the watcher still arms (steward branch) ──
rm -f "$PLUGIN/bin/ateam"
launch HEARTBEAT_SECS=14400
sleep 0.6
if kill -0 "$WPID" 2>/dev/null; then
  pass "D: watcher armed without ateam"
else
  fail "D: watcher exited without ateam"
fi
kill "$WPID" 2>/dev/null || true
wait "$WPID" 2>/dev/null || true

# ── Cases E-G: the poll loop restarts a dead follower (F10 restart) ─────────
# FOLLOW_CHECK_SECS=1 so the in-loop check runs within seconds. The stub never
# writes host-follow.pid, so "no pidfile" is the default state.
PLUGIN_STUB_BIN="$T/ateam.stub"
cat > "$PLUGIN_STUB_BIN" <<'EOF'
#!/usr/bin/env bash
echo "$*" > "$STUB_ARGS"
echo x >> "$STUB_CALLS"
exit 0
EOF
chmod +x "$PLUGIN_STUB_BIN"
mkdir -p "$PLUGIN/bin"
cp "$PLUGIN_STUB_BIN" "$PLUGIN/bin/ateam"
HOSTPID="$MAILBOX/host-follow.pid"
call_count() { wc -l < "$CALLS" 2>/dev/null | tr -d ' ' || echo 0; }
calls_at_least() { [ "$(call_count)" -ge "$1" ]; }

# E: no pidfile -> the watcher re-calls `mail follow` within a few seconds.
rm -f "$HOSTPID"
launch HEARTBEAT_SECS=14400 FOLLOW_CHECK_SECS=1
if wait_until 50 file_nonempty "$ARGS" && wait_until 80 calls_at_least 2; then
  pass "E: missing host-follow.pid -> mail follow re-run"
else
  fail "E: no re-call with a missing pidfile (calls=$(call_count))"
fi
[ "$(cat "$ARGS")" = "mail follow" ] && pass "E: re-call argv is 'mail follow'" || fail "E: re-call argv '$(cat "$ARGS")'"
kill "$WPID" 2>/dev/null || true; wait "$WPID" 2>/dev/null || true

# F: live pid in the pidfile -> no re-call.
sleep 300 </dev/null >/dev/null 2>&1 &
LIVE_PID=$!
printf '%s' "$LIVE_PID" > "$HOSTPID"
launch HEARTBEAT_SECS=14400 FOLLOW_CHECK_SECS=1
wait_until 50 file_nonempty "$ARGS" || true
sleep 3.5
if [ "$(call_count)" = "1" ]; then
  pass "F: live pid in host-follow.pid -> no re-call"
else
  fail "F: live pidfile still triggered re-calls (calls=$(call_count))"
fi
kill "$WPID" 2>/dev/null || true; wait "$WPID" 2>/dev/null || true

# G: dead pid in the pidfile -> re-call.
kill "$LIVE_PID" 2>/dev/null || true; wait "$LIVE_PID" 2>/dev/null || true
printf '%s' "$LIVE_PID" > "$HOSTPID"
launch HEARTBEAT_SECS=14400 FOLLOW_CHECK_SECS=1
if wait_until 50 file_nonempty "$ARGS" && wait_until 80 calls_at_least 2; then
  pass "G: dead pid in host-follow.pid -> mail follow re-run"
else
  fail "G: no re-call with a dead pid (calls=$(call_count))"
fi
kill "$WPID" 2>/dev/null || true; wait "$WPID" 2>/dev/null || true
rm -f "$HOSTPID"

# ── Case H: REAL ateam binary restarts a kill -9'd follower ─────────────────
# Real `ateam mail follow` against a throwaway hosted workspace (hosted
# metadata, stub bd first on PATH). Kill -9 the live follower while the
# watcher lives; a NEW follower must take the lock.
if command -v go >/dev/null 2>&1 && command -v lsof >/dev/null 2>&1; then
  RH="$T/realhome"; RMB="$RH/mailbox"; RBIN="$T/realbin"; RPLUG="$T/realplugin"
  mkdir -p "$RH/.beads" "$RBIN" "$RPLUG/bin" "$RH/steward/session"
  : > "$RH/steward/session/.steward-session"
  printf '%s' '{"database":"dolt","backend":"dolt","dolt_mode":"server","dolt_server_host":"hosted.invalid","dolt_server_user":"nobody","dolt_database":"beads_fake","project_id":"fake"}' > "$RH/.beads/metadata.json"
  cat > "$RBIN/bd" <<EOF
#!/usr/bin/env bash
case "\$*" in
  *"events tail"*) exec -a "bdstub-tail $T" sleep 300 ;;
  *sql*) echo '[{"head":0}]' ;;
  *) : ;;
esac
EOF
  chmod +x "$RBIN/bd"
  if (cd "$ROOT" && go build -o "$RPLUG/bin/ateam" ./cmd/ateam) 2>"$T/build.err"; then
    env AGENT_TEAMS_HOME="$RH" AGENT_TEAMS_HOST=thisbox PATH="$RBIN:$PATH" CLAUDE_PLUGIN_ROOT="$RPLUG" \
      HEARTBEAT_SECS=14400 FOLLOW_CHECK_SECS=1 \
      bash -c 'cd "$1" && exec bash "$2" <<<"$3"' _ "$RH/steward/session" "$HOOKS/wake-watcher.sh" \
      '{"session_id":"real-follow"}' >"$T/real-watcher.out" 2>&1 &
    RWPID=$!
    RLOCK="$RMB/host-follow.lock"
    lock_held() { [ -n "$(lsof -t "$RLOCK" 2>/dev/null)" ]; }
    if wait_until 100 file_nonempty "$RMB/host-follow.pid" && wait_until 50 lock_held; then
      OLDF=$(cat "$RMB/host-follow.pid")
      pass "H: real follower started (pid $OLDF) and holds the lock"
      kill -9 "$OLDF" 2>/dev/null || true
      new_follower() {
        local n; n=$(cat "$RMB/host-follow.pid" 2>/dev/null || true)
        [ -n "$n" ] && [ "$n" != "$OLDF" ] && kill -0 "$n" 2>/dev/null && [ -n "$(lsof -t "$RLOCK" 2>/dev/null)" ]
      }
      if wait_until 80 new_follower; then
        pass "H: watcher restarted the follower (new pid $(cat "$RMB/host-follow.pid")) which holds the lock"
      else
        fail "H: no new follower after kill -9 of $OLDF; watcher log: $(cat "$T/real-watcher.out"); follow log: $(cat "$RH/debug/mail-follow.log" 2>/dev/null)"
      fi
    else
      fail "H: real follower never started; follow log: $(cat "$RH/debug/mail-follow.log" 2>/dev/null)"
    fi
    kill "$RWPID" 2>/dev/null || true; wait "$RWPID" 2>/dev/null || true
    # Detached follower(s) and stub bd tails: kill by pid.
    for p in $(lsof -t "$RLOCK" 2>/dev/null) $(cat "$RMB/host-follow.pid" 2>/dev/null); do kill "$p" 2>/dev/null || true; done
    sleep 0.5
    for p in $(ps -axww -o pid=,command= | awk -v m="bdstub-tail $T" 'index($0,m){print $1}'); do kill "$p" 2>/dev/null || true; done
  else
    fail "H: go build ateam failed: $(cat "$T/build.err")"
  fi
else
  echo "SKIP H: go or lsof unavailable"
fi

echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
