package verbs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── fixtures, verbatim from agent-teams-wf7o.6's spawn prompt and confirmed
// live on this machine (0 of 481 pre-2026-08-04 in_process_teammate sidecars
// carry customAgentType) ─────────────────────────────────────────────────────

const spawnCheckFixtureGood = `{"agentType":"planner-realtest","name":"planner-realtest","spawnDepth":0,"model":"opus","taskKind":"in_process_teammate","teamName":"session-b98b2e7e","customAgentType":"agent-teams-planner","permissionMode":"bypassPermissions"}`

const spawnCheckFixtureBad = `{"agentType":"namedplug","description":"named plugin scoped","name":"namedplug","spawnDepth":0,"model":"opus","taskKind":"in_process_teammate","teamName":"session-3e6b8eee","permissionMode":"bypassPermissions"}`

const spawnCheckFixtureIrrelevant = `{"agentType":"qr2i-scope-probe","spawnDepth":1}`

// makeSpawnCheckCtx builds a minimal *cli.Context with captured stdout/stderr.
func makeSpawnCheckCtx() (*cli.Context, *strings.Builder, *strings.Builder) {
	var stdout, stderr strings.Builder
	return &cli.Context{Stdout: &stdout, Stderr: &stderr}, &stdout, &stderr
}

// putSpawnCheckSidecar writes a sidecar file at
// <root>/<project>/<session>/subagents/<name>.meta.json, creating parents as
// needed, and returns its path.
func putSpawnCheckSidecar(t *testing.T, root, project, session, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, project, session, "subagents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, name+".meta.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// ── scanSpawnCheck: the core predicate ──────────────────────────────────────

func TestScanSpawnCheck_GoodFixtureIsOK(t *testing.T) {
	root := t.TempDir()
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-good", spawnCheckFixtureGood)

	findings, warnings, err := scanSpawnCheck(root, "", nil)
	if err != nil {
		t.Fatalf("scanSpawnCheck: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %v, want exactly 1", findings)
	}
	f := findings[0]
	if f.Status != spawnCheckStatusOK {
		t.Errorf("Status = %q, want %q", f.Status, spawnCheckStatusOK)
	}
	if f.CustomAgentType != "agent-teams-planner" {
		t.Errorf("CustomAgentType = %q, want agent-teams-planner", f.CustomAgentType)
	}
	if f.Name != "planner-realtest" {
		t.Errorf("Name = %q, want planner-realtest", f.Name)
	}
}

// TestScanSpawnCheck_BadFixtureIsDropped is THE critical test: it proves a
// dropped definition is actually detected. Verified non-vacuous by hand —
// inverting the predicate in scanSpawnCheck (checking sc.CustomAgentType ==
// "" instead of != "" for the OK branch) turns this RED, then was reverted.
// See the implementer's report for the red-then-green transcript.
func TestScanSpawnCheck_BadFixtureIsDropped(t *testing.T) {
	root := t.TempDir()
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-bad", spawnCheckFixtureBad)

	findings, warnings, err := scanSpawnCheck(root, "", nil)
	if err != nil {
		t.Fatalf("scanSpawnCheck: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %v, want exactly 1", findings)
	}
	f := findings[0]
	if f.Status != spawnCheckStatusDropped {
		t.Errorf("Status = %q, want %q", f.Status, spawnCheckStatusDropped)
	}
	if f.CustomAgentType != "" {
		t.Errorf("CustomAgentType = %q, want empty", f.CustomAgentType)
	}
	if f.Name != "namedplug" {
		t.Errorf("Name = %q, want namedplug", f.Name)
	}
}

func TestScanSpawnCheck_IrrelevantFixtureIsSkippedNotFlagged(t *testing.T) {
	root := t.TempDir()
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-irrelevant", spawnCheckFixtureIrrelevant)

	findings, warnings, err := scanSpawnCheck(root, "", nil)
	if err != nil {
		t.Fatalf("scanSpawnCheck: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %v, want none (ordinary unnamed subagent must be skipped, not flagged)", findings)
	}
}

func TestScanSpawnCheck_MixedCorpus(t *testing.T) {
	root := t.TempDir()
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-good", spawnCheckFixtureGood)
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-bad", spawnCheckFixtureBad)
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-irrelevant", spawnCheckFixtureIrrelevant)

	findings, _, err := scanSpawnCheck(root, "", nil)
	if err != nil {
		t.Fatalf("scanSpawnCheck: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %v, want exactly 2 (good + bad, irrelevant skipped)", findings)
	}
	byStatus := map[string]int{}
	for _, f := range findings {
		byStatus[f.Status]++
	}
	if byStatus[spawnCheckStatusOK] != 1 || byStatus[spawnCheckStatusDropped] != 1 {
		t.Errorf("byStatus = %v, want 1 OK and 1 DEFINITION-DROPPED", byStatus)
	}
}

// ── edge cases the verification bar calls out by name ───────────────────────

func TestScanSpawnCheck_MissingDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "does-not-exist")

	findings, warnings, err := scanSpawnCheck(root, "", nil)
	if err != nil {
		t.Fatalf("scanSpawnCheck: %v, want nil (missing dir is not an error)", err)
	}
	if len(findings) != 0 || len(warnings) != 0 {
		t.Fatalf("findings=%v warnings=%v, want both empty", findings, warnings)
	}
}

