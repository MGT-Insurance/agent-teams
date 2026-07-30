// Package verbs — hung_workproduct.go implements the work-product stall
// tripwire redesign (at-jolk, agent-teams-sgr5, D1/D2/D3/D5/D9): a stall
// signal orthogonal to `claude agents` session status, so a bg initiative
// whose tied session reports "busy" forever (waiting on a dead child) is
// still caught once its git/bead artifacts stop moving. See
// research/tripwire-design.md for the full rationale and the numbered
// decisions (D1-D9) this file implements.
package verbs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cost"
)

// ── named thresholds (D2/D6) ──────────────────────────────────────────────────

// The four thresholds below are vars, not consts, and are set by
// loadHungConfig (hung_config.go) at process start — see that file for the
// env/file/default resolution and the operator-facing key names.

// hungWorkProductFlatThreshold is how long an initiative's unioned worktrees
// must show zero work-product change before the work-product path is
// eligible to trip (D2).
var hungWorkProductFlatThreshold = defaultHungWorkProductFlatThreshold

// hungWorkProductAlertThreshold is the flatline duration past which a direct,
// LLM-free Telegram alert fires regardless of steward-wake attempt count
// (D6).
var hungWorkProductAlertThreshold = defaultHungWorkProductAlertThreshold

// hungDeadWorktreeThreshold is how long a DEAD-with-worktree-present
// initiative must stay DEAD before it joins the escalation ladder (D4).
//
// It defaults to the same value as hungStuckThreshold but is NOT written as
// an alias of it. As a const, `= hungStuckThreshold` was a live reference;
// as a var it would be a copy taken at package-init time — i.e. before
// loadHungConfig runs — so it would silently keep the compiled default while
// a configured stuck threshold moved out from under it. Its own key and its
// own literal default (hung_config.go) is the fix, and it also lets an
// operator move the two apart on purpose.
var hungDeadWorktreeThreshold = defaultHungDeadWorktreeThreshold

// hungTranscriptCorroboratorWindow is how far back the transcript-tail
// corroborator looks for real assistant/user work turns (D2) and failure
// tokens (D7). It defaults to the same value as
// hungWorkProductFlatThreshold, so that by default "no recent real work" and
// "flat for the threshold" are evaluated over the same window — but it is
// its own key and may be tuned apart from it.
var hungTranscriptCorroboratorWindow = defaultHungTranscriptCorroboratorWindow

// hungFailureTokens are the substrings D7's corroborator greps the
// transcript tail for. Evidence-only (n=1 per signal-survey.md): they
// upgrade wake urgency/evidence, never trigger a wake on their own.
var hungFailureTokens = []string{
	`status="killed"`,
	`status="failed"`,
	"Exit code 143",
	"command timed out",
	"API Error: Connection closed",
}

// ── package-level seams ────────────────────────────────────────────────────────
//
// scanHung's public signature (ctx, agentsFunc, now, persist) is unchanged —
// dozens of existing tests call it directly — so the new D1/D2/D9 seams
// below are package-level vars (mirroring saveHungState's existing pattern
// in hung_scan.go), swappable by tests via simple reassignment + defer
// restore, defaulting to the real implementations for production and for
// every test that doesn't care about work-product behavior (against a
// non-git t.TempDir(), the real git/bd probes below fail fast and
// gracefully, contributing no signal — see gitProbeResult.Available).
var (
	hungGitProbeFunc           gitWorkProductProbeFunc = defaultGitWorkProductProbe
	hungProjectClaimedBeadFunc projectClaimedBeadFunc  = defaultProjectHasClaimedBead
	hungTranscriptTailFunc     transcriptTailFunc      = defaultTranscriptTail
	hungDirListFunc            dirListFunc             = defaultDirList
)

