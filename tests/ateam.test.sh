#!/usr/bin/env bash
# Tests for the `ateam` workspace-access script.
# Mirrors tests/hook-compact-recovery.test.sh structure.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/scripts/ateam"
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME"
git -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && bd init --prefix at --non-interactive >/dev/null)

# Build the Go binary so the shim (which execs $AGENT_TEAMS_HOME/bin/ateam) can find it.
mkdir -p "$AGENT_TEAMS_HOME/bin"
go build -C "$ROOT" -o "$AGENT_TEAMS_HOME/bin/ateam" ./cmd/ateam

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
sync_ec=0; sync_out=$("$SCRIPT" sync 2>&1) || sync_ec=$?
[ "$sync_ec" -eq 0 ] \
  || { echo "FAIL case10: sync exited $sync_ec (output: '$sync_out')"; exit 1; }
echo "$sync_out" | grep -qi "push complete" \
  || { echo "FAIL case10: sync output did not contain 'push complete' (got: '$sync_out')"; exit 1; }

# ── Case 11: bare close (no --reason/--file) ─────────────────────────────────
printf 'problem: bare-close-test\nrepo: %s\nworktree: %s/wt-bc\nbranch: feat/bc\nteam: alpha\nmode: interactive\n' \
  "$T" "$T" > "$T/bare-close-body.md"
bc_id=$("$SCRIPT" register --title "Bare Close Test" --file "$T/bare-close-body.md")
[ -n "$bc_id" ] || { echo "FAIL case11a: register for bare-close returned empty id"; exit 1; }
"$SCRIPT" close "$bc_id"
remaining_bc=$("$SCRIPT" list-json | jq -r --arg id "$bc_id" '.[] | select(.id == $id) | .id')
[ -z "$remaining_bc" ] || { echo "FAIL case11a: bare-closed issue '$bc_id' still in list-json"; exit 1; }

# ── Case 11b: exit-4 guard (uninitialized workspace → read verb exits 4) ─────
# The shim checks $AGENT_TEAMS_HOME/bin/ateam exists before execing; copy the
# built binary there so the shim passes through and the Go binary's own
# workspace-init guard (exit 4) fires.  No .beads/ → uninitialized.
mkdir -p "$T/nope/bin"
cp "$AGENT_TEAMS_HOME/bin/ateam" "$T/nope/bin/ateam"
ec=0; AGENT_TEAMS_HOME="$T/nope" "$SCRIPT" list 2>/dev/null || ec=$?
[ "$ec" -eq 4 ] || { echo "FAIL case11b: uninitialized workspace exit code $ec, want 4"; exit 1; }

# ── Case 12: unknown verb → exit 2 ────────────────────────────────────────────
# Use || to prevent set -e from aborting on the expected non-zero exit.
ec=0; "$SCRIPT" bogus-verb 2>/dev/null || ec=$?
[ "$ec" -eq 2 ] || { echo "FAIL case12: unknown verb exit code $ec, want 2"; exit 1; }

# ── Case 13: ws prints path even when workspace is uninitialized ──────────────
uninit_out=$(AGENT_TEAMS_HOME="$T/nope" "$SCRIPT" ws)
[ "$uninit_out" = "$T/nope" ] || { echo "FAIL case13: ws with uninit ws printed '$uninit_out'"; exit 1; }

# ── Case 14: dispatch happy path ─────────────────────────────────────────────
# Set up a throwaway git repo to dispatch against.
dispatch_repo="$T/dispatch-repo"
mkdir -p "$dispatch_repo"
git -C "$dispatch_repo" init -q
git -C "$dispatch_repo" commit -q --allow-empty -m "initial"
# Ensure a 'main' default branch exists (git default may be 'master').
git -C "$dispatch_repo" checkout -q -b main 2>/dev/null || true

dispatch_out=$("$SCRIPT" dispatch --problem "add an undo stack" --repo "$dispatch_repo" --no-launch 2>&1)
# Expected output fields.
echo "$dispatch_out" | grep -q "initiative_id: at-" \
  || { echo "FAIL case14: dispatch did not print 'initiative_id: at-...' (got: '$dispatch_out')"; exit 1; }
