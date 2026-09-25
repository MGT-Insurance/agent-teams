#!/usr/bin/env bash
# commit-attribution.sh — read-only commit/write attribution witness for the
# global ~/.agent-teams workspace.
#
# WHY: every commit-volume lever under agent-teams-8st0.5 needs one shared,
# repeatable before/after witness. The global workspace's Dolt history is
# flattened periodically (see agent-teams-8st0.4), so dolt_log alone cannot
# attribute writes older than the last flatten. The `events` table survives
# flattening and records every issue write (created/updated/closed/reopened,
# with the changed field in new_value), so it is the source for the issue-
# write breakdown; dolt_log is used only for its own commit-message classes,
# scoped to the window since the last flatten.
#
# This script is READ-ONLY: every query runs through `bd sql --readonly`,
# which rejects any write. It never scans dolt_diff_* tables — a full
# dolt_diff_issues scan timed out against the shared dolt sql-server
# (i/o timeout) — it reads only `events` and `dolt_log`.
#
# Usage:
#   scripts/commit-attribution.sh [START_DATE] [END_DATE]
#     START_DATE, END_DATE  YYYY-MM-DD, half-open [START_DATE, END_DATE).
#     Default: the last 3 full UTC days ending today.
#
# Prints three sections:
#   1. Issue writes from `events` by class (created/closed/reopened/updated),
#      with `updated` split into notes/description/other, and notes further
#      split by the first word of the appended paragraph.
#   2. dolt_log commit messages grouped by first two words, since the last
#      flatten commit.
#   3. Top 10 issues by write count in the window, with reopen/close counts
#      (ping-pong detector).
#
# Exit status: 0 on success, 1 if `bd` is missing or a query fails.

set -euo pipefail

WORKSPACE="${AGENT_TEAMS_WORKSPACE:-$HOME/.agent-teams}"

if ! command -v bd >/dev/null 2>&1; then
  echo "commit-attribution: 'bd' not found in PATH" >&2
  exit 1
fi

# Default window: last 3 full UTC days ending today, half-open [START, END).
# BSD date (macOS) uses -v-3d; GNU date uses -d '3 days ago'.
if date -v-3d >/dev/null 2>&1; then
  DEFAULT_END="$(date -u +%Y-%m-%d)"
  DEFAULT_START="$(date -u -v-3d +%Y-%m-%d)"
else
  DEFAULT_END="$(date -u +%Y-%m-%d)"
  DEFAULT_START="$(date -u -d '3 days ago' +%Y-%m-%d)"
fi

START="${1:-$DEFAULT_START}"
END="${2:-$DEFAULT_END}"

sql() {
  bd -C "$WORKSPACE" sql --readonly "$1"
}

echo "commit-attribution: workspace=$WORKSPACE window=[$START, $END)"
echo

echo "== 1. issue writes from events, by class =="
sql "select event_type, count(*) as n from events \
where created_at >= '$START' and created_at < '$END' \
group by event_type order by n desc"
echo
echo "-- updated events, split by changed field --"
sql "select case \
    when new_value like '{\"notes\"%' then 'notes' \
    when new_value like '{\"description\"%' then 'description' \
    else 'other' end as kind, count(*) as n \
  from events where event_type='updated' \
  and created_at >= '$START' and created_at < '$END' \
  group by kind order by n desc"
echo
echo "-- notes events, split by first word of the appended paragraph --"
sql "select case \
    when p like 'reaped:%' then 'reaped:' \
    when p like 'review-posted:%' then 'review-posted:' \
    when p like 'auto-closed%' then 'auto-closed' \
    when p like '<<<ateam-ask%' then '<<<ateam-ask' \
    when p like 'session%' then 'session' \
    else 'other' end as kind, count(*) as n \
  from (select trim(trailing '\n' from substring_index( \
      trim(trailing '\n' from json_unquote(json_extract(new_value,'\$.notes'))), \
      '\n\n', -1)) p \
    from events where event_type='updated' and new_value like '{\"notes\"%' \
    and created_at >= '$START' and created_at < '$END') paragraphs \
  group by kind order by n desc"
echo

echo "== 2. dolt_log commit messages by first two words, since last flatten =="
sql "select substring_index(message, ' ', 2) as cls, count(*) as n from dolt_log \
where date > (select date from dolt_log where message like 'flatten:%' \
    order by date desc limit 1) \
group by cls order by n desc"
echo

echo "== 3. top 10 issues by write count in window (ping-pong detector) =="
sql "select issue_id, count(*) as writes, \
    sum(event_type='reopened') as reopens, sum(event_type='closed') as closes \
  from events where created_at >= '$START' and created_at < '$END' \
  group by issue_id order by writes desc limit 10"
