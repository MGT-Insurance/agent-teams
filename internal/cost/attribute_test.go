package cost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ---- SlugifyCWD tests -------------------------------------------------------

func TestSlugifyCWD_verifiedExamples(t *testing.T) {
	// These three examples are verified against real on-disk directory names.
	cases := []struct {
		cwd  string
		want string
	}{
		{
			"/Users/ericlloyd/.agent-teams-worktrees/per-initiative-token-cost-attribution-and",
			"-Users-ericlloyd--agent-teams-worktrees-per-initiative-token-cost-attribution-and",
		},
		{
			"/private/tmp",
			"-private-tmp",
		},
		{
			"/Users/ericlloyd/Code/agent-teams",
			"-Users-ericlloyd-Code-agent-teams",
		},
	}
	for _, tc := range cases {
		got := SlugifyCWD(tc.cwd)
		if got != tc.want {
			t.Errorf("SlugifyCWD(%q) = %q, want %q", tc.cwd, got, tc.want)
		}
	}
}

func TestSlugifyCWD_dotBecomesHyphen(t *testing.T) {
	// '.' is non-alphanumeric, so "/.foo" → "--foo" (leading '/' + '.' both become '-').
	got := SlugifyCWD("/.agent-teams")
	want := "--agent-teams"
	if got != want {
		t.Errorf("SlugifyCWD(%q) = %q, want %q", "/.agent-teams", got, want)
	}
}

func TestSlugifyCWD_noLowercase(t *testing.T) {
	got := SlugifyCWD("/Users/Bob")
	want := "-Users-Bob"
	if got != want {
		t.Errorf("SlugifyCWD(%q) = %q, want %q", "/Users/Bob", got, want)
	}
}

// ---- Attribute tests --------------------------------------------------------

// buildFixture creates a minimal synthetic jobs+projects tree and returns
// (jobsDir, projectsDir). The tree contains:
//   - One DRI session matching the given initiativeID
//   - A main .jsonl with 3 assistant records:
//     (a) opus with nested cache_creation split present
//     (b) haiku with NO nested split (exercises fallback floor)
//     (c) a duplicate opus record to verify accumulation
//   - A subagents/agent-x.jsonl with 1 opus assistant record
//   - A non-assistant line (should be skipped)
//   - A malformed JSON line (should be skipped)
func buildFixture(t *testing.T, initiativeID string) (jobsDir, projectsDir string) {
	t.Helper()
	root := t.TempDir()
	jobsDir = filepath.Join(root, "jobs")
	projectsDir = filepath.Join(root, "projects")

	cwd := "/Users/testuser/worktrees/my-initiative"
	sessionID := "aaaabbbb-0000-1111-2222-333344445555"

	// Write state.json for the DRI job.
	jobDir := filepath.Join(jobsDir, "job1")
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		t.Fatal(err)
	}
	stateData, _ := json.Marshal(map[string]string{
		"intent":    "/dri " + initiativeID,
		"sessionId": sessionID,
		"cwd":       cwd,
	})
	if err := os.WriteFile(filepath.Join(jobDir, "state.json"), stateData, 0644); err != nil {
		t.Fatal(err)
	}

	// Build the project dir.
	slug := SlugifyCWD(cwd)
	projDir := filepath.Join(projectsDir, slug)
	if err := os.MkdirAll(filepath.Join(projDir, sessionID, "subagents"), 0755); err != nil {
		t.Fatal(err)
	}

	// Main .jsonl lines.
	//
	// (a) opus WITH nested cache_creation split.
	opusSplit := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-opus-4-8",
			"usage": map[string]any{
				"input_tokens":                int64(1000),
				"output_tokens":               int64(500),
				"cache_creation_input_tokens": int64(800), // total
				"cache_read_input_tokens":     int64(200),
				"cache_creation": map[string]any{
					"ephemeral_5m_input_tokens": int64(300),
					"ephemeral_1h_input_tokens": int64(500),
				},
			},
		},
	}
	// (b) haiku WITHOUT nested cache_creation split (fallback floor path).
	haikuNoSplit := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-haiku-4-5-20251001",
			"usage": map[string]any{
				"input_tokens":                int64(2000),
				"output_tokens":               int64(1000),
				"cache_creation_input_tokens": int64(600),
				"cache_read_input_tokens":     int64(0),
				// no cache_creation nested object
			},
		},
	}
	// (c) second opus record — accumulation check.
	opusMore := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-opus-4-8",
			"usage": map[string]any{
				"input_tokens":  int64(100),
				"output_tokens": int64(50),
			},
		},
	}
	// non-assistant record — must be skipped.
	userRecord := map[string]any{
		"type": "human",
		"message": map[string]any{
			"role": "user",
		},
	}

	lines := []any{opusSplit, haikuNoSplit, opusMore, userRecord}
	writeJSONL(t, filepath.Join(projDir, sessionID+".jsonl"), lines, "malformed line here!!!")

	// Subagent .jsonl: one opus record.
	subRecord := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-opus-4-8",
			"usage": map[string]any{
				"input_tokens":  int64(50),
				"output_tokens": int64(25),
			},
		},
	}
	writeJSONL(t, filepath.Join(projDir, sessionID, "subagents", "agent-abc123.jsonl"), []any{subRecord}, "")

	return jobsDir, projectsDir
}

