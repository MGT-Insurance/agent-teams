// Package verbs — hung_scan.go implements the `ateam hung-scan` verb
// (agent-teams-6rru.2, Track H — hung detection).
//
// Sessions sometimes hang / stop responding without ever raising a gate, and
// nothing durable detects it: the Steward's nudge is a model eyeballing a
// `claude agents` snapshot each wake, with no anchored notion of "how long"
// and no ground-truth classification. hung-scan joins open initiatives ×
// `claude agents --all --json` (defaultAgentsJSONAll) × gate labels and
// classifies each open initiative as WORKING, AWAITING-HUMAN, DEAD, or
// STUCK, persisting a durable "stuck-since" anchor so a STUCK initiative
// crossing hungStuckThreshold is flagged HUNG in the output.
//
// This is detection + the durable anchor + the verb ONLY. Consuming and
// escalating HUNG is the sibling bead (agent-teams-6rru.3, Steward
// SKILL.md); a periodic relay tick reusing this same core is
// agent-teams-6rru.9 — see scanHung below, the reusable entry point both
// depend on.
package verbs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// RegisterHungScanKong registers the hung-scan verb onto p.
func RegisterHungScanKong(p *cli.Parser) {
	p.AddVerb("hung-scan", "Emit JSON array of open initiatives classified for hung-session detection.", &hungScanKong{
		agentsFunc: defaultAgentsJSONAll,
		now:        time.Now,
	})
}

// Classification values for hungScanEntry.Classification.
const (
	hungClassWorking       = "WORKING"
	hungClassAwaitingHuman = "AWAITING-HUMAN"
	hungClassDead          = "DEAD"
	hungClassStuck         = "STUCK"
	hungClassUnknown       = "unknown" // graceful-degrade value; lower-case to mirror execution-status's "unknown"
)

// hungStuckThreshold is how long an initiative must stay STUCK (live
// session, idle, no gate) before hung-scan flags it HUNG.
//
// A var, not a const, and set by loadHungConfig (hung_config.go) at process
// start — see that file for the env/file/default resolution and the
// operator-facing key name.
var hungStuckThreshold = defaultHungStuckThreshold

// hungStateFileName is the JSON file (under StewardHome) hung-scan persists
// its per-initiative stuck-since anchors to.
const hungStateFileName = "hung-state.json"

// hungScanEntry is the per-initiative entry in the emitted JSON array.
//
// Emitted shape:
//
//	{
//	  "id":                     "at-abc",
//	  "title":                  "...",
//	  "worktree":                "/path/to/wt",
//	  "classification":          "STUCK",
//	  "hung":                    true,
//	  "stuck_since":             "2026-07-21T10:00:00Z",
//	  "stuck_elapsed_seconds":   1234,
//	  "session_status":          "idle",
//	  "pid_present":             true,
//	  "cwd_present":             true
//	}
//
// stuck_since/stuck_elapsed_seconds are zero-value ("" / 0) whenever
// classification != STUCK. session_status is "" and pid_present is false
// when no live session matches the initiative's worktree.
type hungScanEntry struct {
	ID                  string `json:"id"`
	Title               string `json:"title"`
	Worktree            string `json:"worktree"`
	Classification      string `json:"classification"`
	Hung                bool   `json:"hung"`
	StuckSince          string `json:"stuck_since"`
	StuckElapsedSeconds int64  `json:"stuck_elapsed_seconds"`
	SessionStatus       string `json:"session_status"`
	PIDPresent          bool   `json:"pid_present"`
	CWDPresent          bool   `json:"cwd_present"`

	// Mode is the initiative's "mode: bg|interactive" routing field
	// (initiative.Fields.Mode). Empty for legacy initiatives predating the
	// field. D5: mode=="interactive" excludes an initiative from every
	// mechanical escalation path below, regardless of classification.
	Mode string `json:"mode,omitempty"`

	// D4 — DEAD-with-worktree-present ladder fields. Populated only when
	// Classification==DEAD and CWDPresent==true; zero-valued otherwise,
	// mirroring how StuckSince/StuckElapsedSeconds/Hung are only meaningful
	// for STUCK above.
	DeadSince          string `json:"dead_since,omitempty"`
	DeadElapsedSeconds int64  `json:"dead_elapsed_seconds,omitempty"`
	DeadHung           bool   `json:"dead_hung,omitempty"`

	// D1/D2/D9 — work-product clock + trip gating. WorkProductLastProgress
	// is "" (unknown) whenever no git/bead signal was available at all
	// (e.g. no real git worktree could be probed and the bead has no
	// updated_at) — callers must not treat that as "flat forever".
	WorkProductLastProgress string `json:"wp_last_progress_at,omitempty"`
	WorkProductFlatSeconds  int64  `json:"wp_flat_seconds,omitempty"`
	WorkProductTripEligible bool   `json:"wp_trip_eligible,omitempty"`
	WorkProductDowngraded   bool   `json:"wp_downgraded,omitempty"` // transcript corroborator held it
	FailureTokensFound      bool   `json:"failure_tokens_found,omitempty"`

	// ReviewPRURL is agent-teams-huq7.1 S1's review-shaped predicate:
	// initiative.ReviewPRURL(iss)'s value when iss's Description carries a
	// "pr-url:" field line, "" otherwise. Additive and classification-neutral
	// — it never changes Classification/Hung/DeadHung/WorkProductTripEligible
	// above, it only lets a consumer (the hung tick's S3/S5 backstop) tell a
	// review-shaped initiative apart from every other kind.
	ReviewPRURL string `json:"review_pr_url,omitempty"`

	// Notes is iss.Notes, carried through unconditionally so a consumer (the
	// hung tick's hasReviewPostedNote gate, S2) can evaluate the
	// review-posted signal without a second bd fetch. Deliberately excluded
	// from the wire format (json:"-") — Notes is free-form prose that can
	// grow large and isn't part of this verb's documented emitted shape; it
	// is an in-process-only convenience field for this package's own
	// consumers.
	Notes string `json:"-"`
}

