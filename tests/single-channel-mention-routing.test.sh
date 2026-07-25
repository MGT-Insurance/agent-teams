#!/usr/bin/env bash
# single-channel-mention-routing.test.sh — two-machine @mention routing test
# (agent-teams-4x83.5, the loop-closing verification for the single-channel
# steward-addressing initiative agent-teams-4x83). Modeled on
# tests/multi-machine-routing.test.sh (same isolation pattern: temp
# AGENT_TEAMS_HOME per machine, stub transport, real `ateam relay`/`ateam
# notify` binary, fake claude shim) but drives the @mention routing rules
# (agent-teams-4x83.2) that REPLACED the old per-machine [Direct] topic
# short-circuit: every General-channel (non-topic, ThreadRef=="") message is
# now routed by @mention instead of a persisted per-machine thread ref.
#
# Two machines, A and B, are modeled with DISTINCT bot identities
# ("machineabot" / "machinebbot"). The stub transport has no network/getMe
# (internal/transport/stub/stub.go), so each machine's identity is modeled by
# the reply file's mentions_self field rather than derived from mentions — a
# SHARED inbound message is written into both machines' stub dirs with the
# SAME mentions list but per-machine mentions_self, mirroring what each
# machine's own Telegram getMe-derived username would make MentionsSelf
# resolve to in the real transport.
#
# Exercises, across real process boundaries, handleReply's three @mention
# rules (relay.go, non-topic / General-channel branch):
#
#   1. MentionsSelf: the addressed machine routes the reply to ITS OWN
#      steward as a steward-direct envelope, regardless of fallback-responder
#      status.
#   2. A mentioned username ends in "bot" (Telegram platform rule: every bot
#      username must end in "bot") and is not this machine's own: skip
#      silently with a "not me, skipping" log line. Covered both as the
#      addressee's PEER (scenario "Rule 1 + Rule 2 (peer)") and as a message
#      addressed to a bot belonging to NEITHER machine (scenario "Rule 2
#      (neither machine addressed)").
#   3. No bot mention at all (including a human-only mention like "@eric"):
#      falls through to the pre-existing fallback-responder steward-unrouted
#      behavior (unchanged from tests/multi-machine-routing.test.sh's
#      scenario 4) — re-verified here as the "no bot mention" arm of this
#      same three-way decision, with both a plain message and a human-mention
#      variant.
#
# Also covers the outbound half: `ateam notify direct` (agent-teams-4x83.3)
# posts straight to the transport's General channel — no forum topic opened,
# no thread ref persisted or returned.
#
# ── DM round trip (agent-teams-ncn5.6, the loop-closing bead for ncn5) ──────
#
# A 1:1 DM to the bot arrives with ThreadRef == "" like a General-channel
# message, but with Direct == true and NO mentions — in a 1:1 there is nobody
# else to address, so the relay treats Direct exactly as it treats an explicit
# self-mention (rule 1). Two halves, both driven through the real binaries:
#
#   inbound  — a DM produces a steward-direct envelope whose sentinel header
#              carries the reply-to ref, on BOTH machines. Machine B is not
#              the fallback responder and routes it anyway: unlike rule 3, the
#              Direct path is deliberately not fallback-gated (a private-chat
#              update reaches only the bot it was addressed to, so it is
#              exactly-once by construction).
#   outbound — `ateam notify direct --to <ref>` addresses the answer at that
#              conversation (chat_ref recorded, General false); `--to general`
#              is unchanged (General true, no chat_ref); an omitted --to still
#              delivers to General but is loud at depth 0.
#
# ⚠️ SCOPE: this proves the PLUMBING, not the prose. It drives the binaries
# directly, so it shows that a ref supplied on the command line reaches the
# right chat. It does NOT and CANNOT show that a real steward, reading only
# the shipped SKILL.md, actually emits that ref — there is no model in this
# test, and putting one in would make a deterministic shell test stochastic
# and slow. That gap is covered by the separate agent-in-the-loop bead
# (agent-teams-ncn5.13). A green run here is not evidence the prose works.
#
# ⚠️ There is deliberately no live Telegram test anywhere in this epic: one
# bot token permits exactly ONE getUpdates consumer, and the production relay
# is the steward's only inbound path from the human. A second poller silently
# steals his messages. The stub transport exists so this bead needs none.
#
# Build: requires -tags e2e for the stub transport (same as
# multi-machine-routing.test.sh).
# Run:   bash tests/single-channel-mention-routing.test.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

