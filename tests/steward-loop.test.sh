#!/usr/bin/env bash
# steward-loop.test.sh — stub-transport round-trip test for the full Steward
# loop (agent-teams-e3mq.6, LOOP Track F). Modeled on tests/e2e-loop.test.sh
# (same isolation pattern: temp AGENT_TEAMS_HOME, scratch bd db, stub
# transport, fake claude shim for liveness checks) — extended with the
# Gate -> Steward -> Relay -> Steward -> DRI hop that Tracks A/B/C added on
# top of the plain notify/relay loop e2e-loop.test.sh already covers.
#
# Exercises, across real process boundaries:
#   1. ateam register (fake initiative) + ateam steward init (session dir/marker)
#   2. ateam gate --kind=review -> notifyToSteward wraps the ask in a
#      Gate->Steward envelope, delivered via `ateam mail send` to the
#      reserved "steward" handle; mailbox/steward.wake touched.
#   3. ateam mail inbox (cwd = steward session dir) drains the envelope and
#      marks it read (exercises the e3mq.15 marker-branch fix in
#      resolveInboxRecipient/isStewardSession).
#   4. ateam notify <id> (skill digest step is STUBBED — this harness composes
#      the digest itself) -> stub transport records it, thread:<ref> label
#      written on the initiative.
#   5. A simulated Eric reply is dropped into the stub's reply-*.json seam;
#      ateam relay reverse-maps thread:<ref> -> initiative and hands the
#      Steward a Relay->Steward envelope via `ateam mail send steward`.
#   6. ateam mail send <initiative-id> --sender steward answers the DRI;
#      the initiative's own inbox (cwd = its worktree) receives it.
#   7. ateam steward ledger record / stats round-trips one decision.
#   8. ateam audit stays clean throughout — steward mail beads are
#      type=message (infra), excluded from audit's `bd list --all --json`
#      (no --include-infra), same invariant Track A verified.
#
# Build: requires -tags e2e for the stub transport (same as e2e-loop.test.sh).
# Run:   bash tests/steward-loop.test.sh
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
export AGENT_TEAMS_TRANSPORT=stub

# Initiative worktree (a directory that must exist so ateam inbox/mail send
# can find it).
export INITIATIVE_WT="$T/wt-test"
mkdir -p "$INITIATIVE_WT"

# Determine the current platform (same logic as ateam.test.sh / e2e-loop.test.sh).
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

# Fake claude shim: makes ateam mail send's liveness check return "live" so it
# delivers via the doorbell and never escalates to ateam resume. Without this
# the test would spawn a real background claude session against the temp
# worktree — an unsafe side effect that prevents re-runs and breaks CI.
# Only step 6 (`ateam mail send <initiative-id> ...`) exercises this path:
# steward-addressed sends (gate's notifyToSteward, relay's reply hand-off)
# resolve "steward" via recipientWorktree's bd-show lookup, which fails
# (steward is not a bd initiative bead) and short-circuits to "skipping
# liveness check" before agentsFunc is ever called — see steward_route.go /
# messaging.go's sendKong.Run.
cat > "$T/bin/claude" <<'SHIM'
#!/usr/bin/env bash
if [ "${1:-}" = "agents" ] && [ "${2:-}" = "--json" -o "${3:-}" = "--json" ]; then
  wt="${AGENT_TEAMS_STUB_WT:-}"
  printf '[{"name":"e2e-test-session","cwd":"%s"}]\n' "$wt"
  exit 0
fi
exit 0
SHIM
chmod +x "$T/bin/claude"

export PATH="$T/bin:$PATH"
export AGENT_TEAMS_STUB_WT="$INITIATIVE_WT"

fail() { echo "FAIL $1: $2"; exit 1; }

# ── Case 1: register initiative + steward init ───────────────────────────────

printf 'problem: steward loop test\nworktree: %s\nbranch: feat/steward-loop\nteam: test\nmode: interactive\n' \
  "$INITIATIVE_WT" > "$T/init-body.md"
init_id=$(ateam register --title "Steward Loop Test Initiative" --file "$T/init-body.md")
[ -n "$init_id" ] || fail case1 "register returned empty id"

# ── Case 1a: gate BEFORE steward init -> marker guard no-ops (agent-teams-e3mq.24) ──
# No steward session/marker exists yet on this machine. notifyToSteward must
# no-op silently (no message bead, no "notify failed" warning) and the gate
# itself must still succeed.

printf 'Should we bump the minor version?\n' > "$T/gate-ask-premarker.txt"
premarker_gate_out=$(ateam gate "$init_id" --file "$T/gate-ask-premarker.txt" --kind=question 2>&1)
echo "$premarker_gate_out" | grep -q "notify failed" \
  && fail case1a "gate printed 'notify failed' warning with no steward marker (got: '$premarker_gate_out')"

premarker_msgs=$(bd -C "$AGENT_TEAMS_HOME" list --include-infra --assignee=steward --status=open --json)
[ "$(echo "$premarker_msgs" | jq 'length')" = "0" ] \
  || fail case1a "expected 0 steward messages before steward init, got: $premarker_msgs"

