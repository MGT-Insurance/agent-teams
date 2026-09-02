#!/usr/bin/env bash
# UserPromptSubmit hook for agent-teams: per-turn watcher disarm + mail signal.
# Fires on every user prompt. Does two things:
#   1. DISARM: kills the pending wake watcher for this initiative (the session
#      is now active; the watcher re-arms on the next Stop).
#   2. SIGNAL: runs `ateam hook-scan` (one combined bd call resolving the
#      initiative id AND unread mail — agent-teams-1y0m.8, replacing the old
#      resolve-initiative + mail inbox --peek pair to cut this hook's per-
#      prompt Dolt opens from 3 to 1); if unread mail is reported, emits an
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

# Extra arg for `ateam hook-scan`: pass the session id when known, so a
# launch-cwd-mismatched session (cwd doesn't match its registered worktree)
# still resolves via its durable session tie instead of going deaf
# (agent-teams-y814.4, at-1k234). "unknown" is the stdin-parse-failure
# sentinel above, not a real session id — omit it too, same as an empty id.
session_id_flag=""
if [ -n "$HOOK_SESSION_ID" ] && [ "$HOOK_SESSION_ID" != "unknown" ]; then
  session_id_flag="$HOOK_SESSION_ID"
fi

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$(dirname "$0")/lib/hook-debug-log.sh"

# Log start BEFORE any guard check.
hook_log_start "inbox-drain.sh"

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
  # The Steward has no worktree: line to resolve, but still needs its unread
  # mail checked — `--id` skips path/worktree resolution and checks unread
  # mail directly for this already-known recipient (internal/verbs/hookscan.go).
  scan_out=$("$ATEAM" hook-scan --id="$match_id" 2>/dev/null || true)
else
  # ── Resolve initiative id for $PWD (the worktree root OR any subdir) AND ───
  # check unread mail, both from ONE combined bd call. `ateam hook-scan` owns
  # the matching rule (internal/verbs/match.go via internal/verbs/hookscan.go);
  # this script must not re-derive it. --session-id (session_id_flag, computed
  # above) lets it resolve via the durable session tie first when cwd alone
  # would find nothing.
  scan_out=$("$ATEAM" hook-scan "$PWD" ${session_id_flag:+--session-id "$session_id_flag"} 2>/dev/null || true)
  match_id=$(printf '%s\n' "$scan_out" | sed -n 's/^id: //p')
  if [ -z "$match_id" ]; then
    HOOK_EXIT_REASON="no-open-match"
    exit 0
  fi
fi
unread=$(printf '%s\n' "$scan_out" | sed -n 's/^unread: //p')

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

# ── Signal: unread mail, from the combined scan above; emit additionalContext ─
if [ -n "$unread" ] && [ "$unread" -gt 0 ]; then
  signal="You have $unread unread message(s) — run \`ateam mail inbox\` to read them."
  HOOK_EXIT_REASON="mail-signaled"
  jq -n --arg ctx "$signal" '{"additionalContext": $ctx}'
fi

if [ "$HOOK_EXIT_REASON" = "unexpected" ]; then
  HOOK_EXIT_REASON="ok"
fi
