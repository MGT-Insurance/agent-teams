package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpExposesWriteAndCheckWithoutInitialization(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	for _, wanted := range []string{"check", "write", "--allow-unmigrated"} {
		if !strings.Contains(stdout.String(), wanted) {
			t.Errorf("help missing %q:\n%s", wanted, stdout.String())
		}
	}
}

func TestFixtureCleanCheckThroughCLI(t *testing.T) {
	root := copyCLIFixture(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"check", "--root", root}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "generated_outputs=6") || !strings.Contains(stdout.String(), "rendered_utf16=") {
		t.Fatalf("unexpected report:\n%s", stdout.String())
	}
}

func copyCLIFixture(t *testing.T) string {
	t.Helper()
	source := filepath.Join("..", "..", "internal", "promptsync", "testdata", "clean")
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
