#!/usr/bin/env sh
# Build ateam for all four supported platforms and drop the binaries into
# plugins/agent-teams/bin/.  Idempotent: safe to re-run after editing cmd/ateam.
set -eu

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$REPO_ROOT/plugins/agent-teams/bin"
mkdir -p "$OUT"

build() {
    os="$1"
    arch="$2"
    dest="$OUT/ateam-${os}-${arch}"
    printf 'building %s ...\n' "ateam-${os}-${arch}"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -C "$REPO_ROOT" -o "$dest" ./cmd/ateam
    chmod +x "$dest"
}

build darwin  arm64
build darwin  amd64
build linux   amd64
build linux   arm64

printf 'done — binaries in %s\n' "$OUT"
ls -la "$OUT"/ateam-*
