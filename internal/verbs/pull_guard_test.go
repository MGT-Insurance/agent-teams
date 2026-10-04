package verbs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

// fakePullExec builds a bd.ContextExecFunc that dispatches on the bd
// subcommand: "sql" calls are answered by sqlFn, "dolt pull" calls by
// pullFn. Either fn may be nil to fail the test if that call is unexpected.
func fakePullExec(t *testing.T, sqlFn func(ctx context.Context) (string, error), pullFn func(ctx context.Context) (string, error)) (bd.ContextExecFunc, *int, *int) {
	t.Helper()
	sqlCalls, pullCalls := 0, 0
	fn := func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		if name != "bd" {
			t.Fatalf("exec called with %q, want bd", name)
		}
		// RunContext prepends "-C", <home> to every call, so the actual
		// bd subcommand starts at args[2].
		sub := args[2:]
		switch {
		case len(sub) >= 1 && sub[0] == "sql":
			sqlCalls++
			if sqlFn == nil {
				t.Fatalf("unexpected sql call: %v", args)
			}
			out, err := sqlFn(ctx)
			if err != nil {
				return nil, nil, err
			}
			return []byte(out), nil, nil
		case len(sub) >= 2 && sub[0] == "dolt" && sub[1] == "pull":
			pullCalls++
			if pullFn == nil {
				t.Fatalf("unexpected dolt pull call: %v", args)
			}
			out, err := pullFn(ctx)
			if err != nil {
				return nil, nil, err
			}
			return []byte(out), nil, nil
		default:
			t.Fatalf("unexpected exec call: %v", args)
			return nil, nil, nil
		}
	}
	return fn, &sqlCalls, &pullCalls
}

func rowsJSON(t *testing.T, rows []processlistRow) string {
	t.Helper()
	b, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal rows: %v", err)
	}
	return string(b)
}

// withTerminateHungPull swaps the package var for the duration of the test
// and restores the original on cleanup, so tests never leak a fake remedy
// into a sibling test.
func withTerminateHungPull(t *testing.T, fn func(ctx context.Context, p inFlightPull) error) {
	t.Helper()
	orig := terminateHungPull
	terminateHungPull = fn
	t.Cleanup(func() { terminateHungPull = orig })
}

func TestGuardedPull_NoInFlight_RunsPull(t *testing.T) {
	execFn, sqlCalls, pullCalls := fakePullExec(t,
		func(context.Context) (string, error) { return rowsJSON(t, nil), nil },
		func(context.Context) (string, error) { return "up to date", nil },
	)
	c := bd.NewClientWithContextExec("/ws", execFn)
	var stderr bytes.Buffer

	out, err := guardedPull(context.Background(), c, pullModeBestEffort, pullTimeoutDefault, &stderr)
	if err != nil {
		t.Fatalf("guardedPull: %v", err)
	}
	if out != "up to date" {
		t.Errorf("stdout = %q, want %q", out, "up to date")
	}
	if *sqlCalls != 1 || *pullCalls != 1 {
		t.Errorf("sqlCalls=%d pullCalls=%d, want 1,1", *sqlCalls, *pullCalls)
	}
}

