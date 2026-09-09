package workspaceconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeDefaultSelection(t *testing.T) {
	tests := []struct {
		name    string
		content *string
		class   RuntimeClass
		want    string
		wantSet bool
	}{
		{name: "missing file", class: WorkRuntime},
		{name: "empty document", content: stringPointer(""), class: WorkRuntime},
		{name: "missing selected key", content: stringPointer("review_runtime = \"claude\"\n"), class: WorkRuntime},
		{name: "work default", content: stringPointer("work_runtime = \"codex\"\nreview_runtime = \"claude\"\n"), class: WorkRuntime, want: "codex", wantSet: true},
		{name: "review default", content: stringPointer("work_runtime = \"codex\"\nreview_runtime = \"claude\"\n"), class: ReviewRuntime, want: "claude", wantSet: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if tt.content != nil {
				writeConfig(t, home, *tt.content)
			}
			got, set, err := RuntimeDefault(home, tt.class)
			if err != nil {
				t.Fatalf("RuntimeDefault() error = %v", err)
			}
			if got != tt.want || set != tt.wantSet {
				t.Fatalf("RuntimeDefault() = %q, %v; want %q, %v", got, set, tt.want, tt.wantSet)
			}
		})
	}
}

func TestAutoCompactWindowSelection(t *testing.T) {
	tests := []struct {
		name    string
		content *string
		want    int64
		wantSet bool
	}{
		{name: "missing file"},
		{name: "empty document", content: stringPointer("")},
		{name: "missing key", content: stringPointer("work_runtime = \"codex\"\n")},
		{name: "configured", content: stringPointer("auto_compact_window = 300000\n"), want: 300000, wantSet: true},
		{name: "coexists with runtime keys", content: stringPointer("work_runtime = \"codex\"\nreview_runtime = \"claude\"\nauto_compact_window = 300000\n"), want: 300000, wantSet: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if tt.content != nil {
				writeConfig(t, home, *tt.content)
			}
			got, set, err := AutoCompactWindow(home)
			if err != nil {
				t.Fatalf("AutoCompactWindow() error = %v", err)
			}
			if got != tt.want || set != tt.wantSet {
				t.Fatalf("AutoCompactWindow() = %d, %v; want %d, %v", got, set, tt.want, tt.wantSet)
			}
		})
	}
}

func TestUseAdvisorsSelection(t *testing.T) {
	tests := []struct {
		name    string
		content *string
		want    bool
		wantSet bool
	}{
		{name: "missing file", want: false, wantSet: false},
		{name: "empty document", content: stringPointer(""), want: false, wantSet: false},
		{name: "missing key", content: stringPointer("work_runtime = \"codex\"\n"), want: false, wantSet: false},
		{name: "configured true", content: stringPointer("use_advisors = true\n"), want: true, wantSet: true},
		{name: "configured false", content: stringPointer("use_advisors = false\n"), want: false, wantSet: true},
		{name: "coexists with other keys", content: stringPointer("work_runtime = \"codex\"\nauto_compact_window = 300000\nuse_advisors = true\n"), want: true, wantSet: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if tt.content != nil {
				writeConfig(t, home, *tt.content)
			}
			got, set, err := UseAdvisors(home)
			if err != nil {
				t.Fatalf("UseAdvisors() error = %v", err)
			}
			if got != tt.want || set != tt.wantSet {
				t.Fatalf("UseAdvisors() = %v, %v; want %v, %v", got, set, tt.want, tt.wantSet)
			}
		})
	}
}

func TestUseAdvisorsRejectsInvalidStrictDocument(t *testing.T) {
	tests := []struct {
		name, content, wantContext string
	}{
		{name: "malformed TOML", content: "use_advisors = [", wantContext: "use_advisors"},
		{name: "unknown key", content: "advisors = true\n", wantContext: "advisors"},
		{name: "table", content: "[use_advisors]\nvalue = true\n", wantContext: "use_advisors"},
		{name: "wrong type", content: "use_advisors = \"true\"\n", wantContext: "use_advisors"},
		{name: "invalid unrelated key", content: "use_advisors = true\nwork_runtime = \"other\"\n", wantContext: "work_runtime"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			path := writeConfig(t, home, tt.content)
			_, _, err := UseAdvisors(home)
			if err == nil {
				t.Fatal("UseAdvisors() must reject invalid config")
			}
			if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), tt.wantContext) {
				t.Fatalf("error %q must contain path %q and context %q", err, path, tt.wantContext)
			}
		})
	}
}

