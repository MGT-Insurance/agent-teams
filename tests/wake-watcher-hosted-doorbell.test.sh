#!/usr/bin/env bash
# wake-watcher-hosted-doorbell.test.sh — on a hosted workspace the watcher fires
# a doorbell without re-running `ateam resolve-initiative` (~2.8 s over the
# hosted server); the closed/disabled check runs at arm time and on the
# heartbeat instead (agent-teams-c4i9.20.28, docs/hosted-beads.md F10).
#
# "Hosted" means $MAILBOX/host-follow.pid exists and its pid is alive. A stale
# or missing pidfile keeps the per-doorbell check, so a closed initiative still
# stops the watcher.
#
# A stub `ateam` counts resolve-initiative calls: the first (arm time) returns
# the initiative id, later ones return nothing, as if the initiative had since
# closed. So a doorbell that fires (exit 2) proves the check was skipped, and
# one that stops (exit 0) proves it ran.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HOOKS="$ROOT/plugins/agent-teams/hooks/scripts"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d)
LIVE=""
cleanup() {
  [ -n "$LIVE" ] && kill "$LIVE" 2>/dev/null || true
  rm -rf "$T"
}
trap cleanup EXIT

export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME"
git -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && bd init --prefix at --non-interactive >/dev/null)
MAILBOX="$AGENT_TEAMS_HOME/mailbox"
mkdir -p "$MAILBOX"
WORK="$T/work"; mkdir -p "$WORK"

PLUGIN="$T/plugin"
mkdir -p "$PLUGIN/bin"
CALLS="$T/resolve.calls"
cat > "$PLUGIN/bin/ateam" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  resolve-initiative)
    echo x >> "$STUB_CALLS"
    [ "$(wc -l < "$STUB_CALLS" | tr -d ' ')" = 1 ] && echo at-x
    ;;
esac
exit 0
EOF
chmod +x "$PLUGIN/bin/ateam"
export STUB_CALLS="$CALLS" CLAUDE_PLUGIN_ROOT="$PLUGIN"

sleep 300 </dev/null >/dev/null 2>&1 &
LIVE=$!
DEAD=$(bash -c 'echo $$')   # a pid that has already exited

file_nonempty() { [ -s "$1" ]; }
wait_until() {
  local max_ticks="$1"; shift
  local i=0
  until "$@" 2>/dev/null; do
    i=$((i+1))
    [ "$i" -lt "$max_ticks" ] || return 1
    sleep 0.1
  done
}

# run_case <label> <pidfile-content or ""> <want-rc> <want-resolve-calls>
run_case() {
  local label="$1" follower="$2" want_rc="$3" want_calls="$4" wpid rc=0 calls
  rm -f "$CALLS" "$MAILBOX"/*
  [ -n "$follower" ] && printf '%s' "$follower" > "$MAILBOX/host-follow.pid"
  bash -c 'cd "$1" && exec bash "$2" <<<"$3"' _ "$WORK" "$HOOKS/wake-watcher.sh" \
    '{"session_id":"hosted-doorbell-test"}' >"$T/watcher.out" 2>&1 &
  wpid=$!
  if ! wait_until 50 file_nonempty "$MAILBOX/at-x.watcher.pid"; then
    fail "$label: watcher never armed: $(cat "$T/watcher.out")"
    kill "$wpid" 2>/dev/null || true
    return
  fi
  touch "$MAILBOX/at-x.wake"
  wait "$wpid" || rc=$?
  calls=$(wc -l < "$CALLS" | tr -d ' ')
  [ "$rc" = "$want_rc" ] && pass "$label: exit $rc" || fail "$label: exit $rc, want $want_rc"
  [ "$calls" = "$want_calls" ] && pass "$label: resolve-initiative ran $calls time(s)" \
    || fail "$label: resolve-initiative ran $calls time(s), want $want_calls"
}

run_case "A hosted (live follower pidfile)" "$LIVE" 2 1
run_case "B stale pidfile (dead pid)" "$DEAD" 0 2
run_case "C no pidfile" "" 0 2

echo "$PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
