#!/usr/bin/env bash
# PreToolUse hook: reject an agent-teams:* helper spawn whose model argument
# diverges from what its role's agent definition sets.
#
# Fires on the Agent tool. Scope: subagent_type matching agent-teams:* ONLY —
# general-purpose, Explore, and every other type pass through silently. The
# role is resolved to its definition file (plugins/agent-teams/agents/<role>.md)
# rather than a hardcoded role->model table, so a new role can't silently
# escape the check. The agents dir is derived from this script's own
# location (<plugin-root>/hooks/scripts/) rather than CLAUDE_PLUGIN_ROOT,
# which is unverified to be exported into hook subprocesses.
#
# Allow (silent pass-through): no model in tool_input; the definition has no
# model: line, or model: inherit; the definition file is missing/unreadable;
# the passed model matches the definition's, once both are trimmed of
# surrounding whitespace and quotes. Reject only a genuine divergence — e.g.
# opus vs opus[1m] are different strings and therefore rejected.
set -euo pipefail

command -v jq >/dev/null 2>&1 || exit 0

# Read the PreToolUse hook payload from stdin.
payload=$(cat)

tool_name=$(printf '%s' "$payload" | jq -r '.tool_name // empty' 2>/dev/null || true)
[ "$tool_name" = "Agent" ] || exit 0

subagent_type=$(printf '%s' "$payload" | jq -r '.tool_input.subagent_type // empty' 2>/dev/null || true)
case "$subagent_type" in
  agent-teams:*) ;;
  *) exit 0 ;;
esac

model=$(printf '%s' "$payload" | jq -r '.tool_input.model // empty' 2>/dev/null || true)
[ -n "$model" ] || exit 0

role="${subagent_type#agent-teams:}"

script_dir="$(cd "$(dirname "$0")" && pwd)"
agents_dir=$(cd "$script_dir/../../agents" 2>/dev/null && pwd) || exit 0
def_file="$agents_dir/$role.md"
[ -r "$def_file" ] || exit 0

# Pull the frontmatter `model:` line — the first `---`-delimited block.
def_model_raw=$(awk '
  /^---$/ { d++; if (d == 2) exit; next }
  d == 1 && /^model:/ { sub(/^model:[ \t]*/, ""); print; exit }
' "$def_file")

trim() {
  printf '%s' "$1" | sed -E 's/^[[:space:]]*//; s/[[:space:]]*$//; s/^"(.*)"$/\1/; s/^'"'"'(.*)'"'"'$/\1/'
}

def_model=$(trim "$def_model_raw")
[ -n "$def_model" ] || exit 0
[ "$def_model" = "inherit" ] && exit 0

model_trimmed=$(trim "$model")
[ "$model_trimmed" = "$def_model" ] && exit 0

# Diverges — emit a deny decision naming the role, what its definition sets,
# and what the caller must do (verbatim shape from block-claude-memory-writes.sh).
DENIAL_MSG="BLOCKED: role $role's definition (plugins/agent-teams/agents/$role.md) sets model: $def_model. Re-issue the identical Agent call with the model argument removed."

printf '%s' "$payload" | jq -n \
  --arg msg "$DENIAL_MSG" \
  '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":$msg}}'

exit 0
