package verbs_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
	"github.com/mgt-insurance/agent-teams/internal/verbs"
)

// newCtx builds a cli.Context backed by a fake bd.Client that responds to
// subcommand calls via the provided map: key is the first arg passed to bd
// (the subcommand), value is the stdout bytes the fake returns.
func newCtx(t *testing.T, home string, responses map[string][]byte) (*cli.Context, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		if name != "bd" {
			t.Errorf("exec called with %q, want bd", name)
			return nil, nil, errors.New("unexpected binary")
		}
		// args is [-C, home, subcommand, ...]
		if len(args) < 3 {
			t.Errorf("expected at least 3 args, got %v", args)
			return nil, nil, errors.New("too few args")
		}
		sub := args[2] // subcommand after -C <home>
		resp, ok := responses[sub]
		if !ok {
			t.Errorf("unexpected subcommand %q (full args: %v)", sub, args)
			return nil, nil, errors.New("unexpected subcommand")
		}
		return resp, nil, nil
	}
	client := bd.NewClientWithExec(home, execFn)
	ctx := &cli.Context{
		Home:   home,
		BD:     client,
		Stdout: out,
		Stderr: &bytes.Buffer{},
	}
	return ctx, out
}

// captureArgs returns an ExecFunc that records every call's args slice.
func captureArgs(calls *[][]string) bd.ExecFunc {
	return func(name string, args ...string) ([]byte, []byte, error) {
		cp := make([]string, len(args))
		copy(cp, args)
		*calls = append(*calls, cp)
		return []byte("result\n"), nil, nil
	}
}

// runQ dispatches a query verb through a cli.Parser backed by RegisterQueryKong.
// tokens are appended after the verb name: runQ(t, "show", ctx, "at-id").
// Returns the error from kctx.Run.
func runQ(t *testing.T, verb string, ctx *cli.Context, tokens ...string) error {
	t.Helper()
	p, err := cli.NewParser(kong.Exit(func(int) {}))
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	verbs.RegisterQueryKong(p)
	args := append([]string{verb}, tokens...)
	kctx, parseErr := p.Parse(args)
	if parseErr != nil {
		return parseErr
	}
	kctx.Bind(ctx)
	return kctx.Run(ctx)
}

// ── ws ────────────────────────────────────────────────────────────────────────

func TestWsPrintsHome(t *testing.T) {
	out := &bytes.Buffer{}
	ctx := &cli.Context{
		Home:   "/my/workspace",
		Stdout: out,
		Stderr: &bytes.Buffer{},
	}
	if err := runQ(t, "ws", ctx); err != nil {
		t.Fatalf("ws.Run: %v", err)
	}
	if got := out.String(); got != "/my/workspace\n" {
		t.Errorf("ws output = %q, want %q", got, "/my/workspace\n")
	}
}

func TestWsNilCtxReturnsError(t *testing.T) {
	if err := runQ(t, "ws", nil); err == nil {
		t.Error("expected error for nil ctx, got nil")
	}
}

// ── list ──────────────────────────────────────────────────────────────────────

func TestListCallsBDArgs(t *testing.T) {
	var calls [][]string
	client := bd.NewClientWithExec("/ws", captureArgs(&calls))
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	if err := runQ(t, "list", ctx); err != nil {
		t.Fatalf("list.Run: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(calls))
	}
	wantArgs := []string{"-C", "/ws", "list", "--status=open"}
	for i, w := range wantArgs {
		if i >= len(calls[0]) || calls[0][i] != w {
			t.Errorf("bd args[%d] = %q, want %q (full: %v)", i, calls[0][i], w, calls[0])
		}
	}
}

func TestListWritesOutput(t *testing.T) {
	ctx, out := newCtx(t, "/ws", map[string][]byte{
		"list": []byte("● issue-1 · My Issue   [● P1 · OPEN]\n"),
	})
	if err := runQ(t, "list", ctx); err != nil {
		t.Fatalf("list.Run: %v", err)
	}
	if out.Len() == 0 {
		t.Error("list produced no output")
	}
}

// ── list-json ─────────────────────────────────────────────────────────────────

func TestListJSONCallsBDArgs(t *testing.T) {
	var calls [][]string
	issues := []bd.Issue{{ID: "at-abc", Title: "T", Status: "open", CreatedAt: "2026-06-01"}}
	raw, _ := json.Marshal(issues)
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		cp := make([]string, len(args))
		copy(cp, args)
		calls = append(calls, cp)
		return append(raw, '\n'), nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	if err := runQ(t, "list-json", ctx); err != nil {
		t.Fatalf("list-json.Run: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(calls))
	}
	wantArgs := []string{"-C", "/ws", "list", "--status=open", "--json"}
	for i, w := range wantArgs {
		if i >= len(calls[0]) || calls[0][i] != w {
			t.Errorf("bd args[%d] = %q, want %q (full: %v)", i, calls[0][i], w, calls[0])
		}
	}
}

func TestListJSONEmitsValidJSON(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-x1", Title: "Init", Status: "open", CreatedAt: "2026-06-01"},
		{ID: "at-x2", Title: "Impl", Status: "open", CreatedAt: "2026-06-02"},
	}
	raw, _ := json.Marshal(issues)
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return append(raw, '\n'), nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	if err := runQ(t, "list-json", ctx); err != nil {
		t.Fatalf("list-json.Run: %v", err)
	}

	var parsed []bd.Issue
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if len(parsed) != 2 {
		t.Errorf("parsed %d issues, want 2", len(parsed))
	}
}

// ── list-json: the "fields" object (agent-teams-ully.12) ─────────────────────

// listJSON runs the verb over a canned bd payload and returns the emitted
// elements, each still keyed by raw JSON so a test can compare bytes.
func listJSON(t *testing.T, bdOutput string, tokens ...string) []map[string]json.RawMessage {
	t.Helper()
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return []byte(bdOutput), nil, nil
	}
	out := &bytes.Buffer{}
	ctx := &cli.Context{
		Home:   "/ws",
		BD:     bd.NewClientWithExec("/ws", execFn),
		Stdout: out,
		Stderr: &bytes.Buffer{},
	}
	if err := runQ(t, "list-json", ctx, tokens...); err != nil {
		t.Fatalf("list-json.Run: %v", err)
	}
	var elements []map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &elements); err != nil {
		t.Fatalf("output is not a JSON array: %v\nraw: %s", err, out.String())
	}
	return elements
}

