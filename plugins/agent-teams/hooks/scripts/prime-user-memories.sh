#!/usr/bin/env sh
# SessionStart prime hook for agent-teams.
# Runs `ateam prime` to inject cross-project user preferences into the session.
# Silent no-op if ateam is not installed. Never fails the session.
set -eu
ATEAM="${CLAUDE_PLUGIN_ROOT:-}/bin/ateam"
[ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -x "$ATEAM" ] || exit 0
"$ATEAM" prime || true
exit 0
