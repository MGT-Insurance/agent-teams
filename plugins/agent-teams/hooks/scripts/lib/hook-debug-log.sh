#!/usr/bin/env bash
# Sourced helper — exposes hook_debug_log <script-name> <initiative-id>.
# Appends one TSV line to $ATH/debug/hooks.log on each hook invocation.
# LINE FORMAT: <iso8601-utc>\t<session_id>\t<script-name>\t<initiative-id>
#
# Callers must:
#   1. Set ATH before sourcing (already done at the top of each hook).
#   2. Set HOOK_SESSION_ID before calling (parsed from captured stdin).
#   3. Call: hook_debug_log "$0_or_name" "${match_id:-unknown}"
#
# Writes are wrapped in '|| true' so a disk/permission error never fails the hook.

hook_debug_log() {
  local script_name="${1:-unknown}"
  local initiative_id="${2:-unknown}"
  local ts session_id log_dir log_file

  ts=$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo "unknown")
  # HOOK_SESSION_ID is set by the caller; default to 'unknown' if absent or empty.
  session_id="${HOOK_SESSION_ID:-unknown}"
  [ -n "$session_id" ] || session_id="unknown"

  log_dir="${ATH}/debug"
  log_file="${log_dir}/hooks.log"

  mkdir -p "$log_dir" 2>/dev/null || true
  printf '%s\t%s\t%s\t%s\n' "$ts" "$session_id" "$script_name" "$initiative_id" \
    >> "$log_file" 2>/dev/null || true
}
