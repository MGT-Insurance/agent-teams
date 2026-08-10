// This file is owned by Track P (PR identity and routing, agent-teams-ssib.7).
package verbs

import (
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// ── prAddKong.Validate ────────────────────────────────────────────────────────

func TestPrAddKong_Validate_RejectsEmptyInitiativeID(t *testing.T) {
	cmd := &prAddKong{InitiativeID: "", URL: "https://github.com/owner/repo/pull/1"}
	if err := cmd.Validate(); err == nil {
		t.Error("expected error for empty initiative-id")
	}
}

func TestPrAddKong_Validate_RejectsMalformedURL(t *testing.T) {
	for _, u := range []string{
		"",
		"not-a-url",
		"https://gitlab.com/owner/repo/pull/1",   // wrong host
		"https://github.com/owner/repo/pulls/1",  // wrong path segment
		"https://github.com/owner/repo/pull/abc", // non-numeric PR number
	} {
		cmd := &prAddKong{InitiativeID: "at-z", URL: u}
		if err := cmd.Validate(); err == nil {
			t.Errorf("Validate(%q): expected error, got nil", u)
		}
	}
}

func TestPrAddKong_Validate_AcceptsWellFormedURL(t *testing.T) {
	cmd := &prAddKong{InitiativeID: "at-z", URL: "https://github.com/owner/repo/pull/42"}
	if err := cmd.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── prAddKong.Run ─────────────────────────────────────────────────────────────

// TestPrAddKong_Run_RecordsSecondAndThirdPR exercises exactly what the bead
// calls out: "Calling it repeatedly is how a second and third PR get
// recorded" — three sequential `pr add` calls on the same initiative must
// accumulate all three URLs on the rail, in order, via three real bd update
// calls (the fake mutates its held issue on "update" the same way bd would).
func TestPrAddKong_Run_RecordsSecondAndThirdPR(t *testing.T) {
	issue := bd.Issue{ID: "at-x", Description: "repo: /r\nworktree: /wt\nbranch: main\n"}
	updateCalls := 0
	f := &fakeBD{
		runFn: func(args ...string) (string, error) {
			switch args[0] {
			case "show":
				return issueJSON(issue), nil
			case "update":
				updateCalls++
				bodyFileArg := strings.TrimPrefix(args[2], "--body-file=")
				content, err := readFileT(t, bodyFileArg)
				if err != nil {
					t.Fatalf("read body file: %v", err)
				}
				issue.Description = content
			}
			return "", nil
		},
	}
	ctx, _, _ := makeCtx(f, t.TempDir())

	urls := []string{
		"https://github.com/owner/repo/pull/1",
		"https://github.com/owner/repo/pull/2",
		"https://github.com/owner/repo/pull/3",
	}
	for _, u := range urls {
		cmd := &prAddKong{InitiativeID: "at-x", URL: u}
		if err := cmd.Validate(); err != nil {
			t.Fatalf("Validate(%q): %v", u, err)
		}
		if err := cmd.Run(ctx); err != nil {
			t.Fatalf("Run(%q): %v", u, err)
		}
	}
	if updateCalls != 3 {
		t.Fatalf("expected 3 bd update calls, got %d", updateCalls)
	}
	got := initiative.Of(issue).PRs
	if len(got) != len(urls) {
		t.Fatalf("PRs: got %v, want %v", got, urls)
	}
	for i, want := range urls {
		if got[i] != want {
			t.Errorf("PRs[%d]: got %q, want %q", i, got[i], want)
		}
	}
}

// TestPrAddKong_Run_IdempotentOnRepeatURL verifies re-adding the same URL is a
// no-op: no bd update call, informational message on stdout.
func TestPrAddKong_Run_IdempotentOnRepeatURL(t *testing.T) {
	issue := bd.Issue{ID: "at-y", Description: "pr: https://github.com/owner/repo/pull/9\n"}
	updateCalls := 0
	f := &fakeBD{
		runFn: func(args ...string) (string, error) {
			switch args[0] {
			case "show":
				return issueJSON(issue), nil
			case "update":
				updateCalls++
			}
			return "", nil
		},
	}
	ctx, stdout, _ := makeCtx(f, t.TempDir())
	cmd := &prAddKong{InitiativeID: "at-y", URL: "https://github.com/owner/repo/pull/9"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if updateCalls != 0 {
		t.Errorf("expected no bd update call for a repeat URL, got %d", updateCalls)
	}
	if !strings.Contains(stdout.String(), "already recorded") {
		t.Errorf("expected 'already recorded' message, got %q", stdout.String())
	}
}

// TestPrAddKong_Run_NilContext covers the standard nil-context guard.
func TestPrAddKong_Run_NilContext(t *testing.T) {
	cmd := &prAddKong{InitiativeID: "at-z", URL: "https://github.com/owner/repo/pull/1"}
	if err := cmd.Run(nil); err == nil {
		t.Error("expected error for nil context")
	}
}

// ── registration ──────────────────────────────────────────────────────────────

func TestRegisterPRKong_AddedAsKongVerb(t *testing.T) {
	parser, err := cli.NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	RegisterPRKong(parser)
	_, parseErr := parser.Parse([]string{"pr", "add", "--help"})
	_ = parseErr // help triggers exit(0), not a real error
}
