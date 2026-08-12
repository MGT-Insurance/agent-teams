#!/usr/bin/env bash
# PreToolUse hook: deny a Bash call that posts a GitHub PR review/comment
# whose body is a bare local file path or an unresolved @path reference,
# instead of the review's actual prose.
#
# WHY (mechanical root cause, proven from artifacts): reviewers have
# repeatedly posted PR comments whose body is just a local file path.
# midgard #5203: a submitted PR review, state=COMMENTED, body LITERALLY
# `@/Users/ericlloyd/.claude/jobs/77164d6f/tmp/review-body-5203.md`. #5163:
# review body was a lone local path. #4988: the `gh api -f body=@file` / `@-`
# variant. The reviewer writes its full review to a temp file, then posts the
# file PATH (or an @path reference gh's raw-field flag passes verbatim)
# instead of the file CONTENTS. A reviewer-role LEARNING already told
# reviewers to reject bare-path bodies and it was in-context on #5203 and
# still failed — a reminder cannot stop a mechanical slip. This hook is the
# machine-enforced guard.
#
# THE @-PREFIX MECHANISM: gh's `-f`/`--raw-field` (and a bare `--body`) treat
# their value as a literal string — `-f body=@/tmp/x.md` posts the literal
# text "@/tmp/x.md", not the file's contents. gh's `-F`/`--field` treats a
# value starting with `@` specially and reads the referenced file's contents
# (`@-` reads stdin) — that is the CORRECT, sanctioned way to post a body
# from a file, alongside `gh pr review <pr> --body-file <file>`.
#
# DENY predicate — ALL of:
#   tool_name == "Bash"
#   AND the command is a GitHub PR comment/review POST: contains gh AND
#       matches one of pulls/<n>/reviews, pulls/<n>/comments,
#       issues/<n>/comments (with --method POST / -X POST), OR `gh pr
#       review`, OR `gh pr comment`.
#   AND the resolved body value is non-self-contained:
#     (a) a raw-field/direct value (-f body=…, --raw-field body=…, --body …,
#         or comments[][body]=… under -f/--raw-field) that STARTS WITH @
#         (gh passes this verbatim — the literal-path footgun); OR
#     (b) any body value (regardless of flag) that IS, in its ENTIRETY, a
#         single whitespace-free bare path token with no surrounding prose:
#         ^(/|\./|\.\./|~/|file://)[^[:space:]]*$ — anchored at both ends, so
#         prose that merely OPENS with a path-shaped word still passes.
#   EXCEPT: a typed-field value (-F/--field) starting with @ is the
#   sanctioned file-read form and always passes, even if what follows @ is a
#   plain path — that's the whole point of -F.
#
# False-positive guardrails: never block on shortness alone (a real "Nit:
# rename to fooBar" body must pass); never block gh reads (no POST method /
# not `pr review`/`pr comment`); never block non-gh Bash; `--body-file
# <file>` and `-F body=@<file>` / `-F body=@-` (correct file-read forms)
# always pass.
set -euo pipefail

command -v jq >/dev/null 2>&1 || exit 0

# Read the PreToolUse hook payload from stdin.
payload=$(cat)

tool_name=$(printf '%s' "$payload" | jq -r '.tool_name // empty' 2>/dev/null || true)
[ "$tool_name" = "Bash" ] || exit 0

command=$(printf '%s' "$payload" | jq -r '.tool_input.command // empty' 2>/dev/null || true)
[ -n "$command" ] || exit 0

# ---- Step 1: is this a GitHub PR comment/review POST? ---------------------

gh_present_re='(^|[^A-Za-z0-9_-])gh([^A-Za-z0-9_-]|$)'
path_re='(pulls/[0-9]+/(reviews|comments)|issues/[0-9]+/comments)'
method_post_re='(--method[[:space:]]+POST|-X[[:space:]]*POST)'
pr_review_re='gh[[:space:]]+pr[[:space:]]+review([[:space:]]|$)'
pr_comment_re='gh[[:space:]]+pr[[:space:]]+comment([[:space:]]|$)'

