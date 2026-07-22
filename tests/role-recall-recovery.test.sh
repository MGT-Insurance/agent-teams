#!/usr/bin/env bash
# role-recall-recovery.test.sh — coverage for the
# SessionStart(clear|compact) role learnings + steward ledger context hook
# (agent-teams-7ew5.2.4; narrowed from compact-only to clear|compact — the
# only two SessionStart reasons that both wipe learnings from context and
# don't re-run the skill prose that would otherwise reload them — and
# absorbing the deleted prime-role-learnings.sh's coverage, by
# agent-teams-7ew5.2.8).
#
# Drives the hook script directly against a temp AGENT_TEAMS_HOME, with a
# fake ateam shim recognizing `learnings <role>`, `steward ledger stats`, and
# `steward ledger recall <cat>` — returning distinct canned strings per
# category, one exactly "no ledger entries" (must be filtered out).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HOOKS="$ROOT/plugins/agent-teams/hooks/scripts"
SCRIPT="$HOOKS/role-recall-recovery.sh"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

# ── Fake ateam binary ─────────────────────────────────────────────────────────
FAKE_PLUGIN_ROOT="$T/plugin-root"
mkdir -p "$FAKE_PLUGIN_ROOT/bin"
cat > "$FAKE_PLUGIN_ROOT/bin/ateam" <<'SHIM'
#!/usr/bin/env bash
if [ "$1" = "learnings" ]; then
  echo "LEARNINGS:${2:-unknown}"
  exit 0
fi
if [ "$1" = "steward" ] && [ "$2" = "ledger" ] && [ "$3" = "stats" ]; then
  echo "LEDGER-STATS:aggregate"
  exit 0
fi
if [ "$1" = "steward" ] && [ "$2" = "ledger" ] && [ "$3" = "recall" ]; then
  case "$4" in
    plan-approval)
      echo "no ledger entries"
      ;;
    scope-call)
      echo "RECALL:scope-call"
      ;;
    merge-approval)
      echo "no ledger entries"
      ;;
    design-fork)
      echo "RECALL:design-fork"
      ;;
    unblock-action)
      echo "no ledger entries"
      ;;
    *)
      echo "no ledger entries"
      ;;
  esac
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

run_hook() {
  # run_hook <cwd> <session_id>
  local cwd="$1" sid="$2"
  ( cd "$cwd" && printf '{"session_id":"%s"}' "$sid" | "$SCRIPT" ) 2>/dev/null
}

run_hook_reason() {
  # run_hook_reason <cwd> <session_id> <reason> — mirrors the extra "source"
  # field Claude Code includes on SessionStart stdin; the script itself never
  # reads it (reason-independent), so this only proves output is unaffected.
  local cwd="$1" sid="$2" reason="$3"
  ( cd "$cwd" && printf '{"session_id":"%s","source":"%s"}' "$sid" "$reason" | "$SCRIPT" ) 2>/dev/null
}

# ── Case: dri marker present -> dri learnings only, no ledger content ────────
DRI_SID="dri-sess-0001"
mkdir -p "$AGENT_TEAMS_HOME/dri-sessions"
: > "$AGENT_TEAMS_HOME/dri-sessions/$DRI_SID"
PLAIN_DIR="$T/plain-cwd"
mkdir -p "$PLAIN_DIR"

out=$(run_hook "$PLAIN_DIR" "$DRI_SID")
if printf '%s' "$out" | grep -q "LEARNINGS:dri"; then
  pass "dri marker -> stdout contains dri learnings"
else
  fail "dri marker -> expected LEARNINGS:dri in stdout; got: $out"
fi
if printf '%s' "$out" | grep -q "LEDGER-STATS\|RECALL:"; then
  fail "dri marker -> ledger content leaked into dri output"
else
  pass "dri marker -> no ledger content in dri output"
fi

# ── Case: steward cwd -> steward learnings + ledger stats + non-empty
# category recalls only (no "no ledger entries" leakage) ────────────────────
STEWARD_DIR="$AGENT_TEAMS_HOME/steward/session"
mkdir -p "$STEWARD_DIR"
: > "$STEWARD_DIR/.steward-session"

out=$(run_hook "$STEWARD_DIR" "no-session-needed")
if printf '%s' "$out" | grep -q "LEARNINGS:steward"; then
  pass "steward cwd -> stdout contains steward learnings"
else
  fail "steward cwd -> expected LEARNINGS:steward in stdout; got: $out"