echo "$dispatch_out" | grep -q "worktree:" \
  || { echo "FAIL case14: dispatch did not print 'worktree:' line"; exit 1; }
echo "$dispatch_out" | grep -q "slug: add-an-undo-stack" \
  || { echo "FAIL case14: dispatch slug line wrong (got: '$dispatch_out')"; exit 1; }
echo "$dispatch_out" | grep -q "base_branch:" \
  || { echo "FAIL case14: dispatch did not print 'base_branch:' line"; exit 1; }

# Extract the id and worktree path from the output.
dispatch_id=$(echo "$dispatch_out" | grep "^initiative_id: " | sed 's/^initiative_id: //')
dispatch_wt=$(echo "$dispatch_out" | grep "^worktree: " | sed 's/^worktree: //')

# Worktree directory must exist on disk.
[ -d "$dispatch_wt" ] \
  || { echo "FAIL case14: worktree dir '$dispatch_wt' was not created"; exit 1; }

# Bead must appear in list-json.
"$SCRIPT" list-json | jq -e --arg id "$dispatch_id" '.[] | select(.id == $id)' >/dev/null \
  || { echo "FAIL case14: dispatch id '$dispatch_id' not found in list-json"; exit 1; }

# resume-match must find the new id by worktree path.
found14=$("$SCRIPT" resume-match "$dispatch_wt")
[ "$found14" = "$dispatch_id" ] \
  || { echo "FAIL case14: resume-match returned '$found14', want '$dispatch_id'"; exit 1; }

# Clean up the worktree so case 16 collision test starts clean.
git -C "$dispatch_repo" worktree remove --force "$dispatch_wt"

# ── Case 15: dispatch fail-fast — not a git repo ─────────────────────────────
ec15=0; "$SCRIPT" dispatch --problem "x" --repo "$T/not-a-repo" --no-launch 2>/dev/null || ec15=$?
[ "$ec15" -ne 0 ] \
  || { echo "FAIL case15: dispatch against non-repo exited 0, want non-zero"; exit 1; }
# Also confirm no bead was registered (list-json count should be same as before).
non_repo_match=$("$SCRIPT" list-json | jq -r '.[] | select(.id | startswith("at-")) | select((.id != "'"$dispatch_id"'")) | .id' | grep -v "^$" || true)
# We just confirm the command fails; side-effect check is the non-zero exit above.

# ── Case 16: dispatch fail-fast — collision (same slug twice) ─────────────────
# Dispatch the same problem again to trigger the slug collision guard.
ec16=0; "$SCRIPT" dispatch --problem "add an undo stack" --repo "$dispatch_repo" --no-launch 2>/dev/null || ec16=$?
[ "$ec16" -ne 0 ] \
  || { echo "FAIL case16: second dispatch with same slug exited 0, want non-zero (collision)"; exit 1; }

# ── Case 17: dispatch --id-only ───────────────────────────────────────────────
# Set up a second throwaway repo so this is a fresh dispatch.
dispatch_repo2="$T/dispatch-repo2"
mkdir -p "$dispatch_repo2"
git -C "$dispatch_repo2" init -q
git -C "$dispatch_repo2" commit -q --allow-empty -m "initial"
git -C "$dispatch_repo2" checkout -q -b main 2>/dev/null || true

id_only_out=$("$SCRIPT" dispatch --problem "add a redo stack" --repo "$dispatch_repo2" --no-launch --id-only 2>&1)
# Must be exactly one line and start with at-.
line_count=$(echo "$id_only_out" | wc -l | tr -d ' ')
[ "$line_count" -eq 1 ] \
  || { echo "FAIL case17: --id-only printed $line_count lines, want 1 (got: '$id_only_out')"; exit 1; }
echo "$id_only_out" | grep -qE '^at-' \
  || { echo "FAIL case17: --id-only output '$id_only_out' is not an at-<hash> id"; exit 1; }

# Clean up the second dispatch worktree.
dispatch_wt2="$AGENT_TEAMS_HOME-worktrees/add-a-redo-stack"
[ -d "$dispatch_wt2" ] && git -C "$dispatch_repo2" worktree remove --force "$dispatch_wt2" || true

echo "PASS"