// parseTimeOK parses s as RFC3339, returning (zero, false) for an empty or
// unparseable string instead of an error — a small ergonomic wrapper used
// throughout this file's anchor bookkeeping, where "no prior value" and "bad
// prior value" are handled identically (treat as unknown).
func parseTimeOK(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// ── D9: worktree discovery (union of worktree:/track-worktree: + fallback) ───

// dirListFunc lists the names of directory entries under dir (not full
// paths). Injected so the D9 fallback heuristic is testable without a real
// filesystem tree.
type dirListFunc func(dir string) ([]string, error)

// defaultDirList lists the directory-only entries of dir.
func defaultDirList(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// discoverWorktrees unions the primary registered worktree with any explicit
// "track-worktree:" lines (D9). When there are NO explicit track-worktree
// lines (legacy initiatives, predating the D9 convention), it additionally
// applies the documented fallback heuristic: any sibling directory under the
// primary worktree's parent whose name contains initiativeID as a substring
// is treated as an additional worktree to probe. Returns a de-duplicated,
// sorted (for determinism) list; primary is always included first if
// non-empty, though the sort makes final ordering deterministic rather than
// insertion-order.
func discoverWorktrees(primary string, explicit []string, initiativeID string, listDir dirListFunc) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}

	add(primary)
	for _, p := range explicit {
		add(p)
	}

	if len(explicit) == 0 && primary != "" && initiativeID != "" && listDir != nil {
		parent := filepath.Dir(primary)
		names, err := listDir(parent)
		if err == nil {
			for _, name := range names {
				if strings.Contains(name, initiativeID) {
					add(filepath.Join(parent, name))
				}
			}
		}
	}

	sort.Strings(out)
	return out
}

// ── D1: git work-product probe ────────────────────────────────────────────────

// gitProbeResult is one worktree's cheaply-probed git state. Available is
// false whenever the path is not a readable git worktree at all (a bare temp
// dir, a removed worktree, `git` not on PATH) — callers must exclude an
// unavailable probe from the work-product clock entirely rather than
// treating its zero-valued fields as "no change since epoch".
type gitProbeResult struct {
	Available  bool
	IndexMtime time.Time // zero if unknown (e.g. index file missing)
	CommitTime time.Time // zero if no commits yet
	StatusHash string    // sha256 hex of `git status --porcelain` output; "" if Available is false
}

// gitWorkProductProbeFunc probes one worktree's git state. Injected so tests
// substitute a fake without a real git subprocess/repo.
type gitWorkProductProbeFunc func(worktree string) gitProbeResult

// gitProbeTimeout bounds every individual git subprocess this file execs
// (review fix, agent-teams-sgr5.6): these ticks run machine-wide and
// unattended, so one wedged git (lock contention, a stale/dead network
// mount under the worktree) must never stall the whole tick. On timeout the
// call fails exactly like any other exec error and the affected component
// degrades to no-signal — never a fabricated flatline or fresh progress.
const gitProbeTimeout = 5 * time.Second

// gitIndexPathCache memoizes the resolved absolute index-file path per
// worktree (review fix, agent-teams-sgr5.6): `git rev-parse --git-path
// index` only changes if the worktree is reconfigured (rare), so re-running
// it every hungTickInterval for the lifetime of an initiative is wasted
// subprocess cost. Guarded by gitIndexPathCacheMu since the relay's
// tick goroutine and any concurrent `ateam hung-scan` CLI invocation could
// in principle call the probe from more than one goroutine in the same
// process.
var (
	gitIndexPathCacheMu sync.Mutex
	gitIndexPathCache   = map[string]string{}
)

