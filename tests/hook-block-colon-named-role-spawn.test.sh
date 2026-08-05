#!/usr/bin/env bash
# Tests for the block-colon-named-role-spawn PreToolUse hook script.
#
# This hook exists because the old failure mode is SILENT: a NAMED spawn of
# an unknown subagent_type launches as a generic agent with full tools and
# no error at all (see agent-teams-wf7o.9 artifact (5)). A test that only
# checks the script runs without crashing would stay green through exactly
# that bug — every assertion below checks the actual deny/allow decision.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/block-colon-named-role-spawn.sh"
HOOKS_JSON="$ROOT/plugins/agent-teams/hooks/hooks.json"

# make_payload builds a PreToolUse Agent-tool payload. name_json is a JSON
# snippet for tool_input.name (e.g. '"planner-x"'); pass OMIT to leave the
# key out entirely. extra_top is raw JSON fragment(s) to splice at the top
# level of the payload (e.g. an agent_id, to prove there is no depth
# carve-out) — pass "" for none.
make_payload() {
  local subagent_type="$1" name_json="$2" tool_name="${3:-Agent}" extra_top="${4:-}"
  local name_field=""
  if [ "$name_json" != "OMIT" ]; then
    name_field=",\"name\":${name_json}"
  fi
  if [ -n "$extra_top" ]; then
    printf '{"tool_name":"%s",%s"tool_input":{"subagent_type":"%s"%s}}' \
      "$tool_name" "${extra_top}," "$subagent_type" "$name_field"
  else
    printf '{"tool_name":"%s","tool_input":{"subagent_type":"%s"%s}}' \
      "$tool_name" "$subagent_type" "$name_field"
  fi
}

run() { printf '%s' "$1" | "$SCRIPT"; }

# Capture stdout+status without tripping this script's own `set -e` — see
# hook-block-model-divergence.test.sh's `call` for why the &&/|| form matters.
call() {
  local payload="$1"
  out=$(run "$payload") && status=0 || status=$?
}

assert_deny() {
  local label="$1" payload="$2"; shift 2
  call "$payload"
  [ "$status" -eq 0 ] \
    || { echo "FAIL $label: expected exit 0, got $status (output: $out)"; exit 1; }
  echo "$out" | grep -q '"deny"' \
    || { echo "FAIL $label: expected deny, got: $out"; exit 1; }
  for needle in "$@"; do
    echo "$out" | grep -qF "$needle" \
      || { echo "FAIL $label: denial message missing '$needle', got: $out"; exit 1; }
  done
}

assert_allow() {
  local label="$1" payload="$2"
  call "$payload"
  [ "$status" -eq 0 ] \
    || { echo "FAIL $label: expected exit 0, got $status (output: $out)"; exit 1; }
  [ -z "$out" ] \
    || { echo "FAIL $label: expected silent allow, got: $out"; exit 1; }
}

# ---- Deny cases ---------------------------------------------------------

# Case 1: "agent-teams:planner" + name -> DENIED with the wf7o.9 (6) message,
# substituted to "planner". This is the load-bearing case: a named colon-key
# spawn used to succeed silently as a generic agent with no error at all.
assert_deny "case1-colon-planner-named" \
  "$(make_payload "agent-teams:planner" '"planner-x"')" \
  "BLOCKED: agent-teams:planner no longer exists" \
  "agent-teams-planner" \
  "anthropics/claude-code#81746" \
  "roles/README.md"

# Case 2: other colon roles substitute correctly too.
assert_deny "case2-colon-implementer-named" \
  "$(make_payload "agent-teams:implementer" '"impl-x"')" \
  "BLOCKED: agent-teams:implementer no longer exists" \
  "agent-teams-implementer"

# Case 3 (NO DEPTH CARVE-OUT, PROVEN): the denied payload plus a top-level
# agent_id is STILL DENIED. A test asserting it passes would be a bug — an
# earlier contract froze a depth/agent_id carve-out and it was falsified by
# live test.
assert_deny "case3-no-depth-carveout" \
  "$(make_payload "agent-teams:planner" '"planner-x"' "Agent" '"agent_id":"a1b2c3"')" \
  "BLOCKED: agent-teams:planner no longer exists"

# ---- Allow cases ---------------------------------------------------------

# Case 4: "agent-teams-planner" (hyphen key) + name -> PASSES silently.
# Load-bearing: this is the call the entire initiative exists to make work.
assert_allow "case4-hyphen-planner-named" \
  "$(make_payload "agent-teams-planner" '"planner-x"')"

# Case 5: "agent-teams:planner" with NO name -> PASSES silently. The
# harness's own unnamed-spawn registry check rejects it with a useful
# message; this hook must not double-handle that path.
assert_allow "case5-colon-planner-unnamed" \
  "$(make_payload "agent-teams:planner" OMIT)"

# Case 6: named non-role spawns are legitimate and common -> PASS.
for t in fork general-purpose Explore claude-code-guide; do
  assert_allow "case6-named-non-role-$t" \
    "$(make_payload "$t" '"some-name"')"
done

# Case 7: non-Agent tool -> out of scope, allow.
assert_allow "case7-non-agent-tool" \
  "$(make_payload "agent-teams:planner" '"planner-x"' "Write")"

# Case 8: malformed JSON -> silent no-op, allow.
assert_allow "case8-malformed-json" "not json"

# Case 9: tool_input missing entirely -> allow.
assert_allow "case9-missing-tool-input" '{"tool_name":"Agent"}'

# Case 10: name present but empty string -> treated as absent -> allow.
assert_allow "case10-empty-name" \
  "$(make_payload "agent-teams:planner" '""')"

# Case 11: missing jq -> exit 0, no stdout. Never break a spawn on hook error.
PATH_NO_JQ=$(mktemp -d)
trap 'rm -rf "$PATH_NO_JQ"' EXIT
for tool in bash cat printf mkdir rm; do
  real=$(command -v "$tool" 2>/dev/null || true)
  [ -n "$real" ] && ln -sf "$real" "$PATH_NO_JQ/$tool"
done
out=$(PATH="$PATH_NO_JQ" bash -c "printf '%s' '$(make_payload "agent-teams:planner" '"planner-x"')' | \"$SCRIPT\"" 2>/dev/null) && status=0 || status=$?
[ "$status" -eq 0 ] \
  || { echo "FAIL case11-missing-jq: expected exit 0, got $status"; exit 1; }
[ -z "$out" ] \
  || { echo "FAIL case11-missing-jq: expected silent allow, got: $out"; exit 1; }
trap - EXIT
rm -rf "$PATH_NO_JQ"

# ---- Wiring assertion -----------------------------------------------------
# A script-only test stays green when the hook is never wired up — precisely
# the silent-failure shape this initiative exists to eliminate. Assert the
# script is registered in hooks.json under a PreToolUse matcher matching the
# Agent tool.
command -v jq >/dev/null 2>&1 || { echo "FAIL wiring: jq not available to verify hooks.json"; exit 1; }
matched=$(jq -r '
  .hooks.PreToolUse[]
  | select(.matcher == "Agent")
  | .hooks[]
  | select(.command | test("block-colon-named-role-spawn\\.sh$"))
  | .command
' "$HOOKS_JSON")
[ -n "$matched" ] \
  || { echo "FAIL wiring: block-colon-named-role-spawn.sh not registered under a PreToolUse \"Agent\" matcher in $HOOKS_JSON"; exit 1; }

echo "PASS"
