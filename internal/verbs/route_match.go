// This file is owned by Track R (route-pr-event verbs).
// route_match.go — PR→initiative match engine (fkr.19).
// Ported from dashboard/server/src/parse.ts (extractPrUrl / parseDescriptionFields
// / matchInitiative logic).  No edits to route_types.go or dispatch.go.
package verbs

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// prURLRE matches a full GitHub PR URL:
//
//	https://github.com/<owner>/<repo>/pull/<number>
//
// Capture groups: [1] owner, [2] repo, [3] number.
var prURLRE = regexp.MustCompile(`https?://github\.com/([^/\s]+)/([^/\s]+)/pull/(\d+)`)

// parseDescriptionFields splits text into key→value pairs by scanning for the
// first ":" on each line.  Keys are lowercased and trimmed; values are trimmed.
// Lines with an empty key or empty value are skipped.
// Mirrors parse.ts parseDescriptionFields.
func parseDescriptionFields(text string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		colon := strings.IndexByte(line, ':')
		if colon == -1 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		value := strings.TrimSpace(line[colon+1:])
		if key != "" && value != "" {
			result[key] = value
		}
	}
	return result
}

// extractPrURL returns the first GitHub PR URL found in text, or "".
// Mirrors parse.ts extractPrUrl.
func extractPrURL(text string) string {
	m := prURLRE.FindString(text)
	return m
}

// parsePrURL parses a GitHub PR URL and returns (owner/repo, prNumber, ok).
// owner/repo is lower-cased for comparison.
func parsePrURL(url string) (ownerRepo string, prNumber int, ok bool) {
	m := prURLRE.FindStringSubmatch(url)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[3])
	if err != nil {
		return "", 0, false
	}
	return strings.ToLower(m[1] + "/" + m[2]), n, true
}

// matchInitiative finds the open initiative that owns the given PR event.
//
// headBranch is passed separately because PREvent (frozen by fkr.18) does not
// carry the head branch — the caller (fkr.21) threads it through from the
// route-pr-event argv.
//
// Precedence (frozen by fkr.18 contract):
//
//  1. MatchPRField (exact): initiative has a "pr: <url>" line in Notes (checked
//     first) or Description whose GitHub owner/repo+number equals event.Repo
//     (owner/repo) + event.PRNumber.
//
//  2. MatchBranch (fallback): basename of initiative "repo:" field equals the
//     repo-name portion of event.Repo (i.e. the part after "/"), AND the
//     initiative's "branch:" field equals headBranch.
//     Known caveat: same-named repo under a different owner is a theoretical
//     collision — acceptable for v1; the MatchPRField path is the robust one.
//
//  3. MatchNone if nothing matched.
//
// If more than one initiative matches, the MatchPRField match wins.  If still
// ambiguous (two MatchPRField matches, or two MatchBranch matches), an error is
// returned rather than guessing.
func matchInitiative(ctx *cli.Context, event PREvent, headBranch string) (MatchResult, error) {
	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--status=open", "--json"); err != nil {
		return MatchResult{}, fmt.Errorf("matchInitiative: list open initiatives: %w", err)
	}
	return matchInitiativeFromIssues(issues, event, headBranch)
}

// matchInitiativeFromIssues is the pure (injectable) core of matchInitiative.
// It accepts the already-fetched issue slice so tests can drive it without a
// real bd binary.
func matchInitiativeFromIssues(issues []bd.Issue, event PREvent, headBranch string) (MatchResult, error) {
	// Normalise event.Repo to lower-case for case-insensitive comparison.
	eventOwnerRepo := strings.ToLower(event.Repo)
	// Repo name is the part after the last "/".
	eventRepoName := strings.ToLower(filepath.Base(event.Repo))

	var prMatches []MatchResult
	var branchMatches []MatchResult

	for _, iss := range issues {
		// ── Tier 1: MatchPRField ───────────────────────────────────────────────
		// Check Notes first, then Description (convention from fkr.20).
		prURL := extractPrURL(iss.Notes)
		if prURL == "" {
			prURL = extractPrURL(iss.Description)
		}
		if prURL != "" {
			ownerRepo, prNumber, ok := parsePrURL(prURL)
			if ok && ownerRepo == eventOwnerRepo && prNumber == event.PRNumber {
				wt := worktreePath(iss.Description)
				prMatches = append(prMatches, MatchResult{
					InitiativeID: iss.ID,
					Worktree:     wt,
					How:          MatchPRField,
				})
				continue // this initiative matched at tier-1; skip tier-2
			}
		}

		// ── Tier 2: MatchBranch ────────────────────────────────────────────────
		if headBranch == "" {
			continue
		}
		fields := parseDescriptionFields(iss.Description)
		repoField := strings.ToLower(filepath.Base(fields["repo"]))
		branchField := fields["branch"]
		if repoField != "" && repoField == eventRepoName && branchField == headBranch {
			wt := worktreePath(iss.Description)
			branchMatches = append(branchMatches, MatchResult{
				InitiativeID: iss.ID,
				Worktree:     wt,
				How:          MatchBranch,
			})
		}
	}

	// MatchPRField wins over MatchBranch.
	if len(prMatches) == 1 {
		return prMatches[0], nil
	}
	if len(prMatches) > 1 {
		return MatchResult{}, fmt.Errorf(
			"matchInitiative: ambiguous — %d initiatives matched PR %s#%d by pr: field",
			len(prMatches), event.Repo, event.PRNumber,
		)
	}

	if len(branchMatches) == 1 {
		return branchMatches[0], nil
	}
	if len(branchMatches) > 1 {
		return MatchResult{}, fmt.Errorf(
			"matchInitiative: ambiguous — %d initiatives matched repo=%s branch=%s",
			len(branchMatches), eventRepoName, headBranch,
		)
	}

	return MatchResult{How: MatchNone}, nil
}
