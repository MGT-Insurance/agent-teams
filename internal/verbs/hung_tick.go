// Package verbs — hung_tick.go implements the periodic hung-detection tick
// (agent-teams-6rru.9): a goroutine started once from the singleton `ateam
// relay` OS process (relay.go's relayKong.Run) that reuses scanHung
// (hung_scan.go) on a fixed interval and, for each HUNG initiative, drives a
// hybrid escalation ladder — a few wake nudges to the Steward, then a
// deterministic canned alert posted directly into the initiative's own
// Telegram topic if the Steward never clears the episode.
//
// Sessions sometimes hang without ever raising a gate, and the Steward's own
// nudge logic (a model eyeballing a `claude agents` snapshot each wake) has
// no durable, ground-truth notion of "this has been stuck for N minutes and
// nobody has acted." scanHung already computes that ground truth; this file
// is the consumer that acts on it.
//
// agent-teams-bq9y.2 removed the agent-teams-ndr4.2 gap-inference machinery
// that used to live here (detectHungSuspendSpan/shiftHungAnchorsForSuspend,
// keyed off the tick loop's own scheduling gap and a persisted
// hung-tick-meta.json). It's superseded: hung_scan.go's STUCK/DEAD/
// work-product clocks now discount real machine-sleep time directly, via
// sleptBetween (machine_sleep.go) reading `pmset -g log`, which is a ground-
// truth measurement rather than an inference from tick cadence — and,
// unlike the old mechanism, it also covers the work-product flatline clock,
// which ndr4.2 explicitly left out.
package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/sentlog"
	"github.com/mgt-insurance/agent-teams/internal/transport"
)

// hungTickInterval is how often the relay's ticker goroutine re-runs
// scanHung looking for HUNG initiatives: infrequent enough not to spam `bd
// list` or the Steward's mailbox, frequent enough that the wake ladder below
// plays out over a bounded time.
//
// A var, not a const, and set by loadHungConfig (hung_config.go) at process
// start — see that file for the env/file/default resolution and the
// operator-facing key name.
var hungTickInterval = defaultHungTickInterval

// hungWakeAttemptsBeforeDirectAlert caps how many consecutive ticks nudge
// the Steward (mail-send, sender "hung-scan") for one HUNG episode before
// concluding the Steward itself isn't responding and escalating directly: a
// deterministic, LLM-free canned alert posted straight into the
// initiative's own Telegram topic, exactly once per episode. Set by
// loadHungConfig; see hung_config.go.
var hungWakeAttemptsBeforeDirectAlert = defaultHungWakeAttemptsBeforeAlert

// hungLadderAction is what nextHungLadderAction decided to do for one HUNG
// initiative on this tick.
type hungLadderAction int

const (
	hungActionNone hungLadderAction = iota
	hungActionWake
	hungActionAlert
)

// nextHungLadderAction is the pure escalation-ladder decision, split out
// from doHungTick so the attempt-counting/threshold logic is unit-testable
// without any I/O. Given anchor's ladder state carried forward from the
// previous tick, it returns the anchor with ladder fields updated for this
// tick, plus the action the caller should take:
//
//   - already alerted this episode (AlertedAt set) -> hungActionNone, anchor
//     unchanged.
//   - fewer than hungWakeAttemptsBeforeDirectAlert wakes sent so far ->
//     WakeAttempts++, LastWakeAt=now, hungActionWake.
//   - wake attempts exhausted -> AlertedAt=now, hungActionAlert (fires
//     exactly once per episode; every later STUCK tick then falls into the
//     already-alerted branch above until the episode ends and scanHung
//     drops the anchor entirely).
func nextHungLadderAction(anchor hungAnchor, nowRFC3339 string) (hungAnchor, hungLadderAction) {
	if anchor.AlertedAt != "" {
		return anchor, hungActionNone
	}
	if anchor.WakeAttempts < hungWakeAttemptsBeforeDirectAlert {
		anchor.WakeAttempts++
		anchor.LastWakeAt = nowRFC3339
		return anchor, hungActionWake
	}
	anchor.AlertedAt = nowRFC3339
	return anchor, hungActionAlert
}

// nextDeadLadderAction is D4's counterpart to nextHungLadderAction: the same
// attempt-count/pacing mechanics ("existing wake pacing" per the bead),
// operating on the Dead* anchor fields instead of the STUCK ones, so a
// DEAD-with-worktree-present episode gets its own independent ladder state.
func nextDeadLadderAction(anchor hungAnchor, nowRFC3339 string) (hungAnchor, hungLadderAction) {
	if anchor.DeadAlertedAt != "" {
		return anchor, hungActionNone
	}
	if anchor.DeadWakeAttempts < hungWakeAttemptsBeforeDirectAlert {
		anchor.DeadWakeAttempts++
		anchor.DeadLastWakeAt = nowRFC3339
		return anchor, hungActionWake
	}
	anchor.DeadAlertedAt = nowRFC3339
	return anchor, hungActionAlert
}

// nextWorkProductLadderAction is D6's distinct-pacing ladder: unlike the
// attempt-count-driven STUCK/DEAD ladders, both the wake and the direct
// alert are gated on ELAPSED FLATLINE DURATION rather than tick-count, per
// D6's explicit thresholds ("steward wake at 30 min flatline... direct
// Telegram alert if the flatline persists past 1h"). The alert is a hard
// backstop keyed on the 1h threshold alone — it still fires even if, for
// whatever reason, fewer than hungWakeAttemptsBeforeDirectAlert wakes were
// sent (e.g. every wake attempt failed), since D6 frames the alert as an
// unconditional escalation once the flatline crosses 1h, not as "after N
// wakes".
func nextWorkProductLadderAction(anchor hungAnchor, flatline time.Duration, nowRFC3339 string) (hungAnchor, hungLadderAction) {
	if anchor.WorkProductAlertedAt != "" {
		return anchor, hungActionNone
	}
	if flatline >= hungWorkProductAlertThreshold {
		anchor.WorkProductAlertedAt = nowRFC3339
		return anchor, hungActionAlert
	}
	if flatline >= hungWorkProductFlatThreshold && anchor.WorkProductWakeAttempts < hungWakeAttemptsBeforeDirectAlert {
		anchor.WorkProductWakeAttempts++
		anchor.WorkProductLastWakeAt = nowRFC3339
		return anchor, hungActionWake
	}
	return anchor, hungActionNone
}