func TestClaudeDriModelSelection(t *testing.T) {
	tests := []struct {
		name    string
		content *string
		want    string
		wantSet bool
	}{
		{name: "missing file", want: claudeDriModelDefault, wantSet: false},
		{name: "empty document", content: stringPointer(""), want: claudeDriModelDefault, wantSet: false},
		{name: "missing key", content: stringPointer("work_runtime = \"codex\"\n"), want: claudeDriModelDefault, wantSet: false},
		{name: "configured", content: stringPointer("claude_dri_model = \"claude-sonnet-4-5\"\n"), want: "claude-sonnet-4-5", wantSet: true},
		{name: "coexists with other keys", content: stringPointer("work_runtime = \"codex\"\nuse_advisors = true\nclaude_dri_model = \"claude-sonnet-4-5\"\n"), want: "claude-sonnet-4-5", wantSet: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if tt.content != nil {
				writeConfig(t, home, *tt.content)
			}
			got, set, err := ClaudeDriModel(home)
			if err != nil {
				t.Fatalf("ClaudeDriModel() error = %v", err)
			}
			if got != tt.want || set != tt.wantSet {
				t.Fatalf("ClaudeDriModel() = %q, %v; want %q, %v", got, set, tt.want, tt.wantSet)
			}
		})
	}
}

func TestClaudeDriModelRejectsInvalidStrictDocument(t *testing.T) {
	tests := []struct {
		name, content, wantContext string
	}{
		{name: "malformed TOML", content: "claude_dri_model = [", wantContext: "claude_dri_model"},
		{name: "unknown key", content: "dri_model = \"claude-opus-4-8\"\n", wantContext: "dri_model"},
		{name: "table", content: "[claude_dri_model]\nvalue = \"claude-opus-4-8\"\n", wantContext: "claude_dri_model"},
		{name: "empty value", content: "claude_dri_model = \"\"\n", wantContext: "claude_dri_model"},
		{name: "wrong type", content: "claude_dri_model = 1\n", wantContext: "claude_dri_model"},
		{name: "invalid unrelated key", content: "claude_dri_model = \"claude-opus-4-8\"\nwork_runtime = \"other\"\n", wantContext: "work_runtime"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			path := writeConfig(t, home, tt.content)
			_, _, err := ClaudeDriModel(home)
			if err == nil {
				t.Fatal("ClaudeDriModel() must reject invalid config")
			}
			if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), tt.wantContext) {
				t.Fatalf("error %q must contain path %q and context %q", err, path, tt.wantContext)
			}
		})
	}
}

func TestRuntimeDefaultRejectsInvalidStrictDocument(t *testing.T) {
	tests := []struct {
		name, content, wantContext string
	}{
		{name: "malformed TOML", content: "work_runtime = [", wantContext: "invalid strict TOML"},
		{name: "unknown key", content: "runtime = \"codex\"\n", wantContext: "runtime"},
		{name: "table", content: "[defaults]\nwork_runtime = \"codex\"\n", wantContext: "defaults"},
		{name: "empty selected value", content: "work_runtime = \"\"\n", wantContext: "work_runtime"},
		{name: "invalid selected value", content: "work_runtime = \"other\"\n", wantContext: "work_runtime"},
		{name: "uppercase value", content: "work_runtime = \"Codex\"\n", wantContext: "work_runtime"},
		{name: "padded value", content: "work_runtime = \" codex \"\n", wantContext: "work_runtime"},
		{name: "invalid unselected value", content: "work_runtime = \"codex\"\nreview_runtime = \"other\"\n", wantContext: "review_runtime"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			path := writeConfig(t, home, tt.content)
			_, _, err := RuntimeDefault(home, WorkRuntime)
			if err == nil {
				t.Fatal("RuntimeDefault() must reject invalid config")
			}
			if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), tt.wantContext) {
				t.Fatalf("error %q must contain path %q and context %q", err, path, tt.wantContext)
			}
		})
	}
}

