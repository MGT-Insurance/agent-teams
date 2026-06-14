#!/usr/bin/env bash
# Tests for the block-claude-memory-writes PreToolUse hook script.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/block-claude-memory-writes.sh"

# Override HOME so we don't touch the real ~/.claude.
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
export HOME="$T"

make_payload() {
  local tool="$1" path="$2"
  printf '{"tool_name":"%s","tool_input":{"file_path":"%s"}}' "$tool" "$path"
}

# Helper: run script and return its stdout.
run() { printf '%s' "$1" | "$SCRIPT"; }

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

echo "PASS"
