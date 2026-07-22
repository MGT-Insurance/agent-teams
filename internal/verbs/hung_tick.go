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
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/transport"
)

// hungTickInterval is how often the relay's ticker goroutine re-runs
// scanHung looking for HUNG initiatives. Eric-approved default: 5 minutes —
// frequent enough that the wake ladder below plays out over a bounded
// number of minutes, infrequent enough not to spam `bd list` or the
// Steward's mailbox.
const hungTickInterval = 5 * time.Minute

// hungWakeAttemptsBeforeDirectAlert caps how many consecutive ticks nudge
// the Steward (mail-send, sender "hung-scan") for one HUNG episode before
// concluding the Steward itself isn't responding and escalating directly: a
// deterministic, LLM-free canned alert posted straight into the
// initiative's own Telegram topic, exactly once per episode.
const hungWakeAttemptsBeforeDirectAlert = 2

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

// postHungAlert resolves entry's own topic and posts the canned alert into
// it. A missing thread ref (no topic yet), a bd lookup failure, or no
// transport configured is returned as an error for the caller to log —
// best-effort, mirroring every other send path in this package; it never
// panics or aborts the tick loop.
func postHungAlert(ctx *cli.Context, deps hungTickDeps, entry hungScanEntry) error {
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
		Body:         hungAlertBody(entry.ID, entry.Title, entry.StuckSince),
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
func doHungTick(ctx *cli.Context, deps hungTickDeps) error {
	entries, err := scanHung(ctx, deps.agentsFunc, deps.now, true)
	if err != nil {
		return fmt.Errorf("hung tick: scan: %w", err)
	}

	statePath := hungStatePath(ctx)
	anchors := loadHungState(statePath)
	nowRFC3339 := deps.now().UTC().Format(time.RFC3339)
	changed := false

	for _, entry := range entries {
		if !entry.Hung {
			continue
		}

		anchor, ok := anchors[entry.ID]
		if !ok {
			// scanHung just classified this entry Hung, so it must have
			// persisted a StuckSince anchor for it; treat a missing one
			// defensively as a fresh episode (no wake attempts yet) rather
			// than skip it outright.
			anchor = hungAnchor{StuckSince: entry.StuckSince}
		}

		updated, action := nextHungLadderAction(anchor, nowRFC3339)

		switch action {
		case hungActionWake:
			body := hungWakeBody(entry.ID, entry.Title, updated.WakeAttempts, entry.StuckSince)
			if err := sendHungWakeEnvelope(ctx, deps.wakeSend, entry.ID, body); err != nil {
				fmt.Fprintf(ctx.Stderr, "ateam relay: hung tick: wake steward for %s failed: %v\n", entry.ID, err)
			}
		case hungActionAlert:
			if err := postHungAlert(ctx, deps, entry); err != nil {
				fmt.Fprintf(ctx.Stderr, "ateam relay: hung tick: post canned alert for %s failed: %v\n", entry.ID, err)
			}
		case hungActionNone:
			// Already alerted this episode — the ladder is unchanged
			// (nextHungLadderAction returned anchor untouched), so skip the
			// write entirely rather than re-saving byte-identical state every
			// tick. Pure write-amplification avoidance, not a race
			// mitigation: this tick is the sole writer of hung-state.json
			// (agent-teams-6rru.19), so there is no concurrent writer to
			// leave a shrinking or widening window for.
			continue
		}

		anchors[entry.ID] = updated
		changed = true
	}

	if changed {
		if err := saveHungState(statePath, anchors); err != nil {
			return fmt.Errorf("hung tick: persist ladder state: %w", err)
		}
	}
	return nil
}

// runHungTick is started as a goroutine from relayKong.Run (relay.go) and
// never returns: it ticks every hungTickInterval and runs doHungTick,
// logging (never panicking) on failure so a transient scan/send error can't
// take down the relay's Receive loop running alongside it.
func runHungTick(ctx *cli.Context, t transport.Transport) {
	deps := hungTickDeps{
		agentsFunc: defaultAgentsJSONAll,
		now:        time.Now,
		wakeSend:   defaultHungWakeSend,
		topicPost:  defaultHungTopicPost,
		transport:  t,
	}
	ticker := time.NewTicker(hungTickInterval)
	defer ticker.Stop()
	for range ticker.C {
		if err := doHungTick(ctx, deps); err != nil {
			fmt.Fprintf(ctx.Stderr, "ateam relay: hung tick: %v\n", err)
		}
	}
}
