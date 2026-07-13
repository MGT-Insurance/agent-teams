// Package verbs: leaf verbs for the unified `ateam mail` command — list
// (read-only), close, and purge. Parent-verb + alias registration lives in
// mail_register.go.
// File owned by the GO track (agent-teams-790o.2).
package verbs

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// mailListKong is the kong struct for the `mail list` subcommand.
type mailListKong struct {
	Limit int  `name:"limit" default:"20" help:"Max number of most-recent messages to show."`
	JSON  bool `name:"json" help:"Output messages as JSON."`
}

// mailRecord is the JSON shape emitted by --json; json tags match the
// dashboard/shared/types.ts MailMessage contract exactly. readAt/readBy/thread
// are *string so an absent label emits JSON null (not "" or an omitted key).
// Closed is a separate axis from Status: Status tracks delivery
// (pending|read|acked) via labels; Closed tracks the bead lifecycle
// (open/closed) — set once auto-close-on-read fires.
type mailRecord struct {
	ID        string  `json:"id"`
	To        string  `json:"to"`
	From      string  `json:"from"`
	Subject   string  `json:"subject"`
	Body      string  `json:"body"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"createdAt"`
	ReadAt    *string `json:"readAt"`
	ReadBy    *string `json:"readBy"`
	Thread    *string `json:"thread"`
	Closed    bool    `json:"closed"`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
// STRICT READ-ONLY: no label/close/note/update calls — query + format only.
func (c *mailListKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam mail list: nil context")
	}

	var msgs []bd.Issue
	if err := ctx.BD.RunJSON(&msgs,
		"list", "--include-infra", "--type=message", "--status=all",
		"--limit="+strconv.Itoa(c.Limit), "--json"); err != nil {
		return fmt.Errorf("ateam mail list: query: %w", err)
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

	if c.JSON {
		records := make([]mailRecord, len(msgs))
		for i, msg := range msgs {
			from := senderFromNotes(msg.Notes)
			if from == "" {
				from = msg.CreatedBy
			}
			records[i] = mailRecord{
				ID:        msg.ID,
				To:        msg.Assignee,
				From:      from,
				Subject:   msg.Title,
				Body:      msg.Description,
				Status:    mailStatus(msg.Labels),
				CreatedAt: msg.CreatedAt,
				ReadAt:    strOrNil(readAtFromLabels(msg.Labels)),
				ReadBy:    strOrNil(readByFromLabels(msg.Labels)),
				Thread:    strOrNil(threadFromLabels(msg.Labels)),
				Closed:    msg.Status == "closed",
			}
		}
		enc := json.NewEncoder(ctx.Stdout)
		enc.SetEscapeHTML(false)
		return enc.Encode(records)
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

// readAtFromLabels returns the value after the "delivery-acked-at:" label
// prefix, or "" if no such label is present.
func readAtFromLabels(labels []string) string {
	for _, l := range labels {
		if strings.HasPrefix(l, "delivery-acked-at:") {
			return strings.TrimPrefix(l, "delivery-acked-at:")
		}
	}
	return ""
}

// readByFromLabels returns the value after the "delivery-acked-by:" label
// prefix, or "" if no such label is present.
func readByFromLabels(labels []string) string {
	for _, l := range labels {
		if strings.HasPrefix(l, "delivery-acked-by:") {
			return strings.TrimPrefix(l, "delivery-acked-by:")
		}
	}
	return ""
}

// threadFromLabels returns the value after the "thread:" label prefix, or ""
// if no such label is present.
func threadFromLabels(labels []string) string {
	for _, l := range labels {
		if strings.HasPrefix(l, "thread:") {
			return strings.TrimPrefix(l, "thread:")
		}
	}
	return ""
}

// strOrNil returns nil for "" and a pointer to s otherwise, so JSON emits
// null instead of an empty string for absent label values.
func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
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

// ── mailCloseKong ─────────────────────────────────────────────────────────────

// mailCloseKong is the kong struct for the `mail close` subcommand. Thin
// wrapper over `bd close` — preserves the invariant that ateam is the ONLY
// sanctioned WRITE interface to the global workspace (the dashboard must
// never shell `bd -C ~/.agent-teams close` directly). Covers the orphan
// escape hatch (unread/dead-recipient mail) and the dashboard Close button.
type mailCloseKong struct {
	ID string `arg:"" name:"id" help:"Message bead id to close."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *mailCloseKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam mail close: nil context")
	}
	out, err := ctx.BD.Run("close", c.ID)
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	if err != nil {
		return err
	}
	return nil
}

// ── mailPurgeKong ─────────────────────────────────────────────────────────────

// mailPurgeKong is the kong struct for the `mail purge` subcommand. Thin
// wrapper over `bd purge`. Purge is ALWAYS manual — never scheduled — so a
// just-closed message being inspected isn't nuked out from under a human.
// Default --older-than of 7d gives the same safety margin.
type mailPurgeKong struct {
	OlderThan string `name:"older-than" default:"7d" help:"Only purge messages closed more than this long ago (e.g. 7d, 2w)."`
	DryRun    bool   `name:"dry-run" help:"Preview what would be purged without deleting."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *mailPurgeKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam mail purge: nil context")
	}
	var out string
	var err error
	if c.DryRun {
		out, err = ctx.BD.Run("purge", "--older-than", c.OlderThan, "--dry-run")
	} else {
		out, err = ctx.BD.Run("purge", "--older-than", c.OlderThan, "--force")
	}
	if out != "" {
		fmt.Fprintln(ctx.Stdout, out)
	}
	if err != nil {
		return err
	}
	return nil
}
