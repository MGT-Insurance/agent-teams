package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/workspace"
)

func TestHomeDefault(t *testing.T) {
	// Ensure AGENT_TEAMS_HOME is unset so we exercise the default path.
	t.Setenv("AGENT_TEAMS_HOME", "")

	got := workspace.Home()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".agent-teams")
	if got != want {
		t.Errorf("Home() = %q, want %q", got, want)
	}
}

func TestHomeEnvOverride(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOME", "/custom/path")
	got := workspace.Home()
	if got != "/custom/path" {
		t.Errorf("Home() = %q, want /custom/path", got)
	}
}

func TestInitialized(t *testing.T) {
	dir := t.TempDir()

	// Not initialized yet.
	if workspace.Initialized(dir) {
		t.Error("Initialized should be false when .beads does not exist")
	}

	// Create .beads as a file (not a dir) — still not initialized.
	beadsFile := filepath.Join(dir, ".beads")
	if err := os.WriteFile(beadsFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if workspace.Initialized(dir) {
		t.Error("Initialized should be false when .beads is a file, not a dir")
	}
	os.Remove(beadsFile)

	// Create .beads as a directory — now initialized.
	if err := os.Mkdir(beadsFile, 0o755); err != nil {
		t.Fatal(err)
	}
	if !workspace.Initialized(dir) {
		t.Error("Initialized should be true when .beads directory exists")
	}
}
