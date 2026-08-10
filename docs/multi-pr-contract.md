# Multi-PR contract — frozen seam for agent-teams-ssib

Freezes the seam every downstream track (P, G, D1, D2) codes against, so none
of them independently diverge on the discriminator shape or the wire format.
Produced for bead `agent-teams-ssib.6`. Contract outranks any single bead: if
an implementer finds this frozen shape wrong, STOP and escalate to the DRI —
do not work around it.

## 1. Empirical keystone — what `bd label add` actually accepts

Run against a throwaway scratch `bd` database (`bd init` in an empty tmp dir,
never the project or global workspace DB). Every row below is a command that
was actually run; nothing here is taken from documentation or assumption.

**Bottom line up front: nothing the frozen grammar (§3) actually uses was
rejected.** Colon, slash, `#`, uppercase, and multiple colons in combination
were all accepted — repo-inclusion required zero substitution. The one
genuine rejection found (below) is a character the grammar never emits
anyway. Recorded per the planner's ask: separator/encoding was negotiable if
the probe forced a change; it didn't force one, but the full transcript is
here so the next reader doesn't have to re-probe to confirm that.

| input                                                          | accepted? |
|-----------------------------------------------------------------|-----------|
| `pr:test` (colon)                                                | yes |
| `erlloyd/pr-shepherd` (slash)                                    | yes |
| `pr#3` (hash)                                                    | yes |
| `PR-Test-UPPER` (uppercase)                                      | yes |
| `pr:erlloyd/pr-shepherd#3` (colon+slash+hash combined)           | yes |
| `pr:MGT-Insurance/midgard#4632` (uppercase+slash+hash combined)  | yes |
| `pr:erlloyd/pr-shepherd#3:gate` (multiple colons)                | yes |
| `gate:review:https://github.com/erlloyd/pr-shepherd/pull/3` (the actual frozen grammar, §3) | yes |
| `gate:question:https://github.com/MGT-Insurance/midgard/pull/4632` | yes |
| `pr:a,b#3` (comma)                                               | yes, but see hazard below |
| a literal space (`"gate review"`)                                | yes — accepted, not rejected; irrelevant to the grammar since it never emits one |
| a literal embedded newline (`"gate:review\nextra"`)              | yes — accepted AND round-trips through `bd label list` with the newline intact; irrelevant to the grammar since it never emits one |
| **empty string `""`**                                            | **REJECTED** — the one genuine rejection found; see exact command/output below |
| 255-char label                                                   | yes, stored verbatim (255 chars) |
| 256-char label                                                   | **silently truncated to 255 chars** — the CLI success message echoes the full untruncated 256-char string, but `bd label list` reads back only the first 255 chars. A 255-char and a 256-char input that share the same first 255 chars collapse into ONE stored label. |

Exact command + output for the two non-accept cases:

```
$ bd label add bd-label-scratch-yy3 "" --json
{
  "error": "label cannot be empty",
  "schema_version": 1
}
exit=1

$ bd label add bd-label-scratch-yy3 "$(python3 -c "print('a'*256)")" --json
[
  {
    "issue_id": "bd-label-scratch-yy3",
    "label": "aaaa...(256 a's, printed in full by bd)...aaaa",
    "status": "added"
  }
]
exit=0
$ bd label list bd-label-scratch-yy3 --json | python3 -c "
import json,sys
for l in json.load(sys.stdin):
    if l.startswith('aaa'): print(len(l))
"
255
```

The 256-char add call exits 0 and echoes the full 256-char string in its
`"label"` field — that field is an echo of the CLI's input argument, not a
read of what got stored. Only the follow-up `bd label list` read exposes the
truncation to 255.

Hazards (both already known from prior work, reconfirmed here):
- **Comma is the AND-separator in label filters** — a label value containing
  a comma is storable and round-trips on exact-match lookup, but is
  permanently unqueryable via a comma-joined filter list. The frozen grammar
  (§3) never emits a comma.
- **255-char hard truncation, success message lies.** Keep every emitted
  label well under 255 chars. A full `gate:<kind>:<PR URL>` label is ~55–75
  chars for realistic GitHub URLs — nowhere near the limit.

