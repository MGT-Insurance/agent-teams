#!/usr/bin/env bash
# steward-config-toml.test.sh — the shipped `ateam steward start` binary
# resolves its --autocompact / --settings launch argv from config.toml's
# auto_compact_window key on a real OS process, with NO Claude Code
# environment at all (agent-teams-qox8.6 / qox8.10).
#
# Why a shell test on top of the Go tests: `go test` exercises
# driAutoCompactWindow and stewardLaunchArgs in-process. What actually runs on
# Eric's machine when he hand-starts the Steward from a terminal is the
# committed platform binary (plugins/agent-teams/bin/ateam-darwin-arm64),
# started with NO CLAUDE_PLUGIN_OPTION_* env vars and no plugin.json in play
# at all — config.toml is the sole config source (agent-teams-qox8). This is
# the exact bug Eric hit live: a bare-terminal `ateam steward start` launched
# with no autocompact window, because the code used to read
# CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW — an env var a plugin-launched
# session carries but a bare terminal never sets. This test regresses the
# moment driAutoCompactWindow (or stewardLaunchArgs) reads that env var, or
# any env var, again instead of config.toml.
#
# Mechanics: a stub `claude` on PATH answers the singleton pre-flight query
# ("claude agents ..." -> "[]", so findLiveStewardSession sees zero live
# sessions and the launch step is reached) and, for the real launch
# invocation, records its argv — one argument per line — to a capture file
# instead of spawning anything real.
#
# Shell tests are INVISIBLE to `go test ./...`.
# Run: bash tests/steward-config-toml.test.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

fail() { echo "FAIL $1: $2"; exit 1; }

# ── binary build (same pattern as tests/hung-config.test.sh) ────────────────

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

# The stub `claude`: answers the singleton pre-flight query
# (`claude agents --all --json` -> "[]", satisfying findLiveStewardSession
# with zero live sessions so the launch step is actually reached) and, for
# any other invocation (the real `claude --bg ...` launch), records its argv
# — one argument per line — to $CLAUDE_CAPTURE_FILE instead of spawning a
# real session.
cat > "$T/bin/claude" <<'SHIM'
#!/usr/bin/env bash
if [ "${1:-}" = "agents" ]; then
  printf '[]\n'
  exit 0
fi
: "${CLAUDE_CAPTURE_FILE:?CLAUDE_CAPTURE_FILE not set}"
printf '%s\n' "$@" > "$CLAUDE_CAPTURE_FILE"
exit 0
SHIM
chmod +x "$T/bin/claude"

export PATH="$T/bin:$PATH"

# ── helpers ────────────────────────────────────────────────────────────────

# mkhome creates a fresh AGENT_TEAMS_HOME-shaped workspace: a git repo (bd
# requires one) with an initialized bd db — same as tests/hung-config.test.sh.
mkhome() {
  local home="$1"
  mkdir -p "$home"
  git -C "$home" init -q
  (cd "$home" && bd init --prefix at --non-interactive >/dev/null)
}

# run_steward_start runs the real committed binary's `ateam steward start`
# against home in a BARE environment: no CLAUDE_PLUGIN_OPTION_* exported at
# all, mirroring the terminal Eric hit the bug in. capture is truncated first
# so a stale prior-run file can never leak a false pass. Combined
# stdout+stderr goes to $T/last-run.log for failure diagnostics; the function
# call's own exit status IS the command's exit status (no subshell in the
# way, unlike a `$(...)` capture, so `if ! run_steward_start ...` sees the
# real result under `set -e`).
run_steward_start() {
  local home="$1" capture="$2"
  : > "$capture"
  AGENT_TEAMS_HOME="$home" CLAUDE_CAPTURE_FILE="$capture" \
    env -u CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW \
        -u CLAUDE_PLUGIN_OPTION_USE_ADVISORS \
        -u CLAUDE_PLUGIN_OPTION_DRI_MODEL \
    ateam steward start >"$T/last-run.log" 2>&1
}

# run_steward_start_with_legacy_env is run_steward_start's case-3 sibling: it
# EXPORTS CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW=value (the removed env
# chain) instead of unsetting it, to prove it is fully ignored.
run_steward_start_with_legacy_env() {
  local home="$1" capture="$2" value="$3"
  : > "$capture"
  AGENT_TEAMS_HOME="$home" CLAUDE_CAPTURE_FILE="$capture" \
    CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW="$value" \
    env -u CLAUDE_PLUGIN_OPTION_USE_ADVISORS \
        -u CLAUDE_PLUGIN_OPTION_DRI_MODEL \
    ateam steward start >"$T/last-run.log" 2>&1
}

