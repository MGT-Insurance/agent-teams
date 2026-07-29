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

func TestHandoff_ShowIssueError(t *testing.T) {
	ctx, _ := newCtx(t, []fakeResp{{errOut: "not found", err: fmt.Errorf("bd show: exit status 1")}})
	err := (&handoffKong{ID: "at-missing"}).Run(ctx)
	if err == nil {
		t.Fatal("expected error when initiative lookup fails")
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
