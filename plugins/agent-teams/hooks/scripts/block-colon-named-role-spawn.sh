#!/usr/bin/env bash
# PreToolUse hook: deny a NAMED spawn of the removed agent-teams:<role> colon
# key. Exists because of an open Claude Code bug (anthropics/claude-code#81746)
# — see plugins/agent-teams/roles/README.md for the full workaround writeup.
#
# The four role definitions moved out of the plugin's agents/ directory into
# roles/ (agent-teams-wf7o.16), so the colon types (agent-teams:planner etc.)
# no longer exist. That would be fine if a bad spawn failed loudly, but the
# two spawn paths validate differently: the UNNAMED path checks the registry
# and rejects an unknown subagent_type with the list of valid ones; the NAMED
# path never consults the registry — it launches a nonexistent type as a
# generic agent with full tools and NO error at all (measured). So a stale
# "agent-teams:<role>" + name — from habit, an old transcript, a copied prose
# snippet — fails silently forever. This hook converts that silence into a
# message naming the correct hyphen key.
#
# Deny predicate (verbatim from agent-teams-wf7o.9 artifact (5)): ALL of —
#   tool_name == "Agent"
#   AND tool_input.subagent_type matches "agent-teams:*" (COLON form)
#   AND tool_input.name is present and non-empty
# NO spawnDepth / agent_id carve-out. An earlier contract (agent-teams-wf7o.1)
# froze one and it was FALSIFIED by live test: a depth-1 subagent's named
# child also became a teammate with the definition dropped. So this hook
# does not look at agent_id/spawn depth at all — the predicate above is
# unconditional.
set -euo pipefail

command -v jq >/dev/null 2>&1 || exit 0

# Read the PreToolUse hook payload from stdin.
payload=$(cat)

tool_name=$(printf '%s' "$payload" | jq -r '.tool_name // empty' 2>/dev/null || true)
[ "$tool_name" = "Agent" ] || exit 0

subagent_type=$(printf '%s' "$payload" | jq -r '.tool_input.subagent_type // empty' 2>/dev/null || true)
case "$subagent_type" in
  agent-teams:*) role="${subagent_type#agent-teams:}" ;;
  *) exit 0 ;;
esac

name=$(printf '%s' "$payload" | jq -r '.tool_input.name // empty' 2>/dev/null || true)
[ -n "$name" ] || exit 0

# Denial message — exact string from agent-teams-wf7o.9 artifact (6), with
# <role> substituted. Do not paraphrase or re-derive.
DENIAL_MSG="BLOCKED: agent-teams:${role} no longer exists. The four role definitions were moved out of the plugin's agents/ directory into roles/ to work around an open Claude Code bug (anthropics/claude-code#81746): the teammate spawn path discards plugin-scope agent definitions when a name is passed, and it does so silently - your agent would have started with a generic system prompt and no error. Re-issue this call with subagent_type agent-teams-${role} and keep the name. If agent-teams-${role} is not in your available agent types, this session was not launched by ateam dispatch/ateam resume, so it has no role agents at all - stop and tell the human to dispatch the initiative instead of running it interactively. See plugins/agent-teams/roles/README.md."

jq -n \
  --arg msg "$DENIAL_MSG" \
  '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":$msg}}'

exit 0
