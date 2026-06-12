// Package workspace resolves and inspects the agent-teams workspace directory.
package workspace

import (
	"os"
	"path/filepath"
)

// Home returns the workspace root directory. It resolves as:
//
//	$AGENT_TEAMS_HOME        if set and non-empty
//	$HOME/.agent-teams       otherwise
func Home() string {
	if h := os.Getenv("AGENT_TEAMS_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// Fall back to literal $HOME expansion if UserHomeDir fails.
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".agent-teams")
}

// Initialized reports whether home is an initialized agent-teams workspace,
// defined as the presence of a ".beads" directory inside home.
func Initialized(home string) bool {
	info, err := os.Stat(filepath.Join(home, ".beads"))
	return err == nil && info.IsDir()
}
