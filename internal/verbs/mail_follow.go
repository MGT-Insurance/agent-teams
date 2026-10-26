package verbs

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// followHeadQuery reads the journal head for the F8 baseline. The table and
// column names are unverified against a hosted server (docs/hosted-beads.md,
// F8); a failing query falls back to replay from --since 0.
const followHeadQuery = "SELECT COALESCE(MAX(seq),0) AS head FROM bd_events_journal"

const (
	followBackoffMin = time.Second
	followBackoffMax = 30 * time.Second

	// followIdleChecks is how many consecutive zero-watcher checks start the
	// exit handshake (F9).
	followIdleChecks = 3
)

// followTruncatedRe extracts the retained-window floor from the
// events_journal_truncated failure that `bd events tail --json` prints.
var followTruncatedRe = regexp.MustCompile(`"floor"\s*:\s*(\d+)`)

// mailFollowKong implements `ateam mail follow`: on a hosted workspace it
// starts the one per-host follower that follows the bd events journal and
// touches the doorbell of each local initiative that receives a message
// (docs/hosted-beads.md, F7-F10). It never writes a bead.
type mailFollowKong struct {
	// Child marks the detached follower process the parent invocation spawns.
	Child bool `name:"child" hidden:"" help:"Internal: run as the detached follower."`

	// Test seams; zero values use the production implementations.
	runBD     func(ctx context.Context, args []string, onLine func(string)) (stderr string, err error)
	headQuery func(query string) (string, error)
	sleep     func(ctx context.Context, d time.Duration)
	lifeSleep func(ctx context.Context, d time.Duration)
	now       func() time.Time
	lookup    func(id string) (initiative.Fields, error)
	pidAlive  func(pid int) bool
	tryLock   func(path string) (release func(), ok bool, err error)
	spawn     func(ctx *cli.Context) error
	pollEvery time.Duration

	// beforeRelease and afterRelease, when set, run inside the exit handshake
	// just before the lock is released and just after, before the final
	// watcher count.
	beforeRelease func()
	afterRelease  func()
}

// followRow is the subset of a bd events journal record the follower reads.
type followRow struct {
	Seq   int64  `json:"seq"`
	TS    string `json:"ts"`
	Op    string `json:"op"`
	Issue *struct {
		IssueType string `json:"issue_type"`
		Assignee  string `json:"assignee"`
	} `json:"issue"`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *mailFollowKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam mail follow: nil context")
	}
	if _, ok := bd.HostedServer(ctx.Home); !ok {
		return nil
	}
	if !c.Child {
		if c.spawn == nil {
			c.spawn = followSpawn
		}
		return c.spawn(ctx)
	}
	run, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	return c.runChild(run, ctx)
}

// followSpawn starts the detached follower: a new session, stdin /dev/null,
// output appended to <home>/debug/mail-follow.log. It returns without waiting.
func followSpawn(ctx *cli.Context) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("ateam mail follow: locate executable: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(ctx.Home, "debug"), 0o755); err != nil {
		return fmt.Errorf("ateam mail follow: %w", err)
	}
	logf, err := os.OpenFile(filepath.Join(ctx.Home, "debug", "mail-follow.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("ateam mail follow: open log: %w", err)
	}
	defer logf.Close()
	null, err := os.Open(os.DevNull)
	if err != nil {
		return fmt.Errorf("ateam mail follow: open %s: %w", os.DevNull, err)
	}
	defer null.Close()
	cmd := exec.Command(exe, "mail", "follow", "--child")
	cmd.Env = append(os.Environ(), "AGENT_TEAMS_HOME="+ctx.Home)
	cmd.Stdin = null
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ateam mail follow: start follower: %w", err)
	}
	return cmd.Process.Release()
}

// followHold tracks whether this process holds the singleton lock.
type followHold struct {
	mu      sync.Mutex
	release func()
}