// hungAnchor is the durable per-initiative record persisted at
// hungStatePath: when the initiative was first observed STUCK, plus the
// escalation-ladder state for the current STUCK episode.
//
// agent-teams-6rru.19: the periodic relay tick (hung_tick.go's doHungTick) is
// the SOLE writer of this file. scanHung is the shared classification+anchor
// engine behind both the tick and the `ateam hung-scan` CLI, but it only
// persists when called with persist=true — the tick path. The CLI path
// (hungScanKong.Run) always calls it with persist=false: it may SEE the
// anchor state (to report StuckSince/elapsed on an already-STUCK id) but
// never writes it. With exactly one writer, there is no concurrent-write race
// to reason about and no lock is needed. This supersedes agent-teams-6rru.18,
// which tracked a residual lost-update race between a CLI scan and a tick
// save; that race no longer exists because the CLI scan never writes.
//
// Because scanHung carries forward the whole hungAnchor struct for a
// still-STUCK id and drops it entirely once an initiative is observed
// non-STUCK, the ladder fields ride the same per-episode lifecycle as
// StuckSince with no extra bookkeeping: a fresh episode always starts with
// these zero-valued.
type hungAnchor struct {
	StuckSince   string `json:"stuck_since"`
	AlertedAt    string `json:"alerted_at,omitempty"`
	WakeAttempts int    `json:"wake_attempts,omitempty"`
	LastWakeAt   string `json:"last_wake_at,omitempty"`

	// D4 — DEAD-with-worktree-present ladder: same shape/semantics as the
	// STUCK fields above (StuckSince/AlertedAt/WakeAttempts/LastWakeAt), but
	// tracking a durable "dead since" episode instead. Kept as distinct
	// fields (not shared with the STUCK ones) so a future change where an
	// initiative could carry both anchors simultaneously doesn't cross-talk;
	// today classification is exclusive so at most one set is ever live.
	DeadSince        string `json:"dead_since,omitempty"`
	DeadAlertedAt    string `json:"dead_alerted_at,omitempty"`
	DeadWakeAttempts int    `json:"dead_wake_attempts,omitempty"`
	DeadLastWakeAt   string `json:"dead_last_wake_at,omitempty"`

	// D1/D3 — durable work-product tracking, independent of session status
	// (a busy/idle blip never touches these). WorkProductStatusHash/At track
	// the combined git-status-porcelain hash across the initiative's unioned
	// worktrees (the one component with no artifact timestamp of its own);
	// WorkProductLastProgressAt is the last computed work-product clock
	// value, persisted so the tick can detect "did progress actually
	// advance since last observation" and reset the D6 ladder fields below
	// when it did (a real work-product change ends the episode; a busy
	// session blip does not).
	WorkProductStatusHash     string `json:"wp_status_hash,omitempty"`
	WorkProductStatusHashAt   string `json:"wp_status_hash_at,omitempty"`
	WorkProductLastProgressAt string `json:"wp_last_progress_at,omitempty"`

	// D6 — work-product-flatline ladder state: distinct pacing from STUCK's
	// (30m wake / 1h direct alert, not STUCK's 15m/attempt-count ladder).
	WorkProductAlertedAt    string `json:"wp_alerted_at,omitempty"`
	WorkProductWakeAttempts int    `json:"wp_wake_attempts,omitempty"`
	WorkProductLastWakeAt   string `json:"wp_last_wake_at,omitempty"`
}

