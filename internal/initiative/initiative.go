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
	Problem, Repo, Worktree, Branch, Team, Mode, Epic string
	Standby                                           bool
	Sessions                                          []string
	Tracks                                            []string
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
// Fields member (i.e. excluding the multi-valued "session" and
// "track-worktree" keys). New does not iterate this slice — it writes each
// field individually in its own fixed order — so this is a set, not an
// ordering claim.
var singleValuedKeys = []string{"problem", "repo", "worktree", "branch", "team", "mode", "standby", "epic"}

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

// Of parses iss's routing fields per the frozen rule. Single-valued keys
// (problem, repo, worktree, branch, team, mode, epic, standby) are
// first-occurrence-wins; the multi-valued session and track-worktree keys
// accumulate into Sessions and Tracks in registration order. Of scans the
// ENTIRE description, not just a leading header block — a field line (most
// commonly a session tie) can appear arbitrarily far down an arbitrarily
// long prose body (package doc comment, frozen item 2); Of must never be
// changed to stop early.
//
// Of takes the whole bd.Issue, not iss.Description, so that a future labels
// backend can read iss.Labels instead without changing this signature or any
// caller (package doc comment, "package surface").
func Of(iss bd.Issue) Fields {
	var f Fields
	seen := make(map[string]bool, len(singleValuedKeys))
	for _, line := range splitLines(iss.Description) {
		key, value, ok := fieldLine(line)
		if !ok {
			continue
		}
		switch key {
		case "session":
			f.Sessions = append(f.Sessions, value)
		case "track-worktree":
			f.Tracks = append(f.Tracks, value)
		case "problem", "repo", "worktree", "branch", "team", "mode", "epic", "standby":
			if seen[key] {
				continue
			}
			seen[key] = true
			switch key {
			case "problem":
				f.Problem = value
			case "repo":
				f.Repo = value
			case "worktree":
				f.Worktree = value
			case "branch":
				f.Branch = value
			case "team":
				f.Team = value
			case "mode":
				f.Mode = value
			case "epic":
				f.Epic = value
			case "standby":
				f.Standby = value == "true"
			}
		}
	}
	return f
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
		{"epic", []string{f.Epic}},
		{"session", f.Sessions},
		{"track-worktree", f.Tracks},
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
	write("epic", f.Epic)
	for _, s := range f.Sessions {
		write("session", s)
	}
	for _, t := range f.Tracks {
		write("track-worktree", t)
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
