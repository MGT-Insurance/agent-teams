#!/usr/bin/env bash
# SessionStart hook for agent-teams: cold-path mailbox drain.
# Fires on startup, resume, clear, and compact. Runs `ateam inbox` so any mail
# that arrived while the session was inactive (or before the first UserPromptSubmit)
# is surfaced as additionalContext at session open.
# Silent no-op when cwd is not a registered initiative.
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-$HOME/.agent-teams}"

command -v bd    >/dev/null 2>&1 || exit 0
command -v jq    >/dev/null 2>&1 || exit 0
command -v ateam >/dev/null 2>&1 || exit 0
[ -d "$ATH/.beads" ] || exit 0

# ── Resolve initiative id by worktree:$PWD ──────────────────────────────────
match_id=$(bd -C "$ATH" list --status=open --json 2>/dev/null \
  | jq -r --arg wt "worktree: $PWD" \
      '[.[] | select((.description // "") | split("\n") | any(. == $wt))][0].id // empty' \
  2>/dev/null || true)
[ -n "$match_id" ] || exit 0

# ── Drain: run ateam inbox; print output for additionalContext injection ──────
ateam inbox 2>/dev/null || true
