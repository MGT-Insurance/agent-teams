package repoconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func writeMarker(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", FileName, err)
	}
}

func TestEnabled_MissingFile(t *testing.T) {
	dir := t.TempDir()
	if Enabled(dir) {
		t.Error("Enabled() = true for a repo with no .agent-teams file, want false")
	}
}

func TestEnabled_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "")
	if !Enabled(dir) {
		t.Error("Enabled() = false for an empty .agent-teams file, want true")
	}
}

func TestEnabled_DisabledFalse(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "disabled: false\n")
	if !Enabled(dir) {
		t.Error("Enabled() = false for 'disabled: false', want true")
	}
}

func TestEnabled_DisabledTrue(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "disabled: true\n")
	if Enabled(dir) {
		t.Error("Enabled() = true for 'disabled: true', want false — same effect as a missing file")
	}
}

func TestEnabled_DisabledTrueNoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "disabled: true")
	if Enabled(dir) {
		t.Error("Enabled() = true for 'disabled: true' with no trailing newline, want false")
	}
}

func TestEnabled_UnrelatedContentIgnored(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "# a comment\nsome: other\n")
	if !Enabled(dir) {
		t.Error("Enabled() = false for unrelated content, want true")
	}
}

func TestEnabled_LeadingWhitespaceKeyIgnored(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "  disabled: true\n")
	if !Enabled(dir) {
		t.Error("Enabled() = false for indented 'disabled: true' line, want true (not a canonical key line)")
	}
}

func TestEnabled_ValueCaseSensitive(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "disabled: TRUE\n")
	if !Enabled(dir) {
		t.Error("Enabled() = false for 'disabled: TRUE' (wrong case), want true — only exact \"true\" disables")
	}
}

func TestEnabled_ExtraWhitespaceAroundValueTrimmed(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "disabled:   true  \n")
	if Enabled(dir) {
		t.Error("Enabled() = true for 'disabled:   true  ', want false — value should be trimmed before comparison")
	}
}
