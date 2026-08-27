package promptsync

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Config struct {
	Root             string
	ManifestPatterns []string
	AllowUnmigrated  bool
	ReadFile         func(string) ([]byte, error)
}

type Report struct {
	Generated    int
	Measurements []SkillMeasurement
}

type renderedOutput struct {
	id     string
	output Output
	bytes  []byte
}

func Check(config Config) (Report, error) {
	root, err := resolveRoot(config.Root)
	if err != nil {
		return Report{}, err
	}
	config.Root = root
	entries, first, report, err := prepare(config, false)
	if err != nil {
		return report, err
	}
	second, _, err := prepareEntries(config, entries, false)
	if err != nil {
		return report, err
	}
	if err := compareRenderPasses(first, second); err != nil {
		return report, err
	}
	reader := config.ReadFile
	if reader == nil {
		reader = os.ReadFile
	}
	var problems []string
	for _, item := range first {
		actualPath := filepath.Join(config.Root, filepath.FromSlash(item.output.Path))
		actual, readErr := reader(actualPath)
		if readErr != nil {
			problems = append(problems, fmt.Sprintf("%s: expected rendered output at %s; actual path %s is unreadable: %v", item.id, item.output.Path, item.output.Path, readErr))
			continue
		}
		if !bytes.Equal(item.bytes, actual) {
			offset := firstDifferentByte(item.bytes, actual)
			problems = append(problems, fmt.Sprintf("%s: drift: expected manifest output %s from ordered inputs; actual file %s differs at byte %d (expected %d bytes, actual %d); run prompt-sync write", item.id, item.output.Path, item.output.Path, offset, len(item.bytes), len(actual)))
		}
	}
	if err := combineProblems(problems); err != nil {
		return report, err
	}
	return report, nil
}

func firstDifferentByte(expected, actual []byte) int {
	limit := len(expected)
	if len(actual) < limit {
		limit = len(actual)
	}
	for i := 0; i < limit; i++ {
		if expected[i] != actual[i] {
			return i
		}
	}
	return limit
}

func Write(config Config) (Report, error) {
	root, err := resolveRoot(config.Root)
	if err != nil {
		return Report{}, err
	}
	config.Root = root
	entries, first, report, err := prepare(config, true)
	if err != nil {
		return report, err
	}
	second, _, err := prepareEntries(config, entries, true)
	if err != nil {
		return report, err
	}
	if err := compareRenderPasses(first, second); err != nil {
		return report, err
	}
	for _, item := range first {
		path := filepath.Join(config.Root, filepath.FromSlash(item.output.Path))
		if err := writeAtomic(path, item.bytes); err != nil {
			return report, fmt.Errorf("%s: write generated output %s: %w", item.id, item.output.Path, err)
		}
	}
	return report, nil
}

func prepare(config Config, writing bool) ([]Entry, []renderedOutput, Report, error) {
	entries, err := LoadManifests(config.Root, config.ManifestPatterns)
	if err != nil {
		return nil, nil, Report{}, err
	}
	outputs, report, err := prepareEntries(config, entries, writing)
	return entries, outputs, report, err
}

func prepareEntries(config Config, entries []Entry, writing bool) ([]renderedOutput, Report, error) {
	if err := validateEntries(entries, config.AllowUnmigrated); err != nil {
		return nil, Report{}, err
	}
	if err := validateCoverage(config.Root, entries, writing); err != nil {
		return nil, Report{}, err
	}
	outputs, report, err := renderAll(config, entries)
	return outputs, report, err
}

func resolveRoot(root string) (string, error) {
	if root == "" {
		root = "."
	}
	return filepath.Abs(root)
}

func renderAll(config Config, entries []Entry) ([]renderedOutput, Report, error) {
	reader := config.ReadFile
	if reader == nil {
		reader = os.ReadFile
	}
	var rendered []renderedOutput
	var report Report
	var problems []string
	for _, entry := range entries {
		if entry.Status != StatusPaired {
			continue
		}
		contents := map[string][]byte{}
		encodings := map[string]Encoding{}
		for _, input := range entry.Inputs {
			path := filepath.Join(config.Root, filepath.FromSlash(input.Path))
			content, err := reader(path)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: canonical input %s (%s) is unreadable: %v", entry.ID, input.ID, input.Path, err))
				continue
			}
			contents[input.ID] = content
			encodings[input.ID] = input.Encoding
		}
		for _, output := range entry.Outputs {
			var buffer bytes.Buffer
			failed := false
			for _, part := range output.Parts {
				content, ok := contents[part]
				if !ok {
					failed = true
					continue
				}
				encoding := encodings[part]
				if output.Format == FormatTOML && encoding == EncodingTOMLBasicMultiline && bytes.Contains(content, []byte(`"""`)) {
					problems = append(problems, fmt.Sprintf("%s: unsafe TOML: input %q for %s contains basic multiline delimiter \\\"\\\"\\\"", entry.ID, part, output.Path))
					failed = true
					continue
				}
				if output.Format == FormatTOML && encoding == EncodingTOMLLiteralMultiline && bytes.Contains(content, []byte("'''")) {
					problems = append(problems, fmt.Sprintf("%s: unsafe TOML: input %q for %s contains literal multiline delimiter '''", entry.ID, part, output.Path))
					failed = true
					continue
				}
				buffer.Write(content)
			}
			if failed {
				continue
			}
			item := renderedOutput{id: entry.ID, output: output, bytes: buffer.Bytes()}
			rendered = append(rendered, item)
			if output.SkillBudget != nil {
				measurement, err := measureSkill(entry.ID, output.Path, item.bytes, *output.SkillBudget)
				report.Measurements = append(report.Measurements, measurement)
				if err != nil {
					problems = append(problems, err.Error())
				}
			}
		}
	}
	sort.Slice(rendered, func(i, j int) bool { return rendered[i].output.Path < rendered[j].output.Path })
	sort.Slice(report.Measurements, func(i, j int) bool { return report.Measurements[i].Path < report.Measurements[j].Path })
	report.Generated = len(rendered)
	return rendered, report, combineProblems(problems)
}

func compareRenderPasses(first, second []renderedOutput) error {
	if len(first) != len(second) {
		return fmt.Errorf("prompt sync validation failed: nondeterministic render produced %d outputs, then %d", len(first), len(second))
	}
	for i := range first {
		if first[i].id != second[i].id || first[i].output.Path != second[i].output.Path || !bytes.Equal(first[i].bytes, second[i].bytes) {
			return fmt.Errorf("prompt sync validation failed: nondeterministic render for %s at %s", first[i].id, first[i].output.Path)
		}
	}
	return nil
}

func writeAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".prompt-sync-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	cleanup := func() { _ = os.Remove(tempName) }
	defer cleanup()
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

func FormatReport(report Report) string {
	var lines []string
	for _, measurement := range report.Measurements {
		lines = append(lines, fmt.Sprintf("skill %s %s: rendered_utf16=%d headroom=%d limit=%d minimum_headroom=%d", measurement.LogicalID, measurement.Path, measurement.RenderedUTF16, measurement.Headroom, SkillUTF16Limit, measurement.MinimumHeadroom))
	}
	lines = append(lines, fmt.Sprintf("generated_outputs=%d", report.Generated))
	return strings.Join(lines, "\n") + "\n"
}
