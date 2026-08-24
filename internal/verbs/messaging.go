// This file is owned by Track M (messaging verbs).
package verbs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
	"github.com/mgt-insurance/agent-teams/internal/repoconfig"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
)

// ── sendKong ──────────────────────────────────────────────────────────────────

// sendKong is the kong-native form of sendCmd.
// DI fields are tagged kong:"-" so kong ignores them; tests substitute fakes
// without touching the struct registration.
type sendKong struct {
	RecipientID        string `arg:"" name:"recipient-initiative-id" help:"Initiative ID of the recipient."`
	File               string `name:"file"   help:"Path to the message body file (required)." required:""`
	Sender             string `name:"sender" help:"Sender identifier (default: steward when sent from the steward session, else git user.name)."`
	Thread             string `name:"thread" help:"Optional thread identifier label."`
	ResumeLaunchPrompt string `name:"resume-launch-prompt" help:"Launch prompt used if the recipient session is gone and must be resumed (default: /dri <id>)."`
	ResumeModel        string `name:"resume-model" help:"Model for a resumed session (only meaningful with --resume-launch-prompt)."`

	agentsFunc     agentsJSONFunc       `kong:"-"`
	resumeFunc     resumeInitiativeFunc `kong:"-"`
	codexWake      codexWakeFunc        `kong:"-"`
	sleeper        sleeperFunc          `kong:"-"`
	doorbellExists doorbellExistsFunc   `kong:"-"`
	respawnFunc    respawnFunc          `kong:"-"`
}

