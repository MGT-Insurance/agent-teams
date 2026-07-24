#!/usr/bin/env bash
# Sourced helper — shared session-role resolution for agent-teams hook
# scripts (agent-teams-7ew5.2.1 CONTRACT).
#
# Backs the durable, role-scoped learnings+ledger re-injection mechanism: a
# UserPromptSubmit leg (prime-role-learnings.sh) and a SessionStart-compact
# leg (role-recall-recovery.sh) both need to answer "is THIS session the
# DRI, the Steward, or neither?" without duplicating the two roles'
# genuinely different underlying session-identity mechanics:
#   - Steward: a fixed, content-ignored, cwd-scoped marker written once at
#     machine-setup time by `ateam steward init` (see lib/resolve-steward.sh's
#     is_steward_cwd) — unrelated to any particular session_id.
#   - DRI: no worktree is exclusively-DRI (teammates share the same worktree
#     cwd), so a per-session_id marker under $ATH/dri-sessions/ is used
#     instead, written by the /dri skill's Phase 0 (dri_mark_session) and
#     removed on SessionEnd (dri_cleanup_session_marker).
#
# Public API:
#   valid_session_id <id>
#     Returns 0 iff <id> matches ^[A-Za-z0-9_-]{1,128}$. Every path built
#     from a session_id MUST go through this first — an invalid id (empty,
#     path-traversal, slash-containing, etc.) is treated as "no session"
#     (silent no-op), never an error.
#
#   dri_session_marker_path <ath> <session_id>
#     Prints "<ath>/dri-sessions/<session_id>". Caller must have already
#     validated session_id.
#
#   dri_mark_session <ath>
#     Reads $CLAUDE_CODE_SESSION_ID from the environment (the real env var
#     available to skill-prose Bash — NOT $CLAUDE_SESSION_ID, and NOT
#     available to hook scripts, which get session_id from stdin JSON
#     instead), validates it, mkdir -p's the dri-sessions dir, and writes the
#     marker (content is a timestamp, ignored by readers — presence is the
#     signal). No-ops silently if the env var is absent/invalid. Called from
#     skill prose (the /dri skill's Phase 0), NOT from a hook script.
#
#   dri_cleanup_session_marker <ath> <session_id>
#     Validates session_id, then removes the marker if present. No-ops
#     silently otherwise (removal is idempotent). Called from the SessionEnd
#     hook script (cleanup-dri-marker.sh).
#
#   resolve_session_role <ath> <session_id>
#     The shared discriminator both new hooks call. Requires the caller to
#     have already sourced lib/resolve-steward.sh (this function calls
#     is_steward_cwd, defined there) and to have ATH exported (is_steward_cwd
#     reads $ATH itself). Ordering:
#       1. is_steward_cwd true  -> echoes "steward"
#       2. else dri marker exists for a validated session_id -> echoes "dri"
#       3. else -> echoes "" (empty = no role = silent no-op for callers)
#     This ordering means a steward session is NEVER misidentified as dri
#     even if some future code path ever wrote both markers.

# ── Public: valid_session_id ─────────────────────────────────────────────────
valid_session_id() {
  local id="$1"
  [[ "$id" =~ ^[A-Za-z0-9_-]{1,128}$ ]]
}

# ── Public: dri_session_marker_path ──────────────────────────────────────────
dri_session_marker_path() {
  local ath="$1" session_id="$2"
  printf '%s/dri-sessions/%s' "$ath" "$session_id"
}

# ── Public: dri_mark_session ─────────────────────────────────────────────────
dri_mark_session() {
  local ath="$1"
  local sid="${CLAUDE_CODE_SESSION_ID:-}"
  valid_session_id "$sid" || return 0

  local marker
  marker=$(dri_session_marker_path "$ath" "$sid")
  mkdir -p "$(dirname "$marker")" 2>/dev/null || return 0
  printf 'created: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo unknown)" > "$marker" 2>/dev/null || true
  return 0
}

# ── Public: dri_cleanup_session_marker ───────────────────────────────────────
dri_cleanup_session_marker() {
  local ath="$1" session_id="$2"
  valid_session_id "$session_id" || return 0

  local marker
  marker=$(dri_session_marker_path "$ath" "$session_id")
  rm -f "$marker" 2>/dev/null || true
  return 0
}

# ── Public: resolve_session_role ─────────────────────────────────────────────
resolve_session_role() {
  local ath="$1" session_id="$2"

  if is_steward_cwd; then
    printf 'steward'
    return 0
  fi

  if valid_session_id "$session_id"; then
    local marker
    marker=$(dri_session_marker_path "$ath" "$session_id")
    if [ -f "$marker" ]; then
      printf 'dri'
      return 0
    fi
  fi

  printf ''
  return 0
}
