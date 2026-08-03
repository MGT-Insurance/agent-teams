// Package repoconfig reads and writes the per-repo .agent-teams opt-in
// marker file.
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
// on disk in the target repo (not in bd), and is read fresh on every call.
//
// Enable is the package's write seam (used by `ateam enable-repo`): it
// creates a missing marker with the canonical Header, or strips a disabling
// "disabled: true" line from a present one, leaving every other byte alone.
// The writer lives here rather than in internal/verbs because the header
// text and the "disabled:"-at-column-0 rule are this package's format
// knowledge — the parser (disabled, via isDisablingLine) and the writer
// must never disagree about what counts as a disabling line.
package repoconfig

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the marker file's name, read from a repo's root directory.
const FileName = ".agent-teams"

// Header is the canonical comment block Enable writes into a newly created
// marker file. Byte-for-byte frozen by contract (agent-teams-qxtw.1) —
// this repo's own .agent-teams file is the reference copy.
const Header = "# agent-teams opt-in marker for this repo (internal/repoconfig).\n" +
	"# Set `disabled: true` on its own line below to temporarily pause\n" +
	"# agent-teams here: no new `ateam dispatch`/`ateam resume`, and hooks\n" +
	"# (wake-watcher, etc.) go quiet for any already-open initiative.\n"

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
		if isDisablingLine(scanner.Text()) {
			return true
		}
	}
	return false
}

// isDisablingLine reports whether a single line is a canonical
// "disabled: true" line: the key must start at column 0 (no leading
// whitespace) and the value must be exactly "true" after trimming. This is
// the one predicate both disabled (the reader) and Enable (the writer) call,
// so the two can never disagree about what counts as disabling.
func isDisablingLine(line string) bool {
	key, value, ok := strings.Cut(line, ":")
	if !ok || key != "disabled" {
		return false
	}
	return strings.TrimSpace(value) == "true"
}

// EnableResult describes which repair Enable performed.
type EnableResult int

const (
	// ResultCreated means the marker file did not exist and was created
	// with Header.
	ResultCreated EnableResult = iota
	// ResultUndisabled means the marker existed with a "disabled: true"
	// line, which was removed; every other line was preserved.
	ResultUndisabled
	// ResultAlreadyEnabled means the marker existed and was already
	// enabled; nothing was written.
	ResultAlreadyEnabled
)

// String renders the parenthetical `ateam enable-repo` prints after
// "enabled: <path> " — e.g. "(created)". Kept here, next to the enum it
// describes, rather than duplicated in internal/verbs.
func (r EnableResult) String() string {
	switch r {
	case ResultCreated:
		return "(created)"
	case ResultUndisabled:
		return `(removed "disabled: true")`
	case ResultAlreadyEnabled:
		return "(already enabled)"
	default:
		return "(unknown)"
	}
}

// Enable ensures repoRoot/FileName exists and is enabled, performing the
// minimal repair needed:
//
//   - missing file -> write Header -> ResultCreated
//   - present, carrying a disabling line -> remove only that line (or
//     lines), preserving every other byte -> ResultUndisabled
//   - present, already enabled -> no write at all -> ResultAlreadyEnabled
//
// A file whose only content was a disabling line becomes empty (still
// enabled per Enabled's contract) rather than being deleted.
func Enable(repoRoot string) (EnableResult, error) {
	path := filepath.Join(repoRoot, FileName)

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if werr := os.WriteFile(path, []byte(Header), 0o644); werr != nil {
			return ResultCreated, fmt.Errorf("write %s: %w", path, werr)
		}
		return ResultCreated, nil
	}
	if err != nil {
		return ResultAlreadyEnabled, fmt.Errorf("read %s: %w", path, err)
	}

	content := string(data)
	if !disabled(content) {
		return ResultAlreadyEnabled, nil
	}

	if werr := os.WriteFile(path, []byte(stripDisablingLines(content)), 0o644); werr != nil {
		return ResultUndisabled, fmt.Errorf("write %s: %w", path, werr)
	}
	return ResultUndisabled, nil
}

// stripDisablingLines removes every isDisablingLine line from content,
// preserving all other lines (and their order) intact. The result always
// ends in a single trailing newline unless it is empty.
func stripDisablingLines(content string) string {
	lines := strings.Split(content, "\n")
	// strings.Split on a trailing-newline string yields a final "" element
	// representing no content after the last "\n" — drop it before
	// filtering so it isn't mistaken for a blank line to preserve.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}

	kept := lines[:0]
	for _, line := range lines {
		if !isDisablingLine(line) {
			kept = append(kept, line)
		}
	}

	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, "\n") + "\n"
}