fail() { echo "FAIL $1: $2"; exit 1; }

# ── binary build (same pattern as multi-machine-routing.test.sh) ───────────

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

# Fake claude shim — belt-and-suspenders, see multi-machine-routing.test.sh's
# identical comment: every mail send this test triggers is steward-addressed
# (relay's defaultRelaySend hardcodes StewardHandle), which short-circuits the
# liveness check before agentsFunc is ever called since "steward" is not a bd
# initiative bead. This shim just guarantees that even if some path did reach
# it, no real background claude session gets spawned.
cat > "$T/bin/claude" <<'SHIM'
#!/usr/bin/env bash
if [ "${1:-}" = "agents" ] && [ "${2:-}" = "--json" -o "${3:-}" = "--json" ]; then
  printf '[]\n'
  exit 0
fi
exit 0
SHIM
chmod +x "$T/bin/claude"

export PATH="$T/bin:$PATH"

# ── helpers (mirrors multi-machine-routing.test.sh) ─────────────────────────

# mkhome creates a fresh AGENT_TEAMS_HOME-shaped workspace: a git repo (bd
# requires one) with an initialized bd db.
mkhome() {
  local home="$1"
  mkdir -p "$home"
  git -C "$home" init -q
  (cd "$home" && bd init --prefix at --non-interactive >/dev/null)
}

# run_relay runs the real `ateam relay` binary against one machine (home +
# its own stub transport dir), returning combined stdout/stderr.
run_relay() {
  local home="$1" stub="$2"
  AGENT_TEAMS_HOME="$home" AGENT_TEAMS_STUB_DIR="$stub" AGENT_TEAMS_TRANSPORT=stub ateam relay 2>&1
}

# steward_open_msgs prints the raw JSON array of open, steward-assigned
# message beads in home's bd db (--include-infra: message beads are
# infra-type, excluded from a default `bd list`).
steward_open_msgs() {
  bd -C "$1" list --include-infra --assignee=steward --status=open --json
}

steward_msg_count() {
  steward_open_msgs "$1" | jq 'length'
}

echo "=== Rule 1 + Rule 2 (peer): message @mentions machine A's bot -> A routes direct; B (peer bot) skips ==="

R1A="$T/home-r1a"
R1B="$T/home-r1b"
mkhome "$R1A"
mkhome "$R1B"

R1A_STUB="$T/stub-r1a"; mkdir -p "$R1A_STUB"
R1B_STUB="$T/stub-r1b"; mkdir -p "$R1B_STUB"

r1_text="@machineabot can you check on the deploy?"
printf '{"thread_ref": "", "text": "%s", "mentions": ["machineabot"], "mentions_self": true}\n' "$r1_text" > "$R1A_STUB/reply-001.json"
printf '{"thread_ref": "", "text": "%s", "mentions": ["machineabot"], "mentions_self": false}\n' "$r1_text" > "$R1B_STUB/reply-001.json"

r1a_out=$(run_relay "$R1A" "$R1A_STUB")
echo "$r1a_out" | grep -q "starting on transport" \
  || fail rule1a "machine A relay did not print starting line (got: '$r1a_out')"
echo "$r1a_out" | grep -qi "mentions me" \
  || fail rule1a "machine A relay stderr did not mention 'mentions me' (got: '$r1a_out')"
[ ! -f "$R1A_STUB/reply-001.json" ] || fail rule1a "machine A stub did not consume reply-001.json"

r1a_count=$(steward_msg_count "$R1A")
[ "$r1a_count" = "1" ] || fail rule1a "expected machine A to route exactly 1 steward message for the self-mention, got $r1a_count"
r1a_body=$(steward_open_msgs "$R1A" | jq -r '.[0].description')
echo "$r1a_body" | grep -qF "<<<steward-direct>>>" \
  || fail rule1a "machine A message missing steward-direct envelope header (got: '$r1a_body')"
echo "$r1a_body" | grep -qF "$r1_text" \
  || fail rule1a "machine A message missing reply text (got: '$r1a_body')"

