// cloud_boot.go — `ateam cloud-boot start|resume`, the boot verb the Dispatcher
// catalog job ateam-dri runs in its start and resume steps. It turns one
// Dispatcher run into a parked-and-resumed `claude /dri` session: start adopts
// an initiative, records where: run:<bead>, launches the DRI under tmux, and
// exits 75 (park) once the session goes idle; resume relaunches the same
// session with the Dispatcher's wake event as its prompt.
package verbs

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
	"github.com/mgt-insurance/agent-teams/internal/sentlog"
	"github.com/mgt-insurance/agent-teams/internal/transport"
	"github.com/mgt-insurance/agent-teams/internal/workspace"
)

const (
	// cloudBootParkExit is the exit code the Dispatcher reads as "park this run".
	cloudBootParkExit = 75
	cloudBootTmuxName = "ateam-dri"
	cloudBootStateRel = ".ateam-cloud-boot.json"
)

// RegisterCloudBootKong registers the `cloud-boot` parent verb onto p.
func RegisterCloudBootKong(p *cli.Parser) {
	p.AddVerb("cloud-boot", "Boot or resume the DRI session of a Dispatcher run.", &cloudBootCmd{})
}

// cloudBootCmd is the kong parent struct. No Run method: kong would run it on
// every subcommand (see prCmd).
type cloudBootCmd struct {
	Start  cloudBootStartKong  `cmd:"" name:"start" help:"Adopt the run's initiative, launch the DRI under tmux, and park (exit 75) when it goes idle."`
	Resume cloudBootResumeKong `cmd:"" name:"resume" help:"Relaunch the parked DRI session with the Dispatcher wake event as its prompt."`
}

// cloudBootCommon holds the flags and test seams shared by both leaves. A nil
// seam falls back to its production implementation (see deps).
type cloudBootCommon struct {
	Initiative string        `name:"initiative" help:"Initiative id; overrides DSP_META_INITIATIVE."`
	PluginDir  string        `name:"plugin-dir" help:"Plugin root; default derives from the running ateam binary."`
	Poll       time.Duration `name:"poll" default:"15s" help:"Interval between idle checks."`
	IdlePolls  int           `name:"idle-polls" default:"3" help:"Consecutive idle polls before parking."`
	Grace      time.Duration `name:"grace" default:"60s" help:"No park decision before this much time has passed."`

	Tmux       func(args ...string) error        `kong:"-"`
	Agents     func() ([]agentSession, error)    `kong:"-"`
	Sleep      func(time.Duration)               `kong:"-"`
	Now        func() time.Time                  `kong:"-"`
	NewID      func() string                     `kong:"-"`
	Executable func() (string, error)            `kong:"-"`
	LookPath   func(file string) (string, error) `kong:"-"`
	Getwd      func() (string, error)            `kong:"-"`
	// CloseTopic closes the initiative's Telegram topic by thread ref; nil
	// falls back to the configured transport's topicCloser capability.
	CloseTopic func(threadRef string) error `kong:"-"`
}

type cloudBootStartKong struct{ cloudBootCommon }
type cloudBootResumeKong struct{ cloudBootCommon }

func (c *cloudBootStartKong) Run(ctx *cli.Context) error  { return c.boot(ctx, false) }
func (c *cloudBootResumeKong) Run(ctx *cli.Context) error { return c.boot(ctx, true) }

// cloudBootState is persisted at $HOME/.ateam-cloud-boot.json so resume can
// find the session. The sandbox filesystem survives park; processes do not.
type cloudBootState struct {
	Initiative string `json:"initiative"`
	Session    string `json:"session"`
	Bead       string `json:"bead"`
}

// cloudWake is the Dispatcher's $DSP_SUP/wake.json.
type cloudWake struct {
	Op     string      `json:"op"`
	Author string      `json:"author"`
	Text   string      `json:"text"`
	Seq    json.Number `json:"seq"`
}

