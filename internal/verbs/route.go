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
		if err := c.runner(c.sendArgs(result.InitiativeID)...); err != nil {
			return fmt.Errorf("ateam route-pr-event: send: %w", err)
		}
		return nil

	case c.Transition == TransitionReviewRequested:
		return c.spawnReviewInitiative(ctx, event, "")

	case c.Transition == TransitionReReview:
		return c.routeReReview(ctx, event)

	case c.Transition == TransitionCommentReply:
		return c.routeCommentReply(ctx, event)

	default:
		fmt.Fprintf(ctx.Stdout, "route-pr-event: unowned %s for %s#%d — no owning initiative; skipping\n",
			c.Transition, c.Repo, c.PRNumber)
		return nil
	}
}

// sendArgs builds the mail-send argv for routing the event body to id. A
// re_review send threads the reviewer launch prompt so a dead session is
// resumed as a reviewer on sonnet (matching the spawn path), never a DRI.
func (c *routePREventKong) sendArgs(id string) []string {
	args := []string{"mail", "send", id, "--file", c.BodyFile, "--sender", "pr-shepherd"}
	if c.Transition == TransitionReReview {
		args = append(args,
			"--resume-launch-prompt", "/agent-teams:review-pr "+id,
			"--resume-model", "sonnet")
	}
	return args
}

// routeReReview handles transition=re_review when no open initiative owns
// the PR: reopen the closed review initiative and mail it the re-review
// request. A fresh spawn is the fallback at every step — no prior
// initiative, reopen failure, or send failure (e.g. deleted worktree) all
// degrade to a new review initiative rather than dropping the event.
// Caveat: if reopen succeeds but the send fails, the spawn fallback leaves
// TWO open initiatives for the same PR (the reopened one never got the
// mail); a later poll may then hit matchInitiative's ambiguity error and
// need manual cleanup (close one). Accepted: rare, and losing the re-review
// event silently would be worse.
func (c *routePREventKong) routeReReview(ctx *cli.Context, event PREvent) error {
	result, err := matchClosedReviewInitiative(ctx, event)
	if err != nil {
		return fmt.Errorf("ateam route-pr-event: re-review match: %w", err)
	}
	if result.How == MatchNone {
		fmt.Fprintf(ctx.Stdout, "route-pr-event: re_review for %s#%d has no prior initiative — spawning fresh review\n",
			event.Repo, event.PRNumber)
		return c.spawnReviewInitiative(ctx, event, "")
	}
	// Disabled repos get NO fallback here, unlike a reopen/send failure below:
	// this is deliberate operator policy, not a transient error, so degrading
	// to spawnReviewInitiative (which would independently re-check the SAME
	// repo via review-repos config and refuse too) would just be a confusing
	// second path to the same "no" — a direct, explicit skip is clearer and
	// does not depend on that second check agreeing.
	if result.Repo != "" && !repoconfig.Enabled(result.Repo) {
		fmt.Fprintf(ctx.Stdout, "route-pr-event: re_review matched closed %s for %s#%d but its repo is disabled (%s); skipping\n",
			result.InitiativeID, event.Repo, event.PRNumber, repoconfig.FileName)
		return nil
	}
	// agent-teams-8st0.7: a closed review initiative whose worktree has been
	// reaped has no session to resume — reopening it anyway is exactly what
	// produced the observed reopen/close ping-pong (hung-scan's backstop
	// reclassifies the worktree-less initiative DEAD and closes it again on
	// the stale round's review-posted note, then the next re_review poll
	// reopens it right back). Treat a reaped worktree the same as a reopen
	// failure below: fall back to spawning a FRESH review, which gets a live
	// worktree and can actually serve the re-review.
	if !reviewInitiativeWorktreeLive(result.Worktree) {
		fmt.Fprintf(ctx.Stdout, "route-pr-event: re_review matched closed %s for %s#%d but its worktree is gone (%s) — spawning fresh review instead of reopening a dead initiative\n",
			result.InitiativeID, event.Repo, event.PRNumber, result.Worktree)
		return c.spawnReviewInitiative(ctx, event, "")
	}
	fmt.Fprintf(ctx.Stdout, "route-pr-event: re_review matched closed %s for %s#%d — reopening\n",
		result.InitiativeID, event.Repo, event.PRNumber)
	if err := c.runner("reopen", result.InitiativeID); err != nil {
		fmt.Fprintf(ctx.Stdout, "route-pr-event: reopen %s failed (%v) — spawning fresh review\n",
			result.InitiativeID, err)
		return c.spawnReviewInitiative(ctx, event, "")
	}
	if err := c.runner(c.sendArgs(result.InitiativeID)...); err != nil {
		fmt.Fprintf(ctx.Stdout, "route-pr-event: send to %s failed (%v) — spawning fresh review\n",
			result.InitiativeID, err)
		return c.spawnReviewInitiative(ctx, event, "")
	}
	return nil
}