// writeJSONL marshals each record as a JSON line; appends extraLine if non-empty.
func writeJSONL(t *testing.T, path string, records []any, extraLine string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, r := range records {
		b, _ := json.Marshal(r)
		f.Write(b)
		f.WriteString("\n")
	}
	if extraLine != "" {
		f.WriteString(extraLine + "\n")
	}
}

// findModel returns the ModelUsage for model in r.ByModel, or zero value + false.
func findModel(r Report, model string) (ModelUsage, bool) {
	for _, m := range r.ByModel {
		if m.Model == model {
			return m, true
		}
	}
	return ModelUsage{}, false
}

func TestAttribute_basic(t *testing.T) {
	const id = "test-init-001"
	jobsDir, projectsDir := buildFixture(t, id)

	report, err := Attribute(id, jobsDir, projectsDir)
	if err != nil {
		t.Fatalf("Attribute error: %v", err)
	}

	if report.InitiativeID != id {
		t.Errorf("InitiativeID=%q, want %q", report.InitiativeID, id)
	}
	if report.DRISessions != 1 {
		t.Errorf("DRISessions=%d, want 1", report.DRISessions)
	}

	// Expect 2 model entries: opus and haiku.
	if len(report.ByModel) != 2 {
		t.Errorf("len(ByModel)=%d, want 2 (got models: %v)", len(report.ByModel), modelNames(report))
	}
}

func TestAttribute_opusAccumulation(t *testing.T) {
	const id = "test-init-002"
	jobsDir, projectsDir := buildFixture(t, id)

	report, err := Attribute(id, jobsDir, projectsDir)
	if err != nil {
		t.Fatalf("Attribute error: %v", err)
	}

	opus, ok := findModel(report, "claude-opus-4-8")
	if !ok {
		t.Fatal("claude-opus-4-8 not found in report")
	}

	// From main: record (a) input=1000, record (c) input=100 + subagent input=50 → 1150
	wantInput := int64(1000 + 100 + 50)
	if opus.InputTokens != wantInput {
		t.Errorf("opus InputTokens=%d, want %d", opus.InputTokens, wantInput)
	}

	// Output: 500 + 50 + 25 = 575
	wantOutput := int64(500 + 50 + 25)
	if opus.OutputTokens != wantOutput {
		t.Errorf("opus OutputTokens=%d, want %d", opus.OutputTokens, wantOutput)
	}

	// CacheCreation5mTokens from record (a): 300 (records c and subagent have no split → 0)
	if opus.CacheCreation5mTokens != 300 {
		t.Errorf("opus CacheCreation5mTokens=%d, want 300", opus.CacheCreation5mTokens)
	}
	// CacheCreation1hTokens from record (a): 500
	if opus.CacheCreation1hTokens != 500 {
		t.Errorf("opus CacheCreation1hTokens=%d, want 500", opus.CacheCreation1hTokens)
	}
	// CacheCreationInputTokens (total) = 800 from (a) + 0 from (c) + 0 from subagent = 800
	if opus.CacheCreationInputTokens != 800 {
		t.Errorf("opus CacheCreationInputTokens=%d, want 800", opus.CacheCreationInputTokens)
	}
}

