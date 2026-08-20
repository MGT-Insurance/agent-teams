// This file is owned by Track P (PR identity and routing, agent-teams-ssib.7).
package verbs

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

func workstreamPtr(value string) *string { return &value }

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

func TestPrAddKong_ValidateWorkstreamToken(t *testing.T) {
	for _, value := range []string{"", "repo epic.2", "repo-epic.2\n"} {
		cmd := &prAddKong{
			InitiativeID: "at-z",
			URL:          "https://github.com/owner/repo/pull/42",
			Workstream:   workstreamPtr(value),
		}
		if err := cmd.Validate(); err == nil {
			t.Errorf("Validate --workstream=%q: expected error", value)
		}
	}
	cmd := &prAddKong{
		InitiativeID: "at-z",
		URL:          "https://github.com/owner/repo/pull/42",
		Workstream:   workstreamPtr("repo-epic.2"),
	}
	if err := cmd.Validate(); err != nil {
		t.Fatalf("valid --workstream rejected: %v", err)
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

// TestPrAddKong_Run_SeedsRailFromNotesOnlyLegacyPR is the load-bearing
// witness for agent-teams-ssib.23: a legacy initiative whose FIRST PR lives
// only in Notes (the dri skill's pre-migration write path, 178 of 549
// registered initiatives) must not lose that PR the moment a second one is
// added via `ateam pr add`. Before this fix, Run called WithPR directly on
// the raw issue, so the rail ended up holding ONLY the newly-added URL —
// ResolvedPRs' rail-wins-wholesale rule then made the Notes-only PR vanish
// from every consumer.
func TestPrAddKong_Run_SeedsRailFromNotesOnlyLegacyPR(t *testing.T) {
	const legacyPR = "https://github.com/erlloyd/pr-shepherd/pull/3"
	const newPR = "https://github.com/owner/repo/pull/9"
	issue := bd.Issue{
		ID:          "at-legacy",
		Description: "repo: /r\nworktree: /wt\nbranch: main\n", // no "pr:" line at all
		Notes:       "delivered.\npr: " + legacyPR + "\n",
	}
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

	cmd := &prAddKong{InitiativeID: "at-legacy", URL: newPR}
	if err := cmd.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if updateCalls != 1 {
		t.Fatalf("expected 1 bd update call, got %d", updateCalls)
	}

	got := initiative.ResolvedPRs(issue)
	want := []string{legacyPR, newPR}
	if len(got) != len(want) {
		t.Fatalf("ResolvedPRs after pr add = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("ResolvedPRs[%d] = %q, want %q", i, got[i], w)
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

func TestPrAddKong_RunPersistsPRAndValidatedWorkstreamInOneUpdate(t *testing.T) {
	const (
		repo       = "/project/repo"
		epic       = "repo-root"
		workstream = "repo-root.2.1"
		canonical  = "https://github.com/owner/repo/pull/9"
	)
	issue := bd.Issue{
		ID:          "at-owned",
		Description: "repo: " + repo + "\nepic: " + epic + "\ncustom: untouched  \n",
	}
	var updates []string
	global := &fakeBD{runFn: func(args ...string) (string, error) {
		switch args[0] {
		case "show":
			return issueJSON(issue), nil
		case "update":
			bodyFile := strings.TrimPrefix(args[2], "--body-file=")
			body, err := readFileT(t, bodyFile)
			if err != nil {
				t.Fatalf("read update body: %v", err)
			}
			updates = append(updates, body)
			issue.Description = body
		}
		return "", nil
	}}

	var projectRepo string
	var projectShows []string
	project := &fakeBD{runFn: func(args ...string) (string, error) {
		projectShows = append(projectShows, args[1])
		switch args[1] {
		case workstream:
			return fmt.Sprintf(`[{"id":%q,"parent":"repo-root.2"}]`, workstream), nil
		case "repo-root.2":
			return `[{"id":"repo-root.2","parent":"repo-root"}]`, nil
		default:
			return `[]`, nil
		}
	}}
	cmd := &prAddKong{
		InitiativeID: "at-owned",
		URL:          "http://github.com/Owner/Repo/pull/9",
		Workstream:   workstreamPtr(workstream),
		newProjectBD: func(got string) projectBDRunner {
			projectRepo = got
			return project
		},
	}
	ctx, _, _ := makeCtx(global, t.TempDir())
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if projectRepo != repo {
		t.Fatalf("project client repo = %q, want %q", projectRepo, repo)
	}
	if got, want := strings.Join(projectShows, ","), workstream+",repo-root.2"; got != want {
		t.Fatalf("project show chain = %q, want %q", got, want)
	}
	if len(updates) != 1 {
		t.Fatalf("global update calls = %d, want exactly 1", len(updates))
	}
	if !strings.Contains(updates[0], "pr: "+canonical+"\n") ||
		!strings.Contains(updates[0], "pr-workstream: "+canonical+" "+workstream+"\n") {
		t.Fatalf("single update does not contain both rails:\n%s", updates[0])
	}
	if !strings.Contains(updates[0], "custom: untouched  \n") {
		t.Fatalf("unmodeled field bytes were not preserved:\n%s", updates[0])
	}
}

func TestPrAddKong_RunMappedRepeatIsIdempotent(t *testing.T) {
	const association = "pr-workstream: https://github.com/owner/repo/pull/9 repo-root.2\n"
	issue := bd.Issue{
		ID:          "at-repeat",
		Description: "repo: /project\nepic: repo-root\npr: https://github.com/owner/repo/pull/9\n" + association,
	}
	updates := 0
	global := &fakeBD{runFn: func(args ...string) (string, error) {
		if args[0] == "show" {
			return issueJSON(issue), nil
		}
		if args[0] == "update" {
			updates++
		}
		return "", nil
	}}
	project := &fakeBD{runFn: func(args ...string) (string, error) {
		return `[{"id":"repo-root.2","parent":"repo-root"}]`, nil
	}}
	ctx, stdout, _ := makeCtx(global, t.TempDir())
	cmd := &prAddKong{
		InitiativeID: "at-repeat",
		URL:          "https://github.com/OWNER/REPO/pull/9",
		Workstream:   workstreamPtr("repo-root.2"),
		newProjectBD: func(string) projectBDRunner { return project },
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if updates != 0 {
		t.Fatalf("repeat pair performed %d updates, want 0", updates)
	}
	if !strings.Contains(stdout.String(), "already recorded") {
		t.Fatalf("stdout = %q, want already-recorded notice", stdout.String())
	}
}

func TestPrAddKong_RunRejectsConflictingWorkstreamBeforeProjectRead(t *testing.T) {
	issue := bd.Issue{
		ID: "at-conflict",
		Description: "repo: /project\nepic: repo-root\n" +
			"pr: https://github.com/owner/repo/pull/9\n" +
			"pr-workstream: https://github.com/owner/repo/pull/9 repo-root.2\n",
	}
	updates := 0
	global := &fakeBD{runFn: func(args ...string) (string, error) {
		if args[0] == "show" {
			return issueJSON(issue), nil
		}
		if args[0] == "update" {
			updates++
		}
		return "", nil
	}}
	ctx, _, _ := makeCtx(global, t.TempDir())
	cmd := &prAddKong{
		InitiativeID: "at-conflict",
		URL:          "https://github.com/owner/repo/pull/9",
		Workstream:   workstreamPtr("repo-root.7"),
		newProjectBD: func(string) projectBDRunner {
			t.Fatal("project must not be read after a persisted mapping already proves a conflict")
			return nil
		},
	}
	err := cmd.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "already associated with workstream repo-root.2") {
		t.Fatalf("conflicting Run error = %v", err)
	}
	if updates != 0 {
		t.Fatalf("conflict performed %d updates, want 0", updates)
	}
}

func TestPrAddKong_RunRejectsNonDescendantWithoutGlobalMutation(t *testing.T) {
	issue := bd.Issue{ID: "at-other", Description: "repo: /project\nepic: repo-root\n"}
	updates := 0
	global := &fakeBD{runFn: func(args ...string) (string, error) {
		if args[0] == "show" {
			return issueJSON(issue), nil
		}
		updates++
		return "", nil
	}}
	project := &fakeBD{runFn: func(args ...string) (string, error) {
		if args[1] == "other-root.2" {
			return `[{"id":"other-root.2","parent":"other-root"}]`, nil
		}
		return `[{"id":"other-root"}]`, nil
	}}
	ctx, _, _ := makeCtx(global, t.TempDir())
	cmd := &prAddKong{
		InitiativeID: "at-other",
		URL:          "https://github.com/owner/repo/pull/9",
		Workstream:   workstreamPtr("other-root.2"),
		newProjectBD: func(string) projectBDRunner { return project },
	}
	err := cmd.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "not a descendant of epic repo-root") {
		t.Fatalf("non-descendant Run error = %v", err)
	}
	if updates != 0 {
		t.Fatalf("non-descendant performed %d global mutations, want 0", updates)
	}
}

func TestPrAddKong_RunMappedRequiresRepoAndEpic(t *testing.T) {
	for _, tc := range []struct {
		name        string
		description string
		want        string
	}{
		{name: "missing repo", description: "epic: repo-root\n", want: "non-empty repo"},
		{name: "missing epic", description: "repo: /project\n", want: "non-empty epic"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issue := bd.Issue{ID: "at-missing", Description: tc.description}
			global := &fakeBD{runFn: func(args ...string) (string, error) {
				if args[0] == "show" {
					return issueJSON(issue), nil
				}
				t.Fatalf("unexpected mutation: %v", args)
				return "", nil
			}}
			ctx, _, _ := makeCtx(global, t.TempDir())
			cmd := &prAddKong{
				InitiativeID: "at-missing",
				URL:          "https://github.com/owner/repo/pull/9",
				Workstream:   workstreamPtr("repo-root.2"),
				newProjectBD: func(string) projectBDRunner {
					t.Fatal("project client must not be created without routing fields")
					return nil
				},
			}
			err := cmd.Run(ctx)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Run error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPrAddKong_RunMappedFailsClosedOnUnreadableMissingOrCyclicProject(t *testing.T) {
	for _, tc := range []struct {
		name       string
		projectRun func(args ...string) (string, error)
		want       string
	}{
		{
			name: "unreadable project",
			projectRun: func(args ...string) (string, error) {
				return "", errors.New("project store unavailable")
			},
			want: "project store unavailable",
		},
		{
			name: "missing bead",
			projectRun: func(args ...string) (string, error) {
				return `[]`, nil
			},
			want: "not found",
		},
		{
			name: "cyclic parents",
			projectRun: func(args ...string) (string, error) {
				if args[1] == "repo-root.2" {
					return `[{"id":"repo-root.2","parent":"repo-root.3"}]`, nil
				}
				return `[{"id":"repo-root.3","parent":"repo-root.2"}]`, nil
			},
			want: "contains a cycle",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issue := bd.Issue{ID: "at-failclosed", Description: "repo: /project\nepic: repo-root\n"}
			global := &fakeBD{runFn: func(args ...string) (string, error) {
				if args[0] == "show" {
					return issueJSON(issue), nil
				}
				t.Fatalf("unexpected global mutation: %v", args)
				return "", nil
			}}
			ctx, _, _ := makeCtx(global, t.TempDir())
			cmd := &prAddKong{
				InitiativeID: "at-failclosed",
				URL:          "https://github.com/owner/repo/pull/9",
				Workstream:   workstreamPtr("repo-root.2"),
				newProjectBD: func(string) projectBDRunner { return &fakeBD{runFn: tc.projectRun} },
			}
			err := cmd.Run(ctx)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Run error = %v, want %q", err, tc.want)
			}
		})
	}
}

// TestPrAddKong_Run_NilContext covers the standard nil-context guard.
func TestPrAddKong_Run_NilContext(t *testing.T) {
	cmd := &prAddKong{InitiativeID: "at-z", URL: "https://github.com/owner/repo/pull/1"}
	if err := cmd.Run(nil); err == nil {
		t.Error("expected error for nil context")
	}
}

// ── resolvePR (agent-teams-ssib.25) ──────────────────────────────────────────

// TestResolvePR_RejectsPRNotOnInitiative is the load-bearing witness for
// agent-teams-ssib.25's --pr validation: a URL that is well-formed but not
// one of the initiative's actual resolved PRs must be REJECTED — a rejected
// command beats minting a label nothing can ever pair with or clear.
func TestResolvePR_RejectsPRNotOnInitiative(t *testing.T) {
	issue := bd.Issue{ID: "at-r1", Description: "pr: https://github.com/owner/repo/pull/1\n"}
	f := &fakeBD{runFn: func(args ...string) (string, error) {
		if args[0] == "show" {
			return issueJSON(issue), nil
		}
		return "", nil
	}}
	ctx, _, _ := makeCtx(f, t.TempDir())

	_, _, err := resolvePR(ctx, "ateam gate", "at-r1", "https://github.com/owner/repo/pull/999")
	if err == nil {
		t.Fatal("expected rejection for a --pr not recorded on the initiative, got nil")
	}
	if !strings.Contains(err.Error(), "not a PR recorded on") {
		t.Errorf("error = %q, want it to name the rejection reason", err.Error())
	}
}

// TestResolvePR_ResolvesDifferentSpellingOfSameResolvedPR is the load-bearing
// witness for the identity half of agent-teams-ssib.25: a --pr spelled
// differently (scheme, case) from how the PR is recorded on the rail must
// still resolve — canonicalized identity, not byte-exact string match — and
// the CANONICAL form is what's returned, so every per-PR label for this PR
// ends up byte-identical regardless of which spelling a caller used.
func TestResolvePR_ResolvesDifferentSpellingOfSameResolvedPR(t *testing.T) {
	issue := bd.Issue{ID: "at-r2", Description: "pr: https://github.com/owner/repo/pull/1\n"}
	f := &fakeBD{runFn: func(args ...string) (string, error) {
		if args[0] == "show" {
			return issueJSON(issue), nil
		}
		return "", nil
	}}
	ctx, _, _ := makeCtx(f, t.TempDir())

	got, _, err := resolvePR(ctx, "ateam gate", "at-r2", "http://github.com/Owner/Repo/pull/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://github.com/owner/repo/pull/1"
	if got != want {
		t.Errorf("resolvePR = %q, want canonical %q", got, want)
	}
}

// TestResolvePR_RejectsMalformedURL confirms a --pr that doesn't even parse
// as a GitHub PR URL fails before any bd show call.
func TestResolvePR_RejectsMalformedURL(t *testing.T) {
	ctx, _, _ := makeCtx(&fakeBD{runFn: func(args ...string) (string, error) {
		t.Fatalf("bd show must not be called for a malformed --pr, got args %v", args)
		return "", nil
	}}, t.TempDir())

	if _, _, err := resolvePR(ctx, "ateam gate", "at-r3", "not-a-url"); err == nil {
		t.Fatal("expected rejection for a malformed --pr, got nil")
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
