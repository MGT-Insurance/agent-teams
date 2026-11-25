package initiative_test

import (
	"reflect"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

func TestOutsiderFieldsRoundTrip(t *testing.T) {
	f := initiative.Fields{
		Problem: "p", Repo: "/r", Origin: "outsider", Requester: "joe/canary",
		Request: "at-q7k2", StewardApproves: []string{"plan", "delivery"},
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
	for k, v := range map[string]string{"origin": "outsider", "requester": "joe/canary", "request": "at-q7k2", "steward-approves": "plan,delivery", "pr-url": "https://x/y"} {
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
	if f.Origin != "" || f.Requester != "" || f.Request != "" || f.StewardApproves != nil {
		t.Errorf("new fields not zero: %+v", f)
	}
}
