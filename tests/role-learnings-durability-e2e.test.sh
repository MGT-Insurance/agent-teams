#!/usr/bin/env bash
# role-learnings-durability-e2e.test.sh — end-to-end lifecycle test for the
# durable, role-scoped learnings+ledger re-injection mechanism
# (agent-teams-7ew5.2.1 through .2.6). Drives the REAL built ateam binary
# (not a fake shim) against a temp AGENT_TEAMS_HOME with a real bd-init'd
# workspace, exercising the full round trip for BOTH roles.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PLUGIN_ROOT="$ROOT/plugins/agent-teams"
HOOKS="$PLUGIN_ROOT/hooks/scripts"
ATEAM="$PLUGIN_ROOT/bin/ateam"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

export AGENT_TEAMS_HOME="$T/ws"
export CLAUDE_PLUGIN_ROOT="$PLUGIN_ROOT"
mkdir -p "$AGENT_TEAMS_HOME"
git -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && bd init --prefix at --non-interactive >/dev/null 2>&1)

# Seed a distinctive memory per role so "contains real ateam learnings <role>
# output" is a meaningful assertion, not just "empty contains empty".
DRI_MARKER_TEXT="E2E-DRI-LEARNING-MARKER-9f31"
STEWARD_MARKER_TEXT="E2E-STEWARD-LEARNING-MARKER-6c72"
printf 'RULE: %s\nTRIGGER: test\nAPPLY: test\n' "$DRI_MARKER_TEXT" > "$T/dri-note.md"
printf 'RULE: %s\nTRIGGER: test\nAPPLY: test\n' "$STEWARD_MARKER_TEXT" > "$T/steward-note.md"
"$ATEAM" learn dri e2e-marker --file "$T/dri-note.md" >/dev/null
"$ATEAM" learn steward e2e-marker --file "$T/steward-note.md" >/dev/null

# Seed exactly one ledger record (scope-call) so the stats/recall legs below
# exercise real, meaningful filtering: scope-call recall is non-empty, the
# other four categories are legitimately empty and must be filtered out of
# role-recall-recovery.sh's per-category sweep.
"$ATEAM" steward ledger record --category=scope-call --initiative=e2e-test-initiative \
  --recommendation="e2e test recommendation" --verdict=accepted >/dev/null

expected_dri_learnings=$("$ATEAM" learnings dri)
expected_steward_learnings=$("$ATEAM" learnings steward)

printf '%s' "$expected_dri_learnings" | grep -q "$DRI_MARKER_TEXT" \
  && pass "setup: ateam learnings dri contains the seeded dri marker" \
  || fail "setup: expected seeded dri marker in ateam learnings dri output"
printf '%s' "$expected_steward_learnings" | grep -q "$STEWARD_MARKER_TEXT" \
  && pass "setup: ateam learnings steward contains the seeded steward marker" \
  || fail "setup: expected seeded steward marker in ateam learnings steward output"

# ═════════════════════════════════════════════════════════════════════════
# DRI leg
# ═════════════════════════════════════════════════════════════════════════

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/resolve-steward.sh
. "$HOOKS/lib/resolve-steward.sh"
# shellcheck source=plugins/agent-teams/hooks/scripts/lib/resolve-session-role.sh
. "$HOOKS/lib/resolve-session-role.sh"

DRI_SID="e2e-dri-session-0001"
ATH="$AGENT_TEAMS_HOME"

CLAUDE_CODE_SESSION_ID="$DRI_SID" dri_mark_session "$ATH"
marker=$(dri_session_marker_path "$ATH" "$DRI_SID")
if [ -f "$marker" ]; then
  pass "DRI leg: dri_mark_session created the marker"
else
  fail "DRI leg: dri_mark_session did not create the marker"
fi

DRI_CWD="$T/dri-plain-cwd"
mkdir -p "$DRI_CWD"

prime_out=$( (cd "$DRI_CWD" && printf '{"session_id":"%s"}' "$DRI_SID" | "$HOOKS/prime-role-learnings.sh") 2>/dev/null )
prime_ctx=$(printf '%s' "$prime_out" | jq -r '.additionalContext // empty' 2>/dev/null || true)
if printf '%s' "$prime_ctx" | grep -q "$DRI_MARKER_TEXT"; then
  pass "DRI leg: prime-role-learnings.sh additionalContext contains real ateam learnings dri output"