// compactJSON strips insignificant whitespace so two encodings of the same
// value compare equal. Needed because the verb re-indents the whole document:
// a nested value bd printed on one line comes back across several. It does NOT
// reorder keys, so comparing compacted forms still catches real corruption.
func compactJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatalf("json.Compact(%s): %v", raw, err)
	}
	return buf.String()
}

// listJSONErr runs the verb over a canned bd payload expecting failure.
func listJSONErr(t *testing.T, bdOutput string) error {
	t.Helper()
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return []byte(bdOutput), nil, nil
	}
	ctx := &cli.Context{
		Home:   "/ws",
		BD:     bd.NewClientWithExec("/ws", execFn),
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	}
	err := runQ(t, "list-json", ctx)
	if err == nil {
		t.Fatalf("list-json.Run succeeded on %q; want an error", bdOutput)
	}
	return err
}

// A real bd element, verbatim key set as captured from `bd list --status=open
// --json` (14 keys), plus one invented key standing in for a field a future bd
// release adds. The invented key is the point: the enrichment must not be a
// struct round-trip that quietly drops what this CLI does not model.
const realBdElement = `[
  {
    "id": "at-tvvr",
    "title": "PR titles should read for outside reviewers",
    "description": "problem: PR titles should read for outside reviewers\nrepo: /Users/erlloyd/Code/agent-teams\nworktree: /Users/erlloyd/.agent-teams-worktrees/pr-text\nbranch: pr-text\nteam: agent-teams-pr-text\nmode: bg\nepic: agent-teams-96bu\n\n## Prose\n\nRepo: ` + "`" + `/wrong/path` + "`" + `\n\nsession: fc4a12ba-05c7-406a-b003-2fee9771bdb5\n",
    "notes": "2026-07-29 executing\n",
    "status": "open",
    "priority": 2,
    "issue_type": "task",
    "owner": "Eric Lloyd",
    "created_at": "2026-07-29T10:00:00Z",
    "created_by": "Eric Lloyd",
    "updated_at": "2026-07-29T18:00:00Z",
    "labels": ["gate:review", "thread:722"],
    "dependency_count": 0,
    "dependent_count": 3,
    "comment_count": 1,
    "some_future_bd_field": {"nested": [1, 2, 3]}
  }
]`

// The enrichment is purely additive: every key bd emitted survives with its
// value intact, including one this CLI does not model at all. "Intact" means
// identical modulo JSON whitespace — the document is re-indented as a whole, so
// a nested value's line breaks move; nothing else about it can.
func TestListJSONPreservesEveryBdKey(t *testing.T) {
	var original []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(realBdElement), &original); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}
	got := listJSON(t, realBdElement)
	if len(got) != 1 {
		t.Fatalf("emitted %d elements, want 1", len(got))
	}
	for key, want := range original[0] {
		value, present := got[0][key]
		if !present {
			t.Errorf("key %q was dropped", key)
			continue
		}
		if got, expected := compactJSON(t, value), compactJSON(t, want); got != expected {
			t.Errorf("key %q = %s, want %s", key, got, expected)
		}
	}
	// Exactly three keys added: fields, prs, pr_reviews.
	if len(got[0]) != len(original[0])+3 {
		t.Errorf("emitted %d keys, want %d (the original set plus \"fields\", \"prs\", \"pr_reviews\")", len(got[0]), len(original[0])+3)
	}
	for _, key := range []string{"fields", "prs", "pr_reviews"} {
		if _, present := got[0][key]; !present {
			t.Errorf("no %q key was added", key)
		}
	}
	// realBdElement has no PR anywhere (rail, notes, or description) — both
	// resolved-PR-derived keys come back as empty arrays, never omitted/null.
	if got0Prs := compactJSON(t, got[0]["prs"]); got0Prs != "[]" {
		t.Errorf(`prs = %s, want "[]"`, got0Prs)
	}
	if got0Reviews := compactJSON(t, got[0]["pr_reviews"]); got0Reviews != "[]" {
		t.Errorf(`pr_reviews = %s, want "[]"`, got0Reviews)
	}
}

// The added object is exactly what internal/initiative produces for that
// issue — the verb adds no rule of its own. Includes the real poison shape:
// the "Repo: `/wrong/path`" prose line must not win.
func TestListJSONFieldsMatchTheComponent(t *testing.T) {
	got := listJSON(t, realBdElement)
	var issue bd.Issue
	var original []bd.Issue
	if err := json.Unmarshal([]byte(realBdElement), &original); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}
	issue = original[0]
	want, err := json.Marshal(initiative.JSONFields(issue))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if gotFields, wantFields := compactJSON(t, got[0]["fields"]), string(want); gotFields != wantFields {
		t.Errorf("fields = %s\nwant     %s", gotFields, wantFields)
	}

	var fields map[string]any
	if err := json.Unmarshal(got[0]["fields"], &fields); err != nil {
		t.Fatalf("fields is not a JSON object: %v", err)
	}
	if fields["repo"] != "/Users/erlloyd/Code/agent-teams" {
		t.Errorf("fields.repo = %#v, want the canonical header value, not the prose echo", fields["repo"])
	}
	// A session tie below the prose body is still picked up (frozen item 2).
	sessions, ok := fields["session"].([]any)
	if !ok || len(sessions) != 1 || sessions[0] != "fc4a12ba-05c7-406a-b003-2fee9771bdb5" {
		t.Errorf("fields.session = %#v, want the one tie appended below the prose", fields["session"])
	}
}

