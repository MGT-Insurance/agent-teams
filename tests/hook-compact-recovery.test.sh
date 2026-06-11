#!/usr/bin/env bash
# Tests for the compact-recovery SessionStart hook script.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/compact-recovery.sh"
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
export AGENT_TEAMS_HOME="$T/ws"
mkdir -p "$AGENT_TEAMS_HOME" "$T/wt"
git -C "$AGENT_TEAMS_HOME" init -q
(cd "$AGENT_TEAMS_HOME" && bd init --prefix at --non-interactive >/dev/null)
printf 'problem: test problem\nrepo: %s\nworktree: %s\nbranch: feat/x\nteam: test-team\nmode: interactive\n' "$T/wt" "$T/wt" > "$T/body.md"
bd -C "$AGENT_TEAMS_HOME" create --title="Hook test initiative" --type=task --priority=2 --body-file="$T/body.md" >/dev/null

# Case 1: cwd matches a registered open initiative -> emits context
out=$(cd "$T/wt" && "$SCRIPT")
echo "$out" | grep -q "Hook test initiative" || { echo "FAIL case1: no context for matching cwd"; exit 1; }
echo "$out" | grep -q "/dri skill governs" || { echo "FAIL case1: missing governance reminder"; exit 1; }

# Case 2: non-matching cwd -> silent
out=$(cd "$T" && "$SCRIPT")
[ -z "$out" ] || { echo "FAIL case2: output for non-matching cwd"; exit 1; }

# Case 3: workspace absent -> silent
out=$( (cd "$T/wt" && AGENT_TEAMS_HOME="$T/nope" "$SCRIPT") )
[ -z "$out" ] || { echo "FAIL case3: output without workspace"; exit 1; }

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

echo "PASS"
