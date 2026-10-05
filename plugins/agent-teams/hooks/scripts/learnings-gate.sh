#!/usr/bin/env bash
# PreToolUse / PostToolUse(Agent) hook: the learnings gate
# (agent-teams-75h7.2; layout and rules frozen in lib/learnings-gate.sh).
#
# Usage: learnings-gate.sh pre | post
#
# pre  (PreToolUse, matcher ".*"): deny every tool call of an agent-teams
#      context that has not yet run `ateam learnings <role>`, except that one
#      exact command. Two kinds of context are gated:
#        - the main thread (calls with no agent_id) while
#          <ATH>/learnings-gate/<sid>/main exists (armed by
#          role-recall-recovery.sh on clear|compact);
#        - a role subagent or teammate (calls with an agent_id) whose role
#          resolves and that has no loaded/<agent_id> record.
#      Allowing the exact command is also the clear: the main marker is
#      removed, or the loaded record is written. Allowed Agent spawns of
#      agent-teams-<role> with a name are recorded so the teammate's later
#      calls (agent_type = the name) resolve to a role.
# post (PostToolUse, matcher "Agent"): record the harness's final teammate
#      name -> role when tool_response.status is "teammate_spawned" (the
#      harness renames duplicates, for example probe-t1 -> probe-t1-2).
#      Never prints.
#
# FAIL OPEN: every doubt path (jq missing, unparsable input, invalid ids, no
# gate dir, unknown role, unreadable files) exits 0 with empty stdout. This
# script never exits 2 and never uses set -e, so no unexpected failure can
# turn into a block on a session that has nothing to do with agent-teams.
# A session with no gate dir costs one jq parse.

mode="${1:-}"
# Read stdin first so an early exit never leaves the harness writing to a
# closed pipe.
payload=$(cat 2>/dev/null || true)
case "$mode" in pre | post) ;; *) exit 0 ;; esac

command -v jq >/dev/null 2>&1 || exit 0

ATH="${AGENT_TEAMS_HOME:-${HOME:-}/.agent-teams}"
[ -n "$ATH" ] || exit 0
export ATH

script_dir="$(cd "$(dirname "$0")" 2>/dev/null && pwd)" || exit 0
roles_dir="$script_dir/../../roles"

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$script_dir/lib/hook-debug-log.sh" 2>/dev/null || exit 0
# shellcheck source=plugins/agent-teams/hooks/scripts/lib/learnings-gate.sh
. "$script_dir/lib/learnings-gate.sh" 2>/dev/null || exit 0
_HOOK_LOG_SCRIPT="learnings-gate.sh"

# Parse stdin once. Fields are NUL-terminated so a multi-line command stays
# exact (the allow-match must see the real bytes).
session_id="" agent_id="" agent_type="" tool_name="" tool_cmd="" subagent_type="" name=""
resp_status="" resp_name="" resp_agent_type=""
{
  IFS= read -r -d '' session_id
  IFS= read -r -d '' agent_id
  IFS= read -r -d '' agent_type
  IFS= read -r -d '' tool_name
  IFS= read -r -d '' tool_cmd
  IFS= read -r -d '' subagent_type
  IFS= read -r -d '' name
  IFS= read -r -d '' resp_status
  IFS= read -r -d '' resp_name
  IFS= read -r -d '' resp_agent_type
} < <(printf '%s' "$payload" | jq -j '
  def s: if type == "string" then . else "" end;
  [ (.session_id | s), (.agent_id | s), (.agent_type | s), (.tool_name | s),
    (.tool_input.command | s), (.tool_input.subagent_type | s),
    (.tool_input.name | s), (.tool_response.status | s),
    (.tool_response.name | s), (.tool_response.agent_type | s) ]
  | map(. + "\u0000") | add' 2>/dev/null)

valid_session_id "$session_id" || exit 0
HOOK_SESSION_ID="$session_id"
export HOOK_SESSION_ID

# Record <name> -> <role> when subagent_type is a known agent-teams-<role>.
record_spawn_from() { # record_spawn_from <name> <agent-teams-role type> <event>
  local spawn_name="$1" spawn_type="$2" event="$3" spawn_role
  case "$spawn_type" in agent-teams-*) ;; *) return 0 ;; esac
  spawn_role="${spawn_type#agent-teams-}"
  lg_known_subagent_role "$spawn_role" "$roles_dir" || return 0
  lg_record_spawn "$ATH" "$session_id" "$spawn_name" "$spawn_role" || return 0
  hook_log_note "$event" "name=${spawn_name} role=${spawn_role}"
}

if [ "$mode" = "post" ]; then
  [ "$tool_name" = "Agent" ] && [ "$resp_status" = "teammate_spawned" ] || exit 0
  record_spawn_from "$resp_name" "$resp_agent_type" "spawn-record-post"
  exit 0
fi

root=$(lg_root "$ATH" "$session_id") || exit 0

is_role_spawn=0
case "$tool_name:$subagent_type" in Agent:agent-teams-*) is_role_spawn=1 ;; esac
# A gate root we cannot read is a doubt path: allow.
if [ -d "$root" ] && { [ ! -r "$root" ] || [ ! -x "$root" ]; }; then exit 0; fi
# Cheap exit: no gate dir, nothing to record, and not a role subagent (an
# agent-teams-<role> agent_type is gated from its first call even when no
# root exists yet, contract item 4).
case "$agent_type" in agent-teams-*) is_role_agent=1 ;; *) is_role_agent=0 ;; esac
[ -d "$root" ] || [ "$is_role_spawn" -eq 1 ] || [ "$is_role_agent" -eq 1 ] || exit 0

gate_role=""
if [ -z "$agent_id" ]; then
  if [ -f "$root/main" ]; then
    gate_role=$(head -n 1 "$root/main" 2>/dev/null) || gate_role=""
    case "$gate_role" in dri | steward) ;; *) gate_role="" ;; esac
  fi
else
  if valid_session_id "$agent_id" && [ ! -f "$root/loaded/$agent_id" ]; then
    gate_role=$(lg_resolve_subagent_role "$ATH" "$session_id" "$agent_type" "$roles_dir")
  fi
fi

if [ -n "$gate_role" ]; then
  if [ "$tool_name" = "Bash" ] && lg_is_allowed_command "$tool_cmd" "$gate_role"; then
    if [ -z "$agent_id" ]; then
      lg_clear_main "$ATH" "$session_id"
    else
      lg_mark_loaded "$ATH" "$session_id" "$agent_id"
    fi
    hook_log_note "clear" "role=${gate_role} agent_id=${agent_id:-main}"
    exit 0
  fi
  reason=$(lg_deny_reason "$gate_role")
  hook_log_note "deny" "role=${gate_role} agent_id=${agent_id:-main} tool=${tool_name}"
  jq -n --arg reason "$reason" \
    '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":$reason}}'
  exit 0
fi

# Allowed. Record a named role spawn so the teammate's calls resolve later.
if [ "$is_role_spawn" -eq 1 ] && [ -n "$name" ] && valid_session_id "$name"; then
  record_spawn_from "$name" "$subagent_type" "spawn-record-pre"
fi
exit 0
