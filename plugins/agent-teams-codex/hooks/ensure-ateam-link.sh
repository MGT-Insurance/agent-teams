#!/bin/sh
set -eu

target="${PLUGIN_ROOT:?PLUGIN_ROOT is required}/bin/ateam"
link_dir="${HOME:?HOME is required}/.local/bin"
link="$link_dir/ateam"

if [ ! -x "$target" ]; then
    printf 'agent-teams: bundled ateam wrapper is missing or not executable: %s\n' "$target" >&2
    exit 1
fi

mkdir -p "$link_dir"
ln -sfn "$target" "$link"
