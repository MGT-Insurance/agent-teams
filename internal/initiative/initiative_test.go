package initiative_test

import (
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// ---------------------------------------------------------------------------
// Fixtures below are transcribed from `ateam show <id>` against the live
// agent-teams initiative registry (read-only, via the sanctioned `ateam`
// CLI — see internal/initiative/doc.go's Background section) rather than
// invented minimal shapes. Long prose paragraphs are trimmed with "[...]"
// where the exact wording doesn't matter to the test; every field line and
// every non-canonical line under test is verbatim.
// ---------------------------------------------------------------------------

// at-jno7's real header plus its real "## Pointers" section, which contains
// (at the real position, line 74 of `ateam show at-jno7`) a standalone,
// case-folded "Repo: ..." line — no list-dash, no backticks. This is the
// shape bead agent-teams-ully.1 (Eric's original root-cause bead, TESTS
// item 4) names directly: "the incident line used a capital first letter."
//
// bead .6's own test-1 spec instead describes the incident line as "wrapped
// in backticks." That does not survive a mechanical check: parseDescriptionFields
// (internal/verbs/route_match.go:29-42) computes
// key = strings.ToLower(strings.TrimSpace(line[:colon])) — TrimSpace strips
// whitespace only, never backticks — so any backtick touching the key before
// the colon, in a line like "`repo: ...`" or "`repo`: ...", makes key !=
// "repo" and the line never collides under parseDescriptionFields either.
// The only shape
// that (a) collides under parseDescriptionFields's case-insensitive map write
// AND (b) is "structurally invisible" under this package's case-sensitive
// fieldLine (frozen item 1) is a case-folded key — exactly bead .1's
// description, and exactly what is live in at-jno7 today. Bead .5's own
// census (comment, 2026-07-29 01:48) independently lists "a backticked
// mention inside at-j4dy's own briefing" as a SEPARATE, fourth non-canonical
// line, distinct from "the at-jno7 incident line" — the most likely
// explanation is bead .6's test-1 spec conflated that unrelated backticked
// mention with the actual at-jno7 mechanism. Flagged to team-lead
// (msg_id 76adbdfd-1bf4-4360-8803-131fa937c29e); proceeding on the
// mechanically-verified capital-letter reading pending a reply.
//
// at-jno7's line 74 value today already matches the canonical header value
// (line 9) — it reads as hand-patched post-incident, capitalization left as
// residue. The wrong value below is reconstructed to demonstrate the
// collision this line caused at the time of the incident; everything else
// (header, section structure, the line's real position under "## Pointers")
// is verbatim.
const jno7Description = `problem: PR reviews: collapse per-review initiative topics into one shared reviews topic, and capture the missing 'waiting on external PR review' state
repo: /Users/erlloyd/Code/agent-teams
worktree: /Users/erlloyd/.agent-teams-worktrees/pr-review-topic-noise-and-external-review-state
branch: pr-review-topic-noise-and-external-review-state
team: agent-teams-pr-review-topic-noise-and-external-review-state
mode: bg
epic: agent-teams-p9dm

## Eric's ask (verbatim, via steward-direct)

| Also, need another DRI to investigate some item's with PR reviews. First, pr reviews are initiatives, so they show up as separate topics, but have no comments. This is just noise. [...]

## Pointers (steward recon only — verify everything yourself)

Repo: /Users/erlloyd/old-clones/agent-teams
`

// at-y7l9's real header plus a real all-caps "GOAL:" prose heading. Confirmed
// by hand-tracing fieldLine's rule against today's parseDescriptionFields
// (internal/verbs/route_match.go:29): that lenient, case-insensitive reader
// adds "goal" to its field map from this exact line, since it does not
// restrict keys to a known set. Of must not.
const y7l9Description = `problem: Consolidate product class-code systems into one NAICS-based industry selection + a shared source-of-truth package
repo: /Users/ericlloyd/Code/midgard
worktree: /Users/ericlloyd/.agent-teams-worktrees/consolidate-product-class-code-systems-into-one
branch: consolidate-product-class-code-systems-into-one
team: midgard-consolidate-product-class-code-systems-into-one
mode: bg
epic: midgard-nsza

CURRENT STATE (the problem): Products use different, siloed classification systems. BOP uses one system; SpecialtyGL and SpecialtyProperty use another — a product-level industry_group selection that drives ISO GL/Property class_code options via a FRONTEND catalog (not Socotra config). There is no single classification a user picks regardless of product. [...]

GOAL: One user-facing INDUSTRY GROUP the user selects no matter which product they're quoting, keyed on NAICS codes — the first 4 digits of NAICS was suggested as the classification key.
`

// at-ig53's real header plus the real bulleted "- Repo: ..." briefing line —
// the census's "escapee ... prefixed with a list dash". Same value as the
// canonical repo: header line above it, so this line is harmless data-wise;
// what it demonstrates is the SHAPE that must not parse as a field. This is
// the exact one-character reason at-ig53 (dash prefix) escaped the incident
// that hit at-jno7 (no prefix) — see TestOf_JNO7PoisonedRepoResolvesUnpoisoned.
const ig53Description = `problem: Condense fires too often and costs too many tokens — reduce its frequency and per-run cost
repo: /Users/erlloyd/Code/agent-teams
worktree: /Users/erlloyd/.agent-teams-worktrees/condense-fires-too-often-and-costs-too-many
branch: condense-fires-too-often-and-costs-too-many
team: agent-teams-condense-fires-too-often-and-costs-too-many
mode: bg
epic: agent-teams-0yd3

## Eric's ask (verbatim, via steward-direct)

| Right now condense happens too often - it's very expensive in terms of tokens.

That is the entire brief. Scope is yours to pin down with him in Phase 2 clarify — he has not said what "too often" should become, nor whether the fix is cadence, cost-per-run, or both.

## Pointers (steward recon only — NOT an investigation)

I grepped to identify the target repo and stopped there. Verify all of this yourself; I did not read the trigger path.

- Repo: /Users/erlloyd/Code/agent-teams
`

// at-dxm5's real description in full. Lines 8-10 (pr-number, pr-repo,
// pr-url) are the three canonical keys the DRI amendment found unmodeled by
// Fields — written by an LLM following the review-pr skill, not by any Go
// code. This is the fixture the amendment's required round-trip test names
// explicitly.
const dxm5Description = `problem: Review PR #4526 (MGT-Insurance/midgard)
repo: /Users/ericlloyd/Code/midgard
worktree: /Users/ericlloyd/.agent-teams-worktrees/review-pr-4526-mgt-insurance-midgard
branch: review-pr-4526-mgt-insurance-midgard
team: midgard-review-pr-4526-mgt-insurance-midgard
mode: bg

pr-number: 4526
pr-repo: MGT-Insurance/midgard
pr-url: https://github.com/MGT-Insurance/midgard/pull/4526
`

// parseDescriptionFieldsLenient mirrors parseDescriptionFields
// (internal/verbs/route_match.go:29-42) verbatim. It is copied rather than
// called because the real function is unexported to package verbs; this is
// the only way to prove, inside this package's own test, that the fixture
// below actually collides under today's lenient reader and is not a straw
// man. Not used by any production code path in this package.
func parseDescriptionFieldsLenient(text string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		colon := strings.IndexByte(line, ':')
		if colon == -1 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		value := strings.TrimSpace(line[colon+1:])
		if key != "" && value != "" {
			result[key] = value
		}
	}
	return result
}

