#!/usr/bin/env sh
# Persist plugin userConfig options that Bash-invoked `ateam` launches depend on.
#
# WHY: Claude Code exports CLAUDE_PLUGIN_OPTION_<KEY> only to hook and MCP/LSP
# subprocesses — NOT to arbitrary Bash tool calls the model makes. `ateam
# dispatch` / `resume` (invoked as Bash calls from /dispatch-dri and friends)
# read CLAUDE_PLUGIN_OPTION_USE_ADVISORS to decide whether a DRI session
# launches sonnet+opus-advisor vs the default opus. Without this hook that var
# is never present at dispatch time, so advisor mode would silently never fire.
#
# This runs at SessionStart (a hook process, which receives both
# CLAUDE_PLUGIN_OPTION_USE_ADVISORS in its environment — when the option is set —
# and the CLAUDE_ENV_FILE path) and appends an export line to $CLAUDE_ENV_FILE,
# which the harness then applies to every subsequent Bash tool call in the
# session.
#
# We read the value from the CLAUDE_PLUGIN_OPTION_USE_ADVISORS env var, NOT from
# an interpolated ${user_config.use_advisors} hook arg: Claude Code rejects
# ${user_config.*} in a shell-form command (2.1.207+), and interpolating it as
# an exec-form arg hard-errors at SessionStart when the option is unset (the
# plugin.json default:false is not applied during arg interpolation). Reading
# the env var sidesteps both — unset simply falls back to "false" here. Only the
# exact string "true" enables advisor mode downstream (see driAdvisorSettings in
# internal/verbs/dispatch.go).
set -eu

# No env file to write to (older harness / unsupported): no-op, never break the
# session.
[ -n "${CLAUDE_ENV_FILE:-}" ] || exit 0

use_advisors="${CLAUDE_PLUGIN_OPTION_USE_ADVISORS:-false}"

printf 'export CLAUDE_PLUGIN_OPTION_USE_ADVISORS=%s\n' "$use_advisors" >> "$CLAUDE_ENV_FILE"
