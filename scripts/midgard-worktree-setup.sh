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
# ever printed. If the worktree has no node_modules, step 2 runs `pnpm install`
# itself before pulling, so a fresh worktree comes out bootable in one command.
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
#    node_modules is required for `pnpm env:pull` to resolve — install it
#    ourselves when absent, so a fresh worktree comes out bootable.
if [ -d "$WT/.vercel" ]; then
  if [ ! -d "$WT/node_modules" ]; then
    echo "→ installing dependencies (pnpm install)…"
    if ( cd "$WT" && pnpm install ); then
      echo "✓ dependencies installed"
    else
      echo "✗ pnpm install failed — cannot pull vercel env" >&2
      exit 1
    fi
  fi

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
fi

# 3. Copy local-only env files NOT covered by env:pull.
#    Paths are relative to the repo root. Add new entries here as needed.
LOCAL_ENV_FILES=(
  "apps/shadowfax/.env.development.local"
  "packages/socotra-config/.env.local"
  "packages/ngrok/.env.local"
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

# 4. Verify the env files this run was responsible for producing actually
#    landed. A deferred/failed vercel pull (step 2) or a missing source file
#    (step 3) must not be reported as success.
missing=()
for rel in "${LOCAL_ENV_FILES[@]}"; do
  if [ -f "$SRC/$rel" ] && [ ! -s "$WT/$rel" ]; then
    missing+=("$rel")
  fi
done
if [ -d "$SRC/.vercel" ] && [ ! -s "$WT/apps/shadowfax/.env.local" ]; then
  missing+=("apps/shadowfax/.env.local")
fi

if [ "${#missing[@]}" -gt 0 ]; then
  echo ""
  echo "✗ worktree NOT provisioned: expected env file(s) missing or empty:"
  for rel in "${missing[@]}"; do
    if [ -f "$WT/$rel" ]; then
      echo "  - $rel (empty)"
    else
      echo "  - $rel (missing)"
    fi
  done
  echo "run 'pnpm install && pnpm env:pull' in $WT, then re-run 'ateam worktree-setup'."
  exit 1
fi

echo "✓ worktree setup complete: $WT"