func TestAttribute_haikuFallbackFloor(t *testing.T) {
	const id = "test-init-003"
	jobsDir, projectsDir := buildFixture(t, id)

	report, err := Attribute(id, jobsDir, projectsDir)
	if err != nil {
		t.Fatalf("Attribute error: %v", err)
	}

	haiku, ok := findModel(report, "claude-haiku-4-5-20251001")
	if !ok {
		t.Fatal("claude-haiku-4-5-20251001 not found in report")
	}

	// Record (b): input=2000, output=1000, cache_creation_total=600, no split.
	if haiku.InputTokens != 2000 {
		t.Errorf("haiku InputTokens=%d, want 2000", haiku.InputTokens)
	}
	// No nested split → both should be 0.
	if haiku.CacheCreation5mTokens != 0 {
		t.Errorf("haiku CacheCreation5mTokens=%d, want 0 (no nested split)", haiku.CacheCreation5mTokens)
	}
	if haiku.CacheCreation1hTokens != 0 {
		t.Errorf("haiku CacheCreation1hTokens=%d, want 0 (no nested split)", haiku.CacheCreation1hTokens)
	}
	// CacheCreationInputTokens carries the top-level total.
	if haiku.CacheCreationInputTokens != 600 {
		t.Errorf("haiku CacheCreationInputTokens=%d, want 600", haiku.CacheCreationInputTokens)
	}
}

func TestAttribute_subagentsCounted(t *testing.T) {
	const id = "test-init-004"
	jobsDir, projectsDir := buildFixture(t, id)

	report, err := Attribute(id, jobsDir, projectsDir)
	if err != nil {
		t.Fatalf("Attribute error: %v", err)
	}

	opus, ok := findModel(report, "claude-opus-4-8")
	if !ok {
		t.Fatal("claude-opus-4-8 not found")
	}
	// Subagent contributes input=50. Total opus input = 1000+100+50 = 1150.
	// If subagents were skipped, total would be 1100.
	if opus.InputTokens < 1150 {
		t.Errorf("subagent tokens not counted: opus InputTokens=%d, want ≥1150", opus.InputTokens)
	}
}

func TestAttribute_noSessions(t *testing.T) {
	const id = "test-init-no-match"
	jobsDir, projectsDir := buildFixture(t, "different-id")

	// Attribute for an id that doesn't match any state.json intent.
	report, err := Attribute(id, jobsDir, projectsDir)
	if err != nil {
		t.Fatalf("Attribute error: %v", err)
	}
	if report.DRISessions != 0 {
		t.Errorf("DRISessions=%d, want 0", report.DRISessions)
	}
	if len(report.ByModel) != 0 {
		t.Errorf("ByModel not empty for no-match initiative")
	}
}

func TestAttribute_missingJobsDir(t *testing.T) {
	root := t.TempDir()
	_, err := Attribute("anything", filepath.Join(root, "nonexistent"), filepath.Join(root, "projects"))
	if err != nil {
		t.Errorf("missing jobsDir should return nil error (no jobs = zero report), got: %v", err)
	}
}