func TestAutoCompactWindowRejectsInvalidStrictDocument(t *testing.T) {
	tests := []struct {
		name, content, wantContext string
	}{
		{name: "malformed TOML", content: "auto_compact_window = [", wantContext: "auto_compact_window"},
		{name: "unknown key", content: "compact_window = 300000\n", wantContext: "compact_window"},
		{name: "table", content: "[auto_compact_window]\nvalue = 300000\n", wantContext: "auto_compact_window"},
		{name: "wrong type", content: "auto_compact_window = \"300000\"\n", wantContext: "auto_compact_window"},
		{name: "zero", content: "auto_compact_window = 0\n", wantContext: "auto_compact_window"},
		{name: "negative", content: "auto_compact_window = -1\n", wantContext: "auto_compact_window"},
		{name: "overflow", content: "auto_compact_window = 9223372036854775808\n", wantContext: "auto_compact_window"},
		{name: "invalid runtime key", content: "work_runtime = \"other\"\nauto_compact_window = 300000\n", wantContext: "work_runtime"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			path := writeConfig(t, home, tt.content)
			_, _, err := AutoCompactWindow(home)
			if err == nil {
				t.Fatal("AutoCompactWindow() must reject invalid config")
			}
			if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), tt.wantContext) {
				t.Fatalf("error %q must contain path %q and context %q", err, path, tt.wantContext)
			}
		})
	}
}

func TestMalformedConfigErrorDoesNotExposeSource(t *testing.T) {
	const (
		adjacentBeforeSentinel = "ADJACENT_BEFORE_SECRET_7f31"
		faultingSentinel       = "FAULTING_SECRET_8a42"
		adjacentAfterSentinel  = "ADJACENT_AFTER_SECRET_9b53"
	)
	home := t.TempDir()
	path := writeConfig(t, home, "work_runtime = \"codex\" # "+adjacentBeforeSentinel+"\n"+
		"auto_compact_window = ["+faultingSentinel+"\n"+
		"review_runtime = \"claude\" # "+adjacentAfterSentinel+"\n")

	_, _, err := RuntimeDefault(home, WorkRuntime)
	if err == nil {
		t.Fatal("RuntimeDefault() must reject malformed config")
	}
	message := err.Error()
	for _, want := range []string{path, "line 2", "column", "auto_compact_window"} {
		if !strings.Contains(message, want) {
			t.Errorf("error %q must contain safe context %q", message, want)
		}
	}
	for _, forbidden := range []string{
		adjacentBeforeSentinel,
		faultingSentinel,
		adjacentAfterSentinel,
		`work_runtime = "codex"`,
		"auto_compact_window = [",
		`review_runtime = "claude"`,
	} {
		if strings.Contains(message, forbidden) {
			t.Errorf("error exposed config source %q: %q", forbidden, message)
		}
	}
}

func TestAutoCompactWindowRejectsUnreadablePresentPath(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, FileName)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err := AutoCompactWindow(home)
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "read runtime config") {
		t.Fatalf("AutoCompactWindow() error = %v, want read error naming %s", err, path)
	}
}

func TestRuntimeDefaultRejectsUnreadablePresentPath(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, FileName)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err := RuntimeDefault(home, WorkRuntime)
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "read runtime config") {
		t.Fatalf("RuntimeDefault() error = %v, want read error naming %s", err, path)
	}
}

func TestRuntimeDefaultDistinguishesMissingFromDanglingSymlink(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		home := t.TempDir()
		got, set, err := RuntimeDefault(home, WorkRuntime)
		if err != nil || got != "" || set {
			t.Fatalf("RuntimeDefault() = %q, %v, %v; want empty unset default without error", got, set, err)
		}
	})

	t.Run("dangling symlink", func(t *testing.T) {
		home := t.TempDir()
		path := filepath.Join(home, FileName)
		if err := os.Symlink(filepath.Join(home, "missing-target"), path); err != nil {
			t.Fatal(err)
		}

		_, _, err := RuntimeDefault(home, WorkRuntime)
		if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "read runtime config") {
			t.Fatalf("RuntimeDefault() error = %v, want read error naming %s", err, path)
		}
	})
}

func stringPointer(value string) *string {
	return &value
}

func writeConfig(t *testing.T, home, content string) string {
	t.Helper()
	path := filepath.Join(home, FileName)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
