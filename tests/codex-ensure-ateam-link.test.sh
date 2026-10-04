#!/usr/bin/env bash
# Scratch-HOME regression tests for the Codex ensure-ateam-link hook.
#
# The Codex contract (agent-teams-nly7.1) is create-only: an occupied ateam
# pathname belongs to the user, regardless of node type or symlink health.
# This harness intentionally does not share the Claude hook's self-healing
# fixtures because the two runtimes have different contracts.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams-codex/hooks/ensure-ateam-link.sh"

SCRATCH="$(mktemp -d)"
trap 'rm -rf "$SCRATCH"' EXIT

fail() {
  printf 'FAIL %s\n' "$1" >&2
  exit 1
}

# Call before every fixture write. In particular, no case may derive a link
# pathname from the process's real HOME.
assert_scratch_path() {
  case "$1" in
    "$SCRATCH"/*) ;;
    *) fail "safety: refusing path outside scratch root: $1" ;;
  esac
}

case_paths() {
  local name="$1"
  CASE_ROOT="$SCRATCH/$name"
  CASE_HOME="$CASE_ROOT/home"
  CASE_PLUGIN="$CASE_ROOT/plugin"
  CASE_LINK="$CASE_HOME/.local/bin/ateam"
  assert_scratch_path "$CASE_ROOT"
  assert_scratch_path "$CASE_HOME"
  assert_scratch_path "$CASE_PLUGIN"
  assert_scratch_path "$CASE_LINK"
  mkdir -p "$CASE_HOME" "$CASE_PLUGIN"
}

make_target() {
  local plugin_root="$1" label="$2"
  local target="$plugin_root/bin/ateam"
  assert_scratch_path "$plugin_root"
  assert_scratch_path "$target"
  mkdir -p "$plugin_root/bin"
  printf '#!/bin/sh\nprintf "%%s\\n" "%s"\n' "$label" >"$target"
  chmod +x "$target"
}

run_hook() {
  local label="$1" expected_rc="$2"
  local stdout_file="$CASE_ROOT/hook.stdout" stderr_file="$CASE_ROOT/hook.stderr"
  local rc=0
  assert_scratch_path "$CASE_HOME"
  assert_scratch_path "$CASE_PLUGIN"
  assert_scratch_path "$stdout_file"
  assert_scratch_path "$stderr_file"

  HOME="$CASE_HOME" PLUGIN_ROOT="$CASE_PLUGIN" "$SCRIPT" \
    >"$stdout_file" 2>"$stderr_file" || rc=$?
  [ "$rc" -eq "$expected_rc" ] ||
    fail "$label: hook exited $rc, want $expected_rc (stderr: $(tr '\n' ' ' <"$stderr_file"))"
}

# A portable lstat snapshot. Access time is deliberately excluded; inode,
# mode, size, mtime, and ctime capture whether the pathname itself changed.
lstat_snapshot() {
  local path="$1"
  if stat -f '%d:%i:%p:%z:%m:%c' "$path" >/dev/null 2>&1; then
    stat -f '%d:%i:%p:%z:%m:%c' "$path"
  else
    stat --printf='%d:%i:%f:%s:%Y:%Z' "$path"
  fi
}

assert_link_unchanged() {
  local label="$1" before_target="$2" before_state="$3"
  local after_target after_state
  [ -L "$CASE_LINK" ] || fail "$label: occupied symlink was replaced by a non-symlink"
  after_target="$(readlink "$CASE_LINK")" || fail "$label: cannot read link after hook"
  after_state="$(lstat_snapshot "$CASE_LINK")" || fail "$label: cannot lstat link after hook"
  [ "$after_target" = "$before_target" ] ||
    fail "$label: readlink changed from '$before_target' to '$after_target'"
  [ "$after_state" = "$before_state" ] ||
    fail "$label: symlink inode/state changed from '$before_state' to '$after_state'"
}

# The load-bearing pre-fix witness comes first. The old `ln -sfn` behavior
# retargets this deliberately different, working wrapper.
case_paths "valid-symlink"
make_target "$CASE_PLUGIN" "CODEX-WRAPPER"
ORIGINAL_WRAPPER="$CASE_ROOT/original-wrapper"
assert_scratch_path "$ORIGINAL_WRAPPER"
printf '#!/bin/sh\nprintf "ORIGINAL-WRAPPER\\n"\n' >"$ORIGINAL_WRAPPER"
chmod +x "$ORIGINAL_WRAPPER"
assert_scratch_path "$(dirname "$CASE_LINK")"
mkdir -p "$(dirname "$CASE_LINK")"
ln -s "$ORIGINAL_WRAPPER" "$CASE_LINK"
VALID_BEFORE_TARGET="$(readlink "$CASE_LINK")"
VALID_BEFORE_STATE="$(lstat_snapshot "$CASE_LINK")"
VALID_BEFORE_OUTPUT="$("$CASE_LINK")" || fail "valid symlink: original wrapper did not execute before hook"
[ "$VALID_BEFORE_OUTPUT" = "ORIGINAL-WRAPPER" ] ||
  fail "valid symlink: original wrapper printed '$VALID_BEFORE_OUTPUT' before hook"
run_hook "valid symlink" 0
assert_link_unchanged "valid symlink" "$VALID_BEFORE_TARGET" "$VALID_BEFORE_STATE"
VALID_AFTER_OUTPUT="$("$CASE_LINK")" || fail "valid symlink: original wrapper did not execute after hook"
[ "$VALID_AFTER_OUTPUT" = "ORIGINAL-WRAPPER" ] ||
  fail "valid symlink: occupied path executed '$VALID_AFTER_OUTPUT', want 'ORIGINAL-WRAPPER'"
printf 'PASS valid symlink (target, inode/state, and executable behavior preserved)\n'

case_paths "absent-create"
make_target "$CASE_PLUGIN" "CODEX-WRAPPER"
[ ! -e "$CASE_LINK" ] && [ ! -L "$CASE_LINK" ] || fail "absent create: fixture link unexpectedly occupied"
run_hook "absent create" 0
[ -L "$CASE_LINK" ] || fail "absent create: hook did not create a symlink"
CREATED_TARGET="$(readlink "$CASE_LINK")" || fail "absent create: cannot read created symlink"
[ "$CREATED_TARGET" = "$CASE_PLUGIN/bin/ateam" ] ||
  fail "absent create: readlink is '$CREATED_TARGET', want '$CASE_PLUGIN/bin/ateam'"
CREATED_STATE="$(lstat_snapshot "$CASE_LINK")" || fail "absent create: cannot lstat created symlink"
CREATED_OUTPUT="$("$CASE_LINK")" || fail "absent create: created symlink is not executable"
[ "$CREATED_OUTPUT" = "CODEX-WRAPPER" ] ||
  fail "absent create: created link printed '$CREATED_OUTPUT', want 'CODEX-WRAPPER'"
run_hook "idempotent second invocation" 0
assert_link_unchanged "idempotent second invocation" "$CREATED_TARGET" "$CREATED_STATE"
printf 'PASS absent create and idempotent second invocation\n'

case_paths "dangling-symlink"
make_target "$CASE_PLUGIN" "CODEX-WRAPPER"
assert_scratch_path "$(dirname "$CASE_LINK")"
mkdir -p "$(dirname "$CASE_LINK")"
DANGLING_TARGET="$CASE_ROOT/does-not-exist"
assert_scratch_path "$DANGLING_TARGET"
ln -s "$DANGLING_TARGET" "$CASE_LINK"
DANGLING_BEFORE_TARGET="$(readlink "$CASE_LINK")"
DANGLING_BEFORE_STATE="$(lstat_snapshot "$CASE_LINK")"
[ ! -e "$CASE_LINK" ] && [ -L "$CASE_LINK" ] || fail "dangling symlink: fixture is not dangling"
run_hook "dangling symlink" 0
assert_link_unchanged "dangling symlink" "$DANGLING_BEFORE_TARGET" "$DANGLING_BEFORE_STATE"
printf 'PASS dangling symlink (exact target and inode/state preserved)\n'

case_paths "self-referential-symlink"
make_target "$CASE_PLUGIN" "CODEX-WRAPPER"
assert_scratch_path "$(dirname "$CASE_LINK")"
mkdir -p "$(dirname "$CASE_LINK")"
ln -s "$CASE_LINK" "$CASE_LINK"
SELF_BEFORE_TARGET="$(readlink "$CASE_LINK")"
SELF_BEFORE_STATE="$(lstat_snapshot "$CASE_LINK")"
run_hook "self-referential symlink" 0
assert_link_unchanged "self-referential symlink" "$SELF_BEFORE_TARGET" "$SELF_BEFORE_STATE"
printf 'PASS self-referential symlink (exact target and inode/state preserved)\n'

case_paths "regular-file"
make_target "$CASE_PLUGIN" "CODEX-WRAPPER"
assert_scratch_path "$(dirname "$CASE_LINK")"
mkdir -p "$(dirname "$CASE_LINK")"
printf '%s\n' 'human-owned bytes' 'second line: !@#^&*()' >"$CASE_LINK"
REGULAR_COPY="$CASE_ROOT/regular.before"
assert_scratch_path "$REGULAR_COPY"
cp -f "$CASE_LINK" "$REGULAR_COPY"
REGULAR_BEFORE_STATE="$(lstat_snapshot "$CASE_LINK")"
run_hook "regular file" 0
[ -f "$CASE_LINK" ] && [ ! -L "$CASE_LINK" ] || fail "regular file: node type changed"
cmp -s "$REGULAR_COPY" "$CASE_LINK" || fail "regular file: bytes changed"
[ "$(lstat_snapshot "$CASE_LINK")" = "$REGULAR_BEFORE_STATE" ] || fail "regular file: inode/state changed"
printf 'PASS regular file (bytes and inode/state preserved)\n'

case_paths "directory"
make_target "$CASE_PLUGIN" "CODEX-WRAPPER"
assert_scratch_path "$CASE_LINK"
mkdir -p "$CASE_LINK"
DIRECTORY_BEFORE_STATE="$(lstat_snapshot "$CASE_LINK")"
run_hook "directory" 0
[ -d "$CASE_LINK" ] && [ ! -L "$CASE_LINK" ] || fail "directory: node type changed"
[ ! -e "$CASE_LINK/ateam" ] && [ ! -L "$CASE_LINK/ateam" ] ||
  fail "directory: hook created a nested ateam entry"
[ "$(lstat_snapshot "$CASE_LINK")" = "$DIRECTORY_BEFORE_STATE" ] || fail "directory: inode/state changed"
printf 'PASS directory (unchanged, with no nested ateam entry)\n'

# An occupied path is a successful no-op before wrapper validation. Using a
# FIFO also covers the contract's "other occupied node" class without deriving
# the expected result from the hook's regular-file and symlink branches.
case_paths "occupied-fifo-missing-target"
assert_scratch_path "$(dirname "$CASE_LINK")"
mkdir -p "$(dirname "$CASE_LINK")"
mkfifo "$CASE_LINK"
FIFO_BEFORE_STATE="$(lstat_snapshot "$CASE_LINK")"
[ -p "$CASE_LINK" ] || fail "occupied FIFO: fixture is not a FIFO"
[ ! -e "$CASE_PLUGIN/bin/ateam" ] || fail "occupied FIFO: wrapper fixture unexpectedly exists"
run_hook "occupied FIFO with missing target" 0
[ -p "$CASE_LINK" ] || fail "occupied FIFO: node type changed"
[ "$(lstat_snapshot "$CASE_LINK")" = "$FIFO_BEFORE_STATE" ] ||
  fail "occupied FIFO: inode/state changed"
printf 'PASS occupied FIFO with missing target (successful unchanged no-op)\n'

assert_wrapper_failure() {
  local label="$1" expected="$2"
  local stderr_file="$CASE_ROOT/hook.stderr"
  run_hook "$label" 1
  [ ! -e "$CASE_LINK" ] && [ ! -L "$CASE_LINK" ] || fail "$label: hook created the ateam path"
  [ ! -e "$CASE_HOME/.local" ] || fail "$label: hook created .local despite invalid wrapper"
  [ ! -s "$CASE_ROOT/hook.stdout" ] || fail "$label: hook wrote unexpected stdout"
  [ "$(cat "$stderr_file")" = "$expected" ] ||
    fail "$label: stderr was '$(cat "$stderr_file")', want '$expected'"
}

case_paths "missing-target"
MISSING_MESSAGE="agent-teams: bundled ateam wrapper is missing or not executable: $CASE_PLUGIN/bin/ateam"
assert_wrapper_failure "missing target" "$MISSING_MESSAGE"
printf 'PASS missing target (failure reported and nothing created)\n'

case_paths "non-executable-target"
NON_EXECUTABLE_TARGET="$CASE_PLUGIN/bin/ateam"
assert_scratch_path "$NON_EXECUTABLE_TARGET"
mkdir -p "$CASE_PLUGIN/bin"
printf '#!/bin/sh\nprintf "must not run\\n"\n' >"$NON_EXECUTABLE_TARGET"
chmod 0644 "$NON_EXECUTABLE_TARGET"
NON_EXECUTABLE_MESSAGE="agent-teams: bundled ateam wrapper is missing or not executable: $NON_EXECUTABLE_TARGET"
assert_wrapper_failure "non-executable target" "$NON_EXECUTABLE_MESSAGE"
printf 'PASS non-executable target (failure reported and nothing created)\n'

printf 'PASS all Codex ensure-ateam-link scratch regressions\n'
