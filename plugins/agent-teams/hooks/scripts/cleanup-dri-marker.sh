#!/usr/bin/env bash
# SessionEnd hook for agent-teams: remove the DRI session marker
# (agent-teams-7ew5.2.5 — cleanup leg of the durable learnings+ledger
# re-injection mechanism, agent-teams-7ew5.2.1). Wired under a brand-new
# top-level "SessionEnd" hooks.json key (agent-teams-7ew5.2.6) — fires
# unconditionally for every session end.
#
# ONLY job: remove $ATH/dri-sessions/<session_id> for the ending session_id,
# if it exists. Never touches .steward-session (StewardSessionMarkerPath) —
# that marker's lifecycle belongs exclusively to `ateam steward init`/
# `ateam steward remove`. Does not resolve_session_role or check
# is_steward_cwd at all — a steward session's SessionEnd firing here is
# simply a no-op (no dri-sessions marker exists for it). Removal is
# idempotent: a missing marker is not an error.
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-${HOME:-}/.agent-teams}"

# Capture stdin once non-blocking — Claude Code passes {session_id, ...} on stdin;
# direct invocations have no stdin.
HOOK_STDIN=$(cat 2>/dev/null || true)
HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
export HOOK_SESSION_ID

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$(dirname "$0")/lib/hook-debug-log.sh"

# Log start BEFORE any guard check.
hook_log_start "cleanup-dri-marker.sh"

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/resolve-session-role.sh
. "$(dirname "$0")/lib/resolve-session-role.sh"

dri_cleanup_session_marker "$ATH" "$HOOK_SESSION_ID"

HOOK_EXIT_REASON="ok"
