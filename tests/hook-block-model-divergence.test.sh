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

# Helper: assert deny — output must contain "deny", a def_pattern substring
# (grep basic-regex) naming the role + the definition's model, and an
# asked_literal substring (fixed string — model values like opus[1m] contain
# BRE metacharacters) naming what the caller actually passed.
assert_deny() {
  local label="$1" payload="$2" def_pattern="$3" asked_literal="$4"
  out=$(run "$payload")
  echo "$out" | grep -q '"deny"' \
    || { echo "FAIL $label: expected deny, got: $out"; exit 1; }
  echo "$out" | grep -q "$def_pattern" \
    || { echo "FAIL $label: denial message missing '$def_pattern', got: $out"; exit 1; }
  echo "$out" | grep -qF "$asked_literal" \
    || { echo "FAIL $label: denial message missing '$asked_literal', got: $out"; exit 1; }
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
  "implementer.*model: sonnet" \
  "asked for opus"

# Case 9: planner (definition: opus) spawned with sonnet -> reject.
assert_deny "case9-planner-sonnet" \
  "$(make_payload "agent-teams:planner" '"sonnet"')" \
  "planner.*model: opus" \
  "asked for sonnet"

# Case 10: reviewer (definition: sonnet) spawned with opus[1m] -> reject.
assert_deny "case10-reviewer-opus-1m" \
  "$(make_payload "agent-teams:reviewer" '"opus[1m]"')" \
  "reviewer.*model: sonnet" \
  "asked for opus[1m]"

# ---- Synthetic fixture: model: inherit and "no model: line" branches ------
# No real agent definition has either shape, so these two allow branches are
# otherwise unwitnessed — a no-op branch on a hook that fires for every Agent
# spawn machine-wide is exactly the risk worth testing directly. Build a temp
# plugin-root layout (hooks/scripts/ + agents/, siblings, mirroring the real
# plugin) and invoke a COPY of the script from there. This also exercises the
# $0-relative resolution from a root that is not the repo — the resolution
# mechanism itself, not just its output on the real repo layout.
FIXTURE=$(mktemp -d)
trap 'rm -rf "$FIXTURE"' EXIT
mkdir -p "$FIXTURE/hooks/scripts" "$FIXTURE/agents"
cp "$SCRIPT" "$FIXTURE/hooks/scripts/block-model-divergence.sh"
chmod +x "$FIXTURE/hooks/scripts/block-model-divergence.sh"

cat > "$FIXTURE/agents/inherit-role.md" <<'EOF'
---
description: fixture role with model: inherit
model: inherit
---
body
EOF

cat > "$FIXTURE/agents/nomodel-role.md" <<'EOF'
---
description: fixture role with no model: line
---
body
EOF

run_fixture() { printf '%s' "$1" | "$FIXTURE/hooks/scripts/block-model-divergence.sh"; }

# Case 11: model: inherit -> allow even with a divergent passed model.
out=$(run_fixture "$(make_payload "agent-teams:inherit-role" '"opus"')")
[ -z "$out" ] || { echo "FAIL case11-inherit-role: expected silent allow, got: $out"; exit 1; }

# Case 12: definition has no model: line -> allow even with a divergent passed model.
out=$(run_fixture "$(make_payload "agent-teams:nomodel-role" '"opus"')")
[ -z "$out" ] || { echo "FAIL case12-nomodel-role: expected silent allow, got: $out"; exit 1; }

echo "PASS"
