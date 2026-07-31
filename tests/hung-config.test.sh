#!/usr/bin/env bash
# hung-config.test.sh — the shipped `ateam relay` binary resolves its
# stall-detection config from a real workspace on disk (agent-teams-rhnc.3).
#
# Why a shell test on top of the Go tests: `go test` exercises an in-process
# build. What actually runs on Eric's machine is the committed platform binary
# (plugins/agent-teams/bin/ateam-darwin-arm64), hand-started from a terminal
# with NO Claude Code environment at all. This is the only layer that does
# what an operator does — a real OS process, a real AGENT_TEAMS_HOME, a real
# config file on disk. Note there is deliberately no Claude Code environment
# here: nothing exports CLAUDE_PLUGIN_OPTION_* or reads plugin.json, which is
# the point — the file tier must work without any of it.
#
# This asserts on the resolved-config STARTUP LOG, not on tick behaviour. With
# the stub transport, Stub.Receive drains its reply-file glob and RETURNS, so
# `ateam relay` exits instead of blocking — which is what lets us read its
# full stderr synchronously, but also means the ticker never fires (the first
# tick would be one interval away). Proving a configured interval reaches the
# ticker is agent-teams-rhnc.2's job, in-process.
#
# Shell tests are INVISIBLE to `go test ./...`.
# Run: bash tests/hung-config.test.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

fail() { echo "FAIL $1: $2"; exit 1; }

# ── binary build (same pattern as tests/multi-machine-routing.test.sh) ──────

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

cat > "$T/bin/claude" <<'SHIM'
#!/usr/bin/env bash
if [ "${1:-}" = "agents" ]; then printf '[]\n'; fi
exit 0
SHIM
chmod +x "$T/bin/claude"

export PATH="$T/bin:$PATH"

# ── helpers ────────────────────────────────────────────────────────────────

# mkhome creates a fresh AGENT_TEAMS_HOME-shaped workspace: a git repo (bd
# requires one) with an initialized bd db.
mkhome() {
  local home="$1"
  mkdir -p "$home"
  git -C "$home" init -q
  (cd "$home" && bd init --prefix at --non-interactive >/dev/null)
}

# run_relay runs the real binary against home with an empty stub dir, so
# Receive drains nothing and returns immediately. Prints combined output;
# writes the exit status to $relay_status rather than letting set -e abort,
# because "a bad config must not crash the relay" is itself an assertion.
relay_status=0
run_relay() {
  local home="$1"
  local stub="$home/stub"
  mkdir -p "$stub"
  relay_status=0
  AGENT_TEAMS_HOME="$home" AGENT_TEAMS_STUB_DIR="$stub" AGENT_TEAMS_TRANSPORT=stub \
    ateam relay 2>&1 || relay_status=$?
}

# assert_resolved greps the startup summary for one exact key=value token.
# Asserting the RESOLVED value, not the absence of an error, is deliberate: a
# relay that silently ignored the config file would pass a weaker test.
assert_resolved() {
  local case_name="$1" out="$2" token="$3"
  grep -qF -- "$token" <<<"$out" \
    || fail "$case_name" "expected '$token' in the startup log, got:
$out"
}

# assert_all_defaults asserts every one of the seven keys reports its shipped
# default — used by the cases where nothing valid was configured.
assert_all_defaults() {
  local case_name="$1" out="$2"
  assert_resolved "$case_name" "$out" "tick_interval=20m0s"
  assert_resolved "$case_name" "$out" "stuck_threshold=2h0m0s"
  assert_resolved "$case_name" "$out" "wake_attempts_before_alert=2"
  assert_resolved "$case_name" "$out" "workproduct_flat_threshold=2h0m0s"
  assert_resolved "$case_name" "$out" "workproduct_alert_threshold=4h0m0s"
  assert_resolved "$case_name" "$out" "dead_worktree_threshold=2h0m0s"
  assert_resolved "$case_name" "$out" "transcript_corroborator_window=2h0m0s"
}

# ── case 1: no env, no file -> the shipped defaults ────────────────────────