// An initiative with no field lines at all still gets an object, so a consumer
// can index into fields unconditionally.
func TestListJSONEmitsAnObjectWhenThereAreNoFieldLines(t *testing.T) {
	got := listJSON(t, `[{"id":"at-x","title":"T","description":"just prose, no fields\n"}]`)
	if string(got[0]["fields"]) != "{}" {
		t.Errorf("fields = %s, want {}", got[0]["fields"])
	}
}

// A missing description is not an error — bd omits the key on a bead with none.
func TestListJSONToleratesAMissingDescription(t *testing.T) {
	got := listJSON(t, `[{"id":"at-x","title":"T"}]`)
	if string(got[0]["fields"]) != "{}" {
		t.Errorf("fields = %s, want {}", got[0]["fields"])
	}
}

func TestListJSONEmptyArrayStaysAnEmptyArray(t *testing.T) {
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return []byte("[]\n"), nil, nil
	}
	out := &bytes.Buffer{}
	ctx := &cli.Context{
		Home:   "/ws",
		BD:     bd.NewClientWithExec("/ws", execFn),
		Stdout: out,
		Stderr: &bytes.Buffer{},
	}
	if err := runQ(t, "list-json", ctx); err != nil {
		t.Fatalf("list-json.Run: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "[]" {
		t.Errorf("output = %q, want %q", got, "[]")
	}
}

// bd returning "null" must still print an array — a consumer that maps over
// the result would break on null.
func TestListJSONNullBecomesAnEmptyArray(t *testing.T) {
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return []byte("null\n"), nil, nil
	}
	out := &bytes.Buffer{}
	ctx := &cli.Context{
		Home:   "/ws",
		BD:     bd.NewClientWithExec("/ws", execFn),
		Stdout: out,
		Stderr: &bytes.Buffer{},
	}
	if err := runQ(t, "list-json", ctx); err != nil {
		t.Fatalf("list-json.Run: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "[]" {
		t.Errorf("output = %q, want %q", got, "[]")
	}
}

// Not-an-array fails loudly instead of passing through unenriched: a consumer
// that asked for fields must never silently get output without them.
func TestListJSONFailsLoudlyOnANonArrayPayload(t *testing.T) {
	for _, payload := range []string{
		`{"id":"at-x"}`,
		`"a string"`,
		`this is not json at all`,
		``,
	} {
		err := listJSONErr(t, payload)
		if !strings.Contains(err.Error(), "did not return a JSON array") {
			t.Errorf("payload %q: error = %v, want a \"did not return a JSON array\" error", payload, err)
		}
	}
}

func TestListJSONFailsLoudlyOnANonObjectElement(t *testing.T) {
	err := listJSONErr(t, `["not an object"]`)
	if !strings.Contains(err.Error(), "not a JSON object") {
		t.Errorf("error = %v, want a \"not a JSON object\" error", err)
	}
}

// If bd ever emits its own "fields" key, adding ours would silently destroy
// real data. Refuse instead.
func TestListJSONRefusesToOverwriteAnExistingFieldsKey(t *testing.T) {
	err := listJSONErr(t, `[{"id":"at-x","title":"T","fields":{"bd":"owns this"}}]`)
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("error = %v, want a \"refusing to overwrite\" error", err)
	}
}

// ── list-json: "prs" / "pr_reviews" (docs/multi-pr-contract.md §2a, §5) ──────

// TestListJSONPRsFallsBackToNotesOnlyPR proves this verb's OWN wiring — not
// initiative.ResolvedPRs itself, which Track P's contract bead already
// mutation-tests — surfaces the resolved-PR fallback: an initiative with no
// "pr" rail line at all, only a "pr:" line in bd Notes (the shape the `dri`
// skill has written for every initiative until now, docs/multi-pr-
// contract.md §2a), must still appear in the "prs" sibling key. Without this
// wiring, list-json's own "prs" key would go empty for the 178/549
// initiatives whose PR lives only in Notes, even though ResolvedPRs itself
// resolves it correctly.
func TestListJSONPRsFallsBackToNotesOnlyPR(t *testing.T) {
	const prURL = "https://github.com/erlloyd/pr-shepherd/pull/3"
	bdOutput := `[{"id":"at-notes-only","title":"T","notes":"pr: ` + prURL + `\n","labels":["human","gate:review"]}]`

	got := listJSON(t, bdOutput)
	if len(got) != 1 {
		t.Fatalf("expected 1 element, got %d", len(got))
	}

	var prs []string
	if err := json.Unmarshal(got[0]["prs"], &prs); err != nil {
		t.Fatalf("prs is not a JSON array: %v", err)
	}
	if len(prs) != 1 || prs[0] != prURL {
		t.Errorf("prs = %v, want [%q]", prs, prURL)
	}

	// fields.pr stays absent — it is a verbatim rail projection, and this
	// fixture has no "pr" rail line at all (docs/multi-pr-contract.md §4).
	var fields map[string]any
	if err := json.Unmarshal(got[0]["fields"], &fields); err != nil {
		t.Fatalf("fields is not a JSON object: %v", err)
	}
	if _, present := fields["pr"]; present {
		t.Errorf("fields.pr = %v, want absent (rail-only; this fixture has no rail line)", fields["pr"])
	}

	// pr_reviews carries the same resolved PR, gate computed from labels —
	// single resolved PR, so the bare "gate:review" label attributes to it.
	var reviews []struct {
		PR   string `json:"pr"`
		Gate string `json:"gate"`
	}
	if err := json.Unmarshal(got[0]["pr_reviews"], &reviews); err != nil {
		t.Fatalf("pr_reviews is not a JSON array: %v", err)
	}
	if len(reviews) != 1 || reviews[0].PR != prURL || reviews[0].Gate != "review" {
		t.Errorf("pr_reviews = %+v, want [{%q review}]", reviews, prURL)
	}
}

