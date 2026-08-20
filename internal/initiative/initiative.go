package initiative

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

// Fields is the typed view of the routing data modeled out of an
// initiative's bd description. It does not enumerate every canonical key —
// see the package doc comment, frozen item 3: an unmodeled key (e.g.
// pr-number) is legitimate data this struct simply has no member for.
type Fields struct {
	Problem, Repo, Worktree, Branch, Team, Mode, Runtime, Epic string
	Standby                                                    bool
	Sessions                                                   []string
	Tracks                                                     []string
	// PRs is every "pr: <url>" line's value, in registration order — the
	// GitHub PR URLs a DRI has opened for this initiative. Multi-valued
	// because one initiative can open more than one PR (agent-teams-ssib).
	// Distinct from the review-pr initiative kind's unmodeled pr-number /
	// pr-repo / pr-url trio (frozen item 3): those describe the single PR a
	// REVIEW initiative is reviewing; PRs describes the PR(s) a DRI
	// initiative has opened. Same URL shape, different key, different
	// initiative kind — no collision.
	PRs []string
	// PRWorkstreams is every well-formed "pr-workstream" association in
	// persisted order. Malformed legacy values remain available through
	// JSONFields but are deliberately excluded from this typed projection.
	PRWorkstreams []PRWorkstream
}

// PRWorkstream durably associates one canonical GitHub PR URL with the
// project Bead whose card owns it.
type PRWorkstream struct {
	PR         string `json:"pr"`
	Workstream string `json:"workstream"`
}

// WritePlan is the result of composing or extending a description. Today
// only Description is populated; Labels is reserved for a future labels
// backend (see the package doc comment, "package surface") and is always
// empty.
type WritePlan struct {
	Description string
	Labels      []string
}

// Collision is one line in a body of prose that would redefine a routing
// field already set on the Fields it was checked against.
type Collision struct {
	Line int    // 1-based line number within the checked text
	Key  string // the routing field key the line redefines
	Text string // the offending line, verbatim
}

// singleValuedKeys lists every canonical key backed by a single-valued
// Fields member (i.e. excluding the multi-valued "session",
// "track-worktree", and "pr" keys). New does not iterate this slice — it
// writes each field individually in its own fixed order — so this is a set,
// not an ordering claim.
var singleValuedKeys = []string{"problem", "repo", "worktree", "branch", "team", "mode", "runtime", "standby", "epic"}

// multiValuedKeys lists the canonical keys that accumulate rather than
// first-wins (frozen item 1). Every other key — modeled or not — is
// single-valued.
var multiValuedKeys = []string{"session", "track-worktree", "pr", "pr-workstream"}

// multiValued reports whether key accumulates instead of first-wins.
func multiValued(key string) bool {
	for _, k := range multiValuedKeys {
		if k == key {
			return true
		}
	}
	return false
}

// keyPattern is the character class a field-line key must match: a lowercase
// letter, then any number of lowercase letters, digits, or hyphens. This
// mirrors the merged TS mirror's ^[a-z][a-z0-9-]*$ (dashboard/server, parity
// reference per team-lead review 2026-07-29). It is deliberately narrower
// than "no case folding" alone — an ordinary prose line like
// "./scripts/build.sh: rebuilds the binaries" must not read as a field line
// just because it happens to be all-lowercase with no internal space before
// the colon.
var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// splitLines splits text on line breaks, treating "\r\n" and "\n" as
// equivalent (team-lead review 2026-07-29: "split on \r?\n anyway so the
// intent is explicit rather than incidental"). A bd description observed in
// practice is LF-only, but a value's trailing right-trim already absorbs a
// stray "\r" — this makes that intentional rather than incidental.
func splitLines(text string) []string {
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}

