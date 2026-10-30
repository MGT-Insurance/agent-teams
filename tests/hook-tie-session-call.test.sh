#!/usr/bin/env bash
# hook-tie-session-call.test.sh — session-start-tie.sh's self-registration
# call (agent-teams-zalv.3, at-ps11): the async SessionStart hook must call
# `ateam tie-session --session-id "$HOOK_SESSION_ID"` fail-soft and print
# nothing to stdout, without depending on the real `ateam` binary or a live bd
# workspace for THIS assertion — the tie-session verb's own no-op/idempotent/
# warn semantics are covered by Go tests in internal/verbs/tie_session_test.go.
# This test only checks the shell wiring: the call happens with the right args,
# a failing/erroring `ateam tie-session` never breaks the hook, and the
# blocking session-start-inbox.sh never calls tie-session.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/session-start-tie.sh"
INBOX_SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/session-start-inbox.sh"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

# ── Fake ateam binary: records every invocation's args, can be told to fail
# on tie-session specifically (TIE_SESSION_EXIT) to exercise fail-soft, and
# can be told to print output on tie-session (TIE_SESSION_OUTPUT) to exercise
# the hook's capture-and-log-non-empty-output path (agent-teams-zalv.14).
FAKE_PLUGIN_ROOT="$T/plugin-root"
mkdir -p "$FAKE_PLUGIN_ROOT/bin"
CALL_LOG="$T/ateam-calls.log"
: > "$CALL_LOG"
cat > "$FAKE_PLUGIN_ROOT/bin/ateam" <<'SHIM'
#!/usr/bin/env bash
echo "$@" >> "$CALL_LOG"
if [ "$1" = "tie-session" ]; then
  if [ "${TIE_SESSION_EXIT:-0}" != "0" ]; then
    exit "${TIE_SESSION_EXIT}"
  fi
  if [ -n "${TIE_SESSION_OUTPUT:-}" ]; then
    echo "${TIE_SESSION_OUTPUT}" >&2
  fi
  exit 0
fi
if [ "$1" = "hook-scan" ]; then
  printf 'id: at-stub01\nunread: 0\n'
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

# Cases 1-4 assert the no-ATEAM_INITIATIVE call shape (no positional arg).
# Unset unconditionally: this test may itself run inside an agent-teams
# session that already has ATEAM_INITIATIVE set in its own environment
# (agent-teams-rjh1.2), which would otherwise leak into the child $SCRIPT
# invocations below and falsely fail cases 1-4.
unset ATEAM_INITIATIVE || true

HOOKS_LOG="$AGENT_TEAMS_HOME/debug/hooks.log"

# ── Case 1: normal session start -> tie-session called with the hook's
# session id, the script exits 0, and stdout stays empty ────────────────────
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
if [ -z "$out" ]; then
  pass "case1: no stdout"
else
  fail "case1: expected empty stdout, got: $out"
fi

# ── Case 2: ateam tie-session fails -> hook is fail-soft, still exits 0 with
# empty stdout ──────────────────────────────────────────────────────────────
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
  pass "case2: ateam tie-session was called"
else
  fail "case2: expected tie-session call not found in log: $(cat "$CALL_LOG")"
fi
if [ -z "$out2" ]; then
  pass "case2: no stdout when tie-session errors"
else
  fail "case2: expected empty stdout, got: $out2"
fi

# ── Case 3: stdin has no parseable .session_id -> HOOK_SESSION_ID resolves
# to the "unknown" sentinel. The hook skips tie-session entirely and logs the
# no-session-id exit reason ─────────────────────────────────────────────────
: > "$CALL_LOG"
rm -f "$HOOKS_LOG"
if out3=$(cd "$T/wt" && printf 'not-json' | "$SCRIPT"); then
  rc3=0
else
  rc3=$?
fi
if [ "$rc3" -ne 0 ]; then
  fail "case3: script exited $rc3, want 0"
else
  pass "case3: script exits 0 with no .session_id on stdin"
fi
if grep -q '^tie-session' "$CALL_LOG"; then
  fail "case3: tie-session must not be called without a session id: $(cat "$CALL_LOG")"
else
  pass "case3: tie-session not called for the unknown sentinel"
