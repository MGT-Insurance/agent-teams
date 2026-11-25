package verbs

// No-PO safety regression test (agent-teams-9yj.13).
//
// A DRI that runs outside any product owner (PO) must see exactly what it saw
// before POs existed: no extra gates, fields or notifications, and the same
// bytes from every existing ateam command. This file pins today's outputs
// with no PO state present, as literals, so a later change to shared code
// (gate mail body, ledger keys or categories, routing, startup text, the
// top-level help) that leaks PO behaviour into the no-PO path fails here.
//
// The one allowed difference is the new "po" command group in help listings.
// Every test name starts with TestNoPO so `go test -run NoPO` selects them.

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// noPOKeyMarkers are the hosted-store key prefixes the PO contract reserves
// (internal/po/doc.go section 3). No non-po verb may read or write them.
var noPOKeyMarkers = []string{"po:", "poledger."}

func noPOHasMarker(s string) bool {
	for _, m := range noPOKeyMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// noPORecordingBD wraps a cli.BDRunner and records every bd argument list so a
// test can assert no po:* key was read or written.
type noPORecordingBD struct {
	inner cli.BDRunner
	calls [][]string
}

func (r *noPORecordingBD) Run(args ...string) (string, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	return r.inner.Run(args...)
}

func (r *noPORecordingBD) RunJSON(dst any, args ...string) error {
	r.calls = append(r.calls, append([]string(nil), args...))
	return r.inner.RunJSON(dst, args...)
}

func (r *noPORecordingBD) assertNoPOKeys(t *testing.T) {
	t.Helper()
	for _, c := range r.calls {
		if noPOHasMarker(strings.Join(c, " ")) {
			t.Errorf("non-po verb touched a PO key: bd %v", c)
		}
	}
}

// noPOAssertStoreClean fails when the in-memory hosted store holds, or was
// asked to write, any PO key.
func noPOAssertStoreClean(t *testing.T, m *memBD) {
	t.Helper()
	for k := range m.mem {
		if noPOHasMarker(k) {
			t.Errorf("hosted store holds a PO key %q", k)
		}
	}
	for _, r := range m.remember {
		if noPOHasMarker(strings.Join(r, " ")) {
			t.Errorf("hosted store was asked to write a PO key: %v", r)
		}
	}
}

// ── ateam gate ───────────────────────────────────────────────────────────────

func TestNoPO_GateLocalAndDispatcherRun(t *testing.T) {
	const wantNotes = "<<<ateam-ask\ndecision: Ship it?\nrecommendation: Yes\nalternative: No\n>>>"
	wantUpdate := []string{"update", "at-9", "--append-notes", wantNotes, "--add-label", "human", "--add-label", "gate:review"}
	// Today's exact outbox line: field order op,type,title,body; body is the
	// newGateMailOp text. Any added field or header line changes these bytes.
	const wantOutbox = `{"op":"file","type":"mail","title":"at-9: review gate","body":"initiative: at-9\nkind: review\n---\nShip it?\n\nRecommended: Yes\nAlternative: No"}` + "\n"

	for _, inRun := range []bool{false, true} {
		name := "local"
		if inRun {
			name = "dispatcher-run"
		}
		t.Run(name, func(t *testing.T) {
			sup := t.TempDir()
			t.Setenv("DSP_SUP", sup)
			t.Setenv("DSP_BEAD", "dsp-1")
			if !inRun {
				os.Unsetenv("DSP_BEAD")
			}
			ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
			g := &gateKong{ID: "at-9", Kind: "review", Decision: "Ship it?", Recommendation: "Yes", Alternative: "No"}
			if err := g.Run(ctx); err != nil {
				t.Fatalf("gate: %v", err)
			}
			if got := stdoutOf(ctx); got != "ok\n" {
				t.Errorf("stdout = %q", got)
			}
			if got := ctx.Stderr.(*bytes.Buffer).String(); got != "" {
				t.Errorf("stderr = %q, want empty", got)
			}
			// rm8 T2: inside a run, one read-only `bd show` follows the gate
			// update so an outsider gate can be told apart; the update itself
			// is unchanged.
			wantCalls := 1
			if inRun {
				wantCalls = 2
			}
			if len(*calls) != wantCalls || !reflect.DeepEqual((*calls)[0].args, wantUpdate) {
				t.Errorf("bd calls = %v, want %v first of %d", *calls, wantUpdate, wantCalls)
			}
			if inRun && !reflect.DeepEqual((*calls)[1].args, []string{"show", "at-9", "--json"}) {
				t.Errorf("second call = %v, want the read-only show", (*calls)[1].args)
			}
			for _, c := range *calls {
				if noPOHasMarker(strings.Join(c.args, " ")) {
					t.Errorf("gate touched a PO key: %v", c.args)
				}
			}
			data, err := os.ReadFile(filepath.Join(sup, "outbox.jsonl"))
			if inRun {
				if err != nil || string(data) != wantOutbox {
					t.Errorf("outbox = %q (err %v), want %q", data, err, wantOutbox)
				}
			} else if !os.IsNotExist(err) {
				t.Errorf("outbox.jsonl must not exist outside a run (err %v)", err)
			}
		})
	}
}

// TestNoPO_GateMailBodyIsFrozen pins newGateMailOp itself: the struct shape on
// the wire and the body text, for every gate kind.
func TestNoPO_GateMailBodyIsFrozen(t *testing.T) {
	for _, kind := range []string{"question", "review", "live-test-review"} {
		op := newGateMailOp("at-1", kind, "the ask")
		raw, err := json.Marshal(op)
		if err != nil {
			t.Fatal(err)
		}
		want := `{"op":"file","type":"mail","title":"at-1: ` + kind + ` gate","body":"initiative: at-1\nkind: ` + kind + `\n---\nthe ask"}`
		if string(raw) != want {
			t.Errorf("%s: mail op = %s\nwant %s", kind, raw, want)
		}
	}
	// Exactly four wire fields: adding one (for example a po: header) fails.
	var fields []string
	rt := reflect.TypeOf(outboxMailOp{})
	for i := 0; i < rt.NumField(); i++ {
		fields = append(fields, rt.Field(i).Tag.Get("json"))
	}
	if want := []string{"op", "type", "title", "body"}; !reflect.DeepEqual(fields, want) {
		t.Errorf("outboxMailOp json fields = %v, want %v", fields, want)
	}
}

// ── ateam pr add and the register-pr-on-open.sh hook ─────────────────────────

func TestNoPO_PrAddOutputAndOutbox(t *testing.T) {
	sup := t.TempDir()
	t.Setenv("DSP_BEAD", "dsp-test")
	t.Setenv("DSP_SUP", sup)
	f, updates := prAddFake(t, "repo: /r\n")
	rec := &noPORecordingBD{inner: f}
	ctx, stdout, stderr := makeCtx(rec, t.TempDir())
	if err := (&prAddKong{InitiativeID: "at-x", URL: dispatcherPR12}).Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "pr add: recorded " + dispatcherPR12 + " on at-x\npr add: labeled Dispatcher run dsp-test\n"
	if stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if *updates != 1 {
		t.Errorf("bd update calls = %d, want 1", *updates)
	}
	if got := readOutbox(t, sup); got != dispatcherPR12Line {
		t.Errorf("outbox = %q, want %q", got, dispatcherPR12Line)
	}
	rec.assertNoPOKeys(t)
}

// TestNoPO_RegisterPROnOpenHook runs the real hook script with a fake ateam and
// pins the exact ateam calls and the hook's additionalContext output.
func TestNoPO_RegisterPROnOpenHook(t *testing.T) {
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	script, err := filepath.Abs("../../plugins/agent-teams/hooks/scripts/register-pr-on-open.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("hook script missing: %v", err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "plugin", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(root, "ws")
	wt := filepath.Join(root, "wt")
	for _, d := range []string{filepath.Join(ws, ".beads"), wt} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	callLog := filepath.Join(root, "calls.log")
	fake := "#!/usr/bin/env bash\necho \"$@\" >> \"$CALL_LOG\"\n" +
		"if [ \"$1\" = resolve-initiative ]; then echo at-stub01; fi\n" +
		"if [ \"$1\" = pr ] && [ \"$2\" = add ]; then echo \"pr add: recorded $4 on $3\"; fi\n"
	if err := os.WriteFile(filepath.Join(root, "plugin", "bin", "ateam"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	const prURL = "https://github.com/mgt-insurance/midgard/pull/5233"
	payload, _ := json.Marshal(map[string]any{
		"session_id":    "sess-1",
		"tool_name":     "Bash",
		"tool_input":    map[string]string{"command": "gh pr create --fill"},
		"tool_response": map[string]string{"stdout": "Creating pull request...\n" + prURL, "stderr": ""},
		"cwd":           wt,
	})
	cmd := exec.Command("bash", script)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(os.Environ(),
		"CLAUDE_PLUGIN_ROOT="+filepath.Join(root, "plugin"),
		"AGENT_TEAMS_HOME="+ws,
		"HOME="+root,
		"CALL_LOG="+callLog,
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("hook: %v", err)
	}
	logged, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := "resolve-initiative " + wt + " --session-id sess-1\npr add at-stub01 " + prURL + "\n"
	if string(logged) != wantCalls {
		t.Errorf("ateam calls = %q, want %q", logged, wantCalls)
	}
	wantOut := `{"additionalContext":"Recorded ` + prURL + ` on initiative at-stub01's pr rail (pr-shepherd can route its events). No need to run ` + "`ateam pr add`" + ` for it."}`
	var got, want map[string]string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("hook output is not JSON: %q", out)
	}
	_ = json.Unmarshal([]byte(wantOut), &want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hook output = %v, want %v", got, want)
	}
	if noPOHasMarker(string(logged)) {
		t.Errorf("hook touched a PO key: %s", logged)
	}
}

// ── ateam route-pr-event ─────────────────────────────────────────────────────

func TestNoPO_RoutePREventDecisions(t *testing.T) {
	t.Run("owned PR routes by mail send, nothing else", func(t *testing.T) {
		bodyFile := writeTempFile(t, "CI failed output")
		issue := prFieldIssue(t, "at-abc.1", "owner/myrepo", 42)
		ctx, stdout, stderr := makeRouteCtx([]bd.Issue{issue})
		fr := &fakeRunner{}
		cmd := &routePREventKong{Repo: "owner/myrepo", PRNumber: 42, HeadBranch: "feat-x", Transition: TransitionCIFailed, BodyFile: bodyFile, runner: fr.run}
		if err := cmd.Run(ctx); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if len(fr.calls) != 1 {
			t.Fatalf("runner calls = %v, want exactly one mail send", fr.calls)
		}
		call := fr.calls[0]
		if len(call) < 3 || call[0] != "mail" || call[1] != "send" || call[2] != "at-abc.1" {
			t.Errorf("call = %v, want mail send at-abc.1 ...", call)
		}
		if got := strings.Join(call[3:], " "); !strings.Contains(got, "--sender pr-shepherd") || noPOHasMarker(got) {
			t.Errorf("call args = %q", got)
		}
		if stderr.Len() != 0 {
			t.Errorf("stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout.String(), "at-abc.1") || noPOHasMarker(stdout.String()) {
			t.Errorf("stdout = %q", stdout)
		}
	})

	t.Run("unowned non-review event is dropped with today's message", func(t *testing.T) {
		bodyFile := writeTempFile(t, "CI failed output")
		ctx, stdout, _ := makeRouteCtx(nil)
		fr := &fakeRunner{}
		cmd := &routePREventKong{Repo: "owner/myrepo", PRNumber: 7, HeadBranch: "feat-y", Transition: TransitionCIFailed, BodyFile: bodyFile, runner: fr.run}
		if err := cmd.Run(ctx); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if len(fr.calls) != 0 {
			t.Errorf("runner calls = %v, want none", fr.calls)
		}
		want := "route-pr-event: unowned ci_failed for owner/myrepo#7 — no owning initiative; skipping\n"
		if stdout.String() != want {
			t.Errorf("stdout = %q, want %q", stdout.String(), want)
		}
	})

	t.Run("review_requested with no review-spawn config is skipped", func(t *testing.T) {
		bodyFile := writeTempFile(t, "please review")
		ctx, stdout, _, home := makeRouteCtxWithHome(t, nil)
		fr := &fakeRunner{}
		cmd := &routePREventKong{Repo: "owner/myrepo", PRNumber: 8, HeadBranch: "feat-z", Transition: TransitionReviewRequested, BodyFile: bodyFile, runner: fr.run}
		if err := cmd.Run(ctx); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if len(fr.calls) != 0 {
			t.Errorf("runner calls = %v, want none (home %s)", fr.calls, home)
		}
		if !strings.Contains(stdout.String(), "route-pr-event: review-spawn not configured for owner/myrepo") || noPOHasMarker(stdout.String()) {
			t.Errorf("stdout = %q", stdout)
		}
	})
}

// ── ateam steward ledger (no --persona) ──────────────────────────────────────

var noPOTimestamp = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`)

func TestNoPO_StewardLedgerDefaultKeysCategoriesAndOutput(t *testing.T) {
	m := newMemBD()
	ctx, out, errOut := hostedLedgerCtx(t, m)
	cats := []string{"plan-approval", "scope-call", "merge-approval", "design-fork", "unblock-action"}
	for i, c := range cats {
		recordLedger(t, ctx, c, "at-"+string(rune('1'+i)), "accepted", "")
		time.Sleep(2 * time.Millisecond) // keys are nanosecond timestamps
	}
	recordLedger(t, ctx, "merge-approval", "at-9", "corrected", "hold the merge")

	wantRecorded := "recorded: plan-approval at-1 -> accepted\n" +
		"recorded: scope-call at-2 -> accepted\n" +
		"recorded: merge-approval at-3 -> accepted\n" +
		"recorded: design-fork at-4 -> accepted\n" +
		"recorded: unblock-action at-5 -> accepted\n" +
		"recorded: merge-approval at-9 -> corrected\n"
	if out.String() != wantRecorded || errOut.Len() != 0 {
		t.Errorf("record output = %q / %q, want %q", out, errOut, wantRecorded)
	}

	// Every row: the default prefix, no PO prefix, and the same record fields.
	keyRe := regexp.MustCompile(`^stewardledger\.\d{19}-[0-9a-f]{4}$`)
	if len(m.remember) != 6 || len(m.mem) != 6 {
		t.Fatalf("hosted store: %d writes, %d rows, want 6 and 6", len(m.remember), len(m.mem))
	}
	for _, r := range m.remember {
		key := strings.TrimPrefix(r[1], "--key=")
		if !keyRe.MatchString(key) {
			t.Errorf("ledger key %q is not stewardledger.<19 digits>-<4 hex>", key)
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(r[2]), &rec); err != nil {
			t.Fatalf("value %q: %v", r[2], err)
		}
		for k := range rec {
			switch k {
			case "ts", "category", "initiative", "recommendation", "verdict", "decision":
			default:
				t.Errorf("record has field %q beyond today's set", k)
			}
		}
	}
	noPOAssertStoreClean(t, m)

	out.Reset()
	if err := (&stewardLedgerStatsKong{}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	wantStats := "CATEGORY        TOTAL  ACCEPTED  CORRECTED  ACCEPT RATE\n" +
		"--------        -----  --------  ---------  -----------\n" +
		"plan-approval   1      1         0          100%\n" +
		"scope-call      1      1         0          100%\n" +
		"merge-approval  2      1         1          50%\n" +
		"design-fork     1      1         0          100%\n" +
		"unblock-action  1      1         0          100%\n"
	if out.String() != wantStats {
		t.Errorf("stats = %q, want %q", out, wantStats)
	}

	out.Reset()
	if err := (&stewardLedgerRecallKong{Category: "merge-approval", Limit: 10}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	wantRecall := "<ts>  merge-approval  initiative=at-9  verdict=corrected\n  recommendation: rec at-9\n  decision: hold the merge\n\n" +
		"<ts>  merge-approval  initiative=at-3  verdict=accepted\n  recommendation: rec at-3\n\n"
	if got := noPOTimestamp.ReplaceAllString(out.String(), "<ts>"); got != wantRecall {
		t.Errorf("recall = %q, want %q", got, wantRecall)
	}

	// The file-backed store (no hosted workspace) is the same verb output and
	// never calls bd.
	fctx, fout, _ := makeCtx(noBDCalls{t}, t.TempDir())
	recordLedger(t, fctx, "scope-call", "at-1", "accepted", "")
	if fout.String() != "recorded: scope-call at-1 -> accepted\n" {
		t.Errorf("file record output = %q", fout)
	}
	if _, err := os.Stat(StewardLedgerPath(fctx)); err != nil {
		t.Errorf("file ledger not written at the default path: %v", err)
	}
}

// TestNoPO_StewardLedgerDefaultCategoryEnum pins the record verb's default flag set and
// category list: a PO persona or category must not widen the no-persona path.
func TestNoPO_StewardLedgerDefaultCategoryEnum(t *testing.T) {
	f, ok := reflect.TypeOf(stewardLedgerRecordKong{}).FieldByName("Category")
	if !ok {
		t.Fatal("no Category field")
	}
	// The five steward categories stay first and unchanged; PO categories are only appended
	// (kong must list them) and are refused under the default steward persona
	// (see steward_ledger_persona_test.go).
	const stewardFive = "plan-approval,scope-call,merge-approval,design-fork,unblock-action"
	if got := f.Tag.Get("enum"); got != stewardFive && !strings.HasPrefix(got, stewardFive+",") {
		t.Errorf("category enum = %q", got)
	}
}

// ── ateam steward startup ────────────────────────────────────────────────────

func TestNoPO_StewardStartupOutput(t *testing.T) {
	m := newMemBD()
	m.mem["steward:hot:ledger-rule"] = "RULE: keep it whole."
	ctx, _, _ := hostedLedgerCtx(t, m)
	recordLedger(t, ctx, "merge-approval", "at-1", "accepted", "")

	var out bytes.Buffer
	sctx, _, _ := hostedLedgerCtx(t, m)
	sctx.Stdout = &out
	if err := (&stewardStartupKong{}).Run(sctx); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	wantTail := "\n## Ledger stats\n" +
		"CATEGORY        TOTAL  ACCEPTED  CORRECTED  ACCEPT RATE\n" +
		"--------        -----  --------  ---------  -----------\n" +
		"merge-approval  1      1         0          100%\n" +
		"\n## Steward learnings\n" +
		"[learnings steward: 1 entries, 45 chars, hot 1 fresh 0 — read in full; do NOT pipe through head/tail or truncate; output ends at the matching trailer line]\n" +
		"steward:hot:ledger-rule\nRULE: keep it whole.\n" +
		"[learnings steward: 1 entries, 45 chars, hot 1 fresh 0]\n"
	wantHead := "## Ledger rules\n" + stewardRulesIntro + "\n\n" + stewardSharedLedgerRules +
		"\n## Outsider rules\n" + stewardOutsiderRules
	if got != wantHead+wantTail {
		t.Errorf("startup output differs.\n got tail: %q\nwant tail: %q", got[max(0, len(got)-400):], wantTail)
	}
	if strings.Contains(got, "poledger") || strings.Contains(got, "## PO") || strings.Contains(strings.ReplaceAll(got, "po-question", ""), "po-") {
		// "po-question" is the frozen rm8 ledger category (docs/outsider-requests.md
		// section 8), not a PO-ledger leak.
		t.Errorf("startup output mentions POs")
	}
	noPOAssertStoreClean(t, m)
}

// ── top-level help ───────────────────────────────────────────────────────────

// TestNoPO_TopLevelHelpMatchesGolden compares `ateam --help` with a golden
// file after removing the new "po" command group. The golden file is today's
// help; any other added, removed or reworded top-level entry fails.
func TestNoPO_TopLevelHelpMatchesGolden(t *testing.T) {
	var buf bytes.Buffer
	p, err := cli.NewParser(kong.Exit(func(int) {}), kong.Writers(&buf, &buf))
	if err != nil {
		t.Fatal(err)
	}
	RegisterAllKong(p)
	// --help prints and exits via the no-op Exit; Parse then reports a usage
	// error, which is expected and ignored here.
	_, _ = p.Parse([]string{"--help"})
	got := noPOStripPoGroup(buf.String())
	wantBytes, err := os.ReadFile(filepath.Join("testdata", "nopo_help_toplevel.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(wantBytes) {
		t.Errorf("top-level --help changed beyond the po group.\n--- got ---\n%s\n--- want ---\n%s", got, wantBytes)
	}
}

// noPOStripPoGroup drops every help entry whose command path starts with "po"
// (an entry is its "  po ..." line plus the indented lines and blank line
// that follow).
func noPOStripPoGroup(help string) string {
	var out []string
	skipping := false
	for _, l := range strings.Split(help, "\n") {
		if strings.HasPrefix(l, "  ") && !strings.HasPrefix(l, "    ") {
			skipping = l == "  po" || strings.HasPrefix(l, "  po ")
		}
		if !skipping {
			out = append(out, l)
		} else if l == "" {
			skipping = false
		}
	}
	return strings.Join(out, "\n")
}
