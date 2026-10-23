#!/usr/bin/env bash
# wake-watcher-follow.test.sh — wake-watcher.sh starts `ateam mail follow` and
# stops it on every exit path (docs/hosted-beads.md, F10).
#
# A stub `ateam` on CLAUDE_PLUGIN_ROOT/bin records its argv and pid, then
# blocks. The watcher runs in the Steward's session dir (the follower wiring is
# shared code, identical for any initiative watcher).
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

PLUGIN="$T/plugin"
mkdir -p "$PLUGIN/bin"
ARGS="$T/stub.args"
STUBPID="$T/stub.pid"
cat > "$PLUGIN/bin/ateam" <<'EOF'
#!/usr/bin/env bash
echo "$*" > "$STUB_ARGS"
echo "$$" > "$STUB_PID"
exec sleep 300
EOF
chmod +x "$PLUGIN/bin/ateam"
export STUB_ARGS="$ARGS" STUB_PID="$STUBPID" CLAUDE_PLUGIN_ROOT="$PLUGIN"

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

# run_case <name> <env assignments...> -> sets WPID; launches watcher, waits for stub.
launch() {
  rm -f "$ARGS" "$STUBPID" "$MAILBOX/steward.wake" "$MAILBOX/steward.watcher.pid"
  env "$@" bash -c 'cd "$1" && exec bash "$2" <<<"$3"' _ "$STEWARD_DIR" "$HOOKS/wake-watcher.sh" \
    '{"session_id":"follow-test"}' >"$T/watcher.out" 2>&1 &
  WPID=$!
}

check_start() {
  local label="$1"
  if wait_until 50 file_nonempty "$STUBPID"; then
    pass "$label: stub ateam started"
  else
    fail "$label: stub ateam never started; watcher log: $(cat "$T/watcher.out")"
    return 1
  fi
  local want="mail follow steward --parent-pid $WPID"
  if [ "$(cat "$ARGS")" = "$want" ]; then
    pass "$label: argv is '$want'"
  else
    fail "$label: argv '$(cat "$ARGS")' want '$want'"
  fi
}

# ── Case A: doorbell exit stops the follower ────────────────────────────────
launch HEARTBEAT_SECS=14400
if check_start A; then
  stub=$(cat "$STUBPID")
  touch "$MAILBOX/steward.wake"
  rc=0; wait "$WPID" || rc=$?
  [ "$rc" = 2 ] && pass "A: watcher exited 2 on doorbell" || fail "A: watcher exit $rc, want 2"
  if wait_until 30 pid_dead "$stub"; then
    pass "A: follower stopped after doorbell exit"
  else
    fail "A: follower pid $stub still alive after doorbell exit"; kill "$stub" 2>/dev/null || true
  fi
else
  kill "$WPID" 2>/dev/null || true
fi

# ── Case B: heartbeat exit stops the follower ───────────────────────────────
launch HEARTBEAT_SECS=1
if check_start B; then
  stub=$(cat "$STUBPID")
  rc=0; wait "$WPID" || rc=$?
  [ "$rc" = 2 ] && pass "B: watcher exited 2 on heartbeat" || fail "B: watcher exit $rc, want 2"
  if wait_until 30 pid_dead "$stub"; then
    pass "B: follower stopped after heartbeat exit"
  else
    fail "B: follower pid $stub still alive after heartbeat exit"; kill "$stub" 2>/dev/null || true
  fi
else
  kill "$WPID" 2>/dev/null || true
fi

# ── Case C: SIGTERM to the watcher stops the follower ───────────────────────
launch HEARTBEAT_SECS=14400
if check_start C; then
  stub=$(cat "$STUBPID")
  kill -TERM "$WPID"
  wait "$WPID" 2>/dev/null || true
  if wait_until 30 pid_dead "$stub"; then
    pass "C: follower stopped after SIGTERM"
  else
    fail "C: follower pid $stub still alive after SIGTERM"; kill "$stub" 2>/dev/null || true
  fi
else
  kill "$WPID" 2>/dev/null || true
fi

echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
