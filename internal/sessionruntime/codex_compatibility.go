package sessionruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type CodexCompatibilityState string

const (
	CodexAbsent       CodexCompatibilityState = "absent"
	CodexIncompatible CodexCompatibilityState = "incompatible"
	CodexCompatible   CodexCompatibilityState = "compatible"
)

// CodexCompatibility is the shared setup/runtime-selection view of the local
// Codex installation. daemon version inspects the standalone management
// contract without starting or stopping the shared daemon.
type CodexCompatibility struct {
	State            CodexCompatibilityState `json:"state"`
	Executable       string                  `json:"executable,omitempty"`
	CLIVersion       string                  `json:"cliVersion,omitempty"`
	ManagedVersion   string                  `json:"managedCodexVersion,omitempty"`
	ManagedCodexPath string                  `json:"managedCodexPath,omitempty"`
	Detail           string                  `json:"detail,omitempty"`
}

func CheckCodexCompatibility(ctx context.Context, executable string) CodexCompatibility {
	if executable == "" {
		executable = "codex"
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return CodexCompatibility{State: CodexAbsent, Executable: executable, Detail: err.Error()}
	}
	report := CodexCompatibility{Executable: path}
	cmd := exec.CommandContext(ctx, path, "app-server", "daemon", "version")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail == "" {
			detail = err.Error()
		}
		report.State = CodexIncompatible
		report.Detail = detail
		return report
	}
	var info ManagedDaemonInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		report.State = CodexIncompatible
		report.Detail = fmt.Sprintf("could not parse managed-daemon version response: %v", err)
		return report
	}
	report.CLIVersion = info.CLIVersion
	report.ManagedVersion = info.ManagedCodexVersion
	report.ManagedCodexPath = info.ManagedCodexPath
	if info.ManagedCodexPath == "" {
		report.State = CodexIncompatible
		report.Detail = "Codex did not report a standalone managed binary path"
		return report
	}
	report.State = CodexCompatible
	return report
}

func RequireCompatibleCodex(ctx context.Context, executable string) error {
	report := CheckCodexCompatibility(ctx, executable)
	switch report.State {
	case CodexCompatible:
		return nil
	case CodexAbsent:
		return fmt.Errorf("Codex is not installed; install the official standalone Codex CLI before selecting --runtime codex")
	default:
		return fmt.Errorf("Codex at %s is not a compatible standalone installation: %s; reinstall with the official standalone Codex installer", report.Executable, report.Detail)
	}
}