is_gh=0
[[ "$command" =~ $gh_present_re ]] && is_gh=1
[ "$is_gh" -eq 1 ] || exit 0

is_post=0
if [[ "$command" =~ $path_re ]] && [[ "$command" =~ $method_post_re ]]; then
  is_post=1
elif [[ "$command" =~ $pr_review_re ]]; then
  is_post=1
elif [[ "$command" =~ $pr_comment_re ]]; then
  is_post=1
fi
[ "$is_post" -eq 1 ] || exit 0

# ---- Step 2: extract candidate body values ---------------------------------

KEY_RE='body|comments\[\]\[body\]'

# match_kv FLAG_RE KEY_RE: find FLAG (e.g. "-f|--raw-field") followed by a
# KEY=VALUE argument. Two independent things can be quoted (or not), so try
# both quoting shapes: the WHOLE "key=value" token wrapped in quotes (e.g.
# -F 'comments[][body]=@/tmp/f.txt'), and just the VALUE wrapped in quotes
# with an unquoted key= before it (e.g. -f body="/Users/x/notes.md"), then
# a fully bare key=value with no quotes at all. Prints the value on stdout
# and returns 0 on the first match; returns 1 with no output otherwise.
match_kv() {
  local flag_re="$1" key_re="$2" re
  # Whole "key=value" token double-quoted.
  re="(^|[^A-Za-z0-9_-])(${flag_re})[[:space:]]+\"(${key_re})=([^\"]*)\""
  if [[ "$command" =~ $re ]]; then printf '%s' "${BASH_REMATCH[4]}"; return 0; fi
  # Whole "key=value" token single-quoted.
  re="(^|[^A-Za-z0-9_-])(${flag_re})[[:space:]]+'(${key_re})=([^']*)'"
  if [[ "$command" =~ $re ]]; then printf '%s' "${BASH_REMATCH[4]}"; return 0; fi
  # key= bare, value double-quoted.
  re="(^|[^A-Za-z0-9_-])(${flag_re})[[:space:]]+(${key_re})=\"([^\"]*)\""
  if [[ "$command" =~ $re ]]; then printf '%s' "${BASH_REMATCH[4]}"; return 0; fi
  # key= bare, value single-quoted.
  re="(^|[^A-Za-z0-9_-])(${flag_re})[[:space:]]+(${key_re})='([^']*)'"
  if [[ "$command" =~ $re ]]; then printf '%s' "${BASH_REMATCH[4]}"; return 0; fi
  # Fully bare key=value.
  re="(^|[^A-Za-z0-9_-])(${flag_re})[[:space:]]+(${key_re})=([^[:space:]]*)"
  if [[ "$command" =~ $re ]]; then printf '%s' "${BASH_REMATCH[4]}"; return 0; fi
  return 1
}

# match_direct_body: find a bare "--body VALUE" (never "--body-file", which
# requires a space right after --body to match at all). Same tri-form.
match_direct_body() {
  local re
  re='--body[[:space:]]+"([^"]*)"'
  if [[ "$command" =~ $re ]]; then printf '%s' "${BASH_REMATCH[1]}"; return 0; fi
  re="--body[[:space:]]+'([^']*)'"
  if [[ "$command" =~ $re ]]; then printf '%s' "${BASH_REMATCH[1]}"; return 0; fi
  re='--body[[:space:]]+([^[:space:]]*)'
  if [[ "$command" =~ $re ]]; then printf '%s' "${BASH_REMATCH[1]}"; return 0; fi
  return 1
}

# Whole-token, both-ends-anchored: a bare path is the ENTIRE value with no
# internal whitespace, not merely a value that starts with a path marker.
# Anchoring only at the start would deny real prose that opens with a path
# ("/internal/v1/quotes endpoint now returns 200, LGTM") — a false positive
# caught live before this guard shipped.
bare_path_re='^(/|\./|\.\./|~/|file://)[^[:space:]]*$'

