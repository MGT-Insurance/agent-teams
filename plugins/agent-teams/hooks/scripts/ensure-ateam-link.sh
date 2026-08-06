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

# Capture stdin once non-blocking — Claude Code passes {session_id, ...} on stdin;
# direct invocations have no stdin.
HOOK_STDIN=$(cat 2>/dev/null || true)
HOOK_SESSION_ID=$(printf '%s' "$HOOK_STDIN" | jq -r '.session_id // "unknown"' 2>/dev/null || echo "unknown")
export HOOK_SESSION_ID

# shellcheck source=plugins/agent-teams/hooks/scripts/lib/hook-debug-log.sh
. "$(dirname "$0")/lib/hook-debug-log.sh"

# Log start BEFORE any guard check.
hook_log_start "ensure-ateam-link.sh"

# C3: link path — env override exists solely so the test can sandbox it. Not
# a user-facing feature; do not document it as one.
# D1 fix (agent-teams-wtf3.8), collapsed by agent-teams-wtf3.11: a bare
# $HOME under set -u aborted the whole hook (C7 violation) when HOME was
# unset. Previously handled as a two-part arrangement — compute LINK as an
# empty-string sentinel, then check for that sentinel further down. Collapsed
# to a single early exit: if neither the override nor HOME is set there is no
# usable directory, so exit "no-home" HERE, immediately — after
# hook_log_start (so the exit is still traced in the debug log; placing this
# any earlier would recreate the untraced-exit defect this hook exists to
# avoid) — rather than carrying an empty-LINK state for downstream code to
# keep checking.
# The bare $HOME below is safe precisely because this guard already ran: if
# the override is set, ${VAR:-...} short-circuits and never evaluates $HOME;
# if it's unset, the guard above has already proven HOME is non-empty. Do
# NOT change it back to ${HOME:-} — that would just reintroduce the
# empty-LINK state this change deletes.
if [ -z "${AGENT_TEAMS_ATEAM_LINK:-}" ] && [ -z "${HOME:-}" ]; then
  HOOK_EXIT_REASON="no-home"
  exit 0
fi
LINK="${AGENT_TEAMS_ATEAM_LINK:-$HOME/.local/bin/ateam}"

# C5: resolve a path through chained symlinks — same POSIX loop already used
# by plugins/agent-teams/bin/ateam:11-19. Do not shell out to readlink -f or
# realpath (not portable to older macOS). Canonicalizes the final directory
# via cd+pwd so relative and ".." segments collapse before comparison.
# D2 fix (agent-teams-wtf3.8): hop-bounded — a self-referential symlink (the
# state /setup-agent-teams/SKILL.md:149 warns ln -sf can create) previously
# looped forever here. Capping hops guarantees termination.
# agent-teams-wtf3.11 narrowed what happens next: hitting the cap no longer
# heals the link. It sets RESOLVE_UNRESOLVABLE so the caller can leave a
# cyclic/unresolvable chain completely untouched instead of falling through
# to the case (d)/(e) relink — a self-referential symlink is user error, not
# something this hook takes ownership of repairing. A DANGLING link (chain
# terminates normally because the final target simply doesn't exist) never
# sets this flag and still heals via (d) — dangling and cyclic are different
# conditions.
# Communicates via globals (RESOLVED_PATH, RESOLVE_UNRESOLVABLE) rather than
# a $(...) capture: command substitution runs in a subshell, so a variable
# set inside one is invisible to the caller once it returns.
resolve_chain() {
  local src="$1" target d b canon hops=0
  RESOLVE_UNRESOLVABLE=""
  while [ -h "$src" ]; do
    hops=$((hops + 1))
    if [ "$hops" -gt 40 ]; then
      RESOLVE_UNRESOLVABLE=1
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
    RESOLVED_PATH="$canon/$b"
  else
    RESOLVED_PATH="$src"
  fi
}

# agent-teams-wtf3.10: FORWARD-ONLY RELINK. A live session's hook re-fires on
# every compact/clear (SessionStart matcher includes both). After a plugin
# update, an old still-running session's hook and a new session's hook can
# both target ~/.local/bin/ateam, dragging the link back and forth for as
# long as both live. Before relinking onto a candidate target, compare the
# hook's OWN plugin version against the version the target install reports;
# decline only when the target is STRICTLY NEWER. Get either side wrong
# (unreadable/absent/malformed) and this must fall back to today's behavior
# (relink) — see get_version()/version_gt() below.

