// This file is owned by Track M (messaging verbs).
package verbs

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── sendKong ──────────────────────────────────────────────────────────────────

// sendKong is the kong-native form of sendCmd.
// DI fields are tagged kong:"-" so kong ignores them; tests substitute fakes
// without touching the struct registration.
type sendKong struct {
	RecipientID string `arg:"" name:"recipient-initiative-id" help:"Initiative ID of the recipient."`
	File        string `name:"file"   help:"Path to the message body file (required)." required:""`
	Sender      string `name:"sender" help:"Sender identifier (default: git user.name)."`
	Thread      string `name:"thread" help:"Optional thread identifier label."`

	agentsFunc     agentsJSONFunc       `kong:"-"`
	resumeFunc     resumeInitiativeFunc `kong:"-"`
	sleeper        sleeperFunc          `kong:"-"`
	doorbellExists doorbellExistsFunc   `kong:"-"`
	respawnFunc    respawnFunc          `kong:"-"`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *sendKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam send: nil context")
	}

	// File must exist.
	if _, err := os.Stat(c.File); err != nil {
		return cli.Usagef("ateam send: file not found: %s", c.File)
	}

	sender := c.Sender
	if sender == "" {
		sender = gitUserName()
	}

	createArgs := []string{
		"create",
		"--type=message",
		"--assignee=" + c.RecipientID,
		"--notes=from: " + sender,
		"--labels=delivery:pending",
		"--body-file=" + c.File,
		"--title=message from " + sender,
		"--json",
	}
	if c.Thread != "" {
		createArgs = append(createArgs, "--labels=thread:"+c.Thread)
	}

	var issue bd.Issue
	if err := ctx.BD.RunJSON(&issue, createArgs...); err != nil {
		return fmt.Errorf("ateam send: create message bead: %w", err)
	}
	if issue.ID == "" {
		return fmt.Errorf("ateam send: bd create returned no id")
	}

	doorbellDir := filepath.Join(ctx.Home, "mailbox")
	if err := os.MkdirAll(doorbellDir, 0o755); err != nil {
		return fmt.Errorf("ateam send: create mailbox dir: %w", err)
	}
	doorbellPath := filepath.Join(doorbellDir, c.RecipientID+".wake")
	if err := touchFile(doorbellPath); err != nil {
		return fmt.Errorf("ateam send: touch doorbell: %w", err)
	}

	fmt.Fprintf(ctx.Stdout, "message_id: %s\n", issue.ID)
	fmt.Fprintf(ctx.Stdout, "recipient: %s\n", c.RecipientID)

	wtPath, liveErr := recipientWorktree(ctx, c.RecipientID)
	if liveErr != nil {
		fmt.Fprintf(ctx.Stdout, "note: could not resolve recipient worktree (%v); skipping liveness check\n", liveErr)
		return nil
	}

	sessions, agentsErr := c.agentsFunc()
	if agentsErr != nil {
		fmt.Fprintf(ctx.Stdout, "note: could not query live sessions (%v); message delivered via doorbell\n", agentsErr)
		return nil
	}

	want := canonicalPath(wtPath)
	fmt.Fprintf(ctx.Stdout, "liveness: recipient worktree=%q; %d session(s) reported by claude agents --all --json\n", wtPath, len(sessions))
	for i, s := range sessions {
		fmt.Fprintf(ctx.Stdout, "liveness:   session[%d] id=%q cwd=%q status=%q pid-present=%t match=%t\n",
			i, s.ID, s.CWD, s.Status, s.PID != nil, canonicalPath(s.CWD) == want)
	}

	entry := matchSessionByWorktree(sessions, wtPath)
	if entry == nil {
		fmt.Fprintf(ctx.Stdout, "recipient not found in claude agents; launching via ateam resume\n")
		if err := c.resumeFunc(ctx, c.RecipientID); err != nil {
			return fmt.Errorf("ateam send: resume escalation: %w", err)
		}
		return nil
	}

	// NEVER respawn busy (interrupts in-flight tool calls) or waiting (would
	// drop a pending AskUserQuestion/permission dialog) — Stop re-arms the
	// watcher, which will see the doorbell the instant the turn ends.
	if entry.Status == "busy" || entry.Status == "waiting" {
		fmt.Fprintf(ctx.Stdout, "recipient is %s; doorbell will be picked up when its turn ends\n", entry.Status)
		return nil
	}

	// idle or pid-less: give an armed watcher a moment to deliver, then
	// re-check the doorbell. inbox-drain is the only consumer now, so "gone"
	// reliably means a turn saw it; "still present" means the recipient is
	// deaf and needs reviving.
	c.sleeper(5 * time.Second)
	if !c.doorbellExists(doorbellPath) {
		fmt.Fprintf(ctx.Stdout, "doorbell consumed; delivery in progress\n")
		return nil
	}

	fmt.Fprintf(ctx.Stdout, "doorbell still present after 5s; recipient is deaf — respawning %s\n", entry.ID)
	if err := c.respawnFunc(entry.ID); err != nil {
		fmt.Fprintf(ctx.Stdout, "warning: respawn %s failed (%v); message %s remains queued for the next turn\n", entry.ID, err, issue.ID)
		return nil
	}
	fmt.Fprintf(ctx.Stdout, "respawned %s to deliver the doorbell\n", entry.ID)
	return nil
}

