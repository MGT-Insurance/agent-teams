#!/usr/bin/env bash
# Regression coverage for the single SessionStart Codex hook surface.
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
[ "$actual_keys" = '["SessionStart"]' ] ||
  fail "hook event keys = $actual_keys, want [\"SessionStart\"]"

group_count="$(jq -r '.hooks.SessionStart | length' "$MANIFEST")"
[ "$group_count" = '1' ] || fail "SessionStart group count = $group_count, want 1"

matcher="$(jq -r '.hooks.SessionStart[0].matcher' "$MANIFEST")"
[ "$matcher" = 'startup|resume|clear|compact' ] ||
  fail "SessionStart matcher = $matcher, want startup|resume|clear|compact"

actual_commands="$(jq -c '.hooks.SessionStart[0].hooks | map(.command)' "$MANIFEST")"
expected_commands='["\"${PLUGIN_ROOT}/hooks/ensure-ateam-link.sh\"","\"${PLUGIN_ROOT}/bin/ateam\" codex-hook session-start"]'
[ "$actual_commands" = "$expected_commands" ] ||
  fail "SessionStart commands = $actual_commands, want $expected_commands"

echo 'PASS Codex hook manifest has only the SessionStart binding and catch-up surface'
