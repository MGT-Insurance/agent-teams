#!/bin/bash
# Runs INSIDE the sandbox. Starts claude under tmux with `/dri <problem>` in the project clone.
#   bash ~/start-dri.sh '<problem statement>'
# The session id is saved to ~/.dri-session-id for resume-dri.sh. Attach with: tmux attach -t lead
set -eu
PROBLEM=${1:?usage: start-dri.sh '<problem statement>'}
SID=$(cat /proc/sys/kernel/random/uuid)
printf '%s' "$SID" > "$HOME/.dri-session-id"
printf '%s' "$PROBLEM" > "$HOME/.dri-problem.txt"
tmux kill-server 2>/dev/null || true
tmux new-session -d -s lead -x 200 -y 50 "bash \$HOME/run-claude.sh --session-id $SID \"/dri \$(cat \$HOME/.dri-problem.txt)\""
sleep 8
tmux capture-pane -p -t lead | tail -25
echo "session $SID"
