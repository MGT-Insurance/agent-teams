#!/usr/bin/env bash
# Tests for the block-bare-path-pr-comment PreToolUse hook script.
#
# This hook exists because a reminder-level LEARNING already told reviewers
# to reject bare-path bodies and it was in-context on midgard #5203 and
# STILL failed — a submitted PR review whose entire body was the literal
# string "@/Users/ericlloyd/.claude/jobs/77164d6f/tmp/review-body-5203.md".
# A test that only checks the script runs without crashing would stay green
# through exactly that failure mode — every assertion below checks the
# actual deny/allow decision on a crafted PreToolUse Bash payload.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/plugins/agent-teams/hooks/scripts/block-bare-path-pr-comment.sh"
HOOKS_JSON="$ROOT/plugins/agent-teams/hooks/hooks.json"

# make_payload builds a PreToolUse Bash-tool payload for the given command
# string, JSON-encoding it safely via jq so embedded quotes need no manual
# escaping here.
make_payload() {
  local command="$1"
  jq -nc --arg cmd "$command" '{"tool_name":"Bash","tool_input":{"command":$cmd}}'
}

run() { printf '%s' "$1" | "$SCRIPT"; }

# Capture stdout+status without tripping this script's own `set -e`.
call() {
  local payload="$1"
  out=$(run "$payload") && status=0 || status=$?
}

assert_deny() {
  local label="$1" command="$2"; shift 2
  call "$(make_payload "$command")"
  [ "$status" -eq 0 ] \
    || { echo "FAIL $label: expected exit 0, got $status (output: $out)"; exit 1; }
  echo "$out" | grep -q '"deny"' \
    || { echo "FAIL $label: expected deny for command [$command], got: $out"; exit 1; }
  for needle in "$@"; do
    echo "$out" | grep -qF -- "$needle" \
      || { echo "FAIL $label: denial message missing '$needle', got: $out"; exit 1; }
  done
}

assert_allow() {
  local label="$1" command="$2"
  call "$(make_payload "$command")"
  [ "$status" -eq 0 ] \
    || { echo "FAIL $label: expected exit 0, got $status (output: $out)"; exit 1; }
  [ -z "$out" ] \
    || { echo "FAIL $label: expected silent allow for command [$command], got: $out"; exit 1; }
}

# ---- Deny cases (KNOWN-BAD) -------------------------------------------------

# Case 1: the EXACT midgard #5203 form — a raw-field (-f) body value that is
# an @path reference, which gh passes verbatim as the literal string, not
# the file's contents. This is the artifact the whole initiative exists for.
assert_deny "case1-midgard-5203-exact" \
  'gh api repos/acme/widgets/pulls/5203/reviews --method POST -f event=COMMENT -f body=@/Users/ericlloyd/.claude/jobs/77164d6f/tmp/review-body-5203.md' \
  "midgard #5203" \
  "--body-file"

# Case 2: a raw-field body value that is a bare path with NO @ prefix at
# all — still a bare filesystem path, still not review prose.
assert_deny "case2-bare-path-no-at" \
  'gh api repos/acme/widgets/pulls/12/reviews --method POST -f event=COMMENT -f body="/Users/x/notes.md"'

# Case 3: gh pr review's direct --body flag given an @path value — --body is
# a verbatim flag (like -f), so this posts the literal "@/tmp/x.md" string.
assert_deny "case3-pr-review-body-at-path" \
  'gh pr review 12 --body @/tmp/x.md'

# Case 4: gh pr comment's --body flag given a bare path with no @.
assert_deny "case4-pr-comment-body-bare-path" \
  'gh pr comment 12 --body /tmp/notes.md'

# Case 5: comments[][body]=@path under -f (raw-field, verbatim) on a PR
# comments POST.
assert_deny "case5-comments-body-raw-field-at" \
  'gh api repos/acme/widgets/pulls/12/comments --method POST -f "comments[][body]=@/tmp/f.txt"'

