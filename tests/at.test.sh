#!/usr/bin/env bash
# Tests for the `at` workspace-access script.
# Mirrors tests/hook-compact-recovery.test.sh structure.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/scripts/at"
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME"
git -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && bd init --prefix at --non-interactive >/dev/null)

# ── Case 1: ws verb prints the resolved workspace path ────────────────────────
out=$("$SCRIPT" ws)
[ "$out" = "$AGENT_TEAMS_HOME" ] || { echo "FAIL case1: ws printed '$out', want '$AGENT_TEAMS_HOME'"; exit 1; }

# ── Case 2: register prints a resolvable id ───────────────────────────────────
printf 'problem: test\nrepo: %s\nworktree: %s/wt1\nbranch: feat/reg\nteam: alpha\nmode: interactive\n' \
  "$T" "$T" > "$T/reg-body.md"
reg_id=$("$SCRIPT" register --title "Registration Test" --file "$T/reg-body.md")
[ -n "$reg_id" ] || { echo "FAIL case2: register returned empty id"; exit 1; }
echo "$reg_id" | grep -qE '^at-' || { echo "FAIL case2: id '$reg_id' doesn't look like at-<hash>"; exit 1; }
# Confirm the id appears in list-json
"$SCRIPT" list-json | jq -e --arg id "$reg_id" '.[] | select(.id == $id)' >/dev/null \
  || { echo "FAIL case2: registered id '$reg_id' not found in list-json"; exit 1; }

# ── Case 3: resume-match — exact line match ───────────────────────────────────
# Create an issue with a worktree line we can query
printf 'problem: match-test\nrepo: %s\nworktree: %s/wt-match\nbranch: feat/match\nteam: beta\nmode: interactive\n' \
  "$T" "$T" > "$T/match-body.md"
match_id=$("$SCRIPT" register --title "Match Test" --file "$T/match-body.md")

found=$("$SCRIPT" resume-match "$T/wt-match")
[ "$found" = "$match_id" ] || { echo "FAIL case3a: resume-match returned '$found', want '$match_id'"; exit 1; }

# Non-matching path → empty
found=$("$SCRIPT" resume-match "$T/no-such-worktree")
[ -z "$found" ] || { echo "FAIL case3b: resume-match returned '$found' for non-matching path, want empty"; exit 1; }

# Prefix-collision: registered /a/b/wt-match, query /a/b → must return empty (exact-line guard)
found=$("$SCRIPT" resume-match "$T")
[ -z "$found" ] || { echo "FAIL case3c: resume-match returned '$found' for prefix '$T', want empty"; exit 1; }

# ── Case 4: gate adds human label; clear-gate removes it ─────────────────────
printf 'QUESTION: Should we proceed?\n' > "$T/question.txt"
"$SCRIPT" gate "$match_id" --file "$T/question.txt" >/dev/null
# human-list should now mention match_id
"$SCRIPT" human-list | grep -q "$match_id" \
  || { echo "FAIL case4a: gate did not add human label for '$match_id'"; exit 1; }

# clear-gate with a response file
printf 'RESPONSE: Yes, proceed.\n' > "$T/response.txt"
"$SCRIPT" clear-gate "$match_id" --file "$T/response.txt" >/dev/null
# human-list should no longer mention match_id
human_out=$("$SCRIPT" human-list)
echo "$human_out" | grep -q "$match_id" \
  && { echo "FAIL case4b: clear-gate did not remove human label for '$match_id'"; exit 1; }
# (may print "No human-needed beads found." — that's fine)

# ── Case 5: clear-gate without --file also clears the label ──────────────────
printf 'QUESTION: Another question?\n' > "$T/q2.txt"
"$SCRIPT" gate "$match_id" --file "$T/q2.txt" >/dev/null
"$SCRIPT" human-list | grep -q "$match_id" \
  || { echo "FAIL case5a: second gate did not set human label"; exit 1; }
"$SCRIPT" clear-gate "$match_id" >/dev/null
human_out=$("$SCRIPT" human-list)
echo "$human_out" | grep -q "$match_id" \
  && { echo "FAIL case5b: clear-gate without --file did not remove human label"; exit 1; }

# ── Case 6: learn then learnings roundtrip ────────────────────────────────────
printf 'test insight. WHY: testing. HOW TO APPLY: use it.' > "$T/insight.txt"
"$SCRIPT" learn planner round-trip-slug --file "$T/insight.txt" >/dev/null
learnings_out=$("$SCRIPT" learnings planner)
echo "$learnings_out" | grep -q "round-trip-slug" \
  || { echo "FAIL case6: learnings did not return round-trip-slug"; exit 1; }
echo "$learnings_out" | grep -q "test insight" \
  || { echo "FAIL case6: learnings did not return insight content"; exit 1; }

# ── Case 7: note appends to issue ─────────────────────────────────────────────
# Capture to variable before grepping: piping bd show directly to grep -q
# triggers SIGPIPE (bd show exits 141) which pipefail converts to a pipeline
# failure — even when grep -q finds the match and exits 0.
printf 'Some extra note.\n' > "$T/note.txt"
"$SCRIPT" note "$match_id" --file "$T/note.txt" >/dev/null
show_after_note=$("$SCRIPT" show "$match_id")
echo "$show_after_note" | grep -q "Some extra note" \
  || { echo "FAIL case7: note not visible in show output"; exit 1; }

# ── Case 8: show returns issue content ────────────────────────────────────────
show_out=$("$SCRIPT" show "$match_id")
echo "$show_out" | grep -q "Match Test" \
  || { echo "FAIL case8: show did not contain issue title"; exit 1; }

# ── Case 9: close ─────────────────────────────────────────────────────────────
"$SCRIPT" close "$match_id" --reason "test done" >/dev/null
# Should no longer appear in list-json
remaining=$("$SCRIPT" list-json | jq -r --arg id "$match_id" '.[] | select(.id == $id) | .id')
[ -z "$remaining" ] || { echo "FAIL case9: closed issue '$match_id' still appears in list-json"; exit 1; }

# ── Case 10: sync — set up local bare remote then push ────────────────────────
bare="$T/remote.git"
git init --bare -q "$bare"
git -C "$AGENT_TEAMS_HOME" remote add origin "$bare"
git -C "$AGENT_TEAMS_HOME" add -A
git -C "$AGENT_TEAMS_HOME" commit -q -m "initial commit"
git -C "$AGENT_TEAMS_HOME" push -q origin main
bd -C "$AGENT_TEAMS_HOME" dolt remote add origin "$bare"
sync_out=$("$SCRIPT" sync 2>&1)
echo "$sync_out" | grep -qi "push" \
  || { echo "FAIL case10: sync output did not mention push (got: '$sync_out')"; exit 1; }

# ── Case 11: unknown verb → exit 2 ────────────────────────────────────────────
# Use || to prevent set -e from aborting on the expected non-zero exit.
ec=0; "$SCRIPT" bogus-verb 2>/dev/null || ec=$?
[ "$ec" -eq 2 ] || { echo "FAIL case11: unknown verb exit code $ec, want 2"; exit 1; }

# ── Case 12: ws prints path even when workspace is uninitialized ──────────────
uninit_out=$(AGENT_TEAMS_HOME="$T/nope" "$SCRIPT" ws)
[ "$uninit_out" = "$T/nope" ] || { echo "FAIL case12: ws with uninit ws printed '$uninit_out'"; exit 1; }

echo "PASS"
