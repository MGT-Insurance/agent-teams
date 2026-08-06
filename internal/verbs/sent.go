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
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/sentlog"
)

// RegisterSentKong registers the `sent` verb onto p using a native kong
// struct. Called from RegisterAllKong in kong_converted.go.
func RegisterSentKong(p *cli.Parser) {
	p.AddVerb("sent", "Read back the audit log of messages sent to the human (who sent what, when).", &sentKong{})
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
// EIGHT QUERYABLE, SEVEN SETTABLE — the asymmetry is deliberate (contract §7
// AMENDMENT 2). KindUndeclared stays out of sentlog.knownKinds so no call
// site can ever SET it, but it is exactly the row a bug report is most
// likely to be about — a send site that failed to identify itself — so it
// must be reachable from the CLI without falling back to --json plus manual
// filtering.
var queryableSenderKinds = []sentlog.Kind{
	sentlog.KindNotify,
	sentlog.KindNotifyBriefing,
	sentlog.KindNotifyReviews,
	sentlog.KindNotifyDirect,
	sentlog.KindDispatch,
	sentlog.KindClose,
	sentlog.KindRelayHung,
	sentlog.KindUndeclared,
}

// queryableSender reports whether s is an accepted --sender value. Not
// sentlog.Kind.Known(): that answers "may a call site declare this?", which
// is the stricter seven-value question. See queryableSenderKinds.
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

// sentEntry pairs a decoded Record with its parsed timestamp. The ts is
// carried so --since doesn't re-parse the string, and so the human-readable
// modes can render the PARSED value rather than echoing the raw one — it is
// NOT an ordering key (see Run, agent-teams-48dh.23).
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

	// FILE ORDER IS THE SOLE ORDERING KEY (agent-teams-48dh.23). entries is
	// built in file order and the log is O_APPEND, so file order IS send
	// order by construction; most-recent-first is therefore exactly the
	// reverse, and there is deliberately no comparison function here at all.
	//
	// The record's timestamp is a CLAIM written by whichever short-lived
	// process made the send, and sorting on it makes `ateam sent --limit 3`
	// answer with three wrong messages after an ordinary backwards clock step
	// (NTP correction, DST, sleep/wake) — no attacker needed. A raw writer
	// can do worse: sub-second precision places a record inside a group the
	// decorator can only write at one-second granularity, and an offset up to
	// ±23:59 resolves ~24h away from what it displays. An audit log whose job
	// is "what did we send, in what order" must not let a claim outrank the
	// structural fact. This also subsumes agent-teams-48dh.11's tiebreak:
	// with file order as the only key there are no ties left to break.
	//
	// --since still filters on the claimed ts (agent-teams-48dh.24) — that is
	// a separate, open design question, not something this ordering fixes.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

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

	if c.JSON {
		recs := make([]sentlog.Record, len(entries))
		for i, e := range entries {
			recs[i] = e.rec
		}
		if recs == nil {
			recs = []sentlog.Record{} // emit [] not null on empty
		}
		enc := json.NewEncoder(ctx.Stdout)
		enc.SetEscapeHTML(false)
		return enc.Encode(recs)
	}

	if len(entries) == 0 {
		fmt.Fprintln(ctx.Stdout, "no messages")
		return nil
	}

	if c.Full {
		writeSentFull(ctx, entries)
		return nil
	}
	return writeSentTable(ctx, entries)
}

// sentSanitize replaces every rune that could let record content — bodies
// are agent-authored and arbitrary — drive the terminal or forge the shape
// of the output (contract §7 AMENDMENT 4). Replaced with U+FFFD:
//
//   - C0 controls other than LF and TAB, and DEL. ESC drives the cursor:
//     records print most-recent-first, so an OLD body sits BELOW newer rows
//     and can repaint them with CUU+EL. BS/CR/NUL/BEL are the same class.
//   - C1 controls (U+0080–U+009F); U+009B is CSI on terminals that decode
//     them.
//   - The bidi embedding/override/isolate formatters, which reorder
//     rendered text and can be left unterminated by the 200-rune cut.
//
// Substitution rather than deletion, one rune out per rune in: the cut
// point of the table's rune budget then does not move, and the reader can
// see that something was there.
//
// LF and TAB survive because neither can forge structure once the two
// human-readable modes are built as they are: the table collapses all
// whitespace to single spaces BEFORE sanitizing, so neither ever reaches a
// cell, and --full prefixes every body line, so an LF cannot begin a
// structural line. Invalid UTF-8 also becomes U+FFFD — ranging over a
// string decodes it that way — so no raw byte reaches the terminal.
func sentSanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
			return '�'
		case r == 0x200e, r == 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			return '�'
		}
		return r
	}, s)
}