func (c *cloudBootCommon) deps() {
	if c.Tmux == nil {
		c.Tmux = func(args ...string) error {
			out, err := exec.Command("tmux", args...).CombinedOutput()
			if err != nil {
				return fmt.Errorf("tmux %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
			}
			return nil
		}
	}
	if c.Agents == nil {
		c.Agents = defaultAgentsJSON
	}
	if c.Sleep == nil {
		c.Sleep = time.Sleep
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.NewID == nil {
		c.NewID = newUUIDv4
	}
	if c.Executable == nil {
		c.Executable = os.Executable
	}
	if c.LookPath == nil {
		c.LookPath = exec.LookPath
	}
	if c.Getwd == nil {
		c.Getwd = os.Getwd
	}
	if c.CloseTopic == nil {
		c.CloseTopic = defaultCloseTopic
	}
}

// defaultCloseTopic closes the topic through the configured transport. No
// transport or no topicCloser capability is a silent skip (nil).
func defaultCloseTopic(threadRef string) error {
	t, err := transport.For(workspace.Home())
	if err != nil {
		return nil
	}
	closer, ok := transport.Capability[topicCloser](t)
	if !ok {
		return nil
	}
	return closer.CloseTopic(threadRef)
}

// closeTopicFailOpen closes the initiative's topic when it carries a thread
// label. Never fails the caller: an error is one redacted stderr line. No
// farewell is posted (ateam close owns that); already-closed is a quiet 200.
func (c *cloudBootCommon) closeTopicFailOpen(ctx *cli.Context, iss bd.Issue) {
	threadRef := threadLabelValue(iss.Labels)
	if threadRef == "" {
		return
	}
	closeTopic := c.CloseTopic
	if closeTopic == nil { // reached without deps() (direct loop callers)
		closeTopic = defaultCloseTopic
	}
	if err := closeTopic(threadRef); err != nil {
		reason, _, _ := strings.Cut(strings.TrimSpace(sentlog.RedactError(err)), "\n")
		fmt.Fprintf(ctx.Stderr, "cloud-boot: closing topic failed: %s\n", reason)
	}
}

// resolveCloudInitiative returns the initiative id for this run: the flag when
// set, else the Dispatcher-exported DSP_META_INITIATIVE value (meta). A
// different source is a change to this function only.
func resolveCloudInitiative(flag, meta string) (string, error) {
	id := strings.TrimSpace(flag)
	if id == "" {
		id = strings.TrimSpace(meta)
	}
	if id == "" {
		return "", errors.New("no initiative: DSP_META_INITIATIVE is missing or empty and --initiative is not set")
	}
	if strings.ContainsAny(id, " \t\r\n") {
		return "", fmt.Errorf("invalid initiative id %q", id)
	}
	return id, nil
}

// resolveCloudPluginDir returns the plugin root: flag when set, else two
// directories above the resolved ateam binary (<root>/bin/ateam-linux-amd64).
// The root must contain .claude-plugin/plugin.json.
func resolveCloudPluginDir(flag string, executable func() (string, error)) (string, error) {
	dir := flag
	if dir == "" {
		exe, err := executable()
		if err != nil {
			return "", fmt.Errorf("resolve ateam binary: %w", err)
		}
		if exe, err = filepath.EvalSymlinks(exe); err != nil {
			return "", fmt.Errorf("resolve ateam binary: %w", err)
		}
		dir = filepath.Dir(filepath.Dir(exe))
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json")); err != nil {
		return "", fmt.Errorf("plugin dir %s: missing .claude-plugin/plugin.json (pass --plugin-dir)", dir)
	}
	return dir, nil
}

// cloudInboxNotice is the resume prompt when the supervisor inbox holds run
// messages. The dash is U+2014.
const cloudInboxNotice = "Dispatcher resumed this parked run. You have new mail — run `ateam mail inbox` to read it."

// writeInboxRing creates $DSP_SUP/inbox and writes the executable
// $DSP_SUP/bin/inbox-ring. Dispatcher runs the script after it writes an inbox
// file; the script touches the initiative's doorbell so the Stop hook wakes
// the DRI. Both paths are fixed at write time and single-quoted.
func writeInboxRing(home, sup, id string) error {
	if err := os.MkdirAll(filepath.Join(sup, "inbox"), 0o755); err != nil {
		return fmt.Errorf("create inbox dir: %w", err)
	}
	binDir := filepath.Join(sup, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}
	mailbox := shellQuote(filepath.Join(home, "mailbox"))
	wake := shellQuote(filepath.Join(home, "mailbox", id+".wake"))
	script := "#!/bin/sh\n" +
		"# Written by ateam cloud-boot. Dispatcher runs this after it writes $DSP_SUP/inbox/<seq>.json.\n" +
		"mkdir -p " + mailbox + " && touch " + wake + "\n"
	path := filepath.Join(binDir, "inbox-ring")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return fmt.Errorf("write inbox-ring: %w", err)
	}
	// WriteFile keeps the mode of an existing file and applies the umask.
	if err := os.Chmod(path, 0o755); err != nil {
		return fmt.Errorf("chmod inbox-ring: %w", err)
	}
	return nil
}

// shellQuote single-quotes s for /bin/sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// cloudWakePrompt builds the resume prompt from wake (nil = no usable event).
func cloudWakePrompt(id string, wake *cloudWake) string {
	if wake == nil {
		return "Dispatcher resumed this parked run with no wake event. Re-read the initiative and check mail, then continue."
	}
	return fmt.Sprintf("Dispatcher resumed this parked run. Wake event seq %s, op %s, author %s:\n\n%s\n\nRe-read the initiative with ateam show %s, act on this event, then continue.",
		wake.Seq, wake.Op, wake.Author, wake.Text, id)
}

// readCloudWake reads wake.json from sup; a missing or invalid file yields nil.
func readCloudWake(sup string) *cloudWake {
	data, err := os.ReadFile(filepath.Join(sup, "wake.json"))
	if err != nil {
		return nil
	}
	var w cloudWake
	if err := json.Unmarshal(data, &w); err != nil || (w.Op == "" && w.Author == "" && w.Text == "") {
		return nil
	}
	return &w
}

func cloudBootStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, cloudBootStateRel), nil
}

