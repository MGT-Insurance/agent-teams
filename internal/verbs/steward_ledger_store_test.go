package verbs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// memBD is an in-memory bd memory store: remember, import, memories, and the
// scoped sql reads the verbs under test issue.
type memBD struct {
	mem       map[string]string
	err       error // returned by every call when set
	remember  [][]string
	imports   int
	importErr error // returned by import only, after the call is counted
}

func newMemBD() *memBD { return &memBD{mem: map[string]string{}} }

var likeClause = regexp.MustCompile("`key` LIKE 'kv\\.memory\\.([^']*)%'")

func (m *memBD) Run(args ...string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	switch args[0] {
	case "remember":
		m.remember = append(m.remember, args)
		key := strings.TrimPrefix(args[1], "--key=")
		m.mem[key] = args[2]
	case "import":
		m.imports++
		if m.importErr != nil {
			return "", m.importErr
		}
		data, err := os.ReadFile(args[1])
		if err != nil {
			return "", err
		}
		for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			var row seedMemoryLine
			if err := json.Unmarshal([]byte(l), &row); err != nil {
				return "", err
			}
			m.mem[row.Key] = row.Value
		}
	default:
		return "", fmt.Errorf("memBD: unexpected %v", args)
	}
	return "", nil
}

func (m *memBD) RunJSON(dst any, args ...string) error {
	if m.err != nil {
		return m.err
	}
	var out any
	if args[len(args)-1] == "--json" && args[len(args)-2] == "memories" {
		whole := map[string]any{}
		for k, v := range m.mem {
			whole[k] = v
		}
		out = whole
	} else if args[len(args)-3] == "sql" {
		query := args[len(args)-1]
		switch {
		case strings.Contains(query, "COUNT(*)"):
			out = []map[string]int{{"n": len(m.mem)}}
		case strings.Contains(query, "LENGTH"):
			var rows []map[string]any
			for k, v := range m.mem {
				if !strings.HasPrefix(k, "applied:") {
					rows = append(rows, map[string]any{"k": k, "n": len(v)})
				}
			}
			out = rows
		default:
			var prefixes []string
			for _, c := range likeClause.FindAllStringSubmatch(query, -1) {
				prefixes = append(prefixes, strings.ReplaceAll(strings.ReplaceAll(c[1], `\_`, "_"), `\%`, "%"))
			}
			rows := []map[string]string{}
			for k, v := range m.mem {
				for _, p := range prefixes {
					if strings.HasPrefix(k, p) {
						rows = append(rows, map[string]string{"k": k, "v": v})
						break
					}
				}
			}
			out = rows
		}
	} else {
		return fmt.Errorf("memBD: unexpected %v", args)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

func (m *memBD) ledgerKeys() []string {
	var keys []string
	for k := range m.mem {
		if strings.HasPrefix(k, stewardLedgerKeyPrefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// noBDCalls fails the test on any call: the file path must not touch bd.
type noBDCalls struct{ t *testing.T }

func (f noBDCalls) Run(args ...string) (string, error) {
	f.t.Helper()
	f.t.Fatalf("unexpected bd call %v", args)
	return "", nil
}

func (f noBDCalls) RunJSON(_ any, args ...string) error {
	f.t.Helper()
	f.t.Fatalf("unexpected bd call %v", args)
	return nil
}

// hostedLedgerCtx is a context whose home is a hosted workspace backed by m.
func hostedLedgerCtx(t *testing.T, m *memBD) (*cli.Context, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	return makeCtx(m, hostedHome(t, hostedMetadata))
}

// remoteLedgerCtx is a laptop context: a local home with cloud_home routing
// learnings (and so the ledger) to the hosted client m.
func remoteLedgerCtx(t *testing.T, m *memBD) (*cli.Context, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	ctx, out, errOut := makeCtx(noBDCalls{t}, t.TempDir())
	ctx.Learn = m
	return ctx, out, errOut
}

func recordLedger(t *testing.T, ctx *cli.Context, cat, init, verdict, decision string) {
	t.Helper()
	cmd := &stewardLedgerRecordKong{Category: cat, Initiative: init, Recommendation: "rec " + init, Verdict: verdict, Decision: decision}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("record: %v", err)
	}
}

func TestStewardLedgerHosted_RecordWritesOneRememberWithContractKey(t *testing.T) {
	m := newMemBD()
	ctx, _, _ := hostedLedgerCtx(t, m)
	recordLedger(t, ctx, "merge-approval", "at-1", "accepted", "")

	if len(m.remember) != 1 {
		t.Fatalf("want 1 remember, got %d", len(m.remember))
	}
	key := strings.TrimPrefix(m.remember[0][1], "--key=")
	value := m.remember[0][2]
	if !regexp.MustCompile(`^stewardledger\.\d{19}-[0-9a-f]{4}$`).MatchString(key) {
		t.Errorf("key %q does not match the contract", key)
	}
	if strings.Contains(key, ":") || strings.HasSuffix(value, "\n") {
		t.Errorf("key has a colon or value has a newline: %q %q", key, value)
	}
	rec, err := ParseStewardLedgerRecord([]byte(value))
	if err != nil || rec.Initiative != "at-1" {
		t.Fatalf("value is not the record JSON: %q (%v)", value, err)
	}
	if want := stewardLedgerKey(rec, value); key != want {
		t.Errorf("key %q, want %q", key, want)
	}
	if _, err := os.Stat(StewardLedgerPath(ctx)); err == nil {
		t.Error("hosted record also wrote the local file")
	}
}

func TestStewardLedgerHosted_StatsAndRecallMatchFilePath(t *testing.T) {
	type rec struct{ cat, init, verdict, decision string }
	recs := []rec{
		{"merge-approval", "at-1", "accepted", ""},
		{"merge-approval", "at-2", "corrected", "hold the merge"},
		{"scope-call", "at-3", "accepted", ""},
	}
	fileCtx, fileOut, _ := makeCtx(noBDCalls{t}, t.TempDir())
	m := newMemBD()
	hostCtx, hostOut, _ := hostedLedgerCtx(t, m)
	for _, r := range recs {
		recordLedger(t, fileCtx, r.cat, r.init, r.verdict, r.decision)
		recordLedger(t, hostCtx, r.cat, r.init, r.verdict, r.decision)
		time.Sleep(2 * time.Millisecond)
	}
	if got := len(m.ledgerKeys()); got != 3 {
		t.Fatalf("want 3 hosted rows, got %d", got)
	}

	// Timestamps differ between the two stores, so compare stats (no ts) and
	// recall with --json fields other than ts.
	fileOut.Reset()
	hostOut.Reset()
	if err := (&stewardLedgerStatsKong{}).Run(fileCtx); err != nil {
		t.Fatal(err)
	}
	if err := (&stewardLedgerStatsKong{}).Run(hostCtx); err != nil {
		t.Fatal(err)
	}
	if fileOut.String() != hostOut.String() {
		t.Errorf("stats differ:\nfile:\n%s\nhosted:\n%s", fileOut, hostOut)
	}

	strip := regexp.MustCompile(`(?m)^\S+  `)
	fileOut.Reset()
	hostOut.Reset()
	if err := (&stewardLedgerRecallKong{Category: "merge-approval", Limit: 10}).Run(fileCtx); err != nil {
		t.Fatal(err)
	}
	if err := (&stewardLedgerRecallKong{Category: "merge-approval", Limit: 10}).Run(hostCtx); err != nil {
		t.Fatal(err)
	}
	if f, h := strip.ReplaceAllString(fileOut.String(), ""), strip.ReplaceAllString(hostOut.String(), ""); f != h {
		t.Errorf("recall differs:\nfile:\n%s\nhosted:\n%s", f, h)
	}
	if !strings.Contains(hostOut.String(), "at-2") || strings.Index(hostOut.String(), "at-2") > strings.Index(hostOut.String(), "at-1") {
		t.Errorf("recall not most-recent-first:\n%s", hostOut)
	}
}

func TestStewardLedgerHosted_IgnoresNonLedgerRows(t *testing.T) {
	m := newMemBD()
	m.mem["implementer:hot:x"] = "RULE: x"
	m.mem["stewardledgerX"] = "not a ledger row"
	ctx, out, _ := hostedLedgerCtx(t, m)
	if err := (&stewardLedgerStatsKong{}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no ledger entries") {
		t.Errorf("want empty ledger, got %q", out)
	}
}

func TestStewardLedgerRemote_CloudHomeSelectsHosted(t *testing.T) {
	m := newMemBD()
	ctx, _, _ := remoteLedgerCtx(t, m)
	recordLedger(t, ctx, "design-fork", "at-9", "accepted", "")
	if len(m.ledgerKeys()) != 1 {
		t.Fatalf("want 1 hosted row, got %v", m.ledgerKeys())
	}
	if _, err := os.Stat(StewardLedgerPath(ctx)); err == nil {
		t.Error("cloud_home record wrote the local file")
	}
}

func TestStewardLedger_NonHostedWithoutCloudHomeUsesFileAndNoRemember(t *testing.T) {
	ctx, _, _ := makeCtx(noBDCalls{t}, t.TempDir())
	recordLedger(t, ctx, "scope-call", "at-5", "accepted", "")
	data, err := os.ReadFile(StewardLedgerPath(ctx))
	if err != nil || !strings.Contains(string(data), `"initiative":"at-5"`) {
		t.Fatalf("file not written: %q %v", data, err)
	}
	if err := (&stewardLedgerStatsKong{}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := (&stewardLedgerSeedKong{}).Run(ctx); err == nil || !strings.Contains(err.Error(), "no hosted ledger store (set cloud_home)") {
		t.Errorf("seed on a non-hosted home: %v", err)
	}
}

func TestStewardLedgerHosted_UnreachableErrors(t *testing.T) {
	m := newMemBD()
	m.err = errors.New("Dolt server unreachable")
	ctx, out, _ := hostedLedgerCtx(t, m)

	err := (&stewardLedgerRecordKong{Category: "scope-call", Initiative: "at-1", Recommendation: "r", Verdict: "accepted"}).Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "ledger store unreachable (sandbox.example.com): may not be saved; retry later") {
		t.Errorf("record error: %v", err)
	}
	if _, statErr := os.Stat(StewardLedgerPath(ctx)); statErr == nil {
		t.Error("record fell back to the local file")
	}
	for _, run := range []func() error{
		func() error { return (&stewardLedgerStatsKong{}).Run(ctx) },
		func() error { return (&stewardLedgerRecallKong{Category: "scope-call"}).Run(ctx) },
	} {
		err := run()
		if err == nil || !strings.Contains(err.Error(), "ledger store unreachable (sandbox.example.com)") {
			t.Errorf("read error: %v", err)
		}
	}
	if strings.Contains(out.String(), "no ledger entries") {
		t.Error("unreachable read printed an empty ledger")
	}
}

func writeLedgerFile(t *testing.T, ctx *cli.Context, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(StewardHome(ctx), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(StewardLedgerPath(ctx), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStewardLedgerSeed_FailedImportPrintsNoCount(t *testing.T) {
	m := newMemBD()
	ctx, out, _ := remoteLedgerCtx(t, m)
	writeLedgerFile(t, ctx, `{"ts":"2026-01-03T03:04:05Z","category":"merge-approval","initiative":"at-2","recommendation":"r2","verdict":"corrected","decision":"no"}`)
	m.importErr = errors.New("import failed")

	err := (&stewardLedgerSeedKong{}).Run(ctx)
	if err == nil || m.imports != 1 {
		t.Fatalf("err %v, imports %d; want an import error after one attempt", err, m.imports)
	}
	if out.String() != "" {
		t.Errorf("failed import printed %q; want no count line", out.String())
	}
}

func TestStewardLedgerSeed_DryRunRealRunAndIdempotent(t *testing.T) {
	m := newMemBD()
	ctx, out, _ := remoteLedgerCtx(t, m)
	good1 := `{"ts":"2026-01-02T03:04:05.123456789Z","category":"scope-call","initiative":"at-1","recommendation":"r1","verdict":"accepted"}`
	good2 := `{"ts":"2026-01-03T03:04:05Z","category":"merge-approval","initiative":"at-2","recommendation":"r2","verdict":"corrected","decision":"no"}`
	writeLedgerFile(t, ctx, good1, "not json", good2, `{"ts":"2026-01-04T00:00:00Z","category":"bogus"}`)
	before, _ := os.ReadFile(StewardLedgerPath(ctx))

	if err := (&stewardLedgerSeedKong{DryRun: true}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := "seed: 2 would write, 0 already present, 2 malformed skipped\n"; out.String() != want {
		t.Errorf("dry-run output %q, want %q", out, want)
	}
	if len(m.mem) != 0 || m.imports != 0 {
		t.Error("dry-run wrote to the store")
	}

	out.Reset()
	if err := (&stewardLedgerSeedKong{}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := "seed: 2 to write, 0 already present, 2 malformed skipped\n"; out.String() != want {
		t.Errorf("run output %q, want %q", out, want)
	}
	keys := m.ledgerKeys()
	if len(keys) != 2 || m.imports != 1 {
		t.Fatalf("want 2 rows in 1 import, got %v (%d imports)", keys, m.imports)
	}
	if !strings.HasPrefix(keys[0], "stewardledger.") || !strings.Contains(m.mem[keys[0]], `"at-1"`) {
		t.Errorf("unexpected rows: %v", m.mem)
	}

	out.Reset()
	if err := (&stewardLedgerSeedKong{}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := "seed: 0 to write, 2 already present, 2 malformed skipped\n"; out.String() != want {
		t.Errorf("second run output %q, want %q", out, want)
	}
	if m.imports != 1 {
		t.Error("second run imported again")
	}
	if after, _ := os.ReadFile(StewardLedgerPath(ctx)); !bytes.Equal(before, after) {
		t.Error("seed modified the local file")
	}

	// A record written by the live path after seeding carries the same key a
	// seeded copy of it would, so seed skips it too.
	out.Reset()
	if err := (&stewardLedgerStatsKong{}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "scope-call") || !strings.Contains(out.String(), "merge-approval") {
		t.Errorf("stats do not read seeded rows:\n%s", out)
	}
}

func TestStewardLedgerRows_DoNotChangeLearningsOutputs(t *testing.T) {
	run := func(withLedger bool) string {
		m := newMemBD()
		m.mem["implementer:hot:a-rule"] = "RULE: a\nTRIGGER: b\nAPPLY: c"
		m.mem["tester:fresh:b-rule"] = "RULE: d"
		if withLedger {
			ctx, _, _ := hostedLedgerCtx(t, m)
			recordLedger(t, ctx, "scope-call", "at-1", "accepted", "")
			recordLedger(t, ctx, "merge-approval", "at-2", "accepted", "")
		}
		ctx, out, _ := hostedLedgerCtx(t, m)
		if err := (&learningsKong{Role: "implementer"}).Run(ctx); err != nil {
			t.Fatalf("learnings: %v", err)
		}
		if err := (&rolesKong{}).Run(ctx); err != nil {
			t.Fatalf("roles: %v", err)
		}
		if err := (&condenseCheckKong{}).Run(ctx); err != nil {
			t.Fatalf("condense-check: %v", err)
		}
		return out.String()
	}
	if without, with := run(false), run(true); without != with {
		t.Errorf("ledger rows changed learnings output:\nwithout:\n%s\nwith:\n%s", without, with)
	}
}

// ── rm8: self decisions, reviews ───────────────────────────────────

// selfLedgerCtxs runs a case against the local file store and the hosted store.
func selfLedgerCtxs(t *testing.T, fn func(t *testing.T, ctx *cli.Context, out *bytes.Buffer)) {
	t.Run("local", func(t *testing.T) {
		ctx, out, _ := makeCtx(noBDCalls{t}, t.TempDir())
		fn(t, ctx, out)
	})
	t.Run("hosted", func(t *testing.T) {
		ctx, out, _ := hostedLedgerCtx(t, newMemBD())
		fn(t, ctx, out)
	})
}

var recordedKey = regexp.MustCompile(`key=(\S+)`)

func recordSelf(t *testing.T, ctx *cli.Context, out *bytes.Buffer, cmd stewardLedgerRecordKong) (string, error) {
	t.Helper()
	out.Reset()
	cmd.Recommendation = "rec"
	err := cmd.Run(ctx)
	if m := recordedKey.FindStringSubmatch(out.String()); m != nil {
		return m[1], err
	}
	return "", err
}

func TestStewardLedgerSelf_ListsThenShowsReviewed_LocalAndHosted(t *testing.T) {
	selfLedgerCtxs(t, func(t *testing.T, ctx *cli.Context, out *bytes.Buffer) {
		key, err := recordSelf(t, ctx, out, stewardLedgerRecordKong{Category: "intake", Initiative: "at-1", Verdict: "self-approved"})
		if err != nil || key == "" {
			t.Fatalf("self record: key=%q err=%v", key, err)
		}
		out.Reset()
		if err := (&stewardLedgerSelfKong{Since: "1h"}).Run(ctx); err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`^\S+  intake  initiative=at-1  verdict=self-approved  reviews=-\n$`).MatchString(out.String()) {
			t.Errorf("self line: %q", out.String())
		}
		if _, err := recordSelf(t, ctx, out, stewardLedgerRecordKong{Category: "intake", Initiative: "at-1", Verdict: "corrected", Decision: "should have denied", Reviews: key}); err != nil {
			t.Fatalf("review: %v", err)
		}
		out.Reset()
		(&stewardLedgerSelfKong{Since: "1h", JSON: true}).Run(ctx)
		var rows []stewardLedgerSelfRow
		if err := json.Unmarshal(out.Bytes(), &rows); err != nil || len(rows) != 2 {
			t.Fatalf("json: %v %s", err, out.String())
		}
		if rows[0].ReviewState != "corrected" || rows[1].Reviews != key {
			t.Errorf("review state not shown: %+v", rows)
		}
		out.Reset()
		(&stewardLedgerSelfKong{Since: "1h"}).Run(ctx)
		if !strings.Contains(out.String(), "reviews="+key) {
			t.Errorf("review line missing key: %q", out.String())
		}
		out.Reset()
		if err := (&stewardLedgerSelfKong{Since: "2999-01-01T00:00:00Z"}).Run(ctx); err != nil || !strings.Contains(out.String(), "no ledger entries") {
			t.Errorf("future since: %v %q", err, out.String())
		}
		if err := (&stewardLedgerSelfKong{Since: "bogus"}).Run(ctx); err == nil {
			t.Error("bogus since accepted")
		}
	})
}

func TestStewardLedgerRecord_ReviewsRefusals(t *testing.T) {
	selfLedgerCtxs(t, func(t *testing.T, ctx *cli.Context, out *bytes.Buffer) {
		key, _ := recordSelf(t, ctx, out, stewardLedgerRecordKong{Category: "po-question", Initiative: "at-2", Verdict: "self-denied"})
		_, human := recordSelf(t, ctx, out, stewardLedgerRecordKong{Category: "scope-call", Initiative: "at-2", Verdict: "accepted"})
		if human != nil {
			t.Fatal(human)
		}
		for name, c := range map[string]stewardLedgerRecordKong{
			"corrected without decision": {Category: "po-question", Initiative: "at-2", Verdict: "corrected", Reviews: key},
			"unknown key":                {Category: "po-question", Initiative: "at-2", Verdict: "accepted", Reviews: "0000000000000000001-abcd"},
			"self verdict reviews":       {Category: "po-question", Initiative: "at-2", Verdict: "self-approved", Reviews: key},
		} {
			if _, err := recordSelf(t, ctx, out, c); err == nil {
				t.Errorf("%s: accepted", name)
			}
		}
		rows, _ := readStewardLedgerRecords(ctx, newStewardLedgerStore(ctx), StewardLedgerPersonaSteward, "t")
		if len(rows) != 2 {
			t.Fatalf("refused records were stored: %d", len(rows))
		}
		if _, err := recordSelf(t, ctx, out, stewardLedgerRecordKong{Category: "scope-call", Initiative: "at-2", Verdict: "accepted", Reviews: rows[1].Key}); err == nil {
			t.Error("review of a human-verdict record accepted")
		}
	})
}

func TestStewardLedgerStats_SplitCounts(t *testing.T) {
	selfLedgerCtxs(t, func(t *testing.T, ctx *cli.Context, out *bytes.Buffer) {
		k1, _ := recordSelf(t, ctx, out, stewardLedgerRecordKong{Category: "intake", Initiative: "a", Verdict: "self-approved"})
		k2, _ := recordSelf(t, ctx, out, stewardLedgerRecordKong{Category: "intake", Initiative: "b", Verdict: "self-denied"})
		recordSelf(t, ctx, out, stewardLedgerRecordKong{Category: "intake", Initiative: "d", Verdict: "accepted"})
		recordSelf(t, ctx, out, stewardLedgerRecordKong{Category: "intake", Initiative: "a", Verdict: "accepted", Reviews: k1})
		recordSelf(t, ctx, out, stewardLedgerRecordKong{Category: "intake", Initiative: "b", Verdict: "corrected", Decision: "x", Reviews: k2})
		out.Reset()
		if err := (&stewardLedgerStatsKong{JSON: true}).Run(ctx); err != nil {
			t.Fatal(err)
		}
		var rows []stewardCategoryStats
		if err := json.Unmarshal(out.Bytes(), &rows); err != nil || len(rows) != 1 {
			t.Fatalf("%v %s", err, out.String())
		}
		want := stewardCategoryStats{Category: "intake", Total: 3, Accepted: 1, AcceptRate: 1, SelfApproved: 1, SelfDenied: 1, Reviewed: 2, ReviewCorrected: 1}
		if rows[0] != want {
			t.Errorf("stats = %+v, want %+v", rows[0], want)
		}
	})
}

func TestStewardLedger_LegacyLinesParseUnchanged(t *testing.T) {
	ctx, out, _ := makeCtx(noBDCalls{t}, t.TempDir())
	legacy := `{"ts":"2026-01-02T03:04:05Z","category":"plan-approval","initiative":"old-1","recommendation":"r","verdict":"corrected","decision":"d"}`
	writeLedgerFile(t, ctx, legacy)
	rec, err := ParseStewardLedgerRecord([]byte(legacy))
	if err != nil || rec.Reviews != "" {
		t.Fatalf("legacy parse: %+v %v", rec, err)
	}
	if err := (&stewardLedgerStatsKong{JSON: true}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != `[{"category":"plan-approval","total":1,"accepted":0,"corrected":1,"accept_rate":0}]` {
		t.Errorf("legacy stats changed: %s", got)
	}
}

// A category with no human verdicts shows "-" for ACCEPT RATE in the split
// table; the legacy table keeps its percent.
func TestStewardLedgerStats_SplitTableDashForNoHumanVerdicts(t *testing.T) {
	ctx, stdout, _ := makeCtx(&fakeBD{}, t.TempDir())
	rows := []stewardCategoryStats{
		{Category: "intake", Total: 2, SelfApproved: 2},
		{Category: "plan", Total: 2, Accepted: 1, Corrected: 1, AcceptRate: 0.5},
	}
	if err := writeStewardStatsTable(ctx, rows); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	if !strings.Contains(out, "intake") || strings.Contains(strings.Split(out, "\n")[2], "0%") {
		t.Errorf("intake row should show -:\n%s", out)
	}
	if !strings.Contains(strings.Split(out, "\n")[3], "50%") {
		t.Errorf("plan row should show 50%%:\n%s", out)
	}
}

// A stored record from the removed autonomy feature still parses, its extra
// field is ignored, and it lists/counts as an ordinary record.
func TestStewardLedger_LegacyAutonomyFieldIgnored(t *testing.T) {
	ctx, out, _ := makeCtx(noBDCalls{t}, t.TempDir())
	legacy := `{"ts":"2026-01-02T03:04:05Z","category":"intake","initiative":"old-1","recommendation":"r","verdict":"self-approved","autonomy":"tg-42"}`
	writeLedgerFile(t, ctx, legacy)
	if _, err := ParseStewardLedgerRecord([]byte(legacy)); err != nil {
		t.Fatalf("legacy autonomy record: %v", err)
	}
	if err := (&stewardLedgerSelfKong{Since: "100000h"}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "2026-01-02T03:04:05Z  intake  initiative=old-1  verdict=self-approved  reviews=-" {
		t.Errorf("self line: %q", got)
	}
}
