#!/usr/bin/env bash
# Tests for the block-claude-memory-writes PreToolUse hook script.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/block-claude-memory-writes.sh"

# Override HOME so we don't touch the real ~/.claude.
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
export HOME="$T"

# An in-scope stub workspace: a dir with a .beads/ child, used as the DEFAULT
# cwd for every case. Without this the payload carries no cwd, the script falls
# back to $PWD, and the deny cases only pass when the suite happens to be run
# from inside a repo that has .beads — i.e. green here, red from /tmp.
IN_SCOPE_CWD="$T/inscope"
mkdir -p "$IN_SCOPE_CWD/.beads"

# Optional 3rd arg overrides the payload's cwd; defaults to the in-scope stub.
make_payload() {
  local tool="$1" path="$2" cwd="${3:-$IN_SCOPE_CWD}"
  printf '{"tool_name":"%s","tool_input":{"file_path":"%s"},"cwd":"%s"}' \
    "$tool" "$path" "$cwd"
}

# Helper: run script and return its stdout. BEADS_DIR is scrubbed so the scope
# gate is exercised via cwd alone — an ambient BEADS_DIR would force every case
# in scope and mask the allow cases below.
run() { printf '%s' "$1" | env -u BEADS_DIR "$SCRIPT"; }

# Helper: assert deny — output must contain "deny" and the canonical message prefix.
assert_deny() {
  local label="$1" payload="$2"
  out=$(run "$payload")
  echo "$out" | grep -q '"deny"' \
    || { echo "FAIL $label: expected deny, got: $out"; exit 1; }
  echo "$out" | grep -q 'BLOCKED: agent-teams routes' \
    || { echo "FAIL $label: denial message missing canonical prefix, got: $out"; exit 1; }
}

# Helper: assert allow — output must be empty (silent pass-through).
assert_allow() {
  local label="$1" payload="$2"
  out=$(run "$payload")
  [ -z "$out" ] \
    || { echo "FAIL $label: expected silent allow, got: $out"; exit 1; }
}

# Case 1: Write to MEMORY.md under ~/.claude/projects/x/memory/ -> deny
assert_deny "case1-write-memory-dir" \
  "$(make_payload Write "$T/.claude/projects/my-proj/memory/MEMORY.md")"

# Case 2: Write to a non-MEMORY.md file under ~/.claude/projects/x/memory/ -> deny
assert_deny "case2-write-memory-subfile" \
  "$(make_payload Write "$T/.claude/projects/my-proj/memory/foo.md")"

# Case 3: Edit to a path under memory/ -> deny
assert_deny "case3-edit-memory-dir" \
  "$(make_payload Edit "$T/.claude/projects/other-proj/memory/bar.txt")"

# Case 4: Write to MEMORY.md directly under ~/.claude (no /projects/ prefix) -> deny
assert_deny "case4-write-memory-md-root" \
  "$(make_payload Write "$T/.claude/MEMORY.md")"

# Case 5: Write to MEMORY.md in a nested dir under ~/.claude -> deny
assert_deny "case5-write-memory-md-nested" \
  "$(make_payload Write "$T/.claude/some/nested/MEMORY.md")"

# Case 6: Normal repo file — path outside ~/.claude -> allow
assert_allow "case6-allow-repo-file" \
  "$(make_payload Write "/Users/x/code/proj/src/memory/util.ts")"

# Case 7: Repo file literally named memory.md outside ~/.claude -> allow
assert_allow "case7-allow-repo-memory-md" \
  "$(make_payload Write "/Users/x/code/proj/docs/memory.md")"

# Case 8: Write to /tmp -> allow
assert_allow "case8-allow-tmp" \
  "$(make_payload Write "/tmp/some-output.txt")"

# Case 9: Non-Write/Edit tool (Read) -> allow regardless of path
assert_allow "case9-allow-non-write-tool" \
  "$(make_payload Read "$T/.claude/projects/x/memory/anything.md")"

# Case 10: MultiEdit to memory path -> deny
assert_deny "case10-multiEdit-memory" \
  "$(make_payload MultiEdit "$T/.claude/projects/y/memory/z.md")"

# Case 11: Tilde expansion — Write to ~/... memory path -> deny
assert_deny "case11-tilde-memory-path" \
  "$(make_payload Write "~/.claude/projects/proj/memory/note.md")"

# Case 12: Write to ~/.claude/projects/ itself (no memory segment) -> allow
assert_allow "case12-allow-projects-root" \
  "$(make_payload Write "$T/.claude/projects/my-proj/some-other-file.md")"

# ---- Scope gate ------------------------------------------------------------
# Enforcement is limited to agent-teams contexts. Outside one, Claude's native
# memory is the right store, so the hook must allow the write.

# Case 13: memory path, but cwd has no .beads at it or any parent -> allow.
# $T/outside walks $T/outside -> $T -> /, none of which has a .beads dir.
mkdir -p "$T/outside"
assert_allow "case13-allow-no-beads-workspace" \
  "$(make_payload Write "$T/.claude/projects/p/memory/note.md" "$T/outside")"

# Case 14: cwd is a DRI worktree -> deny, even though the worktree has NO
# literal .beads dir (worktrees reach beads via git-common-dir, and the parent
# walk $T/.agent-teams-worktrees/demo -> $T/.agent-teams-worktrees -> $T finds
# nothing). Regression guard: the three scope signals must stay OR'd, so
# collapsing them into a single AND on the .beads walk would break every
# background DRI's memory routing.
mkdir -p "$T/.agent-teams-worktrees/demo"
assert_deny "case14-deny-dri-worktree-without-literal-beads" \
  "$(make_payload Write "$T/.claude/projects/p/memory/note.md" \
     "$T/.agent-teams-worktrees/demo")"

echo "PASS"
