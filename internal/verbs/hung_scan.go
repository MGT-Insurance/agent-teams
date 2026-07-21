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
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
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
// session, idle, no gate) before hung-scan flags it HUNG. Eric-approved
// default: 15 minutes. Named constant per the bead so the value is tunable
// in one place rather than a scattered magic number.
const hungStuckThreshold = 15 * time.Minute

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
}

// hungAnchor is the durable per-initiative record persisted at
// hungStatePath: when the initiative was first observed STUCK, plus the
// escalation-ladder state for the current STUCK episode. StuckSince is
// written by both `ateam hung-scan` and the periodic relay tick (via
// scanHung, shared by both). AlertedAt/WakeAttempts/LastWakeAt are written
// ONLY by agent-teams-6rru.9's periodic relay tick (hung_tick.go) — the
// tick is the sole writer of the ladder fields, so no lock is needed
// between it and hung-scan's own StuckSince-only writes (scanHung never
// touches these three). Because scanHung carries forward the whole
// hungAnchor struct for a still-STUCK id and drops it entirely once an
// initiative is observed non-STUCK, the ladder fields ride the same
// per-episode lifecycle as StuckSince with no extra bookkeeping: a fresh
// episode always starts with these zero-valued.
type hungAnchor struct {
	StuckSince   string `json:"stuck_since"`
	AlertedAt    string `json:"alerted_at,omitempty"`
	WakeAttempts int    `json:"wake_attempts,omitempty"`
	LastWakeAt   string `json:"last_wake_at,omitempty"`
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
// needed.
func saveHungState(path string, m map[string]hungAnchor) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

// classifyInitiative returns the hung-scan classification for one
// initiative, given its labels, the current live sessions, and its worktree
// path. Mirrors computeExecutionStatus's first-match-wins style
// (status.go), but with a distinct rule set and outcome enum — see the
// package doc comment for why this is a separate verb rather than an
// extension of execution-status.
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
//  3. DEAD  — no live session matches worktree, OR the matched session's
//     PID is nil (tracked-but-dead) — reached only once a real gate has
//     already been ruled out.
//  4. WORKING — matched session is actively working (status=="busy" or
//     state=="working"; same predicate as isActivelyWorking).
//  5. STUCK — everything else: a live session, idle/waiting, no gate.
//
// matched is nil when no session matches worktree (see
// matchSessionByWorktree).
func classifyInitiative(labels []string, sessions []agentSession, worktree string, dirExists dirExistsFunc) (classification string, matched *agentSession, cwdPresent bool) {
	cwdPresent = worktree != "" && dirExists(worktree)
	matched = matchSessionByWorktree(sessions, worktree)
	pidPresent := matched != nil && matched.PID != nil

	if !cwdPresent {
		return hungClassDead, matched, cwdPresent
	}

	hasHuman := hasLabel(labels, "human")
	hasGate := hasLabel(labels, "gate:question") || hasLabel(labels, "gate:review")
	if hasHuman && hasGate {
		return hungClassAwaitingHuman, matched, cwdPresent
	}

	if !pidPresent {
		return hungClassDead, matched, cwdPresent
	}

	if matched.Status == "busy" || matched.State == "working" {
		return hungClassWorking, matched, cwdPresent
	}

	return hungClassStuck, matched, cwdPresent
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
// Graceful degrade: if agentsFunc fails, every entry gets classification
// "unknown" and the anchor-state file is left untouched (we have no
// evidence to justify clearing anchors), mirroring execution-status's
// degrade behavior. scanHung itself only returns an error when the
// underlying `bd list` call fails.
func scanHung(ctx *cli.Context, agentsFunc agentsJSONFunc, now func() time.Time) ([]hungScanEntry, error) {
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
		wt := worktreePath(iss.Description)

		if agentsErr != nil {
			out = append(out, hungScanEntry{
				ID:             iss.ID,
				Title:          iss.Title,
				Worktree:       wt,
				Classification: hungClassUnknown,
			})
			continue
		}

		class, matched, cwdPresent := classifyInitiative(iss.Labels, sessions, wt, defaultDirExists)

		entry := hungScanEntry{
			ID:             iss.ID,
			Title:          iss.Title,
			Worktree:       wt,
			Classification: class,
			CWDPresent:     cwdPresent,
		}
		if matched != nil {
			entry.SessionStatus = matched.Status
			entry.PIDPresent = matched.PID != nil
		}

		if class == hungClassStuck {
			anchor, existed := prevAnchors[iss.ID]
			if !existed || anchor.StuckSince == "" {
				anchor = hungAnchor{StuckSince: nowT.UTC().Format(time.RFC3339)}
			}
			newAnchors[iss.ID] = anchor

			if since, parseErr := time.Parse(time.RFC3339, anchor.StuckSince); parseErr == nil {
				elapsed := nowT.Sub(since)
				entry.StuckSince = anchor.StuckSince
				entry.StuckElapsedSeconds = int64(elapsed.Seconds())
				entry.Hung = elapsed >= hungStuckThreshold
			}
		}

		out = append(out, entry)
	}

	// Anchors for any id not re-added above (non-STUCK this scan, or the
	// initiative closed and dropped out of the open list entirely) are
	// dropped by construction — newAnchors only ever holds this scan's STUCK
	// ids, which is exactly the "cleared on any non-STUCK observation"
	// contract the bead asks for.
	if agentsErr == nil {
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
func (c *hungScanKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam hung-scan: nil context")
	}

	out, err := scanHung(ctx, c.agentsFunc, c.now)
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
