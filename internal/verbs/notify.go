// This file is owned by Track N (notify verb, agent-teams-2c4d).
package verbs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/sentlog"
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

// generalDest is the one reserved --to value. It means "the shared General
// channel" — i.e. exactly the pre-DM behavior of the direct handle. Every
// other value is an opaque conversation ref that only the owning transport
// may parse.
//
// The sentinel exists so that an ABSENT --to means exactly one thing: the
// steward's prose was not followed. Without it, `ateam notify direct --file x`
// with a dropped reply-to ref is byte-identical to the same command for a
// legitimate General reply, and the failure this epic cares about becomes
// undetectable.
const generalDest = "general"

// notifyKong is the kong-native form of notifyCmd: `ateam notify <id> --file <path> [--title <t>] [--to <ref|general>]`.
//
// To is deliberately NOT `required:""`. "Required" here is a requirement of
// the prose the steward follows, enforced by a loud depth-0 diagnostic on
// absence — not by kong rejecting the command. A steward session running
// older prose during rollout will omit the flag on a legitimate General
// reply; that must warn AND STILL DELIVER, because dropping Eric's replies
// outright is strictly worse than the misrouting this flag exists to catch.
type notifyKong struct {
	ID    string `arg:"" name:"id" help:"Initiative ID, the reserved BriefingHandle for the cross-initiative briefing topic, or the reserved DirectHandle to message the Steward directly via @mention in the shared General channel."`
	File  string `name:"file" help:"Path to the message body file (required)." required:""`
	Title string `name:"title" help:"Optional title (defaults to the initiative's title, \"Briefings\" for the briefing handle, or \"Steward\" for the direct handle)."`
	To    string `name:"to" help:"Conversation to reply in: the opaque ref from a steward-direct envelope, or the literal \"general\" for the shared General channel. Required for the direct handle."`

	transportFor transportForFunc `kong:"-"`
	labelAdd     labelAddFunc     `kong:"-"`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
//
// For the reserved BriefingHandle, see runBriefing: no initiative bead, no
// thread label — the threadRef persists in the file at
// StewardBriefingThreadPath instead.
//
// For a normal initiative id:
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

	if c.ID == BriefingHandle {
		return c.runBriefing(ctx, string(body))
	}
	if c.ID == DirectHandle {
		return c.runDirect(ctx, string(body))
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
		Sender:       sentlog.KindNotify,
	}

	returnedRef, err := sendAndLabelThread(ctx, c.ID, t, msg, c.labelAdd, "ateam notify")
	if err != nil {
		return fmt.Errorf("ateam notify: send: %w", err)
	}

	fmt.Fprintf(ctx.Stdout, "thread_ref: %s\n", returnedRef)
	fmt.Fprintf(ctx.Stdout, "initiative: %s\n", c.ID)
	return nil
}

// labelWriteMaxAttempts bounds sendAndLabelThread's label-write retry loop
// (agent-teams-6rru.10 Part A): a topic whose thread label never lands is
// replyable but permanently unroutable — worse than no topic at all — so
// the old single-shot best-effort write is no longer acceptable. Bounded so
// a persistently failing write (bd/dolt genuinely broken) still returns
// promptly.
const labelWriteMaxAttempts = 3

// labelWriteRetryDelay is the fixed backoff between label-write attempts,
// absorbing a transient local write conflict (e.g. another bd command
// briefly holding the dolt working-set lock).
const labelWriteRetryDelay = 50 * time.Millisecond

// sendAndLabelThread calls t.Send(msg) and, if msg opened a new topic
// (msg.ThreadRef == "" and Send returned a non-empty ref), records
// "thread:<ref>" on the initiative bead via labelAdd so subsequent sends
// reuse the topic and the relay can reverse-map replies (contract section
// 2). Shared by notify's first-send path and dispatch's eager topic
// creation, so there is exactly one create+label code path.
//
// The label write is retried up to labelWriteMaxAttempts times. If it still
// hasn't landed, that is surfaced LOUDLY (agent-teams-6rru.10 Part A): a
// stderr error plus a non-nil returned error, no longer swallowed to a
// best-effort warning. returnedRef is still returned alongside the error
// (Send already succeeded) so a caller that must stay fail-soft — dispatch's
// createInitialTopic — can tell "topic exists but is unroutable" apart from
// "no topic at all".
//
// On a successful label write, this also attempts a best-effort `bd dolt
// push` so peer machines can pull the label before a reply arrives — the
// cross-machine half of the timing window relay.go's freshen-before-untied
// (agent-teams-6rru.10 Part B) exists to absorb from the reader's side. A
// push failure is warned and does not affect the returned error.
func sendAndLabelThread(ctx *cli.Context, id string, t transport.Transport, msg transport.OutboundMessage, labelAdd labelAddFunc, errPrefix string) (string, error) {
	returnedRef, err := t.Send(msg)
	if err != nil {
		return "", err
	}
	if msg.ThreadRef == "" && returnedRef != "" {
		label := "thread:" + returnedRef
		var labErr error
		for attempt := 1; attempt <= labelWriteMaxAttempts; attempt++ {
			if labErr = labelAdd(ctx.BD, id, label); labErr == nil {
				break
			}
			if attempt < labelWriteMaxAttempts {
				time.Sleep(labelWriteRetryDelay)
			}
		}
		if labErr != nil {
			fmt.Fprintf(ctx.Stderr, "%s: ERROR: could not record thread label %q on %s after %d attempts — topic is replyable but UNROUTABLE: %v\n", errPrefix, label, id, labelWriteMaxAttempts, labErr)
			return returnedRef, fmt.Errorf("record thread label %q on %s after %d attempts: %w", label, id, labelWriteMaxAttempts, labErr)
		}
		if _, pushErr := ctx.BD.Run("dolt", "push"); pushErr != nil {
			fmt.Fprintf(ctx.Stderr, "%s: warning: could not push thread label for %s to remote (peers will pick it up on their next sync): %v\n", errPrefix, id, pushErr)
		}
	}
	return returnedRef, nil
}

