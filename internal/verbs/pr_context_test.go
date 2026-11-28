package verbs

import (
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

func runPRContext(t *testing.T, issue bd.Issue, c *prContextKong) (string, error) {
	t.Helper()
	f := &fakeBD{runFn: func(args ...string) (string, error) { return issueJSON(issue), nil }}
	ctx, stdout, _ := makeCtx(f, t.TempDir())
	c.InitiativeID = "at-x"
	err := c.Run(ctx)
	return stdout.String(), err
}

const prCtxBase = "repo: /r\nworktree: /wt\nbranch: main\n"

func TestPRContext_TwoPRsOutsider(t *testing.T) {
	issue := bd.Issue{ID: "at-x", Description: prCtxBase +
		"origin: outsider\nrequester: joe/po-claims\nrequest: bd-9\nlinear: ENG-12\n" +
		"pr: https://github.com/o/r/pull/1\npr: https://github.com/o/r/pull/2\n",
		Notes: "<<<ateam-ask\ndecision: Approve plan: x\nrecommendation: y\nalternative: z\ncontext: https://dispatcher.example/pages/abc\n>>>"}
	got, err := runPRContext(t, issue, &prContextKong{MergeOrder: "o/r#1,o/r#2"})
	if err != nil {
		t.Fatal(err)
	}
	want1 := `<!-- a-team-context -->
Part 1 of 2 for the request on Linear ENG-12 (asked by joe/po-claims).
This PR: o/r#1. The other PR (#2): o/r#2.
Merge order: o/r#1 then o/r#2
Plan: https://dispatcher.example/pages/abc
<!-- /a-team-context -->
`
	want2 := strings.NewReplacer("Part 1", "Part 2", "This PR: o/r#1. The other PR (#2): o/r#2.", "This PR: o/r#2. The other PR (#1): o/r#1.").Replace(want1)
	if got != want1+"\n"+want2 {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want1+"\n"+want2)
	}

	one, err := runPRContext(t, issue, &prContextKong{PR: "https://github.com/o/r/pull/2", MergeOrder: "o/r#1,o/r#2"})
	if err != nil || one != want2 {
		t.Fatalf("--pr: err=%v got:\n%s", err, one)
	}
	if _, err := runPRContext(t, issue, &prContextKong{PR: "https://github.com/o/r/pull/9"}); err == nil {
		t.Fatal("expected error for a PR not on the initiative")
	}
}

func TestPRContext_NoPRsIsError(t *testing.T) {
	_, err := runPRContext(t, bd.Issue{ID: "at-x", Description: prCtxBase}, &prContextKong{})
	if err == nil || !strings.Contains(err.Error(), "no recorded PR") {
		t.Fatalf("err = %v", err)
	}
}

func TestPRContext_NonOutsiderSinglePR(t *testing.T) {
	issue := bd.Issue{ID: "at-x", Description: prCtxBase + "pr: https://github.com/o/r/pull/7\n"}
	got, err := runPRContext(t, issue, &prContextKong{})
	if err != nil {
		t.Fatal(err)
	}
	want := "<!-- a-team-context -->\nPart 1 of 1.\nThis PR: o/r#7.\n<!-- /a-team-context -->\n"
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestPRContext_RequestBeadIDWithoutLinear(t *testing.T) {
	issue := bd.Issue{ID: "at-x", Description: prCtxBase + "origin: outsider\nrequester: joe/po-a\nrequest: bd-9\npr: https://github.com/o/r/pull/7\n"}
	got, err := runPRContext(t, issue, &prContextKong{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "for the request bd-9 (asked by joe/po-a).") || strings.Contains(got, "Plan:") || strings.Contains(got, "Merge order") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestPRContext_LatestPlanURLWins(t *testing.T) {
	notes := "<<<ateam-ask\ndecision: Approve plan: a\nrecommendation: r\nalternative: a\ncontext: https://p/1\n>>>\n" +
		"<<<ateam-ask\ndecision: Scope?\nrecommendation: r\nalternative: a\ncontext: https://not/plan\n>>>\n" +
		"<<<ateam-ask\ndecision: Approve the plan again\nrecommendation: r\nalternative: a\ncontext: https://p/2 more\n>>>"
	if got := latestPlanURL(notes); got != "https://p/2" {
		t.Fatalf("got %q", got)
	}
	if got := latestPlanURL(""); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestPRContext_NoMergeOrderLineUnlessGiven(t *testing.T) {
	issue := bd.Issue{ID: "at-x", Description: prCtxBase + "pr: https://github.com/o/r/pull/1\npr: https://github.com/o/r/pull/2\n"}
	got, err := runPRContext(t, issue, &prContextKong{})
	if err != nil || strings.Contains(got, "Merge order") {
		t.Fatalf("err=%v got:\n%s", err, got)
	}
	for _, bad := range []string{"o/r#1,o/r#9", "o/r#1,o/r#1", "https://github.com/o/r/pull/1"} {
		if _, err := runPRContext(t, issue, &prContextKong{MergeOrder: bad}); err == nil {
			t.Fatalf("expected usage error for --merge-order %q", bad)
		}
	}
}

func TestPRContext_ThreePRsOtherPRsSentence(t *testing.T) {
	issue := bd.Issue{ID: "at-x", Description: prCtxBase +
		"pr: https://github.com/a/x/pull/1\npr: https://github.com/b/y/pull/2\npr: https://github.com/c/z/pull/3\n"}
	got, err := runPRContext(t, issue, &prContextKong{PR: "https://github.com/b/y/pull/2", MergeOrder: "c/z#3,a/x#1,b/y#2"})
	if err != nil {
		t.Fatal(err)
	}
	want := "<!-- a-team-context -->\nPart 2 of 3.\nThis PR: b/y#2. The other PRs (#1, #3): a/x#1; c/z#3.\nMerge order: c/z#3 then a/x#1 then b/y#2\n<!-- /a-team-context -->\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPRContext_ReviewerNamedAndNoReviewerFallback(t *testing.T) {
	base := prCtxBase + "origin: outsider\nrequester: joe/po\nrequest: bd-9\npr: https://github.com/o/r/pull/1\n"
	got, err := runPRContext(t, bd.Issue{ID: "at-x", Description: base + "reviewer: some-user\n"}, &prContextKong{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Review: @some-user is requested. PO value checks are product input, never approvals.\n") {
		t.Errorf("reviewer block:\n%s", got)
	}
	got, err = runPRContext(t, bd.Issue{ID: "at-x", Description: base}, &prContextKong{})
	if err != nil || strings.Contains(got, "Review:") {
		t.Errorf("no-reviewer block: err=%v\n%s", err, got)
	}
	got, _ = runPRContext(t, bd.Issue{ID: "at-x", Description: prCtxBase + "pr: https://github.com/o/r/pull/1\n"}, &prContextKong{})
	if strings.Contains(got, "Review:") {
		t.Errorf("non-outsider block has a Review line:\n%s", got)
	}
}
