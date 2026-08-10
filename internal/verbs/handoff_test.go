package verbs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── handoff ───────────────────────────────────────────────────────────────────

func TestHandoff_AddsLabel(t *testing.T) {
	// ADD path: 2 calls — show <id> --json (read gate:review), then
	// label add <id> external-review.
	issue := bd.Issue{ID: "at-1", Labels: []string{"human", "gate:review"}}
	issueJSON, _ := json.Marshal([]bd.Issue{issue})
	ctx, calls := newCtx(t, []fakeResp{{stdout: string(issueJSON)}, {stdout: "ok"}})

	err := (&handoffKong{ID: "at-1"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 bd calls, got %d: %v", len(*calls), *calls)
	}
	assertArgs(t, *calls, 0, []string{"show", "at-1", "--json"})
	assertArgs(t, *calls, 1, []string{"label", "add", "at-1", "external-review"})

	if stderr := ctx.Stderr.(*bytes.Buffer).String(); stderr != "" {
		t.Errorf("expected no stderr warning when gate:review present, got: %q", stderr)
	}
}

func TestHandoff_ClearRemovesLabel(t *testing.T) {
	// --clear path: exactly 1 call, no label read.
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})

	err := (&handoffKong{ID: "at-1", Clear: true}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d: %v", len(*calls), *calls)
	}
	assertArgs(t, *calls, 0, []string{"label", "remove", "at-1", "external-review"})
}

func TestHandoff_WarnsWhenGateReviewAbsent(t *testing.T) {
	// Un-gated initiative: handoff still writes the label and warns to
	// stderr, exit code 0 (warn-and-proceed, external_review.go §3, §9).
	issue := bd.Issue{ID: "at-2", Labels: []string{}}
	issueJSON, _ := json.Marshal([]bd.Issue{issue})
	ctx, calls := newCtx(t, []fakeResp{{stdout: string(issueJSON)}, {stdout: "ok"}})

	err := (&handoffKong{ID: "at-2"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 bd calls, got %d: %v", len(*calls), *calls)
	}
	assertArgs(t, *calls, 1, []string{"label", "add", "at-2", "external-review"})

	stderr := ctx.Stderr.(*bytes.Buffer).String()
	if !strings.Contains(stderr, "at-2") || !strings.Contains(stderr, "gate:review") {
		t.Errorf("expected stderr warning naming id and gate:review, got: %q", stderr)
	}
}

func TestHandoff_ClearDoesNotWarn(t *testing.T) {
	// --clear never reads labels or warns — it unconditionally removes.
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})

	err := (&handoffKong{ID: "at-3", Clear: true}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d: %v", len(*calls), *calls)
	}
	if stderr := ctx.Stderr.(*bytes.Buffer).String(); stderr != "" {
		t.Errorf("expected no stderr on --clear, got: %q", stderr)
	}
}

func TestHandoff_NilContext(t *testing.T) {
	err := (&handoffKong{ID: "at-1"}).Run(nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestHandoff_ShowIssueErrorStillWritesLabel(t *testing.T) {
	// The lookup feeds only the gate:review warning. Eric's declaration is
	// derivable from nothing else, so a failing lookup degrades the warning
	// and the label add still runs (agent-teams-p9dm.42).
	ctx, calls := newCtx(t, []fakeResp{
		{errOut: "not found", err: fmt.Errorf("bd show: exit status 1")},
		{stdout: "ok"},
	})

	err := (&handoffKong{ID: "at-missing"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 bd calls, got %d: %v", len(*calls), *calls)
	}
	assertArgs(t, *calls, 1, []string{"label", "add", "at-missing", "external-review"})

	stderr := ctx.Stderr.(*bytes.Buffer).String()
	if !strings.Contains(stderr, "at-missing") || !strings.Contains(stderr, "declaring anyway") {
		t.Errorf("expected stderr warning naming id and declaring anyway, got: %q", stderr)
	}
}

func TestHandoff_LabelAddErrorPropagates(t *testing.T) {
	// Degrading the lookup must not swallow a real write failure.
	ctx, _ := newCtx(t, []fakeResp{
		{errOut: "not found", err: fmt.Errorf("bd show: exit status 1")},
		{errOut: "locked", err: fmt.Errorf("bd label add: exit status 1")},
	})

	if err := (&handoffKong{ID: "at-missing"}).Run(ctx); err == nil {
		t.Fatal("expected error when the label write itself fails")
	}
}

// TestGateHandoffPairAcrossDifferentSpellings is the end-to-end witness for
// the third confirmed manifestation of agent-teams-ssib.25: before this fix,
// a PR gated via `ateam gate --pr <spelling A>` and handed off via `ateam
// handoff --pr <spelling B>` (different scheme/case, the SAME real PR) could
// NEVER reach rest — computeExecutionStatus pairs a review gate with its
// handoff by exact discriminator string, so two byte-different spellings of
// one PR produced two different discriminators that could never match, and
// a correctly-run `ateam handoff` could never clear the REVIEWABLE state.
// Both --pr flags here resolve through the SAME initiative.CanonicalPRURL,
// so the labels end up byte-identical and pair despite the different input
// spellings.
func TestGateHandoffPairAcrossDifferentSpellings(t *testing.T) {
	issue := bd.Issue{
		ID:          "at-pair",
		Description: "pr: https://github.com/Owner/Repo/pull/9\n",
	}
	f := &fakeBD{runFn: func(args ...string) (string, error) {
		switch args[0] {
		case "show":
			return issueJSON(issue), nil
		case "label":
			if len(args) < 4 {
				return "", nil
			}
			switch args[1] {
			case "add":
				issue.Labels = append(issue.Labels, args[3])
			case "remove":
				var kept []string
				for _, l := range issue.Labels {
					if l != args[3] {
						kept = append(kept, l)
					}
				}
				issue.Labels = kept
			}
		}
		return "", nil
	}}
	ctx, _, _ := makeCtx(f, t.TempDir())

	gateFile := makeTempFile(t, "ready for review")
	gateSpelling := "http://github.com/owner/REPO/pull/9"
	if err := (&gateKong{ID: "at-pair", File: gateFile, Kind: "review", PR: gateSpelling}).Run(ctx); err != nil {
		t.Fatalf("gate: %v", err)
	}

	handoffSpelling := "https://github.com/OWNER/repo/pull/9"
	if err := (&handoffKong{ID: "at-pair", PR: handoffSpelling}).Run(ctx); err != nil {
		t.Fatalf("handoff: %v", err)
	}

	got := computeExecutionStatus(issue.Labels, nil, "")
	if got != StatusAwaitingExternalReview {
		t.Fatalf("execution status = %q, want %q (labels: %v)", got, StatusAwaitingExternalReview, issue.Labels)
	}
}

func TestHandoff_MissingID(t *testing.T) {
	// ID is a required positional; enforced at parse time.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"handoff"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing <id>")
	}
}
