package verbs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
	"github.com/mgt-insurance/agent-teams/internal/workspace"
)

// The bridge follower delivers hosted message beads labeled bridge:laptop to
// local initiatives. It follows the bd events journal of cloud_home through
// followJournal, the loop host follow uses, with its own checkpoint, lock, and
// log prefix.

const (
	bridgeLogPrefix = "ateam bridge"

	// bridgeCallTimeout bounds each non-streaming bd call against cloud_home.
	bridgeCallTimeout = 30 * time.Second
)

// bridgeRunner runs one bd command against cloud_home, bounded by ctx.
// (*bd.Client).RunContext satisfies it.
type bridgeRunner func(ctx context.Context, args ...string) (string, error)

// bridgeFollower holds the seams of one bridge run. Zero values use the
// production implementations.
type bridgeFollower struct {
	// Once makes one journal pass without --follow plus the catch-up sweep.
	once bool

	// Seams.
	runBD     func(ctx context.Context, args []string, onLine func(string)) (stderr string, err error)
	headQuery func(query string) (string, error)
	cloud     bridgeRunner
	sleep     func(ctx context.Context, d time.Duration)
	now       func() time.Time
	tryLock   func(path string) (release func(), ok bool, err error)
	// target resolves a hosted assignee to the local recipient handle, or
	// ok=false when this machine cannot deliver to it.
	target func(ctx *cli.Context, assignee string) (recipient string, ok bool)
	// deliver sends body to recipient through the local `ateam mail send` path
	// and returns the local message id.
	deliver func(ctx *cli.Context, recipient, sender, body, dedupKey string) (string, error)
	// localCopy finds a local message to recipient carrying the dedup key in
	// any status, so a message the recipient already read is not sent again.
	localCopy func(ctx *cli.Context, recipient, dedupKey string) (id string, found bool, err error)

	delivered int
	left      int
}

// bridgeSkip reports why a pass ended without running: no cloud setup, or
// another process owns the bridge.
type bridgeSkip string

const (
	bridgeSkipNoCloud bridgeSkip = "cloud_home not set; nothing to do"
	bridgeSkipLocked  bridgeSkip = "another follower owns the bridge; nothing to do"
)

func (b *bridgeFollower) defaults(ctx *cli.Context, cloudHome string) {
	if b.now == nil {
		b.now = time.Now
	}
	if b.sleep == nil {
		b.sleep = followSleep
	}
	if b.tryLock == nil {
		b.tryLock = followTryLock
	}
	if b.runBD == nil {
		b.runBD = func(rc context.Context, args []string, onLine func(string)) (string, error) {
			return followExecBDEnv(rc, cloudHome, []string{hostedTLSEnv}, args, onLine)
		}
	}
	client := bd.NewClientWithEnv(cloudHome, hostedTLSEnv)
	if b.headQuery == nil {
		b.headQuery = func(q string) (string, error) { return client.Run("sql", "--json", q) }
	}
	if b.cloud == nil {
		b.cloud = client.RunContext
	}
	if b.target == nil {
		b.target = bridgeLocalTarget
	}
	if b.deliver == nil {
		b.deliver = bridgeDeliver
	}
	if b.localCopy == nil {
		b.localCopy = bridgeLocalCopy
	}
}

// runBridge runs the follower until run is cancelled (or one pass for once).
// It returns a non-empty skip reason, with a nil error, when it did nothing.
func runBridge(run context.Context, ctx *cli.Context, b *bridgeFollower) (bridgeSkip, error) {
	cloudHome, ok, err := bridgeCloudHome(ctx.Home)
	if err != nil {
		return "", fmt.Errorf("%s: %w", bridgeLogPrefix, err)
	}
	if !ok {
		return bridgeSkipNoCloud, nil
	}
	b.defaults(ctx, cloudHome)

	mailbox := filepath.Join(ctx.Home, "mailbox")
	if err := os.MkdirAll(mailbox, 0o755); err != nil {
		return "", err
	}
	release, locked, err := b.tryLock(filepath.Join(mailbox, "bridge-follow.lock"))
	if err != nil {
		return "", fmt.Errorf("%s: lock: %w", bridgeLogPrefix, err)
	}
	if !locked {
		return bridgeSkipLocked, nil
	}
	defer release()

	return "", followJournal(run, followSpec{
		seqPath:   filepath.Join(mailbox, "bridge.seq"),
		runBD:     b.runBD,
		headQuery: b.headQuery,
		sleep:     b.sleep,
		now:       b.now,
		stderr:    ctx.Stderr,
		logPrefix: bridgeLogPrefix,
		once:      b.once,
		onRow: func(row followRow, _ time.Time) {
			if row.Op != "create" || row.Issue == nil || row.Issue.IssueType != "message" || row.IssueID == "" {
				return
			}
			for _, l := range row.Issue.Labels {
				if l == bridgeLabel {
					b.handle(run, ctx, row.IssueID)
					return
				}
			}
		},
		onStart:     func() { b.sweep(run, ctx) },
		onTruncated: func(int64) { b.sweep(run, ctx) },
	})
}

