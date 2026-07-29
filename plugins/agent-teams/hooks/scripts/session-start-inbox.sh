#!/usr/bin/env bash
# SessionStart hook for agent-teams: cold-path mail signal.
# Fires on startup, resume, clear, and compact. Runs `ateam mail inbox --peek` so
# any mail that arrived while the session was inactive (or before the first
# UserPromptSubmit) is signaled as additionalContext at session open.
# Does NOT consume (drain) mail — the model runs `ateam mail inbox` to do that.
# Silent no-op when cwd is not a registered initiative.
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-${HOME:-}/.agent-teams}"
ATEAM="${CLAUDE_PLUGIN_ROOT:-}/bin/ateam"

# Capture stdin once non-blocking — Claude Code passes {session_id, ...} on stdin;
# direct invocations have no stdin.  Must not break set -euo pipefail when empty.
HOOK_STDIN=$(cat 2>/dev/null || true)
HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
export HOOK_SESSION_ID

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$(dirname "$0")/lib/hook-debug-log.sh"

# Log start BEFORE any guard check.
hook_log_start "session-start-inbox.sh"

command -v bd    >/dev/null 2>&1 || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
command -v jq    >/dev/null 2>&1 || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
{ [ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -x "$ATEAM" ]; } || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
[ -d "$ATH/.beads" ] || { HOOK_EXIT_REASON="missing-deps"; exit 0; }

# ── Self-registration: tie this session's id to its initiative ─────────────
# (agent-teams-zalv.3, at-ps11 — resolved writer mechanism). `ateam
# tie-session` resolves the initiative from cwd and is a silent no-op for
# every case that doesn't apply here (no open initiative for cwd, Steward
# session, missing session id) and idempotent on re-run (respawn re-fires
# this hook with the same session id). Fail-soft: never let this block or
# noise a session start. Output is captured (not discarded) because the
# cross-open-initiative conflict warning on stderr is deliberately loud
# (Eric: "error/warn path, not a silent second tie") — route any non-empty
# output through the structured hook log instead of a bare redirect.
tie_session_out=$("$ATEAM" tie-session --session-id "$HOOK_SESSION_ID" 2>&1) || true
if [ -n "$tie_session_out" ]; then
  hook_log_note "note" "tie-session: ${tie_session_out}"
fi

# ── Steward branch: this is the Steward's own session, not an initiative ────
# shellcheck source=plugins/agent-teams/hooks/scripts/lib/resolve-steward.sh
. "$(dirname "$0")/lib/resolve-steward.sh"

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

# ── Steward-only: duplicate-session advisory ─────────────────────────────────
# Advisory only — never kills anything (that's wake-watcher.sh's shared
# pidfile-claim fix, the other half of e3mq.29). If another LIVE session (pid
# present, not just tracked-but-dead) is already sitting in the Steward
# session dir under a different session id, tell THIS session to stand down
# instead of letting two Stewards act at once. Fail-soft when `claude` isn't
# on PATH or `claude agents` errors — the mail signal below still runs.
if [ "$match_id" = "steward" ] && command -v claude >/dev/null 2>&1; then
  steward_dir="$ATH/steward/session"
  dup_count=$(claude agents --all --json 2>/dev/null \
    | jq -r --arg dir "$steward_dir" --arg me "$HOOK_SESSION_ID" \
        '[.[] | select((.cwd // "") == $dir) | select(.pid != null) | select((.sessionId // "") != $me)] | length' \
    2>/dev/null || echo 0)
  if [ "${dup_count:-0}" -gt 0 ] 2>/dev/null; then
    HOOK_EXIT_REASON="duplicate-steward-session"
    signal="You are a DUPLICATE Steward session — another live Steward session is already running for this machine. Do not act as the Steward: tell the human immediately and stop."
    jq -n --arg ctx "$signal" '{"additionalContext": $ctx}'
    exit 0
  fi
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
