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
# model to run `ateam mail inbox`. Does NOT consume (drain) mail — the model is the
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
ATEAM="${CLAUDE_PLUGIN_ROOT:-}/bin/ateam"

# Capture stdin once non-blocking — Claude Code passes {session_id, ...} on stdin;
# direct invocations have no stdin.  Must not break set -euo pipefail when empty.
HOOK_STDIN=$(cat 2>/dev/null || true)
HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
export HOOK_SESSION_ID

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$(dirname "$0")/lib/hook-debug-log.sh"

# Log start BEFORE any guard check so we always know the hook fired.
hook_log_start "wake-watcher.sh"

# Dependency guard — need bd + jq in PATH for the stdin parse above and the
# stop-on-closed status check below. `ateam` is needed only on the initiative
# branch, so it is guarded there instead (see below).
if ! command -v bd  >/dev/null 2>&1 \
   || ! command -v jq  >/dev/null 2>&1 \
   || [ ! -d "$ATH/.beads" ]; then
  HOOK_EXIT_REASON="missing-deps"
  exit 0
fi

# ── Steward branch: this is the Steward's own session, not an initiative ────
# The Steward is machine-scoped rather than tied to a closeable initiative, so
# it skips the worktree-based lookup below entirely, and is_steward_session
# also gates the stop-on-closed check further down in the heartbeat block.
# shellcheck source=plugins/agent-teams/hooks/scripts/lib/resolve-steward.sh
. "$(dirname "$0")/lib/resolve-steward.sh"
# shellcheck source=plugins/agent-teams/hooks/scripts/lib/watcher-pidfile.sh
. "$(dirname "$0")/lib/watcher-pidfile.sh"

is_steward_session=0
if is_steward_cwd; then
  is_steward_session=1
fi

if [ "$is_steward_session" = 1 ]; then
  match_id="steward"
else
  # ── Resolve initiative id for $PWD (the worktree root OR any subdir) ────────
  # `ateam resolve-initiative` owns the matching rule (internal/verbs/match.go);
  # this script must not re-derive it. The ateam guard lives here rather than
  # with bd/jq above because the Steward branch resolves no initiative and has
  # to keep arming its watcher regardless.
  if ! { [ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -x "$ATEAM" ]; }; then
    HOOK_EXIT_REASON="missing-deps"
    exit 0
  fi
  match_id=$("$ATEAM" resolve-initiative "$PWD" 2>/dev/null || true)
  if [ -z "$match_id" ]; then
    HOOK_EXIT_REASON="no-open-match"
    exit 0
  fi
fi

# Now that we have the initiative id, export it so all subsequent log lines carry it.
HOOK_INITIATIVE="$match_id"
export HOOK_INITIATIVE

# ── Paths ───────────────────────────────────────────────────────────────────
mkdir -p "$MAILBOX"
DOORBELL="$MAILBOX/${match_id}.wake"
PIDFILE="$MAILBOX/${match_id}.watcher.pid"

# pidfile_pid/pidfile_session (pid<TAB>session_id parsing) come from
# lib/watcher-pidfile.sh, sourced above.

