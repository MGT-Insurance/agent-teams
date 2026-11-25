#!/usr/bin/env bash
# hook-no-open-match-log.test.sh — when wake-watcher.sh or inbox-drain.sh exits
# no-open-match, hooks.log carries the verb's stderr reason plus the inputs the
# hook passed (cwd, session id). Before this, stderr went to /dev/null and an
# error looked the same as a genuine no-match (dsp-7gr.8, at-dol sandbox).
#
# A stub `ateam` prints a repo-disabled reason on stderr and no id on stdout;
# the hooks must exit 0, arm nothing, and log the reason.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HOOKS="$ROOT/plugins/agent-teams/hooks/scripts"

PASS=0; FAIL=0
pass() { echo "PASS $*"; PASS=$((PASS+1)); }
fail() { echo "FAIL $*"; FAIL=$((FAIL+1)); }

T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME"
git -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && bd init --prefix at --non-interactive >/dev/null)
HOOKS_LOG="$AGENT_TEAMS_HOME/debug/hooks.log"
WORK="$T/work"; mkdir -p "$WORK"

PLUGIN="$T/plugin"
mkdir -p "$PLUGIN/bin"
cat > "$PLUGIN/bin/ateam" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  resolve-initiative|hook-scan)
    echo "$1: reason=repo-disabled initiative=at-x repo=https://github.com/o/r" >&2
    ;;
esac
exit 0
EOF
chmod +x "$PLUGIN/bin/ateam"
export CLAUDE_PLUGIN_ROOT="$PLUGIN"

run_hook() {
  local script="$1" rc=0
  : > "$T/out"
  bash -c 'cd "$1" && exec bash "$2" <<<"$3"' _ "$WORK" "$HOOKS/$script" \
    '{"session_id":"sess-cloud"}' >"$T/out" 2>&1 || rc=$?
  echo "$rc"
}

for script in wake-watcher.sh inbox-drain.sh; do
  rm -f "$HOOKS_LOG"
  rc=$(run_hook "$script")
  [ "$rc" = 0 ] && pass "$script: exit 0" || fail "$script: exit $rc"
  if grep -q "no-open-match" "$HOOKS_LOG" 2>/dev/null; then
    pass "$script: logged no-open-match"
  else
    fail "$script: no no-open-match line in hooks.log"
  fi
  if grep -q "reason=repo-disabled initiative=at-x repo=https://github.com/o/r" "$HOOKS_LOG" 2>/dev/null; then
    pass "$script: stderr reason in hooks.log"
  else
    fail "$script: stderr reason missing from hooks.log: $(cat "$HOOKS_LOG" 2>/dev/null)"
  fi
  if grep -q "session=sess-cloud" "$HOOKS_LOG" 2>/dev/null && grep -q "pwd=" "$HOOKS_LOG"; then
    pass "$script: inputs (pwd, session) in hooks.log"
  else
    fail "$script: inputs missing from hooks.log"
  fi
  if [ -z "$(cat "$T/out")" ]; then
    pass "$script: nothing on stdout/stderr (model prompt stays clean)"
  else
    fail "$script: leaked output: $(cat "$T/out")"
  fi
done

echo "$PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
