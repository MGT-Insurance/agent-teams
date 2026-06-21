#!/usr/bin/env bash
# UserPromptSubmit hook for agent-teams: per-turn mailbox drain + watcher disarm.
# Fires on every user prompt. Does two things:
#   1. DISARM: kills the pending wake watcher for this initiative (the session
#      is now active; the watcher re-arms on the next Stop).
#   2. DRAIN: runs `ateam inbox`; its stdout is returned as additionalContext so
#      the model sees incoming mail in the current turn.
# Silent no-op when cwd is not a registered initiative — teammate subagents and
# ad-hoc claude sessions must not be affected.
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-$HOME/.agent-teams}"
MAILBOX="$ATH/mailbox"

command -v bd    >/dev/null 2>&1 || exit 0
command -v jq    >/dev/null 2>&1 || exit 0
command -v ateam >/dev/null 2>&1 || exit 0
[ -d "$ATH/.beads" ] || exit 0

# ── Resolve initiative id by worktree:$PWD ──────────────────────────────────
match_id=$(bd -C "$ATH" list --status=open --json 2>/dev/null \
  | jq -r --arg wt "worktree: $PWD" \
      '[.[] | select((.description // "") | split("\n") | any(. == $wt))][0].id // empty' \
  2>/dev/null || true)
[ -n "$match_id" ] || exit 0

# ── Disarm: kill the pending watcher ────────────────────────────────────────
PIDFILE="$MAILBOX/${match_id}.watcher.pid"
if [ -f "$PIDFILE" ]; then
  watcher_pid=$(cat "$PIDFILE" 2>/dev/null || true)
  if [ -n "$watcher_pid" ] && kill -0 "$watcher_pid" 2>/dev/null; then
    kill "$watcher_pid" 2>/dev/null || true
  fi
  rm -f "$PIDFILE"
fi

# ── Drain: run ateam inbox; its output becomes additionalContext ─────────────
inbox_out=$(ateam inbox 2>/dev/null || true)
[ -n "$inbox_out" ] || exit 0

# Emit as additionalContext via the UserPromptSubmit hook stdout protocol.
jq -n --arg ctx "$inbox_out" '{"additionalContext": $ctx}'
