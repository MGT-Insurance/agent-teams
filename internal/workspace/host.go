package workspace

import "os"

// Host returns this machine's host identity: $AGENT_TEAMS_HOST if set and
// non-empty, else os.Hostname(), or "" when the hostname cannot be read. The
// sandbox kit sets AGENT_TEAMS_HOST so a sandbox is distinguishable from the
// laptop (docs/hosted-beads.md, F5).
func Host() string {
	if h := os.Getenv("AGENT_TEAMS_HOST"); h != "" {
		return h
	}
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}
