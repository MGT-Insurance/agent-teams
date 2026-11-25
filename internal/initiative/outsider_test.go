package initiative_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

const activeLine = "gates=plan,scope; until=2026-11-26T12:00:00Z; by=eric; via=steward; ref=telegram:123"

func TestOutsiderFieldsRoundTrip(t *testing.T) {
	f := initiative.Fields{
		Problem: "p", Repo: "/r", Origin: "outsider", Requester: "joe/canary",
		Request: "at-q7k2", StewardApproves: []string{"plan", "delivery"}, Autonomy: activeLine,
	}
	plan, err := initiative.New(f)
	if err != nil {
		t.Fatal(err)
	}
	iss := bd.Issue{Description: plan.Description + "pr-url: https://x/y\n"}
	got := initiative.Of(iss)
	if !reflect.DeepEqual(got, f) {
		t.Errorf("Of = %+v, want %+v", got, f)
	}
	jf := initiative.JSONFields(iss)
	for k, v := range map[string]string{"origin": "outsider", "requester": "joe/canary", "request": "at-q7k2", "steward-approves": "plan,delivery", "autonomy": activeLine, "pr-url": "https://x/y"} {
		if jf[k] != v {
			t.Errorf("JSONFields[%s] = %v, want %q", k, jf[k], v)
		}
	}
}

func TestNewRejectsBadOutsiderFields(t *testing.T) {
	for _, f := range []initiative.Fields{
		{Origin: "insider"},
		{Requester: "Joe/canary"},
		{Requester: "joe"},
		{Requester: "a/b/c"},
		{StewardApproves: []string{"merge"}},
		{Autonomy: "gates=plan"},
		{Request: "a\nb"},
	} {
		if _, err := initiative.New(f); err == nil {
			t.Errorf("New(%+v): want error", f)
		}
	}
}

func TestOldInitiativeReadsUnchanged(t *testing.T) {
	plan, err := initiative.New(initiative.Fields{Problem: "p", Repo: "/r", Branch: "b", Sessions: []string{"s1"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "problem: p\nrepo: /r\nbranch: b\nsession: s1\n"; plan.Description != want {
		t.Errorf("golden: %q, want %q", plan.Description, want)
	}
	f := initiative.Of(bd.Issue{Description: plan.Description})
	if f.Origin != "" || f.Requester != "" || f.Request != "" || f.StewardApproves != nil || f.Autonomy != "" {
		t.Errorf("new fields not zero: %+v", f)
	}
}

func TestWithAutonomy(t *testing.T) {
	apply := func(desc, v string) string {
		t.Helper()
		p, err := initiative.WithAutonomy(bd.Issue{Description: desc}, v)
		if err != nil {
			t.Fatal(err)
		}
		return p.Description
	}
	if got, want := apply("problem: p\nbody\n", activeLine), "problem: p\nbody\nautonomy: "+activeLine+"\n"; got != want {
		t.Errorf("append: %q", got)
	}
	revoked := "revoked 2026-11-25T10:00:00Z; ref=telegram:9"
	before := "problem: p\nautonomy: " + activeLine + "\nbranch: b\nprose: x\n"
	if got, want := apply(before, revoked), "problem: p\nautonomy: "+revoked+"\nbranch: b\nprose: x\n"; got != want {
		t.Errorf("replace: %q", got)
	}
	for _, bad := range []string{"", "gates=plan", "revoked yesterday; ref=x", "gates=merge; until=2026-11-26T12:00:00Z; by=eric; via=steward; ref=x", activeLine + "\nx"} {
		if _, err := initiative.WithAutonomy(bd.Issue{}, bad); err == nil {
			t.Errorf("WithAutonomy(%q): want error", bad)
		}
	}
}

func TestActiveAutonomy(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	fl := func(v string) initiative.Fields { return initiative.Fields{Autonomy: v} }
	gates, ref, ok := initiative.ActiveAutonomy(fl(activeLine), now)
	if !ok || ref != "telegram:123" || !reflect.DeepEqual(gates, []string{"plan", "scope"}) {
		t.Errorf("active: %v %q %v", gates, ref, ok)
	}
	cases := map[string]string{
		"absent":    "",
		"expired":   strings.Replace(activeLine, "2026-11-26T12:00:00Z", "2026-11-25T11:59:59Z", 1),
		"at-until":  strings.Replace(activeLine, "2026-11-26T12:00:00Z", "2026-11-25T12:00:00Z", 1),
		"over-72h":  strings.Replace(activeLine, "2026-11-26T12:00:00Z", "2026-11-28T12:00:01Z", 1),
		"revoked":   "revoked 2026-11-25T10:00:00Z; ref=x",
		"malformed": "gates=plan; until=soon",
		"bad-class": strings.Replace(activeLine, "plan,scope", "merge", 1),
		"bad-by":    strings.Replace(activeLine, "by=eric", "by=joe", 1),
		"non-utc":   strings.Replace(activeLine, "12:00:00Z", "12:00:00+02:00", 1),
	}
	for name, v := range cases {
		if _, _, ok := initiative.ActiveAutonomy(fl(v), now); ok {
			t.Errorf("%s: want inactive", name)
		}
	}
	if _, _, ok := initiative.ActiveAutonomy(fl(strings.Replace(activeLine, "2026-11-26T12:00:00Z", "2026-11-28T12:00:00Z", 1)), now); !ok {
		t.Errorf("exactly 72h: want active")
	}
}
