#!/usr/bin/env bash
# Tests for the `ateam` binary exercised through the POSIX dispatch wrapper.
# All cases call bare `ateam` via the wrapper so they validate the shipped model.
# Mirrors tests/hook-compact-recovery.test.sh structure.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME"
git -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && bd init --prefix at --non-interactive >/dev/null)

# Determine the current platform the way the wrapper does.
PLATFORM_OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
raw_arch="$(uname -m)"
case "$raw_arch" in
    x86_64)  PLATFORM_ARCH=amd64 ;;
    aarch64) PLATFORM_ARCH=arm64 ;;
    arm64)   PLATFORM_ARCH=arm64 ;;
    *)       PLATFORM_ARCH="$raw_arch" ;;
esac

# Build the current platform's binary into a temp bin/ as ateam-<os>-<arch>,
# then copy the committed dispatch wrapper alongside it.  Prepend the temp
# bin/ to PATH so every bare `ateam` invocation below goes through the wrapper.
mkdir -p "$T/bin"
go build -C "$ROOT" -o "$T/bin/ateam-${PLATFORM_OS}-${PLATFORM_ARCH}" ./cmd/ateam
cp "$ROOT/plugins/agent-teams/bin/ateam" "$T/bin/ateam"
chmod +x "$T/bin/ateam-${PLATFORM_OS}-${PLATFORM_ARCH}" "$T/bin/ateam"
export PATH="$T/bin:$PATH"

# ── Case 1: ws verb prints the resolved workspace path ────────────────────────
out=$(ateam ws)
[ "$out" = "$AGENT_TEAMS_HOME" ] || { echo "FAIL case1: ws printed '$out', want '$AGENT_TEAMS_HOME'"; exit 1; }

# ── Case 2: register prints a resolvable id ───────────────────────────────────
printf 'problem: test\nrepo: %s\nworktree: %s/wt1\nbranch: feat/reg\nteam: alpha\nmode: interactive\n' \
  "$T" "$T" > "$T/reg-body.md"
reg_id=$(ateam register --title "Registration Test" --file "$T/reg-body.md")
[ -n "$reg_id" ] || { echo "FAIL case2: register returned empty id"; exit 1; }
echo "$reg_id" | grep -qE '^at-' || { echo "FAIL case2: id '$reg_id' doesn't look like at-<hash>"; exit 1; }
# Confirm the id appears in list-json
ateam list-json | jq -e --arg id "$reg_id" '.[] | select(.id == $id)' >/dev/null \
  || { echo "FAIL case2: registered id '$reg_id' not found in list-json"; exit 1; }

# ── Case 3: resume-match — exact line match ───────────────────────────────────
printf 'problem: match-test\nrepo: %s\nworktree: %s/wt-match\nbranch: feat/match\nteam: beta\nmode: interactive\n' \
  "$T" "$T" > "$T/match-body.md"
match_id=$(ateam register --title "Match Test" --file "$T/match-body.md")

found=$(ateam resume-match "$T/wt-match")
[ "$found" = "$match_id" ] || { echo "FAIL case3a: resume-match returned '$found', want '$match_id'"; exit 1; }

# Non-matching path → empty
found=$(ateam resume-match "$T/no-such-worktree")
[ -z "$found" ] || { echo "FAIL case3b: resume-match returned '$found' for non-matching path, want empty"; exit 1; }

# Prefix-collision: registered /a/b/wt-match, query /a/b → must return empty (exact-line guard)
found=$(ateam resume-match "$T")
[ -z "$found" ] || { echo "FAIL case3c: resume-match returned '$found' for prefix '$T', want empty"; exit 1; }

# ── Case 4: gate adds human label; clear-gate removes it ─────────────────────
printf 'QUESTION: Should we proceed?\n' > "$T/question.txt"
ateam gate "$match_id" --file "$T/question.txt" >/dev/null
# human-list should now mention match_id
ateam human-list | grep -q "$match_id" \
  || { echo "FAIL case4a: gate did not add human label for '$match_id'"; exit 1; }

# clear-gate with a response file
printf 'RESPONSE: Yes, proceed.\n' > "$T/response.txt"
ateam clear-gate "$match_id" --file "$T/response.txt" >/dev/null
# human-list should no longer mention match_id
human_out=$(ateam human-list)
echo "$human_out" | grep -q "$match_id" \
  && { echo "FAIL case4b: clear-gate did not remove human label for '$match_id'"; exit 1; }
# (may print "No human-needed beads found." — that's fine)

# ── Case 5: clear-gate without --file also clears the label ──────────────────
printf 'QUESTION: Another question?\n' > "$T/q2.txt"
ateam gate "$match_id" --file "$T/q2.txt" >/dev/null
ateam human-list | grep -q "$match_id" \
  || { echo "FAIL case5a: second gate did not set human label"; exit 1; }
ateam clear-gate "$match_id" >/dev/null
human_out=$(ateam human-list)
echo "$human_out" | grep -q "$match_id" \
  && { echo "FAIL case5b: clear-gate without --file did not remove human label"; exit 1; }

