#!/usr/bin/env bash
# verify-live-settings.sh — on-demand LIVE check that the real `claude` CLI
# actually honors the --settings autoCompactWindow value and the --advisor
# flag the way internal/verbs/dispatch.go's bgSessionArgs() sends them to
# background DRI sessions.
#
# ============================================================================
# WARNING: this launches FOUR real `claude --bg` sessions. Each is a live,
# billed Claude session — same as any background DRI session `ateam dispatch`
# starts. This costs real API dollars and takes real wall-clock time (expect
# on the order of a minute or two total). See eval/README.md for the
# precedent this cost-warning follows.
#
# NEVER run this from `go test`, `go test ./...`, CI, a git hook, or any
# automated/scheduled loop. This is a manual, human-triggered tool for the
# day someone suspects the auto-compact-window or advisor settings have
# silently stopped being respected by the `claude` binary. See bead
# agent-teams-nemc.2 / initiative at-0gno for the investigation that
# motivated this script.
#
# What this does NOT test: internal/verbs/dispatch_test.go's
# TestBGSessionArgs_ContainsSettingsFlag already locks the ateam-SIDE
# contract — that bgSessionArgs() always builds the correct --settings JSON
# string. That test is free and runs in CI. This script is the complementary
# CLI-side check: does the real `claude` binary actually honor what we send
# it. Don't duplicate one with the other.
#
# Usage:
#   scripts/verify-live-settings.sh [A] [B]
#     A, B  autoCompactWindow token values to compare (default: 200000 500000)
#
# Exit status: 0 if all four checks pass, 1 otherwise. Every probe session is
# stopped on exit (success, failure, or Ctrl-C) via `claude stop`.
# ============================================================================

set -euo pipefail

A="${1:-200000}"
B="${2:-500000}"
PROMPT="This is an automated verification probe. Immediately reply with the single word ack and take no other action."
PLACEHOLDER_SYSTEM_PROMPT="verify-live-settings probe session — not a real DRI run."
WAIT_TIMEOUT=90

if ! command -v claude >/dev/null 2>&1; then
  echo "verify-live-settings: 'claude' not found in PATH" >&2
  exit 1
fi

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/verify-live-settings.XXXXXX")"
SESSION_NAMES=()
FAILURES=0

cleanup() {
  local name
  if [ "${#SESSION_NAMES[@]}" -gt 0 ]; then
    for name in "${SESSION_NAMES[@]}"; do
      claude stop "$name" >/dev/null 2>&1 || true
    done
  fi
}
trap cleanup EXIT

log() { echo "[verify-live-settings] $*"; }

