// This file is owned by Track S (execution-state / status-join).
package verbs

// RegisterStatus registers the status-join verb: execution-status.
//
// Emitted JSON shape (array, one entry per open initiative):
//
//	[
//	  {
//	    "id":              "at-abc",         // initiative bead id
//	    "title":          "...",             // bead title
//	    "worktree":       "/path/to/wt",    // from "worktree: <path>" line in description
//	    "labels":         ["human","gate:review"],
//	    "execution_status": "REVIEWABLE",   // see STATUS COMPUTATION below
//	    "ask":            { "decision": "...", ... } // null when no sentinel block
//	    "prs":            ["https://..."]  // initiative.ResolvedPRs(iss); [] when none
//	    "pr_reviews":     [{"pr":"https://...","gate":"review"}] // see computePRReviews
//	  },
//	  ...
//	]
//
// STATUS COMPUTATION (first-match wins, per contract agent-teams-j9s §1 as
// amended by the at-jno7 contract, external_review.go §7, and per-PR-aware
// per docs/multi-pr-contract.md §6 — see computeExecutionStatus's own doc
// comment for how a multi-PR disagreement rolls up into this one string):
//  1. NEEDS-DECISION  — labels contain "human" AND a "gate:question" of ANY
//                       PR (bare or per-PR suffixed) OR a bare
//                       "gate:live-test-review" (pre-PR, never per-PR
//                       suffixed)
//  2. IN-PROGRESS     — the joined session is ACTIVELY WORKING
//                       (overrides any review gate)
//  3. labels contain "human" AND a "gate:review" of ANY PR AND NOT actively
//     working; within rule 3, first match wins:
//     a. AWAITING-EXTERNAL-REVIEW — EVERY review-gated PR also carries its
//        OWN "external-review" declaration
//     b. REVIEWABLE               — otherwise (at least one review-gated PR
//        has NOT been handed off — the case a human actually hits)
//  4. IN-PROGRESS     — everything else (open, no gate, or between gates)
//
// "ACTIVELY WORKING" = a live session whose cwd matches the initiative's
// worktree path (exact-line match) AND (status=="busy" OR state=="working").
// No matching live session => NOT actively working.
//
// Graceful degrade: if `claude agents --json` fails, all initiatives get
// execution_status "unknown" rather than erroring.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// RegisterStatusKong registers status verbs onto p using native kong structs.
func RegisterStatusKong(p *cli.Parser) {
	p.AddVerb("execution-status", "Emit JSON array of open initiatives with execution state.", &executionStatusKong{
		agentsFunc: defaultAgentsJSON,
	})
}

// initiativeStatus is the per-initiative entry in the emitted JSON array.
//
// Emitted shape:
//
//	{
//	  "id":               "at-abc",
//	  "title":            "...",
//	  "worktree":         "/path/to/wt",
//	  "labels":           ["human","gate:review"],
//	  "execution_status": "REVIEWABLE",
//	  "ask":              { "decision": "...", "recommendation": "...", "alternative": "...", "context": "..." },
//	  "prs":              ["https://github.com/..."],  // [] when none — never omitted, never null
//	  "pr_reviews":       [{"pr": "https://...", "gate": "review"}]  // [] when none
//	}
//
// ask is null when no structured ateam-ask block is present in notes.
// prs is sourced from initiative.ResolvedPRs(iss) — docs/multi-pr-contract.md
// §2a — never Of(iss).PRs directly and never its own Notes/Description scan.
// The raw notes field is intentionally omitted — consumers use ask, prs, and
// pr_reviews.
type initiativeStatus struct {
	ID              string        `json:"id"`
	Title           string        `json:"title"`
	Worktree        string        `json:"worktree"`
	Labels          []string      `json:"labels"`
	ExecutionStatus string        `json:"execution_status"`
	Ask             *askBlockJSON `json:"ask"`
	PRs             []string      `json:"prs"`
	PRReviews       []PRReview    `json:"pr_reviews"`
}

// PRReview is one entry in the Go-computed per-PR review array
// (docs/multi-pr-contract.md §5) — emitted on both `execution-status` and
// `ateam list-json`. Gate is one of "review", "question", "external", or ""
// (never omitted or null) when the PR carries no gate label at all.
type PRReview struct {
	PR   string `json:"pr"`
	Gate string `json:"gate"`
}

// askBlockJSON is the JSON-serialisable form of an askBlock.
type askBlockJSON struct {
	Decision       string `json:"decision"`
	Recommendation string `json:"recommendation"`
	Alternative    string `json:"alternative"`
	Context        string `json:"context,omitempty"`
}

