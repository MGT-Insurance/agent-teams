package verbs

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// writeFixtureRole writes a synthetic role .md file into dir. Deliberately
// NOT the real plugins/agent-teams/roles/*.md text — wf7o.4 edits that prose
// in a parallel track, and a golden test over live text would go red for an
// unrelated reason (agent-teams-wf7o.9 test constraint).
func writeFixtureRole(t *testing.T, dir, name, description, model, body string) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("description: " + description + "\n")
	if model != "" {
		sb.WriteString("model: " + model + "\n")
	}
	sb.WriteString("---\n")
	sb.WriteString(body)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write fixture role %s: %v", name, err)
	}
}

// ---- buildAgentsJSON: fixture roles dir ------------------------------------

func TestBuildAgentsJSON_FixtureFourRoles(t *testing.T) {
	dir := t.TempDir()
	writeFixtureRole(t, dir, "planner.md", "Plans things.", "opus", "You are the planner.\nDo planning things.\n")
	writeFixtureRole(t, dir, "implementer.md", "Implements things.", "sonnet", "You are the implementer.\n")
	writeFixtureRole(t, dir, "reviewer.md", "Reviews things.", "sonnet", "You are the reviewer.\n")
	writeFixtureRole(t, dir, "tester.md", "Tests things.", "sonnet", "You are the tester.\n")

	got, err := buildAgentsJSON(dir)
	if err != nil {
		t.Fatalf("buildAgentsJSON: %v", err)
	}

	var payload map[string]agentDefinition
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("payload is not valid JSON: %v\npayload: %s", err, got)
	}

	wantKeys := []string{"agent-teams-planner", "agent-teams-implementer", "agent-teams-reviewer", "agent-teams-tester"}
	if len(payload) != len(wantKeys) {
		t.Fatalf("payload has %d keys, want exactly %d: %v", len(payload), len(wantKeys), payload)
	}
	for _, k := range wantKeys {
		def, ok := payload[k]
		if !ok {
			t.Errorf("payload missing key %q; got keys: %v", k, keysOf(payload))
			continue
		}
		if def.Description == "" {
			t.Errorf("%s: description is empty", k)
		}
		if strings.TrimSpace(def.Prompt) == "" {
			t.Errorf("%s: prompt is empty", k)
		}
		if strings.Contains(def.Prompt, "---") {
			t.Errorf("%s: prompt still contains a frontmatter delimiter '---': %q", k, def.Prompt)
		}
	}

	if payload["agent-teams-planner"].Model != "opus" {
		t.Errorf("planner model = %q, want %q", payload["agent-teams-planner"].Model, "opus")
	}
	for _, k := range []string{"agent-teams-implementer", "agent-teams-reviewer", "agent-teams-tester"} {
		if payload[k].Model != "sonnet" {
			t.Errorf("%s model = %q, want %q", k, payload[k].Model, "sonnet")
		}
	}

	// No "tools" key anywhere: raw-decode into a generic map and check.
	var raw map[string]map[string]any
	if err := json.Unmarshal([]byte(got), &raw); err != nil {
		t.Fatalf("re-decode as generic map: %v", err)
	}
	for k, obj := range raw {
		if _, ok := obj["tools"]; ok {
			t.Errorf("%s: payload must not carry a \"tools\" key", k)
		}
	}
}

func keysOf(m map[string]agentDefinition) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// TestBuildAgentsJSON_DescriptionVerbatim pins that description passes
// through untouched (beyond frontmatter quote/whitespace trimming) — no
// disambiguating prefix is added.
func TestBuildAgentsJSON_DescriptionVerbatim(t *testing.T) {
	dir := t.TempDir()
	const desc = "Does exactly one thing, verbatim."
	writeFixtureRole(t, dir, "planner.md", desc, "opus", "body\n")

	got, err := buildAgentsJSON(dir)
	if err != nil {
		t.Fatalf("buildAgentsJSON: %v", err)
	}
	var payload map[string]agentDefinition
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["agent-teams-planner"].Description != desc {
		t.Errorf("description = %q, want %q", payload["agent-teams-planner"].Description, desc)
	}
}