# ── Case 6: learn then learnings roundtrip ────────────────────────────────────
printf 'test insight. WHY: testing. HOW TO APPLY: use it.' > "$T/insight.txt"
ateam learn planner round-trip-slug --file "$T/insight.txt" >/dev/null
learnings_out=$(ateam learnings planner)
echo "$learnings_out" | grep -q "round-trip-slug" \
  || { echo "FAIL case6: learnings did not return round-trip-slug"; exit 1; }
echo "$learnings_out" | grep -q "test insight" \
  || { echo "FAIL case6: learnings did not return insight content"; exit 1; }

# ── Case 7: note appends to issue ─────────────────────────────────────────────
# Capture to variable before grepping: piping ateam show directly to grep -q
# triggers SIGPIPE (ateam show exits 141) which pipefail converts to a pipeline
# failure — even when grep -q finds the match and exits 0.
printf 'Some extra note.\n' > "$T/note.txt"
ateam note "$match_id" --file "$T/note.txt" >/dev/null
show_after_note=$(ateam show "$match_id")
echo "$show_after_note" | grep -q "Some extra note" \
  || { echo "FAIL case7: note not visible in show output"; exit 1; }

# ── Case 8: show returns issue content ────────────────────────────────────────
show_out=$(ateam show "$match_id")
echo "$show_out" | grep -q "Match Test" \
  || { echo "FAIL case8: show did not contain issue title"; exit 1; }

# ── Case 9: close ─────────────────────────────────────────────────────────────
ateam close "$match_id" --reason "test done" >/dev/null
# Should no longer appear in list-json
remaining=$(ateam list-json | jq -r --arg id "$match_id" '.[] | select(.id == $id) | .id')
[ -z "$remaining" ] || { echo "FAIL case9: closed issue '$match_id' still appears in list-json"; exit 1; }

# ── Case 10: sync — set up local bare remote then push ────────────────────────
bare="$T/remote.git"
git init --bare -q "$bare"
git -C "$AGENT_TEAMS_HOME" remote add origin "$bare"
git -C "$AGENT_TEAMS_HOME" add -A
git -C "$AGENT_TEAMS_HOME" commit -q -m "initial commit"
git -C "$AGENT_TEAMS_HOME" push -q origin main
bd -C "$AGENT_TEAMS_HOME" dolt remote add origin "$bare"
sync_ec=0; sync_out=$(ateam sync 2>&1) || sync_ec=$?
[ "$sync_ec" -eq 0 ] \
  || { echo "FAIL case10: sync exited $sync_ec (output: '$sync_out')"; exit 1; }
echo "$sync_out" | grep -qi "push complete" \
  || { echo "FAIL case10: sync output did not contain 'push complete' (got: '$sync_out')"; exit 1; }

# ── Case 11: bare close (no --reason/--file) ─────────────────────────────────
printf 'problem: bare-close-test\nrepo: %s\nworktree: %s/wt-bc\nbranch: feat/bc\nteam: alpha\nmode: interactive\n' \
  "$T" "$T" > "$T/bare-close-body.md"
bc_id=$(ateam register --title "Bare Close Test" --file "$T/bare-close-body.md")
[ -n "$bc_id" ] || { echo "FAIL case11a: register for bare-close returned empty id"; exit 1; }
ateam close "$bc_id"
remaining_bc=$(ateam list-json | jq -r --arg id "$bc_id" '.[] | select(.id == $id) | .id')
[ -z "$remaining_bc" ] || { echo "FAIL case11a: bare-closed issue '$bc_id' still in list-json"; exit 1; }

# ── Case 11b: exit-4 guard (uninitialized workspace → read verb exits 4) ─────
mkdir -p "$T/nope"
ec=0; AGENT_TEAMS_HOME="$T/nope" ateam list 2>/dev/null || ec=$?
[ "$ec" -eq 4 ] || { echo "FAIL case11b: uninitialized workspace exit code $ec, want 4"; exit 1; }

# ── Case 12: unknown verb → exit 2 ────────────────────────────────────────────
ec=0; ateam bogus-verb 2>/dev/null || ec=$?
[ "$ec" -eq 2 ] || { echo "FAIL case12: unknown verb exit code $ec, want 2"; exit 1; }

# ── Case 13: ws prints path even when workspace is uninitialized ──────────────
uninit_out=$(AGENT_TEAMS_HOME="$T/nope" ateam ws)
[ "$uninit_out" = "$T/nope" ] || { echo "FAIL case13: ws with uninit ws printed '$uninit_out'"; exit 1; }

# ── Case 14: dispatch happy path ─────────────────────────────────────────────
dispatch_repo="$T/dispatch-repo"
mkdir -p "$dispatch_repo"
git -C "$dispatch_repo" init -q
git -C "$dispatch_repo" commit -q --allow-empty -m "initial"
git -C "$dispatch_repo" checkout -q -b main 2>/dev/null || true

