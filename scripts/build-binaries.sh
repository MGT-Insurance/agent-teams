#!/usr/bin/env sh
# Build ateam for all four supported platforms, publish the same artifacts to
# both runtime plugins, and verify that their complete bin directories agree.
# Idempotent: safe to re-run after editing cmd/ateam.
set -eu

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CLAUDE_OUT="$REPO_ROOT/plugins/agent-teams/bin"
CODEX_OUT="$REPO_ROOT/plugins/agent-teams-codex/bin"
mkdir -p "$CLAUDE_OUT" "$CODEX_OUT"

build() {
    os="$1"
    arch="$2"
    name="ateam-${os}-${arch}"
    dest="$CLAUDE_OUT/$name"
    printf 'building %s ...\n' "$name"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
        go build -C "$REPO_ROOT" -trimpath -buildvcs=false -ldflags='-buildid=' \
        -o "$dest" ./cmd/ateam
    chmod +x "$dest"
    cp -f "$dest" "$CODEX_OUT/$name"
    chmod +x "$CODEX_OUT/$name"
}

build darwin  arm64
build darwin  amd64
build linux   amd64
build linux   arm64

cp -f "$CLAUDE_OUT/ateam" "$CODEX_OUT/ateam"
chmod +x "$CODEX_OUT/ateam"

for name in \
    ateam \
    ateam-darwin-arm64 \
    ateam-darwin-amd64 \
    ateam-linux-amd64 \
    ateam-linux-arm64
do
    cmp "$CLAUDE_OUT/$name" "$CODEX_OUT/$name"
done

printf 'done — identical artifacts in:\n  %s\n  %s\n' "$CLAUDE_OUT" "$CODEX_OUT"
ls -la "$CLAUDE_OUT"/ateam-*