# require_success runs and fails loudly (naming case_name) if steward start
# did not exit 0 — a config.toml value or an exported legacy env var must
# never crash the launch. Deliberately not `if ! "$@"; then status=$?` — that
# would capture the negation's own exit status (always 0 inside the then
# branch), not the real one; `"$@" || status=$?`, called directly rather than
# via `$(...)`, keeps both `set -e` safety and the real exit code.
require_success() {
  local case_name="$1"
  shift
  local status=0
  "$@" || status=$?
  if [ "$status" -ne 0 ]; then
    fail "$case_name" "ateam steward start exited $status. Output:
$(cat "$T/last-run.log")"
  fi
}

# assert_autocompact_flag asserts capture contains a "--autocompact" line
# immediately followed by a line equal to want.
assert_autocompact_flag() {
  local case_name="$1" capture="$2" want="$3"
  local n
  # `|| true` after the pipeline: under `pipefail`, a grep miss would
  # otherwise make this assignment itself fail and — since it's a bare
  # top-level command, not part of an if/&&/|| — trip `set -e` and abort
  # with a raw pipefail error instead of the clear `fail` message below,
  # which is exactly the diagnostic this regression test exists to give.
  n=$(grep -n -x -F -- "--autocompact" "$capture" | head -1 | cut -d: -f1 || true)
  [ -n "$n" ] || fail "$case_name" "expected --autocompact in captured argv, got:
$(cat "$capture")"
  local next
  next=$(sed -n "$((n + 1))p" "$capture")
  [ "$next" = "$want" ] || fail "$case_name" "expected --autocompact to be followed by '$want', got '$next'. Full argv:
$(cat "$capture")"
}

# assert_no_autocompact_flag asserts capture contains NO "--autocompact"
# line at all.
assert_no_autocompact_flag() {
  local case_name="$1" capture="$2"
  grep -qxF -- "--autocompact" "$capture" \
    && fail "$case_name" "expected NO --autocompact flag in captured argv, got:
$(cat "$capture")"
  return 0
}

# settings_json extracts the value of the --settings flag: the line
# immediately after a line equal to "--settings" in capture. Empty if no
# --settings flag is present.
settings_json() {
  local capture="$1"
  local n
  # `|| true`: see assert_autocompact_flag above — same pipefail hazard.
  n=$(grep -n -x -F -- "--settings" "$capture" | head -1 | cut -d: -f1 || true)
  [ -n "$n" ] || return 0
  sed -n "$((n + 1))p" "$capture"
}

# ── case 1: config.toml sets auto_compact_window -> --autocompact + settings ─

home1="$T/home1"
mkhome "$home1"
printf 'auto_compact_window = 300000\n' > "$home1/config.toml"
capture1="$T/capture1"
require_success case1 run_steward_start "$home1" "$capture1"
assert_autocompact_flag case1 "$capture1" "300000"
settings1=$(settings_json "$capture1")
[ -n "$settings1" ] || fail case1 "expected a --settings flag in captured argv, got:
$(cat "$capture1")"
grep -qF '"autoCompactWindow":300000' <<<"$settings1" \
  || fail case1 "expected autoCompactWindow\":300000 in --settings, got: $settings1"
grep -qF '"autoCompactEnabled":true' <<<"$settings1" \
  || fail case1 "expected autoCompactEnabled\":true in --settings, got: $settings1"
echo "ok case1: config.toml's auto_compact_window -> --autocompact 300000 and --settings carries autoCompactEnabled/autoCompactWindow"

# ── case 2: no config.toml, bare env -> no --autocompact, no window in settings

home2="$T/home2"
mkhome "$home2"
capture2="$T/capture2"
require_success case2 run_steward_start "$home2" "$capture2"
assert_no_autocompact_flag case2 "$capture2"
settings2=$(settings_json "$capture2")
if [ -n "$settings2" ]; then
  grep -qF 'autoCompactWindow' <<<"$settings2" \
    && fail case2 "expected no autoCompactWindow key in --settings with no config.toml, got: $settings2"
fi
echo "ok case2: no config.toml -> no --autocompact flag, no autoCompactWindow in --settings"

# ── case 3: no config.toml, but the removed CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW
# env var IS exported -> byte-identical to case 2 (the env var stays dead) ──

home3="$T/home3"
mkhome "$home3"
capture3="$T/capture3"
require_success case3 run_steward_start_with_legacy_env "$home3" "$capture3" "300000"
assert_no_autocompact_flag case3 "$capture3"
settings3=$(settings_json "$capture3")
if [ -n "$settings3" ]; then
  grep -qF 'autoCompactWindow' <<<"$settings3" \
    && fail case3 "expected no autoCompactWindow key in --settings even with the legacy env var exported, got: $settings3"
fi
diff -u "$capture2" "$capture3" \
  || fail case3 "expected argv identical to case2 (env var must be fully ignored) — diff above"
echo "ok case3: CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW exported but ignored — argv identical to no-config-toml case"

echo "PASS steward-config-toml.test.sh"
