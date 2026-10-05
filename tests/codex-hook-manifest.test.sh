#!/usr/bin/env bash
# Regression coverage for the Codex hook surface: the SessionStart adapter plus
# the learnings-gate PreToolUse, PostCompact and SessionEnd bindings.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MANIFEST="$ROOT/plugins/agent-teams-codex/hooks/hooks.json"

fail() {
  printf 'FAIL %s\n' "$1" >&2
  exit 1
}

command -v jq >/dev/null 2>&1 || fail "jq is required to parse $MANIFEST"
jq -e . "$MANIFEST" >/dev/null || fail "$MANIFEST is not valid JSON"

actual_keys="$(jq -c '.hooks | keys' "$MANIFEST")"
[ "$actual_keys" = '["PostCompact","PreToolUse","SessionEnd","SessionStart"]' ] ||
  fail "hook event keys = $actual_keys, want [\"PostCompact\",\"PreToolUse\",\"SessionEnd\",\"SessionStart\"]"

group_count="$(jq -r '.hooks.SessionStart | length' "$MANIFEST")"
[ "$group_count" = '1' ] || fail "SessionStart group count = $group_count, want 1"

matcher="$(jq -r '.hooks.SessionStart[0].matcher' "$MANIFEST")"
[ "$matcher" = 'startup|resume|clear|compact' ] ||
  fail "SessionStart matcher = $matcher, want startup|resume|clear|compact"

actual_commands="$(jq -c '.hooks.SessionStart[0].hooks | map(.command)' "$MANIFEST")"
expected_commands='["\"${PLUGIN_ROOT}/hooks/ensure-ateam-link.sh\"","\"${PLUGIN_ROOT}/bin/ateam\" codex-hook session-start"]'
[ "$actual_commands" = "$expected_commands" ] ||
  fail "SessionStart commands = $actual_commands, want $expected_commands"

# Learnings gate bindings: one group, one command each, timeout 5.
check_gate() {
  event="$1" want_matcher="$2" want_arg="$3"
  groups="$(jq -r ".hooks.$event | length" "$MANIFEST")"
  [ "$groups" = '1' ] || fail "$event group count = $groups, want 1"
  got_matcher="$(jq -r ".hooks.$event[0].matcher // \"<none>\"" "$MANIFEST")"
  [ "$got_matcher" = "$want_matcher" ] ||
    fail "$event matcher = $got_matcher, want $want_matcher"
  got_cmds="$(jq -c ".hooks.$event[0].hooks | map(.command)" "$MANIFEST")"
  want_cmds='["\"${PLUGIN_ROOT}/bin/ateam\" codex-hook '"$want_arg"'"]'
  [ "$got_cmds" = "$want_cmds" ] || fail "$event commands = $got_cmds, want $want_cmds"
  got_timeout="$(jq -r ".hooks.$event[0].hooks[0].timeout" "$MANIFEST")"
  [ "$got_timeout" = '5' ] || fail "$event timeout = $got_timeout, want 5"
}

check_gate PreToolUse '.*' pre-tool-use
check_gate PostCompact '<none>' post-compact
check_gate SessionEnd '<none>' session-end

echo 'PASS Codex hook manifest has the SessionStart adapter and the learnings-gate bindings'