// resolveGitIndexPath returns the absolute index-file path for worktree,
// using gitIndexPathCache when the previously-resolved path still exists on
// disk. A cached path that no longer os.Stat's (worktree removed/moved,
// index file relocated) is treated as stale and re-resolved via `git
// rev-parse --git-path index`, bounded by ctx. Returns "" if resolution
// fails or times out — callers must treat that as no signal, never as "no
// index file" (which would be a false flatline).
func resolveGitIndexPath(ctx context.Context, worktree string) string {
	gitIndexPathCacheMu.Lock()
	cached, ok := gitIndexPathCache[worktree]
	gitIndexPathCacheMu.Unlock()
	if ok {
		if _, err := os.Stat(cached); err == nil {
			return cached
		}
		// Stale: the cached path no longer exists (worktree removed/moved,
		// or the index file itself relocated). Fall through and re-resolve.
	}

	indexRelOut, err := exec.CommandContext(ctx, "git", "-C", worktree, "rev-parse", "--git-path", "index").Output()
	if err != nil {
		return ""
	}
	indexPath := strings.TrimSpace(string(indexRelOut))
	if indexPath == "" {
		return ""
	}
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(worktree, indexPath)
	}

	gitIndexPathCacheMu.Lock()
	gitIndexPathCache[worktree] = indexPath
	gitIndexPathCacheMu.Unlock()
	return indexPath
}

// defaultGitWorkProductProbe runs three cheap, LOCAL git subprocesses against
// worktree (never touches the global bd workspace or the network), each
// bounded by gitProbeTimeout so a wedged git process degrades this
// component to no-signal instead of stalling the tick:
//
//  1. `git status --porcelain` — doubles as the availability check (a
//     non-repo or missing directory fails this first, cheaply) and gives the
//     porcelain text hashed into StatusHash.
//  2. `git rev-parse --git-path index` — resolves the REAL index file for
//     this worktree (worktrees have a `.git` file pointing at a per-worktree
//     index under the shared repo's `.git/worktrees/<name>/index`; rev-parse
//     resolves this correctly without hand-parsing the pointer file), then
//     os.Stat's it for ModTime. The resolution itself is cached per worktree
//     (resolveGitIndexPath) so this subprocess only actually runs once per
//     worktree in the common case, not every tick.
//  3. `git log -1 --format=%ct` — last commit's Unix timestamp; empty output
//     (repo with zero commits) leaves CommitTime zero, not an error.
func defaultGitWorkProductProbe(worktree string) gitProbeResult {
	statusCtx, statusCancel := context.WithTimeout(context.Background(), gitProbeTimeout)
	defer statusCancel()
	statusOut, err := exec.CommandContext(statusCtx, "git", "-C", worktree, "status", "--porcelain").Output()
	if err != nil {
		return gitProbeResult{}
	}
	result := gitProbeResult{Available: true, StatusHash: sha256Hex(statusOut)}

	indexCtx, indexCancel := context.WithTimeout(context.Background(), gitProbeTimeout)
	defer indexCancel()
	if indexPath := resolveGitIndexPath(indexCtx, worktree); indexPath != "" {
		if info, statErr := os.Stat(indexPath); statErr == nil {
			result.IndexMtime = info.ModTime()
		}
	}

	commitCtx, commitCancel := context.WithTimeout(context.Background(), gitProbeTimeout)
	defer commitCancel()
	if commitOut, err := exec.CommandContext(commitCtx, "git", "-C", worktree, "log", "-1", "--format=%ct").Output(); err == nil {
		if s := strings.TrimSpace(string(commitOut)); s != "" {
			if sec, parseErr := strconv.ParseInt(s, 10, 64); parseErr == nil {
				result.CommitTime = time.Unix(sec, 0).UTC()
			}
		}
	}

	return result
}

// sha256Hex returns the hex-encoded sha256 digest of data.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// combinedStatusHash deterministically hashes the StatusHash of every
// available probe together (sorted by worktree path — probes is expected to
// already be in that order per discoverWorktrees), so "did ANY unioned
// worktree's uncommitted state change since last tick" collapses to one
// per-initiative hash rather than one per worktree. Returns "" if no probe
// is Available (nothing to hash — the caller must treat this as "unknown",
// not as "unchanged").
func combinedStatusHash(probes []gitProbeResult) string {
	var parts []string
	any := false
	for _, p := range probes {
		if p.Available {
			any = true
		}
		parts = append(parts, p.StatusHash)
	}
	if !any {
		return ""
	}
	return sha256Hex([]byte(strings.Join(parts, "|")))
}

