package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

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

// LoadTaskSpec reads and validates the TaskSpec JSON file at path.
func LoadTaskSpec(path string) (TaskSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TaskSpec{}, fmt.Errorf("eval: load task spec %s: %w", path, err)
	}
	var task TaskSpec
	if err := json.Unmarshal(data, &task); err != nil {
		return TaskSpec{}, fmt.Errorf("eval: parse task spec %s: %w", path, err)
	}
	if err := task.Validate(); err != nil {
		return TaskSpec{}, fmt.Errorf("eval: task spec %s: %w", path, err)
	}
	return task, nil
}

// Validate reports an error naming every required field that is empty.
func (t TaskSpec) Validate() error {
	var missing []string
	if t.ID == "" {
		missing = append(missing, "id")
	}
	if t.Archetype == "" {
		missing = append(missing, "archetype")
	}
	if t.RunShape == "" {
		missing = append(missing, "runShape")
	}
	if t.FixtureRepo == "" {
		missing = append(missing, "fixtureRepo")
	}
	if t.FixtureRef == "" {
		missing = append(missing, "fixtureRef")
	}
	if t.Problem == "" {
		missing = append(missing, "problem")
	}
	if t.BuildCheck == "" {
		missing = append(missing, "buildCheck")
	}
	if len(t.AcceptanceCriteria) == 0 {
		missing = append(missing, "acceptanceCriteria")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required field(s): %s", strings.Join(missing, ", "))
	}
	return nil
}
