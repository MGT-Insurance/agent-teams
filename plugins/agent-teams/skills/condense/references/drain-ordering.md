# Why drain-after-write is load-bearing

Reference material for `condense/SKILL.md`. The ordering RULE ("Ordering is load-bearing") and its "do not tidy this ordering back" warning are stated inline there, verbatim. This file is the supporting mechanism and the rejected alternatives, kept for anyone reconsidering the ordering rather than deleted when the inline text was trimmed for length.

## The mechanism the ordering protects

The packet discriminates tiers by the prefix each STORE key carries at read time: `<role>:hot:*` and `<role>:fresh:*` entries ship with FULL bodies; bare cold keys ship as key + a one-line summary (the packet then re-emits each key role-relative — see the field notes under the JSON sample in SKILL.md). `ateam fresh-drain` rewrites every `<role>:fresh:<slug>` to a bare `<role>:<slug>` and prints only a count — it never emits the key list.

## Printing the key list would not rescue drain-first

Teaching the drain to print its key list would not rescue a drain-first order, so do not propose it as a fix: knowing the names does not restore the bodies, which are elided from the packet either way, and you would be back to N round-trips of `ateam recall` to read what a correctly-ordered run hands you for free.

## Unconditional draining is still correct for promoted entries too

`ateam fresh-drain` does not discriminate within the fresh tier, and does not need to: it drains the whole tier unconditionally. Promotion already wrote a *separate* `<role>:hot:<slug>`, leaving the fresh source untouched, so that source lands in cold either way — which is what "LEAVE IN COLD any learning not promoted" already prescribes, and it also leaves the raw pre-distillation source in cold for anything you did promote.

## Why the batch-then-drain discipline in "Design the hot set" still holds

The batch-before-write rule used to be justified by a gap: the drain ran first, so fresh was empty during design and a partial hot set would under-serve the next session. That gap no longer exists — the drain now runs after the batch write, so fresh stays populated and `ateam learnings <role>` (hot ∪ fresh) keeps serving the un-curated material throughout design; no session is served less than it was before the run started, at any point. The discipline is kept anyway for two reasons that do still hold: writing hot keys mid-design changes the set you are reasoning about while you reason about it, and an interrupted partial batch leaves hot half-restructured — merged umbrella entries sitting alongside the very sources they were meant to replace.