// maxTime returns the latest non-zero time.Time among ts, or the zero value
// if every input is zero.
func maxTime(ts ...time.Time) time.Time {
	var max time.Time
	for _, t := range ts {
		if t.After(max) {
			max = t
		}
	}
	return max
}

// computeWorkProductClock is the pure D1/D3 core: given this tick's git
// probes (already discovered/unioned per D9) and the initiative's bd
// updated_at, plus the PREVIOUS tick's persisted combined-status-hash state,
// it returns:
//
//   - lastProgress: the newest work-product artifact timestamp across every
//     available probe's index mtime and commit time, the git-status-hash's
//     durably-tracked change time, and beadUpdatedAt. Zero if no signal at
//     all is available (git unreadable everywhere AND no bead update) —
//     callers MUST treat a zero lastProgress as "unknown", never as "flat
//     since the Unix epoch".
//   - newHash/newHashAt: the combined-status-hash state to persist for next
//     tick's comparison. newHashAt only advances to now when the combined
//     hash actually changed (or this is the first observation with a real
//     hash) — everything else (index mtime, commit time, bead updated_at)
//     is already a real wall-clock artifact timestamp needing no history.
//
// This function does zero I/O — it is the unit-tested core; the tick/scan
// loop supplies the probes, bead time, and prior state.
func computeWorkProductClock(probes []gitProbeResult, beadUpdatedAt time.Time, prevHash string, prevHashAt time.Time, now time.Time) (lastProgress time.Time, newHash string, newHashAt time.Time) {
	var indexMax, commitMax time.Time
	for _, p := range probes {
		if !p.Available {
			continue
		}
		if p.IndexMtime.After(indexMax) {
			indexMax = p.IndexMtime
		}
		if p.CommitTime.After(commitMax) {
			commitMax = p.CommitTime
		}
	}

	hash := combinedStatusHash(probes)
	var hashAt time.Time
	switch {
	case hash == "":
		// No probe was Available this tick — nothing observed.
	case prevHash == "":
		// First-ever observation of a real hash for this initiative: we
		// cannot tell whether the current uncommitted state is fresh or has
		// sat untouched for hours (there is no prior hash to compare
		// against), so this component deliberately contributes NOTHING to
		// lastProgress this tick — leaving hashAt zero lets the real
		// artifact timestamps (index mtime/commit time/bead updated_at)
		// carry the signal instead of vacuously stamping "now". The hash
		// itself is still persisted (below) so the NEXT tick has something
		// real to compare against.
	case hash != prevHash:
		hashAt = now
	default:
		hashAt = prevHashAt
	}
	// hash == "" means no probe was Available this tick — do not persist a
	// hash (nothing observed), and do not contribute a timestamp: carrying
	// prevHash/prevHashAt forward unchanged would be wrong too (they may
	// describe a worktree that no longer exists), so this tick simply
	// contributes nothing from the git-status-hash component.
	if hash == "" {
		newHash = prevHash
		newHashAt = prevHashAt
	} else {
		newHash = hash
		newHashAt = hashAt
	}

	lastProgress = maxTime(indexMax, commitMax, hashAt, beadUpdatedAt)
	return lastProgress, newHash, newHashAt
}

// ── D2: trip gating ────────────────────────────────────────────────────────────

// hasHumanAnyGateLabel reports whether labels carry both "human" and ANY
// "gate:*" label — the general form D2 asks for, broader than
// classifyInitiative's specific gate:question/gate:review pair (which only
// drives the AWAITING-HUMAN classification). A future gate kind not yet
// wired into classifyInitiative still exempts the work-product trip here.
func hasHumanAnyGateLabel(labels []string) bool {
	if !hasLabel(labels, "human") {
		return false
	}
	for _, l := range labels {
		if strings.HasPrefix(l, "gate:") {
			return true
		}
	}
	return false
}