echo "rule1a PASS: machine A (mentions_self=true) routed the @mention to its steward as a steward-direct envelope"

r1b_out=$(run_relay "$R1B" "$R1B_STUB")
echo "$r1b_out" | grep -q "starting on transport" \
  || fail rule1b "machine B relay did not print starting line (got: '$r1b_out')"
echo "$r1b_out" | grep -qF "mentions @machineabot" \
  || fail rule1b "machine B relay stderr did not mention '@machineabot' (got: '$r1b_out')"
echo "$r1b_out" | grep -qi "not me, skipping" \
  || fail rule1b "machine B relay stderr did not say 'not me, skipping' (got: '$r1b_out')"
[ ! -f "$R1B_STUB/reply-001.json" ] || fail rule1b "machine B stub did not consume reply-001.json"

r1b_count=$(steward_msg_count "$R1B")
[ "$r1b_count" = "0" ] || fail rule1b "expected machine B (peer bot mentioned, not itself) to route 0 steward messages, got $r1b_count"

echo "rule1b PASS: machine B recognized @machineabot as a foreign bot mention and skipped — exactly-once confirmed"

echo ""
echo "=== Rule 2 (neither machine addressed): message @mentions a third bot -> both machines skip ==="

R2A="$T/home-r2a"
R2B="$T/home-r2b"
mkhome "$R2A"
mkhome "$R2B"

R2A_STUB="$T/stub-r2a"; mkdir -p "$R2A_STUB"
R2B_STUB="$T/stub-r2b"; mkdir -p "$R2B_STUB"

r2_text="@thirdpartybot please pick this up"
printf '{"thread_ref": "", "text": "%s", "mentions": ["thirdpartybot"], "mentions_self": false}\n' "$r2_text" > "$R2A_STUB/reply-001.json"
printf '{"thread_ref": "", "text": "%s", "mentions": ["thirdpartybot"], "mentions_self": false}\n' "$r2_text" > "$R2B_STUB/reply-001.json"

r2a_out=$(run_relay "$R2A" "$R2A_STUB")
echo "$r2a_out" | grep -qF "mentions @thirdpartybot" \
  || fail rule2a "machine A relay stderr did not mention '@thirdpartybot' (got: '$r2a_out')"
echo "$r2a_out" | grep -qi "not me, skipping" \
  || fail rule2a "machine A relay stderr did not say 'not me, skipping' (got: '$r2a_out')"
[ "$(steward_msg_count "$R2A")" = "0" ] \
  || fail rule2a "expected machine A to route 0 steward messages for a third-party bot mention"

r2b_out=$(run_relay "$R2B" "$R2B_STUB")
echo "$r2b_out" | grep -qF "mentions @thirdpartybot" \
  || fail rule2b "machine B relay stderr did not mention '@thirdpartybot' (got: '$r2b_out')"
echo "$r2b_out" | grep -qi "not me, skipping" \
  || fail rule2b "machine B relay stderr did not say 'not me, skipping' (got: '$r2b_out')"
[ "$(steward_msg_count "$R2B")" = "0" ] \
  || fail rule2b "expected machine B to route 0 steward messages for a third-party bot mention"

echo "rule2 PASS: neither machine claims a mention addressed to a bot belonging to neither of them"

echo ""
echo "=== Rule 3 (no bot mention): plain message -> only the fallback responder routes ==="

R3A="$T/home-r3a"
R3B="$T/home-r3b"
mkhome "$R3A"
mkhome "$R3B"

# Machine A: designated fallback responder (marker file) + a live steward
# session (StewardSessionMarkerPath, via the real `ateam steward init`).
mkdir -p "$R3A/steward"
: > "$R3A/steward/fallback-responder"
AGENT_TEAMS_HOME="$R3A" ateam steward init >/dev/null

# Machine B: neither marker — a fresh, untouched home.

R3A_STUB="$T/stub-r3a"; mkdir -p "$R3A_STUB"
R3B_STUB="$T/stub-r3b"; mkdir -p "$R3B_STUB"

r3_text="just checking in on progress"
printf '{"thread_ref": "", "text": "%s"}\n' "$r3_text" > "$R3A_STUB/reply-001.json"
printf '{"thread_ref": "", "text": "%s"}\n' "$r3_text" > "$R3B_STUB/reply-001.json"

