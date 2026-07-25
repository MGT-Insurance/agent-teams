// This file is owned by LOOP Track A (agent-teams-e3mq.2): the Steward's
// session identity (init), and its decision ledger (record/stats). Reads the
// frozen contract in steward_seams.go read-only — does not edit it.
package verbs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/plugins/agent-teams/templates"
)

// RegisterStewardKong registers the `steward` parent verb (init, ledger
// record, ledger stats) onto p using native kong structs. Track B calls this
// from RegisterAllKong in kong_converted.go.
func RegisterStewardKong(p *cli.Parser) {
	p.AddVerb("steward", "Steward persona: session identity and decision ledger.", &stewardCmd{})
}

// stewardCmd is the kong parent struct for `ateam steward <subcommand>`. No
// Run method: kong's kctx.Run walks the selected leaf up through its parents
// and runs every node that has a Run method, so a Run here would fire on
// every subcommand (same pattern as mailCmd in mail_register.go).
type stewardCmd struct {
	Init   stewardInitKong   `cmd:"" name:"init" help:"Create the Steward session directory and marker file."`
	Start  stewardStartKong  `cmd:"" name:"start" help:"Init, then singleton/orphan-watcher pre-flight checks, then launch the Steward's background session."`
	Ledger stewardLedgerCmd  `cmd:"" name:"ledger" help:"Record and report Steward decisions."`
	Remove stewardRemoveKong `cmd:"" name:"remove" help:"De-steward this machine: remove the session dir and doorbell (ledger/briefing kept unless --purge)."`
}

// stewardLedgerCmd is the kong parent struct for `ateam steward ledger
// <subcommand>`. No Run method — group node only.
type stewardLedgerCmd struct {
	Record stewardLedgerRecordKong `cmd:"" name:"record" help:"Append one decision to the Steward's ledger."`
	Stats  stewardLedgerStatsKong  `cmd:"" name:"stats" help:"Report per-category accept/correct counts from the ledger."`
	Recall stewardLedgerRecallKong `cmd:"" name:"recall" help:"Recall recent decisions for one category (most recent first)."`
}

// ── steward init ──────────────────────────────────────────────────────────────

// stewardInitKong is the kong struct for `ateam steward init`.
type stewardInitKong struct{}

// Run creates the Steward's session directory and marker file (per contract
// paths StewardSessionDir/StewardSessionMarkerPath) and prints the session
// directory — the cwd a human/skill launches the Steward session in.
// Idempotent: an existing directory or marker is left as-is. Delegates to
// stewardInit, the shared core also used by `ateam steward start`
// (steward_start.go) so the two commands' init behavior never diverges.
func (c *stewardInitKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam steward init: nil context")
	}

	sessionDir, err := stewardInit(ctx)
	if err != nil {
		return fmt.Errorf("ateam steward init: %w", err)
	}

	fmt.Fprintln(ctx.Stdout, sessionDir)
	return nil
}

// stewardInit creates the Steward's session directory and marker file (per
// contract paths StewardSessionDir/StewardSessionMarkerPath), returning the
// session directory. Idempotent: an existing directory or marker is left
// as-is. Shared core behind both stewardInitKong.Run (above) and
// stewardStartKong.Run (steward_start.go).
func stewardInit(ctx *cli.Context) (string, error) {
	sessionDir := StewardSessionDir(ctx)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return "", fmt.Errorf("create session dir: %w", err)
	}

	marker := StewardSessionMarkerPath(ctx)
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		content := fmt.Sprintf("created: %s\n", time.Now().UTC().Format(time.RFC3339))
		if err := os.WriteFile(marker, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("write marker: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("stat marker: %w", err)
	}

	if err := installGlobalPrimeMD(ctx, templates.GlobalPrimeMD); err != nil {
		return "", fmt.Errorf("install global PRIME.md: %w", err)
	}

	return sessionDir, nil
}

// globalPrimeMDPath returns $ATEAM_HOME/.beads/PRIME.md — the beads v1.1.0
// (cmd/bd/prime.go) override path: `bd prime`, run against the global
// agent-teams workspace, treats a PRIME.md file there as a total override of
// its default output (which otherwise dumps the entire all-role memory store
// into every session). Not one of the Steward*Path helpers in
// steward_seams.go: that file is the frozen contract other tracks import
// read-only, and this path isn't Steward-specific — it's a property of the
// global workspace as a whole, installed by `ateam steward init` because
// that's the existing one-time workspace-setup hook, not because it's
// steward state.
func globalPrimeMDPath(ctx *cli.Context) string {
	return filepath.Join(ctx.Home, ".beads", "PRIME.md")
}

