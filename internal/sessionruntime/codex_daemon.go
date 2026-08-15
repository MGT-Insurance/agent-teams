package sessionruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ManagedDaemonInfo is the stable subset of `codex app-server daemon start`
// output needed to connect to the daemon. The command is intentionally the
// source of the socket path rather than ateam duplicating Codex's home-layout
// rules.
type ManagedDaemonInfo struct {
	Status              string `json:"status"`
	Backend             string `json:"backend"`
	PID                 int    `json:"pid,omitempty"`
	ManagedCodexPath    string `json:"managedCodexPath"`
	ManagedCodexVersion string `json:"managedCodexVersion"`
	SocketPath          string `json:"socketPath"`
	CLIVersion          string `json:"cliVersion"`
	AppServerVersion    string `json:"appServerVersion"`
}

type ensureDaemonFunc func(context.Context, string) (ManagedDaemonInfo, error)

func ensureManagedCodexDaemon(ctx context.Context, executable string) (ManagedDaemonInfo, error) {
	if executable == "" {
		executable = "codex"
	}
	if _, err := exec.LookPath(executable); err != nil {
		return ManagedDaemonInfo{}, fmt.Errorf("codex: executable %q not found: %w", executable, err)
	}

	cmd := exec.CommandContext(ctx, executable, "app-server", "daemon", "start")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail != "" {
			return ManagedDaemonInfo{}, fmt.Errorf("codex: start managed app-server: %w: %s", err, detail)
		}
		return ManagedDaemonInfo{}, fmt.Errorf("codex: start managed app-server: %w", err)
	}

	var info ManagedDaemonInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return ManagedDaemonInfo{}, fmt.Errorf("codex: parse managed app-server response: %w", err)
	}
	if info.Status != "started" && info.Status != "alreadyRunning" {
		return ManagedDaemonInfo{}, fmt.Errorf("codex: managed app-server returned unexpected status %q", info.Status)
	}
	if info.SocketPath == "" {
		return ManagedDaemonInfo{}, fmt.Errorf("codex: managed app-server returned no socket path")
	}
	if info.ManagedCodexPath == "" {
		return ManagedDaemonInfo{}, fmt.Errorf("codex: managed app-server returned no standalone managed binary path")
	}
	return info, nil
}
