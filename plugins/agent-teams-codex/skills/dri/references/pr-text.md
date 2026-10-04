# PR text — the outside-reader rule, worked

The rule itself lives in `SKILL.md` under Phase 5. This file holds the reasoning and
worked before/after specimens. If this file and `SKILL.md` ever disagree, `SKILL.md`
wins.

## Why a PR is not like anything else the DRI writes

Every other artifact — beads, registry notes, memories, gate messages — is written for
someone holding the same context you are. A PR is the only one read by people who hold
none of it: no bead DB, no initiative registry, no `ateam` on their PATH, often no
memory of the dispatch. Six months later that includes you. The failure this rule
prevents is register bleed: by Phase 5 the DRI has spent five phases writing to
readers who can resolve `agent-teams-ully.9` with one command, and the PR body arrives
still in that voice — nothing looks wrong from the inside, because the subject of the
sentence is a token the outside reader cannot look up.

## The rule, with its edges

- **NEVER** in a PR title, and **NEVER** in a heading at any level.
- **NOT** woven through prose, and never the subject of a sentence the reader must
  understand — not as subject, not as possessive, not in passing.
- **FINE** in a trailer, a footnote, a table cell, or an end-of-line parenthetical:
  places a reader can skip without losing the sentence.
- **Describe the WORK, not the ticket** — "Fixes silently dropped replies", not
  "implements agent-teams-ully.7".

The permitted shape is the one the house already teaches for memory bodies at seven
other sites (`agents/{implementer,planner,tester,reviewer}.md`,
`skills/dri/references/memory.md`, `skills/steward/SKILL.md`, `skills/condense/SKILL.md`):
provenance as **a bare initiative-id parenthetical only — no narrative retelling**. Same
discipline, new surface.

An id is never load-bearing. Anything it would have told the reader — what changed,
why, what is deferred — has to be in the prose anyway. Deleting the id and losing
meaning means the meaning was never written down.

## Worked specimens — PR #139

`MGT-Insurance/agent-teams` #139 is the fixture for judging the rule — instructive
because of *where* it failed: title clean, all six headings clean, yet every one of
its 14 identifier occurrences sat in running prose. "Not in the title, not in a
heading" alone would have prevented none of them.

**1. An id as the bolded subject of the sentence.**

> Initiative **at-jno7** has the correct value on line 2 and a prose line at line 49
> redefining it with backticks around the path.

> One live initiative's bead description carried the correct value on line 2 and,
> 47 lines further down, a briefing sentence shaped like a field line that redefined
> the same key with backticks around the path.

The id was doing no work: "one live initiative" says everything the reader needs, and
the failure is now legible without a lookup.

**2. An id as a possessive, mid-sentence.**

> Measured by running the real `claimsInitiativeLocally` against at-jno7's live
> description as stored right now (both readers, same bytes):

> Measured by running the real `claimsInitiativeLocally` against that stored
> description exactly as it exists right now — both readers, same bytes:

"Not the subject" is not enough of a rule. This one is a modifier and still stops the
reader cold, which is why the prose clause has to cover *any* position.

**3. A bare suffix shorthand as the subject — the worst shape of all.**

> `.9` puts them behind a narrow `ateam` verb so the Go component is their only
> reader too.

> A filed follow-up puts the four shell hooks behind a narrow `ateam` verb, so the
> Go component becomes their only reader too.

`.9` is unresolvable even by a reader who *does* hold the bead DB, because the prefix
only exists a few paragraphs earlier. Bare suffix shorthand (`.9`, `.10`, `.2`) never
belongs in a PR body in any position.

### The one occurrence that COMPLIES

> | 4 shell hooks — `wake-watcher.sh:66`, `inbox-drain.sh:49`, `session-start-inbox.sh:55`, `compact-recovery.sh:27` | ❌ each hand-rolls the rule in jq; deferred to `agent-teams-ully.9` |

The row names the surface and what's wrong with it in the reader's own terms. The id
is the last token of a table cell — provenance, not content — and deleting it costs
the reader nothing. That's the permitted placement, and it's what makes this row
distinguishable from the three specimens above: a judgment about placement and
grammatical load, not a ban on the characters.

### One borderline, resolved

> Open follow-ups: `agent-teams-ully.9` (shell hooks behind the component), `.10`
> (repo-scan guard), `.2` (duplicate-key check at register time), `.3` (end-to-end
> poisoned-description test).

A closing list of follow-ups is trailer position, and every id there is glossed with
what it covers, so the line is permitted — but `.10`, `.2`, `.3` are still bare
suffixes, the one defect the rule forbids outright. Write each id in full, or drop the
ids and keep the glosses.
