#!/usr/bin/env bash
# Sourced helper — shared Steward-session detection for agent-teams hook
# scripts (agent-teams-e3mq.27).
#
# The Steward is a machine-scoped background session, not tied to any
# closeable initiative bead. wake-watcher.sh, inbox-drain.sh, and
# session-start-inbox.sh each need to recognize "this session's cwd IS the
# Steward's own session" before falling back to the normal worktree->
# initiative lookup — this file is the single place that detection lives, so
# the three call sites can't drift out of sync again.
#
# Identified by the shared contract's marker file path (mirrors
# internal/verbs/steward_seams.go's StewardSessionMarkerPath: keep this
# literal in lockstep with that contract) existing under $PWD's tree.
#
# Public API:
#   is_steward_cwd
#     Returns 0 (true) when $PWD is at or under the Steward session marker's
#     directory, 1 (false) otherwise. Requires ATH to already be set by the
#     caller (steward marker lives at "$ATH/steward/session/.steward-session").
is_steward_cwd() {
  local marker="${ATH}/steward/session/.steward-session"
  [ -f "$marker" ] || return 1
  local marker_dir
  marker_dir=$(dirname "$marker")
  case "$PWD" in
    "$marker_dir"|"$marker_dir"/*)
      return 0
      ;;
  esac
  return 1
}
