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

// RegisterMessaging registers the messaging verbs: send, inbox.
func RegisterMessaging(reg cli.Registry) {
	reg.Register(&sendCmd{agentsFunc: defaultAgentsJSON, resumeFunc: defaultResume})
	reg.Register(&inboxCmd{})
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
type agentSession struct {
	CWD    string `json:"cwd"`
	Kind   string `json:"kind"`   // "interactive" | "background"
	Status string `json:"status"` // "busy" | "idle" | "waiting"
	Name   string `json:"name"`   // background sessions only
	State  string `json:"state"`  // "working" | "done"; background sessions only
}

// defaultAgentsJSON runs `claude agents --json` and parses the result.
func defaultAgentsJSON() ([]agentSession, error) {
	cmd := exec.Command("claude", "agents", "--json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude agents --json: %w", err)
	}
	var sessions []agentSession
	if err := json.Unmarshal(out, &sessions); err != nil {
		return nil, fmt.Errorf("claude agents --json: parse: %w", err)
	}
	return sessions, nil
}

// defaultResume runs `ateam resume <id>` via the resumeCommand directly.
func defaultResume(ctx *cli.Context, id string) error {
	cmd := &resumeCommand{launch: launchBGSession}
	return cmd.Run(ctx, []string{id})
}

// hasLiveSession reports whether any session in sessions has a cwd matching
// worktreePath (exact match after trimming trailing slashes).
func hasLiveSession(sessions []agentSession, worktreePath string) bool {
	want := strings.TrimRight(worktreePath, "/")
	for _, s := range sessions {
		if strings.TrimRight(s.CWD, "/") == want {
			return true
		}
	}
	return false
}

type sendCmd struct {
	agentsFunc agentsJSONFunc
	resumeFunc resumeInitiativeFunc
}

func (c *sendCmd) Name() string { return "send" }

// Run implements:
//
//	ateam send <recipient-initiative-id> --file <path> [--sender <id>] [--thread <id>]
func (c *sendCmd) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam send: nil context")
	}
	recipientID, file, sender, thread, err := parseSendFlags(args)
	if err != nil {
		return err
	}

	// Default sender to current git user (best-effort).
	if sender == "" {
		sender = gitUserName()
	}

	// Build bd create args: type=message bead with assignee=recipient.
	createArgs := []string{
		"create",
		"--type=message",
		"--assignee=" + recipientID,
		"--notes=from: " + sender,
		"--labels=delivery:pending",
		"--body-file=" + file,
		"--title=message from " + sender,
		"--json",
	}
	if thread != "" {
		createArgs = append(createArgs, "--labels=thread:"+thread)
	}

	var issue bd.Issue
	if err := ctx.BD.RunJSON(&issue, createArgs...); err != nil {
		return fmt.Errorf("ateam send: create message bead: %w", err)
	}
	if issue.ID == "" {
		return fmt.Errorf("ateam send: bd create returned no id")
	}

	// Touch the recipient's doorbell file.
	doorbellDir := filepath.Join(ctx.Home, "mailbox")
	if err := os.MkdirAll(doorbellDir, 0o755); err != nil {
		return fmt.Errorf("ateam send: create mailbox dir: %w", err)
	}
	doorbellPath := filepath.Join(doorbellDir, recipientID+".wake")
	if err := touchFile(doorbellPath); err != nil {
		return fmt.Errorf("ateam send: touch doorbell: %w", err)
	}

	fmt.Fprintf(ctx.Stdout, "message_id: %s\n", issue.ID)
	fmt.Fprintf(ctx.Stdout, "recipient: %s\n", recipientID)

	// Liveness check: look up recipient's worktree, then check for a live session.
	wtPath, liveErr := recipientWorktree(ctx, recipientID)
	if liveErr != nil {
		// Non-fatal: can't determine liveness; skip resume escalation.
		fmt.Fprintf(ctx.Stdout, "note: could not resolve recipient worktree (%v); skipping liveness check\n", liveErr)
		return nil
	}

	sessions, agentsErr := c.agentsFunc()
	if agentsErr != nil {
		// Non-fatal: claude may not be in PATH in some envs; skip escalation.
		fmt.Fprintf(ctx.Stdout, "note: could not query live sessions (%v); message delivered via doorbell\n", agentsErr)
		return nil
	}

	want := strings.TrimRight(wtPath, "/")
	fmt.Fprintf(ctx.Stdout, "liveness: recipient worktree=%q; %d session(s) reported by claude agents --json\n", want, len(sessions))
	for i, s := range sessions {
		fmt.Fprintf(ctx.Stdout, "liveness:   session[%d] cwd=%q kind=%q status=%q match=%t\n",
			i, s.CWD, s.Kind, s.Status, strings.TrimRight(s.CWD, "/") == want)
	}

	if hasLiveSession(sessions, wtPath) {
		// Session is live; the doorbell will wake it.
		fmt.Fprintf(ctx.Stdout, "recipient session is live; doorbell will wake it\n")
		return nil
	}

	// No live session — escalate to ateam resume.
	fmt.Fprintf(ctx.Stdout, "recipient session not live; launching via ateam resume\n")
	if err := c.resumeFunc(ctx, recipientID); err != nil {
		return fmt.Errorf("ateam send: resume escalation: %w", err)
	}
	return nil
}