// routeCommentReply handles transition=comment_reply when no open initiative
// owns the PR: reopen the closed review initiative and mail it the reply so
// the relaunched session can respond in-thread (the resume prompt carries the
// comment-reply mode argument). No-match (the PR was never reviewed) and a
// disabled repo still log and skip — a comment-reply session needs a prior
// review to answer into, and a disabled repo is deliberate operator policy,
// not a failure any fallback should route around.
//
// agent-teams-8st0.14 (Eric's rule: a reply on a PR this system reviewed
// must reach a session): every OTHER way this can fail now spawns a fresh
// review initiative in comment-reply mode (spawnReviewInitiative with mode
// "comment-reply") instead of dropping the reply — a reaped worktree (no
// live session to reopen into), a reopen call that itself errors, or a
// reopen that succeeds but whose mail send then fails. None of these leaves
// a live session for the old thread-re-derivation recovery (the
// comment-reply session reads whole threads from GitHub, so the next reply
// on the PR used to re-trigger routing) to actually resume into, so only a
// fresh spawn gives the reply somewhere to land. A send failure AFTER a
// successful reopen still compensating-closes the initiative first (so a
// later event re-matches via matchClosedReviewInitiative instead of piling
// onto a half-delivered open initiative), then spawns — close-then-spawn.
func (c *routePREventKong) routeCommentReply(ctx *cli.Context, event PREvent) error {
	result, err := matchClosedReviewInitiative(ctx, event)
	if err != nil {
		return fmt.Errorf("ateam route-pr-event: comment-reply match: %w", err)
	}
	if result.How == MatchNone {
		fmt.Fprintf(ctx.Stdout, "route-pr-event: comment_reply for %s#%d has no initiative — skipping\n",
			event.Repo, event.PRNumber)
		return nil
	}
	if result.Repo != "" && !repoconfig.Enabled(result.Repo) {
		fmt.Fprintf(ctx.Stdout, "route-pr-event: comment_reply matched closed %s for %s#%d but its repo is disabled (%s); skipping\n",
			result.InitiativeID, event.Repo, event.PRNumber, repoconfig.FileName)
		return nil
	}
	// agent-teams-8st0.7 found the reaped-worktree case; agent-teams-8st0.14
	// changes what happens next: spawn a fresh comment-reply review instead
	// of dropping the reply, since there is no live session this worktree's
	// initiative could ever resume into on its own.
	if !reviewInitiativeWorktreeLive(result.Worktree) {
		fmt.Fprintf(ctx.Stdout, "route-pr-event: comment_reply matched closed %s for %s#%d but its worktree is gone (%s) — spawning a fresh comment-reply review instead of reopening a dead initiative\n",
			result.InitiativeID, event.Repo, event.PRNumber, result.Worktree)
		return c.spawnReviewInitiative(ctx, event, "comment-reply")
	}
	fmt.Fprintf(ctx.Stdout, "route-pr-event: comment_reply matched closed %s for %s#%d — reopening\n",
		result.InitiativeID, event.Repo, event.PRNumber)
	if err := c.runner("reopen", result.InitiativeID); err != nil {
		fmt.Fprintf(ctx.Stdout, "route-pr-event: reopen %s failed (%v) — spawning a fresh comment-reply review instead of dropping\n",
			result.InitiativeID, err)
		return c.spawnReviewInitiative(ctx, event, "comment-reply")
	}
	sendArgs := []string{"mail", "send", result.InitiativeID, "--file", c.BodyFile, "--sender", "pr-shepherd",
		"--resume-launch-prompt", "/agent-teams:review-pr " + result.InitiativeID + " comment-reply",
		"--resume-model", "sonnet"}
	if err := c.runner(sendArgs...); err != nil {
		// Compensating close: leaving the initiative open would capture ALL
		// future events for this PR on the open-match branch (and fail the
		// same way). Restore the closed state so later events re-match via
		// matchClosedReviewInitiative. Best-effort — a close failure only
		// costs us the same zombie we'd otherwise have.
		if closeErr := c.runner("close", result.InitiativeID, "--reason", "comment-reply send failed; restoring closed state"); closeErr != nil {
			fmt.Fprintf(ctx.Stdout, "route-pr-event: compensating close of %s also failed (%v) — initiative left open, needs manual close\n",
				result.InitiativeID, closeErr)
		}
		fmt.Fprintf(ctx.Stdout, "route-pr-event: send to %s failed (%v) — spawning a fresh comment-reply review instead of dropping\n",
			result.InitiativeID, err)
		return c.spawnReviewInitiative(ctx, event, "comment-reply")
	}
	return nil
}

