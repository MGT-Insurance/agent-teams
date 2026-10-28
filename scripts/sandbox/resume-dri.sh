#!/bin/bash
# Runs INSIDE the sandbox after `sb.mjs resume` (which also re-extends the timeout; a resume
# resets it to 2 h). Re-attaches the saved DRI session under tmux.
#   bash ~/resume-dri.sh
# Teammate processes do not survive a stop (ring-0 finding): tell the lead to respawn them.
set -eu
SID=$(cat "$HOME/.dri-session-id")
tmux kill-server 2>/dev/null || true
tmux new-session -d -s lead -x 200 -y 50 "bash \$HOME/run-claude.sh --resume $SID"
sleep 6
tmux capture-pane -p -t lead | grep -v '^$' | tail -15
echo "resumed $SID"