// TestBuildAgentsJSON_PromptStripsLeadingBlankLines verifies the prompt
// starts at the first non-blank line after the closing frontmatter '---',
// per agent-teams-wf7o.9 artifact (2).
func TestBuildAgentsJSON_PromptStripsLeadingBlankLines(t *testing.T) {
	dir := t.TempDir()
	raw := "---\ndescription: d\nmodel: sonnet\n---\n\n\nFirst real line.\nSecond line.\n"
	if err := os.WriteFile(filepath.Join(dir, "implementer.md"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := buildAgentsJSON(dir)
	if err != nil {
		t.Fatalf("buildAgentsJSON: %v", err)
	}
	var payload map[string]agentDefinition
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := "First real line.\nSecond line.\n"
	if got := payload["agent-teams-implementer"].Prompt; got != want {
		t.Errorf("prompt = %q, want %q", got, want)
	}
}

// TestBuildAgentsJSON_ModelOmittedWhenAbsentOrInherit verifies the model key
// is omitted from the marshaled JSON (not emitted as "") when there is no
// model: line, and when the value is exactly "inherit".
func TestBuildAgentsJSON_ModelOmittedWhenAbsentOrInherit(t *testing.T) {
	dir := t.TempDir()
	writeFixtureRole(t, dir, "planner.md", "d1", "", "body\n") // no model: line at all
	writeFixtureRole(t, dir, "implementer.md", "d2", "inherit", "body\n")

	got, err := buildAgentsJSON(dir)
	if err != nil {
		t.Fatalf("buildAgentsJSON: %v", err)
	}

	var raw map[string]map[string]any
	if err := json.Unmarshal([]byte(got), &raw); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	for _, k := range []string{"agent-teams-planner", "agent-teams-implementer"} {
		if _, ok := raw[k]["model"]; ok {
			t.Errorf("%s: payload must omit \"model\" key, got: %v", k, raw[k])
		}
	}
}

// TestBuildAgentsJSON_ModelQuotesAndWhitespaceTrimmed verifies a quoted or
// padded model: value is trimmed before use.
func TestBuildAgentsJSON_ModelQuotesAndWhitespaceTrimmed(t *testing.T) {
	dir := t.TempDir()
	raw := "---\ndescription: d\nmodel: \"sonnet\"  \n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "tester.md"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := buildAgentsJSON(dir)
	if err != nil {
		t.Fatalf("buildAgentsJSON: %v", err)
	}
	var payload map[string]agentDefinition
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["agent-teams-tester"].Model != "sonnet" {
		t.Errorf("model = %q, want %q", payload["agent-teams-tester"].Model, "sonnet")
	}
}

// ---- fail-loud: buildAgentsJSON / resolveRolesDir --------------------------

// TestBuildAgentsJSON_MissingDirFailsLoud proves buildAgentsJSON returns a
// non-nil, non-empty-payload error when the roles directory does not exist
// at all — the fail-loud contract (agent-teams-wf7o.9 artifact (4)). A
// version of this test that instead asserted "" and nil would be the exact
// bug this bead exists to prevent.
func TestBuildAgentsJSON_MissingDirFailsLoud(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	got, err := buildAgentsJSON(missing)
	if err == nil {
		t.Fatalf("expected an error for a missing roles dir, got nil (payload: %q)", got)
	}
	if got != "" {
		t.Errorf("expected an empty payload on failure, got %q", got)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error should name the path tried (%q); got: %v", missing, err)
	}
}

// TestBuildAgentsJSON_EmptyDirFailsLoud proves an existing-but-empty roles
// directory (zero *.md files — the "produces an empty payload" case named in
// the bead) also fails loud rather than returning "{}" or "".
func TestBuildAgentsJSON_EmptyDirFailsLoud(t *testing.T) {
	dir := t.TempDir() // exists, but has no *.md files in it
	got, err := buildAgentsJSON(dir)
	if err == nil {
		t.Fatalf("expected an error for a roles dir with zero *.md files, got nil (payload: %q)", got)
	}
	if got != "" {
		t.Errorf("expected an empty payload on failure, got %q", got)
	}
}

// TestBuildAgentsJSON_UnparseableFileFailsLoud proves one bad role file fails
// the whole build rather than silently producing a partial payload.
func TestBuildAgentsJSON_UnparseableFileFailsLoud(t *testing.T) {
	dir := t.TempDir()
	writeFixtureRole(t, dir, "planner.md", "d", "opus", "body\n")
	if err := os.WriteFile(filepath.Join(dir, "implementer.md"), []byte("not frontmatter at all\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := buildAgentsJSON(dir)
	if err == nil {
		t.Fatalf("expected an error for an unparseable role file, got nil (payload: %q)", got)
	}
	if got != "" {
		t.Errorf("expected an empty payload on failure, got %q", got)
	}
}

// TestBuildAgentsJSON_SkipsReadmeByExactName proves roles/README.md — the
// frontmatter-less workaround doc this package's own doc comment points at
// (agent-teams-wf7o.18) — is excluded from the payload by exact filename,
// while the real role files still build normally.
func TestBuildAgentsJSON_SkipsReadmeByExactName(t *testing.T) {
	dir := t.TempDir()
	writeFixtureRole(t, dir, "planner.md", "d", "opus", "body\n")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Not a role\n\nNo frontmatter here at all.\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}

	got, err := buildAgentsJSON(dir)
	if err != nil {
		t.Fatalf("buildAgentsJSON: %v, want README.md skipped and build to succeed", err)
	}
	var payload map[string]agentDefinition
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("payload has %d keys, want exactly 1 (README.md must not become a role): %v", len(payload), keysOf(payload))
	}
	if _, ok := payload["agent-teams-README"]; ok {
		t.Errorf("payload must not contain an agent-teams-README key")
	}
	if _, ok := payload["agent-teams-planner"]; !ok {
		t.Errorf("payload missing agent-teams-planner despite README.md sitting alongside it")
	}
}

// TestBuildAgentsJSON_OtherFrontmatterLessFileStillFailsLoud proves the
// README.md skip is scoped to that exact filename, NOT to "any file lacking
// frontmatter" — a genuinely malformed role file (any other name) must still
// fail the whole build loudly. This is the non-vacuous half of wf7o.18's
// acceptance bar: it is written to catch the over-broad fix (skip anything
// without frontmatter) that the bead explicitly forbids, and was confirmed to
// go red under that over-broad implementation before this file was restored
// to the exact-name skip (see the implementer's report for that transcript).
func TestBuildAgentsJSON_OtherFrontmatterLessFileStillFailsLoud(t *testing.T) {
	dir := t.TempDir()
	writeFixtureRole(t, dir, "planner.md", "d", "opus", "body\n")
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Not a role, not README.md either\n\nNo frontmatter here.\n"), 0o644); err != nil {
		t.Fatalf("write notes.md: %v", err)
	}

	got, err := buildAgentsJSON(dir)
	if err == nil {
		t.Fatalf("expected an error: a frontmatter-less .md file other than README.md must fail loud, got nil (payload: %q)", got)
	}
	if got != "" {
		t.Errorf("expected an empty payload on failure, got %q", got)
	}
	if !strings.Contains(err.Error(), "notes.md") {
		t.Errorf("error should name the offending file (notes.md); got: %v", err)
	}
}

// TestResolveRolesDir_PluginRootHappyPath verifies the $CLAUDE_PLUGIN_ROOT
// resolution branch: when it is set and <root>/roles exists, that directory
// is returned.
func TestResolveRolesDir_PluginRootHappyPath(t *testing.T) {
	root := t.TempDir()
	rolesDir := filepath.Join(root, "roles")
	if err := os.Mkdir(rolesDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFixtureRole(t, rolesDir, "planner.md", "d", "opus", "body\n")

	t.Setenv("CLAUDE_PLUGIN_ROOT", root)
	got, err := resolveRolesDir()
	if err != nil {
		t.Fatalf("resolveRolesDir: %v", err)
	}
	if got != rolesDir {
		t.Errorf("resolveRolesDir() = %q, want %q", got, rolesDir)
	}
}

// TestResolveRolesDir_FailsLoudWhenAbsent proves resolveRolesDir fails loud,
// naming the paths it tried, when neither $CLAUDE_PLUGIN_ROOT/roles nor the
// self-binary-relative fallback exists. $CLAUDE_PLUGIN_ROOT is pointed at a
// real, existing directory that simply has no roles/ subdirectory (so the
// env-based path is provably tried and provably absent); the self-binary
// fallback is the `go test` binary's own directory, which never has a
// sibling roles/ either, so this is deterministic without any mocking.
func TestResolveRolesDir_FailsLoudWhenAbsent(t *testing.T) {
	root := t.TempDir() // exists, but has no "roles" subdirectory
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	got, err := resolveRolesDir()
	if err == nil {
		t.Fatalf("expected an error, got nil (dir: %q)", got)
	}
	if got != "" {
		t.Errorf("expected an empty dir on failure, got %q", got)
	}
	wantPath := filepath.Join(root, "roles")
	if !strings.Contains(err.Error(), wantPath) {
		t.Errorf("error should name the $CLAUDE_PLUGIN_ROOT-derived path (%q); got: %v", wantPath, err)
	}
}

// TestBuildAgentsPayload_FailsLoudWhenRolesDirUnresolvable proves the
// combined resolve+build helper used by both the agents-json verb and
// rawLaunchBGSession propagates the same fail-loud error rather than
// swallowing it into an empty string.
func TestBuildAgentsPayload_FailsLoudWhenRolesDirUnresolvable(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", t.TempDir()) // no "roles" subdir underneath

	got, err := buildAgentsPayload()
	if err == nil {
		t.Fatalf("expected an error, got nil (payload: %q)", got)
	}
	if got != "" {
		t.Errorf("expected an empty payload on failure, got %q", got)
	}
}

// ---- structural smoke test against the REAL roles/*.md ---------------------

// TestBuildAgentsJSON_RealRolesStructure runs buildAgentsJSON against the
// actual plugins/agent-teams/roles/*.md files, but asserts only STRUCTURE
// (keys present, model mapping, non-empty/frontmatter-free prompts) — never
// literal description/prompt text. wf7o.4 edits that prose in a parallel
// track; a golden-text assertion here would go red for a reason unrelated to
// this bead (agent-teams-wf7o.9 test constraint).
func TestBuildAgentsJSON_RealRolesStructure(t *testing.T) {
	dir := filepath.Join("..", "..", "plugins", "agent-teams", "roles")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("real roles dir not found at %s (unexpected repo layout?): %v", dir, err)
	}

	got, err := buildAgentsJSON(dir)
	if err != nil {
		t.Fatalf("buildAgentsJSON(%s): %v", dir, err)
	}

	var payload map[string]agentDefinition
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("payload is not valid JSON: %v", err)
	}

	wantModel := map[string]string{
		"agent-teams-planner":      "claude-opus-4-8",
		"agent-teams-implementer":  "sonnet",
		"agent-teams-reviewer":     "sonnet",
		"agent-teams-tester":       "sonnet",
		"agent-teams-investigator": "claude-opus-4-8",
	}
	if len(payload) != len(wantModel) {
		t.Fatalf("payload has %d keys, want exactly %d: %v", len(payload), len(wantModel), keysOf(payload))
	}
	for key, model := range wantModel {
		def, ok := payload[key]
		if !ok {
			t.Errorf("payload missing key %q", key)
			continue
		}
		if def.Model != model {
			t.Errorf("%s model = %q, want %q", key, def.Model, model)
		}
		if def.Description == "" {
			t.Errorf("%s: description is empty", key)
		}
		if strings.TrimSpace(def.Prompt) == "" {
			t.Errorf("%s: prompt is empty", key)
		}
		if strings.HasPrefix(strings.TrimSpace(def.Prompt), "---") {
			t.Errorf("%s: prompt still starts with a frontmatter delimiter", key)
		}
	}
}

