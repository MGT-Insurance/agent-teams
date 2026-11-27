package initiative

import (
	"fmt"
	"regexp"
	"strings"
)

// GateClasses is the CLOSED gate-class set carried by the gate mail
// "class:" line and the gate-class label (docs/outsider-requests.md). Never
// extended without a contract bead.
var GateClasses = []string{"plan", "scope", "design-fork", "live-test", "delivery"}

// OriginOutsider is the only accepted value of the "origin" field.
const OriginOutsider = "outsider"

var requesterPattern = regexp.MustCompile(`^[a-z0-9]+([-.][a-z0-9]+)*/[a-z0-9]+([-.][a-z0-9]+)*$`)

// reviewerPattern is GitHub's login shape: alphanumeric tokens joined by single
// hyphens (so no leading, trailing, or doubled hyphen); length is checked apart.
var reviewerPattern = regexp.MustCompile(`^[A-Za-z0-9]+(-[A-Za-z0-9]+)*$`)

// validateOutsiderFields is New's write-side check of the outsider fields.
// Of stays lenient (it never rejects stored data).
func validateOutsiderFields(f Fields) error {
	if f.Origin != "" && f.Origin != OriginOutsider {
		return fmt.Errorf("initiative.New: origin must be %q, got %q", OriginOutsider, f.Origin)
	}
	if f.Requester != "" && !requesterPattern.MatchString(f.Requester) {
		return fmt.Errorf("initiative.New: requester must be <owner-handle>/<agent-name> (lowercase alphanumeric tokens joined by - or ., exactly one slash), got %q", f.Requester)
	}
	if strings.ContainsAny(f.Linear, " \t\r\n") {
		return fmt.Errorf("initiative.New: linear must be a single issue key with no whitespace, got %q", f.Linear)
	}
	if f.Reviewer != "" {
		if f.Origin != OriginOutsider {
			return fmt.Errorf("initiative.New: reviewer is valid only with origin %q", OriginOutsider)
		}
		if len(f.Reviewer) > 39 || !reviewerPattern.MatchString(f.Reviewer) {
			return fmt.Errorf("initiative.New: reviewer must be a GitHub login (alphanumeric or single hyphens, at most 39 chars, no leading or trailing hyphen), got %q", f.Reviewer)
		}
	}
	return nil
}
