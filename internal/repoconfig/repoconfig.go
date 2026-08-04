// Package repoconfig reads the per-repo .agent-teams opt-in marker file.
//
// A repo is agent-teams-enabled only when <repo-root>/.agent-teams exists.
// Its content is a single optional "disabled: true" line; when present, it
// switches the repo back off — the identical effect of the file being absent
// altogether. Any other content (empty file, "disabled: false", unrelated
// lines) leaves the repo enabled.
//
// This is deliberately a separate, minimal parser from internal/initiative:
// that package owns the append-only, multi-key "key: value" contract inside
// a bd initiative's description; this file has exactly one legal key, lives
// on disk in the target repo (not in bd), and is read fresh on every call
// with no write path of its own.
package repoconfig

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the marker file's name, read from a repo's root directory.
const FileName = ".agent-teams"

// Enabled reports whether agent-teams is enabled for the repo rooted at
// repoRoot: true only when repoRoot/FileName exists and is not marked
// "disabled: true". A missing or unreadable file means disabled — the same
// effect as a present file carrying "disabled: true".
func Enabled(repoRoot string) bool {
	data, err := os.ReadFile(filepath.Join(repoRoot, FileName))
	if err != nil {
		return false
	}
	return !disabled(string(data))
}

// disabled reports whether content carries a "disabled: true" line. The key
// must start at column 0 (no leading whitespace) and the value must be
// exactly "true" after trimming; anything else — an absent key, "false", or
// unrelated prose — is not disabled.
func disabled(content string) bool {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok || key != "disabled" {
			continue
		}
		if strings.TrimSpace(value) == "true" {
			return true
		}
	}
	return false
}
