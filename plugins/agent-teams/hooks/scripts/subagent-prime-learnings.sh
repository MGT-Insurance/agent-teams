#!/usr/bin/env bash
# SubagentStart hook for agent-teams.
# Injects role learnings into spawned role agents via `ateam learnings <role>`.
# Silent no-op if ateam/jq not installed, or agent_type is absent. Never fails.
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-$HOME/.agent-teams}"
ATEAM="${CLAUDE_PLUGIN_ROOT:-}/bin/ateam"

# SubagentStart passes JSON on stdin — capture it first (required to read agent_type).
HOOK_STDIN=$(cat 2>/dev/null || true)
HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
export HOOK_SESSION_ID

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$(dirname "$0")/lib/hook-debug-log.sh"

# Log start BEFORE any guard check.
hook_log_start "subagent-prime-learnings.sh"

if ! { [ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -x "$ATEAM" ]; }; then
  HOOK_EXIT_REASON="missing-deps"
  exit 0
fi
command -v jq >/dev/null 2>&1 || { HOOK_EXIT_REASON="missing-deps"; exit 0; }

agent_type=$(printf '%s' "$HOOK_STDIN" | jq -r '.agent_type // empty' 2>/dev/null || true)

if [ -z "$agent_type" ]; then
  HOOK_EXIT_REASON="no-agent-type"
  exit 0
fi

hook_log_note "note" "agent_type=${agent_type}"

# Pull must go through ateam/bd: bd's flock on .beads/embeddeddolt/.lock serializes
# parallel subagent pulls; shelling 'dolt' directly would bypass it and hit the manifest race.
"$ATEAM" pull || true
"$ATEAM" learnings "$agent_type" || true

HOOK_EXIT_REASON="ok"
exit 0