r3a_out=$(run_relay "$R3A" "$R3A_STUB")
echo "$r3a_out" | grep -qi "routing non-topic message to steward" \
  || fail rule3a "machine A relay stderr did not mention routing the non-topic message (got: '$r3a_out')"
r3a_count=$(steward_msg_count "$R3A")
[ "$r3a_count" = "1" ] || fail rule3a "expected fallback machine A to route exactly 1 steward message, got $r3a_count"
r3a_body=$(steward_open_msgs "$R3A" | jq -r '.[0].description')
echo "$r3a_body" | grep -qF "<<<steward-unrouted thread:(general) reason:" \
  || fail rule3a "machine A message missing steward-unrouted envelope with (general) placeholder (got: '$r3a_body')"
echo "$r3a_body" | grep -qF "$r3_text" \
  || fail rule3a "machine A message missing reply text (got: '$r3a_body')"

echo "rule3a PASS: fallback-designated machine A routed the no-mention message as a steward-unrouted envelope"

r3b_out=$(run_relay "$R3B" "$R3B_STUB")
echo "$r3b_out" | grep -qi "skipping non-topic message" \
  || fail rule3b "machine B relay stderr did not mention skipping the non-topic message (got: '$r3b_out')"
[ "$(steward_msg_count "$R3B")" = "0" ] || fail rule3b "expected non-fallback machine B to route 0 steward messages"

echo "rule3b PASS: non-fallback machine B suppressed the identical no-mention message — exactly-once confirmed"

echo ""
echo "=== Rule 3 (human-only mention): @eric is not a bot mention -> same fallback-responder behavior ==="

R3H_A="$T/home-r3h-a"
R3H_B="$T/home-r3h-b"
mkhome "$R3H_A"
mkhome "$R3H_B"

mkdir -p "$R3H_A/steward"
: > "$R3H_A/steward/fallback-responder"
AGENT_TEAMS_HOME="$R3H_A" ateam steward init >/dev/null

R3H_A_STUB="$T/stub-r3h-a"; mkdir -p "$R3H_A_STUB"
R3H_B_STUB="$T/stub-r3h-b"; mkdir -p "$R3H_B_STUB"

r3h_text="@eric can you take a look at this thread?"
printf '{"thread_ref": "", "text": "%s", "mentions": ["eric"], "mentions_self": false}\n' "$r3h_text" > "$R3H_A_STUB/reply-001.json"
printf '{"thread_ref": "", "text": "%s", "mentions": ["eric"], "mentions_self": false}\n' "$r3h_text" > "$R3H_B_STUB/reply-001.json"

r3h_a_out=$(run_relay "$R3H_A" "$R3H_A_STUB")
echo "$r3h_a_out" | grep -qi "routing non-topic message to steward" \
  || fail rule3h_a "machine A relay stderr did not mention routing the human-mention message (got: '$r3h_a_out')"
[ "$(steward_msg_count "$R3H_A")" = "1" ] \
  || fail rule3h_a "expected fallback machine A to route exactly 1 steward message for a human-only mention"
r3h_a_body=$(steward_open_msgs "$R3H_A" | jq -r '.[0].description')
echo "$r3h_a_body" | grep -qF "<<<steward-unrouted thread:(general) reason:" \
  || fail rule3h_a "machine A message missing steward-unrouted envelope (got: '$r3h_a_body')"

echo "rule3h_a PASS: a human-only mention (@eric) is not treated as a bot mention — fallback responder still routes it"

r3h_b_out=$(run_relay "$R3H_B" "$R3H_B_STUB")
echo "$r3h_b_out" | grep -qi "skipping non-topic message" \
  || fail rule3h_b "machine B relay stderr did not mention skipping the human-mention message (got: '$r3h_b_out')"
[ "$(steward_msg_count "$R3H_B")" = "0" ] \
  || fail rule3h_b "expected non-fallback machine B to route 0 steward messages for a human-only mention"

echo "rule3h_b PASS: non-fallback machine B suppressed the identical human-only-mention message"

echo ""
echo "=== DM (rule 1, implicit): 1:1 message, no mentions -> BOTH machines route steward-direct with the ref ==="

DM_A="$T/home-dm-a"
DM_B="$T/home-dm-b"
mkhome "$DM_A"
mkhome "$DM_B"

