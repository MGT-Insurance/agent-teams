#!/usr/bin/env bash
# multi-machine-routing.test.sh — two-machine exactly-once routing test
# (agent-teams-5y8a.6, the loop-closing verification for the multi-machine
# steward/relay initiative agent-teams-5y8a). Modeled on tests/steward-loop.test.sh
# (same isolation pattern: temp AGENT_TEAMS_HOME, scratch bd db, stub
# transport, fake claude shim) but simulates TWO machines instead of one: each
# "machine" is its own AGENT_TEAMS_HOME (own bd db, own steward dir, own stub
# transport dir). A shared inbound reply is modeled by writing the SAME
# reply-*.json content into both machines' stub dirs and running the real
# `ateam relay` binary against each — Design A's privacy-off fan-out, where
# every machine's relay receives every human message and must independently
# decide whether it owns the reply (agent-teams-5y8a.5's relay-gating).
#
# Exercises, across real process boundaries, the four ownership decisions
# relay-gating makes in internal/verbs/relay.go's handleReply:
#
#   1. TIED reply (thread resolves to exactly one open initiative): only the
#      machine whose disk holds that initiative's registered worktree/branch/
#      repo checkout (claimsInitiativeLocally, routing_ownership.go) routes
#      it; the other machine sees the identical reply and suppresses it.
#   2. UNTIED reply (thread resolves to no open initiative, and no closed
#      initiative either): only the designated fallback responder
#      (isFallbackResponder: StewardFallbackMarkerPath + a live steward
#      marker, routing_ownership.go) routes it as a steward-unrouted
#      envelope; every other machine suppresses it.
#   3. PEER TOPIC (thread ref is a KNOWN steward topic belonging to ANOTHER
#      machine, synced via steward:topics:<hostname>, steward_topics.go):
#      skipped outright, before the bd label query ever runs — that peer's
#      own relay already routes it locally. A machine's OWN local briefing
#      thread ref is never subject to this skip; it always short-circuits to
#      a steward-briefing-reply envelope first (checked earlier in
#      handleReply, ahead of the peer-topic skip). NOTE (agent-teams-4x83):
#      the synced record used to also carry a per-machine "direct" thread
#      ref, matched the same way; that [Direct] topic machinery has been
#      ripped out (steward-direct traffic is now @mention-routed in the
#      shared General channel instead — see
#      tests/single-channel-mention-routing.test.sh) and
#      StewardTopicsRecord.Direct no longer exists as a field. A peer record
#      still carrying a legacy "direct" JSON key parses cleanly (Go ignores
#      unknown fields) but confers no routing behavior — scenario 3 below
#      seeds one and proves it's inert.
#   4. NON-TOPIC reply (ThreadRef == "", e.g. a General-topic/DM message):
#      only the designated fallback responder forwards it (as a
#      steward-unrouted envelope carrying the "(general)" placeholder);
#      every other machine keeps the original silent skip.
#
# Each scenario asserts the exactly-once property: routed on exactly one
# side (one steward mail bead created there), skipped on the other (zero
# steward mail beads there) — never both, never neither.
#
# Build: requires -tags e2e for the stub transport (same as steward-loop.test.sh).
# Run:   bash tests/multi-machine-routing.test.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

fail() { echo "FAIL $1: $2"; exit 1; }

# ── binary build (same pattern as steward-loop.test.sh) ─────────────────────

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

# Fake claude shim (belt-and-suspenders — see steward-loop.test.sh's identical
# comment): every mail send this test triggers is steward-addressed
# (relay's defaultRelaySend hardcodes StewardHandle), which short-circuits
# the liveness check before agentsFunc is ever called since "steward" is not
# a bd initiative bead. This shim just guarantees that even if some path did
# reach it, no real background claude session gets spawned.
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

# ── helpers ───────────────────────────────────────────────────────────────

# mkhome creates a fresh AGENT_TEAMS_HOME-shaped workspace: a git repo (bd
# requires one) with an initialized bd db. Mirrors steward-loop.test.sh's
# workspace setup, factored out since this test creates several.
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

# drain_steward_inbox marks all of home's open steward messages read so a
# later scenario's "exactly N open messages" assertion isn't polluted by an
# earlier scenario's leftovers, when a home is reused within one scenario.
drain_steward_inbox() {
  local home="$1" dir="$2"
  mkdir -p "$dir"
  (cd "$dir" && AGENT_TEAMS_HOME="$home" ateam mail inbox >/dev/null 2>&1) || true
}

echo "=== Scenario 1: TIED reply -> owner machine routes, other skips ==="

