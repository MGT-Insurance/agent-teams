#!/usr/bin/env bash
# sent-log.test.sh — sent-message audit log loop-closure test
# (agent-teams-48dh, contract agent-teams-48dh.1).
#
# WHY THIS EXISTS: the Steward once told Eric "that wasn't me" about a
# Telegram message and was wrong, with no way to check. Eric ruled the
# visible Telegram feed stays byte-for-byte unchanged ("keep as it is
# today"), so this log — and specifically the FILTERED readback below — is
# now the ONLY mechanism that will ever answer "was that you?".
#
# LOOP CLOSURE (contract §8): a real `ateam notify` run against a temp
# workspace, through the real transport.For -> decorator -> Send path (stub
# transport, no Telegram credentials, no network), writes a sentlog record;
# `ateam sent --sender notify --since 5m --json` reads THAT EXACT RECORD
# back. Filtered, not a bare list — that's the acceptance bar, not a
# nice-to-have (contract §7).
#
# Also asserts the failure/no-op paths contract §1 and §6 require:
#   - transport not configured -> notify fails, no record written
#   - missing log file -> "no messages" (text) / "[]" (json), exit 0
#   - malformed line mid-log -> skipped with a warning, neighbors intact
#   - invalid --sender -> usage error naming the valid kinds
#   - the §1 build gate: count of `transport.OutboundMessage{` literals in
#     non-test Go equals count of `Sender:` fields among them (must read
#     6==6), proven to actually trip when a Sender: field goes missing.
#
# The UNDECLARED-sender guard and the log-write-failure-doesn't-affect-the-
# send path are covered by internal/transport/sentlog_decorator_test.go
# instead of here: every real call site now declares Sender, so neither
# state is reachable through the CLI any more.
#
# Build: requires -tags e2e for the stub transport.
# Run:   bash tests/sent-log.test.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

# ── workspace setup ───────────────────────────────────────────────────────

export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME"
git -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && bd init --prefix at --non-interactive >/dev/null)

export AGENT_TEAMS_STUB_DIR="$T/stub"
mkdir -p "$AGENT_TEAMS_STUB_DIR"

PLATFORM_OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
raw_arch="$(uname -m)"
case "$raw_arch" in
    x86_64)  PLATFORM_ARCH=amd64 ;;
    aarch64) PLATFORM_ARCH=arm64 ;;
    arm64)   PLATFORM_ARCH=arm64 ;;
    *)       PLATFORM_ARCH="$raw_arch" ;;
esac

mkdir -p "$T/bin"
go build -C "$ROOT" -tags e2e -o "$T/bin/ateam-${PLATFORM_OS}-${PLATFORM_ARCH}" ./cmd/ateam
cp "$ROOT/plugins/agent-teams/bin/ateam" "$T/bin/ateam"
chmod +x "$T/bin/ateam-${PLATFORM_OS}-${PLATFORM_ARCH}" "$T/bin/ateam"
export PATH="$T/bin:$PATH"

SENT_LOG="$AGENT_TEAMS_HOME/sent.jsonl"

# ── Case 1: transport not configured -> notify fails, no record written ──
unset AGENT_TEAMS_TRANSPORT 2>/dev/null || true
printf 'problem: no-transport-test\nbranch: feat/no-transport\nteam: test\nmode: interactive\n' > "$T/init1.md"
id1=$(ateam register --title "No Transport Test" --file "$T/init1.md")
printf 'hello\n' > "$T/body1.txt"
notify_out1=$(ateam notify "$id1" --file "$T/body1.txt" --title "T1" 2>&1) && rc1=0 || rc1=$?
[ "$rc1" -ne 0 ] \
  || { echo "FAIL case1: notify unexpectedly succeeded with no transport configured (got: '$notify_out1')"; exit 1; }
[ ! -f "$SENT_LOG" ] \
  || { echo "FAIL case1: sent.jsonl exists after a send that never happened"; exit 1; }
echo "case1 PASS: no transport configured -> notify fails, no record written"

# ── Case 2: real notify via stub transport writes one sentlog record ─────
export AGENT_TEAMS_TRANSPORT=stub
printf 'problem: sent-log e2e test\nbranch: feat/sent-log\nteam: test\nmode: interactive\n' > "$T/init2.md"
init_id=$(ateam register --title "Sent Log E2E Test" --file "$T/init2.md")
[ -n "$init_id" ] || { echo "FAIL case2: register returned empty id"; exit 1; }

