#!/usr/bin/env bash
# steward-inbox-resolve.test.sh — steward-branch coverage for the doorbell
# consumer hooks (agent-teams-e3mq.27).
#
# Bug: wake-watcher.sh already recognized "cwd is the Steward's own session"
# and set match_id=steward, but inbox-drain.sh and session-start-inbox.sh
# resolved match_id ONLY via worktree lookup over open initiatives — the
# Steward session dir is no initiative's worktree, so both exited
# no-open-match. Consequence: the steward.wake doorbell was never consumed
# and the watcher pidfile never disarmed, so every re-armed watcher instantly
# saw the stale doorbell and exited 2 again (infinite wake storm).
#
# Fix: extract wake-watcher.sh's steward-resolution block into a shared
# lib/resolve-steward.sh helper, used by all three scripts.
#
# This test drives the hook scripts directly (not through `ateam`) against a
# temp AGENT_TEAMS_HOME, with a fake ateam binary that only satisfies the
# scripts' dependency guard — the `ateam mail inbox --peek` signal path
# itself is already covered end-to-end by tests/steward-loop.test.sh.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HOOKS="$ROOT/plugins/agent-teams/hooks/scripts"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

# ── ateam shim: canned mail peek, REAL everything else ──────────────────────
# Only the `ateam mail inbox --peek` signal path is stubbed (it is covered
# end-to-end by tests/steward-loop.test.sh). Every other verb execs a binary
# built from this tree, because the hooks now resolve cwd -> initiative id via
# `ateam resolve-initiative` (agent-teams-ully.9) — a fully canned shim would
# make case B below assert nothing about real resolution, and its old fixed
# "no unread mail" line would come back as the resolved initiative id.
FAKE_PLUGIN_ROOT="$T/plugin-root"
mkdir -p "$FAKE_PLUGIN_ROOT/bin"
go build -C "$ROOT" -o "$FAKE_PLUGIN_ROOT/bin/ateam-real" ./cmd/ateam
cat > "$FAKE_PLUGIN_ROOT/bin/ateam" <<'SHIM'
#!/usr/bin/env bash
case "$1" in
  mail) echo "no unread mail"; exit 0 ;;
esac
exec "$(dirname "$0")/ateam-real" "$@"
SHIM
chmod +x "$FAKE_PLUGIN_ROOT/bin/ateam" "$FAKE_PLUGIN_ROOT/bin/ateam-real"

# ── Shared workspace: a real bd DB (needed for the non-steward-cwd case, and
# for the dependency guard's $ATH/.beads check regardless of branch) ─────────
export AGENT_TEAMS_HOME="$T/ws"
export CLAUDE_PLUGIN_ROOT="$FAKE_PLUGIN_ROOT"
mkdir -p "$AGENT_TEAMS_HOME"
git -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && bd init --prefix at --non-interactive >/dev/null)

# ── Fake steward session: marker file under a session dir ───────────────────
STEWARD_DIR="$AGENT_TEAMS_HOME/steward/session"
mkdir -p "$STEWARD_DIR"
: > "$STEWARD_DIR/.steward-session"

MAILBOX="$AGENT_TEAMS_HOME/mailbox"
mkdir -p "$MAILBOX"
HOOKS_LOG="$AGENT_TEAMS_HOME/debug/hooks.log"

# log_has <script> <detail-substring> — true if hooks.log has a line for
# <script> whose 6th (detail) column contains <detail-substring>. Scoped per
# script so cases sharing one hooks.log can't false-positive off each other.
log_has() {
  awk -F'\t' -v s="$1" -v pat="$2" '$3==s && index($6,pat){f=1} END{exit !f}' \
    "$HOOKS_LOG" 2>/dev/null
}

run_hook() {
  # run_hook <script> <cwd>
  local script="$1" cwd="$2"
  ( cd "$cwd" && bash "$HOOKS/$script" ) >/dev/null 2>&1 || true
}

# ── Case A: inbox-drain.sh in steward cwd consumes the doorbell and disarms
# the watcher pidfile ────────────────────────────────────────────────────────
: > "$MAILBOX/steward.wake"
printf '99999' > "$MAILBOX/steward.watcher.pid"  # not a live pid — exercises the "stale pidfile" path

run_hook inbox-drain.sh "$STEWARD_DIR"

if [ -f "$MAILBOX/steward.wake" ]; then
  fail "A1: steward doorbell not consumed (still present)"
else
  pass "A1: steward doorbell consumed"
