#!/usr/bin/env bash
# UserPromptSubmit hook for agent-teams: per-turn role-learnings re-injection
# (agent-teams-7ew5.2.3 — the structural-survival leg of the durable
# learnings+ledger re-injection mechanism, agent-teams-7ew5.2.1).
#
# Fires on every user prompt. Resolves this session's role (dri/steward/none)
# via the shared lib/resolve-session-role.sh, and if a role resolves, emits
# `ateam learnings <role>` as additionalContext — this re-lands on the FIRST
# prompt after ANY compaction with zero special-casing, exactly like
# inbox-drain.sh's mail signal does today.
#
# Deliberately kept LIGHT: learnings only, no ledger content (that belongs to
# the heavier, less-frequent SessionStart-compact leg,
# role-recall-recovery.sh). Silent no-op for any session with no resolved
# role (teammate subagents, ad-hoc human sessions, /review-pr sessions, etc).
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-$HOME/.agent-teams}"
ATEAM="${CLAUDE_PLUGIN_ROOT:-}/bin/ateam"

# Capture stdin once non-blocking — Claude Code passes {session_id, ...} on stdin;
# direct invocations have no stdin. Must not break set -euo pipefail when empty.
HOOK_STDIN=$(cat 2>/dev/null || true)
HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
export HOOK_SESSION_ID

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$(dirname "$0")/lib/hook-debug-log.sh"

# Log start BEFORE any guard check.
hook_log_start "prime-role-learnings.sh"

command -v bd >/dev/null 2>&1 || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
command -v jq >/dev/null 2>&1 || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
{ [ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -x "$ATEAM" ]; } || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
[ -d "$ATH/.beads" ] || { HOOK_EXIT_REASON="missing-deps"; exit 0; }

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/resolve-steward.sh
. "$(dirname "$0")/lib/resolve-steward.sh"
# shellcheck source=plugins/agent-teams/hooks/scripts/lib/resolve-session-role.sh
. "$(dirname "$0")/lib/resolve-session-role.sh"

role=$(resolve_session_role "$ATH" "$HOOK_SESSION_ID")
if [ -z "$role" ]; then
  HOOK_EXIT_REASON="no-role"
  exit 0
fi

hook_log_note "note" "role-resolved role=${role}"

payload=$("$ATEAM" learnings "$role" 2>/dev/null || true)

jq -n --arg ctx "$payload" '{"additionalContext": $ctx}'

HOOK_EXIT_REASON="ok"
