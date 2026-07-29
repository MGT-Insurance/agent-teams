package initiative_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// ---------------------------------------------------------------------------
// JSONFields — the wire read seam (agent-teams-ully.12).
//
// The properties under test here are the ones an out-of-process consumer
// depends on and cannot check for itself: which keys appear, what each key's
// JSON type is, and that a key with no Fields member is carried rather than
// dropped.
// ---------------------------------------------------------------------------

func issue(description string) bd.Issue {
	return bd.Issue{ID: "at-test", Description: description}
}

func TestJSONFieldsEmitsCanonicalLineKeys(t *testing.T) {
	got := initiative.JSONFields(issue(
		"problem: fix the thing\n" +
			"repo: /Users/x/Code/repo\n" +
			"worktree: /Users/x/.wt/thing\n" +
			"branch: thing\n" +
			"team: repo-thing\n" +
			"mode: bg\n" +
			"epic: repo-abc1\n",
	))
	want := map[string]any{
		"problem":  "fix the thing",
		"repo":     "/Users/x/Code/repo",
		"worktree": "/Users/x/.wt/thing",
		"branch":   "thing",
		"team":     "repo-thing",
		"mode":     "bg",
		"epic":     "repo-abc1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JSONFields =\n  %#v\nwant\n  %#v", got, want)
	}
}

// The two multi-valued keys keep their LINE key ("session", not "sessions")
// and are always arrays — a consumer never has to handle "sometimes a string".
func TestJSONFieldsMultiValuedKeysAreArraysUnderTheirLineKey(t *testing.T) {
	got := initiative.JSONFields(issue(
		"repo: /r\n" +
			"session: aaa\n" +
			"track-worktree: /wt/one\n" +
			"session: bbb\n" +
			"track-worktree: /wt/two\n",
	))
	if _, renamed := got["sessions"]; renamed {
		t.Error(`JSONFields emitted "sessions"; the wire key is the line key "session"`)
	}
	if _, renamed := got["tracks"]; renamed {
		t.Error(`JSONFields emitted "tracks"; the wire key is the line key "track-worktree"`)
	}
	if want := []string{"aaa", "bbb"}; !reflect.DeepEqual(got["session"], want) {
		t.Errorf("session = %#v, want %#v (registration order)", got["session"], want)
	}
	if want := []string{"/wt/one", "/wt/two"}; !reflect.DeepEqual(got["track-worktree"], want) {
		t.Errorf("track-worktree = %#v, want %#v (registration order)", got["track-worktree"], want)
	}
}

// A single session still marshals as a one-element array, not a bare string.
func TestJSONFieldsSingleSessionMarshalsAsArray(t *testing.T) {
	raw, err := json.Marshal(initiative.JSONFields(issue("repo: /r\nsession: only-one\n")))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if want := `{"repo":"/r","session":["only-one"]}`; string(raw) != want {
		t.Errorf("marshalled = %s, want %s", raw, want)
	}
}

func TestJSONFieldsStandbyIsABool(t *testing.T) {
	got := initiative.JSONFields(issue("repo: /r\nstandby: true\n"))
	if got["standby"] != true {
		t.Errorf("standby = %#v (%T), want bool true", got["standby"], got["standby"])
	}
}

// standby is emitted for the exact value "true" only, matching Of. Any other
// value is still a real canonical line, so the key is present and false —
// absent (never written) stays distinguishable from present-but-not-true.
func TestJSONFieldsStandbyOtherValueIsPresentAndFalse(t *testing.T) {
	got := initiative.JSONFields(issue("repo: /r\nstandby: yes\n"))
	value, present := got["standby"]
	if !present {
		t.Fatal("standby key missing; a present line must yield a present key")
	}
	if value != false {
		t.Errorf("standby = %#v, want false (only the exact value \"true\" is true)", value)
	}
}

