package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func fakeHostname(t *testing.T, name string) {
	t.Helper()
	old := hostname
	hostname = func() (string, error) { return name, nil }
	t.Cleanup(func() { hostname = old })
}

func TestHost_EnvOverrides(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOST", "sandbox-ring1")
	home := t.TempDir()
	if got := Host(home); got != "sandbox-ring1" {
		t.Errorf("Host() = %q, want sandbox-ring1", got)
	}
	if _, err := os.Stat(filepath.Join(home, HostFile)); err == nil {
		t.Error("env override must not write the host file")
	}
}

func TestHost_FirstCallSeedsFileFromHostname(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOST", "")
	fakeHostname(t, "Erics-MacBook-Pro.local")
	home := t.TempDir()
	if got := Host(home); got != "Erics-MacBook-Pro.local" {
		t.Fatalf("Host() = %q", got)
	}
	b, err := os.ReadFile(filepath.Join(home, HostFile))
	if err != nil || string(b) != "Erics-MacBook-Pro.local\n" {
		t.Errorf("host file = %q, %v", b, err)
	}
}

func TestHost_PersistedIdSurvivesHostnameDrift(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOST", "")
	home := t.TempDir()
	fakeHostname(t, "Erics-MacBook-Pro.local")
	first := Host(home)
	fakeHostname(t, "Erics-MacBook-Pro-2.local")
	if got := Host(home); got != first {
		t.Errorf("Host() after drift = %q, want %q", got, first)
	}
}

func TestHost_UnwritableHomeFallsBack(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOST", "")
	fakeHostname(t, "h1")
	home := filepath.Join(t.TempDir(), "missing", "dir")
	if got := Host(home); got != "h1" {
		t.Errorf("Host() = %q, want h1", got)
	}
}

func TestHost_HostnameErrorYieldsEmpty(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOST", "")
	old := hostname
	hostname = func() (string, error) { return "", errors.New("boom") }
	t.Cleanup(func() { hostname = old })
	if got := Host(t.TempDir()); got != "" {
		t.Errorf("Host() = %q, want empty", got)
	}
}
