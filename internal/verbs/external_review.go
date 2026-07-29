// This file is owned by the at-jno7 CONTRACT track (agent-teams-p9dm.22),
// re-decomposed 2026-07-29 after Eric rejected the GitHub-derived design
// (agent-teams-p9dm.12, closed void). It freezes every surface shared by
// more than one downstream track — the declared label, the two new
// execution_status values, the pr_probe enum, the exact gh --json field
// list, and the merge-state cache's path/key/schema/TTLs. Nothing else may
// redefine these. Constants and frozen doc comments ONLY: no verb, no gh
// invocation, no cache I/O, no status wiring — those belong to the tracks
// below.
//
// Downstream tracks reading this file:
//
//	agent-teams-p9dm.17  execution-status: wire rule 3 + emit pr_probe (status.go)
//	agent-teams-p9dm.18  /initiatives skill: render the two new statuses
//	agent-teams-p9dm.19  steward skill: recognize a spoken handoff
//	agent-teams-p9dm.20  dri gate-protocol: DRI must NOT run handoff
//	agent-teams-p9dm.23  ateam handoff (handoff.go) + clear-gate + human-list
//	agent-teams-p9dm.24  prmerge.go: the gh merge-state probe + TTL cache
//
// ── §0: why the previous design is dead — do not reintroduce it ────────────
//
// agent-teams-p9dm.12 derived "awaiting external review" from
// `gh pr view --json reviewDecision,reviewRequests`. Eric rejected it. His
// correction, verbatim: "As soon as a PR is created, it automatically adds
// other reviewers. That doesn't mean I have looked at it and am just
// waiting on others to review." CONFIRMED: MGT-Insurance/midgard's
// .github/CODEOWNERS states "Reviewer auto-assignment is handled by
// Canary" — reviewer assignment on the target repo is automated, so the
// field carries zero signal, not even under manual inspection.
//
// 🚫 PROHIBITION, BINDING ON EVERY TRACK: reviewRequests, reviewDecision,
// and latestReviews MUST NOT be read by any code in this initiative, and
// MUST NOT be used by any rule as evidence about Eric's attention. The
// probe this contract defines (§6) requests --json state and nothing else,
// so the fields are not even fetched — see ghProbeJSONFields below. If a
// future change reintroduces them, these two live counterexamples
// (verified 2026-07-28) constrain any rule ordering:
//
//	#4483  state=MERGED, reviewDecision=APPROVED, reviewRequests=[bloedorn-, enn-tee, Kikketer]
//	#4501  state=MERGED, reviewDecision=APPROVED, reviewRequests=[bloedorn-]
//
// reviewRequests SURVIVES both merge and approval. Any rule that tests
// "someone is still requested" BEFORE testing merged/closed is wrong, and
// reproduces agent-teams-p9dm.2's bug wearing a new status string.
package verbs

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── §1–2: the state is declared, not derived — the label ────────────────────
//
// The fact Eric wants tracked is "I have looked at this PR, I am done, it
// is now on the team." That fact comes into existence at the instant he
// finishes looking; nothing in GitHub records that instant, so it is
// DECLARED rather than computed. Eric's chosen mechanism (decided
// 2026-07-29, verbatim): "you just say it whenever, in whatever thread
// you're already in, and it sticks." Already-established corollary, do NOT
// re-derive: the declaration can NOT ride on the DRI's `ateam gate
// --kind=review` delivery gate, because at delivery Eric has not looked
// yet, so the answer there is always "no".

