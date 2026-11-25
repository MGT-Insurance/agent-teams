package verbs

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

func TestRm8Stubs_RegisteredInParser(t *testing.T) {
	p, err := cli.NewParser(kong.Exit(func(int) {}))
	if err != nil {
		t.Fatal(err)
	}
	RegisterAllKong(p)
	for _, args := range [][]string{
		{"pr", "context", "at-1"},
		{"gate", "at-1", "--decision", "d", "--class", "design-fork"},
	} {
		if _, err := p.Parse(args); err != nil {
			t.Errorf("Parse(%v): %v", args, err)
		}
	}
	if _, err := p.Parse([]string{"gate", "at-1", "--decision", "d", "--class", "delivery"}); err == nil {
		t.Error("--class delivery must be rejected by the enum (plan, scope, design-fork)")
	}
}