home1="$T/home1"
mkhome "$home1"
out=$(run_relay "$home1")
[ "$relay_status" -eq 0 ] || fail case1 "relay exited $relay_status, want 0. Output:
$out"
assert_all_defaults case1 "$out"
grep -q 'starting on transport "stub"' <<<"$out" \
  || fail case1 "missing the normal startup line, got:
$out"
# A missing config file is the normal case and must be silent.
grep -qi 'hung config:.*using default' <<<"$out" \
  && fail case1 "a missing config file must not warn, got:
$out"
echo "ok case1: no config -> the shipped defaults, no warning"

# ── case 2: the workspace file beats the default ───────────────────────────
#
# This is the case that proves the mechanism works for a hand-started relay
# with no Claude Code environment: a plain JSON file in the workspace, read by
# a real OS process. The partial-file half matters just as much — one key set
# must not blank the other six.

home2="$T/home2"
mkhome "$home2"
printf '{"tick_interval":"45m"}\n' > "$home2/hung-config.json"
out=$(run_relay "$home2")
[ "$relay_status" -eq 0 ] || fail case2 "relay exited $relay_status, want 0. Output:
$out"
assert_resolved case2 "$out" "tick_interval=45m0s"
assert_resolved case2 "$out" "stuck_threshold=2h0m0s"
assert_resolved case2 "$out" "wake_attempts_before_alert=2"
assert_resolved case2 "$out" "workproduct_flat_threshold=2h0m0s"
assert_resolved case2 "$out" "workproduct_alert_threshold=4h0m0s"
assert_resolved case2 "$out" "dead_worktree_threshold=2h0m0s"
assert_resolved case2 "$out" "transcript_corroborator_window=2h0m0s"
echo "ok case2: hung-config.json beats the default; a partial file leaves the rest alone"

# ── case 3: env beats the file ─────────────────────────────────────────────

out=$(AGENT_TEAMS_HUNG_TICK_INTERVAL=90m run_relay "$home2")
[ "$relay_status" -eq 0 ] || fail case3 "relay exited $relay_status, want 0. Output:
$out"
assert_resolved case3 "$out" "tick_interval=1h30m0s"
echo "ok case3: AGENT_TEAMS_HUNG_TICK_INTERVAL beats the file"

# ── case 4a: malformed JSON degrades to all defaults, never crashes ────────
#
# The relay is an unsupervised singleton that also routes every one of Eric's
# messages. A config typo taking down message routing would be far worse than
# the noise this config exists to tune, so exit 0 + normal startup lines +
# all defaults + a warning naming the file is the required behaviour.

home4a="$T/home4a"
mkhome "$home4a"
printf '{banana\n' > "$home4a/hung-config.json"
out=$(run_relay "$home4a")
[ "$relay_status" -eq 0 ] || fail case4a "malformed JSON must not crash the relay; exited $relay_status. Output:
$out"
grep -q 'starting on transport "stub"' <<<"$out" \
  || fail case4a "relay must still print its normal startup lines, got:
$out"
assert_all_defaults case4a "$out"
grep -q 'hung-config.json' <<<"$out" \
  || fail case4a "the warning must name the config file, got:
$out"
echo "ok case4a: malformed JSON -> exit 0, all defaults, warning names the file"

# ── case 4b: one bad value degrades alone ──────────────────────────────────
#
# Per-field fallback: whole-file rejection here would be a bug, silently
# discarding the good sibling value.

home4b="$T/home4b"
mkhome "$home4b"
printf '{"stuck_threshold":"banana","tick_interval":"45m"}\n' > "$home4b/hung-config.json"
out=$(run_relay "$home4b")
[ "$relay_status" -eq 0 ] || fail case4b "a bad value must not crash the relay; exited $relay_status. Output:
$out"
assert_resolved case4b "$out" "stuck_threshold=2h0m0s"
assert_resolved case4b "$out" "tick_interval=45m0s"
grep -q 'stuck_threshold' <<<"$out" \
  || fail case4b "the warning must name the offending key, got:
$out"
grep -q 'banana' <<<"$out" \
  || fail case4b "the warning must quote the offending value, got:
$out"
echo "ok case4b: one bad value defaults alone; its valid sibling still applies"

echo "PASS hung-config.test.sh"
