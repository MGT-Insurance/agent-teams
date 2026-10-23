package verbs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

type followCall struct {
	args []string
}

// followHarness drives mailFollowKong.follow with scripted bd runs. Each
// script step feeds lines and returns stderr; after the last step the context
// is cancelled so follow returns.
type followHarness struct {
	t       *testing.T
	home    string
	stderr  bytes.Buffer
	calls   []followCall
	sleeps  []time.Duration
	cancel  context.CancelFunc
	steps   []followStep
	headOut string
	headErr error
	headQ   []string
}

type followStep struct {
	lines  []string
	stderr string
}

func newFollowHarness(t *testing.T, hosted bool) *followHarness {
	t.Helper()
	home := t.TempDir()
	if hosted {
		if err := os.MkdirAll(filepath.Join(home, ".beads"), 0o755); err != nil {
			t.Fatal(err)
		}
		meta := `{"dolt_mode":"server","dolt_server_host":"db.example.com"}`
		if err := os.WriteFile(filepath.Join(home, ".beads", "metadata.json"), []byte(meta), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &followHarness{t: t, home: home, headOut: `[{"head":10}]`}
}

func (h *followHarness) writeSeq(n string) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Join(h.home, "mailbox"), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.home, "mailbox", "at-xyz.seq"), []byte(n), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *followHarness) seq() string {
	b, err := os.ReadFile(filepath.Join(h.home, "mailbox", "at-xyz.seq"))
	if err != nil {
		return ""
	}
	return string(b)
}

func (h *followHarness) rung() bool {
	_, err := os.Stat(filepath.Join(h.home, "mailbox", "at-xyz.wake"))
	return err == nil
}

func (h *followHarness) run(c *mailFollowKong) error {
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	defer cancel()
	c.ID = "at-xyz"
	c.runBD = func(_ context.Context, args []string, onLine func(string)) (string, error) {
		h.calls = append(h.calls, followCall{args: args})
		if len(h.steps) == 0 {
			cancel()
			return "", nil
		}
		s := h.steps[0]
		h.steps = h.steps[1:]
		for _, l := range s.lines {
			onLine(l)
		}
		if len(h.steps) == 0 {
			cancel()
		}
		return s.stderr, os.ErrClosed
	}
	c.headQuery = func(q string) (string, error) {
		h.headQ = append(h.headQ, q)
		return h.headOut, h.headErr
	}
	c.sleep = func(_ context.Context, d time.Duration) { h.sleeps = append(h.sleeps, d) }
	if c.now == nil {
		c.now = func() time.Time { return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) }
	}
	return c.follow(ctx, &cli.Context{Home: h.home, Stderr: &h.stderr, Stdout: &bytes.Buffer{}})
}

func row(seq, ts, op, typ, assignee string) string {
	return `{"seq":` + seq + `,"ts":"` + ts + `","op":"` + op + `","issue":{"issue_type":"` + typ + `","assignee":"` + assignee + `"}}`
}

func TestMailFollowNotHostedExitsImmediatelyWithoutExec(t *testing.T) {
	h := newFollowHarness(t, false)
	execed := false
	c := &mailFollowKong{ID: "at-xyz", runBD: func(context.Context, []string, func(string)) (string, error) {
		execed = true
		return "", nil
	}}
	ctx := &cli.Context{Home: h.home, Stderr: &h.stderr, Stdout: &bytes.Buffer{}}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if execed || h.stderr.Len() != 0 {
		t.Fatalf("non-hosted must be inert: execed=%v stderr=%q", execed, h.stderr.String())
	}
}

func TestMailFollowRingsOnlyForMessageCreateForID(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		{"create message for id", row("11", "2026-10-23T13:00:00Z", "create", "message", "at-xyz"), true},
		{"other assignee", row("11", "2026-10-23T13:00:00Z", "create", "message", "at-other"), false},
		{"update op", row("11", "2026-10-23T13:00:00Z", "update", "message", "at-xyz"), false},
		{"comment op", row("11", "2026-10-23T13:00:00Z", "comment", "message", "at-xyz"), false},
		{"close op", row("11", "2026-10-23T13:00:00Z", "close", "message", "at-xyz"), false},
		{"task type", row("11", "2026-10-23T13:00:00Z", "create", "task", "at-xyz"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newFollowHarness(t, true)
			h.writeSeq("10")
			h.steps = []followStep{{lines: []string{tc.line}}}
			if err := h.run(&mailFollowKong{}); err != nil {
				t.Fatal(err)
			}
			if got := h.rung(); got != tc.want {
				t.Fatalf("rung=%v want %v", got, tc.want)
			}
			if h.seq() != "11" {
				t.Fatalf("checkpoint=%q want 11 (advances on every row)", h.seq())
			}
		})
	}
}

func TestMailFollowSkipsUnparseableLineAndKeepsGoing(t *testing.T) {
	h := newFollowHarness(t, true)
	h.writeSeq("10")
	h.steps = []followStep{{lines: []string{`{"seq": bogus`, row("11", "2026-10-23T13:00:00Z", "create", "message", "at-xyz")}}}
	if err := h.run(&mailFollowKong{}); err != nil {
		t.Fatal(err)
	}
	if !h.rung() || !strings.Contains(h.stderr.String(), "unparseable") {
		t.Fatalf("rung=%v stderr=%q", h.rung(), h.stderr.String())
	}
}

