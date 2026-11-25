package verbs

import (
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// TestRm8Stubs_FlagsParseAndErrorWithBeadID proves the front-loaded rm8 seams
// (agent-teams-lrj.1) parse and report which bead owns them.
func TestRm8Stubs_FlagsParseAndErrorWithBeadID(t *testing.T) {
	ctx, _ := newCtx(t, nil)

	gate := &gateKong{ID: "at-1", Class: "plan"}
	if err := gate.Run(ctx); err == nil || !strings.Contains(err.Error(), "agent-teams-lrj.3") {
		t.Errorf("gate --class: err = %v, want not implemented (agent-teams-lrj.3)", err)
	}
	clr := &clearGateKong{ID: "at-1", Autonomy: "telegram:1"}
	if err := clr.Run(ctx); err == nil || !strings.Contains(err.Error(), "agent-teams-lrj.8") {
		t.Errorf("clear-gate --autonomy: err = %v, want not implemented (agent-teams-lrj.8)", err)
	}
	for _, run := range []func(*cli.Context) error{(&autonomyGrantKong{}).Run, (&autonomyRevokeKong{}).Run, (&autonomyShowKong{}).Run} {
		if err := run(ctx); err == nil || !strings.Contains(err.Error(), "agent-teams-lrj.8") {
			t.Errorf("autonomy stub: err = %v", err)
		}
	}
	if err := gateClassClear(ctx, &clearGateKong{}); err != nil {
		t.Errorf("gateClassClear must be a no-op, got %v", err)
	}
}

func TestRm8Stubs_RegisteredInParser(t *testing.T) {
	p, err := cli.NewParser(kong.Exit(func(int) {}))
	if err != nil {
		t.Fatal(err)
	}
	RegisterAllKong(p)
	for _, args := range [][]string{
		{"autonomy", "grant", "at-1"},
		{"autonomy", "revoke", "at-1"},
		{"autonomy", "show", "at-1"},
		{"pr", "context", "at-1"},
		{"clear-gate", "at-1", "--autonomy", "ref"},
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
