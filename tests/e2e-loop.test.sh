#!/usr/bin/env bash
# e2e-loop.test.sh — stub-transport round-trip test.
#
# Exercises the full composed pipeline across real process boundaries:
#   ateam notify <id>
#     -> stub.Send records message + returns threadRef
#     -> notify writes label thread:<ref> on the initiative
#   inject a human reply through the stub
#     -> ateam relay
#     -> reverse-maps thread:<ref> to initiative
#     -> exec ateam send <id> --sender human
#   ateam inbox (from initiative worktree)
#     -> shows the human reply
#
# Also asserts the opt-in / no-op path:
#   - AGENT_TEAMS_TRANSPORT unset: ateam relay prints no-op line, exits 0
#   - AGENT_TEAMS_TRANSPORT=stub, no stub config: ateam gate fires no notify
#
# Build: requires -tags e2e for the stub transport.
# Run:   bash tests/e2e-loop.test.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

# ── workspace setup ───────────────────────────────────────────────────────────

export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME"
git -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && bd init --prefix at --non-interactive >/dev/null)

# stub transport dir
export AGENT_TEAMS_STUB_DIR="$T/stub"
mkdir -p "$AGENT_TEAMS_STUB_DIR"

# Initiative worktree (a directory that must exist so ateam inbox can find it).
export INITIATIVE_WT="$T/wt-test"
mkdir -p "$INITIATIVE_WT"

# Determine the current platform (same logic as ateam.test.sh).
PLATFORM_OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
raw_arch="$(uname -m)"
case "$raw_arch" in
    x86_64)  PLATFORM_ARCH=amd64 ;;
    aarch64) PLATFORM_ARCH=arm64 ;;
    arm64)   PLATFORM_ARCH=arm64 ;;
    *)       PLATFORM_ARCH="$raw_arch" ;;
esac

# Build the e2e-tagged binary (includes stub transport).
mkdir -p "$T/bin"
go build -C "$ROOT" -tags e2e -o "$T/bin/ateam-${PLATFORM_OS}-${PLATFORM_ARCH}" ./cmd/ateam
cp "$ROOT/plugins/agent-teams/bin/ateam" "$T/bin/ateam"
chmod +x "$T/bin/ateam-${PLATFORM_OS}-${PLATFORM_ARCH}" "$T/bin/ateam"

# Fake claude shim: makes ateam send's liveness check return "live" so it
# delivers via the doorbell and never escalates to ateam resume. Without this
# the test would spawn a real background claude session against the temp
# worktree — an unsafe side effect that prevents re-runs and breaks CI.
#
# `claude agents --json` returns a single-element JSON array whose cwd matches
# the initiative worktree. Any other claude subcommand is a no-op (exit 0).
#
# AGENT_TEAMS_STUB_WT is read at shim runtime so it resolves after the
# initiative worktree path is known.
cat > "$T/bin/claude" <<'SHIM'
#!/usr/bin/env bash
if [ "${1:-}" = "agents" ] && [ "${2:-}" = "--json" ]; then
  wt="${AGENT_TEAMS_STUB_WT:-}"
  printf '[{"name":"e2e-test-session","cwd":"%s"}]\n' "$wt"
  exit 0
fi
exit 0
SHIM
chmod +x "$T/bin/claude"

export PATH="$T/bin:$PATH"
export AGENT_TEAMS_STUB_WT="$INITIATIVE_WT"

# ── Case 1: opt-in / no-op — no transport configured → relay exits 0 ─────────
# Clear any transport selection; Telegram creds not present → Enabled() = false.
unset AGENT_TEAMS_TRANSPORT 2>/dev/null || true
relay_out=$(ateam relay 2>&1)
echo "$relay_out" | grep -q "messaging not configured" \
  || { echo "FAIL case1: relay without transport config did not print no-op line (got: '$relay_out')"; exit 1; }
echo "case1 PASS: relay no-op when transport not configured"

# ── Case 2: opt-in / no notify — stub enabled=false (no stub dir) fires no notify ─
export AGENT_TEAMS_TRANSPORT=stub
# Temporarily unset stub dir so Enabled() returns false.
unset AGENT_TEAMS_STUB_DIR
printf 'problem: gate-test\nworktree: %s\nbranch: feat/gate-test\n' "$INITIATIVE_WT" > "$T/gate-init.md"
gate_id=$(ateam register --title "Gate Test No Notify" --file "$T/gate-init.md")
printf 'Should we proceed?\n' > "$T/gate-q.txt"
# gate must succeed and NOT attempt notify (would fail without stub dir).
gate_out=$(ateam gate "$gate_id" --file "$T/gate-q.txt" 2>&1)
# There must be no "notify failed" warning in output.
echo "$gate_out" | grep -q "notify failed" \
  && { echo "FAIL case2: gate printed 'notify failed' warning when stub not configured (got: '$gate_out')"; exit 1; }
echo "case2 PASS: gate fires no notify when transport not enabled"
export AGENT_TEAMS_STUB_DIR="$T/stub"
mkdir -p "$AGENT_TEAMS_STUB_DIR"

# ── Case 3: register the test initiative ─────────────────────────────────────
# The initiative body must have a worktree: line so ateam inbox can resolve it.
printf 'problem: e2e loop test\nworktree: %s\nbranch: feat/e2e-loop\nteam: test\nmode: interactive\n' \
  "$INITIATIVE_WT" > "$T/init-body.md"
init_id=$(ateam register --title "E2E Loop Test Initiative" --file "$T/init-body.md")
[ -n "$init_id" ] \
  || { echo "FAIL case3: register returned empty id"; exit 1; }
