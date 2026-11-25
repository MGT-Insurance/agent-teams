package po

import (
	"os"
	"strings"
	"testing"
)

func sample(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/sample-charter.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func rules(v []Violation) string {
	var r []string
	for _, x := range v {
		r = append(r, x.Rule)
	}
	return strings.Join(r, ",")
}

func TestSampleCharterValid(t *testing.T) {
	c, v := CheckCharter([]byte(sample(t)))
	if len(v) != 0 {
		t.Fatalf("violations: %+v", v)
	}
	if c.Area != "quoting" || len(c.Features) != 2 || len(c.Goals) != 2 || len(c.Signals) != 2 || len(c.Fences) != 1 {
		t.Fatalf("bad parse: %+v", c)
	}
	if !strings.HasPrefix(c.Mission, "Make quoting fast") || c.Reach.TopicTitle != "PO: Quoting" || !c.Reach.Summary {
		t.Fatalf("bad scalars: %q %+v", c.Mission, c.Reach)
	}
	s0, s1 := c.Signals[0], c.Signals[1]
	if s0.Query != "p95:quote.latency{*}" || s0.Target == nil || s0.Target.Op != "<=" || s0.Target.Value != 5 {
		t.Fatalf("signal0: %+v", s0)
	}
	if s1.Target == nil || s1.Target.Op != ">=" || s1.Target.Value != 0.95 {
		t.Fatalf("signal1: %+v", s1)
	}
	f := c.Features[0]
	if len(f.Match.Words) != 3 || f.Match.Paths[0] != "apps/shadowfax/**" || f.Match.Services[0] != "shadowfax" {
		t.Fatalf("feature: %+v", f)
	}
	if len(c.Goals[1].Signals) != 2 || len(c.Repos) != 2 {
		t.Fatalf("lists: %+v", c)
	}
}

func TestViolations(t *testing.T) {
	base := sample(t)
	cases := []struct {
		name, rule string
		mut        func(string) string
	}{
		{"missing goal", RuleGoalRequired, func(s string) string {
			i, j := strings.Index(s, "\ngoals:"), strings.Index(s, "\nsignals:")
			return s[:i] + s[j:]
		}},
		{"unresolved signal", RuleSignalUnres, func(s string) string {
			return strings.Replace(s, "signals: [quote-p95]", "signals: [nope]", 1)
		}},
		{"duplicate id", RuleIDDuplicate, func(s string) string {
			return strings.Replace(s, "id: bind", "id: rating", 1)
		}},
		{"backlog key", RuleBacklogKey, func(s string) string {
			return strings.Replace(s, "fences:", "backlog:\n  - fix it\nfences:", 1)
		}},
		{"parse", RuleParse, func(s string) string { return "no front matter" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, v := CheckCharter([]byte(c.mut(base)))
			if rules(v) != c.rule {
				t.Fatalf("rules = %q, want %q (%+v)", rules(v), c.rule, v)
			}
		})
	}
}
