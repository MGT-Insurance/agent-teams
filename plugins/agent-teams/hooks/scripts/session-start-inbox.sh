#!/usr/bin/env bash
# SessionStart hook for agent-teams: cold-path mail signal.
# Fires on startup, resume, clear, and compact. Runs `ateam inbox --peek` so
# any mail that arrived while the session was inactive (or before the first
# UserPromptSubmit) is signaled as additionalContext at session open.
# Does NOT consume (drain) mail — the model runs `ateam inbox` to do that.
# Silent no-op when cwd is not a registered initiative.
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-$HOME/.agent-teams}"
ATEAM="${CLAUDE_PLUGIN_ROOT:-}/bin/ateam"

# Capture stdin once non-blocking — Claude Code passes {session_id, ...} on stdin;
# direct invocations have no stdin.  Must not break set -euo pipefail when empty.
HOOK_STDIN=$(cat 2>/dev/null || true)
HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
export HOOK_SESSION_ID

command -v bd    >/dev/null 2>&1 || exit 0
command -v jq    >/dev/null 2>&1 || exit 0
[ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -x "$ATEAM" ] || exit 0
[ -d "$ATH/.beads" ] || exit 0

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$(dirname "$0")/lib/hook-debug-log.sh"

# ── Resolve initiative id by worktree:$PWD (match the worktree root OR any subdir) ──
match_id=$(bd -C "$ATH" list --status=open --json 2>/dev/null \
  | jq -r --arg pwd "$PWD" \
      '[.[] | select((.description // "") | split("\n") | map(select(startswith("worktree: ")) | ltrimstr("worktree: ")) | any(. as $w | $pwd == $w or ($pwd | startswith($w + "/"))))][0].id // empty' \
  2>/dev/null || true)
[ -n "$match_id" ] || exit 0

hook_debug_log "session-start-inbox.sh" "${match_id:-unknown}"

# ── Signal: peek at unread mail; emit additionalContext if any ───────────────
peek_out=$("$ATEAM" inbox --peek 2>/dev/null || true)
# peek reports "N unread message(s)" when mail is present, "no unread mail" otherwise.
case "$peek_out" in
  *"unread message"*)
    signal="You have ${peek_out} — run \`ateam inbox\` to read them."
    jq -n --arg ctx "$signal" '{"additionalContext": $ctx}'
    ;;
esac
