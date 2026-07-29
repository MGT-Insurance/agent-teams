// This file is owned by Track R (route-pr-event verbs).
// route_match.go — PR→initiative match engine (fkr.19).
// Ported from dashboard/server/src/parse.ts (extractPrUrl / matchInitiative
// logic). Routing fields are read through internal/initiative, not parsed
// here.  No edits to route_types.go.
package verbs

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// prURLRE matches a full GitHub PR URL:
//
//	https://github.com/<owner>/<repo>/pull/<number>
//
// Capture groups: [1] owner, [2] repo, [3] number.
var prURLRE = regexp.MustCompile(`https?://github\.com/([^/\s]+)/([^/\s]+)/pull/(\d+)`)

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
		f := initiative.Of(iss)

		// ── Tier 1: MatchPRField ───────────────────────────────────────────────
		// Check Notes first, then Description (convention from fkr.20).
		prURL := extractPrURL(iss.Notes)
		if prURL == "" {
			prURL = extractPrURL(iss.Description)
		}
		if prURL != "" {
			ownerRepo, prNumber, ok := parsePrURL(prURL)
			if ok && ownerRepo == eventOwnerRepo && prNumber == event.PRNumber {
				prMatches = append(prMatches, MatchResult{
					InitiativeID: iss.ID,
					Worktree:     f.Worktree,
					How:          MatchPRField,
				})
				continue // this initiative matched at tier-1; skip tier-2
			}
		}

		// ── Tier 2: MatchBranch ────────────────────────────────────────────────
		if headBranch == "" {
			continue
		}
		repoField := strings.ToLower(filepath.Base(f.Repo))
		if f.Repo != "" && repoField == eventRepoName && f.Branch == headBranch {
			branchMatches = append(branchMatches, MatchResult{
				InitiativeID: iss.ID,
				Worktree:     f.Worktree,
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

// matchClosedReviewInitiative finds the most recently created CLOSED
// initiative whose pr: URL matches the event — the reopen target for a
// re_review. Branch matching is deliberately not used here: closed
// initiatives accumulate and repo+branch pairs recur across them, so only
// the exact pr: URL is trustworthy.
func matchClosedReviewInitiative(ctx *cli.Context, event PREvent) (MatchResult, error) {
	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--status=closed", "--json"); err != nil {
		return MatchResult{}, fmt.Errorf("matchClosedReviewInitiative: list closed initiatives: %w", err)
	}
	return matchClosedFromIssues(issues, event), nil
}

// matchClosedFromIssues is the pure core of matchClosedReviewInitiative.
// Multiple matches resolve to the most recently created (RFC3339 CreatedAt
// compares lexicographically) rather than erroring — old review initiatives
// for the same PR are expected.
func matchClosedFromIssues(issues []bd.Issue, event PREvent) MatchResult {
	eventOwnerRepo := strings.ToLower(event.Repo)
	var best *bd.Issue
	for i := range issues {
		prURL := extractPrURL(issues[i].Notes)
		if prURL == "" {
			prURL = extractPrURL(issues[i].Description)
		}
		if prURL == "" {
			continue
		}
		ownerRepo, prNumber, ok := parsePrURL(prURL)
		if !ok || ownerRepo != eventOwnerRepo || prNumber != event.PRNumber {
			continue
		}
		if best == nil || issues[i].CreatedAt > best.CreatedAt {
			best = &issues[i]
		}
	}
	if best == nil {
		return MatchResult{How: MatchNone}
	}
	return MatchResult{InitiativeID: best.ID, Worktree: initiative.Of(*best).Worktree, How: MatchPRField}
}
