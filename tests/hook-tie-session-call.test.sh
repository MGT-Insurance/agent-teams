#!/usr/bin/env bash
# hook-tie-session-call.test.sh — session-start-inbox.sh's self-registration
# call (agent-teams-zalv.3, at-ps11): the hook must call
# `ateam tie-session --session-id "$HOOK_SESSION_ID"` fail-soft, without
# depending on the real `ateam` binary or a live bd workspace for THIS
# assertion — the tie-session verb's own no-op/idempotent/warn semantics are
# covered by Go tests in internal/verbs/tie_session_test.go. This test only
# checks the shell wiring: the call happens with the right args, and a
# failing/erroring `ateam tie-session` never breaks the hook.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/session-start-inbox.sh"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

# ── Fake ateam binary: records every invocation's args, and can be told to
# fail on tie-session specifically (TIE_SESSION_EXIT) to exercise fail-soft.
FAKE_PLUGIN_ROOT="$T/plugin-root"
mkdir -p "$FAKE_PLUGIN_ROOT/bin"
CALL_LOG="$T/ateam-calls.log"
: > "$CALL_LOG"
cat > "$FAKE_PLUGIN_ROOT/bin/ateam" <<'SHIM'
#!/usr/bin/env bash
echo "$@" >> "$CALL_LOG"
if [ "$1" = "tie-session" ] && [ "${TIE_SESSION_EXIT:-0}" != "0" ]; then
  exit "${TIE_SESSION_EXIT}"
fi
if [ "$1" = "mail" ] && [ "${2:-}" = "inbox" ]; then
  echo "no unread mail"
  exit 0
fi
exit 0
SHIM
chmod +x "$FAKE_PLUGIN_ROOT/bin/ateam"

export CLAUDE_PLUGIN_ROOT="$FAKE_PLUGIN_ROOT"
export CALL_LOG

# ── Real bd workspace with one open initiative registered at $T/wt ──────────
export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME" "$T/wt"
git -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && bd init --prefix at --non-interactive >/dev/null)
printf 'problem: test problem\nrepo: %s\nworktree: %s\nbranch: feat/x\nteam: test-team\nmode: interactive\n' "$T/wt" "$T/wt" > "$T/body.md"
bd -C "$AGENT_TEAMS_HOME" create --title="Hook test initiative" --type=task --priority=2 --body-file="$T/body.md" >/dev/null

# ── Case 1: normal session start -> tie-session called with the hook's
# session id, and the script still exits 0 ─────────────────────────────────
: > "$CALL_LOG"
if out=$(cd "$T/wt" && echo '{"session_id":"sess-abc123"}' | "$SCRIPT"); then
  rc=0
else
  rc=$?
fi
if [ "$rc" -ne 0 ]; then
  fail "case1: script exited $rc, want 0"
else
  pass "case1: script exited 0"
fi
if grep -q '^tie-session --session-id sess-abc123$' "$CALL_LOG"; then
  pass "case1: ateam tie-session called with the hook session id"
else
  fail "case1: expected tie-session call not found in log: $(cat "$CALL_LOG")"
fi

# ── Case 2: ateam tie-session fails -> hook is fail-soft, still exits 0 and
# still gets to the mail-peek call ──────────────────────────────────────────
: > "$CALL_LOG"
export TIE_SESSION_EXIT=1
if out2=$(cd "$T/wt" && echo '{"session_id":"sess-fail"}' | "$SCRIPT"); then
  rc2=0
else
  rc2=$?
fi
unset TIE_SESSION_EXIT
if [ "$rc2" -ne 0 ]; then
  fail "case2: script exited $rc2 when ateam tie-session errored, want 0 (fail-soft)"
else
  pass "case2: script still exits 0 when ateam tie-session errors"
fi
if grep -q '^tie-session --session-id sess-fail$' "$CALL_LOG"; then
  pass "case2: ateam tie-session was still called despite prior/next-run failure"
else
  fail "case2: expected tie-session call not found in log: $(cat "$CALL_LOG")"
fi
if grep -q '^mail inbox --peek$' "$CALL_LOG"; then
  pass "case2: hook continued past the failing tie-session call to the mail-peek call"
else
  fail "case2: hook did not reach the mail-peek call after tie-session errored: $(cat "$CALL_LOG")"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