// hungWakeSendFunc sends a wake nudge to the Steward for a HUNG initiative.
// Mirrors relaySendFunc's shape (relay.go) but with a distinct sender
// identity so the Steward — and anyone reading the mail bead's `notes:
// from:` line — can tell a mechanical nudge from an Eric reply. Injected so
// tests capture calls without a subprocess.
type hungWakeSendFunc func(ctx *cli.Context, file string) error

// defaultHungWakeSend execs `ateam mail send steward --file <file> --sender
// hung-scan` — the same mail-send path relay.go's defaultRelaySend uses,
// with sender "hung-scan" in place of "human".
func defaultHungWakeSend(_ *cli.Context, file string) error {
	cmd := exec.Command("ateam", "mail", "send", StewardHandle, "--file", file, "--sender", "hung-scan")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// hungTopicPostFunc posts the canned HUNG alert directly into an
// initiative's own Telegram topic. Injected so tests substitute a fake
// transport without touching Telegram.
type hungTopicPostFunc func(t transport.Transport, msg transport.OutboundMessage) error

// defaultHungTopicPost sends msg via t, discarding the returned thread ref —
// msg.ThreadRef is already the initiative's known, existing topic, so there
// is nothing new to persist.
func defaultHungTopicPost(t transport.Transport, msg transport.OutboundMessage) error {
	_, err := t.Send(msg)
	return err
}

// hungWakeBody is the Relay->Steward wake-nudge text, folded into
// BuildStewardHungWakeEnvelope(id, body) (steward_seams.go) — a dedicated
// envelope kind the Steward recognizes as a mechanical wake, never an Eric
// reply. The "[hung-scan]" prefix and body wording are what make clear to a
// human reading raw mail that this is a mechanical nudge, not Eric —
// combined with the mail bead's own `--sender hung-scan` metadata
// (defaultHungWakeSend above).
func hungWakeBody(id, title string, attempt int, stuckSince string) string {
	return fmt.Sprintf(
		"[hung-scan] %s (%s) has been STUCK since %s with no gate raised (wake attempt %d/%d). Please check on it.",
		id, title, stuckSince, attempt, hungWakeAttemptsBeforeDirectAlert,
	)
}

// hungAlertBody is the deterministic, LLM-free canned alert posted directly
// into the initiative's own topic once the wake ladder is exhausted.
func hungAlertBody(id, title, stuckSince string) string {
	return fmt.Sprintf(
		"HUNG: %s (%s) has been STUCK since %s and the Steward did not respond to %d wake attempt(s). This is an automated alert — please check on this initiative directly.",
		id, title, stuckSince, hungWakeAttemptsBeforeDirectAlert,
	)
}

// hungDeadWakeBody is D4's wake-nudge text for a DEAD-with-worktree-present
// episode — mirrors hungWakeBody's shape/wording so the Steward recognizes
// the same mechanical-nudge pattern for a different underlying condition.
func hungDeadWakeBody(id, title string, attempt int, deadSince string) string {
	return fmt.Sprintf(
		"[hung-scan] %s (%s) is DEAD (worktree present, no live session) since %s (wake attempt %d/%d). Please check on it.",
		id, title, deadSince, attempt, hungWakeAttemptsBeforeDirectAlert,
	)
}

// hungDeadAlertBody is D4's canned direct alert once the DEAD ladder is
// exhausted.
func hungDeadAlertBody(id, title, deadSince string) string {
	return fmt.Sprintf(
		"HUNG: %s (%s) has been DEAD (worktree present, no live session) since %s and the Steward did not respond to %d wake attempt(s). This is an automated alert — please check on this initiative directly.",
		id, title, deadSince, hungWakeAttemptsBeforeDirectAlert,
	)
}

// workProductEvidence renders D7's failure-token corroborator as a short,
// human-readable evidence suffix — empty when no tokens were found, since
// D7 is severity/evidence-only, never a standalone trigger.
func workProductEvidence(failureTokensFound bool) string {
	if !failureTokensFound {
		return ""
	}
	return " Transcript shows failure tokens (killed/failed/timeout/connection-closed) — likely a genuine stall, not a long-running healthy task."
}

// hungWorkProductWakeBody is D6's wake-nudge text for a work-product
// flatline episode (D1: the busy-forever gap — the tied session may still
// report "busy", so this note explicitly frames it as a work-product signal,
// not a session-idle one).
func hungWorkProductWakeBody(id, title, lastProgressAt string, failureTokensFound bool) string {
	return fmt.Sprintf(
		"[hung-scan] %s (%s) has a flat work product (no git/bead change) since %s — the session may still report busy.%s Please check on it.",
		id, title, lastProgressAt, workProductEvidence(failureTokensFound),
	)
}

// hungWorkProductAlertBody is D6's canned direct alert once the work-product
// flatline crosses hungWorkProductAlertThreshold.
func hungWorkProductAlertBody(id, title, lastProgressAt string, failureTokensFound bool) string {
	return fmt.Sprintf(
		"HUNG: %s (%s) has had a flat work product (no git/bead change) since %s, past the %s direct-alert threshold.%s This is an automated alert — please check on this initiative directly.",
		id, title, lastProgressAt, hungWorkProductAlertThreshold, workProductEvidence(failureTokensFound),
	)
}

// sendHungWakeEnvelope builds the steward-hung-wake envelope for a wake
// nudge, writes it to a temp file, and hands it to send — mirroring
// relay.go's sendEnvelopeToSteward write-temp/send/cleanup shape (that
// helper is a relayKong method; this is a free function since the tick has
// no relayKong receiver).
func sendHungWakeEnvelope(ctx *cli.Context, send hungWakeSendFunc, id, body string) error {
	envelope, err := BuildStewardHungWakeEnvelope(id, body)
	if err != nil {
		return fmt.Errorf("build wake envelope: %w", err)
	}
	tmpPath, err := writeEnvelopeToTemp(envelope)
	if err != nil {
		return fmt.Errorf("write wake envelope temp file: %w", err)
	}
	defer os.Remove(tmpPath)
	return send(ctx, tmpPath)
}

// hungAlertThreadRef resolves the initiative's own Telegram topic via its
// "thread:<ref>" bead label (the same convention notify.go's
// threadLabelValue reads for a normal notify call).
func hungAlertThreadRef(ctx *cli.Context, id string) (string, error) {
	issue, err := bd.ShowIssue(ctx.BD, id)
	if err != nil {
		return "", err
	}
	return threadLabelValue(issue.Labels), nil
}

// postHungAlert resolves entry's own topic and posts body into it. A missing
// thread ref (no topic yet), a bd lookup failure, or no transport configured
// is returned as an error for the caller to log — best-effort, mirroring
// every other send path in this package; it never panics or aborts the tick
// loop. body is caller-supplied (rather than computed here) so the same
// plumbing serves all three ladders (STUCK/DEAD/work-product), each with its
// own canned wording.
func postHungAlert(ctx *cli.Context, deps hungTickDeps, entry hungScanEntry, body string) error {
	threadRef, err := hungAlertThreadRef(ctx, entry.ID)
	if err != nil {
		return fmt.Errorf("look up initiative topic: %w", err)
	}
	if threadRef == "" {
		return fmt.Errorf("initiative has no known topic (thread label) to alert into")
	}
	if deps.transport == nil {
		return fmt.Errorf("no transport configured")
	}
	msg := transport.OutboundMessage{
		InitiativeID: entry.ID,
		ThreadRef:    threadRef,
		Title:        entry.Title,
		Body:         body,
		// KindRelayHung is explicit, never derived from environment: this
		// goroutine runs inside the long-lived `ateam relay` OS process,
		// which inherits a stale CLAUDE_CODE_SESSION_ID from whatever
		// session originally started it. Deriving sender identity from env
		// here would misattribute every hung-alert to that stale session.
		Sender: sentlog.KindRelayHung,
	}
	return deps.topicPost(deps.transport, msg)
}

// ── agent-teams-huq7.1 S3/S5: the review-backstop close action ──────────────
//
// A review-shaped initiative (entry.ReviewPRURL != "", agent-teams-huq7.1
// S1) whose review was already posted (S2's hasReviewPostedNote) and whose
// tied session is DEAD or STUCK-past-threshold is a LEAK, not a hang: the
// finished session trusted a stale "already closed" belief and never
// re-closed (agent-teams-huq7.1's transcript-verified root cause). This
// backstop reaches the terminal state the session itself never reached.

// hungReviewBackstopCloseReason is the note/close-reason text S5 specifies
// verbatim for every backstop auto-close, so every closed initiative's audit
// trail reads identically regardless of which tick or which PR closed it.
const hungReviewBackstopCloseReason = "auto-closed by hung-scan backstop: review posted, session dead/stuck, no pending comment"

// hungCloseFunc closes id, recording reason as both a durable note and the
// bd close reason. Injected into hungTickDeps so tests substitute a fake
// instead of shelling to a real bd binary.
type hungCloseFunc func(ctx *cli.Context, id, reason string) error

// defaultHungClose is hungCloseFunc's real implementation: `bd note <id>
// --file=<tmp>` (reason as durable prose) then `bd close <id>
// --reason=<reason>`. Both calls go through ctx.BD.Run — the raw bd CLI —
// deliberately NOT the `ateam close` verb: S3 explicitly rules out an `ateam
// close` read-back here (DROPPED: the at-7xo2 audit already proved `bd
// close` persists, so there is no silent-no-write bug to guard against), and
// `ateam close` layers on signal-sending/main-update behavior (closeKong.Run,
// kong_converted.go) this mechanical backstop has no business triggering.
func defaultHungClose(ctx *cli.Context, id, reason string) error {
	tmp, err := os.CreateTemp("", "ateam-hung-close-note-*")
	if err != nil {
		return fmt.Errorf("create close-note temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(reason + "\n"); err != nil {
		tmp.Close()
		return fmt.Errorf("write close-note temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close close-note temp file: %w", err)
	}
	if _, err := ctx.BD.Run("note", id, "--file="+tmpPath); err != nil {
		return fmt.Errorf("bd note: %w", err)
	}
	if _, err := ctx.BD.Run("close", id, "--reason="+reason); err != nil {
		return fmt.Errorf("bd close: %w", err)
	}
	return nil
}

// reviewBackstopCloseGateHolds evaluates S3(a)-(c) — the classification-only
// part of the close gate — given values already on entry (all computed by
// scanHung, no I/O here). S3(d), the pending-comment probe, is deliberately
// NOT folded in here: it may require a live GitHub call, so the caller
// (doHungTick) evaluates it separately, only for entries this cheap,
// pure predicate already says are candidates — S4's whole "gated behind
// (a)+(b)+(c)" design.
func reviewBackstopCloseGateHolds(entry hungScanEntry) bool {
	if entry.ReviewPRURL == "" { // (a) review-shaped
		return false
	}
	if !hasReviewPostedNote(entry.Notes) { // (b) review-posted
		return false
	}
	// (c) session DEAD (covers worktree-gone true-DEAD AND worktree-present
	// dead-session — classifyInitiative emits DEAD for both) OR STUCK past
	// hungStuckThreshold. NEVER WORKING, NEVER AWAITING-HUMAN — a live
	// working review, or one already parked on a real gate, is untouched.
	return entry.Classification == hungClassDead || (entry.Classification == hungClassStuck && entry.Hung)
}

// hungReviewBackstopJournalEntry builds the review-backstop journal line
// shared by the close-succeeded and close-failed cases in doHungTick below —
// identical shape, differing only in ladderAction ("close" on success,
// "close-failed" on a closeFunc error — agent-teams-huq7.1 CONTRACT
// AMENDMENT).
func hungReviewBackstopJournalEntry(nowRFC3339 string, entry hungScanEntry, ladderAction string) hungJournalEntry {
	return hungJournalEntry{
		Timestamp:           nowRFC3339,
		InitiativeID:        entry.ID,
		Classification:      entry.Classification,
		Mode:                entry.Mode,
		StuckElapsedSeconds: entry.StuckElapsedSeconds,
		DeadElapsedSeconds:  entry.DeadElapsedSeconds,
		Ladder:              "review-backstop",
		LadderAction:        ladderAction,
	}
}

// ── agent-teams-huq7.1 S4: the pending-comment probe (HUMAN-GATED default:
// included, per Eric's ruling — option (a), keep the probe) ────────────────
//
// S3(d) — "no pending unanswered comment" — can only be answered from
// GitHub: a leaked-done review and one reopened for a genuine pending
// comment are BEAD-INDISTINGUISHABLE (route.go mail-sends to an
// already-OPEN initiative, and a CLOSED->reopen, without leaving any note).
// This probe mirrors SKILL.md's comment-reply step 1 exactly, so its answer
// agrees with what a woken review-pr session would itself compute.

// hungReviewCommentProbeTimeout bounds a single `gh api` call so a hanging
// gh can't stall the tick — mirrors the historical prProbeTimeout (10s,
// agent-teams-p9dm.24) this file's cache/preflight lifecycle is modeled on.
const hungReviewCommentProbeTimeout = 10 * time.Second

// pendingReviewCommentFunc probes whether ownerRepo's PR prNumber has a
// pending, unanswered inline-review-comment thread: a thread where OUR login
// authored a comment AND a later comment by someone else exists. true means
// "someone is still waiting on us" — the backstop must NOT close in that
// case even though the review session itself looks dead. Injected into
// hungTickDeps so tests substitute a canned answer without shelling to gh.
type pendingReviewCommentFunc func(ownerRepo string, prNumber int) (bool, error)

// ghPRComment is the subset of `gh api .../pulls/<n>/comments` fields
// defaultPendingReviewComment needs: enough to group comments into threads
// (id/in_reply_to_id) and order them (user.login/created_at). Mirrors
// SKILL.md comment-reply step 1's own vocabulary (root id, in_reply_to_id).
type ghPRComment struct {
	ID          int64  `json:"id"`
	InReplyToID int64  `json:"in_reply_to_id"`
	CreatedAt   string `json:"created_at"`
	User        struct {
		Login string `json:"login"`
	} `json:"user"`
}

// resolveOurGHLogin trims and validates the raw output of `gh api user -q
// .login` for use as hasPendingCommentThread's "ours" comparison. `gh api
// user` exiting 0 does NOT guarantee a real login: some GitHub App /
// installation-token auth passes `gh auth status` but yields an empty
// string or the literal "null" for `.login`. Either value would make
// hasPendingCommentThread match NO comment as "ours" and silently return
// false ("no pending comment"), which would wrongly authorize a backstop
// close of an initiative that may in fact have a genuine pending
// unanswered comment. Split out as a tiny pure helper (agent-teams-huq7.1
// CONTRACT AMENDMENT, S4 guard) so this guard is unit-testable without
// shelling out to a real gh.
func resolveOurGHLogin(raw string) (string, error) {
	login := strings.TrimSpace(raw)
	if login == "" || login == "null" {
		// S3(d) requires PROOF of no pending comment; a bogus/empty login
		// can never provide that proof, so this degrades to an error
		// (probed=false via hungPendingCommentProbe.evaluate) rather than
		// proceeding with a login that would make every thread look
		// resolved.
		return "", fmt.Errorf("gh api user returned no usable login (got %q)", login)
	}
	return login, nil
}

// defaultPendingReviewComment runs, verbatim from SKILL.md comment-reply
// step 1:
//
//	gh api repos/<owner>/<repo>/pulls/<n>/comments --paginate
//	gh api user -q .login
//
// groups comments by root id (in_reply_to_id if set, else id), and reports
// true iff some thread has a comment from ourLogin AND a LATER comment
// (by created_at) from someone else — i.e. a thread awaiting our reply,
// exactly SKILL.md's own selection rule ("Select threads where our login
// authored at least one comment AND a comment by someone else exists with
// created_at later than our last comment in that thread").
func defaultPendingReviewComment(ownerRepo string, prNumber int) (bool, error) {
	cctx, cancel := context.WithTimeout(context.Background(), hungReviewCommentProbeTimeout)
	defer cancel()

	out, err := exec.CommandContext(cctx, "gh", "api", fmt.Sprintf("repos/%s/pulls/%d/comments", ownerRepo, prNumber), "--paginate").Output()
	if err != nil {
		return false, fmt.Errorf("gh api pulls/comments: %w", err)
	}
	var comments []ghPRComment
	if err := json.Unmarshal(out, &comments); err != nil {
		return false, fmt.Errorf("parse gh api pulls/comments output: %w", err)
	}
	if len(comments) == 0 {
		return false, nil
	}

	loginOut, err := exec.CommandContext(cctx, "gh", "api", "user", "-q", ".login").Output()
	if err != nil {
		return false, fmt.Errorf("gh api user: %w", err)
	}
	ourLogin, err := resolveOurGHLogin(string(loginOut))
	if err != nil {
		return false, err
	}

	return hasPendingCommentThread(comments, ourLogin), nil
}

// hasPendingCommentThread is defaultPendingReviewComment's pure grouping
// logic, split out so it is unit-testable with zero I/O (no real gh call):
// group comments by root id (in_reply_to_id if set, else id), then report
// true iff some thread has a comment from ourLogin AND a LATER comment (by
// created_at) from someone else — SKILL.md comment-reply step 1's own
// selection rule verbatim ("Select threads where our login authored at
// least one comment AND a comment by someone else exists with created_at
// later than our last comment in that thread").
func hasPendingCommentThread(comments []ghPRComment, ourLogin string) bool {
	rootOf := func(c ghPRComment) int64 {
		if c.InReplyToID != 0 {
			return c.InReplyToID
		}
		return c.ID
	}

	// First pass: the latest timestamp WE posted in each thread.
	ourLastByThread := make(map[int64]time.Time)
	weAppearIn := make(map[int64]bool)
	for _, c := range comments {
		if c.User.Login != ourLogin {
			continue
		}
		root := rootOf(c)
		weAppearIn[root] = true
		if createdAt, err := time.Parse(time.RFC3339, c.CreatedAt); err == nil {
			if createdAt.After(ourLastByThread[root]) {
				ourLastByThread[root] = createdAt
			}
		}
	}

	// Second pass: does anyone else have a comment in one of those threads
	// AFTER our last one there?
	for _, c := range comments {
		root := rootOf(c)
		if !weAppearIn[root] || c.User.Login == ourLogin {
			continue
		}
		createdAt, err := time.Parse(time.RFC3339, c.CreatedAt)
		if err != nil {
			continue
		}
		if createdAt.After(ourLastByThread[root]) {
			return true
		}
	}
	return false
}

// defaultHungReviewCommentPreflight checks, once per tick, whether probing
// is possible at all: gh must be on PATH and `gh auth status` must exit 0.
// A non-nil result means the caller skips every pending-comment probe this
// tick (S4's degrade path) — every gate-eligible entry then falls through to
// the existing ladder, exactly as if a pending comment had been found, since
// "no pending comment" can never be PROVEN without a working gh. Mirrors the
// historical prMergePreflight (agent-teams-p9dm.24).
func defaultHungReviewCommentPreflight() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh not found on PATH: %w", err)
	}
	cctx, cancel := context.WithTimeout(context.Background(), hungReviewCommentProbeTimeout)
	defer cancel()
	if err := exec.CommandContext(cctx, "gh", "auth", "status").Run(); err != nil {
		return fmt.Errorf("gh auth status: %w", err)
	}
	return nil
}