func (c *cloudBootCommon) boot(ctx *cli.Context, resume bool) error {
	verb := "start"
	if resume {
		verb = "resume"
	}
	if err := c.bootRun(ctx, resume); err != nil {
		var silent *cli.SilentError
		if errors.As(err, &silent) {
			return err
		}
		return fmt.Errorf("ateam cloud-boot %s: %w", verb, err)
	}
	return nil
}

func (c *cloudBootCommon) bootRun(ctx *cli.Context, resume bool) error {
	c.deps()

	// Preconditions: fail before any bd write or tmux call.
	bead, sup, ok := dispatcherRun()
	if !ok {
		return errors.New("not inside a Dispatcher run (DSP_BEAD is empty)")
	}
	if sup == "" {
		return errors.New("DSP_SUP is empty")
	}
	if os.Getenv("AGENT_TEAMS_HOST_FOLLOW") != "off" {
		return errors.New("AGENT_TEAMS_HOST_FOLLOW must be \"off\"")
	}
	if _, ok := bd.HostedServer(ctx.Home); !ok {
		return fmt.Errorf("workspace %s is not bound to a hosted beads server", ctx.Home)
	}
	for _, tool := range []string{"tmux", "claude", "bd"} {
		if _, err := c.LookPath(tool); err != nil {
			return fmt.Errorf("%s not found on PATH", tool)
		}
	}
	pluginDir, err := resolveCloudPluginDir(c.PluginDir, c.Executable)
	if err != nil {
		return err
	}
	cwd, err := c.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	model, advisor, err := driAdvisorSettings(ctx.Home)
	if err != nil {
		return err
	}
	if advisor != "" {
		return errors.New("use_advisors is enabled; the advisor flag leaks into every subagent, so cloud boot refuses it")
	}
	// Fail-soft: the boot itself does not need the setup, but a missing piece
	// breaks later steps (for example `ateam audit` needs PRIME.md), so a
	// failure is loud on stderr and the boot continues.
	ensureHostedHomeSetupSoft(ctx, true)
	installCloudInstructions(ctx, pluginDir)
	if _, err := c.LookPath("jq"); err != nil {
		fmt.Fprintln(ctx.Stderr, cloudJQWarning)
	}
	statePath, err := cloudBootStatePath()
	if err != nil {
		return fmt.Errorf("resolve home: %w", err)
	}

	var state cloudBootState
	var haveState bool
	if resume {
		if data, err := os.ReadFile(statePath); err == nil && json.Unmarshal(data, &state) == nil && state.Initiative != "" && state.Session != "" {
			haveState = true
			if state.Bead != bead {
				return fmt.Errorf("state file %s belongs to run %q, not this run %q; refusing to resume another run's session", statePath, state.Bead, bead)
			}
		}
	}
	var id string
	if haveState && strings.TrimSpace(c.Initiative) == "" {
		id = state.Initiative
	} else if id, err = resolveCloudInitiative(c.Initiative, os.Getenv("DSP_META_INITIATIVE")); err != nil {
		return err
	}
	iss, err := bd.ShowIssue(ctx.BD, id)
	if err != nil {
		return fmt.Errorf("bd show %s: %w", id, err)
	}
	if iss.Status == "closed" {
		c.closeTopicFailOpen(ctx, iss)
		return fmt.Errorf("initiative %s is closed", id)
	}
	// Fail-soft: the turn-start count still delivers inbox files without the
	// doorbell script.
	if err := writeInboxRing(ctx.Home, sup, id); err != nil {
		fmt.Fprintf(ctx.Stderr, "WARNING: inbox-ring setup failed (continuing): %v\n", err)
	}

	var sid string
	args := []string{"new-session", "-d", "-s", cloudBootTmuxName, "-c", cwd,
		"-e", "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1", "--", "claude"}
	var prompt string
	if resume {
		sid = state.Session
		if !haveState {
			sessions := initiative.Of(iss).Sessions
			if len(sessions) == 0 {
				return fmt.Errorf("no recorded session for %s to resume", id)
			}
			sid = sessions[len(sessions)-1]
		}
		if len(listInboxNames(sup)) > 0 {
			prompt = cloudInboxNotice
		} else {
			prompt = cloudWakePrompt(id, readCloudWake(sup))
		}
		args = append(args, "--resume", sid)
	} else {
		sid = c.NewID()
		prompt = "/dri " + id
		args = append(args, "--session-id", sid)
	}

	// Record routing on the initiative: where, plus worktree and session on start.
	plan, err := initiative.WithWhere(iss, "run:"+bead)
	if err != nil {
		return err
	}
	desc := plan.Description
	if !resume {
		if plan, err = initiative.WithWorktree(bd.Issue{Description: desc}, cwd); err != nil {
			return err
		}
		if plan, err = initiative.WithSession(bd.Issue{Description: plan.Description}, sid); err != nil {
			return err
		}
		desc = plan.Description
	}
	if desc != iss.Description {
		if err := c.writeBody(ctx, id, desc); err != nil {
			return err
		}
	}
	if !resume {
		data, err := json.Marshal(cloudBootState{Initiative: id, Session: sid, Bead: bead})
		if err != nil {
			return err
		}
		if err := os.WriteFile(statePath, data, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", statePath, err)
		}
	}

	args = append(args, "--plugin-dir", pluginDir, "--disallowedTools", "AskUserQuestion")
	args = append(args, driSessionArgs(prompt, driSystemPromptAppend, model, "", "dri", id, "")...)
	if err := c.Tmux(args...); err != nil {
		return err
	}
	fmt.Fprintf(ctx.Stderr, "cloud-boot: launched session %s for %s\n", sid, id)
	return c.idleLoop(ctx, id, sid, cwd)
}

