package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTaskSpec_RoundTrip(t *testing.T) {
	task, err := LoadTaskSpec(filepath.Join("testdata", "sample-task.json"))
	if err != nil {
		t.Fatalf("LoadTaskSpec: %v", err)
	}
	if task.ID != "sample-task-1" {
		t.Errorf("ID = %q, want %q", task.ID, "sample-task-1")
	}
	if task.Archetype != "webapp-bugfix" {
		t.Errorf("Archetype = %q, want %q", task.Archetype, "webapp-bugfix")
	}
	if task.RunShape != "implement" {
		t.Errorf("RunShape = %q, want %q", task.RunShape, "implement")
	}
	if task.FixtureRef != "v1-buggy" {
		t.Errorf("FixtureRef = %q, want %q", task.FixtureRef, "v1-buggy")
	}
	if len(task.AcceptanceCriteria) != 2 {
		t.Errorf("AcceptanceCriteria len = %d, want 2", len(task.AcceptanceCriteria))
	}
	if task.BuildCheck != "go test ./..." {
		t.Errorf("BuildCheck = %q", task.BuildCheck)
	}
}

func TestLoadTaskSpec_MissingFile(t *testing.T) {
	if _, err := LoadTaskSpec(filepath.Join("testdata", "does-not-exist.json")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadTaskSpec_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTaskSpec(path); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestTaskSpec_Validate_MissingFields(t *testing.T) {
	err := (TaskSpec{}).Validate()
	if err == nil {
		t.Fatal("expected error for empty TaskSpec, got nil")
	}
}

func TestTaskSpec_Validate_AllFieldsPresent(t *testing.T) {
	task := TaskSpec{
		ID:                 "t1",
		Archetype:          "webapp-bugfix",
		RunShape:           "implement",
		FixtureRepo:        "https://example.com/fixture.git",
		FixtureRef:         "v1",
		Problem:            "fix it",
		AcceptanceCriteria: []string{"criterion"},
		BuildCheck:         "go test ./...",
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
}

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

// TestWebappBugfix1V1TaskSpec_Parses guards eval/tasks/webapp-bugfix-1-v1.json,
// the current (loop-fixed-baseline) version of the webapp-bugfix-1 archetype.
// webapp-bugfix-1 (fixtureRef v0-bug-1) is frozen/historical — see grft.17 —
// because its fixture baseline carried an undetected frontend refetch-loop
// defect that contaminated seeded-bug isolation. This spec pins fixtureRef to
// v1-bug-1 (v1-baseline + the same seeded project-filter bug) and is the id
// all new A/B runs of this archetype should dispatch against.
func TestWebappBugfix1V1TaskSpec_Parses(t *testing.T) {
	data, err := os.ReadFile("../../eval/tasks/webapp-bugfix-1-v1.json")
	if err != nil {
		t.Fatalf("reading webapp-bugfix-1-v1.json: %v", err)
	}

	var task TaskSpec
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatalf("unmarshalling webapp-bugfix-1-v1.json: %v", err)
	}

	if task.ID != "webapp-bugfix-1-v1" {
		t.Errorf("ID = %q, want %q", task.ID, "webapp-bugfix-1-v1")
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
	if task.FixtureRepo == "eval/tasks/webapp-bugfix-1-v1.json" {
		t.Error("FixtureRepo looks like an in-repo path, not an out-of-repo reference")
	}
	if task.FixtureRef != "v1-bug-1" {
		t.Errorf("FixtureRef = %q, want %q", task.FixtureRef, "v1-bug-1")
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
