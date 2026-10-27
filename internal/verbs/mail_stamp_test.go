package verbs

import (
	"path/filepath"
	"strings"
	"testing"
)

func hasArgPrefix(args []string, prefix string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

func TestSendKong_Hosted_StampsToHostLabelAtCreate(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOST", "laptop")
	sf := newSendFixture(t)
	cmd := remoteSendCmd(t, sf, "at-remote")
	ctx, _, _ := makeCtx(whereBD(sf, "at-remote", "at-msg1", "host:sandbox-1"), markHosted(t, sf.home))
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, sf.createArgs, "--labels=to-host:sandbox-1", "create call missing the to-host stamp")
	assertContains(t, sf.createArgs, "--labels=delivery:pending", "create call missing delivery:pending")
}

func TestSendKong_Hosted_RecipientWithoutHostWhere_ErrorsAndCreatesNothing(t *testing.T) {
	for _, where := range []string{"", "run:at-abc", "host:"} {
		t.Run("where="+where, func(t *testing.T) {
			t.Setenv("AGENT_TEAMS_HOST", "laptop")
			sf := newSendFixture(t)
			cmd := remoteSendCmd(t, sf, "at-nowhere")
			ctx, _, _ := makeCtx(whereBD(sf, "at-nowhere", "at-msg2", where), markHosted(t, sf.home))
			err := cmd.Run(ctx)
			if err == nil || !strings.Contains(err.Error(), "at-nowhere") || !strings.Contains(err.Error(), "where: host:<name>") {
				t.Fatalf("err = %v, want one naming the recipient and the where: host:<name> line", err)
			}
			if sf.createArgs != nil {
				t.Errorf("a bead was created: %v", sf.createArgs)
			}
		})
	}
}

func TestSendKong_NotHosted_CreateHasNoToHostLabel(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOST", "laptop")
	sf := newSendFixture(t)
	cmd := remoteSendCmd(t, sf, "at-local")
	cmd.agentsFunc = func() ([]agentSession, error) {
		return []agentSession{{ID: "abc12345", CWD: sf.recipientWt, Status: "busy"}}, nil
	}
	ctx, _, _ := makeCtx(whereBD(sf, "at-local", "at-msg3", "host:sandbox-1"), sf.home)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasArgPrefix(sf.createArgs, "--labels=to-host:") {
		t.Errorf("non-hosted create carries a to-host label: %v", sf.createArgs)
	}
	want := []string{"create", "--type=message", "--assignee=at-local", "--notes=from: test-sender", "--labels=delivery:pending"}
	for i, w := range want {
		if sf.createArgs[i] != w {
			t.Fatalf("createArgs[%d] = %q, want %q (full: %v)", i, sf.createArgs[i], w, sf.createArgs)
		}
	}
}

func TestSendKong_Hosted_StewardIsNotStamped(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOST", "laptop")
	sf := newSendFixture(t)
	cmd := remoteSendCmd(t, sf, StewardHandle)
	cmd.agentsFunc = func() ([]agentSession, error) { return nil, nil }
	ctx, _, _ := makeCtx(sf.fakeBD(StewardHandle, "at-msg4"), markHosted(t, sf.home))
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasArgPrefix(sf.createArgs, "--labels=to-host:") {
		t.Errorf("steward mail carries a to-host label: %v", sf.createArgs)
	}
}

func followRowLabels(seq, assignee, id string, labels ...string) string {
	quoted := make([]string, len(labels))
	for i, l := range labels {
		quoted[i] = `"` + l + `"`
	}
	return `{"seq":` + seq + `,"ts":"2026-10-23T13:00:00Z","op":"create","issue_id":"` + id +
		`","issue":{"issue_type":"message","assignee":"` + assignee + `","labels":[` + strings.Join(quoted, ",") + `]}}`
}

func TestMailFollowRingsFromToHostLabel(t *testing.T) {
	cases := []struct {
		name     string
		assignee string
		labels   []string
		wantRing bool
		wantLog  string
	}{
		{"matching stamp rings", "at-a", []string{"delivery:pending", "to-host:laptop-a"}, true, ""},
		{"other host does not ring", "at-a", []string{"to-host:sandbox-1"}, false, ""},
		{"no stamp logs and skips", "at-a", []string{"delivery:pending"}, false, "message at-m1 to at-a has no to-host label; not ringing"},
		{"two stamps logs and skips", "at-a", []string{"to-host:laptop-a", "to-host:sandbox-1"}, false, "has more than one to-host label"},
		{"steward rings without a stamp", "steward", []string{"delivery:pending"}, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newFollowHarness(t, true)
			h.writeSeq("10")
			h.steps = []followStep{{lines: []string{followRowLabels("11", tc.assignee, "at-m1", tc.labels...)}}}
			if err := h.run(&mailFollowKong{}); err != nil {
				t.Fatal(err)
			}
			if got := h.rungFor(tc.assignee); got != tc.wantRing {
				wakes, _ := filepath.Glob(filepath.Join(h.home, "mailbox", "*.wake"))
				t.Fatalf("rung=%v want %v (wakes %v)", got, tc.wantRing, wakes)
			}
			if tc.wantLog != "" && !strings.Contains(h.stderr.String(), tc.wantLog) {
				t.Errorf("stderr %q missing %q", h.stderr.String(), tc.wantLog)
			}
			if tc.wantLog == "" && strings.Contains(h.stderr.String(), "not ringing") {
				t.Errorf("unexpected skip log: %q", h.stderr.String())
			}
			if h.seq() != "11" {
				t.Errorf("host.seq=%q want 11 (advances past every row)", h.seq())
			}
			if len(h.calls) != 1 {
				t.Errorf("bd calls = %d, want only the events tail (no per-row lookup)", len(h.calls))
			}
		})
	}
}