func (h *followHold) set(release func()) {
	h.mu.Lock()
	h.release = release
	h.mu.Unlock()
}

// drop releases the lock if held and reports whether it was.
func (h *followHold) drop() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.release == nil {
		return false
	}
	h.release()
	h.release = nil
	return true
}

// runChild takes the host singleton lock, runs the journal loop, and applies
// the F9 lifetime rule. A lock held elsewhere is a clean exit.
func (c *mailFollowKong) runChild(run context.Context, ctx *cli.Context) error {
	c.defaults(ctx)
	mailbox := filepath.Join(ctx.Home, "mailbox")
	if err := os.MkdirAll(mailbox, 0o755); err != nil {
		return err
	}
	lockPath := filepath.Join(mailbox, "host-follow.lock")
	pidPath := filepath.Join(mailbox, "host-follow.pid")

	release, ok, err := c.tryLock(lockPath)
	if err != nil {
		return fmt.Errorf("ateam mail follow: lock: %w", err)
	}
	if !ok {
		return nil
	}
	hold := &followHold{}
	hold.set(release)
	if err := writeFollowPidfile(pidPath); err != nil {
		hold.drop()
		return err
	}
	defer func() {
		if hold.drop() {
			_ = os.Remove(pidPath)
		}
	}()

	run, cancel := context.WithCancel(run)
	defer cancel()
	go c.watchLifetime(run, cancel, ctx, hold, lockPath, pidPath)
	return c.follow(run, ctx)
}

// watchLifetime counts live watchers every pollEvery and runs the exit
// handshake after followIdleChecks consecutive empty counts.
func (c *mailFollowKong) watchLifetime(run context.Context, cancel context.CancelFunc, ctx *cli.Context, hold *followHold, lockPath, pidPath string) {
	idle := 0
	for {
		c.lifeSleep(run, c.pollEvery)
		if run.Err() != nil {
			return
		}
		if len(c.liveWatchers(ctx.Home)) > 0 {
			idle = 0
			continue
		}
		if idle++; idle < followIdleChecks {
			continue
		}
		// Exit handshake: unpublish, release, then count once more. A watcher
		// writes its pidfile before it starts a follower (F10), so either this
		// count sees it, or the lock was free before that follower tried it.
		_ = os.Remove(pidPath)
		if c.beforeRelease != nil {
			c.beforeRelease()
		}
		hold.drop()
		if c.afterRelease != nil {
			c.afterRelease()
		}
		if len(c.liveWatchers(ctx.Home)) == 0 {
			cancel()
			return
		}
		release, ok, err := c.tryLock(lockPath)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "ateam mail follow: re-lock: %v\n", err)
		}
		if err != nil || !ok {
			cancel()
			return
		}
		hold.set(release)
		if err := writeFollowPidfile(pidPath); err != nil {
			fmt.Fprintf(ctx.Stderr, "ateam mail follow: write pidfile: %v\n", err)
		}
		idle = 0
	}
}

func (c *mailFollowKong) defaults(ctx *cli.Context) {
	if c.now == nil {
		c.now = time.Now
	}
	if c.sleep == nil {
		c.sleep = followSleep
	}
	if c.lifeSleep == nil {
		c.lifeSleep = followSleep
	}
	if c.runBD == nil {
		c.runBD = func(rc context.Context, args []string, onLine func(string)) (string, error) {
			return followExecBD(rc, ctx.Home, args, onLine)
		}
	}
	if c.headQuery == nil {
		c.headQuery = func(q string) (string, error) { return ctx.BD.Run("sql", "--json", q) }
	}
	if c.lookup == nil {
		c.lookup = func(id string) (initiative.Fields, error) {
			issue, err := bd.ShowIssue(ctx.BD, id)
			if err != nil {
				return initiative.Fields{}, err
			}
			return initiative.Of(issue), nil
		}
	}
	if c.pidAlive == nil {
		c.pidAlive = pidAlive
	}
	if c.tryLock == nil {
		c.tryLock = followTryLock
	}
	if c.pollEvery <= 0 {
		c.pollEvery = time.Second
	}
}

