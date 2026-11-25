package verbs

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

func issueResp(t *testing.T, id, desc string, labels ...string) fakeResp {
	t.Helper()
	raw, err := json.Marshal([]bd.Issue{{ID: id, Description: desc, Labels: labels}})
	if err != nil {
		t.Fatal(err)
	}
	return fakeResp{stdout: string(raw)}
}

func clearMailClass(id string) {
	gateMailClassMu.Lock()
	delete(gateMailClass, id)
	gateMailClassMu.Unlock()
}

func gcRun(t *testing.T) string {
	sup := t.TempDir()
	t.Setenv("DSP_BEAD", "dsp-1")
	t.Setenv("DSP_SUP", sup)
	return sup
}

func TestGateClass_Resolve(t *testing.T) {
	cases := []struct {
		explicit, kind string
		plan           bool
		want           string
	}{
		{"", "question", true, "plan"},
		{"", "question", false, "scope"},
		{"design-fork", "question", false, "design-fork"},
		{"", "live-test-review", false, "live-test"},
		{"", "review", false, "delivery"},
		{"", "message", false, ""},
	}
	for _, c := range cases {
		if got := resolveGateClass(c.explicit, c.kind, c.plan); got != c.want {
			t.Errorf("resolve(%q,%q,%v) = %q, want %q", c.explicit, c.kind, c.plan, got, c.want)
		}
	}
}

func TestGateClass_OutsiderPlanGateMailAndLabel(t *testing.T) {
	defer clearMailClass("at-o1")
	sup := gcRun(t)
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, issueResp(t, "at-o1", "origin: outsider\n"), {stdout: "ok"}})
	f := makeTempFile(t, "https://plan.example/x\nApprove the plan?")
	if err := (&gateKong{ID: "at-o1", File: f, Kind: "question", Plan: true}).Run(ctx); err != nil {
		t.Fatalf("gate: %v", err)
	}
	m := decodeMail(t, outboxLines(t, sup)[0])
	if !strings.HasPrefix(m["body"], "initiative: at-o1\nkind: question\nclass: plan\n---\n") {
		t.Errorf("body = %q", m["body"])
	}
	if got := (*calls)[2].args; !reflect.DeepEqual(got, []string{"update", "at-o1", "--add-label", "gate-class:plan"}) {
		t.Errorf("label call = %v", got)
	}
}

func TestGateClass_ActiveAutonomyGetsClass(t *testing.T) {
	defer clearMailClass("at-a1")
	sup := gcRun(t)
	until := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	desc := "autonomy: gates=scope; until=" + until + "; by=eric; via=steward; ref=tg:1\n"
	ctx, _ := newCtx(t, []fakeResp{{stdout: "ok"}, issueResp(t, "at-a1", desc), {stdout: "ok"}})
	f := makeTempFile(t, "q")
	if err := (&gateKong{ID: "at-a1", File: f, Kind: "question"}).Run(ctx); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if b := decodeMail(t, outboxLines(t, sup)[0])["body"]; !strings.Contains(b, "kind: question\nclass: scope\n---\n") {
		t.Errorf("body = %q", b)
	}
}

// Non-outsider, no autonomy: mail body and labels equal the pre-rm8 form. The
// only extra bd call is the read-only initiative show after the gate update,
// inside a Dispatcher run.
func TestGateClass_NonOutsiderByteIdentical(t *testing.T) {
	defer clearMailClass("at-n1")
	sup := gcRun(t)
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, issueResp(t, "at-n1", "repo: x\n")})
	f := makeTempFile(t, "q")
	if err := (&gateKong{ID: "at-n1", File: f, Kind: "question"}).Run(ctx); err != nil {
		t.Fatalf("gate: %v", err)
	}
	m := decodeMail(t, outboxLines(t, sup)[0])
	if m["body"] != "initiative: at-n1\nkind: question\n---\nq" {
		t.Errorf("body = %q", m["body"])
	}
	if n := len(*calls); n != 2 {
		t.Fatalf("bd calls = %d, want 2 (gate update + show): %v", n, *calls)
	}
	for _, c := range *calls {
		if strings.Contains(strings.Join(c.args, " "), "gate-class") {
			t.Errorf("gate-class label on non-outsider gate: %v", c.args)
		}
	}
	if got := newGateMailOp("at-zz", "question", "q").Body; got != "initiative: at-zz\nkind: question\n---\nq" {
		t.Errorf("newGateMailOp body = %q", got)
	}
}

// Outside a Dispatcher run, with no --class, a gate makes the same bd calls as
// before this track: just the gate update.
func TestGateClass_LocalGateNoExtraCall(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	f := makeTempFile(t, "q")
	if err := (&gateKong{ID: "at-l1", File: f, Kind: "question"}).Run(ctx); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if len(*calls) != 1 {
		t.Errorf("calls = %v", *calls)
	}
}

func TestGateClass_ExplicitClassLabelOutsideRun(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}})
	f := makeTempFile(t, "q")
	if err := (&gateKong{ID: "at-l2", File: f, Kind: "question", Class: "design-fork"}).Run(ctx); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if got := (*calls)[1].args; !reflect.DeepEqual(got, []string{"update", "at-l2", "--add-label", "gate-class:design-fork"}) {
		t.Errorf("first call = %v", got)
	}
}

func TestGateClass_ClearRemovesLabelWithoutExtraCall(t *testing.T) {
	// With a gate-class label: same call count (show + update), update gains the removal.
	ctx, calls := newCtx(t, []fakeResp{issueResp(t, "at-c1", "", "human", "gate:question", "gate-class:plan"), {stdout: "ok"}})
	if err := (&clearGateKong{ID: "at-c1"}).Run(ctx); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("calls = %v", *calls)
	}
	u := strings.Join((*calls)[1].args, " ")
	if !strings.HasSuffix(u, "--remove-label gate-class:plan") {
		t.Errorf("update = %s", u)
	}
	// Without one: the update is exactly the pre-rm8 args.
	ctx2, calls2 := newCtx(t, []fakeResp{issueResp(t, "at-c2", "", "human", "gate:question"), {stdout: "ok"}})
	if err := (&clearGateKong{ID: "at-c2"}).Run(ctx2); err != nil {
		t.Fatalf("clear: %v", err)
	}
	want := []string{"update", "at-c2", "--remove-label", "human", "--remove-label", "gate:review", "--remove-label", "gate:question", "--remove-label", "gate:live-test-review", "--remove-label", externalReviewLabel}
	if len(*calls2) != 2 || !reflect.DeepEqual((*calls2)[1].args, want) {
		t.Errorf("calls = %v", *calls2)
	}
}

// gate-class: must not match any reader of the "gate:" label family.
func TestGateClass_LabelInvisibleToGateReaders(t *testing.T) {
	l := []string{"human", "gate-class:plan"}
	if hasGateKind(l, "gate:question") || hasGateKind(l, "gate:review") || hasGateKind(l, "gate:live-test-review") || anyGateLabelRemains(l) || len(perPRGateLabels(l)) != 0 || hasHumanAnyGateLabel(l) {
		t.Error("gate-class label matched a gate reader")
	}
}
