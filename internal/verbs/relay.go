package verbs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/transport"
	"github.com/mgt-insurance/agent-teams/internal/workspace"
)

// relayEnabledFunc reports whether the active transport is configured.
// Injected so tests can control the result without touching env / config files.
type relayEnabledFunc func(home string) bool

// relayTransportForFunc resolves the active Transport.
// Injected so tests can substitute a fake.
type relayTransportForFunc func(home string) (transport.Transport, error)

// relayBDQueryFunc queries the workspace beads for open initiatives carrying a
// given thread label. Returns the matching issues (may be empty or many).
// Injected so tests can substitute a fake.
type relayBDQueryFunc func(home, label string) ([]bd.Issue, error)

// relaySendFunc execs `ateam mail send steward --file <tmp> --sender human`.
// The destination is always the Steward (StewardHandle); the mapped
// initiative id travels inside the envelope written to file, not as a CLI
// arg. Injected so tests can capture calls without running a subprocess.
type relaySendFunc func(ctx *cli.Context, file string) error

// defaultBDQuery runs `bd list --status=open --label=<label> --json` against
// the global workspace home and returns matching issues.
func defaultBDQuery(home, label string) ([]bd.Issue, error) {
	client := bd.NewClient(home)
	var issues []bd.Issue
	if err := client.RunJSON(&issues, "list", "--status=open", "--label="+label, "--json"); err != nil {
		return nil, err
	}
	return issues, nil
}

// relayBDQueryClosedFunc queries the workspace beads for CLOSED initiatives
// carrying a given thread label. Used only in the case-0 branch of
// handleReply — after the open-only bdQuery seam already found zero open
// matches — as the closed-initiative safety net (agent-teams-7dup.2):
// reopening a Telegram topic in the UI does not change beads state, so this
// is keyed off beads status, not Telegram topic state.
//
// NOTE: `bd list` defaults to open-only, the same as every other bd list
// invocation in this file — there is no "return all statuses" mode via a
// bare --label query (confirmed empirically: `bd list --label=X --json`
// omits closed issues; `bd list --label=X --status=closed --json` is
// required to see them). So this seam queries --status=closed directly
// rather than querying unfiltered and filtering client-side.
// Injected so tests can substitute a fake.
type relayBDQueryClosedFunc func(home, label string) ([]bd.Issue, error)

// defaultBDQueryClosed runs `bd list --status=closed --label=<label> --json`
// against the global workspace home and returns matching closed issues.
func defaultBDQueryClosed(home, label string) ([]bd.Issue, error) {
	client := bd.NewClient(home)
	var issues []bd.Issue
	if err := client.RunJSON(&issues, "list", "--status=closed", "--label="+label, "--json"); err != nil {
		return nil, err
	}
	return issues, nil
}

// defaultRelaySend execs `ateam mail send steward --file <file> --sender
// human` as a subprocess so the relay loop is not blocked by the in-process
// send machinery. The Steward is always the recipient; it reads the mapped
// initiative id out of the reply envelope already written to file.
func defaultRelaySend(_ *cli.Context, file string) error {
	cmd := exec.Command("ateam", "mail", "send", StewardHandle, "--file", file, "--sender", "human")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RegisterRelayKong registers the relay verb onto p using a native kong struct.
func RegisterRelayKong(p *cli.Parser) {
	p.AddVerb("relay", "Long-poll the configured transport and relay human replies to the Steward.", &relayKong{
		enabled:       transport.Enabled,
		transportFor:  transport.For,
		bdQuery:       defaultBDQuery,
		bdQueryClosed: defaultBDQueryClosed,
		send:          defaultRelaySend,
	})
}

// relayKong is the kong-native form of relayCmd: `ateam relay` (no args).
type relayKong struct {
	enabled       relayEnabledFunc       `kong:"-"`
	transportFor  relayTransportForFunc  `kong:"-"`
	bdQuery       relayBDQueryFunc       `kong:"-"`
	bdQueryClosed relayBDQueryClosedFunc `kong:"-"`
	send          relaySendFunc          `kong:"-"`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
//
// If messaging is not configured, prints a clear message and exits 0 (opt-in).
// Otherwise calls transport.Receive, blocking until killed.
func (c *relayKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam relay: nil context")
	}

	home := workspace.Home()

	if !c.enabled(home) {
		fmt.Fprintln(ctx.Stdout, "messaging not configured; relay is a no-op")
		return nil
	}

	t, err := c.transportFor(home)
	if err != nil {
		return fmt.Errorf("ateam relay: resolve transport: %w", err)
	}

	fmt.Fprintf(ctx.Stdout, "ateam relay: starting on transport %q\n", t.Name())

	return t.Receive(func(reply transport.Reply) error {
		return c.handleReply(ctx, reply)
	})
}

// handleReply routes one inbound human reply. Returns nil always (log-and-skip
// on routing failures) unless the error is permanent-transport-level, in which
// case returning non-nil aborts Receive.
func (c *relayKong) handleReply(ctx *cli.Context, reply transport.Reply) error {
	// Non-topic messages (General topic, DMs) arrive with ThreadRef == "".
	// Log and skip; bounce is a deferred enhancement (s4lq).
	if reply.ThreadRef == "" {
		fmt.Fprintln(ctx.Stderr, "ateam relay: skipping non-topic message (no thread ref)")
		return nil
	}

	// Direct-channel short-circuit: a message posted in the Steward's
	// dedicated direct-message topic (contract: DirectHandle,
	// StewardDirectThreadPath in steward_seams.go) has no initiative bead
	// behind it, so the bd label lookup below would always miss and the
	// message would die silently. If this reply's thread ref matches the
	// persisted direct-channel thread ref, route it to the Steward as a
	// steward-direct envelope, bypassing the initiative lookup entirely. An
	// absent/empty thread-ref file (direct channel never opened yet, or a
	// read failure) falls through to the existing initiative-reply path
	// below.
	directRef, err := readThreadRefFile(StewardDirectThreadPath(ctx))
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam relay: read steward direct thread ref: %v\n", err)
	} else if directRef != "" && reply.ThreadRef == directRef {
		return c.handleDirectReply(ctx, reply)
	}

	label := "thread:" + reply.ThreadRef
	home := workspace.Home()

	issues, err := c.bdQuery(home, label)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam relay: bd query for label %q failed: %v — skipping\n", label, err)
		return nil
	}

	// Filter to open issues only (bdQuery already filters, but guard against
	// implementations that do not).
	var open []bd.Issue
	for _, iss := range issues {
		if strings.EqualFold(iss.Status, "open") {
			open = append(open, iss)
		}
	}

	switch len(open) {
	case 0:
		if c.routeClosedInitiativeSafetyNet(ctx, home, label, reply) {
			return nil
		}
		fmt.Fprintf(ctx.Stderr, "ateam relay: no open initiative found for label %q — skipping\n", label)
		return nil
	case 1:
		// Exactly one match — hand off to the Steward.
	default:
		fmt.Fprintf(ctx.Stderr, "ateam relay: ambiguous: %d open initiatives carry label %q — skipping\n", len(open), label)
		return nil
	}

	id := open[0].ID

	envelope, err := BuildStewardReplyEnvelope(id, reply.Text)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam relay: build steward reply envelope for %s: %v — skipping\n", id, err)
		return nil
	}

	// Write the envelope to a temp file so ateam mail send can read it via --file.
	tmpPath, err := writeEnvelopeToTemp(envelope)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam relay: %v — skipping\n", err)
		return nil
	}
	defer os.Remove(tmpPath)

	if err := c.send(ctx, tmpPath); err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam relay: ateam mail send %s failed (initiative %s): %v — skipping\n", StewardHandle, id, err)
	}
	return nil
}

