package verbs_test

import (
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/verbs"
)

var allVerbs = []string{
	// Track A
	"ws", "list", "list-json", "human-list", "show", "learnings", "prime",
	// Track B
	"audit", "resume-match", "resume-match-closed",
	// Track C
	"register", "note", "gate", "clear-gate", "learn", "close", "reopen", "sync",
	// Track D
	"new-initiative", "dispatch", "resume",
	// Track GO
	"worktree-setup",
	// Track R
	"route-pr-event",
}

func buildRegistry() cli.Registry {
	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	verbs.RegisterMatch(reg)
	verbs.RegisterWrite(reg, nil)
	verbs.RegisterDispatch(reg)
	verbs.RegisterWorktreeSetup(reg)
	verbs.RegisterRouteEvent(reg)
	return reg
}

// TestAllVerbsRegistered confirms every expected verb is present in the registry.
func TestAllVerbsRegistered(t *testing.T) {
	reg := buildRegistry()
	for _, name := range allVerbs {
		cmd, ok := reg.Lookup(name)
		if !ok {
			t.Errorf("verb %q not registered", name)
			continue
		}
		if cmd.Name() != name {
			t.Errorf("verb %q: Name() = %q", name, cmd.Name())
		}
	}
}

// TestStubReturnsNotImplemented confirms stubs return an error (not nil, not panic).
func TestStubReturnsNotImplemented(t *testing.T) {
	reg := buildRegistry()
	for _, name := range allVerbs {
		cmd, ok := reg.Lookup(name)
		if !ok {
			continue
		}
		err := cmd.Run(nil, nil)
		if err == nil {
			t.Errorf("verb %q stub returned nil error, want not-implemented error", name)
		}
	}
}

// TestNoDuplicateRegistration confirms RegisterX funcs don't collide.
func TestNoDuplicateRegistration(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("registration panicked (duplicate): %v", r)
		}
	}()
	buildRegistry()
}
