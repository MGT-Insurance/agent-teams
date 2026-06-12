// This file is owned by Track D (dispatch verbs).
package verbs

import "github.com/erlloyd/agent-teams/internal/cli"

// RegisterDispatch registers the dispatch verbs:
// new-initiative, dispatch.
func RegisterDispatch(reg cli.Registry) {
	for _, name := range []string{"new-initiative", "dispatch"} {
		reg.Register(stub(name))
	}
}
