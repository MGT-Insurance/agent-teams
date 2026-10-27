package verbs

import (
	"errors"
	"reflect"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
)

// recordingBD records every argv and answers from a per-call script. A call
// past the end of the script returns the zero response.
type recordingBD struct {
	calls   [][]string
	runResp []bdResp
	jsonErr []error
	issues  []bd.Issue
}

type bdResp struct {
	out string
	err error
}

func (r *recordingBD) Run(args ...string) (string, error) {
	i := len(r.calls)
	r.calls = append(r.calls, args)
	if i < len(r.runResp) {
		return r.runResp[i].out, r.runResp[i].err
	}
	return "", nil
}

func (r *recordingBD) RunJSON(dst any, args ...string) error {
	i := len(r.calls)
	r.calls = append(r.calls, args)
	if i < len(r.jsonErr) && r.jsonErr[i] != nil {
		return r.jsonErr[i]
	}
	if r.issues != nil {
		return unmarshalIssues(dst, r.issues...)
	}
	return nil
}

func TestListOpenIssues_Argv(t *testing.T) {
	cases := []struct {
		infra bool
		want  []string
	}{
		{false, []string{"list", "--status=open", "--limit=0", "--json"}},
		{true, []string{"list", "--status=open", "--include-infra", "--limit=0", "--json"}},
	}
	for _, c := range cases {
		f := &recordingBD{issues: []bd.Issue{{ID: "at-1"}}}
		ctx, _, _ := makeCtx(f, t.TempDir())
		got, err := listOpenIssues(ctx, c.infra)
		if err != nil {
			t.Fatalf("listOpenIssues(%v): %v", c.infra, err)
		}
		if len(got) != 1 || got[0].ID != "at-1" {
			t.Errorf("listOpenIssues(%v) = %+v", c.infra, got)
		}
		if len(f.calls) != 1 || !reflect.DeepEqual(f.calls[0], c.want) {
			t.Errorf("listOpenIssues(%v) argv = %v, want %v", c.infra, f.calls, c.want)
		}
	}
}

func TestListOpenIssues_ErrorNotRetried(t *testing.T) {
	f := &recordingBD{jsonErr: []error{errors.New("boom")}}
	ctx, _, _ := makeCtx(f, t.TempDir())
	if _, err := listOpenIssues(ctx, false); err == nil {
		t.Fatal("want error")
	}
	if len(f.calls) != 1 {
		t.Errorf("calls = %d, want 1", len(f.calls))
	}
}

func TestInitiativeRows_DropsInfraAndEphemeral(t *testing.T) {
	in := []bd.Issue{
		{ID: "task", IssueType: "task"},
		{ID: "epic", IssueType: "epic"},
		{ID: "msg", IssueType: "message"},
		{ID: "agent", IssueType: "agent"},
		{ID: "role", IssueType: "role"},
		{ID: "wisp", IssueType: "task", Ephemeral: true},
	}
	got := initiativeRows(in)
	var ids []string
	for _, iss := range got {
		ids = append(ids, iss.ID)
	}
	if want := []string{"task", "epic"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("initiativeRows ids = %v, want %v", ids, want)
	}
}

func TestMatchInitiativeBySession(t *testing.T) {
	cases := []struct {
		name      string
		issues    []bd.Issue
		runtime   sessionruntime.Kind
		sessionID string
		wantID    string
	}{
		{"empty id", []bd.Issue{{ID: "a", Description: "session: s\n"}}, sessionruntime.Claude, "", ""},
		{"zero matches", []bd.Issue{{ID: "a", Description: "session: other\n"}}, sessionruntime.Claude, "s", ""},
		{"one match", []bd.Issue{
			{ID: "a", Description: "session: other\n"},
			{ID: "b", Description: "session: s\n"},
		}, sessionruntime.Claude, "s", "b"},
		{"two same-runtime", []bd.Issue{
			{ID: "a", Description: "session: s\n"},
			{ID: "b", Description: "session: s\n"},
		}, sessionruntime.Claude, "s", ""},
		{"other runtime excluded", []bd.Issue{
			{ID: "a", Description: "runtime: codex\nsession: s\n"},
		}, sessionruntime.Claude, "s", ""},
		{"other runtime does not make a dup", []bd.Issue{
			{ID: "a", Description: "runtime: codex\nsession: s\n"},
			{ID: "b", Description: "session: s\n"},
		}, sessionruntime.Claude, "s", "b"},
		{"invalid runtime skipped", []bd.Issue{
			{ID: "a", Description: "runtime: bogus\nsession: s\n"},
			{ID: "b", Description: "session: s\n"},
		}, sessionruntime.Claude, "s", "b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			iss, ok := matchInitiativeBySession(c.issues, c.runtime, c.sessionID)
			if ok != (c.wantID != "") || iss.ID != c.wantID {
				t.Errorf("got (%q, %v), want id %q", iss.ID, ok, c.wantID)
			}
		})
	}
}

