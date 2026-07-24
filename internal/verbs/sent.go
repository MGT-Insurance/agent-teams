// This file implements `ateam sent` (agent-teams-48dh.2, contract
// agent-teams-48dh.1 §7): readback of the sent-message audit log. Modeled on
// stewardLedgerRecallKong.Run (steward.go:457+) — reads the whole file,
// filters, orders most-recent-first, caps at --limit.
//
// FROZEN surface for THIS bead (contract §7 lists the full eventual set):
// --sender, --since, --limit, --json only. --initiative, --full, and the
// truncated human table land in agent-teams-48dh.3 — do not add them here.
package verbs

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
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

// validSenderKinds enumerates the six real sender kinds (contract §3) for
// the --sender usage-error message. KindUndeclared is deliberately excluded:
// it is a guard value no caller may request explicitly.
var validSenderKinds = []sentlog.Kind{
	sentlog.KindNotify,
	sentlog.KindNotifyBriefing,
	sentlog.KindNotifyDirect,
	sentlog.KindDispatch,
	sentlog.KindClose,
	sentlog.KindRelayHung,
}

// sentKong is the kong struct for `ateam sent`.
type sentKong struct {
	Sender string        `name:"sender" help:"Filter to one sender kind (notify|notify-briefing|notify-direct|dispatch|close|relay-hung)."`
	Since  time.Duration `name:"since" help:"Only records with ts >= now-since, e.g. 30m or 6h."`
	Limit  int           `name:"limit" default:"20" help:"Max records to return (most recent first)."`
	JSON   bool          `name:"json" help:"Output records as JSON."`
}

// sentEntry pairs a decoded Record with its parsed timestamp so sorting and
// --since filtering don't re-parse the ts string repeatedly.
type sentEntry struct {
	rec sentlog.Record
	ts  time.Time
}

// Run reads sentlog.Path(ctx.Home), filters to c.Sender/c.Since (AND'd),
// orders most-recent-first, and caps at c.Limit. A missing log is not an
// error — it reports "no messages". Malformed lines (bad JSON, or a
// timestamp that fails RFC3339 parsing) are skipped with a stderr warning;
// a corrupted line never makes the rest of the log unreadable.
func (c *sentKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam sent: nil context")
	}

	if c.Sender != "" && !sentlog.Kind(c.Sender).Known() {
		valid := make([]string, len(validSenderKinds))
		for i, k := range validSenderKinds {
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
		if hasCutoff && ts.Before(cutoff) {
			continue
		}
		entries = append(entries, sentEntry{rec: rec, ts: ts})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ts.After(entries[j].ts)
	})

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

	// Minimal plain-text default. The truncated tabwriter table (contract
	// §7's TIME/SENDER/INITIATIVE/OUTCOME/BODY(trunc) columns) needs --full
	// to make sense of the untruncated case, and both land in .3 — this is
	// deliberately not that table yet.
	for _, r := range recs {
		fmt.Fprintf(ctx.Stdout, "%s  %s  initiative=%s  outcome=%s\n", r.Timestamp, r.Sender, r.Initiative, r.Outcome)
		fmt.Fprintf(ctx.Stdout, "  title: %s\n", r.Title)
		fmt.Fprintf(ctx.Stdout, "  body: %s\n", r.Body)
		fmt.Fprintln(ctx.Stdout)
	}
	return nil
}
