package initiative_test

import (
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

func TestWithWhereAndWorktree(t *testing.T) {
	type writer func(bd.Issue, string) (initiative.WritePlan, error)
	cases := []struct {
		name string
		key  string
		with writer
		read func(initiative.Fields) string
	}{
		{"where", "where", initiative.WithWhere, func(f initiative.Fields) string { return f.Where }},
		{"worktree", "worktree", initiative.WithWorktree, func(f initiative.Fields) string { return f.Worktree }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan := func(desc, value string) string {
				t.Helper()
				p, err := c.with(bd.Issue{ID: "at-sv1", Description: desc}, value)
				if err != nil {
					t.Fatalf("with(%q): %v", value, err)
				}
				return p.Description
			}

			// No line yet: appended below existing content.
			got := plan("problem: p\nbody text\n", "v1")
			if want := "problem: p\nbody text\n" + c.key + ": v1\n"; got != want {
				t.Errorf("append: got %q, want %q", got, want)
			}

			// Existing line: replaced in place, every other line byte-identical.
			before := "problem: p\n" + c.key + ": old\nbranch: b\nprose with: colon\n"
			got = plan(before, "new")
			if want := "problem: p\n" + c.key + ": new\nbranch: b\nprose with: colon\n"; got != want {
				t.Errorf("replace: got %q, want %q", got, want)
			}

			// Same value: description unchanged.
			if got = plan(before, "old"); got != before {
				t.Errorf("idempotent: got %q, want %q", got, before)
			}

			// Duplicate lines: only the first is replaced; Of reads the new value.
			dup := c.key + ": first\nmid: x\n" + c.key + ": second\n"
			got = plan(dup, "new")
			if want := c.key + ": new\nmid: x\n" + c.key + ": second\n"; got != want {
				t.Errorf("duplicate: got %q, want %q", got, want)
			}
			if v := c.read(initiative.Of(bd.Issue{Description: got})); v != "new" {
				t.Errorf("Of after duplicate replace = %q, want %q", v, "new")
			}

			// Invalid inputs are rejected.
			for _, bad := range []string{"", "a\nb", "a\rb", " lead", "trail ", "\ttab"} {
				if _, err := c.with(bd.Issue{Description: before}, bad); err == nil {
					t.Errorf("with(%q): got nil error, want rejection", bad)
				}
			}
		})
	}
}