// follow runs the journal loop until run is cancelled: baseline, tail,
// checkpoint, ring, backoff, truncation recovery.
func (c *mailFollowKong) follow(run context.Context, ctx *cli.Context) error {
	c.defaults(ctx)
	start := c.now()
	seqPath := filepath.Join(ctx.Home, "mailbox", "host.seq")

	cp, have := readFollowCheckpoint(seqPath)
	// ringNotBefore is set only when the baseline query failed: rows older
	// than the follower's start must not ring (F8 fallback).
	var ringNotBefore time.Time
	if !have {
		if head, err := c.baselineHead(); err == nil {
			cp = head
			if err := writeFollowCheckpoint(seqPath, cp); err != nil {
				fmt.Fprintf(ctx.Stderr, "ateam mail follow: write checkpoint: %v\n", err)
			}
		} else {
			fmt.Fprintf(ctx.Stderr, "ateam mail follow: baseline query failed (%v); replaying from seq 0\n", err)
			cp = 0
			ringNotBefore = start.UTC().Truncate(time.Second)
		}
	}

	backoff := followBackoffMin
	for run.Err() == nil {
		gotRow := false
		onLine := func(line string) {
			var row followRow
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				if strings.HasPrefix(line, `{"`) {
					fmt.Fprintf(ctx.Stderr, "ateam mail follow: skipping unparseable journal line: %v\n", err)
				}
				return
			}
			if row.Seq <= cp {
				return
			}
			if id, ok := c.ringTarget(ctx, row, ringNotBefore); ok {
				c.ring(ctx, id)
			}
			cp = row.Seq
			if err := writeFollowCheckpoint(seqPath, cp); err != nil {
				fmt.Fprintf(ctx.Stderr, "ateam mail follow: write checkpoint: %v\n", err)
			}
			gotRow = true
		}
		stderr, err := c.runBD(run, []string{"events", "tail", "--follow", "--json", "--since", strconv.FormatInt(cp, 10)}, onLine)
		if run.Err() != nil {
			return nil
		}
		if gotRow {
			backoff = followBackoffMin
		}
		if m := followTruncatedRe.FindStringSubmatch(stderr); strings.Contains(stderr, "events_journal_truncated") && m != nil {
			floor, _ := strconv.ParseInt(m[1], 10, 64)
			if floor > 0 {
				// The pruned gap may hide mail for any initiative with a
				// live watcher: ring each once, then resume (F8).
				cp = floor - 1
				for _, id := range c.liveWatchers(ctx.Home) {
					c.ring(ctx, id)
				}
				if err := writeFollowCheckpoint(seqPath, cp); err != nil {
					fmt.Fprintf(ctx.Stderr, "ateam mail follow: write checkpoint: %v\n", err)
				}
			}
		} else {
			fmt.Fprintf(ctx.Stderr, "ateam mail follow: bd events tail exited (%v); retrying in %s\n", followExitDetail(err, stderr), backoff)
		}
		c.sleep(run, backoff)
		if backoff *= 2; backoff > followBackoffMax {
			backoff = followBackoffMax
		}
	}
	return nil
}

// ringTarget returns the recipient row r rings on this host (F7), honoring
// the F8 fallback floor when set.
func (c *mailFollowKong) ringTarget(ctx *cli.Context, r followRow, notBefore time.Time) (string, bool) {
	if r.Op != "create" || r.Issue == nil || r.Issue.IssueType != "message" {
		return "", false
	}
	id := r.Issue.Assignee
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id {
		return "", false
	}
	if !notBefore.IsZero() {
		ts, err := time.Parse(time.RFC3339, r.TS)
		if err != nil || ts.Before(notBefore) {
			return "", false
		}
	}
	if !c.localRecipient(ctx, id) {
		return "", false
	}
	return id, true
}

