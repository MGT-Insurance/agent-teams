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
		Request: "at-q7k2",
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
	for k, v := range map[string]string{"origin": "outsider", "requester": "joe/canary", "request": "at-q7k2", "pr-url": "https://x/y"} {
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
		{Requester: "eric.lloyd/.steward"},
		{Requester: "eric..lloyd/steward"},
		{Requester: "eric.lloyd/steward."},
		{Requester: "-eric/steward"},
		{Requester: "eric/st-.eward"},
		{Request: "a\nb"},
		{Reviewer: "some-user"},
		{Origin: "outsider", Reviewer: "-bad"},
		{Origin: "outsider", Reviewer: "bad-"},
		{Origin: "outsider", Reviewer: "ba--d"},
		{Origin: "outsider", Reviewer: "bad_user"},
		{Origin: "outsider", Reviewer: "a1234567890123456789012345678901234567890"},
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
	if f.Origin != "" || f.Requester != "" || f.Request != "" {
		t.Errorf("new fields not zero: %+v", f)
	}
}

func TestDottedRequesterAccepted(t *testing.T) {
	for _, r := range []string{"eric.lloyd/steward", "joe.b/scout-2", "joe/canary"} {
		if _, err := initiative.New(initiative.Fields{Problem: "p", Repo: "/r", Requester: r}); err != nil {
			t.Errorf("requester %q: %v", r, err)
		}
	}
}

func TestLegacyStewardApprovesLineIgnored(t *testing.T) {
	iss := bd.Issue{Description: "problem: p\nrepo: /r\norigin: outsider\nsteward-approves: plan,delivery\n"}
	f := initiative.Of(iss)
	if f.Problem != "p" || f.Origin != "outsider" {
		t.Errorf("Of = %+v", f)
	}
}

func TestReviewerStored(t *testing.T) {
	f := initiative.Fields{Problem: "p", Repo: "/r", Origin: "outsider", Requester: "joe/canary", Request: "at-q7k2", Reviewer: "Some-User9"}
	plan, err := initiative.New(f)
	if err != nil {
		t.Fatal(err)
	}
	if got := initiative.Of(bd.Issue{Description: plan.Description}); got.Reviewer != "Some-User9" {
		t.Errorf("Reviewer = %q", got.Reviewer)
	}
	// Of never rejects stored data, even outside the outsider rules.
	if got := initiative.Of(bd.Issue{Description: "reviewer: -weird-\n"}); got.Reviewer != "-weird-" {
		t.Errorf("Of rejected stored reviewer: %q", got.Reviewer)
	}
}