# Case 5b: "@" + a relative path with a directory separator, no leading
# path-marker char and no ".", still a file reference (single whitespace-free
# token containing "/").
assert_deny "case5b-body-at-relative-subdir" \
  'gh pr review 12 --body @sub/dir/review.md'

# Case 5c: "@" + a lone filename with an extension, no directory separator —
# still a single-token file reference, not a mention (mentions don't end in
# a file extension).
assert_deny "case5c-body-at-lone-file-extension" \
  'gh api repos/acme/widgets/pulls/12/reviews --method POST -f event=COMMENT -f body=@review.md'

# Case 5d: a quoted lone bare path via --body must still deny (regression
# guard for the whole-token bare_path_re fix below — anchoring the whole
# value must not accidentally stop matching the exact lone-path shape).
assert_deny "case5d-quoted-lone-path-still-denies" \
  'gh pr review 12 --body "/Users/x/notes.md"'

# Case 5e: shell ANSI-C ($'...') quoting is transparent to the shell — gh
# sees the identical argument as the unquoted @path form — but our
# extraction reads source text, so the $'...' wrapper must be stripped
# before evaluating, or this slips past as an opaque "$..." token.
assert_deny "case5e-ansi-c-quoted-at-path" \
  $'gh api repos/acme/widgets/pulls/12/reviews --method POST -f event=COMMENT -f body=$\'@/tmp/foo.md\''

# Case 5f: same ANSI-C-quoting bypass, this time wrapping a bare path with no
# @ prefix.
assert_deny "case5f-ansi-c-quoted-bare-path" \
  $'gh api repos/acme/widgets/pulls/12/reviews --method POST -f event=COMMENT -f body=$\'/Users/x/notes.md\''

# ---- Allow cases (KNOWN-GOOD) ------------------------------------------------

# Case 6: a real, self-contained prose review body.
assert_allow "case6-real-prose-body" \
  'gh api repos/acme/widgets/pulls/12/reviews --method POST -f event=COMMENT -f "body=Great work, please rename the variable to camelCase and add a test."'

# Case 6b: prose that merely OPENS with a path-shaped token must pass — the
# bare-path rule denies only a value that IS a path in its entirety, not one
# that starts like one. False positive caught live before this guard shipped.
assert_allow "case6b-prose-opens-with-absolute-path" \
  'gh pr review 12 --body "/internal/v1/quotes endpoint now returns 200, LGTM"'

# Case 6c: same false-positive shape with a "./"-relative path opener.
assert_allow "case6c-prose-opens-with-relative-path" \
  'gh pr review 12 --body "./scripts/build-binaries.sh is stale, please rerun it"'

# Case 6d: same shape again via a typed (-F) field — the whole-token fix
# must apply there too, since is_deny_value's bare-path branch is shared by
# both raw and typed values.
assert_allow "case6d-prose-opens-with-path-typed-field" \
  'gh api repos/acme/widgets/pulls/12/reviews --method POST -F body="/api/foo returns 200 now, looks correct"'

# Case 7: --body-file posts the file's CONTENTS — the sanctioned idiom.
assert_allow "case7-body-file" \
  'gh pr review 12 --body-file /tmp/review-body.md'

# Case 8: -F body=@- (typed field, sanctioned file/stdin read form).
assert_allow "case8-typed-field-at-stdin" \
  'gh api repos/acme/widgets/pulls/5203/reviews -X POST -F body=@- -f event=COMMENT'

# Case 9: -F 'comments[][body]=@/tmp/f.txt' (typed field, sanctioned).
assert_allow "case9-typed-field-comments-body-at-file" \
  "gh api repos/acme/widgets/pulls/12/comments --method POST -F 'comments[][body]=@/tmp/f.txt'"

# Case 10: a short but legitimate inline comment — never block on shortness
# alone.
assert_allow "case10-short-legit-comment" \
  'gh pr comment 12 --body "Nit: rename to fooBar"'

