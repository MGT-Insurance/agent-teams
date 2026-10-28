#!/bin/bash
# Laptop half of ring 1. Isolated from the live setup: it only writes under
# ~/.agent-teams-ring1 (the ring-1 AGENT_TEAMS_HOME) and ~/.agent-teams-ring1-work (a scratch
# clone of the agent-teams repo). It never touches ~/.agent-teams, the main checkout, or the
# installed plugin; claude loads the worktree plugin through --plugin-dir.
#
#   bash laptop-ring1.sh prepare     create both hosted workspaces (F1, F11)
#   bash laptop-ring1.sh env         print the exports for this half (eval "$(... env)")
#   bash laptop-ring1.sh claude ...  launch claude with the ring-1 env and the worktree plugin
#   bash laptop-ring1.sh status      bd dolt show plus a data-path read for both workspaces
#   bash laptop-ring1.sh teardown    remove ~/.agent-teams-ring1 and the scratch clone
#
# Uses the same scoped login as the sandbox, eric_lloyd_ext. `prepare` builds a mode-600 credentials
# file ($RING_HOME/ext.credentials) from ~/.config/beads/ring1-ext.json (override with
# RING1_EXT_JSON) and every command exports BEADS_CREDENTIALS_FILE pointing at it. This script never
# reads or writes ~/.config/beads/credentials. Needs jq.
set -eu
HERE=$(cd "$(dirname "$0")" && pwd)
WT=${WORKTREE:-$(cd "$HERE/../.." && pwd)}
RING_HOME=${RING_HOME:-$HOME/.agent-teams-ring1}
RING_WORK=${RING_WORK:-$HOME/.agent-teams-ring1-work}
CLONE=$RING_WORK/agent-teams
export HOSTED_HOST=${HOSTED_HOST:-mgt-spike.dbs.hosted.doltdb.com}
EXT_CREDS=$RING_HOME/ext.credentials
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
  # Keeps the ensure-ateam-link hook off the machine-wide ~/.local/bin/ateam.
  export AGENT_TEAMS_ATEAM_LINK=$RING_HOME/bin/ateam
  export BEADS_DOLT_SERVER_TLS=1
  export BEADS_CREDENTIALS_FILE=$EXT_CREDS
  export CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1
  export PATH=$WT/plugins/agent-teams/bin:$PATH
}

case "${1:-}" in
  prepare)
    . "$HERE/lib-ext.sh"
    . "$HERE/lib-hosted.sh"
    ext_validate || exit 1
    export DB_USER=$EXT_USER_NAME
    ring_env
    mkdir -p "$RING_HOME" "$RING_WORK"
    [ -d "$RING_HOME/.git" ] || git -C "$RING_HOME" init -q
    ext_write_credentials "$EXT_CREDS"
    # The credentials file sits inside a git repo; keep it out of any commit.
    mkdir -p "$RING_HOME/.git/info"
    grep -qx 'ext.credentials' "$RING_HOME/.git/info/exclude" 2>/dev/null || echo 'ext.credentials' >> "$RING_HOME/.git/info/exclude"
    hosted_workspace "$RING_HOME" "$GLOBAL_DB" "$GLOBAL_PREFIX"
    [ -d "$CLONE/.git" ] || git clone -q "$WT" "$CLONE"
    hosted_workspace "$CLONE" "$PROJECT_DB" "$PROJECT_PREFIX"
    git -C "$CLONE" update-index --skip-worktree .beads/metadata.json .beads/config.yaml
    echo "prepared: AGENT_TEAMS_HOME=$RING_HOME clone=$CLONE host=$HOST_ID"
    ;;
  env)
    ring_env
    printf 'export AGENT_TEAMS_HOME=%s\nexport AGENT_TEAMS_HOST=%s\nexport AGENT_TEAMS_ATEAM_LINK=%s\nexport BEADS_DOLT_SERVER_TLS=1\nexport BEADS_CREDENTIALS_FILE=%s\nexport CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1\nexport PATH=%s\n' \
      "$RING_HOME" "$HOST_ID" "$AGENT_TEAMS_ATEAM_LINK" "$EXT_CREDS" "$PATH"
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
    # dolt show connects without TLS on bd 1.3.0, so it cannot prove F2. A real read can.
    for ws in "$RING_HOME" "$CLONE"; do
      if bd -C "$ws" list --limit 1 --json >/dev/null; then
        echo "data path OK: $ws"
      else
        echo "data path FAILED: $ws (bd list errored; check BEADS_DOLT_SERVER_TLS and the credentials)" >&2
        exit 1
      fi
    done
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
