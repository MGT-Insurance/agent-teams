// This file is owned by Track B (JSON-parsing verbs).
package verbs

import "github.com/erlloyd/agent-teams/internal/cli"

// RegisterMatch registers the JSON-parsing verbs:
// audit, resume-match, resume-match-closed.
func RegisterMatch(reg cli.Registry) {
	for _, name := range []string{"audit", "resume-match", "resume-match-closed"} {
		reg.Register(stub(name))
	}
}
