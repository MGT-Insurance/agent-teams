#!/usr/bin/env bash
# SessionStart(source=compact) recovery for agent-teams.
# If cwd is the worktree of a registered OPEN initiative in the global
# workspace, re-inject that initiative's context. Silent no-op otherwise —
# never broadcasts machine-wide state (teammate sessions also fire hooks).
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-$HOME/.agent-teams}"

# Capture stdin once non-blocking — Claude Code passes {session_id, ...} on stdin;
# direct invocations have no stdin.
HOOK_STDIN=$(cat 2>/dev/null || true)
HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
export HOOK_SESSION_ID

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$(dirname "$0")/lib/hook-debug-log.sh"

# Log start BEFORE any guard check.
hook_log_start "compact-recovery.sh"

command -v bd >/dev/null 2>&1 || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
command -v jq >/dev/null 2>&1 || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
[ -d "$ATH/.beads" ] || { HOOK_EXIT_REASON="missing-deps"; exit 0; }

match_id=$(bd -C "$ATH" list --status=open --json 2>/dev/null \
  | jq -r --arg wt "worktree: $PWD" \
      '[.[] | select((.description // "") | split("\n") | any(. == $wt))][0].id // empty' \
  2>/dev/null || true)
if [ -z "$match_id" ]; then
  HOOK_EXIT_REASON="no-open-match"
  exit 0
fi

HOOK_INITIATIVE="$match_id"
export HOOK_INITIATIVE

hook_log_note "note" "initiative-resolved id=${match_id}"

echo "## agent-teams: initiative context (post-compaction recovery)"
bd -C "$ATH" show "$match_id" 2>/dev/null || true
cat <<'EOF'

This session is the DRI for the initiative above. The /dri skill governs it —
re-read the dri skill if its guidance is no longer in context. Recover working
state from: this initiative's notes, `bd human list` in the global workspace
(parked gates), and the project repo's beads (plan, discovery beads).
EOF

HOOK_EXIT_REASON="ok"