// runBriefing handles the reserved BriefingHandle: no initiative bead behind
// it, so the thread ref persists in the file at StewardBriefingThreadPath
// (contract: steward_seams.go) instead of a bead's "thread:<n>" label.
//
//  1. Title from --title, or "Briefings" if not given.
//  2. Reads any existing threadRef from StewardBriefingThreadPath ("" if the
//     file doesn't exist yet — this is the first briefing notify).
//  3. Resolves the active transport via transport.For(workspace.Home()).
//  4. Calls transport.Send with ThreadRef="" (new topic) or the existing ref.
//  5. On a new topic: persists the returned ref to StewardBriefingThreadPath.
//  6. Prints the thread ref and a confirmation line.
func (c *notifyKong) runBriefing(ctx *cli.Context, body string) error {
	title := c.Title
	if title == "" {
		title = "Briefings"
	}

	path := StewardBriefingThreadPath(ctx)
	threadRef, err := readThreadRefFile(path)
	if err != nil {
		return fmt.Errorf("ateam notify: read briefing thread ref: %w", err)
	}

	home := workspace.Home()
	t, err := c.transportFor(home)
	if err != nil {
		return fmt.Errorf("ateam notify: no transport configured: %w", err)
	}

	msg := transport.OutboundMessage{
		InitiativeID: c.ID,
		ThreadRef:    threadRef,
		Title:        title,
		Body:         body,
		Sender:       sentlog.KindNotifyBriefing,
	}

	returnedRef, err := t.Send(msg)
	if err != nil {
		return fmt.Errorf("ateam notify: send: %w", err)
	}

	// If this was a new topic, persist the ref so subsequent briefing
	// notifies reuse it. No bead, no label — file only.
	if threadRef == "" && returnedRef != "" {
		if writeErr := writeThreadRefFile(path, returnedRef); writeErr != nil {
			fmt.Fprintf(ctx.Stderr, "ateam notify: warning: could not persist briefing thread ref: %v\n", writeErr)
		} else if pubErr := publishStewardTopics(ctx); pubErr != nil {
			fmt.Fprintf(ctx.Stderr, "ateam notify: warning: could not publish steward topics: %v\n", pubErr)
		}
	}

	fmt.Fprintf(ctx.Stdout, "thread_ref: %s\n", returnedRef)
	fmt.Fprintf(ctx.Stdout, "initiative: %s\n", c.ID)
	return nil
}

// runDirect handles the reserved DirectHandle: single-channel @mention
// addressing (agent-teams-4x83) posts straight to the shared General
// channel instead of bootstrapping a dedicated per-machine forum topic —
// there is no thread ref to read, reuse, or persist, and no
// publishStewardTopics call.
//
// --to selects the destination (agent-teams-ncn5.11), because a steward-direct
// envelope can now originate from a 1:1 DM as well as from General, and the
// answer belongs in the conversation the question came from:
//
//   - --to general → General:true, ChatRef:"" — the shared channel, exactly
//     the pre-DM behavior.
//   - --to <ref> → General:false, ChatRef:<ref> — the conversation that ref
//     names. The value is OPAQUE here: notify does not parse it, validate its
//     shape, or know it carries a chat id. Only the owning transport splits
//     it, and only the owning transport decides whether that destination is
//     permitted — so a ref notify would consider nonsense still reaches the
//     transport, which is the layer entitled to reject it.
//   - --to absent → treated as general, with a loud depth-0 diagnostic. See
//     notifyKong's doc for why this warns instead of failing.
//
// Steps:
//  1. Title from --title, or "Steward" if not given.
//  2. Resolves the active transport via transport.For(workspace.Home()).
//  3. Calls transport.Send addressed per --to; Send returns "" as threadRef.
//  4. Prints the thread ref (always empty) and a confirmation line.
func (c *notifyKong) runDirect(ctx *cli.Context, body string) error {
	title := c.Title
	if title == "" {
		title = "Steward"
	}

	to := c.To
	if to == "" {
		transport.Logf(ctx.Stderr, 0, "ateam notify: FAULT: --to was omitted on a %q send — the steward prose requires it. Falling back to the shared General channel; if this reply was meant for a DM, it is being delivered to the wrong conversation. Pass --to general for a General reply, or --to <ref> with the reply-to ref from the envelope.", DirectHandle)
		to = generalDest
	}
	general := to == generalDest
	chatRef := ""
	if !general {
		chatRef = to
	}

	home := workspace.Home()
	t, err := c.transportFor(home)
	if err != nil {
		return fmt.Errorf("ateam notify: no transport configured: %w", err)
	}

	msg := transport.OutboundMessage{
		InitiativeID: c.ID,
		Title:        title,
		Body:         body,
		General:      general,
		ChatRef:      chatRef,
		Sender:       sentlog.KindNotifyDirect,
	}

	returnedRef, err := t.Send(msg)
	if err != nil {
		return fmt.Errorf("ateam notify: send: %w", err)
	}

	fmt.Fprintf(ctx.Stdout, "thread_ref: %s\n", returnedRef)
	fmt.Fprintf(ctx.Stdout, "initiative: %s\n", c.ID)
	return nil
}

// readThreadRefFile returns the trimmed contents of path, or "" if the file
// does not exist yet (the not-yet-opened-topic case).
func readThreadRefFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// writeThreadRefFile persists ref to path (0644), creating parent dirs as
// needed.
func writeThreadRefFile(path, ref string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(ref), 0o644)
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
