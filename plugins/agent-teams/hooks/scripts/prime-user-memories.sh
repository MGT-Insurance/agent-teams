#!/usr/bin/env sh
# SessionStart prime hook for agent-teams.
# Runs `ateam prime` to inject cross-project user preferences into the session.
# Silent no-op if ateam is not installed. Never fails the session.
set -eu
command -v ateam >/dev/null 2>&1 || exit 0
ateam prime || true
exit 0
