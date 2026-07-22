#!/usr/bin/env bash
# prime-role-learnings.test.sh — coverage for the UserPromptSubmit
# per-turn role-learnings re-injection hook (agent-teams-7ew5.2.3).
#
# Drives the hook script directly (not through `ateam`) against a temp
# AGENT_TEAMS_HOME, with a fake ateam shim that prints a recognizable
# "LEARNINGS:<role>" string for `ateam learnings <role>` and satisfies the
# dependency guard otherwise.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HOOKS="$ROOT/plugins/agent-teams/hooks/scripts"
SCRIPT="$HOOKS/prime-role-learnings.sh"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

# ── Fake ateam binary — recognizes `learnings <role>` ────────────────────────
FAKE_PLUGIN_ROOT="$T/plugin-root"
mkdir -p "$FAKE_PLUGIN_ROOT/bin"
cat > "$FAKE_PLUGIN_ROOT/bin/ateam" <<'SHIM'
#!/usr/bin/env bash
if [ "$1" = "learnings" ]; then
  echo "LEARNINGS:${2:-unknown}"
  exit 0
fi
echo "no unread mail"
exit 0
SHIM
chmod +x "$FAKE_PLUGIN_ROOT/bin/ateam"

# ── Shared workspace: a real bd DB ───────────────────────────────────────────
export AGENT_TEAMS_HOME="$T/ws"
export CLAUDE_PLUGIN_ROOT="$FAKE_PLUGIN_ROOT"
mkdir -p "$AGENT_TEAMS_HOME"
git -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && bd init --prefix at --non-interactive >/dev/null)

RUN_SCRIPT="$SCRIPT"

run_hook() {
  # run_hook <cwd> <session_id>
  local cwd="$1" sid="$2"
  ( cd "$cwd" && printf '{"session_id":"%s"}' "$sid" | "$RUN_SCRIPT" ) 2>/dev/null
}

# ── Case: dri marker present for session_id -> emits dri learnings ───────────
DRI_SID="dri-sess-0001"
mkdir -p "$AGENT_TEAMS_HOME/dri-sessions"
: > "$AGENT_TEAMS_HOME/dri-sessions/$DRI_SID"
PLAIN_DIR="$T/plain-cwd"
mkdir -p "$PLAIN_DIR"

out=$(run_hook "$PLAIN_DIR" "$DRI_SID")
ctx=$(printf '%s' "$out" | jq -r '.additionalContext // empty' 2>/dev/null || true)
if printf '%s' "$ctx" | grep -q "LEARNINGS:dri"; then
  pass "dri marker -> additionalContext contains dri learnings"
else
  fail "dri marker -> expected LEARNINGS:dri in additionalContext; got: $out"
fi

# ── Case: steward cwd -> emits steward learnings, even with a dri marker for
# the same session_id also present (steward takes precedence) ────────────────
STEWARD_DIR="$AGENT_TEAMS_HOME/steward/session"
mkdir -p "$STEWARD_DIR"
: > "$STEWARD_DIR/.steward-session"

out=$(run_hook "$STEWARD_DIR" "$DRI_SID")
ctx=$(printf '%s' "$out" | jq -r '.additionalContext // empty' 2>/dev/null || true)
if printf '%s' "$ctx" | grep -q "LEARNINGS:steward"; then
  pass "steward cwd -> additionalContext contains steward learnings (precedence over dri marker)"
else
  fail "steward cwd -> expected LEARNINGS:steward in additionalContext; got: $out"
fi

# ── Case: neither steward cwd nor dri marker -> silent no-op ─────────────────
OTHER_SID="no-role-sess-0002"
out=$(run_hook "$PLAIN_DIR" "$OTHER_SID")
if [ -z "$out" ]; then
  pass "no role -> empty stdout"
else
  fail "no role -> expected empty stdout; got: $out"
fi

HOOKS_LOG="$AGENT_TEAMS_HOME/debug/hooks.log"
if awk -F'\t' '$3=="prime-role-learnings.sh" && index($6,"reason=no-role"){f=1} END{exit !f}' "$HOOKS_LOG" 2>/dev/null; then
  pass "no role -> logs reason=no-role"
else
  fail "no role -> expected reason=no-role in hooks.log; tail: $(tail -5 "$HOOKS_LOG" 2>/dev/null)"
fi

# ── Case: ateam/workspace missing -> silent no-op, reason=missing-deps ───────
out=$( (cd "$PLAIN_DIR" && printf '{"session_id":"%s"}' "$DRI_SID" | AGENT_TEAMS_HOME="$T/nope-ws" "$RUN_SCRIPT") 2>/dev/null )
if [ -z "$out" ]; then
  pass "missing workspace -> empty stdout"
else
  fail "missing workspace -> expected empty stdout; got: $out"
fi
NOPE_LOG="$T/nope-ws/debug/hooks.log"
if awk -F'\t' '$3=="prime-role-learnings.sh" && index($6,"reason=missing-deps"){f=1} END{exit !f}' "$NOPE_LOG" 2>/dev/null; then
  pass "missing workspace -> logs reason=missing-deps"
else
  fail "missing workspace -> expected reason=missing-deps in hooks.log; tail: $(tail -5 "$NOPE_LOG" 2>/dev/null)"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
