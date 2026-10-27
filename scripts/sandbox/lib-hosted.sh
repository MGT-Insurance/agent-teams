#!/bin/sh
# Sourced by install.sh (sandbox) and laptop-ring1.sh (laptop). Defines hosted_workspace.
#
# hosted_workspace <dir> <database> <prefix>
#   Points <dir>/.beads at a hosted Dolt database per contract F1, using the
#   Dispatcher-proven recipe in a scratch dir, then installing its files into <dir>:
#   metadata.json, dolt-server.port, and config.yaml (only if <dir> has none).
#   Then sets events-journal: true in <dir>.
#
# bd 1.3.0 quirk: `bd init --server --external` authenticates ONLY through BEADS_DOLT_PASSWORD and
# ignores the credentials file; every other bd command reads the file. hosted_bd_init bridges that:
# it parses the password for $HOSTED_HOST:3306 from the file bd uses afterwards
# ($BEADS_CREDENTIALS_FILE, else ~/.config/beads/credentials) and sets BEADS_DOLT_PASSWORD for
# that one command only. The password is never in argv, never printed, never exported.
#
# First use of a hosted database: Dispatcher creates it EMPTY, so the `bd init` above is the
# step that gives it a beads schema and mints its _project_id. The same command is safe on a
# database that already has the schema: bd adopts the existing _project_id and changes nothing
# (checked against a local dolt sql-server, bd 1.3.0), and it is the only way to get a
# metadata.json that carries that id. So it always runs, and the helper reports which case it hit.
#
# Needs HOSTED_HOST and DB_USER in the environment, plus bd and node on PATH.

hosted_bd_init() {
  hb_file=${BEADS_CREDENTIALS_FILE:-$HOME/.config/beads/credentials}
  [ -r "$hb_file" ] || { echo "hosted_bd_init: credentials file $hb_file is missing or unreadable" >&2; return 1; }
  hb_pw=$(awk -v h="[$HOSTED_HOST:3306]" '$0 == h { s = 1; next } /^\[/ { s = 0 } s && /^password[ \t]*=/ { sub(/^password[ \t]*=[ \t]*/, ""); print; exit }' "$hb_file")
  [ -n "$hb_pw" ] || { echo "hosted_bd_init: $hb_file has no 'password =' line under [$HOSTED_HOST:3306]" >&2; return 1; }
  BEADS_DOLT_PASSWORD=$hb_pw env -u BEADS_DIR bd init "$@"
  hb_rc=$?
  unset hb_pw
  return $hb_rc
}

hosted_workspace() {
  hw_dir=$1 hw_db=$2 hw_prefix=$3
  hw_scratch=$(mktemp -d) || return 1
  (
    cd "$hw_scratch" || exit 1
    git init -q
    hw_out=$(hosted_bd_init --server --external --server-host "$HOSTED_HOST" --server-port 3306 \
      --server-user "$DB_USER" --database "$hw_db" --server-tls --remote '' --prefix "$hw_prefix" 2>&1) \
      || { printf '%s\n' "$hw_out" >&2; exit 1; }
    case $hw_out in
      *"Adopted project identity"*) printf adopted > "$hw_scratch/.init-mode" ;;
      *) printf initialized > "$hw_scratch/.init-mode" ;;
    esac
    printf 3306 > .beads/dolt-server.port
    node -e 'const fs=require("fs");const m=JSON.parse(fs.readFileSync(".beads/metadata.json","utf8"));delete m.dolt_server_port;fs.writeFileSync(".beads/metadata.json",JSON.stringify(m)+"\n")'
  ) || { rm -rf "$hw_scratch"; return 1; }
  mkdir -p "$hw_dir/.beads"
  cp "$hw_scratch/.beads/metadata.json" "$hw_scratch/.beads/dolt-server.port" "$hw_dir/.beads/"
  [ -f "$hw_dir/.beads/config.yaml" ] || cp "$hw_scratch/.beads/config.yaml" "$hw_dir/.beads/config.yaml"
  hw_mode=$(cat "$hw_scratch/.init-mode")
  rm -rf "$hw_scratch"
  (cd "$hw_dir" && env -u BEADS_DIR bd config set events-journal true) || return 1
  echo "hosted workspace $hw_dir -> db $hw_db ($hw_mode; prefix $hw_prefix, host $HOSTED_HOST, user $DB_USER)"
}
