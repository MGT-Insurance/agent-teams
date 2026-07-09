// This file is owned by Track C (write verbs).
package verbs

import (
	"fmt"
	"os"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── gateAsk ───────────────────────────────────────────────────────────────────

// gateAsk holds the parsed structured-ask fields for ateam gate.
type gateAsk struct {
	decision       string
	recommendation string
	alternative    string
	contextFile    string
}

// buildAskBlock serializes a gateAsk into the sentinel-delimited format from
// contract j9s section 2. The context field may be empty.
func buildAskBlock(ask *gateAsk) (string, error) {
	var b strings.Builder
	b.WriteString("<<<ateam-ask\n")
	b.WriteString("decision: " + ask.decision + "\n")
	b.WriteString("recommendation: " + ask.recommendation + "\n")
	b.WriteString("alternative: " + ask.alternative + "\n")
	if ask.contextFile != "" {
		data, err := os.ReadFile(ask.contextFile)
		if err != nil {
			return "", fmt.Errorf("ateam gate: context-file not found: %s", ask.contextFile)
		}
		ctx := strings.TrimRight(string(data), "\n")
		if len(ctx) > 280 {
			return "", fmt.Errorf("ateam gate: --context-file content exceeds 280 chars (got %d)", len(ctx))
		}
		b.WriteString("context: " + ctx + "\n")
	}
	b.WriteString(">>>")
	return b.String(), nil
}

// ── learnKey ──────────────────────────────────────────────────────────────────

// learnKey computes the bd memory key for a learn invocation.
// Precedence:
//   - "cold:<slug>" → role:<slug> (bare cold key, no tier tag)
//   - "hot:<slug>" or "fresh:<slug>" → role:<slug> (passthrough)
//   - anything else → role:fresh:<slug> (default to fresh tier)
func learnKey(role, slug string) string {
	if strings.HasPrefix(slug, "cold:") {
		return role + ":" + slug[len("cold:"):]
	}
	if strings.HasPrefix(slug, "hot:") || strings.HasPrefix(slug, "fresh:") {
		return role + ":" + slug
	}
	return role + ":fresh:" + slug
}

// learnCap detects the tier from slug using the same prefix precedence as
// learnKey and returns the tier name plus its write-time byte cap (frozen by
// contract agent-teams-b2xr.2, item 2). A bare slug (no prefix) defaults to
// fresh, mirroring learnKey's own default.
func learnCap(slug string) (tier string, capBytes int) {
	switch {
	case strings.HasPrefix(slug, "cold:"):
		return "cold", 1500
	case strings.HasPrefix(slug, "hot:"):
		return "hot", 900
	default:
		// "fresh:" prefix and bare/default-to-fresh slugs both land here.
		return "fresh", 900
	}
}

// learnStoragePhilosophy is the frozen storage-philosophy sentence from
// contract agent-teams-b2xr.2, item 5. It must agree verbatim with the
// wording used in the prose bead (agent-teams-b2xr.4) — do not let them
// drift.
const learnStoragePhilosophy = "Store the learning itself, not the story of how it was found — include only enough context to signal WHEN the learning is relevant, not a history lesson."

// learnCapError builds the rejection message for an over-cap ateam learn
// write. It teaches the required RULE/TRIGGER/APPLY shape, bare-id
// provenance, the storage philosophy, and where overflow content belongs, so
// an LLM caller can self-correct in the same tool-call cycle (contract
// agent-teams-b2xr.2, item 4).
func learnCapError(tier string, capBytes, gotBytes int) error {
	return cli.Usagef(
		"ateam learn: %s tier cap is %d bytes, got %d. "+
			"Learnings must follow this shape: RULE (one sentence — the transferable learning itself), "+
			"TRIGGER (when it fires / how to recognize relevance), APPLY (what to do about it), "+
			"and PROVENANCE as a bare initiative-id parenthetical only, e.g. \"(agent-teams-2n1w)\" — "+
			"no narrative retelling of how it was discovered. %s "+
			"If it needs more room, put the overflow in a linked bd issue and reference its id from the learning body.",
		tier, capBytes, gotBytes, learnStoragePhilosophy,
	)
}
