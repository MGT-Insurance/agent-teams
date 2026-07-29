package verbs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/plugins/agent-teams/templates"
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

// ── global PRIME.md install ──────────────────────────────────────────────────

func TestStewardInit_InstallsGlobalPrimeMD(t *testing.T) {
	home := t.TempDir()
	ctx, _, stderr := makeCtx(&fakeBD{}, home)

	if err := (&stewardInitKong{}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	target := globalPrimeMDPath(ctx)
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", target, err)
	}
	if string(got) != templates.GlobalPrimeMD {
		t.Errorf("installed content = %q, want the shipped template", got)
	}
	if !strings.Contains(stderr.String(), "installed: "+target) {
		t.Errorf("stderr = %q, want an %q note", stderr.String(), "installed: "+target)
	}
}

func TestStewardInit_GlobalPrimeMDAlreadyCorrect_NoOp(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	if err := (&stewardInitKong{}).Run(ctx); err != nil {
		t.Fatalf("first init: %v", err)
	}
	target := globalPrimeMDPath(ctx)
	fiBefore, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat after first init: %v", err)
	}

	// Second call: content already matches the shipped template exactly, so
	// this must be a true no-op — no rewrite (mtime unchanged), no error, and
	// no log line on either stream.
	_, stdout2, stderr2 := makeCtx(&fakeBD{}, home)
	ctx.Stdout, ctx.Stderr = stdout2, stderr2
	if err := (&stewardInitKong{}).Run(ctx); err != nil {
		t.Fatalf("second init: %v", err)
	}
	fiAfter, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat after second init: %v", err)
	}
	if !fiAfter.ModTime().Equal(fiBefore.ModTime()) {
		t.Errorf("PRIME.md was rewritten on an already-correct no-op call: mtime %v -> %v", fiBefore.ModTime(), fiAfter.ModTime())
	}
	if got := stderr2.String(); got != "" {
		t.Errorf("stderr on no-op call = %q, want empty (no spurious log line)", got)
	}
}

