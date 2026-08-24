// Package initiative owns reading and writing the routing data that lives as
// key: value text lines inside an agent-teams initiative's bd description.
//
// # Background
//
// An initiative's routing data (repo path, worktree, branch, tied sessions,
// PRs, PR-to-workstream associations, ...) lives as "key: value" text lines
// inside the initiative bead's description. Before this package existed,
// roughly twenty call sites across internal/verbs each re-implemented their
// own line scanner. One (parseDescriptionFields,
// internal/verbs/route_match.go:29) was last-wins and case-insensitive; the
// rest were first-wins and case-sensitive. On
// 2026-07-28 a briefing line in an initiative's description redefined the
// repo-path key, the last-wins reader took the redefined value,
// claimsInitiativeLocally (internal/verbs/routing_ownership.go:40) failed its
// repo check, and the relay silently dropped every human reply in that
// topic. The fix is not another parser patch: it is this package, so that a
// future switch to a different storage backend (e.g. bd labels) is a change
// in one place instead of twenty-two.
//
// This comment is the frozen contract every reader, writer, and call site
// migration in this initiative is built against. Nothing in this package may
// contradict it without a new contract bead.
//
// # Frozen item 1 — the one matching rule
//
// Exactly one rule, used by every reader AND by the redefinition-collision
// check. Stated verbatim:
//
//	A field line is: start of line (column 0), a key matching
//	[a-z][a-z0-9-]*, a single colon, a single space, then a non-empty value
//	whose first character is not whitespace. The value is right-trimmed. The
//	FIRST occurrence of a key wins; later occurrences are ignored.
//	Multi-valued keys (session, track-worktree, pr, and pr-workstream)
//	accumulate in registration order instead.
//
// No leading whitespace. No case folding. No tolerance of a missing or extra
// space after the colon. Under this rule, lines shaped like "Repo: ..."
// (wrong case) or "- Repo: ..." / "• Repo: ..." (list-prefixed) or
// "GOAL: ..." (prose, not a field) are structurally invisible — they never
// match at all, so first-wins is defence in depth rather than the only defence.
//
// A census of every open initiative at the time this contract was written
// found 100% of real field lines already canonical under this rule. The only
// four non-canonical lines found registry-wide were exactly the shapes named
// above — evidence that this rule costs nothing in practice.
//
// # Frozen item 2 — the tail stays append-open forever
//
// A description is: header (the canonical field lines), then a free-form
// prose body of arbitrary length, then an APPEND-OPEN TAIL that grows for
// the life of the initiative (session ties, track-worktree registrations,
// PR registrations, and PR-to-workstream associations) — appended below the
// entire prose body on every registration.
//
// THE TRAP, stated so nobody rediscovers it the hard way: the obvious-looking
// fix is to bound the parser — stop scanning at the first blank line, or
// read only a fixed header block. DO NOT DO THIS. The schema block is not
// contiguous. A bounded parser would stop before the tail lines and
// therefore fail to resolve any session, track, PR, or PR-to-workstream line
// appended below prose. For sessions and tracks that would classify live work
// as dead; for PRs and associations it would silently discard durable routing
// data. Both failures are worse than the bug this package exists to fix.
//
// Any change to this package must preserve: a field line is found no matter
// how far down the description it appears, and a prose body of arbitrary
// length between header and tail changes nothing.
//
// # Frozen item 3 — the field set is NOT closed
//
// [Fields] models the single-valued routing fields plus four multi-valued
// rails: "session", "track-worktree", "pr", and "pr-workstream" (see
// [Fields.Sessions], [Fields.Tracks], [Fields.PRs], and
// [Fields.PRWorkstreams]). The live registry also carries canonical keys that
// [Fields] does not model, including pr-number, pr-repo, and pr-url. Those keys
// are written not by Go code but by an LLM following a skill's instructions,
// which then parses the same keys back out of `ateam show`. A skill file can
// therefore introduce a new canonical key without one line of Go changing.
//
// "pr" and the pr-number/pr-repo/pr-url trio are NOT the same field wearing
// two names — they describe different things for different initiative
// kinds. pr-number/pr-repo/pr-url describe the single PR a review-pr
// initiative is reviewing (plugins/agent-teams/skills/review-pr/SKILL.md).
// "pr" describes the PR(s) a DRI initiative has opened (one initiative can
// open more than one PR); it is unrelated to, and does not replace, the
// pre-existing "pr:" line the dri skill writes into bd NOTES today
// (plugins/agent-teams/skills/dri/SKILL.md — "record the structured pr:
// field") — that Notes-based line is read by a free-text regex
// (extractPrURL, internal/verbs/route_match.go), not by this package's
// field-line rule, which scans Description only. Both happen to store the
// same value shape (a full https GitHub PR URL) under the same key text
// ("pr"), coincidentally — see docs/multi-pr-contract.md for the frozen
// grammar and the empirical keystone behind it.
//
// A typed "pr-workstream" value has the exact grammar
// "<canonical-github-pr-url><one ASCII space><whitespace-free-bead-id>".
// [PRWorkstreams] and [Fields.PRWorkstreams] exclude malformed legacy values,
// while [JSONFields] preserves every raw matching line. [WithPRWorkstream]
// canonicalizes URL identity, treats the same pair as a no-op, and rejects a
// conflicting workstream for the same canonical PR; its caller validates Bead
// ancestry before persistence.
//
// Consequence: no design in this package may assume the typed [Fields]
// struct enumerates everything storable. An unmodeled canonical key (a line
// that satisfies frozen item 1 but has no matching Fields member) is
// legitimate data, not malformed input, and must never be dropped or treated
// as an error.
//
// # Frozen item 4 — the writer must be provably non-lossy
//
// Direct consequence of item 3: if a fresh-compose write were ever used to
// REWRITE an existing initiative's description, every unmodeled key would be
// silently dropped — the exact class of silent data loss this package
// exists to eliminate, reintroduced by the cure. Therefore:
//
//   - [New] composes a fresh description and is for NEW initiatives ONLY. It
//     must never be used to rewrite an existing initiative's description.
//   - Every mutation of an EXISTING initiative — [WithSession], [WithTrack],
//     [WithPR], and [WithPRWorkstream] — is strictly append-only: it takes the
//     issue's current description unchanged and appends below it, never
//     re-deriving or dropping any line. An idempotent repeat is a no-op.
//   - A future migration off this description format (e.g. to bd labels)
//     must carry every canonical key found in the raw text, not merely the
//     ones that happen to have a Fields member. A migration that moves only
//     the modeled fields drops unmodeled keys (e.g. the pr-* trio) on the
//     floor.
//
// # Package surface
//
//	type Fields struct {
//	    Problem, Repo, Worktree, Branch, Team, Mode, Runtime, Epic string
//	    Standby  bool
//	    Sessions []string
//	    Tracks   []string
//	    PRs      []string
//	    PRWorkstreams []PRWorkstream
//	}
//
//	type PRWorkstream struct {
//	    PR         string `json:"pr"`
//	    Workstream string `json:"workstream"`
//	}
//
//	func Of(iss bd.Issue) Fields                        // READ SEAM (typed)
//	func JSONFields(iss bd.Issue) map[string]any         // READ SEAM (wire)
//	func PRWorkstreams(iss bd.Issue) []PRWorkstream      // READ SEAM (valid associations)
//	func ResolvedPRs(iss bd.Issue) []string              // READ SEAM (rail, then legacy fallback)
//	var PRURLRE *regexp.Regexp                            // shared GitHub PR matcher
//	func CanonicalPRURL(url string) (string, bool)
//	func New(f Fields) (WritePlan, error)                // WRITE SEAM (new initiatives only — see item 4)
//	func WithSession(iss bd.Issue, id string) (WritePlan, error)
//	func WithTrack(iss bd.Issue, path string) (WritePlan, error)
//	func WithPR(iss bd.Issue, url string) (WritePlan, error)
//	func WithPRWorkstream(iss bd.Issue, url, workstream string) (WritePlan, error)
//
//	type WritePlan struct {
//	    Description string
//	    Labels      []string
//	}
//
//	type Collision struct { Line int; Key string; Text string }
//	func (f Fields) CollisionsIn(bodyText string) []Collision
//
// [New] returns an error, a deviation from this contract's original listing
// (which showed "func New(f Fields) WritePlan", no error) discovered during
// implementation review, not decided here: a value containing a line break
// would inject a fake field line into the composed description that wins
// under first-wins (frozen item 1), so New validates and rejects rather than
// silently composing broken output. The error-returning signature is the
// shipped API, and every caller must handle that validation failure.
//
// [Fields.Standby] only ever tells you the WRITE half of the standby
// lifecycle. [New] writes a "standby: true" header line and [Of] parses it
// back — but nothing ever appends a change to that line, because the
// "released" half of the lifecycle is recorded in bd Notes ("standby:
// released"), a field [Of] never looks at (it only reads Description). So
// once an initiative is created with standby: true, [Of](iss).Standby stays
// true forever, even after release — checking it alone is not a reliable
// "is this initiative currently on standby" test. A correct reader needs
// both Description (via [Of]) and Notes; do not build one that trusts
// Fields.Standby in isolation.
//
// [JSONFields] is the wire read seam for consumers outside this process —
// specifically the TypeScript dashboard, which cannot import this package and
// until now re-implemented the frozen rule in its own regex
// (agent-teams-ully.12). `ateam list-json` calls it and attaches the result to
// every element as a "fields" object; the dashboard reads that object.
//
// [JSONFields] is deliberately NOT a projection of the typed [Fields] members.
// Doing that would drop every unmodeled canonical key at a read seam, which
// item 3 forbids. Instead it emits the canonical LINE key verbatim for every
// matched line, so the wire shape does not depend on which keys Go models: the
// pr-* trio appears today with no Go change, and giving one of them a [Fields]
// member later moves nothing. A shape that hoisted the modeled keys and nested
// the rest in an "extra" bag would invert that property and is the design to
// reject in review. [Of] and [JSONFields] share one scan of the description
// (the unexported all), so there is still exactly one accumulation of the rule.
//
// [Of] takes the whole bd.Issue, not just its Description field. bd.Issue
// already carries both Description and Labels, and [WritePlan] already has
// slots for both. Today the description backend fills Description and
// leaves Labels empty; a future labels backend would fill Labels and leave
// the header out of Description. Neither signature changes, so no call site
// changes — switching backends is editing two function bodies plus a
// one-shot migration verb, not a twenty-two-site rewrite. A change that
// narrows [Of] to take a description string instead of a bd.Issue defeats
// the entire point of this seam and should be rejected in review.
//
// [Fields.CollisionsIn] is a method on [Fields], not a free function, so
// only a caller that has already composed the header can invoke it — the
// redefinition-collision warning belongs inside the writer, not bolted onto
// a call site.
//
// Labels are DEFERRED, not rejected, and are not built by this package.
// Feasibility was proven empirically before this contract was written: an
// 89-char path with a colon-delimited key round-trips byte-exact through
// label add, JSON read, and JSONL export, with 40 labels on one bead and no
// observed cap. Two hazards for whoever builds the labels backend later:
// label values are hard-truncated at 255 chars while the success message
// echoes the full untruncated string, and a value containing a comma is
// storable but permanently unqueryable because comma is the AND separator
// in label filters.
//
// # Scope boundary
//
// This package is a read/write component for the routing fields above and the
// shared PR identity/read helpers required to consume them. It is not a
// general refactor of description handling: bead notes and free-form prose
// stay where they are.
//
// This package imports internal/bd only (a leaf package, so importing it
// here creates no cycle even though internal/verbs imports both). As of the
// agent-teams-ully.7 migration, internal/verbs reads and writes routing
// fields exclusively through this package; a routing-field scanner anywhere
// else in Go is a regression.
package initiative