func TestAttribute_dedupeBySessionID(t *testing.T) {
	// Two job dirs with the same sessionId and intent → counted once.
	root := t.TempDir()
	jobsDir := filepath.Join(root, "jobs")
	projectsDir := filepath.Join(root, "projects")

	const id = "test-dedupe"
	sessionID := "ddddeeee-0000-1111-2222-333344445555"
	cwd := "/Users/testuser/worktrees/dedupe-test"
	stateData, _ := json.Marshal(map[string]string{
		"intent":    "/dri " + id,
		"sessionId": sessionID,
		"cwd":       cwd,
	})

	// Write two job dirs with identical state.json.
	for _, name := range []string{"job-a", "job-b"} {
		dir := filepath.Join(jobsDir, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "state.json"), stateData, 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create an empty project dir so collectTranscripts has somewhere to look.
	slug := SlugifyCWD(cwd)
	if err := os.MkdirAll(filepath.Join(projectsDir, slug), 0755); err != nil {
		t.Fatal(err)
	}

	report, err := Attribute(id, jobsDir, projectsDir)
	if err != nil {
		t.Fatalf("Attribute error: %v", err)
	}
	if report.DRISessions != 1 {
		t.Errorf("DRISessions=%d, want 1 (duplicate sessionId should be deduped)", report.DRISessions)
	}
}

// TestAttribute_dedupeByMessageID verifies that duplicate JSONL lines sharing the
// same message.id are counted exactly once, while records with no message.id
// still accumulate (the existing fixture relies on this).
//
// Fixture layout (single session, main transcript only):
//   - assistant turn with message.id="msg-X", emitted TWICE (identical usage)
//   - assistant turn with message.id="msg-Y", emitted once
//   - assistant record with NO message.id (must accumulate regardless)
//
// Expected: X tokens counted once, Y once, id-less record once — not 2× for X.
func TestAttribute_dedupeByMessageID(t *testing.T) {
	const id = "test-init-dedup-msgid"
	root := t.TempDir()
	jobsDir := filepath.Join(root, "jobs")
	projectsDir := filepath.Join(root, "projects")

	cwd := "/Users/testuser/worktrees/dedup-msgid"
	sessionID := "ccccdddd-0000-1111-2222-333344445555"

	// Write state.json.
	jobDir := filepath.Join(jobsDir, "job1")
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		t.Fatal(err)
	}
	stateData, _ := json.Marshal(map[string]string{
		"intent":    "/dri " + id,
		"sessionId": sessionID,
		"cwd":       cwd,
	})
	if err := os.WriteFile(filepath.Join(jobDir, "state.json"), stateData, 0644); err != nil {
		t.Fatal(err)
	}

	// Build the project dir.
	slug := SlugifyCWD(cwd)
	projDir := filepath.Join(projectsDir, slug)
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Turn X: emitted twice with identical usage. After dedup: input=100, output=40.
	turnX := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-X",
			"role":  "assistant",
			"model": "claude-opus-4-8",
			"usage": map[string]any{
				"input_tokens":  int64(100),
				"output_tokens": int64(40),
			},
		},
	}
	// Turn Y: single occurrence. input=200, output=80.
	turnY := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-Y",
			"role":  "assistant",
			"model": "claude-opus-4-8",
			"usage": map[string]any{
				"input_tokens":  int64(200),
				"output_tokens": int64(80),
			},
		},
	}
	// No id: must always accumulate. input=50, output=20.
	noID := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-opus-4-8",
			"usage": map[string]any{
				"input_tokens":  int64(50),
				"output_tokens": int64(20),
			},
		},
	}

	// Write: X, X (duplicate), Y, noID — X appears twice.
	writeJSONL(t, filepath.Join(projDir, sessionID+".jsonl"), []any{turnX, turnX, turnY, noID}, "")

	report, err := Attribute(id, jobsDir, projectsDir)
	if err != nil {
		t.Fatalf("Attribute error: %v", err)
	}

	opus, ok := findModel(report, "claude-opus-4-8")
	if !ok {
		t.Fatal("claude-opus-4-8 not found in report")
	}

	// X counted once (100), Y once (200), noID once (50) → 350 total.
	// If X is double-counted the total would be 450 — this guards regression.
	wantInput := int64(100 + 200 + 50)
	if opus.InputTokens != wantInput {
		t.Errorf("InputTokens=%d, want %d (X must be deduped to one occurrence)", opus.InputTokens, wantInput)
	}

	wantOutput := int64(40 + 80 + 20)
	if opus.OutputTokens != wantOutput {
		t.Errorf("OutputTokens=%d, want %d", opus.OutputTokens, wantOutput)
	}
}

// modelNames returns model strings from r.ByModel for diagnostic messages.
func modelNames(r Report) []string {
	names := make([]string, len(r.ByModel))
	for i, m := range r.ByModel {
		names[i] = m.Model
	}
	return names
}
