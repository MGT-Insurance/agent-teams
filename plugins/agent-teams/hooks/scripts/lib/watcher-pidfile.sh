#!/usr/bin/env bash
# Sourced helper — shared watcher-pidfile parsing for agent-teams hook
# scripts (agent-teams-e3mq.30).
#
# The watcher pidfile ($MAILBOX/<id>.watcher.pid) holds "pid<TAB>session_id"
# — session_id is the HOOK_SESSION_ID of the hook invocation that claimed the
# watcher slot, captured from stdin. wake-watcher.sh's claim path and
# inbox-drain.sh's disarm path both need to parse this format identically so
# the two sides of the singleton contract (agent-teams-e3mq.29 claim,
# agent-teams-e3mq.30 disarm) can't drift out of sync again — this file is
# the single place that parsing lives.
#
# Old-format compat: a pidfile with no tab (pid only) predates the
# pid<TAB>session_id format. pidfile_pid still returns the pid; pidfile_session
# returns "" for it, since an old-format entry can't be attributed to any
# session.
#
# Public API:
#   pidfile_pid <entry>
#     Prints the pid portion of a pidfile entry (text before the first tab,
#     or the whole entry when there's no tab).
#   pidfile_session <entry>
#     Prints the session_id portion (text after the first tab), or ""
#     when the entry has no tab (old-format).
pidfile_pid() {
  printf '%s' "${1%%$'\t'*}"
}
pidfile_session() {
  case "$1" in
    *$'\t'*) printf '%s' "${1#*$'\t'}" ;;
    *) printf '%s' "" ;;
  esac
}