// globalPrimeSidecarPath returns $ATEAM_HOME/.prime-installed — records the
// sha256 of the template content this tool last wrote to globalPrimeMDPath,
// so a future revision of templates.GlobalPrimeMD can tell "our own earlier
// template, safe to upgrade" apart from "a human edit or unknown origin,
// never touch." Deliberately OUTSIDE .beads/: that directory is bd/dolt-
// managed, and a PRIME.md committed into the memory repo would carry a
// sidecar recorded on one machine that's simply wrong when read on another.
// Sibling to .beads/ at the workspace root instead — the same place
// per-machine local-only state already lives in this workspace (e.g.
// StewardHome, mailbox/), never synced by dolt (which only manages .beads/)
// and never swept into a git commit (this workspace's routine operation
// never runs `git add -A`/commit against itself — see steward_seams.go's
// StewardFallbackMarkerPath doc for the same local-file convention).
func globalPrimeSidecarPath(ctx *cli.Context) string {
	return filepath.Join(ctx.Home, ".prime-installed")
}

// writeSidecar records hash at sidecarPath as its entire contents (plus a
// trailing newline, matching this file's other single-line marker writes —
// see the session marker write in stewardInit above).
func writeSidecar(sidecarPath, hash string) error {
	return os.WriteFile(sidecarPath, []byte(hash+"\n"), 0o644)
}

// readSidecarHash reads the hash recorded at sidecarPath, reporting ok=false
// if the file is absent, unreadable, or empty — any of which just means
// "unknown provenance" to installGlobalPrimeMD's caller, never an error in
// its own right.
func readSidecarHash(sidecarPath string) (hash string, ok bool) {
	data, err := os.ReadFile(sidecarPath)
	if err != nil {
		return "", false
	}
	hash = strings.TrimSpace(string(data))
	return hash, hash != ""
}

// installGlobalPrimeMD writes template to globalPrimeMDPath, creating
// $ATEAM_HOME/.beads if it doesn't already exist (mirrors the
// os.MkdirAll(filepath.Dir(ledgerPath), ...) pattern used by
// stewardLedgerRecordKong.Run below — bd itself guarantees .beads exists for
// any initialized workspace, but callers exercising stewardInit directly
// against a bare temp dir should not have to pre-create it).
//
// Idempotent, and upgrade-aware via globalPrimeSidecarPath's provenance
// record (the sha256 of the template content this tool last wrote):
//
//   - Absent: write the template, record its hash in the sidecar, log
//     "installed: <path>".
//   - Present, content == current template: no-op. If the sidecar is
//     missing or stale it's self-healed silently (no log line) — this is
//     bookkeeping catching up to reality, not a state change worth
//     reporting, and a self-heal write failure is not fatal: the only
//     consequence of a still-missing/stale sidecar is that the NEXT
//     template revision falls back to "unknown provenance" and refuses
//     instead of upgrading — safe, never a data-loss risk.
//   - Present, content != current template: this is either our own earlier
//     shipped template (safe to upgrade) or a human edit / unknown origin
//     (never touch). Distinguished by asking whether the sidecar's recorded
//     hash matches sha256 of what's ON DISK right now: if it does, nobody
//     has touched the file since we wrote it, so it's provably ours and
//     gets overwritten with the new template (sidecar updated, "updated:
//     <path> (shipped template revised)" logged). Otherwise — sidecar
//     missing, or its hash doesn't match on-disk content — provenance is
//     unknown, so the file is left untouched and reported ("note: ...
//     looks like a human edit"), exactly as before this sidecar existed.
//     A machine whose PRIME.md predates this mechanism has no sidecar and
//     always lands here: conservative by design, since we'd rather leave a
//     file alone than clobber one we can't prove we wrote.
//
// `ateam steward init`/`start` always succeeds regardless of which branch
// fires (fail-soft): a divergent PRIME.md — human-edited or of unknown
// origin — must never block steward startup. Genuine I/O errors (can't
// read/write) ARE hard-fail, returned to the caller, matching the other two
// stewardInit steps above.
//
// Notes go to ctx.Stderr, not ctx.Stdout: stewardInitKong.Run's only stdout
// contract is the session directory on its own line
// (TestStewardInit_CreatesSessionDirAndMarker asserts stdout == sessionDir
// exactly), and this is incidental installer chatter, not that command's
// primary output.
//
// template is passed in (rather than reading templates.GlobalPrimeMD
// directly) purely so tests can exercise the upgrade path — an on-disk file
// that's our own earlier template but no longer matches the current one —
// without mutating the embedded package var; stewardInit's only real caller
// always passes templates.GlobalPrimeMD.
func installGlobalPrimeMD(ctx *cli.Context, template string) error {
	target := globalPrimeMDPath(ctx)
	sidecar := globalPrimeSidecarPath(ctx)
	currentHash := sha256Hex([]byte(template))

	existing, err := os.ReadFile(target)
	switch {
	case err == nil:
		if string(existing) == template {
			if recorded, ok := readSidecarHash(sidecar); !ok || recorded != currentHash {
				_ = writeSidecar(sidecar, currentHash)
			}
			return nil
		}

		if recorded, ok := readSidecarHash(sidecar); ok && recorded == sha256Hex(existing) {
			if err := os.WriteFile(target, []byte(template), 0o644); err != nil {
				return fmt.Errorf("write %s: %w", target, err)
			}
			if err := writeSidecar(sidecar, currentHash); err != nil {
				return fmt.Errorf("write %s: %w", sidecar, err)
			}
			fmt.Fprintf(ctx.Stderr, "updated: %s (shipped template revised)\n", target)
			return nil
		}

		fmt.Fprintf(ctx.Stderr, "note: %s differs from the shipped template — leaving it untouched (looks like a human edit)\n", target)
		return nil
	case os.IsNotExist(err):
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
		}
		if err := os.WriteFile(target, []byte(template), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
		if err := writeSidecar(sidecar, currentHash); err != nil {
			return fmt.Errorf("write %s: %w", sidecar, err)
		}
		fmt.Fprintf(ctx.Stderr, "installed: %s\n", target)
		return nil
	default:
		return fmt.Errorf("read %s: %w", target, err)
	}
}