# Machine A is the designated fallback responder; machine B is not — the same
# split as the rule 3 scenarios above, deliberately, because that is what
# makes this scenario mean something. Rule 3 SUPPRESSES machine B for an
# identical no-mention group message; a DM must NOT be suppressed that way.
# Telegram delivers a private-chat update only to the bot it was addressed
# to, so a DM is exactly-once by construction and needs no fallback gate;
# gating it would silently drop every DM sent to a non-fallback machine.
mkdir -p "$DM_A/steward"
: > "$DM_A/steward/fallback-responder"
AGENT_TEAMS_HOME="$DM_A" ateam steward init >/dev/null

DM_A_STUB="$T/stub-dm-a"; mkdir -p "$DM_A_STUB"
DM_B_STUB="$T/stub-dm-b"; mkdir -p "$DM_B_STUB"

# The ref is composite ("<chat_id>:<message_id>") because a Telegram message
# id is unique only within its chat. The colon inside it is why every
# assertion below greps the whole header as a fixed string: a naive
# split-on-colon would truncate the ref and still "pass".
dm_ref="12345678:10"
dm_text="what's the status?"
# No mentions, no mentions_self — that absence IS the scenario. Nothing but
# direct:true can route this message to a steward.
printf '{"direct": true, "thread_ref": "", "message_ref": "%s", "text": "%s"}\n' "$dm_ref" "$dm_text" > "$DM_A_STUB/reply-001.json"
printf '{"direct": true, "thread_ref": "", "message_ref": "%s", "text": "%s"}\n' "$dm_ref" "$dm_text" > "$DM_B_STUB/reply-001.json"

dm_a_out=$(run_relay "$DM_A" "$DM_A_STUB")
echo "$dm_a_out" | grep -qi "direct message (1:1 chat)" \
  || fail dm_a "machine A relay stderr did not log the 1:1 direct-message routing decision (got: '$dm_a_out')"
[ ! -f "$DM_A_STUB/reply-001.json" ] || fail dm_a "machine A stub did not consume reply-001.json"

dm_a_count=$(steward_msg_count "$DM_A")
[ "$dm_a_count" = "1" ] || fail dm_a "expected machine A to route exactly 1 steward message for the DM, got $dm_a_count"
dm_a_body=$(steward_open_msgs "$DM_A" | jq -r '.[0].description')
echo "$dm_a_body" | grep -qF "<<<steward-direct reply-to:$dm_ref>>>" \
  || fail dm_a "machine A message missing a steward-direct header carrying the WHOLE composite ref (got: '$dm_a_body')"
echo "$dm_a_body" | grep -qF "$dm_text" \
  || fail dm_a "machine A message missing the DM text (got: '$dm_a_body')"

echo "dm_a PASS: machine A routed the DM as a steward-direct envelope carrying the full reply-to ref"

dm_b_out=$(run_relay "$DM_B" "$DM_B_STUB")
echo "$dm_b_out" | grep -qi "direct message (1:1 chat)" \
  || fail dm_b "machine B relay stderr did not log the 1:1 direct-message routing decision (got: '$dm_b_out')"
if echo "$dm_b_out" | grep -qi "skipping non-topic message"; then
  fail dm_b "machine B suppressed the DM as an unaddressed non-topic message — the Direct path must not be fallback-gated (got: '$dm_b_out')"
fi

dm_b_count=$(steward_msg_count "$DM_B")
[ "$dm_b_count" = "1" ] \
  || fail dm_b "expected non-fallback machine B to route the DM anyway (a DM is exactly-once by construction, so it is not fallback-gated), got $dm_b_count"
dm_b_body=$(steward_open_msgs "$DM_B" | jq -r '.[0].description')
echo "$dm_b_body" | grep -qF "<<<steward-direct reply-to:$dm_ref>>>" \
  || fail dm_b "machine B message missing a steward-direct header carrying the WHOLE composite ref (got: '$dm_b_body')"

echo "dm_b PASS: machine B — NOT the fallback responder — routed the identical DM as steward-direct too"

echo ""
echo "=== Outbound: 'ateam notify direct' posts to General — no thread ref, no topic ==="

ND_HOME="$T/home-notify-direct"
mkhome "$ND_HOME"
ND_STUB="$T/stub-notify-direct"; mkdir -p "$ND_STUB"

