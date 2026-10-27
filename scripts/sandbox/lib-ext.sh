#!/bin/bash
# Sourced on the laptop by put-credentials.sh and laptop-ring1.sh. Reads the Dispatcher credentials
# JSON (the saved response of GET /api/dolt/databases/credentials: {user, host, port, databases,
# credentials}); `credentials` is the bd credentials-file text. Never prints the secret.
# Needs jq.
#
# ext_validate     checks $RING1_EXT_JSON exists, parses, has the scoped login, and that
#                  .credentials is a '[host:3306]' section with a password line (shape only).
#                  Sets EXT_JSON and EXT_USER_NAME. Prints the problem to stderr and returns 1.
# ext_write_credentials <file>
#                  writes .credentials to <file> at mode 600 (umask 077, then chmod).

EXT_JSON=${RING1_EXT_JSON:-$HOME/.config/beads/ring1-ext.json}
EXT_USER=${EXT_USER:-eric_lloyd_ext}
HOSTED_HOST=${HOSTED_HOST:-mgt-spike.dbs.hosted.doltdb.com}

ext_validate() {
  [ -f "$EXT_JSON" ] || { echo "$EXT_JSON is missing. Create a database in Dispatcher Settings, then save the response of GET /api/dolt/databases/credentials there." >&2; return 1; }
  EXT_USER_NAME=$(jq -r '.user // empty' "$EXT_JSON") || { echo "$EXT_JSON is not valid JSON" >&2; return 1; }
  [ "$EXT_USER_NAME" = "$EXT_USER" ] || { echo "$EXT_JSON has .user '$EXT_USER_NAME'; expected the scoped login '$EXT_USER' (set EXT_USER to override)" >&2; return 1; }
  # Checks the shape only, never the value: a header line, then a password line.
  jq -e --arg h "[$HOSTED_HOST:3306]" '.credentials | type == "string" and startswith($h + "\npassword = ")' "$EXT_JSON" >/dev/null \
    || { echo "$EXT_JSON .credentials is not a '[$HOSTED_HOST:3306]' section with a password line" >&2; return 1; }
}

ext_write_credentials() {
  (umask 077 && jq -j '.credentials' "$EXT_JSON" > "$1") && chmod 600 "$1"
}