// ── steward remove ────────────────────────────────────────────────────────────

// stewardRemoveKong is the kong struct for `ateam steward remove`, the
// companion to init (agent-teams-e3mq.25): it de-steward a machine by
// removing StewardSessionDir (marker included) and the doorbell, which is
// what disables gate->steward routing going forward (the agent-teams-e3mq.24
// guard in notifyToSteward short-circuits once the marker is gone) and stops
// wake-watcher.sh from recognizing this cwd as the Steward's session. It also
// tears down the singleton `ateam relay` process started by `ateam steward
// start` (agent-teams-5y8a.4, relay_supervise.go) — de-stewarding a machine
// should leave nothing behind still polling the transport.
type stewardRemoveKong struct {
	Purge bool `name:"purge" help:"Also delete the ledger and briefing-thread (default: kept, for relocating the Steward to another machine)."`

	agentsFunc agentsJSONFunc  `kong:"-"`
	killFunc   stewardKillFunc `kong:"-"`
}

// Run removes the Steward's session dir and doorbell (idempotent — nothing
// to remove is success, not an error), keeps the ledger and briefing-thread
// by default (printing their paths — that's the state to carry when moving
// the Steward to another machine), and with --purge deletes those too. It
// also reports (never modifies) the count of unread messages still assigned
// to the Steward handle, so mid-flight mail isn't silently lost, and prints a
// best-effort, non-blocking warning if a live session's cwd matches the
// session dir.
func (c *stewardRemoveKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam steward remove: nil context")
	}

	agentsFunc := c.agentsFunc
	if agentsFunc == nil {
		agentsFunc = defaultAgentsJSONAll
	}
	kill := c.killFunc
	if kill == nil {
		kill = defaultStewardKill
	}

	sessionDir := StewardSessionDir(ctx)
	if sessions, err := agentsFunc(); err == nil {
		if hasLiveSession(sessions, sessionDir) {
			fmt.Fprintf(ctx.Stderr, "ateam steward remove: warning: a live session appears to be running in %s; not blocking removal\n", sessionDir)
		}
	}
	// Best-effort: a failure to query live sessions is not reported or
	// treated as an error — this check never blocks removal.

	if pid := teardownRelay(ctx, kill); pid != 0 {
		fmt.Fprintf(ctx.Stdout, "stopped: relay (pid %d)\n", pid)
	} else {
		fmt.Fprintln(ctx.Stdout, "note: no relay running")
	}

	removedSession, err := removeIfExists(sessionDir)
	if err != nil {
		return fmt.Errorf("ateam steward remove: remove session dir: %w", err)
	}
	doorbell := StewardDoorbellPath(ctx)
	removedDoorbell, err := removeIfExists(doorbell)
	if err != nil {
		return fmt.Errorf("ateam steward remove: remove doorbell: %w", err)
	}

	switch {
	case removedSession && removedDoorbell:
		fmt.Fprintf(ctx.Stdout, "removed: %s\n", sessionDir)
		fmt.Fprintf(ctx.Stdout, "removed: %s\n", doorbell)
	case removedSession:
		fmt.Fprintf(ctx.Stdout, "removed: %s\n", sessionDir)
		fmt.Fprintln(ctx.Stdout, "note: no doorbell file found")
	case removedDoorbell:
		fmt.Fprintln(ctx.Stdout, "note: no session dir found")
		fmt.Fprintf(ctx.Stdout, "removed: %s\n", doorbell)
	default:
		fmt.Fprintln(ctx.Stdout, "nothing to remove: no steward session or doorbell found")
	}

	if c.Purge {
		stewardHome := StewardHome(ctx)
		purged, err := removeIfExists(stewardHome)
		if err != nil {
			return fmt.Errorf("ateam steward remove: purge steward home: %w", err)
		}
		if purged {
			fmt.Fprintf(ctx.Stdout, "purged: %s (ledger and briefing-thread deleted)\n", stewardHome)
		} else {
			fmt.Fprintln(ctx.Stdout, "purge: nothing to purge")
		}
	} else {
		var kept []string
		if _, err := os.Stat(StewardLedgerPath(ctx)); err == nil {
			kept = append(kept, StewardLedgerPath(ctx))
		}
		if _, err := os.Stat(StewardBriefingThreadPath(ctx)); err == nil {
			kept = append(kept, StewardBriefingThreadPath(ctx))
		}
		if len(kept) == 0 {
			fmt.Fprintln(ctx.Stdout, "kept: nothing (no ledger or briefing-thread found)")
		} else {
			fmt.Fprintln(ctx.Stdout, "kept (carry these when relocating the Steward to another machine):")
			for _, p := range kept {
				fmt.Fprintf(ctx.Stdout, "  %s\n", p)
			}
		}
	}

	count, err := countUnreadStewardMessages(ctx)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam steward remove: warning: could not count unread steward messages: %v\n", err)
	} else {
		fmt.Fprintf(ctx.Stdout, "unread steward messages: %d\n", count)
	}

	return nil
}

