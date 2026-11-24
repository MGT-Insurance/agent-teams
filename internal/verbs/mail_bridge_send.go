package verbs

import (
	"errors"
	"fmt"
	"os"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// sendBridgeFromRun is the cloud-to-laptop hook for mail sent from a
// Dispatcher run. Two call sites reach it: a send to LocalStewardHandle
// (cause is nil), and a peer send whose recipient is not in the hosted
// workspace (cause is the bd.IsNotFound error from reading the recipient).
//
// It creates a message bead in the run's hosted home, labeled bridgeLabel in
// the same create call. The laptop relay reads the bead through cloud_home and
// delivers it. Every refusal returns an error and writes nothing.
func (c *sendKong) sendBridgeFromRun(ctx *cli.Context, cause error) error {
	from := os.Getenv("DSP_META_INITIATIVE")
	if from == "" {
		return errors.New("ateam send: DSP_META_INITIATIVE is empty; cannot address the laptop from this run; nothing sent")
	}
	if c.RecipientID == from {
		return fmt.Errorf("ateam send: %s is this run's own initiative; nothing sent", c.RecipientID)
	}
	if c.RecipientID != LocalStewardHandle && !peerInitiativeID.MatchString(c.RecipientID) {
		return fmt.Errorf("ateam send: %q is not a valid initiative id; nothing sent", c.RecipientID)
	}
	text, err := os.ReadFile(c.File)
	if err != nil {
		return fmt.Errorf("ateam send: read %s: %w", c.File, err)
	}
	if len(text) == 0 {
		return fmt.Errorf("ateam send: %s is empty; nothing sent", c.File)
	}

	id, hit, err := openMailWithDedupKey(ctx, c.RecipientID, c.DedupKey)
	if err != nil {
		return fmt.Errorf("ateam send: dedup lookup: %w", err)
	}
	if hit {
		fmt.Fprintf(ctx.Stdout, "message_id: %s (deduplicated)\n", id)
	} else {
		id, err = c.createBridgeMessage(ctx, from)
		if err != nil {
			return err
		}
		fmt.Fprintf(ctx.Stdout, "message_id: %s\n", id)
	}
	fmt.Fprintln(ctx.Stdout, "queued for laptop pickup (bridge:laptop); a laptop running `ateam relay` delivers it within seconds")
	if cause != nil {
		fmt.Fprintf(ctx.Stdout, "WARNING: %s is not an initiative in this hosted workspace, so this was queued for a LAPTOP initiative. If no laptop initiative %s exists on the bridge, the message waits unread. For a cloud peer, address it by its initiative id (like at-xxx); find it with: ateam list\n", c.RecipientID, c.RecipientID)
	}
	return nil
}

// createBridgeMessage creates the hosted message bead and returns its id. The
// bridge label rides on the create call because the laptop follower reads
// labels from the journal's create row.
func (c *sendKong) createBridgeMessage(ctx *cli.Context, from string) (string, error) {
	args := []string{
		"create",
		"--type=message",
		"--assignee=" + c.RecipientID,
		"--notes=from: " + from,
		"--labels=delivery:pending",
		"--labels=" + bridgeLabel,
		"--body-file=" + c.File,
		"--title=message from " + from,
		"--json",
	}
	if c.Thread != "" {
		args = append(args, "--labels=thread:"+c.Thread)
	}
	if c.DedupKey != "" {
		args = append(args, "--labels=dedup:"+c.DedupKey)
	}
	var issue bd.Issue
	if err := ctx.BD.RunJSON(&issue, args...); err != nil {
		return "", fmt.Errorf("ateam send: create bridge message bead: %w", err)
	}
	if issue.ID == "" {
		return "", errors.New("ateam send: bd create returned no id")
	}
	return issue.ID, nil
}

// sendBridgeFromCloudSteward is the hosted-steward-to-laptop hook of send. It
// is a stub until the cloud-steward bridge bead replaces it.
func (c *sendKong) sendBridgeFromCloudSteward(ctx *cli.Context) error {
	return errors.New("ateam send: bridge mail from the cloud steward is not built yet; nothing sent")
}
