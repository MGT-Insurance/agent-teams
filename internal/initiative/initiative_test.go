package initiative_test

import (
	"reflect"
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
// (at the real position, under "## Pointers") the actual poisoned line that
// caused the incident this package exists to fix. This is a REAL captured
// incident line from at-jno7 (2026-07-28), not a synthetic case, verified
// byte-exact by team-lead against raw `bd list --json` output plus `od -c`
// (not `ateam show`'s rendered output, which strips markdown backticks and
// would silently produce a lossy copy of this fixture):
//
//	Repo: `/Users/erlloyd/Code/agent-teams`
//
// Two mechanisms combine to make this line dangerous:
//
//  1. Capital "R" — under today's parseDescriptionFields
//     (internal/verbs/route_match.go:29-42), key :=
//     strings.ToLower(strings.TrimSpace(line[:colon])) folds "Repo" to
//     "repo", so this line collides with the canonical "repo:" header line
//     and, being case-insensitive/last-wins, overwrites it.
//  2. The value is wrapped in backticks — strings.TrimSpace never strips
//     backticks, so the value that wins is the literal string
//     "`/Users/erlloyd/Code/agent-teams`", not a real filesystem path. That
//     is what made the collision POISON rather than a harmless duplicate:
//     the backtick-wrapped string fails gitRepoMatches
//     (internal/verbs/routing_ownership.go), so the initiative resolved to
//     no local claimant at all.
//
// Nothing about at-jno7 has been hand-patched — this line is still live and
// still poisoned today. Under this package's fieldLine (frozen item 1,
// exact-lowercase key, no case folding), "Repo: ..." is structurally
// invisible regardless of the backticks: capital R alone already fails
// keyPattern, so Of never even reaches the value.
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

Repo: ` + "`" + `/Users/erlloyd/Code/agent-teams` + "`" + `
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

// TestOf_JNO7PoisonedRepoResolvesUnpoisoned is bead .6's required test 1,
// using the real captured incident line (see the jno7Description doc
// comment): "Repo: `/Users/erlloyd/Code/agent-teams`" — capital R,
// backtick-wrapped value. Under today's parseDescriptionFields this line
// DOES win (case-insensitive, last-wins, and TrimSpace never strips the
// backticks) — asserted below via parseDescriptionFieldsLenient against the
// exact poisoned value, so this fixture genuinely witnesses the incident
// rather than a shape that was never actually dangerous. Of must resolve to
// the canonical, un-backticked header value regardless, because "Repo"
// fails frozen item 1's no-case-folding rule and is structurally invisible
// to fieldLine.
func TestOf_JNO7PoisonedRepoResolvesUnpoisoned(t *testing.T) {
	const canonicalRepo = "/Users/erlloyd/Code/agent-teams"
	const poisonedRepo = "`/Users/erlloyd/Code/agent-teams`"

	lenient := parseDescriptionFieldsLenient(jno7Description)
	if lenient["repo"] != poisonedRepo {
		t.Fatalf("fixture does not witness the incident: today's lenient reader resolves repo to %q, want the poisoned value %q — this test would be a straw man", lenient["repo"], poisonedRepo)
	}

	f := initiative.Of(bd.Issue{ID: "at-jno7", Description: jno7Description})

	if f.Repo != canonicalRepo {
		t.Errorf("Repo = %q, want unpoisoned canonical header value %q (poisoned value was %q)", f.Repo, canonicalRepo, poisonedRepo)
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

// TestOf_TabAsFirstValueCharacterIsRejected is team-lead review 2026-07-29,
// divergence 3: fieldLine's original guard only checked for a literal second
// space (rest[1] == ' '), so "repo:\t/tabbed" wrongly passed as a field
// line with value "/tabbed". The TS mirror requires \S as the first value
// character — ANY whitespace immediately after the mandatory single space
// must reject the line, not just another space.
func TestOf_TabAsFirstValueCharacterIsRejected(t *testing.T) {
	desc := "problem: p\n" +
		"repo: \t/tabbed\n" +
		"worktree: /w\n" +
		"branch: b\n" +
		"team: t\n" +
		"mode: bg\n"

	f := initiative.Of(bd.Issue{ID: "at-tabfirst", Description: desc})

	if f.Repo != "" {
		t.Errorf(`Repo = %q, want "" (a tab as the first value character is not a field line)`, f.Repo)
	}
}

// TestOf_NonKeyCharsetLinesPopulateNothing is team-lead review 2026-07-29,
// divergence 4: fieldLine's key check must match keyPattern
// (^[a-z][a-z0-9-]*$, TS parity), not merely "all-lowercase with no
// internal space." Each of these lines is all-lowercase and colon-shaped
// but must not read as a field line: a path-like prose key, an underscore
// (TS parity forbids '_', only hyphen is allowed), and a non-ASCII letter.
func TestOf_NonKeyCharsetLinesPopulateNothing(t *testing.T) {
	desc := "problem: p\n" +
		"repo: /r\n" +
		"worktree: /w\n" +
		"branch: b\n" +
		"team: t\n" +
		"mode: bg\n" +
		"./scripts/build.sh: rebuilds the binaries\n" +
		"a_b: underscore is not in keyPattern\n" +
		"répo: non-ascii key\n"

	f := initiative.Of(bd.Issue{ID: "at-charset", Description: desc})

	if f.Repo != "/r" {
		t.Errorf("Repo = %q, want %q (none of the prose lines should have overwritten it)", f.Repo, "/r")
	}
}

// TestOf_CRLFLineBreaksAreNormalized is team-lead review 2026-07-29,
// divergence 2: a description using "\r\n" line breaks must parse
// identically to one using "\n" — splitLines must split on \r?\n, not just
// \n, so a field line's value never ends up with a trailing "\r" folded
// into it by the old plain strings.Split(text, "\n").
func TestOf_CRLFLineBreaksAreNormalized(t *testing.T) {
	desc := "problem: p\r\n" +
		"repo: /crlf/repo\r\n" +
		"worktree: /w\r\n" +
		"branch: b\r\n" +
		"team: t\r\n" +
		"mode: bg\r\n" +
		"session: sess-1\r\n"

	f := initiative.Of(bd.Issue{ID: "at-crlf", Description: desc})

	if f.Repo != "/crlf/repo" {
		t.Errorf("Repo = %q, want %q (no trailing carriage return)", f.Repo, "/crlf/repo")
	}
	if len(f.Sessions) != 1 || f.Sessions[0] != "sess-1" {
		t.Errorf("Sessions = %v, want [sess-1]", f.Sessions)
	}
}

// TestWithTrack_RejectsEdgeWhitespace is team-lead review 2026-07-29,
// finding 6: once Of right-trims a value on read, an appended path with
// leading or trailing whitespace would never read back equal to what the
// caller passed in, so WithTrack's own idempotency check would never find
// it and would re-append it on every call. WithTrack rejects rather than
// silently trims — team-lead's stated preference: "it surfaces the caller's
// bug instead of quietly rewriting their argument."
func TestWithTrack_RejectsEdgeWhitespace(t *testing.T) {
	iss := bd.Issue{ID: "at-track1", Description: "problem: p\nrepo: /r\n"}

	for _, path := range []string{" /leading", "/trailing ", "\t/tabbed-leading", "/tabbed-trailing\t"} {
		if _, err := initiative.WithTrack(iss, path); err == nil {
			t.Errorf("WithTrack(%q): got nil error, want a rejection for edge whitespace", path)
		}
	}

	// A path with only INTERIOR whitespace is still allowed.
	plan, err := initiative.WithTrack(iss, "/has interior space/ok")
	if err != nil {
		t.Fatalf("WithTrack with interior space only: %v", err)
	}
	if !strings.Contains(plan.Description, "track-worktree: /has interior space/ok") {
		t.Errorf("track line missing from result:\n%s", plan.Description)
	}
}

// TestWithPR_AppendsAndIsIdempotent is WithPR's core-path test, mirroring
// WithTrack/WithSession: appends below the untouched existing description,
// resolves back through Of, and a repeat call with the same url is a no-op
// (agent-teams-ssib.6).
func TestWithPR_AppendsAndIsIdempotent(t *testing.T) {
	iss := bd.Issue{ID: "at-pr1", Description: "problem: p\nrepo: /r\n"}

	plan, err := initiative.WithPR(iss, "https://github.com/erlloyd/pr-shepherd/pull/3")
	if err != nil {
		t.Fatalf("WithPR: %v", err)
	}
	if !strings.HasPrefix(plan.Description, iss.Description) {
		t.Fatalf("WithPR did not append below the untouched original description.\noriginal:\n%s\ngot:\n%s", iss.Description, plan.Description)
	}
	got := initiative.Of(bd.Issue{ID: "at-pr1", Description: plan.Description})
	if len(got.PRs) != 1 || got.PRs[0] != "https://github.com/erlloyd/pr-shepherd/pull/3" {
		t.Fatalf("PRs after WithPR = %v, want one entry", got.PRs)
	}

	// Idempotent: appending the same url again changes nothing.
	iss2 := bd.Issue{ID: "at-pr1", Description: plan.Description}
	plan2, err := initiative.WithPR(iss2, "https://github.com/erlloyd/pr-shepherd/pull/3")
	if err != nil {
		t.Fatalf("WithPR (repeat): %v", err)
	}
	if plan2.Description != iss2.Description {
		t.Errorf("WithPR repeat call was not a no-op:\nbefore:\n%s\nafter:\n%s", iss2.Description, plan2.Description)
	}
}

// TestOf_MultiplePRsAccumulateInRegistrationOrder proves "pr" is multi-valued
// (accumulates) rather than first-wins, and proves it the way the keystone
// mandates: this test is a witness, not decoration. It genuinely fails if
// "pr" is removed from multiValuedKeys (confirmed by hand during
// implementation: reverting that one-line change turns this red, restoring
// it turns this green again) — this is the discriminator that must survive
// the at-d9ck two-repo case (agent-teams-ssib.6).
func TestOf_MultiplePRsAccumulateInRegistrationOrder(t *testing.T) {
	got := initiative.Of(bd.Issue{ID: "at-d9ck", Description: "repo: /r\n" +
		"pr: https://github.com/erlloyd/pr-shepherd/pull/3\n" +
		"pr: https://github.com/MGT-Insurance/midgard/pull/4632\n",
	})
	want := []string{
		"https://github.com/erlloyd/pr-shepherd/pull/3",
		"https://github.com/MGT-Insurance/midgard/pull/4632",
	}
	if !reflect.DeepEqual(got.PRs, want) {
		t.Errorf("PRs = %v, want %v (registration order, both retained)", got.PRs, want)
	}
}

// TestWithPR_RejectsEdgeWhitespace mirrors TestWithTrack_RejectsEdgeWhitespace:
// an edge-whitespace url would never read back equal to what the caller
// passed in (fieldLine right-trims and requires a non-whitespace first
// value character), so WithPR's own idempotency check would never find it
// and would keep re-appending it forever. Reject rather than silently trim.
func TestWithPR_RejectsEdgeWhitespace(t *testing.T) {
	iss := bd.Issue{ID: "at-pr2", Description: "problem: p\nrepo: /r\n"}

	for _, url := range []string{" https://x/y", "https://x/y ", "\thttps://x/y", "https://x/y\t"} {
		if _, err := initiative.WithPR(iss, url); err == nil {
			t.Errorf("WithPR(%q): got nil error, want a rejection for edge whitespace", url)
		}
	}
}

// TestResolvedPRs_FallsBackToNotesOnlyPR is the exact case the DRI review
// (team-lead, on top of agent-teams-ssib.6) demanded a witness for: 178 of
// 549 registered initiatives recorded their PR by writing "pr: <url>" into
// bd NOTES via the dri skill (plugins/agent-teams/skills/dri/SKILL.md) —
// never into Description, so Of(iss).PRs is empty for every one of them.
// Without this fallback, execution-status/list-json/route_match.go would
// silently report zero PRs for all 178 the moment they start reading the
// rail — worse than the bug this initiative set out to fix. ResolvedPRs
// must still resolve the PR when it lives ONLY in Notes.
func TestResolvedPRs_FallsBackToNotesOnlyPR(t *testing.T) {
	iss := bd.Issue{
		ID:          "at-notesonly",
		Description: "problem: p\nrepo: /r\n", // no "pr:" line at all
		Notes:       "delivered, ready for review.\npr: https://github.com/erlloyd/pr-shepherd/pull/3\n",
	}
	got := initiative.ResolvedPRs(iss)
	want := []string{"https://github.com/erlloyd/pr-shepherd/pull/3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolvedPRs (Notes-only PR) = %v, want %v", got, want)
	}
}

// TestResolvedPRs_RailWinsWholesaleOverNotes proves the rail is never
// unioned with the Notes/Description fallback: when the rail has ANY
// entries, Notes is not consulted at all, even if Notes also carries a
// (possibly stale, possibly different) "pr:" line.
func TestResolvedPRs_RailWinsWholesaleOverNotes(t *testing.T) {
	iss := bd.Issue{
		ID:          "at-railwins",
		Description: "repo: /r\npr: https://github.com/erlloyd/pr-shepherd/pull/3\n",
		Notes:       "pr: https://github.com/some/other/pull/999\n",
	}
	got := initiative.ResolvedPRs(iss)
	want := []string{"https://github.com/erlloyd/pr-shepherd/pull/3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolvedPRs (rail present) = %v, want %v (rail only, Notes ignored)", got, want)
	}
}

// TestResolvedPRs_FallsBackToDescriptionFreeTextWhenNotesEmpty mirrors the
// pre-existing extractPrURL order (Notes checked first, then Description)
// for the case where the PR URL sits in Description prose rather than as a
// canonical "pr:" rail line — e.g. an old initiative whose PR link was
// pasted into free text, not recorded via either mechanism.
func TestResolvedPRs_FallsBackToDescriptionFreeTextWhenNotesEmpty(t *testing.T) {
	iss := bd.Issue{
		ID:          "at-descfallback",
		Description: "problem: p\nSee https://github.com/acme/widget/pull/12 for the PR.\n",
		Notes:       "",
	}
	got := initiative.ResolvedPRs(iss)
	want := []string{"https://github.com/acme/widget/pull/12"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolvedPRs (Description free-text fallback) = %v, want %v", got, want)
	}
}

// TestResolvedPRs_NilWhenNothingFound covers the no-PR-anywhere case.
func TestResolvedPRs_NilWhenNothingFound(t *testing.T) {
	iss := bd.Issue{ID: "at-nopr", Description: "problem: p\n", Notes: "no PR yet.\n"}
	if got := initiative.ResolvedPRs(iss); got != nil {
		t.Errorf("ResolvedPRs (nothing found) = %v, want nil", got)
	}
}

// TestNew_ComposesHeaderInFixedOrder is New's core-path test: given a
// populated Fields, the composed description has one "key: value" line per
// non-empty field, each line individually resolvable via Of.
func TestNew_ComposesHeaderInFixedOrder(t *testing.T) {
	plan, err := initiative.New(initiative.Fields{
		Problem:  "fix the thing",
		Repo:     "/r",
		Worktree: "/w",
		Branch:   "b",
		Team:     "t",
		Mode:     "bg",
		Epic:     "epic-1",
		Standby:  true,
		Sessions: []string{"sess-1"},
		Tracks:   []string{"/track-1"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got := initiative.Of(bd.Issue{ID: "at-new1", Description: plan.Description})
	if got.Problem != "fix the thing" || got.Repo != "/r" || got.Worktree != "/w" || got.Branch != "b" || got.Team != "t" || got.Mode != "bg" || got.Epic != "epic-1" {
		t.Fatalf("round-trip through Of did not preserve composed fields: %+v", got)
	}
	if !got.Standby {
		t.Error("Standby = false, want true")
	}
	if len(got.Sessions) != 1 || got.Sessions[0] != "sess-1" {
		t.Errorf("Sessions = %v, want [sess-1]", got.Sessions)
	}
	if len(got.Tracks) != 1 || got.Tracks[0] != "/track-1" {
		t.Errorf("Tracks = %v, want [/track-1]", got.Tracks)
	}
}

// TestNew_RejectsLineBreakInValue is team-lead review 2026-07-29, finding 7:
// a newline in any field value would inject a bogus line into the composed
// description that, being written earlier than a later canonical field
// (Problem is written first), would win under first-wins when Of reads it
// back. New must reject this outright rather than compose broken output.
// Explicitly requested test ("Reject a newline in any value, and add the
// test").
func TestNew_RejectsLineBreakInValue(t *testing.T) {
	_, err := initiative.New(initiative.Fields{
		Problem: "legit text\nrepo: /evil/injected",
		Repo:    "/real/repo",
	})
	if err == nil {
		t.Fatal("New: got nil error, want a rejection for a newline embedded in Problem")
	}
}
