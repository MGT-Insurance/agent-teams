package cli_test

import (
	"errors"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

func TestExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"UsageError", cli.Usagef("bad flag"), 2},
		{"DepError", cli.Depf("bd not found"), 3},
		{"WorkspaceError", cli.Workspacef("not initialized"), 4},
		{"SilentError code 1", cli.Silent(1), 1},
		{"SilentError code 5", cli.Silent(5), 5},
		{"generic error", errors.New("something broke"), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cli.ExitCode(tc.err)
			if got != tc.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestErrorMessages(t *testing.T) {
	u := cli.Usagef("missing %s", "arg")
	if u.Error() != "missing arg" {
		t.Errorf("UsageError.Error() = %q, want %q", u.Error(), "missing arg")
	}

	d := cli.Depf("bd not in PATH")
	if d.Error() != "bd not in PATH" {
		t.Errorf("DepError.Error() = %q", d.Error())
	}

	w := cli.Workspacef("no .beads at %s", "/home/x")
	if w.Error() != "no .beads at /home/x" {
		t.Errorf("WorkspaceError.Error() = %q", w.Error())
	}

	s := cli.Silent(1)
	if s.Error() != "exit 1" {
		t.Errorf("SilentError.Error() = %q", s.Error())
	}
}

func TestRegistryDuplicatePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration, got none")
		}
	}()
	reg := make(cli.Registry)
	cmd := &testCmd{name: "dup"}
	reg.Register(cmd)
	reg.Register(cmd) // should panic
}

func TestRegistryLookup(t *testing.T) {
	reg := make(cli.Registry)
	cmd := &testCmd{name: "myverb"}
	reg.Register(cmd)

	got, ok := reg.Lookup("myverb")
	if !ok || got != cmd {
		t.Errorf("Lookup(myverb) = %v, %v; want cmd, true", got, ok)
	}

	_, ok = reg.Lookup("nope")
	if ok {
		t.Error("Lookup(nope) returned true, want false")
	}
}

type testCmd struct{ name string }

func (c *testCmd) Name() string                         { return c.name }
func (c *testCmd) Run(_ *cli.Context, _ []string) error { return nil }

// ── Parser / kong contract tests ──────────────────────────────────────────────

// TestParserHelpExitsZero confirms that --help triggers Exit(0) (help was shown).
func TestParserHelpExitsZero(t *testing.T) {
	var exitCode *int
	p, err := cli.NewParser(kong.Exit(func(code int) { exitCode = &code }))
	if err != nil {
		t.Fatal(err)
	}
	p.AddVerb("myverb", "a test verb", &trivialKongVerb{})

	_, _ = p.Parse([]string{"--help"})
	if exitCode == nil {
		t.Error("Exit was not called; expected help to trigger Exit(0)")
	} else if *exitCode != 0 {
		t.Errorf("Exit(%d), want Exit(0)", *exitCode)
	}
}

// TestParserBridgeDispatch confirms AddBridgeVerb forwards raw args to the legacy cmd.
func TestParserBridgeDispatch(t *testing.T) {
	var got []string
	bridge := &captureCmd{name: "cap", fn: func(args []string) error {
		got = args
		return nil
	}}

	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	p.AddBridgeVerb(bridge)

	kctx, parseErr := p.Parse([]string{"cap", "arg1", "arg2"})
	if parseErr != nil {
		t.Fatalf("Parse: %v", parseErr)
	}
	cliCtx := &cli.Context{}
	kctx.Bind(cliCtx)
	if runErr := kctx.Run(cliCtx); runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
	if len(got) != 2 || got[0] != "arg1" || got[1] != "arg2" {
		t.Errorf("forwarded args = %v, want [arg1 arg2]", got)
	}
}

// TestParserUnknownVerbError confirms that an unknown verb produces a parse error.
func TestParserUnknownVerbError(t *testing.T) {
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	p.AddVerb("known", "a known verb", &trivialKongVerb{})

	_, parseErr := p.Parse([]string{"unknown-xyzzy"})
	if parseErr == nil {
		t.Error("expected parse error for unknown verb, got nil")
	}
}

// trivialKongVerb is the minimal kong-style verb struct with Run(*cli.Context) error.
type trivialKongVerb struct{}

func (v *trivialKongVerb) Run(ctx *cli.Context) error { return nil }

// captureCmd is a legacy cli.Command that records args passed to Run.
type captureCmd struct {
	name string
	fn   func([]string) error
}

func (c *captureCmd) Name() string                            { return c.name }
func (c *captureCmd) Run(_ *cli.Context, args []string) error { return c.fn(args) }
