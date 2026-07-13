// This file is owned by LOOP Track A (agent-teams-e3mq.2): the Steward's
// session identity (init), and its decision ledger (record/stats). Reads the
// frozen contract in steward_seams.go read-only — does not edit it.
package verbs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
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
	Ledger stewardLedgerCmd  `cmd:"" name:"ledger" help:"Record and report Steward decisions."`
	Remove stewardRemoveKong `cmd:"" name:"remove" help:"De-steward this machine: remove the session dir and doorbell (ledger/briefing kept unless --purge)."`
}

// stewardLedgerCmd is the kong parent struct for `ateam steward ledger
// <subcommand>`. No Run method — group node only.
type stewardLedgerCmd struct {
	Record stewardLedgerRecordKong `cmd:"" name:"record" help:"Append one decision to the Steward's ledger."`
	Stats  stewardLedgerStatsKong  `cmd:"" name:"stats" help:"Report per-category accept/correct counts from the ledger."`
}

// ── steward init ──────────────────────────────────────────────────────────────

// stewardInitKong is the kong struct for `ateam steward init`.
type stewardInitKong struct{}

// Run creates the Steward's session directory and marker file (per contract
// paths StewardSessionDir/StewardSessionMarkerPath) and prints the session
// directory — the cwd a human/skill launches the Steward session in.
// Idempotent: an existing directory or marker is left as-is.
func (c *stewardInitKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam steward init: nil context")
	}

	sessionDir := StewardSessionDir(ctx)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return fmt.Errorf("ateam steward init: create session dir: %w", err)
	}

	marker := StewardSessionMarkerPath(ctx)
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		content := fmt.Sprintf("created: %s\n", time.Now().UTC().Format(time.RFC3339))
		if err := os.WriteFile(marker, []byte(content), 0o644); err != nil {
			return fmt.Errorf("ateam steward init: write marker: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("ateam steward init: stat marker: %w", err)
	}

	fmt.Fprintln(ctx.Stdout, sessionDir)
	return nil
}

// ── steward remove ────────────────────────────────────────────────────────────

// stewardRemoveKong is the kong struct for `ateam steward remove`, the
// companion to init (agent-teams-e3mq.25): it de-steward a machine by
// removing StewardSessionDir (marker included) and the doorbell, which is
// what disables gate->steward routing going forward (the agent-teams-e3mq.24
// guard in notifyToSteward short-circuits once the marker is gone) and stops
// wake-watcher.sh from recognizing this cwd as the Steward's session.
type stewardRemoveKong struct {
	Purge bool `name:"purge" help:"Also delete the ledger and briefing-thread (default: kept, for relocating the Steward to another machine)."`

	agentsFunc agentsJSONFunc `kong:"-"`
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

	sessionDir := StewardSessionDir(ctx)
	if sessions, err := agentsFunc(); err == nil {
		if hasLiveSession(sessions, sessionDir) {
			fmt.Fprintf(ctx.Stderr, "ateam steward remove: warning: a live session appears to be running in %s; not blocking removal\n", sessionDir)
		}
	}
	// Best-effort: a failure to query live sessions is not reported or
	// treated as an error — this check never blocks removal.

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
}

// Run builds a StewardLedgerRecord, validates it against the contract's
// enums (defense-in-depth alongside the kong `enum:""` tags above — this
// path also fires when Run is called directly in tests, bypassing kong
// parsing), and appends it as one JSONL line to StewardLedgerPath.
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
