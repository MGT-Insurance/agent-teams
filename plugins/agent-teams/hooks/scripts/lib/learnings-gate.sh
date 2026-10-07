#!/usr/bin/env bash
# Sourced helper — shared marker layout, role resolution, allow-match and
# deny text for the learnings gate (agent-teams-75h7.1 CONTRACT).
#
# The gate blocks an agent-teams context (DRI/steward main thread, or a role
# subagent/teammate) until it runs `ateam learnings <role>`. Three hook
# scripts cooperate through the files below, so none of them re-implements
# paths:
#   - role-recall-recovery.sh (SessionStart clear|compact)  -> lg_arm_main
#   - learnings-gate.sh (PreToolUse + PostToolUse(Agent))   -> everything else
#   - cleanup-dri-marker.sh (SessionEnd)                    -> lg_cleanup_session
#
# Layout (ATH="${AGENT_TEAMS_HOME:-$HOME/.agent-teams}"):
#   <ATH>/learnings-gate/<session_id>/main            one line: "dri" | "steward".
#                                                     Presence = the main thread
#                                                     must load learnings.
#   <ATH>/learnings-gate/<session_id>/pending        three lines: role ("dri" |
#                                                     "steward"), the main
#                                                     transcript's compact_boundary
#                                                     count, and the total across
#                                                     its subagents/agent-*.jsonl,
#                                                     all as of the SessionStart
#                                                     (compact) hook. Presence = a
#                                                     compaction happened whose
#                                                     owner (main thread or an
#                                                     in-process subagent) is not
#                                                     yet known; the first
#                                                     main-thread PreToolUse
#                                                     recounts, then arms main or
#                                                     drops it. The oldest
#                                                     baseline wins.
#   <ATH>/learnings-gate/<session_id>/spawns/<name>   bare role of a named
#                                                     teammate spawn.
#   <ATH>/learnings-gate/<session_id>/loaded/<id>     presence = subagent <id>
#                                                     has run its command. No
#                                                     record = not loaded.
# Every session_id / name / agent_id used in a path must match
# ^[A-Za-z0-9_-]{1,128}$ (valid_session_id). An invalid value is "no gate":
# every function returns non-zero and prints nothing, and callers allow.
#
# FAIL-SOFT: no function exits the shell, prints errors, or relies on set -e.
# Callers treat a non-zero return or an empty result as "allow".
#
# Public API:
#   lg_root <ath> <sid>                  prints the gate root; 1 if invalid
#   lg_arm_main <ath> <sid> <role>       role must be dri|steward
#   lg_count_boundaries <file>           prints the compact_boundary line count (0 if missing)
#   lg_boundary_counts <transcript_path> <sid>
#                                        prints "<main> <subagents-total>"
#   lg_write_pending <ath> <sid> <role> <transcript_path>
#                                        no-op if pending exists
#   lg_read_pending <ath> <sid>          sets LG_PENDING_ROLE/_MAIN/_SUB; 1 if absent or malformed
#   lg_drop_pending <ath> <sid>
#   lg_record_spawn <ath> <sid> <name> <role>
#   lg_known_subagent_role <role> <roles_dir>
#   lg_resolve_subagent_role <ath> <sid> <agent_type> <roles_dir>
#                                        prints the role, or nothing
#   lg_is_allowed_command <command> <role>
#   lg_mark_loaded <ath> <sid> <agent_id>
#   lg_clear_main <ath> <sid>
#   lg_cleanup_session <ath> <sid>
#   lg_deny_reason <role>                prints the verbatim deny text

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/resolve-session-role.sh
. "$(dirname "${BASH_SOURCE[0]}")/resolve-session-role.sh"

# ── Public: lg_root ──────────────────────────────────────────────────────────
lg_root() {
  local ath="$1" sid="$2"
  [ -n "$ath" ] || return 1
  valid_session_id "$sid" || return 1
  printf '%s/learnings-gate/%s' "$ath" "$sid"
}

# ── Public: lg_arm_main ──────────────────────────────────────────────────────
lg_arm_main() {
  local ath="$1" sid="$2" role="$3" root
  case "$role" in dri | steward) ;; *) return 1 ;; esac
  root=$(lg_root "$ath" "$sid") || return 1
  mkdir -p "$root" 2>/dev/null || return 1
  printf '%s\n' "$role" > "$root/main" 2>/dev/null || return 1
}

# ── Public: lg_count_boundaries ──────────────────────────────────────────────
# Each compaction appends one line containing "compact_boundary" to the
# transcript of the context that compacted.
lg_count_boundaries() {
  local n
  n=$(grep -c -F '"compact_boundary"' "$1" 2>/dev/null) || n=0
  printf '%s' "${n:-0}"
}

# ── Public: lg_boundary_counts ───────────────────────────────────────────────
# Subagent transcripts live at <dirname transcript>/<sid>/subagents/agent-*.jsonl.
lg_boundary_counts() {
  local tp="$1" sid="$2" main sub=0 f n
  main=$(lg_count_boundaries "$tp")
  for f in "$(dirname "$tp")/$sid/subagents"/agent-*.jsonl; do
    [ -f "$f" ] || continue
    n=$(lg_count_boundaries "$f")
    sub=$((sub + n))
  done
  printf '%s %s' "$main" "$sub"
}