// TestListJSONRefusesToOverwriteAnExistingPRsKey mirrors the "fields"
// collision guard for the new "prs" sibling key.
func TestListJSONRefusesToOverwriteAnExistingPRsKey(t *testing.T) {
	err := listJSONErr(t, `[{"id":"at-x","title":"T","prs":["already here"]}]`)
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("error = %v, want a \"refusing to overwrite\" error", err)
	}
}

// --status is what lets the dashboard's closed-initiative slice come through
// this verb instead of shelling bd against the global workspace directly.
func TestListJSONStatusFlagReachesBD(t *testing.T) {
	for _, tc := range []struct {
		tokens []string
		want   string
	}{
		{nil, "--status=open"},
		{[]string{"--status=closed"}, "--status=closed"},
		{[]string{"--status=all"}, "--status=all"},
	} {
		var calls [][]string
		execFn := func(name string, args ...string) ([]byte, []byte, error) {
			cp := make([]string, len(args))
			copy(cp, args)
			calls = append(calls, cp)
			return []byte("[]\n"), nil, nil
		}
		ctx := &cli.Context{
			Home:   "/ws",
			BD:     bd.NewClientWithExec("/ws", execFn),
			Stdout: &bytes.Buffer{},
			Stderr: &bytes.Buffer{},
		}
		if err := runQ(t, "list-json", ctx, tc.tokens...); err != nil {
			t.Fatalf("list-json.Run %v: %v", tc.tokens, err)
		}
		if len(calls) != 1 {
			t.Fatalf("tokens %v: expected 1 bd call, got %d", tc.tokens, len(calls))
		}
		want := []string{"-C", "/ws", "list", tc.want, "--json"}
		if len(calls[0]) != len(want) {
			t.Fatalf("tokens %v: bd args = %v, want %v", tc.tokens, calls[0], want)
		}
		for i, w := range want {
			if calls[0][i] != w {
				t.Errorf("tokens %v: bd args[%d] = %q, want %q (full: %v)", tc.tokens, i, calls[0][i], w, calls[0])
			}
		}
	}
}

func TestListJSONRejectsAnEmptyStatus(t *testing.T) {
	ctx := &cli.Context{
		Home:   "/ws",
		BD:     bd.NewClientWithExec("/ws", func(string, ...string) ([]byte, []byte, error) { return []byte("[]\n"), nil, nil }),
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	}
	if err := runQ(t, "list-json", ctx, "--status="); err == nil {
		t.Error("list-json --status= succeeded; want a usage error")
	}
}

// ── human-list ────────────────────────────────────────────────────────────────

func TestHumanListCallsBDArgs(t *testing.T) {
	var calls [][]string
	// captureArgs returns "result\n" which is not valid JSON; use a JSON stub instead.
	emptyJSON := []byte("[]\n")
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		cp := make([]string, len(args))
		copy(cp, args)
		calls = append(calls, cp)
		return emptyJSON, nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	if err := runQ(t, "human-list", ctx); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(calls))
	}
	wantArgs := []string{"-C", "/ws", "human", "list", "--json"}
	for i, w := range wantArgs {
		if i >= len(calls[0]) || calls[0][i] != w {
			t.Errorf("bd args[%d] = %q, want %q (full: %v)", i, calls[0][i], w, calls[0])
		}
	}
}

// newHumanListCtx builds a cli.Context whose bd fake returns the given issues
// as JSON for any "human" subcommand.
func newHumanListCtx(t *testing.T, issues []bd.Issue) (*cli.Context, *bytes.Buffer) {
	t.Helper()
	raw, err := json.Marshal(issues)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	raw = append(raw, '\n')
	out := &bytes.Buffer{}
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return raw, nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	ctx := &cli.Context{
		Home:   "/ws",
		BD:     client,
		Stdout: out,
		Stderr: &bytes.Buffer{},
	}
	return ctx, out
}

func TestHumanListReviewGate(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-r1", Title: "Ship feature", Labels: []string{"human", "gate:review"}, Notes: "PR https://github.com/org/repo/pull/42 ready for review"},
	}
	ctx, out := newHumanListCtx(t, issues)

	if err := runQ(t, "human-list", ctx); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "[REVIEW]") {
		t.Errorf("expected [REVIEW] in output, got: %q", got)
	}
	if !strings.Contains(got, "at-r1") {
		t.Errorf("expected id at-r1 in output, got: %q", got)
	}
	if !strings.Contains(got, "PR https://github.com/org/repo/pull/42 ready for review") {
		t.Errorf("expected note text in output, got: %q", got)
	}
}

func TestHumanListQuestionGate(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-q1", Title: "Which approach?", Labels: []string{"human", "gate:question"}, Notes: "Should we use approach A or B?"},
	}
	ctx, out := newHumanListCtx(t, issues)

	if err := runQ(t, "human-list", ctx); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "[QUESTION]") {
		t.Errorf("expected [QUESTION] in output, got: %q", got)
	}
	if !strings.Contains(got, "at-q1") {
		t.Errorf("expected id at-q1 in output, got: %q", got)
	}
}

