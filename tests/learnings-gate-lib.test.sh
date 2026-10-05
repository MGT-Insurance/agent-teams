#!/usr/bin/env bash
# learnings-gate-lib.test.sh — coverage for lib/learnings-gate.sh
# (agent-teams-75h7.1 CONTRACT).
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/plugins/agent-teams/hooks/scripts/lib/learnings-gate.sh"
ROLES="$ROOT/plugins/agent-teams/roles"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }
check() { # check <desc> <cmd...>
  local d="$1"; shift
  if "$@"; then pass "$d"; else fail "$d"; fi
}
check_not() { # check_not <desc> <cmd...>
  local d="$1"; shift
  if "$@"; then fail "$d"; else pass "$d"; fi
}

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
ATH="$T/ws"; mkdir -p "$ATH"
SID="sess-0001"

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/learnings-gate.sh
. "$LIB"

# ── lg_root path validation ──────────────────────────────────────────────────
check "lg_root valid" test "$(lg_root "$ATH" "$SID")" = "$ATH/learnings-gate/$SID"
check_not "lg_root rejects ../" lg_root "$ATH" "../x"
check_not "lg_root rejects slash" lg_root "$ATH" "a/b"
check_not "lg_root rejects empty sid" lg_root "$ATH" ""
check_not "lg_root rejects empty ath" lg_root "" "$SID"
check_not "lg_root rejects 129-char id" lg_root "$ATH" "$(printf 'a%.0s' $(seq 1 129))"

# ── arm / clear main ─────────────────────────────────────────────────────────
check_not "arm rejects unknown role" lg_arm_main "$ATH" "$SID" planner
check_not "arm rejects bad sid" lg_arm_main "$ATH" "../x" dri
check "arm dri" lg_arm_main "$ATH" "$SID" dri
check "main marker content" test "$(cat "$ATH/learnings-gate/$SID/main")" = "dri"
check "clear main" lg_clear_main "$ATH" "$SID"
check_not "main marker gone" test -e "$ATH/learnings-gate/$SID/main"
check "clear main idempotent" lg_clear_main "$ATH" "$SID"
check "arm steward" lg_arm_main "$ATH" "$SID" steward
check "steward content" test "$(cat "$ATH/learnings-gate/$SID/main")" = "steward"

# ── known role ───────────────────────────────────────────────────────────────
check "known: planner" lg_known_subagent_role planner "$ROLES"
check "known: implementer" lg_known_subagent_role implementer "$ROLES"
check_not "known: README excluded" lg_known_subagent_role README "$ROLES"
check_not "known: bogus" lg_known_subagent_role bogus "$ROLES"
check_not "known: traversal" lg_known_subagent_role "../roles/planner" "$ROLES"
check_not "known: dri is not a subagent role" lg_known_subagent_role dri "$ROLES"

# ── spawn record + resolution ────────────────────────────────────────────────
check "record spawn" lg_record_spawn "$ATH" "$SID" probe-t1 planner
check "spawn file content" test "$(cat "$ATH/learnings-gate/$SID/spawns/probe-t1")" = "planner"
check_not "record rejects slash name" lg_record_spawn "$ATH" "$SID" "a/b" planner
check_not "record rejects traversal name" lg_record_spawn "$ATH" "$SID" ".." planner
check_not "record rejects bad sid" lg_record_spawn "$ATH" "../x" probe planner

check "resolve (a) unnamed role" test "$(lg_resolve_subagent_role "$ATH" "$SID" agent-teams-reviewer "$ROLES")" = "reviewer"
check "resolve (b) teammate" test "$(lg_resolve_subagent_role "$ATH" "$SID" probe-t1 "$ROLES")" = "planner"
check "resolve (c) unknown name" test -z "$(lg_resolve_subagent_role "$ATH" "$SID" nobody "$ROLES")"
check "resolve bogus agent-teams-*" test -z "$(lg_resolve_subagent_role "$ATH" "$SID" agent-teams-bogus "$ROLES")"
check "resolve general-purpose" test -z "$(lg_resolve_subagent_role "$ATH" "$SID" general-purpose "$ROLES")"
check "resolve empty agent_type" test -z "$(lg_resolve_subagent_role "$ATH" "$SID" "" "$ROLES")"
check "resolve other session isolated" test -z "$(lg_resolve_subagent_role "$ATH" other-sess probe-t1 "$ROLES")"
printf 'bogus\n' > "$ATH/learnings-gate/$SID/spawns/probe-t1"
check "resolve spawn with unknown role" test -z "$(lg_resolve_subagent_role "$ATH" "$SID" probe-t1 "$ROLES")"

# ── allow match ──────────────────────────────────────────────────────────────
check "allow exact" lg_is_allowed_command "ateam learnings planner" planner
check "allow trimmed" lg_is_allowed_command "  ateam learnings planner " planner
check "allow trimmed newline" lg_is_allowed_command $'ateam learnings planner\n' planner
check_not "deny pipe" lg_is_allowed_command "ateam learnings planner | head" planner
check_not "deny other role" lg_is_allowed_command "ateam learnings dri" planner
check_not "deny absolute path" lg_is_allowed_command "/x/bin/ateam learnings planner" planner
check_not "deny 2>&1" lg_is_allowed_command "ateam learnings planner 2>&1" planner
check_not "deny second line" lg_is_allowed_command $'ateam learnings planner\nrm -rf /' planner
check_not "deny chained" lg_is_allowed_command "ateam learnings planner; ls" planner
check_not "deny empty command" lg_is_allowed_command "" planner
check_not "deny empty role" lg_is_allowed_command "ateam learnings " ""

# ── loaded record ────────────────────────────────────────────────────────────
check "mark loaded" lg_mark_loaded "$ATH" "$SID" aprobe-abc123
check "loaded file exists" test -f "$ATH/learnings-gate/$SID/loaded/aprobe-abc123"
check_not "mark loaded rejects slash" lg_mark_loaded "$ATH" "$SID" "a/b"
check_not "mark loaded rejects @ id" lg_mark_loaded "$ATH" "$SID" "probe-t1@session-x"

# ── cleanup ──────────────────────────────────────────────────────────────────
lg_arm_main "$ATH" other-sess dri
lg_cleanup_session "$ATH" "../x"
check "cleanup invalid id is a no-op" test -d "$ATH/learnings-gate/$SID"
lg_cleanup_session "$ATH" "$SID"
check_not "cleanup removes whole root" test -e "$ATH/learnings-gate/$SID"
check "cleanup leaves other session" test -f "$ATH/learnings-gate/other-sess/main"
check "cleanup of absent root ok" lg_cleanup_session "$ATH" never-existed

# ── deny reason (verbatim) ───────────────────────────────────────────────────
EXPECT='BLOCKED: your agent-teams planner learnings are not loaded in this context. Every other tool call stays blocked until you load them. Read the whole output when you do. Run this Bash command by itself, exactly as written, with no period or anything else added: ateam learnings planner'
check "deny reason verbatim" test "$(lg_deny_reason planner)" = "$EXPECT"
check "deny reason ends exactly with the command" \
  test "$(lg_deny_reason planner | sed 's/.*: //')" = "ateam learnings planner"

echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