// cloudInstructionsRel is where the committed per-role instruction files live,
// relative to the plugin root.
const cloudInstructionsRel = "cloud/instructions"

// cloudJQWarning is printed when jq is missing: learnings-gate.sh exits 0
// without jq, so no role agent is gated.
const cloudJQWarning = "cloud-boot: WARNING: jq not on PATH; the learnings gate hook will not enforce learnings loading"

// installCloudInstructions mirrors <pluginDir>/cloud/instructions/*.md into
// <home>/instructions/ so `ateam instructions <role>` finds them, and removes
// files there that the source no longer has. It is fail-soft: a missing source
// directory or an unwritable file prints a WARNING and the boot continues.
func installCloudInstructions(ctx *cli.Context, pluginDir string) {
	src := filepath.Join(pluginDir, cloudInstructionsRel)
	entries, err := os.ReadDir(src)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "cloud-boot: WARNING: cannot read instruction files from %s (continuing without them): %v\n", src, err)
		return
	}
	dst := filepath.Join(ctx.Home, "instructions")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		fmt.Fprintf(ctx.Stderr, "cloud-boot: WARNING: create %s failed (continuing): %v\n", dst, err)
		return
	}
	want := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "cloud-boot: WARNING: read %s failed (continuing): %v\n", e.Name(), err)
			continue
		}
		// A failed write keeps the name in want so the stale pass leaves the
		// previous copy in place.
		want[e.Name()] = true
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			fmt.Fprintf(ctx.Stderr, "cloud-boot: WARNING: write %s failed (continuing): %v\n", e.Name(), err)
		}
	}
	if existing, err := os.ReadDir(dst); err == nil {
		for _, e := range existing {
			if e.IsDir() || want[e.Name()] {
				continue
			}
			if err := os.Remove(filepath.Join(dst, e.Name())); err != nil {
				fmt.Fprintf(ctx.Stderr, "cloud-boot: WARNING: remove stale %s failed (continuing): %v\n", e.Name(), err)
			}
		}
	}
	fmt.Fprintf(ctx.Stderr, "cloud-boot: installed %d instruction files from %s\n", len(want), src)
}