# ── Singleton: claim the watcher pidfile for this match_id ──────────────────
# First-one-wins: an ALIVE incumbent from a DIFFERENT session is left running
# untouched — this is the fix for two Steward sessions racing over one
# pidfile slot (the old last-one-wins takeover let the second session's
# watcher unconditionally kill the first's, stealing its wake while the
# first session sat there thinking it still owned the watcher). A session
# re-arming its OWN watcher (same session_id, e.g. after a Stop) still
# supersedes cleanly, and a DEAD incumbent is always claimed regardless of
# session. An old-format pidfile (pid only, no session) can't be attributed
# to a session: dead -> claim as before; alive -> treat as foreign and
# refuse, since we can't prove it's this same session — it self-heals on the
# incumbent's next re-arm, which writes a new-format entry.
if [ -f "$PIDFILE" ]; then
  old_entry=$(cat "$PIDFILE" 2>/dev/null || true)
  old_pid=$(pidfile_pid "$old_entry")
  old_session=$(pidfile_session "$old_entry")
  if [ -n "$old_pid" ] && kill -0 "$old_pid" 2>/dev/null; then
    if [ -n "$old_session" ] && [ "$old_session" = "$HOOK_SESSION_ID" ]; then
      kill "$old_pid" 2>/dev/null || true
      # Brief wait — the old watcher may be in a sleep; give it a tick to die.
      sleep 0.1 2>/dev/null || true
      hook_log_note "note" "pidfile-takeover old_pid=${old_pid} new_pid=$$"
    else
      hook_log_note "note" "duplicate-watcher old_pid=${old_pid} old_session=${old_session:-unknown} new_session=${HOOK_SESSION_ID}"
      HOOK_EXIT_REASON="duplicate-watcher"
      exit 0
    fi
  else
    hook_log_note "note" "pidfile-claim pid=$$ (old_pid=${old_pid:-none} was not running)"
  fi
else
  hook_log_note "note" "pidfile-claim pid=$$"
fi
printf '%s\t%s' "$$" "$HOOK_SESSION_ID" > "$PIDFILE"

# ── Heartbeat interval: 4 hours = 14400 seconds, just under the 24h timeout ──
HEARTBEAT_SECS=14400
deadline=$(( $(date +%s) + HEARTBEAT_SECS ))
start_epoch=$(date +%s)
last_alive_log=$start_epoch
alive_interval=60   # log an "alive" tick every 60 seconds

# ── Poll-loop ────────────────────────────────────────────────────────────────
while true; do
  # Guard: still the registered watcher for this initiative?
  live_pid=$(pidfile_pid "$(cat "$PIDFILE" 2>/dev/null || true)")
  if [ "$live_pid" != "$$" ]; then
    HOOK_EXIT_REASON="superseded"
    exit 0
  fi

  # Doorbell check. Do NOT consume the doorbell here — the rewake this exit
  # requests can be lost (e.g. respawn's first worker attempt crashing after
  # SessionStart). inbox-drain.sh removes it when a turn actually starts, so
  # a lost rewake just means the next armed watcher fires again.
  if [ -f "$DOORBELL" ]; then
    hook_log_note "note" "doorbell-seen initiative=${match_id}"
    HOOK_EXIT_REASON="doorbell-fired"
    printf "You have new mail — run \`ateam mail inbox\` to read it. (Messages are beads, not files — nothing to read on disk.)\n" >&2
    exit 2
  fi

  # Heartbeat deadline: exit 2 to trigger a cheap re-arm turn.
  now=$(date +%s)
  if [ "$now" -ge "$deadline" ]; then
    # Stop-on-closed: check initiative status before re-arming. Skipped for
    # the Steward — it is not a closeable initiative bead and always re-arms.
    if [ "$is_steward_session" != 1 ]; then
      initiative_status=$(bd -C "$ATH" show "$match_id" --json 2>/dev/null \
        | jq -r '.status // empty' 2>/dev/null || true)
      case "$initiative_status" in
        closed|CLOSED|done|DONE)
          # Initiative is closed — stop pulsing, go quiet.
          HOOK_EXIT_REASON="initiative-closed"
          exit 0
          ;;
      esac
    fi
    HOOK_EXIT_REASON="heartbeat-rearm"
    if [ "$is_steward_session" = 1 ]; then
      # Under FROZEN-4 (see skills/steward/SKILL.md §5), the Steward has
      # something to do on this wake, unlike a DRI: check whether anything
      # went to Eric during this window, and if not, post one briefing line.
      printf 'agent-teams: heartbeat re-arm for the Steward — check whether anything went to Eric in this window; if nothing did, post one briefing line confirming what is running and that it is green.\n' >&2
    else
      printf 'agent-teams: heartbeat re-arm for initiative %s — no new mail, do nothing.\n' "$match_id" >&2
    fi
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
