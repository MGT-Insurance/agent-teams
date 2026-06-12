// This file is owned by Track C (write verbs).
package verbs

import "github.com/erlloyd/agent-teams/internal/cli"

// RegisterWrite registers the write verbs:
// register, note, gate, clear-gate, learn, close, reopen, sync.
func RegisterWrite(reg cli.Registry) {
	for _, name := range []string{"register", "note", "gate", "clear-gate", "learn", "close", "reopen", "sync"} {
		reg.Register(stub(name))
	}
}
