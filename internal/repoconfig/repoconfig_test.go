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

// ---- Enable (write seam) ----------------------------------------------------

func TestEnable_MissingFileCreatesHeader(t *testing.T) {
	dir := t.TempDir()

	result, err := Enable(dir)
	if err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if result != ResultCreated {
		t.Errorf("Enable() result = %v, want ResultCreated", result)
	}

	got, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != Header {
		t.Errorf("written content = %q, want the canonical Header %q", got, Header)
	}
	if !Enabled(dir) {
		t.Error("Enabled() = false right after Enable() created the marker, want true")
	}
}

func TestEnable_TableDriven(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantResult  EnableResult
		wantContent string
	}{
		{
			name:        "disabled line with unrelated line after it survives",
			content:     "disabled: true\nsome custom note\n",
			wantResult:  ResultUndisabled,
			wantContent: "some custom note\n",
		},
		{
			name:        "disabled line with unrelated line before it survives",
			content:     "some custom note\ndisabled: true\n",
			wantResult:  ResultUndisabled,
			wantContent: "some custom note\n",
		},
		{
			name:        "disabled-only file becomes empty",
			content:     "disabled: true\n",
			wantResult:  ResultUndisabled,
			wantContent: "",
		},
		{
			name:        "already enabled empty file",
			content:     "",
			wantResult:  ResultAlreadyEnabled,
			wantContent: "",
		},
		{
			name:        "already enabled disabled:false left alone",
			content:     "disabled: false\n",
			wantResult:  ResultAlreadyEnabled,
			wantContent: "disabled: false\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeMarker(t, dir, tt.content)

			result, err := Enable(dir)
			if err != nil {
				t.Fatalf("Enable() error = %v", err)
			}
			if result != tt.wantResult {
				t.Errorf("Enable() result = %v, want %v", result, tt.wantResult)
			}

			got, err := os.ReadFile(filepath.Join(dir, FileName))
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			if string(got) != tt.wantContent {
				t.Errorf("resulting content = %q, want %q", got, tt.wantContent)
			}
			if !Enabled(dir) {
				t.Error("Enabled() = false after Enable(), want true")
			}
		})
	}
}

func TestEnable_AlreadyEnabledDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "disabled: false\nsome note\n")
	path := filepath.Join(dir, FileName)

	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	result, err := Enable(dir)
	if err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if result != ResultAlreadyEnabled {
		t.Errorf("Enable() result = %v, want ResultAlreadyEnabled", result)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("mtime changed (before=%v after=%v): Enable() wrote despite already being enabled", before.ModTime(), after.ModTime())
	}
}
