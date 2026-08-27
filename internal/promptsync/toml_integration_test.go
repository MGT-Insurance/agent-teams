package promptsync

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestCommittedCodexRolesRoundTripCanonicalInstructions(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	entries, err := LoadManifests(root, nil)
	if err != nil {
		t.Fatal(err)
	}

	installedRoles := 0
	for _, entry := range entries {
		if entry.Kind != KindRole || entry.Status != StatusPaired {
			continue
		}
		inputs := make(map[string]Input, len(entry.Inputs))
		for _, input := range entry.Inputs {
			inputs[input.ID] = input
		}
		for _, output := range entry.Outputs {
			if output.Format != FormatTOML {
				continue
			}
			installedRoles++

			var canonical bytes.Buffer
			for _, part := range output.InstructionParts {
				input := inputs[part]
				content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(input.Path)))
				if err != nil {
					t.Fatalf("%s: read %s: %v", entry.ID, input.Path, err)
				}
				canonical.Write(content)
			}
			want := canonical.Bytes()

			generated, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(output.Path)))
			if err != nil {
				t.Fatalf("%s: read %s: %v", entry.ID, output.Path, err)
			}
			var decoded struct {
				DeveloperInstructions string `toml:"developer_instructions"`
			}
			if err := toml.Unmarshal(generated, &decoded); err != nil {
				t.Fatalf("%s: parse %s: %v", entry.ID, output.Path, err)
			}
			if !bytes.Equal([]byte(decoded.DeveloperInstructions), want) {
				t.Fatalf("%s: decoded developer instructions differ from canonical bytes", entry.ID)
			}
		}
	}
	if installedRoles != 5 {
		t.Fatalf("parsed %d committed Codex roles, want 5", installedRoles)
	}
}

func TestInvalidUTF8CanonicalInputBlocksCheckAndWriteReadOnly(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(Config) (Report, error)
	}{
		{name: "check", run: Check},
		{name: "write", run: Write},
	} {
		t.Run(operation.name, func(t *testing.T) {
			root := fixtureCopy(t)
			path := filepath.Join(root, "promptsrc/agent-teams/roles/shared.md")
			writeFile(t, path, []byte{'a', 0xff, 'z'})
			before := snapshot(t, root)

			_, err := operation.run(Config{Root: root})
			if err == nil || !strings.Contains(err.Error(), "role.planner") || !strings.Contains(err.Error(), "valid UTF-8") {
				t.Fatalf("error = %v", err)
			}
			if after := snapshot(t, root); !equalSnapshots(after, before) {
				t.Fatal("invalid canonical UTF-8 changed the fixture tree")
			}
		})
	}
}

func equalSnapshots(left, right map[string][32]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for path, sum := range left {
		if right[path] != sum {
			return false
		}
	}
	return true
}
