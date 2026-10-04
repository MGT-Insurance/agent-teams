# Verifying a cold demotion with `ateam recall`

The spot-check in SKILL.md's curation step exists to prove one thing: the entry
you just demoted is actually reachable at the key you wrote. That proof is the
**match count**, not the presence of output. This file records why.

## Why a descriptive query proves nothing

`recall` splits the query on whitespace, lowercases each token, and counts an
entry as a match when **any single token** appears anywhere in its key or body.
Results rank by how many distinct tokens hit, tie-broken by key ascending.

So a plausible-sounding phrase matches almost everything. Measured against the
live store:

```
$ ateam recall dri "worktree cwd discipline for agents"
[recall dri "worktree cwd discipline for agents": 218 matches]
dri:always-address-git-with-dash-C          <- unrelated to the query
...
```

**218 of 243 entries**, led by an entry that has nothing to do with the query —
common tokens like `for` and `agents` appear in most bodies. Output appeared and
the top hit looked plausible, so the check reads as a pass. It proved nothing.

Passing the entry's own key instead gives an unambiguous answer:

```
$ ateam recall dri "dri:agent-vs-human-persona-ambiguity"
[recall dri "dri:agent-vs-human-persona-ambiguity": 1 matches]
dri:agent-vs-human-persona-ambiguity
```

The full `<role>:<slug>` and the bare slug each return exactly `1 matches`. That
is the outcome to look for, and nothing else is.

## The `nearest:` line is not a "did you mean"

On zero matches `recall` prints a `nearest:` list of up to five keys. It wears
the shape of a suggestion and is not one.

The branch only runs when **every** candidate scored zero, so the score
comparison never discriminates and the tie-break — key ascending — fully
determines the output. It is the alphabetically first keys of that role,
identical for every failing query:

```
$ ateam recall dri "zzqqxx"   -> nearest: dri:agent-vs-human-persona-ambiguity dri:always-address-git-with-dash-C ...
$ ateam recall dri ""         -> nearest: <byte-identical list>
```

Two unrelated queries returning identical "suggestions" is the proof. Do not
read a `nearest:` entry as related to what you searched for. Tracked as
`agent-teams-a8nn` against the initiative that owns the recall surface.

## Historical note

Before `ateam` gained ranked recall, `recall` was a single literal substring
match, and a descriptive query failed the opposite way: empty output at exit 0.
The instruction to pass the key verbatim was correct then and is correct now,
but the failure mode inverted — from a false negative you would notice to a
false positive you would not. If you find prose anywhere claiming a descriptive
query "returns empty", it predates that change.