# get_version <plugin.json path> -> prints .version, or empty if the file is
# missing/unreadable/malformed. Tries jq if present, falls back to a grep/sed
# scrape so this never hard-depends on jq being installed.
get_version() {
  local f="$1" v=""
  if [ -f "$f" ]; then
    if command -v jq >/dev/null 2>&1; then
      v="$(jq -r '.version // empty' "$f" 2>/dev/null || true)"
    fi
    if [ -z "$v" ]; then
      v="$(grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' "$f" 2>/dev/null \
        | head -n1 | sed 's/.*"\([^"]*\)"[[:space:]]*$/\1/' || true)"
    fi
  fi
  printf '%s' "$v"
}

# version_gt <a> <b> -> true (0) iff <a> is a strictly greater X.Y.Z... version
# than <b>, comparing components numerically (0.51.10 > 0.51.9 — a string
# compare gets this wrong). Missing trailing components default to 0. ANY
# ambiguity — empty input, a non-numeric component on either side — returns
# false rather than erroring, so a malformed/unreadable version can never
# cause an incorrect decline; the caller's default action (relink) always
# wins on doubt.
version_gt() {
  local a="$1" b="$2"
  [ -n "$a" ] && [ -n "$b" ] || return 1
  local -a ap bp
  IFS=. read -r -a ap <<<"$a"
  IFS=. read -r -a bp <<<"$b"
  local len=${#ap[@]} i x y
  [ "${#bp[@]}" -gt "$len" ] && len=${#bp[@]}
  for ((i = 0; i < len; i++)); do
    x="${ap[i]:-0}"
    y="${bp[i]:-0}"
    case "$x" in '' | *[!0-9]*) return 1 ;; esac
    case "$y" in '' | *[!0-9]*) return 1 ;; esac
    if [ "$x" -gt "$y" ]; then return 0; fi
    if [ "$x" -lt "$y" ]; then return 1; fi
  done
  return 1 # equal
}

OWN_VERSION="$(get_version "$PLUGIN_ROOT/.claude-plugin/plugin.json")"

# C4 — the five original cases below, in this order (agent-teams-wtf3.11
# adds a sixth: the unresolvable-chain check just after resolving LINK,
# which must run before any of these — it is not itself lettered (a)-(e)
# since it can only fire when LINK is a symlink, ahead of case (b)).

# (a) WRAPPER not executable -> do nothing.
if [ ! -x "$WRAPPER" ]; then
  HOOK_EXIT_REASON="no-wrapper"
  exit 0
fi

# Resolve LINK's chain once (if it's a symlink) and reuse it for both the
# already-current check (b) and the forward-only version check in (d).
RESOLVED=""
if [ -L "$LINK" ]; then
  resolve_chain "$LINK"
  RESOLVED="$RESOLVED_PATH"
  # agent-teams-wtf3.11: an UNRESOLVABLE chain (hop cap exhausted — a cyclic
  # or self-referential symlink) is a distinct condition from a dangling one
  # (chain resolves fine, final target just doesn't exist). Only the former
  # is left completely untouched: exit here, before (b)/(c)/(d)/(e) ever
  # run, so nothing below can heal it. A dangling link never sets this flag
  # and still falls through to the (d) relink below.
  if [ -n "$RESOLVE_UNRESOLVABLE" ]; then
    HOOK_EXIT_REASON="unresolvable-link"
    exit 0
  fi
fi

# (b) LINK already resolves (through chained symlinks) to WRAPPER -> do
# nothing. This is the overwhelmingly common path.
if [ -n "$RESOLVED" ] && [ "$RESOLVED" = "$WRAPPER" ]; then
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
# relink — UNLESS the target is a strictly newer install than this hook's
# own (the forward-only rule above). A dangling target, or one whose version
# can't be read, has no comparable version and falls straight through to the
# relink it already got before this change. (An unresolvable/cyclic chain
# never reaches this case — it exits above, at C4's sixth case.)
if [ -L "$LINK" ]; then
  TARGET_VERSION=""
  if [ -e "$RESOLVED" ]; then
    TARGET_PLUGIN_ROOT="$(dirname "$(dirname "$RESOLVED")")"
    TARGET_VERSION="$(get_version "$TARGET_PLUGIN_ROOT/.claude-plugin/plugin.json")"
  fi
  if version_gt "$TARGET_VERSION" "$OWN_VERSION"; then
    HOOK_EXIT_REASON="target-newer"
    exit 0
  fi
  mkdir -p "$(dirname "$LINK")" 2>/dev/null || true
  ln -sf "$WRAPPER" "$LINK" 2>/dev/null || true
  HOOK_EXIT_REASON="relinked"
  exit 0
fi

# (e) LINK does not exist -> create it. No prior install to compare against.
mkdir -p "$(dirname "$LINK")" 2>/dev/null || true
ln -sf "$WRAPPER" "$LINK" 2>/dev/null || true
HOOK_EXIT_REASON="created"
exit 0