// hungStatePath returns the path to the durable anchor-state file:
// <StewardHome>/hung-state.json. Reuses the existing StewardHome helper
// (steward_seams.go) rather than duplicating the workspace-relative-path
// convention.
func hungStatePath(ctx *cli.Context) string {
	return filepath.Join(StewardHome(ctx), hungStateFileName)
}

// loadHungState reads the anchor-state file at path. Any read/parse error
// (including a not-yet-created file) yields an empty map — the anchor state
// is best-effort persistence, never a hard dependency.
func loadHungState(path string) map[string]hungAnchor {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]hungAnchor{}
	}
	var m map[string]hungAnchor
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]hungAnchor{}
	}
	if m == nil {
		m = map[string]hungAnchor{}
	}
	return m
}

// saveHungState writes m to path as JSON, creating the parent directory if
// needed. The write is atomic: the JSON is written to a temp file in the SAME
// directory and then os.Rename'd over path. Rename is atomic within one
// filesystem, so a concurrent loadHungState reader always observes either the
// complete old file or the complete new one — never the torn, mid-write state
// that would parse-fail and (per loadHungState) collapse to an empty map,
// resetting StuckSince for every currently-STUCK initiative.
//
// A package-level var (not a plain func) so the persist-on-change guard test
// can wrap it to count writes: scanHung persists once per tick unconditionally,
// so counting total writes is the only way to observe doHungTick skipping its
// own redundant second write when the ladder didn't advance.
var saveHungState = func(path string, m map[string]hungAnchor) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "hung-state-*.json")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp state: %w", err)
	}
	// os.CreateTemp makes the file 0o600; match the prior 0o644 so the
	// atomic rewrite doesn't silently tighten the state file's permissions.
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp state: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename state into place: %w", err)
	}
	return nil
}

