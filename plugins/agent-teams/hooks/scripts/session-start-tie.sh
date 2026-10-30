#!/usr/bin/env bash
# SessionStart hook for agent-teams: async session-to-initiative tie.
# Fires on startup, resume, clear, and compact, registered async so nothing
# waits on the tie write (about 5 s on hosted beads). `ateam tie-session`
# (agent-teams-zalv.3, at-ps11) resolves the initiative and writes the
# `session:` line; it is a silent no-op for every case that doesn't apply (no
# open initiative for cwd, Steward session, missing session id) and idempotent
# on re-run (respawn re-fires this hook with the same session id). A failed tie
# is retried at the next SessionStart. Fail-soft and silent on stdout: this
# hook must never inject context or wake the model.
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-${HOME:-}/.agent-teams}"
ATEAM="${CLAUDE_PLUGIN_ROOT:-}/bin/ateam"

# Capture stdin once non-blocking — Claude Code passes {session_id, ...} on stdin;
# direct invocations have no stdin.  Must not break set -euo pipefail when empty.
HOOK_STDIN=$(cat 2>/dev/null || true)
HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
export HOOK_SESSION_ID

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$(dirname "$0")/lib/hook-debug-log.sh"

# Log start BEFORE any guard check.
hook_log_start "session-start-tie.sh"

command -v jq    >/dev/null 2>&1 || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
{ [ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -x "$ATEAM" ]; } || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
[ -d "$ATH/.beads" ] || { HOOK_EXIT_REASON="missing-deps"; exit 0; }

if [ -z "$HOOK_SESSION_ID" ] || [ "$HOOK_SESSION_ID" = "unknown" ]; then
  HOOK_EXIT_REASON="no-session-id"
  exit 0
fi

# When the launcher published ATEAM_INITIATIVE (dispatch.go:748-751,
# bgSessionSettingsJSON at :809-814 — reaches a claimed bg-spare via
# --settings even when cmd.Env doesn't), pass it as the positional
# initiative-id so the tie doesn't depend on cwd resolution (agent-teams-
# rjh1.2 — a claimed spare's cwd can lag the worktree at hook time).
# Omit-when-empty expansion preserves cwd resolution for generic (unclaimed)
# spares. Output is captured (not discarded) because the cross-open-initiative
# conflict warning on stderr is deliberately loud (Eric: "error/warn path, not
# a silent second tie") — route any non-empty output through the structured
# hook log.
tie_out=$("$ATEAM" tie-session ${ATEAM_INITIATIVE:+"$ATEAM_INITIATIVE"} --session-id "$HOOK_SESSION_ID" 2>&1) || true
if [ -n "$tie_out" ]; then
  hook_log_note "note" "tie-session: ${tie_out}"
fi

if [ "$HOOK_EXIT_REASON" = "unexpected" ]; then
  HOOK_EXIT_REASON="ok"
fi
