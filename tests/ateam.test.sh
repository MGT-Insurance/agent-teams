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

# Several cases below register initiatives with "repo: $T" (a plain scratch
# dir, not a git repo). resume's repoconfig.Enabled(f.Repo) check runs before
# the worktree-exists check, so without a marker here those repo-not-enabled
# refusals (exit 6) would mask the worktree-related exit-1 cases they intend
# to test. Mark $T enabled once, up front, for all of them.
touch "$T/.agent-teams"

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
# Opt this scratch repo in — since e8bf328, dispatch refuses an un-opted-in
# repo (exit 6), which this "happy path" case is not testing.
touch "$dispatch_repo/.agent-teams"

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
touch "$dispatch_repo2/.agent-teams"

# --id-only's contract is exactly one line on STDOUT (see dispatch.go's
# `fmt.Fprintln(ctx.Stdout, issue.ID)`); a fail-soft "could not create root
# epic" diagnostic (expected here — this scratch repo has no .beads) goes to
# STDERR on a separate stream, so it must not be captured alongside stdout or
# it breaks the line-count assertion below.
id_only_out=$(ateam dispatch --problem "add a redo stack" --repo "$dispatch_repo2" --no-launch --id-only 2>/dev/null)
line_count=$(echo "$id_only_out" | wc -l | tr -d ' ')
[ "$line_count" -eq 1 ] \
  || { echo "FAIL case17: --id-only printed $line_count lines, want 1 (got: '$id_only_out')"; exit 1; }
echo "$id_only_out" | grep -qE '^at-' \
  || { echo "FAIL case17: --id-only output '$id_only_out' is not an at-<hash> id"; exit 1; }

dispatch_wt2="$AGENT_TEAMS_HOME-worktrees/add-a-redo-stack"
[ -d "$dispatch_wt2" ] && git -C "$dispatch_repo2" worktree remove --force "$dispatch_wt2" || true

# ── Case 19: resume guardrails ───────────────────────────────────────────────
# 19a: missing arg → exit 2
ec19a=0; ateam resume 2>/dev/null || ec19a=$?
[ "$ec19a" -eq 2 ] \
  || { echo "FAIL case19a: resume with no arg exited $ec19a, want 2"; exit 1; }

# 19b: unknown id → exit 1
ec19b=0; ateam resume at-nosuchid-99999 2>/dev/null || ec19b=$?
[ "$ec19b" -eq 1 ] \
  || { echo "FAIL case19b: resume with unknown id exited $ec19b, want 1"; exit 1; }

# 19c: closed initiative → exit 1
# Register an initiative and immediately close it, then confirm resume refuses.
printf 'problem: closed-resume-test\nrepo: %s\nworktree: %s/wt-cr\nbranch: feat/cr\nteam: alpha\nmode: interactive\n' \
  "$T" "$T" > "$T/closed-resume-body.md"
cr_id=$(ateam register --title "Closed Resume Test" --file "$T/closed-resume-body.md")
ateam close "$cr_id" >/dev/null
ec19c=0; ateam resume "$cr_id" 2>/dev/null || ec19c=$?
[ "$ec19c" -eq 1 ] \
  || { echo "FAIL case19c: resume of closed initiative exited $ec19c, want 1"; exit 1; }

# 19d: open initiative with a worktree path that does not exist → exit 1
# Register an initiative with a worktree path that is never created.
printf 'problem: missing-wt-test\nrepo: %s\nworktree: /no/such/path/ever\nbranch: feat/mwt\nteam: alpha\nmode: interactive\n' \
  "$T" > "$T/missing-wt-body.md"
mwt_id=$(ateam register --title "Missing WT Test" --file "$T/missing-wt-body.md")
ec19d=0; ateam resume "$mwt_id" 2>/dev/null || ec19d=$?
[ "$ec19d" -eq 1 ] \
  || { echo "FAIL case19d: resume with missing worktree exited $ec19d, want 1"; exit 1; }

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

# ── Case 20: enable-repo creates the marker on a fresh repo with no marker ───
enable_repo1="$T/enable-repo-fresh"
mkdir -p "$enable_repo1"
git -C "$enable_repo1" init -q
git -C "$enable_repo1" commit -q --allow-empty -m "initial"
enable_repo1_root=$(git -C "$enable_repo1" rev-parse --show-toplevel)

ec20=0; enable_out1=$(ateam enable-repo "$enable_repo1") || ec20=$?
[ "$ec20" -eq 0 ] \
  || { echo "FAIL case20: enable-repo exited $ec20, want 0 (output: '$enable_out1')"; exit 1; }
