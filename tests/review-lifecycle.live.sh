#!/usr/bin/env bash
# review-lifecycle.live.sh — LOOP L5 live lifecycle harness (agent-teams-8st0.23).
#
# Drives a real ateam binary (ATEAM_BIN) and the real bd/git binaries end to
# end, against a throwaway scratch AGENT_TEAMS_HOME and a throwaway git
# fixture repo, to witness the review-mail delivery contract frozen by
# agent-teams-8st0.18 across its full lifecycle: dispatch, reopen/resume,
# reap, the hung-tick backstop, and mail send to a closed recipient.
#
# WHY: unit tests fake every seam (bd, gh, claude, the transport). The
# 9,082-message failure this loop exists to close was only visible end to
# end. This harness is the repeatable, real-process witness.
#
# Modeled on tests/e2e-loop.test.sh and the loop-live-proof / reap-live-proof
# harnesses (see agent-teams-8st0.23 for pointers). Manual only — never run
# from CI, and never touches ~/.agent-teams, a real repo, a real GitHub PR,
# or a real `claude`.
#
# Usage:
#   ATEAM_BIN=/path/to/ateam bash tests/review-lifecycle.live.sh
#   bash tests/review-lifecycle.live.sh /path/to/ateam
#
# Scenarios: S1-S8 (S4-moot is a variant of S4) plus K1 (a static text check,
# not a live run). Every live scenario ends by asserting there are 0 open
# type=message wisps left in the scratch home. Prints a PASS/FAIL table and
# exits nonzero if any scenario failed.
set -uo pipefail
# Deliberately NOT `set -e`: several scenarios below assert a NONZERO exit
# code from `ateam` (that is the point of testing a real binary end to end),
# so the harness must inspect exit codes itself rather than abort on them.

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# ── resolve ATEAM_BIN ────────────────────────────────────────────────────────

ATEAM_BIN="${ATEAM_BIN:-${1:-}}"
if [ -z "$ATEAM_BIN" ]; then
  echo "usage: ATEAM_BIN=/path/to/ateam $0   (or: $0 /path/to/ateam)" >&2
  exit 2
