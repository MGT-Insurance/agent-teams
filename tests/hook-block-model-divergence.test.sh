#!/usr/bin/env bash
# Tests for the block-model-divergence PreToolUse hook script.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/block-model-divergence.sh"

# model_json is a JSON snippet for the "model" field's value (e.g. '"opus"',
# 'null'). Pass the sentinel OMIT to leave the field out of tool_input
# entirely — distinct from an explicit JSON null.
make_payload() {
  local subagent_type="$1" model_json="$2" tool_name="${3:-Agent}"
  if [ "$model_json" = "OMIT" ]; then
    printf '{"tool_name":"%s","tool_input":{"subagent_type":"%s"}}' "$tool_name" "$subagent_type"
  else
    printf '{"tool_name":"%s","tool_input":{"subagent_type":"%s","model":%s}}' \
      "$tool_name" "$subagent_type" "$model_json"
  fi
}

run() { printf '%s' "$1" | "$SCRIPT"; }

# Helper: assert deny — output must contain "deny" and a given substring
# (grep basic-regex) expected in the denial message.
assert_deny() {
  local label="$1" payload="$2" msg_pattern="$3"
  out=$(run "$payload")
  echo "$out" | grep -q '"deny"' \
    || { echo "FAIL $label: expected deny, got: $out"; exit 1; }
  echo "$out" | grep -q "$msg_pattern" \
    || { echo "FAIL $label: denial message missing '$msg_pattern', got: $out"; exit 1; }
}

# Helper: assert allow — output must be empty (silent pass-through).
assert_allow() {
  local label="$1" payload="$2"
  out=$(run "$payload")
  [ -z "$out" ] \
    || { echo "FAIL $label: expected silent allow, got: $out"; exit 1; }
}

# ---- Allow cases -------------------------------------------------------

# Case 1: no model key in tool_input -> allow.
assert_allow "case1-no-model" \
  "$(make_payload "agent-teams:implementer" OMIT)"

# Case 1b: model explicitly null (the shape a caller's retry actually sends,
# per ~/.agent-teams/model-override-evidence/attempts.txt) -> allow.
assert_allow "case1b-null-model" \
  "$(make_payload "agent-teams:implementer" 'null')"

# Case 2: passed model matches the role's definition -> allow.
assert_allow "case2-matching-model" \
  "$(make_payload "agent-teams:implementer" '"sonnet"')"

# Case 3: general-purpose with a divergent model -> out of scope, allow.
assert_allow "case3-general-purpose-out-of-scope" \
  "$(make_payload "general-purpose" '"opus"')"

# Case 4: unknown agent-teams role -> definition file missing, allow.
assert_allow "case4-unknown-role" \
  "$(make_payload "agent-teams:nosuchrole" '"opus"')"

# Case 5: malformed JSON -> silent no-op, allow.
out=$(printf 'not json' | "$SCRIPT")
[ -z "$out" ] || { echo "FAIL case5-malformed-json: expected silent allow, got: $out"; exit 1; }

# Case 6: tool_input missing entirely -> allow.
out=$(printf '{"tool_name":"Agent"}' | "$SCRIPT")
[ -z "$out" ] || { echo "FAIL case6-missing-tool-input: expected silent allow, got: $out"; exit 1; }

# Case 7: non-Agent tool -> out of scope, allow (even with a divergent model shape).
assert_allow "case7-non-agent-tool" \
  "$(make_payload "agent-teams:implementer" '"opus"' "Write")"

# ---- Reject cases -------------------------------------------------------

# Case 8: implementer (definition: sonnet) spawned with opus -> reject.
assert_deny "case8-implementer-opus" \
  "$(make_payload "agent-teams:implementer" '"opus"')" \
  "implementer.*model: sonnet"

# Case 9: planner (definition: opus) spawned with sonnet -> reject.
assert_deny "case9-planner-sonnet" \
  "$(make_payload "agent-teams:planner" '"sonnet"')" \
  "planner.*model: opus"

# Case 10: reviewer (definition: sonnet) spawned with opus[1m] -> reject.
assert_deny "case10-reviewer-opus-1m" \
  "$(make_payload "agent-teams:reviewer" '"opus[1m]"')" \
  "reviewer.*model: sonnet"

echo "PASS"
