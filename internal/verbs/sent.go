// This file implements `ateam sent` (agent-teams-48dh.2 and .3, contract
// agent-teams-48dh.1 §7): readback of the sent-message audit log. Modeled on
// stewardLedgerRecallKong.Run (steward.go:457+) — reads the whole file,
// filters, orders most-recent-first, caps at --limit.
//
// The surface is now the complete frozen §7 set: --sender, --since,
// --initiative, --limit, --full, --json. Do not extend it without a contract
// amendment on agent-teams-48dh.1 first.
package verbs

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/sentlog"
)

// RegisterSentKong registers the `sent` verb onto p using a native kong
// struct. Called from RegisterAllKong in kong_converted.go.
func RegisterSentKong(p *cli.Parser) {
	p.AddVerb("sent", "Read back the audit log of messages sent to Eric (who sent what, when).", &sentKong{})
}

// sentDefaultLimit matches stewardLedgerDefaultRecallLimit's role for this
// verb's own default (contract §7: "--limit N default 20").
const sentDefaultLimit = 20

// sentBodyTruncRunes is how much of a body the default table shows
// (contract §7: "truncate the displayed body to 200 runes"). Bodies are
// routinely multi-KB Steward digests, so an untruncated default makes the
// common case unreadable; --full and --json opt out.
const sentBodyTruncRunes = 200

// queryableSenderKinds enumerates the sender values --sender accepts.
//
// SEVEN QUERYABLE, SIX SETTABLE — the asymmetry is deliberate (contract §7
// AMENDMENT 2). KindUndeclared stays out of sentlog.knownKinds so no call
// site can ever SET it, but it is exactly the row a bug report is most
// likely to be about — a send site that failed to identify itself — so it
// must be reachable from the CLI without falling back to --json plus manual
// filtering.
var queryableSenderKinds = []sentlog.Kind{
	sentlog.KindNotify,
	sentlog.KindNotifyBriefing,
	sentlog.KindNotifyDirect,
	sentlog.KindDispatch,
	sentlog.KindClose,
	sentlog.KindRelayHung,
	sentlog.KindUndeclared,
}

// queryableSender reports whether s is an accepted --sender value. Not
// sentlog.Kind.Known(): that answers "may a call site declare this?", which
// is the stricter six-value question. See queryableSenderKinds.
func queryableSender(s string) bool {
	for _, k := range queryableSenderKinds {
		if string(k) == s {
			return true
		}
	}
	return false
}

// sentKong is the kong struct for `ateam sent`.
type sentKong struct {
	Sender     string        `name:"sender" help:"Filter to one sender kind (notify|notify-briefing|notify-direct|dispatch|close|relay-hung|UNDECLARED)."`
	Since      time.Duration `name:"since" help:"Only records with ts >= now-since, e.g. 30m or 6h."`
	Initiative string        `name:"initiative" help:"Filter to records whose initiative field matches exactly."`
	Limit      int           `name:"limit" default:"20" help:"Max records to return, most recent first. Zero or negative means the default of 20, not unlimited."`
	Full       bool          `name:"full" help:"Print whole bodies instead of the truncated table."`
	JSON       bool          `name:"json" help:"Output records as JSON (never truncated)."`
}

// sentEntry pairs a decoded Record with its parsed timestamp so sorting and
// --since filtering don't re-parse the ts string repeatedly.
type sentEntry struct {
	rec sentlog.Record
	ts  time.Time
}

