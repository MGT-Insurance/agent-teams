// This file is owned by Track R (route-pr-event verbs).
// route.go — route-pr-event verb: decision matrix + registration (fkr.21).
// Depends on route_types.go (PREvent, MatchResult, ateamRunner) and
// route_match.go (matchInitiative). File-disjoint from both.
package verbs

import (
	"fmt"
	"os"
	"strconv"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// RegisterRouteEvent registers the route-pr-event verb.
// Injects defaultAteamRunner as the production runner (execs the ateam binary).
func RegisterRouteEvent(reg cli.Registry) {
	reg.Register(&routePREventCommand{runner: defaultAteamRunner})
}

// routePREventCommand implements the route-pr-event verb.
type routePREventCommand struct {
	runner ateamRunner
}

func (c *routePREventCommand) Name() string { return "route-pr-event" }

// Run implements:
//
//	ateam route-pr-event --repo <owner/repo> --pr-number <n> --head-branch <br>
//	                     --transition <t> --body-file <path> [--pr-url <url>]
//
// Decision matrix (Eric-approved, fkr.18 contract):
//
//	owned (MatchPRField | MatchBranch) → ateam send <id> --file <body> --sender pr-shepherd
//	unowned + review_requested        → spawnReviewInitiative (TODO fkr.23 stub)
//	unowned + other transition        → log and exit 0
func (c *routePREventCommand) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam route-pr-event: nil context")
	}

	repo, prNumber, headBranch, transition, bodyFile, prURL, err := parseRoutePREventFlags(args)
	if err != nil {
		return err
	}

	event := PREvent{
		Repo:       repo,
		PRNumber:   prNumber,
		PRURL:      prURL,
		Transition: transition,
	}

	result, err := matchInitiative(ctx, event, headBranch)
	if err != nil {
		return fmt.Errorf("ateam route-pr-event: match: %w", err)
	}

	switch {
	case result.How == MatchPRField || result.How == MatchBranch:
		// ROUTE: owned initiative — deliver via ateam send.
		fmt.Fprintf(ctx.Stdout, "route-pr-event: matched %s (%s) for %s#%d — routing via send\n",
			result.InitiativeID, matchHowLabel(result.How), repo, prNumber)
		if err := c.runner("send", result.InitiativeID, "--file", bodyFile, "--sender", "pr-shepherd"); err != nil {
			return fmt.Errorf("ateam route-pr-event: send: %w", err)
		}
		return nil

	case transition == TransitionReviewRequested:
		// SPAWN: unowned PR + review_requested → secondary track (fkr.23).
		return spawnReviewInitiative(ctx, event, headBranch)

	default:
		// LOG-AND-SKIP: unowned PR, non-review transition → do nothing.
		fmt.Fprintf(ctx.Stdout, "route-pr-event: unowned %s for %s#%d — no owning initiative; skipping\n",
			transition, repo, prNumber)
		return nil
	}
}

// spawnReviewInitiative is the seam for the secondary track (fkr.23).
// It is called when a PR event arrives with transition=review_requested and
// no owning initiative has been found. fkr.23 fills in the body of this
// function with the actual dispatch/external-repo logic.
//
// Signature (frozen): func(ctx *cli.Context, event PREvent, headBranch string) error
func spawnReviewInitiative(ctx *cli.Context, event PREvent, headBranch string) error {
	// TODO fkr.23: implement spawn review initiative for unowned review_requested PRs.
	fmt.Fprintf(ctx.Stdout, "route-pr-event: unowned review_requested for %s#%d — spawn review initiative (TODO fkr.23)\n",
		event.Repo, event.PRNumber)
	return nil
}

// matchHowLabel returns a human-readable label for a MatchHow value.
func matchHowLabel(how MatchHow) string {
	switch how {
	case MatchPRField:
		return "pr-field"
	case MatchBranch:
		return "branch"
	default:
		return "none"
	}
}

// parseRoutePREventFlags parses the route-pr-event argv:
//
//	--repo <owner/repo> --pr-number <n> --head-branch <br>
//	--transition <t> --body-file <path> [--pr-url <url>]
func parseRoutePREventFlags(args []string) (repo string, prNumber int, headBranch string, transition PRTransition, bodyFile, prURL string, err error) {
	var prNumberStr string
	var transitionStr string

	for i := 0; i < len(args); {
		if v, n := parseFlag(args, i, "--repo"); n > 0 {
			repo = v
			i += n
			continue
		}
		if v, n := parseFlag(args, i, "--pr-number"); n > 0 {
			prNumberStr = v
			i += n
			continue
		}
		if v, n := parseFlag(args, i, "--head-branch"); n > 0 {
			headBranch = v
			i += n
			continue
		}
		if v, n := parseFlag(args, i, "--transition"); n > 0 {
			transitionStr = v
			i += n
			continue
		}
		if v, n := parseFlag(args, i, "--body-file"); n > 0 {
			bodyFile = v
			i += n
			continue
		}
		if v, n := parseFlag(args, i, "--pr-url"); n > 0 {
			prURL = v
			i += n
			continue
		}
		return "", 0, "", "", "", "", cli.Usagef("ateam route-pr-event: unknown flag %q", args[i])
	}

	if repo == "" {
		return "", 0, "", "", "", "", cli.Usagef("ateam route-pr-event: --repo required")
	}
	if prNumberStr == "" {
		return "", 0, "", "", "", "", cli.Usagef("ateam route-pr-event: --pr-number required")
	}
	n, convErr := strconv.Atoi(prNumberStr)
	if convErr != nil || n <= 0 {
		return "", 0, "", "", "", "", cli.Usagef("ateam route-pr-event: --pr-number must be a positive integer, got %q", prNumberStr)
	}
	prNumber = n
	if headBranch == "" {
		return "", 0, "", "", "", "", cli.Usagef("ateam route-pr-event: --head-branch required")
	}
	if transitionStr == "" {
		return "", 0, "", "", "", "", cli.Usagef("ateam route-pr-event: --transition required")
	}
	transition, err = parseTransition(transitionStr)
	if err != nil {
		return "", 0, "", "", "", "", err
	}
	if bodyFile == "" {
		return "", 0, "", "", "", "", cli.Usagef("ateam route-pr-event: --body-file required")
	}
	if _, statErr := os.Stat(bodyFile); statErr != nil {
		return "", 0, "", "", "", "", cli.Usagef("ateam route-pr-event: body-file not found: %s", bodyFile)
	}

	return repo, prNumber, headBranch, transition, bodyFile, prURL, nil
}

// parseTransition maps a --transition string to a PRTransition constant.
// Returns a UsageError for unknown values.
func parseTransition(s string) (PRTransition, error) {
	switch PRTransition(s) {
	case TransitionCIFailed,
		TransitionChangesRequested,
		TransitionReviewRequested,
		TransitionBotFindings,
		TransitionApproved,
		TransitionMerged,
		TransitionStale,
		TransitionOther:
		return PRTransition(s), nil
	default:
		return "", cli.Usagef("ateam route-pr-event: unknown --transition %q; valid values: ci_failed changes_requested review_requested bot_findings approved merged stale other", s)
	}
}
