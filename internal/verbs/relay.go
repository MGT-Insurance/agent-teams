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

// relaySendFunc execs `ateam send <id> --file <tmp> --sender human`.
// Injected so tests can capture calls without running a subprocess.
type relaySendFunc func(ctx *cli.Context, id, file string) error

// relayCmd implements `ateam relay`.
type relayCmd struct {
	enabled      relayEnabledFunc
	transportFor relayTransportForFunc
	bdQuery      relayBDQueryFunc
	send         relaySendFunc
}

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

// defaultRelaySend execs `ateam send <id> --file <file> --sender human` as a
// subprocess so the relay loop is not blocked by the in-process send machinery.
func defaultRelaySend(_ *cli.Context, id, file string) error {
	cmd := exec.Command("ateam", "send", id, "--file", file, "--sender", "human")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RegisterRelay registers the relay verb.
func RegisterRelay(reg cli.Registry) {
	reg.Register(&relayCmd{
		enabled:      transport.Enabled,
		transportFor: transport.For,
		bdQuery:      defaultBDQuery,
		send:         defaultRelaySend,
	})
}

func (c *relayCmd) Name() string { return "relay" }

// Run implements `ateam relay` (no args).
//
// If messaging is not configured, prints a clear message and exits 0 (opt-in).
// Otherwise calls transport.Receive, blocking until killed.
func (c *relayCmd) Run(ctx *cli.Context, args []string) error {
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
func (c *relayCmd) handleReply(ctx *cli.Context, reply transport.Reply) error {
	// Non-topic messages (General topic, DMs) arrive with ThreadRef == "".
	// Log and skip; bounce is a deferred enhancement (s4lq).
	if reply.ThreadRef == "" {
		fmt.Fprintln(ctx.Stderr, "ateam relay: skipping non-topic message (no thread ref)")
		return nil
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
		fmt.Fprintf(ctx.Stderr, "ateam relay: no open initiative found for label %q — skipping\n", label)
		return nil
	case 1:
		// Exactly one match — hand off to ateam send.
	default:
		fmt.Fprintf(ctx.Stderr, "ateam relay: ambiguous: %d open initiatives carry label %q — skipping\n", len(open), label)
		return nil
	}

	id := open[0].ID

	// Write reply text to a temp file so ateam send can read it via --file.
	tmp, err := os.CreateTemp("", "ateam-relay-reply-*")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam relay: create temp file: %v — skipping\n", err)
		return nil
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(reply.Text); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		fmt.Fprintf(ctx.Stderr, "ateam relay: write temp file: %v — skipping\n", err)
		return nil
	}
	tmp.Close()
	defer os.Remove(tmpPath)

	if err := c.send(ctx, id, tmpPath); err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam relay: ateam send %s failed: %v — skipping\n", id, err)
	}
	return nil
}

