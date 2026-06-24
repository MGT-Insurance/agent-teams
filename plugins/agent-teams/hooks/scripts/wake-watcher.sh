#!/usr/bin/env bash
# asyncRewake Stop-hook watcher for agent-teams.
# Arms on every Stop (session goes idle); singleton-per-initiative prevents
# accumulation; blocks on a dependency-free poll-loop until either the
# per-initiative doorbell fires (real mail) or the 4h heartbeat deadline
# arrives (keepalive re-arm); exits 2 in both cases to wake the session.
# Silently exits 0 when cwd is not a registered OPEN initiative or when
# the initiative has since been CLOSED.
#
# On doorbell: emits a SIGNAL to stderr (the woken turn's prompt) telling the
# model to run `ateam inbox`. Does NOT consume (drain) mail — the model is the
# single consume path. The doorbell firing is sufficient proof of mail; no peek
# is needed here.
#
# Keying: doorbell  ~/.agent-teams/mailbox/<initiative-id>.wake
#         pidfile   ~/.agent-teams/mailbox/<initiative-id>.watcher.pid
#
# Wired in hooks.json as:
#   { type:command, command:"${CLAUDE_PLUGIN_ROOT}/hooks/scripts/wake-watcher.sh",
#     async:true, asyncRewake:true, timeout:86400 }
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-$HOME/.agent-teams}"
MAILBOX="$ATH/mailbox"

# Capture stdin once non-blocking — Claude Code passes {session_id, ...} on stdin;
# direct invocations have no stdin.  Must not break set -euo pipefail when empty.
HOOK_STDIN=$(cat 2>/dev/null || true)
HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
export HOOK_SESSION_ID

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$(dirname "$0")/lib/hook-debug-log.sh"

# Log start BEFORE any guard check so we always know the hook fired.
hook_log_start "wake-watcher.sh"

# Dependency guard — need bd + jq in PATH.
if ! command -v bd  >/dev/null 2>&1 \
   || ! command -v jq  >/dev/null 2>&1 \
   || [ ! -d "$ATH/.beads" ]; then
  HOOK_EXIT_REASON="missing-deps"
  exit 0
fi

# ── Resolve initiative id by worktree:$PWD (match the worktree root OR any subdir) ──
match_id=$(bd -C "$ATH" list --status=open --json 2>/dev/null \
  | jq -r --arg pwd "$PWD" \
      '[.[] | select((.description // "") | split("\n") | map(select(startswith("worktree: ")) | ltrimstr("worktree: ")) | any(. as $w | $pwd == $w or ($pwd | startswith($w + "/"))))][0].id // empty' \
  2>/dev/null || true)
if [ -z "$match_id" ]; then
  HOOK_EXIT_REASON="no-open-match"
  exit 0
fi

# Now that we have the initiative id, export it so all subsequent log lines carry it.
HOOK_INITIATIVE="$match_id"
export HOOK_INITIATIVE

# ── Paths ───────────────────────────────────────────────────────────────────
mkdir -p "$MAILBOX"
DOORBELL="$MAILBOX/${match_id}.wake"
PIDFILE="$MAILBOX/${match_id}.watcher.pid"

# ── Singleton: kill any prior watcher for this initiative ───────────────────
if [ -f "$PIDFILE" ]; then
  old_pid=$(cat "$PIDFILE" 2>/dev/null || true)
  if [ -n "$old_pid" ] && kill -0 "$old_pid" 2>/dev/null; then
    kill "$old_pid" 2>/dev/null || true
    # Brief wait — the old watcher may be in a sleep; give it a tick to die.
    sleep 0.1 2>/dev/null || true
    hook_log_note "note" "pidfile-takeover old_pid=${old_pid} new_pid=$$"
  else
    hook_log_note "note" "pidfile-claim pid=$$ (old_pid=${old_pid:-none} was not running)"
  fi
else
  hook_log_note "note" "pidfile-claim pid=$$"
fi
printf '%s' "$$" > "$PIDFILE"

# ── Heartbeat interval: 4 hours = 14400 seconds, just under the 24h timeout ──
HEARTBEAT_SECS=14400
deadline=$(( $(date +%s) + HEARTBEAT_SECS ))
start_epoch=$(date +%s)
last_alive_log=$start_epoch
alive_interval=60   # log an "alive" tick every 60 seconds

# ── Poll-loop ────────────────────────────────────────────────────────────────
while true; do
  # Guard: still the registered watcher for this initiative?
  live_pid=$(cat "$PIDFILE" 2>/dev/null || true)
  if [ "$live_pid" != "$$" ]; then
    HOOK_EXIT_REASON="superseded"
    exit 0
  fi

  # Doorbell check.
  if [ -f "$DOORBELL" ]; then
    rm -f "$DOORBELL"
    hook_log_note "note" "doorbell-seen initiative=${match_id}"
    HOOK_EXIT_REASON="doorbell-fired"
    printf "You have new mail — run \`ateam inbox\` to read it. (Messages are beads, not files — nothing to read on disk.)\n" >&2
    exit 2
  fi

  # Heartbeat deadline: exit 2 to trigger a cheap re-arm turn.
  now=$(date +%s)
  if [ "$now" -ge "$deadline" ]; then
    # Stop-on-closed: check initiative status before re-arming.
    initiative_status=$(bd -C "$ATH" show "$match_id" --json 2>/dev/null \
      | jq -r '.status // empty' 2>/dev/null || true)
    case "$initiative_status" in
      closed|CLOSED|done|DONE)
        # Initiative is closed — stop pulsing, go quiet.
        HOOK_EXIT_REASON="initiative-closed"
        exit 0
        ;;
    esac
    HOOK_EXIT_REASON="heartbeat-rearm"
    printf 'agent-teams: heartbeat re-arm for initiative %s — no new mail, do nothing.\n' "$match_id" >&2
    exit 2
  fi

  # Alive tick: log approximately every alive_interval seconds.
  elapsed=$(( now - start_epoch ))
  if [ $(( now - last_alive_log )) -ge "$alive_interval" ]; then
    hook_log_note "note" "alive elapsed=${elapsed}s"
    last_alive_log=$now
  fi

  sleep 1
done
