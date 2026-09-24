// Package verbs — pull_guard.go declares the frozen pull-guard surface for
// agent-teams-qdeh (bd v1.1.0's shared dolt sql-server has no timeout on the
// git+ssh fetch a DOLT_PULL triggers; a stalled transport can hang that
// query for hours, and every later `ateam pull`/`ateam sync` call queues
// behind it — see the contract bead, agent-teams-qdeh.1, for the full
// incident writeup).
//
// This file is the CONTRACT the guard bead (qdeh.2), the stale-remedy bead
// (qdeh.3), and the wiring bead (qdeh.4) all build against. The constants,
// types, and function signatures below are frozen; only their bodies are
// implemented elsewhere/later. Do not change a signature or the decision
// table here without re-freezing the contract bead first.
package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

const (
	// pullTimeoutDefault bounds a single `bd dolt pull` exec issued by
	// `ateam pull`/`ateam sync`. Ruled by Eric 2026-09-24: 20s, overridable
	// per call via `ateam pull --timeout=<duration>` (qdeh.4).
	pullTimeoutDefault = 20 * time.Second

	// pullStaleAfter marks an in-flight DOLT_PULL as hung once it has been
	// running this long — past this age, skip-and-wait stops being a
	// reasonable default and the stale remedy (terminateHungPull) applies.
	// Ruled by Eric 2026-09-24: 120s.
	pullStaleAfter = 120 * time.Second

	// pullProbeTimeout bounds the information_schema.processlist probe
	// (probeInFlightPull) that checks for an in-flight DOLT_PULL before
	// issuing a new one.
	pullProbeTimeout = 5 * time.Second
)

// pullMode selects how guardedPull behaves when it finds a young (not yet
// stale) in-flight DOLT_PULL.
type pullMode int

const (
	// pullModeBestEffort skips the pull and returns success (nil error) —
	// used by `ateam pull` and the relay tick, where degrading to local
	// state is always an acceptable outcome.
	pullModeBestEffort pullMode = iota

	// pullModeRequired fails the call instead of skipping — used by
	// `ateam sync`, which must not silently proceed on stale local state.
	pullModeRequired
)

// errPullInFlight is the sentinel guardedPull returns (wrapped with the
// connection id and age) when pullModeRequired finds a young in-flight pull.
// Callers/tests match it with errors.Is.
var errPullInFlight = errors.New("dolt pull already in flight")

// inFlightPull describes one DOLT_PULL row found by probeInFlightPull.
type inFlightPull struct {
	// ConnID is information_schema.processlist's connection ID (its "ID"
	// column) for the running DOLT_PULL.
	ConnID int64
	// Seconds is the row's "TIME" column: how long the query has been
	// running.
	Seconds int64
}

// probeInFlightPull checks information_schema.processlist for a running
// DOLT_PULL and returns the one with the LARGEST TIME (the query actually
// executing; any others are queued behind it), or nil if none is in flight.
//
// Frozen implementation contract (qdeh.2):
//   - runs `bd sql --json "SELECT ID, TIME FROM information_schema.processlist
//     WHERE UPPER(TRIM(INFO)) LIKE 'CALL DOLT_PULL%'"` via c.RunContext,
//     bounded by pullProbeTimeout.
//   - the LIKE predicate MUST NOT self-match: the probe's own INFO starts
//     with SELECT, not CALL DOLT_PULL, so it never matches its own query.
//   - a probe error (exec failure or unparseable JSON) is reported to the
//     caller as a non-nil error; guardedPull treats that as fail-open (see
//     its decision table below), never as "no pull in flight".

// inFlightPullQuery is the exact SQL probeInFlightPull runs. It is a SELECT,
// so its own INFO text never starts with "CALL DOLT_PULL" and can never
// match its own LIKE predicate — see the self-match note above.
const inFlightPullQuery = "SELECT ID, TIME FROM information_schema.processlist WHERE UPPER(TRIM(INFO)) LIKE 'CALL DOLT_PULL%'"

// processlistRow is the shape of one element of probeInFlightPull's `bd sql
// --json` output: information_schema.processlist columns come back with
// upper-case keys (verified live 2026-09-24 against ~/.agent-teams).
type processlistRow struct {
	ID   int64 `json:"ID"`
	Time int64 `json:"TIME"`
}

func probeInFlightPull(ctx context.Context, c *bd.Client) (*inFlightPull, error) {
	probeCtx, cancel := context.WithTimeout(ctx, pullProbeTimeout)
	defer cancel()

	out, err := c.RunContext(probeCtx, "sql", "--json", inFlightPullQuery)
	if err != nil {
		return nil, fmt.Errorf("probeInFlightPull: %w", err)
	}

	var rows []processlistRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		return nil, fmt.Errorf("probeInFlightPull: unmarshal: %w (raw: %.200s)", err, out)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	// Several rows means several queued CALL DOLT_PULLs; the one with the
	// largest TIME is the one actually executing (the rest are queued
	// behind it and haven't started their own git+ssh fetch yet).
	largest := rows[0]
	for _, row := range rows[1:] {
		if row.Time > largest.Time {
			largest = row
		}
	}
	return &inFlightPull{ConnID: largest.ID, Seconds: largest.Time}, nil
}