// Validate enforces the resume-flag pairing (mirrors resumeKong.Validate).
func (c *sendKong) Validate() error {
	if c.ResumeModel != "" && c.ResumeLaunchPrompt == "" {
		return cli.Usagef("ateam mail send: --resume-model requires --resume-launch-prompt")
	}
	return nil
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

	sender := resolveSender(ctx, c.Sender)

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

	recipIssue, wtPath, liveErr := recipientWorktree(ctx, c.RecipientID)
	if liveErr != nil {
		fmt.Fprintf(ctx.Stdout, "note: could not resolve recipient worktree (%v); skipping liveness check\n", liveErr)
		return nil
	}

	// Disabled repo: the message stays queued (the bd issue above already
	// exists, a mailbox entry rather than a delivery), but this call must not
	// actively wake or revive anything. Remove the doorbell just touched
	// above so neither an already-armed watcher nor any other doorbell-driven
	// path has something to react to, and skip the resume/respawn escalation
	// below entirely — respawn in particular has no gate of its own (unlike
	// resumeFunc, which routes through the already-gated resumeKong), so
	// this is the one place that closes it. recipientWorktree's doc comment
	// makes recipIssue the zero value for the Steward, so repo is always ""
	// there and this never gates Steward mail.
	if repo := initiative.Of(recipIssue).Repo; repo != "" && !repoconfig.Enabled(repo) {
		_ = os.Remove(doorbellPath)
		fmt.Fprintf(ctx.Stdout, "note: recipient %s's repo is disabled (%s); message queued, not waking\n",
			c.RecipientID, repoconfig.FileName)
		return nil
	}

	fields := initiative.Of(recipIssue)
	runtimeKind, runtimeErr := sessionruntime.ResolveStored(fields.Runtime)
	if runtimeErr != nil {
		fmt.Fprintf(ctx.Stdout, "warning: recipient runtime is invalid (%v); message %s remains queued\n", runtimeErr, issue.ID)
		return nil
	}
	if runtimeKind == sessionruntime.Codex {
		wake := c.codexWake
		if wake == nil {
			wake = defaultCodexWake
		}
		err := wake(ctx, recipIssue)
		if errors.Is(err, errCodexDeliveryBusy) {
			fmt.Fprintln(ctx.Stdout, "Codex delivery already in progress; durable mail and doorbell remain pending")
			return nil
		}
		if err != nil {
			fmt.Fprintf(ctx.Stdout, "warning: Codex wake failed (%v); message %s remains queued\n", err, issue.ID)
			return nil
		}
		fmt.Fprintln(ctx.Stdout, "Codex thread accepted the mail wake request")
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

	// Resolve the liveness/respawn target via matchSessionsForInitiative
	// (agent-teams-zalv.1 §3/§4a): take the PRIMARY (first live tied
	// session); falls back to the worktree/Name match for legacy entries
	// with no session: lines. The Steward is not an initiative bead (no bd
	// show possible) and is NOT tied via session: lines — it keeps the
	// original worktree/Name match untouched (agent-teams-zalv.1 "Steward
	// routing is UNAFFECTED"). recipientWorktree above already fetched the
	// recipient issue once (single bd.ShowIssue per send); reuse it here
	// instead of fetching again.
	var entry *agentSession
	if c.RecipientID == StewardHandle {
		entry = matchSessionByWorktree(sessions, wtPath)
	} else if matched := matchSessionsForInitiative(sessions, recipIssue); len(matched) > 0 {
		entry = &matched[0]
	}
	if entry == nil {
		// The Steward has no initiative-resume path (there's no "/dri <id>" to
		// launch it with) — auto-relaunch is e3mq.10's scope. Just leave the
		// mail queued for whenever a steward session next comes up.
		if c.RecipientID == StewardHandle {
			fmt.Fprintf(ctx.Stdout, "note: steward session not running; mail queued\n")
			return nil
		}
		fmt.Fprintf(ctx.Stdout, "recipient not found in claude agents; launching via ateam resume\n")
		if err := c.resumeFunc(ctx, c.RecipientID, c.ResumeLaunchPrompt, c.ResumeModel); err != nil {
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

	myID, codexRecipient, err := resolveInboxRecipientRuntime(ctx, cwd)
	if err != nil {
		return nil
	}

	// Session-of-record guard (agent-teams-e3mq.31): only the Steward path
	// needs this — a duplicate steward session must not be able to consume
	// (or peek) the incumbent's mail just because it ran `ateam mail inbox`
	// per its own playbook. One guard site ahead of the c.Peek branch below
	// covers BOTH the consuming and --peek paths (defense in depth).
	if myID == StewardHandle {
		if err := checkStewardInboxGuard(ctx); err != nil {
			return err
		}
	}

	messages, err := queryUnreadMessages(ctx, myID)
	if err != nil {
		return fmt.Errorf("ateam inbox: query: %w", err)
	}

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
		if codexRecipient {
			reconcileCodexInboxDoorbell(ctx, myID)
		}
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
	if codexRecipient {
		reconcileCodexInboxDoorbell(ctx, myID)
	}

	return nil
}

func queryUnreadMessages(ctx *cli.Context, recipientID string) ([]bd.Issue, error) {
	var messages []bd.Issue
	if err := ctx.BD.RunJSON(&messages,
		"list",
		"--include-infra",
		"--assignee="+recipientID,
		"--exclude-label=read",
		"--status=open",
		"--json",
	); err != nil {
		return nil, err
	}
	return filterMessageType(messages), nil
}

// ── send ──────────────────────────────────────────────────────────────────────

// agentsJSONFunc is the function type for querying live bg sessions.
// Injected so tests can substitute a fake without touching os/exec.
type agentsJSONFunc func() ([]agentSession, error)

// resumeInitiativeFunc is the function type for escalating to ateam resume.
// launchPrompt/model are threaded through to resumeKong ("" = default /dri).
// Injected so tests can substitute a fake.
type resumeInitiativeFunc func(ctx *cli.Context, id, launchPrompt, model string) error

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
func defaultResume(ctx *cli.Context, id, launchPrompt, model string) error {
	cmd := &resumeKong{ID: id, LaunchPrompt: launchPrompt, Model: model, launch: launchBGSession, launchRaw: rawLaunchBGSession}
	return cmd.Run(ctx)
}

// hasLiveSession reports whether any session in sessions matches
// worktreePath (see matchSessionByWorktree).
func hasLiveSession(sessions []agentSession, worktreePath string) bool {
	return matchSessionByWorktree(sessions, worktreePath) != nil
}

// matchSessionByWorktree returns the session in sessions that best matches
// worktreePath. Background sessions carry Name = filepath.Base of the
// REGISTERED worktree they were dispatched with (`-n <name>`, dispatch.go)
// and Name stays stable even after the session's cwd wanders into a sibling
// track worktree — so Name is the durable initiative<->session link and is
// matched FIRST, preferring a PID-present (live) match over a PID-nil (dead)
// one so a live session always wins over a dead duplicate parked at the
// registered worktree's cwd (agent-teams-6rru.15; also fixes the mail-send
// duplicate-DRI bug at at-wisp-e50, since this helper backs messaging.go's
// liveness check too). Exact canonicalPath(CWD) equality remains a fallback
// for sessions with no Name (e.g. foreground/interactive sessions, which are
// never dispatched with `-n` and so can only be found by cwd).
//
// Precedence:
//  1. Name == filepath.Base(canonicalPath(worktreePath)) AND PID != nil.
//  2. Name == filepath.Base(canonicalPath(worktreePath)), any PID state.
//  3. exact canonicalPath(CWD) == canonicalPath(worktreePath).
//
// Returns nil if none match.
func matchSessionByWorktree(sessions []agentSession, worktreePath string) *agentSession {
	wantName := filepath.Base(canonicalPath(worktreePath))

	var deadNamed *agentSession
	for i := range sessions {
		if sessions[i].Name == "" || sessions[i].Name != wantName {
			continue
		}
		if sessions[i].PID != nil {
			return &sessions[i]
		}
		if deadNamed == nil {
			deadNamed = &sessions[i]
		}
	}
	if deadNamed != nil {
		return deadNamed
	}

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

// resolveSender determines the mail envelope sender. An explicit --sender
// always wins (relay=human, hung_tick=hung-scan, route=pr-shepherd, and a
// human typing the CLI can override too) and never touches the filesystem, so
// those paths cannot be aborted by a cwd lookup. With no explicit sender, cwd
// is resolved best-effort: a send from the Steward's own session is stamped as
// the Steward (so model-driven steward->DRI mail is attributed to the steward
// rather than collapsing to git user.name), while every other session — and a
// cwd that cannot be read — keeps the git user.name fallback rather than
// failing the send.
func resolveSender(ctx *cli.Context, explicit string) string {
	if explicit != "" {
		return explicit
	}
	cwd, err := os.Getwd()
	if err != nil {
		return gitUserName()
	}
	return defaultSender(ctx, cwd)
}

// defaultSender picks the no-explicit-sender default for a known cwd: the
// Steward when the send originates from the Steward's session, otherwise git
// user.name.
func defaultSender(ctx *cli.Context, cwd string) string {
	if isStewardSession(ctx, cwd) {
		return StewardHandle
	}
	return gitUserName()
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

// recipientWorktree looks up the initiative by id, extracts its worktree
// path from the description, and returns the fetched issue alongside it so
// callers (sendKong.Run) can reuse it for session-set matching instead of
// fetching it a second time. The Steward is a machine-scoped singleton, not
// an initiative bead — id == StewardHandle resolves directly to
// StewardSessionDir instead of going through bd show (which would error,
// since "steward" isn't an initiative), so the liveness/deaf-check/respawn
// escalation below runs for the Steward exactly like it does for any other
// recipient (mirrors isStewardSession/resolveInboxRecipient's precedent).
// The returned bd.Issue is the zero value for the Steward case (unused by
// callers, since the Steward short-circuits to the worktree/Name match).
func recipientWorktree(ctx *cli.Context, id string) (bd.Issue, string, error) {
	if id == StewardHandle {
		return bd.Issue{}, StewardSessionDir(ctx), nil
	}
	issue, err := bd.ShowIssue(ctx.BD, id)
	if err != nil {
		return bd.Issue{}, "", fmt.Errorf("bd show %s: %w", id, err)
	}
	wt := initiative.Of(issue).Worktree
	if wt == "" {
		return bd.Issue{}, "", fmt.Errorf("initiative %s has no worktree: line", id)
	}
	return issue, wt, nil
}

// resolveMyInitiative finds the open initiative whose worktree: line matches
// cwd, or whose worktree: line is an ancestor directory of cwd (so `ateam
// mail inbox` resolves correctly from any subdirectory of a registered
// worktree, e.g. apps/mithril nested under the worktree root). When multiple
// registered initiatives are ancestors of cwd, the most specific (longest)
// worktree path wins. Returns the initiative id or an error if none matches.
func resolveMyInitiative(ctx *cli.Context, cwd string) (string, error) {
	issue, err := resolveMyInitiativeIssue(ctx, cwd)
	if err != nil {
		return "", err
	}
	return issue.ID, nil
}

func resolveMyInitiativeIssue(ctx *cli.Context, cwd string) (bd.Issue, error) {
	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--status=open", "--json"); err != nil {
		return bd.Issue{}, err
	}
	if match := matchByWorktreeOrAncestor(issues, cwd); match != nil {
		return *match, nil
	}
	return bd.Issue{}, fmt.Errorf("no initiative registered for worktree: %s", cwd)
}

// isStewardSession reports whether cwd is the Steward's own session
// directory (StewardSessionDir) or a subdirectory of it, identified by the
// contract's marker file (StewardSessionMarkerPath) existing on disk. Mirrors
// the marker-based branch wake-watcher.sh uses to recognize the Steward's
// session, symlink-normalised (canonicalPath) like every other cwd-vs-path
// comparison in this package.
func isStewardSession(ctx *cli.Context, cwd string) bool {
	marker := StewardSessionMarkerPath(ctx)
	if _, err := os.Stat(marker); err != nil {
		return false
	}
	sessionDir := canonicalPath(filepath.Dir(marker))
	wantCwd := canonicalPath(cwd)
	return wantCwd == sessionDir || strings.HasPrefix(wantCwd, sessionDir+string(filepath.Separator))
}

// resolveInboxRecipient resolves the recipient id `ateam mail inbox` queries
// and marks-read as: StewardHandle when cwd is the Steward's own session
// (isStewardSession), otherwise the open initiative whose worktree: line
// matches cwd (resolveMyInitiative, unchanged for every non-Steward caller).
func resolveInboxRecipient(ctx *cli.Context, cwd string) (string, error) {
	id, _, err := resolveInboxRecipientRuntime(ctx, cwd)
	return id, err
}

// resolveInboxRecipientRuntime returns whether the resolved recipient is a
// Codex initiative. Legacy or invalid runtime metadata still permits inbox
// consumption; it simply does not opt into the Codex doorbell contract.
func resolveInboxRecipientRuntime(ctx *cli.Context, cwd string) (string, bool, error) {
	if isStewardSession(ctx, cwd) {
		return StewardHandle, false, nil
	}
	issue, err := resolveMyInitiativeIssue(ctx, cwd)
	if err != nil {
		return "", false, err
	}
	runtimeKind, runtimeErr := sessionruntime.ResolveStored(initiative.Of(issue).Runtime)
	return issue.ID, runtimeErr == nil && runtimeKind == sessionruntime.Codex, nil
}

// sessionIDEnvVar is the env var Claude Code exports into every session's
// Bash env carrying that session's id (verified live against the
// steward-duplicate incident that motivated agent-teams-e3mq.31).
// checkStewardInboxGuard reads it to identify the calling session.
const sessionIDEnvVar = "CLAUDE_CODE_SESSION_ID"

// pidfileEntryPid and pidfileEntrySession parse a watcher pidfile entry of
// the form "pid<TAB>session_id" (or a bare pid for a pre-e3mq.30 entry,
// whose session is then unattributable — pidfileEntrySession returns "").
// This MUST mirror pidfile_pid/pidfile_session in
// plugins/agent-teams/hooks/scripts/lib/watcher-pidfile.sh exactly — that
// shell lib and this Go twin parse the one pidfile format the hooks and this
// guard both depend on; letting them drift reopens the singleton race
// e3mq.29/e3mq.30 closed.
func pidfileEntryPid(entry string) string {
	if idx := strings.IndexByte(entry, '\t'); idx >= 0 {
		return entry[:idx]
	}
	return entry
}

func pidfileEntrySession(entry string) string {
	if idx := strings.IndexByte(entry, '\t'); idx >= 0 {
		return entry[idx+1:]
	}
	return ""
}

// checkStewardInboxGuard enforces session-of-record protection for steward
// mail (agent-teams-e3mq.31): observed live, a duplicate steward session got
// every hook-level advisory and guard, yet still ran `ateam mail inbox` per
// its own startup playbook and would have consumed the incumbent's unread
// mail — mail consumption is a model-driven CLI call the hooks cannot
// intercept, so the backstop has to live here. Decision table mirrors
// inbox-drain.sh's foreign-watcher-live disarm rule exactly:
//
//   - pidfile absent/unreadable, or pid dead/unparseable -> proceed (nil):
//     no session-of-record to protect.
//   - pid alive and pidfile session == caller session -> proceed (nil):
//     the caller IS the session of record.
//   - caller env var unset/empty -> proceed (nil): the caller can't be
//     attributed, which keeps manual/debug invocations working — this guard
//     only fires on a positive mismatch, never on ambiguity.
//   - pid alive and pidfile session != caller session (including an
//     old-format entry, whose session is unattributable) -> refuse (error).
func checkStewardInboxGuard(ctx *cli.Context) error {
	callerSession := os.Getenv(sessionIDEnvVar)
	if callerSession == "" {
		return nil
	}

	pidfilePath := filepath.Join(ctx.Home, "mailbox", StewardHandle+".watcher.pid")
	data, err := os.ReadFile(pidfilePath)
	if err != nil {
		return nil // no pidfile -> no session-of-record to protect
	}

	entry := strings.TrimRight(string(data), "\n")
	pid, err := strconv.Atoi(pidfileEntryPid(entry))
	if err != nil || pid <= 0 || !pidAlive(pid) {
		return nil // dead/unparseable pidfile -> no session-of-record to protect
	}

	pidfileSession := pidfileEntrySession(entry)
	if pidfileSession != "" && pidfileSession == callerSession {
		return nil // caller is the session of record
	}

	sessionDesc := pidfileSession
	if sessionDesc == "" {
		sessionDesc = "unknown"
	}
	return fmt.Errorf(
		"ateam inbox: refusing to read steward mail: another steward session of record exists "+
			"(watcher pid %d held by session %s). You appear to be a DUPLICATE steward session. "+
			"Announce to the human: 'Looks like I'm a duplicate steward — shut down my session "+
			"(claude stop <your-session-short-id>)' and do nothing else.",
		pid, sessionDesc)
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
