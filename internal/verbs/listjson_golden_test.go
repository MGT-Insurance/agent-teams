package verbs_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ---------------------------------------------------------------------------
// The cross-language golden (agent-teams-ully.12).
//
// The dashboard is a separate deploy in a separate language, so it cannot
// import internal/initiative and cannot call this verb from a unit test. Two
// hand-maintained fixture sets — one per language — would drift, which is the
// failure mode this whole initiative exists to remove. Instead there is ONE
// artifact: this test produces testdata/list-json/ateam-list-json.golden.json
// from the verb itself, and the dashboard's parse.test.ts reads that same file
// as its input. If the Go output shape changes, this test fails until the
// golden is regenerated, and the regenerated golden is what the TypeScript
// suite then asserts against.
//
// Regenerate with:
//
//	go test ./internal/verbs/ -run TestListJSONGolden -update
// ---------------------------------------------------------------------------

var updateGolden = flag.Bool("update", false, "rewrite the list-json golden file from the current verb output")

const (
	goldenDir    = "../../testdata/list-json"
	goldenInput  = goldenDir + "/bd-list.json"
	goldenOutput = goldenDir + "/ateam-list-json.golden.json"
)

// goldenBdList is the checked-in raw `bd list --json` payload the golden is
// built from: five REAL initiative records captured from the live registry
// (open + closed), selected to cover every shape the field rule has to handle.
// Descriptions are byte-verbatim — they are the subject of the test. The only
// edit is that each record's freeform `notes` is sliced to its first 400 bytes,
// purely to keep the fixture reviewable; notes are not routing data and
// initiative.Of never reads them.
func goldenBdList(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(goldenInput)
	if err != nil {
		t.Fatalf("reading %s: %v", goldenInput, err)
	}
	return raw
}

// runListJSONOverGoldenInput runs the real verb over the checked-in bd payload
// and returns exactly the bytes `ateam list-json` would print.
func runListJSONOverGoldenInput(t *testing.T) []byte {
	t.Helper()
	input := goldenBdList(t)
	out := &bytes.Buffer{}
	ctx := &cli.Context{
		Home: "/ws",
		BD: bd.NewClientWithExec("/ws", func(string, ...string) ([]byte, []byte, error) {
			return input, nil, nil
		}),
		Stdout: out,
		Stderr: &bytes.Buffer{},
	}
	if err := runQ(t, "list-json", ctx); err != nil {
		t.Fatalf("list-json.Run: %v", err)
	}
	return out.Bytes()
}

func TestListJSONGolden(t *testing.T) {
	got := runListJSONOverGoldenInput(t)

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenOutput), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(goldenOutput), err)
		}
		if err := os.WriteFile(goldenOutput, got, 0o644); err != nil {
			t.Fatalf("writing %s: %v", goldenOutput, err)
		}
		t.Logf("wrote %s (%d bytes)", goldenOutput, len(got))
		return
	}

	want, err := os.ReadFile(goldenOutput)
	if err != nil {
		t.Fatalf("reading %s: %v\nregenerate with: go test ./internal/verbs/ -run TestListJSONGolden -update", goldenOutput, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is stale: the verb's output no longer matches it.\n"+
			"The dashboard's parse.test.ts consumes this file, so regenerate it and re-run the dashboard suite:\n"+
			"  go test ./internal/verbs/ -run TestListJSONGolden -update\n"+
			"  cd dashboard && pnpm --filter @agent-teams/dashboard-server test\n"+
			"got %d bytes, want %d", goldenOutput, len(got), len(want))
	}
}

// The golden is worthless if it doesn't actually contain the shapes it exists
// to pin. These assertions name each one, so a regeneration that quietly lost
// coverage (e.g. a record dropped from the input) fails here rather than going
// unnoticed.
func TestListJSONGoldenCoversEveryFieldShape(t *testing.T) {
	var elements []struct {
		ID     string         `json:"id"`
		Fields map[string]any `json:"fields"`
	}
	if err := json.Unmarshal(runListJSONOverGoldenInput(t), &elements); err != nil {
		t.Fatalf("golden output is not a JSON array of objects: %v", err)
	}

	byID := make(map[string]map[string]any, len(elements))
	for _, element := range elements {
		byID[element.ID] = element.Fields
	}
	for _, id := range []string{"at-2mol", "at-4xl7", "at-jno7", "at-kbd2", "at-o0v"} {
		if _, present := byID[id]; !present {
			t.Fatalf("golden input lost record %s; it is there to cover a specific field shape", id)
		}
	}

	// at-jno7: the real incident. A "Repo: `/path`" line 47 lines below the
	// header must not win — this is the value whose corruption silently dropped
	// every human reply in that topic.
	if got, want := byID["at-jno7"]["repo"], "/Users/erlloyd/Code/agent-teams"; got != want {
		t.Errorf("at-jno7 repo = %#v, want %q (the header line, not the poisoned prose echo)", got, want)
	}

	// at-o0v: standby crosses the wire as a bool, not the string "true".
	if got := byID["at-o0v"]["standby"]; got != true {
		t.Errorf("at-o0v standby = %#v (%T), want bool true", got, got)
	}

	// at-kbd2: the pr-* trio — canonical keys with no Fields member, written by
	// a skill file with no Go involved. Frozen item 3: they must survive.
	for key, want := range map[string]string{
		"pr-number": "4567",
		"pr-repo":   "MGT-Insurance/midgard",
		"pr-url":    "https://github.com/MGT-Insurance/midgard/pull/4567",
	} {
		if got := byID["at-kbd2"][key]; got != want {
			t.Errorf("at-kbd2 %s = %#v, want %q (an unmodeled canonical key must not be dropped)", key, got, want)
		}
	}

	// at-2mol: a multi-valued key with several real values, in registration order.
	tracks, ok := byID["at-2mol"]["track-worktree"].([]any)
	if !ok {
		t.Fatalf("at-2mol track-worktree = %#v, want an array", byID["at-2mol"]["track-worktree"])
	}
	if len(tracks) != 5 {
		t.Errorf("at-2mol has %d track-worktree values, want 5", len(tracks))
	}
	if len(tracks) > 0 && tracks[0] != "/Users/ericlloyd/.agent-teams-worktrees/midgard-2mol-dto" {
		t.Errorf("at-2mol track-worktree[0] = %#v, want the first registered track", tracks[0])
	}

	// at-4xl7: a record bd emitted with no "notes" key at all. It is in the
	// fixture so the TypeScript consumer is exercised against a missing key
	// rather than only against complete records.
	if _, present := byID["at-4xl7"]["repo"]; !present {
		t.Error("at-4xl7 has no repo field; the no-notes record should still carry routing data")
	}
}
