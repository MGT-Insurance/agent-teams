#!/bin/bash
# Laptop: put the sandbox's DB login into a ring-1 sandbox, from the Dispatcher credentials JSON.
#   bash put-credentials.sh [sb.mjs flags, e.g. --name ring1]
#   bash put-credentials.sh --dry-run <dir>     write the two files into <dir> instead (no network)
#
# Reads $RING1_EXT_JSON (default ~/.config/beads/ring1-ext.json), validated by lib-ext.sh. The
# `credentials` field is the bd credentials-file text. It is piped through stdin to `sb.mjs put` (mode 600), so the
# secret never appears in argv, a log, or stdout. Puts ring1.credentials and ring1.user
# (the `.user` login) for install.sh. Needs jq and, outside --dry-run, node plus sb.mjs's setup.
set -eu
HERE=$(cd "$(dirname "$0")" && pwd)
. "$HERE/lib-ext.sh"
ext_validate || exit 1
user=$EXT_USER_NAME

if [ "${1:-}" = "--dry-run" ]; then
  out=${2:?usage: put-credentials.sh --dry-run <dir>}
  mkdir -p "$out"
  ext_write_credentials "$out/ring1.credentials"
  (umask 077 && jq -j '.user' "$EXT_JSON" > "$out/ring1.user")
  echo "dry-run: wrote $out/ring1.credentials (mode 600, secret not printed) and $out/ring1.user ($user)"
  exit 0
fi

jq -j '.credentials' "$EXT_JSON" | node "$HERE/sb.mjs" put - ring1.credentials 600 "$@"
jq -j '.user' "$EXT_JSON" | node "$HERE/sb.mjs" put - ring1.user 600 "$@"
