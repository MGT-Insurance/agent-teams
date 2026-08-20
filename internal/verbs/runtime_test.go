package verbs

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
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

func TestRuntimeOpenClaudeUsesNativeAgentsView(t *testing.T) {
	ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
	var executable string
	var args []string
	err := (&runtimeOpenKong{
		Runtime: "claude",
		openNative: func(_ *cli.Context, gotExecutable string, gotArgs ...string) error {
			executable = gotExecutable
			args = append([]string(nil), gotArgs...)
			return nil
		},
	}).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if executable != "claude" {
		t.Fatalf("executable = %q, want claude", executable)
	}
	want := []string{"agents", "--permission-mode", "bypassPermissions"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestRuntimeOpenCodexUsesManagedNativeThreadPicker(t *testing.T) {
	executable := fakeCodexCLI(t, `
case "$*" in
  "app-server daemon version")
    printf '%s\n' '{"status":"stopped","managedCodexPath":"/standalone/codex","managedCodexVersion":"0.147.0","cliVersion":"0.147.0"}'
    ;;
  *)
    printf '%s\n' "unexpected args: $*" >&2
    exit 2
    ;;
esac`)
	ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
	var gotExecutable string
	var gotArgs []string
	err := (&runtimeOpenKong{
		Runtime:    "codex",
		executable: executable,
		ensureCodex: func(_ context.Context, got string) (sessionruntime.ManagedDaemonInfo, error) {
			if got != executable {
				t.Fatalf("ensure executable = %q, want %q", got, executable)
			}
			return sessionruntime.ManagedDaemonInfo{
				ManagedCodexPath: "/standalone/codex",
				SocketPath:       "/tmp/codex.sock",
			}, nil
		},
		openNative: func(_ *cli.Context, executable string, args ...string) error {
			gotExecutable = executable
			gotArgs = append([]string(nil), args...)
			return nil
		},
	}).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if gotExecutable != "/standalone/codex" {
		t.Fatalf("executable = %q, want managed standalone binary", gotExecutable)
	}
	want := []string{
		"resume",
		"--remote", "unix:///tmp/codex.sock",
		"--all",
		"--include-non-interactive",
		"--sandbox", "danger-full-access",
		"--ask-for-approval", "never",
	}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("args = %#v, want %#v", gotArgs, want)
	}
}

func TestRuntimeOpenRejectsUnknownRuntime(t *testing.T) {
	ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
	err := (&runtimeOpenKong{Runtime: "other"}).Run(ctx)
	if cli.ExitCode(err) != 2 || !strings.Contains(err.Error(), `unknown runtime "other"`) {
		t.Fatalf("error = %v, exit = %d", err, cli.ExitCode(err))
	}
}