// routeClosedInitiativeSafetyNet handles the case-0 branch of handleReply:
// zero OPEN initiatives carry the reply's thread label. Before giving up, it
// queries CLOSED initiatives for the same label — reopening a topic in the
// Telegram UI does not change beads state, so a human reply posted into a
// since-closed initiative's topic would otherwise be silently dropped
// forever (agent-teams-7dup, the bug this safety net closes). If exactly one
// CLOSED initiative owns the label, the reply is routed to the Steward as a
// steward-closed-initiative envelope carrying the closed initiative's id,
// and true is returned so the caller skips its own "no open initiative" log.
// Zero or ambiguous (2+) closed matches, a query error, or no bdQueryClosed
// seam configured, all fall through to the existing skip behavior (false) —
// the caller logs the existing message.
func (c *relayKong) routeClosedInitiativeSafetyNet(ctx *cli.Context, home, label string, reply transport.Reply) bool {
	if c.bdQueryClosed == nil {
		return false
	}
	all, err := c.bdQueryClosed(home, label)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam relay: bd query (closed) for label %q failed: %v\n", label, err)
		return false
	}

	// Defensive filter in case the seam ever returns more than closed
	// issues (e.g. a future fake or a bd behavior change) — bdQueryClosed
	// is expected to already filter to --status=closed server-side.
	var closed []bd.Issue
	for _, iss := range all {
		if strings.EqualFold(iss.Status, "closed") {
			closed = append(closed, iss)
		}
	}
	if len(closed) != 1 {
		return false
	}
	id := closed[0].ID

	envelope, err := BuildStewardClosedInitiativeEnvelope(id, reply.Text)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam relay: build steward closed-initiative envelope for %s: %v — skipping\n", id, err)
		return false
	}

	tmpPath, err := writeEnvelopeToTemp(envelope)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam relay: %v — skipping\n", err)
		return false
	}
	defer os.Remove(tmpPath)

	if err := c.send(ctx, tmpPath); err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam relay: ateam mail send %s failed (closed initiative %s): %v — skipping\n", StewardHandle, id, err)
		return true
	}

	fmt.Fprintf(ctx.Stderr, "ateam relay: routed message to steward for closed initiative %s (label %q)\n", id, label)
	return true
}

// handleDirectReply routes a reply whose thread ref matches the Steward's
// persisted direct-message channel thread ref (see the short-circuit in
// handleReply above): it wraps reply.Text in a steward-direct envelope and
// sends it straight to the Steward, with no initiative lookup involved.
func (c *relayKong) handleDirectReply(ctx *cli.Context, reply transport.Reply) error {
	envelope, err := BuildStewardDirectEnvelope(reply.Text)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam relay: build steward direct envelope: %v — skipping\n", err)
		return nil
	}

	tmpPath, err := writeEnvelopeToTemp(envelope)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam relay: %v — skipping\n", err)
		return nil
	}
	defer os.Remove(tmpPath)

	if err := c.send(ctx, tmpPath); err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam relay: ateam mail send %s failed (direct message): %v — skipping\n", StewardHandle, err)
	}
	return nil
}

// writeEnvelopeToTemp writes envelope to a new temp file (so ateam mail send
// can read it via --file) and returns its path. Caller owns removing it.
func writeEnvelopeToTemp(envelope string) (string, error) {
	tmp, err := os.CreateTemp("", "ateam-relay-reply-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(envelope); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("write temp file: %w", err)
	}
	tmp.Close()
	return tmpPath, nil
}