// computeExecutionStatus returns the execution-state for one initiative, given
// its labels, the current live sessions, and its worktree path.
//
// Evaluation order (first match wins):
//  1. NEEDS-DECISION  — "human" + a "gate:question" of ANY PR present in
//     labels, OR "human" + a bare "gate:live-test-review" (raised pre-PR,
//     so it never carries a per-PR suffix)
//  2. IN-PROGRESS     — session actively working (overrides gate:review)
//  3. "human" + a "gate:review" of ANY PR + NOT actively working; see the
//     sub-cascade below
//  4. IN-PROGRESS     — everything else
//
// Only rule 3's body knows about the declared label, so an un-gated
// initiative sees zero behaviour change from it — including one carrying
// externalReviewLabel with no review gate, which stays inert (external_
// review.go §9's U/Q -> H row).
//
// MULTI-PR ROLLUP (docs/multi-pr-contract.md §6 — Track G's call, not frozen
// by the contract beyond "stays a single string"): execution_status is one
// value for the whole initiative, but a PR can now carry its OWN gate and
// its OWN handoff declaration (§3). Rules 1 and 2 treat "does ANY PR have
// this gate" as the question — a question on ONE PR is exactly as urgent as
// a question on the only PR, so NEEDS-DECISION wins even if a second PR is
// merely review-gated. Rule 3a is the one place PRs can genuinely disagree:
// PR A handed off, PR B still awaiting review. AWAITING-EXTERNAL-REVIEW
// claims "nothing here needs you" — that is only true when EVERY
// review-gated PR has ITS OWN matching handoff declaration (paired by PR,
// not "a handoff exists somewhere on this initiative"). If even one
// review-gated PR lacks its own handoff, the initiative reports REVIEWABLE:
// that is the case a human actually hits, and REVIEWABLE is the state that
// gets looked at, so disagreement resolves toward the state that demands
// attention rather than the one that suppresses it.
func computeExecutionStatus(labels []string, sessions []agentSession, worktree string) string {
	hasHuman := hasLabel(labels, "human")
	hasQuestion := hasGateKind(labels, "gate:question")
	hasLiveTestReview := hasGateKind(labels, "gate:live-test-review")
	hasReview := hasGateKind(labels, "gate:review")

	// Rule 1: NEEDS-DECISION
	if hasHuman && (hasQuestion || hasLiveTestReview) {
		return "NEEDS-DECISION"
	}

	// Rule 2: IN-PROGRESS overrides review gate when actively working.
	if isActivelyWorking(sessions, worktree) {
		return "IN-PROGRESS"
	}

	// Rule 3: review-gated (external_review.go §7), first match wins.
	if hasHuman && hasReview {
		// Pair each review-gated PR (bare "" id, or a per-PR URL suffix)
		// with ITS OWN handoff declaration — declared by Eric via
		// `ateam handoff`, never derived (§0). AWAITING-EXTERNAL-REVIEW
		// only when every review id has a matching handoff id.
		reviewIDs := gateIDs(labels, "gate:review")
		handoffIDs := gateIDs(labels, externalReviewLabel)
		allHandedOff := true
		for id := range reviewIDs {
			if !handoffIDs[id] {
				allHandedOff = false
				break
			}
		}
		if allHandedOff {
			return StatusAwaitingExternalReview
		}
		return "REVIEWABLE"
	}

	// Rule 4: default IN-PROGRESS
	return "IN-PROGRESS"
}

// hasLabel reports whether label is present in labels.
func hasLabel(labels []string, label string) bool {
	for _, l := range labels {
		if l == label {
			return true
		}
	}
	return false
}

