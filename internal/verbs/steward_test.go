package verbs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
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

// ── steward remove ────────────────────────────────────────────────────────────

// stewardMessagesFakeBD returns a fixed set of message issues from RunJSON
// (countUnreadStewardMessages' "bd list --assignee=steward ..." query) and
// records whether RunJSON was called at all.
type stewardMessagesFakeBD struct {
	fakeBD
	messages []bd.Issue
	queryErr error
	queried  bool
}

func (f *stewardMessagesFakeBD) RunJSON(dst any, args ...string) error {
	f.queried = true
	if f.queryErr != nil {
		return f.queryErr
	}
	if out, ok := dst.(*[]bd.Issue); ok {
		*out = f.messages
	}
	return nil
}

func TestStewardRemove_NoExistingSession_IdempotentSuccess(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	if err := (&stewardRemoveKong{}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "nothing to remove") {
		t.Errorf("expected 'nothing to remove' note, got: %q", out)
	}
	if !strings.Contains(out, "kept: nothing") {
		t.Errorf("expected 'kept: nothing' note, got: %q", out)
	}
	if !strings.Contains(out, "unread steward messages: 0") {
		t.Errorf("expected unread count of 0, got: %q", out)
	}
}

func TestStewardRemove_RemovesSessionDirAndDoorbell(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	if err := (&stewardInitKong{}).Run(ctx); err != nil {
		t.Fatalf("seed steward init: %v", err)
	}
	doorbell := StewardDoorbellPath(ctx)
	if err := os.MkdirAll(filepath.Dir(doorbell), 0o755); err != nil {
		t.Fatalf("seed doorbell dir: %v", err)
	}
	if err := os.WriteFile(doorbell, []byte("wake"), 0o644); err != nil {
		t.Fatalf("seed doorbell file: %v", err)
	}

	if err := (&stewardRemoveKong{}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sessionDir := StewardSessionDir(ctx)
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Errorf("expected session dir %s removed, stat err: %v", sessionDir, err)
	}
	if _, err := os.Stat(doorbell); !os.IsNotExist(err) {
		t.Errorf("expected doorbell %s removed, stat err: %v", doorbell, err)
	}
	out := stdout.String()
	if !strings.Contains(out, "removed: "+sessionDir) {
		t.Errorf("expected removed session dir noted, got: %q", out)
	}
	if !strings.Contains(out, "removed: "+doorbell) {
		t.Errorf("expected removed doorbell noted, got: %q", out)
	}
}

func TestStewardRemove_KeepsLedgerAndBriefingByDefault(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	if err := os.MkdirAll(StewardHome(ctx), 0o755); err != nil {
		t.Fatalf("seed steward home: %v", err)
	}
	ledgerPath := StewardLedgerPath(ctx)
	briefingPath := StewardBriefingThreadPath(ctx)
	if err := os.WriteFile(ledgerPath, []byte(`{}`+"\n"), 0o644); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
	if err := os.WriteFile(briefingPath, []byte("thread"), 0o644); err != nil {
		t.Fatalf("seed briefing-thread: %v", err)
	}

	if err := (&stewardRemoveKong{}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(ledgerPath); err != nil {
		t.Errorf("expected ledger kept at %s: %v", ledgerPath, err)
	}
	if _, err := os.Stat(briefingPath); err != nil {
		t.Errorf("expected briefing-thread kept at %s: %v", briefingPath, err)
	}
	out := stdout.String()
	if !strings.Contains(out, "kept") || !strings.Contains(out, ledgerPath) || !strings.Contains(out, briefingPath) {
		t.Errorf("expected both kept paths reported, got: %q", out)
	}
}

func TestStewardRemove_Purge_DeletesLedgerBriefingAndHome(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	if err := os.MkdirAll(StewardHome(ctx), 0o755); err != nil {
		t.Fatalf("seed steward home: %v", err)
	}
	if err := os.WriteFile(StewardLedgerPath(ctx), []byte(`{}`+"\n"), 0o644); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
	if err := os.WriteFile(StewardBriefingThreadPath(ctx), []byte("thread"), 0o644); err != nil {
		t.Fatalf("seed briefing-thread: %v", err)
	}

	if err := (&stewardRemoveKong{Purge: true}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(StewardHome(ctx)); !os.IsNotExist(err) {
		t.Errorf("expected steward home fully purged, stat err: %v", err)
	}
	if !strings.Contains(stdout.String(), "purged: "+StewardHome(ctx)) {
		t.Errorf("expected purge note, got: %q", stdout.String())
	}
}

func TestStewardRemove_Purge_NothingToPurgeWhenHomeMissing(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	if err := (&stewardRemoveKong{Purge: true}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "purge: nothing to purge") {
		t.Errorf("expected 'purge: nothing to purge', got: %q", stdout.String())
	}
}

func TestStewardRemove_ReportsUnreadStewardMessageCount(t *testing.T) {
	home := t.TempDir()
	fbd := &stewardMessagesFakeBD{messages: []bd.Issue{
		{ID: "at-msg1", IssueType: "message"},
		{ID: "at-msg2", IssueType: "message"},
		{ID: "at-not-a-message", IssueType: "task"}, // excluded by filterMessageType
	}}
	ctx, stdout, _ := makeCtx(fbd, home)

	if err := (&stewardRemoveKong{}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fbd.queried {
		t.Fatal("expected unread-message count query to run")
	}
	if !strings.Contains(stdout.String(), "unread steward messages: 2") {
		t.Errorf("expected unread count of 2, got: %q", stdout.String())
	}
}

func TestStewardRemove_UnreadCountQueryError_FailsSoft(t *testing.T) {
	home := t.TempDir()
	fbd := &stewardMessagesFakeBD{queryErr: fmt.Errorf("bd list: boom")}
	ctx, stdout, stderr := makeCtx(fbd, home)

	if err := (&stewardRemoveKong{}).Run(ctx); err != nil {
		t.Fatalf("expected remove to fail soft on count-query error, got: %v", err)
	}
	if strings.Contains(stdout.String(), "unread steward messages:") {
		t.Errorf("expected no unread count line on query error, got: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "warning") {
		t.Errorf("expected warning on stderr, got: %q", stderr.String())
	}
}

func TestStewardRemove_LiveSessionWarning(t *testing.T) {
	home := t.TempDir()
	ctx, _, stderr := makeCtx(&fakeBD{}, home)

	if err := (&stewardInitKong{}).Run(ctx); err != nil {
		t.Fatalf("seed steward init: %v", err)
	}
	sessionDir := StewardSessionDir(ctx)

	cmd := &stewardRemoveKong{
		agentsFunc: func() ([]agentSession, error) {
			return []agentSession{{CWD: sessionDir}}, nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr.String(), "warning") || !strings.Contains(stderr.String(), "live session") {
		t.Errorf("expected live-session warning, got: %q", stderr.String())
	}
	// Best-effort: warning does not block removal.
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Errorf("expected session dir still removed despite live-session warning, stat err: %v", err)
	}
}

func TestStewardRemove_LiveSessionCheckFailsSoft(t *testing.T) {
	home := t.TempDir()
	ctx, _, stderr := makeCtx(&fakeBD{}, home)

	cmd := &stewardRemoveKong{
		agentsFunc: func() ([]agentSession, error) {
			return nil, fmt.Errorf("claude agents: boom")
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(stderr.String(), "warning") {
		t.Errorf("expected no warning when live-session check itself errors (fail-soft), got: %q", stderr.String())
	}
}

func TestStewardRemove_NilContext(t *testing.T) {
	if err := (&stewardRemoveKong{}).Run(nil); err == nil {
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