# ── Public: lg_write_pending ─────────────────────────────────────────────────
lg_write_pending() {
  local ath="$1" sid="$2" role="$3" tp="$4" root counts
  case "$role" in dri | steward) ;; *) return 1 ;; esac
  root=$(lg_root "$ath" "$sid") || return 1
  [ -e "$root/pending" ] && return 0
  counts=$(lg_boundary_counts "$tp" "$sid")
  mkdir -p "$root" 2>/dev/null || return 1
  printf '%s\n%s\n%s\n' "$role" "${counts% *}" "${counts#* }" > "$root/pending" 2>/dev/null || return 1
}

# ── Public: lg_read_pending ──────────────────────────────────────────────────
lg_read_pending() {
  local ath="$1" sid="$2" root
  LG_PENDING_ROLE="" LG_PENDING_MAIN="" LG_PENDING_SUB=""
  root=$(lg_root "$ath" "$sid") || return 1
  [ -f "$root/pending" ] || return 1
  {
    IFS= read -r LG_PENDING_ROLE
    IFS= read -r LG_PENDING_MAIN
    IFS= read -r LG_PENDING_SUB
  } < "$root/pending" 2>/dev/null || true
  case "$LG_PENDING_ROLE" in dri | steward) ;; *) return 1 ;; esac
  case "$LG_PENDING_MAIN" in '' | *[!0-9]*) return 1 ;; esac
  case "$LG_PENDING_SUB" in '' | *[!0-9]*) return 1 ;; esac
}

# ── Public: lg_drop_pending ──────────────────────────────────────────────────
lg_drop_pending() {
  local ath="$1" sid="$2" root
  root=$(lg_root "$ath" "$sid") || return 1
  rm -f "$root/pending" 2>/dev/null || return 1
}

# ── Public: lg_record_spawn ──────────────────────────────────────────────────
lg_record_spawn() {
  local ath="$1" sid="$2" name="$3" role="$4" root
  valid_session_id "$name" || return 1
  valid_session_id "$role" || return 1
  root=$(lg_root "$ath" "$sid") || return 1
  mkdir -p "$root/spawns" 2>/dev/null || return 1
  printf '%s\n' "$role" > "$root/spawns/$name" 2>/dev/null || return 1
}

# ── Public: lg_known_subagent_role ───────────────────────────────────────────
# True iff <role> is the basename of a roles/*.md file (README excluded).
lg_known_subagent_role() {
  local role="$1" roles_dir="$2"
  valid_session_id "$role" || return 1
  [ "$role" != "README" ] || return 1
  [ -f "$roles_dir/$role.md" ]
}

# ── Public: lg_resolve_subagent_role ─────────────────────────────────────────
lg_resolve_subagent_role() {
  local ath="$1" sid="$2" agent_type="$3" roles_dir="$4" root role
  case "$agent_type" in
    agent-teams-*)
      role="${agent_type#agent-teams-}"
      lg_known_subagent_role "$role" "$roles_dir" && printf '%s' "$role"
      return 0
      ;;
  esac
  valid_session_id "$agent_type" || return 0
  root=$(lg_root "$ath" "$sid") || return 0
  [ -f "$root/spawns/$agent_type" ] || return 0
  role=$(head -n 1 "$root/spawns/$agent_type" 2>/dev/null) || return 0
  lg_known_subagent_role "$role" "$roles_dir" && printf '%s' "$role"
  return 0
}

# ── Public: lg_is_allowed_command ────────────────────────────────────────────
# Exactly "ateam learnings <role>" after trimming outer whitespace. Nothing
# else: no pipes, redirects, absolute paths, or other roles.
lg_is_allowed_command() {
  local cmd="$1" role="$2"
  valid_session_id "$role" || return 1
  cmd="${cmd#"${cmd%%[![:space:]]*}"}"
  cmd="${cmd%"${cmd##*[![:space:]]}"}"
  [ "$cmd" = "ateam learnings $role" ]
}

# ── Public: lg_mark_loaded ───────────────────────────────────────────────────
lg_mark_loaded() {
  local ath="$1" sid="$2" agent_id="$3" root
  valid_session_id "$agent_id" || return 1
  root=$(lg_root "$ath" "$sid") || return 1
  mkdir -p "$root/loaded" 2>/dev/null || return 1
  : > "$root/loaded/$agent_id" 2>/dev/null || return 1
}

# ── Public: lg_clear_main ────────────────────────────────────────────────────
lg_clear_main() {
  local ath="$1" sid="$2" root
  root=$(lg_root "$ath" "$sid") || return 1
  rm -f "$root/main" 2>/dev/null || return 1
}

# ── Public: lg_cleanup_session ───────────────────────────────────────────────
lg_cleanup_session() {
  local ath="$1" sid="$2" root
  root=$(lg_root "$ath" "$sid") || return 0
  rm -rf "$root" 2>/dev/null || true
  return 0
}

# ── Public: lg_deny_reason ───────────────────────────────────────────────────
lg_deny_reason() {
  local role="$1"
  printf 'BLOCKED: your agent-teams %s learnings are not loaded in this context. Every other tool call stays blocked until you load them. Read the whole output when you do. Run this Bash command by itself, exactly as written, with no period or anything else added: ateam learnings %s' "$role" "$role"
}