// classifyInitiative returns the hung-scan classification for one
// initiative, given its labels, the current live sessions, and the
// initiative's bd.Issue (for its worktree: line and session: lines). Mirrors
// computeExecutionStatus's first-match-wins style (status.go), but with a
// distinct rule set and outcome enum — see the package doc comment for why
// this is a separate verb rather than an extension of execution-status.
//
// Evaluation order (first match wins; agent-teams-6rru.13 split DEAD into
// two separate checks straddling the gate check, so a gated-but-not-live
// initiative reports AWAITING-HUMAN instead of DEAD — DEAD used to preempt
// the gate check unconditionally whenever PID was absent, defeating
// AWAITING-HUMAN's entire purpose for exactly the initiatives it exists to
// catch):
//  1. DEAD  — worktree directory missing (orphan).
//  2. AWAITING-HUMAN — labels carry "human" AND ("gate:question" OR
//     "gate:review") — checked regardless of PID presence, since a real gate
//     means the initiative is waiting on the human, not hung.
//  3. DEAD  — no tied session is alive (matchSessionsForInitiative returned
//     no live entry) — reached only once a real gate has already been ruled
//     out.
//  4. WORKING — ANY live tied session is actively working (status=="busy"
//     or state=="working"; same predicate as isActivelyWorking).
//  5. STUCK — everything else: at least one live tied session, all
//     idle/waiting, no gate.
//
// matched is the tied-session set from matchSessionsForInitiative
// (agent-teams-zalv.1 §6): for a session-tied initiative this holds only
// LIVE sessions, primary-first; for a legacy initiative (no session: lines)
// it falls back to the single matchSessionByWorktree result, which — unlike
// the session-set path — may be a PID-nil (tracked-but-dead) session, so
// pidPresent below is computed from the live subset, not len(matched).
func classifyInitiative(labels []string, sessions []agentSession, iss bd.Issue, dirExists dirExistsFunc) (classification string, matched []agentSession, cwdPresent bool) {
	worktree := initiative.Of(iss).Worktree
	cwdPresent = worktree != "" && dirExists(worktree)
	matched = matchSessionsForInitiative(sessions, iss)

	var live []agentSession
	for _, s := range matched {
		if s.PID != nil {
			live = append(live, s)
		}
	}

	if !cwdPresent {
		return hungClassDead, matched, cwdPresent
	}

	hasHuman := hasLabel(labels, "human")
	// hasGateKind (status.go), not hasLabel: a per-PR-gated initiative's
	// label is "gate:review:<pr-url>", not the bare "gate:review" — hasLabel
	// alone never learned the per-PR suffix form, so a correctly-parked
	// per-PR-gated initiative misclassified as DEAD/STUCK and accumulated a
	// false stall alert (agent-teams-ssib.22). Call the same predicate
	// status.go uses for computeExecutionStatus's identical question rather
	// than writing a second implementation of it.
	hasGate := hasGateKind(labels, "gate:question") || hasGateKind(labels, "gate:review")
	if hasHuman && hasGate {
		return hungClassAwaitingHuman, matched, cwdPresent
	}

	if len(live) == 0 {
		return hungClassDead, matched, cwdPresent
	}

	for _, s := range live {
		if s.Status == "busy" || s.State == "working" {
			return hungClassWorking, matched, cwdPresent
		}
	}

	return hungClassStuck, matched, cwdPresent
}

// hasReviewPostedNote reports whether notes (an initiative's bd Notes text)
// has a line beginning "review-posted:" or "comment-replies:" — the exact
// markers plugins/agent-teams/skills/review-pr/SKILL.md's step 10 (L228) and
// comment-reply step 4 (L339) write via `ateam note` once a review or a
// comment-reply round has actually been posted to GitHub. This is the LOCAL,
// no-network S2 signal (agent-teams-huq7.1 S2): the note is the
// authoritative record that WE did our job, so the hung-tick backstop never
// needs to ask GitHub whether a review exists at all — only (via the
// separate S4 probe) whether a LATER comment is still awaiting a reply.
//
// "review-timeout:" is the NO-REVIEW-POSTED case (the skill gave up waiting
// for the diff, SKILL.md's timeout path) and deliberately does not match
// this prefix scan — a timed-out review must not be treated as posted.
//
// This is a line-PREFIX scan, not internal/initiative's frozen field-line
// rule: Notes is free-form prose the skill appends lines like
// "review-posted: <detail>" into (not routing data), so this intentionally
// does not reuse fieldLine's "exact key, single colon, single space"
// grammar from a different package meant for a different kind of text.
func hasReviewPostedNote(notes string) bool {
	for _, line := range strings.Split(notes, "\n") {
		if strings.HasPrefix(line, "review-posted:") || strings.HasPrefix(line, "comment-replies:") {
			return true
		}
	}
	return false
}

