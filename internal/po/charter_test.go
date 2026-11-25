package po

import (
	"os"
	"strings"
	"testing"
	"time"
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

func checkWithTimeout(t *testing.T, src string) []Violation {
	t.Helper()
	done := make(chan []Violation, 1)
	go func() {
		_, v := CheckCharter([]byte(src))
		done <- v
	}()
	select {
	case v := <-done:
		return v
	case <-time.After(3 * time.Second):
		t.Fatalf("CheckCharter hung on %q", src)
		return nil
	}
}

func TestMalformedFlowDoesNotHang(t *testing.T) {
	for _, repos := range []string{"[a, }]", "[: ]", "{a: 1, ]}", "[a, ]]"} {
		v := checkWithTimeout(t, "---\narea: x\nrepos: "+repos+"\n---\n")
		if len(v) != 1 || v[0].Rule != RuleParse {
			t.Errorf("repos %s: want one parse violation, got %+v", repos, v)
		}
	}
}

func TestEmptyFlowMapValueStillParses(t *testing.T) {
	src := strings.Replace(sample(t), `target: {op: "<=", value: 5}`, `target: {op: "<=", value: 5, note: }`, 1)
	if _, v := CheckCharter([]byte(src)); len(v) != 0 {
		t.Fatalf("violations: %+v", v)
	}
}

func TestNestedDecodeErrorsSurface(t *testing.T) {
	for name, repl := range map[string][2]string{
		"signals list":     {"    signals: [quote-p95]\n", "    signals: [[a], b]\n"},
		"match not map":    {"    match:\n      words: [bind]", "    match: oops"},
		"signal id nest":   {"  - id: quote-p95\n", "  - id: [x]\n"},
		"target not map":   {"    target: {op: \"<=\", value: 5}", "    target: 5"},
		"match words nest": {"      words: [bind]", "      words: [[bind]]"},
	} {
		src := strings.Replace(sample(t), repl[0], repl[1], 1)
		if src == sample(t) {
			t.Fatalf("%s: replacement did not apply", name)
		}
		v := checkWithTimeout(t, src)
		if len(v) != 1 || v[0].Rule != RuleParse {
			t.Errorf("%s: want parse violation, got %+v", name, v)
		}
	}
}

func TestSignalAndAreaRules(t *testing.T) {
	cases := map[string][3]string{
		RuleSignalSource: {"source: datadog", "source: grafana", RuleSignalSource},
		RuleTargetOp:     {`op: "<="`, `op: "=="`, RuleTargetOp},
		RuleTargetValue:  {"value: 5}", "value: .nan}", RuleTargetValue},
		"inf":            {"value: 5}", "value: .inf}", RuleTargetValue},
		RuleAreaInvalid:  {"area: quoting", "area: hot", RuleAreaInvalid},
	}
	for name, c := range cases {
		src := strings.Replace(sample(t), c[0], c[1], 1)
		if name == "inf" {
			src = strings.Replace(sample(t), c[0], "value: Inf}", 1)
		}
		if name == RuleTargetValue {
			src = strings.Replace(sample(t), c[0], "value: NaN}", 1)
		}
		if got := rules(checkWithTimeout(t, src)); got != c[2] {
			t.Errorf("%s: rules=%q want %q", name, got, c[2])
		}
	}
}
