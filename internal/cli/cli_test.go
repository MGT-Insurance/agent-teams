package cli_test

import (
	"errors"
	"testing"

	"github.com/erlloyd/agent-teams/internal/cli"
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
