// Package verbs: mail verb — read-only cross-initiative mail table.
// File owned by Track M (agent-teams-euat).
package verbs

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// mailKong is the kong struct for the mail verb.
type mailKong struct {
	Limit int `name:"limit" default:"20" help:"Max number of most-recent messages to show."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
// STRICT READ-ONLY: no label/close/note/update calls — query + format only.
func (c *mailKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam mail: nil context")
	}

	var msgs []bd.Issue
	if err := ctx.BD.RunJSON(&msgs,
		"list", "--include-infra", "--type=message",
		"--limit="+strconv.Itoa(c.Limit), "--json"); err != nil {
		return fmt.Errorf("ateam mail: query: %w", err)
	}

	// Defensively re-filter: bd --type= may be honored inconsistently across
	// bd builds (mirrors inbox).
	msgs = filterMessageType(msgs)

	// Sort newest-first (bd returns newest-first empirically, but sort in Go
	// as a defensive guarantee; RFC3339 sorts lexicographically = chronologically).
	sort.Slice(msgs, func(i, j int) bool {
		return msgs[i].CreatedAt > msgs[j].CreatedAt
	})

	// Cap to Limit: after filterMessageType the count may differ from what bd
	// returned; this guarantees we show at most Limit rows.
	if len(msgs) > c.Limit {
		msgs = msgs[:c.Limit]
	}

	if len(msgs) == 0 {
		fmt.Fprintln(ctx.Stdout, "no mail")
		return nil
	}

	w := tabwriter.NewWriter(ctx.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTO\tFROM\tSUBJECT\tSTATUS\tCREATED")
	fmt.Fprintln(w, "--\t--\t----\t-------\t------\t-------")
	for _, msg := range msgs {
		from := senderFromNotes(msg.Notes)
		if from == "" {
			from = msg.CreatedBy
		}
		subject := mailTruncate(msg.Title, 40)
		status := mailStatus(msg.Labels)
		created := mailFormatCreatedAt(msg.CreatedAt)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			msg.ID, msg.Assignee, from, subject, status, created)
	}
	return w.Flush()
}

// mailStatus derives the STATUS column value from a message's labels.
// Precedence (high→low): acked > read > pending.
func mailStatus(labels []string) string {
	for _, l := range labels {
		if l == "delivery:acked" || strings.HasPrefix(l, "delivery-acked-by:") {
			return "acked"
		}
	}
	for _, l := range labels {
		if l == "read" {
			return "read"
		}
	}
	return "pending"
}

// mailTruncate shortens s to at most n runes, appending "..." if truncated.
func mailTruncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// mailFormatCreatedAt parses an RFC3339 timestamp and returns "2006-01-02 15:04".
// On parse failure, returns the raw string best-effort.
func mailFormatCreatedAt(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return t.UTC().Format("2006-01-02 15:04")
}

// RegisterMailKong registers the mail verb onto p.
func RegisterMailKong(p *cli.Parser) {
	p.AddVerb("mail", "Show a read-only table of recent mail across all initiatives.", &mailKong{})
}
