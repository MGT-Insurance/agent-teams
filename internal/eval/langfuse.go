package eval

import "errors"

// Push creates/reuses dataset item for task, records an experiment run tagged
// by cfg.Name+Hash(); attaches metrics + judge as scores. See the LANGFUSE
// MAPPING section of agent-teams-grft.1 for the canonical score names:
// cost_usd, n_total_tokens, n_toolcalls, n_turns, latency_s, correctness
// (numeric); objective_floor_pass (boolean).
//
// Credentials arrive via env: LANGFUSE_HOST, LANGFUSE_PUBLIC_KEY,
// LANGFUSE_SECRET_KEY.
func Push(res RunResult, task TaskSpec) error {
	return errors.New("not implemented")
}
