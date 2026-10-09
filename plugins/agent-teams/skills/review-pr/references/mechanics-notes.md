# Why these review-pr mechanics work the way they do

Background for reviewer learning self-fetch and review-body file-content
handling. SKILL.md keeps the actionable
commands and format inline; this file holds the rationale.

## Reviewer learning self-fetch: why the SubagentStart hook can't fetch it

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
