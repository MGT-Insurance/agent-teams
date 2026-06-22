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

# Dependency guard — need bd + jq in PATH.
command -v bd  >/dev/null 2>&1 || exit 0
command -v jq  >/dev/null 2>&1 || exit 0
[ -d "$ATH/.beads" ] || exit 0

# ── Resolve initiative id by worktree:$PWD (match the worktree root OR any subdir) ──
match_id=$(bd -C "$ATH" list --status=open --json 2>/dev/null \
  | jq -r --arg pwd "$PWD" \
      '[.[] | select((.description // "") | split("\n") | map(select(startswith("worktree: ")) | ltrimstr("worktree: ")) | any(. as $w | $pwd == $w or ($pwd | startswith($w + "/"))))][0].id // empty' \
  2>/dev/null || true)
[ -n "$match_id" ] || exit 0

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
  fi
fi
printf '%s' "$$" > "$PIDFILE"

# ── Heartbeat interval: 4 hours = 14400 seconds, just under the 24h timeout ──
HEARTBEAT_SECS=14400
deadline=$(( $(date +%s) + HEARTBEAT_SECS ))

# ── Poll-loop ────────────────────────────────────────────────────────────────
while true; do
  # Guard: still the registered watcher for this initiative?
  live_pid=$(cat "$PIDFILE" 2>/dev/null || true)
  [ "$live_pid" = "$$" ] || exit 0

  # Doorbell check.
  if [ -f "$DOORBELL" ]; then
    rm -f "$DOORBELL"
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
        exit 0
        ;;
    esac
    printf 'agent-teams: heartbeat re-arm for initiative %s — no new mail, do nothing.\n' "$match_id" >&2
    exit 2
  fi

  sleep 1
done
