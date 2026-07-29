#!/usr/bin/env bash
# UserPromptSubmit hook for agent-teams: per-turn watcher disarm + mail signal.
# Fires on every user prompt. Does two things:
#   1. DISARM: kills the pending wake watcher for this initiative (the session
#      is now active; the watcher re-arms on the next Stop).
#   2. SIGNAL: runs `ateam mail inbox --peek`; if unread mail is reported, emits an
#      additionalContext message telling the model to run `ateam mail inbox`.
#      Does NOT consume (drain) mail — the model runs `ateam mail inbox` to do that.
# Silent no-op when cwd is not a registered initiative — teammate subagents and
# ad-hoc claude sessions must not be affected.
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-${HOME:-}/.agent-teams}"
MAILBOX="$ATH/mailbox"
ATEAM="${CLAUDE_PLUGIN_ROOT:-}/bin/ateam"

# Capture stdin once non-blocking — Claude Code passes {session_id, ...} on stdin;
# direct invocations have no stdin.  Must not break set -euo pipefail when empty.
HOOK_STDIN=$(cat 2>/dev/null || true)
HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
export HOOK_SESSION_ID

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$(dirname "$0")/lib/hook-debug-log.sh"

# Log start BEFORE any guard check.
hook_log_start "inbox-drain.sh"

command -v bd    >/dev/null 2>&1 || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
command -v jq    >/dev/null 2>&1 || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
{ [ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -x "$ATEAM" ]; } || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
[ -d "$ATH/.beads" ] || { HOOK_EXIT_REASON="missing-deps"; exit 0; }

# ── Steward branch: this is the Steward's own session, not an initiative ────
# The Steward doorbell/pidfile use the same "$MAILBOX/${match_id}.*" naming as
# an initiative's, so once match_id=steward is set the disarm/consume logic
# below runs unmodified.
# shellcheck source=plugins/agent-teams/hooks/scripts/lib/resolve-steward.sh
. "$(dirname "$0")/lib/resolve-steward.sh"
# shellcheck source=plugins/agent-teams/hooks/scripts/lib/watcher-pidfile.sh
. "$(dirname "$0")/lib/watcher-pidfile.sh"

if is_steward_cwd; then
  match_id="steward"
else
  # ── Resolve initiative id by worktree:$PWD (match the worktree root OR any subdir) ──
  match_id=$(bd -C "$ATH" list --status=open --json 2>/dev/null \
    | jq -r --arg pwd "$PWD" \
        '[.[] | select((.description // "") | split("\n") | map(select(startswith("worktree: ")) | ltrimstr("worktree: ")) | any(. as $w | $pwd == $w or ($pwd | startswith($w + "/"))))][0].id // empty' \
    2>/dev/null || true)
  if [ -z "$match_id" ]; then
    HOOK_EXIT_REASON="no-open-match"
    exit 0
  fi
fi

HOOK_INITIATIVE="$match_id"
export HOOK_INITIATIVE

hook_log_note "note" "initiative-resolved id=${match_id}"

# ── Disarm: kill the pending watcher ────────────────────────────────────────
# Session-aware, symmetric with wake-watcher.sh's claim rules
# (agent-teams-e3mq.30): only kill/rm a watcher entry this session owns, or
# one whose pid is already dead. A LIVE watcher owned by a DIFFERENT session
# (or an old-format entry, whose session is unattributable) means this
# session is not the session-of-record for this mailbox — leave the pidfile
# and doorbell untouched and exit before the doorbell-consume/mail-peek
# blocks below. Otherwise consuming the doorbell would deliver the
# incumbent's wake into the wrong session, and the peek would prompt this
# session to read the incumbent's mail (the e3mq.29 failure mode, via the
# drain path instead of the claim path).
PIDFILE="$MAILBOX/${match_id}.watcher.pid"
if [ -f "$PIDFILE" ]; then
  entry=$(cat "$PIDFILE" 2>/dev/null || true)
  old_pid=$(pidfile_pid "$entry")
  old_session=$(pidfile_session "$entry")
  if [ -n "$old_pid" ] && kill -0 "$old_pid" 2>/dev/null; then
    if [ -n "$old_session" ] && [ "$old_session" = "$HOOK_SESSION_ID" ]; then
      kill "$old_pid" 2>/dev/null || true
      rm -f "$PIDFILE"
      hook_log_note "note" "watcher-disarmed pid=${old_pid}"
    else
      hook_log_note "note" "foreign-watcher-live old_pid=${old_pid} old_session=${old_session:-unknown} my_session=${HOOK_SESSION_ID}"
      HOOK_EXIT_REASON="foreign-watcher-live"
      exit 0
    fi
  else
    rm -f "$PIDFILE"
  fi
fi

# ── Consume the doorbell: a turn is now definitely running. Watchers no longer
# remove it at fire time (their rewake can be lost); this is the single ack
# point that the wake was actually delivered into a turn.
DOORBELL="$MAILBOX/${match_id}.wake"
if [ -f "$DOORBELL" ]; then
  rm -f "$DOORBELL"
  hook_log_note "note" "doorbell-consumed initiative=${match_id}"
fi

# ── Signal: peek at unread mail; emit additionalContext if any ───────────────
peek_out=$("$ATEAM" mail inbox --peek 2>/dev/null || true)
# peek reports "N unread message(s)" when mail is present, "no unread mail" otherwise.
case "$peek_out" in
  *"unread message"*)
    signal="You have ${peek_out} — run \`ateam mail inbox\` to read them."
    HOOK_EXIT_REASON="mail-signaled"
    jq -n --arg ctx "$signal" '{"additionalContext": $ctx}'
    ;;
esac

if [ "$HOOK_EXIT_REASON" = "unexpected" ]; then
  HOOK_EXIT_REASON="ok"
fi