body_text="Human: please review the sent-log design."
printf '%s\n' "$body_text" > "$T/body2.txt"
notify_out=$(ateam notify "$init_id" --file "$T/body2.txt" --title "Sent Log Review" 2>&1)
echo "$notify_out" | grep -q "thread_ref:" \
  || { echo "FAIL case2: notify output missing thread_ref line (got: '$notify_out')"; exit 1; }
thread_ref=$(echo "$notify_out" | grep "^thread_ref: " | sed 's/^thread_ref: //')

[ -f "$SENT_LOG" ] || { echo "FAIL case2: sentlog was not written at $SENT_LOG"; exit 1; }
[ "$(wc -l < "$SENT_LOG" | tr -d ' ')" = "1" ] \
  || { echo "FAIL case2: expected exactly one line in sent.jsonl, got $(wc -l < "$SENT_LOG")"; exit 1; }

logged_sender=$(jq -r '.sender' "$SENT_LOG")
logged_initiative=$(jq -r '.initiative' "$SENT_LOG")
logged_ref=$(jq -r '.thread_ref' "$SENT_LOG")
logged_title=$(jq -r '.title' "$SENT_LOG")
logged_body=$(jq -r '.body' "$SENT_LOG")
logged_outcome=$(jq -r '.outcome' "$SENT_LOG")

[ "$logged_sender" = "notify" ] || { echo "FAIL case2: sender = '$logged_sender', want 'notify'"; exit 1; }
[ "$logged_initiative" = "$init_id" ] || { echo "FAIL case2: initiative = '$logged_initiative', want '$init_id'"; exit 1; }
[ "$logged_ref" = "$thread_ref" ] || { echo "FAIL case2: thread_ref = '$logged_ref', want '$thread_ref'"; exit 1; }
[ "$logged_title" = "Sent Log Review" ] || { echo "FAIL case2: title = '$logged_title'"; exit 1; }
[ "$logged_body" = "$body_text"$'\n' ] || [ "$logged_body" = "$body_text" ] \
  || { echo "FAIL case2: body = '$logged_body', want '$body_text'"; exit 1; }
[ "$logged_outcome" = "sent" ] || { echo "FAIL case2: outcome = '$logged_outcome', want 'sent'"; exit 1; }
echo "case2 PASS: real notify via stub transport wrote one sentlog record"

# ── Case 3: THE LOOP CLOSURE — filtered readback returns THAT EXACT record ─
sent_out=$(ateam sent --sender notify --since 5m --json)
echo "sent readback: $sent_out"

sent_count=$(echo "$sent_out" | jq 'length')
[ "$sent_count" = "1" ] \
  || { echo "FAIL case3: expected 1 filtered record, got $sent_count (raw: $sent_out)"; exit 1; }

sent_sender=$(echo "$sent_out" | jq -r '.[0].sender')
sent_initiative=$(echo "$sent_out" | jq -r '.[0].initiative')
sent_ref=$(echo "$sent_out" | jq -r '.[0].thread_ref')
sent_title=$(echo "$sent_out" | jq -r '.[0].title')
sent_outcome=$(echo "$sent_out" | jq -r '.[0].outcome')

[ "$sent_sender" = "notify" ] || { echo "FAIL case3: readback sender = '$sent_sender'"; exit 1; }
[ "$sent_initiative" = "$init_id" ] || { echo "FAIL case3: readback initiative = '$sent_initiative'"; exit 1; }
[ "$sent_ref" = "$thread_ref" ] || { echo "FAIL case3: readback thread_ref = '$sent_ref'"; exit 1; }
[ "$sent_title" = "Sent Log Review" ] || { echo "FAIL case3: readback title = '$sent_title'"; exit 1; }
[ "$sent_outcome" = "sent" ] || { echo "FAIL case3: readback outcome = '$sent_outcome'"; exit 1; }

# Sanity check the filters are real, not accidentally pass-through.
excl_since_out=$(ateam sent --sender notify --since 1ns --json)
excl_since_count=$(echo "$excl_since_out" | jq 'length')
[ "$excl_since_count" = "0" ] \
  || { echo "FAIL case3: --since 1ns should exclude the just-written record, got $excl_since_count"; exit 1; }