Round-trip and collision checks, both passing:
- `bd label list <id>` reads back every label above byte-exact.
- `bd list --label "gate:review:https://github.com/erlloyd/pr-shepherd/pull/3"`
  and `bd list --label "gate:question:https://github.com/MGT-Insurance/midgard/pull/4632"`
  each return exactly the one issue carrying that exact label — no
  cross-match between the two PRs.
- The two-repo case from at-d9ck (`erlloyd/pr-shepherd#3` and
  `MGT-Insurance/midgard#4632`) is the concrete case this was checked
  against — a same-numbered PR in two different repos is the live collision
  case a discriminator must survive by construction, which is why
  repo-inclusion is not optional even though this specific pairing happens to
  have different numbers too.

**Round-trip PARSE verification (not just storage round-trip).** Storing and
reading a label back byte-exact proves nothing about whether it still means
the same repo+number once parsed — so this checks that too, deliberately
against the *existing* parser rather than a new one: `prURLRE`
(`internal/verbs/route_match.go:24`,
`` `https?://github\.com/([^/\s]+)/([^/\s]+)/pull/(\d+)` ``) transcribed
verbatim into a standalone Python check (no code added to `internal/verbs` —
out of this bead's FILES). For each of the three per-PR labels actually
written above: strip the frozen `<base>:` prefix, run the transcribed regex
on the remainder, and confirm it recovers the exact owner/repo/number that
went in:

```
OK: label='gate:review:https://github.com/erlloyd/pr-shepherd/pull/3'
      stripped-suffix='https://github.com/erlloyd/pr-shepherd/pull/3'
      parsePrURL(suffix)=('erlloyd', 'pr-shepherd', 3)  want=('erlloyd', 'pr-shepherd', 3)

OK: label='gate:question:https://github.com/MGT-Insurance/midgard/pull/4632'
      stripped-suffix='https://github.com/MGT-Insurance/midgard/pull/4632'
      parsePrURL(suffix)=('MGT-Insurance', 'midgard', 4632)  want=('MGT-Insurance', 'midgard', 4632)

OK: label='external-review:https://github.com/erlloyd/pr-shepherd/pull/3'
      stripped-suffix='https://github.com/erlloyd/pr-shepherd/pull/3'
      parsePrURL(suffix)=('erlloyd', 'pr-shepherd', 3)  want=('erlloyd', 'pr-shepherd', 3)
```

All three round-trip to the exact repo+number written. This also confirms
the parse-back is unambiguous by construction, not just by luck: the only
separator inside the frozen grammar's suffix portion is the literal string
`/pull/` inside a URL `prURLRE` already anchors on, so stripping the
`<base>:` prefix can never leave an ambiguous remainder to parse.

## 2. The `pr` rail key (multi-valued, Description)

`pr` joins `session` and `track-worktree` in `multiValuedKeys`
(`internal/initiative/initiative.go`). `Fields.PRs []string` holds every
`pr: <url>` line's value in registration order. `JSONFields` emits it as an
array for free (no code change needed there — it already branches on
`multiValued(key)`).

**Value format: the full GitHub PR URL, verbatim** — e.g.
`https://github.com/erlloyd/pr-shepherd/pull/3`. This is not a new format
invented for this contract: it is dictated by the existing
`prURLRE`/`parsePrURL`/`extractPrURL` machinery in
`internal/verbs/route_match.go` (ported from `dashboard/server/src/parse.ts`
`extractPrUrl`), which Track P's `matchInitiative` rewrite must keep using
unchanged — iterating `Fields.PRs` instead of regex-scanning free text, not
reimplementing URL parsing.

**This is a *different* mechanism from the pre-existing `pr:` line the `dri`
skill writes into bd NOTES today** (`plugins/agent-teams/skills/dri/SKILL.md`,
"record the structured `pr:` field") — that line is read by a free-text regex
over Notes-then-Description and is NOT parsed through
`internal/initiative`'s field-line rule at all. Both happen to use the same
key text (`pr`) and the same value shape (a full URL) by coincidence, not by
design; migrating the DRI skill's note-writing step onto `ateam pr add` /
`WithPR` is available follow-up work, not settled by this contract.

It is also distinct from the pre-existing unmodeled `pr-number` / `pr-repo` /
`pr-url` trio (`plugins/agent-teams/skills/review-pr/SKILL.md`) — those
describe the single PR a *review-pr* initiative is reviewing; `pr` describes
the PR(s) a *DRI* initiative has opened. See `doc.go` frozen item 3 (amended).

**Writer:** `WithPR(iss bd.Issue, url string) (WritePlan, error)` — sibling
of `WithSession`/`WithTrack`, same append-only, idempotent shape, same
structural-only validation (non-empty, no line break, no leading/trailing
whitespace). It deliberately does NOT validate that `url` matches
`prURLRE` — that is the frozen grammar's job to state, enforced by a
caller with a reason to reject a malformed value (e.g. the `ateam pr add`
verb), not baked into the low-level writer, mirroring how `WithSession`
doesn't validate a session id looks like a real session and `WithTrack`
doesn't validate a path exists.

## 3. The per-PR gate label grammar (collision-safe, repo-inclusive)

Today's gate mechanism (`internal/verbs/status.go`, `query.go`) is
initiative-scoped: bare labels `gate:review`, `gate:question`, `human`,
`external-review` apply to the whole initiative bead, because there was only
ever one PR. Multi-PR needs these to become per-PR — clearing PR X's gate
must not touch PR Y's.

**Frozen grammar:**

```
per-PR label := <base> ":" <pr-url>
base ∈ { "gate:review", "gate:question", "external-review" }
pr-url := the exact string stored in that PR's "pr" rail line, byte-identical
          (a full https GitHub PR URL matching prURLRE)
```

Examples (the at-d9ck two-repo case):
```
gate:review:https://github.com/erlloyd/pr-shepherd/pull/3
gate:question:https://github.com/MGT-Insurance/midgard/pull/4632
external-review:https://github.com/erlloyd/pr-shepherd/pull/3
```

`human` is the one exception and stays a **bare, initiative-scoped** label,
unchanged — it means "at least one PR on this initiative needs human
attention," an aggregate flag, not per-PR data. (Whether `human` is set on
the first per-PR gate and cleared only when the last one clears is Track G's
implementation call within `clear-gate`'s conditional-wipe fix — this
contract freezes the label *shape*, not that algorithm.)