[ "$enable_out1" = "enabled: $enable_repo1_root/.agent-teams (created)" ] \
  || { echo "FAIL case20: enable-repo stdout '$enable_out1', want 'enabled: $enable_repo1_root/.agent-teams (created)'"; exit 1; }
[ -f "$enable_repo1_root/.agent-teams" ] \
  || { echo "FAIL case20: marker file was not created at repo root"; exit 1; }
grep -q "agent-teams opt-in marker" "$enable_repo1_root/.agent-teams" \
  || { echo "FAIL case20: created marker missing the canonical comment header"; exit 1; }

# ── Case 21: enable-repo resolves a subdirectory to the repo root ───────────
enable_repo2="$T/enable-repo-subdir"
mkdir -p "$enable_repo2/nested/deep"
git -C "$enable_repo2" init -q
git -C "$enable_repo2" commit -q --allow-empty -m "initial"
enable_repo2_root=$(git -C "$enable_repo2" rev-parse --show-toplevel)

ec21=0; enable_out2=$(ateam enable-repo "$enable_repo2/nested/deep") || ec21=$?
[ "$ec21" -eq 0 ] \
  || { echo "FAIL case21: enable-repo from a subdirectory exited $ec21, want 0 (output: '$enable_out2')"; exit 1; }
[ "$enable_out2" = "enabled: $enable_repo2_root/.agent-teams (created)" ] \
  || { echo "FAIL case21: enable-repo from a subdirectory printed '$enable_out2', want the repo-root path"; exit 1; }
[ -f "$enable_repo2_root/.agent-teams" ] \
  || { echo "FAIL case21: marker was not created at the resolved repo root"; exit 1; }
[ ! -f "$enable_repo2_root/nested/deep/.agent-teams" ] \
  || { echo "FAIL case21: marker was incorrectly created in the subdirectory instead of the repo root"; exit 1; }

# ── Case 22: enable-repo removes "disabled: true", preserving other lines ───
enable_repo3="$T/enable-repo-disabled"
mkdir -p "$enable_repo3"
git -C "$enable_repo3" init -q
git -C "$enable_repo3" commit -q --allow-empty -m "initial"
enable_repo3_root=$(git -C "$enable_repo3" rev-parse --show-toplevel)
printf 'disabled: true\nsome custom note\n' > "$enable_repo3_root/.agent-teams"

ec22=0; enable_out3=$(ateam enable-repo "$enable_repo3_root") || ec22=$?
[ "$ec22" -eq 0 ] \
  || { echo "FAIL case22: enable-repo exited $ec22, want 0 (output: '$enable_out3')"; exit 1; }
[ "$enable_out3" = "enabled: ${enable_repo3_root}/.agent-teams (removed \"disabled: true\")" ] \
  || { echo "FAIL case22: enable-repo stdout '$enable_out3', want the removed-disabled message"; exit 1; }
grep -q '^disabled: true$' "$enable_repo3_root/.agent-teams" \
  && { echo "FAIL case22: 'disabled: true' line still present after enable-repo"; exit 1; }
grep -q "some custom note" "$enable_repo3_root/.agent-teams" \
  || { echo "FAIL case22: unrelated line 'some custom note' was not preserved"; exit 1; }

# ── Case 23: enable-repo on an already-enabled repo does not write ──────────
before23=$(cat "$enable_repo1_root/.agent-teams")
ec23=0; enable_out4=$(ateam enable-repo "$enable_repo1_root") || ec23=$?
[ "$ec23" -eq 0 ] \
  || { echo "FAIL case23: enable-repo exited $ec23, want 0 (output: '$enable_out4')"; exit 1; }
[ "$enable_out4" = "enabled: $enable_repo1_root/.agent-teams (already enabled)" ] \
  || { echo "FAIL case23: enable-repo stdout '$enable_out4', want the already-enabled message"; exit 1; }
after23=$(cat "$enable_repo1_root/.agent-teams")
[ "$before23" = "$after23" ] \
  || { echo "FAIL case23: marker content changed despite already being enabled"; exit 1; }

# ── Case 24: enable-repo on a non-git directory → exit 1 ─────────────────────
not_a_repo="$T/enable-repo-not-a-repo"
mkdir -p "$not_a_repo"
ec24=0; ateam enable-repo "$not_a_repo" 2>/dev/null || ec24=$?
[ "$ec24" -eq 1 ] \
  || { echo "FAIL case24: enable-repo on a non-git directory exited $ec24, want 1"; exit 1; }

