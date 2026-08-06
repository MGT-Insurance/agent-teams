#!/usr/bin/env bash
# SubagentStart hook for agent-teams. SYNC-ONLY: SubagentStart hook stdout is
# never rendered into a spawned agent's context at any size (verified,
# bbsz.15) — a role-learnings payload written here would reach nobody. Actual
# delivery is the spawned agent's own on-spawn step 1, which unconditionally
# self-fetches via `ateam learnings <role>` (roles/{implementer,planner,investigator,
# tester,reviewer}.md). This hook's only job is to freshen the local memory
# store (`ateam pull`) before that self-fetch runs, so it reads current data.
# Silent no-op if ateam/jq not installed. Never fails.
#
# MATCHER IS A CATCH-ALL, DELIBERATELY (agent-teams-wf7o.13). On the teammate
# (named) spawn path, agent_type in this hook's payload is the ARBITRARY
# SPAWN NAME the DRI invented for this call (e.g. "planner-at-qr2i",
# "hookprobe") — NOT the agent definition/role. No field in this payload
# identifies the definition at all (the sidecar's customAgentType does, but
# that's not exposed here). So no regex can match a named spawn by role, and
# the action below is role-independent anyway (ateam pull), so a catch-all
# loses nothing. See plugins/agent-teams/roles/README.md for the full
# workaround writeup this is part of.
#
# THROTTLED: a catch-all fires for every subagent (Explore, general-purpose,
# fork, nested spawns too), and `ateam pull` takes bd's flock on
# .beads/embeddeddolt/.lock — unthrottled, a fan-out session would trade a
# silent no-op for real lock contention and spawn latency on work that has
# nothing to do with agent-teams. A timestamp file skips the pull if one ran
# within the last THROTTLE_SECONDS. 60s is the starting value: short enough
# that a role's on-spawn self-fetch (ateam learnings <role>) still sees data
# no more than a minute stale, long enough that a fan-out of a dozen
# subagents in the same burst collapses to one real pull. No locking on the
# throttle file — a racy read just costs one extra pull, which is harmless.
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-${HOME:-}/.agent-teams}"
ATEAM="${CLAUDE_PLUGIN_ROOT:-}/bin/ateam"
THROTTLE_SECONDS=60
THROTTLE_FILE="${ATH}/.subagent-prime-learnings.last-pull"

# SubagentStart passes JSON on stdin — capture it first (required below).
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

# agent_type is the free-form spawn name on the teammate path, not a role —
# log it as-is alongside agent_id rather than asserting a role this hook
# cannot know (see header). Purely informational; nothing below branches on it.
agent_type=$(printf '%s' "$HOOK_STDIN" | jq -r '.agent_type // empty' 2>/dev/null || true)
agent_id=$(printf '%s' "$HOOK_STDIN" | jq -r '.agent_id // empty' 2>/dev/null || true)
hook_log_note "note" "agent_type=${agent_type} agent_id=${agent_id}"

# Throttle: skip the pull if one already ran within THROTTLE_SECONDS.
now=$(date +%s 2>/dev/null || echo 0)
last=0
if [ -f "$THROTTLE_FILE" ]; then
  last=$(cat "$THROTTLE_FILE" 2>/dev/null || echo 0)
  case "$last" in ''|*[!0-9]*) last=0 ;; esac
fi
if [ "$last" -gt 0 ] && [ $((now - last)) -lt "$THROTTLE_SECONDS" ]; then
  HOOK_EXIT_REASON="throttled"
  exit 0
fi

mkdir -p "$ATH" 2>/dev/null || true
printf '%s' "$now" > "$THROTTLE_FILE" 2>/dev/null || true

# Pull must go through ateam/bd: bd's flock on .beads/embeddeddolt/.lock serializes
# parallel subagent pulls; shelling 'dolt' directly would bypass it and hit the manifest race.
# Stdout is dropped by the harness regardless (see header) — redirected here so
# the hook stays silent by design rather than by accident of the render bug.
"$ATEAM" pull >/dev/null || true

HOOK_EXIT_REASON="ok"
exit 0
