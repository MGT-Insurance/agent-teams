#!/usr/bin/env bash
# SessionStart hook for agent-teams.
# Best-effort remote pull so DRIs read fresh learnings+initiatives, not stale local Dolt.
# Pull must go through ateam/bd: the dolt sql-server serializes DOLT_PULL
# calls behind its own lock; `ateam pull` probes for one already in flight and
# skips (using local state) rather than queuing behind it, and bounds its own
# pull with a timeout — so this hook never waits on a hung transport.
# Never fails — a pull failure degrades to local read, which is always correct.
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-${HOME:-}/.agent-teams}"
ATEAM="${CLAUDE_PLUGIN_ROOT:-}/bin/ateam"

# Capture stdin once non-blocking — Claude Code passes {session_id, ...} on stdin;
# direct invocations have no stdin.
HOOK_STDIN=$(cat 2>/dev/null || true)
HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
export HOOK_SESSION_ID

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$(dirname "$0")/lib/hook-debug-log.sh"

# Log start BEFORE any guard check.
hook_log_start "session-start-pull.sh"

if ! { [ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -x "$ATEAM" ]; }; then
  HOOK_EXIT_REASON="missing-deps"
  exit 0
fi

"$ATEAM" pull || true

HOOK_EXIT_REASON="ok"
exit 0
