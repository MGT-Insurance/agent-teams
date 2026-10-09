// This file is owned by Track R (route-pr-event verbs).
// route.go — route-pr-event verb: decision matrix + registration (fkr.21, fkr.23).
// Depends on route_types.go (PREvent, MatchResult, ateamRunner) and
// route_match.go (matchInitiative). File-disjoint from both.
package verbs

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/gitutil"
	"github.com/mgt-insurance/agent-teams/internal/repoconfig"
)

// routePREventKong is the kong-native form of route-pr-event.
// runner is injected via RegisterRouteEventKong (kong:"-" so kong ignores it).
type routePREventKong struct {
	Repo       string       `name:"repo"        help:"Owner/repo (e.g. owner/myrepo)."     required:""`
	PRNumber   int          `name:"pr-number"   help:"Pull request number (positive int)."  required:""`
	HeadBranch string       `name:"head-branch" help:"Head branch of the pull request."     required:""`
	Transition PRTransition `name:"transition"  help:"PR event transition."                 required:"" enum:"ci_failed,changes_requested,review_requested,bot_findings,approved,merged,stale,re_review,comment_reply,other"`
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
		// Checked BEFORE the send: this is the direct "already-open, matched
		// by field/branch" path — the one route Codex flagged as reviving a
		// disabled repo, since it never touched spawnReviewInitiative's gate
		// (that only guards the SPAWN path, an unowned PR with no match at
		// all). result.Repo empty (legacy data with no "repo:" field) skips
		// the check rather than judging a marker file against "".
		if result.Repo != "" && !repoconfig.Enabled(result.Repo) {
			fmt.Fprintf(ctx.Stdout, "route-pr-event: matched %s (%s) for %s#%d but its repo is disabled (%s); skipping\n",
				result.InitiativeID, matchHowLabel(result.How), c.Repo, c.PRNumber, repoconfig.FileName)
			return nil
		}
		fmt.Fprintf(ctx.Stdout, "route-pr-event: matched %s (%s) for %s#%d — routing via mail send\n",
			result.InitiativeID, matchHowLabel(result.How), c.Repo, c.PRNumber)
		return c.send(ctx, result.InitiativeID)

	case c.Transition == TransitionReviewRequested, c.Transition == TransitionReReview, c.Transition == TransitionCommentReply:
		return c.routeClosedOrSpawn(ctx, event)

	default:
		fmt.Fprintf(ctx.Stdout, "route-pr-event: unowned %s for %s#%d — no owning initiative; skipping\n",
			c.Transition, c.Repo, c.PRNumber)
		return nil
	}
}

// send builds the mail-send argv (sendArgs) and runs it, removing the temp
// body file it wrote regardless of the runner's outcome.
func (c *routePREventKong) send(ctx *cli.Context, id string) error {
	args, tmpPath, err := c.sendArgs(id)
	if err != nil {
		return fmt.Errorf("ateam route-pr-event: build send args: %w", err)
	}
	sendErr := c.runner(args...)
	os.Remove(tmpPath)
	if sendErr != nil {
		return fmt.Errorf("ateam route-pr-event: send: %w", sendErr)
	}
	return nil
}

// sendArgs builds the mail-send argv that routes the event to id (CONTRACT
// agent-teams-8st0.18 item 4). The message body is "transition: <t>"
// prepended to the event body (read from c.BodyFile) and written to a fresh
// temp file — mail send reads it via --file, same contract as BodyFile
// itself. --dedup-key is the first 16 hex chars of
// sha256(repo#pr|transition|body), so pr-shepherd's retry after a failed
// reopen or a failed send (mail send always stores per 8st0.19, so the
// message already exists) dedups into the same message instead of creating
// a second one. Returns the temp file's path so the caller removes it once
// the runner returns.
func (c *routePREventKong) sendArgs(id string) (args []string, tmpPath string, err error) {
	body, err := os.ReadFile(c.BodyFile)
	if err != nil {
		return nil, "", fmt.Errorf("read body-file: %w", err)
	}

	tmp, err := os.CreateTemp("", "route-body-*.txt")
	if err != nil {
		return nil, "", fmt.Errorf("create temp body file: %w", err)
	}
	if _, err := tmp.WriteString("transition: " + string(c.Transition) + "\n" + string(body)); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, "", fmt.Errorf("write temp body file: %w", err)
	}
	tmp.Close()

	sum := sha256.Sum256([]byte(fmt.Sprintf("%s#%d|%s|%s", c.Repo, c.PRNumber, c.Transition, body)))
	key := fmt.Sprintf("%x", sum)[:16]

	return []string{"mail", "send", id, "--file", tmp.Name(), "--sender", "pr-shepherd", "--dedup-key", key}, tmp.Name(), nil
}