// scanHung is the reusable classification+anchor engine behind `ateam
// hung-scan`. Per agent-teams-6rru.9's dep note, a future periodic relay
// tick must reuse this SAME core rather than reimplementing it — the kong
// verb's Run below is a thin wrapper: list open initiatives -> classify ->
// marshal JSON.
//
// agentsFunc and now are injected so callers (and tests) can substitute
// fakes; ctx supplies ctx.BD (to list open initiatives) and ctx.Home (to
// locate the durable anchor-state file via hungStatePath/StewardHome).
//
// persist controls whether this call is allowed to write the anchor-state
// file (agent-teams-6rru.19: single-writer invariant — see hungAnchor's doc
// comment). scanHung always LOADS existing anchors and classifies/reports
// against them regardless of persist, so a read-only caller still sees an
// accurate StuckSince/elapsed for an already-anchored STUCK id; it just never
// calls saveHungState. doHungTick (hung_tick.go) is the only caller that
// passes persist=true; hungScanKong.Run below always passes false.
//
// Graceful degrade: if agentsFunc fails, every entry gets classification
// "unknown" and the anchor-state file is left untouched (we have no
// evidence to justify clearing anchors), mirroring execution-status's
// degrade behavior. scanHung itself only returns an error when the
// underlying `bd list` call fails.
func scanHung(ctx *cli.Context, agentsFunc agentsJSONFunc, now func() time.Time, persist bool) ([]hungScanEntry, error) {
	statePath := hungStatePath(ctx)
	prevAnchors := loadHungState(statePath)

	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--status=open", "--json"); err != nil {
		return nil, fmt.Errorf("list initiatives: %w", err)
	}

	sessions, agentsErr := agentsFunc()

	nowT := now()
	newAnchors := make(map[string]hungAnchor)
	out := make([]hungScanEntry, 0, len(issues))

	for _, iss := range issues {
		f := initiative.Of(iss)
		wt := f.Worktree
		mode := f.Mode

		reviewPRURL, _ := initiative.ReviewPRURL(iss)

		if agentsErr != nil {
			out = append(out, hungScanEntry{
				ID:             iss.ID,
				Title:          iss.Title,
				Worktree:       wt,
				Classification: hungClassUnknown,
				Mode:           mode,
				ReviewPRURL:    reviewPRURL,
				Notes:          iss.Notes,
			})
			continue
		}

		class, matched, cwdPresent := classifyInitiative(iss.Labels, sessions, iss, defaultDirExists)

		entry := hungScanEntry{
			ID:             iss.ID,
			Title:          iss.Title,
			Worktree:       wt,
			Classification: class,
			CWDPresent:     cwdPresent,
			Mode:           mode,
			ReviewPRURL:    reviewPRURL,
			Notes:          iss.Notes,
		}
		if len(matched) > 0 {
			// Report the primary (first tied session) for the diagnostic
			// session_status/pid_present fields — matchSessionsForInitiative
			// returns primary-first (agent-teams-zalv.1 §3).
			entry.SessionStatus = matched[0].Status
			entry.PIDPresent = matched[0].PID != nil
		}

		// newAnchor/keep decide what (if anything) is carried into
		// newAnchors for this id this scan. Exactly one of the STUCK/DEAD-
		// with-worktree/WORKING branches below applies (classification is
		// exclusive); AWAITING-HUMAN and true-DEAD (worktree gone) fall
		// through to keep==false, dropping any anchor entirely — mirroring
		// the original "cleared on any non-STUCK observation" contract, now
		// extended to the new sub-states (D3/D4).
		var newAnchor hungAnchor
		keep := false

		switch {
		case class == hungClassStuck:
			// Unchanged STUCK semantics (backward compat): StuckSince is set
			// fresh only when there was no live StuckSince episode already;
			// otherwise the whole prior anchor carries forward untouched
			// (which also naturally preserves any WorkProduct* sub-state —
			// D3 durability — since we mutate individual fields below rather
			// than replacing the struct).
			anchor, existed := prevAnchors[iss.ID]
			if !existed || anchor.StuckSince == "" {
				anchor.StuckSince = nowT.UTC().Format(time.RFC3339)
				anchor.AlertedAt = ""
				anchor.WakeAttempts = 0
				anchor.LastWakeAt = ""
			}
			// Not DEAD this tick — any stale DEAD sub-episode is over.
			anchor.DeadSince, anchor.DeadAlertedAt, anchor.DeadLastWakeAt = "", "", ""
			anchor.DeadWakeAttempts = 0
			newAnchor = anchor
			keep = true

			if since, parseErr := time.Parse(time.RFC3339, newAnchor.StuckSince); parseErr == nil {
				// agent-teams-bq9y.2: discount real machine-sleep time from
				// the elapsed measurement — a maintenance-sleep span this
				// machine spends looks identical to "the session stopped
				// responding" unless it's subtracted out here.
				elapsed := nowT.Sub(since) - sleptBetween(since, nowT)
				if elapsed < 0 {
					elapsed = 0
				}
				entry.StuckSince = newAnchor.StuckSince
				entry.StuckElapsedSeconds = int64(elapsed.Seconds())
				entry.Hung = elapsed >= hungStuckThreshold
			}

		case class == hungClassDead && cwdPresent:
			// D4: DEAD-with-worktree-present joins the ladder, same
			// tick-observation-based anchor/clear mechanics as STUCK's
			// (not the new durable-artifact style — this is "STUCK's
			// mechanism, applied to DEAD", not a new durability contract).
			anchor, existed := prevAnchors[iss.ID]
			if !existed || anchor.DeadSince == "" {
				anchor.DeadSince = nowT.UTC().Format(time.RFC3339)
				anchor.DeadAlertedAt = ""
				anchor.DeadWakeAttempts = 0
				anchor.DeadLastWakeAt = ""
			}
			// Not STUCK this tick — any stale STUCK sub-episode is over.
			anchor.StuckSince, anchor.AlertedAt, anchor.LastWakeAt = "", "", ""
			anchor.WakeAttempts = 0
			newAnchor = anchor
			keep = true

			if since, parseErr := time.Parse(time.RFC3339, newAnchor.DeadSince); parseErr == nil {
				// agent-teams-bq9y.2: same machine-sleep discount as STUCK
				// above.
				elapsed := nowT.Sub(since) - sleptBetween(since, nowT)
				if elapsed < 0 {
					elapsed = 0
				}
				entry.DeadSince = newAnchor.DeadSince
				entry.DeadElapsedSeconds = int64(elapsed.Seconds())
				entry.DeadHung = elapsed >= hungDeadWorktreeThreshold
			}

		case class == hungClassWorking:
			// D1/D3: the busy-forever gap. No session-status-based anchor
			// applies here (that's the whole point) — only the durable
			// work-product clock below, computed unconditionally so a
			// flickering busy/idle/dead sequence never resets it.
			anchor := prevAnchors[iss.ID]
			anchor.StuckSince, anchor.AlertedAt, anchor.LastWakeAt = "", "", ""
			anchor.WakeAttempts = 0
			anchor.DeadSince, anchor.DeadAlertedAt, anchor.DeadLastWakeAt = "", "", ""
			anchor.DeadWakeAttempts = 0
			newAnchor = anchor
			keep = true
		}

		// D1/D2/D3/D6/D9: work-product clock + trip gating, computed
		// whenever there is a live anchor to carry (STUCK/DEAD-with-
		// worktree/WORKING all qualify) and the initiative isn't
		// mode:interactive (D5 excludes interactive sessions from every
		// mechanical path, including this one).
		if keep && mode != "interactive" {
			worktrees := discoverWorktrees(wt, f.Tracks, iss.ID, hungDirListFunc)
			probes := make([]gitProbeResult, 0, len(worktrees))
			for _, w := range worktrees {
				probes = append(probes, hungGitProbeFunc(w))
			}

			var beadUpdated time.Time
			if t, err := time.Parse(time.RFC3339, iss.UpdatedAt); err == nil {
				beadUpdated = t
			}
			prevHashAt, _ := parseTimeOK(newAnchor.WorkProductStatusHashAt)
			lastProgress, newHash, newHashAt := computeWorkProductClock(probes, beadUpdated, newAnchor.WorkProductStatusHash, prevHashAt, nowT)

			prevLastProgress, hadPrevProgress := parseTimeOK(newAnchor.WorkProductLastProgressAt)
			// WorkProductLastProgressAt is persisted at whole-second
			// precision (time.RFC3339), but lastProgress is freshly
			// recomputed every tick at full (sub-second) precision from
			// real git index/commit mtimes. Truncate to second precision
			// before comparing so an unchanged flatline doesn't alias as
			// "progress" against its own truncated prior self.
			if !lastProgress.IsZero() && (!hadPrevProgress || lastProgress.Truncate(time.Second).After(prevLastProgress)) {
				// Real work-product progress since we last looked — the D6
				// ladder episode (if any) is over.
				newAnchor.WorkProductAlertedAt = ""
				newAnchor.WorkProductWakeAttempts = 0
				newAnchor.WorkProductLastWakeAt = ""
			}
			newAnchor.WorkProductStatusHash = newHash
			if !newHashAt.IsZero() {
				newAnchor.WorkProductStatusHashAt = newHashAt.UTC().Format(time.RFC3339)
			}

			if !lastProgress.IsZero() {
				newAnchor.WorkProductLastProgressAt = lastProgress.UTC().Format(time.RFC3339)
				entry.WorkProductLastProgress = newAnchor.WorkProductLastProgressAt
				// agent-teams-bq9y.2: discount real machine-sleep time —
				// the #1 previously sleep-blind clock (lastProgress is built
				// from external timestamps that don't advance during sleep,
				// so a suspend inflated this exactly like STUCK/DEAD above).
				flat := nowT.Sub(lastProgress) - sleptBetween(lastProgress, nowT)
				if flat < 0 {
					flat = 0
				}
				entry.WorkProductFlatSeconds = int64(flat.Seconds())

				// Cost gate (signal-survey.md §1c/§4): only reach into the
				// project repo's own bd DB and the session transcript once
				// the cheap git/bead-registry signal already suggests
				// staleness past the flatline threshold.
				if class == hungClassWorking && mode == "bg" && flat >= hungWorkProductFlatThreshold {
					claimed, _ := hungProjectClaimedBeadFunc(wt)

					var recentWork, failureTokens bool
					if len(matched) > 0 && matched[0].SessionID != "" {
						recentWork, failureTokens, _ = hungTranscriptTailFunc(matched[0].CWD, matched[0].SessionID, nowT.Add(-hungTranscriptCorroboratorWindow))
					}
					entry.FailureTokensFound = failureTokens

					wouldTripIgnoringCorroborator := workProductTripEligible(mode, iss.Labels, claimed, flat, false)
					eligible := workProductTripEligible(mode, iss.Labels, claimed, flat, recentWork)
					entry.WorkProductTripEligible = eligible
					entry.WorkProductDowngraded = wouldTripIgnoringCorroborator && !eligible
				}
			}
		}

		if keep {
			newAnchors[iss.ID] = newAnchor
		}

		out = append(out, entry)
	}

	// Anchors for any id not re-added above (AWAITING-HUMAN this scan, truly
	// DEAD with no worktree, or the initiative closed and dropped out of the
	// open list entirely) are dropped by construction — newAnchors only ever
	// holds ids classified STUCK, DEAD-with-worktree-present, or WORKING
	// this scan (agent-teams-sgr5: D3/D4 extended "cleared on any
	// non-qualifying observation" from STUCK-only to all three tracked
	// sub-states; a gate or a closed initiative still drops everything,
	// including the D1/D3 work-product clock state, per D3's "a gate...
	// clears it" rule).
	//
	// persist gates the write itself: only the tick path (persist=true) may
	// call saveHungState (agent-teams-6rru.19 single-writer invariant). The
	// CLI path (persist=false) computes newAnchors identically above — so
	// StuckSince/elapsed in the returned entries are still accurate — it just
	// never reaches this write.
	if persist && agentsErr == nil {
		if err := saveHungState(statePath, newAnchors); err != nil {
			fmt.Fprintf(ctx.Stderr, "ateam hung-scan: persist anchor state: %v\n", err)
		}
	}

	return out, nil
}

