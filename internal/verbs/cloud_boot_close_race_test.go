package verbs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/transport"
)

// raceBD is one bd shared by the lead's `ateam close` and cloud-boot's poll.
type raceBD struct {
	mu       sync.Mutex
	closed   bool
	closedCh chan struct{}
}

func (b *raceBD) Run(args ...string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch args[0] {
	case "close":
		if !b.closed {
			b.closed = true
			close(b.closedCh)
		}
		return "ok", nil
	case "show":
		st := "open"
		if b.closed {
			st = "closed"
		}
		raw, _ := json.Marshal([]bd.Issue{{ID: "at-ab12", Title: "t", Status: st, Labels: []string{"thread:42"}}})
		return string(raw), nil
	}
	return "", fmt.Errorf("raceBD: unexpected %v", args)
}

func (b *raceBD) RunJSON(dst any, args ...string) error {
	return fmt.Errorf("raceBD: unexpected RunJSON")
}

// raceTransport blocks Send until released, so the poll observes "closed"
// while ateam close sits between bd close and CloseTopic. It records an
// ordered event log shared with the kill-session fake.
type raceTransport struct {
	log     *raceLog
	release chan struct{}
}

type raceLog struct {
	mu     sync.Mutex
	events []string
}

func (l *raceLog) add(e string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *raceLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

func (r *raceTransport) Name() string { return "race" }
func (r *raceTransport) Send(transport.OutboundMessage) (string, error) {
	<-r.release
	r.log.add("send")
	return "42", nil
}
func (r *raceTransport) Receive(func(transport.Reply) error) error { return nil }
func (r *raceTransport) CloseTopic(string) error {
	r.log.add("close_topic")
	return nil
}

// TestCloudBootClosedWaitsForInFlightCloseBeforeKill forces the observed
// order: bd close lands, cloud-boot's next poll sees "closed" while the
// farewell Send is still in flight. kill-session stands for the SIGTERM that
// ends the lead's Bash tool command, so any event after "kill" is lost. The
// topic must be closed before the kill.
func TestCloudBootClosedWaitsForInFlightCloseBeforeKill(t *testing.T) {
	log := &raceLog{}
	release := make(chan struct{})
	var releaseOnce sync.Once
	rel := func() { releaseOnce.Do(func() { close(release) }) }
	b := &raceBD{closedCh: make(chan struct{})}
	tr := &raceTransport{log: log, release: release}

	done := make(chan struct{})
	closeCtx := &cli.Context{Home: t.TempDir(), BD: b, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	go func() {
		defer close(done)
		cmd := &closeKong{
			ID:                 "at-ab12",
			runUpdateLocalMain: func(string) (string, error) { return "", fmt.Errorf("no repo") },
			transportFor:       func(string) (transport.Transport, error) { return tr, nil },
		}
		_ = cmd.Run(closeCtx)
	}()
	running := func() bool {
		select {
		case <-done:
			return false
		default:
			return true
		}
	}

	firstSleep := true
	c := &cloudBootCommon{
		Poll: 15 * time.Second, IdlePolls: 3, Grace: 60 * time.Second,
		Now: time.Now,
		Tmux: func(args ...string) error {
			if args[0] == "kill-session" {
				log.add("kill")
				rel() // unblock the goroutine so the test ends; its later events count as lost
			}
			return nil
		},
		Sleep: func(time.Duration) {
			if firstSleep {
				firstSleep = false
				<-b.closedCh // the poll fires right after bd close
			}
		},
		Agents: func() ([]agentSession, error) {
			st := "idle"
			if running() {
				// The lead is busy while ateam close runs. Let the close
				// finish, as the real farewell Send eventually answers.
				st = "busy"
				rel()
				<-done
			}
			return []agentSession{lead("sid-1", st)}, nil
		},
	}
	pollCtx := &cli.Context{Home: t.TempDir(), BD: b, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if err := c.idleLoop(pollCtx, "at-ab12", "sid-1", t.TempDir()); err != nil {
		t.Fatalf("idleLoop err = %v, want nil", err)
	}
	<-done

	events := log.snapshot()
	kill, topic := -1, -1
	for i, e := range events {
		if e == "kill" && kill < 0 {
			kill = i
		}
		if e == "close_topic" && topic < 0 {
			topic = i
		}
	}
	if topic < 0 || kill < 0 || topic > kill {
		t.Fatalf("events = %v: want close_topic before kill (a killed ateam close never closes the topic)", events)
	}
}

// closedLoopEnv returns a cloud-boot env whose initiative closes on the first
// loop poll, with a fake clock. The clock advances by each drain Sleep and by
// agentCost per Agents call once the initiative is closed, so clk.used is the
// wall time the drain used.
func closedLoopEnv(t *testing.T, agentCost time.Duration) (*cbEnv, *fakeClock) {
	t.Helper()
	e := newCBEnv(t)
	e.closeAt = 2 // show 1 is the precondition check; 2 is the first loop poll
	clk := &fakeClock{base: time.Unix(1_700_000_000, 0)}
	e.common.Now = clk.Now
	e.common.Sleep = func(d time.Duration) {
		if d == cloudBootCloseDrainPoll {
			clk.advance(d)
		}
	}
	inner := e.common.Agents
	e.common.Agents = func() ([]agentSession, error) {
		if e.showN >= e.closeAt {
			clk.advance(agentCost)
		}
		return inner()
	}
	return e, clk
}

type fakeClock struct {
	base time.Time
	used time.Duration
}

func (c *fakeClock) Now() time.Time          { return c.base.Add(c.used) }
func (c *fakeClock) advance(d time.Duration) { c.used += d }

// TestCloudBootClosedDrainIsBounded proves the bound holds in wall time: a
// lead that stays busy, with and without Agents calls that fail slowly, ends
// the drain within one poll interval plus one Agents call of the cap.
func TestCloudBootClosedDrainIsBounded(t *testing.T) {
	cases := map[string]struct {
		agentCost time.Duration
		agentsErr bool
	}{
		"busy forever":           {},
		"agents errors, 5s each": {agentCost: 5 * time.Second, agentsErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e, clk := closedLoopEnv(t, tc.agentCost)
			e.leadSeq("sid-1", "busy") // busy forever: the last poll repeats
			if tc.agentsErr {
				e.common.Agents = func() ([]agentSession, error) {
					if e.showN >= e.closeAt {
						clk.advance(tc.agentCost)
						return nil, fmt.Errorf("claude agents: timed out")
					}
					return []agentSession{lead("sid-1", "busy")}, nil
				}
			}
			if err := e.start(); err != nil {
				t.Fatalf("err = %v, want nil (exit 0)", err)
			}
			if n := e.calls("kill-session"); n != 1 {
				t.Errorf("kill-session calls = %d, want 1", n)
			}
			maxWall := cloudBootCloseDrain + cloudBootCloseDrainPoll + tc.agentCost
			if clk.used < cloudBootCloseDrain || clk.used > maxWall {
				t.Errorf("drain wall time %v, want >= %v and <= %v", clk.used, cloudBootCloseDrain, maxWall)
			}
			stderr := e.ctx.Stderr.(*bytes.Buffer).String()
			if !strings.Contains(stderr, "lead still busy after") {
				t.Errorf("stderr = %q, want the drain timeout named", stderr)
			}
			if !strings.Contains(stderr, "initiative at-ab12 closed") {
				t.Errorf("stderr = %q, want the closed line", stderr)
			}
		})
	}
}

func TestCloudBootClosedLeadGoneOrIdleKillsAtOnce(t *testing.T) {
	cases := map[string]func(e *cbEnv){
		"lead gone": func(e *cbEnv) {
			e.polls = append(e.polls, func() []agentSession { return nil })
		},
		"lead idle": func(e *cbEnv) { e.leadSeq("sid-1", "idle") },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			e, clk := closedLoopEnv(t, 0)
			setup(e)
			if err := e.start(); err != nil {
				t.Fatalf("err = %v, want nil (exit 0)", err)
			}
			if n := e.calls("kill-session"); n != 1 {
				t.Errorf("kill-session calls = %d, want 1", n)
			}
			if clk.used != 0 {
				t.Errorf("drain wall time %v, want no drain sleep", clk.used)
			}
		})
	}
}
