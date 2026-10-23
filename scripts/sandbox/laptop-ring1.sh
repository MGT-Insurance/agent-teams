#!/bin/bash
# Laptop half of ring 1. Isolated from the live setup: it only writes under
# ~/.agent-teams-ring1 (the ring-1 AGENT_TEAMS_HOME) and ~/.agent-teams-ring1-work (a scratch
# clone of the agent-teams repo). It never touches ~/.agent-teams, the main checkout, or the
# installed plugin; claude loads the worktree plugin through --plugin-dir.
#
#   bash laptop-ring1.sh prepare     create both hosted workspaces (F1, F11)
#   bash laptop-ring1.sh env         print the exports for this half (eval "$(... env)")
#   bash laptop-ring1.sh claude ...  launch claude with the ring-1 env and the worktree plugin
#   bash laptop-ring1.sh status      bd dolt show for both workspaces
#   bash laptop-ring1.sh teardown    remove ~/.agent-teams-ring1 and the scratch clone
#
# Needs ~/.config/beads/credentials to hold a [mgt-spike.dbs.hosted.doltdb.com:3306] section whose
# password belongs to $DB_USER (default eric_lloyd, the existing laptop login). This script never reads that file.
set -eu
HERE=$(cd "$(dirname "$0")" && pwd)
WT=${WORKTREE:-$(cd "$HERE/../.." && pwd)}
RING_HOME=${RING_HOME:-$HOME/.agent-teams-ring1}
RING_WORK=${RING_WORK:-$HOME/.agent-teams-ring1-work}
CLONE=$RING_WORK/agent-teams
export HOSTED_HOST=${HOSTED_HOST:-mgt-spike.dbs.hosted.doltdb.com}
export DB_USER=${DB_USER:-eric_lloyd}
GLOBAL_DB=${GLOBAL_DB:-beads_eric_lloyd_at}
GLOBAL_PREFIX=${GLOBAL_PREFIX:-at}
PROJECT_DB=${PROJECT_DB:-beads_eric_lloyd_agent_teams}
PROJECT_PREFIX=${PROJECT_PREFIX:-agent-teams}
HOST_ID=${AGENT_TEAMS_HOST:-laptop-ring1}

# Refuse any path that could be the live workspace.
for p in "$RING_HOME" "$RING_WORK"; do
  case "$p" in
    "$HOME"|"/"|"$HOME/.agent-teams"|"$HOME/.agent-teams/"*|"$WT"|"$WT/"*)
      echo "refusing: $p is the live workspace or the worktree" >&2; exit 1 ;;
  esac
done

ring_env() {
  export AGENT_TEAMS_HOME=$RING_HOME
  export AGENT_TEAMS_HOST=$HOST_ID
  export BEADS_DOLT_SERVER_TLS=1
  export CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1
  export PATH=$WT/plugins/agent-teams/bin:$PATH
}

case "${1:-}" in
  prepare)
    . "$HERE/lib-hosted.sh"
    ring_env
    mkdir -p "$RING_HOME" "$RING_WORK"
    [ -d "$RING_HOME/.git" ] || git -C "$RING_HOME" init -q
    hosted_workspace "$RING_HOME" "$GLOBAL_DB" "$GLOBAL_PREFIX"
    [ -d "$CLONE/.git" ] || git clone -q "$WT" "$CLONE"
    hosted_workspace "$CLONE" "$PROJECT_DB" "$PROJECT_PREFIX"
    git -C "$CLONE" update-index --skip-worktree .beads/metadata.json .beads/config.yaml
    echo "prepared: AGENT_TEAMS_HOME=$RING_HOME clone=$CLONE host=$HOST_ID"
    ;;
  env)
    ring_env
    printf 'export AGENT_TEAMS_HOME=%s\nexport AGENT_TEAMS_HOST=%s\nexport BEADS_DOLT_SERVER_TLS=1\nexport CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1\nexport PATH=%s\n' \
      "$RING_HOME" "$HOST_ID" "$PATH"
    ;;
  claude)
    shift
    ring_env
    cd "$CLONE"
    exec claude --plugin-dir "$WT/plugins/agent-teams" "$@"
    ;;
  status)
    ring_env
    bd -C "$RING_HOME" dolt show
    bd -C "$CLONE" dolt show
    ;;
  teardown)
    rm -rf "$RING_HOME" "$RING_WORK"
    echo "removed $RING_HOME and $RING_WORK"
    ;;
  *)
    echo "usage: laptop-ring1.sh prepare|env|claude [args]|status|teardown" >&2
    exit 2
    ;;
esac
