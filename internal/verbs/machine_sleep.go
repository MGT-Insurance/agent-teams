// Package verbs — machine_sleep.go implements sleptBetween, the single seam
// every hung-detection clock consumes to discount real machine sleep from
// elapsed wall-clock time (agent-teams-bq9y.1).
//
// This machine spends long stretches in macOS maintenance sleep. Every hung
// clock (STUCK/DEAD threshold, work-product flatline — agent-teams-bq9y.2)
// previously measured plain wall-clock elapsed time, so a maintenance-sleep
// window looked identical to "the session stopped responding" and killed
// live sessions. sleptBetween sums the real Sleep→Wake/DarkWake intervals
// `pmset -g log` retains that fall inside [start, end], so a caller can
// subtract it from elapsed and get a true awake-time measurement.
//
// Fail-safe: any error reading the log is logged to stderr and treated as
// zero sleep — today's undiscounted behavior. A genuine stall still trips;
// only a real, parsed machine-sleep interval ever reduces the count.
package verbs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// sleepInterval is one closed asleep span: the machine was unavailable for
// the half-open range [start, end).
type sleepInterval struct {
	start time.Time
	end   time.Time
}

// machineSleepLog is the injectable source seam: it returns raw pmset log
// text, exactly as `pmset -g log` prints it. Tests replace this var with
// canned text (or an error) without shelling out; production leaves it
// wired to runPmsetLog.
var machineSleepLog = runPmsetLog

// sleptBetween returns the total wall-clock duration the machine was asleep
// within [start, end), clipping any interval that only partially overlaps
// the window. Any error from the underlying source (pmset missing, exec
// failure) is logged and treated as zero sleep — the fail-safe default.
func sleptBetween(start, end time.Time) time.Duration {
	if !end.After(start) {
		return 0
	}
	text, err := machineSleepLog()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ateam: sleptBetween: pmset -g log: %v\n", err)
		return 0
	}

	var total time.Duration
	for _, iv := range parsePmsetSleepLog(text) {
		lo, hi := iv.start, iv.end
		if lo.Before(start) {
			lo = start
		}
		if hi.After(end) {
			hi = end
		}
		if hi.After(lo) {
			total += hi.Sub(lo)
		}
	}
	return total
}

// runPmsetLog shells out to `pmset -g log` and returns its stdout.
func runPmsetLog() (string, error) {
	out, err := exec.Command("pmset", "-g", "log").Output()
	if err != nil {
		return "", fmt.Errorf("pmset -g log: %w", err)
	}
	return string(out), nil
}

// pmsetTimestampLayout is the "date time zone" prefix pmset -g log emits on
// every event line, e.g. "2026-08-23 22:29:56 -0500" — a numeric UTC offset,
// space-separated from the date and time.
const pmsetTimestampLayout = "2006-01-02 15:04:05 -0700"

// parsePmsetSleepLog parses pmset -g log text into closed asleep intervals.
// A "Sleep" line opens an interval; the next "Wake" or "DarkWake" line
// closes it. pmset emits many other event kinds on the same tab-delimited
// shape (e.g. "Wake Requests", "Assertions") which this ignores — matching
// on the exact kind field (not a prefix) matters: "Wake Requests" is not a
// "Wake" event and must not close an open interval as one. Unparseable or
// unrecognized lines are skipped rather than treated as errors; a log with
// no recognizable Sleep/Wake pairs simply yields zero intervals. A trailing
// unclosed Sleep (log ends mid-sleep) is dropped: we cannot know when it
// ended, and dropping it undercounts sleep rather than inventing an end
// time — the fail-safe direction.
func parsePmsetSleepLog(text string) []sleepInterval {
	var intervals []sleepInterval
	var sleepStart time.Time
	haveSleepStart := false

	for _, line := range strings.Split(text, "\n") {
		tab := strings.IndexByte(line, '\t')
		if tab < 0 {
			continue
		}
		head := strings.Fields(line[:tab])
		if len(head) < 4 {
			continue
		}
		ts, err := time.Parse(pmsetTimestampLayout, strings.Join(head[:3], " "))
		if err != nil {
			continue
		}
		switch strings.Join(head[3:], " ") {
		case "Sleep":
			// Keep the EARLIEST open sleep start. Back-to-back "Sleep" lines
			// with no intervening Wake/DarkWake occur in real pmset logs; a
			// later one must not overwrite the first, which would discard the
			// gap between them and undercount sleep (biasing toward a false
			// "still hung"). A Sleep while one is already open is redundant.
			if !haveSleepStart {
				sleepStart = ts
				haveSleepStart = true
			}
		case "Wake", "DarkWake":
			if haveSleepStart {
				if ts.After(sleepStart) {
					intervals = append(intervals, sleepInterval{start: sleepStart, end: ts})
				}
				haveSleepStart = false
			}
		}
	}
	return intervals
}
