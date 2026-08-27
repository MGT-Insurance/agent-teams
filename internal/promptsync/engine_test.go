package promptsync

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/pelletier/go-toml/v2"
)

func TestKnownGoodDriftAndWriteControls(t *testing.T) {
	root := fixtureCopy(t)
	config := Config{Root: root}

	report, err := Check(config)
	if err != nil {
		t.Fatalf("known-good check: %v", err)
	}
	if report.Generated != 6 || len(report.Measurements) != 2 {
		t.Fatalf("report = %+v, want 6 outputs and 2 skill measurements", report)
	}

	rolePath := filepath.Join(root, "plugins/agent-teams/roles/planner.md")
	mutatedRole := append(readFile(t, rolePath), []byte("hand edit\n")...)
	writeFile(t, rolePath, mutatedRole)
	before := snapshot(t, root)
	_, err = Check(config)
	if err == nil || !strings.Contains(err.Error(), "role.planner") || !strings.Contains(err.Error(), "plugins/agent-teams/roles/planner.md") {
		t.Fatalf("role drift error = %v", err)
	}
	if after := snapshot(t, root); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatal("read-only check changed the fixture tree")
	}

	skillPath := filepath.Join(root, "plugins/agent-teams-codex/skills/dispatch-dri/SKILL.md")
	writeFile(t, rolePath, readFile(t, filepath.Join("testdata", "clean", "plugins/agent-teams/roles/planner.md")))
	writeFile(t, skillPath, append(readFile(t, skillPath), []byte("hand edit\n")...))
	_, err = Check(config)
	if err == nil || !strings.Contains(err.Error(), "skill.dispatch-dri") || !strings.Contains(err.Error(), "plugins/agent-teams-codex/skills/dispatch-dri/SKILL.md") {
		t.Fatalf("skill drift error = %v", err)
	}

	if _, err := Write(config); err != nil {
		t.Fatalf("write after drift: %v", err)
	}
	if _, err := Check(config); err != nil {
		t.Fatalf("check after regeneration: %v", err)
	}
}

func TestOrderedInputsPreserveExactBytesAndFinalNewline(t *testing.T) {
	root := fixtureCopy(t)
	shared := filepath.Join(root, "promptsrc/agent-teams/roles/shared.md")
	writeFile(t, shared, []byte("Second fragment.\nFinal fragment.\n"))
	if _, err := Write(Config{Root: root}); err != nil {
		t.Fatal(err)
	}
	wantClaude := "---\ndescription: Fixture planner\nmodel: fixture\n---\nSecond fragment.\nFinal fragment.\n"
	if got := string(readFile(t, filepath.Join(root, "plugins/agent-teams/roles/planner.md"))); got != wantClaude {
		t.Fatalf("ordered Claude bytes:\n got %q\nwant %q", got, wantClaude)
	}
	wantCodex := "name = \"fixture-planner\"\ndeveloper_instructions = \"\"\"\nSecond fragment.\nFinal fragment.\n\"\"\"\n"
	if got := string(readFile(t, filepath.Join(root, "internal/verbs/codex_agents/agent-teams-planner.toml"))); got != wantCodex {
		t.Fatalf("ordered Codex bytes:\n got %q\nwant %q", got, wantCodex)
	}
}

