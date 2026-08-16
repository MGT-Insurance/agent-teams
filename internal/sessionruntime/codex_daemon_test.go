package sessionruntime

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeCodexCommand(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake executable fixture is POSIX-only")
	}
	path := filepath.Join(t.TempDir(), "codex")
	contents := "#!/bin/sh\nset -eu\n" + body + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnsureManagedCodexDaemonAcceptsStartedAndAlreadyRunning(t *testing.T) {
	for _, status := range []string{"started", "alreadyRunning"} {
		t.Run(status, func(t *testing.T) {
			executable := fakeCodexCommand(t, `printf '%s\n' '{"status":"`+status+`","backend":"pid","managedCodexPath":"/home/.codex/packages/standalone/current/codex","managedCodexVersion":"0.146.1","socketPath":"/home/.codex/app-server-control/app-server-control.sock","cliVersion":"0.146.1","appServerVersion":"0.146.1"}'`)
			info, err := ensureManagedCodexDaemon(context.Background(), executable)
			if err != nil {
				t.Fatalf("ensure: %v", err)
			}
			if info.Status != status || info.SocketPath == "" || info.ManagedCodexPath == "" {
				t.Fatalf("info = %+v", info)
			}
		})
	}
}

func TestEnsureManagedCodexDaemonRejectsIncompatibleInstall(t *testing.T) {
	executable := fakeCodexCommand(t, `printf '%s\n' 'standalone installation required' >&2
exit 1`)
	_, err := ensureManagedCodexDaemon(context.Background(), executable)
	if err == nil || !strings.Contains(err.Error(), "standalone installation required") {
		t.Fatalf("error = %v", err)
	}
}

func TestEnsureManagedCodexDaemonRequiresManagedPath(t *testing.T) {
	executable := fakeCodexCommand(t, `printf '%s\n' '{"status":"started","socketPath":"/socket"}'`)
	_, err := ensureManagedCodexDaemon(context.Background(), executable)
	if err == nil || !strings.Contains(err.Error(), "standalone managed binary path") {
		t.Fatalf("error = %v", err)
	}
}
