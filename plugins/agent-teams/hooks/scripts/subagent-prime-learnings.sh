#!/usr/bin/env sh
# SubagentStart hook for agent-teams.
# Injects role learnings into spawned role agents via `ateam learnings <role>`.
# Silent no-op if ateam/jq not installed, or agent_type is absent. Never fails.
set -eu
ATEAM="${CLAUDE_PLUGIN_ROOT:-}/bin/ateam"
[ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -x "$ATEAM" ] || exit 0
command -v jq >/dev/null 2>&1 || exit 0

input=$(cat)
agent_type=$(printf '%s' "$input" | jq -r '.agent_type // empty' 2>/dev/null || true)

[ -n "$agent_type" ] || exit 0

"$ATEAM" learnings "$agent_type" || true
exit 0
