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
//	    "notes":          "..."             // raw bead notes (crisp-ask block lives here)
//	  },
//	  ...
//	]
//
// STATUS COMPUTATION (first-match wins, per contract agent-teams-j9s §1):
//  1. NEEDS-DECISION  — labels contain "human" AND "gate:question"
//  2. IN-PROGRESS     — the joined session is ACTIVELY WORKING
//                       (overrides any review gate)
//  3. REVIEWABLE      — labels contain "human" AND "gate:review"
//                       AND NOT actively working
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

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// RegisterStatus adds the execution-status verb to reg.
func RegisterStatus(reg cli.Registry) {
	reg.Register(&executionStatusCmd{agentsFunc: defaultAgentsJSON})
}

// executionStatusCmd implements `ateam execution-status`.
type executionStatusCmd struct {
	// agentsFunc is injected so tests can substitute a fake without touching os/exec.
	agentsFunc agentsJSONFunc
}

func (c *executionStatusCmd) Name() string { return "execution-status" }

// Run implements: ateam execution-status
//
// Reads all open initiatives, joins them to live claude sessions by worktree
// cwd, computes execution-state per contract agent-teams-j9s §1, and emits a
// JSON array to stdout.
func (c *executionStatusCmd) Run(ctx *cli.Context, _ []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam execution-status: nil context")
	}

	// Load all open initiatives.
	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--status=open", "--json"); err != nil {
		return fmt.Errorf("ateam execution-status: list initiatives: %w", err)
	}

	// Load live sessions. Graceful degrade: on failure emit "unknown" for all.
	sessions, agentsErr := c.agentsFunc()

	// Build output entries.
	out := make([]initiativeStatus, 0, len(issues))
	for _, iss := range issues {
		wt := worktreePath(iss.Description)

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

		out = append(out, initiativeStatus{
			ID:              iss.ID,
			Title:           iss.Title,
			Worktree:        wt,
			Labels:          iss.Labels,
			ExecutionStatus: execStatus,
			Ask:             ask,
			PR:              extractPrURL(iss.Notes),
		})
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("ateam execution-status: marshal: %w", err)
	}
	fmt.Fprintln(ctx.Stdout, string(raw))
	return nil
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
//	}
//
// ask is null when no structured ateam-ask block is present in notes.
// pr is the first GitHub PR URL found in notes, or "".
// The raw notes field is intentionally omitted — consumers use ask and pr.
type initiativeStatus struct {
	ID              string        `json:"id"`
	Title           string        `json:"title"`
	Worktree        string        `json:"worktree"`
	Labels          []string      `json:"labels"`
	ExecutionStatus string        `json:"execution_status"`
	Ask             *askBlockJSON `json:"ask"`
	PR              string        `json:"pr"`
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
//  1. NEEDS-DECISION  — "human" + "gate:question" present in labels
//  2. IN-PROGRESS     — session actively working (overrides gate:review)
//  3. REVIEWABLE      — "human" + "gate:review" + NOT actively working
//  4. IN-PROGRESS     — everything else
func computeExecutionStatus(labels []string, sessions []agentSession, worktree string) string {
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

	// Rule 3: REVIEWABLE
	if hasHuman && hasReview {
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

// isActivelyWorking reports whether any session in sessions has a cwd matching
// worktree (exact-line match) AND meets the "actively working" predicate:
//
//	status == "busy" OR state == "working"
//
// No matching live session => returns false.
func isActivelyWorking(sessions []agentSession, worktree string) bool {
	if worktree == "" {
		return false
	}
	// matchByWorktree uses exact-line matching against bd.Issue.Description.
	// Here we do the same cwd comparison that hasLiveSession does — exact string
	// equality after trimming trailing slashes — because sessions have cwd not a
	// full description. The join key is the worktree path string.
	for _, s := range sessions {
		if s.CWD == worktree {
			if s.Status == "busy" || s.State == "working" {
				return true
			}
		}
	}
	return false
}
