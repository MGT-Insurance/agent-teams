package verbs

import (
	"errors"
	"testing"
	"time"
)

// canned pmset -g log excerpts, shaped exactly like real samples from this
// machine (agent-teams-bq9y.1 bead notes): tab-delimited "date time zone
// KIND<TAB>message" lines. Timestamps carry a numeric zone offset.
const pmsetTwoSleeps = "" +
	"2026-08-23 22:29:56 -0500 Sleep               \tEntering Sleep state due to 'Maintenance Sleep':TCPKeepAlive=active\n" +
	"2026-08-23 22:45:50 -0500 DarkWake            \tDarkWake from Deep Idle [CDNPB]\n" +
	"2026-08-23 22:46:35 -0500 Sleep               \tEntering Sleep state due to 'Maintenance Sleep'\n" +
	"2026-08-23 23:02:10 -0500 Wake                \tDarkWake to FullWake from Deep Idle [CDNVAP] : due to Notification\n"

// includes a non-transition "Wake Requests" line between a Sleep and its
// closing DarkWake — must not be mistaken for the closing Wake event.
const pmsetWithWakeRequests = "" +
	"2026-08-23 10:00:00 -0500 Sleep               \tEntering Sleep state due to 'Maintenance Sleep'\n" +
	"2026-08-23 10:00:03 -0500 Wake Requests       \t[*process=dasd request=SleepService deltaSecs=949]\n" +
	"2026-08-23 10:15:00 -0500 DarkWake            \tDarkWake from Deep Idle [CDNP]\n"

func mustParse(t *testing.T, layout, value string) time.Time {
	t.Helper()
	tm, err := time.Parse(layout, value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return tm
}

func TestSleptBetween(t *testing.T) {
	const layout = "2006-01-02 15:04:05 -0700"

	tests := []struct {
		name        string
		log         string
		start, end  string
		wantSeconds float64
	}{
		{
			name:        "multiple sleeps fully inside window",
			log:         pmsetTwoSleeps,
			start:       "2026-08-23 22:00:00 -0500",
			end:         "2026-08-23 23:30:00 -0500",
			wantSeconds: (15*60 + 54) + (15*60 + 35), // 22:29:56-22:45:50 + 22:46:35-23:02:10
		},
		{
			name:        "window straddles the left edge of a sleep interval",
			log:         pmsetTwoSleeps,
			start:       "2026-08-23 22:40:00 -0500",
			end:         "2026-08-23 22:46:00 -0500",
			wantSeconds: 5*60 + 50, // clipped: 22:40:00-22:45:50 only; window ends before the next sleep starts (22:46:35)
		},
		{
			name:        "window straddles the right edge of a sleep interval",
			log:         pmsetTwoSleeps,
			start:       "2026-08-23 22:50:00 -0500",
			end:         "2026-08-23 22:55:00 -0500",
			wantSeconds: 5 * 60, // clipped: 22:50:00-22:55:00, all inside the second sleep
		},
		{
			name:        "fully awake window before any sleep",
			log:         pmsetTwoSleeps,
			start:       "2026-08-23 20:00:00 -0500",
			end:         "2026-08-23 21:00:00 -0500",
			wantSeconds: 0,
		},
		{
			name:        "empty pmset text",
			log:         "",
			start:       "2026-08-23 20:00:00 -0500",
			end:         "2026-08-23 21:00:00 -0500",
			wantSeconds: 0,
		},
		{
			name:        "garbled pmset text",
			log:         "not a pmset log\nrandom garbage\n\t\tmore garbage\n",
			start:       "2026-08-23 20:00:00 -0500",
			end:         "2026-08-23 21:00:00 -0500",
			wantSeconds: 0,
		},
		{
			name:        "Wake Requests line does not close the interval early",
			log:         pmsetWithWakeRequests,
			start:       "2026-08-23 09:00:00 -0500",
			end:         "2026-08-23 11:00:00 -0500",
			wantSeconds: 15 * 60, // 10:00:00-10:15:00, closed by DarkWake not Wake Requests
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			restore := setMachineSleepLog(t, tc.log)
			defer restore()

			start := mustParse(t, layout, tc.start)
			end := mustParse(t, layout, tc.end)

			got := sleptBetween(start, end)
			want := time.Duration(tc.wantSeconds) * time.Second
			if got != want {
				t.Errorf("sleptBetween(%s, %s) = %s, want %s", tc.start, tc.end, got, want)
			}
		})
	}
}

// TestSleptBetween_SourceError proves the fail-safe path: any error from the
// injected source seam yields zero, never a propagated error or panic.
func TestSleptBetween_SourceError(t *testing.T) {
	prev := machineSleepLog
	machineSleepLog = func() (string, error) { return "", errors.New("pmset: command not found") }
	defer func() { machineSleepLog = prev }()

	start := time.Now().Add(-time.Hour)
	end := time.Now()
	if got := sleptBetween(start, end); got != 0 {
		t.Errorf("sleptBetween with source error = %s, want 0", got)
	}
}

// TestSleptBetween_DegenerateWindow proves end<=start returns 0 without
// touching the source seam at all.
func TestSleptBetween_DegenerateWindow(t *testing.T) {
	prev := machineSleepLog
	called := false
	machineSleepLog = func() (string, error) {
		called = true
		return "", nil
	}
	defer func() { machineSleepLog = prev }()

	now := time.Now()
	if got := sleptBetween(now, now); got != 0 {
		t.Errorf("sleptBetween(now, now) = %s, want 0", got)
	}
	if got := sleptBetween(now, now.Add(-time.Minute)); got != 0 {
		t.Errorf("sleptBetween(now, earlier) = %s, want 0", got)
	}
	if called {
		t.Error("sleptBetween should short-circuit on a degenerate window without calling the source seam")
	}
}

// setMachineSleepLog swaps the package-var source seam for the duration of a
// subtest and returns a restore func.
func setMachineSleepLog(t *testing.T, text string) func() {
	t.Helper()
	prev := machineSleepLog
	machineSleepLog = func() (string, error) { return text, nil }
	return func() { machineSleepLog = prev }
}
