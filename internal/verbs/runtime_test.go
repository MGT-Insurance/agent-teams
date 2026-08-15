package verbs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

func fakeCodexCLI(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRuntimeCheckCodexReportsCompatibilityStates(t *testing.T) {
	t.Run("absent optional warns without failure", func(t *testing.T) {
		ctx, _, stderr := makeCtx(&fakeBD{}, t.TempDir())
		err := (&runtimeCheckKong{Runtime: "codex", Optional: true, executable: filepath.Join(t.TempDir(), "missing")}).Run(ctx)
		if err != nil || !strings.Contains(stderr.String(), "codex: absent") {
			t.Fatalf("err=%v stderr=%q", err, stderr.String())
		}
	})

	t.Run("absent required fails", func(t *testing.T) {
		ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
		err := (&runtimeCheckKong{Runtime: "codex", executable: filepath.Join(t.TempDir(), "missing")}).Run(ctx)
		if cli.ExitCode(err) != 1 {
			t.Fatalf("error = %v, exit = %d", err, cli.ExitCode(err))
		}
	})

	t.Run("compatible JSON", func(t *testing.T) {
		executable := fakeCodexCLI(t, `printf '%s\n' '{"status":"stopped","managedCodexPath":"/standalone/codex","managedCodexVersion":"0.146.1","cliVersion":"0.146.1"}'`)
		ctx, stdout, _ := makeCtx(&fakeBD{}, t.TempDir())
		if err := (&runtimeCheckKong{Runtime: "codex", JSON: true, executable: executable}).Run(ctx); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout.String(), `"state":"compatible"`) {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})
}
