package gitutil

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	remoteSCPLike = regexp.MustCompile(`^git@github\.com:([^/\s]+)/([^/\s]+?)(?:\.git)?/?$`)
	remoteURL     = regexp.MustCompile(`^(?:https://|ssh://git@)github\.com/([^/\s]+)/([^/\s]+?)(?:\.git)?/?$`)
	fullSHA       = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// NormalizeGitHubRemote converts a GitHub remote URL to
// https://github.com/<owner>/<repo>, preserving case. It accepts
// git@github.com:o/r(.git), https://github.com/o/r(.git), and
// ssh://git@github.com/o/r(.git), and returns an error for any other form.
func NormalizeGitHubRemote(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	for _, re := range []*regexp.Regexp{remoteSCPLike, remoteURL} {
		if m := re.FindStringSubmatch(raw); m != nil {
			return "https://github.com/" + m[1] + "/" + m[2], nil
		}
	}
	return "", fmt.Errorf("remote %q is not a github.com URL", raw)
}

// RemoteURL returns the origin remote of repoRoot as
// https://github.com/<owner>/<repo>.
// Equivalent to: git -C <repoRoot> remote get-url origin
func (r *Runner) RemoteURL(repoRoot string) (string, error) {
	out, errOut, err := r.exec("git", "-C", repoRoot, "remote", "get-url", "origin")
	if err != nil {
		msg := strings.TrimSpace(string(errOut))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git remote get-url origin: %s", msg)
	}
	return NormalizeGitHubRemote(string(out))
}

// RemoteHead returns the 40-hex commit sha at the head of branch on origin.
// It reads the remote, not the local ref, so unpushed commits are invisible.
// Equivalent to: git -C <repoRoot> ls-remote origin refs/heads/<branch>
func (r *Runner) RemoteHead(repoRoot, branch string) (string, error) {
	ref := "refs/heads/" + branch
	out, errOut, err := r.exec("git", "-C", repoRoot, "ls-remote", "origin", ref)
	if err != nil {
		msg := strings.TrimSpace(string(errOut))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git ls-remote origin %s: %s", ref, msg)
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	switch len(lines) {
	case 0:
		return "", fmt.Errorf("branch %q not found on origin (push it first)", branch)
	case 1:
	default:
		return "", fmt.Errorf("git ls-remote origin %s returned %d lines, want 1", ref, len(lines))
	}
	fields := strings.Fields(lines[0])
	if len(fields) != 2 || fields[1] != ref || !fullSHA.MatchString(fields[0]) {
		return "", fmt.Errorf("git ls-remote origin %s: unexpected output %q", ref, lines[0])
	}
	return fields[0], nil
}
