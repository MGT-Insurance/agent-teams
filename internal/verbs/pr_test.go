// This file is owned by Track P (PR identity and routing, agent-teams-ssib.7).
package verbs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

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

func TestPrAddKong_RunFailsBeforeReadWhenInitiativeLockCannotBeCreated(t *testing.T) {
	homeFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(homeFile, []byte("occupied"), 0o600); err != nil {
		t.Fatalf("write fake home file: %v", err)
	}
	global := &fakeBD{runFn: func(args ...string) (string, error) {
		t.Fatalf("bd must not be read when lock acquisition fails, got %v", args)
		return "", nil
	}}
	ctx, _, _ := makeCtx(global, homeFile)
	err := (&prAddKong{
		InitiativeID: "at-lock-error",
		URL:          "https://github.com/owner/repo/pull/9",
	}).Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "acquire initiative at-lock-error lock") {
		t.Fatalf("Run error = %v, want initiative lock acquisition failure", err)
	}
}

func TestPrAddKong_RunReleasesInitiativeLockAfterUpdateError(t *testing.T) {
	home := t.TempDir()
	issue := bd.Issue{ID: "at-release", Description: "repo: /project\nepic: repo-root\n"}
	global := &fakeBD{runFn: func(args ...string) (string, error) {
		if args[0] == "show" {
			return issueJSON(issue), nil
		}
		return "", errors.New("injected update failure")
	}}
	ctx, _, _ := makeCtx(global, home)
	err := (&prAddKong{
		InitiativeID: "at-release",
		URL:          "https://github.com/owner/repo/pull/9",
	}).Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "injected update failure") {
		t.Fatalf("Run error = %v, want injected update failure", err)
	}

	path := initiativeMutationLockPath(home, "at-release")
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open lock after failed command: %v", err)
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("lock remained held after failed command: %v", err)
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	info, err := file.Stat()
	if err != nil {
		t.Fatalf("stat lock: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("lock permissions = %o, want 600", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat lock directory: %v", err)
	}
	if got := dirInfo.Mode().Perm() & 0o077; got != 0 {
		t.Fatalf("lock directory grants group/other permissions: %o", dirInfo.Mode().Perm())
	}
}