nd_body_text="Heads up: the nightly build is green."
printf '%s\n' "$nd_body_text" > "$T/nd-body.txt"

nd_out=$(AGENT_TEAMS_HOME="$ND_HOME" AGENT_TEAMS_STUB_DIR="$ND_STUB" AGENT_TEAMS_TRANSPORT=stub \
  ateam notify direct --file "$T/nd-body.txt" --title "Nightly Build" 2>&1)
echo "$nd_out" | grep -qx "thread_ref: " \
  || fail notify_direct "notify direct printed a non-empty thread_ref (got: '$nd_out')"
echo "$nd_out" | grep -q "^initiative: direct$" \
  || fail notify_direct "notify direct did not print 'initiative: direct' (got: '$nd_out')"

[ -f "$ND_STUB/sent.jsonl" ] || fail notify_direct "stub sent.jsonl was not written"
[ "$(wc -l < "$ND_STUB/sent.jsonl" | tr -d ' ')" = "1" ] || fail notify_direct "expected exactly 1 line in sent.jsonl"
sent_record=$(head -n1 "$ND_STUB/sent.jsonl")
[ "$(echo "$sent_record" | jq -r '.thread_ref')" = "" ] \
  || fail notify_direct "sent record thread_ref is not empty — General post must not carry a thread ref (got: '$sent_record')"
[ "$(echo "$sent_record" | jq -r '.initiative_id')" = "direct" ] \
  || fail notify_direct "sent record initiative_id != direct (got: '$sent_record')"
[ "$(echo "$sent_record" | jq -r '.body')" = "$nd_body_text" ] \
  || fail notify_direct "sent record body mismatch (got: '$sent_record')"
[ ! -f "$ND_STUB/next-ref" ] \
  || fail notify_direct "stub's next-ref counter was bumped — a General post must not open/consume a thread ref"

echo "notify_direct PASS: 'ateam notify direct' posted straight to the General channel — empty thread_ref, no forum topic opened"

echo ""
echo "=== Outbound addressing: 'ateam notify direct --to' selects the conversation the answer lands in ==="

# One home, three stub dirs: notify direct persists nothing per-send, so the
# home is reusable, but each send needs its own sent.jsonl to assert against.
NT_HOME="$T/home-notify-to"
mkhome "$NT_HOME"
printf 'Status: green across the board.\n' > "$T/nt-body.txt"

nt_ref="12345678:10"

# (a) --to <ref>: the answer is addressed at the conversation the ref names,
#     not at the configured channel. This is the outbound half of the DM
#     round trip — dm_a above proved the ref reaches the steward, this proves
#     handing that ref back to notify puts the answer in the right chat.
NT_REF_STUB="$T/stub-notify-to-ref"; mkdir -p "$NT_REF_STUB"
nt_ref_out=$(AGENT_TEAMS_HOME="$NT_HOME" AGENT_TEAMS_STUB_DIR="$NT_REF_STUB" AGENT_TEAMS_TRANSPORT=stub \
  ateam notify direct --to "$nt_ref" --file "$T/nt-body.txt" --title "Steward" 2>&1)

[ -f "$NT_REF_STUB/sent.jsonl" ] || fail notify_to_ref "stub sent.jsonl was not written"
[ "$(wc -l < "$NT_REF_STUB/sent.jsonl" | tr -d ' ')" = "1" ] || fail notify_to_ref "expected exactly 1 line in sent.jsonl"
nt_ref_record=$(head -n1 "$NT_REF_STUB/sent.jsonl")
[ "$(echo "$nt_ref_record" | jq -r '.chat_ref')" = "$nt_ref" ] \
  || fail notify_to_ref "sent record chat_ref != the --to ref — the answer is NOT addressed at the DM (got: '$nt_ref_record')"
[ "$(echo "$nt_ref_record" | jq -r '.general')" = "false" ] \
  || fail notify_to_ref "sent record general is true — a ref-addressed answer must not be downgraded into the shared channel (got: '$nt_ref_record')"
[ "$(echo "$nt_ref_record" | jq -r '.thread_ref')" = "" ] \
  || fail notify_to_ref "sent record thread_ref is not empty — a 1:1 chat has no forum topic (got: '$nt_ref_record')"