else
  fail "DRI leg: expected seeded dri marker in prime-role-learnings.sh output; got: $prime_out"
fi

recall_out=$( (cd "$DRI_CWD" && printf '{"session_id":"%s"}' "$DRI_SID" | "$HOOKS/role-recall-recovery.sh") 2>/dev/null )
if printf '%s' "$recall_out" | grep -q "$DRI_MARKER_TEXT"; then
  pass "DRI leg: role-recall-recovery.sh stdout contains real ateam learnings dri output"
else
  fail "DRI leg: expected seeded dri marker in role-recall-recovery.sh output; got: $recall_out"
fi
if printf '%s' "$recall_out" | grep -q "ledger"; then
  fail "DRI leg: role-recall-recovery.sh leaked ledger content into dri output"
else
  pass "DRI leg: role-recall-recovery.sh emitted no ledger content for dri role"
fi

( cd "$DRI_CWD" && printf '{"session_id":"%s"}' "$DRI_SID" | "$HOOKS/cleanup-dri-marker.sh" ) >/dev/null 2>&1
if [ -f "$marker" ]; then
  fail "DRI leg: cleanup-dri-marker.sh did not remove the marker"
else
  pass "DRI leg: cleanup-dri-marker.sh removed the marker"
fi

post_cleanup_out=$( (cd "$DRI_CWD" && printf '{"session_id":"%s"}' "$DRI_SID" | "$HOOKS/prime-role-learnings.sh") 2>/dev/null )
if [ -z "$post_cleanup_out" ]; then
  pass "DRI leg: prime-role-learnings.sh is silent after marker cleanup (no-role now)"
else
  fail "DRI leg: expected empty output after marker cleanup; got: $post_cleanup_out"
fi

# ═════════════════════════════════════════════════════════════════════════
# Steward leg
# ═════════════════════════════════════════════════════════════════════════

STEWARD_DIR="$AGENT_TEAMS_HOME/steward/session"
mkdir -p "$STEWARD_DIR"
: > "$STEWARD_DIR/.steward-session"

STEWARD_SID="e2e-steward-session-0002"

steward_prime_out=$( (cd "$STEWARD_DIR" && printf '{"session_id":"%s"}' "$STEWARD_SID" | "$HOOKS/prime-role-learnings.sh") 2>/dev/null )
steward_prime_ctx=$(printf '%s' "$steward_prime_out" | jq -r '.additionalContext // empty' 2>/dev/null || true)
if printf '%s' "$steward_prime_ctx" | grep -q "$STEWARD_MARKER_TEXT"; then
  pass "Steward leg: prime-role-learnings.sh additionalContext contains real ateam learnings steward output"
else
  fail "Steward leg: expected seeded steward marker in prime-role-learnings.sh output; got: $steward_prime_out"
fi

steward_recall_out=$( (cd "$STEWARD_DIR" && printf '{"session_id":"%s"}' "$STEWARD_SID" | "$HOOKS/role-recall-recovery.sh") 2>/dev/null )
if printf '%s' "$steward_recall_out" | grep -q "$STEWARD_MARKER_TEXT"; then
  pass "Steward leg: role-recall-recovery.sh stdout contains real ateam learnings steward output"
else
  fail "Steward leg: expected seeded steward marker in role-recall-recovery.sh output; got: $steward_recall_out"
fi
if printf '%s' "$steward_recall_out" | grep -q "scope-call"; then
  pass "Steward leg: role-recall-recovery.sh contains the seeded scope-call category recall"
else
  fail "Steward leg: expected scope-call recall content in role-recall-recovery.sh output; got: $steward_recall_out"
fi
if printf '%s' "$steward_recall_out" | grep -q "e2e-test-initiative"; then
  pass "Steward leg: role-recall-recovery.sh stdout contains real ateam steward ledger stats/recall output (seeded record)"
else
  fail "Steward leg: expected seeded ledger record content in role-recall-recovery.sh output; got: $steward_recall_out"
fi
if printf '%s' "$steward_recall_out" | grep -q "no ledger entries"; then
  fail "Steward leg: 'no ledger entries' leaked into role-recall-recovery.sh output (per-category filtering broken for the 4 empty categories)"
else
  pass "Steward leg: per-category ledger recall sweep filtered the 4 empty categories correctly"
fi

# ═════════════════════════════════════════════════════════════════════════
# Summary
# ═════════════════════════════════════════════════════════════════════════
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