// TestHumanListOmitsExternalReview confirms a handed-off initiative
// (external-review label present, agent-teams-p9dm.23) is skipped by
// human-list even though it still carries human + gate:review — it is no
// longer awaiting Eric. A plain review-gated initiative alongside it is
// still listed.
func TestHumanListOmitsExternalReview(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-handed", Title: "Handed off PR", Labels: []string{"human", "gate:review", "external-review"}, Notes: "PR ready"},
		{ID: "at-r1", Title: "Still awaiting Eric", Labels: []string{"human", "gate:review"}, Notes: "PR ready"},
	}
	ctx, out := newHumanListCtx(t, issues)

	if err := runQ(t, "human-list", ctx); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "at-handed") {
		t.Errorf("expected handed-off initiative to be omitted, got: %q", got)
	}
	if !strings.Contains(got, "at-r1") {
		t.Errorf("expected non-handed-off initiative to still be listed, got: %q", got)
	}
}

// TestHumanListAllHandedOffStillReportsEmpty guards the regression found when
// agent-teams-p9dm.23 landed: the empty check ran before the external-review
// filter, so when EVERY returned row was handed off, human-list printed no
// rows and no message — completely blank stdout.
//
// That is this feature's success case (Eric hands off every PR, then asks what
// needs him), and blank output there is indistinguishable from a crashed
// command or a broken bd. "Nothing needs you" must be stated, never inferred
// from absence.
func TestHumanListAllHandedOffStillReportsEmpty(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-h1", Title: "Handed off one", Labels: []string{"human", "gate:review", "external-review"}, Notes: "PR ready"},
		{ID: "at-h2", Title: "Handed off two", Labels: []string{"human", "gate:review", "external-review"}, Notes: "PR ready"},
	}
	ctx, out := newHumanListCtx(t, issues)

	if err := runQ(t, "human-list", ctx); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if strings.TrimSpace(got) == "" {
		t.Fatal("human-list printed nothing when every row was handed off; expected the no-beads message")
	}
	if !strings.Contains(got, "No human-needed beads found.") {
		t.Errorf("expected the no-beads message, got: %q", got)
	}
	for _, id := range []string{"at-h1", "at-h2"} {
		if strings.Contains(got, id) {
			t.Errorf("expected handed-off %s to be omitted, got: %q", id, got)
		}
	}
}

func TestHumanListBackwardCompatHumanOnly(t *testing.T) {
	// Pre-existing gated bead: only "human" label, no gate:* — must render as QUESTION.
	issues := []bd.Issue{
		{ID: "at-old1", Title: "Old gate bead", Labels: []string{"human"}, Notes: "Legacy question"},
	}
	ctx, out := newHumanListCtx(t, issues)

	if err := runQ(t, "human-list", ctx); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "[QUESTION]") {
		t.Errorf("expected [QUESTION] for backward-compat human-only bead, got: %q", got)
	}
	if strings.Contains(got, "[REVIEW]") {
		t.Errorf("backward-compat bead must not render as [REVIEW], got: %q", got)
	}
}

func TestHumanListEmptyNoteOmitsNoteLine(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-notnote", Title: "No note bead", Labels: []string{"human", "gate:review"}, Notes: ""},
	}
	ctx, out := newHumanListCtx(t, issues)

	if err := runQ(t, "human-list", ctx); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	// Should be exactly one line: the id/kind/title line.
	if len(lines) != 1 {
		t.Errorf("expected 1 line for bead with no note, got %d: %q", len(lines), got)
	}
	if !strings.Contains(got, "[REVIEW]") {
		t.Errorf("expected [REVIEW] in output, got: %q", got)
	}
}

// ── human-list: structured ask extraction ────────────────────────────────────

func TestHumanListStructuredAskRendered(t *testing.T) {
	// Notes contain a sentinel block — structured fields must appear, not raw Notes.
	notes := "<<<ateam-ask\ndecision: Use approach A\nrecommendation: A\nalternative: B\ncontext: Faster path\n>>>"
	issues := []bd.Issue{
		{ID: "at-s1", Title: "Pick approach", Labels: []string{"human", "gate:question"}, Notes: notes},
	}
	ctx, out := newHumanListCtx(t, issues)

	if err := runQ(t, "human-list", ctx); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "decision: Use approach A") {
		t.Errorf("expected decision field, got: %q", got)
	}
	if !strings.Contains(got, "recommendation: A") {
		t.Errorf("expected recommendation field, got: %q", got)
	}
	if !strings.Contains(got, "alternative: B") {
		t.Errorf("expected alternative field, got: %q", got)
	}
	if !strings.Contains(got, "context: Faster path") {
		t.Errorf("expected context field, got: %q", got)
	}
	// Raw Notes blob must NOT appear verbatim (sentinel markers must not leak).
	if strings.Contains(got, "<<<ateam-ask") {
		t.Errorf("sentinel open marker must not appear in output, got: %q", got)
	}
	if strings.Contains(got, ">>>") {
		t.Errorf("sentinel close marker must not appear in output, got: %q", got)
	}
}

func TestHumanListStructuredAskLastBlockWins(t *testing.T) {
	// Multiple blocks — only the last one should be rendered.
	notes := "<<<ateam-ask\ndecision: Old decision\nrecommendation: old-rec\nalternative: old-alt\n>>>\nsome notes\n<<<ateam-ask\ndecision: New decision\nrecommendation: new-rec\nalternative: new-alt\ncontext: updated\n>>>"
	issues := []bd.Issue{
		{ID: "at-s2", Title: "Multi-block", Labels: []string{"human", "gate:question"}, Notes: notes},
	}
	ctx, out := newHumanListCtx(t, issues)

	if err := runQ(t, "human-list", ctx); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "decision: New decision") {
		t.Errorf("expected last block's decision, got: %q", got)
	}
	if strings.Contains(got, "Old decision") {
		t.Errorf("earlier block must not appear, got: %q", got)
	}
	if !strings.Contains(got, "context: updated") {
		t.Errorf("expected last block's context, got: %q", got)
	}
}

