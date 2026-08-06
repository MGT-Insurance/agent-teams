package verbs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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

// spawnCheckFixtureBuiltinNamed is the fixture shape agent-teams-wf7o.19
// found missing from the original suite: a NAMED spawn of a BUILT-IN type
// (general-purpose, Explore, fork, claude-code-guide, ...). It carries
// taskKind == in_process_teammate (unlike spawnCheckFixtureIrrelevant, which
// has no taskKind at all) but, being a built-in type, never carries
// customAgentType even though the spawn is completely healthy. Presence of a
// matching Agent tool_use call in the parent transcript (added per-test via
// putSpawnCheckParentTranscript) requesting a non-role type is what proves
// this must resolve OK rather than DEFINITION-DROPPED.
const spawnCheckFixtureBuiltinNamed = `{"agentType":"ci-helper","name":"ci-helper","spawnDepth":0,"model":"sonnet","taskKind":"in_process_teammate","teamName":"session-builtin","permissionMode":"bypassPermissions"}`

// spawnCheckFixtureNestedBuiltinNamed is spawnCheckFixtureBuiltinNamed's
// spawnDepth-1 counterpart: a named built-in spawn made BY another agent
// (parentAgentId set), not by the top-level session. Its Agent tool_use call
// lives in the parent agent's own subagent transcript, not the top-level
// <session-id>.jsonl.
const spawnCheckFixtureNestedBuiltinNamed = `{"agentType":"nested-helper","name":"nested-helper","parentAgentId":"aparent0011","spawnDepth":1,"model":"sonnet","taskKind":"in_process_teammate","teamName":"session-builtin","permissionMode":"bypassPermissions"}`

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

// spawnCheckAgentToolUseLine builds one transcript line in the real shape
// spawnCheckReadAgentCalls parses: a message whose content is a one-element
// tool_use array for the Agent tool.
func spawnCheckAgentToolUseLine(t *testing.T, subagentType, name, description string) string {
	t.Helper()
	rec := map[string]any{
		"message": map[string]any{
			"content": []map[string]any{
				{
					"type": "tool_use",
					"name": "Agent",
					"input": map[string]any{
						"subagent_type": subagentType,
						"name":          name,
						"description":   description,
					},
				},
			},
		},
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal transcript line: %v", err)
	}
	return string(b)
}

