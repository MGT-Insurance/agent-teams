// Package verbs contains per-track verb registration functions.
// This file is owned by Track A (read/query verbs).
package verbs

import "github.com/erlloyd/agent-teams/internal/cli"

// RegisterQuery registers the read/query verbs:
// ws, list, list-json, human-list, show, learnings.
//
// NOTE: ws is also special-cased in main before workspace initialization is
// checked; it is registered here for completeness and usage listing.
func RegisterQuery(reg cli.Registry) {
	for _, name := range []string{"ws", "list", "list-json", "human-list", "show", "learnings"} {
		reg.Register(stub(name))
	}
}
