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
package verbs

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// hungSuspendGapMultiplier (agent-teams-ndr4.2) is how many multiples of
// hungTickInterval a gap between two consecutive doHungTick invocations must
// exceed before it's treated as a machine suspend rather than an ordinary
// slow tick. Set by loadHungConfig; see hung_config.go.
var hungSuspendGapMultiplier = defaultHungSuspendGapMultiplier

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

// hungTickDeps bundles the seams doHungTick needs beyond ctx. A fake in
// tests substitutes every field so no subprocess/network/filesystem beyond
// ctx.Home's temp dir is touched.
type hungTickDeps struct {
	agentsFunc agentsJSONFunc
	now        func() time.Time
	wakeSend   hungWakeSendFunc
	topicPost  hungTopicPostFunc
	transport  transport.Transport
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

// ── agent-teams-ndr4.2: sleep-aware suspend detection ────────────────────────
//
// scanHung's STUCK/DEAD anchors (hung_scan.go's hungAnchor.StuckSince/
// DeadSince) are wall-clock timestamps, and every "how long has this been
// going on" computation is a plain now.Sub(since) against them. That's
// correct as long as the tick loop itself runs continuously — but when the
// laptop sleeps for hours, the whole `ateam relay` OS process (this tick's
// goroutine included) is SUSPENDED, not merely idle: on wake, the elapsed
// time since the anchor was set is inflated by the entire sleep span, so a
// session that was actually fine (or itself suspended) reads as STUCK/DEAD
// far past the threshold and gets wrongly escalated. This is the root
// trigger of the duplicate-review incident that motivated this bead.
//
// The fix detects a suspend from the TICK LOOP's OWN scheduling gap rather
// than any OS-specific sleep API: runHungTickUntil's real ticker fires every
// exactly hungTickInterval, so if the wall-clock gap between two consecutive
// doHungTick invocations far exceeds that interval (hungSuspendGapMultiplier,
// hung_config.go), the excess can only be explained by the process having
// been suspended for it — nothing that happens inside a single tick body can
// produce that gap. On detecting one, every live STUCK/DEAD anchor is shifted
// forward by the inferred suspend span BEFORE scanHung reads them as
// prevAnchors this tick, so the elapsed math comes out as if the suspend span
// had simply not been observed.
//
// This deliberately does NOT touch the work-product flatline clock
// (hung_workproduct.go's computeWorkProductClock / WorkProductLastProgressAt):
// that clock is recomputed every tick straight from real, external
// timestamps (git index/commit mtimes, the bead's updated_at) rather than
// accumulated against a persisted "since" anchor, so it has no drift for a
// suspend to introduce — there is nothing to shift.

// hungTickMetaFileName is the JSON file (under StewardHome, alongside
// hung-state.json and the journal) that persists the wall-clock time of the
// PREVIOUS doHungTick invocation — the raw material detectHungSuspendSpan
// needs. A file of its own rather than a new field on hungAnchor/hung-state.json:
// it's not per-initiative state, and keeping it separate leaves hungAnchor's
// shape (and the ~15 existing tests that construct map[string]hungAnchor
// directly) untouched.
const hungTickMetaFileName = "hung-tick-meta.json"

// hungTickMetaPath returns the path to that file, mirroring hungStatePath's
// StewardHome-relative convention (hung_scan.go).
func hungTickMetaPath(ctx *cli.Context) string {
	return filepath.Join(StewardHome(ctx), hungTickMetaFileName)
}

// hungTickMeta is hungTickMetaPath's on-disk shape.
type hungTickMeta struct {
	LastTickAt string `json:"last_tick_at"`
}

// loadHungTickMeta reads hungTickMeta at path. Any read/parse error
// (including a not-yet-created file — e.g. the process's very first tick)
// yields a zero-value struct, whose empty LastTickAt tells
// detectHungSuspendSpan there is no baseline to compare against yet:
// best-effort persistence, never a hard dependency, mirroring loadHungState.
func loadHungTickMeta(path string) hungTickMeta {
	data, err := os.ReadFile(path)
	if err != nil {
		return hungTickMeta{}
	}
	var m hungTickMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return hungTickMeta{}
	}
	return m
}