func TestMailFollowReconnectsFromCheckpointWithBackoff(t *testing.T) {
	h := newFollowHarness(t, true)
	h.writeSeq("10")
	h.steps = []followStep{
		{lines: []string{row("11", "2026-10-23T13:00:00Z", "update", "task", "x")}},
		{}, // exits with no row: backoff doubles
		{},
	}
	if err := h.run(&mailFollowKong{}); err != nil {
		t.Fatal(err)
	}
	if len(h.calls) != 3 {
		t.Fatalf("calls=%d want 3", len(h.calls))
	}
	wantSince := []string{"10", "11", "11"}
	for i, c := range h.calls {
		if got := c.args[len(c.args)-1]; got != wantSince[i] {
			t.Errorf("call %d --since %s want %s (args %v)", i, got, wantSince[i], c.args)
		}
	}
	// The row in step 0 resets backoff to 1s, so step 1 sleeps 1s and step 2
	// (the last, which cancels the run) never sleeps. Step 0 sleeps 1s and
	// step 1 doubles to 2s.
	want := []time.Duration{time.Second, 2 * time.Second}
	if !reflect.DeepEqual(h.sleeps, want) {
		t.Fatalf("sleeps=%v want %v", h.sleeps, want)
	}
}

func TestMailFollowBackoffCapsAtThirtySeconds(t *testing.T) {
	h := newFollowHarness(t, true)
	h.writeSeq("1")
	for i := 0; i < 8; i++ {
		h.steps = append(h.steps, followStep{})
	}
	if err := h.run(&mailFollowKong{}); err != nil {
		t.Fatal(err)
	}
	last := h.sleeps[len(h.sleeps)-1]
	if last != 30*time.Second {
		t.Fatalf("last backoff %v want 30s (all %v)", last, h.sleeps)
	}
}

func TestMailFollowTruncatedResumesFromFloorMinusOneAndRingsOnce(t *testing.T) {
	h := newFollowHarness(t, true)
	h.writeSeq("2")
	trunc := "{\n  \"code\": \"events_journal_truncated\",\n  \"floor\": 50,\n  \"head\": 80,\n  \"since\": 2\n}"
	h.steps = []followStep{{stderr: trunc}, {}}
	if err := h.run(&mailFollowKong{}); err != nil {
		t.Fatal(err)
	}
	if !h.rung() {
		t.Fatal("truncation must ring the doorbell")
	}
	if got := h.calls[1].args[len(h.calls[1].args)-1]; got != "49" {
		t.Fatalf("resume --since %s want 49", got)
	}
}

func TestMailFollowMissingCheckpointUsesBaselineQuery(t *testing.T) {
	h := newFollowHarness(t, true)
	h.steps = []followStep{{}}
	if err := h.run(&mailFollowKong{}); err != nil {
		t.Fatal(err)
	}
	if len(h.headQ) != 1 || h.headQ[0] != followHeadQuery {
		t.Fatalf("headQ=%v", h.headQ)
	}
	if got := h.calls[0].args[len(h.calls[0].args)-1]; got != "10" {
		t.Fatalf("--since %s want baseline head 10", got)
	}
	if h.seq() != "10" {
		t.Fatalf("baseline checkpoint=%q want 10", h.seq())
	}
}

func TestMailFollowBaselineFailureReplaysFromZeroRingingOnlyNewRows(t *testing.T) {
	h := newFollowHarness(t, true)
	h.headErr = os.ErrNotExist
	h.steps = []followStep{{lines: []string{
		row("1", "2026-10-23T11:59:59Z", "create", "message", "at-xyz"), // before start (12:00:00)
		row("2", "2026-10-23T12:00:00Z", "create", "message", "at-xyz"), // at start
	}}}
	if err := h.run(&mailFollowKong{}); err != nil {
		t.Fatal(err)
	}
	if got := h.calls[0].args[len(h.calls[0].args)-1]; got != "0" {
		t.Fatalf("--since %s want 0", got)
	}
	if !h.rung() {
		t.Fatal("row at follower start must ring")
	}
	// Prove the pre-start row alone does not ring.
	h2 := newFollowHarness(t, true)
	h2.headErr = os.ErrNotExist
	h2.steps = []followStep{{lines: []string{row("1", "2026-10-23T11:59:59Z", "create", "message", "at-xyz")}}}
	if err := h2.run(&mailFollowKong{}); err != nil {
		t.Fatal(err)
	}
	if h2.rung() {
		t.Fatal("row older than follower start must not ring in fallback mode")
	}
}

func TestMailFollowParentGoneExitsAndCancelsChild(t *testing.T) {
	h := newFollowHarness(t, true)
	h.writeSeq("1")
	childCancelled := make(chan struct{})
	childStarted := make(chan struct{})
	c := &mailFollowKong{
		ID:        "at-xyz",
		ParentPID: 4242,
		pollEvery: 5 * time.Millisecond,
		parentAlive: func(int) bool {
			select {
			case <-childStarted:
				return false
			default:
				return true
			}
		},
	}
	c.runBD = func(ctx context.Context, _ []string, _ func(string)) (string, error) {
		close(childStarted)
		<-ctx.Done()
		close(childCancelled)
		return "", ctx.Err()
	}
	c.headQuery = func(string) (string, error) { return h.headOut, nil }
	done := make(chan error, 1)
	go func() {
		done <- c.follow(context.Background(), &cli.Context{Home: h.home, Stderr: &h.stderr, Stdout: &bytes.Buffer{}})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("follow: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("follow did not exit when parent was gone")
	}
	select {
	case <-childCancelled:
	default:
		t.Fatal("bd child context was not cancelled")
	}
}

func TestParseFollowHeadAcceptsArrayAndObject(t *testing.T) {
	for _, in := range []string{`[{"head":7}]`, `{"head":7}`, `[{"head":"7"}]`} {
		if n, err := parseFollowHead(in); err != nil || n != 7 {
			t.Errorf("parseFollowHead(%s) = %d, %v", in, n, err)
		}
	}
	if _, err := parseFollowHead(`[]`); err == nil {
		t.Error("empty rows must error")
	}
}
