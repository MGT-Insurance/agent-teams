package eval

// TaskSpec is one JSON file per fixture task under eval/tasks/.
type TaskSpec struct {
	ID          string `json:"id"`          // "webapp-bugfix-1"
	Archetype   string `json:"archetype"`   // "webapp-bugfix"
	RunShape    string `json:"runShape"`    // "implement" | "pr-review" | "plan-only"
	FixtureRepo string `json:"fixtureRepo"` // OUT-OF-REPO fixture reference: a git URL or a resolvable
	// fixture name — NEVER a path inside the agent-teams repo.
	// Resolved to a local clone under the fixtures cache (see below).
	FixtureRef         string   `json:"fixtureRef"`         // pinned tag/branch/commit the run starts from
	Problem            string   `json:"problem"`            // problem statement handed to /dri
	AcceptanceCriteria []string `json:"acceptanceCriteria"` // authored; the LLM judge consumes these
	BuildCheck         string   `json:"buildCheck"`         // shell cmd run against produced diff; exit 0 = objective-floor green
}