// guardedPull is the single entry point every pull caller (ateam pull,
// ateam sync, the relay tick) routes through. It probes for an in-flight
// DOLT_PULL, then either skips, fails, remedies, or proceeds to a bounded
// pull, per the frozen decision table below. On success it returns the
// underlying `bd dolt pull` stdout unchanged; on skip it returns ("", nil).
// stderr is where the one-line status messages in the stderr/exit contract
// (below) are written; the caller decides the process exit code from the
// returned error.
//
// Frozen decision table:
//
//	probe error                        -> proceed to bounded pull (fail-open;
//	                                       a probe failure never blocks a
//	                                       pull). One stderr line.
//	no in-flight pull                  -> bounded pull.
//	in-flight, age < pullStaleAfter:
//	    pullModeBestEffort              -> SKIP: return ("", nil), exit 0.
//	    pullModeRequired                -> return an error wrapping
//	                                       errPullInFlight, exit 1. Never
//	                                       queue behind it.
//	in-flight, age >= pullStaleAfter    -> call terminateHungPull, then
//	                                       bounded pull. If the remedy is not
//	                                       built (default stub) or fails,
//	                                       behave as the young case plus a
//	                                       manual-recovery hint.
//
// Frozen stderr / exit contract (exact prefixes; callers/tests assert the
// prefix, not the tail):
//
//	success          exit 0; bd stdout passthrough (unchanged).
//	skipped          exit 0; stderr "ateam pull: skipped: dolt pull already in flight (conn <id>, <n>s); using local state"
//	timed out        exit 1; stderr "ateam pull: timed out after <d>; using local state"
//	stale recovered  stderr "ateam pull: cleared hung dolt pull (conn <id>, <n>s)" then the bounded pull's own outcome.
//	in flight (sync) exit 1; error "ateam sync: dolt pull already in flight (conn <id>, <n>s); retry later"
//
// Hooks keep `|| true`; a timed-out or skipped pull degrades to local state,
// which is always correct.
func guardedPull(ctx context.Context, c *bd.Client, mode pullMode, timeout time.Duration, stderr io.Writer) (stdout string, err error) {
	inFlight, probeErr := probeInFlightPull(ctx, c)
	if probeErr != nil {
		// Fail-open: a probe failure never blocks a pull, it just loses the
		// early-skip optimization for this call.
		fmt.Fprintf(stderr, "ateam pull: probe failed (proceeding): %v\n", probeErr)
		return boundedPull(ctx, c, timeout, stderr)
	}
	if inFlight == nil {
		return boundedPull(ctx, c, timeout, stderr)
	}

	if inFlight.Seconds >= int64(pullStaleAfter.Seconds()) {
		if remedyErr := terminateHungPull(ctx, *inFlight); remedyErr != nil {
			fmt.Fprintf(stderr, "ateam pull: stale-pull remedy failed (conn %d, %ds): %v; manual recovery: on the dolt sql-server host, find and kill the git/ssh child under connection %d\n", inFlight.ConnID, inFlight.Seconds, remedyErr, inFlight.ConnID)
			return youngInFlightResult(*inFlight, mode, stderr)
		}
		fmt.Fprintf(stderr, "ateam pull: cleared hung dolt pull (conn %d, %ds)\n", inFlight.ConnID, inFlight.Seconds)
		return boundedPull(ctx, c, timeout, stderr)
	}

	return youngInFlightResult(*inFlight, mode, stderr)
}

// youngInFlightResult implements guardedPull's decision-table row for an
// in-flight pull younger than pullStaleAfter (and the stale-remedy-failed
// fallback, which behaves identically plus its own extra stderr line above).
func youngInFlightResult(p inFlightPull, mode pullMode, stderr io.Writer) (string, error) {
	if mode == pullModeRequired {
		return "", fmt.Errorf("ateam sync: %w (conn %d, %ds); retry later", errPullInFlight, p.ConnID, p.Seconds)
	}
	fmt.Fprintf(stderr, "ateam pull: skipped: dolt pull already in flight (conn %d, %ds); using local state\n", p.ConnID, p.Seconds)
	return "", nil
}

// boundedPull runs the actual `bd dolt pull`, bounded by timeout, and
// translates a ctx-deadline error into the frozen "timed out" stderr/exit
// contract.
func boundedPull(ctx context.Context, c *bd.Client, timeout time.Duration, stderr io.Writer) (string, error) {
	pullCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := c.RunContext(pullCtx, "dolt", "pull")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintf(stderr, "ateam pull: timed out after %s; using local state\n", timeout)
			return "", fmt.Errorf("ateam pull: timed out after %s: %w", timeout, err)
		}
		return "", err
	}
	return out, nil
}

// terminateHungPull is the stale-remedy seam guardedPull's stale branch
// calls: it kills a hung DOLT_PULL's git/ssh transport, identified by dolt
// sql-server process ancestry only (never the server PID itself, never a
// negative/process-group signal), so the server-side pull lock releases
// without disturbing the server. Ruled by Eric 2026-09-24 (Q3): yes, build
// this remedy.
//
// This package var is a test seam (tests fake it directly) and the join
// point for the stale-remedy bead (qdeh.3): its unix build
// (pull_stale_unix.go) overrides this default via an init() rather than
// editing this file, keeping qdeh.3 file-disjoint from the contract. Until
// that override lands (or on a build where it doesn't apply), the default
// below reports not-implemented, which guardedPull's stale branch treats as
// a remedy failure — falling back to the young-case behavior plus a
// manual-recovery hint, per the decision table above.
var terminateHungPull = func(ctx context.Context, p inFlightPull) error {
	return errors.New("terminateHungPull: not implemented")
}