fi
if printf '%s' "$out" | grep -q "LEDGER-STATS:aggregate"; then
  pass "steward cwd -> stdout contains ledger stats"
else
  fail "steward cwd -> expected LEDGER-STATS:aggregate in stdout; got: $out"
fi
if printf '%s' "$out" | grep -q "RECALL:scope-call" && printf '%s' "$out" | grep -q "RECALL:design-fork"; then
  pass "steward cwd -> stdout contains non-empty category recalls"
else
  fail "steward cwd -> expected RECALL:scope-call and RECALL:design-fork in stdout; got: $out"
fi
if printf '%s' "$out" | grep -q "no ledger entries"; then
  fail "steward cwd -> 'no ledger entries' leaked into output (empty categories not filtered)"
else
  pass "steward cwd -> empty categories filtered (no 'no ledger entries' in output)"
fi

# ── Case: neither dri marker nor steward cwd -> empty stdout, reason=no-role ─
out=$(run_hook "$PLAIN_DIR" "no-role-sess-0002")
if [ -z "$out" ]; then
  pass "no role -> empty stdout"
else
  fail "no role -> expected empty stdout; got: $out"
fi

HOOKS_LOG="$AGENT_TEAMS_HOME/debug/hooks.log"
if awk -F'\t' '$3=="role-recall-recovery.sh" && index($6,"reason=no-role"){f=1} END{exit !f}' "$HOOKS_LOG" 2>/dev/null; then
  pass "no role -> logs reason=no-role"
else
  fail "no role -> expected reason=no-role in hooks.log; tail: $(tail -5 "$HOOKS_LOG" 2>/dev/null)"
fi

# ── Case: malformed/path-traversal session_id -> silent no-op (treated as no
# role, never an error — valid_session_id rejects it before any marker path
# is built) ────────────────────────────────────────────────────────────────
TRAVERSAL_SID="../../etc/passwd"
out=$(run_hook "$PLAIN_DIR" "$TRAVERSAL_SID")
if [ -z "$out" ]; then
  pass "path-traversal session_id -> empty stdout"
else
  fail "path-traversal session_id -> expected empty stdout; got: $out"
fi
if [ -e "$AGENT_TEAMS_HOME/dri-sessions/$TRAVERSAL_SID" ]; then
  fail "path-traversal session_id -> unexpectedly created a marker path"
else
  pass "path-traversal session_id -> zero side effects (no marker path created)"
fi

# ── Case: ateam/workspace missing -> silent no-op, reason=missing-deps ───────
out=$( (cd "$PLAIN_DIR" && printf '{"session_id":"%s"}' "$DRI_SID" | AGENT_TEAMS_HOME="$T/nope-ws" "$SCRIPT") 2>/dev/null )
if [ -z "$out" ]; then
  pass "missing workspace -> empty stdout"
else
  fail "missing workspace -> expected empty stdout; got: $out"
fi
NOPE_LOG="$T/nope-ws/debug/hooks.log"
if awk -F'\t' '$3=="role-recall-recovery.sh" && index($6,"reason=missing-deps"){f=1} END{exit !f}' "$NOPE_LOG" 2>/dev/null; then
  pass "missing workspace -> logs reason=missing-deps"
else
  fail "missing workspace -> expected reason=missing-deps in hooks.log; tail: $(tail -5 "$NOPE_LOG" 2>/dev/null)"
fi

# ── Case: reason-independence — identical dri-role output across both
# matched SessionStart reasons (clear, compact) ──────────────────────────────
out_clear=$(run_hook_reason "$PLAIN_DIR" "$DRI_SID" "clear")
out_compact=$(run_hook_reason "$PLAIN_DIR" "$DRI_SID" "compact")

all_have_dri=true
for o in "$out_clear" "$out_compact"; do
  printf '%s' "$o" | grep -q "LEARNINGS:dri" || all_have_dri=false
done
if [ "$all_have_dri" = "true" ]; then
  pass "dri role -> LEARNINGS:dri present under clear/compact reasons alike"
else
  fail "dri role -> expected LEARNINGS:dri under every reason; got clear=[$out_clear] compact=[$out_compact]"
fi

if [ "$out_clear" = "$out_compact" ]; then
  pass "dri role -> byte-identical output regardless of reason (clear vs compact)"
else
  fail "dri role -> output differs by reason; clear=[$out_clear] compact=[$out_compact]"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
