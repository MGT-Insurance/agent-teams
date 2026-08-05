#!/usr/bin/env bash
# Tests for the subagent-prime-learnings SubagentStart hook and its wiring
# in hooks.json (agent-teams-wf7o.13).
#
# The bug this closes: the old matcher `(^|:)(implementer|planner|tester|
# reviewer)$` keyed off agent_type, but on a NAMED spawn agent_type is the
# ARBITRARY SPAWN NAME the DRI invented for that call (e.g. "planner-at-qr2i",
# "hookprobe") — never the role. A test that only checks the script behaves
# correctly when invoked stays green through exactly that failure, so the
# static check below asserts the matcher against realistic free-form spawn
# names, not against the script's internals.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/subagent-prime-learnings.sh"
HOOKS_JSON="$ROOT/plugins/agent-teams/hooks/hooks.json"

command -v jq >/dev/null 2>&1 || { echo "SKIP: jq not available"; exit 0; }

fail() { echo "FAIL $1"; exit 1; }

# ---- Static: matcher must catch free-form spawn names ---------------------
# Assert against sample names, not the literal ".*" — this way the test
# still protects the property if someone rewrites the pattern.
matcher=$(jq -r '.hooks.SubagentStart[0].matcher // empty' "$HOOKS_JSON")
[ -n "$matcher" ] || fail "static-matcher-present: no SubagentStart[0].matcher in $HOOKS_JSON"

for sample in "planner-at-qr2i" "impl-a1" "hookprobe" "implementer" "reviewer" "namedplug"; do
  printf '%s' "$sample" | grep -Eq "^(${matcher})\$" \
    || fail "static-matcher-catches-$sample: matcher '$matcher' does not match free-form spawn name '$sample'"
done

# ---- Wiring: subagent-prime-learnings.sh registered under SubagentStart ---
matched=$(jq -r '
  .hooks.SubagentStart[]
  | .hooks[]
  | select(.command | test("subagent-prime-learnings\\.sh$"))
  | .command
' "$HOOKS_JSON")
[ -n "$matched" ] || fail "wiring: subagent-prime-learnings.sh not registered under SubagentStart in $HOOKS_JSON"

# ---- Behavioral fixture setup ----------------------------------------------
FIXTURE=$(mktemp -d)
trap 'rm -rf "$FIXTURE"' EXIT

mkdir -p "$FIXTURE/plugin-root/bin" "$FIXTURE/ath"
PULL_LOG="$FIXTURE/pull.log"
export PULL_LOG
: > "$PULL_LOG"

cat > "$FIXTURE/plugin-root/bin/ateam" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = "pull" ]; then
  echo "pull" >> "$PULL_LOG"
fi
exit 0
EOF
chmod +x "$FIXTURE/plugin-root/bin/ateam"

THROTTLE_FILE="$FIXTURE/ath/.subagent-prime-learnings.last-pull"

payload() {
  printf '{"agent_id":"a%s","agent_type":"%s","cwd":"/tmp","hook_event_name":"SubagentStart","prompt_id":"p1","session_id":"s1","transcript_path":"/tmp/t"}' \
    "$1" "$2"
}

invoke() {
  CLAUDE_PLUGIN_ROOT="$FIXTURE/plugin-root" AGENT_TEAMS_HOME="$FIXTURE/ath" \
    bash -c "printf '%s' '$(payload "$1" "$2")' | \"$SCRIPT\""
}

pull_count() { wc -l < "$PULL_LOG" | tr -d ' '; }

# ---- Throttle proven --------------------------------------------------
# Invocation 1: no throttle file yet -> pulls.
out=$(invoke "1" "planner-at-qr2i") || fail "invoke1: script exited non-zero"
[ -z "$out" ] || fail "invoke1: expected no stdout, got: $out"
[ "$(pull_count)" -eq 1 ] || fail "invoke1: expected 1 pull, got $(pull_count)"

# Invocation 2: immediately after, inside the 60s window -> throttled.
# Together, invocations 1+2 (both "inside the window") produce exactly one pull.
out=$(invoke "2" "impl-a1") || fail "invoke2: script exited non-zero"
[ -z "$out" ] || fail "invoke2: expected no stdout, got: $out"
[ "$(pull_count)" -eq 1 ] || fail "invoke2 (inside window): expected pull count still 1, got $(pull_count)"

# Backdate the throttle file past the window, then invoke once more —
# "outside the window" — which must produce a second pull (total: two).
now=$(date +%s)
echo $((now - 61)) > "$THROTTLE_FILE"
out=$(invoke "3" "hookprobe") || fail "invoke3: script exited non-zero"
[ -z "$out" ] || fail "invoke3: expected no stdout, got: $out"
[ "$(pull_count)" -eq 2 ] || fail "invoke3 (outside window): expected pull count 2, got $(pull_count)"

# ---- Fail-soft: missing ateam (CLAUDE_PLUGIN_ROOT unset) -> exit 0, no pull, no stdout.
: > "$PULL_LOG"
rm -f "$THROTTLE_FILE"
out=$(AGENT_TEAMS_HOME="$FIXTURE/ath" bash -c "printf '%s' '$(payload "4" "planner-x")' | \"$SCRIPT\"") \
  && status=0 || status=$?
[ "$status" -eq 0 ] || fail "missing-ateam: expected exit 0, got $status"
[ -z "$out" ] || fail "missing-ateam: expected no stdout, got: $out"
[ "$(pull_count)" -eq 0 ] || fail "missing-ateam: expected no pull, got $(pull_count)"

# ---- Fail-soft: missing jq -> exit 0, no stdout, never break a spawn.
: > "$PULL_LOG"
NO_JQ_PATH=$(mktemp -d)
for tool in bash cat printf mkdir rm date wc dirname; do
  real=$(command -v "$tool" 2>/dev/null || true)
  [ -n "$real" ] && ln -sf "$real" "$NO_JQ_PATH/$tool"
done
out=$(PATH="$NO_JQ_PATH" CLAUDE_PLUGIN_ROOT="$FIXTURE/plugin-root" AGENT_TEAMS_HOME="$FIXTURE/ath" \
  bash -c "printf '%s' '$(payload "5" "planner-x")' | \"$SCRIPT\"" 2>/dev/null) \
  && status=0 || status=$?
rm -rf "$NO_JQ_PATH"
[ "$status" -eq 0 ] || fail "missing-jq: expected exit 0, got $status"
[ -z "$out" ] || fail "missing-jq: expected no stdout, got: $out"

# ---- Fail-soft: malformed payload -> exit 0, no stdout, never break a spawn.
: > "$PULL_LOG"
rm -f "$THROTTLE_FILE"
out=$(CLAUDE_PLUGIN_ROOT="$FIXTURE/plugin-root" AGENT_TEAMS_HOME="$FIXTURE/ath" \
  bash -c "printf 'not json' | \"$SCRIPT\"") && status=0 || status=$?
[ "$status" -eq 0 ] || fail "malformed-payload: expected exit 0, got $status"
[ -z "$out" ] || fail "malformed-payload: expected no stdout, got: $out"

echo "PASS"
