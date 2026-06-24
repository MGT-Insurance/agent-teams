#!/usr/bin/env sh
# SessionStart hook for agent-teams.
# Best-effort remote pull so DRIs read fresh learnings+initiatives, not stale local Dolt.
# Pull must go through ateam/bd: bd's flock on .beads/embeddeddolt/.lock serializes
# parallel subagent pulls; shelling 'dolt' directly would bypass it and hit the manifest race.
# Never fails — a pull failure degrades to local read, which is always correct.
set -eu
ATEAM="${CLAUDE_PLUGIN_ROOT:-}/bin/ateam"
[ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -x "$ATEAM" ] || exit 0

"$ATEAM" pull || true
exit 0