// putSpawnCheckParentTranscript writes a top-level session transcript at
// <root>/<project>/<session>.jsonl — the file spawnCheckParentTranscriptPath
// looks up for a spawnDepth-0 sidecar (no parentAgentId) — one line per
// entry in lines.
func putSpawnCheckParentTranscript(t *testing.T, root, project, session string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(root, project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, session+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// putSpawnCheckNestedParentTranscript writes a subagent's OWN transcript at
// <root>/<project>/<session>/subagents/agent-<parentAgentID>.jsonl — the
// file spawnCheckParentTranscriptPath looks up for a spawnDepth>=1 sidecar
// whose parentAgentId is parentAgentID.
func putSpawnCheckNestedParentTranscript(t *testing.T, root, project, session, parentAgentID string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(root, project, session, "subagents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, "agent-"+parentAgentID+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
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
	// The Agent tool_use call the sidecar was recovered from: a role was
	// requested via the removed colon key, which is exactly the KNOWN-bad
	// shape this verb was built to catch.
	putSpawnCheckParentTranscript(t, root, "proj", "sess1",
		spawnCheckAgentToolUseLine(t, "agent-teams:planner", "namedplug", "named plugin scoped"))

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
	if f.RequestedType != "agent-teams:planner" {
		t.Errorf("RequestedType = %q, want agent-teams:planner", f.RequestedType)
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
	putSpawnCheckParentTranscript(t, root, "proj", "sess1",
		spawnCheckAgentToolUseLine(t, "agent-teams:planner", "namedplug", "named plugin scoped"))

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

// TestScanSpawnCheck_NamedBuiltinTypeIsNotFlagged is THE verification-bar
// fixture agent-teams-wf7o.19 found missing: a NAMED spawn of a BUILT-IN
// type (general-purpose here) with no customAgentType — exactly the healthy,
// common pattern (forking yourself, spawning a general-purpose helper) that
// the pre-fix predicate flagged as DEFINITION-DROPPED, because it could not
// tell "no definition was ever expected" apart from "one was expected and
// didn't attach". Reverting the fix (checking only CustomAgentType == "" as
// the pre-wf7o.19 code did) turns this test red; restoring it goes green —
// see the implementer's report for that transcript.
func TestScanSpawnCheck_NamedBuiltinTypeIsNotFlagged(t *testing.T) {
	root := t.TempDir()
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-ci-helper", spawnCheckFixtureBuiltinNamed)
	putSpawnCheckParentTranscript(t, root, "proj", "sess1",
		spawnCheckAgentToolUseLine(t, "general-purpose", "ci-helper", ""))

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
		t.Errorf("Status = %q, want %q (a named built-in spawn must not be flagged)", f.Status, spawnCheckStatusOK)
	}
	if f.RequestedType != "general-purpose" {
		t.Errorf("RequestedType = %q, want general-purpose", f.RequestedType)
	}
}

// TestScanSpawnCheck_NestedNamedBuiltinTypeIsOK covers the spawnDepth >= 1
// path: the Agent tool_use call that spawned this sidecar was made by
// ANOTHER agent, not the top-level session, so the join must look in that
// parent agent's own subagent transcript (keyed by parentAgentId), not
// <session-id>.jsonl. Mirrors the real ci-gate-runner / nested-explore-probe
// shape from agent-teams-wf7o.19's evidence session.
func TestScanSpawnCheck_NestedNamedBuiltinTypeIsOK(t *testing.T) {
	root := t.TempDir()
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-nested-helper", spawnCheckFixtureNestedBuiltinNamed)
	putSpawnCheckNestedParentTranscript(t, root, "proj", "sess1", "aparent0011",
		spawnCheckAgentToolUseLine(t, "Explore", "nested-helper", ""))

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
	if f.RequestedType != "Explore" {
		t.Errorf("RequestedType = %q, want Explore", f.RequestedType)
	}
}

// TestScanSpawnCheck_MissingParentTranscriptIsTypeUnknown proves the third
// classification bucket: when the join cannot be made at all (no parent
// transcript on disk), the result must be its own distinct TYPE-UNKNOWN
// status — never silently folded into OK or DEFINITION-DROPPED.
func TestScanSpawnCheck_MissingParentTranscriptIsTypeUnknown(t *testing.T) {
	root := t.TempDir()
	fixture := `{"agentType":"mystery","name":"mystery","spawnDepth":0,"taskKind":"in_process_teammate","teamName":"session-mystery","permissionMode":"bypassPermissions"}`
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-mystery", fixture)
	// Deliberately no parent transcript written.

	findings, warnings, err := scanSpawnCheck(root, "", nil)
	if err != nil {
		t.Fatalf("scanSpawnCheck: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none (this is a per-finding Note, not a global warning)", warnings)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %v, want exactly 1", findings)
	}
	f := findings[0]
	if f.Status != spawnCheckStatusUnknown {
		t.Errorf("Status = %q, want %q", f.Status, spawnCheckStatusUnknown)
	}
	if f.Note == "" {
		t.Error("Note = \"\", want a non-empty explanation")
	}
	if f.RequestedType != "" {
		t.Errorf("RequestedType = %q, want empty (undetermined)", f.RequestedType)
	}
}

// TestScanSpawnCheck_DescriptionDisambiguatesCollision exercises the exact
// disambiguation rule agent-teams-wf7o.19 specifies: when the parent
// transcript has more than one Agent tool_use call sharing the sidecar's
// spawn name, the sidecar's description picks out the right one.
func TestScanSpawnCheck_DescriptionDisambiguatesCollision(t *testing.T) {
	root := t.TempDir()
	fixture := `{"agentType":"dup","name":"dup","description":"second call","spawnDepth":0,"taskKind":"in_process_teammate","teamName":"session-dup","permissionMode":"bypassPermissions"}`
	putSpawnCheckSidecar(t, root, "proj", "sess1", "agent-dup", fixture)
	putSpawnCheckParentTranscript(t, root, "proj", "sess1",
		spawnCheckAgentToolUseLine(t, "general-purpose", "dup", "first call"),
		spawnCheckAgentToolUseLine(t, "agent-teams-planner", "dup", "second call"))

	findings, _, err := scanSpawnCheck(root, "", nil)
	if err != nil {
		t.Fatalf("scanSpawnCheck: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %v, want exactly 1", findings)
	}
	f := findings[0]
	if f.RequestedType != "agent-teams-planner" {
		t.Errorf("RequestedType = %q, want agent-teams-planner (the description-matched call)", f.RequestedType)
	}
	if f.Status != spawnCheckStatusDropped {
		t.Errorf("Status = %q, want %q (role requested, no customAgentType)", f.Status, spawnCheckStatusDropped)
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
	putSpawnCheckParentTranscript(t, root, "proj-a", "target-session",
		spawnCheckAgentToolUseLine(t, "agent-teams:planner", "namedplug", "named plugin scoped"))

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
	putSpawnCheckParentTranscript(t, root, "proj", "sess1",
		spawnCheckAgentToolUseLine(t, "agent-teams:planner", "namedplug", "named plugin scoped"))

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
	putSpawnCheckParentTranscript(t, root, "proj", "sess1",
		spawnCheckAgentToolUseLine(t, "agent-teams:planner", "namedplug", "named plugin scoped"))

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

// TestSpawnCheckRoleNamesMatchRolesDir pins spawnCheckRoleNames to the actual
// shipped role definitions. Without this, adding plugins/agent-teams/roles/
// <role>.md and forgetting this list produces NO failure anywhere: spawn-check
// silently stops classifying that role's named spawns as role requests, so the
// only guard that catches a teammate whose definition never attached quietly
// stops watching the new role. That is the exact silent-miss spawn-check was
// built to eliminate, so it must not be reintroducible by omission.
func TestSpawnCheckRoleNamesMatchRolesDir(t *testing.T) {
	dir := filepath.Join("..", "..", "plugins", "agent-teams", "roles")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("real roles dir not found at %s (unexpected repo layout?): %v", dir, err)
	}

	var onDisk []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if nonRoleFiles[e.Name()] {
			continue
		}
		onDisk = append(onDisk, strings.TrimSuffix(e.Name(), ".md"))
	}
	if len(onDisk) == 0 {
		t.Fatalf("no role *.md files found in %s — the comparison would pass vacuously", dir)
	}

	got := append([]string(nil), spawnCheckRoleNames...)
	sort.Strings(got)
	sort.Strings(onDisk)

	if !reflect.DeepEqual(got, onDisk) {
		t.Errorf("spawnCheckRoleNames = %v, but roles/ ships %v.\n"+
			"Add the missing role(s) to spawnCheckRoleNames — otherwise spawn-check "+
			"stops witnessing them and a generic teammate goes undetected.", got, onDisk)
	}
}