excl_sender_out=$(ateam sent --sender close --since 5m --json)
excl_sender_count=$(echo "$excl_sender_out" | jq 'length')
[ "$excl_sender_count" = "0" ] \
  || { echo "FAIL case3: --sender close should not match a 'notify' record, got $excl_sender_count"; exit 1; }

echo "case3 PASS: LOOP CLOSED — ateam sent --sender notify --since 5m --json returned the exact record, filters are real"

# ── Case 4: invalid --sender -> usage error naming the valid kinds ────────
bad_out=$(ateam sent --sender bogus --json 2>&1) && bad_rc=0 || bad_rc=$?
[ "$bad_rc" -ne 0 ] \
  || { echo "FAIL case4: --sender bogus unexpectedly succeeded"; exit 1; }
echo "$bad_out" | grep -qi "invalid" \
  || { echo "FAIL case4: expected an 'invalid' usage error for --sender bogus (got: '$bad_out')"; exit 1; }
echo "$bad_out" | grep -q "notify" \
  || { echo "FAIL case4: usage error did not list valid sender kinds (got: '$bad_out')"; exit 1; }
echo "case4 PASS: invalid --sender rejected with a usage error listing valid kinds"

# ── Case 5: missing log file -> "no messages" / "[]", exit 0 ──────────────
empty_home="$T/ws-empty"
mkdir -p "$empty_home"
(cd "$empty_home" && bd init --prefix at --non-interactive >/dev/null)

empty_json_out=$(AGENT_TEAMS_HOME="$empty_home" ateam sent --json)
[ "$empty_json_out" = "[]" ] \
  || { echo "FAIL case5: --json against a missing log should print [] (got: '$empty_json_out')"; exit 1; }

empty_text_out=$(AGENT_TEAMS_HOME="$empty_home" ateam sent)
echo "$empty_text_out" | grep -q "no messages" \
  || { echo "FAIL case5: plain output against a missing log should say 'no messages' (got: '$empty_text_out')"; exit 1; }
echo "case5 PASS: missing log file -> '[]' for --json, 'no messages' for plain, exit 0 both ways"

# ── Case 6: malformed line mid-log -> skipped with warning, neighbors intact ─
printf 'more human review please\n' > "$T/body3.txt"
ateam notify "$init_id" --file "$T/body3.txt" --title "Second Notify" >/dev/null

[ "$(wc -l < "$SENT_LOG" | tr -d ' ')" = "2" ] \
  || { echo "FAIL case6 setup: expected 2 lines before corruption, got $(wc -l < "$SENT_LOG")"; exit 1; }

awk 'NR==1{print; print "not-json-at-all"; next} {print}' "$SENT_LOG" > "$T/sent-corrupted.jsonl"
mv "$T/sent-corrupted.jsonl" "$SENT_LOG"

sent_stdout=$(ateam sent --json 2> "$T/sent-stderr.txt")
sent_stderr=$(cat "$T/sent-stderr.txt")

sent_after_count=$(echo "$sent_stdout" | jq 'length')
[ "$sent_after_count" = "2" ] \
  || { echo "FAIL case6: expected the 2 valid records back despite the corrupted line, got $sent_after_count (stdout: '$sent_stdout')"; exit 1; }
echo "$sent_stderr" | grep -qi "malformed" \
  || { echo "FAIL case6: expected a stderr warning about the malformed line (got: '$sent_stderr')"; exit 1; }
echo "case6 PASS: malformed line skipped with a warning, both surrounding records still read back"

# ── Case 7: §1 build gate — OutboundMessage literal count == Sender field count ─
#
# Matches BOTH the package-qualified literal (transport.OutboundMessage{,
# used everywhere outside package transport) and the unqualified literal
# (OutboundMessage{, only meaningful written inside package transport
# itself) — a send path added inside package transport would use the
# unqualified form and was previously invisible to this gate
# (agent-teams-48dh.28). Excludes full-line comments so a doc comment that
# merely MENTIONS "OutboundMessage{" (e.g. transport.go's field doc) can't
# inflate the count — see case10 below, which proves the comment exclusion
# doesn't also blind the gate to a real unqualified literal.
count_literals() {
  grep -rn "OutboundMessage{" --include="*.go" "$ROOT" 2>/dev/null \
    | grep -v "_test\.go" \
    | grep -vE '^[^:]+:[0-9]+:[[:space:]]*//' \
    | wc -l | tr -d ' '
}