# A real local git checkout — this is what "machine A's disk" has. Machine
# B's copy of the initiative record (below) points at a worktree path that
# is never created, simulating the same initiative's fields as recorded on
# a machine that never checked it out (agent-teams-5y8a.2's
# claimsInitiativeLocally fails closed on a missing path).
TIED_REPO="$T/tied-repo"
mkdir -p "$TIED_REPO"
git -C "$TIED_REPO" init -q
git -C "$TIED_REPO" config user.email test@example.com
git -C "$TIED_REPO" config user.name Test
git -C "$TIED_REPO" commit --allow-empty -q -m init
TIED_WT="$T/tied-wt"
git -C "$TIED_REPO" worktree add "$TIED_WT" -b feat/tied -q

S1A="$T/home-s1a"
S1B="$T/home-s1b"
mkhome "$S1A"
mkhome "$S1B"

TIED_REF="tied-ref-1"

printf 'problem: tied initiative\nrepo: %s\nworktree: %s\nbranch: feat/tied\n' "$TIED_REPO" "$TIED_WT" > "$T/tied-body-a.md"
tied_id_a=$(bd -C "$S1A" create --title="Tied Initiative" --type=task --priority=2 --body-file="$T/tied-body-a.md" --json | jq -r '.id')
[ -n "$tied_id_a" ] && [ "$tied_id_a" != "null" ] || fail case1 "machine A: bd create returned no id"
bd -C "$S1A" label add "$tied_id_a" "thread:$TIED_REF" >/dev/null

printf 'problem: tied initiative\nrepo: %s\nworktree: %s\nbranch: feat/tied\n' "$TIED_REPO" "$T/tied-wt-not-on-b" > "$T/tied-body-b.md"
tied_id_b=$(bd -C "$S1B" create --title="Tied Initiative" --type=task --priority=2 --body-file="$T/tied-body-b.md" --json | jq -r '.id')
[ -n "$tied_id_b" ] && [ "$tied_id_b" != "null" ] || fail case1 "machine B: bd create returned no id"
bd -C "$S1B" label add "$tied_id_b" "thread:$TIED_REF" >/dev/null

S1A_STUB="$T/stub-s1a"; mkdir -p "$S1A_STUB"
S1B_STUB="$T/stub-s1b"; mkdir -p "$S1B_STUB"

tied_reply_text="Approved for the tied initiative."
printf '{"thread_ref": "%s", "text": "%s"}\n' "$TIED_REF" "$tied_reply_text" > "$S1A_STUB/reply-001.json"
printf '{"thread_ref": "%s", "text": "%s"}\n' "$TIED_REF" "$tied_reply_text" > "$S1B_STUB/reply-001.json"

s1a_out=$(run_relay "$S1A" "$S1A_STUB")
echo "$s1a_out" | grep -q "starting on transport" \
  || fail case1a "machine A relay did not print starting line (got: '$s1a_out')"
[ ! -f "$S1A_STUB/reply-001.json" ] || fail case1a "machine A stub did not consume reply-001.json"

s1a_count=$(steward_msg_count "$S1A")
[ "$s1a_count" = "1" ] || fail case1a "expected machine A to route exactly 1 steward message for the tied reply, got $s1a_count"
s1a_body=$(steward_open_msgs "$S1A" | jq -r '.[0].description')
echo "$s1a_body" | grep -qF "<<<steward-reply initiative:$tied_id_a>>>" \
  || fail case1a "machine A message missing steward-reply envelope for $tied_id_a (got: '$s1a_body')"
echo "$s1a_body" | grep -qF "$tied_reply_text" \
  || fail case1a "machine A message missing reply text (got: '$s1a_body')"

echo "case1a PASS: machine A (holds the tied initiative's checkout) routed the reply to its steward as $tied_id_a"

s1b_out=$(run_relay "$S1B" "$S1B_STUB")
echo "$s1b_out" | grep -q "starting on transport" \
  || fail case1b "machine B relay did not print starting line (got: '$s1b_out')"
echo "$s1b_out" | grep -qi "not claimed locally" \
  || fail case1b "machine B relay stderr did not mention 'not claimed locally' (got: '$s1b_out')"
[ ! -f "$S1B_STUB/reply-001.json" ] || fail case1b "machine B stub did not consume reply-001.json"

s1b_count=$(steward_msg_count "$S1B")
[ "$s1b_count" = "0" ] || fail case1b "expected machine B to route 0 steward messages for the tied reply (it doesn't hold the checkout), got $s1b_count"