func TestReadMemoriesJSON(t *testing.T) {
	ro := []string{"--readonly", "memories", "--json"}
	rw := []string{"memories", "--json"}

	t.Run("readonly succeeds, no retry", func(t *testing.T) {
		f := &recordingBD{}
		ctx, _, _ := makeCtx(f, t.TempDir())
		var dst map[string]any
		if err := readMemoriesJSON(ctx, &dst); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.calls, [][]string{ro}) {
			t.Errorf("calls = %v", f.calls)
		}
	})
	t.Run("readonly fails, retried once", func(t *testing.T) {
		f := &recordingBD{jsonErr: []error{errors.New("ro")}}
		ctx, _, _ := makeCtx(f, t.TempDir())
		var dst map[string]any
		if err := readMemoriesJSON(ctx, &dst); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.calls, [][]string{ro, rw}) {
			t.Errorf("calls = %v", f.calls)
		}
	})
	t.Run("both fail, retry error returned", func(t *testing.T) {
		f := &recordingBD{jsonErr: []error{errors.New("ro"), errors.New("rw")}}
		ctx, _, _ := makeCtx(f, t.TempDir())
		var dst map[string]any
		err := readMemoriesJSON(ctx, &dst)
		if err == nil || err.Error() != "rw" {
			t.Errorf("err = %v, want rw", err)
		}
		if len(f.calls) != 2 {
			t.Errorf("calls = %d, want 2", len(f.calls))
		}
	})
}

func TestReadConfigValue(t *testing.T) {
	ro := []string{"--readonly", "config", "get", "k"}
	rw := []string{"config", "get", "k"}

	t.Run("readonly succeeds, no retry", func(t *testing.T) {
		f := &recordingBD{runResp: []bdResp{{out: "v"}}}
		ctx, _, _ := makeCtx(f, t.TempDir())
		got, err := readConfigValue(ctx, "k")
		if err != nil || got != "v" {
			t.Fatalf("got (%q, %v)", got, err)
		}
		if !reflect.DeepEqual(f.calls, [][]string{ro}) {
			t.Errorf("calls = %v", f.calls)
		}
	})
	t.Run("readonly fails, retried once", func(t *testing.T) {
		f := &recordingBD{runResp: []bdResp{{err: errors.New("ro")}, {out: "v2"}}}
		ctx, _, _ := makeCtx(f, t.TempDir())
		got, err := readConfigValue(ctx, "k")
		if err != nil || got != "v2" {
			t.Fatalf("got (%q, %v)", got, err)
		}
		if !reflect.DeepEqual(f.calls, [][]string{ro, rw}) {
			t.Errorf("calls = %v", f.calls)
		}
	})
	t.Run("both fail", func(t *testing.T) {
		f := &recordingBD{runResp: []bdResp{{err: errors.New("ro")}, {err: errors.New("rw")}}}
		ctx, _, _ := makeCtx(f, t.TempDir())
		if _, err := readConfigValue(ctx, "k"); err == nil || err.Error() != "rw" {
			t.Errorf("err = %v, want rw", err)
		}
		if len(f.calls) != 2 {
			t.Errorf("calls = %d, want 2", len(f.calls))
		}
	})
}