// sentSafeField renders one single-line field (ts, sender, initiative,
// outcome, title) for either human-readable mode: whitespace collapsed to
// single spaces so it cannot break a table row or start a line of its own,
// then sanitized. Every one of these fields is copied from the log, so
// every one of them is attacker-reachable, not just the body.
func sentSafeField(s string) string {
	return sentSanitize(strings.Join(strings.Fields(s), " "))
}

// sentDisplayTS renders a record's PARSED timestamp for the human-readable
// modes, normalized to UTC with sub-second precision preserved
// (agent-teams-48dh.23).
//
// Never the raw rec.Timestamp string. Two things follow from rendering the
// parsed value: the TIME column reads as ordered, because a record stamped
// with a nonzero UTC offset can no longer display one instant while
// resolving to another up to ~24h away; and the field is bounded to 30 runes
// by construction, because time.Parse(RFC3339) accepts fractional seconds of
// UNLIMITED length (100,000 digits parse fine) while Format never emits more
// than nine.
func sentDisplayTS(ts time.Time) string {
	return ts.UTC().Format(time.RFC3339Nano)
}

// sentFieldTruncRunes is the rune budget for the table's four non-body
// columns (agent-teams-48dh.22). The body has had a budget since §7; these
// had none, and tabwriter sizes each column to its WIDEST cell — so one
// oversized field in one record indents every GENUINE record's columns by
// that width, pushing them off the screen. The damage lands on the honest
// records, and nothing looks wrong.
//
// 40 clears every legitimate value with room to spare: the widest is the
// 30-rune normalized timestamp above, with sender kinds at 15 and initiative
// ids well under that.
const sentFieldTruncRunes = 40

// writeSentTable renders entries as contract §7's tab-aligned default table
// (TIME, SENDER, INITIATIVE, OUTCOME, BODY(trunc)), matching
// writeStewardStatsTable's shape.
func writeSentTable(ctx *cli.Context, entries []sentEntry) error {
	w := tabwriter.NewWriter(ctx.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tSENDER\tINITIATIVE\tOUTCOME\tBODY")
	fmt.Fprintln(w, "----\t------\t----------\t-------\t----")
	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			sentTableField(sentDisplayTS(e.ts)), sentTableField(string(e.rec.Sender)),
			sentTableField(e.rec.Initiative), sentTableField(string(e.rec.Outcome)),
			sentTableBody(e.rec.Body))
	}
	return w.Flush()
}

// sentTableField renders one non-body table cell: sentSafeField, then the
// column's rune budget. Table only — --full gives each record its own lines,
// so a long field there costs one long line and cannot displace anything.
func sentTableField(s string) string {
	return truncate(sentSafeField(s), sentFieldTruncRunes)
}

// sentTableBody renders one body for the table's BODY column: every run of
// whitespace collapsed to a single space, sanitized, then truncated to
// sentBodyTruncRunes runes. The collapse is required, not cosmetic — a
// newline inside a tabwriter cell ends the row early and destroys the
// column alignment for the whole table, and bodies here are multi-line
// Steward digests. Truncation stays last so the 200-rune budget is spent on
// what is actually displayed.
func sentTableBody(body string) string {
	return truncate(sentSafeField(body), sentBodyTruncRunes)
}

// sentFullBodyPrefix marks every line of a body under --full. This is what
// makes the block form unforgeable: a record header starts at column 0 and
// a title line starts with exactly two spaces, so no prefixed body line can
// be read as either, no matter what the body contains. A body ends at its
// last prefixed line — which also answers the non-hostile half of the
// problem, since a multi-KB multi-line Steward digest otherwise gives the
// reader no way to see where one record stops and the next begins.
const sentFullBodyPrefix = "  | "

// writeSentFull prints one block per record with the body untruncated
// (--full). Not the table: a multi-KB body cannot live in a tabwriter cell,
// so the format that shows whole bodies has to be the block form. Line
// structure is preserved — prefixed, not escaped — because these bodies are
// digests, and %q on a multi-KB digest is unreadable.
func writeSentFull(ctx *cli.Context, entries []sentEntry) {
	for _, e := range entries {
		r := e.rec
		fmt.Fprintf(ctx.Stdout, "%s  %s  initiative=%s  outcome=%s\n",
			sentSafeField(sentDisplayTS(e.ts)), sentSafeField(string(r.Sender)),
			sentSafeField(r.Initiative), sentSafeField(string(r.Outcome)))
		fmt.Fprintf(ctx.Stdout, "  title: %s\n", sentSafeField(r.Title))
		fmt.Fprintln(ctx.Stdout, "  body:")
		for _, line := range strings.Split(sentSanitize(r.Body), "\n") {
			fmt.Fprintf(ctx.Stdout, "%s%s\n", sentFullBodyPrefix, line)
		}
		fmt.Fprintln(ctx.Stdout)
	}
}