// hungPendingCommentStateFileName is the JSON file (under StewardHome)
// caching each PR's last pending-comment probe result, so a still-open,
// still-DEAD/STUCK review-shaped initiative doesn't force a live gh probe on
// every single tick. Mirrors hung-state.json's file-based,
// StewardHome-relative, load-at-tick-start/store-at-tick-end convention
// (hung_scan.go) — the same lifecycle the historical prmerge.go cache used
// for execution-status's merge probe (agent-teams-p9dm.24), reimplemented
// here scoped to this file since that mechanism was dropped, not revived.
const hungPendingCommentStateFileName = "hung-pending-comment-state.json"

// hungPendingCommentTTL bounds how long a cached probe result (either
// answer) is trusted before the next tick re-probes GitHub for that PR. Set
// just under the default hungTickInterval so an ordinary tick cadence always
// re-probes (a genuinely pending comment might get answered between ticks,
// so staleness here is a real cost), while a rapid re-invocation within one
// tick's window (a manual retrigger, or ticks catching up after a suspend)
// doesn't multiply gh calls.
var hungPendingCommentTTL = 15 * time.Minute

// hungPendingCommentEntry is one cached probe result.
type hungPendingCommentEntry struct {
	ProbedAt string `json:"probed_at"`
	Pending  bool   `json:"pending"`
}