// Absent keys are omitted, not emitted empty: a consumer must be able to tell
// "never set" from "set to something empty".
func TestJSONFieldsOmitsAbsentKeys(t *testing.T) {
	got := initiative.JSONFields(issue("repo: /r\n"))
	for _, key := range []string{"problem", "worktree", "branch", "team", "mode", "epic", "standby", "session", "track-worktree"} {
		if _, present := got[key]; present {
			t.Errorf("absent key %q was emitted as %#v; absent keys must be omitted", key, got[key])
		}
	}
	if len(got) != 1 {
		t.Errorf("JSONFields has %d keys, want 1: %#v", len(got), got)
	}
}

func TestJSONFieldsEmptyDescriptionMarshalsAsEmptyObject(t *testing.T) {
	raw, err := json.Marshal(initiative.JSONFields(issue("")))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if string(raw) != "{}" {
		t.Errorf("marshalled = %s, want {} (never null — a consumer indexes into it)", raw)
	}
}

// Frozen item 3, at the wire seam: a canonical key with no Fields member is
// legitimate data and must survive. These five keys are real — captured from
// the live registry's closed initiatives (pr-* written by the review-pr skill,
// review-focus/review-instructions/goal/summary by other skill files), none of
// them known to one line of Go.
func TestJSONFieldsCarriesKeysWithNoFieldsMember(t *testing.T) {
	got := initiative.JSONFields(issue(
		"problem: Review PR #4567 (MGT-Insurance/midgard)\n" +
			"repo: /Users/x/Code/midgard\n" +
			"pr-number: 4567\n" +
			"pr-repo: MGT-Insurance/midgard\n" +
			"pr-url: https://github.com/MGT-Insurance/midgard/pull/4567\n" +
			"review-focus: the appetite gate\n" +
			"goal: land it\n" +
			"summary: one paragraph\n",
	))
	for key, want := range map[string]string{
		"pr-number":    "4567",
		"pr-repo":      "MGT-Insurance/midgard",
		"pr-url":       "https://github.com/MGT-Insurance/midgard/pull/4567",
		"review-focus": "the appetite gate",
		"goal":         "land it",
		"summary":      "one paragraph",
	} {
		if got[key] != want {
			t.Errorf("unmodeled key %q = %#v, want %q — an unmodeled canonical key must not be dropped", key, got[key], want)
		}
	}
}

// A description-embedded "status:" line collides by NAME with bd's own element
// key of the same name. It is carried here because "fields" is a nested object,
// which is why the fields go under one key instead of being merged into the
// element. Real: "status" appears among the live registry's canonical keys.
func TestJSONFieldsCarriesAKeyThatCollidesWithABdElementKey(t *testing.T) {
	got := initiative.JSONFields(issue("repo: /r\nstatus: awaiting review\n"))
	if got["status"] != "awaiting review" {
		t.Errorf("status = %#v, want %q", got["status"], "awaiting review")
	}
}

// First-wins applies to unmodeled keys too — the frozen rule is stated over
// keys generally, not over the ten Go happens to model.
func TestJSONFieldsFirstWinsForAnUnmodeledKey(t *testing.T) {
	got := initiative.JSONFields(issue("pr-url: https://first\npr-url: https://second\n"))
	if got["pr-url"] != "https://first" {
		t.Errorf("pr-url = %#v, want %q (first occurrence wins)", got["pr-url"], "https://first")
	}
}

// The colon/space/column-0 half of the frozen rule, as observed at the wire
// seam: a near-miss line emits NO key at all, so a consumer sees "absent",
// never "" or a partial value.
//
// These cases came from the dashboard's own TypeScript rule tests
// (agent-teams-ully.8). Deleting that second implementation (agent-teams-ully.12)
// would have deleted the only assertions covering them, so they move here — the
// coverage relocates with the code, it does not evaporate.
func TestJSONFieldsRejectsNearMissFieldLines(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
	}{
		{"no space after the colon", "repo:x"},
		{"colon at end of line, no value", "repo:"},
		{"two spaces after the colon", "repo:  /x"},
		{"leading whitespace before the key", "  repo: /x"},
		{"value is the mandatory space and nothing else", "repo: "},
		{"value is whitespace only", "repo:    "},
		{"mis-cased key", "Repo: /x"},
		{"list-item prefix", "- repo: /x"},
		{"key followed by a space before the colon", "repo : /x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := initiative.JSONFields(issue(tc.line + "\n"))
			if len(got) != 0 {
				t.Errorf("JSONFields(%q) = %#v, want no keys at all", tc.line, got)
			}
		})
	}
}

