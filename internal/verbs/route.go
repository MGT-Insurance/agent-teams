// This file is owned by Track R (route-pr-event verbs).
// route.go — route-pr-event verb: decision matrix + registration (fkr.21, fkr.23).
// Depends on route_types.go (PREvent, MatchResult, ateamRunner) and
// route_match.go (matchInitiative). File-disjoint from both.
package verbs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/gitutil"
)

// routePREventKong is the kong-native form of route-pr-event.
// runner is injected via RegisterRouteEventKong (kong:"-" so kong ignores it).
type routePREventKong struct {
	Repo       string       `name:"repo"        help:"Owner/repo (e.g. owner/myrepo)."     required:""`
	PRNumber   int          `name:"pr-number"   help:"Pull request number (positive int)."  required:""`
	HeadBranch string       `name:"head-branch" help:"Head branch of the pull request."     required:""`
	Transition PRTransition `name:"transition"  help:"PR event transition."                 required:"" enum:"ci_failed,changes_requested,review_requested,bot_findings,approved,merged,stale,other"`
	BodyFile   string       `name:"body-file"   help:"Path to the event body file."         required:""`
	PRURL      string       `name:"pr-url"      help:"Full PR URL (optional, for logging)."`
	runner     ateamRunner  `kong:"-"`
}

// Validate is called by kong after parsing. Enforces --pr-number > 0.
func (c *routePREventKong) Validate() error {
	if c.PRNumber <= 0 {
		return cli.Usagef("ateam route-pr-event: --pr-number must be a positive integer, got %d", c.PRNumber)
	}
	return nil
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *routePREventKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam route-pr-event: nil context")
	}
	if _, statErr := os.Stat(c.BodyFile); statErr != nil {
		return cli.Usagef("ateam route-pr-event: body-file not found: %s", c.BodyFile)
	}

	event := PREvent{
		Repo:       c.Repo,
		PRNumber:   c.PRNumber,
		PRURL:      c.PRURL,
		Transition: c.Transition,
	}

	result, err := matchInitiative(ctx, event, c.HeadBranch)
	if err != nil {
		return fmt.Errorf("ateam route-pr-event: match: %w", err)
	}

	switch {
	case result.How == MatchPRField || result.How == MatchBranch:
		fmt.Fprintf(ctx.Stdout, "route-pr-event: matched %s (%s) for %s#%d — routing via send\n",
			result.InitiativeID, matchHowLabel(result.How), c.Repo, c.PRNumber)
		if err := c.runner("send", result.InitiativeID, "--file", c.BodyFile, "--sender", "pr-shepherd"); err != nil {
			return fmt.Errorf("ateam route-pr-event: send: %w", err)
		}
		return nil

	case c.Transition == TransitionReviewRequested:
		return c.spawnReviewInitiative(ctx, event)

	default:
		fmt.Fprintf(ctx.Stdout, "route-pr-event: unowned %s for %s#%d — no owning initiative; skipping\n",
			c.Transition, c.Repo, c.PRNumber)
		return nil
	}
}

// RegisterRouteEventKong registers route-pr-event as a native kong verb onto p.
func RegisterRouteEventKong(p *cli.Parser) {
	p.AddVerb("route-pr-event", "Route a PR event to an owning initiative.", &routePREventKong{runner: defaultAteamRunner})
}

