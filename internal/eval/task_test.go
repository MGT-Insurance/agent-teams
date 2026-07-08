package eval

import (
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