// workProductTripEligible is the pure D2 gate: mode must be bg, no
// human+gate:* label, the project repo must have a claimed in-progress bead,
// the flatline must have crossed hungWorkProductFlatThreshold, and the
// transcript-tail corroborator must NOT have found recent real work turns
// (recentWorkTurns==true downgrades/holds the trip).
func workProductTripEligible(mode string, labels []string, projectHasClaimedBead bool, flatline time.Duration, recentWorkTurns bool) bool {
	if mode != "bg" {
		return false
	}
	if hasHumanAnyGateLabel(labels) {
		return false
	}
	if !projectHasClaimedBead {
		return false
	}
	if flatline < hungWorkProductFlatThreshold {
		return false
	}
	if recentWorkTurns {
		return false
	}
	return true
}

// ── project-repo claimed-bead check (cost-gated: only called once the cheap
// git-flatline check already suggests staleness — see scanHung) ────────────

// projectClaimedBeadFunc reports whether the project repo at worktree has at
// least one claimed (assignee non-empty) in_progress bead. Injected so tests
// avoid a real bd subprocess.
type projectClaimedBeadFunc func(worktree string) (bool, error)

// defaultProjectHasClaimedBead runs `bd -C <worktree> list --status=in_progress
// --json` against the PROJECT repo's own .beads (never the global
// workspace) and reports whether any returned issue has a non-empty
// assignee (bd's contract: `bd update --claim` sets both status=in_progress
// and assignee). A git worktree's .beads resolves to the shared repo DB via
// git-common-dir, so any one of the initiative's unioned worktrees gives the
// same answer — callers pass just the primary.
func defaultProjectHasClaimedBead(worktree string) (bool, error) {
	out, err := exec.Command("bd", "-C", worktree, "list", "--status=in_progress", "--json").Output()
	if err != nil {
		return false, err
	}
	var issues []bd.Issue
	if err := json.Unmarshal(out, &issues); err != nil {
		return false, err
	}
	for _, iss := range issues {
		if iss.Assignee != "" {
			return true, nil
		}
	}
	return false, nil
}

// ── D2/D7: transcript-tail corroborator ───────────────────────────────────────

// transcriptRecord is the subset of one JSONL transcript line this file
// reads: Type discriminates content-typed turns (assistant/user) from
// framing records (system/queue-operation/attachment/task-notification —
// see signal-survey.md §1d), and Content lets us require the turn actually
// carries something (an empty-content record proves nothing).
type transcriptRecord struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// isContentTypedWorkTurn reports whether rec is a real assistant/user turn
// carrying actual content — signal-survey.md §1d's refinement of the
// design doc's looser "not queue/system/heartbeat" wording: only
// type=="assistant" or type=="user" with a non-empty message.content count,
// which structurally excludes "system"/"queue-operation"/"attachment"/
// "task-notification" records (none of which carry a message.content array
// with this shape) without needing to enumerate every framing type by name.
func isContentTypedWorkTurn(rec transcriptRecord) bool {
	if rec.Type != "assistant" && rec.Type != "user" {
		return false
	}
	trimmed := strings.TrimSpace(string(rec.Message.Content))
	return trimmed != "" && trimmed != "null" && trimmed != "[]"
}

// transcriptTailFunc scans a session's transcript for D2's real-work-turn
// corroborator and D7's failure-token corroborator. since bounds the
// work-turn window (hungTranscriptCorroboratorWindow); failure tokens are
// searched across the whole available transcript (cheap grep, evidence-only
// per D7). Injected so tests avoid real ~/.claude/projects files.
type transcriptTailFunc func(sessionCWD, sessionID string, since time.Time) (recentWorkTurns bool, failureTokensFound bool, err error)

