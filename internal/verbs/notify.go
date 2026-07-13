// This file is owned by Track N (notify verb, agent-teams-2c4d).
package verbs

import (
	"fmt"
	"os"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/transport"
	"github.com/mgt-insurance/agent-teams/internal/workspace"
)

// transportForFunc is the function type for resolving the active transport.
// Injected so tests can substitute a fake without touching os/env.
type transportForFunc func(home string) (transport.Transport, error)

// labelAddFunc is the function type for adding a label to a bead.
// Injected so tests can capture bd calls without an exec.
type labelAddFunc func(bd cli.BDRunner, id, label string) error

// RegisterNotifyKong registers the notify verb onto p using a native kong struct.
func RegisterNotifyKong(p *cli.Parser) {
	p.AddVerb("notify", "Send a message to a human via the configured transport.", &notifyKong{
		transportFor: transport.For,
		labelAdd:     defaultLabelAdd,
	})
}

// notifyForGate adapts notifyKong into the gateNotifyFunc signature so
// `ateam gate` can fire a best-effort phone ping in-process after recording
// the gate. kong has no runtime verb registry to look "notify" up by name
// post-registration, so gate constructs and runs a notifyKong directly.
func notifyForGate(ctx *cli.Context, id, file string) error {
	cmd := &notifyKong{ID: id, File: file, transportFor: transport.For, labelAdd: defaultLabelAdd}
	return cmd.Run(ctx)
}

// defaultLabelAdd runs `bd label add <id> <label>`.
func defaultLabelAdd(b cli.BDRunner, id, label string) error {
	_, err := b.Run("label", "add", id, label)
	return err
}

// notifyKong is the kong-native form of notifyCmd: `ateam notify <id> --file <path> [--title <t>]`.
type notifyKong struct {
	ID    string `arg:"" name:"id" help:"Initiative ID."`
	File  string `name:"file" help:"Path to the message body file (required)." required:""`
	Title string `name:"title" help:"Optional title (defaults to the initiative's title)."`

	transportFor transportForFunc `kong:"-"`
	labelAdd     labelAddFunc     `kong:"-"`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
//
//  1. Reads body from --file; title from --title or derived from the initiative.
//  2. Resolves the active transport via transport.For(workspace.Home()).
//  3. Reads the initiative bead's labels for an existing "thread:<N>" label.
//  4. Calls transport.Send with ThreadRef="" (new topic) or the existing ref.
//  5. On a new topic: records "thread:<returned-ref>" on the initiative bead.
//  6. Prints the thread ref and a confirmation line.
func (c *notifyKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam notify: nil context")
	}
	if _, err := os.Stat(c.File); err != nil {
		return cli.Usagef("ateam notify: file not found: %s", c.File)
	}

	body, err := os.ReadFile(c.File)
	if err != nil {
		return fmt.Errorf("ateam notify: read file: %w", err)
	}

	// Look up the initiative bead.
	issue, err := bd.ShowIssue(ctx.BD, c.ID)
	if err != nil {
		return fmt.Errorf("ateam notify: look up initiative %s: %w", c.ID, err)
	}

	// Derive title from the initiative if not provided.
	title := c.Title
	if title == "" {
		title = issue.Title
	}

	// Find an existing thread label on the initiative.
	threadRef := threadLabelValue(issue.Labels)

	// Resolve transport.
	home := workspace.Home()
	t, err := c.transportFor(home)
	if err != nil {
		return fmt.Errorf("ateam notify: no transport configured: %w", err)
	}

	msg := transport.OutboundMessage{
		InitiativeID: c.ID,
		ThreadRef:    threadRef,
		Title:        title,
		Body:         string(body),
	}

	returnedRef, err := t.Send(msg)
	if err != nil {
		return fmt.Errorf("ateam notify: send: %w", err)
	}

	// If this was a new topic, record the thread label so subsequent notifies
	// reuse it and the relay can reverse-map (contract section 2).
	if threadRef == "" && returnedRef != "" {
		label := "thread:" + returnedRef
		if labErr := c.labelAdd(ctx.BD, c.ID, label); labErr != nil {
			// Non-fatal: notify succeeded; label write failure is surfaced but
			// does not break the caller.
			fmt.Fprintf(ctx.Stderr, "ateam notify: warning: could not record thread label on %s: %v\n", c.ID, labErr)
		}
	}

	fmt.Fprintf(ctx.Stdout, "thread_ref: %s\n", returnedRef)
	fmt.Fprintf(ctx.Stdout, "initiative: %s\n", c.ID)
	return nil
}

// threadLabelValue scans labels for a "thread:<ref>" entry and returns the ref
// portion. Returns "" if no such label is present.
func threadLabelValue(labels []string) string {
	for _, l := range labels {
		if strings.HasPrefix(l, "thread:") {
			return strings.TrimPrefix(l, "thread:")
		}
	}
	return ""
}