func TestStewardInit_GlobalPrimeMDHumanEdit_NotClobbered(t *testing.T) {
	home := t.TempDir()
	ctx, _, stderr := makeCtx(&fakeBD{}, home)

	target := globalPrimeMDPath(ctx)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	humanContent := "# My customized PRIME.md\n\nDon't touch this.\n"
	if err := os.WriteFile(target, []byte(humanContent), 0o644); err != nil {
		t.Fatalf("seed human-edited PRIME.md: %v", err)
	}

	if err := (&stewardInitKong{}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	if string(got) != humanContent {
		t.Errorf("human-edited PRIME.md was clobbered: got %q, want %q", got, humanContent)
	}
	if !strings.Contains(stderr.String(), target) || !strings.Contains(stderr.String(), "human edit") {
		t.Errorf("stderr = %q, want an obvious divergence note naming %s", stderr.String(), target)
	}
}

// ── global PRIME.md provenance sidecar (agent-teams-e81h.1 upgrade path) ─────

// TestInstallGlobalPrimeMD_OurOlderTemplate_IsUpgraded covers the case a
// machine already has v1 installed by this tool (matching sidecar) and the
// shipped template is revised to v2: the old file must be overwritten and
// the sidecar updated to v2's hash, since we can PROVE we wrote what's on
// disk and nothing has touched it since.
func TestInstallGlobalPrimeMD_OurOlderTemplate_IsUpgraded(t *testing.T) {
	home := t.TempDir()
	ctx, _, stderr := makeCtx(&fakeBD{}, home)

	const v1 = "template v1 content\n"
	const v2 = "template v2 content, revised\n"

	if err := installGlobalPrimeMD(ctx, v1); err != nil {
		t.Fatalf("install v1: %v", err)
	}

	if err := installGlobalPrimeMD(ctx, v2); err != nil {
		t.Fatalf("install v2: %v", err)
	}

	target := globalPrimeMDPath(ctx)
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	if string(got) != v2 {
		t.Errorf("PRIME.md = %q, want upgraded content %q", got, v2)
	}
	sidecar := globalPrimeSidecarPath(ctx)
	recorded, ok := readSidecarHash(sidecar)
	if !ok {
		t.Fatalf("expected sidecar %s to exist after upgrade", sidecar)
	}
	if want := sha256Hex([]byte(v2)); recorded != want {
		t.Errorf("sidecar hash = %q, want %q (sha256 of v2)", recorded, want)
	}
	if !strings.Contains(stderr.String(), "updated: "+target) {
		t.Errorf("stderr = %q, want an %q note", stderr.String(), "updated: "+target)
	}
}

// TestInstallGlobalPrimeMD_HumanEditWithStaleSidecar_NotUpgraded covers a
// sidecar that's present but doesn't match what's actually on disk (the
// file was edited after we last wrote it, or the sidecar is simply wrong) —
// provenance is NOT provable, so this must refuse exactly like the no-
// sidecar-at-all case, never upgrade.
func TestInstallGlobalPrimeMD_HumanEditWithStaleSidecar_NotUpgraded(t *testing.T) {
	home := t.TempDir()
	ctx, _, stderr := makeCtx(&fakeBD{}, home)

	target := globalPrimeMDPath(ctx)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	editedContent := "# Edited after install\n"
	if err := os.WriteFile(target, []byte(editedContent), 0o644); err != nil {
		t.Fatalf("seed edited PRIME.md: %v", err)
	}
	// Sidecar recorded for a DIFFERENT template than what's on disk now —
	// simulates an edit made after install, not a fresh human file.
	sidecar := globalPrimeSidecarPath(ctx)
	if err := writeSidecar(sidecar, sha256Hex([]byte("something else entirely"))); err != nil {
		t.Fatalf("seed stale sidecar: %v", err)
	}

	const currentTemplate = "current shipped template\n"
	if err := installGlobalPrimeMD(ctx, currentTemplate); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	if string(got) != editedContent {
		t.Errorf("edited PRIME.md was clobbered despite stale sidecar: got %q, want %q", got, editedContent)
	}
	if !strings.Contains(stderr.String(), "human edit") {
		t.Errorf("stderr = %q, want a human-edit note", stderr.String())
	}
}

// TestInstallGlobalPrimeMD_MissingSidecarButContentMatches_SelfHeals covers
// a machine whose PRIME.md matches the current template exactly but has no
// sidecar yet (e.g. it predates this mechanism, or the sidecar was lost) —
// this is a true no-op on the file (no rewrite, no log line), but the
// sidecar gets silently created so a FUTURE template revision can prove
// provenance and upgrade instead of refusing.
func TestInstallGlobalPrimeMD_MissingSidecarButContentMatches_SelfHeals(t *testing.T) {
	home := t.TempDir()
	ctx, _, stderr := makeCtx(&fakeBD{}, home)

	target := globalPrimeMDPath(ctx)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	const currentTemplate = "current shipped template\n"
	if err := os.WriteFile(target, []byte(currentTemplate), 0o644); err != nil {
		t.Fatalf("seed matching PRIME.md: %v", err)
	}
	sidecar := globalPrimeSidecarPath(ctx)
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Fatalf("expected no sidecar to exist yet, stat err = %v", err)
	}

	if err := installGlobalPrimeMD(ctx, currentTemplate); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	if string(got) != currentTemplate {
		t.Errorf("PRIME.md content changed on a no-op call: got %q, want %q", got, currentTemplate)
	}
	recorded, ok := readSidecarHash(sidecar)
	if !ok {
		t.Fatalf("expected sidecar %s to be self-healed into existence", sidecar)
	}
	if want := sha256Hex([]byte(currentTemplate)); recorded != want {
		t.Errorf("self-healed sidecar hash = %q, want %q", recorded, want)
	}
	if got := stderr.String(); got != "" {
		t.Errorf("stderr on self-heal no-op = %q, want empty (no spurious log line)", got)
	}
}

