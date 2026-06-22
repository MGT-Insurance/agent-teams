#!/usr/bin/env bash
# UserPromptSubmit hook for agent-teams: per-turn watcher disarm + mail signal.
# Fires on every user prompt. Does two things:
#   1. DISARM: kills the pending wake watcher for this initiative (the session
#      is now active; the watcher re-arms on the next Stop).
#   2. SIGNAL: runs `ateam inbox --peek`; if unread mail is reported, emits an
#      additionalContext message telling the model to run `ateam inbox`.
#      Does NOT consume (drain) mail — the model runs `ateam inbox` to do that.
# Silent no-op when cwd is not a registered initiative — teammate subagents and
# ad-hoc claude sessions must not be affected.
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-$HOME/.agent-teams}"
MAILBOX="$ATH/mailbox"
ATEAM="${CLAUDE_PLUGIN_ROOT:-}/bin/ateam"

command -v bd    >/dev/null 2>&1 || exit 0
command -v jq    >/dev/null 2>&1 || exit 0
[ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -x "$ATEAM" ] || exit 0
[ -d "$ATH/.beads" ] || exit 0

# ── Resolve initiative id by worktree:$PWD (match the worktree root OR any subdir) ──
match_id=$(bd -C "$ATH" list --status=open --json 2>/dev/null \
  | jq -r --arg pwd "$PWD" \
      '[.[] | select((.description // "") | split("\n") | map(select(startswith("worktree: ")) | ltrimstr("worktree: ")) | any(. as $w | $pwd == $w or ($pwd | startswith($w + "/"))))][0].id // empty' \
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

# ── Signal: peek at unread mail; emit additionalContext if any ───────────────
peek_out=$("$ATEAM" inbox --peek 2>/dev/null || true)
# peek reports "N unread message(s)" when mail is present, "no unread mail" otherwise.
case "$peek_out" in
  *"unread message"*)
    signal="You have ${peek_out} — run \`ateam inbox\` to read them."
    jq -n --arg ctx "$signal" '{"additionalContext": $ctx}'
    ;;
esac