# Case 10b: a real review reply that OPENS with a GitHub @mention followed by
# prose. This is the corrected-predicate case: jbarneson replied exactly this
# shape on midgard #5203 ("@matt-evanoff Considered, declining. See the
# code."), and an earlier draft of this guard's predicate false-positived on
# it (any raw @-prefixed value denied, no mention carve-out). A mention has
# whitespace after the handle — a file reference never does.
assert_allow "case10b-mention-then-prose-raw-field" \
  'gh api repos/acme/widgets/pulls/12/reviews --method POST -f event=COMMENT -f "body=@matt-evanoff Considered, declining."'

# Case 10c: same shape via gh pr review's direct --body flag.
assert_allow "case10c-mention-then-prose-direct-body" \
  'gh pr review 12 --body "@erlloyd please take a look"'

# Case 10d: a LONE @mention with no trailing prose at all — still not a file
# reference (no path marker, no "/", no file extension).
assert_allow "case10d-lone-mention" \
  'gh pr review 12 --body "@erlloyd"'

# Case 11: a non-gh Bash command is entirely out of scope.
assert_allow "case11-non-gh-command" \
  'rm -rf /tmp/scratch-dir'

# Case 12: a gh READ (no POST method, no pr review/comment subcommand) must
# never be touched by this guard.
assert_allow "case12-gh-read" \
  'gh api repos/acme/widgets/pulls/5203/reviews --jq ".[].body"'

# Case 13: gh pr view (unrelated pr subcommand) passes.
assert_allow "case13-gh-pr-view" \
  'gh pr view 12'

# Case 14: malformed JSON -> silent no-op, allow.
call "not json"
[ "$status" -eq 0 ] \
  || { echo "FAIL case14-malformed-json: expected exit 0, got $status"; exit 1; }
[ -z "$out" ] \
  || { echo "FAIL case14-malformed-json: expected silent allow, got: $out"; exit 1; }

# Case 15: non-Bash tool -> out of scope, allow.
call '{"tool_name":"Write","tool_input":{"command":"gh pr review 12 --body @/tmp/x.md"}}'
[ "$status" -eq 0 ] \
  || { echo "FAIL case15-non-bash-tool: expected exit 0, got $status"; exit 1; }
[ -z "$out" ] \
  || { echo "FAIL case15-non-bash-tool: expected silent allow, got: $out"; exit 1; }

# Case 16: missing jq -> exit 0, no stdout. Never break a Bash call on hook
# error.
PATH_NO_JQ=$(mktemp -d)
trap 'rm -rf "$PATH_NO_JQ"' EXIT
for tool in bash cat printf mkdir rm; do
  real=$(command -v "$tool" 2>/dev/null || true)
  [ -n "$real" ] && ln -sf "$real" "$PATH_NO_JQ/$tool"
done
payload=$(make_payload 'gh pr review 12 --body @/tmp/x.md')
out=$(PATH="$PATH_NO_JQ" bash -c "printf '%s' '$payload' | \"$SCRIPT\"" 2>/dev/null) && status=0 || status=$?
[ "$status" -eq 0 ] \
  || { echo "FAIL case16-missing-jq: expected exit 0, got $status"; exit 1; }
[ -z "$out" ] \
  || { echo "FAIL case16-missing-jq: expected silent allow, got: $out"; exit 1; }
trap - EXIT
rm -rf "$PATH_NO_JQ"

# ---- Wiring assertion -------------------------------------------------------
# A script-only test stays green when the hook is never wired up. Assert the
# script is registered in hooks.json under a PreToolUse "Bash" matcher.
command -v jq >/dev/null 2>&1 || { echo "FAIL wiring: jq not available to verify hooks.json"; exit 1; }
matched=$(jq -r '
  .hooks.PreToolUse[]
  | select(.matcher == "Bash")
  | .hooks[]
  | select(.command | test("block-bare-path-pr-comment\\.sh$"))
  | .command
' "$HOOKS_JSON")
[ -n "$matched" ] \
  || { echo "FAIL wiring: block-bare-path-pr-comment.sh not registered under a PreToolUse \"Bash\" matcher in $HOOKS_JSON"; exit 1; }

echo "PASS"
