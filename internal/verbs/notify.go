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

// RegisterNotify registers the notify verb.
func RegisterNotify(reg cli.Registry) {
	reg.Register(&notifyCmd{
		transportFor: transport.For,
		labelAdd:     defaultLabelAdd,
	})
}

// defaultLabelAdd runs `bd label add <id> <label>`.
func defaultLabelAdd(b cli.BDRunner, id, label string) error {
	_, err := b.Run("label", "add", id, label)
	return err
}

// notifyCmd implements `ateam notify <initiative-id> --file <path> [--title <t>]`.
type notifyCmd struct {
	transportFor transportForFunc
	labelAdd     labelAddFunc
}

func (c *notifyCmd) Name() string { return "notify" }

// Run implements:
//
//		ateam notify <initiative-id> --file <path> [--title <t>]
//
//	 1. Reads body from --file; title from --title or derived from the initiative.
//	 2. Resolves the active transport via transport.For(workspace.Home()).
//	 3. Reads the initiative bead's labels for an existing "thread:<N>" label.
//	 4. Calls transport.Send with ThreadRef="" (new topic) or the existing ref.
//	 5. On a new topic: records "thread:<returned-ref>" on the initiative bead.
//	 6. Prints the thread ref and a confirmation line.
func (c *notifyCmd) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam notify: nil context")
	}

	initiativeID, file, title, err := parseNotifyFlags(args)
	if err != nil {
		return err
	}

	body, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("ateam notify: read file: %w", err)
	}

	// Look up the initiative bead.
	issue, err := bd.ShowIssue(ctx.BD, initiativeID)
	if err != nil {
		return fmt.Errorf("ateam notify: look up initiative %s: %w", initiativeID, err)
	}

	// Derive title from the initiative if not provided.
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
		InitiativeID: initiativeID,
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
		if labErr := c.labelAdd(ctx.BD, initiativeID, label); labErr != nil {
			// Non-fatal: notify succeeded; label write failure is surfaced but
			// does not break the caller.
			fmt.Fprintf(ctx.Stderr, "ateam notify: warning: could not record thread label on %s: %v\n", initiativeID, labErr)
		}
	}

	fmt.Fprintf(ctx.Stdout, "thread_ref: %s\n", returnedRef)
	fmt.Fprintf(ctx.Stdout, "initiative: %s\n", initiativeID)
	return nil
}

// parseNotifyFlags parses <initiative-id> --file <p> [--title <t>] from args.
func parseNotifyFlags(args []string) (initiativeID, file, title string, err error) {
	if len(args) == 0 {
		return "", "", "", cli.Usagef("ateam notify: missing <initiative-id>")
	}
	initiativeID = args[0]
	for i := 1; i < len(args); {
		if v, n := parseFlag(args, i, "--file"); n > 0 {
			file = v
			i += n
			continue
		}
		if v, n := parseFlag(args, i, "--title"); n > 0 {
			title = v
			i += n
			continue
		}
		return "", "", "", cli.Usagef("ateam notify: unknown flag %q", args[i])
	}
	if file == "" {
		return "", "", "", cli.Usagef("ateam notify: --file required")
	}
	if _, statErr := os.Stat(file); statErr != nil {
		return "", "", "", cli.Usagef("ateam notify: file not found: %s", file)
	}
	return initiativeID, file, title, nil
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
