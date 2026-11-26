package verbs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCloudBootStallPark drives pollLoop with a lead that reports "busy" on
// every poll (the orphan-background-process shape) and a faked activity seam.
func TestCloudBootStallPark(t *testing.T) {
	const minute = time.Minute
	cases := map[string]struct {
		act      func(now time.Time) cloudActivity
		closeAt  int // close the initiative at this bd show call
		wantPark bool
	}{
		"busy, silent 91m, no output files": {
			act:      func(now time.Time) cloudActivity { return cloudActivity{} },
			closeAt:  200,
			wantPark: true,
		},
		"busy, silent 91m, newest output 91m old": {
			act: func(now time.Time) cloudActivity {
				return cloudActivity{TaskOutput: now.Add(-91 * minute)}
			},
			closeAt:  200,
			wantPark: true,
		},
		"busy, record 30m ago": {
			act: func(now time.Time) cloudActivity {
				return cloudActivity{Transcript: now.Add(-30 * minute)}
			},
			closeAt: 200,
		},
		"busy, transcript silent 2h, output written 2m ago": {
			act: func(now time.Time) cloudActivity {
				return cloudActivity{Transcript: now.Add(-2 * time.Hour), TaskOutput: now.Add(-2 * minute)}
			},
			closeAt: 200,
		},
		"busy, silent, loop started 30m ago": {
			act: func(now time.Time) cloudActivity {
				return cloudActivity{Transcript: now.Add(-5 * time.Hour)}
			},
			closeAt: 31,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newCBEnv(t)
			e.common.Poll = minute
			e.common.Stall = 90 * minute
			e.common.Activity = func(string, string) cloudActivity { return tc.act(e.common.Now()) }
			e.closeAt = tc.closeAt
			e.leadSeq("sid-1", "busy")
			err := e.start()
			log := e.ctx.Stderr.(interface{ String() string }).String()
			if !tc.wantPark {
				if err != nil {
					t.Fatalf("err = %v, want nil (initiative closed)", err)
				}
				if strings.Contains(log, "reason=stall-park") {
					t.Fatalf("parked unexpectedly:\n%s", log)
				}
				return
			}
			wantExit(t, err, cloudBootParkExit)
			if !strings.Contains(log, "reason=stall-park") || !strings.Contains(log, "lead=busy silent=") {
				t.Errorf("missing stall-park exit line:\n%s", log)
			}
			if strings.Contains(log, "reason=idle-park") {
				t.Errorf("stall park logged as idle park:\n%s", log)
			}
			if e.calls("kill-session") != 1 {
				t.Errorf("kill-session calls = %d, want 1", e.calls("kill-session"))
			}
			if e.pollN < 91 || e.pollN > 92 {
				t.Errorf("polls = %d, want park just past 90m at 1m poll", e.pollN)
			}
		})
	}
}

func TestCloudBootStallEnvFlag(t *testing.T) {
	var c cloudBootCommon
	c.deps()
	if c.Stall != cloudBootDefaultStall {
		t.Errorf("zero Stall defaults to %s, got %s", cloudBootDefaultStall, c.Stall)
	}
}

func TestNewestTranscriptRecordCountsContentTurnsOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	lines := []string{
		`{"type":"assistant","timestamp":"2026-11-25T10:00:00.000Z","message":{"content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"user","timestamp":"2026-11-25T11:00:00.000Z","message":{"content":"go"}}`,
		`{"type":"system","timestamp":"2026-11-25T12:00:00.000Z"}`,
		`{"type":"assistant","timestamp":"2026-11-25T13:00:00.000Z","message":{"content":[]}}`,
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 25, 11, 0, 0, 0, time.UTC)
	if got := newestTranscriptRecord(p); !got.Equal(want) {
		t.Errorf("newest = %s, want %s", got, want)
	}
	if got := newestTranscriptRecord(p + ".missing"); !got.IsZero() {
		t.Errorf("missing file = %s, want zero", got)
	}
}

func TestNewestTranscriptRecordOversizedLastRecordFallsBackToMtime(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	small := `{"type":"assistant","timestamp":"2026-11-25T10:00:00.000Z","message":{"content":[{"type":"text","text":"hi"}]}}`
	big := `{"type":"assistant","timestamp":"2026-11-25T11:00:00.000Z","message":{"content":[{"type":"text","text":"` +
		strings.Repeat("x", cloudBootTranscriptTailBytes+1024) + `"}]}}`
	if err := os.WriteFile(p, []byte(small+"\n"+big+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := time.Date(2026, 11, 25, 12, 30, 0, 0, time.UTC)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	if got := newestTranscriptRecord(p); !got.Equal(mt) {
		t.Errorf("newest = %s, want mtime %s (zero would regress the stall clock)", got, mt)
	}
}

func TestStallClockRefreshNeverMovesBackward(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	c := newStallClock(start, 90*time.Minute)
	t1 := start.Add(10 * time.Minute)
	t2 := start.Add(20 * time.Minute)
	c.refresh(start.Add(6*time.Minute), func() cloudActivity { return cloudActivity{Transcript: t2, TaskOutput: t1} })
	c.refresh(start.Add(12*time.Minute), func() cloudActivity { return cloudActivity{} })
	c.refresh(start.Add(18*time.Minute), func() cloudActivity { return cloudActivity{Transcript: t1, TaskOutput: t1.Add(time.Minute)} })
	if !c.act.Transcript.Equal(t2) {
		t.Errorf("Transcript = %s, want %s", c.act.Transcript, t2)
	}
	if !c.act.TaskOutput.Equal(t1.Add(time.Minute)) {
		t.Errorf("TaskOutput = %s, want %s", c.act.TaskOutput, t1.Add(time.Minute))
	}
}

// TestCloudBootStallParkWithoutAgentListing covers a lead missing from
// `claude agents` (tmux alive) and a failing `claude agents` call: both paths
// still stall-park from file activity.
func TestCloudBootStallParkWithoutAgentListing(t *testing.T) {
	for name, agents := range map[string]func() ([]agentSession, error){
		"lead missing": func() ([]agentSession, error) { return nil, nil },
		// Alternates with a successful empty listing so the six-error budget
		// does not end the run first; the park lands on the 91st (odd, failing) poll.
		"agents erroring": func() func() ([]agentSession, error) {
			n := 0
			return func() ([]agentSession, error) {
				n++
				if n%2 == 1 {
					return nil, errors.New("boom")
				}
				return nil, nil
			}
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			e := newCBEnv(t)
			e.common.Poll = time.Minute
			e.common.Stall = 90 * time.Minute
			e.common.Activity = func(string, string) cloudActivity { return cloudActivity{} }
			e.common.Agents = agents
			e.closeAt = 500
			wantExit(t, e.start(), cloudBootParkExit)
			if log := e.stderr(); !strings.Contains(log, "reason=stall-park") {
				t.Errorf("missing stall-park exit line:\n%s", log)
			} else if want := map[string]string{"lead missing": "lead=missing", "agents erroring": "lead=unknown"}[name]; !strings.Contains(log, want+" silent=") {
				t.Errorf("exit line lacks %q:\n%s", want, log)
			}
		})
	}
}