func TestGuardedPull_YoungInFlight_BestEffort_Skips(t *testing.T) {
	execFn, _, pullCalls := fakePullExec(t,
		func(context.Context) (string, error) {
			return rowsJSON(t, []processlistRow{{ID: 42, Time: 5}}), nil
		},
		nil, // dolt pull must not be called
	)
	c := bd.NewClientWithContextExec("/ws", execFn)
	var stderr bytes.Buffer

	out, err := guardedPull(context.Background(), c, pullModeBestEffort, pullTimeoutDefault, &stderr)
	if err != nil {
		t.Fatalf("guardedPull: got error %v, want nil", err)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if *pullCalls != 0 {
		t.Errorf("pullCalls = %d, want 0", *pullCalls)
	}
	if !strings.HasPrefix(stderr.String(), "ateam pull: skipped:") {
		t.Errorf("stderr = %q, want prefix %q", stderr.String(), "ateam pull: skipped:")
	}
}

func TestGuardedPull_YoungInFlight_Required_Errors(t *testing.T) {
	execFn, _, pullCalls := fakePullExec(t,
		func(context.Context) (string, error) {
			return rowsJSON(t, []processlistRow{{ID: 7, Time: 3}}), nil
		},
		nil, // dolt pull must not be called
	)
	c := bd.NewClientWithContextExec("/ws", execFn)
	var stderr bytes.Buffer

	_, err := guardedPull(context.Background(), c, pullModeRequired, pullTimeoutDefault, &stderr)
	if err == nil {
		t.Fatal("guardedPull: got nil error, want in-flight error")
	}
	if !strings.HasPrefix(err.Error(), "ateam sync: dolt pull already in flight") {
		t.Errorf("err = %q, want prefix %q", err.Error(), "ateam sync: dolt pull already in flight")
	}
	if !errors.Is(err, errPullInFlight) {
		t.Errorf("errors.Is(err, errPullInFlight) = false, want true")
	}
	if *pullCalls != 0 {
		t.Errorf("pullCalls = %d, want 0", *pullCalls)
	}
}

func TestGuardedPull_SeveralRows_UsesLargestTime(t *testing.T) {
	execFn, _, pullCalls := fakePullExec(t,
		func(context.Context) (string, error) {
			return rowsJSON(t, []processlistRow{
				{ID: 1, Time: 2},
				{ID: 2, Time: 9}, // largest — this is the one actually running
				{ID: 3, Time: 4},
			}), nil
		},
		nil,
	)
	c := bd.NewClientWithContextExec("/ws", execFn)
	var stderr bytes.Buffer

	_, err := guardedPull(context.Background(), c, pullModeBestEffort, pullTimeoutDefault, &stderr)
	if err != nil {
		t.Fatalf("guardedPull: %v", err)
	}
	if *pullCalls != 0 {
		t.Fatalf("pullCalls = %d, want 0", *pullCalls)
	}
	want := "conn 2, 9s"
	if !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q (the largest-TIME row)", stderr.String(), want)
	}
}

func TestGuardedPull_StaleInFlight_RemedySucceeds_ThenPulls(t *testing.T) {
	execFn, _, pullCalls := fakePullExec(t,
		func(context.Context) (string, error) {
			return rowsJSON(t, []processlistRow{{ID: 99, Time: int64(pullStaleAfter.Seconds())}}), nil
		},
		func(context.Context) (string, error) { return "up to date", nil },
	)
	c := bd.NewClientWithContextExec("/ws", execFn)
	var stderr bytes.Buffer

	remedyCalls := 0
	var gotArg inFlightPull
	withTerminateHungPull(t, func(_ context.Context, p inFlightPull) error {
		remedyCalls++
		gotArg = p
		return nil
	})

	out, err := guardedPull(context.Background(), c, pullModeBestEffort, pullTimeoutDefault, &stderr)
	if err != nil {
		t.Fatalf("guardedPull: %v", err)
	}
	if out != "up to date" {
		t.Errorf("stdout = %q, want %q", out, "up to date")
	}
	if remedyCalls != 1 {
		t.Fatalf("remedyCalls = %d, want 1", remedyCalls)
	}
	if gotArg.ConnID != 99 {
		t.Errorf("remedy called with ConnID %d, want 99", gotArg.ConnID)
	}
	if *pullCalls != 1 {
		t.Errorf("pullCalls = %d, want 1", *pullCalls)
	}
	if !strings.HasPrefix(stderr.String(), "ateam pull: cleared hung dolt pull") {
		t.Errorf("stderr = %q, want prefix %q", stderr.String(), "ateam pull: cleared hung dolt pull")
	}
}