// Run reads sentlog.Path(ctx.Home), filters to c.Sender/c.Since/c.Initiative
// (AND'd), orders most-recent-first, and caps at c.Limit. A missing log is
// not an error — it reports "no messages". Malformed lines (bad JSON, or a
// timestamp that fails RFC3339 parsing) are skipped with a stderr warning;
// a corrupted line never makes the rest of the log unreadable.
func (c *sentKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam sent: nil context")
	}

	if c.Sender != "" && !queryableSender(c.Sender) {
		valid := make([]string, len(queryableSenderKinds))
		for i, k := range queryableSenderKinds {
			valid[i] = string(k)
		}
		return cli.Usagef("ateam sent: invalid --sender %q (valid: %s)", c.Sender, strings.Join(valid, ", "))
	}

	// A missing log is not an error (contract §7: "prints 'no messages'").
	// Treated as empty data rather than an early return so --json still
	// gets [] instead of the plain-text line — same shape as
	// stewardLedgerRecallKong.Run's IsNotExist handling.
	data, err := os.ReadFile(sentlog.Path(ctx.Home))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ateam sent: read log: %w", err)
	}

	var cutoff time.Time
	hasCutoff := c.Since > 0
	if hasCutoff {
		cutoff = time.Now().Add(-c.Since)
	}

	var entries []sentEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec sentlog.Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			fmt.Fprintf(ctx.Stderr, "ateam sent: warning: skipping malformed line: %v\n", err)
			continue
		}
		ts, err := time.Parse(time.RFC3339, rec.Timestamp)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "ateam sent: warning: skipping record with unparseable timestamp %q: %v\n", rec.Timestamp, err)
			continue
		}
		if c.Sender != "" && string(rec.Sender) != c.Sender {
			continue
		}
		if c.Initiative != "" && rec.Initiative != c.Initiative {
			continue
		}
		if hasCutoff && ts.Before(cutoff) {
			continue
		}
		entries = append(entries, sentEntry{rec: rec, ts: ts})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ts.After(entries[j].ts)
	})

	// A non-positive --limit falls back to the documented default rather
	// than meaning "unlimited". Explicit, not incidental: `--limit 0` on an
	// audit log reads as "give me everything", and silently honouring that
	// would dump a multi-MB log to a terminal. It also catches a direct Run
	// call in a test, which bypasses kong's `default:"20"` tag and so
	// arrives with Limit left at the zero value.
	limit := c.Limit
	if limit <= 0 {
		limit = sentDefaultLimit
	}
	if len(entries) > limit {
		entries = entries[:limit]
	}

	recs := make([]sentlog.Record, len(entries))
	for i, e := range entries {
		recs[i] = e.rec
	}

	if c.JSON {
		if recs == nil {
			recs = []sentlog.Record{} // emit [] not null on empty
		}
		enc := json.NewEncoder(ctx.Stdout)
		enc.SetEscapeHTML(false)
		return enc.Encode(recs)
	}

	if len(recs) == 0 {
		fmt.Fprintln(ctx.Stdout, "no messages")
		return nil
	}

	if c.Full {
		writeSentFull(ctx, recs)
		return nil
	}
	return writeSentTable(ctx, recs)
}

// writeSentTable renders recs as contract §7's tab-aligned default table
// (TIME, SENDER, INITIATIVE, OUTCOME, BODY(trunc)), matching
// writeStewardStatsTable's shape.
func writeSentTable(ctx *cli.Context, recs []sentlog.Record) error {
	w := tabwriter.NewWriter(ctx.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tSENDER\tINITIATIVE\tOUTCOME\tBODY")
	fmt.Fprintln(w, "----\t------\t----------\t-------\t----")
	for _, r := range recs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Timestamp, r.Sender, r.Initiative, r.Outcome, sentTableBody(r.Body))
	}
	return w.Flush()
}

// sentTableBody renders one body for the table's BODY column: every run of
// whitespace collapsed to a single space, then truncated to
// sentBodyTruncRunes runes. The collapse is required, not cosmetic — a
// newline inside a tabwriter cell ends the row early and destroys the
// column alignment for the whole table, and bodies here are multi-line
// Steward digests.
func sentTableBody(body string) string {
	return truncate(strings.Join(strings.Fields(body), " "), sentBodyTruncRunes)
}

// writeSentFull prints one block per record with the body untruncated
// (--full). Not the table: a multi-KB body cannot live in a tabwriter cell,
// so the format that shows whole bodies has to be the block form.
func writeSentFull(ctx *cli.Context, recs []sentlog.Record) {
	for _, r := range recs {
		fmt.Fprintf(ctx.Stdout, "%s  %s  initiative=%s  outcome=%s\n", r.Timestamp, r.Sender, r.Initiative, r.Outcome)
		fmt.Fprintf(ctx.Stdout, "  title: %s\n", r.Title)
		fmt.Fprintf(ctx.Stdout, "  body: %s\n", r.Body)
		fmt.Fprintln(ctx.Stdout)
	}
}
