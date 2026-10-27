#!/bin/bash
# Laptop: put the sandbox's DB login into a ring-1 sandbox, from the Dispatcher credentials JSON.
#   bash put-credentials.sh [sb.mjs flags, e.g. --name ring1]
#   bash put-credentials.sh --dry-run <dir>     write the two files into <dir> instead (no network)
#
# Reads $RING1_EXT_JSON (default ~/.config/beads/ring1-ext.json), the saved response of
# GET /api/dolt/databases/credentials: {user, host, port, databases, credentials}. `credentials`
# is the bd credentials-file text. It is piped through stdin to `sb.mjs put` (mode 600), so the
# secret never appears in argv, a log, or stdout. Puts ring1.credentials and ring1.user
# (the `.user` login) for install.sh. Needs jq and, outside --dry-run, node plus sb.mjs's setup.
set -eu
HERE=$(cd "$(dirname "$0")" && pwd)
EXT=${RING1_EXT_JSON:-$HOME/.config/beads/ring1-ext.json}
HOSTED_HOST=${HOSTED_HOST:-mgt-spike.dbs.hosted.doltdb.com}
EXT_USER=${EXT_USER:-eric_lloyd_ext}

fail() { echo "put-credentials: $*" >&2; exit 1; }

[ -f "$EXT" ] || fail "$EXT is missing. Create a database in Dispatcher Settings, then save the response of GET /api/dolt/databases/credentials there."
user=$(jq -r '.user // empty' "$EXT") || fail "$EXT is not valid JSON"
[ "$user" = "$EXT_USER" ] || fail "$EXT has .user '$user'; expected the scoped login '$EXT_USER' (set EXT_USER to override)"
# Checks the shape only, never the value: a header line, then a password line.
jq -e --arg h "[$HOSTED_HOST:3306]" '.credentials | type == "string" and startswith($h + "\npassword = ")' "$EXT" >/dev/null \
  || fail "$EXT .credentials is not a '[$HOSTED_HOST:3306]' section with a password line"

if [ "${1:-}" = "--dry-run" ]; then
  out=${2:?usage: put-credentials.sh --dry-run <dir>}
  mkdir -p "$out"
  (umask 077 && jq -j '.credentials' "$EXT" > "$out/ring1.credentials" && jq -j '.user' "$EXT" > "$out/ring1.user")
  chmod 600 "$out/ring1.credentials"
  echo "dry-run: wrote $out/ring1.credentials (mode 600, secret not printed) and $out/ring1.user ($user)"
  exit 0
fi

jq -j '.credentials' "$EXT" | node "$HERE/sb.mjs" put - ring1.credentials 600 "$@"
jq -j '.user' "$EXT" | node "$HERE/sb.mjs" put - ring1.user 600 "$@"
