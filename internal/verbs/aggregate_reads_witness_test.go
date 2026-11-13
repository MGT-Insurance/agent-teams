package verbs

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// TestAggregateVerbs_SizesReadMatchesWholeStore is the witness for the
// key-and-length read behind `ateam roles` and the all-roles condense-check.
// Each verb must issue exactly one bd sql call and no whole-store memories
// read, and print the same stdout as a run forced onto the whole-store
// fallback (embedded-mode bd).
func TestAggregateVerbs_SizesReadMatchesWholeStore(t *testing.T) {
	cases := []struct {
		name string
		run  func(*cli.Context) error
	}{
		{"roles", runRoles},
		{"condense-check", func(c *cli.Context) error { return (&condenseCheckKong{JSON: true}).Run(c) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sqlOut, sqlCalls := witnessRun(t, true, tc.run)
			assertSizesRead(t, sqlCalls)

			wholeOut, wholeCalls := witnessRun(t, false, tc.run)
			if sqlOut != wholeOut {
				t.Errorf("stdout differs\n sql:   %q\n whole: %q", sqlOut, wholeOut)
			}
			if len(wholeCalls) < 2 || !isWholeStoreRead(wholeCalls[len(wholeCalls)-1]) {
				t.Errorf("fallback run did not read the whole store: %v", wholeCalls)
			}
			if strings.TrimSpace(sqlOut) == "" {
				t.Errorf("witness fixture produced no output; the parity check is vacuous")
			}
		})
	}
}

// TestCondenseCheckAllRoles_FixedNumbers pins the all-roles output to numbers
// worked out by hand from witnessStore, including a role with no fresh tier.
func TestCondenseCheckAllRoles_FixedNumbers(t *testing.T) {
	out, _ := witnessRun(t, true, func(c *cli.Context) error { return (&condenseCheckKong{JSON: true}).Run(c) })
	var got []condenseCheckRoleResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	want := []condenseCheckRoleResult{
		{Role: "dri", LearningsBytes: 78, FreshBytes: 14},
		{Role: "planner", LearningsBytes: 27, FreshBytes: 0},
	}
	if len(got) != len(want) {
		t.Fatalf("roles = %+v", got)
	}
	for i, w := range want {
		if got[i].Role != w.Role || got[i].LearningsBytes != w.LearningsBytes || got[i].FreshBytes != w.FreshBytes {
			t.Errorf("role %d = %+v, want %+v", i, got[i], w)
		}
	}
}
