# PR text — the outside-reader rule, worked

The rule itself lives in `SKILL.md` under Phase 5. This file holds the reasoning and
one worked before/after specimen. If this file and `SKILL.md` ever disagree,
`SKILL.md` wins.

## Why a PR is not like anything else the DRI writes

Every other artifact of an initiative is written for someone holding the same context
you are: beads for the team, notes for the registry, memories for the next session,
gate messages for a human who can open any of those. A pull request is the only one
read by people who hold none of it — a reviewer with no bead DB, no initiative
registry, no `ateam` on their PATH, and often no memory of the dispatch. Six months
later that includes you.

The failure this rule exists to prevent is not carelessness. It is register bleed. By
Phase 5 the DRI has spent five phases writing to readers who can resolve
`agent-teams-ully.9` with one command, and it arrives at the PR body still writing in
that voice. Nothing in the text looks wrong from the inside. From the outside, the
subject of the sentence is a token the reader cannot look up.

## The rule, with its edges

- **NEVER** in a PR title.
- **NEVER** in a heading, at any level.
- **NOT** woven through prose, and never the subject of a sentence the reader must
  understand — not as its subject, not as a possessive, not in passing.
- **FINE** in a trailer, a footnote, a table cell, or an end-of-line parenthetical:
  places a reader can skip without losing the sentence.
- **Describe the WORK, not the ticket** — "Fixes silently dropped replies", not
  "implements agent-teams-ully.7".

The permitted shape is the one the house already teaches for memory bodies at seven
sites (`agents/{implementer,planner,tester,reviewer}.md`, `skills/dri/references/memory.md`,
`skills/steward/SKILL.md`, `skills/condense/SKILL.md`): provenance as **a bare
initiative-id parenthetical only — no narrative retelling**. Same discipline, new
surface. There is no new vocabulary to learn here.

An id is never load-bearing. Anything it would have told the reader — what changed,
why, what is deferred — has to be in the prose anyway. Deleting the id and losing
meaning means the meaning was never written down.

## Worked specimen — PR #139

`MGT-Insurance/agent-teams` #139, "One component owns the initiative routing fields
(first-read-wins) — fixes silently dropped replies". This is the fixture for judging
the rule, and it is instructive because of *where* it failed. The title is clean. All
six headings are clean. Every one of its 14 identifier occurrences is in running
prose. A rule phrased only as "not in the title, not in a heading" would have
prevented none of them.

### BEFORE → AFTER

**1. An id as the bolded subject of the sentence.**

> Initiative **at-jno7** has the correct value on line 2 and a prose line at line 49
> redefining it with backticks around the path.

> One live initiative's bead description carried the correct value on line 2 and,
> 47 lines further down, a briefing sentence shaped like a field line that redefined
> the same key with backticks around the path.

The id was doing no work: "one live initiative" says everything the reader needs, and
the failure is now legible without a lookup.

**2. An id opening a sentence, carrying the whole claim.**

> at-jno7 is **deliberately left un-repaired** — it is the live repro Eric is
> currently watching with the `claims-debug-1` instrumentation, and it's the
> regression fixture (its byte-exact poison line is checked in at
> `internal/initiative/initiative_test.go:64`).

> **That record is deliberately left un-repaired** — it is the live reproduction
> still under observation, and its byte-exact poison line is checked in as the
> regression fixture at `internal/initiative/initiative_test.go:64`.

The emphasis moves off the token and onto the decision, which is the part a reviewer
might disagree with.

**3. An id as a possessive, mid-sentence.**

> Measured by running the real `claimsInitiativeLocally` against at-jno7's live
> description as stored right now (both readers, same bytes):

> Measured by running the real `claimsInitiativeLocally` against that stored
> description exactly as it exists right now — both readers, same bytes:

"Not the subject" is not enough of a rule. This one is a modifier and still stops the
reader cold, which is why the prose clause has to cover *any* position.

**4. An id in passing, inside the conclusion.**

> So no bead edit, no migration, no data repair is needed for at-jno7 or any other
> initiative poisoned this way.

> So no stored-record edit, no migration, and no data repair is needed for that
> initiative or any other poisoned the same way.

Reads identically to someone who holds the id, and is now readable by someone who
does not. That is the whole trade.

**5. A bare suffix shorthand as the subject.**

> `.9` puts them behind a narrow `ateam` verb so the Go component is their only
> reader too. It is filed, argued, and sequenced after this PR because it touches
> `internal/verbs/match.go` and cannot run beside this migration.

> A filed follow-up puts the four shell hooks behind a narrow `ateam` verb, so the
> Go component becomes their only reader too. It is sequenced after this PR because
> it touches `internal/verbs/match.go` and cannot run beside this migration.

The worst shape of all: `.9` is unresolvable even by a reader who *does* hold the bead
DB, because the prefix only exists a few paragraphs earlier. Bare suffix shorthand
(`.9`, `.10`, `.2`) never belongs in a PR body in any position.

### The one occurrence that COMPLIES

Not every id in #139 was wrong. This table row was right, and it is the positive half
of the rule:

> | 4 shell hooks — `wake-watcher.sh:66`, `inbox-drain.sh:49`, `session-start-inbox.sh:55`, `compact-recovery.sh:27` | ❌ each hand-rolls the rule in jq; deferred to `agent-teams-ully.9` |

The row names the surface and what is wrong with it in the reader's own terms. The id
is the last token of a table cell, it is provenance rather than content, and deleting
it costs the reader nothing. That is the permitted placement — and putting it beside
the five above is what makes the two distinguishable. The rule is a judgment about
placement and grammatical load, not a ban on the characters.

### One borderline, resolved

> Open follow-ups: `agent-teams-ully.9` (shell hooks behind the component), `.10`
> (repo-scan guard), `.2` (duplicate-key check at register time), `.3` (end-to-end
> poisoned-description test).

A closing list of follow-ups is trailer position, and every id there is glossed with
what it covers — so the line is permitted. It still carries the one defect the rule
forbids outright: `.10`, `.2` and `.3` are bare suffixes. Write each id in full, or
drop the ids and keep the glosses.
