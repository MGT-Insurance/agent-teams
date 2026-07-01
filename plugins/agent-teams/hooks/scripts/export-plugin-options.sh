#!/usr/bin/env sh
# Persist plugin userConfig options that Bash-invoked `ateam` launches depend on.
#
# WHY: Claude Code exports CLAUDE_PLUGIN_OPTION_<KEY> only to hook and MCP/LSP
# subprocesses — NOT to arbitrary Bash tool calls the model makes. `ateam
# dispatch` / `resume` (invoked as Bash calls from /dri-dispatch and friends)
# read CLAUDE_PLUGIN_OPTION_USE_ADVISORS to decide whether a DRI session
# launches sonnet+opus-advisor vs the default opus. Without this hook that var
# is never present at dispatch time, so advisor mode would silently never fire.
#
# This runs at SessionStart (a hook process, which DOES receive both the
# interpolated ${user_config.*} value as $1 and the CLAUDE_ENV_FILE path) and
# appends an export line to $CLAUDE_ENV_FILE, which the harness then applies to
# every subsequent Bash tool call in the session.
#
# Arg $1 is the interpolated ${user_config.use_advisors} value ("true"/"false",
# possibly empty if unset). Only the exact string "true" enables advisor mode
# downstream (see driAdvisorSettings in internal/verbs/dispatch.go).
set -eu

# No env file to write to (older harness / unsupported): no-op, never break the
# session.
[ -n "${CLAUDE_ENV_FILE:-}" ] || exit 0

use_advisors="${1:-false}"

printf 'export CLAUDE_PLUGIN_OPTION_USE_ADVISORS=%s\n' "$use_advisors" >> "$CLAUDE_ENV_FILE"