// localRecipient reports whether initiative id is local to this host. A failed
// lookup counts as local: a stray doorbell is harmless, lost mail is not.
func (c *mailFollowKong) localRecipient(ctx *cli.Context, id string) bool {
	if id == StewardHandle {
		return true
	}
	f, err := c.lookup(id)
	if err != nil {
		return true
	}
	return !remoteInitiative(ctx.Home, f)
}

func (c *mailFollowKong) ring(ctx *cli.Context, id string) {
	if err := touchFollowDoorbell(filepath.Join(ctx.Home, "mailbox", id+".wake")); err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam mail follow: touch doorbell: %v\n", err)
	}
}

// liveWatchers returns the ids of initiatives whose watcher pidfile names a
// live process. A pidfile holds "pid<TAB>session"; a bare pid is accepted.
func (c *mailFollowKong) liveWatchers(home string) []string {
	files, err := filepath.Glob(filepath.Join(home, "mailbox", "*.watcher.pid"))
	if err != nil {
		return nil
	}
	var ids []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		pidText, _, _ := strings.Cut(strings.TrimSpace(string(data)), "\t")
		pid, err := strconv.Atoi(strings.TrimSpace(pidText))
		if err != nil || pid <= 0 || !c.pidAlive(pid) {
			continue
		}
		ids = append(ids, strings.TrimSuffix(filepath.Base(f), ".watcher.pid"))
	}
	return ids
}

// followTryLock takes an exclusive non-blocking flock on path. Closing the
// file releases it, and the kernel releases it if the process dies.
func followTryLock(path string) (func(), bool, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { f.Close() }, true, nil
}

func writeFollowPidfile(path string) error {
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644)
}

func (c *mailFollowKong) baselineHead() (int64, error) {
	out, err := c.headQuery(followHeadQuery)
	if err != nil {
		return 0, err
	}
	return parseFollowHead(out)
}

// parseFollowHead reads the head value from `bd sql --json` output, accepting
// a bare row object or an array of rows.
func parseFollowHead(out string) (int64, error) {
	type headRow struct {
		Head json.Number `json:"head"`
	}
	var rows []headRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		var one headRow
		if err2 := json.Unmarshal([]byte(out), &one); err2 != nil {
			return 0, fmt.Errorf("parse head: %w", err)
		}
		rows = []headRow{one}
	}
	if len(rows) == 0 {
		return 0, errors.New("parse head: no rows")
	}
	n, err := rows[0].Head.Int64()
	if err != nil {
		return 0, fmt.Errorf("parse head: %w", err)
	}
	return n, nil
}

func followSleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func followExitDetail(err error, stderr string) string {
	if s := strings.TrimSpace(stderr); s != "" {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[:i]
		}
		return s
	}
	if err != nil {
		return err.Error()
	}
	return "exit 0"
}

// followExecBD runs `bd -C home <args>`, feeding each stdout line to onLine.
// Cancelling ctx kills the child.
func followExecBD(ctx context.Context, home string, args []string, onLine func(string)) (string, error) {
	cmd := exec.CommandContext(ctx, "bd", append([]string{"-C", home}, args...)...)
	cmd.WaitDelay = 2 * time.Second
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	r := bufio.NewReader(out)
	for {
		line, rerr := r.ReadString('\n')
		if line = strings.TrimRight(line, "\r\n"); line != "" {
			onLine(line)
		}
		if rerr != nil {
			if rerr != io.EOF {
				_ = cmd.Process.Kill()
			}
			break
		}
	}
	err = cmd.Wait()
	return stderr.String(), err
}

func touchFollowDoorbell(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return touchFile(path)
}

func readFollowCheckpoint(path string) (int64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// writeFollowCheckpoint writes the seq by rename so a reader never sees a
// partial value.
func writeFollowCheckpoint(path string, seq int64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.FormatInt(seq, 10)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
