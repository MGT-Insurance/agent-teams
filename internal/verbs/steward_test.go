package verbs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── steward init ──────────────────────────────────────────────────────────────

func TestStewardInit_CreatesSessionDirAndMarker(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	if err := (&stewardInitKong{}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sessionDir := StewardSessionDir(ctx)
	if fi, err := os.Stat(sessionDir); err != nil || !fi.IsDir() {
		t.Fatalf("expected session dir %s to exist: %v", sessionDir, err)
	}
	marker := StewardSessionMarkerPath(ctx)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected marker %s to exist: %v", marker, err)
	}
	if got := strings.TrimSpace(stdout.String()); got != sessionDir {
		t.Errorf("stdout = %q, want %q", got, sessionDir)
	}
}

func TestStewardInit_Idempotent(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	if err := (&stewardInitKong{}).Run(ctx); err != nil {
		t.Fatalf("first init: %v", err)
	}
	marker := StewardSessionMarkerPath(ctx)
	first, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}

	if err := (&stewardInitKong{}).Run(ctx); err != nil {
		t.Fatalf("second init: %v", err)
	}
	second, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker after second init: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("marker content changed across idempotent init calls: %q -> %q", first, second)
	}
}

func TestStewardInit_NilContext(t *testing.T) {
	if err := (&stewardInitKong{}).Run(nil); err == nil {
		t.Fatal("expected error for nil context")
	}
}

// ── steward ledger record ────────────────────────────────────────────────────

func TestStewardLedgerRecord_AppendsLine(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	cmd := &stewardLedgerRecordKong{
		Category:       "merge-approval",
		Initiative:     "agent-teams-e3mq",
		Recommendation: "merge PR #100",
		Verdict:        "accepted",
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(StewardLedgerPath(ctx))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 ledger line, got %d: %q", len(lines), data)
	}
	rec, err := ParseStewardLedgerRecord([]byte(lines[0]))
	if err != nil {
		t.Fatalf("ParseStewardLedgerRecord: %v", err)
	}
	if rec.Category != StewardLedgerCategoryMergeApproval || rec.Initiative != "agent-teams-e3mq" ||
		rec.Recommendation != "merge PR #100" || rec.Verdict != StewardLedgerVerdictAccepted {
		t.Errorf("unexpected record: %+v", rec)
	}
}

func TestStewardLedgerRecord_CreatesParentDirs(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	cmd := &stewardLedgerRecordKong{
		Category:       "scope-call",
		Initiative:     "at-x",
		Recommendation: "narrow scope",
		Verdict:        "corrected",
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(StewardLedgerPath(ctx))); err != nil {
		t.Fatalf("expected ledger parent dir to exist: %v", err)
	}
}

func TestStewardLedgerRecord_InvalidCategoryRejected(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	cmd := &stewardLedgerRecordKong{
		Category:       "not-a-category",
		Initiative:     "at-x",
		Recommendation: "x",
		Verdict:        "accepted",
	}
	if err := cmd.Run(ctx); err == nil {
		t.Fatal("expected error for invalid category, got nil")
	}
	if _, err := os.Stat(StewardLedgerPath(ctx)); !os.IsNotExist(err) {
		t.Errorf("expected no ledger file written on validation failure")
	}
}

func TestStewardLedgerRecord_InvalidVerdictRejected(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	cmd := &stewardLedgerRecordKong{
		Category:       "scope-call",
		Initiative:     "at-x",
		Recommendation: "x",
		Verdict:        "maybe",
	}
	if err := cmd.Run(ctx); err == nil {
		t.Fatal("expected error for invalid verdict, got nil")
	}
}

func TestStewardLedgerRecord_NilContext(t *testing.T) {
	cmd := &stewardLedgerRecordKong{Category: "scope-call", Initiative: "x", Recommendation: "y", Verdict: "accepted"}
	if err := cmd.Run(nil); err == nil {
		t.Fatal("expected error for nil context")
	}
}

// ── steward ledger stats ─────────────────────────────────────────────────────

func TestStewardLedgerStats_RoundTrip(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	records := []*stewardLedgerRecordKong{
		{Category: "merge-approval", Initiative: "at-a", Recommendation: "r1", Verdict: "accepted"},
		{Category: "merge-approval", Initiative: "at-a", Recommendation: "r2", Verdict: "corrected"},
		{Category: "scope-call", Initiative: "at-b", Recommendation: "r3", Verdict: "accepted"},
	}
	for _, r := range records {
		if err := r.Run(ctx); err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}
	stdout.Reset()

	if err := (&stewardLedgerStatsKong{JSON: true}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()

	if !strings.Contains(out, `"category":"merge-approval"`) || !strings.Contains(out, `"total":2`) {
		t.Errorf("expected merge-approval total 2 in JSON output: %s", out)
	}
	if !strings.Contains(out, `"category":"scope-call"`) {
		t.Errorf("expected scope-call category in JSON output: %s", out)
	}
}

func TestStewardLedgerStats_CategoryFilter(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	records := []*stewardLedgerRecordKong{
		{Category: "merge-approval", Initiative: "at-a", Recommendation: "r1", Verdict: "accepted"},
		{Category: "scope-call", Initiative: "at-b", Recommendation: "r2", Verdict: "accepted"},
	}
	for _, r := range records {
		if err := r.Run(ctx); err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}
	stdout.Reset()

	if err := (&stewardLedgerStatsKong{Category: "scope-call", JSON: true}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if strings.Contains(out, "merge-approval") {
		t.Errorf("expected merge-approval excluded by filter: %s", out)
	}
	if !strings.Contains(out, "scope-call") {
		t.Errorf("expected scope-call present: %s", out)
	}
}

func TestStewardLedgerStats_NoLedgerFile(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	if err := (&stewardLedgerStatsKong{}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "no ledger entries") {
		t.Errorf("expected 'no ledger entries'; got: %q", stdout.String())
	}
}

func TestStewardLedgerStats_InvalidCategoryRejected(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	if err := (&stewardLedgerStatsKong{Category: "bogus"}).Run(ctx); err == nil {
		t.Fatal("expected error for invalid category filter, got nil")
	}
}

func TestStewardLedgerStats_NilContext(t *testing.T) {
	if err := (&stewardLedgerStatsKong{}).Run(nil); err == nil {
		t.Fatal("expected error for nil context")
	}
}
