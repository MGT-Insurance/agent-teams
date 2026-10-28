package workspace

import (
	"os"
	"testing"
)

func TestHost_EnvOverrides(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOST", "sandbox-ring1")
	if got := Host(); got != "sandbox-ring1" {
		t.Errorf("Host() = %q, want sandbox-ring1", got)
	}
}

func TestHost_EmptyEnvFallsBackToHostname(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOST", "")
	want, err := os.Hostname()
	if err != nil {
		want = ""
	}
	if got := Host(); got != want {
		t.Errorf("Host() = %q, want %q", got, want)
	}
}