// TestOf_JNO7PoisonedRepoResolvesUnpoisoned is bead .6's required test 1: a
// prose line deep in the description (here, at-jno7's real "Repo: ..." line
// under its real "## Pointers" heading) redefines the repo key. Under
// today's parseDescriptionFields this line DOES win (case-insensitive,
// last-wins) — asserted below via parseDescriptionFieldsLenient, so this
// fixture genuinely witnesses the incident rather than a shape that was
// never actually dangerous. Of must resolve to the canonical header value
// regardless, because "Repo" fails frozen item 1's no-case-folding rule.
func TestOf_JNO7PoisonedRepoResolvesUnpoisoned(t *testing.T) {
	lenient := parseDescriptionFieldsLenient(jno7Description)
	if lenient["repo"] != "/Users/erlloyd/old-clones/agent-teams" {
		t.Fatalf("fixture does not witness the incident: today's lenient reader resolves repo to %q, want the poisoned value — this test would be a straw man", lenient["repo"])
	}

	f := initiative.Of(bd.Issue{ID: "at-jno7", Description: jno7Description})

	if f.Repo != "/Users/erlloyd/Code/agent-teams" {
		t.Errorf("Repo = %q, want unpoisoned canonical header value %q (poisoned value was %q)", f.Repo, "/Users/erlloyd/Code/agent-teams", "/Users/erlloyd/old-clones/agent-teams")
	}
	if f.Problem == "" || f.Worktree == "" || f.Branch == "" || f.Team == "" || f.Mode == "" || f.Epic != "agent-teams-p9dm" {
		t.Fatalf("canonical header fields did not parse: %+v", f)
	}
}

