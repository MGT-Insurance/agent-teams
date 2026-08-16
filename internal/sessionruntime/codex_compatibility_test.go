package sessionruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckCodexCompatibilityStates(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		report := CheckCodexCompatibility(context.Background(), filepath.Join(t.TempDir(), "missing-codex"))
		if report.State != CodexAbsent {
			t.Fatalf("report = %+v", report)
		}
	})

	t.Run("incompatible", func(t *testing.T) {
		executable := fakeCodexCommand(t, `printf '%s\n' 'standalone installation required' >&2
exit 1`)
		report := CheckCodexCompatibility(context.Background(), executable)
		if report.State != CodexIncompatible || !strings.Contains(report.Detail, "standalone installation required") {
			t.Fatalf("report = %+v", report)
		}
		if err := RequireCompatibleCodex(context.Background(), executable); err == nil || !strings.Contains(err.Error(), "official standalone") {
			t.Fatalf("require error = %v", err)
		}
	})

	t.Run("compatible", func(t *testing.T) {
		executable := fakeCodexCommand(t, `printf '%s\n' '{"status":"stopped","managedCodexPath":"/standalone/codex","managedCodexVersion":"0.146.1","cliVersion":"0.146.1"}'`)
		report := CheckCodexCompatibility(context.Background(), executable)
		if report.State != CodexCompatible || report.ManagedVersion != "0.146.1" || report.ManagedCodexPath != "/standalone/codex" {
			t.Fatalf("report = %+v", report)
		}
		if err := RequireCompatibleCodex(context.Background(), executable); err != nil {
			t.Fatalf("require: %v", err)
		}
	})
}