Matching rule for a reader: `strings.HasPrefix(label, base+":")`, then the
suffix after that exact prefix is the PR URL — recovered directly, no new
parser, since none of the three bases is a prefix of another. Collision
safety is structural: two PRs differ in owner and/or repo and/or number, so
their URLs differ byte-for-byte, so their labels differ (empirically proven
in §1, including the exact at-d9ck pairing).

## 4. Wire shape — `fields.pr[]` (list-json)

`ateam list-json`'s existing `"fields"` object (`internal/verbs/query.go`,
`withRoutingFields`) gets `pr` for free once `pr` is in `multiValuedKeys` — no
new code in `query.go` for this half. Absent case: key omitted entirely
(same rule as every other `JSONFields` key — "not set" is never confused
with "set to something empty").

```json
{
  "id": "at-d9ck",
  "title": "...",
  "fields": {
    "worktree": "/path/to/wt",
    "pr": [
      "https://github.com/erlloyd/pr-shepherd/pull/3",
      "https://github.com/MGT-Insurance/midgard/pull/4632"
    ]
  }
}
```

Single-PR initiative: `"pr": ["https://github.com/acme/widget/pull/12"]` —
still an array, length 1. No PRs opened yet: `"pr"` key absent from `fields`
entirely (not `"pr": []`).

## 5. Wire shape — the Go-computed per-PR review array (second wire element)

Mutable review state (which PR is gated, at what kind) cannot ride the
append-only `fields` rail — it changes on every gate/clear-gate/handoff call,
and `fields` is a projection of Description lines that only ever grow. It is
therefore computed in Go from the current label set (§3) and attached as a
**second, sibling wire element** — not nested inside `fields` — on both
`list-json` and `execution-status`.

**Key name:** `pr_reviews` (snake_case, matching every other Go-emitted key
at this level: `execution_status`, `issue_id`, `dependency_count` — `fields`
itself is the one exception, being a single word with no case choice to
make).

**Element shape**, one entry per PR present in `fields.pr[]` /
`Fields.PRs`:

