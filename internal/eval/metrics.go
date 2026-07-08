package eval

import "errors"

// ExtractMetrics computes cost/tokens via `cost.Attribute(m.InitiativeID,
// jobsDir, projectsDir)`; wall-clock = max−min JSONL timestamp; tool-call
// count = count of tool_use blocks.
func ExtractMetrics(m RunManifest) (Metrics, error) {
	return Metrics{}, errors.New("not implemented")
}