// fieldLine reports whether line satisfies the one frozen field-line rule
// (package doc comment, frozen item 1): start of line, an exact-lowercase
// key restricted to keyPattern, a single colon, a single space, then a
// non-empty value. Any other shape — leading whitespace, a folded-case or
// out-of-pattern key, a missing or doubled space after the colon, a value
// that is empty or entirely whitespace — is not a field line at all and
// returns ok=false. This is the single primitive every reader and writer in
// this package builds on; there is no second matching rule anywhere else.
//
// The captured value is right-trimmed (DRI ruling on agent-teams-ully.5,
// 2026-07-29 02:04): "repo: /a/b" and "repo: /a/b   " resolve to the
// identical value "/a/b". Leading whitespace in the value is still
// structurally excluded — the first value character must be non-whitespace
// (any whitespace, not just a second literal space; team-lead review
// 2026-07-29 caught a tab passing the old space-only check). A value that is
// entirely whitespace right-trims to empty and is therefore not a field line
// either, per the same ruling.
func fieldLine(line string) (key, value string, ok bool) {
	colon := strings.IndexByte(line, ':')
	if colon <= 0 {
		return "", "", false
	}
	key = line[:colon]
	if !keyPattern.MatchString(key) {
		return "", "", false
	}
	rest := line[colon+1:]
	if len(rest) < 2 || rest[0] != ' ' {
		return "", "", false
	}
	valuePart := rest[1:]
	firstRune, _ := utf8.DecodeRuneInString(valuePart)
	if unicode.IsSpace(firstRune) {
		return "", "", false
	}
	value = strings.TrimRightFunc(valuePart, unicode.IsSpace)
	if value == "" {
		return "", "", false
	}
	return key, value, true
}

// all returns every canonical field line in iss, keyed by canonical key, with
// each key's values in the order they occur. It is the ONE accumulation of the
// frozen rule in this package: Of projects a typed view out of it and
// JSONFields projects a wire view out of it, so neither reader carries its own
// scanner. A third reader must project from all as well rather than add a
// second loop over splitLines.
//
// all scans the ENTIRE description, not just a leading header block — a field
// line (most commonly a session tie) can appear arbitrarily far down an
// arbitrarily long prose body (package doc comment, frozen item 2); it must
// never be changed to stop early.
//
// all keeps keys with no Fields member. An unmodeled canonical key is
// legitimate data, not malformed input (frozen item 3), so the shared scan is
// the wrong place to decide which keys matter — that is each projection's
// call.
func all(iss bd.Issue) map[string][]string {
	out := make(map[string][]string)
	for _, line := range splitLines(iss.Description) {
		key, value, ok := fieldLine(line)
		if !ok {
			continue
		}
		if !multiValued(key) && len(out[key]) > 0 {
			continue // first occurrence wins
		}
		out[key] = append(out[key], value)
	}
	return out
}

