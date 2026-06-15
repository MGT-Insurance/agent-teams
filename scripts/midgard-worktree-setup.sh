#!/usr/bin/env bash
set -euo pipefail

# midgard-worktree-setup.sh — make a fresh midgard git worktree runnable.
#
# A new worktree is missing the gitignored env wiring the main checkout has, so
# creds-dependent tooling (e.g. socotra-config's pre-commit validate.sh, which
# POSTs to the Socotra API) hard-fails. This restores that wiring by:
#   1. copying the repo-level Vercel link (.vercel/ — project/org IDs, not secrets)
#   2. pulling Vercel-backed env (`pnpm env:pull`; today only shadowfax implements it)
#   3. copying the local-only env files Vercel does NOT own (socotra creds, etc.)
#
# Secrets are moved by opaque `cp`/`vercel env pull` only — no env contents are
# ever printed. Worktree must already have deps installed (`pnpm install`) for
# step 2's `turbo run env:pull` to resolve.
#
# Usage: midgard-worktree-setup.sh <worktree-path> [source-checkout]
#   source-checkout defaults to the main checkout behind the worktree's git dir.

WT="${1:?usage: midgard-worktree-setup.sh <worktree-path> [source-checkout]}"
WT="$(cd "$WT" && pwd)"
SRC="${2:-$(dirname "$(git -C "$WT" rev-parse --path-format=absolute --git-common-dir)")}"
SRC="$(cd "$SRC" && pwd)"

if [ "$WT" = "$SRC" ]; then
  echo "refusing to run: worktree path equals source checkout ($WT)" >&2
  exit 1
fi

echo "→ source checkout: $SRC"
echo "→ target worktree: $WT"

# 1. Vercel repo link (ids only) — enables the vercel CLI inside the worktree.
if [ -d "$SRC/.vercel" ]; then
  mkdir -p "$WT/.vercel"
  cp -R "$SRC/.vercel/." "$WT/.vercel/"
  echo "✓ copied .vercel link"
else
  echo "⚠ no .vercel in source checkout — skipping vercel link + env:pull"
fi

# 2. Pull Vercel-backed env (regenerates e.g. apps/shadowfax/.env.local).
if [ -d "$WT/.vercel" ] && [ -d "$WT/node_modules" ]; then
  _env_pull_err="/tmp/midgard-setup-env-pull-$$.err"
  if ( cd "$WT" && pnpm env:pull ) 2>"$_env_pull_err"; then
    echo "✓ pulled vercel env"
  else
    _exit=$?
    # Surface a clear message for the most common failure: missing auth.
    if grep -qiE "no (existing )?credentials|not logged in|please run.*login|vercel login|VERCEL_TOKEN" "$_env_pull_err" 2>/dev/null; then
      echo "✗ env:pull failed: vercel auth missing — run \`vercel login\` or set VERCEL_TOKEN" >&2
    else
      echo "✗ env:pull failed (exit $_exit) — see output above for details" >&2
    fi
    rm -f "$_env_pull_err"
    exit "$_exit"
  fi
  rm -f "$_env_pull_err"
elif [ -d "$WT/.vercel" ]; then
  echo "⚠ worktree has no node_modules — run 'pnpm install' then 'pnpm env:pull' for vercel-backed env"
fi

# 3. Copy local-only env files NOT covered by env:pull.
#    Paths are relative to the repo root. Add new entries here as needed.
LOCAL_ENV_FILES=(
  "apps/shadowfax/.env.development.local"
  "packages/socotra-config/.env.local"
)
for rel in "${LOCAL_ENV_FILES[@]}"; do
  if [ -f "$SRC/$rel" ]; then
    mkdir -p "$WT/$(dirname "$rel")"
    cp "$SRC/$rel" "$WT/$rel"
    echo "✓ copied $rel"
  else
    echo "⚠ source missing $rel — skipped"
  fi
done

echo "✓ worktree setup complete: $WT"
