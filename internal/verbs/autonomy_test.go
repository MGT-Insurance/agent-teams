package verbs

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

var autonomyTestNow = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

func withAutonomyClock(t *testing.T) {
	t.Helper()
	old := autonomyNow
	autonomyNow = func() time.Time { return autonomyTestNow }
	t.Cleanup(func() { autonomyNow = old })
}

func autonomyShow(t *testing.T, desc string, labels ...string) fakeResp {
	t.Helper()
	b, err := json.Marshal([]bd.Issue{{ID: "at-1", Description: desc, Labels: labels}})
	if err != nil {
		t.Fatal(err)
	}
	return fakeResp{stdout: string(b)}
}

const activeLine = "autonomy: gates=plan,scope; until=2026-11-25T20:00:00Z; by=eric; via=steward; ref=telegram:42"

func grantCmd() *autonomyGrantKong {
	return &autonomyGrantKong{ID: "at-1", Gates: "plan,scope", Until: "8h", Ref: "telegram:42"}
}

func TestAutonomyGrant_Steward(t *testing.T) {
	withAutonomyClock(t)
	t.Setenv("DSP_BEAD", "")
	t.Setenv("BEADS_ACTOR", "agent:steward")
	ctx, calls := newCtx(t, []fakeResp{autonomyShow(t, "repo: x"), {}, {}})
	if err := grantCmd().Run(ctx); err != nil {
		t.Fatal(err)
	}
	last := (*calls)[len(*calls)-1].args
	want := "autonomy granted gates=plan,scope; until=2026-11-25T20:00:00Z; by=eric; via=steward; ref=telegram:42"
	if appendNotesOf(last) != want {
		t.Errorf("note = %q, want %q", appendNotesOf(last), want)
	}
	if !strings.Contains(stdoutOf(ctx), "autonomy granted on at-1") {
		t.Errorf("stdout = %q", stdoutOf(ctx))
	}
}

func TestAutonomyGrant_Refusals(t *testing.T) {
	withAutonomyClock(t)
	cases := []struct{ name, dsp, actor string }{
		{"dispatcher run", "bead-1", "agent:steward"},
		{"other agent", "", "agent:canary"},
		{"peer", "", "peer:joe/canary"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DSP_BEAD", tc.dsp)
			t.Setenv("BEADS_ACTOR", tc.actor)
			ctx, calls := newCtx(t, nil)
			if err := grantCmd().Run(ctx); err == nil || !strings.Contains(err.Error(), "refused") {
				t.Fatalf("grant err = %v, want refused", err)
			}
			if err := (&autonomyRevokeKong{ID: "at-1", Ref: "r"}).Run(ctx); err == nil || !strings.Contains(err.Error(), "refused") {
				t.Fatalf("revoke err = %v, want refused", err)
			}
			if len(*calls) != 0 {
				t.Errorf("refused call reached bd: %v", *calls)
			}
		})
	}
}

func TestAutonomyGrant_72hCap(t *testing.T) {
	withAutonomyClock(t)
	t.Setenv("DSP_BEAD", "")
	t.Setenv("BEADS_ACTOR", "agent:steward")
	ctx, _ := newCtx(t, nil)
	g := grantCmd()
	g.Until = "73h"
	if err := g.Run(ctx); err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Errorf("73h err = %v", err)
	}
}

func TestAutonomyRevoke_Writes(t *testing.T) {
	withAutonomyClock(t)
	t.Setenv("DSP_BEAD", "")
	t.Setenv("BEADS_ACTOR", "")
	ctx, calls := newCtx(t, []fakeResp{autonomyShow(t, "repo: x\n"+activeLine), {}, {}})
	if err := (&autonomyRevokeKong{ID: "at-1", Ref: "telegram:43"}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := appendNotesOf((*calls)[len(*calls)-1].args); got != "autonomy revoked revoked 2026-11-25T12:00:00Z; ref=telegram:43" {
		t.Errorf("note = %q", got)
	}
}

func TestAutonomyClearGate(t *testing.T) {
	withAutonomyClock(t)
	expired := strings.Replace(activeLine, "20:00:00Z", "11:00:00Z", 1)
	revoked := "autonomy: revoked 2026-11-25T10:00:00Z; ref=telegram:42"
	cases := []struct {
		name, desc, ref, label string
		ok                     bool
	}{
		{"covered", activeLine, "telegram:42", "gate-class:plan", true},
		{"uncovered class", activeLine, "telegram:42", "gate-class:delivery", false},
		{"expired", expired, "telegram:42", "gate-class:plan", false},
		{"revoked", revoked, "telegram:42", "gate-class:plan", false},
		{"wrong ref", activeLine, "telegram:99", "gate-class:plan", false},
		{"outsider covered", "origin: outsider\nsteward-approves: plan", "outsider", "gate-class:plan", true},
		{"outsider uncovered", "origin: outsider\nsteward-approves: plan", "outsider", "gate-class:delivery", false},
		{"outsider ref on non-outsider", activeLine, "outsider", "gate-class:plan", false},
		{"no class label", activeLine, "telegram:42", "human", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, calls := newCtx(t, []fakeResp{autonomyShow(t, tc.desc, tc.label), {}})
			err := clearGateAutonomyCheck(ctx, &clearGateKong{ID: "at-1", Autonomy: tc.ref})
			if tc.ok != (err == nil) {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
			if tc.ok {
				want := "cleared under autonomy ref=" + tc.ref + " class=plan"
				if got := appendNotesOf((*calls)[len(*calls)-1].args); got != want {
					t.Errorf("note = %q, want %q", got, want)
				}
			} else if len(*calls) != 1 {
				t.Errorf("refused check wrote to bd: %v", *calls)
			}
		})
	}
}

func TestAutonomyShow(t *testing.T) {
	withAutonomyClock(t)
	ctx, _ := newCtx(t, []fakeResp{autonomyShow(t, activeLine)})
	if err := (&autonomyShowKong{ID: "at-1"}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdoutOf(ctx), "active: yes (gates=plan,scope ref=telegram:42)") {
		t.Errorf("stdout = %q", stdoutOf(ctx))
	}
}