func TestHumanListNoStructuredBlockFallsBackToRawNotes(t *testing.T) {
	// No sentinel block — raw Notes must appear unchanged.
	rawNote := "PR https://github.com/org/repo/pull/42 ready for review"
	issues := []bd.Issue{
		{ID: "at-s3", Title: "Review PR", Labels: []string{"human", "gate:review"}, Notes: rawNote},
	}
	ctx, out := newHumanListCtx(t, issues)

	if err := runQ(t, "human-list", ctx); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, rawNote) {
		t.Errorf("expected raw note text %q in output, got: %q", rawNote, got)
	}
}

func TestHumanListMalformedBlockFallsBackToRawNotes(t *testing.T) {
	// Block with missing closing sentinel — treated as no block, raw fallback.
	notes := "some preamble\n<<<ateam-ask\ndecision: incomplete block\n"
	issues := []bd.Issue{
		{ID: "at-s4", Title: "Malformed", Labels: []string{"human", "gate:question"}, Notes: notes},
	}
	ctx, out := newHumanListCtx(t, issues)

	if err := runQ(t, "human-list", ctx); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	// Raw Notes should appear (fallback), not structured rendering.
	if !strings.Contains(got, notes) {
		t.Errorf("expected raw notes fallback for malformed block, got: %q", got)
	}
}

func TestHumanListStructuredAskContextOmittedWhenEmpty(t *testing.T) {
	// Block without context field — context line must not appear in output.
	notes := "<<<ateam-ask\ndecision: Ship it\nrecommendation: yes\nalternative: wait\n>>>"
	issues := []bd.Issue{
		{ID: "at-s5", Title: "Ship decision", Labels: []string{"human", "gate:question"}, Notes: notes},
	}
	ctx, out := newHumanListCtx(t, issues)

	if err := runQ(t, "human-list", ctx); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "context:") {
		t.Errorf("context line must be omitted when empty, got: %q", got)
	}
	if !strings.Contains(got, "decision: Ship it") {
		t.Errorf("expected decision field, got: %q", got)
	}
}

// ── human-list: lastNoteBlock fallback ───────────────────────────────────────

func TestHumanListFallbackMultiBlockShowsOnlyLast(t *testing.T) {
	// Multi-block notes: only the last block should appear; earlier blocks absent.
	notes := "First entry: old context\n\nSecond entry: more history\n\nThird entry: current status"
	issues := []bd.Issue{
		{ID: "at-mb1", Title: "Multi-block bead", Labels: []string{"human", "gate:question"}, Notes: notes},
	}
	ctx, out := newHumanListCtx(t, issues)

	if err := runQ(t, "human-list", ctx); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Third entry: current status") {
		t.Errorf("expected last block in output, got: %q", got)
	}
	if strings.Contains(got, "First entry: old context") {
		t.Errorf("earlier block must not appear, got: %q", got)
	}
	if strings.Contains(got, "Second entry: more history") {
		t.Errorf("earlier block must not appear, got: %q", got)
	}
}

func TestHumanListFallbackLongBlockTruncated(t *testing.T) {
	// A last block longer than 10 lines: truncation indicator must appear,
	// and only the last 10 lines of the block are shown.
	var linesBuf strings.Builder
	for i := 1; i <= 15; i++ {
		fmt.Fprintf(&linesBuf, "line %d\n", i)
	}
	notes := strings.TrimRight(linesBuf.String(), "\n")
	issues := []bd.Issue{
		{ID: "at-long1", Title: "Long note bead", Labels: []string{"human", "gate:question"}, Notes: notes},
	}
	ctx, out := newHumanListCtx(t, issues)

	if err := runQ(t, "human-list", ctx); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "…older lines truncated") {
		t.Errorf("expected truncation indicator, got: %q", got)
	}
	// Last 10 lines (6-15) should appear.
	if !strings.Contains(got, "line 15") {
		t.Errorf("expected last line in output, got: %q", got)
	}
	if !strings.Contains(got, "line 6") {
		t.Errorf("expected line 6 (10th from end) in output, got: %q", got)
	}
	// First 5 lines should NOT appear.
	if strings.Contains(got, "line 1\n") {
		t.Errorf("truncated lines must not appear, got: %q", got)
	}
	if strings.Contains(got, "line 5\n") {
		t.Errorf("truncated lines must not appear, got: %q", got)
	}
}

func TestHumanListFallbackSingleBlockNoTruncation(t *testing.T) {
	// Single-block notes (no blank lines) rendered as-is when short enough.
	notes := "PR https://github.com/org/repo/pull/99 needs approval"
	issues := []bd.Issue{
		{ID: "at-sb1", Title: "Single block", Labels: []string{"human", "gate:review"}, Notes: notes},
	}
	ctx, out := newHumanListCtx(t, issues)

	if err := runQ(t, "human-list", ctx); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, notes) {
		t.Errorf("expected note text in output, got: %q", got)
	}
	if strings.Contains(got, "truncated") {
		t.Errorf("must not show truncation indicator for short note, got: %q", got)
	}
}

