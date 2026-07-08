package eval

import (
	"os"

	"github.com/mgt-insurance/agent-teams/internal/cost"
)

// ExtractMetrics computes the descriptive cost/latency/tool-use metrics for
// one run by attributing m.InitiativeID's local Claude Code session data.
// jobsDir/projectsDir default to ~/.claude/{jobs,projects}, matching
// `ateam cost` (internal/verbs/kong_converted.go's costKong.Run) so results
// are directly comparable. Cost/token totals reuse cost.Attribute + cost.Cost
// with the exact same priced-only summation `ateam cost` uses
// (internal/verbs/cost.go's buildJSONReport); wall-clock/tool-call/turn
// counts come from cost.ExtractTimeline, a sibling walk in the same package
// that reuses Attribute's frozen session discovery.
func ExtractMetrics(m RunManifest) (Metrics, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Metrics{}, err
	}
	jobsDir := home + "/.claude/jobs"
	projectsDir := home + "/.claude/projects"

	report, err := cost.Attribute(m.InitiativeID, jobsDir, projectsDir)
	if err != nil {
		return Metrics{}, err
	}
	timeline, err := cost.ExtractTimeline(m.InitiativeID, jobsDir, projectsDir)
	if err != nil {
		return Metrics{}, err
	}

	var metrics Metrics
	for _, mu := range report.ByModel {
		metrics.InputTokens += mu.InputTokens
		metrics.OutputTokens += mu.OutputTokens
		if usd, priced := cost.Cost(mu.Model, mu.TokenUsage); priced {
			metrics.CostUSD += usd
		}
	}
	metrics.TotalTokens = metrics.InputTokens + metrics.OutputTokens

	if !timeline.Start.IsZero() && !timeline.End.IsZero() {
		metrics.WallClockSeconds = timeline.End.Sub(timeline.Start).Seconds()
	}
	metrics.ToolCallCount = timeline.ToolCallCount
	metrics.NTurns = timeline.NTurns

	return metrics, nil
}