// defaultTranscriptTail locates the session transcript at
// ~/.claude/projects/<SlugifyCWD(sessionCWD)>/<sessionID>.jsonl (the same
// convention internal/cost uses for cost attribution) and scans it line by
// line. A missing transcript is normal (session may have no file yet, or the
// slug/session no longer resolves) and returns (false, false, nil) rather
// than an error — this is a best-effort corroborator, never a hard
// dependency of the trip decision.
func defaultTranscriptTail(sessionCWD, sessionID string, since time.Time) (bool, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, false, err
	}
	path := filepath.Join(home, ".claude", "projects", cost.SlugifyCWD(sessionCWD), sessionID+".jsonl")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, false, nil
		}
		return false, false, err
	}

	recentWork := false
	failureFound := false
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		for _, tok := range hungFailureTokens {
			if strings.Contains(line, tok) {
				failureFound = true
				break
			}
		}
		if recentWork {
			continue // still scan remaining lines for failure tokens
		}
		var rec transcriptRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if !isContentTypedWorkTurn(rec) {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, rec.Timestamp)
			if err != nil {
				continue
			}
		}
		if !ts.Before(since) {
			recentWork = true
		}
	}
	return recentWork, failureFound, nil
}

// ── D8: append-only tick journal ──────────────────────────────────────────────

// hungJournalFileName is the JSONL file (under StewardHome, alongside
// hung-state.json) hung-scan/hung-tick append one line to per tick per
// non-WORKING initiative.
const hungJournalFileName = "hung-journal.jsonl"

// hungJournalMaxBytes caps the journal file's size; rotateHungJournal moves
// the file aside to a single ".1" backup once it's exceeded, so the journal
// never grows unbounded but the incident-reconstruction value (this was
// nearly unreconstructable for at-pp7z) is kept bounded rather than
// discarded outright. A var (not a const) so tests can shrink it rather than
// writing 5 MiB of fixtures to exercise rotation.
var hungJournalMaxBytes int64 = 5 * 1024 * 1024 // 5 MiB

// hungJournalEntry is one journal line: classification, clock values, and
// whatever ladder action (if any) this tick decided for this initiative.
type hungJournalEntry struct {
	Timestamp              string `json:"ts"`
	InitiativeID           string `json:"id"`
	Classification         string `json:"classification"`
	Mode                   string `json:"mode,omitempty"`
	StuckElapsedSeconds    int64  `json:"stuck_elapsed_seconds,omitempty"`
	DeadElapsedSeconds     int64  `json:"dead_elapsed_seconds,omitempty"`
	WorkProductFlatSeconds int64  `json:"wp_flat_seconds,omitempty"`
	WorkProductTripped     bool   `json:"wp_tripped,omitempty"`
	Ladder                 string `json:"ladder,omitempty"`        // "stuck" | "dead" | "workproduct" | ""
	LadderAction           string `json:"ladder_action,omitempty"` // "wake" | "alert" | ""
}

// hungJournalPath returns <StewardHome>/hung-journal.jsonl.
func hungJournalPath(home string) string {
	return filepath.Join(home, hungJournalFileName)
}

// rotateHungJournalIfNeeded moves path aside to path+".1" (best-effort,
// overwriting any previous backup) once it exceeds hungJournalMaxBytes. A
// stat/rename failure is swallowed — the journal is diagnostic, never load-
// bearing for the tripwire's own correctness.
func rotateHungJournalIfNeeded(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < hungJournalMaxBytes {
		return
	}
	_ = os.Rename(path, path+".1")
}

// appendHungJournal appends one JSON-marshaled line to path, creating the
// parent directory and file as needed, rotating first if the file has grown
// past the cap. Best-effort: an I/O error here must never block or fail the
// tick itself, so this returns an error for the caller to log, not to
// abort on.
func appendHungJournal(path string, entry hungJournalEntry) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	rotateHungJournalIfNeeded(path)

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}
