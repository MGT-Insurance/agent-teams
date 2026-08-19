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
if [ "$1" = "resolve-initiative" ]; then
  # The hook resolves cwd -> initiative id here before reaching the mail peek
  # (agent-teams-ully.9). A stub id keeps this test's stated scope — shell
  # wiring only, no dependence on real resolution, which is covered by
  # tests/hook-compact-recovery.test.sh and internal/verbs/match_test.go.
  echo "at-stub01"
  exit 0
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

# Cases 1-4 assert the no-ATEAM_INITIATIVE call shape (no positional arg).
# Unset unconditionally: this test may itself run inside an agent-teams
# session that already has ATEAM_INITIATIVE set in its own environment
# (agent-teams-rjh1.2), which would otherwise leak into the child $SCRIPT
# invocations below and falsely fail cases 1-4.
unset ATEAM_INITIATIVE || true

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

# ── Case 3: stdin has no parseable .session_id -> HOOK_SESSION_ID resolves
# to the "unknown" sentinel (line 16's fallback; jq's own exit-0-empty-output
# behavior on a truly empty pipe means garbage/unparseable stdin is what
# actually exercises the `|| echo "unknown"` fallback here, not an empty
# pipe). The hook still calls tie-session unconditionally (the no-op guard
# for "unknown" lives in the verb — see internal/verbs/tie_session_test.go —
# not in the hook), and since the stub produces no output for this call
# (mirroring the verb's real silent no-op), nothing gets logged ────────────
: > "$CALL_LOG"
HOOKS_LOG="$AGENT_TEAMS_HOME/debug/hooks.log"
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
if grep -q '^tie-session --session-id unknown$' "$CALL_LOG"; then
  pass "case3: hook calls tie-session with the unknown sentinel (verb-level no-op is not this test's concern)"
else
  fail "case3: expected tie-session call not found in log: $(cat "$CALL_LOG")"
fi
if [ -f "$HOOKS_LOG" ] && grep -q 'tie-session:' "$HOOKS_LOG"; then
  fail "case3: expected no tie-session note logged for a silent no-op, found: $(grep 'tie-session:' "$HOOKS_LOG")"
else
  pass "case3: no tie-session note logged when the call produced no output"
fi

# ── Case 4: tie-session emits a cross-open-initiative conflict warning ─────
# (or any non-empty output) -> the hook must capture it and route it through
# hook_log_note instead of swallowing it via >/dev/null ────────────────────
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

# ── Case 5: ATEAM_INITIATIVE set (launcher-published, agent-teams-rjh1.2) ──
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

# ── Case 6: ATEAM_INITIATIVE unset (the common case) -> no positional arg,
# exactly cases 1-4's shape — guards against a regression that always adds
# a (possibly empty) positional ────────────────────────────────────────────
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

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