// externalReviewLabel is the plain bd label, written by `ateam handoff`
// (agent-teams-p9dm.23), that carries the declared fact on the initiative
// bead.
//
// It is ADDITIVE: it sits ON TOP of the existing "human" + "gate:review"
// pair the DRI already set at delivery (§3's ateam gate --kind=review).
// Those two labels are DELIBERATELY RETAINED — removing them was
// considered and rejected, because dropping them re-arms two escalation
// ladders this initiative exists to disarm:
//
//   - hung_scan.go:293-298 classifies "human" + ("gate:question" OR
//     "gate:review") as AWAITING-HUMAN, checked regardless of live-session
//     presence. Drop the pair and a handed-off initiative with no live
//     session falls to hungClassDead (hung_scan.go:300-301), which arms
//     the DEAD escalation ladder in hung_tick.go: a couple of steward wake
//     nudges (nextDeadLadderAction, hung_tick.go:81-95) followed by a
//     canned alert posted straight into the initiative's own Telegram
//     topic (hung_tick.go:35-39). That is exactly the noise this
//     initiative exists to delete.
//   - hung_workproduct.go:396-411's hasHumanAnyGateLabel exempts "human" +
//     any "gate:*" from the work-product flatline trip. Same argument:
//     drop the pair and a quiet, handed-off initiative trips the flatline
//     alert too.
//
// Keeping the pair means BOTH ladders stay disarmed for free, and the
// blast radius of this whole change is confined to rows that already
// report REVIEWABLE.
//
// The one cost — `ateam human-list` would still list a handed-off
// initiative as awaiting human input — is paid explicitly by a filter in
// query.go (owned by agent-teams-p9dm.23's handoff bead), not left as a
// wart here.
//
// Not a gate label: a "gate:" prefix means "blocked on Eric." This state
// means the exact opposite, so it does NOT take a "gate:external" form — a
// gate:external label would also miss hung_scan's specific
// gate:question|gate:review pair and re-arm the DEAD ladder for no reason.
const externalReviewLabel = "external-review"

// ── §3: the verb — semantics frozen here, `ateam handoff` owned by .23 ──────
//
//	ateam handoff <id>            // declare: Eric has looked; it's on the team
//	ateam handoff <id> --clear    // undo: Eric has re-opened the question
//
// Semantics, frozen for agent-teams-p9dm.23's handoff.go:
//   - adds/removes exactly externalReviewLabel via `bd label add|remove`,
//     the same mechanism gateKong (kong_converted.go:308-321) and
//     clearGateKong (kong_converted.go:391-409) already use for "human"
//     and "gate:*". Idempotent: `bd label add` on a present label is a
//     no-op.
//   - touches NO other label. "human" and "gate:review" are left alone by
//     `ateam handoff`; only `ateam clear-gate` (existing verb) removes
//     them, and per §9's H -> U transition, `ateam clear-gate` on a
//     handed-off initiative MUST also remove externalReviewLabel — that is
//     the DRI's resume/close-out path (dri/references/wind-down.md:13 runs
//     it before `ateam close` on merge).
//   - writes no note, sends no notification, opens no topic.
//   - if the initiative does not carry "gate:review", the label is still
//     written and a WARNING goes to ctx.Stderr saying the reported status
//     will not change until a review gate exists. Warn-and-proceed, not a
//     hard error — this can run from a steward turn, and a failed command
//     there is worse than a labelled-but-inert initiative. See §9's
//     U/Q -> H row.
//
// NAME: "handoff" is a recommendation, not a frozen decision — Eric may
// rename the verb. The label string (externalReviewLabel) and the verb
// name are independent; do not couple them in prose elsewhere.

// ── §4 / §7: the two new execution_status values + where rule 3 places them ─
//
// Existing execution_status strings ("NEEDS-DECISION", "IN-PROGRESS",
// "REVIEWABLE", "unknown" — status.go rules 1, 2, and 4, status.go:99-115)
// keep their exact current meaning and are UNTOUCHED by this contract.
// REVIEWABLE now means what it always claimed to mean: work genuinely
// awaiting ERIC.

const (
	// StatusAwaitingExternalReview means Eric has declared he is done
	// looking (externalReviewLabel present); the PR is with third-party
	// reviewers. NOT in Eric's action queue. Independent of the merge
	// probe — reported even when pr_probe is prProbeUnreachable or
	// prProbeNone, because it is a declared fact, not a derived one.
	StatusAwaitingExternalReview = "AWAITING-EXTERNAL-REVIEW"

	// StatusStaleMerged means the PR is MERGED or CLOSED on GitHub but the
	// initiative is still open with a review gate — a 10-second cleanup,
	// not a review. Emitted ONLY when pr_probe == prProbeOK; see the
	// invariant on the pr_probe constants below. A probe failure can NEVER
	// produce this status.
	StatusStaleMerged = "STALE-MERGED"
)

