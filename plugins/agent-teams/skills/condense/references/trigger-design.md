# Why the fresh-tier trigger is designed this way

Reference material for `condense/SKILL.md`. The RULES this backs are stated inline there, verbatim; this file is the argument for them, kept for anyone reconsidering the design rather than deleted when the inline text was trimmed for length.

## Do not recompute the verdict

The reason `ateam condense-check`'s printed verdict must never be recomputed by hand is not that a model computes the arithmetic badly. Commit dc77c80 rewrote the budget line in `condense/SKILL.md` and left a contradictory byte gloss sitting inside the very clause it was editing, while a second stale threshold elsewhere in that same file went unrevisited entirely. Prose restatements of a constant desynchronise from it; a printed verdict cannot. A hand-sweep of SKILL.md is the fix that has already been tried and already failed.

(Those particular stale figures are gone now. This is the precedent for the rule, not a live pointer at a bug.)

## Why total size cannot be a trigger

A trigger has to be CLEARABLE by the action it triggers. A condense run does drain fresh and re-curate hot, but it lands only about a thousand tokens under the old union ceiling and re-arms within roughly two sweeps — so a total-size ceiling fired at every wind-down, on material that had already been curated, forever. Apply the same clearability test to any future proposal to reinstate a size-based trigger.

## Direct `hot:` writes bypass the trigger

The fresh-tier trigger is complete only because normal contribution routes to fresh. `ateam learn <role> <slug>` with a BARE slug falls through to `<role>:fresh:<slug>` (`learnKey`, `internal/verbs/write.go:78`), and that DEFAULT is what makes a fresh-tier trigger see every accumulation. But `ateam learn <role> hot:<slug>` writes straight to hot, bypassing fresh entirely (`internal/verbs/write.go:75-77`), and the tier prefix is ADVERTISED in public help (the `learn` verb's slug flag: "prefix with `hot:`, `fresh:`, or `cold:` to target a tier") — a first-class documented affordance, not an internal path, and nothing in the CLI restricts it to condense. Today the only caller using it is the condense instruction contract in `internal/verbs/kong_converted.go`, i.e. this procedure itself. A direct `hot:` write by anything other than condense bypasses the trigger and is invisible to it — the gate holds by convention, not by construction: add a code path that writes `hot:` directly and you have silently broken it, with no failure to observe.
