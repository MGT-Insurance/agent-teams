package verbs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

const (
	uiWatchTick   = 30 * time.Second
	uiWatchExpiry = 24 * time.Hour
	// uiStopQuestionMax mirrors the Dispatcher's UI_TEST_STOP_QUESTION_MAX.
	uiStopQuestionMax = 2000
	uiWatchSender     = "ui-test"
	uiWatchLog        = "ui-test-watch.log"
	uiWatchRetries    = 3
)

// Stop kinds. question, blocked, done, and failed mirror the Dispatcher's
// stopKind; expired is laptop-only and never comes from the Dispatcher.
const (
	uiKindNone     = ""
	uiKindQuestion = "question"
	uiKindBlocked  = "blocked"
	uiKindDone     = "done"
	uiKindFailed   = "failed"
	uiKindExpired  = "expired"
)

var uiWatchIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// uiWatchDeps are the seams of the stop-event watcher. A nil field takes the
// production default. The clock and sleep seams come from uiTestDeps.
type uiWatchDeps struct {
	// resolve returns the open initiative that owns path, or "" when none does.
	resolve func(ctx *cli.Context, path string) (string, error)
	// spawn starts the detached watcher child for the bead.
	spawn func(ctx *cli.Context, beadID, initiative string) error
	// send mails body to the initiative under dedupKey.
	send func(ctx *cli.Context, initiative, body, dedupKey string) error
	// lock takes the per-bead singleton lock; ok is false when held elsewhere.
	lock func(path string) (release func(), ok bool, err error)
}

func (w *uiWatchDeps) resolved() uiWatchDeps {
	var r uiWatchDeps
	if w != nil {
		r = *w
	}
	if r.resolve == nil {
		r.resolve = resolveUIWatchInitiative
	}
	if r.spawn == nil {
		r.spawn = uiWatchSpawn
	}
	if r.send == nil {
		r.send = uiWatchSend
	}
	if r.lock == nil {
		r.lock = followTryLock
	}
	return r
}

// uiTestStopEvent builds the mail body for a stop event. It is byte-identical
// to the Dispatcher's uiTestStopEvent (run-tools/contract.ts), plus the
// laptop-only expired kind: line 1 names the bead and kind, a question follows
// on its own line, and the last line says what to run next.
func uiTestStopEvent(id, kind, question string) string {
	lines := []string{"ui-test " + id + " " + kind}
	if kind == uiKindQuestion {
		lines = append(lines, uiCapUTF16(strings.TrimSpace(question), uiStopQuestionMax))
	}
	next := "next: ateam ui-test wait " + id + " --out <dir>"
	if kind == uiKindQuestion {
		next += " | ateam ui-test reply " + id + " --file <answer.md>"
	}
	lines = append(lines, next)
	return strings.Join(lines, "\n")
}

// uiExpiredEvent is the mail body when the watcher's 24h cap passes.
func uiExpiredEvent(id, initiative string) string {
	return "ui-test " + id + " " + uiKindExpired + "\n" +
		"next: ateam ui-test watch " + id + " --initiative " + initiative +
		" | ateam ui-test wait " + id + " --out <dir>"
}

// uiCapUTF16 cuts s to n UTF-16 code units, as JavaScript's slice does.
func uiCapUTF16(s string, n int) string {
	u := utf16.Encode([]rune(s))
	if len(u) <= n {
		return s
	}
	return string(utf16.Decode(u[:n]))
}

// uiStopKind maps the bead state to a stop kind, mirroring the Dispatcher's
// stopKind. question is the pending question text when kind is question.
func uiStopKind(bead uiTestBead, comments []uiTestComment) (kind, question string) {
	switch bead.Status {
	case "closed":
		if uiTestClosedFailed(bead.CloseReason) || slices.Contains(bead.Labels, "failed") {
			return uiKindFailed, ""
		}
		return uiKindDone, ""
	case "blocked":
		if q, ok := uiTestPendingQuestion(comments); ok {
			return uiKindQuestion, strings.TrimSpace(q)
		}
		return uiKindBlocked, ""
	}
	return uiKindNone, ""
}