func TestInitiativeMutationLockPathScopesUntrustedIDUnderHome(t *testing.T) {
	home := t.TempDir()
	first := initiativeMutationLockPath(home, "../other/initiative")
	second := initiativeMutationLockPath(home, "at-safe")
	if first == second {
		t.Fatal("distinct initiative IDs resolved to the same lock path")
	}
	rel, err := filepath.Rel(home, first)
	if err != nil {
		t.Fatalf("relative lock path: %v", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("initiative lock escaped home: %s", first)
	}
	if got, want := filepath.Dir(first), filepath.Join(home, ".locks", "initiatives"); got != want {
		t.Fatalf("initiative lock dir = %q, want %q", got, want)
	}
}

const prAddProcessHelperEnv = "ATEAM_PR_ADD_PROCESS_HELPER"

type concurrentInitiativeMutation struct {
	action     string
	url        string
	workstream string
	session    string
	track      string
}

// TestPrAddKong_ConcurrentProcessesPreserveDistinctMappedAdds is a real
// process-level regression for the lost-update race. Two copies of this test
// executable start together and use independent clients against one shared
// file-backed initiative. Each show deliberately waits for the peer's show;
// without the process lock both read the old description and the last update
// drops one mapping. With the lock, the second process cannot read until the
// first process has completed its update.
func TestPrAddKong_ConcurrentProcessesPreserveDistinctMappedAdds(t *testing.T) {
	if os.Getenv(prAddProcessHelperEnv) != "" {
		t.Skip("parent-only concurrency test")
	}
	description := runConcurrentInitiativeMutations(t, []concurrentInitiativeMutation{
		{action: "pr", url: "https://github.com/owner/repo/pull/41", workstream: "repo-root.1"},
		{action: "pr", url: "https://github.com/owner/repo/pull/42", workstream: "repo-root.2"},
	})
	issue := bd.Issue{ID: "at-concurrent", Description: description}
	fields := initiative.Of(issue)
	wantPRs := map[string]bool{
		"https://github.com/owner/repo/pull/41": false,
		"https://github.com/owner/repo/pull/42": false,
	}
	for _, pr := range fields.PRs {
		if _, ok := wantPRs[pr]; ok {
			wantPRs[pr] = true
		}
	}
	for pr, found := range wantPRs {
		if !found {
			t.Errorf("final PR rail omitted %s:\n%s", pr, description)
		}
	}
	wantMappings := map[string]string{
		"https://github.com/owner/repo/pull/41": "repo-root.1",
		"https://github.com/owner/repo/pull/42": "repo-root.2",
	}
	gotMappings := make(map[string]string)
	for _, association := range initiative.PRWorkstreams(issue) {
		gotMappings[association.PR] = association.Workstream
	}
	for pr, wantWorkstream := range wantMappings {
		if got := gotMappings[pr]; got != wantWorkstream {
			t.Errorf("final mapping for %s = %q, want %q:\n%s", pr, got, wantWorkstream, description)
		}
	}
}

func TestPrAddKong_ConcurrentProcessPRAddAndSessionTiePreserveBoth(t *testing.T) {
	if os.Getenv(prAddProcessHelperEnv) != "" {
		t.Skip("parent-only concurrency test")
	}
	const (
		prURL      = "https://github.com/owner/repo/pull/41"
		workstream = "repo-root.1"
		sessionID  = "sess-concurrent"
	)
	description := runConcurrentInitiativeMutations(t, []concurrentInitiativeMutation{
		{action: "pr", url: prURL, workstream: workstream},
		{action: "session", session: sessionID},
	})
	issue := bd.Issue{ID: "at-concurrent", Description: description}
	fields := initiative.Of(issue)
	if len(fields.Sessions) != 1 || fields.Sessions[0] != sessionID {
		t.Errorf("final sessions = %v, want [%s]:\n%s", fields.Sessions, sessionID, description)
	}
	if len(fields.PRs) != 1 || fields.PRs[0] != prURL {
		t.Errorf("final PRs = %v, want [%s]:\n%s", fields.PRs, prURL, description)
	}
	associations := initiative.PRWorkstreams(issue)
	if len(associations) != 1 || associations[0].PR != prURL || associations[0].Workstream != workstream {
		t.Errorf("final PR associations = %#v, want %s -> %s:\n%s", associations, prURL, workstream, description)
	}
}

func runConcurrentInitiativeMutations(t *testing.T, mutations []concurrentInitiativeMutation) string {
	t.Helper()
	if len(mutations) != 2 {
		t.Fatalf("concurrent mutation helper requires exactly 2 children, got %d", len(mutations))
	}
	home := t.TempDir()
	statePath := filepath.Join(home, "initiative-description")
	if err := os.WriteFile(statePath, []byte("repo: /project\nepic: repo-root\n"), 0o600); err != nil {
		t.Fatalf("write initial state: %v", err)
	}
	startPath := filepath.Join(home, "start")
	readyPaths := []string{filepath.Join(home, "ready-1"), filepath.Join(home, "ready-2")}
	shownPaths := []string{filepath.Join(home, "shown-1"), filepath.Join(home, "shown-2")}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	type child struct {
		cmd    *exec.Cmd
		output bytes.Buffer
	}
	children := make([]child, len(mutations))
	for i, mutation := range mutations {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPrAddKong_ProcessHelper$")
		cmd.Env = append(os.Environ(),
			prAddProcessHelperEnv+"=1",
			"ATEAM_PR_ADD_HOME="+home,
			"ATEAM_PR_ADD_STATE="+statePath,
			"ATEAM_PR_ADD_START="+startPath,
			"ATEAM_PR_ADD_READY="+readyPaths[i],
			"ATEAM_PR_ADD_SHOWN="+shownPaths[i],
			"ATEAM_PR_ADD_OTHER_SHOWN="+shownPaths[1-i],
			"ATEAM_PR_ADD_ACTION="+mutation.action,
			"ATEAM_PR_ADD_URL="+mutation.url,
			"ATEAM_PR_ADD_WORKSTREAM="+mutation.workstream,
			"ATEAM_PR_ADD_SESSION="+mutation.session,
			"ATEAM_PR_ADD_TRACK="+mutation.track,
			fmt.Sprintf("ATEAM_PR_ADD_TEMP_SUFFIX=%d", i+1),
		)
		cmd.Stdout = &children[i].output
		cmd.Stderr = &children[i].output
		children[i].cmd = cmd
		if err := cmd.Start(); err != nil {
			t.Fatalf("start child %d: %v", i+1, err)
		}
	}

	waitForPRAddFiles(t, ctx, readyPaths...)
	if err := os.WriteFile(startPath, []byte("go"), 0o600); err != nil {
		t.Fatalf("release children: %v", err)
	}
	for i := range children {
		if err := children[i].cmd.Wait(); err != nil {
			t.Fatalf("child %d failed: %v\n%s", i+1, err, children[i].output.String())
		}
	}

	description, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read final state: %v", err)
	}
	return string(description)
}