// saveHungTickMeta writes m to path as JSON, creating the parent directory if
// needed. A package-level var (not a plain func), mirroring saveHungState, so
// a future test can substitute it without touching doHungTick. Unlike
// saveHungState this skips the atomic temp-file-rename dance: a crash
// mid-write here just resets the next tick's suspend baseline to "unknown",
// which safely degrades to "no shift applied" for that one tick — a
// materially smaller blast radius than losing every initiative's STUCK/DEAD
// anchors, which is what saveHungState's atomicity guards against.
var saveHungTickMeta = func(path string, m hungTickMeta) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir tick meta dir: %w", err)
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal tick meta: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// detectHungSuspendSpan infers a machine-suspend duration from the wall-clock
// gap between this tick (now) and the previous one (lastTickAtRFC3339, ""
// meaning no previous tick is known — the process's first tick, which is
// always treated as normal cadence). tickInterval and multiplier are
// hungTickInterval/hungSuspendGapMultiplier, passed as parameters so this
// stays a pure, seam-free function to unit test.
//
// Returns the inferred suspend span for the caller to correct for, or 0 if
// the gap is consistent with normal tick cadence (including a gap smaller
// than tickInterval, e.g. an operator-triggered extra scan).
func detectHungSuspendSpan(lastTickAtRFC3339 string, now time.Time, tickInterval time.Duration, multiplier int) time.Duration {
	if lastTickAtRFC3339 == "" {
		return 0
	}
	lastTick, err := time.Parse(time.RFC3339, lastTickAtRFC3339)
	if err != nil {
		return 0
	}
	gap := now.Sub(lastTick)
	threshold := tickInterval * time.Duration(multiplier)
	if gap <= threshold {
		return 0
	}
	suspend := gap - tickInterval
	if suspend < 0 {
		suspend = 0
	}
	return suspend
}

// shiftHungTimestamp adds suspend to the RFC3339 timestamp ts, returning ts
// unchanged if it's empty (no live episode to shift) or unparseable (leave
// corrupt data alone rather than fabricate a time from it).
func shiftHungTimestamp(ts string, suspend time.Duration) string {
	if ts == "" {
		return ts
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Add(suspend).UTC().Format(time.RFC3339)
}

// shiftHungAnchorsForSuspend loads the persisted anchor state at statePath
// and pushes every live STUCK/DEAD since-baseline (StuckSince/DeadSince)
// forward by suspend, then re-saves — run once, BEFORE scanHung reads
// prevAnchors for this tick (hung_scan.go's elapsed := nowT.Sub(since) at
// ~l.425/451), so that computation comes out as if the suspend span had
// simply not been observed. Best-effort: a read/write failure here degrades
// to "no shift applied" (logged by the caller), never blocks the tick.
func shiftHungAnchorsForSuspend(statePath string, suspend time.Duration) error {
	anchors := loadHungState(statePath)
	if len(anchors) == 0 {
		return nil
	}
	for id, anchor := range anchors {
		anchor.StuckSince = shiftHungTimestamp(anchor.StuckSince, suspend)
		anchor.DeadSince = shiftHungTimestamp(anchor.DeadSince, suspend)
		anchors[id] = anchor
	}
	return saveHungState(statePath, anchors)
}

func doHungTick(ctx *cli.Context, deps hungTickDeps) error {
	nowT := deps.now()
	metaPath := hungTickMetaPath(ctx)
	meta := loadHungTickMeta(metaPath)
	if suspend := detectHungSuspendSpan(meta.LastTickAt, nowT, hungTickInterval, hungSuspendGapMultiplier); suspend > 0 {
		if err := shiftHungAnchorsForSuspend(hungStatePath(ctx), suspend); err != nil {
			transport.Logf(ctx.Stderr, 0, "ateam relay: hung tick: shift anchors for suspend (%s): %v", suspend, err)
		}
	}
	if err := saveHungTickMeta(metaPath, hungTickMeta{LastTickAt: nowT.UTC().Format(time.RFC3339)}); err != nil {
		transport.Logf(ctx.Stderr, 0, "ateam relay: hung tick: persist tick meta: %v", err)
	}

	entries, err := scanHung(ctx, deps.agentsFunc, deps.now, true)
	if err != nil {
		return fmt.Errorf("hung tick: scan: %w", err)
	}

	statePath := hungStatePath(ctx)
	anchors := loadHungState(statePath)
	nowRFC3339 := deps.now().UTC().Format(time.RFC3339)
	changed := false
	journalPath := hungJournalPath(StewardHome(ctx))

	for _, entry := range entries {
		ladder := ""
		ladderAction := ""

		// D5: mode:interactive initiatives are excluded from every
		// mechanical escalation path below (STUCK/DEAD/work-product ladders
		// alike) — they are still classified and journaled above/below for
		// visibility, just never nudged or alerted on.
		if entry.Mode != "interactive" {
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
		agentsFunc: defaultAgentsJSONAll,
		now:        time.Now,
		wakeSend:   defaultHungWakeSend,
		topicPost:  defaultHungTopicPost,
		transport:  t,
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
