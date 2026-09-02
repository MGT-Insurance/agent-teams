#!/usr/bin/env bash
# PostToolUse hook (matcher: Bash) for agent-teams: auto-record a freshly
# opened PR on its initiative's `pr` rail, so routing never depends on the DRI
# remembering to run `ateam pr add`.
#
# WHY (mechanical root cause): a PR only routes back to its owning session when
# it is on the initiative's `pr` rail — route_match.go tier-1 iterates every
# ResolvedPR. Today that rail is populated only by the /dri skill's Phase 5
# `ateam pr add`. If a DRI opens a SECOND or THIRD PR and forgets that call,
# the PR is unroutable: `branch` is single-valued and frozen at dispatch (no
# WithBranch mutator), so tier-2 branch match only ever matches the ORIGINAL
# branch, and the Notes free-text fallback returns only the FIRST PR — so
# route-pr-event's default branch silently drops every non-review_requested
# event for the forgotten PR. route-pr-event cannot self-heal it: the drop
# happens precisely when there is no match to hang a rail-append on. This hook
# closes the gap at the one place the PR->initiative association is unambiguous
# — the moment the PR is created, inside the initiative's worktree.
#
# WHAT IT DOES — ALL of:
#   tool_name == "Bash"
#   AND the command is a `gh pr create`
#   AND the tool output contains a GitHub PR URL (gh prints it on success; a
#       failed create prints none -> no-op)
#   AND cwd resolves to an OPEN initiative via `ateam resolve-initiative`
#       (ancestor-or-self; empty = not an agent-teams worktree = silent no-op —
#       this is the "am I an agent-teams session" gate, the same mechanism
#       session-start-inbox.sh / wake-watcher.sh already use)
# THEN `ateam pr add <initiative-id> <pr-url>` (idempotent — WithPR dedups, so
# it is harmless if the DRI also ran it) and confirm via additionalContext.
#
# FAIL-SOFT everywhere: a missing dep, an unresolved cwd, or a failing
# `ateam pr add` never fails the tool call. On a pr-add error the hook falls
# back to a reminder (additionalContext) so the DRI can register it by hand —
# a reminder is the weaker guarantee this hook exists to replace, used only
# when the automatic path itself failed.
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-${HOME:-}/.agent-teams}"
ATEAM="${CLAUDE_PLUGIN_ROOT:-}/bin/ateam"

# Capture stdin once non-blocking — Claude Code passes the PostToolUse payload
# (tool_name, tool_input, tool_response, cwd, session_id) on stdin; direct
# invocations have no stdin. Must not break set -euo pipefail when empty.
HOOK_STDIN=$(cat 2>/dev/null || true)
HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
export HOOK_SESSION_ID

# Extra arg for `ateam resolve-initiative`: pass the session id when known, so
# a launch-cwd-mismatched session (cwd doesn't match its registered worktree)
# still resolves via its durable session tie instead of going deaf
# (agent-teams-y814.8, at-1k234). "unknown" is the stdin-parse-failure
# sentinel above, not a real session id — omit it too, same as an empty id.
session_id_flag=""
if [ -n "$HOOK_SESSION_ID" ] && [ "$HOOK_SESSION_ID" != "unknown" ]; then
  session_id_flag="$HOOK_SESSION_ID"
fi

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$(dirname "$0")/lib/hook-debug-log.sh"

# Log start BEFORE any guard check.
hook_log_start "register-pr-on-open.sh"

command -v jq >/dev/null 2>&1 || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
{ [ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -x "$ATEAM" ]; } || { HOOK_EXIT_REASON="missing-deps"; exit 0; }
[ -d "$ATH/.beads" ] || { HOOK_EXIT_REASON="missing-deps"; exit 0; }

tool_name=$(printf '%s' "$HOOK_STDIN" | jq -r '.tool_name // empty' 2>/dev/null || true)
[ "$tool_name" = "Bash" ] || { HOOK_EXIT_REASON="not-bash"; exit 0; }

command=$(printf '%s' "$HOOK_STDIN" | jq -r '.tool_input.command // empty' 2>/dev/null || true)
[ -n "$command" ] || { HOOK_EXIT_REASON="no-command"; exit 0; }

# ── Guard: is this a `gh pr create`? ────────────────────────────────────────
# gh must be a bare word (not a substring of another token) followed by the
# `pr create` subcommand; flags/args after it are irrelevant, and a leading
# `cd x && ` / env prefix still matches.
pr_create_re='(^|[^A-Za-z0-9_-])gh[[:space:]]+pr[[:space:]]+create([[:space:]]|$)'
[[ "$command" =~ $pr_create_re ]] || { HOOK_EXIT_REASON="not-pr-create"; exit 0; }

# ── Extract the created PR URL from the tool output ─────────────────────────
# tool_response is an object for Bash ({stdout, stderr, ...}); older shapes may
# hand a bare string. Coerce either to text, then take the first GitHub PR URL.
# gh prints the new PR's URL to stdout on success; a failed create prints none.
output=$(printf '%s' "$HOOK_STDIN" | jq -r '
  .tool_response
  | if type == "object" then ((.stdout // "") + "\n" + (.stderr // ""))
    elif type == "string" then .
    else tostring end
' 2>/dev/null || true)

pr_url=$(printf '%s' "$output" | grep -oiE 'https://github\.com/[^/[:space:]]+/[^/[:space:]]+/pull/[0-9]+' | head -n1 || true)
[ -n "$pr_url" ] || { HOOK_EXIT_REASON="no-pr-url"; exit 0; }

# ── Resolve the owning initiative from cwd ──────────────────────────────────
# `ateam resolve-initiative` owns the matching rule (internal/verbs/match.go);
# this script must not re-derive it. Empty output = cwd is not a registered
# worktree = this is not an agent-teams initiative session -> silent no-op.
# --session-id (session_id_flag, computed above) is a durable fallback beside
# it, not a replacement.
cwd=$(printf '%s' "$HOOK_STDIN" | jq -r '.cwd // empty' 2>/dev/null || true)
[ -n "$cwd" ] || cwd="$PWD"

initiative_id=$("$ATEAM" resolve-initiative "$cwd" ${session_id_flag:+--session-id "$session_id_flag"} 2>/dev/null || true)
if [ -z "$initiative_id" ]; then
  HOOK_EXIT_REASON="no-open-match"
  exit 0
fi
HOOK_INITIATIVE="$initiative_id"
export HOOK_INITIATIVE

# ── Register the PR on the rail (idempotent) ────────────────────────────────
if add_out=$("$ATEAM" pr add "$initiative_id" "$pr_url" 2>&1); then
  hook_log_note "note" "pr-registered ${pr_url} -> ${initiative_id}: ${add_out}"
  HOOK_EXIT_REASON="pr-registered"
  jq -n --arg ctx "Recorded ${pr_url} on initiative ${initiative_id}'s pr rail (pr-shepherd can route its events). No need to run \`ateam pr add\` for it." \
    '{"additionalContext": $ctx}'
  exit 0
fi

# pr add failed — fall back to a reminder so the PR is not silently lost.
hook_log_note "note" "pr-add-failed ${pr_url} -> ${initiative_id}: ${add_out}"
HOOK_EXIT_REASON="pr-add-failed"
jq -n --arg ctx "Could not auto-record ${pr_url} on initiative ${initiative_id} (\`ateam pr add\` failed: ${add_out}). Run \`ateam pr add ${initiative_id} ${pr_url}\` yourself so pr-shepherd can route this PR's events." \
  '{"additionalContext": $ctx}'
exit 0