# For each literal site, look at the following 20 lines for a "Sender:"
# field. The literals here are all short (well under 20 lines to their
# closing brace), so this window can't spill into the next literal — the
# sites are each 40+ lines apart.
count_sender_fields() {
  local total=0
  while IFS=: read -r file line; do
    if sed -n "${line},$((line+20))p" "$file" | grep -q "Sender:"; then
      total=$((total+1))
    fi
  done < <(grep -rn "OutboundMessage{" --include="*.go" "$ROOT" 2>/dev/null | grep -v "_test\.go" | grep -vE '^[^:]+:[0-9]+:[[:space:]]*//' | cut -d: -f1,2)
  echo "$total"
}

lit_count=$(count_literals)
sender_count=$(count_sender_fields)
[ "$lit_count" = "6" ] \
  || { echo "FAIL gate: expected exactly 6 OutboundMessage literals in non-test Go, found $lit_count"; exit 1; }
[ "$lit_count" = "$sender_count" ] \
  || { echo "FAIL gate: OutboundMessage literal count ($lit_count) != Sender field count ($sender_count)"; exit 1; }
echo "case7 PASS: build gate reads $lit_count==$sender_count — every OutboundMessage literal declares Sender"

# ── Case 8: prove the gate actually trips when a Sender: field goes missing ─
target_file="$ROOT/internal/verbs/hung_tick.go"
cp "$target_file" "$T/hung_tick.go.bak"

sender_line_count=$(grep -c "Sender:.*KindRelayHung" "$target_file")
[ "$sender_line_count" = "1" ] \
  || { echo "FAIL case8 setup: expected exactly one 'Sender: ... KindRelayHung' line in $target_file, found $sender_line_count"; exit 1; }

# Restore the file no matter how this case exits, in addition to the
# top-level workspace cleanup.
trap 'cp "$T/hung_tick.go.bak" "$target_file" 2>/dev/null || true; rm -rf "$T"' EXIT

grep -v "Sender:.*KindRelayHung" "$target_file" > "$T/hung_tick.go.broken"
cp "$T/hung_tick.go.broken" "$target_file"

broken_lit_count=$(count_literals)
broken_sender_count=$(count_sender_fields)

if [ "$broken_lit_count" = "$broken_sender_count" ]; then
  cp "$T/hung_tick.go.bak" "$target_file"
  echo "FAIL case8: removing a Sender: field did not trip the gate (still $broken_lit_count==$broken_sender_count)"
  exit 1
fi
echo "case8 PASS: removing one Sender: field broke the gate ($broken_lit_count != $broken_sender_count), as required"

# Restore and reset the trap to plain cleanup for the rest of the script.
cp "$T/hung_tick.go.bak" "$target_file"
trap 'rm -rf "$T"' EXIT

# ── Case 9: egress gate — pins the chokepoint the construction gate can't see
# (agent-teams-48dh.28). Case7/8 count OutboundMessage LITERAL constructions,
# which is blind to a capability that POSTs message text to the Bot API
# directly without building one — CloseTopic and Ack already do this today,
# carrying no text, so they're already invisible to case7/8 and harmless.
# This gate instead pins EGRESS: exactly one apiURL("sendMessage") call in
# telegram.go, exactly two t.sendMessage( call sites and BOTH inside Send,
# and none of the other message-bearing Bot API methods present. A future
# capability that emits user-visible text without going through Send adds
# one of those and trips this immediately, even though it adds zero
# OutboundMessage literals.
TELEGRAM_GO="$ROOT/internal/transport/telegram/telegram.go"
cp "$TELEGRAM_GO" "$T/telegram.go.bak"
trap 'cp "$T/telegram.go.bak" "$TELEGRAM_GO" 2>/dev/null || true; rm -rf "$T"' EXIT

count_send_message_egress() {
  grep -c 'apiURL("sendMessage")' "$TELEGRAM_GO"
}

count_send_message_call_sites() {
  grep -c '\bt\.sendMessage(' "$TELEGRAM_GO"
}

