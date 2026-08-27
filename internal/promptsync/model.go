// Package promptsync renders and validates the checked-in prompt artifacts
// shared by the Claude and Codex agent-teams plugins.
package promptsync

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const ManifestVersion = 1

type Kind string

const (
	KindRole      Kind = "role"
	KindSkill     Kind = "skill"
	KindReference Kind = "reference"
)

type Status string

const (
	StatusPaired                Status = "paired"
	StatusRuntimeOnly           Status = "runtime-only"
	StatusTemporarilyUnmigrated Status = "temporarily-unmigrated"
)

type Encoding string

const (
	EncodingRaw                  Encoding = "raw"
	EncodingTOMLTemplate         Encoding = "toml-template"
	EncodingTOMLBasicMultiline   Encoding = "toml-basic-multiline"
	EncodingTOMLLiteralMultiline Encoding = "toml-literal-multiline"
)

type Format string

const (
	FormatMarkdown Format = "markdown"
	FormatTOML     Format = "toml"
)

// Manifest is deliberately self-contained. Multiple manifest files can be
// loaded together, allowing role and skill migrations to own disjoint files.
type Manifest struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

type Entry struct {
	ID      string   `json:"id"`
	Kind    Kind     `json:"kind"`
	Status  Status   `json:"status"`
	Reason  string   `json:"reason,omitempty"`
	Inputs  []Input  `json:"inputs,omitempty"`
	Outputs []Output `json:"outputs"`
	source  string
}

type Input struct {
	ID       string   `json:"id"`
	Path     string   `json:"path"`
	Encoding Encoding `json:"encoding,omitempty"`
}

type Output struct {
	Path        string       `json:"path"`
	Format      Format       `json:"format"`
	Parts       []string     `json:"parts,omitempty"`
	SkillBudget *SkillBudget `json:"skill_budget,omitempty"`
}

// SkillBudget records the human-selected safety policy for one generated
// SKILL.md. BaseDirectory is the worst supported installed directory, not the
// current checkout path, so results remain deterministic across machines.
type SkillBudget struct {
	BaseDirectory string `json:"base_directory"`
	MinHeadroom   int    `json:"min_headroom"`
}

var logicalIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func LoadManifests(root string, patterns []string) ([]Entry, error) {
	if len(patterns) == 0 {
		patterns = []string{"promptsrc/agent-teams/manifests/*.json"}
	}
	var files []string
	seenFiles := map[string]bool{}
	for _, pattern := range patterns {
		if filepath.IsAbs(pattern) {
			return nil, fmt.Errorf("manifest pattern must be relative to the repository root: %q", pattern)
		}
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			return nil, fmt.Errorf("manifest pattern %q: %w", pattern, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("manifest pattern %q matched no files", pattern)
		}
		for _, match := range matches {
			if !seenFiles[match] {
				seenFiles[match] = true
				files = append(files, match)
			}
		}
	}
	sort.Strings(files)

	var entries []Entry
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read manifest %s: %w", displayPath(root, path), err)
		}
		dec := json.NewDecoder(strings.NewReader(string(data)))
		dec.DisallowUnknownFields()
		var manifest Manifest
		if err := dec.Decode(&manifest); err != nil {
			return nil, fmt.Errorf("parse manifest %s: %w", displayPath(root, path), err)
		}
		var trailing any
		if err := dec.Decode(&trailing); err != io.EOF {
			if err == nil {
				return nil, fmt.Errorf("parse manifest %s: trailing JSON value", displayPath(root, path))
			}
			return nil, fmt.Errorf("parse manifest %s: trailing data: %w", displayPath(root, path), err)
		}
		if manifest.Version != ManifestVersion {
			return nil, fmt.Errorf("manifest %s: version %d is unsupported (want %d)", displayPath(root, path), manifest.Version, ManifestVersion)
		}
		for i := range manifest.Entries {
			manifest.Entries[i].source = displayPath(root, path)
		}
		entries = append(entries, manifest.Entries...)
	}
	return entries, nil
}

