#!/usr/bin/env bash
# hook-register-pr-on-open.test.sh — register-pr-on-open.sh (agent-teams-9lq5):
# a PostToolUse Bash hook must auto-record a freshly opened PR on its
# initiative's `pr` rail via `ateam pr add`, resolving the initiative from cwd
# with `ateam resolve-initiative`. This test checks the SHELL WIRING only —
# guard predicate, URL extraction, the resolve->add call sequence, and
# fail-soft behaviour — without depending on the real `ateam` binary or a live
# bd workspace (the verbs' own semantics are covered by internal/verbs Go
# tests). `ateam` is faked and records every invocation's args.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/register-pr-on-open.sh"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

# Assertion helpers (if/then/else form — matches the repo's test idiom).
exits0()  { if [ "$RC" -eq 0 ]; then pass "$1"; else fail "$1 (exit $RC)"; fi; }
logged()  { if grep -q "$1" "$CALL_LOG"; then pass "$2"; else fail "$2: $(cat "$CALL_LOG")"; fi; }
unlogged(){ if grep -q "$1" "$CALL_LOG"; then fail "$2: $(cat "$CALL_LOG")"; else pass "$2"; fi; }
noCalls() { if [ ! -s "$CALL_LOG" ]; then pass "$1"; else fail "$1: $(cat "$CALL_LOG")"; fi; }
inCtx()   { if printf '%s' "$OUT" | grep -q "$1"; then pass "$2"; else fail "$2: $OUT"; fi; }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

# ── Fake ateam: records calls; resolve-initiative output and pr-add exit are
# env-controllable (RESOLVE_OUT, PR_ADD_EXIT). ──────────────────────────────
FAKE_PLUGIN_ROOT="$T/plugin-root"
mkdir -p "$FAKE_PLUGIN_ROOT/bin"
CALL_LOG="$T/ateam-calls.log"
: > "$CALL_LOG"
cat > "$FAKE_PLUGIN_ROOT/bin/ateam" <<'SHIM'
#!/usr/bin/env bash
echo "$@" >> "$CALL_LOG"
if [ "$1" = "resolve-initiative" ]; then
  printf '%s' "${RESOLVE_OUT-at-stub01}"
  [ -n "${RESOLVE_OUT-at-stub01}" ] && echo
  exit 0
fi
if [ "$1" = "pr" ] && [ "${2:-}" = "add" ]; then
  if [ "${PR_ADD_EXIT:-0}" != "0" ]; then
    echo "pr add: boom" >&2
    exit "${PR_ADD_EXIT}"
  fi
  echo "pr add: recorded ${4:-} on ${3:-}"
  exit 0
fi
exit 0
SHIM
chmod +x "$FAKE_PLUGIN_ROOT/bin/ateam"

export CLAUDE_PLUGIN_ROOT="$FAKE_PLUGIN_ROOT"
export CALL_LOG
# Guard in the hook requires $ATH/.beads to exist.
export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME/.beads"

PR_URL="https://github.com/mgt-insurance/midgard/pull/5233"
mkdir -p "$T/wt"

# payload TOOL_NAME COMMAND STDOUT [CWD] — build a PostToolUse stdin JSON.
payload() {
  jq -nc \
    --arg tn "$1" --arg cmd "$2" --arg out "$3" --arg cwd "${4:-$T/wt}" \
    '{session_id:"sess-1", tool_name:$tn, tool_input:{command:$cmd}, tool_response:{stdout:$out, stderr:""}, cwd:$cwd}'
}

run() { # STDIN_JSON -> sets RC, OUT
  if OUT=$(printf '%s' "$1" | "$SCRIPT" 2>/dev/null); then RC=0; else RC=$?; fi
}

# ── Case 1: gh pr create with a PR URL -> resolve then pr add, confirm ctx ──
: > "$CALL_LOG"
run "$(payload Bash "gh pr create --fill" "Creating pull request...
${PR_URL}")"
exits0 "case1: exits 0"
logged "^resolve-initiative $T/wt\$"      "case1: resolve-initiative called with cwd"
logged "^pr add at-stub01 ${PR_URL}\$"    "case1: pr add called with resolved id + url"
inCtx  "$PR_URL"                          "case1: additionalContext confirms the PR"

# ── Case 2: not a gh pr create -> no ateam calls at all ─────────────────────
: > "$CALL_LOG"
run "$(payload Bash "gh pr view 5233 --json url" "$PR_URL")"
exits0  "case2: exits 0"
noCalls "case2: no ateam call for a non-create gh command"

# ── Case 3: gh pr create but no PR URL in output (failed create) -> no calls ─
: > "$CALL_LOG"
run "$(payload Bash "gh pr create --fill" "pull request create failed: a pull request already exists")"
exits0  "case3: exits 0"
noCalls "case3: no pr add when output has no PR URL"

# ── Case 4: create + URL but cwd is not a registered worktree -> resolve
# returns empty, no pr add ──────────────────────────────────────────────────
: > "$CALL_LOG"
export RESOLVE_OUT=""
run "$(payload Bash "gh pr create" "$PR_URL")"
unset RESOLVE_OUT
exits0   "case4: exits 0"
logged   "^resolve-initiative " "case4: resolve-initiative attempted"
unlogged "^pr add"              "case4: no pr add when cwd is not an agent-teams worktree"

# ── Case 5: pr add fails -> fail-soft, exit 0, reminder context ──────────────
: > "$CALL_LOG"
export PR_ADD_EXIT=1
run "$(payload Bash "gh pr create" "$PR_URL")"
unset PR_ADD_EXIT
exits0 "case5: exits 0 despite pr add failure (fail-soft)"
logged "^pr add at-stub01 ${PR_URL}\$"        "case5: pr add was attempted"
inCtx  "ateam pr add at-stub01 ${PR_URL}"     "case5: falls back to a manual-registration reminder"

echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