fi
case "$ATEAM_BIN" in
  /*) : ;;
  *) ATEAM_BIN="$(cd "$(dirname "$ATEAM_BIN")" && pwd)/$(basename "$ATEAM_BIN")" ;;
esac
[ -x "$ATEAM_BIN" ] || { echo "review-lifecycle: ATEAM_BIN not executable: $ATEAM_BIN" >&2; exit 2; }

REAL_BD="$(command -v bd || true)"
[ -n "$REAL_BD" ] || { echo "review-lifecycle: bd not found on PATH" >&2; exit 2; }
REAL_GIT="$(command -v git || true)"
[ -n "$REAL_GIT" ] || { echo "review-lifecycle: git not found on PATH" >&2; exit 2; }

# ── scratch workspace ────────────────────────────────────────────────────────

# Scratch under $CLAUDE_JOB_DIR/tmp when set (this machine shares /tmp across
# concurrent agent-teams jobs; a bare /tmp mktemp risks another job's cleanup
# racing this one). Falls back to TMPDIR/tmp for a plain interactive shell.
BASE_TMP="${CLAUDE_JOB_DIR:+$CLAUDE_JOB_DIR/tmp}"
BASE_TMP="${BASE_TMP:-${TMPDIR:-/tmp}}"
T="$(mktemp -d "$BASE_TMP/review-lifecycle.XXXXXX")"
cleanup() {
  # Kill anything the harness itself backgrounded (S5's relay), by explicit
  # PID only — never by process-name pattern.
  if [ -n "${RELAY_PID:-}" ] && kill -0 "$RELAY_PID" 2>/dev/null; then
    kill "$RELAY_PID" 2>/dev/null || true
    wait "$RELAY_PID" 2>/dev/null || true
  fi
  rm -rf "$T"
}
trap cleanup EXIT

echo "review-lifecycle: scratch dir $T"
echo "review-lifecycle: ATEAM_BIN=$ATEAM_BIN"

# ── PASS/FAIL bookkeeping ────────────────────────────────────────────────────

RESULTS_FILE="$T/results.tsv"
: > "$RESULTS_FILE"
OVERALL_RC=0

record() { # $1=name $2=PASS|FAIL $3=reason
  printf '%s\t%s\t%s\n' "$1" "$2" "$3" >> "$RESULTS_FILE"
  if [ "$2" = "PASS" ]; then
    echo "PASS  $1"
  else
    echo "FAIL  $1 -- $3"
    OVERALL_RC=1
  fi
}

# ── scratch AGENT_TEAMS_HOME ─────────────────────────────────────────────────

export AGENT_TEAMS_HOME="$T/home"
mkdir -p "$AGENT_TEAMS_HOME"
"$REAL_GIT" -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && "$REAL_BD" init --prefix lt --non-interactive >/dev/null)

# Hard safety: never operate against the real global workspace.
REAL_AGENT_TEAMS_HOME="$(cd ~/.agent-teams 2>/dev/null && pwd || true)"
if [ -n "$REAL_AGENT_TEAMS_HOME" ] && [ "$AGENT_TEAMS_HOME" = "$REAL_AGENT_TEAMS_HOME" ]; then
  echo "review-lifecycle: REFUSING — scratch AGENT_TEAMS_HOME resolved to the real ~/.agent-teams" >&2
  exit 2
fi

# ── stub bin dir (claude, gh) + ateam symlink ────────────────────────────────

BIN_DIR="$T/bin"
mkdir -p "$BIN_DIR"
ln -s "$ATEAM_BIN" "$BIN_DIR/ateam"

STATE_DIR="$T/state"
mkdir -p "$STATE_DIR"

export CLAUDE_STUB_LOG="$STATE_DIR/claude-argv.log"
: > "$CLAUDE_STUB_LOG"
export CLAUDE_STUB_SESSIONS="$STATE_DIR/sessions.json"
echo '[]' > "$CLAUDE_STUB_SESSIONS"

export GH_STUB_LOG="$STATE_DIR/gh-argv.log"
: > "$GH_STUB_LOG"
export GH_STUB_STATE_FILE="$STATE_DIR/gh-state.tsv"
: > "$GH_STUB_STATE_FILE"
export GH_STUB_FAILCOUNT_FILE="$STATE_DIR/gh-failcount.tsv"
: > "$GH_STUB_FAILCOUNT_FILE"

# claude stub: logs argv; `agents [--all] --json` prints the controlled
# session list; `--bg ...` runs the scripted review session SYNCHRONOUSLY in
# its own cwd, mirroring the real rawLaunchBGSession (cmd.Dir=<worktree>,
# cmd.Run() blocks): it resolves which initiative owns this cwd, reads mail,
# then closes that initiative. This is the ONLY stub session behavior the
# harness has — every scenario's "scripted session" is this same read-then-
# close sequence (K1 exists because of exactly this: the stub can't witness
# the review-pr skill's own wording).
cat > "$BIN_DIR/claude" <<'CLAUDE_STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CLAUDE_STUB_LOG"

if [ "${1:-}" = "agents" ]; then
  cat "$CLAUDE_STUB_SESSIONS"
  exit 0
fi

if [ "${1:-}" = "--bg" ]; then
  id="$(ateam resolve-initiative "$PWD" 2>/dev/null || true)"
  if [ -n "$id" ]; then
    ateam mail inbox >"$CLAUDE_STUB_LOG.session-$id.inbox" 2>&1
    ateam close "$id" --reason "scripted review session done" >"$CLAUDE_STUB_LOG.session-$id.close" 2>&1
  else
    echo "claude-stub: --bg launch but no initiative resolved for cwd $PWD" >> "$CLAUDE_STUB_LOG"
  fi
  exit 0
fi
exit 0
CLAUDE_STUB
chmod +x "$BIN_DIR/claude"

# gh stub: `pr view <n> -R <ownerRepo> --json state` answers from
# GH_STUB_STATE_FILE (default OPEN), decrementing a per-key fail-countdown in
# GH_STUB_FAILCOUNT_FILE first (S6's "gh exits 1 N times, then heals"). `auth
# status` always succeeds. `api .../comments` reports no pending comments.
# `api user` reports a stub login. Anything else is a silent no-op success —
# logged for post-hoc inspection, never treated as a scenario input.
cat > "$BIN_DIR/gh" <<'GH_STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$GH_STUB_LOG"

if [ "${1:-}" = "auth" ] && [ "${2:-}" = "status" ]; then
  exit 0
fi

if [ "${1:-}" = "pr" ] && [ "${2:-}" = "view" ]; then
  n="${3:-}"
  repo=""
  shift 3 2>/dev/null || shift $#
  while [ $# -gt 0 ]; do
    case "$1" in
      -R) repo="$2"; shift 2 ;;
      *) shift ;;
    esac
  done
  key="${repo}#${n}"
  if [ -s "$GH_STUB_FAILCOUNT_FILE" ]; then
    remaining="$(awk -F'\t' -v k="$key" '$1==k{print $2}' "$GH_STUB_FAILCOUNT_FILE")"
    if [ -n "$remaining" ] && [ "$remaining" -gt 0 ]; then
      tmp="$(mktemp "${GH_STUB_FAILCOUNT_FILE}.XXXXXX")"
      awk -F'\t' -v k="$key" -v r="$((remaining - 1))" 'BEGIN{OFS="\t"} $1==k{$2=r} {print}' \
        "$GH_STUB_FAILCOUNT_FILE" > "$tmp"
      mv "$tmp" "$GH_STUB_FAILCOUNT_FILE"
      exit 1
    fi
  fi
  state="$(awk -F'\t' -v k="$key" '$1==k{print $2}' "$GH_STUB_STATE_FILE")"
  state="${state:-OPEN}"
  printf '{"state":"%s"}\n' "$state"
  exit 0
fi

if [ "${1:-}" = "api" ]; then
  case "${2:-}" in
    *"/comments") printf '[]\n'; exit 0 ;;
    user) printf '{"login":"stub-bot"}\n'; exit 0 ;;
    *) printf '{}\n'; exit 0 ;;
  esac
fi

exit 0
GH_STUB
chmod +x "$BIN_DIR/gh"

export PATH="$BIN_DIR:$PATH"

# ── safety check: ateam ws must print the scratch home ──────────────────────

WS_REPORT="$(ateam ws 2>&1)"
if [ "$WS_REPORT" != "$AGENT_TEAMS_HOME" ]; then
  echo "review-lifecycle: REFUSING — 'ateam ws' printed '$WS_REPORT', expected scratch home '$AGENT_TEAMS_HOME'" >&2
  exit 2
fi

# CLAUDE_PLUGIN_ROOT: dispatch's launch path (buildAgentsPayload) reads the
# REAL plugins/agent-teams/roles/*.md — a workaround for Claude Code bug
# #78234/#81746 (see plugins/agent-teams/roles/README.md). This is read-only.
export CLAUDE_PLUGIN_ROOT="$ROOT/plugins/agent-teams"

# ── bd shim for S8 (reopen-failure injection) ────────────────────────────────
#
# Not on PATH by default. S8 prepends BD_SHIM_DIR for exactly one command: a
# PATH shim for `bd` that fails `bd reopen <id>` once (while REOPEN_FAIL_FLAG
# exists), then deletes its own flag and delegates to the real bd for every
# other call and every subsequent invocation.

BD_SHIM_DIR="$T/bd-shim"
mkdir -p "$BD_SHIM_DIR"
REOPEN_FAIL_FLAG="$STATE_DIR/reopen-fail-flag"
cat > "$BD_SHIM_DIR/bd" <<BDSHIM
#!/usr/bin/env bash
# ateam's bd.Client always prepends "-C <home>", so "reopen" lands at
# argv[3], not argv[1] -- scan all args instead of checking \$1 alone.
is_reopen=false
for a in "\$@"; do
  if [ "\$a" = "reopen" ]; then
    is_reopen=true
    break
  fi
done
if [ "\$is_reopen" = true ] && [ -f "$REOPEN_FAIL_FLAG" ]; then
  rm -f "$REOPEN_FAIL_FLAG"
  echo "bd-shim: simulated reopen failure for \$*" >&2
  exit 1
fi
exec "$REAL_BD" "\$@"
BDSHIM
chmod +x "$BD_SHIM_DIR/bd"

# ── fixture git repo (bare "origin" + working clone) ─────────────────────────
#
# One shared fixture repo for the whole run. Each scenario's PR gets its own
# branch + commit, pushed to the bare remote as both refs/heads/pr-<n> and
# refs/pull/<n>/head (the real GitHub PR-head ref shape) — a commit that
# differs from the default branch, so a worktree recreated at a PR's head is
# distinguishable from one left on the default branch.

OWNER_REPO="acme/widgets"
REPO_KEY="widgets" # gitutil.Slugify(filepath.Base("acme/widgets"))

FIXTURE_BARE="$T/fixture-origin.git"
FIXTURE_CLONE="$T/fixture-clone"

"$REAL_GIT" init -q --bare "$FIXTURE_BARE"
"$REAL_GIT" init -q "$FIXTURE_CLONE"
(cd "$FIXTURE_CLONE" && "$REAL_GIT" checkout -q -b main)
: > "$FIXTURE_CLONE/.agent-teams" # present, no "disabled: true" line => enabled
echo "widgets fixture repo" > "$FIXTURE_CLONE/README.md"
(cd "$FIXTURE_CLONE" && "$REAL_GIT" add -A && "$REAL_GIT" -c user.email=t@t -c user.name=t commit -q -m init)
(cd "$FIXTURE_CLONE" && "$REAL_GIT" remote add origin "$FIXTURE_BARE")
(cd "$FIXTURE_CLONE" && "$REAL_GIT" push -q origin main)
(cd "$FIXTURE_CLONE" && "$REAL_GIT" remote set-head origin main)
"$REAL_GIT" -C "$FIXTURE_BARE" symbolic-ref HEAD refs/heads/main

mkdir -p "$AGENT_TEAMS_HOME/review-repos"
printf '%s' "$FIXTURE_CLONE" > "$AGENT_TEAMS_HOME/review-repos/$REPO_KEY"

# make_pr_ref <n> <content> — commits <content> on a throwaway branch off
# main and pushes it to the bare origin as refs/heads/pr-<n> AND
# refs/pull/<n>/head (never touching the clone's own checked-out branch
# afterward).
make_pr_ref() {
  local n="$1" content="$2"
  ( cd "$FIXTURE_CLONE" || exit 1
    "$REAL_GIT" checkout -q -b "pr-branch-$n" main
    printf '%s\n' "$content" > "pr-$n.txt"
    "$REAL_GIT" add -A
    "$REAL_GIT" -c user.email=t@t -c user.name=t commit -q -m "pr $n content"
    "$REAL_GIT" push -q origin "HEAD:refs/heads/pr-$n" "HEAD:refs/pull/$n/head"
    "$REAL_GIT" checkout -q main
    "$REAL_GIT" branch -q -D "pr-branch-$n" )
}

pr_head_sha() { # <n> -> the commit sha refs/pull/<n>/head points at, in the bare origin
  "$REAL_GIT" -C "$FIXTURE_BARE" rev-parse "refs/pull/$1/head" 2>/dev/null
}

# ── small helpers ────────────────────────────────────────────────────────────

gh_set_state() { # <n> <STATE>
  printf '%s#%s\t%s\n' "$OWNER_REPO" "$1" "$2" >> "$GH_STUB_STATE_FILE"
}
gh_set_failcount() { # <n> <count>
  printf '%s#%s\t%s\n' "$OWNER_REPO" "$1" "$2" >> "$GH_STUB_FAILCOUNT_FILE"
}

list_initiative_ids() { # non-message initiative beads only (mail.go's own
  # list call passes --include-infra --type=message specifically because
  # plain `bd list` does NOT include message-type infra beads by default).
  bd -C "$AGENT_TEAMS_HOME" list --status=all --json 2>/dev/null | jq -r '.[].id' | sort
}
new_initiative_since() { # <before-list (one id per line)> -> new ids since then
  comm -13 <(printf '%s\n' "$1") <(list_initiative_ids)
}

issue_json() { bd -C "$AGENT_TEAMS_HOME" show "$1" --json 2>/dev/null; }
issue_status() { issue_json "$1" | jq -r '.[0].status // ""'; }

mail_open_count_all() {
  ateam mail list --json --limit=2000 2>/dev/null | jq '[.[] | select(.closed==false)] | length'
}
mail_open_count_for() { # <recipient-id>
  local id="$1"
  ateam mail list --json --limit=2000 2>/dev/null | jq --arg id "$id" '[.[] | select(.closed==false and .to==$id)] | length'
}
assert_zero_open_mail() { # <scenario-label>
  local label="$1" n
  n="$(mail_open_count_all)"
  if [ "$n" = "0" ]; then
    return 0
  fi
  echo "  note: $label ends with $n open mail wisp(s) left in the scratch home" >&2
  return 1
}

set_sessions_live() { # <cwd> — one fake background session tied to <cwd>
  printf '[{"cwd":%s,"kind":"background","status":"idle","name":"fake","id":"fake","pid":999999,"sessionId":"fake-session"}]\n' \
    "$(printf '%s' "$1" | jq -R .)" > "$CLAUDE_STUB_SESSIONS"
}
set_sessions_empty() { echo '[]' > "$CLAUDE_STUB_SESSIONS"; }

# make_review_initiative <slug> <worktree> <n> <open|closed> — registers a
# review-shaped initiative (pr-number/pr-repo/pr-url lines, scanned by both
# initiative.ResolvedPRs' free-text fallback and matchClosedFromIssues'
# Notes-then-Description scan) at <worktree> (created as a plain directory —
# reviewInitiativeWorktreeLive/resume's liveness checks are existence-only).
make_review_initiative() {
  local slug="$1" wt="$2" n="$3" state="$4"
  mkdir -p "$wt"
  local body="$T/body-$slug.txt"
  cat > "$body" <<EOF
problem: review PR #$n ($OWNER_REPO)
repo: $FIXTURE_CLONE
worktree: $wt
branch: $slug
team: widgets-$slug
mode: bg
runtime: claude
pr-number: $n
pr-repo: $OWNER_REPO
pr-url: https://github.com/$OWNER_REPO/pull/$n
EOF
  local id
  id="$(ateam register --title "Review PR #$n ($OWNER_REPO)" --file "$body")"
  [ -n "$id" ] || { echo "FATAL: ateam register failed for $slug" >&2; exit 1; }
  if [ "$state" = "closed" ]; then
    bd -C "$AGENT_TEAMS_HOME" close "$id" >/dev/null 2>&1
  fi
  printf '%s' "$id"
}

# make_dri_initiative <slug> <worktree> <open|closed> — a plain (non-review)
# DRI initiative: no pr-* lines, so it is not review-shaped.
make_dri_initiative() {
  local slug="$1" wt="$2" state="$3"
  mkdir -p "$wt"
  local body="$T/body-$slug.txt"
  cat > "$body" <<EOF
problem: $slug
repo: $FIXTURE_CLONE
worktree: $wt
branch: $slug
team: widgets-$slug
mode: bg
runtime: claude
EOF
  local id
  id="$(ateam register --title "DRI: $slug" --file "$body")"
  [ -n "$id" ] || { echo "FATAL: ateam register failed for $slug" >&2; exit 1; }
  if [ "$state" = "closed" ]; then
    bd -C "$AGENT_TEAMS_HOME" close "$id" >/dev/null 2>&1
  fi
  printf '%s' "$id"
}

launch_count_since() { # <before-line-count of CLAUDE_STUB_LOG> -> number of --bg launches after
  local before="$1"
  tail -n "+$((before + 1))" "$CLAUDE_STUB_LOG" | grep -c -- '--bg' || true
}
log_line_count() { wc -l < "$CLAUDE_STUB_LOG" | tr -d ' '; }

# ══════════════════════════════════════════════════════════════════════════
# S1 — review_requested -> dispatch -> scripted session closes; initiative closed.
# ══════════════════════════════════════════════════════════════════════════
run_S1() {
  local n=101
  local branch="pr-branch-$n"
  make_pr_ref "$n" "s1 content"
  gh_set_state "$n" OPEN

  local bodyfile="$T/s1-body.txt"
  printf 'PR #%d opened for review.\n' "$n" > "$bodyfile"

  local before; before="$(list_initiative_ids)"
  local out rc
  out="$(ateam route-pr-event --repo "$OWNER_REPO" --pr-number "$n" --head-branch "$branch" \
        --transition review_requested --body-file "$bodyfile" \
        --pr-url "https://github.com/$OWNER_REPO/pull/$n" 2>&1)"
  rc=$?

  local id; id="$(new_initiative_since "$before")"
  if [ -z "$id" ] || [ "$(printf '%s\n' "$id" | wc -l | tr -d ' ')" != "1" ]; then
    record "S1" FAIL "expected exactly one new initiative for PR #$n; route-pr-event rc=$rc, new ids='$id', output: $out"
    return
  fi

  local status; status="$(issue_status "$id")"
  if [ "$status" != "closed" ]; then
    record "S1" FAIL "initiative $id status='$status', expected closed (route-pr-event rc=$rc)"
  elif ! assert_zero_open_mail "S1"; then
    record "S1" FAIL "initiative $id closed correctly, but mail wisps remain open"
  else
    record "S1" PASS "review_requested dispatched $id, scripted session closed it, 0 open mail"
  fi
}

# ══════════════════════════════════════════════════════════════════════════
# S2 — closed, worktree kept, comment_reply -> route reopens the same id;
# mail send resumes it; session reads and closes.
# ══════════════════════════════════════════════════════════════════════════
run_S2() {
  local n=102
  local branch="pr-branch-$n"
  make_pr_ref "$n" "s2 content"
  gh_set_state "$n" OPEN

  local wt="$T/manual-worktrees/s2"
  local id; id="$(make_review_initiative "s2" "$wt" "$n" closed)"

  local bodyfile="$T/s2-body.txt"
  printf 'New comment on PR #%d.\n' "$n" > "$bodyfile"

  local before; before="$(list_initiative_ids)"
  local out rc
  out="$(ateam route-pr-event --repo "$OWNER_REPO" --pr-number "$n" --head-branch "$branch" \
        --transition comment_reply --body-file "$bodyfile" \
        --pr-url "https://github.com/$OWNER_REPO/pull/$n" 2>&1)"
  rc=$?

  local new_ids; new_ids="$(new_initiative_since "$before")"
  local status; status="$(issue_status "$id")"

  local reason=""
  [ "$rc" = "0" ] || reason="route-pr-event exited $rc (output: $out); "
  [ -z "$new_ids" ] || reason="${reason}a NEW initiative was spawned ($new_ids) instead of reopening $id; "
  [ "$status" = "closed" ] || reason="${reason}initiative $id status='$status', expected reopened+re-closed (closed) by the scripted session; "

  if [ -n "$reason" ]; then
    record "S2" FAIL "$reason"
    return
  fi
  if ! assert_zero_open_mail "S2"; then
    record "S2" FAIL "same-id reopen/resume/close worked, but mail wisps remain open"
    return
  fi
  record "S2" PASS "comment_reply reopened the SAME id $id, resumed session read+closed, 0 open mail"
}

# ══════════════════════════════════════════════════════════════════════════
# S3 — no event; reap scan with gh OPEN -> [initiative stays] closed, worktree
# still on disk (the PR-open gate on worktree removal, agent-teams-8st0.13).
# ══════════════════════════════════════════════════════════════════════════
run_S3() {
  local n=103
  make_pr_ref "$n" "s3 content"
  gh_set_state "$n" OPEN

  local wt="$T/manual-worktrees/s3"
  local id; id="$(make_review_initiative "s3" "$wt" "$n" closed)"

  local out rc
  out="$(ateam reap --grace=0s 2>&1)"
  rc=$?

  local status; status="$(issue_status "$id")"
  local reason=""
  [ "$rc" = "0" ] || reason="ateam reap exited $rc (output: $out); "
  [ "$status" = "closed" ] || reason="${reason}initiative $id status='$status', expected closed; "
  [ -d "$wt" ] || reason="${reason}worktree $wt was removed, expected kept while PR is OPEN; "

  if [ -n "$reason" ]; then
    record "S3" FAIL "$reason"
  else
    record "S3" PASS "reap scan kept $id's worktree on disk while gh reports PR #$n OPEN"
  fi
}

# ══════════════════════════════════════════════════════════════════════════
# S4 — closed, worktree removed, PR OPEN, comment_reply -> route reopens the
# SAME id, worktree recreated at the same path (HEAD == PR-head commit),
# session reads and closes. No new initiative exists.
# ══════════════════════════════════════════════════════════════════════════
run_S4() {
  local n=104
  local branch="pr-branch-$n"
  make_pr_ref "$n" "s4 content"
  gh_set_state "$n" OPEN
  local want_head; want_head="$(pr_head_sha "$n")"

  local wt="$T/manual-worktrees/s4"
  local id; id="$(make_review_initiative "s4" "$wt" "$n" closed)"
  rm -rf "$wt" # worktree removed (reaped)

  local bodyfile="$T/s4-body.txt"
  printf 'New comment on PR #%d.\n' "$n" > "$bodyfile"

  local before; before="$(list_initiative_ids)"
  local out rc
  out="$(ateam route-pr-event --repo "$OWNER_REPO" --pr-number "$n" --head-branch "$branch" \
        --transition comment_reply --body-file "$bodyfile" \
        --pr-url "https://github.com/$OWNER_REPO/pull/$n" 2>&1)"
  rc=$?

  local new_ids; new_ids="$(new_initiative_since "$before")"
  local status; status="$(issue_status "$id")"
  local got_head=""
  [ -d "$wt" ] && got_head="$("$REAL_GIT" -C "$wt" rev-parse HEAD 2>/dev/null || true)"

  local reason=""
  [ "$rc" = "0" ] || reason="route-pr-event exited $rc (output: $out); "
  [ -z "$new_ids" ] || reason="${reason}a NEW initiative was spawned ($new_ids) instead of reopening $id; "
  [ -d "$wt" ] || reason="${reason}worktree $wt was not recreated; "
  if [ -d "$wt" ] && [ "$got_head" != "$want_head" ]; then
    reason="${reason}recreated worktree HEAD=$got_head, expected the PR #$n head $want_head; "
  fi
  [ "$status" = "closed" ] || reason="${reason}initiative $id status='$status', expected closed (scripted session should have re-closed it); "

  if [ -n "$reason" ]; then
    record "S4" FAIL "$reason"
    return
  fi
  if ! assert_zero_open_mail "S4"; then
    record "S4" FAIL "recreate/reopen/close worked, but mail wisps remain open"
    return
  fi
  record "S4" PASS "reopened SAME id $id, worktree recreated at PR #$n head $want_head, 0 open mail"
}

# ══════════════════════════════════════════════════════════════════════════
# S4-moot — same shape as S4 but gh reports MERGED: message closed with
# delivery:moot, initiative closed, no launch.
# ══════════════════════════════════════════════════════════════════════════
run_S4_moot() {
  local n=105
  local branch="pr-branch-$n"
  make_pr_ref "$n" "s4-moot content"
  gh_set_state "$n" MERGED

  local wt="$T/manual-worktrees/s4moot"
  local id; id="$(make_review_initiative "s4moot" "$wt" "$n" closed)"
  rm -rf "$wt"

  local bodyfile="$T/s4moot-body.txt"
  printf 'New comment on PR #%d.\n' "$n" > "$bodyfile"

  local before_launch; before_launch="$(log_line_count)"
  local before_ids; before_ids="$(list_initiative_ids)"
  local out rc
  out="$(ateam route-pr-event --repo "$OWNER_REPO" --pr-number "$n" --head-branch "$branch" \
        --transition comment_reply --body-file "$bodyfile" \
        --pr-url "https://github.com/$OWNER_REPO/pull/$n" 2>&1)"
  rc=$?

  local new_ids; new_ids="$(new_initiative_since "$before_ids")"
  local status; status="$(issue_status "$id")"
  local launches; launches="$(launch_count_since "$before_launch")"
  local moot_ids
  moot_ids="$(ateam mail list --json --limit=2000 2>/dev/null | jq -r --arg id "$id" '[.[] | select(.to==$id)] | .[].id')"
  local moot_count; moot_count="$(printf '%s\n' "$moot_ids" | grep -c . || true)"

  local reason=""
  [ "$rc" = "0" ] || reason="route-pr-event exited $rc (output: $out); "
  [ -z "$new_ids" ] || reason="${reason}a NEW initiative was spawned ($new_ids); "
  [ "$launches" = "0" ] || reason="${reason}a session was launched ($launches --bg call(s)) despite the PR being MERGED; "
  [ "$status" = "closed" ] || reason="${reason}initiative $id status='$status', expected closed; "
  if [ "$moot_count" -lt 1 ]; then
    reason="${reason}no message recorded for $id; "
  else
    local mid labels
    for mid in $moot_ids; do
      labels="$(bd -C "$AGENT_TEAMS_HOME" show "$mid" --json 2>/dev/null | jq -r '.[0].labels[]?')"
      if ! printf '%s\n' "$labels" | grep -qx 'delivery:moot'; then
        reason="${reason}message $mid missing the delivery:moot label (got: $(printf '%s' "$labels" | tr '\n' ',')); "
      fi
    done
  fi

  if [ -n "$reason" ]; then
    record "S4-moot" FAIL "$reason"
    return
  fi
  if ! assert_zero_open_mail "S4-moot"; then
    record "S4-moot" FAIL "moot handling worked, but mail wisps remain open"
    return
  fi
  record "S4-moot" PASS "PR #$n MERGED — mail closed moot ($moot_count msg), $id closed, no launch, 0 open mail"
}

# ══════════════════════════════════════════════════════════════════════════
# S5 — hung tick on a review with 1 unread -> not closed (then the scripted
# session reads it; 0 open).
# ══════════════════════════════════════════════════════════════════════════
run_S5() {
  local n=106
  make_pr_ref "$n" "s5 content"
  gh_set_state "$n" OPEN

  local wt="$T/manual-worktrees/s5"
  local id; id="$(make_review_initiative "s5" "$wt" "$n" open)"

  # Fake a live session for this cwd so `ateam mail send` delivers via the
  # doorbell (no resume/launch) — leaving exactly 1 unread message sitting on
  # an initiative the hung tick will otherwise see as DEAD (no live tied
  # session) once the fake is retracted below.
  set_sessions_live "$wt"
  local nudge="$T/s5-nudge.txt"
  printf 'Please take another look.\n' > "$nudge"
  local send_out send_rc
  send_out="$(ateam mail send "$id" --file "$nudge" --sender ops 2>&1)"
  send_rc=$?
  set_sessions_empty

  if [ "$send_rc" != "0" ]; then
    record "S5" FAIL "setup: could not deliver the 1 unread message (ateam mail send rc=$send_rc: $send_out)"
    return
  fi
  local unread_before; unread_before="$(mail_open_count_for "$id")"
  if [ "$unread_before" != "1" ]; then
    record "S5" FAIL "setup: expected exactly 1 unread message on $id before the hung tick, got $unread_before"
    return
  fi

  export AGENT_TEAMS_TRANSPORT=stub
  export AGENT_TEAMS_STUB_DIR="$STATE_DIR/transport-stub"
  mkdir -p "$AGENT_TEAMS_STUB_DIR"
  export AGENT_TEAMS_HUNG_TICK_INTERVAL=1s

  ateam relay >"$STATE_DIR/relay-s5.log" 2>&1 &
  RELAY_PID=$!
  sleep 3
  if kill -0 "$RELAY_PID" 2>/dev/null; then
    kill "$RELAY_PID" 2>/dev/null || true
    wait "$RELAY_PID" 2>/dev/null || true
  fi
  RELAY_PID=""
  unset AGENT_TEAMS_TRANSPORT AGENT_TEAMS_STUB_DIR AGENT_TEAMS_HUNG_TICK_INTERVAL

  local status_after; status_after="$(issue_status "$id")"

  local reason=""
  if [ "$status_after" = "closed" ]; then
    reason="the hung-tick backstop closed $id despite 1 unread message (relay log: $(tr '\n' ' ' < "$STATE_DIR/relay-s5.log")); "
  fi

  # Regardless of the assertion above, drain the mail so the scenario still
  # ends at 0 open, per the blanket invariant every scenario must satisfy.
  if [ "$status_after" = "closed" ]; then
    bd -C "$AGENT_TEAMS_HOME" reopen "$id" >/dev/null 2>&1 || true
  fi
  local resume_out resume_rc
  resume_out="$(ateam resume "$id" --launch-prompt "/agent-teams:review-pr $id" --model sonnet 2>&1)"
  resume_rc=$?
  if [ "$resume_rc" != "0" ]; then
    reason="${reason}cleanup: ateam resume $id failed rc=$resume_rc ($resume_out); "
  fi

  if [ -n "$reason" ]; then
    record "S5" FAIL "$reason"
    return
  fi
  if ! assert_zero_open_mail "S5"; then
    record "S5" FAIL "not-closed held, but mail wisps remain open after the scripted session drained it"
    return
  fi
  record "S5" PASS "hung tick did not close $id with 1 unread; scripted session then read+closed it, 0 open mail"
}

# ══════════════════════════════════════════════════════════════════════════
# S6 — open, worktree gone (the at-76ynq shape). gh exits 1 three polls in a
# row -> exit 1 each time, exactly 1 open message during the failure; then gh
# OPEN -> healed, 0 open.
# ══════════════════════════════════════════════════════════════════════════
run_S6() {
  local n=107
  local branch="pr-branch-$n"
  make_pr_ref "$n" "s6 content"
  gh_set_failcount "$n" 3

  local wt="$T/manual-worktrees/s6"
  local id; id="$(make_review_initiative "s6" "$wt" "$n" open)"
  rm -rf "$wt" # worktree gone — the at-76ynq shape

  # "3 polls in a row" models pr-shepherd retrying the SAME PR event while gh
  # is unreachable (route.go's own documented purpose for --dedup-key: "so
  # pr-shepherd's retry after a failed reopen or a failed send ... dedups
  # into the same message instead of creating a second one", route.go:107-
  # 109). Retrying via route-pr-event with an IDENTICAL body each time is
  # what actually exercises that contract — 3 distinct hand-written bodies
  # (the harness's original approach) would each hash to a different
  # --dedup-key by design and correctly produce 3 messages, which isn't what
  # this scenario is testing. One fixed body-file, reused for every poll AND
  # the heal retry, keeps repo#pr|transition|body constant so every call
  # computes the same dedup-key (route.go:129-130).
  local bodyfile="$T/s6-body.txt"
  printf 'PR #%d review requires another look; gh unreachable.\n' "$n" > "$bodyfile"

  local reason=""
  local i
  for i in 1 2 3; do
    local out rc
    out="$(ateam route-pr-event --repo "$OWNER_REPO" --pr-number "$n" --head-branch "$branch" \
          --transition comment_reply --body-file "$bodyfile" \
          --pr-url "https://github.com/$OWNER_REPO/pull/$n" 2>&1)"
    rc=$?
    if [ "$rc" = "0" ]; then
      reason="${reason}poll $i: expected exit 1 (gh down), got exit 0 ($out); "
    fi
  done

  local open_during; open_during="$(mail_open_count_for "$id")"
  if [ "$open_during" != "1" ]; then
    reason="${reason}expected exactly 1 open message during the 3 failed polls, got $open_during; "
  fi

  gh_set_state "$n" OPEN
  local heal_out heal_rc
  heal_out="$(ateam route-pr-event --repo "$OWNER_REPO" --pr-number "$n" --head-branch "$branch" \
        --transition comment_reply --body-file "$bodyfile" \
        --pr-url "https://github.com/$OWNER_REPO/pull/$n" 2>&1)"
  heal_rc=$?
  if [ "$heal_rc" != "0" ]; then
    reason="${reason}heal poll: expected exit 0 once gh reports OPEN, got exit $heal_rc ($heal_out); "
  fi
  if [ ! -d "$wt" ]; then
    reason="${reason}heal poll: worktree $wt was not recreated; "
  fi

  if [ -n "$reason" ]; then
    record "S6" FAIL "$reason"
    return
  fi
  if ! assert_zero_open_mail "S6"; then
    record "S6" FAIL "healed, but mail wisps remain open"
    return
  fi
  record "S6" PASS "3 failed gh polls each exited 1 with exactly 1 open message; healed to 0 open once gh reported OPEN"
}

# ══════════════════════════════════════════════════════════════════════════
# S7 — closed DRI initiative (no pr lines).
#  (a) three sends with distinct bodies -> exit 0 each, 3 open messages, stub
#      claude records NO launch, still closed.
#  (b) one body sent 3 times with the same --dedup-key -> 1 more message,
#      exit 0 each.
#  (c) ateam reopen <id>, then start the scripted session -> it reads all 4,
#      0 open.
# ══════════════════════════════════════════════════════════════════════════
run_S7() {
  local wt="$T/manual-worktrees/s7"
  local id; id="$(make_dri_initiative "s7" "$wt" closed)"

  local reason=""

  # (a) three distinct-body sends.
  local before_launch; before_launch="$(log_line_count)"
  local i
  for i in 1 2 3; do
    local f="$T/s7-a-$i.txt"
    printf 's7 distinct body %d\n' "$i" > "$f"
    local out rc
    out="$(ateam mail send "$id" --file "$f" --sender ops 2>&1)"
    rc=$?
    if [ "$rc" != "0" ]; then
      reason="${reason}(a) send $i: expected exit 0, got $rc ($out); "
    fi
  done
  local count_a; count_a="$(mail_open_count_for "$id")"
  [ "$count_a" = "3" ] || reason="${reason}(a) expected 3 open messages, got $count_a; "
  local launches_a; launches_a="$(launch_count_since "$before_launch")"
  [ "$launches_a" = "0" ] || reason="${reason}(a) expected NO launch, got $launches_a --bg call(s); "
  local status_a; status_a="$(issue_status "$id")"
  [ "$status_a" = "closed" ] || reason="${reason}(a) expected $id to remain closed, got '$status_a'; "

  # (b) same body, same --dedup-key, sent 3 times -> should dedup to 1 more.
  local dedup_body="$T/s7-b.txt"
  printf 's7 dedup body\n' > "$dedup_body"
  for i in 1 2 3; do
    local out rc
    out="$(ateam mail send "$id" --file "$dedup_body" --sender ops --dedup-key s7-dedup 2>&1)"
    rc=$?
    if [ "$rc" != "0" ]; then
      reason="${reason}(b) send $i: expected exit 0, got $rc ($out); "
    fi
  done
  local count_b; count_b="$(mail_open_count_for "$id")"
  [ "$count_b" = "4" ] || reason="${reason}(b) expected 4 open messages total (3 + 1 deduped), got $count_b; "

  # (c) reopen, then start the scripted session — it should read all 4 and
  # end at 0 open.
  local reopen_out reopen_rc
  reopen_out="$(ateam reopen "$id" 2>&1)"
  reopen_rc=$?
  if [ "$reopen_rc" != "0" ]; then
    reason="${reason}(c) ateam reopen $id failed rc=$reopen_rc ($reopen_out); "
  fi
  local resume_out resume_rc
  resume_out="$(ateam resume "$id" 2>&1)"
  resume_rc=$?
  if [ "$resume_rc" != "0" ]; then
    reason="${reason}(c) ateam resume $id failed rc=$resume_rc ($resume_out); "
  fi

  if [ -n "$reason" ]; then
    record "S7" FAIL "$reason"
    return
  fi
  if ! assert_zero_open_mail "S7"; then
    record "S7" FAIL "(a)/(b)/(c) held, but mail wisps remain open after the scripted session drained them"
    return
  fi
  record "S7" PASS "3 distinct sends (no launch) + keyed dedup + reopen/resume drain — 4 msgs total, 0 open"
}

# ══════════════════════════════════════════════════════════════════════════
# S8 — closed review, worktree kept, reopen fails once -> route exits 1,
# exactly 1 message stored for <id>; retry (reopen succeeds) -> resend
# dedups, session launched, reads and closes; 1 message total, 0 open.
# ══════════════════════════════════════════════════════════════════════════
run_S8() {
  local n=108
  local branch="pr-branch-$n"
  make_pr_ref "$n" "s8 content"
  gh_set_state "$n" OPEN

  local wt="$T/manual-worktrees/s8"
  local id; id="$(make_review_initiative "s8" "$wt" "$n" closed)"

  local bodyfile="$T/s8-body.txt"
  printf 'New comment on PR #%d.\n' "$n" > "$bodyfile"

  # First attempt: bd shim makes `bd reopen <id>` fail exactly once.
  touch "$REOPEN_FAIL_FLAG"
  local out1 rc1
  out1="$(PATH="$BD_SHIM_DIR:$PATH" ateam route-pr-event --repo "$OWNER_REPO" --pr-number "$n" \
        --head-branch "$branch" --transition comment_reply --body-file "$bodyfile" \
        --pr-url "https://github.com/$OWNER_REPO/pull/$n" 2>&1)"
  rc1=$?
  rm -f "$REOPEN_FAIL_FLAG"

  local count_after_fail; count_after_fail="$(mail_open_count_for "$id")"

  local reason=""
  [ "$rc1" != "0" ] || reason="first attempt: expected route-pr-event to exit nonzero (reopen failed), got 0 (output: $out1); "
  [ "$count_after_fail" = "1" ] || reason="${reason}first attempt: expected exactly 1 message stored for $id, got $count_after_fail; "

  # Retry: reopen succeeds this time (no shim active).
  local before_launch; before_launch="$(log_line_count)"
  local out2 rc2
  out2="$(ateam route-pr-event --repo "$OWNER_REPO" --pr-number "$n" --head-branch "$branch" \
        --transition comment_reply --body-file "$bodyfile" \
        --pr-url "https://github.com/$OWNER_REPO/pull/$n" 2>&1)"
  rc2=$?
  [ "$rc2" = "0" ] || reason="${reason}retry: expected route-pr-event to exit 0, got $rc2 (output: $out2); "

  local launches; launches="$(launch_count_since "$before_launch")"
  [ "$launches" -ge "1" ] || reason="${reason}retry: expected a session launch, got $launches --bg call(s); "

  local status_after; status_after="$(issue_status "$id")"
  [ "$status_after" = "closed" ] || reason="${reason}retry: expected $id closed by the scripted session, got '$status_after'; "

  local count_total; count_total="$(ateam mail list --json --limit=2000 2>/dev/null | jq --arg id "$id" '[.[] | select(.to==$id)] | length')"
  [ "$count_total" = "1" ] || reason="${reason}expected 1 message total for $id (retry should dedup, not add a second), got $count_total; "

  if [ -n "$reason" ]; then
    record "S8" FAIL "$reason"
    return
  fi
  if ! assert_zero_open_mail "S8"; then
    record "S8" FAIL "reopen-failure/retry/close held, but mail wisps remain open"
    return
  fi
  record "S8" PASS "reopen failed once (route exit 1, 1 msg stored); retry healed same id $id, deduped, 0 open"
}

# ══════════════════════════════════════════════════════════════════════════
# K1 — text check (not a live run): the review-pr SKILL.md has no "do NOT run
# ateam mail inbox" line, and has an inbox read before ateam close. The
# stubbed session above always reads mail regardless of what the skill file
# says, so no S1-S8 run can witness agent-teams-8st0.22 — K1 is the only
# automated witness (the real witness is a real review-pr session at the
# checkpoint).
# ══════════════════════════════════════════════════════════════════════════
run_K1() {
  # K1 is a static text check of the SKILL.md that ships alongside whichever
  # binary ATEAM_BIN was built from — NOT necessarily this script's own
  # checkout. Default to $ROOT (the common case: testing this worktree's own
  # binary), but let a caller comparing a control binary built from a
  # different checkout (e.g. origin/main + a cherry-pick) point K1 at THAT
  # checkout's SKILL.md via SKILL_ROOT, so K1 actually varies with the binary
  # under test instead of always reading this worktree's copy.
  local skill_root="${SKILL_ROOT:-$ROOT}"
  local f="$skill_root/plugins/agent-teams/skills/review-pr/SKILL.md"
  if [ ! -f "$f" ]; then
    record "K1" FAIL "SKILL.md not found at $f"
    return
  fi

  local reason=""
  if grep -qiE 'do NOT run.*ateam mail inbox' "$f"; then
    reason="the skill still tells the session NOT to run \`ateam mail inbox\`; "
  fi

  local inbox_line close_line
  inbox_line="$(grep -n 'ateam mail inbox' "$f" | grep -vi 'do NOT run' | head -1 | cut -d: -f1)"
  close_line="$(grep -n 'ateam close' "$f" | head -1 | cut -d: -f1)"
  if [ -z "$inbox_line" ] || [ -z "$close_line" ] || [ "$inbox_line" -ge "$close_line" ]; then
    reason="${reason}no 'ateam mail inbox' read instruction appears before an 'ateam close' line; "
  fi

  if [ -n "$reason" ]; then
    record "K1" FAIL "$reason"
  else
    record "K1" PASS "SKILL.md has no 'do NOT run mail inbox' line and reads mail before closing"
  fi
}

# ══════════════════════════════════════════════════════════════════════════
# main
# ══════════════════════════════════════════════════════════════════════════

run_S1
run_S2
run_S3
run_S4
run_S4_moot
run_S5
run_S6
run_S7
run_S8
run_K1

echo ""
echo "── review-lifecycle results ─────────────────────────────────────────────"
awk -F'\t' '{printf "%-10s %-5s %s\n", $1, $2, $3}' "$RESULTS_FILE"
echo "─────────────────────────────────────────────────────────────────────────"

exit "$OVERALL_RC"
