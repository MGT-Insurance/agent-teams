package verbs

import (
	"regexp"
	"strings"
	"testing"
)

func TestLedgerPersona_PORoundTripHostedAndIsolated(t *testing.T) {
	m := newMemBD()
	ctx, out, _ := hostedLedgerCtx(t, m)

	po := &stewardLedgerRecordKong{Category: "nudge", Initiative: "at-1", Recommendation: "nudge the dri", Verdict: "corrected", Decision: "wait", Persona: "po-billing"}
	if err := po.Run(ctx); err != nil {
		t.Fatalf("po record: %v", err)
	}
	recordLedger(t, ctx, "merge-approval", "at-2", "accepted", "")

	key := strings.TrimPrefix(m.remember[0][1], "--key=")
	if !regexp.MustCompile(`^poledger\.billing\.\d{19}-[0-9a-f]{4}$`).MatchString(key) {
		t.Errorf("po key %q", key)
	}
	if !strings.HasPrefix(strings.TrimPrefix(m.remember[1][1], "--key="), "stewardledger.") {
		t.Errorf("steward key %q", m.remember[1][1])
	}

	out.Reset()
	if err := (&stewardLedgerStatsKong{Persona: "po-billing"}).Run(ctx); err != nil {
		t.Fatalf("po stats: %v", err)
	}
	if s := out.String(); !strings.Contains(s, "nudge") || strings.Contains(s, "merge-approval") {
		t.Errorf("po stats: %q", s)
	}
	out.Reset()
	if err := (&stewardLedgerStatsKong{}).Run(ctx); err != nil {
		t.Fatalf("steward stats: %v", err)
	}
	if s := out.String(); !strings.Contains(s, "merge-approval") || strings.Contains(s, "nudge") {
		t.Errorf("steward stats: %q", s)
	}
	out.Reset()
	if err := (&stewardLedgerStatsKong{Persona: "po-other"}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no ledger entries") {
		t.Errorf("other po sees rows: %q", out.String())
	}
	out.Reset()
	if err := (&stewardLedgerRecallKong{Category: "nudge", Persona: "po-billing", Limit: 5}).Run(ctx); err != nil {
		t.Fatalf("po recall: %v", err)
	}
	if !strings.Contains(out.String(), "at-1") {
		t.Errorf("po recall: %q", out.String())
	}
}

func TestLedgerPersona_POFileRoundTripSeparateFromSteward(t *testing.T) {
	ctx, out, _ := makeCtx(noBDCalls{t}, t.TempDir())
	if err := (&stewardLedgerRecordKong{Category: "pr-fit", Initiative: "at-1", Recommendation: "r", Verdict: "accepted", Persona: "po-billing"}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	recordLedger(t, ctx, "scope-call", "at-2", "accepted", "")
	out.Reset()
	if err := (&stewardLedgerStatsKong{}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); !strings.Contains(s, "scope-call") || strings.Contains(s, "pr-fit") {
		t.Errorf("steward stats: %q", s)
	}
	out.Reset()
	if err := (&stewardLedgerStatsKong{Persona: "po-billing"}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); !strings.Contains(s, "pr-fit") || strings.Contains(s, "scope-call") {
		t.Errorf("po stats: %q", s)
	}
}

func TestLedgerPersona_POCategoryRefusedUnderSteward(t *testing.T) {
	ctx, _, _ := makeCtx(noBDCalls{t}, t.TempDir())
	for _, persona := range []string{"", "steward"} {
		err := (&stewardLedgerRecordKong{Category: "nudge", Initiative: "at-1", Recommendation: "r", Verdict: "accepted", Persona: persona}).Run(ctx)
		if err == nil || !strings.Contains(err.Error(), "invalid category") {
			t.Errorf("persona %q: want invalid category, got %v", persona, err)
		}
	}
	if err := (&stewardLedgerStatsKong{Category: "nudge"}).Run(ctx); err == nil {
		t.Error("stats accepted po category under steward")
	}
	if err := (&stewardLedgerRecallKong{Category: "nudge"}).Run(ctx); err == nil {
		t.Error("recall accepted po category under steward")
	}
	if StewardLedgerCategory("nudge").Valid() {
		t.Error("Valid() must stay steward-only")
	}
}

func TestLedgerPersona_InvalidPersonaRefused(t *testing.T) {
	ctx, _, _ := makeCtx(noBDCalls{t}, t.TempDir())
	for _, p := range []string{"po-", "po-Bad", "po--x", "eric", "po-a/b"} {
		err := (&stewardLedgerRecordKong{Category: "nudge", Initiative: "at-1", Recommendation: "r", Verdict: "accepted", Persona: p}).Run(ctx)
		if err == nil || !strings.Contains(err.Error(), "invalid persona") {
			t.Errorf("persona %q: %v", p, err)
		}
	}
}