// spawnReviewInitiative handles the SPAWN path (fkr.23): an unowned PR with
// transition=review_requested. It resolves the event repo to a local clone
// path via a config file at <ctx.Home>/review-repos/<repo-key>, where
// repo-key = Slugify(basename(event.Repo)). If the config file is absent,
// it logs a skip message and returns nil. If configured, it writes a temp
// file containing review instructions and invokes the ateamRunner with:
//
//	dispatch --repo <clonePath> --problem <title> --body-file <tmpFile>
//
// Registration (one-time, out of band):
//
//	mkdir -p ~/.agent-teams/review-repos
//	echo /abs/path/to/local-clone > ~/.agent-teams/review-repos/<repo-key>
//
// e.g. for MGT-Insurance/midgard (key = "midgard"):
//
//	echo /Users/ericlloyd/Code/midgard > ~/.agent-teams/review-repos/midgard
func (c *routePREventKong) spawnReviewInitiative(ctx *cli.Context, event PREvent) error {
	// repo-key = Slugify(basename of owner/repo)
	repoKey := gitutil.Slugify(filepath.Base(event.Repo))

	// Read the config file that maps the key to a local clone path.
	configFile := filepath.Join(ctx.Home, "review-repos", repoKey)
	data, err := os.ReadFile(configFile)
	if err != nil {
		// Not configured for this repo — log and skip.
		fmt.Fprintf(ctx.Stdout, "route-pr-event: review-spawn not configured for %s (no %s); skipping\n",
			event.Repo, configFile)
		return nil
	}
	clonePath := strings.TrimSpace(string(data))

	// Build the review title and instruction body.
	title := fmt.Sprintf("Review PR #%d (%s)", event.PRNumber, event.Repo)

	var sb strings.Builder
	sb.WriteString("You are reviewing an EXISTING pull request — you are NOT building a feature ")
	sb.WriteString("and you must NOT open a new PR.\n\n")
	sb.WriteString(fmt.Sprintf("You are in a fresh worktree of %s.\n\n", event.Repo))
	sb.WriteString("Steps:\n")
	sb.WriteString(fmt.Sprintf("(1) Run `gh pr checkout %d` to get the PR's code locally.\n", event.PRNumber))
	sb.WriteString(fmt.Sprintf("(2) Read the full diff: `gh pr diff %d`\n", event.PRNumber))
	sb.WriteString("(3) Read the surrounding code as needed.\n")
	sb.WriteString("(4) Review for correctness, tests, edge cases, and security.\n")
	sb.WriteString("(5) Post your review. Comment rules:\n")
	sb.WriteString("    - NO nit comments. Skip style/preference/trivia entirely — post only\n")
	sb.WriteString("      substantive findings (correctness, tests, edge cases, security).\n")
	sb.WriteString("    - Post each finding as an INLINE comment anchored to its line, NOT one\n")
	sb.WriteString("      big block comment. Create the review in a single API call with a\n")
	sb.WriteString("      comments array, one entry per finding:\n")
	sb.WriteString(fmt.Sprintf("        gh api repos/%s/pulls/%d/reviews --method POST \\\n", event.Repo, event.PRNumber))
	sb.WriteString("          -f event=COMMENT -f body=\"<one-sentence summary>\" \\\n")
	sb.WriteString("          -F 'comments[][path]=FILE' -F 'comments[][line]=N' -F 'comments[][body]=...' ...\n")
	sb.WriteString("    - The review body is ONE final comment: a single sentence summarizing the\n")
	sb.WriteString("      outcome. No long block summary.\n\n")
	sb.WriteString("If the worktree needs a live env to run/build, run `ateam worktree-setup <this-worktree-abs-path>`.\n\n")
	sb.WriteString("Do NOT open a PR.\n")
	sb.WriteString("Do NOT raise a merge/review gate for your own work.\n")
	sb.WriteString("Your deliverable is the posted review on PR #" + fmt.Sprintf("%d", event.PRNumber) + ".\n")
	if event.PRURL != "" {
		sb.WriteString("\nPR URL: " + event.PRURL + "\n")
	}

	// Write the instruction body to a temp file.
	tmpFile, err := os.CreateTemp("", "review-instructions-*.txt")
	if err != nil {
		return fmt.Errorf("route-pr-event: review-spawn: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.WriteString(sb.String()); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("route-pr-event: review-spawn: write temp file: %w", err)
	}
	tmpFile.Close()

	// Invoke dispatch via the runner.
	runErr := c.runner("dispatch", "--repo", clonePath, "--problem", title, "--body-file", tmpPath)
	// Clean up temp file after the runner returns (dispatch has already read it).
	os.Remove(tmpPath)

	if runErr != nil {
		return fmt.Errorf("route-pr-event: review-spawn: dispatch: %w", runErr)
	}

	fmt.Fprintf(ctx.Stdout, "route-pr-event: spawned review initiative for %s#%d\n",
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