dispatch_out=$(ateam dispatch --problem "add an undo stack" --repo "$dispatch_repo" --no-launch 2>&1)
echo "$dispatch_out" | grep -q "initiative_id: at-" \
  || { echo "FAIL case14: dispatch did not print 'initiative_id: at-...' (got: '$dispatch_out')"; exit 1; }
echo "$dispatch_out" | grep -q "worktree:" \
  || { echo "FAIL case14: dispatch did not print 'worktree:' line"; exit 1; }
echo "$dispatch_out" | grep -q "slug: add-an-undo-stack" \
  || { echo "FAIL case14: dispatch slug line wrong (got: '$dispatch_out')"; exit 1; }
echo "$dispatch_out" | grep -q "base_branch:" \
  || { echo "FAIL case14: dispatch did not print 'base_branch:' line"; exit 1; }

dispatch_id=$(echo "$dispatch_out" | grep "^initiative_id: " | sed 's/^initiative_id: //')
dispatch_wt=$(echo "$dispatch_out" | grep "^worktree: " | sed 's/^worktree: //')

[ -d "$dispatch_wt" ] \
  || { echo "FAIL case14: worktree dir '$dispatch_wt' was not created"; exit 1; }

ateam list-json | jq -e --arg id "$dispatch_id" '.[] | select(.id == $id)' >/dev/null \
  || { echo "FAIL case14: dispatch id '$dispatch_id' not found in list-json"; exit 1; }

found14=$(ateam resume-match "$dispatch_wt")
[ "$found14" = "$dispatch_id" ] \
  || { echo "FAIL case14: resume-match returned '$found14', want '$dispatch_id'"; exit 1; }

git -C "$dispatch_repo" worktree remove --force "$dispatch_wt"

# ── Case 15: dispatch fail-fast — not a git repo ─────────────────────────────
ec15=0; ateam dispatch --problem "x" --repo "$T/not-a-repo" --no-launch 2>/dev/null || ec15=$?
[ "$ec15" -ne 0 ] \
  || { echo "FAIL case15: dispatch against non-repo exited 0, want non-zero"; exit 1; }

# ── Case 16: dispatch fail-fast — collision (same slug twice) ─────────────────
ec16=0; ateam dispatch --problem "add an undo stack" --repo "$dispatch_repo" --no-launch 2>/dev/null || ec16=$?
[ "$ec16" -ne 0 ] \
  || { echo "FAIL case16: second dispatch with same slug exited 0, want non-zero (collision)"; exit 1; }

# ── Case 17: dispatch --id-only ───────────────────────────────────────────────
dispatch_repo2="$T/dispatch-repo2"
mkdir -p "$dispatch_repo2"
git -C "$dispatch_repo2" init -q
git -C "$dispatch_repo2" commit -q --allow-empty -m "initial"
git -C "$dispatch_repo2" checkout -q -b main 2>/dev/null || true

id_only_out=$(ateam dispatch --problem "add a redo stack" --repo "$dispatch_repo2" --no-launch --id-only 2>&1)
line_count=$(echo "$id_only_out" | wc -l | tr -d ' ')
[ "$line_count" -eq 1 ] \
  || { echo "FAIL case17: --id-only printed $line_count lines, want 1 (got: '$id_only_out')"; exit 1; }
echo "$id_only_out" | grep -qE '^at-' \
  || { echo "FAIL case17: --id-only output '$id_only_out' is not an at-<hash> id"; exit 1; }

dispatch_wt2="$AGENT_TEAMS_HOME-worktrees/add-a-redo-stack"
[ -d "$dispatch_wt2" ] && git -C "$dispatch_repo2" worktree remove --force "$dispatch_wt2" || true

# ── Case 18: wrapper unsupported-platform error path ─────────────────────────
# Point the wrapper at a temp bin/ that only has a binary for a fake platform
# so the host platform's target is missing.
mkdir -p "$T/unsup-bin"
cp "$ROOT/plugins/agent-teams/bin/ateam" "$T/unsup-bin/ateam"
# Create a dummy binary for a non-existent platform so the "available binaries"
# listing is non-empty but the host's target is absent.
touch "$T/unsup-bin/ateam-fakeos-fakearch"
chmod +x "$T/unsup-bin/ateam-fakeos-fakearch"
unsup_ec=0
unsup_out=$("$T/unsup-bin/ateam" ws 2>&1) || unsup_ec=$?
[ "$unsup_ec" -ne 0 ] \
  || { echo "FAIL case18: wrapper with missing host target exited 0, want non-zero"; exit 1; }
echo "$unsup_out" | grep -qi "unsupported platform" \
  || { echo "FAIL case18: wrapper error missing 'unsupported platform' (got: '$unsup_out')"; exit 1; }
echo "$unsup_out" | grep -qi "fakeos-fakearch" \
  || { echo "FAIL case18: wrapper error did not list available binaries (got: '$unsup_out')"; exit 1; }

echo "PASS"
