#!/usr/bin/env bash
# SessionStart(source=compact) recovery for agent-teams.
# If cwd is the worktree of a registered OPEN initiative in the global
# workspace, re-inject that initiative's context. Silent no-op otherwise —
# never broadcasts machine-wide state (teammate sessions also fire hooks).
set -euo pipefail
ATH="${AGENT_TEAMS_HOME:-$HOME/.agent-teams}"
command -v bd >/dev/null 2>&1 || exit 0
command -v jq >/dev/null 2>&1 || exit 0
[ -d "$ATH/.beads" ] || exit 0

match_id=$(bd -C "$ATH" list --status=open --json 2>/dev/null \
  | jq -r --arg wt "worktree: $PWD" \
      '[.[] | select((.description // "") | split("\n") | any(. == $wt))][0].id // empty')
[ -n "$match_id" ] || exit 0

echo "## agent-teams: initiative context (post-compaction recovery)"
bd -C "$ATH" show "$match_id" 2>/dev/null || true
cat <<'EOF'

This session is the DRI for the initiative above. The /dri skill governs it —
re-read the dri skill if its guidance is no longer in context. Recover working
state from: this initiative's notes, `bd human list` in the global workspace
(parked gates), and the project repo's beads (plan, discovery beads).
EOF
