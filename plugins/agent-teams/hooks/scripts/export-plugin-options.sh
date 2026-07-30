#!/usr/bin/env sh
# Persist plugin userConfig options that Bash-invoked `ateam` launches depend on.
#
# WHY: Claude Code exports CLAUDE_PLUGIN_OPTION_<KEY> only to hook and MCP/LSP
# subprocesses — NOT to arbitrary Bash tool calls the model makes. `ateam
# dispatch` / `resume` (invoked as Bash calls from /dispatch-dri and friends)
# read CLAUDE_PLUGIN_OPTION_USE_ADVISORS and CLAUDE_PLUGIN_OPTION_DRI_MODEL to
# decide whether a DRI session launches sonnet+opus-advisor vs the default
# opus[1m], and which model fills the "strong model" slot (advisor model when
# advisors are on, the DRI session's own model when advisors are off).
# Without this hook those vars are never present at dispatch time, so advisor
# mode and dri_model would silently never take effect.
#
# This runs at SessionStart (a hook process, which receives
# CLAUDE_PLUGIN_OPTION_USE_ADVISORS and CLAUDE_PLUGIN_OPTION_DRI_MODEL in its
# environment — when each option is set — and the CLAUDE_ENV_FILE path) and
# appends export lines to $CLAUDE_ENV_FILE, which the harness then applies to
# every subsequent Bash tool call in the session.
#
# We read the values from the CLAUDE_PLUGIN_OPTION_* env vars, NOT from
# interpolated ${user_config.*} hook args: Claude Code rejects
# ${user_config.*} in a shell-form command (2.1.207+), and interpolating it as
# an exec-form arg hard-errors at SessionStart when the option is unset (the
# plugin.json defaults are not applied during arg interpolation). Reading
# the env vars sidesteps both — unset simply falls back to the documented
# default here. Only the exact string "true" enables advisor mode downstream
# (see driAdvisorSettings in internal/verbs/dispatch.go).
set -eu

# No env file to write to (older harness / unsupported): no-op, never break the
# session.
[ -n "${CLAUDE_ENV_FILE:-}" ] || exit 0

use_advisors="${CLAUDE_PLUGIN_OPTION_USE_ADVISORS:-false}"
# The [1m] suffix is load-bearing, not cosmetic, but it does NOT set the
# compaction threshold on its own. The CLI clamps its auto-compact window to
# min(the model's real context window, the requested window), and dispatch.go
# requests 500000 via --settings. On a sub-1M model that clamp drops the
# window straight back to 200000 and the session compacts at ~167000 tokens,
# whatever was requested. So this default and autoCompactWindowTokens in
# internal/verbs/dispatch.go are ONE setting in two layers: reverting either
# one alone silently un-does both. Keep them in step, and see that constant's
# doc comment for the full resolution formula.
# Kept unquoted in the printf below on purpose: a POSIX assignment RHS does
# not undergo pathname expansion, so the brackets survive verbatim, and
# quoting would break a naive KEY=VALUE reader of $CLAUDE_ENV_FILE.
dri_model="${CLAUDE_PLUGIN_OPTION_DRI_MODEL:-opus[1m]}"

printf 'export CLAUDE_PLUGIN_OPTION_USE_ADVISORS=%s\n' "$use_advisors" >> "$CLAUDE_ENV_FILE"
printf 'export CLAUDE_PLUGIN_OPTION_DRI_MODEL=%s\n' "$dri_model" >> "$CLAUDE_ENV_FILE"