# Line range of the Send method body: from its signature to the next
# top-level "func " (exclusive).
send_method_range() {
  awk '
    /^func \(t \*Telegram\) Send\(/ { start=NR; next }
    start && /^func / { print start","(NR-1); exit }
  ' "$TELEGRAM_GO"
}

send_range="$(send_method_range)"
[ -n "$send_range" ] \
  || { echo "FAIL case9 setup: could not locate Send method bounds in telegram.go"; exit 1; }
send_start="${send_range%,*}"
send_end="${send_range#*,}"
call_sites_inside_send=$(sed -n "${send_start},${send_end}p" "$TELEGRAM_GO" | grep -c '\bt\.sendMessage(')

egress_count=$(count_send_message_egress)
callsite_count=$(count_send_message_call_sites)

[ "$egress_count" = "1" ] \
  || { echo "FAIL case9: expected exactly 1 apiURL(\"sendMessage\") egress, found $egress_count"; exit 1; }
[ "$callsite_count" = "2" ] \
  || { echo "FAIL case9: expected exactly 2 t.sendMessage( call sites, found $callsite_count"; exit 1; }
[ "$call_sites_inside_send" = "2" ] \
  || { echo "FAIL case9: expected both t.sendMessage( call sites inside Send (lines $send_start-$send_end), found $call_sites_inside_send"; exit 1; }
for forbidden in sendPhoto sendDocument sendAnimation copyMessage forwardMessage editMessageText; do
  if grep -q "$forbidden" "$TELEGRAM_GO"; then
    echo "FAIL case9: forbidden message-bearing Bot API method '$forbidden' found in telegram.go"
    exit 1
  fi
done
echo "case9 PASS: egress gate — exactly one sendMessage chokepoint, both call sites inside Send, no other message-bearing methods present"

# ── Case 9b: prove the egress gate actually trips (a stray sendPhoto reference) ─
printf '\n// case9b-injected: sendPhoto\n' >> "$TELEGRAM_GO"
tripped=0
for forbidden in sendPhoto sendDocument sendAnimation copyMessage forwardMessage editMessageText; do
  if grep -q "$forbidden" "$TELEGRAM_GO"; then
    tripped=1
    break
  fi
done
cp "$T/telegram.go.bak" "$TELEGRAM_GO"
[ "$tripped" = "1" ] \
  || { echo "FAIL case9b: injecting a stray sendPhoto reference did not trip the forbidden-method check"; exit 1; }
echo "case9b PASS: a stray sendPhoto reference trips the egress gate, as required"

# Reset the trap to plain cleanup for the rest of the script.
trap 'rm -rf "$T"' EXIT

# ── Case 10: prove the qualification-hole fix (an in-package unqualified literal) ─
# Before agent-teams-48dh.28, count_literals only matched the package-
# qualified "transport.OutboundMessage{", so a literal written inside
# package transport itself (which uses the unqualified "OutboundMessage{")
# was invisible to case7/8's gate. Prove the fix: a transient file inside
# package transport containing an unqualified, Sender-less literal must now
# inflate lit_count without inflating sender_count, tripping the 6==6 check.
PROBE_GO="$ROOT/internal/transport/zz_case10_probe.go"
trap 'rm -f "$PROBE_GO"; rm -rf "$T"' EXIT
cat > "$PROBE_GO" <<'EOF'
package transport

// zz_case10_probe.go is a transient probe written by
// tests/sent-log.test.sh case10 to prove the qualification-hole fix. It is
// deleted before the script exits and never reaches git.
var _ = OutboundMessage{}
EOF

probe_lit_count=$(count_literals)
probe_sender_count=$(count_sender_fields)
rm -f "$PROBE_GO"
trap 'rm -rf "$T"' EXIT

[ "$probe_lit_count" != "$probe_sender_count" ] \
  || { echo "FAIL case10: an in-package unqualified OutboundMessage{} literal with no Sender field did not trip the gate (still $probe_lit_count==$probe_sender_count)"; exit 1; }
[ "$probe_lit_count" = "7" ] \
  || { echo "FAIL case10: expected the probe literal to raise lit_count to 7, got $probe_lit_count"; exit 1; }
echo "case10 PASS: an in-package unqualified literal now counts and trips the gate ($probe_lit_count != $probe_sender_count), as required"

echo ""
echo "ALL CASES PASSED — sent-log loop closed and build gate proven."