func TestJSONFieldsFirstWinsForAModeledKey(t *testing.T) {
	got := initiative.JSONFields(issue("repo: /first\nrepo: /second\n"))
	if got["repo"] != "/first" {
		t.Errorf("repo = %#v, want %q (first occurrence wins)", got["repo"], "/first")
	}
}

// The frozen rule reaches the wire seam unchanged: the real poisoned at-jno7
// line ("Repo: `/path`", capital R, backtick-wrapped, 47 lines down) is
// structurally invisible, so the emitted repo is the real path from line 2 —
// the value that makes the ownership check pass.
func TestJSONFieldsIgnoresThePoisonedLineInTheRealAtJno7Record(t *testing.T) {
	got := initiative.JSONFields(issue(jno7Description))
	if want := "/Users/erlloyd/Code/agent-teams"; got["repo"] != want {
		t.Errorf("repo = %#v, want %q (the canonical header line, not the poisoned echo)", got["repo"], want)
	}
	if _, folded := got["Repo"]; folded {
		t.Error(`emitted a "Repo" key; a mis-cased line is not a field line at all`)
	}
}

// A field line arbitrarily far down the description is still found (frozen
// item 2): the wire seam must not acquire a bounded header scan either.
func TestJSONFieldsFindsAFieldLineBelowALongProseBody(t *testing.T) {
	prose := ""
	for i := 0; i < 400; i++ {
		prose += "some prose that is not a field line at all\n"
	}
	got := initiative.JSONFields(issue("repo: /r\n\n" + prose + "\nsession: tied-late\n"))
	if want := []string{"tied-late"}; !reflect.DeepEqual(got["session"], want) {
		t.Errorf("session = %#v, want %#v — a tail field line must be found however far down it is", got["session"], want)
	}
}

// Of and JSONFields project the same scan, so they can never disagree about a
// value. Checked over the real poisoned record, where a disagreement is
// exactly what the incident was.
func TestJSONFieldsAgreesWithOf(t *testing.T) {
	iss := issue(jno7Description)
	typed := initiative.Of(iss)
	wire := initiative.JSONFields(iss)
	for key, want := range map[string]string{
		"problem":  typed.Problem,
		"repo":     typed.Repo,
		"worktree": typed.Worktree,
		"branch":   typed.Branch,
		"team":     typed.Team,
		"mode":     typed.Mode,
		"epic":     typed.Epic,
	} {
		if got := wire[key]; got != want {
			t.Errorf("wire[%q] = %#v, Of gives %q", key, got, want)
		}
	}
}

// Go's encoding/json sorts map keys, so the emitted object is byte-stable
// across runs — a checked-in golden file stays diff-clean.
func TestJSONFieldsMarshalsDeterministically(t *testing.T) {
	iss := issue("worktree: /w\nrepo: /r\nsession: b\nsession: a\nbranch: br\n")
	first, err := json.Marshal(initiative.JSONFields(iss))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	for i := 0; i < 20; i++ {
		again, err := json.Marshal(initiative.JSONFields(iss))
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if string(again) != string(first) {
			t.Fatalf("marshalled output varies between calls:\n  %s\n  %s", first, again)
		}
	}
	if want := `{"branch":"br","repo":"/r","session":["b","a"],"worktree":"/w"}`; string(first) != want {
		t.Errorf("marshalled = %s, want %s", first, want)
	}
}
