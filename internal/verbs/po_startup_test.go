package verbs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func poCharterFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func poSampleCharter(t *testing.T, area string) string {
	t.Helper()
	data, err := os.ReadFile("../po/testdata/sample-charter.md")
	if err != nil {
		t.Fatal(err)
	}
	return poCharterFile(t, strings.Replace(string(data), "area: quoting", "area: "+area, 1))
}

func poStartupFixture(t *testing.T) (*memBD, string) {
	t.Helper()
	m := newMemBD()
	m.mem["po:quoting:view"] = "revised: 2026-11-20T00:00:00Z\nQuoting is healthy."
	m.mem["po:quoting:claim:at-1"] = `{"fit":"clear","reason":"rating","at":"2026-11-24T00:00:00Z","lead":true}`
	m.mem["po:pricing:claim:at-9"] = `{"fit":"clear"}`
	m.mem["po:fresh:shared-rule"] = "RULE: shared po lesson."
	m.mem["po-quoting:fresh:area-rule"] = "RULE: quoting lesson."
	return m, poSampleCharter(t, "quoting")
}

func TestPoStartup_Golden(t *testing.T) {
	old := poStartupNow
	poStartupNow = func() time.Time { return time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC) }
	defer func() { poStartupNow = old }()
	m, charter := poStartupFixture(t)
	ctx, out, _ := hostedLedgerCtx(t, m)
	recordCtx, _, _ := hostedLedgerCtx(t, m)
	cmd := &stewardLedgerRecordKong{Category: "claim", Persona: "po-quoting", Initiative: "at-1", Recommendation: "r", Verdict: "accepted"}
	if err := cmd.Run(recordCtx); err != nil {
		t.Fatal(err)
	}
	if err := poStartupRun(ctx, &poStartupKong{Area: "quoting", Charter: charter}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"## Charter\nCharter " + charter + " (valid): area=quoting",
		"- rating (Rating engine) words=[rating, rate, premium]",
		"- g-latency: Quotes return quickly; done when: p95 quote latency under 5s for a week; signals: quote-p95",
		"## View\nview last revised 3 days ago\n",
		"Quoting is healthy.",
		"## Claims\n- at-1: {\"fit\":\"clear\",\"reason\":\"rating\",\"at\":\"2026-11-24T00:00:00Z\",\"lead\":true}\n",
		"## Ledger stats (po-quoting)\n",
		"claim",
		"RULE: shared po lesson.",
		"RULE: quoting lesson.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "at-9") || strings.Contains(got, "DRAFTING") {
		t.Errorf("leaked other area's claim or banner:\n%s", got)
	}
	order := []string{"## Charter", "## View", "## Claims", "## Ledger rules", "## Ledger stats", "## PO learnings (shared", "## PO learnings (po-quoting"}
	last := -1
	for _, h := range order {
		i := strings.Index(got, h)
		if i <= last {
			t.Errorf("section %q out of order (%d <= %d)", h, i, last)
		}
		last = i
	}
	if len(got) > poStartupLimit {
		t.Errorf("over cap: %d", len(got))
	}
}

func TestPoStartup_DraftingBanner(t *testing.T) {
	m, _ := poStartupFixture(t)
	bad := poCharterFile(t, "---\narea: quoting\nmission: x\n---\n")
	for name, path := range map[string]string{
		"invalid":  bad,
		"missing":  filepath.Join(t.TempDir(), "nope.md"),
		"mismatch": poSampleCharter(t, "other"),
	} {
		ctx, out, _ := hostedLedgerCtx(t, m)
		if err := poStartupRun(ctx, &poStartupKong{Area: "quoting", Charter: path}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(out.String(), "## Charter\nDRAFTING MODE:") || !strings.Contains(out.String(), "## Claims\n- at-1") {
			t.Errorf("%s: no banner or rest not loaded:\n%s", name, out.String())
		}
	}
}

func TestPoStartup_UnreachablePrintsNothing(t *testing.T) {
	m, charter := poStartupFixture(t)
	ctx, out, _ := hostedLedgerCtx(t, m)
	m.err = errors.New("Dolt server unreachable")
	if err := poStartupRun(ctx, &poStartupKong{Area: "quoting", Charter: charter}); err == nil || out.Len() != 0 {
		t.Fatalf("err %v, out %q", err, out.String())
	}
}

func TestPoStartup_OverLimitPrintsNothing(t *testing.T) {
	m, charter := poStartupFixture(t)
	m.mem["po-quoting:fresh:big"] = strings.Repeat("x", poStartupLimit)
	ctx, out, _ := hostedLedgerCtx(t, m)
	err := poStartupRun(ctx, &poStartupKong{Area: "quoting", Charter: charter})
	if err == nil || !strings.Contains(err.Error(), "over the") || out.Len() != 0 {
		t.Fatalf("err %v, out len %d", err, out.Len())
	}
}

func TestPoStartup_BadArea(t *testing.T) {
	ctx, _, _ := hostedLedgerCtx(t, newMemBD())
	if err := poStartupRun(ctx, &poStartupKong{Area: "Bad Area"}); err == nil {
		t.Fatal("want error")
	}
}

// PO state keys must never read as role learnings.
func TestPoStateKeysAreNotLearnings(t *testing.T) {
	m := newMemBD()
	m.mem["po:quoting:view"] = "STATE-VIEW"
	m.mem["po:hot:view"] = "LEARNING-HOT-VIEW" // tier word, not an area: a real learning
	m.mem["po:quoting:claim:at-1"] = "STATE-CLAIM"
	for _, withLearning := range []bool{false, true} {
		if withLearning {
			m.mem["po:fresh:real"] = "RULE: real"
		}
		ctx, out, _ := hostedLedgerCtx(t, m)
		if err := runLearnings(ctx, "po"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "LEARNING-HOT-VIEW") {
			t.Errorf("learning po:hot:view hidden by the guard: %s", out.String())
		}
		if strings.Contains(out.String(), "STATE-") {
			t.Errorf("learnings served state (learning=%v): %s", withLearning, out.String())
		}
		ctx, out, _ = hostedLedgerCtx(t, m)
		if err := runRecall(ctx, "po", "VIEW STATE claim"); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "STATE-") || strings.Contains(out.String(), "po:quoting") || !strings.Contains(out.String(), "LEARNING-HOT-VIEW") {
			t.Errorf("recall matched state: %s", out.String())
		}
	}
	if !isPOStateKey("po:quoting:reading:f:s:2026-01-01T00:00:00Z") || isPOStateKey("po:fresh:x") || isPOStateKey("steward:hot:view") {
		t.Error("isPOStateKey misclassifies")
	}
}

func TestPoStateOnlyRoleNotListed(t *testing.T) {
	m := newMemBD()
	m.mem["po:quoting:view"] = "v"
	m.mem["dri:fresh:x"] = "y"
	ctx, out, _ := hostedLedgerCtx(t, m)
	if err := runRoles(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "po") {
		t.Errorf("roles lists po from state keys only: %q", out.String())
	}
}
