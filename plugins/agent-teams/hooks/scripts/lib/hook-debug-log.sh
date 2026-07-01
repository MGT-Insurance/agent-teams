#!/usr/bin/env bash
# Sourced helper — lifecycle logging for agent-teams hook scripts.
#
# LINE FORMAT (6 TAB-separated columns):
#   <iso8601-utc>\t<session_id>\t<script>\t<initiative_id>\t<event>\t<detail>
# Old readers (expecting 4 cols) still work: the first 4 columns are unchanged.
#
# Public API:
#   hook_log_start <script-name>
#     Call at the very top of a hook script, before any guard check.
#     Installs EXIT and signal traps automatically.
#
#   hook_log_note <event> <detail>
#     Emit a mid-run marker (event=note or any custom event).
#     Use HOOK_INITIATIVE to carry the initiative id (set once resolved).
#
#   hook_debug_log <script-name> <initiative-id>
#     Legacy compat — still works, routes through _hook_write_log.
#     Does NOT install traps (callers that only use this keep old behaviour).
#
# Script variables the caller may set:
#   HOOK_INITIATIVE   — initiative id (set once resolved, else "unknown")
#   HOOK_EXIT_REASON  — human reason string; set before each intentional exit
#   HOOK_SESSION_ID   — set from parsed stdin before sourcing this lib
#
# FAIL-SOFT: every write is wrapped with '|| true'; logging errors never fail
# the hook. Does NOT depend on bd or jq. Safe under set -euo pipefail.
#
# SIZE ROTATION: if hooks.log exceeds ~5 MB before an append, the current log
# is rolled to hooks.log.1 (overwriting any prior generation) and a fresh log
# starts. A rare race between concurrent hooks losing a few lines is acceptable.

# ── Internal: compute log path ───────────────────────────────────────────────
_hook_log_file() {
  local log_dir="${ATH}/debug"
  printf '%s/hooks.log' "$log_dir"
}

# ── Internal: size-rotate if needed ──────────────────────────────────────────
_hook_rotate_if_needed() {
  local log_file="$1"
  local size_threshold=5242880  # 5 MB in bytes
  if [ -f "$log_file" ]; then
    local size
    size=$(wc -c < "$log_file" 2>/dev/null || echo 0)
    if [ "$size" -ge "$size_threshold" ] 2>/dev/null; then
      mv "$log_file" "${log_file}.1" 2>/dev/null || true
    fi
  fi
}

# ── Internal: write one TSV line ─────────────────────────────────────────────
_hook_write_log() {
  local script_name="${1:-unknown}"
  local initiative_id="${2:-${HOOK_INITIATIVE:-unknown}}"
  local event="${3:-note}"
  local detail="${4:-}"
  local ts session_id log_dir log_file

  ts=$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo "unknown")
  session_id="${HOOK_SESSION_ID:-unknown}"
  [ -n "$session_id" ] || session_id="unknown"

  log_dir="${ATH}/debug"
  log_file="${log_dir}/hooks.log"

  mkdir -p "$log_dir" 2>/dev/null || true
  _hook_rotate_if_needed "$log_file" 2>/dev/null || true
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$ts" "$session_id" "$script_name" "$initiative_id" "$event" "$detail" \
    >> "$log_file" 2>/dev/null || true
}

# ── Internal: EXIT trap handler ───────────────────────────────────────────────
# Must be called by the trap; uses _HOOK_LOG_SCRIPT set at hook_log_start time.
_hook_exit_trap() {
  local exit_code="${_HOOK_TRAP_EXIT_CODE:-0}"
  local reason="${HOOK_EXIT_REASON:-unexpected}"
  _hook_write_log "${_HOOK_LOG_SCRIPT:-unknown}" "${HOOK_INITIATIVE:-unknown}" \
    "exit" "code=${exit_code} reason=${reason}" 2>/dev/null || true
}

# ── Internal: signal trap handler ────────────────────────────────────────────
_hook_signal_trap() {
  local signame="${1:-UNKNOWN}"
  # Log signal BEFORE removing EXIT trap to avoid a double-log on exit.
  _hook_write_log "${_HOOK_LOG_SCRIPT:-unknown}" "${HOOK_INITIATIVE:-unknown}" \
    "signal" "$signame" 2>/dev/null || true
  # Unset EXIT trap so we don't also fire the exit handler for this.
  trap - EXIT 2>/dev/null || true
  HOOK_EXIT_REASON="signal-${signame}"
  _hook_write_log "${_HOOK_LOG_SCRIPT:-unknown}" "${HOOK_INITIATIVE:-unknown}" \
    "exit" "code=130 reason=signal-${signame}" 2>/dev/null || true
  # Re-raise so the process exits with the proper signal exit code.
  trap - "$signame" 2>/dev/null || true
  kill -"$signame" "$$" 2>/dev/null || exit 130
}

# ── Public: hook_log_start ───────────────────────────────────────────────────
# Call at the very top of each hook script (before any guard check).
# Logs a "start" event and installs EXIT + signal traps.
hook_log_start() {
  local script_name="${1:-unknown}"
  # Store script name for use in traps.
  _HOOK_LOG_SCRIPT="$script_name"
  export _HOOK_LOG_SCRIPT

  # Default exit reason — overridden by the script before each intentional exit.
  HOOK_EXIT_REASON="${HOOK_EXIT_REASON:-unexpected}"
  export HOOK_EXIT_REASON

  _hook_write_log "$script_name" "${HOOK_INITIATIVE:-unknown}" "start" "" 2>/dev/null || true

  # EXIT trap: capture $? immediately into a variable so subsequent commands
  # inside the handler don't clobber it. Under set -e, $? is the failing
  # command's code at trap invocation time.
  # We store it before any other action; _hook_exit_trap reads _HOOK_TRAP_EXIT_CODE.
  trap '
    _HOOK_TRAP_EXIT_CODE=$?
    export _HOOK_TRAP_EXIT_CODE
    _hook_exit_trap
  ' EXIT 2>/dev/null || true

  # Signal traps — log signame then re-raise cleanly.
  # shellcheck disable=SC2064
  trap '_hook_signal_trap TERM' TERM 2>/dev/null || true
  # shellcheck disable=SC2064
  trap '_hook_signal_trap HUP'  HUP  2>/dev/null || true
  # shellcheck disable=SC2064
  trap '_hook_signal_trap INT'  INT  2>/dev/null || true
}

# ── Public: hook_log_note ────────────────────────────────────────────────────
# Emit a mid-run marker. event defaults to "note".
hook_log_note() {
  local event="${1:-note}"
  local detail="${2:-}"
  _hook_write_log "${_HOOK_LOG_SCRIPT:-unknown}" "${HOOK_INITIATIVE:-unknown}" \
    "$event" "$detail" 2>/dev/null || true
}

# ── Public: hook_debug_log (legacy compat) ───────────────────────────────────
# Original 2-arg call: hook_debug_log <script-name> <initiative-id>
# Routes through the new writer; does NOT install traps.
hook_debug_log() {
  local script_name="${1:-unknown}"
  local initiative_id="${2:-unknown}"
  _hook_write_log "$script_name" "$initiative_id" "start" "" 2>/dev/null || true
}
