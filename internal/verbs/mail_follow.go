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
	"syscall"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// followHeadQuery reads the journal head for the F8 baseline. The table and
// column names are unverified against a hosted server (docs/hosted-beads.md,
// F8); a failing query falls back to replay from --since 0.
const followHeadQuery = "SELECT COALESCE(MAX(seq),0) AS head FROM bd_events_journal"

const (
	followBackoffMin = time.Second
	followBackoffMax = 30 * time.Second
)

// followTruncatedRe extracts the retained-window floor from the
// events_journal_truncated failure that `bd events tail --json` prints.
var followTruncatedRe = regexp.MustCompile(`"floor"\s*:\s*(\d+)`)

// mailFollowKong implements `ateam mail follow <initiative-id>`: on a hosted
// workspace it follows the bd events journal and touches the initiative's
// doorbell for each new message addressed to it (docs/hosted-beads.md, F7-F9).
// It never writes a bead.
type mailFollowKong struct {
	ID        string `arg:"" name:"initiative-id" help:"Initiative id whose doorbell to ring."`
	ParentPID int    `name:"parent-pid" help:"Exit when this process is gone."`

	// Test seams; zero values use the production implementations.
	runBD       func(ctx context.Context, args []string, onLine func(string)) (stderr string, err error)
	headQuery   func(query string) (string, error)
	sleep       func(ctx context.Context, d time.Duration)
	now         func() time.Time
	parentAlive func(pid int) bool
	pollEvery   time.Duration
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
	run, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	return c.follow(run, ctx)
}

func (c *mailFollowKong) follow(run context.Context, ctx *cli.Context) error {
	run, cancel := context.WithCancel(run)
	defer cancel()

	if c.now == nil {
		c.now = time.Now
	}
	if c.sleep == nil {
		c.sleep = followSleep
	}
	if c.runBD == nil {
		c.runBD = func(rc context.Context, args []string, onLine func(string)) (string, error) {
			return followExecBD(rc, ctx.Home, args, onLine)
		}
	}
	if c.headQuery == nil {
		c.headQuery = func(q string) (string, error) { return ctx.BD.Run("sql", "--json", q) }
	}
	if c.parentAlive == nil {
		c.parentAlive = pidAlive
	}
	if c.pollEvery <= 0 {
		c.pollEvery = time.Second
	}
	if c.ParentPID > 0 {
		go c.watchParent(run, cancel)
	}

	start := c.now()
	seqPath := filepath.Join(ctx.Home, "mailbox", c.ID+".seq")
	doorbell := filepath.Join(ctx.Home, "mailbox", c.ID+".wake")

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
			if c.rings(row, ringNotBefore) {
				if err := touchFollowDoorbell(doorbell); err != nil {
					fmt.Fprintf(ctx.Stderr, "ateam mail follow: touch doorbell: %v\n", err)
				}
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
				// The pruned gap may hide mail: ring once, then resume.
				cp = floor - 1
				if err := touchFollowDoorbell(doorbell); err != nil {
					fmt.Fprintf(ctx.Stderr, "ateam mail follow: touch doorbell: %v\n", err)
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

// rings reports whether row r rings this initiative (F7), honoring the F8
// fallback floor when set.
func (c *mailFollowKong) rings(r followRow, notBefore time.Time) bool {
	if r.Op != "create" || r.Issue == nil || r.Issue.IssueType != "message" || r.Issue.Assignee != c.ID {
		return false
	}
	if !notBefore.IsZero() {
		ts, err := time.Parse(time.RFC3339, r.TS)
		if err != nil || ts.Before(notBefore) {
			return false
		}
	}
	return true
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

// watchParent cancels the run when the parent pid is gone.
func (c *mailFollowKong) watchParent(run context.Context, cancel context.CancelFunc) {
	t := time.NewTicker(c.pollEvery)
	defer t.Stop()
	for {
		if !c.parentAlive(c.ParentPID) {
			cancel()
			return
		}
		select {
		case <-run.Done():
			return
		case <-t.C:
		}
	}
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