echo "case1b PASS: machine B (no local checkout for $tied_id_b) suppressed the identical reply — exactly-once confirmed"

echo ""
echo "=== Scenario 2: UNTIED reply -> only the fallback responder routes ==="

S2A="$T/home-s2a"
S2B="$T/home-s2b"
mkhome "$S2A"
mkhome "$S2B"

# Machine A: designated fallback responder (marker file) + a live steward
# session (StewardSessionMarkerPath, via the real `ateam steward init`).
mkdir -p "$S2A/steward"
: > "$S2A/steward/fallback-responder"
AGENT_TEAMS_HOME="$S2A" ateam steward init >/dev/null

# Machine B: neither marker — a fresh, untouched home.

UNTIED_REF="untied-ref-1"
S2A_STUB="$T/stub-s2a"; mkdir -p "$S2A_STUB"
S2B_STUB="$T/stub-s2b"; mkdir -p "$S2B_STUB"

untied_reply_text="What is the status here?"
printf '{"thread_ref": "%s", "text": "%s"}\n' "$UNTIED_REF" "$untied_reply_text" > "$S2A_STUB/reply-001.json"
printf '{"thread_ref": "%s", "text": "%s"}\n' "$UNTIED_REF" "$untied_reply_text" > "$S2B_STUB/reply-001.json"

s2a_out=$(run_relay "$S2A" "$S2A_STUB")
echo "$s2a_out" | grep -q "starting on transport" \
  || fail case2a "machine A relay did not print starting line (got: '$s2a_out')"
[ ! -f "$S2A_STUB/reply-001.json" ] || fail case2a "machine A stub did not consume reply-001.json"

s2a_count=$(steward_msg_count "$S2A")
[ "$s2a_count" = "1" ] || fail case2a "expected fallback machine A to route exactly 1 steward message for the untied reply, got $s2a_count"
s2a_body=$(steward_open_msgs "$S2A" | jq -r '.[0].description')
echo "$s2a_body" | grep -qF "<<<steward-unrouted thread:$UNTIED_REF reason:" \
  || fail case2a "machine A message missing steward-unrouted envelope header (got: '$s2a_body')"
echo "$s2a_body" | grep -qF "$untied_reply_text" \
  || fail case2a "machine A message missing reply text (got: '$s2a_body')"

echo "case2a PASS: fallback-designated machine A routed the untied reply as a steward-unrouted envelope"

s2b_out=$(run_relay "$S2B" "$S2B_STUB")
echo "$s2b_out" | grep -q "starting on transport" \
  || fail case2b "machine B relay did not print starting line (got: '$s2b_out')"
echo "$s2b_out" | grep -qi "not fallback responder" \
  || fail case2b "machine B relay stderr did not mention 'not fallback responder' (got: '$s2b_out')"
[ ! -f "$S2B_STUB/reply-001.json" ] || fail case2b "machine B stub did not consume reply-001.json"

s2b_count=$(steward_msg_count "$S2B")
[ "$s2b_count" = "0" ] || fail case2b "expected non-fallback machine B to route 0 steward messages for the untied reply, got $s2b_count"

echo "case2b PASS: non-fallback machine B suppressed the identical untied reply — exactly-once confirmed"

echo ""
echo "=== Scenario 3: PEER TOPIC -> skipped; OWN briefing topic still short-circuits ==="

S3A="$T/home-s3a"
mkhome "$S3A"
S3A_STUB="$T/stub-s3a"; mkdir -p "$S3A_STUB"

printf 'Daily digest: all initiatives nominal.\n' > "$T/s3-briefing.txt"
s3_notify_out=$(AGENT_TEAMS_HOME="$S3A" AGENT_TEAMS_STUB_DIR="$S3A_STUB" AGENT_TEAMS_TRANSPORT=stub \
  ateam notify briefing --file "$T/s3-briefing.txt" --title "Daily Briefing" 2>&1)
echo "$s3_notify_out" | grep -q "thread_ref:" \
  || fail case3 "notify briefing output missing thread_ref line (got: '$s3_notify_out')"
own_briefing_ref=$(echo "$s3_notify_out" | grep "^thread_ref: " | sed 's/^thread_ref: //')
[ -n "$own_briefing_ref" ] || fail case3 "could not extract own briefing thread_ref from: '$s3_notify_out'"