func TestMalformedManifestAndInputs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
		want   string
	}{
		{
			name: "unknown manifest field",
			mutate: func(t *testing.T, root string) {
				path := rolesManifest(root)
				data := strings.Replace(string(readFile(t, path)), `"version": 1`, `"version": 1, "mystery": true`, 1)
				writeFile(t, path, []byte(data))
			},
			want: "unknown field",
		},
		{
			name: "missing canonical input",
			mutate: func(t *testing.T, root string) {
				path := rolesManifest(root)
				data := strings.Replace(string(readFile(t, path)), `roles/shared.md`, `roles/missing.md`, 1)
				writeFile(t, path, []byte(data))
			},
			want: "canonical input shared",
		},
		{
			name: "missing ordered part",
			mutate: func(t *testing.T, root string) {
				path := rolesManifest(root)
				data := strings.Replace(string(readFile(t, path)), `["claude-header", "shared"]`, `["claude-header", "not-declared"]`, 1)
				writeFile(t, path, []byte(data))
			},
			want: `references missing input "not-declared"`,
		},
		{
			name: "TOML part lacks explicit context",
			mutate: func(t *testing.T, root string) {
				path := rolesManifest(root)
				data := strings.Replace(string(readFile(t, path)), `, "encoding": "toml-basic-multiline"`, ``, 1)
				writeFile(t, path, []byte(data))
			},
			want: "needs explicit toml-template or multiline encoding",
		},
		{
			name: "generated output cannot be canonical input",
			mutate: func(t *testing.T, root string) {
				path := rolesManifest(root)
				data := strings.Replace(string(readFile(t, path)), `promptsrc/agent-teams/roles/shared.md`, `plugins/agent-teams/roles/planner.md`, 1)
				writeFile(t, path, []byte(data))
			},
			want: "is also a canonical input",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := fixtureCopy(t)
			tt.mutate(t, root)
			_, err := Check(Config{Root: root})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestCoverageMissingUnclassifiedDuplicateAndKindMismatch(t *testing.T) {
	t.Run("unclassified", func(t *testing.T) {
		root := fixtureCopy(t)
		writeFile(t, filepath.Join(root, "plugins/agent-teams/roles/reviewer.md"), []byte("unclassified\n"))
		_, err := Check(Config{Root: root})
		if err == nil || !strings.Contains(err.Error(), "unclassified role surface: plugins/agent-teams/roles/reviewer.md") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		root := fixtureCopy(t)
		path := filepath.Join(root, "internal/verbs/codex_agents/agent-teams-planner.toml")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		_, err := Check(Config{Root: root})
		if err == nil || !strings.Contains(err.Error(), "manifest output missing from discovery") || !strings.Contains(err.Error(), "role.planner") {
			t.Fatalf("error = %v", err)
		}
		if _, err := Write(Config{Root: root}); err != nil {
			t.Fatalf("write must be able to create a declared generated output: %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("generated output was not recreated: %v", err)
		}
	})

	t.Run("duplicate across split manifests", func(t *testing.T) {
		root := fixtureCopy(t)
		writeFile(t, filepath.Join(root, "promptsrc/agent-teams/manifests/duplicate.json"), readFile(t, rolesManifest(root)))
		_, err := Check(Config{Root: root})
		if err == nil || !strings.Contains(err.Error(), `duplicate logical id "role.planner"`) || !strings.Contains(err.Error(), "duplicate output path") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("kind mismatch", func(t *testing.T) {
		root := fixtureCopy(t)
		path := rolesManifest(root)
		data := strings.Replace(string(readFile(t, path)), `"kind": "role"`, `"kind": "reference"`, 1)
		writeFile(t, path, []byte(data))
		_, err := Check(Config{Root: root})
		if err == nil || !strings.Contains(err.Error(), "role surface, not reference") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestStrictAndTemporaryUnmigratedValidation(t *testing.T) {
	entry := Entry{
		ID:     "role.legacy",
		Kind:   KindRole,
		Status: StatusTemporarilyUnmigrated,
		Reason: "migration is dependency-gated",
		Outputs: []Output{
			{Path: "plugins/agent-teams/roles/legacy.md", Format: FormatMarkdown},
			{Path: "internal/verbs/codex_agents/legacy.toml", Format: FormatTOML},
		},
		source: "roles.json",
	}
	if err := validateEntries([]Entry{entry}, false); err == nil || !strings.Contains(err.Error(), "rejected in strict mode") {
		t.Fatalf("strict error = %v", err)
	}
	if err := validateEntries([]Entry{entry}, true); err != nil {
		t.Fatalf("allow-unmigrated: %v", err)
	}

	root := fixtureCopy(t)
	skillsPath := filepath.Join(root, "promptsrc/agent-teams/manifests/skills.json")
	entries, err := LoadManifests(root, []string{"promptsrc/agent-teams/manifests/skills.json"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range entries {
		if entries[i].ID == "reference.dispatch-execution" {
			entries[i].Status = StatusTemporarilyUnmigrated
			entries[i].Reason = "dependency-gated fixture migration"
			entries[i].Inputs = nil
			for j := range entries[i].Outputs {
				entries[i].Outputs[j].Parts = nil
			}
		}
	}
	encoded, err := json.MarshalIndent(Manifest{Version: ManifestVersion, Entries: entries}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, skillsPath, append(encoded, '\n'))
	if _, err := Check(Config{Root: root}); err == nil || !strings.Contains(err.Error(), "rejected in strict mode") {
		t.Fatalf("strict engine error = %v", err)
	}
	if _, err := Check(Config{Root: root, AllowUnmigrated: true}); err != nil {
		t.Fatalf("allow-unmigrated engine check: %v", err)
	}

	entry.Status = StatusRuntimeOnly
	entry.Outputs = entry.Outputs[:1]
	entry.Reason = ""
	if err := validateEntries([]Entry{entry}, true); err == nil || !strings.Contains(err.Error(), "needs a reason") {
		t.Fatalf("unexplained runtime-only error = %v", err)
	}
}

func TestTOMLBasicMultilineEncodingPreservesCanonicalText(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
	}{
		{name: "valid escape spelling", content: []byte(`literal \n and C:\new\file`)},
		{name: "invalid escape spelling", content: []byte(`literal \q remains literal`)},
		{name: "triple delimiter", content: []byte(`embedded """ delimiter and " quote`)},
		{name: "controls", content: []byte{'a', '\b', '\t', '\f', '\r', 0, 0x1f, 0x7f, 'z'}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := encodeTOMLBasicMultiline(tt.content)
			if err != nil {
				t.Fatal(err)
			}
			document := append([]byte("developer_instructions = \"\"\"\n"), encoded...)
			document = append(document, []byte("\"\"\"\n")...)
			var decoded struct {
				DeveloperInstructions string `toml:"developer_instructions"`
			}
			if err := toml.Unmarshal(document, &decoded); err != nil {
				t.Fatalf("parse encoded TOML: %v\n%s", err, document)
			}
			if got := []byte(decoded.DeveloperInstructions); !bytes.Equal(got, tt.content) {
				t.Fatalf("decoded content:\n got %q\nwant %q\nTOML:\n%s", got, tt.content, document)
			}
		})
	}
}

func TestTOMLBasicMultilineEncodingRejectsInvalidUTF8(t *testing.T) {
	if _, err := encodeTOMLBasicMultiline([]byte{0xff}); err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("error = %v", err)
	}
}

func TestRenderedTOMLPreservesCanonicalEscapeAndDelimiterText(t *testing.T) {
	root := fixtureCopy(t)
	path := filepath.Join(root, "promptsrc/agent-teams/roles/shared.md")
	want := []byte("literal \\n, invalid \\q, delimiter \"\"\", control \x00\n")
	writeFile(t, path, want)
	if _, err := Write(Config{Root: root}); err != nil {
		t.Fatalf("write: %v", err)
	}

	generated := readFile(t, filepath.Join(root, "internal/verbs/codex_agents/agent-teams-planner.toml"))
	var decoded struct {
		DeveloperInstructions string `toml:"developer_instructions"`
	}
	if err := toml.Unmarshal(generated, &decoded); err != nil {
		t.Fatalf("parse generated TOML: %v", err)
	}
	if got := []byte(decoded.DeveloperInstructions); !bytes.Equal(got, want) {
		t.Fatalf("decoded generated prompt:\n got %q\nwant %q", got, want)
	}
	if _, err := Check(Config{Root: root}); err != nil {
		t.Fatalf("check generated TOML: %v", err)
	}
}

func TestInvalidFinalTOMLBlocksCheckAndWrite(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(Config) (Report, error)
	}{
		{name: "check", run: Check},
		{name: "write", run: Write},
	} {
		t.Run(operation.name, func(t *testing.T) {
			root := fixtureCopy(t)
			path := filepath.Join(root, "promptsrc/agent-teams/roles/codex-header.toml")
			content := strings.Replace(string(readFile(t, path)), `name = "fixture-planner"`, `name = "invalid\q"`, 1)
			writeFile(t, path, []byte(content))
			before := snapshot(t, root)
			_, err := operation.run(Config{Root: root})
			if err == nil || !strings.Contains(err.Error(), "role.planner") || !strings.Contains(err.Error(), "rendered TOML") || !strings.Contains(err.Error(), "invalid escape") {
				t.Fatalf("error = %v", err)
			}
			if after := snapshot(t, root); fmt.Sprint(after) != fmt.Sprint(before) {
				t.Fatal("invalid rendered TOML changed the fixture tree")
			}
		})
	}
}

func TestNondeterministicInputIsRejected(t *testing.T) {
	root := fixtureCopy(t)
	target := filepath.Join(root, "promptsrc/agent-teams/roles/shared.md")
	reads := 0
	reader := func(path string) ([]byte, error) {
		if path == target {
			reads++
			if reads%2 == 0 {
				return []byte("changed between render passes\n"), nil
			}
		}
		return os.ReadFile(path)
	}
	_, err := Check(Config{Root: root, ReadFile: reader})
	if err == nil || !strings.Contains(err.Error(), "nondeterministic render") || !strings.Contains(err.Error(), "role.planner") {
		t.Fatalf("error = %v", err)
	}
}

func TestSkillMeasurementUsesFrontmatterStrippedUTF16(t *testing.T) {
	content := []byte("---\nname: fixture\n---\nBody 😀\n")
	budget := SkillBudget{BaseDirectory: "/installed/fixture", MinHeadroom: 1}
	measurement, err := measureSkill("skill.fixture", "skills/fixture/SKILL.md", content, budget)
	if err != nil {
		t.Fatal(err)
	}
	wantUnits := len(utf16.Encode([]rune("Base directory for this skill: /installed/fixture\n\nBody 😀\n")))
	if measurement.RenderedUTF16 != wantUnits || measurement.Headroom != SkillUTF16Limit-wantUnits {
		t.Fatalf("measurement = %+v, want units %d", measurement, wantUnits)
	}
	if _, err := measureSkill("skill.fixture", "skills/fixture/SKILL.md", []byte("no frontmatter\n"), budget); err == nil || !strings.Contains(err.Error(), "frontmatter") {
		t.Fatalf("malformed SKILL error = %v", err)
	}
	budget.MinHeadroom = SkillUTF16Limit
	if _, err := measureSkill("skill.fixture", "skills/fixture/SKILL.md", content, budget); err == nil || !strings.Contains(err.Error(), "requires at least") {
		t.Fatalf("budget error = %v", err)
	}
}

func fixtureCopy(t *testing.T) string {
	t.Helper()
	source := filepath.Join("testdata", "clean")
	destination := t.TempDir()
	err := filepath.WalkDir(source, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if item.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func rolesManifest(root string) string {
	return filepath.Join(root, "promptsrc/agent-teams/manifests/roles.json")
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func snapshot(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	result := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil || item.IsDir() {
			return walkErr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		result[rel] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