// ── inboxKong ─────────────────────────────────────────────────────────────────

// inboxKong is the kong-native form of inboxCmd.
type inboxKong struct {
	JSON bool `name:"json" help:"Output messages as JSON instead of a system-reminder block."`
	Peek bool `name:"peek" help:"Print unread count without consuming messages."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *inboxKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam inbox: nil context")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("ateam inbox: getwd: %w", err)
	}

	myID, err := resolveMyInitiative(ctx, cwd)
	if err != nil {
		return nil
	}

	var messages []bd.Issue
	if err := ctx.BD.RunJSON(&messages,
		"list",
		"--include-infra",
		"--assignee="+myID,
		"--exclude-label=read",
		"--status=open",
		"--json",
	); err != nil {
		return fmt.Errorf("ateam inbox: query: %w", err)
	}

	messages = filterMessageType(messages)

	if c.Peek {
		if len(messages) == 0 {
			fmt.Fprintln(ctx.Stdout, "no unread mail")
		} else {
			fmt.Fprintf(ctx.Stdout, "%d unread message(s)\n", len(messages))
		}
		return nil
	}

	if len(messages) == 0 {
		fmt.Fprintln(ctx.Stdout, "no unread mail")
		return nil
	}

	if c.JSON {
		raw, err := json.Marshal(messages)
		if err != nil {
			return fmt.Errorf("ateam inbox: marshal: %w", err)
		}
		fmt.Fprintln(ctx.Stdout, string(raw))
	} else {
		printMessagesBlock(ctx, messages)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, msg := range messages {
		if err := markMessageRead(ctx, msg.ID, myID, now); err != nil {
			fmt.Fprintf(ctx.Stderr, "ateam inbox: mark read %s: %v\n", msg.ID, err)
		}
	}

	return nil
}

// ── send ──────────────────────────────────────────────────────────────────────

// agentsJSONFunc is the function type for querying live bg sessions.
// Injected so tests can substitute a fake without touching os/exec.
type agentsJSONFunc func() ([]agentSession, error)

// resumeInitiativeFunc is the function type for escalating to ateam resume.
// Injected so tests can substitute a fake.
type resumeInitiativeFunc func(ctx *cli.Context, id string) error

// agentSession is the subset of fields from `claude agents --json` relevant
// to ateam verbs.
//
// Field availability by session kind (from contract agent-teams-j9s §1):
//   - Every session:    CWD, Kind, Status (busy|idle|waiting), SessionID, StartedAt.
//   - Background only:  ID, Name, State (working|done).
//     Interactive sessions have no State/Name/ID; JSON absence is fine — Go
//     leaves the fields at their zero values ("").
//
// PID and Status are both absent (PID nil, Status "") for a tracked-but-dead
// session — one `claude agents --all --json` reports but has no live process
// backing it. Never branch on State; it's unreliable. Only PID presence and
// Status matter.
type agentSession struct {
	CWD       string `json:"cwd"`
	Kind      string `json:"kind"`      // "interactive" | "background"
	Status    string `json:"status"`    // "busy" | "idle" | "waiting"; absent for dead sessions
	Name      string `json:"name"`      // background sessions only
	State     string `json:"state"`     // unreliable; do not branch on this
	ID        string `json:"id"`        // short id for background sessions; used by claude stop/respawn
	PID       *int   `json:"pid"`       // nil => absent => tracked-but-dead session
	SessionID string `json:"sessionId"` // full session id present on all sessions
}

// defaultAgentsJSON runs `claude agents --json` and parses the result.
// Omits pid-less tracked-but-dead sessions — see defaultAgentsJSONAll.
func defaultAgentsJSON() ([]agentSession, error) {
	return runAgentsJSON("--json")
}

// defaultAgentsJSONAll runs `claude agents --all --json` and parses the
// result. --all surfaces pid-less tracked sessions that a plain
// `claude agents --json` omits; ateam send needs to see these to distinguish
// "recipient is idle" from "recipient's process is gone but claude still
// tracks it" (both require the same idle/pid-less handling in sendKong).
func defaultAgentsJSONAll() ([]agentSession, error) {
	return runAgentsJSON("--all", "--json")
}

// runAgentsJSON runs `claude agents <args...>` and parses the JSON result.
func runAgentsJSON(args ...string) ([]agentSession, error) {
	cmd := exec.Command("claude", append([]string{"agents"}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude agents %s: %w", strings.Join(args, " "), err)
	}
	var sessions []agentSession
	if err := json.Unmarshal(out, &sessions); err != nil {
		return nil, fmt.Errorf("claude agents %s: parse: %w", strings.Join(args, " "), err)
	}
	return sessions, nil
}

// defaultResume runs `ateam resume <id>` via the resumeKong directly.
func defaultResume(ctx *cli.Context, id string) error {
	cmd := &resumeKong{ID: id, launch: launchBGSession}
	return cmd.Run(ctx)
}

// hasLiveSession reports whether any session in sessions has a cwd matching
// worktreePath (symlink-normalised, see canonicalPath).
func hasLiveSession(sessions []agentSession, worktreePath string) bool {
	return matchSessionByWorktree(sessions, worktreePath) != nil
}

// matchSessionByWorktree returns a pointer to the first session in sessions
// whose cwd resolves (symlink-normalised, see canonicalPath) to worktreePath,
// or nil if none match.
func matchSessionByWorktree(sessions []agentSession, worktreePath string) *agentSession {
	want := canonicalPath(worktreePath)
	for i := range sessions {
		if canonicalPath(sessions[i].CWD) == want {
			return &sessions[i]
		}
	}
	return nil
}

// sleeperFunc pauses execution for d. Injected so tests can substitute a no-op.
type sleeperFunc func(d time.Duration)

// defaultSleeper is time.Sleep, wired in as the production sleeperFunc.
func defaultSleeper(d time.Duration) {
	time.Sleep(d)
}

// doorbellExistsFunc reports whether the doorbell file at path still exists
// (i.e. no turn has consumed it yet via inbox-drain). Injected so tests can
// substitute a fake without touching the filesystem.
type doorbellExistsFunc func(path string) bool

// defaultDoorbellExists checks the real filesystem for path.
func defaultDoorbellExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// respawnFunc revives a tracked-but-dead or unresponsive session in place via
// `claude respawn <shortid>` (same sessionId, same single entry in
// `claude agents`, full conversation preserved — verified 6+ times). Injected
// so tests can substitute a fake without executing real claude commands.
type respawnFunc func(id string) error

// defaultRespawn execs `claude respawn <id>`.
func defaultRespawn(id string) error {
	cmd := exec.Command("claude", "respawn", id)
	return cmd.Run()
}

// gitUserName returns the current git user.name (best-effort; empty on error).
func gitUserName() string {
	cmd := exec.Command("git", "config", "--get", "user.name")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// touchFile creates or updates the modification time of path.
func touchFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	f.Close()
	now := time.Now()
	return os.Chtimes(path, now, now)
}

// recipientWorktree looks up the initiative by id and extracts its worktree
// path from the description.
func recipientWorktree(ctx *cli.Context, id string) (string, error) {
	issue, err := bd.ShowIssue(ctx.BD, id)
	if err != nil {
		return "", fmt.Errorf("bd show %s: %w", id, err)
	}
	wt := worktreePath(issue.Description)
	if wt == "" {
		return "", fmt.Errorf("initiative %s has no worktree: line", id)
	}
	return wt, nil
}

// resolveMyInitiative finds the open initiative whose worktree: line matches cwd.
// Returns the initiative id or an error if none matches.
func resolveMyInitiative(ctx *cli.Context, cwd string) (string, error) {
	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--status=open", "--json"); err != nil {
		return "", err
	}
	if match := matchByWorktree(issues, cwd); match != nil {
		return match.ID, nil
	}
	return "", fmt.Errorf("no initiative registered for worktree: %s", cwd)
}

// filterMessageType returns only issues with IssueType == "message".
func filterMessageType(issues []bd.Issue) []bd.Issue {
	var out []bd.Issue
	for _, iss := range issues {
		if iss.IssueType == "message" {
			out = append(out, iss)
		}
	}
	return out
}

// printMessagesBlock writes messages as a <system-reminder>-style block to ctx.Stdout.
func printMessagesBlock(ctx *cli.Context, messages []bd.Issue) {
	fmt.Fprintln(ctx.Stdout, "<system-reminder>")
	fmt.Fprintf(ctx.Stdout, "You have %d unread message(s):\n", len(messages))
	for _, msg := range messages {
		sender := senderFromNotes(msg.Notes)
		if sender == "" {
			sender = msg.CreatedBy
		}
		fmt.Fprintf(ctx.Stdout, "\n[%s] from: %s\n%s\n", msg.ID, sender, msg.Description)
	}
	fmt.Fprintln(ctx.Stdout, "To re-read a consumed message:")
	for _, msg := range messages {
		fmt.Fprintf(ctx.Stdout, "  ateam show %s\n", msg.ID)
	}
	fmt.Fprintln(ctx.Stdout, "</system-reminder>")
}

// senderFromNotes extracts the sender from a "from: <sender>" line in notes.
func senderFromNotes(notes string) string {
	for _, line := range strings.Split(notes, "\n") {
		if strings.HasPrefix(line, "from: ") {
			return strings.TrimPrefix(line, "from: ")
		}
	}
	return ""
}

// markMessageRead adds the 'read' label and delivery ack labels to a message
// bead, then closes it. Closing is idempotent — `bd close` on an
// already-closed bead succeeds as a no-op, so a re-drain (or a duplicate
// inbox read) never fails here.
func markMessageRead(ctx *cli.Context, msgID, myID, ts string) error {
	// Add 'read' label (idempotent — bd label add is no-op if already present).
	if _, err := ctx.BD.Run("label", "add", msgID, "read"); err != nil {
		return fmt.Errorf("add read label: %w", err)
	}
	// Two-phase delivery ack labels.
	if _, err := ctx.BD.Run("label", "add", msgID, "delivery:acked"); err != nil {
		return fmt.Errorf("add delivery:acked: %w", err)
	}
	if _, err := ctx.BD.Run("label", "add", msgID, "delivery-acked-by:"+myID); err != nil {
		return fmt.Errorf("add delivery-acked-by: %w", err)
	}
	if _, err := ctx.BD.Run("label", "add", msgID, "delivery-acked-at:"+ts); err != nil {
		return fmt.Errorf("add delivery-acked-at: %w", err)
	}
	// Remove delivery:pending (idempotent).
	if _, err := ctx.BD.Run("label", "remove", msgID, "delivery:pending"); err != nil {
		return fmt.Errorf("remove delivery:pending: %w", err)
	}
	// Auto-close on read. Fires only here (post-ack), never on delivery —
	// unread/pending messages and messages to a dead initiative stay open.
	if _, err := ctx.BD.Run("close", msgID); err != nil {
		return fmt.Errorf("close message: %w", err)
	}
	return nil
}
