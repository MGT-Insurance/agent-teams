#!/usr/bin/env sh
# Persist plugin userConfig options that Bash-invoked `ateam` launches depend on.
#
# WHY: Claude Code exports CLAUDE_PLUGIN_OPTION_<KEY> only to hook and MCP/LSP
# subprocesses — NOT to arbitrary Bash tool calls the model makes. `ateam
# dispatch` / `resume` (invoked as Bash calls from /dispatch-dri and friends)
# read CLAUDE_PLUGIN_OPTION_USE_ADVISORS and CLAUDE_PLUGIN_OPTION_DRI_MODEL to
# decide whether a DRI session launches sonnet+opus-advisor vs the default
# opus, and which model fills the "strong model" slot (advisor model when
# advisors are on, the DRI session's own model when advisors are off).
# CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW carries the --autocompact value for
# those same background sessions; empty means "send nothing", leaving the
# launch's argv byte-identical to today. Without this hook none of these vars
# are present at dispatch time, so advisor mode, dri_model, and the
# auto-compact window would silently never take effect.
#
# This runs at SessionStart (a hook process, which receives
# CLAUDE_PLUGIN_OPTION_USE_ADVISORS, CLAUDE_PLUGIN_OPTION_DRI_MODEL, and
# CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW in its environment — when each
# option is set — and the CLAUDE_ENV_FILE path) and appends export lines to
# $CLAUDE_ENV_FILE, which the harness then applies to every subsequent Bash
# tool call in the session.
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
# Keep this default byte-identical to driDefaultModel in
# internal/verbs/dispatch.go — the two are one setting in two layers, and
# tests/hook-export-plugin-options.test.sh fails if they drift. No [1m] suffix:
# the "opus" alias already resolves to a native 1M-context model.
dri_model="${CLAUDE_PLUGIN_OPTION_DRI_MODEL:-opus}"
# Empty default is deliberate: an empty export is what the Go side reads as
# "unset" (omit --autocompact entirely). No numeric parsing or range checking
# here — the value passes through verbatim; Claude Code's own --autocompact
# flag validates it.
auto_compact_window="${CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW:-}"

printf 'export CLAUDE_PLUGIN_OPTION_USE_ADVISORS=%s\n' "$use_advisors" >> "$CLAUDE_ENV_FILE"
printf 'export CLAUDE_PLUGIN_OPTION_DRI_MODEL=%s\n' "$dri_model" >> "$CLAUDE_ENV_FILE"
printf 'export CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW=%s\n' "$auto_compact_window" >> "$CLAUDE_ENV_FILE"