// TestOf_AllCapsGoalHeadingPopulatesNoField covers the census's second
// non-canonical shape: a "GOAL:" prose heading. Today's parseDescriptionFields
// (route_match.go:29) lowercases the key before storing it, so it would add
// "goal" -> "One user-facing INDUSTRY GROUP ..." to its field map — that map
// has no notion of "known" keys, so from its perspective this heading DOES
// populate a field. Of must reject it outright: GOAL is not lowercase, so it
// fails frozen item 1's no-case-folding rule and contributes nothing.
func TestOf_AllCapsGoalHeadingPopulatesNoField(t *testing.T) {
	f := initiative.Of(bd.Issue{ID: "at-y7l9", Description: y7l9Description})

	if f.Problem == "" || f.Repo == "" || f.Worktree == "" || f.Branch == "" || f.Team == "" || f.Mode == "" || f.Epic == "" {
		t.Fatalf("canonical header fields did not parse: %+v", f)
	}
	if f.Repo != "/Users/ericlloyd/Code/midgard" {
		t.Errorf("Repo = %q, want the canonical header value", f.Repo)
	}
	// There is no Fields member for "goal" at all — the compiler already
	// proves Of cannot assign it anywhere. What's left to assert is that
	// nothing else on f was corrupted by the heading (e.g. Standby flipping
	// true, or Epic getting overwritten by a stray later match).
	if f.Standby {
		t.Error("Standby = true, want false: no standby: line exists in this fixture")
	}
	if f.Epic != "midgard-nsza" {
		t.Errorf("Epic = %q, want unpoisoned header value %q", f.Epic, "midgard-nsza")
	}
}

// TestOf_ListDashPrefixedFieldLinePopulatesNothing covers the census's
// third non-canonical shape: a bulleted "- Repo: ..." briefing line. Under
// today's parseDescriptionFields this line's key is "- repo" (the dash and
// space survive TrimSpace), which happens not to collide with "repo" either
// — this is, verbatim, the one-character reason at-ig53 escaped the exact
// bug that hit at-jno7 (see TestOf_JNO7PoisonedRepoResolvesUnpoisoned, whose
// incident line has no such prefix). Of must resolve Repo to the canonical
// header value regardless.
func TestOf_ListDashPrefixedFieldLinePopulatesNothing(t *testing.T) {
	f := initiative.Of(bd.Issue{ID: "at-ig53", Description: ig53Description})

	if f.Repo != "/Users/erlloyd/Code/agent-teams" {
		t.Errorf("Repo = %q, want canonical header value %q", f.Repo, "/Users/erlloyd/Code/agent-teams")
	}
	if f.Problem == "" || f.Worktree == "" || f.Branch == "" || f.Team == "" || f.Mode == "" || f.Epic != "agent-teams-0yd3" {
		t.Fatalf("canonical header fields did not parse: %+v", f)
	}
}

// TestOf_DuplicateCanonicalKeyResolvesToFirst is frozen item 1's core rule,
// stated directly: a single-valued key that appears twice resolves to the
// FIRST occurrence, not the last. This is what makes at-jno7's incident
// structurally impossible under the new rule even setting case aside — but
// case is still the thing that hid it from the first-wins rule dispatch.go
// already had, which is why frozen item 1 also insists on no case folding.
func TestOf_DuplicateCanonicalKeyResolvesToFirst(t *testing.T) {
	desc := "problem: real work\n" +
		"repo: /correct/repo\n" +
		"worktree: /w\n" +
		"branch: b\n" +
		"team: t\n" +
		"mode: bg\n" +
		"\n" +
		"Some prose paragraph in between the header and the redefinition below.\n" +
		"\n" +
		"repo: /wrong/repo\n"

	f := initiative.Of(bd.Issue{ID: "at-dup1", Description: desc})

	if f.Repo != "/correct/repo" {
		t.Errorf("Repo = %q, want first occurrence %q (last-wins would give %q)", f.Repo, "/correct/repo", "/wrong/repo")
	}
}