// uiWatchDedupKey is the mail dedup key of one stop event.
func uiWatchDedupKey(id, kind, question string) string {
	sum := sha256.Sum256([]byte(question))
	return "ui-test:" + id + ":" + kind + ":" + hex.EncodeToString(sum[:])[:12]
}

// uiTestWatchKong implements `ateam ui-test watch`.
type uiTestWatchKong struct {
	ID         string `arg:"" name:"bead-id" help:"The ui-test bead id printed by 'ui-test file'."`
	Initiative string `name:"initiative" help:"Initiative to mail the stop events to; default: the open initiative that owns the current directory."`
	Child      bool   `name:"child" hidden:"" help:"Internal: run as the detached watcher."`

	deps  *uiTestDeps
	watch *uiWatchDeps
}

// Run starts the detached watcher for the bead, or, with --child, runs its loop.
func (c *uiTestWatchKong) Run(ctx *cli.Context) error {
	if _, inRun := uiRunToolsFromEnv(); inRun {
		fmt.Fprintln(ctx.Stderr, "ui-test watch: a cloud run gets stop events from the Dispatcher")
		return nil
	}
	settings, err := uiTestGate(ctx)
	if err != nil {
		return err
	}
	d, w := c.deps.resolved(), c.watch.resolved()
	if !uiWatchIDRE.MatchString(c.ID) {
		return fmt.Errorf("ui-test watch: invalid bead id %q", c.ID)
	}
	if c.Child {
		if c.Initiative == "" {
			return fmt.Errorf("ui-test watch: --child needs --initiative")
		}
		return runUIWatchChild(ctx, d, w, settings, c.ID, c.Initiative)
	}
	initiative := c.Initiative
	if initiative == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("ui-test watch: %w", err)
		}
		if initiative, err = w.resolve(ctx, cwd); err != nil {
			return fmt.Errorf("ui-test watch: %w", err)
		}
		if initiative == "" {
			return fmt.Errorf("ui-test watch: no open initiative owns %s; pass --initiative", cwd)
		}
	}
	if err := w.spawn(ctx, c.ID, initiative); err != nil {
		return fmt.Errorf("ui-test watch: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "watching %s for %s\n", c.ID, initiative)
	return nil
}

// startUITestWatch starts the watcher after `ui-test file` filed bead id. The
// initiative comes from --initiative, else the worktree, else the current
// directory. A failure prints one stderr line and never fails the filing.
func startUITestWatch(ctx *cli.Context, w uiWatchDeps, id, initiative, worktree string) {
	init, err := uiWatchInitiativeFor(ctx, w, initiative, worktree)
	if err == nil {
		err = w.spawn(ctx, id, init)
	}
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ui-test file: no stop watcher (%v); run: ateam ui-test watch %s\n", err, id)
	}
}

func uiWatchInitiativeFor(ctx *cli.Context, w uiWatchDeps, initiative, worktree string) (string, error) {
	if initiative != "" {
		return initiative, nil
	}
	paths := []string{worktree}
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, cwd)
	}
	var lastErr error
	for _, p := range paths {
		if p == "" {
			continue
		}
		id, err := w.resolve(ctx, p)
		if err != nil {
			lastErr = err
			continue
		}
		if id != "" {
			return id, nil
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("no open initiative owns %s; pass --initiative", worktree)
}

// resolveUIWatchInitiative resolves path the way `ateam resolve-initiative`
// does, by running that verb and reading its one output line.
func resolveUIWatchInitiative(ctx *cli.Context, path string) (string, error) {
	var out bytes.Buffer
	sub := *ctx
	sub.Stdout = &out
	if err := (&resolveInitiativeKong{Path: path}).Run(&sub); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// uiWatchSpawn starts the detached watcher: a new session, stdin /dev/null,
// output appended to <home>/debug/ui-test-watch.log. It returns without
// waiting, as followSpawn does.
func uiWatchSpawn(ctx *cli.Context, beadID, initiative string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(ctx.Home, "debug"), 0o755); err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(ctx.Home, "debug", uiWatchLog), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer logf.Close()
	null, err := os.Open(os.DevNull)
	if err != nil {
		return fmt.Errorf("open %s: %w", os.DevNull, err)
	}
	defer null.Close()
	cmd := exec.Command(exe, "ui-test", "watch", beadID, "--initiative", initiative, "--child")
	cmd.Env = append(os.Environ(), "AGENT_TEAMS_HOME="+ctx.Home)
	cmd.Stdin = null
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start watcher: %w", err)
	}
	return cmd.Process.Release()
}

