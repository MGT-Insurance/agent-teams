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
//	    "pr":             "https://..."    // first GitHub PR URL in notes, or ""
//	    "pr_probe":       "ok"             // "ok" | "unreachable" | "none"
//	  },
//	  ...
//	]
//
// STATUS COMPUTATION (first-match wins, per contract agent-teams-j9s §1 as
// amended by the at-jno7 contract, external_review.go §7):
//  1. NEEDS-DECISION  — labels contain "human" AND "gate:question"
//  2. IN-PROGRESS     — the joined session is ACTIVELY WORKING
//                       (overrides any review gate)
//  3. labels contain "human" AND "gate:review" AND NOT actively working;
//     within rule 3, first match wins:
//     a. STALE-MERGED             — probe returned MERGED/CLOSED
//     b. AWAITING-EXTERNAL-REVIEW — "external-review" label present
//     c. REVIEWABLE               — otherwise
//  4. IN-PROGRESS     — everything else (open, no gate, or between gates)
//
// "ACTIVELY WORKING" = a live session whose cwd matches the initiative's
// worktree path (exact-line match) AND (status=="busy" OR state=="working").
// No matching live session => NOT actively working.
//
// Graceful degrade: if `claude agents --json` fails, all initiatives get
// execution_status "unknown" rather than erroring. A gh preflight or per-PR
// probe failure degrades only pr_probe (external_review.go §8) — the declared
// AWAITING-EXTERNAL-REVIEW answer needs no probe and survives it.

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// RegisterStatusKong registers status verbs onto p using native kong structs.
func RegisterStatusKong(p *cli.Parser) {
	p.AddVerb("execution-status", "Emit JSON array of open initiatives with execution state.", &executionStatusKong{
		agentsFunc:    defaultAgentsJSON,
		prMergeFunc:   defaultPRMerge,
		preflightFunc: prMergePreflight,
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
//	  "pr":               "https://github.com/..."   // empty string when absent
//	  "pr_probe":         "ok"                       // "ok" | "unreachable" | "none"
//	}
//
// ask is null when no structured ateam-ask block is present in notes.
// pr is the first GitHub PR URL found in notes, or "".
// pr_probe reports whether the merge-state probe answered for this row
// (external_review.go §5); "none" means there was no PR URL to probe.
// The raw notes field is intentionally omitted — consumers use ask and pr.
type initiativeStatus struct {
	ID              string        `json:"id"`
	Title           string        `json:"title"`
	Worktree        string        `json:"worktree"`
	Labels          []string      `json:"labels"`
	ExecutionStatus string        `json:"execution_status"`
	Ask             *askBlockJSON `json:"ask"`
	PR              string        `json:"pr"`
	PRProbe         string        `json:"pr_probe"`
}

// askBlockJSON is the JSON-serialisable form of an askBlock.
type askBlockJSON struct {
	Decision       string `json:"decision"`
	Recommendation string `json:"recommendation"`
	Alternative    string `json:"alternative"`
	Context        string `json:"context,omitempty"`
}

// computeExecutionStatus returns the execution-state for one initiative, given
// its labels, the current live sessions, its worktree path, and this row's
// merge probe (probe is one of the prProbe* values; mergeState is the probed
// state and is meaningful only when probe == prProbeOK).
//
// Evaluation order (first match wins):
//  1. NEEDS-DECISION  — "human" + "gate:question" present in labels
//  2. IN-PROGRESS     — session actively working (overrides gate:review)
//  3. "human" + "gate:review" + NOT actively working; see the sub-cascade below
//  4. IN-PROGRESS     — everything else
//
// Only rule 3's body knows about the probe or the declared label, so an
// un-gated initiative sees zero behaviour change from either — including one
// carrying externalReviewLabel with no review gate, which stays inert
// (external_review.go §9's U/Q -> H row).
func computeExecutionStatus(labels []string, sessions []agentSession, worktree, probe, mergeState string) string {
	hasHuman := hasLabel(labels, "human")
	hasQuestion := hasLabel(labels, "gate:question")
	hasReview := hasLabel(labels, "gate:review")

	// Rule 1: NEEDS-DECISION
	if hasHuman && hasQuestion {
		return "NEEDS-DECISION"
	}

	// Rule 2: IN-PROGRESS overrides review gate when actively working.
	if isActivelyWorking(sessions, worktree) {
		return "IN-PROGRESS"
	}

	// Rule 3: review-gated (external_review.go §7), first match wins.
	if hasHuman && hasReview {
		// A merged/closed PR is finished work, so it is tested BEFORE the
		// declared label: letting the label win would park completed work
		// permanently out of Eric's sight. Gated on prProbeOK — a probe
		// failure can never manufacture StatusStaleMerged (§5's INVARIANT).
		if probe == prProbeOK && (mergeState == prStateMerged || mergeState == prStateClosed) {
			return StatusStaleMerged
		}
		// Declared by Eric via `ateam handoff`, never derived (§0). Reported
		// independently of the probe, so it survives a missing or
		// unauthenticated gh.
		if hasLabel(labels, externalReviewLabel) {
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

// ── native kong struct ────────────────────────────────────────────────────────

// executionStatusKong is the kong-converted form of executionStatusCmd.
type executionStatusKong struct {
	// agentsFunc is injected so tests can substitute a fake; kong must ignore it.
	agentsFunc agentsJSONFunc `kong:"-"`
	// prMergeFunc and preflightFunc are the gh seams (prmerge.go), injected
	// for the same reason: a test must never shell out to real gh.
	prMergeFunc   prMergeFunc  `kong:"-"`
	preflightFunc func() error `kong:"-"`
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

	now := time.Now()
	cache := loadPRStateCache(ctx.Home)
	cacheDirty := false

	// Merge-state probing (external_review.go §6). Preflight runs AT MOST ONCE
	// per invocation, and only when some row would actually shell out to gh
	// (anyNeedsLiveProbe) — `gh auth status` validates the token over the
	// network, so running it on a zero-PR or all-cache-fresh run costs a third
	// of a second of latency for a verdict nothing consults. When it does run
	// and fails, every row degrades to pr_probe=prProbeUnreachable behind a
	// single stderr line rather than erroring (§8) — including cache-fresh
	// rows, since mergeProbe tests enabled before the cache. Probes are also
	// skipped wholesale when the agents join already failed — every row is
	// "unknown" then, so no probe could change an answer (§8's EXISTING
	// DEGRADE UNCHANGED). Rows carrying a PR URL still report
	// prProbeUnreachable in that case: reporting prProbeNone would assert the
	// initiative has no PR, contradicting the pr field in the same row.
	probesEnabled := agentsErr == nil
	if probesEnabled && anyNeedsLiveProbe(issues, &cache, now) {
		if err := c.preflightFunc(); err != nil {
			fmt.Fprintf(ctx.Stderr, "ateam execution-status: PR merge probes disabled for this run: %v\n", err)
			probesEnabled = false
		}
	}

	out := make([]initiativeStatus, 0, len(issues))
	for _, iss := range issues {
		wt := worktreePath(iss.Description)

		prURL := extractPrURL(iss.Notes)
		probe, mergeState, dirty := c.mergeProbe(ctx, &cache, prURL, now, probesEnabled)
		cacheDirty = cacheDirty || dirty

		var execStatus string
		if agentsErr != nil {
			execStatus = "unknown"
		} else {
			execStatus = computeExecutionStatus(iss.Labels, sessions, wt, probe, mergeState)
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

		out = append(out, initiativeStatus{
			ID:              iss.ID,
			Title:           iss.Title,
			Worktree:        wt,
			Labels:          iss.Labels,
			ExecutionStatus: execStatus,
			Ask:             ask,
			PR:              prURL,
			PRProbe:         probe,
		})
	}

	// Stored ONCE per run, not per initiative. A write failure costs only
	// redundant probes next run, so it degrades to a stderr line — stdout
	// stays pure JSON either way (§8).
	if cacheDirty {
		if err := storePRStateCache(ctx.Home, cache); err != nil {
			fmt.Fprintf(ctx.Stderr, "ateam execution-status: write PR merge-state cache: %v\n", err)
		}
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("ateam execution-status: marshal: %w", err)
	}
	fmt.Fprintln(ctx.Stdout, string(raw))
	return nil
}

// anyNeedsLiveProbe reports whether any of issues would make mergeProbe reach
// its c.prMergeFunc call this run: the row needs a parseable PR URL AND no
// fresh cache entry. It is the exact complement of mergeProbe's two gh-free
// early exits, which is what lets Run skip the preflight when it answers
// false — every row then answers from prProbeNone or the cache, and no gh
// process is started, so the preflight's verdict would have gone unread.
//
// Deliberately evaluated for ALL rows up front rather than lazily at the first
// probe: a run that mixes cache-fresh rows with one needing a probe must still
// degrade every row to prProbeUnreachable when the preflight fails (§8), which
// a first-probe trigger would get wrong for whichever rows happened to be
// processed before it.
func anyNeedsLiveProbe(issues []bd.Issue, cache *prStateCache, now time.Time) bool {
	for _, iss := range issues {
		ownerRepo, prNumber, ok := parsePrURL(extractPrURL(iss.Notes))
		if !ok {
			continue
		}
		if _, fresh := cache.lookup(prStateKey(ownerRepo, prNumber), now); !fresh {
			return true
		}
	}
	return false
}

// mergeProbe resolves one initiative's pr_probe value and merge state,
// consulting and updating cache. dirty reports whether cache was mutated, so
// the caller can store it once at end of run.
//
// Results, in order:
//   - no parseable PR URL           -> (prProbeNone, "", false), never probed,
//     no stderr line: normal, not a fault
//   - probing disabled for this run -> (prProbeUnreachable, "", false)
//   - fresh cache entry             -> that entry, no gh call
//   - otherwise                     -> one gh call via c.prMergeFunc
//
// A probe failure writes a negative cache entry and emits exactly ONE stderr
// line, and only when that entry is NEWLY written (putFailure's isNew) — so a
// permanently dead PR costs one line per prProbeFailureTTL rather than one per
// invocation under the hung tick (external_review.go §8).
func (c *executionStatusKong) mergeProbe(ctx *cli.Context, cache *prStateCache, prURL string, now time.Time, enabled bool) (probe, mergeState string, dirty bool) {
	ownerRepo, prNumber, ok := parsePrURL(prURL)
	if !ok {
		return prProbeNone, "", false
	}
	if !enabled {
		return prProbeUnreachable, "", false
	}

	key := prStateKey(ownerRepo, prNumber)
	if entry, fresh := cache.lookup(key, now); fresh {
		if !entry.OK {
			return prProbeUnreachable, "", false
		}
		return prProbeOK, entry.State, false
	}

	state, err := c.prMergeFunc(ownerRepo, prNumber)
	if err != nil {
		if cache.putFailure(key, err, now) {
			fmt.Fprintf(ctx.Stderr, "ateam execution-status: PR merge probe failed for %s: %v\n", key, err)
		}
		return prProbeUnreachable, "", true
	}
	cache.putOK(key, state, now)
	return prProbeOK, state, true
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
