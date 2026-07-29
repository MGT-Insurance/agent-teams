package initiative

import (
	"fmt"
	"strings"
	"unicode"

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

// singleValuedKeys lists, in the order New writes them, every canonical key
// backed by a single-valued Fields member (i.e. excluding the multi-valued
// "session" and "track-worktree" keys).
var singleValuedKeys = []string{"problem", "repo", "worktree", "branch", "team", "mode", "standby", "epic"}

// fieldLine reports whether line satisfies the one frozen field-line rule
// (package doc comment, frozen item 1): start of line, an exact-lowercase
// key, a single colon, a single space, then a non-empty value. Any other
// shape — leading whitespace, a folded-case key, a missing or doubled space
// after the colon, an empty value — is not a field line at all and returns
// ok=false. This is the single primitive every reader and writer in this
// package builds on; there is no second matching rule anywhere else.
//
// The captured value is right-trimmed (DRI ruling on agent-teams-ully.5,
// 2026-07-29 02:04): "repo: /a/b" and "repo: /a/b   " resolve to the
// identical value "/a/b". Leading whitespace in the value is still
// structurally excluded by the single-space check above — unchanged. A
// value that is entirely whitespace right-trims to empty and is therefore
// not a field line either, per the same ruling.
func fieldLine(line string) (key, value string, ok bool) {
	colon := strings.IndexByte(line, ':')
	if colon <= 0 {
		return "", "", false
	}
	key = line[:colon]
	if key != strings.ToLower(key) || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	rest := line[colon+1:]
	if len(rest) < 2 || rest[0] != ' ' || rest[1] == ' ' {
		return "", "", false
	}
	value = strings.TrimRightFunc(rest[1:], unicode.IsSpace)
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
	for _, line := range strings.Split(iss.Description, "\n") {
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
func New(f Fields) WritePlan {
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
	return WritePlan{Description: b.String()}
}

// appendLine returns description with one new "key: value" line appended
// below its entire existing content, verbatim and untouched — the append-
// only mutation frozen item 4 requires. It never re-derives, reflows, or
// drops a single byte of what was already there.
func appendLine(description, key, value string) string {
	return strings.TrimRight(description, "\n") + "\n" + key + ": " + value + "\n"
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
// worktree path may legitimately contain spaces, so plain spaces are not
// rejected.
func WithTrack(iss bd.Issue, path string) (WritePlan, error) {
	if path == "" {
		return WritePlan{}, fmt.Errorf("initiative.WithTrack: path must not be empty")
	}
	if strings.ContainsAny(path, "\r\n") {
		return WritePlan{}, fmt.Errorf("initiative.WithTrack: path must not contain a line break: %q", path)
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
	for i, line := range strings.Split(bodyText, "\n") {
		key, _, ok := fieldLine(line)
		if !ok || !already[key] {
			continue
		}
		out = append(out, Collision{Line: i + 1, Key: key, Text: line})
	}
	return out
}