// Rule 3 placement, frozen for agent-teams-p9dm.17's status.go (rules 1, 2,
// and 4 are untouched — status.go:99-115). Rule 3's body becomes,
// first-match-wins:
//
//	pr_probe == prProbeOK && state ∈ {prStateMerged, prStateClosed}  -> StatusStaleMerged
//	hasLabel(labels, externalReviewLabel)                            -> StatusAwaitingExternalReview
//	otherwise                                                        -> "REVIEWABLE" (today's answer)
//
// StatusStaleMerged is checked BEFORE the declared label deliberately: a
// merged PR is finished work, and reporting it as awaiting external review
// would park completed work out of Eric's sight forever — the same
// ordering failure §0 exists to prevent, just one step later in the chain.
//
// Consequence: un-gated initiatives (no "gate:review") see ZERO behavior
// change — rule 3 (and both new statuses) are only reachable through the
// existing "human" + "gate:review" precondition rules 1/2/4 already gate
// on.
//
// Justification for keeping the merge probe at all (agent-teams-p9dm.2,
// verified 2026-07-28): at-nbvt/#4501 and at-mzd6/#4483 are both MERGED and
// APPROVED yet still sat in Eric's REVIEWABLE list, because nothing in this
// repo ever asked GitHub anything and the gate cleared only if a DRI
// happened to be resumed and noticed.

// ── §5: the pr_probe field ───────────────────────────────────────────────────
//
// New JSON field on initiativeStatus (status.go), frozen for
// agent-teams-p9dm.17:
//
//	"pr_probe": "ok" | "unreachable" | "none"

const (
	// prProbeOK means gh returned a parseable merge state for this PR
	// (fresh or cache-hit).
	prProbeOK = "ok"

	// prProbeUnreachable means a probe was attempted and failed, OR the gh
	// preflight failed for the whole run (§8's DEGRADE contract).
	prProbeUnreachable = "unreachable"

	// prProbeNone means the initiative has no parseable GitHub PR URL; it
	// was never probed. Normal, not a fault — no stderr line accompanies
	// this value.
	prProbeNone = "none"
)

// INVARIANT, binding on agent-teams-p9dm.17 and .24: StatusStaleMerged is
// emitted ONLY when pr_probe == prProbeOK. A probe failure can NEVER
// produce it. StatusAwaitingExternalReview is independent of the probe —
// see its doc comment above.

// ── §6: the surviving probe — merge detection ONLY ──────────────────────────
//
// This is the ONE derived piece Eric kept, narrowed accordingly: merge
// state is a plain fact requiring no judgment, unlike review status (§0).
//
//	gh pr view <n> --repo <owner/repo> --json state
//
// ghProbeJSONFields is the exact --json argument agent-teams-p9dm.24's
// prmerge.go MUST pass — state and nothing else. This constant is itself
// the enforcement mechanism for §0's prohibition: any future change that
// widens the probe to read reviewDecision/reviewRequests/latestReviews has
// to touch this frozen line, which is the point.
const ghProbeJSONFields = "state"

// Merge-state values consumed from the gh probe's "state" field. Any other
// value (gh emits none today) is treated as an unrecognized/non-terminal
// state, i.e. NOT prStateMerged/prStateClosed.
const (
	prStateMerged = "MERGED"
	prStateClosed = "CLOSED"
	prStateOpen   = "OPEN"
)

// ── §8: cache schema, path, key, TTL, timeout ────────────────────────────────

// prStateFileName is the merge-state cache's filename, joined against
// ctx.Home (== ~/.agent-teams) by prStatePath below — never a hardcoded
// path, mirroring hungStatePath's use of ctx.Home-derived paths
// (hung_scan.go) so tests can redirect it.
const prStateFileName = "pr-state.json"

// prStateSchemaVersion is the current value of prStateFile.SchemaVersion.
const prStateSchemaVersion = 1

// prStatePath returns the path to the merge-state cache file:
// <ctx.Home>/pr-state.json.
func prStatePath(ctx *cli.Context) string {
	return filepath.Join(ctx.Home, prStateFileName)
}