// removeIfExists removes path (file or directory tree) if it exists,
// reporting whether it existed. A non-existent path is not an error —
// remove is idempotent.
func removeIfExists(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := os.RemoveAll(path); err != nil {
		return false, err
	}
	return true, nil
}

// countUnreadStewardMessages reports the number of unread message beads
// assigned to StewardHandle in the global workspace — the same query
// inboxKong.Run (messaging.go) uses, scoped to StewardHandle rather than a
// caller-resolved recipient.
func countUnreadStewardMessages(ctx *cli.Context) (int, error) {
	var messages []bd.Issue
	if err := ctx.BD.RunJSON(&messages,
		"list",
		"--include-infra",
		"--assignee="+StewardHandle,
		"--exclude-label=read",
		"--status=open",
		"--json",
	); err != nil {
		return 0, err
	}
	return len(filterMessageType(messages)), nil
}

// ── steward ledger record ────────────────────────────────────────────────────

// stewardLedgerRecordKong is the kong struct for `ateam steward ledger record`.
// The `enum:""` tags below are spelled out from the contract's
// StewardLedgerCategory/StewardLedgerVerdict constants (struct tags must be
// string literals, so the constants can't be referenced directly) so the CLI
// rejects an unrecognized value at parse time (exit 2), same as gateKong's
// Kind and lockKong's Action elsewhere in this package.
type stewardLedgerRecordKong struct {
	Category       string `name:"category" required:"" enum:"plan-approval,scope-call,merge-approval,design-fork,unblock-action" help:"Decision category (plan-approval|scope-call|merge-approval|design-fork|unblock-action)."`
	Initiative     string `name:"initiative" required:"" help:"Initiative id the decision concerns."`
	Recommendation string `name:"recommendation" required:"" help:"What the Steward recommended."`
	Verdict        string `name:"verdict" required:"" enum:"accepted,corrected" help:"Outcome: accepted or corrected."`
	Decision       string `name:"decision" help:"What Eric actually decided (REQUIRED when verdict=corrected)."`
}