// TestInstallGlobalPrimeMD_HumanEditNoSidecar_NotUpgraded covers a machine
// that has never had this tool run against it: a divergent PRIME.md with no
// sidecar at all. Provenance is unknown, so this must refuse — conservative
// by design, since we'd rather leave a file alone than clobber one we can't
// prove we wrote.
func TestInstallGlobalPrimeMD_HumanEditNoSidecar_NotUpgraded(t *testing.T) {
	home := t.TempDir()
	ctx, _, stderr := makeCtx(&fakeBD{}, home)

	target := globalPrimeMDPath(ctx)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	preexistingContent := "# Pre-existing, never installed by us\n"
	if err := os.WriteFile(target, []byte(preexistingContent), 0o644); err != nil {
		t.Fatalf("seed pre-existing PRIME.md: %v", err)
	}

	const currentTemplate = "current shipped template\n"
	if err := installGlobalPrimeMD(ctx, currentTemplate); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	if string(got) != preexistingContent {
		t.Errorf("pre-existing PRIME.md was clobbered: got %q, want %q", got, preexistingContent)
	}
	sidecar := globalPrimeSidecarPath(ctx)
	if _, ok := readSidecarHash(sidecar); ok {
		t.Errorf("expected no sidecar to be written when refusing an unknown-provenance file")
	}
	if !strings.Contains(stderr.String(), "human edit") {
		t.Errorf("stderr = %q, want a human-edit note", stderr.String())
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

func TestStewardRemove_KeepsLedgerBriefingAndReviewsByDefault(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	if err := os.MkdirAll(StewardHome(ctx), 0o755); err != nil {
		t.Fatalf("seed steward home: %v", err)
	}
	ledgerPath := StewardLedgerPath(ctx)
	briefingPath := StewardBriefingThreadPath(ctx)
	reviewsPath := StewardReviewsThreadPath(ctx)
	if err := os.WriteFile(ledgerPath, []byte(`{}`+"\n"), 0o644); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
	if err := os.WriteFile(briefingPath, []byte("thread"), 0o644); err != nil {
		t.Fatalf("seed briefing-thread: %v", err)
	}
	if err := os.WriteFile(reviewsPath, []byte("813"), 0o644); err != nil {
		t.Fatalf("seed reviews-thread: %v", err)
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
	if _, err := os.Stat(reviewsPath); err != nil {
		t.Errorf("expected reviews-thread kept at %s: %v", reviewsPath, err)
	}
	out := stdout.String()
	if !strings.Contains(out, "kept") || !strings.Contains(out, ledgerPath) || !strings.Contains(out, briefingPath) {
		t.Errorf("expected both kept paths reported, got: %q", out)
	}
	// An operator who follows this list must carry the reviews-thread ref too;
	// omitting it makes the new machine open a second Reviews topic
	// (agent-teams-p9dm.41).
	if !strings.Contains(out, reviewsPath) {
		t.Errorf("expected reviews-thread in the relocation list, got: %q", out)
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
	if err := os.WriteFile(StewardReviewsThreadPath(ctx), []byte("813"), 0o644); err != nil {
		t.Fatalf("seed reviews-thread: %v", err)
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
		Decision:       "narrow to just the API layer",
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

// TestStewardLedgerRecord_CorrectedWithoutDecisionRejected verifies the
// agent-teams-7ew5.1 CLI-boundary rule: `record --verdict corrected` without
// `--decision` fails, and nothing is written to the ledger.
func TestStewardLedgerRecord_CorrectedWithoutDecisionRejected(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	cmd := &stewardLedgerRecordKong{
		Category:       "scope-call",
		Initiative:     "at-x",
		Recommendation: "narrow scope",
		Verdict:        "corrected",
	}
	if err := cmd.Run(ctx); err == nil {
		t.Fatal("expected error for corrected verdict without --decision, got nil")
	}
	if _, err := os.Stat(StewardLedgerPath(ctx)); !os.IsNotExist(err) {
		t.Errorf("expected no ledger file written on validation failure")
	}
}

// TestStewardLedgerRecord_CorrectedWithDecisionWrites verifies --decision is
// threaded onto the written record and round-trips through
// ParseStewardLedgerRecord.
func TestStewardLedgerRecord_CorrectedWithDecisionWrites(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	cmd := &stewardLedgerRecordKong{
		Category:       "scope-call",
		Initiative:     "at-x",
		Recommendation: "narrow scope",
		Verdict:        "corrected",
		Decision:       "keep the UI layer too, just stub the backend",
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(StewardLedgerPath(ctx))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	rec, err := ParseStewardLedgerRecord([]byte(strings.TrimSpace(string(data))))
	if err != nil {
		t.Fatalf("ParseStewardLedgerRecord: %v", err)
	}
	if rec.Decision != cmd.Decision {
		t.Errorf("Decision = %q, want %q", rec.Decision, cmd.Decision)
	}
}

// ── steward ledger stats ─────────────────────────────────────────────────────

func TestStewardLedgerStats_RoundTrip(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	records := []*stewardLedgerRecordKong{
		{Category: "merge-approval", Initiative: "at-a", Recommendation: "r1", Verdict: "accepted"},
		{Category: "merge-approval", Initiative: "at-a", Recommendation: "r2", Verdict: "corrected", Decision: "hold the merge"},
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

// ── steward ledger recall ────────────────────────────────────────────────────

func TestStewardLedgerRecall_FiltersByCategoryAndOrdersMostRecentFirst(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	records := []*stewardLedgerRecordKong{
		{Category: "scope-call", Initiative: "at-a", Recommendation: "r1", Verdict: "corrected", Decision: "decision-1"},
		{Category: "merge-approval", Initiative: "at-b", Recommendation: "r2", Verdict: "accepted"},
		{Category: "scope-call", Initiative: "at-c", Recommendation: "r3", Verdict: "corrected", Decision: "decision-3"},
	}
	for _, r := range records {
		if err := r.Run(ctx); err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}
	stdout.Reset()

	if err := (&stewardLedgerRecallKong{Category: "scope-call", JSON: true}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got []StewardLedgerRecord
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal output: %v (out=%s)", err, stdout.String())
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 scope-call records, got %d: %+v", len(got), got)
	}
	// Most recent first: r3 (at-c) was recorded after r1 (at-a).
	if got[0].Initiative != "at-c" || got[1].Initiative != "at-a" {
		t.Errorf("expected most-recent-first order [at-c, at-a], got [%s, %s]", got[0].Initiative, got[1].Initiative)
	}
	for _, r := range got {
		if r.Category != StewardLedgerCategoryScopeCall {
			t.Errorf("expected only scope-call records, got %+v", r)
		}
	}
}

func TestStewardLedgerRecall_RespectsLimit(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	for i := 0; i < 5; i++ {
		rec := &stewardLedgerRecordKong{
			Category:       "unblock-action",
			Initiative:     "at-x",
			Recommendation: fmt.Sprintf("r%d", i),
			Verdict:        "accepted",
		}
		if err := rec.Run(ctx); err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}
	stdout.Reset()

	if err := (&stewardLedgerRecallKong{Category: "unblock-action", Limit: 2, JSON: true}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got []StewardLedgerRecord
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 records (limit), got %d", len(got))
	}
}

func TestStewardLedgerRecall_NoLedgerFile(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	if err := (&stewardLedgerRecallKong{Category: "scope-call"}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "no ledger entries") {
		t.Errorf("expected 'no ledger entries'; got: %q", stdout.String())
	}
}

func TestStewardLedgerRecall_EmptyJSONIsArrayNotNull(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)

	if err := (&stewardLedgerRecallKong{Category: "scope-call", JSON: true}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "[]" {
		t.Errorf("expected empty JSON array %q; got: %q", "[]", got)
	}
}

func TestStewardLedgerRecall_SkipsMalformedLine(t *testing.T) {
	home := t.TempDir()
	ctx, stdout, stderr := makeCtx(&fakeBD{}, home)

	rec := &stewardLedgerRecordKong{Category: "scope-call", Initiative: "at-a", Recommendation: "r1", Verdict: "accepted"}
	if err := rec.Run(ctx); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	// Append a malformed line directly.
	f, err := os.OpenFile(StewardLedgerPath(ctx), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	if _, err := f.WriteString("not json\n"); err != nil {
		t.Fatalf("write malformed line: %v", err)
	}
	f.Close()
	stdout.Reset()

	if err := (&stewardLedgerRecallKong{Category: "scope-call", JSON: true}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got []StewardLedgerRecord
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 valid record (malformed line skipped), got %d", len(got))
	}
	if !strings.Contains(stderr.String(), "warning") {
		t.Errorf("expected warning on stderr for malformed line, got: %q", stderr.String())
	}
}

func TestStewardLedgerRecall_InvalidCategoryRejected(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	if err := (&stewardLedgerRecallKong{Category: "bogus"}).Run(ctx); err == nil {
		t.Fatal("expected error for invalid category, got nil")
	}
}

func TestStewardLedgerRecall_NilContext(t *testing.T) {
	if err := (&stewardLedgerRecallKong{Category: "scope-call"}).Run(nil); err == nil {
		t.Fatal("expected error for nil context")
	}
}
