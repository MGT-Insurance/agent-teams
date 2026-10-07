#!/usr/bin/env bash
# SessionStart(matcher=clear|compact) hook for agent-teams: role learnings +
# steward ledger context on context-wiping session boundaries
# (agent-teams-7ew5.2.4; narrowed from compact-only to clear|compact by
# agent-teams-7ew5.2.8). /clear and /compact are the only two SessionStart
# reasons that BOTH wipe learnings from context AND don't re-run the
# skill prose that would otherwise reload them:
#   - startup: a fresh session loads learnings via skill prose (DRI Phase 0 /
#     steward SKILL) — and a fresh DRI's session marker isn't written yet, so
#     this hook would no-op anyway. Deliberately excluded.
#   - resume: the full conversation is preserved (respawn/wake revives in
#     place), so learnings already sit in context — re-injecting would
#     duplicate them, and background sessions resume often (token waste).
#     Deliberately excluded.
# The old per-turn UserPromptSubmit leg (prime-role-learnings.sh) this used
# to sit alongside is gone entirely — learnings are static within a
# session-epoch, so per-turn injection was unnecessarily frequent. Runs in
# its OWN SessionStart matcher block, separate from compact-recovery.sh's
# "compact"-only matcher — that script handles a different concern
# (initiative-context re-injection) and its matcher is intentionally left
# narrower, untouched by this change.
#
# Resolves this session's role (dri/steward/none) via the shared
# lib/resolve-session-role.sh. For role dri or steward it ARMS the learnings
# gate (lib/learnings-gate.sh: writes <ATH>/learnings-gate/<session_id>/main;
# on compact it defers arming, see ARMING below) and prints one short notice
# instead of the learnings body: learnings-gate.sh
# then denies every tool call until the model runs `ateam learnings <role>`
# itself, which puts the body in context where it cannot be skipped. role=
# steward additionally gets the ledger track record
# (`ateam steward ledger stats`) and, for each of the five fixed decision
# categories, `ateam steward ledger recall <category> --limit 3` — skipping
# any category reporting exactly "no ledger entries" to keep the payload
# tight. Emitted as plain stdout text (NOT JSON — SessionStart output is raw
# text, matching compact-recovery.sh's own pattern, unlike UserPromptSubmit's
# jq/additionalContext shape). Silent no-op, and no marker, for any session
# with no resolved role or an invalid session id.
#
# ARMING depends on .source (agent-teams-7r33.1): an in-process subagent's
# auto-compaction fires SessionStart(compact) with the PARENT session_id and
# no agent_id, so the hook cannot tell whose context compacted. On
# source=compact with a usable transcript_path it therefore does NOT arm: it
# writes <sid>/pending (role plus the compact_boundary counts of the main and
# subagent transcripts, oldest baseline kept) and prints no arm notice or
# learnings body, since the output would land in whichever context compacted
# (a steward still gets its ledger section). learnings-gate.sh arms or
# drops it on the next main-thread call. source=clear, a missing source, or no
# usable transcript_path arm immediately and print the notice.
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-${HOME:-}/.agent-teams}"
ATEAM="${CLAUDE_PLUGIN_ROOT:-}/bin/ateam"

# Capture stdin once non-blocking — Claude Code passes {session_id, ...} on stdin;
# direct invocations have no stdin.
HOOK_STDIN=$(cat 2>/dev/null || true)
HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
export HOOK_SESSION_ID

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$(dirname "$0")/lib/hook-debug-log.sh"

# Log start BEFORE any guard check.
hook_log_start "role-recall-recovery.sh"

command -v bd >/dev/null 2>&1 || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
command -v jq >/dev/null 2>&1 || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
{ [ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -x "$ATEAM" ]; } || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
[ -d "$ATH/.beads" ] || { HOOK_EXIT_REASON="missing-deps"; exit 0; }

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/resolve-steward.sh
. "$(dirname "$0")/lib/resolve-steward.sh"
# shellcheck source=plugins/agent-teams/hooks/scripts/lib/resolve-session-role.sh
. "$(dirname "$0")/lib/resolve-session-role.sh"
# shellcheck source=plugins/agent-teams/hooks/scripts/lib/learnings-gate.sh
. "$(dirname "$0")/lib/learnings-gate.sh"

role=$(resolve_session_role "$ATH" "$HOOK_SESSION_ID")
if [ -z "$role" ]; then
  HOOK_EXIT_REASON="no-role"
  exit 0
fi

hook_log_note "note" "role-resolved role=${role}"

# Fixed category order, mirroring steward_seams.go's StewardLedgerCategory
# declaration order (also the order stewardLedgerCategoryOrder uses in
# internal/verbs/steward.go).
STEWARD_LEDGER_CATEGORIES="plan-approval scope-call merge-approval design-fork unblock-action"

pending_written=0
hook_source=$(printf '%s' "$HOOK_STDIN" | jq -r '.source // empty' 2>/dev/null || true)
hook_tp=$(printf '%s' "$HOOK_STDIN" | jq -r '.transcript_path // empty' 2>/dev/null || true)
if [ "$hook_source" = "compact" ] && [ -n "$hook_tp" ] && [ -f "$hook_tp" ] \
  && [ "$HOOK_SESSION_ID" != "unknown" ] && lg_write_pending "$ATH" "$HOOK_SESSION_ID" "$role" "$hook_tp"; then
  hook_log_note "note" "learnings-gate-pending role=${role}"
  pending_written=1
elif [ "$HOOK_SESSION_ID" != "unknown" ] && lg_arm_main "$ATH" "$HOOK_SESSION_ID" "$role"; then
  hook_log_note "note" "learnings-gate-armed role=${role}"
  echo "## agent-teams: ${role} learnings not loaded. Your next tool call must be the Bash command: ateam learnings ${role}"
else
  # No usable session id to key a gate on: fall back to printing the body.
  echo "## agent-teams: ${role} role learnings (session-start recovery)"
  "$ATEAM" learnings "$role" 2>/dev/null || true
fi

if [ "$role" = "steward" ]; then
  echo ""
  echo "## agent-teams: steward ledger track record"
  "$ATEAM" steward ledger stats 2>/dev/null || true
  for cat in $STEWARD_LEDGER_CATEGORIES; do
    recall_out=$("$ATEAM" steward ledger recall "$cat" --limit 3 2>/dev/null || true)
    if [ "$recall_out" != "no ledger entries" ]; then
      echo ""
      echo "## agent-teams: steward ledger recall (${cat})"
      printf '%s\n' "$recall_out"
    fi
  done
fi

if [ "$pending_written" -eq 1 ]; then
  HOOK_EXIT_REASON="pending"
else
  HOOK_EXIT_REASON="ok"
fi