// ---- agents-json verb -------------------------------------------------------

func TestAgentsJSONKong_PrintsPayload(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}
	cmd := &agentsJSONKong{buildPayload: func() (string, error) {
		return `{"agent-teams-planner":{"description":"d","prompt":"p","model":"opus"}}`, nil
	}}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != `{"agent-teams-planner":{"description":"d","prompt":"p","model":"opus"}}` {
		t.Errorf("stdout = %q", got)
	}
}

func TestAgentsJSONKong_PropagatesFailLoudError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}
	cmd := &agentsJSONKong{buildPayload: func() (string, error) { return "", errFixtureRolesUnavailable }}

	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay empty on failure, got %q", stdout.String())
	}
}

var errFixtureRolesUnavailable = &fixtureError{"roles dir unavailable"}

type fixtureError struct{ msg string }

func (e *fixtureError) Error() string { return e.msg }

// ---- bgSessionArgs: --agents flag -------------------------------------------

// TestBGSessionArgs_AgentsFlag verifies "--agents" is emitted immediately
// followed by the exact payload passed in, and that the prompt remains the
// last argv element (agent-teams-wf7o.10 acceptance).
func TestBGSessionArgs_AgentsFlag(t *testing.T) {
	const payload = `{"agent-teams-planner":{"description":"d","prompt":"p","model":"opus"}}`
	prompt := "/dri at-abc123"
	args := bgSessionArgs("my-session", prompt, "", "", "", "", payload, "")

	found := false
	for i, a := range args {
		if a == "--agents" {
			if i+1 >= len(args) {
				t.Fatal("--agents has no following value in argv")
			}
			if args[i+1] != payload {
				t.Errorf("value after --agents = %q, want %q", args[i+1], payload)
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("argv missing --agents; got: %v", args)
	}

	if last := args[len(args)-1]; last != prompt {
		t.Errorf("last argv element = %q, want %q (--agents payload must not push the prompt out of last position)", last, prompt)
	}
}
