#!/usr/bin/env bash
# Tests for the compact-recovery SessionStart hook script.
#
# The hook resolves cwd -> initiative id through `ateam resolve-initiative`
# (internal/verbs/match.go), not its own jq over the description, so these
# cases drive it against a REAL binary built from this tree into a temp plugin
# root — a canned shim could not exercise the matching rule at all.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/compact-recovery.sh"
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

# ── Temp plugin root holding a freshly built ateam + the committed wrapper ────
PLATFORM_OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in
    x86_64)         PLATFORM_ARCH=amd64 ;;
    aarch64|arm64)  PLATFORM_ARCH=arm64 ;;
    *)              PLATFORM_ARCH="$(uname -m)" ;;
esac
export CLAUDE_PLUGIN_ROOT="$T/plugin-root"
mkdir -p "$CLAUDE_PLUGIN_ROOT/bin"
go build -C "$ROOT" -o "$CLAUDE_PLUGIN_ROOT/bin/ateam-${PLATFORM_OS}-${PLATFORM_ARCH}" ./cmd/ateam
cp "$ROOT/plugins/agent-teams/bin/ateam" "$CLAUDE_PLUGIN_ROOT/bin/ateam"
chmod +x "$CLAUDE_PLUGIN_ROOT/bin/ateam" "$CLAUDE_PLUGIN_ROOT/bin/ateam-${PLATFORM_OS}-${PLATFORM_ARCH}"

export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME" "$T/wt/apps/nested"
git -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && bd init --prefix at --non-interactive >/dev/null)
printf 'problem: test problem\nrepo: %s\nworktree: %s\nbranch: feat/x\nteam: test-team\nmode: interactive\n' "$T/wt" "$T/wt" > "$T/body.md"
bd -C "$AGENT_TEAMS_HOME" create --title="Hook test initiative" --type=task --priority=2 --body-file="$T/body.md" >/dev/null

# Case 1: cwd matches a registered open initiative -> emits context
out=$(cd "$T/wt" && "$SCRIPT")
echo "$out" | grep -q "Hook test initiative" || { echo "FAIL case1: no context for matching cwd"; exit 1; }
echo "$out" | grep -q "/dri skill governs" || { echo "FAIL case1: missing governance reminder"; exit 1; }

# Case 1b: cwd is a SUBDIRECTORY of the worktree -> still recovers.
# Regression guard for agent-teams-ully.9: the old jq matched the worktree line
# by whole-line equality, so post-compaction recovery silently did nothing from
# anywhere below the worktree root.
out=$(cd "$T/wt/apps/nested" && "$SCRIPT")
echo "$out" | grep -q "Hook test initiative" || { echo "FAIL case1b: no context from a worktree subdirectory"; exit 1; }

# Case 2: non-matching cwd -> silent. $T is the worktree's PARENT, which must
# not match: ancestor semantics resolve downward from the worktree, not upward.
out=$(cd "$T" && "$SCRIPT")
[ -z "$out" ] || { echo "FAIL case2: output for non-matching cwd"; exit 1; }

# Case 2b: a sibling directory sharing a string prefix with the worktree
# ("$T/wt" vs "$T/wt-decoy") must not match.
mkdir -p "$T/wt-decoy"
out=$(cd "$T/wt-decoy" && "$SCRIPT")
[ -z "$out" ] || { echo "FAIL case2b: prefix-sharing sibling directory matched"; exit 1; }

# Case 3: workspace absent -> silent
out=$( (cd "$T/wt" && AGENT_TEAMS_HOME="$T/nope" "$SCRIPT") )
[ -z "$out" ] || { echo "FAIL case3: output without workspace"; exit 1; }

# Case 3b: ateam unreachable -> silent (fail-soft, never noise a session start)
out=$( (cd "$T/wt" && CLAUDE_PLUGIN_ROOT="$T/nonexistent" "$SCRIPT") )
[ -z "$out" ] || { echo "FAIL case3b: output without a reachable ateam"; exit 1; }

# Case 4: closed initiatives never match
id=$(bd -C "$AGENT_TEAMS_HOME" list --status=open --json | jq -r '.[0].id')
bd -C "$AGENT_TEAMS_HOME" close "$id" >/dev/null
out=$(cd "$T/wt" && "$SCRIPT")
[ -z "$out" ] || { echo "FAIL case4: matched a closed initiative"; exit 1; }

# Case 5: two open initiatives, only one matches -> emits the right one only
printf 'problem: decoy\nrepo: %s\nworktree: %s/elsewhere\nbranch: other\nteam: other\nmode: interactive\n' "$T" "$T" > "$T/decoy-body.md"
bd -C "$AGENT_TEAMS_HOME" create --title="Decoy initiative" --type=task --priority=3 --body-file="$T/decoy-body.md" >/dev/null
printf 'problem: real\nrepo: %s\nworktree: %s\nbranch: feat/x\nteam: test-team\nmode: interactive\n' "$T/wt" "$T/wt" > "$T/body2.md"
bd -C "$AGENT_TEAMS_HOME" create --title="Second real initiative" --type=task --priority=2 --body-file="$T/body2.md" >/dev/null
out=$(cd "$T/wt" && "$SCRIPT")
echo "$out" | grep -q "Second real initiative" || { echo "FAIL case5: matching initiative not emitted"; exit 1; }
echo "$out" | grep -q "Decoy initiative" && { echo "FAIL case5: decoy leaked"; exit 1; }

# Case 6: a nested worktree wins over the outer one from inside it — the old jq
# took the first list-order match, which could recover the wrong initiative.
mkdir -p "$T/wt/inner/sub"
printf 'problem: inner\nrepo: %s\nworktree: %s/inner\nbranch: feat/inner\nteam: t\nmode: interactive\n' "$T/wt" "$T/wt" > "$T/inner-body.md"
bd -C "$AGENT_TEAMS_HOME" create --title="Inner initiative" --type=task --priority=2 --body-file="$T/inner-body.md" >/dev/null
out=$(cd "$T/wt/inner/sub" && "$SCRIPT")
echo "$out" | grep -q "Inner initiative" || { echo "FAIL case6: most-specific worktree did not win"; exit 1; }
echo "$out" | grep -q "Second real initiative" && { echo "FAIL case6: outer initiative leaked"; exit 1; }

echo "PASS"