func TestScanSpawnCheck_EmptyDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "proj", "sess1", "subagents"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	findings, warnings, err := scanSpawnCheck(root, "", nil)
	if err != nil {
		t.Fatalf("scanSpawnCheck: %v", err)
	}
	if len(findings) != 0 || len(warnings) != 0 {
		t.Fatalf("findings=%v warnings=%v, want both empty", findings, warnings)
	}
}

func TestScanSpawnCheck_MalformedJSONIsWarnedNotFatal(t *testing.T) {
	root := t.TempDir()
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-malformed", "{not valid json")
	// A well-formed sibling must still be scanned despite the malformed file.
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-good", spawnCheckFixtureGood)

	findings, warnings, err := scanSpawnCheck(root, "", nil)
	if err != nil {
		t.Fatalf("scanSpawnCheck: %v, want nil (malformed sidecar must not be fatal)", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly 1", warnings)
	}
	if !strings.Contains(warnings[0], "malformed JSON") {
		t.Errorf("warning = %q, want it to mention malformed JSON", warnings[0])
	}
	if len(findings) != 1 || findings[0].Status != spawnCheckStatusOK {
		t.Fatalf("findings = %v, want exactly 1 OK (the well-formed sibling)", findings)
	}
}

func TestScanSpawnCheck_NoRecognisableFieldsIsSkipped(t *testing.T) {
	root := t.TempDir()
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-empty", "{}")

	findings, warnings, err := scanSpawnCheck(root, "", nil)
	if err != nil {
		t.Fatalf("scanSpawnCheck: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none (valid JSON with no taskKind is a normal skip, not a warning)", warnings)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %v, want none", findings)
	}
}

// ── --session and --since filters ───────────────────────────────────────────

func TestScanSpawnCheck_SessionFilterCrossesProjects(t *testing.T) {
	root := t.TempDir()
	putSpawnCheckSidecar(t, root, "proj-a", "target-session", "agent-bad", spawnCheckFixtureBad)
	putSpawnCheckSidecar(t, root, "proj-b", "other-session", "agent-good", spawnCheckFixtureGood)

	findings, _, err := scanSpawnCheck(root, "target-session", nil)
	if err != nil {
		t.Fatalf("scanSpawnCheck: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %v, want exactly 1 (only target-session's sidecar)", findings)
	}
	if findings[0].Status != spawnCheckStatusDropped {
		t.Errorf("Status = %q, want %q", findings[0].Status, spawnCheckStatusDropped)
	}
}

func TestScanSpawnCheck_SinceFilterExcludesOlderFiles(t *testing.T) {
	root := t.TempDir()
	oldPath := putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-old-bad", spawnCheckFixtureBad)
	newPath := putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-new-good", spawnCheckFixtureGood)

	oldTime := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes old: %v", err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatalf("Chtimes new: %v", err)
	}

	since := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	findings, _, err := scanSpawnCheck(root, "", &since)
	if err != nil {
		t.Fatalf("scanSpawnCheck: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %v, want exactly 1 (only the file at/after --since)", findings)
	}
	if findings[0].Name != "planner-realtest" {
		t.Errorf("Name = %q, want planner-realtest (the newer file)", findings[0].Name)
	}
}

// ── parseSpawnCheckSince ─────────────────────────────────────────────────────

func TestParseSpawnCheckSince(t *testing.T) {
	if _, err := parseSpawnCheckSince("2026-08-04"); err != nil {
		t.Errorf("YYYY-MM-DD: %v", err)
	}
	if _, err := parseSpawnCheckSince("2026-08-04T00:00:00Z"); err != nil {
		t.Errorf("RFC3339: %v", err)
	}
	if _, err := parseSpawnCheckSince("not-a-date"); err == nil {
		t.Error("garbage input: want error, got nil")
	}
}

// ── Run: kong wiring, flags, exit codes ─────────────────────────────────────

func TestSpawnCheckKong_Run_DefaultSessionOK(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(sessionIDEnvVar, "sess1")

	root := filepath.Join(home, ".claude", "projects")
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-good", spawnCheckFixtureGood)

	ctx, stdout, _ := makeSpawnCheckCtx()
	c := &spawnCheckKong{}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("Run: %v, want nil exit", err)
	}
	if !strings.Contains(stdout.String(), spawnCheckStatusOK) {
		t.Errorf("stdout = %q, want it to contain %q", stdout.String(), spawnCheckStatusOK)
	}
}

