package verbs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func compatibleCodexFixture(t *testing.T) string {
	t.Helper()
	return fakeCodexCLI(t, `printf '%s\n' '{"status":"stopped","managedCodexPath":"/standalone/codex","managedCodexVersion":"0.146.1","cliVersion":"0.146.1"}'`)
}

func TestSetupCodexInstallsDefinitionsAndDetectsDrift(t *testing.T) {
	codexHome := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, t.TempDir())
	cmd := &setupCodexKong{executable: compatibleCodexFixture(t), codexHome: codexHome}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("first setup: %v", err)
	}
	distinctiveRules := map[string]string{
		"planner":      "Decompose in concentric circles",
		"implementer":  "Never guess on design",
		"reviewer":     "After-the-fact identifiability",
		"tester":       "Only the DRI starts a dev server",
		"investigator": "vs PLANNER",
	}
	for _, role := range []string{"planner", "implementer", "tester", "reviewer", "investigator"} {
		path := filepath.Join(codexHome, "agents", "agent-teams-"+role+".toml")
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", role, err)
		}
		if !strings.Contains(string(body), `name = "agent-teams-`+role+`"`) ||
			!strings.Contains(string(body), "ateam learnings "+role) ||
			!strings.Contains(string(body), distinctiveRules[role]) {
			t.Fatalf("invalid %s definition: %s", role, body)
		}
	}
	if !strings.Contains(stdout.String(), "start a new Codex session") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	drifted := filepath.Join(codexHome, "agents", "agent-teams-planner.toml")
	if err := os.WriteFile(drifted, []byte("local change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(ctx); err == nil || !strings.Contains(err.Error(), "local changes") {
		t.Fatalf("drift error = %v", err)
	}
	cmd.Force = true
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("forced setup: %v", err)
	}
	restored, _ := os.ReadFile(drifted)
	if strings.Contains(string(restored), "local change") {
		t.Fatal("force did not restore bundled definition")
	}
}

func TestSetupCodexFailsBeforeWritingWhenInstallIsIncompatible(t *testing.T) {
	codexHome := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
	err := (&setupCodexKong{executable: filepath.Join(t.TempDir(), "missing"), codexHome: codexHome}).Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "official standalone") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(codexHome, "agents")); !os.IsNotExist(err) {
		t.Fatalf("agents directory should not be created, stat err = %v", err)
	}
}
