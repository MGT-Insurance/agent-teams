package verbs_test

import (
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/verbs"
)

var allVerbs = []string{
	// Track A
	"ws", "list", "list-json", "human-list", "show", "learnings", "recall", "prime", "roles",
	// Track B
	"audit", "resume-match", "resume-match-closed",
	// Track C
	"register", "note", "gate", "clear-gate", "learn", "close", "reopen", "pull", "sync",
	"forget", "condense", "fresh-drain", "condense-lock",
	// Track D
	"new-initiative", "dispatch", "resume",
	// Track GO
	"worktree-setup",
	// Track MS
	"send", "inbox",
	// Track R
	"route-pr-event",
	// Track S
	"execution-status",
	// Track A (watchers)
	"watchers",
	// Track C (cost)
	"cost",
}

// buildParser registers all verbs onto a kong Parser and returns it.
func buildParser(t *testing.T) *cli.Parser {
	t.Helper()
	p, err := cli.NewParser(kong.Exit(func(int) {}))
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	verbs.RegisterAllKong(p)
	return p
}

// TestAllVerbsRegistered confirms every expected verb is parseable from the
// full kong registration. Verbs with required positionals/flags may return
// validation errors when invoked without args; what matters is that they are
// NOT reported as unknown commands ("unexpected argument <name>").
func TestAllVerbsRegistered(t *testing.T) {
	for _, name := range allVerbs {
		t.Run(name, func(t *testing.T) {
			p := buildParser(t)
			// --help triggers Exit(0) for verbs that need no args; for verbs
			// with required args, Validate may fire first. Either way, an
			// "unexpected argument <name>" error means the verb isn't registered.
			_, err := p.Parse([]string{name, "--help"})
			if err != nil && strings.Contains(err.Error(), "unexpected argument "+name) {
				t.Errorf("verb %q: not registered (unexpected argument): %v", name, err)
			}
		})
	}
}

// TestNoDuplicateRegistration confirms RegisterAllKong doesn't panic on
// duplicate registration.
func TestNoDuplicateRegistration(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("RegisterAllKong panicked (duplicate): %v", r)
		}
	}()
	buildParser(t)
}