func TestHumanListStructuredAskPathUnchanged(t *testing.T) {
	// Structured ask present: renderAsk is used, lastNoteBlock fallback not taken.
	// The raw sentinel markers must not appear, and structured fields must.
	notes := "preamble note\n\n<<<ateam-ask\ndecision: Use X\nrecommendation: X is faster\nalternative: Y\ncontext: benchmarked\n>>>"
	issues := []bd.Issue{
		{ID: "at-struct1", Title: "Structured ask", Labels: []string{"human", "gate:question"}, Notes: notes},
	}
	ctx, out := newHumanListCtx(t, issues)

	if err := runQ(t, "human-list", ctx); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "decision: Use X") {
		t.Errorf("expected structured decision field, got: %q", got)
	}
	if !strings.Contains(got, "context: benchmarked") {
		t.Errorf("expected structured context field, got: %q", got)
	}
	// Fallback must not be taken — sentinel markers must not leak.
	if strings.Contains(got, "<<<ateam-ask") {
		t.Errorf("sentinel open must not appear in output, got: %q", got)
	}
	// The preamble note block must not appear (fallback not taken).
	if strings.Contains(got, "preamble note") {
		t.Errorf("preamble must not appear when structured ask is present, got: %q", got)
	}
}

// ── show ──────────────────────────────────────────────────────────────────────

func TestShowMissingIDReturnsUsageError(t *testing.T) {
	ctx, _ := newCtx(t, "/ws", nil)
	// show requires <id>; omitting it triggers Validate() → UsageError (exit 2).
	err := runQ(t, "show", ctx)
	if err == nil {
		t.Fatal("expected UsageError, got nil")
	}
	if cli.ExitCode(err) != 2 {
		t.Errorf("expected exit code 2, got %d (err: %v)", cli.ExitCode(err), err)
	}
}

func TestShowEmptyIDReturnsUsageError(t *testing.T) {
	ctx, _ := newCtx(t, "/ws", nil)
	// Passing empty string triggers Validate() → UsageError (exit 2).
	err := runQ(t, "show", ctx, "")
	if err == nil {
		t.Fatal("expected UsageError, got nil")
	}
	if cli.ExitCode(err) != 2 {
		t.Errorf("expected exit code 2, got %d", cli.ExitCode(err))
	}
}