// parseSendFlags parses <recipient-id> --file <p> [--sender <s>] [--thread <t>]
// from args.
func parseSendFlags(args []string) (recipientID, file, sender, thread string, err error) {
	if len(args) == 0 {
		return "", "", "", "", cli.Usagef("ateam send: missing <recipient-initiative-id>")
	}
	recipientID = args[0]
	for i := 1; i < len(args); {
		if v, n := parseFlag(args, i, "--file"); n > 0 {
			file = v
			i += n
			continue
		}
		if v, n := parseFlag(args, i, "--sender"); n > 0 {
			sender = v
			i += n
			continue
		}
		if v, n := parseFlag(args, i, "--thread"); n > 0 {
			thread = v
			i += n
			continue
		}
		return "", "", "", "", cli.Usagef("ateam send: unknown flag %q", args[i])
	}
	if file == "" {
		return "", "", "", "", cli.Usagef("ateam send: --file required")
	}
	if _, statErr := os.Stat(file); statErr != nil {
		return "", "", "", "", cli.Usagef("ateam send: file not found: %s", file)
	}
	return recipientID, file, sender, thread, nil
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

// ── inbox ─────────────────────────────────────────────────────────────────────

type inboxCmd struct{}

func (c *inboxCmd) Name() string { return "inbox" }

// Run implements:
//
//	ateam inbox [--json] [--peek]
//
// --peek: query unread count and print a brief summary WITHOUT marking messages
// read. Safe to call from hooks that need to decide whether to signal the model.
// Normal mode (no --peek): render the <system-reminder> block and mark read
// (single consume path — the model is the only consumer).
// Zero unread: prints "no unread mail" so repeated calls are visibly clean.
func (c *inboxCmd) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam inbox: nil context")
	}

	jsonOut, peek, err := parseInboxFlags(args)
	if err != nil {
		return err
	}

	// Resolve my initiative by worktree: $PWD.
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("ateam inbox: getwd: %w", err)
	}

	myID, err := resolveMyInitiative(ctx, cwd)
	if err != nil {
		// Not registered as an initiative — silent no-op per contract.
		return nil
	}

	// Query unread: type=message beads, Assignee=me, no 'read' label.
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

	// Filter to type=message only (include-infra returns all infra types).
	messages = filterMessageType(messages)

	if peek {
		// Non-consuming summary for hooks. Print count and return; never mark read.
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

	if jsonOut {
		raw, err := json.Marshal(messages)
		if err != nil {
			return fmt.Errorf("ateam inbox: marshal: %w", err)
		}
		fmt.Fprintln(ctx.Stdout, string(raw))
	} else {
		printMessagesBlock(ctx, messages)
	}

	// Mark each message read + write delivery acks.
	now := time.Now().UTC().Format(time.RFC3339)
	for _, msg := range messages {
		if err := markMessageRead(ctx, msg.ID, myID, now); err != nil {
			// Non-fatal: report but don't abort the drain.
			fmt.Fprintf(ctx.Stderr, "ateam inbox: mark read %s: %v\n", msg.ID, err)
		}
	}

	return nil
}

// parseInboxFlags parses [--json] [--peek] from args.
func parseInboxFlags(args []string) (jsonOut, peek bool, err error) {
	for i := 0; i < len(args); {
		if args[i] == "--json" {
			jsonOut = true
			i++
			continue
		}
		if args[i] == "--peek" {
			peek = true
			i++
			continue
		}
		return false, false, cli.Usagef("ateam inbox: unknown flag %q", args[i])
	}
	return jsonOut, peek, nil
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
// bead, idempotently. The bead stays open (not closed).
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
	return nil
}