# Seed a PEER machine's synced steward-topics record directly into machine
# A's memory store (this is the dolt-synced record a real peer's `ateam
# notify` would have published; hardcoding it here is the sanctioned
# shortcut — see steward_topics.go's doc comment — since the goal is the
# routing property, not real cross-machine dolt sync).
peer_briefing_ref="peer-briefing-ref"
bd -C "$S3A" remember --key=steward:topics:sim-peer-machine \
  "{\"briefing\":\"$peer_briefing_ref\",\"direct\":\"peer-direct-ref\"}" >/dev/null

# ── 3a: reply on the PEER's briefing ref -> machine A skips outright ────────
peer_reply_text="Nice work over there."
printf '{"thread_ref": "%s", "text": "%s"}\n' "$peer_briefing_ref" "$peer_reply_text" > "$S3A_STUB/reply-001.json"

s3a_out=$(run_relay "$S3A" "$S3A_STUB")
echo "$s3a_out" | grep -q "starting on transport" \
  || fail case3a "relay did not print starting line (got: '$s3a_out')"
echo "$s3a_out" | grep -qi "skipping peer steward topic" \
  || fail case3a "relay stderr did not mention skipping the peer steward topic (got: '$s3a_out')"
[ ! -f "$S3A_STUB/reply-001.json" ] || fail case3a "stub did not consume reply-001.json"

s3a_count=$(steward_msg_count "$S3A")
[ "$s3a_count" = "0" ] || fail case3a "expected 0 steward messages for a reply on a peer's own topic, got $s3a_count"

echo "case3a PASS: machine A recognized $peer_briefing_ref as a peer steward topic and emitted nothing"

# ── 3b: reply on machine A's OWN briefing ref -> still short-circuits ──────
own_reply_text="Great briefing, thanks."
printf '{"thread_ref": "%s", "text": "%s"}\n' "$own_briefing_ref" "$own_reply_text" > "$S3A_STUB/reply-001.json"

s3b_out=$(run_relay "$S3A" "$S3A_STUB")
echo "$s3b_out" | grep -q "starting on transport" \
  || fail case3b "relay did not print starting line (got: '$s3b_out')"
[ ! -f "$S3A_STUB/reply-001.json" ] || fail case3b "stub did not consume reply-001.json"

s3b_count=$(steward_msg_count "$S3A")
[ "$s3b_count" = "1" ] || fail case3b "expected 1 steward message for a reply on machine A's own briefing thread, got $s3b_count"
s3b_body=$(steward_open_msgs "$S3A" | jq -r '.[0].description')
echo "$s3b_body" | grep -qF "<<<steward-briefing-reply>>>" \
  || fail case3b "message missing steward-briefing-reply envelope header (got: '$s3b_body')"
echo "$s3b_body" | grep -qF "$own_reply_text" \
  || fail case3b "message missing reply text (got: '$s3b_body')"

echo "case3b PASS: machine A's own briefing-topic short-circuit still fired despite the peer-topic skip logic above it in the routing order"

# Close case3b's own-briefing-reply message directly so case3c's "exactly 0"
# assertion below isn't polluted by it (both reuse machine A's home S3A).
# NOT drain_steward_inbox: `ateam mail inbox` resolves its recipient via
# resolveInboxRecipient(cwd), which requires either a live Steward session
# marker file or an open initiative bead whose worktree: line matches cwd —
# neither exists in this bare bd home, so it silently no-ops (returns nil
# without draining anything; see agent-teams-4x83 discovery bead). A direct
# bd close is the reliable way to retire the message bead here.
s3b_msg_id=$(steward_open_msgs "$S3A" | jq -r '.[0].id')
bd -C "$S3A" close "$s3b_msg_id" >/dev/null

# ── 3c: reply on the peer's LEGACY "direct" ref -> NOT a peer-topic skip ───
#
# agent-teams-4x83.4 dropped StewardTopicsRecord.Direct from the schema; the
# "direct":"peer-direct-ref" key seeded above (case3's remember call) is
# tolerated-but-ignored — it still parses (Go ignores unknown JSON fields,
# locked in by TestParseStewardTopicsRecord_ToleratesLegacyDirectField), but
# isKnownStewardTopic now matches on Briefing only, so it confers NO routing
# behavior. A reply whose thread ref equals that legacy value must NOT hit
# the peer-topic-skip branch (no "skipping peer steward topic" log) — it
# falls through to the ordinary bd-label/untied path like any other
# unrecognized thread ref, and is suppressed there instead (machine A is not
# configured as the fallback responder in this scenario).
legacy_peer_direct_ref="peer-direct-ref"
legacy_reply_text="Anyone home?"
printf '{"thread_ref": "%s", "text": "%s"}\n' "$legacy_peer_direct_ref" "$legacy_reply_text" > "$S3A_STUB/reply-001.json"

