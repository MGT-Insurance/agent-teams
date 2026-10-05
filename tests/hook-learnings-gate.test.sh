#!/usr/bin/env bash
# hook-learnings-gate.test.sh — coverage for learnings-gate.sh
# (agent-teams-75h7.2). Payload shapes are copied from real Claude Code
# 2.1.289 hook input captured in the spike (PreToolUse / PostToolUse(Agent),
# main-thread calls without agent_id, teammate calls with agent_id +
# agent_type = the spawn name).
#
# The gate must never block a non-agent-teams session: the "normal session"
# matrix asserts allow, and the "controls" assert it DOES deny when armed —
# a matrix without controls would pass with a gate that never fires.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GATE="$ROOT/plugins/agent-teams/hooks/scripts/learnings-gate.sh"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME"
GATES="$AGENT_TEAMS_HOME/learnings-gate"

SID="9e356ad7-2688-4156-99a7-33eb283597db"
OTHER="11111111-2222-3333-4444-555555555555"

REASON_DRI='BLOCKED: your agent-teams dri learnings are not loaded in this context. Every other tool call stays blocked until you load them. Read the whole output when you do. Run this Bash command by itself, exactly as written, with no period or anything else added: ateam learnings dri'
REASON_PLANNER='BLOCKED: your agent-teams planner learnings are not loaded in this context. Every other tool call stays blocked until you load them. Read the whole output when you do. Run this Bash command by itself, exactly as written, with no period or anything else added: ateam learnings planner'
REASON_REVIEWER=${REASON_PLANNER//planner/reviewer}

# ── payload builders ─────────────────────────────────────────────────────────
# pre_payload <sid> <agent_id|""> <agent_type|""> <tool_name> <tool_input_json>
pre_payload() {
  jq -nc --arg sid "$1" --arg aid "$2" --arg at "$3" --arg tn "$4" --argjson ti "$5" '
    {session_id:$sid, cwd:"/private/tmp/x", permission_mode:"bypassPermissions"}
    + (if $aid != "" then {agent_id:$aid} else {} end)
    + (if $at != "" then {agent_type:$at} else {} end)
    + {hook_event_name:"PreToolUse", tool_name:$tn, tool_input:$ti, tool_use_id:"toolu_x"}'
}
bash_in() { jq -nc --arg c "$1" '{command:$c, description:"d"}'; }
agent_in() { # agent_in <subagent_type> [name]
  if [ -n "${2:-}" ]; then
    jq -nc --arg st "$1" --arg n "$2" '{description:"d", prompt:"p", subagent_type:$st, name:$n}'
  else
    jq -nc --arg st "$1" '{description:"d", prompt:"p", subagent_type:$st}'
  fi
}
# post_agent <sid> <tool_input_json> <status> <resp_name> <resp_agent_type>
post_agent() {
  jq -nc --arg sid "$1" --argjson ti "$2" --arg st "$3" --arg rn "$4" --arg rt "$5" '
    {session_id:$sid, hook_event_name:"PostToolUse", tool_name:"Agent", tool_input:$ti,
     tool_response:{status:$st, name:$rn, agent_type:$rt, agent_id:($rn+"@session-9e356ad7")},
     tool_use_id:"toolu_x"}'
}

OUT=""; RC=0
run_gate() { # run_gate <mode> <stdin-json>
  OUT=$(printf '%s' "$2" | "$GATE" "$1" 2>/dev/null); RC=$?
}
expect_allow() { # expect_allow <desc> <mode> <json>
  run_gate "$2" "$3"
  if [ "$RC" -eq 0 ] && [ -z "$OUT" ]; then pass "$1"; else fail "$1 (rc=$RC out=$OUT)"; fi
}
expect_deny() { # expect_deny <desc> <mode> <json> <reason>
  run_gate "$2" "$3"
  local d r
  d=$(printf '%s' "$OUT" | jq -r '.hookSpecificOutput.permissionDecision // empty' 2>/dev/null)
  r=$(printf '%s' "$OUT" | jq -r '.hookSpecificOutput.permissionDecisionReason // empty' 2>/dev/null)
  if [ "$RC" -eq 0 ] && [ "$d" = "deny" ] && [ "$r" = "$4" ]; then pass "$1"; else fail "$1 (rc=$RC out=$OUT)"; fi
}
reset() { rm -rf "$GATES"; }
arm_main() { mkdir -p "$GATES/$1"; printf '%s\n' "$2" > "$GATES/$1/main"; }

# ── Normal-session matrix: no gate dir anywhere -> everything allows ─────────
reset
expect_allow "normal: Bash ls" pre "$(pre_payload "$SID" "" "" Bash "$(bash_in ls)")"
expect_allow "normal: Read" pre "$(pre_payload "$SID" "" "" Read '{"file_path":"/x"}')"
expect_allow "normal: MCP tool" pre "$(pre_payload "$SID" "" "" mcp__x__y '{}')"
expect_allow "normal: ToolSearch" pre "$(pre_payload "$SID" "" "" ToolSearch '{"query":"q"}')"
expect_allow "normal: Agent general-purpose" pre "$(pre_payload "$SID" "" "" Agent "$(agent_in general-purpose)")"
expect_allow "normal: Agent Explore" pre "$(pre_payload "$SID" "" "" Agent "$(agent_in Explore)")"
expect_allow "normal: subagent Bash" pre "$(pre_payload "$SID" "aexplore-1" Explore Bash "$(bash_in ls)")"
expect_allow "normal: ateam learnings (no gate) allowed" pre "$(pre_payload "$SID" "" "" Bash "$(bash_in 'ateam learnings dri')")"
expect_allow "normal: post Agent" post "$(post_agent "$SID" "$(agent_in general-purpose x)" teammate_spawned x general-purpose)"
if [ -e "$GATES" ]; then fail "normal matrix created a gate dir"; else pass "normal matrix created no gate dir"; fi

# Armed for ANOTHER session only: this session still allows everything.
arm_main "$OTHER" dri
expect_allow "other session armed: Bash ls" pre "$(pre_payload "$SID" "" "" Bash "$(bash_in ls)")"
expect_allow "other session armed: Read" pre "$(pre_payload "$SID" "" "" Read '{"file_path":"/x"}')"

# ── Controls: the gate DOES deny when armed ──────────────────────────────────
reset
arm_main "$SID" dri
expect_deny "control main: Bash ls denied" pre "$(pre_payload "$SID" "" "" Bash "$(bash_in ls)")" "$REASON_DRI"
expect_deny "control main: Read denied" pre "$(pre_payload "$SID" "" "" Read '{"file_path":"/x"}')" "$REASON_DRI"
expect_deny "control main: MCP tool denied" pre "$(pre_payload "$SID" "" "" mcp__x__y '{}')" "$REASON_DRI"
expect_deny "control main: Agent denied" pre "$(pre_payload "$SID" "" "" Agent "$(agent_in general-purpose)")" "$REASON_DRI"
expect_deny "control main: other role's command denied" pre "$(pre_payload "$SID" "" "" Bash "$(bash_in 'ateam learnings planner')")" "$REASON_DRI"
expect_deny "control main: piped command denied" pre "$(pre_payload "$SID" "" "" Bash "$(bash_in 'ateam learnings dri | head')")" "$REASON_DRI"
expect_deny "control main: absolute path denied" pre "$(pre_payload "$SID" "" "" Bash "$(bash_in '/x/bin/ateam learnings dri')")" "$REASON_DRI"
expect_deny "control main: chained command denied" pre "$(pre_payload "$SID" "" "" Bash "$(bash_in $'ateam learnings dri\nls')")" "$REASON_DRI"
if [ -f "$GATES/$SID/main" ]; then pass "control main: denials left marker armed"; else fail "denials cleared the marker"; fi

# ── Main-thread clear ────────────────────────────────────────────────────────
expect_allow "main: exact command allowed" pre "$(pre_payload "$SID" "" "" Bash "$(bash_in '  ateam learnings dri ')")"
if [ -f "$GATES/$SID/main" ]; then fail "main marker not removed after exact command"; else pass "main marker removed after exact command"; fi
expect_allow "main: next call allowed" pre "$(pre_payload "$SID" "" "" Bash "$(bash_in ls)")"

# Steward role is gated with its own text.
reset
arm_main "$SID" steward
expect_deny "steward: Bash ls denied" pre "$(pre_payload "$SID" "" "" Bash "$(bash_in ls)")" "${REASON_DRI//dri/steward}"
expect_allow "steward: exact command allowed" pre "$(pre_payload "$SID" "" "" Bash "$(bash_in 'ateam learnings steward')")"

# ── Main marker does not affect subagent calls ───────────────────────────────
reset
arm_main "$SID" dri
expect_allow "main armed: general-purpose subagent allowed" pre "$(pre_payload "$SID" "ag-1" general-purpose Bash "$(bash_in ls)")"
expect_allow "main armed: Explore subagent allowed" pre "$(pre_payload "$SID" "ag-2" Explore Bash "$(bash_in ls)")"
expect_allow "main armed: subagent with empty agent_type allowed" pre "$(pre_payload "$SID" "ag-3" "" Bash "$(bash_in ls)")"
expect_allow "main armed: bogus agent-teams-* subagent allowed" pre "$(pre_payload "$SID" "ag-4" agent-teams-bogus Bash "$(bash_in ls)")"
expect_allow "main armed: unknown-name teammate allowed" pre "$(pre_payload "$SID" "ag-5" nobody Bash "$(bash_in ls)")"
expect_allow "main armed: role subagent's own exact command allowed" \
  pre "$(pre_payload "$SID" "ag-6" agent-teams-reviewer Bash "$(bash_in 'ateam learnings reviewer')")"
if [ -f "$GATES/$SID/main" ]; then pass "subagent calls left main marker armed"; else fail "subagent call removed main marker"; fi

# ── Unnamed role subagent ────────────────────────────────────────────────────
reset
expect_deny "unnamed reviewer: denied even with no gate dir yet" pre "$(pre_payload "$SID" "areviewer-0" agent-teams-reviewer Bash "$(bash_in ls)")" "$REASON_REVIEWER"
reset
mkdir -p "$GATES/$SID"
expect_deny "unnamed reviewer: first call denied" pre "$(pre_payload "$SID" "areviewer-1" agent-teams-reviewer Bash "$(bash_in ls)")" "$REASON_REVIEWER"
expect_deny "unnamed reviewer: Read denied" pre "$(pre_payload "$SID" "areviewer-1" agent-teams-reviewer Read '{"file_path":"/x"}')" "$REASON_REVIEWER"
expect_allow "unnamed reviewer: exact command allowed" pre "$(pre_payload "$SID" "areviewer-1" agent-teams-reviewer Bash "$(bash_in 'ateam learnings reviewer')")"
if [ -f "$GATES/$SID/loaded/areviewer-1" ]; then pass "loaded record written"; else fail "loaded record missing"; fi
expect_allow "unnamed reviewer: later call allowed" pre "$(pre_payload "$SID" "areviewer-1" agent-teams-reviewer Bash "$(bash_in ls)")"
expect_deny "unnamed reviewer: a different agent_id is still gated" pre "$(pre_payload "$SID" "areviewer-2" agent-teams-reviewer Bash "$(bash_in ls)")" "$REASON_REVIEWER"

# ── Teammate: spawn recorded on PreToolUse(Agent) ────────────────────────────
reset
expect_allow "teammate: Agent spawn allowed" pre "$(pre_payload "$SID" "" "" Agent "$(agent_in agent-teams-planner probe-t1)")"
if [ "$(cat "$GATES/$SID/spawns/probe-t1" 2>/dev/null)" = "planner" ]; then pass "teammate: spawn recorded from tool_input.name"; else fail "teammate: spawn not recorded"; fi
expect_deny "teammate: later call denied" pre "$(pre_payload "$SID" "aprobe-t1-ad6c18a93e38fe02" probe-t1 Bash "$(bash_in 'echo hello-from-role')")" "$REASON_PLANNER"
expect_deny "teammate: wrong role command denied" pre "$(pre_payload "$SID" "aprobe-t1-ad6c18a93e38fe02" probe-t1 Bash "$(bash_in 'ateam learnings dri')")" "$REASON_PLANNER"
expect_allow "teammate: exact command allowed" pre "$(pre_payload "$SID" "aprobe-t1-ad6c18a93e38fe02" probe-t1 Bash "$(bash_in 'ateam learnings planner')")"
expect_allow "teammate: later call allowed" pre "$(pre_payload "$SID" "aprobe-t1-ad6c18a93e38fe02" probe-t1 Bash "$(bash_in 'echo hello-from-role')")"
# A wake re-fires with the same agent_id: still loaded.
expect_allow "teammate: wake (same agent_id) stays loaded" pre "$(pre_payload "$SID" "aprobe-t1-ad6c18a93e38fe02" probe-t1 SendMessage '{"to":"team-lead"}')"

# ── Dedupe: PostToolUse(Agent) records the harness-final name ────────────────
reset
mkdir -p "$GATES/$SID"
expect_allow "dedupe: post teammate_spawned" post "$(post_agent "$SID" "$(agent_in agent-teams-planner probe-t1)" teammate_spawned probe-t1-2 agent-teams-planner)"
if [ "$(cat "$GATES/$SID/spawns/probe-t1-2" 2>/dev/null)" = "planner" ]; then pass "dedupe: probe-t1-2 recorded as planner"; else fail "dedupe: probe-t1-2 not recorded"; fi
expect_deny "dedupe: call with agent_type probe-t1-2 gated as planner" pre "$(pre_payload "$SID" "aprobe-t1-2-bb" probe-t1-2 Bash "$(bash_in ls)")" "$REASON_PLANNER"
expect_allow "post: non-spawned status records nothing" post "$(post_agent "$SID" "$(agent_in agent-teams-planner zed)" completed zed agent-teams-planner)"
if [ -e "$GATES/$SID/spawns/zed" ]; then fail "post recorded a non-spawned status"; else pass "post: non-spawned status not recorded"; fi
expect_allow "post: unknown role not recorded" post "$(post_agent "$SID" "$(agent_in agent-teams-bogus q)" teammate_spawned q agent-teams-bogus)"
if [ -e "$GATES/$SID/spawns/q" ]; then fail "post recorded an unknown role"; else pass "post: unknown role not recorded"; fi

# ── Bogus agent-teams-* type, unknown role ───────────────────────────────────
reset
mkdir -p "$GATES/$SID"
expect_allow "bogus: agent_type agent-teams-bogus allowed" pre "$(pre_payload "$SID" "abogus-1" agent-teams-bogus Bash "$(bash_in ls)")"
expect_allow "bogus: Agent spawn of agent-teams-bogus allowed, not recorded" pre "$(pre_payload "$SID" "" "" Agent "$(agent_in agent-teams-bogus nm)")"
if [ -e "$GATES/$SID/spawns/nm" ]; then fail "bogus spawn recorded"; else pass "bogus spawn not recorded"; fi
expect_allow "unnamed role spawn records nothing" pre "$(pre_payload "$SID" "" "" Agent "$(agent_in agent-teams-planner)")"
if [ -d "$GATES/$SID/spawns" ]; then fail "unnamed spawn created a spawns dir"; else pass "unnamed spawn recorded nothing"; fi

# ── A denied Agent call records no spawn ─────────────────────────────────────
reset
arm_main "$SID" dri
expect_deny "denied Agent spawn" pre "$(pre_payload "$SID" "" "" Agent "$(agent_in agent-teams-planner probe-t2)")" "$REASON_DRI"
if [ -e "$GATES/$SID/spawns/probe-t2" ]; then fail "denied Agent call recorded a spawn"; else pass "denied Agent call recorded no spawn"; fi

# ── Fail open ────────────────────────────────────────────────────────────────
reset
arm_main "$SID" dri
expect_allow "fail-open: malformed JSON" pre '{not json'
expect_allow "fail-open: empty stdin" pre ''
expect_allow "fail-open: invalid session_id ../x" pre "$(pre_payload '../x' "" "" Bash "$(bash_in ls)")"
expect_allow "fail-open: empty session_id" pre "$(pre_payload '' "" "" Bash "$(bash_in ls)")"
expect_allow "fail-open: invalid agent_id" pre "$(pre_payload "$SID" "../x" agent-teams-planner Bash "$(bash_in ls)")"
expect_allow "fail-open: bad mode" bogus "$(pre_payload "$SID" "" "" Bash "$(bash_in ls)")"
expect_allow "fail-open: non-string fields" pre '{"session_id":"s1","tool_name":["x"],"tool_input":{"command":5}}'
printf 'planner\n' > "$GATES/$SID/main"
expect_allow "fail-open: main marker with a non-main role" pre "$(pre_payload "$SID" "" "" Bash "$(bash_in ls)")"
printf '../../etc\n' > "$GATES/$SID/main"
expect_allow "fail-open: main marker with a garbage role" pre "$(pre_payload "$SID" "" "" Bash "$(bash_in ls)")"

# Missing jq (empty PATH, system bash): allow, exit 0, empty stdout.
arm_main "$SID" dri
EMPTY="$T/nojqbin"; mkdir -p "$EMPTY"; ln -s "$(command -v cat)" "$EMPTY/cat"
out=$(pre_payload "$SID" "" "" Bash "$(bash_in ls)" | PATH="$EMPTY" /bin/bash "$GATE" pre 2>/dev/null); rc=$?
if [ "$rc" -eq 0 ] && [ -z "$out" ]; then pass "fail-open: jq missing"; else fail "jq missing (rc=$rc out=$out)"; fi

# Unreadable gate root / roles dir resolution failures do not block.
rm -rf "$GATES"; mkdir -p "$GATES/$SID"; chmod 000 "$GATES/$SID"
expect_allow "fail-open: unreadable gate root" pre "$(pre_payload "$SID" "areviewer-1" agent-teams-reviewer Bash "$(bash_in ls)")"
chmod 755 "$GATES/$SID"

# AGENT_TEAMS_HOME missing entirely.
out=$(pre_payload "$SID" "" "" Bash "$(bash_in ls)" | AGENT_TEAMS_HOME="$T/nope" "$GATE" pre 2>/dev/null); rc=$?
if [ "$rc" -eq 0 ] && [ -z "$out" ]; then pass "fail-open: missing AGENT_TEAMS_HOME dir"; else fail "missing ATH (rc=$rc out=$out)"; fi

# ── Logging: only on deny / clear / spawn-record, never per allowed call ─────
reset
LOG="$AGENT_TEAMS_HOME/debug/hooks.log"
rm -f "$LOG"
expect_allow "log: normal calls" pre "$(pre_payload "$SID" "" "" Bash "$(bash_in ls)")"
if [ -e "$LOG" ]; then fail "allowed call wrote to hooks.log"; else pass "allowed call wrote nothing to hooks.log"; fi
arm_main "$SID" dri
run_gate pre "$(pre_payload "$SID" "" "" Bash "$(bash_in ls)")"
run_gate pre "$(pre_payload "$SID" "" "" Bash "$(bash_in 'ateam learnings dri')")"
if grep -q "deny" "$LOG" 2>/dev/null && grep -q "clear" "$LOG" 2>/dev/null; then pass "log: deny and clear noted"; else fail "log: missing deny/clear notes"; fi

echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