```json
{
  "pr": "https://github.com/erlloyd/pr-shepherd/pull/3",
  "gate": "review"
}
```

`gate` is one of `"review"`, `"question"`, `"external"`, or `""` (empty
string — not omitted, not null) when that PR carries no per-PR gate label at
all. These four values are exactly `deriveExplicitGate`'s
`ExplicitGateKind | null` domain today (`dashboard/server/src/parse.ts:212`)
with `null` represented as `""` instead, for a uniform array element shape
(no per-entry optional field). Precedence when a PR somehow carries more
than one per-PR gate label — mirrors `deriveExplicitGate`'s existing order,
unchanged: `external` (handed off) outranks `review`; `question` outranks
both being handed off and not; i.e. check `question` first, then `external`,
then `review`. (Track G may find this precedence needs a tweak once it
implements the conditional clear-gate fix — the CONTRACT freezes the
element's *shape*, this precedence note is a carry-forward of existing
behavior, not new ground being broken here.)

**Full example**, one initiative with two PRs in different states:

```json
{
  "id": "at-d9ck",
  "title": "...",
  "worktree": "/path/to/wt",
  "labels": ["human", "gate:review:https://github.com/erlloyd/pr-shepherd/pull/3", "gate:question:https://github.com/MGT-Insurance/midgard/pull/4632"],
  "execution_status": "NEEDS-DECISION",
  "ask": null,
  "prs": [
    "https://github.com/erlloyd/pr-shepherd/pull/3",
    "https://github.com/MGT-Insurance/midgard/pull/4632"
  ],
  "pr_reviews": [
    { "pr": "https://github.com/erlloyd/pr-shepherd/pull/3", "gate": "review" },
    { "pr": "https://github.com/MGT-Insurance/midgard/pull/4632", "gate": "question" }
  ]
}
```

No PRs at all: `"prs": []` and `"pr_reviews": []` (both empty arrays, not
omitted — `execution-status`'s existing `initiativeStatus` struct has no
precedent for omitting array fields; `Labels`/`Sessions` etc. are always
present, possibly empty).

## 6. `execution-status` / `human-list` per-PR output shape

`internal/verbs/status.go`'s `initiativeStatus.PR string` (singular, sourced
from `extractPrURL(iss.Notes)` — the free-text scan) is REPLACED by:

```go
PRs       []string   `json:"prs"`        // from initiative.Of(iss).PRs — the rail, not a Notes regex-scan
PRReviews []PRReview `json:"pr_reviews"` // see §5
```

```go
// PRReview is one entry in the Go-computed per-PR review array (§5).
type PRReview struct {
    PR   string `json:"pr"`
    Gate string `json:"gate"` // "review" | "question" | "external" | ""
}
```

`execution_status` (singular, top-level) stays a single string on the
existing four-value scale (`NEEDS-DECISION` / `IN-PROGRESS` /
`REVIEWABLE` / `AWAITING-EXTERNAL-REVIEW`) — it is an initiative-level
rollup (rules 1, 2, and 4 of `computeExecutionStatus` don't reference gate
labels at all; only rule 3's sub-cascade is PR-specific), so it does not
decompose per PR the way `pr_reviews` does. The exact rollup algorithm when
different PRs disagree (e.g. one `review`, one `question`) is Track G's
implementation call, not frozen here — this contract only freezes that the
field stays a single string and that `pr_reviews` is where the granular
per-PR breakdown lives; D1/D2 render from `pr_reviews`, not from
`execution_status`, for per-PR detail.

**`human-list`** (`internal/verbs/query.go`, `humanListKong`) reshapes to one
output row **per gated PR**, not one row per initiative — an initiative with
two gated PRs prints two rows:

```
at-d9ck  [REVIEW]  <title>
    pr: https://github.com/erlloyd/pr-shepherd/pull/3
at-d9ck  [QUESTION]  <title>
    pr: https://github.com/MGT-Insurance/midgard/pull/4632
```

(Note text and ask-block rendering per row, same as today, omitted above for
brevity.) This is plain-text CLI output, not a wire format a dashboard codes
against — the row-per-gated-PR shape is what's frozen; exact column
spacing/formatting is Track G's call.
