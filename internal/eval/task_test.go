package eval

import (
	"encoding/json"
	"os"
	"testing"
)

// TestWebappBugfix1TaskSpec_Parses guards the one in-repo artifact this
// track owns: eval/tasks/webapp-bugfix-1.json must parse against the
// frozen TaskSpec shape and reference an out-of-repo fixture.
func TestWebappBugfix1TaskSpec_Parses(t *testing.T) {
	data, err := os.ReadFile("../../eval/tasks/webapp-bugfix-1.json")
	if err != nil {
		t.Fatalf("reading webapp-bugfix-1.json: %v", err)
	}

	var task TaskSpec
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatalf("unmarshalling webapp-bugfix-1.json: %v", err)
	}

	if task.ID != "webapp-bugfix-1" {
		t.Errorf("ID = %q, want %q", task.ID, "webapp-bugfix-1")
	}
	if task.Archetype != "webapp-bugfix" {
		t.Errorf("Archetype = %q, want %q", task.Archetype, "webapp-bugfix")
	}
	if task.RunShape != "implement" {
		t.Errorf("RunShape = %q, want %q", task.RunShape, "implement")
	}
	if task.FixtureRepo == "" {
		t.Error("FixtureRepo is empty")
	}
	if task.FixtureRepo == "eval/tasks/webapp-bugfix-1.json" {
		t.Error("FixtureRepo looks like an in-repo path, not an out-of-repo reference")
	}
	if task.FixtureRef == "" {
		t.Error("FixtureRef is empty")
	}
	if task.Problem == "" {
		t.Error("Problem is empty")
	}
	if len(task.AcceptanceCriteria) == 0 {
		t.Error("AcceptanceCriteria is empty")
	}
	if task.BuildCheck == "" {
		t.Error("BuildCheck is empty")
	}
}