fi
if [ -f "$HOOKS_LOG" ] && grep -q 'no-session-id' "$HOOKS_LOG"; then
  pass "case3: exit reason no-session-id logged"
else
  fail "case3: expected no-session-id in hook log: $(cat "$HOOKS_LOG" 2>/dev/null || echo MISSING)"
fi

# ── Case 4: tie-session emits a cross-open-initiative conflict warning
# (or any non-empty output) -> the hook must capture it and route it through
# hook_log_note instead of swallowing it, and still print nothing to stdout ─
: > "$CALL_LOG"
rm -f "$HOOKS_LOG"
export TIE_SESSION_OUTPUT="ateam tie-session: session sess-conflict already tied to at-other (open) -- not re-tying"
if out4=$(cd "$T/wt" && echo '{"session_id":"sess-conflict"}' | "$SCRIPT"); then
  rc4=0
else
  rc4=$?
fi
unset TIE_SESSION_OUTPUT
if [ "$rc4" -ne 0 ]; then
  fail "case4: script exited $rc4, want 0"
else
  pass "case4: script exits 0 when tie-session emits a warning"
fi
if [ -f "$HOOKS_LOG" ] && grep -q 'tie-session: ateam tie-session: session sess-conflict already tied to at-other' "$HOOKS_LOG"; then
  pass "case4: tie-session's conflict warning ended up in the structured hook log"
else
  fail "case4: expected warning not found in hook log: $(cat "$HOOKS_LOG" 2>/dev/null || echo MISSING)"
fi
if [ -z "$out4" ]; then
  pass "case4: warning is not printed to stdout"
else
  fail "case4: expected empty stdout, got: $out4"
fi

# ── Case 5: ATEAM_INITIATIVE set (launcher-published, agent-teams-rjh1.2)
# -> tie-session is called with that id as the positional initiative-id arg,
# ahead of --session-id, so the tie no longer depends on cwd resolution ────
: > "$CALL_LOG"
export ATEAM_INITIATIVE="at-launched01"
if out5=$(cd "$T/wt" && echo '{"session_id":"sess-withinit"}' | "$SCRIPT"); then
  rc5=0
else
  rc5=$?
fi
unset ATEAM_INITIATIVE
if [ "$rc5" -ne 0 ]; then
  fail "case5: script exited $rc5, want 0"
else
  pass "case5: script exits 0 when ATEAM_INITIATIVE is set"
fi
if grep -q '^tie-session at-launched01 --session-id sess-withinit$' "$CALL_LOG"; then
  pass "case5: ateam tie-session called with ATEAM_INITIATIVE as the positional arg"
else
  fail "case5: expected tie-session call not found in log: $(cat "$CALL_LOG")"
fi

# ── Case 6: ATEAM_INITIATIVE unset (the common case) -> no positional arg —
# guards against a regression that always adds a (possibly empty) positional ─
: > "$CALL_LOG"
if out6=$(cd "$T/wt" && echo '{"session_id":"sess-noinit"}' | "$SCRIPT"); then
  rc6=0
else
  rc6=$?
fi
if [ "$rc6" -ne 0 ]; then
  fail "case6: script exited $rc6, want 0"
else
  pass "case6: script exits 0 when ATEAM_INITIATIVE is unset"
fi
if grep -q '^tie-session --session-id sess-noinit$' "$CALL_LOG"; then
  pass "case6: ateam tie-session called with no positional arg when ATEAM_INITIATIVE is unset"
else
  fail "case6: expected tie-session call not found in log: $(cat "$CALL_LOG")"
fi

# ── Case 7: the blocking session-start-inbox.sh never invokes tie-session ───
: > "$CALL_LOG"
if (cd "$T/wt" && echo '{"session_id":"sess-inbox"}' | "$INBOX_SCRIPT" >/dev/null); then
  pass "case7: session-start-inbox.sh exits 0"
else
  fail "case7: session-start-inbox.sh exited non-zero"
fi
if grep -q 'tie-session' "$CALL_LOG"; then
  fail "case7: session-start-inbox.sh invoked tie-session: $(cat "$CALL_LOG")"
else
  pass "case7: session-start-inbox.sh never invokes tie-session"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
