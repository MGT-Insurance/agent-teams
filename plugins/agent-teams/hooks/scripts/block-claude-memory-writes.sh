#!/usr/bin/env bash
# PreToolUse hook: block writes to Claude memory locations.
# Fires on Write, Edit, and MultiEdit tools. Denies writes to:
#   - any path under $HOME/.claude/projects/*/memory/**
#   - any MEMORY.md under $HOME/.claude/**
# Allows everything else — including repo files named memory.md or source
# dirs named memory/ that are outside $HOME/.claude.
#
# NOTE: CLAUDE_CONFIG_DIR is not honored here — only $HOME/.claude is matched.
# If that env var points elsewhere, false negatives are possible. Scoped to
# $HOME/.claude to avoid over-engineering; revisit if CLAUDE_CONFIG_DIR
# adoption widens.
set -euo pipefail

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

# Matched — emit a deny decision and exit 0.
# Canonical hook denial message (verbatim from agent-teams-8qm):
DENIAL_MSG="BLOCKED: agent-teams routes persistent memory to ateam, not to Claude memory files. Do NOT write MEMORY.md or files under a Claude memory/ dir. Instead: role/process learning -> \`ateam learn <role> <slug> --file <tmpfile>\` (role = dri|planner|implementer|tester|reviewer); user/cross-project preference -> \`ateam learn user <slug> --file <tmpfile>\`; repo-shared project fact -> \`bd remember\`. (If you genuinely intended a normal repo file that is not agent memory, this matcher only fires on ~/.claude memory paths — re-check your target path.)"

printf '%s' "$payload" | jq -n \
  --arg msg "$DENIAL_MSG" \
  '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":$msg}}'

exit 0