# Shell ANSI-C / $-quoting ($'...' or $"...") is transparent to the shell —
# `-f body=$'@/tmp/x.md'` and `-f body=@/tmp/x.md` are the same argument once
# the shell parses them — but our extraction reads the SOURCE TEXT, so the
# $'...' wrapper survives into the captured value unless stripped explicitly.
# Without this, a $'...'-wrapped bare path or @path slips past every check
# above and below, since "$'@/tmp/x.md'" itself matches neither the @-prefix
# check (it starts with "$", not "@") nor the bare-path regex (starts with
# "$", not "/"). Unwrap before evaluating.
dollar_squote_re='^\$'\''(.*)'\''$'
dollar_dquote_re='^\$"(.*)"$'

unwrap_dollar_quote() {
  local val="$1"
  if [[ "$val" =~ $dollar_squote_re ]]; then
    printf '%s' "${BASH_REMATCH[1]}"
    return 0
  fi
  if [[ "$val" =~ $dollar_dquote_re ]]; then
    printf '%s' "${BASH_REMATCH[1]}"
    return 0
  fi
  printf '%s' "$val"
}

# is_deny_value VALUE IS_TYPED: 0 if VALUE is a non-self-contained body per
# the predicate above, 1 (pass) otherwise. IS_TYPED="typed" means the value
# arrived via -F/--field, where a leading @ is the sanctioned file-read form
# (always allowed, whatever follows the @).
#
# For a raw-field/--body value starting with @, a leading @ alone is NOT
# enough to deny: a real review reply routinely opens with a GitHub @mention
# ("@matt-evanoff Considered, declining." — this is how jbarneson replied on
# midgard #5203 itself). Deny only when what follows @ is actually a file
# reference:
#   1. a path marker right after the @ (/, ./, ../, ~/, file://); OR
#   2. the value is exactly "@-" (stdin marker); OR
#   3. the value has no whitespace anywhere AND (contains "/" OR ends in a
#      file extension like .md/.txt) — a single-token @path/@file.ext.
# A bare "@handle" or "@handle" followed by prose (i.e. it has whitespace)
# is a mention, not a path, and passes.
is_deny_value() {
  local value="$1" is_typed="$2"
  [ -n "$value" ] || return 1
  if [[ "$value" == @* ]]; then
    [ "$is_typed" = "typed" ] && return 1
    local rest="${value#@}"
    [[ "$rest" =~ ^(/|\./|\.\./|~/|file://)[^[:space:]]*$ ]] && return 0
    [ "$value" = "@-" ] && return 0
    if [[ "$value" != *[[:space:]]* ]]; then
      if [[ "$rest" == */* ]] || [[ "$rest" =~ \.[A-Za-z0-9]+$ ]]; then
        return 0
      fi
    fi
    return 1
  fi
  [[ "$value" =~ $bare_path_re ]] && return 0
  return 1
}

deny=1
v=""
if v=$(match_kv '-f|--raw-field' "$KEY_RE"); then
  v=$(unwrap_dollar_quote "$v")
  is_deny_value "$v" raw && deny=0
fi
if [ "$deny" -ne 0 ]; then
  if v=$(match_kv '-F|--field' "$KEY_RE"); then
    v=$(unwrap_dollar_quote "$v")
    is_deny_value "$v" typed && deny=0
  fi
fi
if [ "$deny" -ne 0 ]; then
  if v=$(match_direct_body); then
    v=$(unwrap_dollar_quote "$v")
    is_deny_value "$v" raw && deny=0
  fi
fi

[ "$deny" -eq 0 ] || exit 0

DENIAL_MSG="BLOCKED: this PR review/comment body looks like a bare local file path or an unresolved @path reference (found: '$v'), not review prose - GitHub reviewers cannot open a path on your machine (see midgard #5203, where a submitted review's entire body was one such path). Put the actual review text in the body. To post a long or multiline body from a file, post the file's CONTENTS: 'gh pr review <pr> --body-file <file>' OR 'gh api ... -F body=@<file>' / '-F body=@-'. NEVER use '-f body=@<file>', '--raw-field body=@<file>', or '--body @<file>' - those post the literal path string, not the file's contents."

jq -n \
  --arg msg "$DENIAL_MSG" \
  '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":$msg}}'

exit 0