// Run builds a StewardLedgerRecord, validates it against the contract's
// enums and cross-field rules (defense-in-depth alongside the kong
// `enum:""` tags above — this path also fires when Run is called directly
// in tests, bypassing kong parsing) — this is also where verdict=corrected
// requiring --decision is enforced, via MarshalLine's call to Validate() —
// and appends it as one JSONL line to StewardLedgerPath.
func (c *stewardLedgerRecordKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam steward ledger record: nil context")
	}

	rec := StewardLedgerRecord{
		Timestamp:      time.Now().UTC(),
		Category:       StewardLedgerCategory(c.Category),
		Initiative:     c.Initiative,
		Recommendation: c.Recommendation,
		Verdict:        StewardLedgerVerdict(c.Verdict),
		Decision:       c.Decision,
	}

	line, err := rec.MarshalLine()
	if err != nil {
		return cli.Usagef("ateam steward ledger record: %v", err)
	}

	ledgerPath := StewardLedgerPath(ctx)
	if err := os.MkdirAll(filepath.Dir(ledgerPath), 0o755); err != nil {
		return fmt.Errorf("ateam steward ledger record: create ledger dir: %w", err)
	}

	f, err := os.OpenFile(ledgerPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("ateam steward ledger record: open ledger: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("ateam steward ledger record: append: %w", err)
	}

	fmt.Fprintf(ctx.Stdout, "recorded: %s %s -> %s\n", rec.Category, rec.Initiative, rec.Verdict)
	return nil
}

// ── steward ledger stats ─────────────────────────────────────────────────────

// stewardLedgerStatsKong is the kong struct for `ateam steward ledger stats`.
// Category has no kong `enum:""` tag (unlike Record's --category): kong's
// enum validation runs even when an optional flag is left unset, and
// Category's zero value ("" = no filter) would then need to be added to the
// enum list as a footgun-prone special case. Validated explicitly in Run
// instead — which also covers the direct-Run-call test path that bypasses
// kong parsing entirely.
type stewardLedgerStatsKong struct {
	Category string `name:"category" help:"Restrict to one category (default: all)."`
	JSON     bool   `name:"json" help:"Output stats as JSON."`
}

// stewardCategoryStats is one aggregated row emitted by `steward ledger stats`.
type stewardCategoryStats struct {
	Category   string  `json:"category"`
	Total      int     `json:"total"`
	Accepted   int     `json:"accepted"`
	Corrected  int     `json:"corrected"`
	AcceptRate float64 `json:"accept_rate"`
}

// stewardLedgerCategoryOrder is the fixed display order for stats rows,
// mirroring the contract's StewardLedgerCategory declaration order.
var stewardLedgerCategoryOrder = []StewardLedgerCategory{
	StewardLedgerCategoryPlanApproval,
	StewardLedgerCategoryScopeCall,
	StewardLedgerCategoryMergeApproval,
	StewardLedgerCategoryDesignFork,
	StewardLedgerCategoryUnblockAction,
}

// Run reads StewardLedgerPath and reports per-category accepted/corrected
// counts and accept rate. A missing ledger (Steward never recorded a
// decision yet) is not an error — it reports zero rows. Malformed lines are
// skipped with a warning to stderr rather than failing the whole report.
func (c *stewardLedgerStatsKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam steward ledger stats: nil context")
	}
	if c.Category != "" && !StewardLedgerCategory(c.Category).Valid() {
		return cli.Usagef("ateam steward ledger stats: invalid category %q", c.Category)
	}

	counts := make(map[StewardLedgerCategory]*stewardCategoryStats)

	data, err := os.ReadFile(StewardLedgerPath(ctx))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ateam steward ledger stats: read ledger: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rec, err := ParseStewardLedgerRecord([]byte(line))
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "ateam steward ledger stats: warning: skipping malformed line: %v\n", err)
			continue
		}
		if c.Category != "" && string(rec.Category) != c.Category {
			continue
		}
		s, ok := counts[rec.Category]
		if !ok {
			s = &stewardCategoryStats{Category: string(rec.Category)}
			counts[rec.Category] = s
		}
		s.Total++
		switch rec.Verdict {
		case StewardLedgerVerdictAccepted:
			s.Accepted++
		case StewardLedgerVerdictCorrected:
			s.Corrected++
		}
	}

	var rows []stewardCategoryStats
	if c.Category != "" {
		s, ok := counts[StewardLedgerCategory(c.Category)]
		if !ok {
			s = &stewardCategoryStats{Category: c.Category}
		}
		rows = append(rows, finalizeStewardStats(*s))
	} else {
		for _, cat := range stewardLedgerCategoryOrder {
			if s, ok := counts[cat]; ok {
				rows = append(rows, finalizeStewardStats(*s))
			}
		}
	}

	if c.JSON {
		enc := json.NewEncoder(ctx.Stdout)
		enc.SetEscapeHTML(false)
		return enc.Encode(rows)
	}

	if len(rows) == 0 {
		fmt.Fprintln(ctx.Stdout, "no ledger entries")
		return nil
	}

	return writeStewardStatsTable(ctx, rows)
}

