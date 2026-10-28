#!/bin/sh
# Laptop: pack the plugin directory of a worktree into a tarball for install.sh.
#   sh pack-plugin.sh <worktree> <out.tgz>
# Run it AFTER `sh scripts/build-binaries.sh`, so bin/ateam-linux-amd64 carries the ring-1 changes.
# The archive root is `agent-teams/`. Reads only; writes only <out.tgz>.
set -eu
WT=${1:?usage: pack-plugin.sh <worktree> <out.tgz>}
OUT=${2:?usage: pack-plugin.sh <worktree> <out.tgz>}
[ -d "$WT/plugins/agent-teams" ] || { echo "no plugins/agent-teams in $WT" >&2; exit 1; }
[ -f "$WT/plugins/agent-teams/bin/ateam-linux-amd64" ] || { echo "missing bin/ateam-linux-amd64" >&2; exit 1; }
# COPYFILE_DISABLE stops bsdtar writing AppleDouble ._* entries, which the sandbox plugin loader registers as agents.
COPYFILE_DISABLE=1 tar -czf "$OUT" -C "$WT/plugins" agent-teams
echo "packed $OUT"