func validateEntries(entries []Entry, allowUnmigrated bool) error {
	var problems []string
	ids := map[string]string{}
	outputs := map[string]string{}
	canonicalPaths := map[string][]string{}
	for _, entry := range entries {
		where := entry.source
		if previous, ok := ids[entry.ID]; ok {
			problems = append(problems, fmt.Sprintf("duplicate logical id %q in %s and %s", entry.ID, previous, where))
		} else {
			ids[entry.ID] = where
		}
		if !logicalIDPattern.MatchString(entry.ID) {
			problems = append(problems, fmt.Sprintf("%s: invalid stable logical id %q", where, entry.ID))
		}
		switch entry.Kind {
		case KindRole, KindSkill, KindReference:
		default:
			problems = append(problems, fmt.Sprintf("%s (%s): unknown kind %q", entry.ID, where, entry.Kind))
		}

		switch entry.Status {
		case StatusPaired:
			if len(entry.Outputs) != 2 {
				problems = append(problems, fmt.Sprintf("%s (%s): paired entry needs exactly two counterpart outputs", entry.ID, where))
			}
			if len(entry.Inputs) == 0 {
				problems = append(problems, fmt.Sprintf("%s (%s): paired entry needs canonical inputs", entry.ID, where))
			}
		case StatusRuntimeOnly:
			if len(entry.Outputs) != 1 {
				problems = append(problems, fmt.Sprintf("%s (%s): runtime-only entry must classify exactly one output", entry.ID, where))
			}
			if strings.TrimSpace(entry.Reason) == "" {
				problems = append(problems, fmt.Sprintf("%s (%s): runtime-only entry needs a reason", entry.ID, where))
			}
			if len(entry.Inputs) != 0 {
				problems = append(problems, fmt.Sprintf("%s (%s): runtime-only entry cannot own generated inputs", entry.ID, where))
			}
		case StatusTemporarilyUnmigrated:
			if !allowUnmigrated {
				problems = append(problems, fmt.Sprintf("%s (%s): temporarily unmigrated entry rejected in strict mode; use --allow-unmigrated only during loop closure", entry.ID, where))
			}
			if len(entry.Outputs) == 0 || strings.TrimSpace(entry.Reason) == "" {
				problems = append(problems, fmt.Sprintf("%s (%s): temporarily unmigrated entry needs exact outputs and a reason", entry.ID, where))
			}
			if len(entry.Inputs) != 0 {
				problems = append(problems, fmt.Sprintf("%s (%s): temporarily unmigrated entry cannot own generated inputs", entry.ID, where))
			}
		default:
			problems = append(problems, fmt.Sprintf("%s (%s): unknown counterpart status %q", entry.ID, where, entry.Status))
		}

		inputIDs := map[string]bool{}
		for _, input := range entry.Inputs {
			if input.ID == "" || inputIDs[input.ID] {
				problems = append(problems, fmt.Sprintf("%s (%s): duplicate or empty input id %q", entry.ID, where, input.ID))
			}
			inputIDs[input.ID] = true
			canonicalPaths[input.Path] = append(canonicalPaths[input.Path], entry.ID)
			if err := validateRepoPath(input.Path); err != nil {
				problems = append(problems, fmt.Sprintf("%s (%s): input %q: %v", entry.ID, where, input.ID, err))
			}
			switch input.Encoding {
			case "", EncodingRaw, EncodingTOMLTemplate, EncodingTOMLBasicMultiline, EncodingTOMLLiteralMultiline:
			default:
				problems = append(problems, fmt.Sprintf("%s (%s): input %q has unknown encoding %q", entry.ID, where, input.ID, input.Encoding))
			}
		}
		usedInputs := map[string]bool{}
		pairedRuntimes := map[string]bool{}
		for _, output := range entry.Outputs {
			if err := validateRepoPath(output.Path); err != nil {
				problems = append(problems, fmt.Sprintf("%s (%s): output: %v", entry.ID, where, err))
			}
			if previous, ok := outputs[output.Path]; ok {
				problems = append(problems, fmt.Sprintf("duplicate output path %q owned by %s and %s", output.Path, previous, entry.ID))
			} else {
				outputs[output.Path] = entry.ID
			}
			if entry.Status == StatusPaired {
				if len(output.Parts) == 0 {
					problems = append(problems, fmt.Sprintf("%s (%s): generated output %s has no ordered parts", entry.ID, where, output.Path))
				}
				for _, part := range output.Parts {
					usedInputs[part] = true
					if !inputIDs[part] {
						problems = append(problems, fmt.Sprintf("%s (%s): output %s references missing input %q", entry.ID, where, output.Path, part))
					}
				}
			} else if len(output.Parts) != 0 || output.SkillBudget != nil {
				problems = append(problems, fmt.Sprintf("%s (%s): classified-only output %s cannot have parts or a skill budget", entry.ID, where, output.Path))
			}
			switch output.Format {
			case FormatMarkdown, FormatTOML:
			default:
				problems = append(problems, fmt.Sprintf("%s (%s): output %s has unknown format %q", entry.ID, where, output.Path, output.Format))
			}
			discoveredKind, runtime, expectedFormat, recognized := classifySurfacePath(output.Path)
			if !recognized {
				problems = append(problems, fmt.Sprintf("%s (%s): output %s is not a role, SKILL.md, or skill-reference surface", entry.ID, where, output.Path))
			} else {
				pairedRuntimes[runtime] = true
				if discoveredKind != entry.Kind {
					problems = append(problems, fmt.Sprintf("%s (%s): output %s is a %s surface, not %s", entry.ID, where, output.Path, discoveredKind, entry.Kind))
				}
				if expectedFormat != output.Format {
					problems = append(problems, fmt.Sprintf("%s (%s): output %s must use format %s", entry.ID, where, output.Path, expectedFormat))
				}
			}
			if entry.Status == StatusPaired && output.Format == FormatTOML {
				for _, part := range output.Parts {
					encoding := inputEncoding(entry.Inputs, part)
					if encoding != EncodingTOMLTemplate && encoding != EncodingTOMLBasicMultiline && encoding != EncodingTOMLLiteralMultiline {
						problems = append(problems, fmt.Sprintf("%s (%s): TOML output %s input %q needs explicit toml-template or multiline encoding", entry.ID, where, output.Path, part))
					}
				}
			}
			isSkillFile := entry.Kind == KindSkill && filepath.Base(output.Path) == "SKILL.md"
			if entry.Status == StatusPaired && isSkillFile && output.SkillBudget == nil {
				problems = append(problems, fmt.Sprintf("%s (%s): generated SKILL.md %s needs skill_budget", entry.ID, where, output.Path))
			}
			if output.SkillBudget != nil && (!isSkillFile || output.SkillBudget.BaseDirectory == "" || output.SkillBudget.MinHeadroom < 0) {
				problems = append(problems, fmt.Sprintf("%s (%s): output %s has an invalid skill_budget", entry.ID, where, output.Path))
			}
		}
		if entry.Status == StatusPaired && (!pairedRuntimes["claude"] || !pairedRuntimes["codex"]) {
			problems = append(problems, fmt.Sprintf("%s (%s): paired entry must have one Claude and one Codex counterpart", entry.ID, where))
		}
		for _, input := range entry.Inputs {
			if !usedInputs[input.ID] {
				problems = append(problems, fmt.Sprintf("%s (%s): canonical input %q is not used by any output", entry.ID, where, input.ID))
			}
		}
	}
	for path, owners := range canonicalPaths {
		if outputOwner, ok := outputs[path]; ok {
			problems = append(problems, fmt.Sprintf("generated output %q owned by %s is also a canonical input of %s", path, outputOwner, strings.Join(owners, ", ")))
		}
	}
	return combineProblems(problems)
}

func inputEncoding(inputs []Input, wanted string) Encoding {
	for _, input := range inputs {
		if input.ID == wanted {
			return input.Encoding
		}
	}
	return ""
}

func validateRepoPath(path string) error {
	if path == "" {
		return fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(path) || path != filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) || path == "." || strings.HasPrefix(path, "../") {
		return fmt.Errorf("path %q must be a clean repository-relative slash path", path)
	}
	return nil
}

func displayPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}

func combineProblems(problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("prompt sync validation failed:\n- %s", strings.Join(problems, "\n- "))
}