echo "case3 PASS: initiative registered as $init_id"

# ── Case 4: ateam notify → stub records message, thread:<ref> label written ──
printf 'Human: please review the architecture.\n' > "$T/notify-body.txt"
notify_out=$(ateam notify "$init_id" --file "$T/notify-body.txt" --title "Architecture Review" 2>&1)
echo "$notify_out" | grep -q "thread_ref:" \
  || { echo "FAIL case4: notify output missing thread_ref line (got: '$notify_out')"; exit 1; }

# Extract the returned threadRef from notify output.
thread_ref=$(echo "$notify_out" | grep "^thread_ref: " | sed 's/^thread_ref: //')
[ -n "$thread_ref" ] \
  || { echo "FAIL case4: could not extract thread_ref from: '$notify_out'"; exit 1; }

# Verify stub recorded the outbound message.
[ -f "$AGENT_TEAMS_STUB_DIR/sent.jsonl" ] \
  || { echo "FAIL case4: stub did not write sent.jsonl"; exit 1; }
sent_content=$(jq -r '.thread_ref' "$AGENT_TEAMS_STUB_DIR/sent.jsonl")
[ "$sent_content" = "$thread_ref" ] \
  || { echo "FAIL case4: sent.jsonl thread_ref '$sent_content' != notify output '$thread_ref'"; exit 1; }

# Verify thread:<ref> label was written on the initiative.
labels_out=$(ateam show "$init_id")
echo "$labels_out" | grep -q "thread:$thread_ref" \
  || { echo "FAIL case4: thread:$thread_ref label not found on initiative (show: '$labels_out')"; exit 1; }

echo "case4 PASS: notify sent via stub, thread_ref=$thread_ref, label written"

# ── Case 5: inject a human reply and run ateam relay ─────────────────────────
reply_text="Looks good — proceed with the plan."
printf '{"thread_ref": "%s", "text": "%s"}\n' "$thread_ref" "$reply_text" \
  > "$AGENT_TEAMS_STUB_DIR/reply-001.json"

# Capture the real claude session list before relay so we can prove no new
# sessions are spawned. The fake claude shim on PATH intercepts ateam send's
# internal liveness call, but we want the REAL agent count for the before/after
# assertion — so we call the real claude (bypassing our shim) via its full path.
real_claude="$(PATH="${PATH#"$T/bin:"}" command -v claude 2>/dev/null || echo "")"
sessions_before=""
if [ -n "$real_claude" ]; then
  sessions_before="$("$real_claude" agents --json 2>/dev/null || echo "[]")"
else
  sessions_before="[]"
fi
echo "sessions before relay: $sessions_before"

# Run relay. It calls stub.Receive which drains reply files and executes
# ateam send <init_id> --sender human for each one. The fake claude shim
# makes ateam send's hasLiveSession return true (cwd matches $INITIATIVE_WT),
# so send delivers via the doorbell and never escalates to ateam resume.
relay_out=$(ateam relay 2>&1)
echo "relay output: $relay_out"

# Capture session list after relay and assert count did not increase.
sessions_after=""
if [ -n "$real_claude" ]; then
  sessions_after="$("$real_claude" agents --json 2>/dev/null || echo "[]")"
else
  sessions_after="[]"
fi
echo "sessions after relay: $sessions_after"

count_before=$(echo "$sessions_before" | jq 'length' 2>/dev/null || echo 0)
count_after=$(echo "$sessions_after" | jq 'length' 2>/dev/null || echo 0)
[ "$count_after" -le "$count_before" ] \
  || { echo "FAIL case5: relay spawned a background claude session (before=$count_before after=$count_after)"; exit 1; }
echo "sessions before=$count_before after=$count_after — no orphan spawned"

# Verify relay found and processed the reply.
echo "$relay_out" | grep -q "starting on transport" \
  || { echo "FAIL case5: relay did not print starting line (got: '$relay_out')"; exit 1; }

# The reply file should have been consumed (removed) by stub.Receive.
[ ! -f "$AGENT_TEAMS_STUB_DIR/reply-001.json" ] \
  || { echo "FAIL case5: stub did not consume reply-001.json after relay"; exit 1; }

echo "case5 PASS: relay processed reply, file consumed, no session spawned"

# ── Case 6: ateam inbox (from initiative worktree) shows the human reply ─────
# inbox resolves initiative via cwd matching worktree: line.
inbox_out=$(cd "$INITIATIVE_WT" && ateam inbox 2>&1)
echo "inbox output: $inbox_out"

echo "$inbox_out" | grep -q "$reply_text" \
  || { echo "FAIL case6: inbox did not show reply text '$reply_text' (got: '$inbox_out')"; exit 1; }
echo "$inbox_out" | grep -qi "from: human" \
  || { echo "FAIL case6: inbox did not show sender 'human' (got: '$inbox_out')"; exit 1; }

echo "case6 PASS: inbox shows the human reply — LOOP CLOSED"

# ── Case 7: second inbox call → messages marked read, no duplication ─────────
inbox2_out=$(cd "$INITIATIVE_WT" && ateam inbox 2>&1)
echo "$inbox2_out" | grep -q "no unread mail" \
  || { echo "FAIL case7: second inbox call did not return 'no unread mail' (got: '$inbox2_out')"; exit 1; }
echo "case7 PASS: messages marked read after first inbox drain"

echo ""
echo "ALL CASES PASSED — stub-transport e2e round trip complete."
