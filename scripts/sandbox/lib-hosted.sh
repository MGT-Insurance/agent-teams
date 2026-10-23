#!/bin/sh
# Sourced by install.sh (sandbox) and laptop-ring1.sh (laptop). Defines hosted_workspace.
#
# hosted_workspace <dir> <database> <prefix>
#   Points <dir>/.beads at a hosted Dolt database per contract F1, using the
#   Dispatcher-proven recipe in a scratch dir, then installing its files into <dir>:
#   metadata.json, dolt-server.port, and config.yaml (only if <dir> has none).
#   Then sets events-journal: true in <dir>. Reads the DB password from
#   ~/.config/beads/credentials via bd; never reads or prints it here.
#
# Needs HOSTED_HOST and DB_USER in the environment, plus bd and node on PATH.

hosted_workspace() {
  hw_dir=$1 hw_db=$2 hw_prefix=$3
  hw_scratch=$(mktemp -d) || return 1
  (
    cd "$hw_scratch" || exit 1
    git init -q
    env -u BEADS_DIR bd init --server --external --server-host "$HOSTED_HOST" --server-port 3306 \
      --server-user "$DB_USER" --database "$hw_db" --server-tls --remote '' --prefix "$hw_prefix" || exit 1
    printf 3306 > .beads/dolt-server.port
    node -e 'const fs=require("fs");const m=JSON.parse(fs.readFileSync(".beads/metadata.json","utf8"));delete m.dolt_server_port;fs.writeFileSync(".beads/metadata.json",JSON.stringify(m)+"\n")'
  ) || { rm -rf "$hw_scratch"; return 1; }
  mkdir -p "$hw_dir/.beads"
  cp "$hw_scratch/.beads/metadata.json" "$hw_scratch/.beads/dolt-server.port" "$hw_dir/.beads/"
  [ -f "$hw_dir/.beads/config.yaml" ] || cp "$hw_scratch/.beads/config.yaml" "$hw_dir/.beads/config.yaml"
  rm -rf "$hw_scratch"
  (cd "$hw_dir" && env -u BEADS_DIR bd config set events-journal true) || return 1
  echo "hosted workspace $hw_dir -> db $hw_db (prefix $hw_prefix, host $HOSTED_HOST, user $DB_USER)"
}