// hungPendingCommentCache is hungPendingCommentStateFileName's in-memory
// working set.
type hungPendingCommentCache struct {
	Entries map[string]hungPendingCommentEntry `json:"entries"`
}

// pendingCommentCacheKey identifies one PR for cache purposes.
func pendingCommentCacheKey(ownerRepo string, prNumber int) string {
	return fmt.Sprintf("%s#%d", ownerRepo, prNumber)
}

// hungPendingCommentStatePath returns <StewardHome>/hung-pending-comment-state.json.
func hungPendingCommentStatePath(ctx *cli.Context) string {
	return filepath.Join(StewardHome(ctx), hungPendingCommentStateFileName)
}

// loadHungPendingCommentCache reads the cache file at path. Any read/parse
// error (including a not-yet-created file) yields an empty cache — this is
// best-effort persistence, never a hard dependency, mirroring
// loadHungState.
func loadHungPendingCommentCache(path string) hungPendingCommentCache {
	empty := hungPendingCommentCache{Entries: map[string]hungPendingCommentEntry{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return empty
	}
	var c hungPendingCommentCache
	if err := json.Unmarshal(data, &c); err != nil || c.Entries == nil {
		return empty
	}
	return c
}

// saveHungPendingCommentCache writes c to path as JSON, atomically (temp
// file in the same directory, then os.Rename over the target), mirroring
// saveHungState so a concurrent reader never observes a torn write.
var saveHungPendingCommentCache = func(path string, c hungPendingCommentCache) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir pending-comment state dir: %w", err)
	}
	entries := c.Entries
	if entries == nil {
		entries = map[string]hungPendingCommentEntry{}
	}
	data, err := json.Marshal(hungPendingCommentCache{Entries: entries})
	if err != nil {
		return fmt.Errorf("marshal pending-comment state: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "hung-pending-comment-state-*.json")
	if err != nil {
		return fmt.Errorf("create temp pending-comment state file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp pending-comment state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp pending-comment state: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp pending-comment state: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename pending-comment state into place: %w", err)
	}
	return nil
}

// lookup returns the cached pending value for key and whether it is still
// fresh as of now (within hungPendingCommentTTL). fresh==false means the
// caller must probe: no entry exists, its ProbedAt failed to parse, or its
// TTL has elapsed.
func (c hungPendingCommentCache) lookup(key string, now time.Time, ttl time.Duration) (pending bool, fresh bool) {
	entry, ok := c.Entries[key]
	if !ok {
		return false, false
	}
	probedAt, err := time.Parse(time.RFC3339, entry.ProbedAt)
	if err != nil || now.Sub(probedAt) >= ttl {
		return false, false
	}
	return entry.Pending, true
}

// put records a fresh probe result for key.
func (c *hungPendingCommentCache) put(key string, pending bool, now time.Time) {
	if c.Entries == nil {
		c.Entries = map[string]hungPendingCommentEntry{}
	}
	c.Entries[key] = hungPendingCommentEntry{ProbedAt: now.UTC().Format(time.RFC3339), Pending: pending}
}

// hungPendingCommentProbe bundles S4's per-tick probe lifecycle: the TTL
// cache (loaded lazily, only once some entry actually needs a live probe)
// and the once-per-tick gh preflight (also lazy — a tick with zero
// backstop-close candidates costs nothing extra, mirroring
// agent-teams-p9dm.43's anyNeedsLiveProbe optimization for the analogous
// merge probe). Constructed fresh by doHungTick every tick; flush persists
// the cache file only if this tick actually wrote a new entry.
type hungPendingCommentProbe struct {
	ctx  *cli.Context
	deps hungTickDeps

	cachePath   string
	cache       hungPendingCommentCache
	cacheLoaded bool
	cacheDirty  bool

	preflightChecked bool
	preflightErr     error
}

func newHungPendingCommentProbe(ctx *cli.Context, deps hungTickDeps) *hungPendingCommentProbe {
	return &hungPendingCommentProbe{ctx: ctx, deps: deps, cachePath: hungPendingCommentStatePath(ctx)}
}

// evaluate resolves S3(d) for one gate-eligible entry (the caller must
// already have confirmed reviewBackstopCloseGateHolds). probed==false means
// "could not determine" — the caller must treat that exactly like a pending
// comment: S3(d) requires PROOF of no pending comment, not merely absence of
// proof of one.
func (p *hungPendingCommentProbe) evaluate(entry hungScanEntry) (pending bool, probed bool) {
	if p.deps.pendingReviewComment == nil {
		return false, false
	}
	ownerRepo, prNumber, ok := parsePrURL(entry.ReviewPRURL)
	if !ok {
		return false, false
	}

	if !p.cacheLoaded {
		p.cache = loadHungPendingCommentCache(p.cachePath)
		p.cacheLoaded = true
	}
	now := p.deps.now()
	key := pendingCommentCacheKey(ownerRepo, prNumber)
	if cachedPending, fresh := p.cache.lookup(key, now, hungPendingCommentTTL); fresh {
		return cachedPending, true
	}

	if !p.preflightChecked {
		p.preflightChecked = true
		if p.deps.ghPreflight != nil {
			p.preflightErr = p.deps.ghPreflight()
		}
	}
	if p.preflightErr != nil {
		transport.Logf(p.ctx.Stderr, 0, "ateam relay: hung tick: gh preflight failed, skipping pending-comment probes this tick: %v", p.preflightErr)
		return false, false
	}

	result, err := p.deps.pendingReviewComment(ownerRepo, prNumber)
	if err != nil {
		transport.Logf(p.ctx.Stderr, 0, "ateam relay: hung tick: pending-comment probe for %s failed: %v", entry.ID, err)
		return false, false
	}
	p.cache.put(key, result, now)
	p.cacheDirty = true
	return result, true
}

// flush persists the cache once, at the end of the tick, only if evaluate
// actually wrote a new entry this tick.
func (p *hungPendingCommentProbe) flush() {
	if !p.cacheDirty {
		return
	}
	if err := saveHungPendingCommentCache(p.cachePath, p.cache); err != nil {
		transport.Logf(p.ctx.Stderr, 0, "ateam relay: hung tick: persist pending-comment cache: %v", err)
	}
}

// hungTickDeps bundles the seams doHungTick needs beyond ctx. A fake in
// tests substitutes every field so no subprocess/network/filesystem beyond
// ctx.Home's temp dir is touched.
type hungTickDeps struct {
	agentsFunc agentsJSONFunc
	now        func() time.Time
	wakeSend   hungWakeSendFunc
	topicPost  hungTopicPostFunc
	transport  transport.Transport

	// closeFunc is S5's backstop close action. nil disables the backstop
	// entirely (every review-shaped entry falls through to the existing
	// ladder, unchanged) — the safe default for a caller that hasn't wired
	// it, and how every pre-huq7.4 test in this file keeps working untouched
	// (none of their fixtures carry a "pr-url:" line, so
	// reviewBackstopCloseGateHolds is false regardless).
	closeFunc hungCloseFunc

	// pendingReviewComment and ghPreflight are S4's probe seams (default:
	// included per Eric's ruling). Both nil-safe: a nil pendingReviewComment
	// makes hungPendingCommentProbe.evaluate report probed=false for every
	// entry, which — like a probe or preflight failure — conservatively
	// never authorizes a close.
	pendingReviewComment pendingReviewCommentFunc
	ghPreflight          func() error
}

// doHungTick runs one periodic tick. scanHung (reused, called with
// persist=true) computes this scan's classifications and persists the anchor
// state (StuckSince plus whatever ladder fields it round-trips forward);
// doHungTick then re-loads that same state and applies the escalation ladder
// ONLY to entries scanHung flagged Hung, mutating just the ladder fields
// (WakeAttempts/AlertedAt/LastWakeAt) and re-saving when the ladder actually
// advanced. This second pass is the only place the ladder fields are
// ADVANCED.
//
// agent-teams-6rru.19: this tick is the SOLE writer of hung-state.json. The
// `ateam hung-scan` CLI (hung_scan.go's hungScanKong.Run) always calls
// scanHung with persist=false, so it never writes — there is no concurrent
// writer to race with, and saveHungState's atomicity (agent-teams-6rru.17)
// is now a pure torn-write guard against this single writer's own
// crash-mid-write case, not a mitigation for a lost update. This supersedes
// agent-teams-6rru.18, which tracked that now-eliminated race.
// ladderActionName renders action as the short string the D8 journal
// records ("" for hungActionNone — the common no-op case is deliberately
// left blank rather than a magic "none" literal, per the sentinel-literal
// discipline: absence of an action IS the zero value here, not a token
// masquerading as one).
func ladderActionName(action hungLadderAction) string {
	switch action {
	case hungActionWake:
		return "wake"
	case hungActionAlert:
		return "alert"
	default:
		return ""
	}
}

func doHungTick(ctx *cli.Context, deps hungTickDeps) error {
	entries, err := scanHung(ctx, deps.agentsFunc, deps.now, true)
	if err != nil {
		return fmt.Errorf("hung tick: scan: %w", err)
	}

	statePath := hungStatePath(ctx)
	anchors := loadHungState(statePath)
	nowRFC3339 := deps.now().UTC().Format(time.RFC3339)
	changed := false
	journalPath := hungJournalPath(StewardHome(ctx))
	pendingProbe := newHungPendingCommentProbe(ctx, deps)

	for _, entry := range entries {
		ladder := ""
		ladderAction := ""

		// D5: mode:interactive initiatives are excluded from every
		// mechanical escalation path below (STUCK/DEAD/work-product ladders
		// alike) — they are still classified and journaled above/below for
		// visibility, just never nudged or alerted on.
		if entry.Mode != "interactive" {
			// agent-teams-huq7.1 S3/S5: the review-backstop auto-close. A
			// leaked review-shaped initiative (posted, session dead/stuck)
			// with no pending comment is closed HERE, before the DEAD/STUCK/
			// work-product ladder ever sees it — closing is strictly the
			// minimal change: every other classification, and any
			// review-shaped entry that fails this gate (not posted, pending
			// comment, still WORKING), falls through to the ladder exactly
			// as before this bead.
			if deps.closeFunc != nil && reviewBackstopCloseGateHolds(entry) {
				if pending, probed := pendingProbe.evaluate(entry); probed && !pending {
					if err := deps.closeFunc(ctx, entry.ID, hungReviewBackstopCloseReason); err != nil {
						transport.Logf(ctx.Stderr, 0, "ateam relay: hung tick: backstop close %s failed: %v", entry.ID, err)
						// agent-teams-huq7.1 CONTRACT AMENDMENT: a failing
						// close must still escalate — do NOT journal a
						// successful close (misleading: it didn't happen) and
						// do NOT continue (would silently drop the entry,
						// worse than pre-backstop behavior). Journal an
						// accurate close-failed marker instead and fall
						// through to the existing ladder switch below, exactly
						// as if the gate hadn't held.
						if err := appendHungJournal(journalPath, hungReviewBackstopJournalEntry(nowRFC3339, entry, "close-failed")); err != nil {
							transport.Logf(ctx.Stderr, 0, "ateam relay: hung tick: journal write for %s failed: %v", entry.ID, err)
						}
					} else {
						if err := appendHungJournal(journalPath, hungReviewBackstopJournalEntry(nowRFC3339, entry, "close")); err != nil {
							transport.Logf(ctx.Stderr, 0, "ateam relay: hung tick: journal write for %s failed: %v", entry.ID, err)
						}
						continue
					}
				}
			}

			switch {
			case entry.Hung:
				// STUCK ladder — unchanged mechanics/pacing (backward compat).
				ladder = "stuck"
				anchor, ok := anchors[entry.ID]
				if !ok {
					// scanHung just classified this entry Hung, so it must
					// have persisted a StuckSince anchor for it; treat a
					// missing one defensively as a fresh episode (no wake
					// attempts yet) rather than skip it outright.
					anchor = hungAnchor{StuckSince: entry.StuckSince}
				}
				updated, action := nextHungLadderAction(anchor, nowRFC3339)
				switch action {
				case hungActionWake:
					body := hungWakeBody(entry.ID, entry.Title, updated.WakeAttempts, entry.StuckSince)
					if err := sendHungWakeEnvelope(ctx, deps.wakeSend, entry.ID, body); err != nil {
						transport.Logf(ctx.Stderr, 0, "ateam relay: hung tick: wake steward for %s failed: %v", entry.ID, err)
					}
				case hungActionAlert:
					if err := postHungAlert(ctx, deps, entry, hungAlertBody(entry.ID, entry.Title, entry.StuckSince)); err != nil {
						transport.Logf(ctx.Stderr, 0, "ateam relay: hung tick: post canned alert for %s failed: %v", entry.ID, err)
					}
				}
				if action != hungActionNone {
					anchors[entry.ID] = updated
					changed = true
					ladderAction = ladderActionName(action)
				}

			case entry.DeadHung:
				// D4: DEAD-with-worktree-present ladder — same pacing as
				// STUCK's, independent anchor state.
				ladder = "dead"
				anchor, ok := anchors[entry.ID]
				if !ok {
					anchor = hungAnchor{DeadSince: entry.DeadSince}
				}
				updated, action := nextDeadLadderAction(anchor, nowRFC3339)
				switch action {
				case hungActionWake:
					body := hungDeadWakeBody(entry.ID, entry.Title, updated.DeadWakeAttempts, entry.DeadSince)
					if err := sendHungWakeEnvelope(ctx, deps.wakeSend, entry.ID, body); err != nil {
						transport.Logf(ctx.Stderr, 0, "ateam relay: hung tick: wake steward for %s (dead) failed: %v", entry.ID, err)
					}
				case hungActionAlert:
					if err := postHungAlert(ctx, deps, entry, hungDeadAlertBody(entry.ID, entry.Title, entry.DeadSince)); err != nil {
						transport.Logf(ctx.Stderr, 0, "ateam relay: hung tick: post canned alert for %s (dead) failed: %v", entry.ID, err)
					}
				}
				if action != hungActionNone {
					anchors[entry.ID] = updated
					changed = true
					ladderAction = ladderActionName(action)
				}

			case entry.WorkProductTripEligible:
				// D1/D2/D6: work-product-flatline ladder — elapsed-duration
				// gated (30m wake / 1h alert), not attempt-count gated.
				ladder = "workproduct"
				anchor := anchors[entry.ID] // zero value if absent — safe fallback
				flat := time.Duration(entry.WorkProductFlatSeconds) * time.Second
				updated, action := nextWorkProductLadderAction(anchor, flat, nowRFC3339)
				switch action {
				case hungActionWake:
					body := hungWorkProductWakeBody(entry.ID, entry.Title, entry.WorkProductLastProgress, entry.FailureTokensFound)
					if err := sendHungWakeEnvelope(ctx, deps.wakeSend, entry.ID, body); err != nil {
						transport.Logf(ctx.Stderr, 0, "ateam relay: hung tick: wake steward for %s (work-product) failed: %v", entry.ID, err)
					}
				case hungActionAlert:
					if err := postHungAlert(ctx, deps, entry, hungWorkProductAlertBody(entry.ID, entry.Title, entry.WorkProductLastProgress, entry.FailureTokensFound)); err != nil {
						transport.Logf(ctx.Stderr, 0, "ateam relay: hung tick: post canned alert for %s (work-product) failed: %v", entry.ID, err)
					}
				}
				if action != hungActionNone {
					anchors[entry.ID] = updated
					changed = true
					ladderAction = ladderActionName(action)
				}
			}
		}

		// D8: append-only journal line for every non-WORKING classification,
		// PLUS any WORKING entry this tick actually flagged trip-eligible —
		// a session reporting WORKING throughout (D1's own motivating gap)
		// is exactly the case D8 exists to make reconstructable, so
		// excluding it purely because its classification says "WORKING"
		// would defeat the journal's headline purpose.
		if entry.Classification != hungClassWorking || entry.WorkProductTripEligible {
			je := hungJournalEntry{
				Timestamp:              nowRFC3339,
				InitiativeID:           entry.ID,
				Classification:         entry.Classification,
				Mode:                   entry.Mode,
				StuckElapsedSeconds:    entry.StuckElapsedSeconds,
				DeadElapsedSeconds:     entry.DeadElapsedSeconds,
				WorkProductFlatSeconds: entry.WorkProductFlatSeconds,
				WorkProductTripped:     entry.WorkProductTripEligible,
				Ladder:                 ladder,
				LadderAction:           ladderAction,
			}
			if err := appendHungJournal(journalPath, je); err != nil {
				transport.Logf(ctx.Stderr, 0, "ateam relay: hung tick: journal write for %s failed: %v", entry.ID, err)
			}
		}
	}

	pendingProbe.flush()

	if changed {
		if err := saveHungState(statePath, anchors); err != nil {
			return fmt.Errorf("hung tick: persist ladder state: %w", err)
		}
	}
	return nil
}

// hungTickFunc is the body runHungTick invokes once per tick — a
// package-level seam (mirroring hung_workproduct.go's seams) swappable by
// tests via reassignment + defer restore.
//
// It exists because the ticker is otherwise unobservable: time.NewTicker's
// argument cannot be read back, so asserting hungTickInterval holds a value
// proves only that a variable holds a value, not that the ticker was handed
// it. With this seam a test can configure a small interval through the real
// resolution path and count the ticks that actually arrive.
var hungTickFunc = doHungTick

// runHungTickUntil is started as a goroutine from relayKong.Run (relay.go):
// it ticks every hungTickInterval and runs the tick body, logging (never
// panicking) on failure so a transient scan/send error can't take down the
// relay's Receive loop running alongside it. The stop channel lets the
// caller join the goroutine instead of leaking it — relayKong.Run closes
// stop and waits on a done channel before returning, and tests get the same
// join for free. A leaked ticker goroutine is not harmless here: it
// resolves hungTickFunc at call time, so once a test restored the seam it
// would resume driving the REAL doHungTick — shelling out to bd and git —
// every few milliseconds for the remainder of the test binary.
//
// Production's stop channel is closed only at process shutdown (Receive
// blocks forever until then), so the select below effectively never takes
// that branch in practice.
func runHungTickUntil(ctx *cli.Context, t transport.Transport, stop <-chan struct{}) {
	deps := hungTickDeps{
		agentsFunc:           defaultAgentsJSONAll,
		now:                  time.Now,
		wakeSend:             defaultHungWakeSend,
		topicPost:            defaultHungTopicPost,
		transport:            t,
		closeFunc:            defaultHungClose,
		pendingReviewComment: defaultPendingReviewComment,
		ghPreflight:          defaultHungReviewCommentPreflight,
	}
	ticker := time.NewTicker(hungTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if err := hungTickFunc(ctx, deps); err != nil {
				transport.Logf(ctx.Stderr, 0, "ateam relay: hung tick: %v", err)
			}
		}
	}
}