// RegisterRouteEventKong registers route-pr-event as a native kong verb onto p.
func RegisterRouteEventKong(p *cli.Parser) {
	p.AddVerb("route-pr-event", "Route a PR event to an owning initiative.", &routePREventKong{runner: defaultAteamRunner})
}

// spawnReviewInitiative handles the SPAWN path (fkr.23): an unowned PR with
// transition=review_requested, plus (agent-teams-8st0.14) the fresh-review
// fallback for a dead-worktree or send-failed re_review or comment_reply. It
// resolves the event repo to a local clone path via a config file at
// <ctx.Home>/review-repos/<repo-key>, where repo-key =
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
//	         --launch-prompt "/agent-teams:review-pr {id}[ mode]" --skip-epic \
//	         --model sonnet --topic reviews
//
// mode is appended to the launch prompt as the review-pr skill's optional
// second argument (SKILL.md) when non-empty — "" for a normal review spawn,
// "comment-reply" so the relaunched session answers reply threads instead of
// running the full review flow.
//
// Registration (one-time, out of band):
//
//	mkdir -p ~/.agent-teams/review-repos
//	echo /abs/path/to/local-clone > ~/.agent-teams/review-repos/<repo-key>
//
// e.g. for MGT-Insurance/midgard (key = "midgard"):
//
//	echo /Users/ericlloyd/Code/midgard > ~/.agent-teams/review-repos/midgard
func (c *routePREventKong) spawnReviewInitiative(ctx *cli.Context, event PREvent, mode string) error {
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
	// --topic ReviewsHandle is what actually removes the noise: this webhook
	// path — not /dispatch-review-pr — spawned every one of the observed
	// single-line per-PR topics, so the shared Reviews topic only wins here.
	launchPrompt := "/agent-teams:review-pr {id}"
	if mode != "" {
		launchPrompt += " " + mode
	}
	runErr := c.runner("dispatch", "--repo", clonePath, "--problem", title, "--body-file", tmpPath,
		"--launch-prompt", launchPrompt, "--skip-epic", "--model", "sonnet",
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

// reviewInitiativeWorktreeLive reports whether a closed review initiative's
// worktree still exists on disk — i.e. whether reopening it could resume a
// real session rather than an inert one (agent-teams-8st0.7). An empty
// worktree (legacy data with no "worktree:" field) is treated the same as a
// reaped one: there is no session to resume either way. Reuses
// defaultDirExists (reap_orphans.go) rather than a second os.Stat wrapper —
// same question ("does this worktree cwd still exist"), same answer.
func reviewInitiativeWorktreeLive(worktree string) bool {
	return worktree != "" && defaultDirExists(worktree)
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