# wait_for_pattern <file> <extended-regex> <timeout-seconds>
# Polls <file> until it exists and matches <extended-regex>, or the timeout
# elapses. Bounded poll loop (not an unbounded busy-wait) — mirrors the
# ~/.claude/scripts/wait-for-url pattern for "wait until async state is
# ready" since no repo-local helper covers "wait until a file contains X".
wait_for_pattern() {
  local file="$1" pattern="$2" timeout="$3"
  local deadline=$(( $(date +%s) + timeout ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    if [ -f "$file" ] && grep -qE "$pattern" "$file" 2>/dev/null; then
      return 0
    fi
    sleep 2
  done
  return 1
}

# launch_probe <name> <settings-json> <advisor-or-empty> <debug-file>
# Builds argv matching bgSessionArgs()'s shape: --bg -n <name> --model opus
# --permission-mode bypassPermissions --settings <json> --append-system-prompt
# <rule> [--advisor <model>] <prompt>, plus --debug-file for this script's own
# inspection (not part of production argv).
launch_probe() {
  local name="$1" settings_json="$2" advisor="$3" debug_file="$4"
  local args=(--bg -n "$name" --model opus --permission-mode bypassPermissions \
    --settings "$settings_json" \
    --append-system-prompt "$PLACEHOLDER_SYSTEM_PROMPT" \
    --debug-file "$debug_file")
  if [ -n "$advisor" ]; then
    args+=(--advisor "$advisor")
  fi
  args+=("$PROMPT")
  claude "${args[@]}" >/dev/null
  SESSION_NAMES+=("$name")
}

extract_effective_window() {
  grep -m1 -oE 'effectiveWindow=[0-9]+' "$1" | grep -oE '[0-9]+'
}

STAMP="$(date +%s)"

# ---- Checks 1-3: auto-compact-window tracks the configured value ----------

log "launching auto-compact probe A (autoCompactWindow=$A)..."
NAME_A="verify-settings-acw-a-$STAMP"
LOG_A="$WORKDIR/acw-a.log"
launch_probe "$NAME_A" "{\"autoCompactWindow\":$A}" "" "$LOG_A"

log "launching auto-compact probe B (autoCompactWindow=$B)..."
NAME_B="verify-settings-acw-b-$STAMP"
LOG_B="$WORKDIR/acw-b.log"
launch_probe "$NAME_B" "{\"autoCompactWindow\":$B}" "" "$LOG_B"

if wait_for_pattern "$LOG_A" 'autocompact:.*effectiveWindow=[0-9]+' "$WAIT_TIMEOUT"; then
  EFFECTIVE_A="$(extract_effective_window "$LOG_A")"
  log "probe A: effectiveWindow=$EFFECTIVE_A (requested $A, margin $((A - EFFECTIVE_A)))"
else
  log "FAIL: probe A never logged an autocompact: effectiveWindow= line within ${WAIT_TIMEOUT}s"
  EFFECTIVE_A=""
  FAILURES=$((FAILURES + 1))
fi

if wait_for_pattern "$LOG_B" 'autocompact:.*effectiveWindow=[0-9]+' "$WAIT_TIMEOUT"; then
  EFFECTIVE_B="$(extract_effective_window "$LOG_B")"
  log "probe B: effectiveWindow=$EFFECTIVE_B (requested $B, margin $((B - EFFECTIVE_B)))"
else
  log "FAIL: probe B never logged an autocompact: effectiveWindow= line within ${WAIT_TIMEOUT}s"
  EFFECTIVE_B=""
  FAILURES=$((FAILURES + 1))
fi

if [ -n "$EFFECTIVE_A" ] && [ -n "$EFFECTIVE_B" ]; then
  WANT_DELTA=$((B - A))
  GOT_DELTA=$((EFFECTIVE_B - EFFECTIVE_A))
  if [ "$GOT_DELTA" -eq "$WANT_DELTA" ]; then
    log "PASS: effectiveWindow tracks autoCompactWindow 1:1 (delta=$GOT_DELTA)"
  else
    log "FAIL: effectiveWindow delta=$GOT_DELTA, want $WANT_DELTA (B-A) — the CLI is no longer tracking the requested value"
    FAILURES=$((FAILURES + 1))
  fi
else
  log "SKIP: cannot check the delta invariant without both effectiveWindow values"
  FAILURES=$((FAILURES + 1))
fi

# ---- Checks 4-5: --advisor activates the AdvisorTool, and only when set ---

log "launching advisor-positive probe (--advisor opus)..."
NAME_ADV="verify-settings-advisor-on-$STAMP"
LOG_ADV="$WORKDIR/advisor-on.log"
launch_probe "$NAME_ADV" "{\"autoCompactWindow\":$A}" "opus" "$LOG_ADV"

log "launching advisor-negative probe (no --advisor)..."
NAME_NOADV="verify-settings-advisor-off-$STAMP"
LOG_NOADV="$WORKDIR/advisor-off.log"
launch_probe "$NAME_NOADV" "{\"autoCompactWindow\":$A}" "" "$LOG_NOADV"

# The literal bracket-prefixed debug-log line, not a bare substring match —
# distinguishes genuine tool activation from incidental prose mentions of the
# word "advisor" (CLAUDE.md content, bd-remember echoes, etc).
ADVISOR_LINE='\[AdvisorTool\] Server-side tool enabled'

if wait_for_pattern "$LOG_ADV" "$ADVISOR_LINE" "$WAIT_TIMEOUT"; then
  log "PASS: advisor-positive probe logged a genuine AdvisorTool activation:"
  grep -m1 -E "$ADVISOR_LINE" "$LOG_ADV" | sed 's/^/  /'
else
  log "FAIL: advisor-positive probe (--advisor opus) never logged '$ADVISOR_LINE' within ${WAIT_TIMEOUT}s"
  FAILURES=$((FAILURES + 1))
fi

# Negative control: sync on the autocompact line (present regardless of
# advisor) to know the baseline session reached the same startup point,
# then assert the genuine advisor line never appeared.
if wait_for_pattern "$LOG_NOADV" 'autocompact:.*effectiveWindow=[0-9]+' "$WAIT_TIMEOUT"; then
  if grep -qE "$ADVISOR_LINE" "$LOG_NOADV" 2>/dev/null; then
    log "FAIL: advisor-negative probe (no --advisor) logged a genuine AdvisorTool activation — should be none"
    FAILURES=$((FAILURES + 1))
  else
    log "PASS: advisor-negative probe logged zero genuine AdvisorTool activations"
  fi
else
  log "FAIL: advisor-negative probe never reached the startup sync point (autocompact: line) within ${WAIT_TIMEOUT}s"
  FAILURES=$((FAILURES + 1))
fi

log "debug logs kept at: $WORKDIR"

if [ "$FAILURES" -eq 0 ]; then
  log "ALL CHECKS PASSED"
  exit 0
else
  log "$FAILURES CHECK(S) FAILED"
  exit 1
fi
