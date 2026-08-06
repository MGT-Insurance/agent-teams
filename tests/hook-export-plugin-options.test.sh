#!/usr/bin/env bash
# Tests for the export-plugin-options SessionStart hook script.
#
# This hook is the ONLY path by which the dri_model default reaches `ateam
# dispatch` (see the script's own WHY comment), so its env-file round trip is
# load-bearing and was previously verified only by hand.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/export-plugin-options.sh"
DISPATCH_GO="$ROOT/internal/verbs/dispatch.go"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/export-plugin-options.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

# run_hook <env-file-path> [env-args ...]
# Invokes the hook with CLAUDE_ENV_FILE set, passing any extra env args
# through. The extra args go BEFORE the CLAUDE_ENV_FILE assignment because BSD
# env stops parsing options at the first non-option argument, so a trailing
# `-u VAR` would be handed to the command instead. Runs with cwd inside $WORK
# so nothing in the repo can influence the result.
run_hook() {
  local envfile="$1"; shift
  ( cd "$WORK" && env "$@" CLAUDE_ENV_FILE="$envfile" sh "$SCRIPT" )
}

# sourced_value <env-file-path> <var-name>
# Sources the emitted env file in a subshell and prints one variable, so the
# assertions test what a Bash tool call actually receives rather than the raw
# file text.
sourced_value() {
  ( set +u; . "$1"; eval "printf '%s' \"\${$2:-}\"" )
}

fail() { echo "FAIL $1"; exit 1; }

# --- Case 1: no CLAUDE_ENV_FILE is a silent no-op, never a broken session ---
( cd "$WORK" && env -u CLAUDE_ENV_FILE sh "$SCRIPT" ) \
  || fail "no-env-file: hook must exit 0 when CLAUDE_ENV_FILE is unset"

# --- Case 2: unset options fall back to the documented defaults -------------
EF="$WORK/unset.env"
run_hook "$EF" -u CLAUDE_PLUGIN_OPTION_DRI_MODEL -u CLAUDE_PLUGIN_OPTION_USE_ADVISORS -u CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW
got="$(sourced_value "$EF" CLAUDE_PLUGIN_OPTION_DRI_MODEL)"
[ "$got" = "opus" ] \
  || fail "unset-default: sourced dri_model = '$got', want 'opus'"
got="$(sourced_value "$EF" CLAUDE_PLUGIN_OPTION_USE_ADVISORS)"
[ "$got" = "false" ] \
  || fail "unset-default: sourced use_advisors = '$got', want 'false'"
got="$(sourced_value "$EF" CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW)"
[ "$got" = "" ] \
  || fail "unset-default: sourced auto_compact_window = '$got', want '' (empty = send nothing downstream)"

# --- Case 3: an explicitly set option wins over the default -----------------
EF="$WORK/override.env"
run_hook "$EF" CLAUDE_PLUGIN_OPTION_DRI_MODEL=sonnet CLAUDE_PLUGIN_OPTION_USE_ADVISORS=true CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW=500k
got="$(sourced_value "$EF" CLAUDE_PLUGIN_OPTION_DRI_MODEL)"
[ "$got" = "sonnet" ] \
  || fail "override: sourced dri_model = '$got', want 'sonnet'"
got="$(sourced_value "$EF" CLAUDE_PLUGIN_OPTION_USE_ADVISORS)"
[ "$got" = "true" ] \
  || fail "override: sourced use_advisors = '$got', want 'true'"
got="$(sourced_value "$EF" CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW)"
[ "$got" = "500k" ] \
  || fail "override: sourced auto_compact_window = '$got', want '500k'"

# --- Case 4: the hook default and the Go default must not drift -------------
# Two layers hold this same default: this hook (the value `ateam dispatch`
# actually reads on the /dispatch-dri path) and driDefaultModel in
# dispatch.go (the fallback used from cron, direct CLI, and
# verify-live-settings.sh). They diverged once already; this pins them.
hook_default="$(sed -n 's/^dri_model="\${CLAUDE_PLUGIN_OPTION_DRI_MODEL:-\(.*\)}"$/\1/p' "$SCRIPT")"
[ -n "$hook_default" ] \
  || fail "drift: could not parse the dri_model default out of $SCRIPT"
go_default="$(sed -n 's/^const driDefaultModel = "\(.*\)"$/\1/p' "$DISPATCH_GO")"
[ -n "$go_default" ] \
  || fail "drift: could not parse driDefaultModel out of $DISPATCH_GO"
[ "$hook_default" = "$go_default" ] \
  || fail "drift: hook default '$hook_default' != driDefaultModel '$go_default' — these are one setting in two layers; change both or neither"

echo "PASS hook-export-plugin-options ($hook_default in both layers)"