[ ! -f "$NT_REF_STUB/next-ref" ] \
  || fail notify_to_ref "stub's next-ref counter was bumped — a DM reply must not open/consume a thread ref"
if echo "$nt_ref_out" | grep -q "FAULT"; then
  fail notify_to_ref "notify direct emitted the missing---to FAULT diagnostic even though --to was supplied (got: '$nt_ref_out')"
fi

echo "notify_to_ref PASS: '--to <ref>' addressed the send at that conversation — chat_ref carried, General false, no topic"

# (b) --to general: the mirror case. The pre-existing @mention reply path is
#     provably unchanged — no chat_ref, General true.
NT_GEN_STUB="$T/stub-notify-to-general"; mkdir -p "$NT_GEN_STUB"
nt_gen_out=$(AGENT_TEAMS_HOME="$NT_HOME" AGENT_TEAMS_STUB_DIR="$NT_GEN_STUB" AGENT_TEAMS_TRANSPORT=stub \
  ateam notify direct --to general --file "$T/nt-body.txt" --title "Steward" 2>&1)

[ "$(wc -l < "$NT_GEN_STUB/sent.jsonl" | tr -d ' ')" = "1" ] || fail notify_to_general "expected exactly 1 line in sent.jsonl"
nt_gen_record=$(head -n1 "$NT_GEN_STUB/sent.jsonl")
[ "$(echo "$nt_gen_record" | jq -r '.chat_ref')" = "" ] \
  || fail notify_to_general "sent record carries a chat_ref — 'general' must address the shared channel, not a conversation (got: '$nt_gen_record')"
[ "$(echo "$nt_gen_record" | jq -r '.general')" = "true" ] \
  || fail notify_to_general "sent record general != true (got: '$nt_gen_record')"
[ ! -f "$NT_GEN_STUB/next-ref" ] \
  || fail notify_to_general "stub's next-ref counter was bumped — a General post must not open/consume a thread ref"
if echo "$nt_gen_out" | grep -q "FAULT"; then
  fail notify_to_general "notify direct emitted the missing---to FAULT diagnostic even though --to general was supplied (got: '$nt_gen_out')"
fi

echo "notify_to_general PASS: '--to general' still posts to the shared channel — General true, no chat_ref"

# (c) --to omitted: delivery is NOT dropped (it falls back to General), but
#     the omission is loud at depth 0 — a silently-misdelivered DM answer is
#     the failure this diagnostic exists to make visible.
NT_NONE_STUB="$T/stub-notify-to-none"; mkdir -p "$NT_NONE_STUB"
nt_none_out=$(AGENT_TEAMS_HOME="$NT_HOME" AGENT_TEAMS_STUB_DIR="$NT_NONE_STUB" AGENT_TEAMS_TRANSPORT=stub \
  ateam notify direct --file "$T/nt-body.txt" --title "Steward" 2>&1)

echo "$nt_none_out" | grep -qF "FAULT: --to was omitted" \
  || fail notify_to_absent "notify direct did not emit the depth-0 FAULT diagnostic when --to was omitted (got: '$nt_none_out')"
[ "$(wc -l < "$NT_NONE_STUB/sent.jsonl" | tr -d ' ')" = "1" ] \
  || fail notify_to_absent "the message was not delivered at all — the missing --to must warn, not drop"
nt_none_record=$(head -n1 "$NT_NONE_STUB/sent.jsonl")
[ "$(echo "$nt_none_record" | jq -r '.general')" = "true" ] \
  || fail notify_to_absent "sent record general != true — an omitted --to must fall back to the shared channel (got: '$nt_none_record')"
[ "$(echo "$nt_none_record" | jq -r '.chat_ref')" = "" ] \
  || fail notify_to_absent "sent record carries a chat_ref with no --to supplied (got: '$nt_none_record')"

echo "notify_to_absent PASS: an omitted --to still delivers to General AND is loud about it at depth 0"

echo ""
echo "ALL SCENARIOS PASSED — single-channel @mention routing (rule 1 self-mention, rule 2 foreign-bot skip x2, rule 3 no-bot-mention fallback x2, outbound notify direct), plus the DM round trip (1:1 inbound on both machines, --to ref/general/absent outbound) confirmed end-to-end against the real ateam relay/notify binary."