// uiWatchSend mails body through the full `ateam mail send` path, so the
// recipient's doorbell and Codex wake run.
func uiWatchSend(ctx *cli.Context, initiative, body, dedupKey string) error {
	tmp, err := os.CreateTemp("", "ateam-ui-test-stop-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, werr := tmp.WriteString(body)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	return (&sendKong{RecipientID: initiative, File: tmp.Name(), Sender: uiWatchSender, DedupKey: dedupKey}).Run(ctx)
}

// runUIWatchChild takes the per-bead lock and runs the loop; a held lock means
// another watcher already covers the bead.
func runUIWatchChild(ctx *cli.Context, d uiTestDeps, w uiWatchDeps, s uiTestSettings, id, initiative string) error {
	dir := filepath.Join(ctx.Home, "uitest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("ui-test watch: %w", err)
	}
	lockPath := filepath.Join(dir, id+".lock")
	pidPath := filepath.Join(dir, id+".pid")
	release, ok, err := w.lock(lockPath)
	if err != nil {
		return fmt.Errorf("ui-test watch: lock: %w", err)
	}
	if !ok {
		return nil
	}
	defer func() {
		release()
		os.Remove(lockPath)
		os.Remove(pidPath)
	}()
	if err := writeFollowPidfile(pidPath); err != nil {
		return fmt.Errorf("ui-test watch: %w", err)
	}
	uiWatchLoop(ctx, d, w, s, id, initiative)
	return nil
}

// uiWatchLoop polls the bead every tick and mails each new stop event. It
// returns after a done or failed mail succeeds, or after the expired mail.
func uiWatchLoop(ctx *cli.Context, d uiTestDeps, w uiWatchDeps, s uiTestSettings, id, initiative string) {
	logf := func(format string, a ...any) {
		fmt.Fprintf(ctx.Stdout, "%s ui-test watch %s: %s\n", d.now().Format(time.RFC3339), id, fmt.Sprintf(format, a...))
	}
	deadline := d.now().Add(uiWatchExpiry)
	lastKey := ""
	expiredTries := 0
	for {
		bead, err := uiTestFetchBead(d, s.beadsDir, id)
		var comments []uiTestComment
		if err == nil {
			comments, err = uiTestFetchComments(d, s.beadsDir, id)
		}
		if err != nil {
			logf("read failed: %v", err)
		} else if kind, q := uiStopKind(bead, comments); kind == uiKindNone {
			lastKey = ""
		} else if key := kind + "\x00" + q; key != lastKey {
			if err := w.send(ctx, initiative, uiTestStopEvent(id, kind, q), uiWatchDedupKey(id, kind, q)); err != nil {
				logf("send %s failed: %v", kind, err)
			} else {
				lastKey = key
				logf("sent %s", kind)
				if kind == uiKindDone || kind == uiKindFailed {
					return
				}
			}
		}
		if !d.now().Before(deadline) {
			expiredTries++
			if err := w.send(ctx, initiative, uiExpiredEvent(id, initiative), "ui-test:"+id+":"+uiKindExpired); err == nil {
				logf("expired")
				return
			} else if expiredTries >= uiWatchRetries {
				logf("expired mail failed %d times, giving up: %v", expiredTries, err)
				return
			}
		}
		d.sleep(uiWatchTick)
	}
}