echo "case1a PASS: gate with no steward marker created no steward message and gate still succeeded"

steward_dir=$(ateam steward init)
[ -n "$steward_dir" ] || fail case1 "steward init returned empty session dir"
[ -f "$steward_dir/.steward-session" ] || fail case1 "steward session marker not created at $steward_dir/.steward-session"

echo "case1 PASS: initiative registered as $init_id, steward session at $steward_dir"

# ── Case 2: DRI raises a gate -> Gate->Steward envelope, doorbell touched ────

printf 'Should we ship the v2 release this week?\n' > "$T/gate-ask.txt"
gate_out=$(ateam gate "$init_id" --file "$T/gate-ask.txt" --kind=review 2>&1)
echo "$gate_out" | grep -q "notify failed" \
  && fail case2 "gate printed 'notify failed' warning (got: '$gate_out')"

doorbell="$AGENT_TEAMS_HOME/mailbox/steward.wake"
[ -f "$doorbell" ] || fail case2 "mailbox/steward.wake not touched (expected $doorbell)"

gate_msgs=$(bd -C "$AGENT_TEAMS_HOME" list --include-infra --assignee=steward --status=open --json)
gate_msg_count=$(echo "$gate_msgs" | jq 'length')
[ "$gate_msg_count" = "1" ] || fail case2 "expected 1 open message bead assigned to steward, got $gate_msg_count (raw: $gate_msgs)"

gate_msg_type=$(echo "$gate_msgs" | jq -r '.[0].issue_type')
[ "$gate_msg_type" = "message" ] || fail case2 "message bead issue_type = '$gate_msg_type', want 'message'"

gate_msg_body=$(echo "$gate_msgs" | jq -r '.[0].description')
echo "$gate_msg_body" | grep -qF "<<<steward-gate initiative:$init_id kind:review>>>" \
  || fail case2 "message body missing steward-gate envelope header (got: '$gate_msg_body')"
echo "$gate_msg_body" | grep -qF "Should we ship the v2 release this week?" \
  || fail case2 "message body missing ask text (got: '$gate_msg_body')"
echo "$gate_msg_body" | tail -1 | grep -qF ">>>" \
  || fail case2 "message body missing closing sentinel (got: '$gate_msg_body')"

# audit must stay clean right after the gate creates a message bead.
audit_out=$(ateam audit 2>&1)
echo "$audit_out" | grep -q "audit: clean" \
  || fail case2 "ateam audit not clean after gate (got: '$audit_out')"

echo "case2 PASS: gate routed steward-gate envelope (kind:review), doorbell touched, audit clean"

# ── Case 3: steward drains its inbox -> envelope delivered + marked read ─────

inbox_out=$(cd "$steward_dir" && ateam mail inbox 2>&1)
echo "$inbox_out" | grep -qF "<<<steward-gate initiative:$init_id kind:review>>>" \
  || fail case3 "steward inbox did not show the gate envelope (got: '$inbox_out')"
echo "$inbox_out" | grep -qi "from: gate" \
  || fail case3 "steward inbox did not show sender 'gate' (got: '$inbox_out')"

# Second drain: message now closed/read, no duplication.
inbox2_out=$(cd "$steward_dir" && ateam mail inbox 2>&1)
echo "$inbox2_out" | grep -q "no unread mail" \
  || fail case3 "second steward inbox drain did not return 'no unread mail' (got: '$inbox2_out')"

gate_msgs_after=$(bd -C "$AGENT_TEAMS_HOME" list --include-infra --assignee=steward --status=open --json)
[ "$(echo "$gate_msgs_after" | jq 'length')" = "0" ] \
  || fail case3 "expected 0 open messages assigned to steward after drain (got: '$gate_msgs_after')"

echo "case3 PASS: steward inbox drained the gate envelope, marked read, no duplication"

# ── Case 4: skill digest step (STUBBED) -> ateam notify -> stub captures it ──

printf 'Digest: DRI is on track; gate resolved, waiting on plan approval.\n' > "$T/digest.txt"
notify_out=$(ateam notify "$init_id" --file "$T/digest.txt" --title "Steward Digest" 2>&1)
echo "$notify_out" | grep -q "thread_ref:" \
  || fail case4 "notify output missing thread_ref line (got: '$notify_out')"

thread_ref=$(echo "$notify_out" | grep "^thread_ref: " | sed 's/^thread_ref: //')
[ -n "$thread_ref" ] || fail case4 "could not extract thread_ref from: '$notify_out'"

[ -f "$AGENT_TEAMS_STUB_DIR/sent.jsonl" ] || fail case4 "stub did not write sent.jsonl"
sent_content=$(jq -r 'select(.initiative_id == "'"$init_id"'") | .thread_ref' "$AGENT_TEAMS_STUB_DIR/sent.jsonl")
[ "$sent_content" = "$thread_ref" ] \
  || fail case4 "sent.jsonl thread_ref '$sent_content' != notify output '$thread_ref'"