fi
if [ -f "$MAILBOX/steward.watcher.pid" ]; then
  fail "A2: steward watcher pidfile not disarmed (still present)"
else
  pass "A2: steward watcher pidfile disarmed"
fi
if log_has inbox-drain.sh "initiative-resolved id=steward"; then
  pass "A3: inbox-drain.sh resolved steward cwd to match_id=steward"
else
  fail "A3: inbox-drain.sh did not resolve steward match_id; log tail: $(tail -5 "$HOOKS_LOG" 2>/dev/null)"
fi

# ── Case B: inbox-drain.sh in a non-steward cwd — no-open-match unaffected ───
NON_STEWARD_DIR="$T/plain-cwd"
mkdir -p "$NON_STEWARD_DIR"
run_hook inbox-drain.sh "$NON_STEWARD_DIR"

if log_has inbox-drain.sh "reason=no-open-match"; then
  pass "B: non-steward cwd with no matching initiative still exits no-open-match"
else
  fail "B: expected reason=no-open-match for non-steward cwd; log tail: $(tail -5 "$HOOKS_LOG" 2>/dev/null)"
fi

# ── Case C: session-start-inbox.sh in steward cwd resolves match_id=steward ──
run_hook session-start-inbox.sh "$STEWARD_DIR"

if log_has session-start-inbox.sh "initiative-resolved id=steward"; then
  pass "C: session-start-inbox.sh resolved steward cwd to match_id=steward"
else
  fail "C: session-start-inbox.sh did not resolve steward match_id; log tail: $(tail -5 "$HOOKS_LOG" 2>/dev/null)"
fi

# ── Case D: wake-watcher.sh steward resolution still works through the shared
# helper — it should block in its poll loop (not exit immediately with
# no-open-match) once resolved as steward. ───────────────────────────────────
rm -f "$MAILBOX/steward.wake" "$MAILBOX/steward.watcher.pid"
ww_log="$T/wake-watcher-d.out"
bash -c 'cd "$1" && exec bash "$2"' _ "$STEWARD_DIR" "$HOOKS/wake-watcher.sh" \
  >"$ww_log" 2>&1 &
ww_pid=$!
sleep 0.5
if kill -0 "$ww_pid" 2>/dev/null; then
  pass "D: wake-watcher.sh entered the poll loop for steward cwd (resolution via shared helper unchanged)"
  kill "$ww_pid" 2>/dev/null || true
  wait "$ww_pid" 2>/dev/null || true
else
  wait "$ww_pid" 2>/dev/null; ww_exit=$?
  fail "D: wake-watcher.sh exited immediately (code $ww_exit) instead of blocking; output: $(cat "$ww_log" 2>/dev/null)"
fi
rm -f "$MAILBOX/steward.watcher.pid" "$MAILBOX/steward.wake"

# ── Case E: a non-steward cwd that DOES match resolves through the verb ──────
# The only shell-level coverage that inbox-drain.sh and session-start-inbox.sh
# resolve a real registered initiative via `ateam resolve-initiative`, from a
# SUBDIRECTORY of the registered worktree rather than its root.
mkdir -p "$T/wt/apps/nested"
printf 'problem: p\nrepo: %s\nworktree: %s\nbranch: feat/x\nteam: t\nmode: interactive\n' "$T/wt" "$T/wt" \
  > "$T/init-body.md"
bd -C "$AGENT_TEAMS_HOME" create --title="Resolve target" --type=task --priority=2 \
  --body-file="$T/init-body.md" >/dev/null
init_id=$(bd -C "$AGENT_TEAMS_HOME" list --status=open --json | jq -r '.[0].id')

run_hook inbox-drain.sh "$T/wt/apps/nested"
if log_has inbox-drain.sh "initiative-resolved id=$init_id"; then
  pass "E1: inbox-drain.sh resolved a worktree subdirectory to $init_id"
else
  fail "E1: inbox-drain.sh did not resolve $init_id from a subdirectory; log tail: $(tail -5 "$HOOKS_LOG" 2>/dev/null)"
fi

run_hook session-start-inbox.sh "$T/wt/apps/nested"
if log_has session-start-inbox.sh "initiative-resolved id=$init_id"; then
  pass "E2: session-start-inbox.sh resolved a worktree subdirectory to $init_id"
else
  fail "E2: session-start-inbox.sh did not resolve $init_id from a subdirectory; log tail: $(tail -5 "$HOOKS_LOG" 2>/dev/null)"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
echo "PASS"
