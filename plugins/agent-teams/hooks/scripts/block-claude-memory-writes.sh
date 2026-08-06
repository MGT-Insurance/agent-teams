#!/usr/bin/env bash
# PreToolUse hook: block writes to Claude memory locations.
# Fires on Write, Edit, and MultiEdit tools. Denies writes to:
#   - any path under $HOME/.claude/projects/*/memory/**
#   - any MEMORY.md under $HOME/.claude/**
# Allows everything else — including repo files named memory.md or source
# dirs named memory/ that are outside $HOME/.claude.
#
# SCOPE GATE: enforcement is limited to agent-teams contexts — a .beads/
# workspace at cwd or any parent, BEADS_DIR set, or a DRI worktree under
# ~/.agent-teams-worktrees. In any other repo the hook allows the write,
# so Claude's native memory keeps working where ateam/bd don't exist.
#
# NOTE: CLAUDE_CONFIG_DIR is not honored here — only $HOME/.claude is matched.
# If that env var points elsewhere, false negatives are possible. Scoped to
# $HOME/.claude to avoid over-engineering; revisit if CLAUDE_CONFIG_DIR
# adoption widens.
set -euo pipefail
HOME="${HOME:-}"

command -v jq >/dev/null 2>&1 || exit 0

# Read the PreToolUse hook payload from stdin.
payload=$(cat)

tool_name=$(printf '%s' "$payload" | jq -r '.tool_name // empty' 2>/dev/null || true)

# Only intercept Write, Edit, and MultiEdit.
case "$tool_name" in
  Write|Edit|MultiEdit) ;;
  *) exit 0 ;;
esac

file_path=$(printf '%s' "$payload" | jq -r '.tool_input.file_path // empty' 2>/dev/null || true)
[ -n "$file_path" ] || exit 0

# Expand a leading ~ to $HOME.
# Use \~/ in the strip pattern so ~ is treated as a literal character,
# not as a tilde-expansion trigger.
case "$file_path" in
  "~/"*) file_path="${HOME}/${file_path#\~/}" ;;
  "~")   file_path="$HOME" ;;
esac

claude_dir="${HOME}/.claude"

# MATCHER 1: any path under $HOME/.claude/projects/*/memory/**
# Pattern: starts with $claude_dir/projects/ followed by any segment, then /memory/
case "$file_path" in
  "${claude_dir}/projects/"*/memory/*)
    # Verify there is an actual project segment between /projects/ and /memory/
    # (i.e. not $claude_dir/projects/memory/ itself, which is not a valid pattern)
    rest="${file_path#"${claude_dir}/projects/"}"
    case "$rest" in
      */memory/*|*/memory) : ;;  # has a project segment then /memory
      *) exit 0 ;;
    esac
    ;;
  # MATCHER 2: any MEMORY.md anywhere under $HOME/.claude/
  "${claude_dir}/"*"/MEMORY.md"|"${claude_dir}/MEMORY.md")
    : ;;
  *)
    exit 0 ;;
esac

# ---- Scope gate (2026-07-13): only enforce where agent-teams is actually in
# play. The plugin is enabled machine-wide, but ateam/bd routing only exists in
# repos with a beads workspace. Signals (any one = enforce):
#   - the session cwd is inside ~/.agent-teams-worktrees (DRI worktrees)
#   - BEADS_DIR is set
#   - a .beads/ directory exists at cwd or any parent (mirrors bd's own walk)
# No signal -> allow the write: Claude's native memory is the right store there.
hook_cwd=$(printf '%s' "$payload" | jq -r '.cwd // empty' 2>/dev/null || true)
[ -n "$hook_cwd" ] || hook_cwd="$PWD"

in_scope=""
case "$hook_cwd" in
  "${HOME}/.agent-teams-worktrees"/*|"${HOME}/.agent-teams-worktrees") in_scope=1 ;;
esac
if [ -z "$in_scope" ] && [ -n "${BEADS_DIR:-}" ]; then in_scope=1; fi
if [ -z "$in_scope" ]; then
  d="$hook_cwd"
  while [ -n "$d" ] && [ "$d" != "/" ]; do
    if [ -d "$d/.beads" ]; then in_scope=1; break; fi
    d=$(dirname "$d")
  done
fi
[ -n "$in_scope" ] || exit 0

# Matched — emit a deny decision and exit 0.
# Canonical hook denial message (verbatim from agent-teams-8qm):
DENIAL_MSG="BLOCKED: agent-teams routes persistent memory to ateam, not to Claude memory files. Do NOT write MEMORY.md or files under a Claude memory/ dir. Instead: role/process learning -> \`ateam learn <role> <slug> --file <tmpfile>\` (role = dri|planner|implementer|tester|reviewer|investigator); user/cross-project preference -> \`ateam learn user <slug> --file <tmpfile>\`; repo-shared project fact -> \`bd remember\`. (If you genuinely intended a normal repo file that is not agent memory, this matcher only fires on ~/.claude memory paths — re-check your target path.)"

printf '%s' "$payload" | jq -n \
  --arg msg "$DENIAL_MSG" \
  '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":$msg}}'

exit 0