labels_out=$(ateam show "$init_id")
echo "$labels_out" | grep -q "thread:$thread_ref" \
  || fail case4 "thread:$thread_ref label not found on initiative (show: '$labels_out')"

echo "case4 PASS: digest notified via stub, thread_ref=$thread_ref, label written"

# ── Case 5: simulated Eric reply -> relay -> Relay->Steward envelope ─────────

reply_text="Approved — proceed with the plan."
printf '{"thread_ref": "%s", "text": "%s"}\n' "$thread_ref" "$reply_text" \
  > "$AGENT_TEAMS_STUB_DIR/reply-001.json"

relay_out=$(ateam relay 2>&1)
echo "$relay_out" | grep -q "starting on transport" \
  || fail case5 "relay did not print starting line (got: '$relay_out')"

[ ! -f "$AGENT_TEAMS_STUB_DIR/reply-001.json" ] \
  || fail case5 "stub did not consume reply-001.json after relay"

reply_msgs=$(bd -C "$AGENT_TEAMS_HOME" list --include-infra --assignee=steward --status=open --json)
reply_msg_count=$(echo "$reply_msgs" | jq 'length')
[ "$reply_msg_count" = "1" ] || fail case5 "expected 1 open message bead assigned to steward after relay, got $reply_msg_count (raw: $reply_msgs)"

reply_msg_body=$(echo "$reply_msgs" | jq -r '.[0].description')
echo "$reply_msg_body" | grep -qF "<<<steward-reply initiative:$init_id>>>" \
  || fail case5 "message body missing steward-reply envelope header (got: '$reply_msg_body')"
echo "$reply_msg_body" | grep -qF "$reply_text" \
  || fail case5 "message body missing reply text (got: '$reply_msg_body')"

# audit must stay clean right after the relay creates another message bead.
audit_out=$(ateam audit 2>&1)
echo "$audit_out" | grep -q "audit: clean" \
  || fail case5 "ateam audit not clean after relay (got: '$audit_out')"

echo "case5 PASS: relay routed reply into a steward-reply envelope, audit clean"

# ── Case 6: steward answers the DRI -> initiative inbox receives it ──────────

printf 'Approved — proceed. — Steward\n' > "$T/steward-answer.txt"
send_out=$(ateam mail send "$init_id" --file "$T/steward-answer.txt" --sender steward 2>&1)
echo "$send_out" | grep -q "message_id:" \
  || fail case6 "steward's mail send did not report a message_id (got: '$send_out')"

dri_inbox_out=$(cd "$INITIATIVE_WT" && ateam mail inbox 2>&1)
echo "$dri_inbox_out" | grep -qF "Approved — proceed. — Steward" \
  || fail case6 "DRI inbox did not show the steward's answer (got: '$dri_inbox_out')"
echo "$dri_inbox_out" | grep -qi "from: steward" \
  || fail case6 "DRI inbox did not show sender 'steward' (got: '$dri_inbox_out')"

echo "case6 PASS: steward answered the DRI, delivered via the initiative's own inbox"

# ── Case 7: ledger record + stats round-trip ──────────────────────────────────

ledger_out=$(ateam steward ledger record --category plan-approval --initiative "$init_id" \
  --recommendation "Ship the v2 release" --verdict accepted 2>&1)
echo "$ledger_out" | grep -q "recorded:" \
  || fail case7 "ledger record did not report success (got: '$ledger_out')"

ledger_path="$AGENT_TEAMS_HOME/steward/ledger.jsonl"
[ -f "$ledger_path" ] || fail case7 "ledger.jsonl not created at $ledger_path"
ledger_lines=$(grep -c . "$ledger_path")
[ "$ledger_lines" = "1" ] || fail case7 "expected exactly 1 ledger line, got $ledger_lines"

jq -e --arg id "$init_id" \
  '.category == "plan-approval" and .initiative == $id and .verdict == "accepted" and (.recommendation | length > 0) and (.ts | length > 0)' \
  "$ledger_path" >/dev/null \
  || fail case7 "ledger record is malformed: $(cat "$ledger_path")"

stats_out=$(ateam steward ledger stats --json 2>&1)
echo "$stats_out" | jq -e '.[] | select(.category == "plan-approval") | .total == 1 and .accepted == 1 and .corrected == 0' >/dev/null \
  || fail case7 "ledger stats did not reflect the recorded decision (got: '$stats_out')"

echo "case7 PASS: ledger has one well-formed accepted plan-approval record, stats reflect it"

# ── Case 8: ateam audit stays clean at the end of the full loop ─────────────

final_audit_out=$(ateam audit 2>&1)
echo "$final_audit_out" | grep -q "audit: clean" \
  || fail case8 "ateam audit not clean at end of loop (got: '$final_audit_out')"

echo "case8 PASS: audit stays clean — steward mail beads never leaked as work beads"

echo ""
echo "ALL CASES PASSED — steward loop (gate -> steward -> relay -> steward -> DRI, ledger, audit) closed end-to-end."