// finalizeStewardStats computes AcceptRate for s (left 0 when Total is 0,
// avoiding a divide-by-zero NaN).
func finalizeStewardStats(s stewardCategoryStats) stewardCategoryStats {
	if s.Total > 0 {
		s.AcceptRate = float64(s.Accepted) / float64(s.Total)
	}
	return s
}

// writeStewardStatsTable renders rows as a tab-aligned human-readable table.
func writeStewardStatsTable(ctx *cli.Context, rows []stewardCategoryStats) error {
	w := tabwriter.NewWriter(ctx.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CATEGORY\tTOTAL\tACCEPTED\tCORRECTED\tACCEPT RATE")
	fmt.Fprintln(w, "--------\t-----\t--------\t---------\t-----------")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%.0f%%\n", r.Category, r.Total, r.Accepted, r.Corrected, r.AcceptRate*100)
	}
	return w.Flush()
}

// ── steward ledger recall ────────────────────────────────────────────────────

// stewardLedgerDefaultRecallLimit is the default cap on the number of
// records `steward ledger recall` returns when --limit is unset (or, in a
// direct-Run test call that bypasses kong's `default:""` tag, left zero).
const stewardLedgerDefaultRecallLimit = 10

// stewardLedgerRecallKong is the kong struct for `ateam steward ledger
// recall <category>`. Category is a required positional arg with no kong
// `enum:""` tag (same reasoning as stats' --category filter above): kong
// enum validation would need special-casing, and Run must validate anyway
// to cover the direct-Run-call test path that bypasses kong parsing.
type stewardLedgerRecallKong struct {
	Category string `arg:"" name:"category" required:"" help:"Decision category to recall (plan-approval|scope-call|merge-approval|design-fork|unblock-action)."`
	Limit    int    `name:"limit" default:"10" help:"Max records to return (most recent first)."`
	JSON     bool   `name:"json" help:"Output records as JSON."`
}

// Run reads StewardLedgerPath, filters to c.Category, orders most-recent-
// first, and caps at c.Limit. A missing ledger is not an error — it reports
// no entries. Malformed lines are skipped with a warning to stderr, same as
// stats.
func (c *stewardLedgerRecallKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam steward ledger recall: nil context")
	}
	if !StewardLedgerCategory(c.Category).Valid() {
		return cli.Usagef("ateam steward ledger recall: invalid category %q", c.Category)
	}

	data, err := os.ReadFile(StewardLedgerPath(ctx))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ateam steward ledger recall: read ledger: %w", err)
	}

	var recs []StewardLedgerRecord
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rec, err := ParseStewardLedgerRecord([]byte(line))
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "ateam steward ledger recall: warning: skipping malformed line: %v\n", err)
			continue
		}
		if string(rec.Category) != c.Category {
			continue
		}
		recs = append(recs, rec)
	}

	sort.Slice(recs, func(i, j int) bool {
		return recs[i].Timestamp.After(recs[j].Timestamp)
	})

	limit := c.Limit
	if limit <= 0 {
		limit = stewardLedgerDefaultRecallLimit
	}
	if len(recs) > limit {
		recs = recs[:limit]
	}

	if c.JSON {
		if recs == nil {
			recs = []StewardLedgerRecord{} // emit [] not null on empty
		}
		enc := json.NewEncoder(ctx.Stdout)
		enc.SetEscapeHTML(false)
		return enc.Encode(recs)
	}

	if len(recs) == 0 {
		fmt.Fprintln(ctx.Stdout, "no ledger entries")
		return nil
	}

	for _, r := range recs {
		fmt.Fprintf(ctx.Stdout, "%s  %s  initiative=%s  verdict=%s\n", r.Timestamp.Format(time.RFC3339), r.Category, r.Initiative, r.Verdict)
		fmt.Fprintf(ctx.Stdout, "  recommendation: %s\n", r.Recommendation)
		if r.Decision != "" {
			fmt.Fprintf(ctx.Stdout, "  decision: %s\n", r.Decision)
		}
		fmt.Fprintln(ctx.Stdout)
	}
	return nil
}
