// This file is owned by Track C (write verbs).
package verbs

import (
	"fmt"
	"os"
	"strings"
)

// ── gateAsk ───────────────────────────────────────────────────────────────────

// gateAsk holds the parsed structured-ask fields for ateam gate.
type gateAsk struct {
	decision       string
	recommendation string
	alternative    string
	contextFile    string
}

// buildAskMessage renders a clean human-readable plain-text body for a phone
// transport from a structured gateAsk. Sentinel markers are not included —
// this is the human-facing form only (bead notes use buildAskBlock instead).
// Context is appended best-effort: a read error is silently ignored because
// buildAskBlock already validated the file path earlier in gateKong.Run.
func buildAskMessage(ask *gateAsk) string {
	var b strings.Builder
	b.WriteString(ask.decision + "\n\n")
	b.WriteString("Recommended: " + ask.recommendation + "\n")
	b.WriteString("Alternative: " + ask.alternative)
	if ask.contextFile != "" {
		data, err := os.ReadFile(ask.contextFile)
		if err == nil {
			b.WriteString("\nContext: " + strings.TrimRight(string(data), "\n"))
		}
	}
	return b.String()
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
