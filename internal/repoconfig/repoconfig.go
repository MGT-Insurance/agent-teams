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
//
// Enabled treats a URL as a path, so it reports false for a cloud initiative
// (whose "repo:" is a URL). That is deliberate for the senders and routers
// (ateam send, route-pr-event, resume, dispatch): they would otherwise run a
// local resume of a cloud run, so a cloud initiative stays off for them until
// those paths treat it as not local. Use Enabled there. A hook running inside
// the live session uses EnabledForSession.
func Enabled(repoRoot string) bool {
	data, err := os.ReadFile(filepath.Join(repoRoot, FileName))
	if err != nil {
		return false
	}
	return !disabled(string(data))
}

// EnabledForSession is Enabled for code that runs inside the live session
// itself, namely the wake hooks (resolve-initiative, hook-scan). A repo that
// is a URL (https://, http://, git@, ssh://) counts as enabled: a cloud
// initiative records its repo as a URL, no marker file can sit at that value,
// and the session is already running, so there is nothing to opt in to. Any
// other value is judged exactly as Enabled judges it.
//
// Do not use it in send, route, resume or dispatch. Turning a cloud
// initiative on there would start local resumes of a cloud run; use Enabled.
func EnabledForSession(repo string) bool {
	if isURL(repo) {
		return true
	}
	return Enabled(repo)
}

func isURL(repo string) bool {
	for _, prefix := range []string{"https://", "http://", "ssh://", "git@"} {
		if strings.HasPrefix(repo, prefix) {
			return true
		}
	}
	return false
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
