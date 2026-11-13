package verbs

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// witnessStore holds every tier and namespace the learnings verbs read.
func witnessStore() map[string]any {
	return map[string]any{
		"dri:hot:h1":      "hot body one",
		"dri:hot:h2":      "hot body two",
		"dri:fresh:f1":    "fresh body one",
		"dri:cold1":       "a cold dri body with the word ship in it",
		"dri:cold2":       "another cold dri body",
		"planner:hot:p1":  "planner hot",
		"planner:cold9":   "planner cold",
		"user:pref":       "user preference",
		"applied:dri:h1":  `{"count":3,"last_applied":"2026-01-02T03:04:05Z"}`,
		"applied:dri:f1":  `{"count":1,"last_applied":"2026-02-02T03:04:05Z"}`,
		"applied:planner": "not a dri row",
		"schema_version":  1,
	}
}

// witnessRun runs one verb against a fake bd that serves store. With
// sqlSupported false the fake answers bd sql like embedded-mode bd (an error),
// which forces every scoped read onto its whole-store fallback.
func witnessRun(t *testing.T, sqlSupported bool, run func(*cli.Context) error) (stdout string, calls [][]string) {
	t.Helper()
	store := witnessStore()
	storeJSON, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	execFn := func(_ string, args ...string) ([]byte, []byte, error) {
		if len(args) >= 2 && args[0] == "-C" {
			args = args[2:]
		}
		calls = append(calls, append([]string(nil), args...))
		switch {
		case isSQLCall(args):
			if !sqlSupported {
				return nil, []byte("Error: 'bd sql' is not yet supported in embedded mode"), errors.New("exit 1")
			}
			return storeSQLAnswer(args[3], store), nil, nil
		case isWholeStoreRead(args):
			return storeJSON, nil, nil
		}
		return nil, nil, nil // remember / forget
	}
	var out bytes.Buffer
	ctx := &cli.Context{Home: t.TempDir(), BD: bd.NewClientWithExec(t.TempDir(), execFn), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := run(ctx); err != nil {
		t.Fatalf("verb failed: %v", err)
	}
	return out.String(), calls
}

// TestLearningsVerbs_ScopedReadsMatchWholeStore is the witness for the scoped
// memory reads. For each prefix-scoped verb it proves (1) the scoped run issues
// no whole-store memories read and names the verb's prefixes in a bd sql call,
// and (2) its stdout and write calls equal those of a run forced onto the
// whole-store fallback, so the scoped read returns the same data.
func TestLearningsVerbs_ScopedReadsMatchWholeStore(t *testing.T) {
	cases := []struct {
		name     string
		run      func(*cli.Context) error
		prefixes [][]string
	}{
		{"learnings", func(c *cli.Context) error { return runLearnings(c, "dri") }, [][]string{{"dri:hot:", "dri:fresh:"}}},
		{"prime", runPrime, [][]string{{"user:"}}},
		{"recall", func(c *cli.Context) error { return runRecall(c, "dri", "ship") }, [][]string{{"dri:"}}},
		{"condense", func(c *cli.Context) error { return (&condenseKong{Role: "dri"}).Run(c) }, [][]string{{"dri:", "applied:dri:"}}},
		{"condense-check", func(c *cli.Context) error { return (&condenseCheckKong{Role: "dri", JSON: true}).Run(c) }, [][]string{{"dri:"}}},
		{"fresh-drain", func(c *cli.Context) error { return (&freshDrainKong{Role: "dri"}).Run(c) }, [][]string{{"dri:fresh:"}}},
		{"applied", func(c *cli.Context) error { return (&appliedKong{Role: "dri", Slug: "h1"}).Run(c) }, [][]string{{"applied:dri:h1"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scopedOut, scopedCalls := witnessRun(t, true, tc.run)
			assertScopedReads(t, scopedCalls, tc.prefixes...)

			wholeOut, wholeCalls := witnessRun(t, false, tc.run)
			if scopedOut != wholeOut {
				t.Errorf("stdout differs\n scoped: %q\n whole:  %q", scopedOut, wholeOut)
			}
			if !reflect.DeepEqual(writeCalls(scopedCalls), writeCalls(wholeCalls)) {
				t.Errorf("write calls differ\n scoped: %v\n whole:  %v", writeCalls(scopedCalls), writeCalls(wholeCalls))
			}
			if strings.TrimSpace(scopedOut) == "" && tc.name != "fresh-drain" && tc.name != "applied" {
				t.Errorf("witness fixture produced no output; the parity check is vacuous")
			}
		})
	}
}

// writeCalls returns the remember and forget calls, with the applied record's
// timestamp body dropped (it differs per run).
func writeCalls(calls [][]string) [][]string {
	var out [][]string
	for _, c := range calls {
		switch c[0] {
		case "remember":
			out = append(out, c[:2])
		case "forget":
			out = append(out, c)
		}
	}
	return out
}