s3c_out=$(run_relay "$S3A" "$S3A_STUB")
echo "$s3c_out" | grep -q "starting on transport" \
  || fail case3c "relay did not print starting line (got: '$s3c_out')"
echo "$s3c_out" | grep -qi "skipping peer steward topic" \
  && fail case3c "relay treated the legacy peer 'direct' ref as a peer steward topic — Direct must be inert (got: '$s3c_out')"
echo "$s3c_out" | grep -qi "not fallback responder" \
  || fail case3c "relay stderr did not fall through to the ordinary untied/non-fallback skip (got: '$s3c_out')"
[ ! -f "$S3A_STUB/reply-001.json" ] || fail case3c "stub did not consume reply-001.json"

s3c_count=$(steward_msg_count "$S3A")
[ "$s3c_count" = "0" ] || fail case3c "expected 0 steward messages for a reply on the legacy peer-direct ref, got $s3c_count"

echo "case3c PASS: the peer's legacy 'direct' JSON key is tolerated but confers no peer-topic-skip routing — the reply fell through to the ordinary untied/non-fallback path instead"

echo ""
echo "=== Scenario 4: NON-TOPIC reply (empty ThreadRef) -> only the fallback responder routes ==="

S4A="$T/home-s4a"
S4B="$T/home-s4b"
mkhome "$S4A"
mkhome "$S4B"

mkdir -p "$S4A/steward"
: > "$S4A/steward/fallback-responder"
AGENT_TEAMS_HOME="$S4A" ateam steward init >/dev/null

# Machine B: neither marker.

S4A_STUB="$T/stub-s4a"; mkdir -p "$S4A_STUB"
S4B_STUB="$T/stub-s4b"; mkdir -p "$S4B_STUB"

general_reply_text="Hello from the general topic."
printf '{"thread_ref": "", "text": "%s"}\n' "$general_reply_text" > "$S4A_STUB/reply-001.json"
printf '{"thread_ref": "", "text": "%s"}\n' "$general_reply_text" > "$S4B_STUB/reply-001.json"

s4a_out=$(run_relay "$S4A" "$S4A_STUB")
echo "$s4a_out" | grep -q "starting on transport" \
  || fail case4a "machine A relay did not print starting line (got: '$s4a_out')"
echo "$s4a_out" | grep -qi "routing non-topic message to steward" \
  || fail case4a "machine A relay stderr did not mention routing the non-topic message (got: '$s4a_out')"
[ ! -f "$S4A_STUB/reply-001.json" ] || fail case4a "machine A stub did not consume reply-001.json"

s4a_count=$(steward_msg_count "$S4A")
[ "$s4a_count" = "1" ] || fail case4a "expected fallback machine A to route exactly 1 steward message for the non-topic reply, got $s4a_count"
s4a_body=$(steward_open_msgs "$S4A" | jq -r '.[0].description')
echo "$s4a_body" | grep -qF "<<<steward-unrouted thread:(general) reason:" \
  || fail case4a "machine A message missing steward-unrouted envelope with (general) placeholder (got: '$s4a_body')"
echo "$s4a_body" | grep -qF "$general_reply_text" \
  || fail case4a "machine A message missing reply text (got: '$s4a_body')"

echo "case4a PASS: fallback-designated machine A routed the non-topic reply as a steward-unrouted envelope (thread:(general))"

s4b_out=$(run_relay "$S4B" "$S4B_STUB")
echo "$s4b_out" | grep -q "starting on transport" \
  || fail case4b "machine B relay did not print starting line (got: '$s4b_out')"
echo "$s4b_out" | grep -qi "skipping non-topic message" \
  || fail case4b "machine B relay stderr did not mention skipping the non-topic message (got: '$s4b_out')"
[ ! -f "$S4B_STUB/reply-001.json" ] || fail case4b "machine B stub did not consume reply-001.json"

s4b_count=$(steward_msg_count "$S4B")
[ "$s4b_count" = "0" ] || fail case4b "expected non-fallback machine B to route 0 steward messages for the non-topic reply, got $s4b_count"

echo "case4b PASS: non-fallback machine B suppressed the identical non-topic reply — exactly-once confirmed"

echo ""
echo "ALL SCENARIOS PASSED — multi-machine exactly-once routing (tied-owner, untied-fallback, peer-topic-skip + own-topic-short-circuit + legacy-direct-field-inert, non-topic-fallback) confirmed end-to-end against the real ateam relay binary."