// ── native kong struct ────────────────────────────────────────────────────

// hungScanKong is the kong-native form of the hung-scan verb. DI fields are
// tagged kong:"-" so kong ignores them; tests substitute fakes without
// touching the struct registration.
type hungScanKong struct {
	agentsFunc agentsJSONFunc   `kong:"-"`
	now        func() time.Time `kong:"-"`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
//
// agent-teams-6rru.19: this CLI path is READ-ONLY — it calls scanHung with
// persist=false, so it never writes hung-state.json. The periodic relay tick
// (hung_tick.go's doHungTick) is the sole writer; see hungAnchor's doc
// comment (hung_scan.go) for the single-writer invariant this maintains.
func (c *hungScanKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam hung-scan: nil context")
	}

	// Must resolve identically to the relay's own load (relay.go), or this
	// verb reports against different thresholds than the relay acts on.
	// Warnings go to Stderr so the JSON on Stdout stays machine-readable.
	loadHungConfig(ctx.Stderr, ctx.Home)

	out, err := scanHung(ctx, c.agentsFunc, c.now, false)
	if err != nil {
		return fmt.Errorf("ateam hung-scan: %w", err)
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("ateam hung-scan: marshal: %w", err)
	}
	fmt.Fprintln(ctx.Stdout, string(raw))
	return nil
}