func (c *cloudBootCommon) writeBody(ctx *cli.Context, id, body string) error {
	f, err := os.CreateTemp("", "ateam-cloud-boot-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if _, err := ctx.BD.Run("update", id, "--body-file="+f.Name()); err != nil {
		return fmt.Errorf("bd update %s: %w", id, err)
	}
	return nil
}

// cloudBootMaxPollErrors is how many consecutive failed polls idleLoop
// tolerates before it gives up.
const cloudBootMaxPollErrors = 5

const (
	// cloudBootCloseDrain bounds, in wall time, how long a closed initiative
	// waits for the lead's turn to end before the kill. A normal ateam close
	// finishes in 2 to 4 s. The measured worst case after bd close is the
	// update-local-main git step (no timeout) plus a 45 s farewell Send, two
	// 45 s CloseTopic tries, and a 45 s failure-note Send: about 180 s plus
	// git. The bound leaves headroom over the 180 s but does not cover a hung
	// git step; past it, cloud-boot kills the lead.
	cloudBootCloseDrain = 4 * time.Minute
	// cloudBootCloseDrainPoll is the interval between lead status checks
	// while draining.
	cloudBootCloseDrainPoll = 2 * time.Second
)

// idleLoop polls until the initiative closes (exit 0), the DRI goes idle for
// IdlePolls consecutive polls after Grace (exit 75), or the lead vanishes
// while the initiative is open (error). It tolerates up to
// cloudBootMaxPollErrors consecutive poll errors. A closed initiative waits for
// the lead's turn to end (waitLeadIdle, bounded) before the kill, so an in-flight
// ateam close finishes. Every return other than park and closed kills the tmux
// session, so no claude outlives the run.
func (c *cloudBootCommon) idleLoop(ctx *cli.Context, id, sid, cwd string) error {
	err := c.pollLoop(ctx, id, sid, cwd)
	var silent *cli.SilentError
	if err != nil && !(errors.As(err, &silent) && silent.Code == cloudBootParkExit) {
		_ = c.Tmux("kill-session", "-t", cloudBootTmuxName)
	}
	return err
}