// prStateKey returns the cache key for one PR: "<owner/repo>#<number>",
// owner/repo lower-cased. Lower-cases defensively so a caller that forgets
// to normalize its ownerRepo (e.g. one not routed through route_match.go's
// parsePrURL, which already lower-cases) still produces the canonical key —
// two tracks (the writer, .24, and any future reader) computing this key
// independently MUST agree on it bit-for-bit or cache lookups silently miss
// forever.
func prStateKey(ownerRepo string, prNumber int) string {
	return strings.ToLower(ownerRepo) + "#" + strconv.Itoa(prNumber)
}

// prStateFile is the schema of the file at prStatePath.
//
// Example:
//
//	{
//	  "schema_version": 1,
//	  "entries": {
//	    "mgt-insurance/midgard#4501": {"probed_at": "2026-07-29T12:00:00Z", "ok": true, "state": "MERGED"},
//	    "someorg/gone#12":            {"probed_at": "2026-07-29T12:00:00Z", "ok": false, "error": "gh: HTTP 404"}
//	  }
//	}
type prStateFile struct {
	SchemaVersion int                     `json:"schema_version"`
	Entries       map[string]prStateEntry `json:"entries"`
}

// prStateEntry is one cached probe result, keyed by prStateKey in
// prStateFile.Entries. State is set only when OK is true; Error is set only
// when OK is false — the two are mutually exclusive, both `omitempty`.
type prStateEntry struct {
	// ProbedAt is an RFC3339 UTC timestamp, e.g. "2026-07-29T12:00:00Z".
	ProbedAt string `json:"probed_at"`
	OK       bool   `json:"ok"`
	State    string `json:"state,omitempty"` // prStateMerged | prStateClosed | prStateOpen
	Error    string `json:"error,omitempty"`
}

// TTLs are FLAT — no backoff ladder. prProbeSuccessTTL intentionally
// numerically matches hungTickInterval (hung_tick.go:33, 5 minutes): that
// is the cadence execution-status is actually invoked on (steward wake +
// hung tick + /initiatives), so a shorter TTL would just re-probe on every
// tick for no benefit. prProbeFailureTTL is longer so a permanently dead PR
// doesn't get re-probed every 5 minutes forever.
const (
	prProbeSuccessTTL = 5 * time.Minute
	prProbeFailureTTL = 60 * time.Minute
)

// prProbeTimeout bounds a single `gh pr view` invocation so a hanging gh
// process cannot wedge the verb.
const prProbeTimeout = 10 * time.Second

// DEGRADE CONTRACT — frozen for agent-teams-p9dm.24's prmerge.go. This is
// the whole safety story for a verb that shells out to gh on every
// execution-status call:
//
//   - PREFLIGHT, once per invocation: if gh is absent from PATH, or
//     `gh auth status` exits non-zero, skip ALL probes for the run, emit
//     exactly ONE stderr line, set pr_probe=prProbeUnreachable on every
//     initiative, and fall back to the declared-label answer (§4), which
//     still works — it needs no probe.
//   - PER-PR FAILURE (non-zero exit, HTTP 404, unparseable JSON, timeout):
//     flat taxonomy, no branching. StatusStaleMerged is not computed;
//     pr_probe=prProbeUnreachable makes the degrade visible; a negative
//     cache entry (OK: false, Error set) is written and not re-probed for
//     prProbeFailureTTL; exactly ONE stderr line is emitted, ONLY when the
//     negative entry is NEWLY written (never on a cache hit) — so a
//     permanently dead PR costs one line per prProbeFailureTTL, not one
//     per invocation (execution-status runs roughly every hungTickInterval
//     under the hung tick).
//   - stdout stays pure JSON. Diagnostics NEVER go to stdout — consumers
//     parse it.
//   - NO PR URL: pr_probe=prProbeNone, never probed, no stderr line.
//     Normal, not a fault.
//   - EXISTING DEGRADE UNCHANGED: if `claude agents --json` fails, every
//     initiative is still "unknown" (status.go:154-155) and no probe runs
//     at all — this contract adds a new degrade path, it does not touch
//     the existing one.
//
// CONCURRENCY: execution-status can run concurrently (steward wake +
// /initiatives render). Write via temp-file-then-rename, mirroring
// saveHungState (hung_scan.go) — that makes a torn read impossible; a lost
// update costs at most one redundant probe. No lock. Stated, accepted, not
// a bug.