// TestOf_TrailingWhitespaceIsRightTrimmed is the DRI ruling's required test
// (bead agent-teams-ully.5 comment, 2026-07-29 02:04): "repo: /a/b" and
// "repo: /a/b   " (trailing spaces) must resolve to the identical value
// "/a/b". A whitespace-only value (empty once right-trimmed) must not be
// treated as a field line at all.
func TestOf_TrailingWhitespaceIsRightTrimmed(t *testing.T) {
	desc := "problem: p\n" +
		"repo: /a/b   \n" +
		"worktree: /w\t\n" +
		"branch: b\n" +
		"team: t\n" +
		"mode: bg\n" +
		"standby:    \n" // whitespace-only value: not a field line, Standby stays false

	f := initiative.Of(bd.Issue{ID: "at-trailingws", Description: desc})

	if f.Repo != "/a/b" {
		t.Errorf(`Repo = %q, want "/a/b" (trailing spaces right-trimmed)`, f.Repo)
	}
	if f.Worktree != "/w" {
		t.Errorf(`Worktree = %q, want "/w" (trailing tab right-trimmed)`, f.Worktree)
	}
	if f.Standby {
		t.Error("Standby = true, want false: a whitespace-only value is not a field line")
	}
}

// TestOf_SessionTieBelowArbitrarilyLongProseBodyIsStillFound guards the
// append-open-tail invariant (frozen item 2, doc.go's "THE TRAP"): a session
// tie appended below an arbitrarily long prose body must still resolve. This
// test PASSES today by design — Of must never be changed in a way that
// makes it start failing (e.g. bounding the scan at the first blank line or
// at some fixed header size), because that would silently classify every
// live initiative as dead.
func TestOf_SessionTieBelowArbitrarilyLongProseBodyIsStillFound(t *testing.T) {
	var body strings.Builder
	body.WriteString("problem: long body test\n")
	body.WriteString("repo: /r\n")
	body.WriteString("worktree: /w\n")
	body.WriteString("branch: b\n")
	body.WriteString("team: t\n")
	body.WriteString("mode: bg\n\n")

	// Real prose (at-y7l9's CURRENT STATE paragraph), repeated to build an
	// arbitrarily long body rather than one paragraph's worth.
	paragraph := "CURRENT STATE (the problem): Products use different, siloed classification systems. BOP uses one system; SpecialtyGL and SpecialtyProperty use another — a product-level industry_group selection that drives ISO GL/Property class_code options via a FRONTEND catalog (not Socotra config). There is no single classification a user picks regardless of product.\n\n"
	for i := 0; i < 200; i++ {
		body.WriteString(paragraph)
	}
	body.WriteString("session: sess-tied-at-the-bottom\n")

	f := initiative.Of(bd.Issue{ID: "at-longbody", Description: body.String()})

	if len(f.Sessions) != 1 || f.Sessions[0] != "sess-tied-at-the-bottom" {
		t.Fatalf("Sessions = %v, want [sess-tied-at-the-bottom] found below a %d-byte prose body", f.Sessions, body.Len())
	}
}

// TestWithSession_PreservesUnmodeledCanonicalKeysByteIdentical is the DRI
// amendment's required test (bead agent-teams-ully.5 comment, frozen items
// 3-4): using at-dxm5's real pr-number/pr-repo/pr-url trio — canonical keys
// with no Fields member — apply a session-tie append and assert all three
// survive byte-identical. New (fresh-compose) would drop them; WithSession
// (append-only) must not.
func TestWithSession_PreservesUnmodeledCanonicalKeysByteIdentical(t *testing.T) {
	iss := bd.Issue{ID: "at-dxm5", Description: dxm5Description}

	plan, err := initiative.WithSession(iss, "sess-abc123")
	if err != nil {
		t.Fatalf("WithSession: %v", err)
	}

	if !strings.HasPrefix(plan.Description, dxm5Description) {
		t.Fatalf("WithSession did not append below the untouched original description.\noriginal:\n%s\ngot:\n%s", dxm5Description, plan.Description)
	}
	for _, unmodeled := range []string{
		"pr-number: 4526",
		"pr-repo: MGT-Insurance/midgard",
		"pr-url: https://github.com/MGT-Insurance/midgard/pull/4526",
	} {
		if !strings.Contains(plan.Description, unmodeled) {
			t.Errorf("unmodeled canonical key line %q did not survive byte-identical:\n%s", unmodeled, plan.Description)
		}
	}
	if !strings.Contains(plan.Description, "session: sess-abc123") {
		t.Errorf("session tie line missing from result:\n%s", plan.Description)
	}

	// Modeled fields must also still resolve correctly off the new
	// description — the append must not have disturbed first-wins for any
	// of them either.
	got := initiative.Of(bd.Issue{ID: "at-dxm5", Description: plan.Description})
	if got.Repo != "/Users/ericlloyd/Code/midgard" {
		t.Errorf("Repo after append = %q, want unchanged canonical value", got.Repo)
	}
	if len(got.Sessions) != 1 || got.Sessions[0] != "sess-abc123" {
		t.Errorf("Sessions after append = %v, want [sess-abc123]", got.Sessions)
	}
}