func TestShowCallsBDArgs(t *testing.T) {
	var calls [][]string
	client := bd.NewClientWithExec("/ws", captureArgs(&calls))
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	if err := runQ(t, "show", ctx, "at-abc123"); err != nil {
		t.Fatalf("show.Run: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(calls))
	}
	wantArgs := []string{"-C", "/ws", "show", "at-abc123"}
	for i, w := range wantArgs {
		if i >= len(calls[0]) || calls[0][i] != w {
			t.Errorf("bd args[%d] = %q, want %q (full: %v)", i, calls[0][i], w, calls[0])
		}
	}
}

// ── learnings ─────────────────────────────────────────────────────────────────

func TestLearningsMissingRoleReturnsUsageError(t *testing.T) {
	ctx, _ := newCtx(t, "/ws", nil)
	// learnings requires <role>; omitting it triggers Validate() → UsageError (exit 2).
	err := runQ(t, "learnings", ctx)
	if err == nil {
		t.Fatal("expected UsageError, got nil")
	}
	if cli.ExitCode(err) != 2 {
		t.Errorf("expected exit code 2, got %d (err: %v)", cli.ExitCode(err), err)
	}
}

func TestLearningsCallsBDArgs(t *testing.T) {
	var calls [][]string
	// The new implementation calls `memories --json`, not `memories <role>`.
	// captureArgs returns "result\n" which is not valid JSON; use a JSON stub.
	emptyJSON := []byte("{}\n")
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		cp := make([]string, len(args))
		copy(cp, args)
		calls = append(calls, cp)
		return emptyJSON, nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	if err := runQ(t, "learnings", ctx, "planner"); err != nil {
		t.Fatalf("learnings.Run: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(calls))
	}
	wantArgs := []string{"-C", "/ws", "memories", "--json"}
	for i, w := range wantArgs {
		if i >= len(calls[0]) || calls[0][i] != w {
			t.Errorf("bd args[%d] = %q, want %q (full: %v)", i, calls[0][i], w, calls[0])
		}
	}
}

func TestLearningsWritesOutput(t *testing.T) {
	// The new implementation filters by role prefix and prints full bodies.
	memoriesJSON := []byte(`{"planner:foo":"first line\nsecond line","dri:bar":"should not appear"}` + "\n")
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return memoriesJSON, nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	if err := runQ(t, "learnings", ctx, "planner"); err != nil {
		t.Fatalf("learnings.Run: %v", err)
	}
	if out.Len() == 0 {
		t.Error("learnings produced no output")
	}
	got := out.String()
	if !strings.Contains(got, "planner:foo") {
		t.Errorf("expected planner:foo key in output; got: %q", got)
	}
	if strings.Contains(got, "dri:") {
		t.Errorf("cross-role dri: key must not appear in output; got: %q", got)
	}
}

// ── roles ─────────────────────────────────────────────────────────────────────

func TestRolesExcludesAppliedNamespace(t *testing.T) {
	// applied:<role>:<slug> is the applied-signal counter namespace, not a
	// role — it must never appear in `ateam roles` output (frozen contract
	// agent-teams-u71p.1). A real role's own memory (dri:hot:bar) must still
	// be listed.
	memoriesJSON := []byte(`{"applied:dri:foo":"{\"count\":1}","dri:hot:bar":"some learning"}` + "\n")
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return memoriesJSON, nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	if err := runQ(t, "roles", ctx); err != nil {
		t.Fatalf("roles.Run: %v", err)
	}

	got := out.String()
	lines := strings.Split(strings.TrimSpace(got), "\n")
	for _, l := range lines {
		if l == "applied" {
			t.Errorf("roles output must not include phantom \"applied\" role; got: %q", got)
		}
	}
	if !strings.Contains(got, "dri") {
		t.Errorf("expected real role \"dri\" in roles output; got: %q", got)
	}
}

// ── memories-json ─────────────────────────────────────────────────────────────

// wantMemoryEntry mirrors the memoryRecord json shape (unexported in package
// verbs) for decoding `memories-json` output in tests.
type wantMemoryEntry struct {
	Role         string  `json:"role"`
	Key          string  `json:"key"`
	Slug         string  `json:"slug"`
	Tier         string  `json:"tier"`
	Body         string  `json:"body"`
	AppliedCount int     `json:"appliedCount"`
	LastApplied  *string `json:"lastApplied"`
}

// decodeMemoriesJSON runs the memories-json verb and decodes its stdout.
func decodeMemoriesJSON(t *testing.T, out *bytes.Buffer) []wantMemoryEntry {
	t.Helper()
	var got []wantMemoryEntry
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode memories-json output: %v (raw: %s)", err, out.String())
	}
	return got
}

func TestMemoriesJSONTierDerivation(t *testing.T) {
	memoriesJSON := []byte(`{
		"dri:hot:verify-live": "hot body",
		"dri:fresh:new-thing": "fresh body",
		"dri:cold-thing": "cold body"
	}`)
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return memoriesJSON, nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	if err := runQ(t, "memories-json", ctx); err != nil {
		t.Fatalf("memories-json.Run: %v", err)
	}
	entries := decodeMemoriesJSON(t, out)

	byKey := make(map[string]wantMemoryEntry, len(entries))
	for _, e := range entries {
		byKey[e.Key] = e
	}

	if got := byKey["dri:hot:verify-live"]; got.Tier != "hot" || got.Slug != "verify-live" {
		t.Errorf("hot entry = %+v, want tier=hot slug=verify-live", got)
	}
	if got := byKey["dri:fresh:new-thing"]; got.Tier != "fresh" || got.Slug != "new-thing" {
		t.Errorf("fresh entry = %+v, want tier=fresh slug=new-thing", got)
	}
	if got := byKey["dri:cold-thing"]; got.Tier != "cold" || got.Slug != "cold-thing" {
		t.Errorf("cold entry = %+v, want tier=cold slug=cold-thing", got)
	}
}

func TestMemoriesJSONAppliedJoinPresentAndAbsent(t *testing.T) {
	memoriesJSON := []byte(`{
		"dri:hot:foo": "foo body",
		"dri:hot:bar": "bar body",
		"applied:dri:foo": "{\"count\":3,\"last_applied\":\"2026-01-01T00:00:00Z\"}"
	}`)
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return memoriesJSON, nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	if err := runQ(t, "memories-json", ctx); err != nil {
		t.Fatalf("memories-json.Run: %v", err)
	}
	entries := decodeMemoriesJSON(t, out)

	byKey := make(map[string]wantMemoryEntry, len(entries))
	for _, e := range entries {
		byKey[e.Key] = e
	}

	foo := byKey["dri:hot:foo"]
	if foo.AppliedCount != 3 {
		t.Errorf("foo.AppliedCount = %d, want 3", foo.AppliedCount)
	}
	if foo.LastApplied == nil || *foo.LastApplied != "2026-01-01T00:00:00Z" {
		t.Errorf("foo.LastApplied = %v, want 2026-01-01T00:00:00Z", foo.LastApplied)
	}

	bar := byKey["dri:hot:bar"]
	if bar.AppliedCount != 0 {
		t.Errorf("bar.AppliedCount = %d, want 0 (no applied record)", bar.AppliedCount)
	}
	if bar.LastApplied != nil {
		t.Errorf("bar.LastApplied = %v, want nil (no applied record)", *bar.LastApplied)
	}
}

func TestMemoriesJSONSkipsColonlessAppliedAndNonString(t *testing.T) {
	memoriesJSON := []byte(`{
		"schema_version": 1,
		"looseval": "colonless string value, must be skipped",
		"applied:dri:foo": "{\"count\":1,\"last_applied\":\"2026-01-01T00:00:00Z\"}",
		"dri:hot:num": 5,
		"dri:hot:foo": "real entry"
	}`)
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return memoriesJSON, nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	if err := runQ(t, "memories-json", ctx); err != nil {
		t.Fatalf("memories-json.Run: %v", err)
	}
	entries := decodeMemoriesJSON(t, out)

	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 entry (colonless/applied/non-string skipped), got %d: %+v", len(entries), entries)
	}
	if entries[0].Key != "dri:hot:foo" || entries[0].Role != "dri" {
		t.Errorf("unexpected surviving entry: %+v", entries[0])
	}
}

func TestMemoriesJSONSortOrderAscendingByKey(t *testing.T) {
	memoriesJSON := []byte(`{
		"planner:hot:mmm": "m body",
		"dri:hot:zzz": "z body",
		"dri:hot:aaa": "a body"
	}`)
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return memoriesJSON, nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	if err := runQ(t, "memories-json", ctx); err != nil {
		t.Fatalf("memories-json.Run: %v", err)
	}
	entries := decodeMemoriesJSON(t, out)

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	wantOrder := []string{"dri:hot:aaa", "dri:hot:zzz", "planner:hot:mmm"}
	for i, k := range wantOrder {
		if entries[i].Key != k {
			t.Errorf("entries[%d].Key = %q, want %q (full order: %v)", i, entries[i].Key, k, entries)
		}
	}
}

func TestMemoriesJSONEmptyEmitsEmptyArray(t *testing.T) {
	memoriesJSON := []byte(`{}`)
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return memoriesJSON, nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	if err := runQ(t, "memories-json", ctx); err != nil {
		t.Fatalf("memories-json.Run: %v", err)
	}
	got := strings.TrimSpace(out.String())
	if got != "[]" {
		t.Errorf("empty memories-json output = %q, want %q", got, "[]")
	}
}