func TestSpawnCheckKong_Run_DroppedIsNonZeroExit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(sessionIDEnvVar, "sess1")

	root := filepath.Join(home, ".claude", "projects")
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-bad", spawnCheckFixtureBad)

	ctx, stdout, _ := makeSpawnCheckCtx()
	c := &spawnCheckKong{}
	err := c.Run(ctx)
	if err == nil {
		t.Fatal("Run: want non-nil error for a dropped definition")
	}
	if code := cli.ExitCode(err); code == 0 {
		t.Errorf("ExitCode = %d, want non-zero", code)
	}
	if !strings.Contains(stdout.String(), spawnCheckStatusDropped) {
		t.Errorf("stdout = %q, want it to contain %q", stdout.String(), spawnCheckStatusDropped)
	}
}

func TestSpawnCheckKong_Run_NoSessionNoSinceIsUsageFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(sessionIDEnvVar, "")

	ctx, _, stderr := makeSpawnCheckCtx()
	c := &spawnCheckKong{}
	err := c.Run(ctx)
	if err == nil {
		t.Fatal("Run: want an error when no session id is available anywhere")
	}
	if code := cli.ExitCode(err); code == 0 {
		t.Errorf("ExitCode = %d, want non-zero", code)
	}
	if !strings.Contains(stderr.String(), sessionIDEnvVar) {
		t.Errorf("stderr = %q, want it to mention %s", stderr.String(), sessionIDEnvVar)
	}
}

func TestSpawnCheckKong_Run_JSONOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(sessionIDEnvVar, "sess1")

	root := filepath.Join(home, ".claude", "projects")
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-good", spawnCheckFixtureGood)
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-bad", spawnCheckFixtureBad)

	ctx, stdout, _ := makeSpawnCheckCtx()
	c := &spawnCheckKong{JSON: true}
	err := c.Run(ctx)
	if err == nil {
		t.Fatal("Run: want non-nil error (one dropped finding)")
	}

	var result spawnCheckResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, stdout.String())
	}
	if result.DroppedCount != 1 {
		t.Errorf("DroppedCount = %d, want 1", result.DroppedCount)
	}
	if len(result.Findings) != 2 {
		t.Errorf("Findings = %v, want 2 entries", result.Findings)
	}
}

func TestSpawnCheckKong_Run_NoFindingsIsCleanExit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(sessionIDEnvVar, "sess-with-nothing")

	ctx, stdout, _ := makeSpawnCheckCtx()
	c := &spawnCheckKong{}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("Run: %v, want nil (no sidecars at all is not a failure)", err)
	}
	if !strings.Contains(stdout.String(), "no in_process_teammate sidecars found") {
		t.Errorf("stdout = %q, want the nothing-to-verify message", stdout.String())
	}
}

func TestSpawnCheckKong_Run_InvalidSince(t *testing.T) {
	ctx, _, stderr := makeSpawnCheckCtx()
	c := &spawnCheckKong{Since: "not-a-date"}
	err := c.Run(ctx)
	if err == nil {
		t.Fatal("Run: want an error for an unparseable --since")
	}
	if code := cli.ExitCode(err); code == 0 {
		t.Errorf("ExitCode = %d, want non-zero", code)
	}
	if !strings.Contains(stderr.String(), "invalid --since") {
		t.Errorf("stderr = %q, want it to mention invalid --since", stderr.String())
	}
}