// sweep delivers every open bridge:laptop message in cloud_home. It catches
// mail sent while the laptop was off or while delivery was failing.
func (b *bridgeFollower) sweep(run context.Context, ctx *cli.Context) {
	cctx, cancel := context.WithTimeout(run, bridgeCallTimeout)
	defer cancel()
	out, err := b.cloud(cctx, "list", "--include-infra", "--type=message", "--label="+bridgeLabel, "--status=open", "--json")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: catch-up sweep failed: %v\n", bridgeLogPrefix, err)
		return
	}
	issues, err := parseBridgeIssues(out)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: catch-up sweep: %v\n", bridgeLogPrefix, err)
		return
	}
	for _, is := range issues {
		if run.Err() != nil {
			return
		}
		b.handle(run, ctx, is.ID)
	}
}

// handle delivers one hosted message: fetch, check, deliver locally, then
// close it in cloud_home. A failure at any step is logged and leaves the bead
// open for the next sweep.
func (b *bridgeFollower) handle(run context.Context, ctx *cli.Context, id string) {
	cctx, cancel := context.WithTimeout(run, bridgeCallTimeout)
	defer cancel()
	out, err := b.cloud(cctx, "show", id, "--json")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: show %s: %v\n", bridgeLogPrefix, id, err)
		return
	}
	issues, err := parseBridgeIssues(out)
	if err != nil || len(issues) == 0 {
		fmt.Fprintf(ctx.Stderr, "%s: show %s: unreadable result\n", bridgeLogPrefix, id)
		return
	}
	is := issues[0]
	if is.Status != "open" || !hasLabel(is.Labels, bridgeLabel) || hasLabel(is.Labels, bridgeDeliveredLabel) {
		return
	}
	recipient, ok := b.target(ctx, is.Assignee)
	if !ok {
		b.left++
		fmt.Fprintf(ctx.Stderr, "%s: message %s to %s is not deliverable on this machine; leaving it\n", bridgeLogPrefix, id, is.Assignee)
		return
	}
	key := bridgeDedupKey(id)
	localID, have, err := b.localCopy(ctx, recipient, key)
	if err != nil {
		b.left++
		fmt.Fprintf(ctx.Stderr, "%s: local dedup check for %s: %v; leaving it open\n", bridgeLogPrefix, id, err)
		return
	}
	if have {
		fmt.Fprintf(ctx.Stderr, "%s: %s already delivered to %s as %s; closing it out\n", bridgeLogPrefix, id, recipient, localID)
	} else {
		sender := bridgeSender(is)
		body := strings.TrimRight(is.Description, "\n") + "\n\nReply with: ateam mail send " + sender + " --file <file>\n"
		localID, err = b.deliver(ctx, recipient, sender, body, key)
		if err != nil {
			b.left++
			fmt.Fprintf(ctx.Stderr, "%s: deliver %s to %s: %v; leaving it open\n", bridgeLogPrefix, id, recipient, err)
			return
		}
		b.delivered++
		fmt.Fprintf(ctx.Stdout, "%s: delivered %s to %s as %s\n", bridgeLogPrefix, id, recipient, localID)
	}
	reason := "bridged to " + workspace.Host(ctx.Home)
	steps := [][]string{
		{"close", id, "--reason=" + reason, "--force"},
		{"label", "add", id, bridgeDeliveredLabel},
		{"label", "remove", id, bridgeLabel},
		{"label", "remove", id, "delivery:pending"},
	}
	for _, args := range steps {
		if _, err := b.cloud(cctx, args...); err != nil {
			fmt.Fprintf(ctx.Stderr, "%s: %s %s: %v\n", bridgeLogPrefix, args[0], id, err)
			// A failed close leaves the bead open: the next sweep finds the
			// local copy by its dedup key and retries only the close-out. A later failure only skips cleanup.
			if args[0] == "close" {
				return
			}
		}
	}
}

