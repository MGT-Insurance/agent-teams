package po

import (
	"regexp"
	"strings"
)

// Stamp line contract (doc.go item 8). Every PO note, review body and relayed
// note opens with one stamp line naming its writer:
//
//	po: note (not Eric's words): from <owner>/<agent> (internal|external)
//
// The stamp starts with CommentPrefix, so every reader that only checks the
// old opening words still matches. A note without a stamp is a legacy note:
// still recognised by its prefix, source unknown.
const (
	// StampKindInternal and StampKindExternal are the two writer kinds.
	StampKindInternal = "internal"
	StampKindExternal = "external"

	// RelayedByPrefix starts the LAST line the steward adds when it copies an
	// external note onto a run. Full line:
	// "relayed-by: steward (writer peer:<owner>/<agent>)".
	RelayedByPrefix = "relayed-by: steward (writer peer:"

	// ValueCheckFits and ValueCheckDoesNotFit are line 2 of a PO review body
	// (reviews only).
	ValueCheckFits       = "Value check: fits"
	ValueCheckDoesNotFit = "Value check: does not fit"

	// RequestChangesDrift and RequestChangesOneWayDoor are the optional line 3,
	// only on a request-changes review.
	RequestChangesDrift      = "Request changes: extreme drift"
	RequestChangesOneWayDoor = "Request changes: one-way door"
)

const stampFrom = " from "

// nameRe is a lowercase alphanumeric token run joined by "." or "-". It is
// used for both <owner> (the Dispatcher handle) and <agent>, and cannot match
// whitespace, newlines, "/", "(" or ")".
var nameRe = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

// ValidStampName reports whether s is a valid stamp owner or agent name.
func ValidStampName(s string) bool { return nameRe.MatchString(s) }

// FormatStamp returns the stamp line for owner/agent. It returns "" when owner
// or agent is not a valid name, so a bad value can never inject extra text
// into a stamp.
func FormatStamp(owner, agent string, external bool) string {
	if !ValidStampName(owner) || !ValidStampName(agent) {
		return ""
	}
	kind := StampKindInternal
	if external {
		kind = StampKindExternal
	}
	return CommentPrefix + stampFrom + owner + "/" + agent + " (" + kind + ")"
}

// ParseStamp parses a single stamp line (no trailing newline). ok is false for
// anything that is not exactly a well-formed stamp, including the bare legacy
// prefix, multi-line input and bad owner, agent or kind; it never errors.
func ParseStamp(line string) (owner, agent string, external bool, ok bool) {
	rest, found := strings.CutPrefix(line, CommentPrefix+stampFrom)
	if !found {
		return "", "", false, false
	}
	var kind string
	switch {
	case strings.HasSuffix(rest, " ("+StampKindInternal+")"):
		kind = StampKindInternal
	case strings.HasSuffix(rest, " ("+StampKindExternal+")"):
		kind = StampKindExternal
	default:
		return "", "", false, false
	}
	id := rest[:len(rest)-len(" ("+kind+")")]
	o, a, cut := strings.Cut(id, "/")
	if !cut || !ValidStampName(o) || !ValidStampName(a) {
		return "", "", false, false
	}
	return o, a, kind == StampKindExternal, true
}