// ── §9: the full state machine — no undefined transitions ───────────────────
//
// States, as label sets on an OPEN initiative:
//
//	U  UNGATED        no human, no gate:*                          -> IN-PROGRESS
//	Q  QUESTION-GATED  human + gate:question                        -> NEEDS-DECISION
//	R  REVIEW-GATED    human + gate:review                          -> REVIEWABLE
//	H  HANDED-OFF      human + gate:review + external-review        -> AWAITING-EXTERNAL-REVIEW
//
// Overlays, applied at read time, mutating nothing:
//   - a live session working in the worktree  -> IN-PROGRESS (rule 2, beats R and H)
//   - probe says MERGED/CLOSED, in R or H     -> STALE-MERGED
//
// Transitions:
//
//	U -> R       ateam gate --kind=review    (DRI, Phase 5). Unchanged.
//	U -> Q       ateam gate --kind=question. Unchanged.
//	R -> H       ateam handoff <id>          — Eric declares. THE NEW TRANSITION.
//	H -> R       ateam handoff <id> --clear  — Eric re-opens the question.
//	H -> U       ateam clear-gate <id>       — MUST also remove externalReviewLabel;
//	                                            the DRI's resume/close-out path.
//	R -> U       ateam clear-gate <id>. Unchanged.
//	H -> closed  ateam close <id>. Unchanged — closing ends every state.
//	U/Q -> H     NOT REACHABLE by design, but `ateam handoff` on an un-gated
//	             initiative still writes the label and WARNS (§3). The label
//	             is inert until a review gate exists; no status is
//	             misreported.
//
// Deliberately NOT a transition: the probe (§6) never mutates a label.
// execution-status is a read verb invoked every ~5 minutes from the hung
// tick and from /initiatives; giving it write authority over the gate would
// make a display refresh mutate state. Merge only changes what is REPORTED
// (StatusStaleMerged); cleanup remains Eric's `ateam close`, or the DRI's
// close-out.
//
// ── §10: explicitly out of scope for this contract ──────────────────────────
//
//   - The periodic steward nudge ("these three PRs are sitting — which are
//     you done with?"). Offered to Eric 2026-07-29 and explicitly deferred:
//     "second one for now." Filed as a blocked enhancement.
//   - changes_requested auto-clearing externalReviewLabel. route-pr-event
//     performs NO label mutation today — every owned-PR transition,
//     including changes_requested and merged, resolves to a single mail
//     send to the owning initiative (route.go:57-65, sendArgs:84-93).
//     Adding label writes there would be the first in that file, AND the
//     producer does not run on the machine that owns these initiatives:
//     ~/.agent-teams/review-repos does not exist on the erlloyd machine and
//     `ateam sent` has 0 messages from sender pr-shepherd (verified
//     2026-07-29). Filed as a blocked enhancement. The state is NOT left
//     without an exit: `ateam clear-gate` is on the DRI's resume path, and
//     the work-resumed case is already covered by rule 2 (§9's overlay).
//   - Dashboard (dashboard/**) — NOT because it's harmless. Corrected
//     2026-07-29: the dashboard does not consume execution_status at all;
//     deriveExplicitGate (dashboard/server/src/parse.ts:185-193) re-derives
//     from RAW LABELS and returns "review" for anything carrying
//     "gate:review". Because externalReviewLabel is ADDITIVE (§2), a
//     handed-off initiative still carries human + gate:review, so the
//     dashboard keeps showing it as needing Eric's review — the inverse of
//     the declared meaning. New status strings can't break it (it never
//     reads them); the new label actively contradicts it. Filed as
//     agent-teams-p9dm.29, blocked behind the loop set. Same divergence
//     agent-teams-p9dm.3 documents, now with a concrete consequence.