# ── Case 25: dispatch exits 6 before enable-repo, succeeds after ────────────
enable_dispatch_repo="$T/enable-dispatch-repo"
mkdir -p "$enable_dispatch_repo"
git -C "$enable_dispatch_repo" init -q
git -C "$enable_dispatch_repo" commit -q --allow-empty -m "initial"
git -C "$enable_dispatch_repo" checkout -q -b main 2>/dev/null || true

ec25a=0; out25a=$(ateam dispatch --problem "enable then dispatch" --repo "$enable_dispatch_repo" --no-launch 2>&1) || ec25a=$?
[ "$ec25a" -eq 6 ] \
  || { echo "FAIL case25a: dispatch into a not-opted-in repo exited $ec25a, want 6 (output: '$out25a')"; exit 1; }
echo "$out25a" | grep -q "agent-teams is not enabled for" \
  || { echo "FAIL case25a: refusal message missing required substring (got: '$out25a')"; exit 1; }

ateam enable-repo "$enable_dispatch_repo" >/dev/null

ec25b=0; out25b=$(ateam dispatch --problem "enable then dispatch" --repo "$enable_dispatch_repo" --no-launch 2>&1) || ec25b=$?
[ "$ec25b" -eq 0 ] \
  || { echo "FAIL case25b: dispatch after enable-repo exited $ec25b, want 0 (output: '$out25b')"; exit 1; }
echo "$out25b" | grep -q "initiative_id: at-" \
  || { echo "FAIL case25b: dispatch after enable-repo missing 'initiative_id: at-...' (got: '$out25b')"; exit 1; }

dispatch_wt25=$(echo "$out25b" | grep "^worktree: " | sed 's/^worktree: //')
[ -d "$dispatch_wt25" ] && git -C "$enable_dispatch_repo" worktree remove --force "$dispatch_wt25"

# ── Case 26: resume exits 6 for an initiative whose repo is not opted in ────
resume_norepo="$T/resume-not-enabled-repo"
mkdir -p "$resume_norepo"
git -C "$resume_norepo" init -q
git -C "$resume_norepo" commit -q --allow-empty -m "initial"
resume_norepo_root=$(git -C "$resume_norepo" rev-parse --show-toplevel)

printf 'problem: resume-exit6-test\nrepo: %s\nworktree: %s/resume-wt\nbranch: feat/r6\nteam: alpha\nmode: interactive\n' \
  "$resume_norepo_root" "$T" > "$T/resume-exit6-body.md"
resume6_id=$(ateam register --title "Resume Exit6 Test" --file "$T/resume-exit6-body.md")
[ -n "$resume6_id" ] || { echo "FAIL case26: register for resume-exit6 returned empty id"; exit 1; }

ec26=0; out26=$(ateam resume "$resume6_id" 2>&1) || ec26=$?
[ "$ec26" -eq 6 ] \
  || { echo "FAIL case26: resume into a not-opted-in repo exited $ec26, want 6 (output: '$out26')"; exit 1; }
echo "$out26" | grep -q "agent-teams is not enabled for" \
  || { echo "FAIL case26: resume refusal message missing required substring (got: '$out26')"; exit 1; }

# ── Case 27: exit-code taxonomy guard — dispatch on a non-git dir stays 1 ───
# Mirrors cases 11b/12 (exit 4, exit 2): assert the number, not the prose.
# Must stay 1, not drift to 6 (opt-in) or 5 (condense-lock, a different verb).
notgit_dispatch="$T/notgit-dispatch-dir"
mkdir -p "$notgit_dispatch"
ec27=0; ateam dispatch --problem "x" --repo "$notgit_dispatch" --no-launch 2>/dev/null || ec27=$?
[ "$ec27" -eq 1 ] \
  || { echo "FAIL case27: dispatch on a non-git directory exited $ec27, want 1"; exit 1; }

# ── Case 28: condense-lock regression guard — held lock still exits 5 ───────
# Exit 5 belongs to condense-lock (internal/verbs/lock.go) and /condense
# branches on it; this initiative's exit 6 must never collide with it.
ateam condense-lock acquire >/dev/null
ec28=0; ateam condense-lock acquire 2>/dev/null || ec28=$?
[ "$ec28" -eq 5 ] \
  || { echo "FAIL case28: condense-lock acquire against a held lock exited $ec28, want 5"; exit 1; }
ateam condense-lock release >/dev/null

echo "PASS"
