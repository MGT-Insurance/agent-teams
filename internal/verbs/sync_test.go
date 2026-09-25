package verbs

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// TestSync_PushIsBoundedByPushTimeout_NotPullTimeout witnesses the property
// agent-teams-8st0.1 fixes: `ateam sync`'s `dolt push` execs must be bounded
// by pushTimeoutDefault (90s), not pullTimeoutDefault (20s) — a real push
// against the global workspace takes 22-30s of CPU work, so binding it to
// the pull-tuned 20s bound fails every push with a deadline error.
//
// Rather than actually waiting out a real timeout, this captures the ctx
// deadline RunContext hands to each exec and asserts the remaining duration
// at call time: ~pullTimeoutDefault for `dolt commit`, ~pushTimeoutDefault
// for `dolt push`. Reverting boundedBDRun's push call site back to
// pullTimeoutDefault makes this test fail (verified below the test).
func TestSync_PushIsBoundedByPushTimeout_NotPullTimeout(t *testing.T) {
	var commitRemaining, pushRemaining time.Duration
	sawCommit, sawPush := false, false

	execFn := func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		// RunContext prepends "-C", <home> to every call.
		sub := args
		if len(args) >= 2 && args[0] == "-C" {
			sub = args[2:]
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatalf("exec call %v: context has no deadline", sub)
		}
		remaining := time.Until(deadline)

		switch {
		case len(sub) >= 2 && sub[0] == "dolt" && sub[1] == "commit":
			sawCommit = true
			commitRemaining = remaining
			return []byte("nothing to commit"), nil, nil
		case len(sub) >= 2 && sub[0] == "dolt" && sub[1] == "pull":
			return []byte("up to date"), nil, nil
		case len(sub) >= 2 && sub[0] == "dolt" && sub[1] == "push":
			sawPush = true
			pushRemaining = remaining
			return []byte("pushed"), nil, nil
		case len(sub) >= 1 && sub[0] == "sql":
			return []byte("[]"), nil, nil // probeInFlightPull: no in-flight pull
		default:
			t.Fatalf("unexpected exec call: %v", sub)
			return nil, nil, nil
		}
	}

	client := bd.NewClientWithContextExec(t.TempDir(), execFn)
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Home: t.TempDir(), BD: client, Stdout: &stdout, Stderr: &stderr}

	if err := (&syncKong{}).Run(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !sawCommit || !sawPush {
		t.Fatalf("sync did not exercise both commit and push execs (commit=%v push=%v)", sawCommit, sawPush)
	}

	const tolerance = 2 * time.Second
	if diff := (commitRemaining - pullTimeoutDefault).Abs(); diff > tolerance {
		t.Errorf("dolt commit ctx deadline remaining = %v, want ~%v (pullTimeoutDefault)", commitRemaining, pullTimeoutDefault)
	}
	if diff := (pushRemaining - pushTimeoutDefault).Abs(); diff > tolerance {
		t.Errorf("dolt push ctx deadline remaining = %v, want ~%v (pushTimeoutDefault), not pullTimeoutDefault (%v)", pushRemaining, pushTimeoutDefault, pullTimeoutDefault)
	}
}