// first returns the winning value for a single-valued key, or "" when absent.
func first(lines map[string][]string, key string) string {
	if v := lines[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// parsePRWorkstream parses the exact persisted association grammar:
//
//	<canonical-github-pr-url><one ASCII space><whitespace-free-bead-id>
//
// It intentionally rejects rather than repairs legacy near-misses. Their raw
// values still survive in JSONFields; only the typed association rail is
// filtered.
func parsePRWorkstream(value string) (PRWorkstream, bool) {
	pr, workstream, ok := strings.Cut(value, " ")
	if !ok || pr == "" || workstream == "" || strings.Contains(workstream, " ") {
		return PRWorkstream{}, false
	}
	if strings.IndexFunc(pr, unicode.IsSpace) >= 0 || strings.IndexFunc(workstream, unicode.IsSpace) >= 0 {
		return PRWorkstream{}, false
	}
	canonical, ok := CanonicalPRURL(pr)
	if !ok || canonical != pr {
		return PRWorkstream{}, false
	}
	return PRWorkstream{PR: pr, Workstream: workstream}, true
}

// PRWorkstreams returns valid durable PR-to-workstream associations in their
// persisted order. Invalid legacy lines are omitted without changing the raw
// routing-fields projection.
func PRWorkstreams(iss bd.Issue) []PRWorkstream {
	return parsePRWorkstreamValues(all(iss)["pr-workstream"])
}

func parsePRWorkstreamValues(values []string) []PRWorkstream {
	out := make([]PRWorkstream, 0, len(values))
	for _, value := range values {
		if association, ok := parsePRWorkstream(value); ok {
			out = append(out, association)
		}
	}
	return out
}

// Of parses iss's routing fields per the frozen rule. Single-valued keys
// (problem, repo, worktree, branch, team, mode, runtime, epic, standby) are
// first-occurrence-wins; the multi-valued session, track-worktree, pr, and
// pr-workstream keys accumulate in registration order. Malformed
// pr-workstream values are retained by JSONFields but excluded here.
//
// Of is the TYPED projection of all, and models thirteen keys only — an
// initiative carrying an unmodeled canonical key (e.g. pr-url) has no Fields
// member to hold it, so a caller that needs every stored key wants
// JSONFields instead (package doc comment, frozen item 3).
//
// Of takes the whole bd.Issue, not iss.Description, so that a future labels
// backend can read iss.Labels instead without changing this signature or any
// caller (package doc comment, "package surface").
func Of(iss bd.Issue) Fields {
	lines := all(iss)
	return Fields{
		Problem:       first(lines, "problem"),
		Repo:          first(lines, "repo"),
		Worktree:      first(lines, "worktree"),
		Branch:        first(lines, "branch"),
		Team:          first(lines, "team"),
		Mode:          first(lines, "mode"),
		Runtime:       first(lines, "runtime"),
		Epic:          first(lines, "epic"),
		Standby:       first(lines, "standby") == "true",
		Sessions:      lines["session"],
		Tracks:        lines["track-worktree"],
		PRs:           lines["pr"],
		PRWorkstreams: parsePRWorkstreamValues(lines["pr-workstream"]),
	}
}

// JSONFields is the WIRE projection of all: iss's routing data as a map ready
// to marshal into JSON for a consumer outside this process (today, the
// "fields" object `ateam list-json` adds to every element, which the
// TypeScript dashboard reads instead of re-implementing the frozen rule).
//
// Every key is the canonical LINE key, verbatim and unrenamed — "session" and
// "track-worktree", not "sessions" and "tracks". That is the whole design:
// a key's name on the wire never depends on whether Go happens to model it, so
// giving pr-url a Fields member later, or a skill inventing a new key with no
// Go change at all (frozen item 3), changes nothing for any consumer. A shape
// that hoisted the modeled keys and swept the rest into a nested bag would
// have the opposite property — modeling a key would MOVE it and break readers.
//
// Types: the multi-valued keys are always arrays; standby is a bool
// (true only for the exact value "true", matching Of); every other key is its
// value string. An absent key is OMITTED rather than emitted empty, so a
// consumer can tell "not set" from "set to something empty" — the frozen rule
// admits no empty value, so an emitted key always has real data behind it.
func JSONFields(iss bd.Issue) map[string]any {
	out := make(map[string]any)
	for key, values := range all(iss) {
		switch {
		case multiValued(key):
			out[key] = values
		case key == "standby":
			out[key] = values[0] == "true"
		default:
			out[key] = values[0]
		}
	}
	return out
}

// New composes a fresh description from f, for a brand-new initiative ONLY.
// New must never be used to rewrite an existing initiative's description —
// every unmodeled canonical key already on record would be silently dropped,
// reintroducing the exact data loss this package exists to eliminate
// (package doc comment, frozen item 4). To mutate an existing initiative use
// WithSession or WithTrack instead.
//
// New rejects (does not silently sanitize) any field value containing a line
// break. Problem in particular is free-form human text arriving from a
// dispatch argument — a newline in it is not exotic — and every value here
// is spliced into the description at column 0. An unrejected newline in an
// early-written field (e.g. Problem, written first) would inject a line that
// LOOKS like a later canonical field and, under first-wins, would win over
// the real one (team-lead review 2026-07-29, finding 7). This is a signature
// change from the frozen package surface (doc.go / bead agent-teams-ully.5
// body item 3, "func New(f Fields) WritePlan") — flagged, not made
// unilaterally; see the session report.
func New(f Fields) (WritePlan, error) {
	associationValues := make([]string, 0, len(f.PRWorkstreams))
	associationByPR := make(map[string]string, len(f.PRWorkstreams))
	for _, association := range f.PRWorkstreams {
		canonical, ok := CanonicalPRURL(association.PR)
		if !ok {
			return WritePlan{}, fmt.Errorf("initiative.New: pr-workstream PR must be a GitHub PR URL: %q", association.PR)
		}
		if association.Workstream == "" || strings.IndexFunc(association.Workstream, unicode.IsSpace) >= 0 {
			return WritePlan{}, fmt.Errorf("initiative.New: pr-workstream workstream must be a whitespace-free Bead ID: %q", association.Workstream)
		}
		if existing, seen := associationByPR[canonical]; seen {
			if existing != association.Workstream {
				return WritePlan{}, fmt.Errorf("initiative.New: PR %s maps to both %s and %s", canonical, existing, association.Workstream)
			}
			continue
		}
		associationByPR[canonical] = association.Workstream
		associationValues = append(associationValues, canonical+" "+association.Workstream)
	}
	fields := []struct {
		key    string
		values []string
	}{
		{"problem", []string{f.Problem}},
		{"repo", []string{f.Repo}},
		{"worktree", []string{f.Worktree}},
		{"branch", []string{f.Branch}},
		{"team", []string{f.Team}},
		{"mode", []string{f.Mode}},
		{"runtime", []string{f.Runtime}},
		{"epic", []string{f.Epic}},
		{"session", f.Sessions},
		{"track-worktree", f.Tracks},
		{"pr", f.PRs},
		{"pr-workstream", associationValues},
	}
	for _, fv := range fields {
		for _, v := range fv.values {
			if strings.ContainsAny(v, "\r\n") {
				return WritePlan{}, fmt.Errorf("initiative.New: %s value must not contain a line break: %q", fv.key, v)
			}
		}
	}

	var b strings.Builder
	write := func(key, value string) {
		if value == "" {
			return
		}
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteString("\n")
	}
	write("problem", f.Problem)
	write("repo", f.Repo)
	write("worktree", f.Worktree)
	write("branch", f.Branch)
	write("team", f.Team)
	write("mode", f.Mode)
	if f.Standby {
		write("standby", "true")
	}
	write("runtime", f.Runtime)
	write("epic", f.Epic)
	for _, s := range f.Sessions {
		write("session", s)
	}
	for _, t := range f.Tracks {
		write("track-worktree", t)
	}
	for _, u := range f.PRs {
		write("pr", u)
	}
	for _, association := range associationValues {
		write("pr-workstream", association)
	}
	return WritePlan{Description: b.String()}, nil
}

// appendLine returns description with one new "key: value" line appended
// below its entire existing content. It preserves every byte of existing
// content EXCEPT the trailing newline run, which it normalizes to exactly
// one "\n" before appending — so repeated appends stay stable (a
// description ending in three newlines does not grow a fourth on every
// call) rather than re-deriving or dropping any line of substance.
func appendLine(description, key, value string) string {
	return strings.TrimRight(description, "\n") + "\n" + key + ": " + value + "\n"
}

// hasEdgeWhitespace reports whether s has leading or trailing whitespace.
func hasEdgeWhitespace(s string) bool {
	if s == "" {
		return false
	}
	first, _ := utf8.DecodeRuneInString(s)
	last, _ := utf8.DecodeLastRuneInString(s)
	return unicode.IsSpace(first) || unicode.IsSpace(last)
}

// WithSession returns the WritePlan that ties sessionID to iss by appending
// a "session: <id>" line below iss's entire current description. It is
// idempotent: if sessionID is already tied to iss, the returned
// WritePlan.Description is iss.Description, unchanged.
//
// sessionID must be non-empty and contain no whitespace — it is spliced
// verbatim into a "session: <id>" line, and an unvalidated value could
// inject an extra line or corrupt the field parse for a later reader.
//
// WithSession does NOT check whether sessionID is already tied to a
// DIFFERENT open initiative; that guard needs to list every open initiative,
// which requires a live bd client this pure function is not given. It stays
// the caller's responsibility.
func WithSession(iss bd.Issue, sessionID string) (WritePlan, error) {
	if sessionID == "" {
		return WritePlan{}, fmt.Errorf("initiative.WithSession: sessionID must not be empty")
	}
	if strings.ContainsAny(sessionID, " \t\r\n") {
		return WritePlan{}, fmt.Errorf("initiative.WithSession: sessionID must not contain whitespace: %q", sessionID)
	}
	for _, existing := range Of(iss).Sessions {
		if existing == sessionID {
			return WritePlan{Description: iss.Description}, nil
		}
	}
	return WritePlan{Description: appendLine(iss.Description, "session", sessionID)}, nil
}

// WithTrack returns the WritePlan that registers path as a track-worktree of
// iss by appending a "track-worktree: <path>" line below iss's entire
// current description. It is idempotent: if path is already registered on
// iss, the returned WritePlan.Description is iss.Description, unchanged.
//
// path must be non-empty and contain no newline or carriage return — those
// would inject an extra line into the description. Unlike a session id, a
// worktree path may legitimately contain interior spaces, so those are not
// rejected — but a LEADING or TRAILING whitespace character is: fieldLine
// right-trims a value on read (package doc comment via the frozen matching
// rule) and requires a non-whitespace first character, so an edge-whitespace
// path appended verbatim would never read back equal to the path the caller
// passed in — WithTrack's own idempotency check (Of(iss).Tracks) would then
// never find it and would keep re-appending it forever. Team-lead review
// 2026-07-29, finding 6: reject rather than silently trim, since trimming
// would hide the caller's bug instead of surfacing it.
func WithTrack(iss bd.Issue, path string) (WritePlan, error) {
	if path == "" {
		return WritePlan{}, fmt.Errorf("initiative.WithTrack: path must not be empty")
	}
	if strings.ContainsAny(path, "\r\n") {
		return WritePlan{}, fmt.Errorf("initiative.WithTrack: path must not contain a line break: %q", path)
	}
	if hasEdgeWhitespace(path) {
		return WritePlan{}, fmt.Errorf("initiative.WithTrack: path must not have leading or trailing whitespace: %q", path)
	}
	for _, existing := range Of(iss).Tracks {
		if existing == path {
			return WritePlan{Description: iss.Description}, nil
		}
	}
	return WritePlan{Description: appendLine(iss.Description, "track-worktree", path)}, nil
}

// PRURLRE matches a full GitHub PR URL:
//
//	https://github.com/<owner>/<repo>/pull/<number>
//
// Capture groups: [1] owner, [2] repo, [3] number.
//
// This is the ONE Go implementation of "find/parse a GitHub PR URL" — it used
// to have a sibling copy in internal/verbs/route_match.go (prURLRE), byte-
// identical, because internal/verbs already imports internal/initiative and
// importing back would cycle. Exporting this instead of duplicating it lets
// route_match.go's extractPrURL/parsePrURL delegate here, so the pattern has
// exactly one definition again (docs/multi-pr-contract.md, "read
// precedence" — this consolidation was that section's flagged follow-up).
var PRURLRE = regexp.MustCompile(`https?://github\.com/([^/\s]+)/([^/\s]+)/pull/(\d+)`)

// CanonicalPRURL returns the canonical identity form of a GitHub PR URL —
// forced https scheme, lower-cased owner/repo, the number verbatim — or ok
// == false when url doesn't match PRURLRE at all.
//
// This is the ONE canonicalization point (agent-teams-ssib.25): two
// spellings of the same PR (http vs https, differently-cased owner/repo)
// must become byte-identical before they are ever compared, stored on the
// "pr" rail, or embedded in a per-PR label — otherwise the rail dedups by
// exact string match and stores both, and a gate label written with one
// spelling can never be paired with a handoff label written with the other,
// producing a PR that can never reach rest. ResolvedPRs (read side) and
// WithPR (write side) both canonicalize through this function so every
// caller downstream — status.go's gateForPR, query.go's per-PR ask
// tagging, the --pr resolver gate/clear-gate/handoff share — can compare
// PR-identity strings with plain "==" and get the right answer, instead of
// each writing its own case/scheme-insensitive comparator.
func CanonicalPRURL(url string) (string, bool) {
	m := PRURLRE.FindStringSubmatch(url)
	if m == nil {
		return "", false
	}
	return "https://github.com/" + strings.ToLower(m[1]) + "/" + strings.ToLower(m[2]) + "/pull/" + m[3], true
}

// extractPRURLFallback returns the first GitHub PR URL found in text, or ""
// — the free-text scan every initiative's PR was recorded through before
// the "pr" rail existed (and still is, via the dri skill's Notes-based
// "pr:" line — see [ResolvedPRs]).
func extractPRURLFallback(text string) string {
	return PRURLRE.FindString(text)
}

// ResolvedPRs returns iss's PR URLs per the frozen, PERMANENT read
// precedence (docs/multi-pr-contract.md, "read precedence"): the "pr" rail
// (Of(iss).PRs) wins WHOLESALE when non-empty; only when the rail is
// completely empty does this fall back to a single URL extracted from free
// text — Notes first, then Description, matching the pre-existing
// extractPrURL order (internal/verbs/route_match.go). The two sources are
// NEVER unioned — mixing a rail entry with a free-text-scanned one risks
// duplicates and an undefined ordering that nobody could reconstruct later.
//
// This is permanent, not transitional. A repo-wide census at the time this
// was written found "pr:" in a Description line 0 times and in Notes 178
// times across 549 registered initiatives — the DRI skill has always
// written the PR to Notes, never to Description, and Eric's ruling repairs
// only the one still-open multi-PR initiative onto the rail, leaving the
// rest on Notes indefinitely. Every reader of "what PR(s) does this
// initiative have" — execution-status, list-json's resolved sibling key,
// and route_match.go's tier-1 matching — MUST call ResolvedPRs, not
// Of(iss).PRs directly: reading the rail alone silently returns zero PRs
// for every initiative that has never called WithPR, which today is all of
// them. (Of(iss).PRs itself stays a pure rail projection — unchanged — for
// callers that specifically want the rail's own state, e.g. list-json's
// "fields.pr" key.)
//
// Every returned URL is canonicalized (CanonicalPRURL, agent-teams-ssib.25)
// regardless of which source it came from — the rail, Notes, or
// Description — so a caller never has to re-canonicalize or fuzzy-compare
// what this function hands back; a value that doesn't parse as a GitHub PR
// URL at all is returned verbatim (can only happen for a rail entry written
// before this fix shipped, or a caller misusing WithPR directly).
func ResolvedPRs(iss bd.Issue) []string {
	var raw []string
	switch {
	case len(Of(iss).PRs) > 0:
		raw = Of(iss).PRs
	case extractPRURLFallback(iss.Notes) != "":
		raw = []string{extractPRURLFallback(iss.Notes)}
	case extractPRURLFallback(iss.Description) != "":
		raw = []string{extractPRURLFallback(iss.Description)}
	default:
		return nil
	}
	out := make([]string, len(raw))
	for i, url := range raw {
		if canon, ok := CanonicalPRURL(url); ok {
			out[i] = canon
		} else {
			out[i] = url
		}
	}
	return out
}

// WithPR returns the WritePlan that registers url as a pr of iss by
// appending a "pr: <url>" line below iss's entire current description. It is
// idempotent: if url is already registered on iss, the returned
// WritePlan.Description is iss.Description, unchanged.
//
// url must be non-empty and have no leading/trailing whitespace or line
// break, for the identical structural reason WithTrack rejects them on path
// (frozen matching rule: fieldLine right-trims a value on read and requires
// a non-whitespace first character, so an edge-whitespace or multi-line
// value would never read back equal to what the caller passed in, and this
// function's own idempotency check — Of(iss).PRs — would then never find it
// and would keep re-appending it forever).
//
// WithPR does not validate that url is a well-formed GitHub PR URL — that is
// the frozen grammar's job (docs/multi-pr-contract.md), enforced by callers
// (the ateam pr add verb) that have a reason to reject a malformed value.
// This writer, like WithSession and WithTrack, only enforces the structural
// constraints the field-line rule itself imposes.
//
// url is canonicalized (CanonicalPRURL, agent-teams-ssib.25) before both the
// idempotency check and storage, so two spellings of one PR (http vs https,
// differently-cased owner/repo) collapse to a single rail entry instead of
// appending a second, unpairable one. A url that doesn't parse as a GitHub
// PR URL at all is compared and stored verbatim, unchanged from before this
// fix — WithPR still does not enforce the URL shape.
func WithPR(iss bd.Issue, url string) (WritePlan, error) {
	if url == "" {
		return WritePlan{}, fmt.Errorf("initiative.WithPR: url must not be empty")
	}
	if strings.ContainsAny(url, "\r\n") {
		return WritePlan{}, fmt.Errorf("initiative.WithPR: url must not contain a line break: %q", url)
	}
	if hasEdgeWhitespace(url) {
		return WritePlan{}, fmt.Errorf("initiative.WithPR: url must not have leading or trailing whitespace: %q", url)
	}
	stored := url
	if canon, ok := CanonicalPRURL(url); ok {
		stored = canon
	}
	for _, existing := range Of(iss).PRs {
		existingCanon := existing
		if canon, ok := CanonicalPRURL(existing); ok {
			existingCanon = canon
		}
		if existingCanon == stored {
			return WritePlan{Description: iss.Description}, nil
		}
	}
	return WritePlan{Description: appendLine(iss.Description, "pr", stored)}, nil
}

// WithPRWorkstream returns the WritePlan that associates url with workstream.
// URL identity is canonicalized before comparison and storage. Repeating the
// same pair is a no-op; mapping the same canonical PR to another workstream is
// rejected. The caller remains responsible for validating that workstream is
// a descendant of the initiative's project epic before persisting the plan.
func WithPRWorkstream(iss bd.Issue, url, workstream string) (WritePlan, error) {
	canonical, ok := CanonicalPRURL(url)
	if !ok {
		return WritePlan{}, fmt.Errorf("initiative.WithPRWorkstream: url must be a GitHub PR URL: %q", url)
	}
	if workstream == "" {
		return WritePlan{}, fmt.Errorf("initiative.WithPRWorkstream: workstream must not be empty")
	}
	if strings.IndexFunc(workstream, unicode.IsSpace) >= 0 {
		return WritePlan{}, fmt.Errorf("initiative.WithPRWorkstream: workstream must not contain whitespace: %q", workstream)
	}
	existingWorkstream := ""
	for _, existing := range PRWorkstreams(iss) {
		if existing.PR != canonical {
			continue
		}
		if existingWorkstream != "" && existingWorkstream != existing.Workstream {
			return WritePlan{}, fmt.Errorf("initiative.WithPRWorkstream: PR %s already has conflicting persisted workstreams %s and %s", canonical, existingWorkstream, existing.Workstream)
		}
		existingWorkstream = existing.Workstream
	}
	if existingWorkstream == workstream {
		return WritePlan{Description: iss.Description}, nil
	}
	if existingWorkstream != "" {
		return WritePlan{}, fmt.Errorf("initiative.WithPRWorkstream: PR %s is already associated with workstream %s", canonical, existingWorkstream)
	}
	value := canonical + " " + workstream
	return WritePlan{Description: appendLine(iss.Description, "pr-workstream", value)}, nil
}

// singleValued returns the set of single-valued canonical keys f currently
// holds a non-empty value for.
func (f Fields) singleValued() map[string]bool {
	set := make(map[string]bool, len(singleValuedKeys))
	mark := func(key, value string) {
		if value != "" {
			set[key] = true
		}
	}
	mark("problem", f.Problem)
	mark("repo", f.Repo)
	mark("worktree", f.Worktree)
	mark("branch", f.Branch)
	mark("team", f.Team)
	mark("mode", f.Mode)
	mark("runtime", f.Runtime)
	mark("epic", f.Epic)
	if f.Standby {
		set["standby"] = true
	}
	return set
}

// CollisionsIn scans bodyText for lines that would redefine a single-valued
// routing field already set on f, judged by the exact same rule fieldLine
// enforces everywhere in this package (frozen item 1) — a line the strict
// rule ignores (wrong case, list-dash prefixed, missing the space, ...)
// never reports as a collision, even though a lenient reader would have
// silently accepted it.
//
// CollisionsIn is a method on Fields, not a free function: only a caller
// that has already composed f — the header a writer is about to combine
// with bodyText — can invoke it. That is deliberate: the redefinition
// warning lives inside the writer, not bolted onto a call site (package doc
// comment, "package surface").
func (f Fields) CollisionsIn(bodyText string) []Collision {
	already := f.singleValued()
	var out []Collision
	for i, line := range splitLines(bodyText) {
		key, _, ok := fieldLine(line)
		if !ok || !already[key] {
			continue
		}
		out = append(out, Collision{Line: i + 1, Key: key, Text: line})
	}
	return out
}