// hasGateKind reports whether labels contain base ("gate:review",
// "gate:question", or externalReviewLabel) in either its bare,
// initiative-scoped form or ANY per-PR "<base>:<url>" suffixed form
// (docs/multi-pr-contract.md §3). A multi-PR initiative gates per PR, so a
// caller that only checked the bare label would go blind to every gate the
// moment a second PR is opened.
func hasGateKind(labels []string, base string) bool {
	prefix := base + ":"
	for _, l := range labels {
		if l == base || strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

// gateIDs returns the set of PR discriminators that carry base as a label —
// "" for the bare, initiative-scoped form, or the PR URL suffix for a
// per-PR "<base>:<url>" label (docs/multi-pr-contract.md §3). Used to pair a
// review gate with its OWN handoff declaration rather than any handoff
// anywhere on the initiative (computeExecutionStatus rule 3).
func gateIDs(labels []string, base string) map[string]bool {
	ids := make(map[string]bool)
	prefix := base + ":"
	for _, l := range labels {
		switch {
		case l == base:
			ids[""] = true
		case strings.HasPrefix(l, prefix):
			ids[strings.TrimPrefix(l, prefix)] = true
		}
	}
	return ids
}

// computePRReviews returns one PRReview per PR in prs (the initiative's
// RESOLVED PR list — initiative.ResolvedPRs, never the raw rail alone, see
// docs/multi-pr-contract.md §2a), each Gate computed from labels.
//
// Precedence when a PR carries more than one gate label is inherited
// verbatim from the dashboard's deriveExplicitGate
// (dashboard/server/src/parse.ts:212), unchanged: "question" outranks both
// "external" and "review"; "external" outranks "review".
//
// Bare, un-suffixed gate labels (predating this bead's --pr discriminator,
// docs/multi-pr-contract.md §3) are attributed to EVERY resolved PR,
// regardless of how many the initiative has (agent-teams-ssib.24). A bare
// gate names no PR, so with 2+ PRs there is no way to attribute it to one
// without guessing — but the alternative (attributing it to none, the
// original behavior once a second PR existed) meant computeExecutionStatus
// still reported NEEDS-DECISION/REVIEWABLE from the SAME bare label while
// human-list, driven by this function, emitted zero rows for any PR: an
// initiative the rollup calls gated and the queue calls empty, with no way
// for the human to find it. Loud, possibly-over-reporting agreement beats
// that silent disagreement — every PR shows the gate until it's cleared or
// migrated onto its own per-PR label. (It also, unconditionally, counts
// toward the initiative-scoped execution_status rollup above, which is
// label-text-based and needs no PR attribution of its own.)
func computePRReviews(labels []string, prs []string) []PRReview {
	reviews := make([]PRReview, 0, len(prs))
	for _, pr := range prs {
		reviews = append(reviews, PRReview{PR: pr, Gate: gateForPR(labels, pr)})
	}
	return reviews
}

// gateForPR computes the gate kind for one PR: "question", "external",
// "review", or "" — checking the per-PR suffixed label first, then falling
// back to the bare label of the same base (attributed to every PR, per
// computePRReviews's doc comment).
func gateForPR(labels []string, pr string) string {
	matches := func(base string) bool {
		return hasLabel(labels, base+":"+pr) || hasLabel(labels, base)
	}
	switch {
	case matches("gate:question"):
		return "question"
	case matches(externalReviewLabel):
		return "external"
	case matches("gate:review"):
		return "review"
	default:
		return ""
	}
}

// ── native kong struct ────────────────────────────────────────────────────────

// executionStatusKong is the kong-converted form of executionStatusCmd.
type executionStatusKong struct {
	// agentsFunc is injected so tests can substitute a fake; kong must ignore it.
	agentsFunc agentsJSONFunc `kong:"-"`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *executionStatusKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam execution-status: nil context")
	}

	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--status=open", "--json"); err != nil {
		return fmt.Errorf("ateam execution-status: list initiatives: %w", err)
	}

	sessions, agentsErr := c.agentsFunc()

	out := make([]initiativeStatus, 0, len(issues))
	for _, iss := range issues {
		wt := initiative.Of(iss).Worktree

		var execStatus string
		if agentsErr != nil {
			execStatus = "unknown"
		} else {
			execStatus = computeExecutionStatus(iss.Labels, sessions, wt)
		}

		var ask *askBlockJSON
		if b, ok := extractLatestAsk(iss.Notes); ok {
			ask = &askBlockJSON{
				Decision:       b.decision,
				Recommendation: b.recommendation,
				Alternative:    b.alternative,
				Context:        b.context,
			}
		}

		// prs is the RESOLVED list (docs/multi-pr-contract.md §2a) — never
		// Of(iss).PRs directly and never its own Notes/Description scan.
		// nil becomes [] so the field is never emitted as null.
		prs := initiative.ResolvedPRs(iss)
		if prs == nil {
			prs = []string{}
		}

		out = append(out, initiativeStatus{
			ID:              iss.ID,
			Title:           iss.Title,
			Worktree:        wt,
			Labels:          iss.Labels,
			ExecutionStatus: execStatus,
			Ask:             ask,
			PRs:             prs,
			PRReviews:       computePRReviews(iss.Labels, prs),
		})
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("ateam execution-status: marshal: %w", err)
	}
	fmt.Fprintln(ctx.Stdout, string(raw))
	return nil
}

// isActivelyWorking reports whether any session in sessions has a cwd matching
// worktree (symlink-normalised, see canonicalPath) AND meets the "actively
// working" predicate:
//
//	status == "busy" OR state == "working"
//
// No matching live session => returns false.
func isActivelyWorking(sessions []agentSession, worktree string) bool {
	if worktree == "" {
		return false
	}
	want := canonicalPath(worktree)
	for _, s := range sessions {
		if canonicalPath(s.CWD) == want {
			if s.Status == "busy" || s.State == "working" {
				return true
			}
		}
	}
	return false
}