// bridgeSender returns the cloud sender of a hosted message: the "from:" notes
// line, else the title suffix after "message from ".
func bridgeSender(is bd.Issue) string {
	for _, line := range strings.Split(is.Notes, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "from:"); ok {
			if v = strings.TrimSpace(v); v != "" {
				return v
			}
		}
	}
	if v, ok := strings.CutPrefix(is.Title, "message from "); ok {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return "cloud"
}

// bridgeLocalTarget resolves a hosted assignee to a local recipient: the
// laptop steward when its session directory exists, or an open local
// initiative whose worktree exists on this machine.
func bridgeLocalTarget(ctx *cli.Context, assignee string) (string, bool) {
	if assignee == LocalStewardHandle {
		if _, err := os.Stat(StewardSessionDir(ctx)); err != nil {
			return "", false
		}
		return StewardHandle, true
	}
	if assignee == "" || ctx.BD == nil {
		return "", false
	}
	is, err := bd.ShowIssue(ctx.BD, assignee)
	if err != nil || is.Status != "open" {
		return "", false
	}
	wt := initiative.Of(is).Worktree
	if wt == "" {
		return "", false
	}
	if _, err := os.Stat(wt); err != nil {
		return "", false
	}
	return assignee, true
}

// bridgeDeliver mails body through the full `ateam mail send` path, so the
// recipient's doorbell and wake run.
func bridgeDeliver(ctx *cli.Context, recipient, sender, body, dedupKey string) (string, error) {
	tmp, err := os.CreateTemp("", "ateam-bridge-*.txt")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	_, werr := tmp.WriteString(body)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return "", werr
	}
	c := newSendKong()
	c.RecipientID, c.File, c.Sender, c.DedupKey = recipient, tmp.Name(), sender, dedupKey
	var out bytes.Buffer
	sendCtx := *ctx
	sendCtx.Stdout = &out
	if err := c.Run(&sendCtx); err != nil {
		return "", err
	}
	return sentMessageID(out.String()), nil
}

// sentMessageID reads the "message_id:" line the send path prints.
func sentMessageID(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(line, "message_id: "); ok {
			if f := strings.Fields(v); len(f) > 0 {
				return f[0]
			}
		}
	}
	return "?"
}

// bridgeLocalCopy looks for a local message to recipient labeled with the
// dedup key in any status.
func bridgeLocalCopy(ctx *cli.Context, recipient, dedupKey string) (string, bool, error) {
	var messages []bd.Issue
	if err := ctx.BD.RunJSON(&messages, "list", "--include-infra", "--all",
		"--assignee="+recipient, "--label=dedup:"+dedupKey, "--json"); err != nil {
		return "", false, err
	}
	messages = filterMessageType(messages)
	if len(messages) == 0 {
		return "", false, nil
	}
	return messages[0].ID, true, nil
}

// runBridgeUntil runs the bridge follower until run is cancelled. A skip or an
// error is logged; neither stops the caller.
func runBridgeUntil(ctx *cli.Context, run context.Context) {
	skip, err := runBridge(run, ctx, &bridgeFollower{})
	switch {
	case err != nil:
		fmt.Fprintf(ctx.Stderr, "%s: %v\n", bridgeLogPrefix, err)
	case skip != "":
		fmt.Fprintf(ctx.Stderr, "%s: %s\n", bridgeLogPrefix, skip)
	}
}

// mailBridgeKong implements `ateam mail bridge`: deliver hosted bridge:laptop
// mail to this machine. With --once it makes one pass and exits.
type mailBridgeKong struct {
	Once bool `name:"once" help:"Make one journal pass plus the catch-up sweep, then exit."`

	follower *bridgeFollower
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *mailBridgeKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam mail bridge: nil context")
	}
	b := c.follower
	if b == nil {
		b = &bridgeFollower{}
	}
	b.once = c.Once
	run, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	skip, err := runBridge(run, ctx, b)
	if err != nil {
		return err
	}
	if skip != "" {
		fmt.Fprintf(ctx.Stdout, "bridge: %s\n", skip)
		return nil
	}
	if c.Once {
		fmt.Fprintf(ctx.Stdout, "delivered %d, left %d\n", b.delivered, b.left)
	}
	return nil
}

// parseBridgeIssues reads the array `bd list --json` and `bd show --json` print.
func parseBridgeIssues(out string) ([]bd.Issue, error) {
	var issues []bd.Issue
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		return nil, fmt.Errorf("unmarshal: %w (raw: %.200s)", err, out)
	}
	return issues, nil
}
