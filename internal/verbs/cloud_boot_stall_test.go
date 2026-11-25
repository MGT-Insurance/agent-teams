package verbs

import (
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
