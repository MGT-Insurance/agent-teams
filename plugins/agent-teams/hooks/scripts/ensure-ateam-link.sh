#!/usr/bin/env bash
# SessionStart hook for agent-teams.
# Self-heals ~/.local/bin/ateam so it always points at the ateam wrapper
# belonging to the plugin installation THIS session actually loaded. A
# marketplace `claude plugin update` rewrites installed_plugins.json but
# never re-points a pre-existing symlink, so without this the link keeps
# shadowing PATH with a stale, version-pinned ateam.
# See agent-teams-wtf3.1 for the frozen contract this implements.
set -euo pipefail

ATH="${AGENT_TEAMS_HOME:-${HOME:-}/.agent-teams}"

# C2: target derivation. CLAUDE_PLUGIN_ROOT is the ONLY sanctioned source of
# truth for "which install is current" — do not read
# ~/.claude/plugins/installed_plugins.json, it resolves to the wrong copy for
# a directory-source marketplace. Fall back to self-location only when
# CLAUDE_PLUGIN_ROOT is unset (e.g. direct invocation while testing).
PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT:-}"
if [ -z "$PLUGIN_ROOT" ]; then
  PLUGIN_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fi
WRAPPER="$PLUGIN_ROOT/bin/ateam"

# C3: link path — env override exists solely so the test can sandbox it. Not
# a user-facing feature; do not document it as one.
# D1 fix (agent-teams-wtf3.8): a bare $HOME under set -u aborts the whole
# hook (C7 violation) when HOME is unset — measured-reachable. Use ${HOME:-}
# so an unset HOME degrades LINK to empty rather than crashing; empty is
# handled below as "no usable directory", never as "/.local/bin/ateam".
if [ -n "${AGENT_TEAMS_ATEAM_LINK:-}" ]; then
  LINK="$AGENT_TEAMS_ATEAM_LINK"
elif [ -n "${HOME:-}" ]; then
  LINK="$HOME/.local/bin/ateam"
else
  LINK=""
fi

# Capture stdin once non-blocking — Claude Code passes {session_id, ...} on stdin;
# direct invocations have no stdin.
HOOK_STDIN=$(cat 2>/dev/null || true)
HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
export HOOK_SESSION_ID

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$(dirname "$0")/lib/hook-debug-log.sh"

# Log start BEFORE any guard check.
hook_log_start "ensure-ateam-link.sh"

# C5: resolve a path through chained symlinks — same POSIX loop already used
# by plugins/agent-teams/bin/ateam:11-19. Do not shell out to readlink -f or
# realpath (not portable to older macOS). Canonicalizes the final directory
# via cd+pwd so relative and ".." segments collapse before comparison.
# D2 fix (agent-teams-wtf3.8): hop-bounded — a self-referential symlink (the
# state /setup-agent-teams/SKILL.md:149 warns ln -sf can create) previously
# looped forever here. Capping hops makes an unresolvable/cyclic chain
# terminate returning $src unresolved (its own path, not WRAPPER), so the
# case (b) comparison below fails and the case (d)/(e) relink heals it
# instead of hanging or failing the session.
resolve_chain() {
  local src="$1" target d b canon hops=0
  while [ -h "$src" ]; do
    hops=$((hops + 1))
    if [ "$hops" -gt 40 ]; then
      break
    fi
    target="$(readlink "$src")"
    case "$target" in
      /*) src="$target" ;;
      *) src="$(dirname "$src")/$target" ;;
    esac
  done
  d="$(dirname "$src")"
  b="$(basename "$src")"
  if canon="$(cd "$d" 2>/dev/null && pwd)"; then
    printf '%s/%s\n' "$canon" "$b"
  else
    printf '%s\n' "$src"
  fi
}

# D1 fix: no usable LINK path (HOME unset and no override given) -> nothing
# safe to act on. Never write under an empty HOME (e.g. "/.local/bin/ateam",
# a root-owned path unrelated to any user).
if [ -z "$LINK" ]; then
  HOOK_EXIT_REASON="no-home"
  exit 0
fi

# C4 — exactly these five cases, in this order.

# (a) WRAPPER not executable -> do nothing.
if [ ! -x "$WRAPPER" ]; then
  HOOK_EXIT_REASON="no-wrapper"
  exit 0
fi

# (b) LINK already resolves (through chained symlinks) to WRAPPER -> do
# nothing. This is the overwhelmingly common path.
if [ -L "$LINK" ] && [ "$(resolve_chain "$LINK")" = "$WRAPPER" ]; then
  HOOK_EXIT_REASON="already-current"
  exit 0
fi

# (c) LINK exists and is NOT a symlink -> do nothing. NEVER clobber
# something the human placed there by hand. Deliberately [ -e ] && [ ! -L ]:
# a directory is -f FALSE but -x TRUE, so both of the obvious guards get
# this wrong in opposite directions.
if [ -e "$LINK" ] && [ ! -L "$LINK" ]; then
  HOOK_EXIT_REASON="not-a-symlink"
  exit 0
fi

# (d) LINK is a symlink pointing elsewhere, including a dangling one ->
# relink.
if [ -L "$LINK" ]; then
  mkdir -p "$(dirname "$LINK")" 2>/dev/null || true
  ln -sf "$WRAPPER" "$LINK" 2>/dev/null || true
  HOOK_EXIT_REASON="relinked"
  exit 0
fi

# (e) LINK does not exist -> create it.
mkdir -p "$(dirname "$LINK")" 2>/dev/null || true
ln -sf "$WRAPPER" "$LINK" 2>/dev/null || true
HOOK_EXIT_REASON="created"
exit 0
