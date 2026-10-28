#!/bin/bash
# Runs INSIDE the sandbox (copy it and lib-hosted.sh in with `sb.mjs put`, then
# `sb.mjs exec 'bash ~/install.sh <sandbox-name>'`). Idempotent.
#
# Expects, already put into $HOME:
#   plugin.tgz          the plugin tarball from pack-plugin.sh (bin/ateam-linux-amd64 rebuilt)
#   ring1.credentials   the eric_lloyd_ext credentials text (mode 600), piped in by
#                       put-credentials.sh from ring1-ext.json; moved, never printed
#   ring1.user          the login name from ring1-ext.json (`.user`); DB_USER defaults to it
#   lib-hosted.sh       the shared hosted_workspace helper
#
# Contract: docs/hosted-beads.md F1, F2, F5, F11, F12.
set -eu
NAME=${1:?usage: install.sh <sandbox-name>}

BD_VERSION=${BD_VERSION:-1.3.0}
BD_URL=${BD_URL:-https://github.com/gastownhall/beads/releases/download/v${BD_VERSION}/beads_${BD_VERSION}_linux_amd64.tar.gz}
REPO_URL=${REPO_URL:-https://github.com/MGT-Insurance/agent-teams.git}
REPO_BRANCH=${REPO_BRANCH:-main}
export HOSTED_HOST=${HOSTED_HOST:-mgt-spike.dbs.hosted.doltdb.com}
GLOBAL_DB=${GLOBAL_DB:-beads_eric_lloyd_at}
GLOBAL_PREFIX=${GLOBAL_PREFIX:-at}
PROJECT_DB=${PROJECT_DB:-beads_eric_lloyd_agent_teams}
PROJECT_PREFIX=${PROJECT_PREFIX:-agent-teams}
MODEL=${MODEL:-anthropic/claude-sonnet-5.5}

export PATH=$HOME/bin:$HOME/.local/bin:$PATH
export AGENT_TEAMS_HOME=$HOME/.agent-teams
export AGENT_TEAMS_HOST=sandbox-$NAME
export BEADS_DOLT_SERVER_TLS=1
CLONE=$HOME/work/agent-teams

for f in plugin.tgz ring1.credentials ring1.user lib-hosted.sh; do
  [ -f "$HOME/$f" ] || { echo "missing $HOME/$f (put it first)" >&2; exit 1; }
done
export DB_USER=${DB_USER:-$(cat "$HOME/ring1.user")}
. "$HOME/lib-hosted.sh"
mkdir -p "$HOME/bin" "$HOME/.local/bin" "$HOME/plugins" "$HOME/work" "$HOME/.claude" "$HOME/.config/beads"

echo "== bd $BD_VERSION"
if ! bd version 2>/dev/null | grep -q "$BD_VERSION"; then
  t=$(mktemp -d)
  curl -fsSL "$BD_URL" | tar -xz -C "$t"
  install -m 755 "$(find "$t" -type f -name bd | head -1)" "$HOME/bin/bd"
  rm -rf "$t"
fi
bd version

echo "== plugin"
rm -rf "$HOME/plugins/agent-teams"
tar -xzf "$HOME/plugin.tgz" -C "$HOME/plugins"
# Guard: AppleDouble files from a macOS pack would register as bogus agents.
find "$HOME/plugins/agent-teams" -name '._*' -delete
ln -sf "$HOME/plugins/agent-teams/bin/ateam" "$HOME/.local/bin/ateam"
ateam --help >/dev/null && echo "ateam ok"

echo "== credentials (moved by path, mode 600, not printed)"
mv -f "$HOME/ring1.credentials" "$HOME/.config/beads/credentials"
chmod 600 "$HOME/.config/beads/credentials"

echo "== git identity"
git config --global user.email ring1.sandbox@example.com
git config --global user.name "ring1 sandbox"

echo "== global workspace (F1, F11)"
mkdir -p "$AGENT_TEAMS_HOME"
[ -d "$AGENT_TEAMS_HOME/.git" ] || git -C "$AGENT_TEAMS_HOME" init -q
hosted_workspace "$AGENT_TEAMS_HOME" "$GLOBAL_DB" "$GLOBAL_PREFIX"

echo "== project clone (F1, F11)"
[ -d "$CLONE/.git" ] || git clone --branch "$REPO_BRANCH" "$REPO_URL" "$CLONE"
hosted_workspace "$CLONE" "$PROJECT_DB" "$PROJECT_PREFIX"
# The clone tracks an embedded .beads/metadata.json and config.yaml; keep the hosted edits out of git.
git -C "$CLONE" update-index --skip-worktree .beads/metadata.json .beads/config.yaml

echo "== claude trust + env"
node -e 'const fs=require("fs"),h=process.env.HOME;
const rw=(p,f)=>{let c={};try{c=JSON.parse(fs.readFileSync(p,"utf8"))}catch{}f(c);fs.writeFileSync(p,JSON.stringify(c))};
rw(h+"/.claude.json",c=>{c.projects=c.projects||{};for(const d of [h+"/work/agent-teams"])c.projects[d]=Object.assign({},c.projects[d],{hasTrustDialogAccepted:true});c.hasCompletedOnboarding=true});
rw(h+"/.claude/settings.json",c=>{c.skipDangerousModePermissionPrompt=true})'

cat > "$HOME/ring1-env.sh" <<EOF
export PATH=\$HOME/bin:\$HOME/.local/bin:\$PATH
export AGENT_TEAMS_HOME=\$HOME/.agent-teams
export AGENT_TEAMS_HOST=sandbox-$NAME
export BEADS_DOLT_SERVER_TLS=1
EOF
grep -q 'ring1-env.sh' "$HOME/.profile" 2>/dev/null || echo '. "$HOME/ring1-env.sh"' >> "$HOME/.profile"

cat > "$HOME/run-claude.sh" <<EOF
. "\$HOME/ring1-env.sh"
export ANTHROPIC_BASE_URL=https://ai-gateway.vercel.sh/claude-code ANTHROPIC_AUTH_TOKEN=placeholder ANTHROPIC_API_KEY=
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1
export CLAUDE_CODE_MAX_CONTEXT_TOKENS=1000000 DISABLE_COMPACT=1
# Non-secret: gh needs some token set; the api.github.com header transform injects the real one.
export GH_TOKEN=placeholder
cd "\$HOME/work/agent-teams"
exec claude --permission-mode bypassPermissions --disallowedTools AskUserQuestion --model $MODEL --plugin-dir "\$HOME/plugins/agent-teams" "\$@"
EOF
chmod +x "$HOME/run-claude.sh"

echo "== check"
bd -C "$AGENT_TEAMS_HOME" dolt show
# dolt show connects without TLS on bd 1.3.0, so it cannot prove F2. A real read can.
for ws in "$AGENT_TEAMS_HOME" "$CLONE"; do
  if bd -C "$ws" list --limit 1 --json >/dev/null; then
    echo "data path OK: $ws"
  else
    echo "data path FAILED: $ws (bd list errored; check BEADS_DOLT_SERVER_TLS and the credentials)" >&2
    exit 1
  fi
done
echo "install done for sandbox-$NAME"
