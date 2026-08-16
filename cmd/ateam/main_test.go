package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRunEmptyVerbShowsHelp checks that empty argv prints help and exits 0.
// Under the kong contract, no-verb shows help rather than an error.
// (Pre-kong behavior was exit 2; this test was updated in bead f738.)
func TestRunEmptyVerbShowsHelp(t *testing.T) {
	code := run(nil)
	if code != 0 {
		t.Errorf("run(nil) = %d, want 0 (help)", code)
	}
}

// TestRunEmptyVerbStringShowsHelp checks that an empty string verb shows help.
func TestRunEmptyVerbStringShowsHelp(t *testing.T) {
	code := run([]string{""})
	if code != 0 {
		t.Errorf("run([\"\"]) = %d, want 0 (help)", code)
	}
}

// TestRunHelpVerb checks that "help" as a verb shows help and exits 0.
func TestRunHelpVerb(t *testing.T) {
	code := run([]string{"help"})
	if code != 0 {
		t.Errorf("run([help]) = %d, want 0 (help)", code)
	}
}

// TestRunHelpFlag checks that --help shows help and exits 0.
func TestRunHelpFlag(t *testing.T) {
	code := run([]string{"--help"})
	if code != 0 {
		t.Errorf("run([--help]) = %d, want 0 (help)", code)
	}
}

// TestRunShortHelpFlag checks that -h shows help and exits 0.
func TestRunShortHelpFlag(t *testing.T) {
	code := run([]string{"-h"})
	if code != 0 {
		t.Errorf("run([-h]) = %d, want 0 (help)", code)
	}
}

// TestRunUnknownVerb checks that an unknown verb returns exit 2 (kong parse error).
func TestRunUnknownVerb(t *testing.T) {
	code := run([]string{"no-such-verb-xyzzy"})
	if code != 2 {
		t.Errorf("run([no-such-verb-xyzzy]) = %d, want 2", code)
	}
}

// TestRunWsPreInit checks that the ws verb exits 0 without a workspace (pre-init).
func TestRunWsPreInit(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOME", t.TempDir())
	code := run([]string{"ws"})
	if code != 0 {
		t.Errorf("run([ws]) with uninitialised workspace = %d, want 0", code)
	}
}

// TestRunVerbHelpFlagPreInit checks that <verb> --help exits 0 without a workspace.
func TestRunVerbHelpFlagPreInit(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOME", t.TempDir())
	code := run([]string{"reopen", "--help"})
	if code != 0 {
		t.Errorf("run([reopen --help]) uninitialised = %d, want 0", code)
	}
}

func TestRunRuntimeCheckPreInitDoesNotRequireBDOrWorkspace(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	code := run([]string{"runtime", "check", "codex"})
	if code != 1 {
		t.Errorf("run([runtime check codex]) with Codex absent = %d, want compatibility failure 1", code)
	}
}

func TestRunSetupCodexPreInitDoesNotRequireBDOrWorkspace(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOME", t.TempDir())
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	binDir := t.TempDir()
	codex := filepath.Join(binDir, "codex")
	body := "#!/bin/sh\nset -eu\nprintf '%s\\n' '{\"status\":\"stopped\",\"managedCodexPath\":\"/standalone/codex\",\"managedCodexVersion\":\"0.146.1\",\"cliVersion\":\"0.146.1\"}'\n"
	if err := os.WriteFile(codex, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	if code := run([]string{"setup", "codex"}); code != 0 {
		t.Fatalf("run([setup codex]) with no bd or workspace = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(codexHome, "agents", "agent-teams-planner.toml")); err != nil {
		t.Fatalf("setup codex did not install agent definitions: %v", err)
	}
}
