# Why these review-pr mechanics work the way they do

Background for step 7's subagent self-fetch, step 9's temp-file-not-path
rule, and step 10's completion line. SKILL.md keeps the actionable commands
and format inline; this file holds the rationale.

## Step 7: why the SubagentStart hook can't fetch learnings for the reviewer

The reviewer subagent is told to run `ateam learnings reviewer` itself,
bare, rather than have the SubagentStart hook do it and hand the result
over. That's not a style choice — SubagentStart's stdout is never rendered
into a spawned agent's context, at any size, so anything the hook printed
there would reach nobody. The hook only runs `ateam pull`, which refreshes
the local data so the reviewer's own self-fetch reads current state; it
cannot substitute for the self-fetch.

## Body quoting: why `-f body=@<file>` is dangerous

`gh api` and `gh pr review` treat `-f`/`--raw-field`/`--body` as literal
string values — including a value that starts with `@`. Only `-F body=@<file>`
(or `--body-file <file>`) tells `gh` to read the file's contents. Use the
wrong flag and `gh` posts the literal path text (e.g. `/tmp/review-body.txt`)
as the review body, not the file's contents — this is exactly how midgard
#5203 shipped a review whose entire body was a local file path, silently,
because the post itself succeeded.

A PreToolUse guard now denies a bare-path/`@path` review or comment body
outright, so a slip here fails loudly instead of posting silently. But the
guard is a backstop, not a substitute for using the right flag in the first
place.

## Completion line: why the separator and variable handling are strict

`TITLE_SEG` must be built as its own variable (`" — "` plus the title, or the
empty string) and substituted whole — never spliced into a hardcoded
`"%s — %s"` format string. Two reasons:

- The separator is a space, an em dash (**U+2014**), and a space. An en dash
  or a plain hyphen typed by hand looks right in a diff review but is the
  wrong character, and nothing catches the difference visually.
- `ateam dispatch` builds the "Review started" line using this identical
  rule. If the two constructions drift (one uses the frozen `TITLE_SEG`
  variable, the other reconstructs the separator inline), the "Review
  started" and "Review complete" lines for the same PR can end up with
  visibly different formatting, which reads as a bug in the notify topic
  even though nothing else is wrong.

Copying the separator character out of the code block (rather than retyping
it) sidesteps both failure modes.