// waitLeadIdle blocks until the lead session is gone or not busy, or
// cloudBootCloseDrain has passed. ateam close writes the bd close before it
// posts the farewell and closes the topic, so the kill that follows a closed
// initiative must wait for the lead's turn to end, or it ends ateam close
// mid-send. Agents errors count as still busy. The bound is measured with
// c.Now, so slow Agents calls count against it.
func (c *cloudBootCommon) waitLeadIdle(ctx *cli.Context, sid string) {
	deadline := c.Now().Add(cloudBootCloseDrain)
	for {
		if sessions, err := c.Agents(); err == nil {
			busy := false
			for _, s := range sessions {
				if s.SessionID == sid && s.Status == "busy" {
					busy = true
				}
			}
			if !busy {
				return
			}
		}
		if !c.Now().Before(deadline) {
			fmt.Fprintf(ctx.Stderr, "cloud-boot: lead still busy after %s; stopping it\n", cloudBootCloseDrain)
			return
		}
		c.Sleep(cloudBootCloseDrainPoll)
	}
}

func (c *cloudBootCommon) pollLoop(ctx *cli.Context, id, sid, cwd string) error {
	var elapsed time.Duration
	idle := 0
	pollErrs := 0
	// pollFailed records a failed poll; it returns an error once the budget is spent.
	pollFailed := func(what string, err error) error {
		pollErrs++
		idle = 0
		if pollErrs > cloudBootMaxPollErrors {
			return fmt.Errorf("%s: %d consecutive poll errors, last: %w", what, pollErrs, err)
		}
		fmt.Fprintf(ctx.Stderr, "cloud-boot: %s: %v (poll error %d/%d)\n", what, err, pollErrs, cloudBootMaxPollErrors)
		return nil
	}
	for {
		c.Sleep(c.Poll)
		elapsed += c.Poll

		iss, err := bd.ShowIssue(ctx.BD, id)
		if err != nil {
			if perr := pollFailed("bd show "+id, err); perr != nil {
				return perr
			}
			continue
		}
		if iss.Status == "closed" {
			c.waitLeadIdle(ctx, sid)
			c.closeTopicFailOpen(ctx, iss)
			_ = c.Tmux("kill-session", "-t", cloudBootTmuxName)
			fmt.Fprintf(ctx.Stderr, "cloud-boot: initiative %s closed\n", id)
			return nil
		}

		sessions, err := c.Agents()
		if err != nil {
			if perr := pollFailed("claude agents", err); perr != nil {
				return perr
			}
			continue
		}
		pollErrs = 0
		var lead *agentSession
		othersBusy := false
		for i := range sessions {
			s := &sessions[i]
			switch {
			case s.SessionID == sid:
				lead = s
			case s.PID != nil && s.Status == "busy" && cloudUnder(cwd, s.CWD):
				othersBusy = true
			}
		}
		if lead == nil {
			if err := c.Tmux("has-session", "-t", cloudBootTmuxName); err != nil {
				return fmt.Errorf("lead session %s exited while initiative %s is open", sid, id)
			}
			idle = 0
			continue
		}
		if elapsed >= c.Grace && !othersBusy && (lead.Status == "idle" || lead.Status == "waiting") {
			idle++
		} else {
			idle = 0
		}
		if idle >= c.IdlePolls {
			if err := c.Tmux("kill-session", "-t", cloudBootTmuxName); err != nil {
				fmt.Fprintf(ctx.Stderr, "cloud-boot: kill-session: %v\n", err)
			}
			fmt.Fprintf(ctx.Stderr, "cloud-boot: %s idle, parking\n", id)
			return cli.Silent(cloudBootParkExit)
		}
	}
}

// newUUIDv4 returns a random RFC 4122 version 4 UUID.
func newUUIDv4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// cloudUnder reports whether dir is root or inside it.
func cloudUnder(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