func waitForPRAddFiles(t *testing.T, ctx context.Context, paths ...string) {
	t.Helper()
	for {
		allPresent := true
		for _, path := range paths {
			if _, err := os.Stat(path); err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("stat helper marker %s: %v", path, err)
				}
				allPresent = false
			}
		}
		if allPresent {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for helper markers %v: %v", paths, ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestPrAddKong_ProcessHelper(t *testing.T) {
	if os.Getenv(prAddProcessHelperEnv) == "" {
		t.Skip("subprocess helper")
	}
	home := os.Getenv("ATEAM_PR_ADD_HOME")
	statePath := os.Getenv("ATEAM_PR_ADD_STATE")
	readyPath := os.Getenv("ATEAM_PR_ADD_READY")
	shownPath := os.Getenv("ATEAM_PR_ADD_SHOWN")
	otherShownPath := os.Getenv("ATEAM_PR_ADD_OTHER_SHOWN")
	if err := os.WriteFile(readyPath, []byte("ready"), 0o600); err != nil {
		t.Fatalf("write ready marker: %v", err)
	}
	helperCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	waitForPRAddFiles(t, helperCtx, os.Getenv("ATEAM_PR_ADD_START"))

	readIssue := func() (bd.Issue, error) {
		description, err := os.ReadFile(statePath)
		return bd.Issue{ID: "at-concurrent", Status: "open", Description: string(description)}, err
	}
	global := &fakeBD{
		runFn: func(args ...string) (string, error) {
			switch args[0] {
			case "show":
				issue, err := readIssue()
				if err != nil {
					return "", err
				}
				if err := os.WriteFile(shownPath, []byte("shown"), 0o600); err != nil {
					return "", err
				}
				deadline := time.Now().Add(500 * time.Millisecond)
				for time.Now().Before(deadline) {
					if _, err := os.Stat(otherShownPath); err == nil {
						break
					}
					time.Sleep(5 * time.Millisecond)
				}
				return issueJSON(issue), nil
			case "update":
				bodyPath := strings.TrimPrefix(args[2], "--body-file=")
				description, err := os.ReadFile(bodyPath)
				if err != nil {
					return "", err
				}
				tmpPath := statePath + "." + os.Getenv("ATEAM_PR_ADD_TEMP_SUFFIX")
				if err := os.WriteFile(tmpPath, description, 0o600); err != nil {
					return "", err
				}
				if err := os.Rename(tmpPath, statePath); err != nil {
					return "", err
				}
			}
			return "", nil
		},
		runJSONFn: func(dst any, args ...string) error {
			issue, err := readIssue()
			if err != nil {
				return err
			}
			issues, ok := dst.(*[]bd.Issue)
			if !ok {
				return fmt.Errorf("unexpected list destination %T", dst)
			}
			*issues = []bd.Issue{issue}
			return nil
		},
	}
	project := &fakeBD{runFn: func(args ...string) (string, error) {
		return fmt.Sprintf(`[{"id":%q,"parent":"repo-root"}]`, args[1]), nil
	}}
	ctx, _, _ := makeCtx(global, home)
	switch os.Getenv("ATEAM_PR_ADD_ACTION") {
	case "pr":
		workstream := os.Getenv("ATEAM_PR_ADD_WORKSTREAM")
		cmd := &prAddKong{
			InitiativeID: "at-concurrent",
			URL:          os.Getenv("ATEAM_PR_ADD_URL"),
			Workstream:   &workstream,
			newProjectBD: func(string) projectBDRunner { return project },
		}
		if err := cmd.Run(ctx); err != nil {
			t.Fatalf("mapped pr add: %v", err)
		}
	case "session":
		if err := appendSessionID(ctx, "at-concurrent", os.Getenv("ATEAM_PR_ADD_SESSION")); err != nil {
			t.Fatalf("session tie: %v", err)
		}
	case "track":
		cmd := &trackAddKong{
			InitiativeID: "at-concurrent",
			Path:         os.Getenv("ATEAM_PR_ADD_TRACK"),
		}
		if err := cmd.Run(ctx); err != nil {
			t.Fatalf("track add: %v", err)
		}
	default:
		t.Fatalf("unknown helper action %q", os.Getenv("ATEAM_PR_ADD_ACTION"))
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
