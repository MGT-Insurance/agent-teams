#!/usr/bin/env bash
# SubagentStart hook for agent-teams. SYNC-ONLY: SubagentStart hook stdout is
# never rendered into a spawned agent's context at any size (verified,
# bbsz.15) — a role-learnings payload written here would reach nobody. Actual
# delivery is the spawned agent's own on-spawn step 1, which unconditionally
# self-fetches via `ateam learnings <role>` (agents/{implementer,planner,
# tester,reviewer}.md). This hook's only job is to freshen the local memory
# store (`ateam pull`) before that self-fetch runs, so it reads current data.
# Silent no-op if ateam/jq not installed, or agent_type is absent. Never fails.
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-${HOME:-}/.agent-teams}"
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

# agent_type may be namespace-qualified on cold spawn (e.g. "agent-teams:reviewer");
# strip any "namespace:" prefix since bd-memory keys are stored bare-role-keyed.
role="${agent_type##*:}"
hook_log_note "note" "agent_type=${agent_type} role=${role}"

# Pull must go through ateam/bd: bd's flock on .beads/embeddeddolt/.lock serializes
# parallel subagent pulls; shelling 'dolt' directly would bypass it and hit the manifest race.
# Stdout is dropped by the harness regardless (see header) — redirected here so
# the hook stays silent by design rather than by accident of the render bug.
"$ATEAM" pull >/dev/null || true

HOOK_EXIT_REASON="ok"
exit 0