// routeClosedOrSpawn handles review_requested, re_review, and comment_reply
// when no OPEN initiative matches the PR (CONTRACT agent-teams-8st0.18 item
// 4, agent-teams-8st0.27 rev 5): a closed review initiative matching the PR
// is reopened and ALWAYS sent to, regardless of whether the reopen
// succeeded — mail send always stores the message (8st0.19), so a failed
// reopen just leaves it queued; pr-shepherd's retry re-matches the
// still-closed initiative, reopens it, and the resend dedups (no second
// message). A reopen failure is reported by returning an error AFTER the
// send call, so the process exits 1 and the caller retries. No closed match
// at all spawns a fresh review for review_requested/re_review; comment_reply
// has nothing to answer into without a prior review, so it skips.
func (c *routePREventKong) routeClosedOrSpawn(ctx *cli.Context, event PREvent) error {
	result, err := matchClosedReviewInitiative(ctx, event)
	if err != nil {
		return fmt.Errorf("ateam route-pr-event: closed-match: %w", err)
	}
	if result.How == MatchNone {
		if c.Transition == TransitionCommentReply {
			fmt.Fprintf(ctx.Stdout, "route-pr-event: comment_reply for %s#%d has no initiative — skipping\n",
				event.Repo, event.PRNumber)
			return nil
		}
		fmt.Fprintf(ctx.Stdout, "route-pr-event: %s for %s#%d has no prior initiative — spawning fresh review\n",
			c.Transition, event.Repo, event.PRNumber)
		return c.spawnReviewInitiative(ctx, event)
	}
	// Disabled repos get NO reopen and NO fallback here: this is deliberate
	// operator policy, not a transient error, so degrading to
	// spawnReviewInitiative (which would independently re-check the SAME
	// repo via review-repos config and refuse too) would just be a confusing
	// second path to the same "no" — a direct, explicit skip is clearer and
	// does not depend on that second check agreeing.
	if result.Repo != "" && !repoconfig.Enabled(result.Repo) {
		fmt.Fprintf(ctx.Stdout, "route-pr-event: %s matched closed %s for %s#%d but its repo is disabled (%s); skipping\n",
			c.Transition, result.InitiativeID, event.Repo, event.PRNumber, repoconfig.FileName)
		return nil
	}
	fmt.Fprintf(ctx.Stdout, "route-pr-event: %s matched closed %s for %s#%d — reopening\n",
		c.Transition, result.InitiativeID, event.Repo, event.PRNumber)
	reopenErr := c.runner("reopen", result.InitiativeID)
	if reopenErr != nil {
		fmt.Fprintf(ctx.Stdout, "route-pr-event: reopen %s failed (%v) — sending anyway\n",
			result.InitiativeID, reopenErr)
	}
	if sendErr := c.send(ctx, result.InitiativeID); sendErr != nil {
		return sendErr
	}
	if reopenErr != nil {
		return fmt.Errorf("ateam route-pr-event: reopen %s failed: %w", result.InitiativeID, reopenErr)
	}
	return nil
}

// RegisterRouteEventKong registers route-pr-event as a native kong verb onto p.
func RegisterRouteEventKong(p *cli.Parser) {
	p.AddVerb("route-pr-event", "Route a PR event to an owning initiative.", &routePREventKong{runner: defaultAteamRunner})
}

// spawnReviewInitiative handles the SPAWN path (fkr.23): an unowned PR with
// transition=review_requested or re_review and no prior (open or closed)
// initiative for it. It resolves the event repo to a local clone path via a
// config file at <ctx.Home>/review-repos/<repo-key>, where repo-key =
// Slugify(basename(event.Repo)). If the config file is absent, or if it's
// present but the clone has no (or a disabled) .agent-teams file
// (internal/repoconfig), it logs a skip message and returns nil — the latter
// check exists so a disabled repo with an open review_requested PR degrades
// to one quiet log line per pr-shepherd poll instead of a dispatch subprocess
// spawned (and refused, loudly) every cycle. If configured and enabled, it
// writes a temp file containing structured PR metadata and invokes the
// ateamRunner with:
//
//	dispatch --repo <clonePath> --problem <title> --body-file <tmpFile> \
//	         --launch-prompt "/agent-teams:review-pr {id}" --skip-epic \
//	         --model sonnet --topic reviews
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

	// A disabled/not-yet-opted-in clone is skipped here, quietly and without
	// a subprocess — same shape as the "not configured" branch above. Without
	// this, a repeatedly-polled review_requested PR on a disabled repo would
	// spawn a real `dispatch` subprocess every poll (pr-shepherd's 180s cycle,
	// shepherd.config.json) just to have it print its (louder, multi-clause)
	// own refusal — chatty in pr-shepherd's logs for as long as the PR sits
	// open.
	if !repoconfig.Enabled(clonePath) {
		fmt.Fprintf(ctx.Stdout, "route-pr-event: review-spawn: agent-teams not enabled for %s (%s); skipping\n",
			clonePath, repoconfig.FileName)
		return nil
	}

	// Build the review title.
	title := fmt.Sprintf("Review PR #%d (%s)", event.PRNumber, event.Repo)

	// Build structured metadata body parseable by the review-pr skill.
	prURL := event.PRURL
	if prURL == "" {
		prURL = fmt.Sprintf("https://github.com/%s/pull/%d", event.Repo, event.PRNumber)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("pr-number: %d\n", event.PRNumber))
	sb.WriteString(fmt.Sprintf("pr-repo: %s\n", event.Repo))
	sb.WriteString(fmt.Sprintf("pr-url: %s\n", prURL))

	// Write the metadata to a temp file.
	tmpFile, err := os.CreateTemp("", "review-metadata-*.txt")
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

	// Invoke dispatch via the runner with --launch-prompt and --skip-epic so the
	// lightweight /agent-teams:review-pr skill runs instead of a full DRI.
	// --model sonnet keeps automated review sessions cheaper than the opus
	// default used for full DRI initiatives.
	//
	// --topic ReviewsHandle marks this a review initiative: dispatch opens no
	// Telegram topic and posts no message for it, and selects review_runtime.
	runErr := c.runner("dispatch", "--repo", clonePath, "--problem", title, "--body-file", tmpPath,
		"--launch-prompt", "/agent-teams:review-pr {id}", "--skip-epic", "--model", "sonnet",
		"--topic", ReviewsHandle)
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