func TestGuardedPull_StaleInFlight_RemedyFails_FallsBackToYoungCase(t *testing.T) {
	execFn, _, pullCalls := fakePullExec(t,
		func(context.Context) (string, error) {
			return rowsJSON(t, []processlistRow{{ID: 11, Time: int64(pullStaleAfter.Seconds()) + 30}}), nil
		},
		nil, // dolt pull must not be called — remedy failure falls back to skip
	)
	c := bd.NewClientWithContextExec("/ws", execFn)
	var stderr bytes.Buffer

	withTerminateHungPull(t, func(context.Context, inFlightPull) error {
		return errors.New("kill: permission denied")
	})

	out, err := guardedPull(context.Background(), c, pullModeBestEffort, pullTimeoutDefault, &stderr)
	if err != nil {
		t.Fatalf("guardedPull: got error %v, want nil (best-effort young-case fallback)", err)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if *pullCalls != 0 {
		t.Errorf("pullCalls = %d, want 0", *pullCalls)
	}
	msg := stderr.String()
	if !strings.Contains(msg, "remedy failed") {
		t.Errorf("stderr = %q, want a remedy-failure hint", msg)
	}
	if !strings.Contains(msg, "ateam pull: skipped:") {
		t.Errorf("stderr = %q, want the young-case skip message too", msg)
	}
}

func TestGuardedPull_ProbeError_FailsOpen_PullStillRuns(t *testing.T) {
	execFn, _, pullCalls := fakePullExec(t,
		func(context.Context) (string, error) { return "", errors.New("bd: connection refused") },
		func(context.Context) (string, error) { return "up to date", nil },
	)
	c := bd.NewClientWithContextExec("/ws", execFn)
	var stderr bytes.Buffer

	out, err := guardedPull(context.Background(), c, pullModeBestEffort, pullTimeoutDefault, &stderr)
	if err != nil {
		t.Fatalf("guardedPull: %v", err)
	}
	if out != "up to date" {
		t.Errorf("stdout = %q, want %q", out, "up to date")
	}
	if *pullCalls != 1 {
		t.Errorf("pullCalls = %d, want 1 (fail-open)", *pullCalls)
	}
	if strings.Count(stderr.String(), "\n") != 1 {
		t.Errorf("stderr = %q, want exactly one line", stderr.String())
	}
}

func TestGuardedPull_ProbeUnparseableJSON_FailsOpen(t *testing.T) {
	execFn, _, pullCalls := fakePullExec(t,
		func(context.Context) (string, error) { return "not json", nil },
		func(context.Context) (string, error) { return "up to date", nil },
	)
	c := bd.NewClientWithContextExec("/ws", execFn)
	var stderr bytes.Buffer

	out, err := guardedPull(context.Background(), c, pullModeBestEffort, pullTimeoutDefault, &stderr)
	if err != nil {
		t.Fatalf("guardedPull: %v", err)
	}
	if out != "up to date" {
		t.Errorf("stdout = %q, want %q", out, "up to date")
	}
	if *pullCalls != 1 {
		t.Errorf("pullCalls = %d, want 1 (fail-open)", *pullCalls)
	}
}

func TestGuardedPull_PullTimesOut(t *testing.T) {
	execFn, _, _ := fakePullExec(t,
		func(context.Context) (string, error) { return rowsJSON(t, nil), nil },
		func(ctx context.Context) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	)
	c := bd.NewClientWithContextExec("/ws", execFn)
	var stderr bytes.Buffer

	_, err := guardedPull(context.Background(), c, pullModeBestEffort, 50*time.Millisecond, &stderr)
	if err == nil {
		t.Fatal("guardedPull: got nil error, want timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) = false, want true (err: %v)", err)
	}
	if !strings.HasPrefix(stderr.String(), "ateam pull: timed out after") {
		t.Errorf("stderr = %q, want prefix %q", stderr.String(), "ateam pull: timed out after")
	}
}

// TestInFlightPullQuery_DoesNotSelfMatch proves the probe's own SQL text
// never satisfies its own `UPPER(TRIM(INFO)) LIKE 'CALL DOLT_PULL%'`
// predicate — if it did, the probe would see itself running in
// processlist and misreport a false in-flight pull on every call.
func TestInFlightPullQuery_DoesNotSelfMatch(t *testing.T) {
	normalized := strings.ToUpper(strings.TrimSpace(inFlightPullQuery))
	if strings.HasPrefix(normalized, "CALL DOLT_PULL") {
		t.Fatalf("inFlightPullQuery self-matches its own LIKE predicate: %q", inFlightPullQuery)
	}
	if !strings.HasPrefix(normalized, "SELECT") {
		t.Fatalf("inFlightPullQuery = %q, want a SELECT (so it can never be its own match)", inFlightPullQuery)
	}
}

// TestProbeInFlightPull_ParsesLiveShape guards against the live JSON shape
// (verified 2026-09-24 against ~/.agent-teams: upper-case ID/TIME keys)
// silently drifting out from under processlistRow.
func TestProbeInFlightPull_ParsesLiveShape(t *testing.T) {
	live := `[{"ID": 210963, "TIME": 7}]`
	execFn, _, _ := fakePullExec(t,
		func(context.Context) (string, error) { return live, nil },
		nil,
	)
	c := bd.NewClientWithContextExec("/ws", execFn)

	got, err := probeInFlightPull(context.Background(), c)
	if err != nil {
		t.Fatalf("probeInFlightPull: %v", err)
	}
	if got == nil {
		t.Fatal("probeInFlightPull: got nil, want a row")
	}
	if got.ConnID != 210963 || got.Seconds != 7 {
		t.Errorf("got %+v, want {ConnID:210963 Seconds:7}", got)
	}
}
